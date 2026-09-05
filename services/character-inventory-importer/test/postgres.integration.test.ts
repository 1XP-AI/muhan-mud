import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { createRequire } from 'node:module'
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'
import { createBatchIdentity } from '../src/batch-identity.js'
import { BatchImportError, expectedShard, importBatch, importRecords, type InventoryRecord } from '../src/inventory.js'
import { PostgresImportStore } from '../src/postgres-store.js'
import { scanMudHome } from '../src/scanner.js'

const require = createRequire(import.meta.url)
const { Pool } = require('pg') as { Pool: new (options: { connectionString: string, max: number }) => DisposablePool }

interface QueryResult<Row = Record<string, unknown>> { rows: Row[], rowCount?: number }
interface DisposableClient {
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<QueryResult<Row>>
  release(): void
}
interface DisposablePool {
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<QueryResult<Row>>
  connect(): Promise<DisposableClient>
  end(): Promise<void>
}

const allow = process.env.INVENTORY_E2E_ALLOW_DISPOSABLE === '1'
const skipReason = allow
  ? (process.platform === 'linux' ? false : 'requires Linux openat/O_NOFOLLOW filesystem semantics')
  : 'set INVENTORY_E2E_ALLOW_DISPOSABLE=1 to enable disposable Postgres integration tests'

function disposableDatabaseUrl(): string {
  const value = process.env.INVENTORY_E2E_DATABASE_URL ?? process.env.DATABASE_URL
  assert.ok(value, 'INVENTORY_E2E_DATABASE_URL or DATABASE_URL is required when integration tests are enabled')
  let url: URL
  try { url = new URL(value) } catch { throw new Error('integration DATABASE_URL must be a PostgreSQL URL') }
  assert.ok(url.protocol === 'postgres:' || url.protocol === 'postgresql:', 'integration DATABASE_URL must be PostgreSQL')
  assert.ok(['127.0.0.1', 'localhost', '::1', '[::1]'].includes(url.hostname), 'integration DATABASE_URL must target loopback')
  assert.ok(url.username && !/service_role|anon|authenticated/i.test(url.username), 'integration DB user must be disposable admin')
  assert.equal(url.search, '', 'integration DATABASE_URL must not carry credential-like query parameters')
  assert.doesNotMatch(value, /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/, 'integration DATABASE_URL must not contain a JWT')
  return value
}

function digest(value: string): string {
  return createHash('sha256').update(value).digest('hex')
}

function record(name: string, body = `player:${name}`): InventoryRecord {
  const shard = expectedShard(name)
  return {
    name,
    canonicalNameKey: name,
    relativePath: `player/${shard}/${name}`,
    observedShard: shard,
    expectedShard: shard,
    byteSize: Buffer.byteLength(body),
    sha256: digest(body),
  }
}

async function countWorld(pool: DisposablePool, world: string): Promise<number> {
  const result = await pool.query<{ count: string }>('select count(*)::text as count from public.game_characters where world_id = $1', [world])
  return Number(result.rows[0]?.count ?? 0)
}

async function countBatchMembers(pool: DisposablePool, world: string): Promise<number> {
  const result = await pool.query<{ count: string }>('select count(*)::text as count from private.game_imported_unclaimed_batch_members where world_id = $1', [world])
  return Number(result.rows[0]?.count ?? 0)
}

