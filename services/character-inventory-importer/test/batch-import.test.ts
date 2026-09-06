import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import { createBatchIdentity } from '../src/batch-identity.js'
import { parseImportedUnclaimedManifest } from '../src/imported-unclaimed-manifest.js'
import { bindLegacyPlayerShadowEvidenceV1 } from '../src/legacy-player-shadow-binding.js'
import { BatchImportError, expectedShard, importBatch, importRecords, sameBatchMemberIdentity, type BatchMemberIdentity, type ExistingCharacter, type ImportStore, type ImportTransaction, type InventoryRecord } from '../src/inventory.js'
import { EXPECTED_LEGACY_PLAYER_FILE_SHA256, importerBindingFixture } from './legacy-identity-evidence-fixture.js'

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
  members = new Map<string, { worldId: string, streamId: string, sequence: number, characterId: string }>()
  memberIdentities = new Map<string, BatchMemberIdentity>()
  locators = new Map<string, { characterId: string, canonicalName: string, legacyNameSha1: string, legacyShard: string }>()
  watermarks = new Map<string, number>()
  writes = 0
  memberWrites = 0
  memberIdentityWrites = 0
  locatorWrites = 0
  batchInputs: Array<{ identity: ReturnType<typeof createBatchIdentity>, streamId: string, sequence: number, recordCount: number }> = []
  insertInputs: Array<{ worldId: string, record: InventoryRecord }> = []
  memberInputs: Array<{ worldId: string, streamId: string, sequence: number, characterId: string, legacyLocator?: { canonicalName: string, legacyNameSha1: string, legacyShard: string } }> = []
  memberIdentityInputs: Array<{ worldId: string, streamId: string, sequence: number, identity: BatchMemberIdentity }> = []
  locatorInputs: Array<{ characterId: string, canonicalName: string, legacyNameSha1: string, legacyShard: string }> = []
  events: string[] = []
  failInsert = false
  failMember = false
  failLocator = false
  private tail = Promise.resolve()

  constructor(private readonly insertedCharacterId = (worldId: string, row: InventoryRecord) => `character:${worldId}:${row.canonicalNameKey}`) {}

  async transaction<T>(work: (transaction: ImportTransaction) => Promise<T>): Promise<T> {
    let release: (() => void) | undefined
    const previous = this.tail
    this.tail = new Promise<void>((resolve) => { release = resolve })
    await previous
    const rows = new Map(this.rows)
    const batches = new Map(this.batches)
    const identities = new Map(this.identities)
    const members = new Map(this.members)
    const memberIdentities = new Map(this.memberIdentities)
    const locators = new Map(this.locators)
    const watermarks = new Map(this.watermarks)
    let writes = 0
    let memberWrites = 0
    let memberIdentityWrites = 0
    let locatorWrites = 0
    const batchInputs: typeof this.batchInputs = []
    const insertInputs: typeof this.insertInputs = []
    const memberInputs: typeof this.memberInputs = []
    const memberIdentityInputs: typeof this.memberIdentityInputs = []
    const locatorInputs: typeof this.locatorInputs = []
    const events: string[] = []
    try {
      const result = await work({
        lockIdentity: async () => undefined,
        findCharacter: async (world, name) => rows.get(`${world}|${name}`),
        insertImportedUnclaimed: async ({ worldId, record: row }) => {
          if (this.failInsert) throw new Error('injected insert failure')
          insertInputs.push({ worldId, record: { ...row } })
          rows.set(`${worldId}|${row.canonicalNameKey}`, existing(row)); writes++; events.push('character')
          return this.insertedCharacterId(worldId, row)
        },
        lockBatchStream: async () => undefined,
        findBatchBySequence: async (world, stream, sequence) => batches.get(`${world}|${stream}|${sequence}`),
        findBatchMemberIdentities: async (world, stream, sequence): Promise<readonly BatchMemberIdentity[]> => [...memberIdentities.entries()]
          .filter(([key]) => key.startsWith(`${world}|${stream}|${sequence}|`))
          .map(([, member]) => ({ ...member })),
        findBatchByIdentity: async (world, stream, stableKey) => identities.get(`${world}|${stream}|${stableKey}`),
        createBatch: async ({ identity: value, streamId, sequence, recordCount }) => {
          batchInputs.push({ identity: { ...value }, streamId, sequence, recordCount })
          const batch = { stableKey: value.stableKey, sequence, recordCount }
          batches.set(`${value.worldId}|${streamId}|${sequence}`, batch)
          identities.set(`${value.worldId}|${streamId}|${value.stableKey}`, batch)
          events.push('batch')
        },
        recordBatchMemberIdentity: async ({ worldId, streamId, sequence, identity: memberIdentity }) => {
          const key = `${worldId}|${streamId}|${sequence}|${memberIdentity.canonicalName}`
          memberIdentityInputs.push({ worldId, streamId, sequence, identity: { ...memberIdentity } })
          const previous = memberIdentities.get(key)
          if (previous) {
            if (previous.canonicalName !== memberIdentity.canonicalName || previous.shard !== memberIdentity.shard
              || previous.sha256 !== memberIdentity.sha256 || previous.storageFormat !== memberIdentity.storageFormat) {
              throw new Error('batch member identity conflict')
            }
            return
          }
          memberIdentities.set(key, { ...memberIdentity }); memberIdentityWrites++; events.push('member-identity')
        },
        recordBatchMember: async ({ worldId, streamId, sequence, characterId, legacyLocator }) => {
          if (this.failMember) throw new Error('injected member failure')
          memberInputs.push({ worldId, streamId, sequence, characterId, legacyLocator: legacyLocator && { ...legacyLocator } })
          const previousMember = members.get(characterId)
          if (previousMember) {
            if (previousMember.worldId !== worldId || previousMember.streamId !== streamId || previousMember.sequence !== sequence) throw new Error('batch member conflict')
          } else {
            members.set(characterId, { worldId, streamId, sequence, characterId }); memberWrites++; events.push('member')
          }
          if (legacyLocator) {
            if (this.failLocator) throw new Error('injected locator failure')
            locatorInputs.push({ characterId, ...legacyLocator })
            const previous = locators.get(characterId)
            if (previous) {
              if (previous.canonicalName !== legacyLocator.canonicalName || previous.legacyNameSha1 !== legacyLocator.legacyNameSha1 || previous.legacyShard !== legacyLocator.legacyShard) {
                throw new Error('legacy locator conflict')
              }
              return
            }
            locators.set(characterId, { characterId, ...legacyLocator }); locatorWrites++; events.push('locator')
          }
        },
        readWatermark: async (world, stream) => watermarks.get(`${world}|${stream}`),
        advanceWatermark: async (world, stream, sequence) => { watermarks.set(`${world}|${stream}`, sequence); events.push('watermark') },
      })
      this.rows = rows; this.batches = batches; this.identities = identities; this.members = members; this.memberIdentities = memberIdentities; this.locators = locators; this.watermarks = watermarks; this.writes += writes; this.memberWrites += memberWrites; this.memberIdentityWrites += memberIdentityWrites; this.locatorWrites += locatorWrites; this.batchInputs.push(...batchInputs); this.insertInputs.push(...insertInputs); this.memberInputs.push(...memberInputs); this.memberIdentityInputs.push(...memberIdentityInputs); this.locatorInputs.push(...locatorInputs); this.events = events
      return result
    } finally { release?.() }
  }
}

