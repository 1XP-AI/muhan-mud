import assert from 'node:assert/strict'
import { test } from 'node:test'
import { comparePlayerSnapshotV2JournalLevel } from '../src/player-snapshot-v1-level-comparator.js'
import { PostgresPlayerSnapshotV1LevelProjectionReader, type PgClient, type PgPool } from '../src/store.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const readerDatabaseUrl = 'postgresql://mud_replay_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don'
const evidence = {
  commandId,
  characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  receiptRequestSha256: 'a'.repeat(64),
  sourcePostSha256: 'b'.repeat(64),
  snapshotSha256: 'c'.repeat(64),
  snapshotOctets: 42,
  rawLevelU8: 42,
}

const connectionContract = {
  currentUser: 'mud_replay_reader_login', sessionUser: 'mud_replay_reader_login',
  defaultTransactionReadOnly: 'on', transactionReadOnly: 'on',
  canInsert: false, canUpdate: false, canDelete: false, canTruncate: false, canReferences: false, canTrigger: false,
}

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

test('level projection reader uses only the dedicated read-only connection and a parameterized metadata-only SELECT', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const reader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, poolForRows([
    { ...evidence, snapshotOctets: '42', rawLevelU8: '42' },
  ], queries))

  assert.deepEqual(await reader.findByCommandId(commandId), [evidence])
  assert.equal(queries.length, 2)
  for (const query of queries) assert.match(query.sql, /^\s*select\b/i)
  assert.equal(queries.some((query) => /(?<!')\b(set|insert|update|delete|truncate|call|create|alter|drop)\b(?!')/i.test(query.sql)), false)
  assert.match(queries[0]!.sql, /game_character_player_snapshot_v1_level_projections/)
  assert.match(queries[0]!.sql, /has_table_privilege[\s\S]*'TRIGGER'/)
  assert.match(queries[1]!.sql, /command_id[\s\S]*character_id[\s\S]*receipt_request_sha256[\s\S]*source_post_sha256[\s\S]*snapshot_sha256[\s\S]*snapshot_octets[\s\S]*raw_level_u8/)
  assert.doesNotMatch(queries[1]!.sql, /\bpayload\b/i)
  assert.match(queries[1]!.sql, /where command_id = \$1::uuid/)
  assert.match(queries[1]!.sql, /order by character_id/)
  assert.deepEqual(queries[1]!.values, [commandId])
})

test('level projection reader preserves 0, 42, and 255 as raw U8 values', async () => {
  for (const rawLevelU8 of [0, 42, 255]) {
    const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
    const reader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, poolForRows([
      { ...evidence, snapshotOctets: '42', rawLevelU8: String(rawLevelU8) },
    ], queries))
    const rows = await reader.findByCommandId(commandId)
    assert.deepEqual(rows, [{ ...evidence, rawLevelU8 }])
    assert.equal(await comparePlayerSnapshotV2JournalLevel({ ...evidence, rawLevelU8 }, reader), 'MATCH')
  }
})

test('level projection reader leaves missing, duplicate, and read failures to the pure comparator classifications', async () => {
  const missingReader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, poolForRows([], []))
  assert.equal(await comparePlayerSnapshotV2JournalLevel(evidence, missingReader), 'MISSING_PROJECTION')

  const duplicateQueries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const duplicateClient: PgClient = {
    query: async <Row>(sql: string) => {
      duplicateQueries.push({ sql })
      return { rows: (/current_user/.test(sql) ? [connectionContract] : [
        { ...evidence, snapshotOctets: '42', rawLevelU8: '42' },
        { ...evidence, characterId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', snapshotOctets: '42', rawLevelU8: '42' },
      ]) as Row[] }
    },
    release: () => undefined,
  }
  const duplicateReader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, {
    connect: async () => duplicateClient, end: async () => undefined,
  })
  assert.equal(await comparePlayerSnapshotV2JournalLevel(evidence, duplicateReader), 'UNEXPECTED_DUPLICATE')

  const failingReader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, {
    connect: async () => { throw new Error('connection failed') }, end: async () => undefined,
  })
  assert.equal(await comparePlayerSnapshotV2JournalLevel(evidence, failingReader), 'PROJECTION_READ_ERROR')
})

test('level projection reader rejects malformed metadata and privilege or read-only contract failures without mutation', async () => {
  const malformedRows = [
    { ...evidence, commandId: 'not-a-uuid', snapshotOctets: '42', rawLevelU8: '42' },
    { ...evidence, receiptRequestSha256: 'not-a-sha256', snapshotOctets: '42', rawLevelU8: '42' },
    { ...evidence, snapshotOctets: '-1', rawLevelU8: '42' },
    { ...evidence, snapshotOctets: '42', rawLevelU8: '-1' },
    { ...evidence, snapshotOctets: '42', rawLevelU8: '256' },
    { ...evidence, snapshotOctets: '42', rawLevelU8: 'not-a-number' },
  ]
  for (const row of malformedRows) {
    const reader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, poolForRows([row], []))
    await assert.rejects(() => reader.findByCommandId(commandId), /invalid replay level projection database result/)
  }

  for (const contract of [
    { ...connectionContract, currentUser: 'mud_writer_login' },
    { ...connectionContract, sessionUser: 'mud_writer_login' },
    { ...connectionContract, defaultTransactionReadOnly: 'off' },
    { ...connectionContract, transactionReadOnly: 'off' },
    { ...connectionContract, canInsert: true },
    { ...connectionContract, canUpdate: true },
    { ...connectionContract, canDelete: true },
    { ...connectionContract, canTruncate: true },
    { ...connectionContract, canReferences: true },
    { ...connectionContract, canTrigger: true },
  ]) {
    const client: PgClient = { query: async <Row>() => ({ rows: [contract as Row] }), release: () => undefined }
    const reader = new PostgresPlayerSnapshotV1LevelProjectionReader(readerDatabaseUrl, {
      connect: async () => client, end: async () => undefined,
    })
    await assert.rejects(() => reader.findByCommandId(commandId), /invalid replay level projection database connection/)
  }
})
