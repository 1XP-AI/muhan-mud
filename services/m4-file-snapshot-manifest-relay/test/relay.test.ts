import assert from 'node:assert/strict'
import { chmod, link, mkdtemp, rm, writeFile } from 'node:fs/promises'
import { test } from 'node:test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { parseManifest } from '../src/manifest.js'
import { NodeManifestFilesystem, relayOnce, type ManifestFilesystem } from '../src/relay.js'
import { PostgresManifestStore, assertDatabaseUrl, type ManifestStore, type PgClient, type PgPool } from '../src/store.js'

const first = '11111111-1111-4111-8111-111111111111'
const second = '22222222-2222-4222-8222-222222222222'

function body(commandId: string, revision = '1'): Uint8Array {
  const value = `version=1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${commandId}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\npost_sha256=${'b'.repeat(64)}\nwriter_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb\nsnapshot_format=legacy-file-manifest-v1\nwriter_epoch=7\nwriter_revision=${revision}\nstorage_format=1\nsnapshot_octets=128\n`
  return Buffer.from(value)
}

test('parses the exact thirteen-line canonical encoding', () => {
  const manifest = parseManifest(body(first))
  assert.equal(manifest.commandId, first)
  assert.equal(manifest.snapshotOctets, '128')
  assert.throws(() => parseManifest(Buffer.from(`${body(first).toString()}\n`)))
  assert.throws(() => parseManifest(Buffer.from(body(first).toString().replace('writer_revision=1', 'writer_revision=01'))))
})

test('relays only recorded and exact-retry outcomes in lexical order', async () => {
  const calls: string[] = []
  const fs: ManifestFilesystem = { scan: async () => [
    { name: `${second}.manifest`, bytes: body(second, '2') },
    { name: 'bad.manifest', bytes: body(first) },
    { name: `${first}.manifest`, bytes: body(first) },
    { name: 'unsafe.manifest', error: 'invalid' },
    { name: 'io.manifest', error: 'io' },
  ] }
  const store: ManifestStore = {
    recordManifest: async (manifest) => {
      calls.push(manifest.commandId)
      return manifest.commandId === first ? 'RECORDED' : 'EXACT_RETRY'
    },
  }
  const result = await relayOnce('/ignored', store, fs)
  assert.deepEqual(calls, [first, second])
  assert.deepEqual(result, { visited: 5, valid: 2, delivered: 2, recorded: 1, exactRetry: 1, invalid: 2, conflict: 0, retryable: 0, unknown: 0, ioError: 1 })
})

test('classifies SQLSTATE without retrying or leaking evidence', async () => {
  const fs: ManifestFilesystem = { scan: async () => [
    { name: `${first}.manifest`, bytes: body(first) },
    { name: `${second}.manifest`, bytes: body(second) },
  ] }
  let calls = 0
  const store: ManifestStore = { recordManifest: async () => {
    calls++
    const error = Object.assign(new Error('db'), { code: calls === 1 ? '22023' : '08P01' })
    throw error
  } }
  const result = await relayOnce('/ignored', store, fs)
  assert.equal(calls, 2)
  assert.deepEqual(result, { visited: 2, valid: 2, delivered: 0, recorded: 0, exactRetry: 0, invalid: 1, conflict: 0, retryable: 1, unknown: 0, ioError: 0 })
  assert.equal(Object.keys(result).some((key) => /path|hash|name|payload/i.test(key)), false)
})

test('filesystem rejects unsafe links and preserves every evidence file', async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-relay-'))
  const filename = `${first}.manifest`
  const original = Buffer.from(body(first))
  try {
    await chmod(root, 0o700)
    await writeFile(join(root, filename), original, { mode: 0o600 })
    const safe = await new NodeManifestFilesystem().scan(root)
    assert.equal(safe.length, 1)
    assert.equal(safe[0]?.bytes?.length, original.length)
    await link(join(root, filename), join(root, 'hardlink.manifest'))
    const files = await new NodeManifestFilesystem().scan(root)
    assert.equal(files.length, 2)
    assert.equal(files.find((file) => file.name === filename)?.error, 'invalid')
    assert.equal(files.find((file) => file.name === 'hardlink.manifest')?.error, 'invalid')
    assert.deepEqual(await import('node:fs/promises').then(({ readFile }) => readFile(join(root, filename))), original)
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('postgres adapter uses SET ROLE then one parameterized M4 function call', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const client: PgClient = {
    query: async <Row>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      return { rows: sql.startsWith('select outcome') ? [{ outcome: 'RECORDED' } as Row] : [] }
    },
    release: () => undefined,
  }
  const pool: PgPool = { connect: async () => client, end: async () => undefined }
  const store = new PostgresManifestStore(
    'postgresql://mud_writer_login@localhost/postgres', pool,
  )
  const manifest = parseManifest(body(first))
  assert.equal(await store.recordManifest(manifest), 'RECORDED')
  assert.deepEqual(queries.map((query) => query.sql), [
    'set role mud_writer',
    'select outcome from private.record_m4_file_snapshot_manifest_for_receipt($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::bigint)',
  ])
  assert.deepEqual(queries[1]?.values, [manifest.characterId, manifest.commandId, manifest.requestSha256, manifest.snapshotFormat, manifest.postSha256, manifest.snapshotOctets])
  assert.throws(() => assertDatabaseUrl('https://example.test/rest'))
  assert.throws(() => assertDatabaseUrl('postgresql://service_role@localhost/postgres'))
})
