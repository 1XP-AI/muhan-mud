import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'
import {
  comparePlayerSnapshotV2JournalLevelShadowJournal,
} from '../src/player-snapshot-v2-journal-level-shadow-comparator.js'
import { main as shadowComparatorMain } from '../src/player-snapshot-v2-journal-level-shadow-comparator-cli.js'
import { relayPlayerSnapshotV1ArtifactsOnce } from '../src/player-snapshot-v1-artifact-relay.js'
import { NodePlayerSnapshotV2ReplayShadowJournal } from '../src/player-snapshot-v1-replay-shadow-journal.js'
import { PlayerSnapshotV2ReplayObserver } from '../src/player-snapshot-v2-replay-observer.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const characterId = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const request = 'a'.repeat(64)
const source = 'b'.repeat(64)
const digest = 'c'.repeat(64)
const readerDatabaseUrl = 'postgresql://mud_replay_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don'

const input = {
  commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source,
  snapshotSha256: digest, snapshotOctets: 42, rawLevelU8: 42,
}

/**
 * This C encoder-derived PlayerSnapshotV1 wire fixture is wrapped below in
 * synthetic headers matching the C artifact contract. This test proves the
 * Node relay-to-comparator compatibility boundary only; native PlayerStore
 * capture remains covered by its dedicated C integration harness.
 */
async function cEncodedSnapshotFixturePayload(): Promise<Buffer> {
  return Buffer.from((await readFile(
    new URL('../../../tests/fixtures/player_snapshot_v1_canonical.hex', import.meta.url),
    'utf8',
  )).trim(), 'hex')
}

function syntheticCCompatibleManifest(payload: Uint8Array): Buffer {
  return Buffer.from([
    'version=1', 'world_id=m3shadow', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4d336865726f', `request_sha256=${request}`, `post_sha256=${source}`,
    'writer_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'snapshot_format=legacy-file-manifest-v1',
    'writer_epoch=1', 'writer_revision=1', 'storage_format=1', `snapshot_octets=${payload.length}`, '',
  ].join('\n'), 'ascii')
}

function syntheticCCompatibleArtifact(payload: Uint8Array): Buffer {
  return Buffer.concat([Buffer.from([
    'version=1', 'world_id=m3shadow', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4d336865726f', `request_sha256=${request}`, `source_post_sha256=${source}`,
    'writer_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'writer_epoch=1', 'writer_revision=1',
    'storage_format=1', 'snapshot_format=player-snapshot-v1', `source_octets=${payload.length}`,
    `snapshot_sha256=${createHash('sha256').update(payload).digest('hex')}`,
    `snapshot_octets=${payload.length}`, '', '',
  ].join('\n'), 'ascii'), Buffer.from(payload)])
}

function journal(value: Record<string, unknown> = {}): string {
  return JSON.stringify({
    format: 'player-snapshot-v1-replay-shadow-journal', version: '2', commandId, characterId,
    receiptRequestSha256: request, sourcePostSha256: source, rawLevelU8: 42,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: 42, inventoryNodeCount: 0,
    },
    ...value,
  })
}

async function withJournal<T>(files: Readonly<Record<string, string>>, run: (path: string) => Promise<T>): Promise<T> {
  const path = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v2-level-shadow-comparator-'))
  try {
    await Promise.all(Object.entries(files).map(([name, body]) => writeFile(join(path, name), body, { mode: 0o600 })))
    return await run(path)
  } finally { await rm(path, { recursive: true, force: true }) }
}

