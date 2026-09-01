import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, mkdir, readdir, symlink, truncate, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import test from 'node:test'
import {
  canonicalNameKey,
  expectedShard,
  importInventory,
  type ExistingCharacter,
  type ImportStore,
  type ImportTransaction,
} from '../src/inventory.js'
import { assertAdminDatabaseUrl, isRetryableTransactionError, withSerializationRetry } from '../src/postgres-store.js'
import { scanMudHome } from '../src/scanner.js'
import { parseArgs, readMetadataLines } from '../src/cli.js'

const execFileAsync = promisify(execFile)

const digest = (value: string) => createHash('sha256').update(value).digest('hex')

function inventoryLine(name: string, overrides: Record<string, unknown> = {}): string {
  const shard = expectedShard(name)
  return JSON.stringify({
    name,
    canonical_name_key: name,
    name_not_canonical: false,
    relative_path: `player/${shard}/${name}`,
    observed_shard: shard,
    expected_shard: shard,
    shard_match: true,
    valid_name: true,
    byte_size: 23,
    sha256: digest(`bytes:${name}`),
    status: 'ok',
    duplicate_name: false,
    ...overrides,
  })
}

function existing(name: string, hash = digest(`bytes:${name}`), overrides: Partial<ExistingCharacter> = {}): ExistingCharacter {
  return {
    legacyName: name,
    legacyNameKey: name,
    legacyShard: expectedShard(name),
    importedFileSha256: hash,
    lifecycle: 'imported_unclaimed',
    ownerUserId: null,
    storageFormat: 1,
    ...overrides,
  }
}

class MemoryStore implements ImportStore {
  readonly rows = new Map<string, ExistingCharacter>()
  inserts = 0
  private tail = Promise.resolve()

  async transaction<T>(work: (transaction: ImportTransaction) => Promise<T>): Promise<T> {
    // Serialize whole transactions like the production advisory lock for this deterministic concurrency test.
    let release: (() => void) | undefined
    const previous = this.tail
    this.tail = new Promise<void>((resolve) => { release = resolve })
    await previous
    try {
      return await work({
        lockIdentity: async () => undefined,
        findCharacter: async (world, name) => this.rows.get(`${world}|${name}`),
        insertImportedUnclaimed: async ({ worldId, record }) => {
          const key = `${worldId}|${record.canonicalNameKey}`
          if (this.rows.has(key)) throw new Error('duplicate insert')
          this.inserts++
          this.rows.set(key, existing(record.name, record.sha256))
        },
      })
    } finally {
      release?.()
    }
  }
}

test('dry run validates metadata but never inserts', async () => {
  const store = new MemoryStore()
  const summary = await importInventory(store, [inventoryLine('Alice')], { worldId: 'muhan', apply: false })
  assert.equal(summary.wouldInsert, 1)
  assert.equal(summary.inserted, 0)
  assert.equal(store.inserts, 0)
  assert.equal(summary.idempotent, 0)
})

test('apply inserts the exact canonical unclaimed identity and retry is idempotent', async () => {
  const store = new MemoryStore()
  const line = inventoryLine('Alice')
  const first = await importInventory(store, [line], { worldId: 'muhan', apply: true })
  const second = await importInventory(store, [line], { worldId: 'muhan', apply: true })
  assert.equal(first.inserted, 1)
  assert.equal(second.idempotent, 1)
  assert.equal(store.inserts, 1)
  assert.deepEqual(store.rows.get('muhan|Alice'), existing('Alice'))
})

test('concurrent applies produce one insert and one idempotent observation', async () => {
  const store = new MemoryStore()
  const line = inventoryLine('Alice')
  const [left, right] = await Promise.all([
    importInventory(store, [line], { worldId: 'muhan', apply: true }),
    importInventory(store, [line], { worldId: 'muhan', apply: true }),
  ])
  assert.equal(left.inserted + right.inserted, 1)
  assert.equal(left.idempotent + right.idempotent, 1)
  assert.equal(store.inserts, 1)
})

test('owned, lifecycle, hash, and identity conflicts are quarantined without mutation', async () => {
  const cases: Array<[string, Partial<ExistingCharacter>, string]> = [
    ['Owned', { ownerUserId: '00000000-0000-0000-0000-000000000001' }, 'owned_row'],
    ['Paused', { lifecycle: 'suspended' }, 'lifecycle_conflict'],
    ['Changed', { importedFileSha256: 'a'.repeat(64) }, 'hash_conflict'],
    ['Format', { storageFormat: 2 }, 'identity_conflict'],
  ]
  for (const [name, changes, reason] of cases) {
    const store = new MemoryStore()
    store.rows.set(`muhan|${name}`, existing(name, digest(`bytes:${name}`), changes))
    const before = JSON.stringify([...store.rows])
    const summary = await importInventory(store, [inventoryLine(name)], { worldId: 'muhan', apply: true })
    assert.equal(summary.quarantined[reason as keyof typeof summary.quarantined], 1)
    assert.equal(store.inserts, 0)
    assert.equal(JSON.stringify([...store.rows]), before)
  }
})

