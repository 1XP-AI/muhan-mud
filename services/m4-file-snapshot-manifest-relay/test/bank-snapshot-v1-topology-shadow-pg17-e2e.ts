import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
import { parseBankSnapshotV1Artifact } from '../src/bank-snapshot-v1-artifact.js'
import { parseManifest } from '../src/manifest.js'
import { PostgresBankSnapshotV1TopologyShadowStore } from '../src/store.js'

const require = createRequire(import.meta.url)

interface SqlClient {
  connect(): Promise<void>
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<{ rows: Row[] }>
  end(): Promise<void>
}
interface PgModule { Client: new (options: { connectionString: string }) => SqlClient }

const databaseUrl = process.env.BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_DATABASE_URL
const superDatabaseUrl = process.env.BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_SUPER_DATABASE_URL
const requestSha256 = process.env.BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_REQUEST_SHA256
if (!databaseUrl || !superDatabaseUrl || !requestSha256) throw new Error('BankSnapshotV1 topology PG17 E2E configuration is missing')

const characterId = 'a9520000-0000-0000-0000-000000000001'
const commandId = 'c9520000-0000-0000-0000-000000000001'
const sourcePostSha256 = 'a'.repeat(64)

function be16(value: number) { const bytes = Buffer.alloc(2); bytes.writeUInt16BE(value); return bytes }
function be32(value: number) { const bytes = Buffer.alloc(4); bytes.writeUInt32BE(value); return bytes }
function cdto(kind: number, body: Uint8Array) {
  return Buffer.concat([Buffer.from('MUHCDTO\0', 'ascii'), Buffer.from([0, 1, 0, kind]), be32(body.length), Buffer.from(body), createHash('sha256').update(body).digest()])
}
function objectNode(index: number, parent: number | null, sibling: number) {
  const bytes = Buffer.alloc(349)
  bytes.writeUInt32BE(index, 0)
  bytes.writeUInt32BE(parent ?? 0xffffffff, 4)
  bytes.writeUInt32BE(sibling, 8)
  return bytes
}

/** Builds canonical wire bytes, which the test then parses through the production parser. */
function bank(nodes: readonly Buffer[]) {
  const graphFields: Buffer[] = [be16(1), Buffer.from([3]), be32(4), be32(nodes.length)]
  for (let index = 0; index < nodes.length; index++) graphFields.push(be16(index + 2), Buffer.from([9]), be32(349), nodes[index]!)
  const graph = cdto(6, Buffer.concat(graphFields))
  return cdto(8, Buffer.concat([be16(1), Buffer.from([9]), be32(graph.length), graph]))
}
function receipt() {
  return parseManifest(Buffer.from([
    'version=1', 'world_id=bank-relay-e2e', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4532656865726f', `request_sha256=${requestSha256}`, `post_sha256=${sourcePostSha256}`,
    'writer_instance_id=b9520000-0000-0000-0000-000000000001', 'snapshot_format=legacy-file-manifest-v1',
    'writer_epoch=1', 'writer_revision=1', 'storage_format=1', 'snapshot_octets=9', '',
  ].join('\n'), 'ascii'))
}
function artifact(payload: Uint8Array) {
  return Buffer.concat([Buffer.from([
    'version=1', 'artifact_format=bank-snapshot-v1', 'world_id=bank-relay-e2e', `character_id=${characterId}`,
    `command_id=${commandId}`, 'canonical_name_hex=4532656865726f', `request_sha256=${requestSha256}`,
    `source_post_sha256=${sourcePostSha256}`, 'writer_epoch=1', 'writer_revision=1',
    `bank_sha256=${createHash('sha256').update(payload).digest('hex')}`, `bank_octets=${payload.length}`, '', '',
  ].join('\n'), 'ascii'), Buffer.from(payload)])
}
function parse(payload: Uint8Array) {
  return parseBankSnapshotV1Artifact(`${commandId}.bank-snapshot-v1`, artifact(payload), receipt())
}

async function main(): Promise<void> {
  const canonical = parse(bank([objectNode(0, null, 0)]))
  const branching = parse(bank([objectNode(0, null, 0), objectNode(1, 0, 0), objectNode(2, 0, 1)]))
  assert.deepEqual(canonical.nodes, [{ nodeIndex: 0, parentNodeIndex: null, siblingOrdinal: 0 }], 'the real parser must produce canonical root topology')
  assert.deepEqual(branching.nodes, [
    { nodeIndex: 0, parentNodeIndex: null, siblingOrdinal: 0 },
    { nodeIndex: 1, parentNodeIndex: 0, siblingOrdinal: 0 },
    { nodeIndex: 2, parentNodeIndex: 0, siblingOrdinal: 1 },
  ], 'the real parser must preserve a canonical branching topology')

  const store = new PostgresBankSnapshotV1TopologyShadowStore(databaseUrl)
  const superClient = new (require('pg') as PgModule).Client({ connectionString: superDatabaseUrl })
  try {
    await superClient.connect()
    assert.equal(await store.recordBankSnapshotV1TopologyShadow(canonical), 'RECORDED')
    assert.equal(await store.recordBankSnapshotV1TopologyShadow(canonical), 'EXACT_RETRY')
    await assert.rejects(
      store.recordBankSnapshotV1TopologyShadow(branching),
      (error: { code?: unknown }) => error.code === 'P0001',
      'a parsed branching artifact must not replace immutable canonical topology evidence',
    )
    await assert.rejects(
      superClient.query(
        'select outcome from private.record_bank_snapshot_v1_topology_shadow_for_receipt($1::uuid,$2::uuid,$3::text,$4::text,$5::bigint,$6::text,$7::bigint,$8::jsonb)',
        [canonical.characterId, canonical.commandId, canonical.receiptRequestSha256, canonical.sourcePostSha256, canonical.sourceOctets, canonical.bankSha256, canonical.bankOctets, JSON.stringify(canonical.nodes)],
      ),
      (error: { code?: unknown }) => error.code === 'P0001',
      'the recorder must reject a caller outside the writer login and role session',
    )
    const state = await superClient.query<{ item_count: string, head_state: string, head_sha256: string, revision: string }>(`
      select s.item_count::text, h.head_state, h.head_sha256, h.revision::text
      from private.game_character_bank_snapshot_v1_topology_shadows s
      join private.game_character_legacy_heads h on h.character_id = s.character_id
      where s.character_id = $1::uuid and s.command_id = $2::uuid
    `, [characterId, commandId])
    assert.deepEqual(state.rows, [{ item_count: '1', head_state: 'existing', head_sha256: sourcePostSha256, revision: '1' }], 'receipt publication advances the legacy head once; detached topology evidence cannot advance it further')
    console.log('GREEN PostgreSQL 17: real BankSnapshotV1 parser and topology adapter recorded, exact-retried, and rejected conflicting or wrong-session writes')
  } finally {
    await Promise.all([store.close(), superClient.end()])
  }
}

await main()
