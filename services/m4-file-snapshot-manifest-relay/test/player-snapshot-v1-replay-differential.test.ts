import assert from 'node:assert/strict'
import { mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import {
  PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES,
  comparePlayerSnapshotV1ReplayShadowJournal,
  readPlayerSnapshotV1ReplayLevelDifferentialInputs,
  type PlayerSnapshotV1ReplayArtifactDifferentialReader,
  type PlayerSnapshotV1ReplayArtifactEvidence,
  type PlayerSnapshotV1ReplayDifferentialJournalFileReader,
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

function levelJournal(rawLevelU8: number, value: Record<string, unknown> = {}): string {
  return JSON.stringify({
    format: 'player-snapshot-v1-replay-shadow-journal', version: '2', commandId, characterId,
    receiptRequestSha256: request, sourcePostSha256: source, rawLevelU8,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
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
      format: 'player-snapshot-v1-replay-differential', version: '1', classification: 'INCONSISTENT',
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

test('differential default filesystem reader returns EXACT for one valid journal entry without mutation', async () => {
  await withJournal({ 'entry.json': journal() }, async (path) => {
    const entryPath = join(path, 'entry.json')
    const before = await readFile(entryPath, 'utf8')
    const namesBefore = await readdir(path, { encoding: 'buffer' })
    let metadataReads = 0

    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, {
      findByCommandId: async (id) => {
        metadataReads++
        assert.equal(id, commandId)
        return [artifact()]
      },
    })

    assert.equal(result.classification, 'EXACT')
    assert.equal(result.records[0]?.classification, 'MATCH')
    assert.equal(metadataReads, 1)
    assert.equal(await readFile(entryPath, 'utf8'), before)
    assert.deepEqual(await readdir(path, { encoding: 'buffer' }), namesBefore)
  })
})

test('level differential input reader emits only closed v2 metadata for raw U8 boundary values', async () => {
  await withJournal({
    'z.json': levelJournal(255),
    'a.json': levelJournal(0),
    'm.json': levelJournal(42),
  }, async (path) => {
    const result = await readPlayerSnapshotV1ReplayLevelDifferentialInputs(path)
    assert.deepEqual(result, {
      format: 'player-snapshot-v1-replay-level-differential-input', version: '1', classification: 'READY',
      records: [
        { index: 0, classification: 'INPUT', input: { commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source, snapshotSha256: digest, snapshotOctets: 42, rawLevelU8: 0 } },
        { index: 1, classification: 'INPUT', input: { commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source, snapshotSha256: digest, snapshotOctets: 42, rawLevelU8: 42 } },
        { index: 2, classification: 'INPUT', input: { commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source, snapshotSha256: digest, snapshotOctets: 42, rawLevelU8: 255 } },
      ],
    })
    assert.equal(JSON.stringify(result).includes('payload'), false)
  })
})

test('level differential input reader does not treat v1 journals as a source of level metadata', async () => {
  await withJournal({ 'legacy.json': journal() }, async (path) => {
    const result = await readPlayerSnapshotV1ReplayLevelDifferentialInputs(path)
    assert.deepEqual(result, {
      format: 'player-snapshot-v1-replay-level-differential-input', version: '1', classification: 'INCONSISTENT',
      records: [{ index: 0, classification: 'JOURNAL_INVALID' }],
    })
  })
})

test('existing artifact differential continues to compare closed v2 journal identity and digest metadata', async () => {
  await withJournal({ 'v2.json': levelJournal(42) }, async (path) => {
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, reader([artifact()]))
    assert.equal(result.classification, 'EXACT')
    assert.deepEqual(result.records[0], {
      index: 0,
      classification: 'MATCH',
      evidence: {
        journal: {
          commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source,
          verificationFormat: 'player-snapshot-v1-replay-verification', verificationVersion: '2', verificationAlgorithm: 'sha-256',
          snapshotSha256: digest, snapshotOctets: 42,
        },
        artifact: artifact(),
      },
    })
  })
})

test('level differential input reader rejects non-closed v2 level records without exposing their extras', async () => {
  await withJournal({
    'a.json': levelJournal(256),
    'b.json': levelJournal(42, { payload: 'forbidden' }),
    'c.json': levelJournal(42, { verification: {
      format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: 42, inventoryNodeCount: 0, rawLevelU8: 42,
    } }),
  }, async (path) => {
    const result = await readPlayerSnapshotV1ReplayLevelDifferentialInputs(path)
    assert.equal(result.classification, 'INCONSISTENT')
    assert.deepEqual(result.records, [
      { index: 0, classification: 'JOURNAL_INVALID' },
      { index: 1, classification: 'JOURNAL_INVALID' },
      { index: 2, classification: 'JOURNAL_INVALID' },
    ])
    assert.equal(JSON.stringify(result).includes('forbidden'), false)
  })
})

test('differential default filesystem reader rejects a symlinked journal entry before database access', async (t) => {
  const path = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-differential-symlink-'))
  try {
    const targetPath = join(path, 'entry-target')
    const entryPath = join(path, 'entry.json')
    await writeFile(targetPath, journal(), { mode: 0o600 })
    try {
      await symlink(targetPath, entryPath)
    } catch (error) {
      t.skip(`symlink fixture unavailable: ${error instanceof Error ? error.message : String(error)}`)
      return
    }

    let metadataReads = 0
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, {
      findByCommandId: async () => { metadataReads++; return [artifact()] },
    })

    assert.deepEqual(result.records, [{ index: 0, classification: 'JOURNAL_INVALID' }])
    assert.equal(metadataReads, 0)
  } finally { await rm(path, { recursive: true, force: true }) }
})

test('differential processes journal entries in deterministic lexical order', async () => {
  const commandIds = [
    '11111111-1111-4111-8111-111111111111',
    '22222222-2222-4222-8222-222222222222',
    '33333333-3333-4333-8333-333333333333',
  ]
  const observed: string[] = []
  await withJournal({
    'z.json': journal({ commandId: commandIds[2] }),
    'a.json': journal({ commandId: commandIds[0] }),
    'm.json': journal({ commandId: commandIds[1] }),
  }, async (path) => {
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, {
      findByCommandId: async (id) => {
        observed.push(id)
        return [artifact({ commandId: id })]
      },
    })
    assert.deepEqual(observed, commandIds)
    assert.deepEqual(result.records.map((record) => record.index), [0, 1, 2])
    assert.equal(result.classification, 'EXACT')
  })
})

test('differential accepts and deterministically processes exactly 256 valid journal entries', async () => {
  const commandIds = Array.from({ length: PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES }, (_, index) =>
    `11111111-1111-4111-8111-${index.toString(16).padStart(12, '0')}`,
  )
  const filenames = commandIds.map((_, index) => `${String(index).padStart(3, '0')}.json`)
  const observed: string[] = []
  let contentReads = 0
  let databaseReads = 0
  await withJournal({}, async (path) => {
    const entries = new Map(filenames.map((name, index) => [join(path, name), journal({ commandId: commandIds[index]! })]))
    const journalFiles: PlayerSnapshotV1ReplayDifferentialJournalFileReader = {
      readDirectory: async () => filenames.map((name) => Buffer.from(name)).reverse(),
      readEntry: async (entryPath) => {
        contentReads++
        const entry = entries.get(entryPath)
        assert.notEqual(entry, undefined)
        return entry
      },
    }
    const countingReader: PlayerSnapshotV1ReplayArtifactDifferentialReader = {
      findByCommandId: async (id) => {
        databaseReads++
        observed.push(id)
        return [artifact({ commandId: id })]
      },
    }
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, countingReader, journalFiles)
    assert.equal(result.classification, 'EXACT')
    assert.deepEqual(result.records.map((record) => record.index), Array.from({ length: commandIds.length }, (_, index) => index))
    assert.deepEqual(observed, commandIds)
    assert.equal(contentReads, PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES)
    assert.equal(databaseReads, PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES)
  })
})