test('v2 level shadow comparator preserves lexical journal indexes and all fail-closed reader outcomes as closed records', async () => {
  await withJournal({
    'e.json': journal(),
    'a.json': '{ invalid journal json',
    'c.json': journal({ rawLevelU8: 41 }),
    'd.json': journal({ commandId: '22222222-2222-4222-8222-222222222222' }),
    'b.json': journal({ commandId: '33333333-3333-4333-8333-333333333333' }),
  }, async (path) => {
    const result = await comparePlayerSnapshotV2JournalLevelShadowJournal(path, {
      findByCommandId: async (id) => {
        if (id === '22222222-2222-4222-8222-222222222222') return []
        if (id === '33333333-3333-4333-8333-333333333333') throw new Error('postgresql://secret/password')
        if (id === commandId) return [{ ...input, rawLevelU8: 41 }]
        throw new Error('unexpected test input')
      },
    })

    assert.deepEqual(result, {
      format: 'player-snapshot-v2-journal-level-shadow-comparison', version: '1', classification: 'INCONSISTENT',
      records: [
        { index: 0, classification: 'JOURNAL_INVALID' },
        { index: 1, classification: 'PROJECTION_READ_ERROR', input: { ...input, commandId: '33333333-3333-4333-8333-333333333333' } },
        { index: 2, classification: 'MATCH', input: { ...input, rawLevelU8: 41 } },
        { index: 3, classification: 'MISSING_PROJECTION', input: { ...input, commandId: '22222222-2222-4222-8222-222222222222' } },
        { index: 4, classification: 'MISMATCH_LEVEL', input },
      ],
    })
    const output = JSON.stringify(result)
    assert.equal(output.includes('postgresql://secret/password'), false)
    assert.equal(output.includes(path), false)
    assert.equal(output.includes('payload'), false)
  })
})

