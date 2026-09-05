import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import { createBatchIdentity } from '../src/batch-identity.js'
import { BatchImportError, expectedShard, importBatch, type ExistingCharacter, type ImportStore, type ImportTransaction, type InventoryRecord } from '../src/inventory.js'

const digest = (value: string) => createHash('sha256').update(value).digest('hex')

function identity(suffix = '0') {
  return createBatchIdentity({
    worldId: 'batch-world', sourceManifestId: `manifest-${suffix}`,
    sourceSha256: digest(`source-${suffix}`), sourceByteSize: 12,
    parserVersion: '1.2.3', abi: 1, startMarker: `start-${suffix}`, endMarker: `end-${suffix}`,
  })
}

function record(name: string): InventoryRecord {
  const shard = expectedShard(name)
  return { name, canonicalNameKey: name, relativePath: `player/${shard}/${name}`, observedShard: shard, expectedShard: shard, byteSize: 1, sha256: digest(name) }
}

function existing(recordValue: InventoryRecord): ExistingCharacter {
  return { legacyName: recordValue.name, legacyNameKey: recordValue.name, legacyShard: recordValue.expectedShard, importedFileSha256: recordValue.sha256, lifecycle: 'imported_unclaimed', ownerUserId: null, storageFormat: 1 }
}

class BatchMemoryStore implements ImportStore {
  rows = new Map<string, ExistingCharacter>()
  batches = new Map<string, { stableKey: string, sequence: number, recordCount: number }>()
  identities = new Map<string, { stableKey: string, sequence: number, recordCount: number }>()
  watermarks = new Map<string, number>()
  writes = 0
  failInsert = false
  private tail = Promise.resolve()

  async transaction<T>(work: (transaction: ImportTransaction) => Promise<T>): Promise<T> {
    let release: (() => void) | undefined
    const previous = this.tail
    this.tail = new Promise<void>((resolve) => { release = resolve })
    await previous
    const rows = new Map(this.rows)
    const batches = new Map(this.batches)
    const identities = new Map(this.identities)
    const watermarks = new Map(this.watermarks)
    let writes = 0
    try {
      const result = await work({
        lockIdentity: async () => undefined,
        findCharacter: async (world, name) => rows.get(`${world}|${name}`),
        insertImportedUnclaimed: async ({ worldId, record: row }) => {
          if (this.failInsert) throw new Error('injected insert failure')
          rows.set(`${worldId}|${row.canonicalNameKey}`, existing(row)); writes++
        },
        lockBatchStream: async () => undefined,
        findBatchBySequence: async (world, stream, sequence) => batches.get(`${world}|${stream}|${sequence}`),
        findBatchByIdentity: async (world, stream, stableKey) => identities.get(`${world}|${stream}|${stableKey}`),
        createBatch: async ({ identity: value, streamId, sequence, recordCount }) => {
          const batch = { stableKey: value.stableKey, sequence, recordCount }
          batches.set(`${value.worldId}|${streamId}|${sequence}`, batch)
          identities.set(`${value.worldId}|${streamId}|${value.stableKey}`, batch)
        },
        readWatermark: async (world, stream) => watermarks.get(`${world}|${stream}`),
        advanceWatermark: async (world, stream, sequence) => { watermarks.set(`${world}|${stream}`, sequence) },
      })
      this.rows = rows; this.batches = batches; this.identities = identities; this.watermarks = watermarks; this.writes += writes
      return result
    } finally { release?.() }
  }
}

test('batch import commits characters, immutable evidence, then a stream watermark', async () => {
  const store = new BatchMemoryStore()
  const result = await importBatch(store, [record('Alice'), record('Bob')], { identity: identity(), streamId: 'main', sequence: 0, apply: true })
  assert.equal(result.inserted, 2)
  assert.equal(result.ledger, 'committed')
  assert.equal(store.writes, 2)
  assert.equal(store.batches.size, 1)
  assert.equal(store.watermarks.get('batch-world|main'), 0)
})

test('exact identity and sequence retry is ledger-idempotent with no character writes', async () => {
  const store = new BatchMemoryStore()
  const value = identity()
  await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  const retry = await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  assert.equal(retry.ledger, 'idempotent')
  assert.equal(retry.idempotent, 1)
  assert.equal(store.writes, 1)
  assert.equal(store.batches.size, 1)
})

test('changed identity at an occupied sequence rejects without ledger, character, or watermark mutation', async () => {
  const store = new BatchMemoryStore()
  await importBatch(store, [record('Alice')], { identity: identity('0'), streamId: 'main', sequence: 0, apply: true })
  const before = JSON.stringify({ rows: [...store.rows], batches: [...store.batches], watermarks: [...store.watermarks] })
  await assert.rejects(() => importBatch(store, [record('Bob')], { identity: identity('1'), streamId: 'main', sequence: 0, apply: true }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_identity_conflict')
  assert.equal(JSON.stringify({ rows: [...store.rows], batches: [...store.batches], watermarks: [...store.watermarks] }), before)
})

test('failure rolls back batch evidence and watermark with character inserts', async () => {
  const store = new BatchMemoryStore()
  store.failInsert = true
  await assert.rejects(() => importBatch(store, [record('Alice')], { identity: identity(), streamId: 'main', sequence: 0, apply: true }))
  assert.equal(store.rows.size, 0)
  assert.equal(store.batches.size, 0)
  assert.equal(store.watermarks.size, 0)
})

test('an overlong batch world ID is rejected before any ledger, character, or watermark mutation', async () => {
  const store = new BatchMemoryStore()
  const valid = identity()
  const overlongIdentity = { ...valid, worldId: 'w'.repeat(65) }
  const before = JSON.stringify({ rows: [...store.rows], batches: [...store.batches], watermarks: [...store.watermarks] })

  await assert.rejects(
    () => importBatch(store, [record('Alice')], { identity: overlongIdentity, streamId: 'main', sequence: 0, apply: true }),
    /invalid batch identity/,
  )

  assert.equal(JSON.stringify({ rows: [...store.rows], batches: [...store.batches], watermarks: [...store.watermarks] }), before)
})

test('watermarks are per stream, require strict contiguous sequences, and concurrent retry writes once', async () => {
  const store = new BatchMemoryStore()
  await assert.rejects(() => importBatch(store, [record('Alice')], { identity: identity('1'), streamId: 'main', sequence: 1, apply: true }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_out_of_order')
  await importBatch(store, [record('Alice')], { identity: identity('0'), streamId: 'main', sequence: 0, apply: true })
  await importBatch(store, [record('Bob')], { identity: identity('1'), streamId: 'main', sequence: 1, apply: true })
  await importBatch(store, [record('Alice')], { identity: identity('2'), streamId: 'recovery', sequence: 0, apply: true })
  assert.equal(store.watermarks.get('batch-world|main'), 1)
  assert.equal(store.watermarks.get('batch-world|recovery'), 0)

  const concurrent = new BatchMemoryStore()
  const value = identity('9')
  const [left, right] = await Promise.all([
    importBatch(concurrent, [record('Concurrent')], { identity: value, streamId: 'main', sequence: 0, apply: true }),
    importBatch(concurrent, [record('Concurrent')], { identity: value, streamId: 'main', sequence: 0, apply: true }),
  ])
  assert.equal(left.inserted + right.inserted, 1)
  assert.equal(left.idempotent + right.idempotent, 1)
  assert.equal(concurrent.writes, 1)
})
