import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import {
  rehearsePlayerSnapshotV1FullPayload,
  type ImmutablePlayerSnapshotV1FullPayloadEvidence,
  type ImmutablePlayerSnapshotV1FullPayloadReader,
} from '../src/player-snapshot-v1-full-payload-rehearsal.js'
import { PostgresPlayerSnapshotV1FullPayloadRehearsalReader, type PgClient, type PgPool } from '../src/store.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const characterId = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const payload = Buffer.alloc(48, 7)
const digest = createHash('sha256').update(payload).digest('hex')
const readerUrl = 'postgresql://mud_full_payload_rehearsal_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don'
const local = {
  characterId, commandId, receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64),
  sourceOctets: '99', snapshotFormat: 'player-snapshot-v1' as const, snapshotSha256: digest,
  snapshotOctets: payload.length, payload,
}
const receipt = {
  characterId, commandId, worldId: 'm4-rehearsal', legacyNameKey: 'M4alpha', requestSha256: 'a'.repeat(64),
  writerInstanceId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', writerEpoch: '7', writerRevision: '9',
  postSha256: 'b'.repeat(64), storageFormat: '1', acknowledgedAt: '2026-10-04 00:00:00+00',
}
const evidence: ImmutablePlayerSnapshotV1FullPayloadEvidence = {
  ...local, worldId: 'm4-rehearsal', legacyNameKey: 'M4alpha', writerInstanceId: receipt.writerInstanceId,
  writerEpoch: '7', writerRevision: '9', storageFormat: '1', receiptAcknowledgedAt: receipt.acknowledgedAt, receipt,
}
const connectionContract = {
  currentUser: 'mud_full_payload_rehearsal_reader_login', sessionUser: 'mud_full_payload_rehearsal_reader_login',
  defaultTransactionReadOnly: 'on', transactionReadOnly: 'on',
  canArtifactInsert: false, canArtifactUpdate: false, canArtifactDelete: false,
  canArtifactTruncate: false, canArtifactReferences: false, canArtifactTrigger: false,
  canReceiptInsert: false, canReceiptUpdate: false, canReceiptDelete: false,
  canReceiptTruncate: false, canReceiptReferences: false, canReceiptTrigger: false,
}

function reader(rows: readonly unknown[] | Error): ImmutablePlayerSnapshotV1FullPayloadReader {
  return { findByCommandId: async () => { if (rows instanceof Error) throw rows; return rows } }
}

async function decode(value: Uint8Array) {
  const valueDigest = createHash('sha256').update(value).digest('hex')
  return {
    format: 'player-snapshot-v1-replay-verification' as const, version: '1' as const, algorithm: 'sha-256' as const,
    inputDigest: valueDigest, canonicalDigest: valueDigest, canonicalOctets: value.length, inventoryNodeCount: 3,
  }
}

test('full payload rehearsal is explicit and receipt-bound before decode or persisted semantic comparison', async () => {
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([evidence]), decode), 'MATCH')
  for (const [field, value] of [
    ['characterId', 'cccccccc-cccc-4ccc-8ccc-cccccccccccc'], ['commandId', 'dddddddd-dddd-4ddd-8ddd-dddddddddddd'],
    ['worldId', 'other'], ['legacyNameKey', 'M4other'], ['requestSha256', 'c'.repeat(64)],
    ['writerInstanceId', 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee'], ['writerEpoch', '8'], ['writerRevision', '10'],
    ['postSha256', 'c'.repeat(64)], ['storageFormat', '2'], ['acknowledgedAt', '2026-10-04 00:01:00+00'],
  ] as const) {
    assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([{
      ...evidence, receipt: { ...receipt, [field]: value },
    }]), decode), 'RECEIPT_MISMATCH', field)
  }
})

