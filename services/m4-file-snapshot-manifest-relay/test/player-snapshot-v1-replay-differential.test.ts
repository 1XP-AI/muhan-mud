import assert from 'node:assert/strict'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import {
  comparePlayerSnapshotV1ReplayShadowJournal,
  type PlayerSnapshotV1ReplayArtifactDifferentialReader,
  type PlayerSnapshotV1ReplayArtifactEvidence,
} from '../src/player-snapshot-v1-replay-differential.js'
import { main as differentialMain } from '../src/player-snapshot-v1-replay-differential-cli.js'
import { PostgresPlayerSnapshotV1ArtifactDifferentialReader, assertReplayDifferentialDatabaseUrl, type PgClient, type PgPool } from '../src/store.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const characterId = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const request = 'a'.repeat(64)
const source = 'b'.repeat(64)
const digest = 'c'.repeat(64)
const readerDatabaseUrl = 'postgresql://mud_replay_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don'

function journal(value: Record<string, unknown> = {}): string {
  return JSON.stringify({
    format: 'player-snapshot-v1-replay-shadow-journal', version: '1', commandId, characterId,
    receiptRequestSha256: request, sourcePostSha256: source,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: 42, inventoryNodeCount: 0,
    },
    ...value,
  })
}

function artifact(value: Partial<PlayerSnapshotV1ReplayArtifactEvidence> = {}): PlayerSnapshotV1ReplayArtifactEvidence {
  return {
    commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source,
    snapshotFormat: 'player-snapshot-v1', snapshotSha256: digest, snapshotOctets: 42,
    ...value,
  }
}

function reader(result: readonly PlayerSnapshotV1ReplayArtifactEvidence[] | Error): PlayerSnapshotV1ReplayArtifactDifferentialReader {
  return { findByCommandId: async () => { if (result instanceof Error) throw result; return result } }
}

async function withJournal<T>(files: Readonly<Record<string, string>>, run: (path: string) => Promise<T>): Promise<T> {
  const path = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-differential-'))
  try {
    await Promise.all(Object.entries(files).map(([name, body]) => writeFile(join(path, name), body, { mode: 0o600 })))
    return await run(path)
  } finally { await rm(path, { recursive: true, force: true }) }
}

test('differential reader processes journal JSON in lexical order with stable metadata-only output', async () => {
  await withJournal({ 'z.json': journal(), 'a.json': '{bad json' }, async (path) => {
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, reader([artifact()]))
    assert.deepEqual(result, {
      format: 'player-snapshot-v1-replay-differential', version: '1',
      records: [
        { index: 0, classification: 'JOURNAL_INVALID' },
        {
          index: 1, classification: 'MATCH', evidence: {
            journal: {
              commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source,
              verificationFormat: 'player-snapshot-v1-replay-verification', verificationVersion: '1', verificationAlgorithm: 'sha-256',
              snapshotSha256: digest, snapshotOctets: 42,
            },
            artifact: artifact(),
          },
        },
      ],
    })
    assert.equal(JSON.stringify(result).includes('bad json'), false)
  })
})