test('fixture-backed evidence binding reaches the exact batch, character, member, and locator write inputs', async () => {
  const { evidence, record: fixtureRecord } = importerBindingFixture()
  const binding = bindLegacyPlayerShadowEvidenceV1(fixtureRecord, evidence)
  assert.ok(binding)
  const store = new BatchMemoryStore()
  const batchIdentity = identity('fixture-evidence')

  const result = await importBatch(store, [fixtureRecord], { identity: batchIdentity, streamId: 'main', sequence: 0, apply: true })

  assert.equal(result.ledger, 'committed')
  assert.deepEqual(store.batchInputs, [{ identity: batchIdentity, streamId: 'main', sequence: 0, recordCount: 1 }])
  assert.deepEqual(store.insertInputs, [{ worldId: batchIdentity.worldId, record: fixtureRecord }])
  assert.equal(store.insertInputs[0]?.record.sha256, EXPECTED_LEGACY_PLAYER_FILE_SHA256)
  assert.equal(store.insertInputs[0]?.record.sha256, evidence.playerFileSha256)
  assert.deepEqual(store.memberIdentityInputs, [{
    worldId: batchIdentity.worldId,
    streamId: 'main',
    sequence: 0,
    identity: { canonicalName: fixtureRecord.canonicalNameKey, shard: fixtureRecord.expectedShard, sha256: fixtureRecord.sha256, storageFormat: 1 },
  }])
  assert.deepEqual(store.memberInputs, [{
    worldId: batchIdentity.worldId,
    streamId: 'main',
    sequence: 0,
    characterId: `character:${batchIdentity.worldId}:${fixtureRecord.canonicalNameKey}`,
    legacyLocator: {
      canonicalName: binding.canonicalName,
      legacyNameSha1: binding.nameSha1,
      legacyShard: binding.shard,
    },
  }])
})