test('full payload rehearsal distinguishes absence, duplicates, reader failure, artifact mismatch, and decode/semantic outcomes', async () => {
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([]), decode), 'MISSING_ARTIFACT')
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([evidence, evidence]), decode), 'UNEXPECTED_DUPLICATE')
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader(new Error('must not escape')), decode), 'DB_READ_ERROR')
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([{ ...evidence, sourceOctets: '100' }]), decode), 'ARTIFACT_MISMATCH')
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([evidence]), async () => { throw new Error('invalid') }), 'DECODE_MISMATCH')
  let decodeCalls = 0
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([evidence]), async (value) => ({
    ...(await decode(value)), inventoryNodeCount: ++decodeCalls === 1 ? 3 : 4,
  })), 'SEMANTIC_MISMATCH')
  assert.equal(await rehearsePlayerSnapshotV1FullPayload(local, reader([{
    ...evidence, snapshotSha256: 'c'.repeat(64),
  }]), decode), 'INVALID_INPUT')
})

function poolForRows(rows: readonly Record<string, unknown>[], queries: Array<{ sql: string, values?: readonly unknown[] }>): PgPool {
  const client: PgClient = {
    query: async <Row>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      return { rows: (/current_user/.test(sql) ? [connectionContract] : rows) as Row[] }
    },
    release: () => undefined,
  }
  return { connect: async () => client, end: async () => undefined }
}

test('full payload reader uses its dedicated read-only login and exactly one parameterized joined SELECT', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const reader = new PostgresPlayerSnapshotV1FullPayloadRehearsalReader(readerUrl, poolForRows([
    { ...evidence, snapshotOctets: String(evidence.snapshotOctets) },
  ], queries))
  assert.deepEqual(await reader.findByCommandId(commandId), [evidence])
  assert.equal(queries.length, 2)
  for (const query of queries) assert.match(query.sql, /^\s*select\b/i)
  assert.equal(queries.some((query) => /(?<!')\b(set|insert|update|delete|truncate|call|create|alter|drop)\b(?!')/i.test(query.sql)), false)
  assert.match(queries[1]!.sql, /game_character_player_snapshot_v1_artifacts/)
  assert.match(queries[1]!.sql, /game_character_shadow_receipts/)
  assert.match(queries[1]!.sql, /\ba\.payload\b/)
  assert.match(queries[1]!.sql, /where a\.command_id = \$1::uuid/)
  assert.deepEqual(queries[1]!.values, [commandId])
  for (const [relation, prefix] of [
    ['private.game_character_player_snapshot_v1_artifacts', 'canArtifact'],
    ['private.game_character_shadow_receipts', 'canReceipt'],
  ] as const) {
    for (const [privilege, suffix] of [
      ['INSERT', 'Insert'], ['UPDATE', 'Update'], ['DELETE', 'Delete'],
      ['TRUNCATE', 'Truncate'], ['REFERENCES', 'References'], ['TRIGGER', 'Trigger'],
    ] as const) {
      assert.match(queries[0]!.sql, new RegExp(`has_table_privilege\\(current_user, '${relation}', '${privilege}'\\) as "${prefix}${suffix}"`))
    }
  }
})

test('full payload reader rejects malformed evidence and any non-read-only or mutating database session', async () => {
  const invalidRows = [
    { ...evidence, payload: Buffer.alloc(47) }, { ...evidence, snapshotOctets: 'not-a-number' },
    { ...evidence, receipt: { ...receipt, requestSha256: 'nope' } },
  ]
  for (const row of invalidRows) {
    const reader = new PostgresPlayerSnapshotV1FullPayloadRehearsalReader(readerUrl, poolForRows([row], []))
    await assert.rejects(() => reader.findByCommandId(commandId), /invalid full payload rehearsal database result/)
  }
  const mutatingContracts = Object.keys(connectionContract)
    .filter((field) => field.startsWith('canArtifact') || field.startsWith('canReceipt'))
    .map((field) => ({ ...connectionContract, [field]: true }))
  for (const contract of [
    { ...connectionContract, currentUser: 'mud_writer_login' }, { ...connectionContract, transactionReadOnly: 'off' },
    ...mutatingContracts,
  ]) {
    const client: PgClient = { query: async <Row>() => ({ rows: [contract as Row] }), release: () => undefined }
    const reader = new PostgresPlayerSnapshotV1FullPayloadRehearsalReader(readerUrl, { connect: async () => client, end: async () => undefined })
    await assert.rejects(() => reader.findByCommandId(commandId), /invalid full payload rehearsal database connection/)
  }
})