test('differential classifies missing, identity, digest, octets, duplicates, and database read failures', async () => {
  const cases: Array<[string, PlayerSnapshotV1ReplayArtifactDifferentialReader, string]> = [
    ['missing', reader([]), 'MISSING_DB_ARTIFACT'],
    ['identity', reader([artifact({ sourcePostSha256: 'd'.repeat(64) })]), 'IDENTITY_MISMATCH'],
    ['digest', reader([artifact({ snapshotSha256: 'd'.repeat(64) })]), 'DIGEST_MISMATCH'],
    ['octets', reader([artifact({ snapshotOctets: 43 })]), 'OCTETS_MISMATCH'],
    ['duplicate', reader([artifact(), artifact({ characterId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' })]), 'UNEXPECTED_DUPLICATE'],
    ['database error', reader(new Error('connection details must not be emitted')), 'DB_READ_ERROR'],
  ]
  for (const [, artifactReader, classification] of cases) {
    await withJournal({ 'entry.json': journal() }, async (path) => {
      const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, artifactReader)
      assert.equal(result.records[0]?.classification, classification)
      assert.equal(JSON.stringify(result).includes('connection details'), false)
    })
  }
})

test('differential rejects mutated or non-closed journal entries before database access and is repeatable', async () => {
  let calls = 0
  const countingReader: PlayerSnapshotV1ReplayArtifactDifferentialReader = {
    findByCommandId: async () => { calls++; return [artifact()] },
  }
  await withJournal({
    'a.json': journal({ unexpected: true }),
    'b.json': journal({ verification: { format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256', inputDigest: digest, canonicalDigest: digest, canonicalOctets: 42, inventoryNodeCount: 0, payload: 'forbidden' } }),
    'c.json': journal(),
  }, async (path) => {
    const first = await comparePlayerSnapshotV1ReplayShadowJournal(path, countingReader)
    const second = await comparePlayerSnapshotV1ReplayShadowJournal(path, countingReader)
    assert.deepEqual(first, second)
    assert.deepEqual(first.records.map((record) => record.classification), ['JOURNAL_INVALID', 'JOURNAL_INVALID', 'MATCH'])
    assert.equal(calls, 2)
  })
})

test('unreadable or missing configured journal directory is a stable journal-invalid result without a database read', async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-differential-missing-'))
  let calls = 0
  try {
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(join(root, 'missing'), {
      findByCommandId: async () => { calls++; return [artifact()] },
    })
    assert.deepEqual(result.records, [{ index: 0, classification: 'JOURNAL_INVALID' }])
    assert.equal(calls, 0)
  } finally { await rm(root, { recursive: true, force: true }) }
})

test('dedicated --once CLI requires separate opt-in settings, emits one stable JSON result, and returns nonzero on non-match', async () => {
  await withJournal({ 'entry.json': journal() }, async (path) => {
    const output: string[] = []
    let closed = false
    const dependencies = {
      createReader: () => ({ findByCommandId: async () => [], close: async () => { closed = true } }),
      writeStdout: (value: string) => { output.push(value) },
    }
    const env = {
      M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_JOURNAL_PATH: path,
      M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_DATABASE_URL: readerDatabaseUrl,
      DATABASE_URL: 'postgresql://mud_writer_login@localhost/postgres',
    }
    assert.equal(await differentialMain(env, ['--once'], dependencies), 1)
    assert.equal(output.length, 1)
    assert.equal(JSON.parse(output[0]!).records[0].classification, 'MISSING_DB_ARTIFACT')
    assert.equal(closed, true)
    await assert.rejects(() => differentialMain({ ...env, DATABASE_URL: env.M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_DATABASE_URL }, ['--once'], dependencies))
  })
})

test('CLI fails without stdout when closing the reader fails', async () => {
  await withJournal({ 'entry.json': journal() }, async (path) => {
    const output: string[] = []
    const env = {
      M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_JOURNAL_PATH: path,
      M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_DATABASE_URL: readerDatabaseUrl,
    }
    await assert.rejects(() => differentialMain(env, ['--once'], {
      createReader: () => ({ findByCommandId: async () => [artifact()], close: async () => { throw new Error('close failed') } }),
      writeStdout: (value) => { output.push(value) },
    }))
    assert.deepEqual(output, [])
  })

  const client: PgClient = { query: async <Row>() => ({ rows: [] as Row[] }), release: () => undefined }
  const pool: PgPool = { connect: async () => client, end: async () => { throw new Error('pool close failed') } }
  const postgresReader = new PostgresPlayerSnapshotV1ArtifactDifferentialReader(readerDatabaseUrl, pool)
  await assert.rejects(() => postgresReader.close(), /pool close failed/)
})

test('Postgres differential reader verifies the dedicated read-only connection and uses SELECT only', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const client: PgClient = {
    query: async <Row>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      if (queries.length === 1) return {
        rows: [{
          currentUser: 'mud_replay_reader_login', sessionUser: 'mud_replay_reader_login',
          defaultTransactionReadOnly: 'on', transactionReadOnly: 'on',
          canInsert: false, canUpdate: false, canDelete: false, canTruncate: false, canReferences: false, canTrigger: false,
        } as Row],
      }
      return { rows: [{ ...artifact(), snapshotOctets: '42' } as Row] }
    },
    release: () => undefined,
  }
  const pool: PgPool = { connect: async () => client, end: async () => undefined }
  const differentialReader = new PostgresPlayerSnapshotV1ArtifactDifferentialReader(
    readerDatabaseUrl, pool,
  )
  assert.deepEqual(await differentialReader.findByCommandId(commandId), [artifact()])
  assert.equal(queries.length, 2)
  for (const query of queries) assert.match(query.sql, /^\s*select\b/i)
  assert.equal(queries.some((query) => /(?<!')\b(set|insert|update|delete|truncate|call|create|alter|drop)\b(?!')/i.test(query.sql)), false)
  assert.match(queries[0]!.sql, /has_table_privilege[\s\S]*'TRIGGER'/)
  assert.deepEqual(queries[1]!.values, [commandId])
})

test('Postgres differential reader rejects malformed artifact rows and connection contract failures', async () => {
  const validContract = {
    currentUser: 'mud_replay_reader_login', sessionUser: 'mud_replay_reader_login',
    defaultTransactionReadOnly: 'on', transactionReadOnly: 'on',
    canInsert: false, canUpdate: false, canDelete: false, canTruncate: false, canReferences: false, canTrigger: false,
  }
  const invalidContracts = [
    { ...validContract, currentUser: 'mud_writer_login' },
    { ...validContract, sessionUser: 'mud_writer_login' },
    { ...validContract, defaultTransactionReadOnly: 'off' },
    { ...validContract, transactionReadOnly: 'off' },
    { ...validContract, canInsert: true },
    { ...validContract, canUpdate: true },
    { ...validContract, canDelete: true },
    { ...validContract, canTruncate: true },
    { ...validContract, canReferences: true },
    { ...validContract, canTrigger: true },
    { ...validContract, canTrigger: 'false' },
  ]
  for (const contract of invalidContracts) {
    const client: PgClient = { query: async <Row>() => ({ rows: [contract as Row] }), release: () => undefined }
    const pool: PgPool = { connect: async () => client, end: async () => undefined }
    const differentialReader = new PostgresPlayerSnapshotV1ArtifactDifferentialReader(readerDatabaseUrl, pool)
    await assert.rejects(() => differentialReader.findByCommandId(commandId), /invalid replay differential database connection/)
  }

  const malformedClient: PgClient = {
    query: async <Row>(sql: string) => ({ rows: [sql.includes('current_user') ? validContract : { ...artifact(), snapshotOctets: 'not-an-integer' }] as Row[] }),
    release: () => undefined,
  }
  const malformedPool: PgPool = { connect: async () => malformedClient, end: async () => undefined }
  const malformedReader = new PostgresPlayerSnapshotV1ArtifactDifferentialReader(readerDatabaseUrl, malformedPool)
  await assert.rejects(() => malformedReader.findByCommandId(commandId), /invalid replay differential database result/)
})

test('replay differential URL accepts only the dedicated reader login with read-only startup configuration', () => {
  assert.equal(assertReplayDifferentialDatabaseUrl(readerDatabaseUrl), readerDatabaseUrl)
  for (const value of [
    'postgresql://mud_replay_reader_login@localhost/postgres',
    'postgresql://mud_replay_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Doff',
    'postgresql://mud_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don',
    'postgresql://mud_writer_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don',
  ]) assert.throws(() => assertReplayDifferentialDatabaseUrl(value))
})