test('fixture-derived reviewed manifest commits an exact resolver-compatible legacy character evidence chain', async () => {
  const { evidence, record: fixtureRecord } = importerBindingFixture()
  const worldId = 'legacy-evidence-world'
  const streamId = 'legacy-evidence'
  const reviewedCandidate = {
    legacyNameKey: 'Alice',
    legacyShard: '35',
    sourceSha256: EXPECTED_LEGACY_PLAYER_FILE_SHA256,
    sourceSize: 1,
  }
  const reviewedManifest = Buffer.from(JSON.stringify({
    format: 'muhan.imported_unclaimed_manifest',
    format_version: 1,
    dry_run: true,
    candidates: [{
      legacy_name_key: reviewedCandidate.legacyNameKey,
      legacy_shard: reviewedCandidate.legacyShard,
      source_sha256: reviewedCandidate.sourceSha256,
      source_size: reviewedCandidate.sourceSize,
    }],
    rejections: [],
  }))
  const manifest = parseImportedUnclaimedManifest(reviewedManifest)
  const reviewedBatchIdentity = {
    worldId,
    sourceManifestId: 'legacy-identity-evidence-v1',
    sourceSha256: manifest.sourceManifestSha256,
    sourceByteSize: reviewedManifest.byteLength,
    parserVersion: '1.0.0',
    abi: 1,
    startMarker: 'legacy:alice:0',
    endMarker: 'legacy:alice:1',
  }
  const batchIdentity = createBatchIdentity(reviewedBatchIdentity)
  const importedCharacterId = '7e4316c8-0742-4f22-8a6b-cd9e877a0b7f'
  const store = new BatchMemoryStore(() => importedCharacterId)

  const result = await importBatch(store, [fixtureRecord], { identity: batchIdentity, streamId, sequence: 0, apply: true })

  assert.equal(result.ledger, 'committed')
  assert.deepEqual(manifest, {
    sourceManifestSha256: digest(reviewedManifest.toString('utf8')),
    candidates: [reviewedCandidate],
  })
  assert.deepEqual(manifest.candidates, [{
    legacyNameKey: fixtureRecord.canonicalNameKey,
    legacyShard: fixtureRecord.expectedShard,
    sourceSha256: fixtureRecord.sha256,
    sourceSize: fixtureRecord.byteSize,
  }])
  assert.deepEqual(store.batchInputs, [{
    identity: {
      ...reviewedBatchIdentity,
      canonicalSerialization: JSON.stringify(reviewedBatchIdentity),
      stableKey: JSON.stringify(reviewedBatchIdentity),
    },
    streamId,
    sequence: 0,
    recordCount: 1,
  }])
  assert.deepEqual(store.insertInputs, [{ worldId, record: fixtureRecord }])
  assert.deepEqual(store.rows, new Map([[
    `${worldId}|Alice`,
    {
      legacyName: 'Alice',
      legacyNameKey: 'Alice',
      legacyShard: '35',
      importedFileSha256: EXPECTED_LEGACY_PLAYER_FILE_SHA256,
      lifecycle: 'imported_unclaimed',
      ownerUserId: null,
      storageFormat: 1,
    },
  ]]))
  assert.deepEqual(store.members, new Map([[
    importedCharacterId,
    { worldId, streamId, sequence: 0, characterId: importedCharacterId },
  ]]))
  assert.deepEqual(store.locators, new Map([[
    importedCharacterId,
    {
      characterId: importedCharacterId,
      canonicalName: 'Alice',
      legacyNameSha1: createHash('sha1').update('Alice', 'utf8').digest('hex'),
      legacyShard: '35',
    },
  ]]))
  assert.deepEqual(store.memberInputs, [{
    worldId,
    streamId,
    sequence: 0,
    characterId: importedCharacterId,
    legacyLocator: {
      canonicalName: reviewedCandidate.legacyNameKey,
      legacyNameSha1: createHash('sha1').update(reviewedCandidate.legacyNameKey, 'utf8').digest('hex'),
      legacyShard: reviewedCandidate.legacyShard,
    },
  }])
  assert.deepEqual(store.locatorInputs, [{
    characterId: importedCharacterId,
    canonicalName: reviewedCandidate.legacyNameKey,
    legacyNameSha1: createHash('sha1').update(reviewedCandidate.legacyNameKey, 'utf8').digest('hex'),
    legacyShard: reviewedCandidate.legacyShard,
  }])

  const character = store.rows.get(`${worldId}|Alice`)!
  assert.deepEqual(
    { worldId, canonicalName: character.legacyNameKey },
    { worldId: 'legacy-evidence-world', canonicalName: 'Alice' },
  )
  assert.equal(character.legacyName, evidence.canonicalName)
  assert.equal(character.legacyShard, evidence.legacyShard)
})