test('one quarantine rolls back the whole reviewed batch instead of partially inserting', async () => {
  const store = new MemoryStore()
  store.rows.set('muhan|Owned', existing('Owned', digest('bytes:Owned'), {
    ownerUserId: '00000000-0000-4000-8000-000000000001',
  }))
  const summary = await importInventory(store, [inventoryLine('Alice'), inventoryLine('Owned')], { worldId: 'muhan', apply: true })
  assert.equal(summary.quarantined.owned_row, 1)
  assert.equal(summary.inserted, 0)
  assert.equal(store.inserts, 0)
  assert.equal(store.rows.has('muhan|Alice'), false)

  const malformed = await importInventory(store, [inventoryLine('Bob'), '{not-json'], { worldId: 'muhan', apply: true })
  assert.equal(malformed.quarantined.invalid_metadata, 1)
  assert.equal(store.rows.has('muhan|Bob'), false)
})

test('adversarial lines are quarantined: raw fields, traversal, malformed hash, wrong shard, and duplicate identity', async () => {
  const alice = inventoryLine('Alice')
  const records = [
    inventoryLine('Raw', { password: 'do-not-accept' }),
    inventoryLine('Path', { relative_path: 'player/00/../Path' }),
    inventoryLine('Hash', { sha256: 'A'.repeat(64) }),
    inventoryLine('Shard', { expected_shard: '00', observed_shard: '00' }),
    alice,
    alice,
    inventoryLine('\ud800'),
  ]
  const summary = await importInventory(new MemoryStore(), records, { worldId: 'muhan', apply: true })
  assert.equal(summary.inserted, 0)
  assert.equal(summary.quarantined.invalid_metadata, 5)
  assert.equal(summary.quarantined.duplicate_input_identity, 2)
})

test('canonicalizer matches legacy ASCII casing and admin URL rejects JWT/service-role-like configuration', () => {
  assert.equal(canonicalNameKey('aLiCE'), 'Alice')
  assert.equal(canonicalNameKey('타봇'), '타봇')
  assert.equal(assertAdminDatabaseUrl('postgresql://inventory_admin:secret@db.internal:5432/postgres'), 'postgresql://inventory_admin:secret@db.internal:5432/postgres')
  for (const unsafe of [undefined, 'https://db.example/rest/v1', 'postgresql://service_role:x@db.internal/postgres', 'postgresql://admin:x@db.internal/postgres?apikey=no', 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature']) {
    assert.throws(() => assertAdminDatabaseUrl(unsafe))
  }
})

test('serialization retry retries 40001 once with a fixed bounded backoff', async () => {
  let attempts = 0
  const delays: number[] = []
  const result = await withSerializationRetry(async () => {
    attempts++
    if (attempts === 1) throw Object.assign(new Error('serialization failure'), { code: '40001' })
    return 'ok'
  }, { sleep: async (milliseconds) => { delays.push(milliseconds) } })
  assert.equal(result, 'ok')
  assert.equal(attempts, 2)
  assert.deepEqual(delays, [25])
  assert.equal(isRetryableTransactionError({ code: '40001' }), true)
})

test('serialization retry stops after three attempts', async () => {
  let attempts = 0
  await assert.rejects(
    () => withSerializationRetry(async () => {
      attempts++
      throw Object.assign(new Error('serialization failure'), { code: '40001' })
    }, { sleep: async () => undefined }),
    (error: unknown) => typeof error === 'object' && error !== null && 'code' in error && (error as { code?: unknown }).code === '40001',
  )
  assert.equal(attempts, 3)
})

test('serialization retry does not retry non-retryable errors', async () => {
  let attempts = 0
  const uniqueError = Object.assign(new Error('unique violation'), { code: '23505' })
  await assert.rejects(
    () => withSerializationRetry(async () => {
      attempts++
      throw uniqueError
    }, { sleep: async () => undefined }),
    (error) => error === uniqueError,
  )
  assert.equal(attempts, 1)
  assert.equal(isRetryableTransactionError({ code: '23505' }), false)
  assert.equal(isRetryableTransactionError(new Error('validation')), false)
})

test('CLI permits exactly one metadata source and keeps apply opt-in', () => {
  assert.deepEqual(parseArgs(['--mud-home', '/mnt/muhan']), { input: { kind: 'mud_home', path: '/mnt/muhan' }, worldId: 'muhan', apply: false })
  assert.deepEqual(parseArgs(['--inventory', '/secure/inventory.jsonl', '--world-id', 'staging', '--apply']), { input: { kind: 'inventory', path: '/secure/inventory.jsonl' }, worldId: 'staging', apply: true })
  for (const invalid of [[], ['--inventory', '/a', '--mud-home', '/b'], ['--mud-home'], ['--unknown']]) {
    assert.throws(() => parseArgs(invalid))
  }
})

test('JSONL metadata input rejects non-UTF-8 bytes instead of synthesizing names', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'muhan-importer-jsonl-'))
  t.after(async () => { await (await import('node:fs/promises')).rm(root, { recursive: true, force: true }) })
  const path = join(root, 'inventory.jsonl')
  await writeFile(path, Buffer.from([0x7b, 0xff, 0x7d, 0x0a]))
  await assert.rejects(() => readMetadataLines(path), /invalid import input/)
})