test('differential rejects 257 journal entries before content or database reads', async () => {
  const names = Array.from(
    { length: PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES + 1 },
    (_, index) => Buffer.from(`${String(index).padStart(3, '0')}.json`),
  )
  let contentReads = 0
  let databaseCalls = 0
  const journalFiles: PlayerSnapshotV1ReplayDifferentialJournalFileReader = {
    readDirectory: async () => names,
    readEntry: async () => { contentReads++; return journal() },
  }
  await withJournal({}, async (path) => {
    const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, {
      findByCommandId: async () => { databaseCalls++; return [artifact()] },
    }, journalFiles)
    assert.equal(result.classification, 'INCONSISTENT')
    assert.deepEqual(result.records, [{ index: PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES, classification: 'JOURNAL_BOUND_EXCEEDED' }])
    assert.equal(contentReads, 0)
    assert.equal(databaseCalls, 0)
  })
})

test('differential maps detailed evidence to EXACT, MISSING, and INCONSISTENT primary classifications', async () => {
  const cases: Array<[string, PlayerSnapshotV1ReplayArtifactDifferentialReader, 'EXACT' | 'MISSING' | 'INCONSISTENT']> = [
    ['exact', reader([artifact()]), 'EXACT'],
    ['missing', reader([]), 'MISSING'],
    ['mismatch', reader([artifact({ snapshotSha256: 'd'.repeat(64) })]), 'INCONSISTENT'],
    ['read failure', reader(new Error('safe failure')), 'INCONSISTENT'],
  ]
  for (const [, artifactReader, classification] of cases) {
    await withJournal({ 'entry.json': journal() }, async (path) => {
      const result = await comparePlayerSnapshotV1ReplayShadowJournal(path, artifactReader)
      assert.equal(result.classification, classification)
    })
  }
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

test('dedicated --once CLI requires separate opt-in settings, emits one stable JSON result, and exits zero only for EXACT', async () => {
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
    assert.equal(JSON.parse(output[0]!).classification, 'MISSING')
    assert.equal(JSON.parse(output[0]!).records[0].classification, 'MISSING_DB_ARTIFACT')
    assert.equal(closed, true)
    await assert.rejects(() => differentialMain({ ...env, DATABASE_URL: env.M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_DATABASE_URL }, ['--once'], dependencies))

    const exactOutput: string[] = []
    assert.equal(await differentialMain(env, ['--once'], {
      createReader: () => ({ findByCommandId: async () => [artifact()] }),
      writeStdout: (value: string) => { exactOutput.push(value) },
    }), 0)
    assert.equal(JSON.parse(exactOutput[0]!).classification, 'EXACT')
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