test('batch import commits every new character as an immutable member of its exact ledger batch before the watermark', async () => {
  const store = new BatchMemoryStore()
  const result = await importBatch(store, [record('Alice'), record('Bob')], { identity: identity(), streamId: 'main', sequence: 0, apply: true })
  assert.equal(result.inserted, 2)
  assert.equal(result.ledger, 'committed')
  assert.equal(store.writes, 2)
  assert.equal(store.batches.size, 1)
  assert.deepEqual([...store.members.values()], [
    { worldId: 'batch-world', streamId: 'main', sequence: 0, characterId: 'character:batch-world:Alice' },
    { worldId: 'batch-world', streamId: 'main', sequence: 0, characterId: 'character:batch-world:Bob' },
  ])
  assert.deepEqual([...store.locators.values()], [
    { characterId: 'character:batch-world:Alice', canonicalName: 'Alice', legacyNameSha1: createHash('sha1').update('Alice').digest('hex'), legacyShard: expectedShard('Alice') },
    { characterId: 'character:batch-world:Bob', canonicalName: 'Bob', legacyNameSha1: createHash('sha1').update('Bob').digest('hex'), legacyShard: expectedShard('Bob') },
  ])
  assert.deepEqual(store.events, ['batch', 'member-identity', 'member-identity', 'character', 'member', 'locator', 'character', 'member', 'locator', 'watermark'])
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
  assert.equal(store.memberWrites, 1)
  assert.equal(store.memberIdentityWrites, 1)
  assert.equal(store.locatorWrites, 1)
  assert.equal(store.members.size, 1)
  assert.equal(store.locators.size, 1)
  assert.equal(store.batches.size, 1)
})

test('a pre-tuple historic ledger batch fails closed instead of accepting an unverifiable exact replay', async () => {
  const store = new BatchMemoryStore()
  const value = identity()
  await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  store.memberIdentities.clear()

  await assert.rejects(
    () => importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_identity_conflict',
  )
  assert.equal(store.writes, 1)
  assert.equal(store.members.size, 1)
  assert.equal(store.watermarks.get('batch-world|main'), 0)
})