test('a C-encoded snapshot fixture reaches the explicit v2 journal comparator without changing synthetic evidence', async () => {
  await withJournal({}, async (journalPath) => {
    const payload = await cEncodedSnapshotFixturePayload()
    const manifest = syntheticCCompatibleManifest(payload)
    const artifact = syntheticCCompatibleArtifact(payload)
    let recordCalls = 0
    const relay = await relayPlayerSnapshotV1ArtifactsOnce('/c-playerstore-outbox', {
      recordPlayerSnapshotV1Artifact: async () => { recordCalls++; return 'RECORDED' },
    }, {
      scan: async () => [{
        name: `${commandId}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest,
      }],
    }, new PlayerSnapshotV2ReplayObserver(
      '/opt/muhan/player_snapshot_v2_replay_verify',
      async (observed) => ({
        format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
        inputDigest: createHash('sha256').update(observed).digest('hex'),
        canonicalDigest: createHash('sha256').update(observed).digest('hex'),
        canonicalOctets: observed.length, inventoryNodeCount: 0, rawLevelU8: 42,
      }),
      new NodePlayerSnapshotV2ReplayShadowJournal(journalPath),
    ))
    assert.equal(recordCalls, 1)
    assert.deepEqual(relay, {
      visited: 1, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
      replayObserved: 1, replayDisabled: 0,
      projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
    })
    assert.deepEqual(artifact, syntheticCCompatibleArtifact(payload), 'shadow observation must not mutate synthetic C-compatible artifact bytes')
    assert.deepEqual(manifest, syntheticCCompatibleManifest(payload), 'shadow observation must not mutate synthetic C-compatible receipt bytes')

    const journalFiles = await readdir(journalPath)
    assert.equal(journalFiles.length, 1)
    const journalEntry = JSON.parse(await readFile(join(journalPath, journalFiles[0]!), 'utf8'))
    assert.deepEqual(journalEntry, {
      format: 'player-snapshot-v1-replay-shadow-journal', version: '2', commandId, characterId,
      receiptRequestSha256: request, sourcePostSha256: source, rawLevelU8: 42,
      verification: {
        format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
        inputDigest: createHash('sha256').update(payload).digest('hex'),
        canonicalDigest: createHash('sha256').update(payload).digest('hex'),
        canonicalOctets: payload.length, inventoryNodeCount: 0,
      },
    })

    const journalInput = {
      commandId, characterId, receiptRequestSha256: request, sourcePostSha256: source,
      snapshotSha256: createHash('sha256').update(payload).digest('hex'), snapshotOctets: payload.length, rawLevelU8: 42,
    }
    for (const [name, rows, classification] of [
      ['match', [journalInput], 'MATCH'],
      ['missing projection', [], 'MISSING_PROJECTION'],
      ['level mismatch', [{ ...journalInput, rawLevelU8: 41 }], 'MISMATCH_LEVEL'],
      ['identity mismatch', [{ ...journalInput, characterId: 'cccccccc-cccc-4ccc-8ccc-cccccccccccc' }], 'IDENTITY_MISMATCH'],
    ] as const) {
      const result = await comparePlayerSnapshotV2JournalLevelShadowJournal(journalPath, {
        findByCommandId: async () => rows,
      })
      assert.equal(result.records[0]?.classification, classification, name)
      assert.equal(result.classification, classification === 'MATCH' ? 'MATCH' : 'INCONSISTENT', name)
    }
  })
})

test('v2 level shadow comparator preserves every pure comparator classification and the journal bound', async () => {
  const cases: Array<[string, (id: string) => Promise<readonly unknown[]>, string]> = [
    ['match', async () => [input], 'MATCH'],
    ['mismatch', async () => [{ ...input, rawLevelU8: 41 }], 'MISMATCH_LEVEL'],
    ['missing', async () => [], 'MISSING_PROJECTION'],
    ['identity', async () => [{ ...input, characterId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' }], 'IDENTITY_MISMATCH'],
    ['invalid projection', async () => [{ ...input, rawLevelU8: 256 }], 'INVALID_INPUT'],
    ['duplicate', async () => [input, input], 'UNEXPECTED_DUPLICATE'],
    ['read error', async () => { throw new Error('raw database error') }, 'PROJECTION_READ_ERROR'],
  ]
  for (const [, findByCommandId, classification] of cases) {
    await withJournal({ 'entry.json': journal() }, async (path) => {
      const result = await comparePlayerSnapshotV2JournalLevelShadowJournal(path, { findByCommandId })
      assert.equal(result.records[0]?.classification, classification)
      assert.equal(result.classification, classification === 'MATCH' ? 'MATCH' : 'INCONSISTENT')
      assert.equal(JSON.stringify(result).includes('raw database error'), false)
    })
  }

  await withJournal({}, async (path) => {
    let readerCalls = 0
    const result = await comparePlayerSnapshotV2JournalLevelShadowJournal(path, {
      findByCommandId: async () => { readerCalls++; return [input] },
    }, {
      readDirectory: async () => Array.from({ length: 257 }, (_, index) => Buffer.from(`${String(index).padStart(3, '0')}.json`)),
      readEntry: async () => { throw new Error('must not read bounded journal') },
    })
    assert.deepEqual(result.records, [{ index: 256, classification: 'JOURNAL_BOUND_EXCEEDED' }])
    assert.equal(result.classification, 'INCONSISTENT')
    assert.equal(readerCalls, 0)
  })
})

test('v2 level shadow comparator fails closed when its journal provides no evidence', async () => {
  await withJournal({}, async (path) => {
    let readerCalls = 0
    const result = await comparePlayerSnapshotV2JournalLevelShadowJournal(path, {
      findByCommandId: async () => { readerCalls++; return [input] },
    })

    assert.deepEqual(result, {
      format: 'player-snapshot-v2-journal-level-shadow-comparison', version: '1',
      classification: 'INCONSISTENT', records: [],
    })
    assert.equal(readerCalls, 0)

    const output: string[] = []
    let closed = false
    assert.equal(await shadowComparatorMain({
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH: path,
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL: readerDatabaseUrl,
    }, ['--once'], {
      createReader: () => ({
        findByCommandId: async () => { readerCalls++; return [input] },
        close: async () => { closed = true },
      }),
      writeStdout: (line) => { output.push(line) },
    }), 1)
    assert.equal(closed, true)
    assert.deepEqual(output, ['{"format":"player-snapshot-v2-journal-level-shadow-comparison","version":"1","classification":"INCONSISTENT","records":[]}\n'])
    assert.equal(readerCalls, 0)
  })
})

test('default relay entrypoints remain independent of the opt-in shadow comparator', async () => {
  const [relayCli, image] = await Promise.all([
    readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'),
    readFile(new URL('../Dockerfile', import.meta.url), 'utf8'),
  ])
  assert.doesNotMatch(relayCli, /v2-journal-level-shadow-comparator/i)
  assert.doesNotMatch(image, /v2-journal-level-shadow-comparator/i)
})

test('one-shot v2 level shadow CLI is separately opt-in, emits exactly one closed JSON line, closes its reader, and exits zero only for MATCH', async () => {
  await withJournal({ 'entry.json': journal() }, async (path) => {
    const output: string[] = []
    let closed = false
    const env = {
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH: path,
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL: readerDatabaseUrl,
      DATABASE_URL: 'postgresql://mud_writer_login@localhost/postgres',
    }
    assert.equal(await shadowComparatorMain(env, ['--once'], {
      createReader: () => ({ findByCommandId: async () => [{ ...input, rawLevelU8: 41 }], close: async () => { closed = true } }),
      writeStdout: (line) => { output.push(line) },
    }), 1)
    assert.equal(closed, true)
    assert.equal(output.length, 1)
    assert.equal(JSON.parse(output[0]!).records[0].classification, 'MISMATCH_LEVEL')
    assert.equal(output[0]!.split('\n').filter(Boolean).length, 1)

    const exactOutput: string[] = []
    assert.equal(await shadowComparatorMain(env, ['--once'], {
      createReader: () => ({ findByCommandId: async () => [input] }),
      writeStdout: (line) => { exactOutput.push(line) },
    }), 0)
    assert.equal(JSON.parse(exactOutput[0]!).classification, 'MATCH')

    await assert.rejects(() => shadowComparatorMain({
      ...env,
      DATABASE_URL: readerDatabaseUrl,
    }, ['--once'], {
      createReader: () => ({ findByCommandId: async () => [input] }), writeStdout: () => undefined,
    }))
    await assert.rejects(() => shadowComparatorMain(env, [], {
      createReader: () => ({ findByCommandId: async () => [input] }), writeStdout: () => undefined,
    }))
  })
})

test('one-shot v2 level shadow CLI rejects semantically equivalent DATABASE_URL spellings before reader creation or stdout', async () => {
  await withJournal({ 'entry.json': journal() }, async (path) => {
    let readerCalls = 0
    const output: string[] = []
    await assert.rejects(() => shadowComparatorMain({
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH: path,
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL:
        'postgresql://mud_replay_reader_login@LOCALHOST:5432/%70ostgres?application_name=level%2Dshadow&options=-c+default_transaction_read_only%3Don',
      DATABASE_URL:
        'postgres://mud_replay_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don&application_name=level-shadow',
    }, ['--once'], {
      createReader: () => { readerCalls++; return { findByCommandId: async () => [input] } },
      writeStdout: (line) => { output.push(line) },
    }))
    assert.equal(readerCalls, 0)
    assert.deepEqual(output, [])
  })
})

test('one-shot v2 level shadow CLI rejects a validated reader URL with an uncanonicalizable identity that collides with DATABASE_URL before reader creation or stdout', async () => {
  await withJournal({ 'entry.json': journal() }, async (path) => {
    let readerCalls = 0
    const output: string[] = []
    const malformedPercentReaderUrl =
      'postgresql://mud_replay_reader_login@localhost/%?options=-c%20default_transaction_read_only%3Don'
    await assert.rejects(() => shadowComparatorMain({
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH: path,
      M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL: malformedPercentReaderUrl,
      DATABASE_URL: malformedPercentReaderUrl,
    }, ['--once'], {
      createReader: () => { readerCalls++; return { findByCommandId: async () => [input] } },
      writeStdout: (line) => { output.push(line) },
    }))
    assert.equal(readerCalls, 0)
    assert.deepEqual(output, [])
  })
})