test('disposable Linux/Postgres importer contract is atomic and serializes retries', { skip: skipReason }, async (t) => {
  const databaseUrl = disposableDatabaseUrl()
  const pool = new Pool({ connectionString: databaseUrl, max: 2 })
  const world = `ci-importer-${process.pid}-${Date.now()}-${randomBytes(6).toString('hex')}`
  const root = await mkdtemp(join(tmpdir(), 'muhan-importer-e2e-'))
  const importer = new PostgresImportStore(databaseUrl)
  const concurrentLeft = new PostgresImportStore(databaseUrl)
  const concurrentRight = new PostgresImportStore(databaseUrl)
  t.after(async () => {
    // S4/S5a evidence is deliberately non-deletable. This guarded test uses
    // a per-run world identifier and expects its disposable database to be
    // discarded by its explicit runner rather than bypassing immutability.
    await Promise.all([importer.close(), concurrentLeft.close(), concurrentRight.close(), pool.end(), rm(root, { recursive: true, force: true })])
  })

  const alice = record('Alice', 'alice-player-body')
  const bob = record('Bob', 'bob-player-body')
  const player = join(root, 'player', alice.expectedShard)
  await mkdir(player, { recursive: true })
  await writeFile(join(player, 'Alice'), 'alice-player-body')
  await mkdir(join(root, 'player', bob.expectedShard), { recursive: true })
  await writeFile(join(root, 'player', bob.expectedShard, 'Bob'), 'bob-player-body')

  const scan = await scanMudHome(root)
  assert.equal(scan.rejected, 0)
  assert.deepEqual(scan.records.map((entry) => entry.name), ['Alice', 'Bob'])
  assert.doesNotMatch(JSON.stringify(scan.records), /alice-player-body|bob-player-body/)

  const dryRun = await importRecords(importer, scan.records, { worldId: world, apply: false })
  assert.equal(dryRun.wouldInsert, 2)
  assert.equal(dryRun.inserted, 0)
  assert.equal(await countWorld(pool, world), 0)

  const applied = await importRecords(importer, scan.records, { worldId: world, apply: true })
  assert.equal(applied.inserted, 2)
  assert.equal(applied.idempotent, 0)
  assert.equal(await countWorld(pool, world), 2)

  const retried = await importRecords(importer, scan.records, { worldId: world, apply: true })
  assert.equal(retried.inserted, 0)
  assert.equal(retried.idempotent, 2)
  assert.equal(await countWorld(pool, world), 2)

  const invalid = { ...record('Invalid'), sha256: 'not-a-sha256' }
  const invalidBatch = await importRecords(importer, [record('Freshinvalid'), invalid], { worldId: world, apply: true })
  assert.equal(invalidBatch.quarantined.invalid_metadata, 1)
  assert.equal(invalidBatch.inserted, 0)
  assert.equal(await countWorld(pool, world), 2)

  const conflict = record('Conflict', 'conflict-player-body')
  const fresh = record('Fresh', 'fresh-player-body')
  await pool.query(
    `insert into public.game_characters
       (world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format, imported_file_sha256)
     values ($1, $2, $2, $3, 'suspended', 1, $4)`,
    [world, conflict.name, conflict.expectedShard, conflict.sha256],
  )
  const conflictedBatch = await importRecords(importer, [fresh, conflict], { worldId: world, apply: true })
  assert.equal(conflictedBatch.quarantined.lifecycle_conflict, 1)
  assert.equal(conflictedBatch.inserted, 0)
  assert.equal(await countWorld(pool, world), 3)
  const freshRows = await pool.query('select 1 from public.game_characters where world_id = $1 and legacy_name_key = $2', [world, fresh.canonicalNameKey])
  assert.equal(freshRows.rowCount, 0)

  const concurrent = record('Concurrent', 'concurrent-player-body')
  const [left, right] = await Promise.all([
    importRecords(concurrentLeft, [concurrent], { worldId: world, apply: true }),
    importRecords(concurrentRight, [concurrent], { worldId: world, apply: true }),
  ])
  assert.equal(left.inserted + right.inserted, 1)
  assert.equal(left.idempotent + right.idempotent, 1)
  assert.equal(await countWorld(pool, world), 4)

  const ledgerIdentity = createBatchIdentity({
    worldId: world,
    sourceManifestId: 'integration-manifest-0',
    sourceSha256: digest('integration-source-0'),
    sourceByteSize: 20,
    parserVersion: '1.2.3',
    abi: 1,
    startMarker: 'range-start-0',
    endMarker: 'range-end-0',
  })
  const ledgerRecords = [record('LedgerOne'), record('LedgerTwo')]
  const batchApplied = await importBatch(importer, ledgerRecords, { identity: ledgerIdentity, streamId: 'main', sequence: 0, apply: true })
  assert.equal(batchApplied.inserted, 2)
  assert.equal(await countWorld(pool, world), 6)
  assert.equal(await countBatchMembers(pool, world), 2)
  const batchRetry = await importBatch(importer, ledgerRecords, { identity: ledgerIdentity, streamId: 'main', sequence: 0, apply: true })
  assert.equal(batchRetry.ledger, 'idempotent')
  assert.equal(await countWorld(pool, world), 6)
  assert.equal(await countBatchMembers(pool, world), 2)
  const secondBatch = await importBatch(importer, [ledgerRecords[0]!], {
    identity: createBatchIdentity({ worldId: world, sourceManifestId: 'integration-manifest-1', sourceSha256: digest('integration-source-1'), sourceByteSize: 20, parserVersion: '1.2.3', abi: 1, startMarker: 'range-start-1', endMarker: 'range-end-1' }),
    streamId: 'main', sequence: 1, apply: true,
  })
  assert.equal(secondBatch.idempotent, 1)
  assert.equal(await countBatchMembers(pool, world), 2)
  await assert.rejects(
    () => importBatch(importer, [record('LedgerThree')], {
      identity: createBatchIdentity({ worldId: world, sourceManifestId: 'integration-manifest-1', sourceSha256: digest('integration-source-1'), sourceByteSize: 20, parserVersion: '1.2.3', abi: 1, startMarker: 'range-start-1', endMarker: 'range-end-1' }),
      streamId: 'main', sequence: 0, apply: true,
    }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_identity_conflict',
  )
  await assert.rejects(
    () => importBatch(importer, [record('LedgerThree')], {
      identity: createBatchIdentity({ worldId: world, sourceManifestId: 'integration-manifest-2', sourceSha256: digest('integration-source-2'), sourceByteSize: 20, parserVersion: '1.2.3', abi: 1, startMarker: 'range-start-2', endMarker: 'range-end-2' }),
      streamId: 'main', sequence: 3, apply: true,
    }),
    (error: unknown) => error instanceof BatchImportError && error.code === 'batch_sequence_out_of_order',
  )
  const watermark = await pool.query<{ watermark_sequence: string, committed_batch_sequence: string }>(
    'select watermark_sequence::text, committed_batch_sequence::text from private.game_imported_unclaimed_batch_watermarks where world_id = $1 and stream_id = $2', [world, 'main'],
  )
  assert.deepEqual(watermark.rows[0], { watermark_sequence: '0', committed_batch_sequence: '0' })
})