async function scannerFixture(): Promise<{ root: string, player: string, shard: string }> {
  const root = await mkdtemp(join(tmpdir(), 'muhan-importer-scan-'))
  const player = join(root, 'player')
  const shard = expectedShard('Alice')
  await mkdir(join(player, shard), { recursive: true })
  await writeFile(join(player, shard, 'Alice'), 'legacy-player-body')
  return { root, player, shard }
}

test('scanner deterministically emits only safe metadata records without exposing payload bytes', async (t) => {
  const fixture = await scannerFixture()
  t.after(async () => { await (await import('node:fs/promises')).rm(fixture.root, { recursive: true, force: true }) })
  const result = await scanMudHome(fixture.root)
  assert.equal(result.rejected, 0)
  assert.deepEqual(result.records, [{
    name: 'Alice',
    canonicalNameKey: 'Alice',
    relativePath: `player/${fixture.shard}/Alice`,
    observedShard: fixture.shard,
    expectedShard: fixture.shard,
    byteSize: Buffer.byteLength('legacy-player-body'),
    sha256: digest('legacy-player-body'),
  }])
  assert.doesNotMatch(JSON.stringify(result.records), /legacy-player-body/)
})

test('scanner rejects symlink root, shard, file, invalid entries, duplicate identities, and record caps', async (t) => {
  const fixture = await scannerFixture()
  t.after(async () => { await (await import('node:fs/promises')).rm(fixture.root, { recursive: true, force: true }) })
  const escapedRoot = `${fixture.root}-link`
  await symlink(fixture.root, escapedRoot)
  t.after(async () => { await (await import('node:fs/promises')).rm(escapedRoot, { force: true }) })
  assert.deepEqual(await scanMudHome(escapedRoot), { records: [], rejected: 1 })

  await symlink(join(fixture.player, fixture.shard, 'Alice'), join(fixture.player, fixture.shard, 'symlinked'))
  await symlink(join(fixture.player, fixture.shard), join(fixture.player, 'aa'))
  await mkdir(join(fixture.player, fixture.shard, 'directory-entry'))
  await writeFile(join(fixture.player, fixture.shard, 'bad:name'), 'invalid-name')
  const bobShard = expectedShard('Bob')
  await mkdir(join(fixture.player, bobShard), { recursive: true })
  await writeFile(join(fixture.player, bobShard, 'Bob'), 'another-player')
  const result = await scanMudHome(fixture.root)
  assert.equal(result.records.length, 2)
  assert.equal(result.rejected, 4)
  const capped = await scanMudHome(fixture.root, { maxRecords: 1 })
  assert.equal(capped.records.length, 0)
  assert.ok(capped.rejected > 0)
})

test('scanner enforces maxRecords across shards and discards partial results', async (t) => {
  const fixture = await scannerFixture()
  t.after(async () => { await (await import('node:fs/promises')).rm(fixture.root, { recursive: true, force: true }) })
  const bobShard = expectedShard('Bob')
  await mkdir(join(fixture.player, bobShard), { recursive: true })
  await writeFile(join(fixture.player, bobShard, 'Bob'), 'another-player')

  assert.deepEqual(await scanMudHome(fixture.root, { maxRecords: 1 }), { records: [], rejected: 1 })
})

test('scanner rejects oversized and nonregular files without blocking', async (t) => {
  const fixture = await scannerFixture()
  t.after(async () => { await (await import('node:fs/promises')).rm(fixture.root, { recursive: true, force: true }) })
  const large = join(fixture.player, fixture.shard, 'Large')
  await writeFile(large, '')
  await truncate(large, 64 * 1024 * 1024 + 1)
  const fifo = join(fixture.player, fixture.shard, 'Fifo')
  try {
    await execFileAsync('mkfifo', [fifo])
  } catch {
    t.skip('mkfifo unavailable on this platform')
    return
  }
  const result = await scanMudHome(fixture.root)
  assert.equal(result.records.length, 1)
  assert.equal(result.rejected, 2)
})

test('scanner quarantines both duplicate canonical identities on case-sensitive filesystems', async (t) => {
  const fixture = await scannerFixture()
  t.after(async () => { await (await import('node:fs/promises')).rm(fixture.root, { recursive: true, force: true }) })
  await writeFile(join(fixture.player, fixture.shard, 'ALICE'), 'case-variant')
  const names = await readdir(join(fixture.player, fixture.shard))
  if (!names.includes('Alice') || !names.includes('ALICE')) {
    t.skip('filesystem is case-insensitive')
    return
  }
  assert.deepEqual(await scanMudHome(fixture.root), { records: [], rejected: 2 })
})
