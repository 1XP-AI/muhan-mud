import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import { parseManifest } from '../src/manifest.js'
import { InvalidBankSnapshotV1ArtifactError, parseBankSnapshotV1Artifact } from '../src/bank-snapshot-v1-artifact.js'
import { relayBankSnapshotV1RootValueShadowsOnce } from '../src/bank-snapshot-v1-root-value-shadow-relay.js'
import { PostgresBankSnapshotV1RootValueShadowStore } from '../src/store.js'

const id = '11111111-1111-4111-8111-111111111111'
function be16(n: number) { const b = Buffer.alloc(2); b.writeUInt16BE(n); return b }
function be32(n: number) { const b = Buffer.alloc(4); b.writeUInt32BE(n); return b }
function cdto(kind: number, body: Buffer) { return Buffer.concat([Buffer.from('MUHCDTO\0'), Buffer.from([0, 1, 0, kind]), be32(body.length), body, createHash('sha256').update(body).digest()]) }
function manifest(command = id) { return Buffer.from(`version=1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${command}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\npost_sha256=${'b'.repeat(64)}\nwriter_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb\nsnapshot_format=legacy-file-manifest-v1\nwriter_epoch=7\nwriter_revision=2\nstorage_format=1\nsnapshot_octets=128\n`) }
function node(value: bigint) { const bytes = Buffer.alloc(349); bytes.writeUInt32BE(0, 0); bytes.writeUInt32BE(0xffffffff, 4); bytes.writeUInt32BE(0, 8); bytes.writeBigInt64BE(value, 312); return bytes }
function bank(value: bigint) { const graph = cdto(6, Buffer.concat([be16(1), Buffer.from([3]), be32(4), be32(1), be16(2), Buffer.from([9]), be32(349), node(value)])); return cdto(8, Buffer.concat([be16(1), Buffer.from([9]), be32(graph.length), graph])) }
function artifact(payload: Buffer, command = id) { const hash = createHash('sha256').update(payload).digest('hex'); return Buffer.concat([Buffer.from(`version=1\nartifact_format=bank-snapshot-v1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${command}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\nsource_post_sha256=${'b'.repeat(64)}\nwriter_epoch=7\nwriter_revision=2\nbank_sha256=${hash}\nbank_octets=${payload.length}\n\n`), payload]) }

test('root-value parser accepts only canonical BankSnapshotV1 root bytes and preserves signed i64 exactly', () => {
  for (const value of [-9223372036854775808n, -1n, 0n, 9223372036854775807n]) {
    assert.equal(parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`, artifact(bank(value)), parseManifest(manifest())).rootValue, value.toString())
  }
  const malformed = bank(7n); malformed[malformed.length - 33] ^= 1
  assert.throws(() => parseBankSnapshotV1Artifact(`${id}.bank-snapshot-v1`, artifact(malformed), parseManifest(manifest())), InvalidBankSnapshotV1ArtifactError)
})

test('root-value relay invokes its narrow writer only after artifact, manifest, and topology evidence settles', async () => {
  const calls: string[] = []
  const fs = { scan: async () => [
    { name: `${id}.bank-snapshot-v1`, bytes: artifact(bank(-42n)), receiptManifestBytes: manifest() },
    { name: `${id}.bank-snapshot-v1`, bytes: artifact(bank(-42n)), receiptManifestBytes: manifest(id.replace('1111', '2222')) },
  ] }
  const store = { recordBankSnapshotV1RootValueShadow: async (input: { rootValue: string }) => { calls.push(input.rootValue); return calls.length === 1 ? 'RECORDED' as const : 'EXACT_RETRY' as const } }
  const result = await relayBankSnapshotV1RootValueShadowsOnce('/ignored', store, fs)
  assert.equal(result.recorded, 1, JSON.stringify(result))
  assert.equal(result.invalid, 1)
  assert.deepEqual(calls, ['-42'])
})

test('changed root value under the same command is a conflict, and normal relay/image/topology source stays detached', async () => {
  const fs = { scan: async () => [{ name: `${id}.bank-snapshot-v1`, bytes: artifact(bank(2n)), receiptManifestBytes: manifest() }] }
  const store = { recordBankSnapshotV1RootValueShadow: async () => { const error = Object.assign(new Error('conflict'), { code: 'P0001' }); throw error } }
  assert.equal((await relayBankSnapshotV1RootValueShadowsOnce('/ignored', store, fs)).conflict, 1)
  const [defaultCli, image, topology] = await Promise.all([
    readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'),
    readFile(new URL('../Dockerfile', import.meta.url), 'utf8'),
    readFile(new URL('../src/bank-snapshot-v1-artifact-relay.ts', import.meta.url), 'utf8'),
  ])
  assert.doesNotMatch(defaultCli, /bank-snapshot-v1-root-value-shadow/i)
  assert.doesNotMatch(image, /bank-snapshot-v1-root-value-shadow/i)
  assert.doesNotMatch(topology, /root-value-shadow/i)
})

test('a deterministic narrow-writer failure is surfaced once and never retried by the relay', async () => {
  let attempts = 0
  const fs = { scan: async () => [{ name: `${id}.bank-snapshot-v1`, bytes: artifact(bank(3n)), receiptManifestBytes: manifest() }] }
  const store = { recordBankSnapshotV1RootValueShadow: async () => { attempts++; throw new Error('deterministic writer failure') } }
  const result = await relayBankSnapshotV1RootValueShadowsOnce('/ignored', store, fs)
  assert.equal(attempts, 1)
  assert.equal(result.unknown, 1)
  assert.equal(result.delivered, 0)
})

test('root-value store uses one parameterized private RPC and rejects noncanonical i64 text before opening a writer session', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const client = {
    query: async <Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      return { rows: (sql.startsWith('select outcome') ? [{ outcome: 'RECORDED' }] : []) as Row[] }
    },
    release: () => undefined,
  }
  const store = new PostgresBankSnapshotV1RootValueShadowStore('postgresql://mud_writer_login@localhost/postgres', {
    connect: async () => client,
    end: async () => undefined,
  })
  const input = { characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', commandId: id, receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64), sourceOctets: '128', bankSha256: 'c'.repeat(64), bankOctets: 97, rootValue: '-42' }
  assert.equal(await store.recordBankSnapshotV1RootValueShadow(input), 'RECORDED')
  assert.deepEqual(queries.map((q) => q.sql), [
    'set role mud_writer',
    'select outcome from private.record_bank_snapshot_v1_root_value_shadow_for_receipt($1::uuid,$2::uuid,$3::text,$4::text,$5::bigint,$6::text,$7::bigint,$8::bigint)',
  ])
  assert.deepEqual(queries[1]!.values, [...Object.values(input)])
  const before = queries.length
  await assert.rejects(store.recordBankSnapshotV1RootValueShadow({ ...input, rootValue: '-0' }))
  assert.equal(queries.length, before)
})