test('exact retry uses the durable tuple after its imported character has live lifecycle and storage drift', async () => {
  const store = new BatchMemoryStore()
  const value = identity()
  await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  const row = store.rows.get('batch-world|Alice')!
  row.lifecycle = 'claimed'
  row.ownerUserId = 'owner'
  row.storageFormat = 2
  row.importedFileSha256 = digest('repacked')

  const retry = await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  assert.equal(retry.ledger, 'idempotent')
  assert.equal(store.memberIdentityWrites, 1)
})

test('a new batch fails closed on live lifecycle or storage drift even though its tuple evidence remains immutable', async () => {
  for (const mutate of [
    (row: ExistingCharacter) => { row.lifecycle = 'claimed'; row.ownerUserId = 'owner' },
    (row: ExistingCharacter) => { row.storageFormat = 2 },
  ]) {
    const store = new BatchMemoryStore()
    await importBatch(store, [record('Alice')], { identity: identity('0'), streamId: 'main', sequence: 0, apply: true })
    mutate(store.rows.get('batch-world|Alice')!)

    const result = await importBatch(store, [record('Alice')], { identity: identity('1'), streamId: 'main', sequence: 1, apply: true })
    assert.equal(result.inserted, 0)
    assert.equal(Object.values(result.quarantined).reduce((total, count) => total + count, 0), 1)
    assert.equal(store.batches.size, 1)
    assert.equal(store.memberIdentities.size, 1)
    assert.equal(store.watermarks.get('batch-world|main'), 0)
  }
})

test('same batch identity rejects a retry whose canonical file tuple differs', async () => {
  const store = new BatchMemoryStore()
  const value = identity()
  await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  await assert.rejects(
    () => importBatch(store, [{ ...record('Alice'), sha256: digest('different-file') }], { identity: value, streamId: 'main', sequence: 0, apply: true }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_identity_conflict',
  )
  assert.equal(store.writes, 1)
  assert.equal(store.members.size, 1)
})

test('same batch identity rejects a retry when the committed record count drifts', async () => {
  const store = new BatchMemoryStore()
  const value = identity()
  await importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true })
  store.batches.get('batch-world|main|0')!.recordCount = 2
  await assert.rejects(
    () => importBatch(store, [record('Alice')], { identity: value, streamId: 'main', sequence: 0, apply: true }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_identity_conflict',
  )
})

test('same batch identity rejects a retry when immutable tuple rows are not bijective with admitted records', async () => {
  const store = new BatchMemoryStore()
  const value = identity()
  const candidate = record('Alice')
  await importBatch(store, [candidate], { identity: value, streamId: 'main', sequence: 0, apply: true })
  store.memberIdentities.set('batch-world|main|0|duplicate', {
    canonicalName: candidate.canonicalNameKey,
    shard: candidate.expectedShard,
    sha256: candidate.sha256,
    storageFormat: 1,
  })
  await assert.rejects(
    () => importBatch(store, [candidate], { identity: value, streamId: 'main', sequence: 0, apply: true }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_identity_conflict',
  )
})

test('batch member identity is an exact canonical name, shard, SHA-256, and format tuple', () => {
  const candidate = record('Alice')
  const valid: BatchMemberIdentity = {
    canonicalName: candidate.canonicalNameKey,
    shard: candidate.expectedShard,
    sha256: candidate.sha256,
    storageFormat: 1,
  }
  assert.equal(sameBatchMemberIdentity(candidate, valid), true)
  for (const invalid of [
    { ...valid, canonicalName: 'Bob' },
    { ...valid, shard: '00' },
    { ...valid, sha256: digest('different-file') },
    { ...valid, storageFormat: 2 },
  ]) assert.equal(sameBatchMemberIdentity(candidate, invalid), false)
})

test('an already idempotent character is never retrospectively targeted to a later batch', async () => {
  const store = new BatchMemoryStore()
  await importBatch(store, [record('Alice')], { identity: identity('0'), streamId: 'main', sequence: 0, apply: true })
  const second = await importBatch(store, [record('Alice')], { identity: identity('1'), streamId: 'main', sequence: 1, apply: true })
  assert.equal(second.inserted, 0)
  assert.equal(second.idempotent, 1)
  assert.equal(store.members.size, 1)
  assert.equal(store.memberIdentities.size, 2)
  assert.equal(store.locators.size, 1)
  assert.deepEqual(store.members.get('character:batch-world:Alice'), {
    worldId: 'batch-world', streamId: 'main', sequence: 0, characterId: 'character:batch-world:Alice',
  })
  const retry = await importBatch(store, [record('Alice')], { identity: identity('1'), streamId: 'main', sequence: 1, apply: true })
  assert.equal(retry.ledger, 'idempotent')
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
  assert.equal(store.members.size, 0)
  assert.equal(store.memberIdentities.size, 0)
  assert.equal(store.locators.size, 0)
  assert.equal(store.watermarks.size, 0)
})

test('member-write failure rolls back the ledger, inserted character, member, and watermark together', async () => {
  const store = new BatchMemoryStore()
  store.failMember = true
  await assert.rejects(() => importBatch(store, [record('Alice')], { identity: identity(), streamId: 'main', sequence: 0, apply: true }))
  assert.equal(store.rows.size, 0)
  assert.equal(store.batches.size, 0)
  assert.equal(store.members.size, 0)
  assert.equal(store.memberIdentities.size, 0)
  assert.equal(store.locators.size, 0)
  assert.equal(store.watermarks.size, 0)
})

test('locator-write failure rolls back the ledger, character, member, and watermark together', async () => {
  const store = new BatchMemoryStore()
  store.failLocator = true
  await assert.rejects(() => importBatch(store, [record('Alice')], { identity: identity(), streamId: 'main', sequence: 0, apply: true }))
  assert.equal(store.rows.size, 0)
  assert.equal(store.batches.size, 0)
  assert.equal(store.members.size, 0)
  assert.equal(store.memberIdentities.size, 0)
  assert.equal(store.locators.size, 0)
  assert.equal(store.watermarks.size, 0)
})

test('dry-run, quarantine, and non-batch import remain provenance-member free', async () => {
  const store = new BatchMemoryStore()
  const dryRun = await importBatch(store, [record('Alice')], { identity: identity(), streamId: 'main', sequence: 0, apply: false })
  assert.equal(dryRun.wouldInsert, 1)
  const quarantined = await importBatch(store, [{ ...record('Bob'), sha256: 'invalid' }], { identity: identity(), streamId: 'main', sequence: 0, apply: true })
  assert.equal(quarantined.quarantined.invalid_metadata, 1)
  await importRecords(store, [record('Carol')], { worldId: 'batch-world', apply: true })
  assert.equal(store.members.size, 0)
  assert.equal(store.memberIdentities.size, 0)
  assert.equal(store.locators.size, 0)
  assert.equal(store.batches.size, 0)
  assert.equal(store.watermarks.size, 0)
  assert.equal(store.rows.size, 1)
})

test('a batch-member locator is exact-retry idempotent and rejects a conflicting three-field locator', async () => {
  const store = new BatchMemoryStore()
  const candidate = record('Alice')
  const locator = {
    characterId: 'member:Alice',
    canonicalName: candidate.canonicalNameKey,
    legacyNameSha1: createHash('sha1').update(candidate.canonicalNameKey, 'utf8').digest('hex'),
    legacyShard: candidate.expectedShard,
  }
  const member = { worldId: 'batch-world', streamId: 'main', sequence: 0, characterId: locator.characterId }
  await store.transaction((transaction) => transaction.recordBatchMember({ ...member, legacyLocator: locator }))
  await store.transaction((transaction) => transaction.recordBatchMember({ ...member, legacyLocator: locator }))
  assert.equal(store.locatorWrites, 1)
  assert.equal(store.memberWrites, 1)
  await assert.rejects(
    () => store.transaction((transaction) => transaction.recordBatchMember({ ...member, legacyLocator: { ...locator, legacyShard: '00' } })),
    /legacy locator conflict/,
  )
  assert.deepEqual(store.locators.get(locator.characterId), locator)
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
