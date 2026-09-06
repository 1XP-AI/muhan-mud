import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, readdir, rm } from 'node:fs/promises'
import { test } from 'node:test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  PlayerSnapshotV1ReplayObserver,
  playerSnapshotV1ReplayObserverFromEnvironment,
  type PlayerSnapshotV1ReplayObservationContext,
  type PlayerSnapshotV1ReplayVerifier,
} from '../src/player-snapshot-v1-replay-observer.js'
import {
  NodePlayerSnapshotV1ReplayShadowJournal,
  type PlayerSnapshotV1ReplayJournalEntry,
  type PlayerSnapshotV1ReplayShadowJournalFilesystem,
} from '../src/player-snapshot-v1-replay-shadow-journal.js'

const payload = Buffer.from('player snapshot payload')
const context: PlayerSnapshotV1ReplayObservationContext = {
  commandId: '11111111-1111-4111-8111-111111111111',
  characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  receiptRequestSha256: 'a'.repeat(64),
  sourcePostSha256: 'b'.repeat(64),
  snapshotSha256: createHash('sha256').update(payload).digest('hex'),
}

test('replay observer projects fixed verification metadata and deduplicates idempotent replays', async () => {
  const journalPath = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-replay-journal-'))
  const calls: Array<{ payload: Uint8Array, runnerPath?: string, snapshotSha256?: string }> = []
  const digest = createHash('sha256').update(payload).digest('hex')
  const verifier: PlayerSnapshotV1ReplayVerifier = async (value, options) => {
    calls.push({ payload: Buffer.from(value), runnerPath: options.runnerPath, snapshotSha256: options.snapshotSha256 })
    return {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: value.length, inventoryNodeCount: 0,
      payload: value, gameState: { inventory: ['must not be journaled'] }, ignored: 'must not be journaled',
    } as never
  }
  const observer = playerSnapshotV1ReplayObserverFromEnvironment({
    M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
    M4_PLAYER_SNAPSHOT_V1_REPLAY_JOURNAL_PATH: journalPath,
  }, verifier)

  try {
    assert.equal(await observer.observe(payload, context), 'observed')
    assert.equal(await observer.observe(payload, context), 'observed')
    assert.deepEqual(calls, [
      { payload, runnerPath: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify', snapshotSha256: context.snapshotSha256 },
    ])
    const entries = await readdir(journalPath)
    assert.equal(entries.length, 1)
    const record = JSON.parse(await readFile(join(journalPath, entries[0]!), 'utf8')) as Record<string, unknown>
    assert.deepEqual(record, {
      format: 'player-snapshot-v1-replay-shadow-journal', version: '1',
      commandId: context.commandId, characterId: context.characterId,
      receiptRequestSha256: context.receiptRequestSha256, sourcePostSha256: context.sourcePostSha256,
      verification: {
        format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
        inputDigest: digest, canonicalDigest: digest, canonicalOctets: payload.length, inventoryNodeCount: 0,
      },
    })
    assert.equal(JSON.stringify(record).includes(payload.toString('utf8')), false)
    assert.equal(JSON.stringify(record).includes('must not be journaled'), false)
  } finally {
    await rm(journalPath, { recursive: true, force: true })
  }
})

test('replay observer remains explicitly disabled without absolute runner and journal configuration', async () => {
  let called = false
  const verifier: PlayerSnapshotV1ReplayVerifier = async () => {
    called = true
    throw new Error('must not run')
  }
  for (const env of [
    {},
    { M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: 'player_snapshot_v1_replay_verify' },
    { M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/bad\0path' },
    { M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify' },
    { M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify', M4_PLAYER_SNAPSHOT_V1_REPLAY_JOURNAL_PATH: 'relative' },
  ]) {
    const observer = playerSnapshotV1ReplayObserverFromEnvironment(env, verifier)
    assert.equal(await observer.observe(payload, context), 'disabled')
  }
  assert.equal(called, false)
})

test('replay observer propagates the immutable artifact digest and reports verifier failures diagnostically', async () => {
  const journalPath = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-replay-observer-diagnostic-'))
  const snapshotSha256 = createHash('sha256').update(payload).digest('hex')
  const options: Array<{ runnerPath?: string, snapshotSha256?: string }> = []
  const observer = playerSnapshotV1ReplayObserverFromEnvironment({
    M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
    M4_PLAYER_SNAPSHOT_V1_REPLAY_JOURNAL_PATH: journalPath,
  }, async (_value, value) => {
    options.push(value)
    throw new Error('digest mismatch must remain diagnostic')
  })

  try {
    assert.equal(await observer.observe(payload, { ...context, snapshotSha256 }), 'failed')
    assert.deepEqual(options, [{ runnerPath: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify', snapshotSha256 }])
    assert.deepEqual(await readdir(journalPath), [])
  } finally {
    await rm(journalPath, { recursive: true, force: true })
  }
})

test('shadow journal canonicalizes allowlisted metadata across extra fields, property order, concurrent retries, and restart', async () => {
  const journalPath = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-replay-journal-canonical-'))
  const digest = createHash('sha256').update(payload).digest('hex')
  const first = {
    gameState: { gold: 1 }, payload: Buffer.from(payload), inventoryNodeCount: 0,
    canonicalOctets: payload.length, canonicalDigest: digest, inputDigest: digest,
    algorithm: 'sha-256' as const, version: '1' as const, format: 'player-snapshot-v1-replay-verification' as const,
  }
  const second = {
    format: 'player-snapshot-v1-replay-verification' as const, version: '1' as const, algorithm: 'sha-256' as const,
    inputDigest: digest, canonicalDigest: digest, canonicalOctets: payload.length, inventoryNodeCount: 0,
    gameState: { gold: 999 }, payload: Buffer.from('different ignored payload'),
  }
  const observer = (result: unknown) => playerSnapshotV1ReplayObserverFromEnvironment({
    M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
    M4_PLAYER_SNAPSHOT_V1_REPLAY_JOURNAL_PATH: journalPath,
  }, async () => result as never)

  try {
    assert.deepEqual(await Promise.all([
      observer(first).observe(payload, context),
      observer(second).observe(payload, context),
    ]), ['observed', 'observed'])
    const [filename] = await readdir(journalPath)
    assert.ok(filename)
    const originalBytes = await readFile(join(journalPath, filename))
    assert.equal(await observer(second).observe(payload, context), 'observed')
    assert.deepEqual(await readdir(journalPath), [filename])
    assert.deepEqual(await readFile(join(journalPath, filename)), originalBytes)
    const record = JSON.parse(originalBytes.toString('utf8')) as Record<string, unknown>
    assert.equal(JSON.stringify(record).includes('gameState'), false)
    assert.equal(JSON.stringify(record).includes('different ignored payload'), false)
  } finally {
    await rm(journalPath, { recursive: true, force: true })
  }
})

test('shadow journal derives bytes and identity only from its normalized allowlist', async () => {
  const journalPath = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-replay-journal-direct-canonical-'))
  const digest = createHash('sha256').update(payload).digest('hex')
  const first = {
    ignoredRoot: 'not durable',
    sourcePostSha256: context.sourcePostSha256,
    receiptRequestSha256: context.receiptRequestSha256,
    characterId: context.characterId,
    commandId: context.commandId,
    version: 'unexpected root version',
    format: 'unexpected root format',
    verification: {
      ignored: 'not durable', gameState: { level: 1 }, inventoryNodeCount: 0, canonicalOctets: payload.length,
      canonicalDigest: digest, inputDigest: digest, algorithm: 'sha-256', version: '1', format: 'player-snapshot-v1-replay-verification',
    },
  } as PlayerSnapshotV1ReplayJournalEntry
  const second = {
    format: 'player-snapshot-v1-replay-shadow-journal', version: '1',
    commandId: context.commandId, characterId: context.characterId,
    receiptRequestSha256: context.receiptRequestSha256, sourcePostSha256: context.sourcePostSha256,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256', inputDigest: digest,
      canonicalDigest: digest, canonicalOctets: payload.length, inventoryNodeCount: 0, ignored: 'different extra', payload,
    },
  } as PlayerSnapshotV1ReplayJournalEntry
  try {
    const outcomes = await Promise.all([
      new NodePlayerSnapshotV1ReplayShadowJournal(journalPath).append(first),
      new NodePlayerSnapshotV1ReplayShadowJournal(journalPath).append(second),
    ])
    assert.deepEqual(new Set(outcomes), new Set(['appended', 'duplicate']))
    const [filename] = await readdir(journalPath)
    assert.ok(filename)
    const bytes = await readFile(join(journalPath, filename))
    assert.equal(await new NodePlayerSnapshotV1ReplayShadowJournal(journalPath).append(first), 'duplicate')
    assert.deepEqual(await readdir(journalPath), [filename])
    assert.equal(bytes.toString('utf8').includes('not durable'), false)
    assert.equal(bytes.toString('utf8').includes('different extra'), false)
  } finally {
    await rm(journalPath, { recursive: true, force: true })
  }
})

test('public observer construction cannot bypass absolute runner or Node journal path validation', async () => {
  let verifierCalls = 0
  let appendCalls = 0
  const journal = { append: async () => { appendCalls++; return 'appended' as const } }
  const verifier: PlayerSnapshotV1ReplayVerifier = async () => {
    verifierCalls++
    throw new Error('must not run')
  }
  for (const runnerPath of ['relative-runner', '/bad\0runner']) {
    const observer = new PlayerSnapshotV1ReplayObserver(runnerPath, verifier, journal)
    assert.equal(await observer.observe(payload, context), 'disabled')
  }
  const invalidJournal = new NodePlayerSnapshotV1ReplayShadowJournal('relative-journal')
  const observer = new PlayerSnapshotV1ReplayObserver('/usr/local/libexec/muhan/player_snapshot_v1_replay_verify', verifier, invalidJournal)
  assert.equal(await observer.observe(payload, context), 'disabled')
  assert.equal(verifierCalls, 0)
  assert.equal(appendCalls, 0)
})

test('replay observer treats runner and report failures as disabled without exposing their details', async () => {
  const observer = new PlayerSnapshotV1ReplayObserver(
    '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
    async () => { throw new Error('untrusted runner output') },
  )

  assert.equal(await observer.observe(payload, context), 'disabled')
})

test('replay observer records malformed runtime results as diagnostic failures', async () => {
  const malformedResults: unknown[] = [
    undefined,
    null,
    'observed',
    {},
    {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
      inputDigest: 'a'.repeat(64), canonicalDigest: 'a'.repeat(64), canonicalOctets: '22', inventoryNodeCount: 0,
    },
  ]

  for (const result of malformedResults) {
    let verifierCalls = 0
    const journal = new NodePlayerSnapshotV1ReplayShadowJournal('/explicit/journal', {
      open: async () => { throw new Error('malformed verification must not be journaled') },
      link: async () => { throw new Error('malformed verification must not be published') },
      unlink: async () => { throw new Error('malformed verification must not need cleanup') },
    })
    const observer = new PlayerSnapshotV1ReplayObserver(
      '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
      async () => { verifierCalls++; return result as never },
      journal,
    )
    assert.equal(await observer.observe(payload, context), 'failed')
    assert.equal(verifierCalls, 1)
  }
})

test('journal failures remain diagnostic and never leave a completed entry', async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-replay-journal-failure-'))
  const journalPath = join(root, 'not-a-directory')
  const digest = createHash('sha256').update(payload).digest('hex')
  await (await import('node:fs/promises')).writeFile(journalPath, 'not a journal directory')
  const observer = playerSnapshotV1ReplayObserverFromEnvironment({
    M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
    M4_PLAYER_SNAPSHOT_V1_REPLAY_JOURNAL_PATH: journalPath,
  }, async () => ({
    format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
    inputDigest: digest, canonicalDigest: digest, canonicalOctets: payload.length, inventoryNodeCount: 0,
  }))
  try {
    assert.equal(await observer.observe(payload, context), 'failed')
    assert.equal(await readFile(journalPath, 'utf8'), 'not a journal directory')
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('shadow journal ignores a partial write instead of publishing an entry', async () => {
  let published = false
  const temporaryPaths: string[] = []
  const journal = new NodePlayerSnapshotV1ReplayShadowJournal('/explicit/journal', {
    open: async () => ({
      writeFile: async () => { throw new Error('partial write') },
      sync: async () => undefined,
      close: async () => undefined,
    }),
    link: async () => { published = true },
    unlink: async (path) => { temporaryPaths.push(path) },
  })
  const entry: PlayerSnapshotV1ReplayJournalEntry = {
    format: 'player-snapshot-v1-replay-shadow-journal', version: '1',
    commandId: context.commandId, characterId: context.characterId,
    receiptRequestSha256: context.receiptRequestSha256, sourcePostSha256: context.sourcePostSha256,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
      inputDigest: createHash('sha256').update(payload).digest('hex'), canonicalDigest: createHash('sha256').update(payload).digest('hex'),
      canonicalOctets: payload.length, inventoryNodeCount: 0,
    },
  }
  await assert.rejects(() => journal.append(entry))
  assert.equal(published, false)
  assert.equal(temporaryPaths.length, 1)
})

function completeJournalEntry(): PlayerSnapshotV1ReplayJournalEntry {
  const digest = createHash('sha256').update(payload).digest('hex')
  return {
    format: 'player-snapshot-v1-replay-shadow-journal', version: '1',
    commandId: context.commandId, characterId: context.characterId,
    receiptRequestSha256: context.receiptRequestSha256, sourcePostSha256: context.sourcePostSha256,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: payload.length, inventoryNodeCount: 0,
    },
  }
}

test('shadow journal reports both an incomplete append and failed temporary cleanup without publication', async () => {
  for (const failedStep of ['open', 'write', 'sync', 'close', 'link'] as const) {
    const temporaryPaths: string[] = []
    let linkAttempts = 0
    let closeAttempts = 0
    const appendError = new Error(`${failedStep} failed`)
    const cleanupError = new Error('unlink failed')
    const filesystem: PlayerSnapshotV1ReplayShadowJournalFilesystem = {
      open: async () => {
        if (failedStep === 'open') throw appendError
        return {
          writeFile: async () => { if (failedStep === 'write') throw appendError },
          sync: async () => { if (failedStep === 'sync') throw appendError },
          close: async () => { if (failedStep === 'close' && ++closeAttempts === 1) throw appendError },
        }
      },
      link: async () => {
        linkAttempts++
        if (failedStep === 'link') throw appendError
      },
      unlink: async (path) => { temporaryPaths.push(path); throw cleanupError },
    }
    await assert.rejects(
      () => new NodePlayerSnapshotV1ReplayShadowJournal('/explicit/journal', filesystem).append(completeJournalEntry()),
      (error: unknown) => {
        assert.ok(error instanceof AggregateError)
        assert.deepEqual(error.errors, [appendError, cleanupError])
        return true
      },
    )
    assert.equal(linkAttempts, failedStep === 'link' ? 1 : 0)
    assert.equal(temporaryPaths.length, 1)
    assert.match(temporaryPaths[0]!, /^\/explicit\/journal\/.+\.tmp$/)
  }
})

test('shadow journal reports failed cleanup after publication instead of returning success', async () => {
  let published = false
  const cleanupError = new Error('unlink failed')
  const journal = new NodePlayerSnapshotV1ReplayShadowJournal('/explicit/journal', {
    open: async () => ({
      writeFile: async () => undefined,
      sync: async () => undefined,
      close: async () => undefined,
    }),
    link: async () => { published = true },
    unlink: async () => { throw cleanupError },
  })

  await assert.rejects(
    () => journal.append(completeJournalEntry()),
    (error: unknown) => {
      assert.ok(error instanceof AggregateError)
      assert.deepEqual(error.errors, [cleanupError])
      return true
    },
  )
  assert.equal(published, true)
})

test('every journal filesystem failure is diagnostic without escaping the observer boundary', async () => {
  for (const failedStep of ['open', 'write', 'sync', 'close', 'link', 'unlink'] as const) {
    let published = false
    const temporaryPaths: string[] = []
    const journal = new NodePlayerSnapshotV1ReplayShadowJournal('/explicit/journal', {
      open: async () => {
        if (failedStep === 'open') throw new Error('open failed')
        return {
          writeFile: async () => { if (failedStep === 'write') throw new Error('write failed') },
          sync: async () => { if (failedStep === 'sync') throw new Error('sync failed') },
          close: async () => { if (failedStep === 'close') throw new Error('close failed') },
        }
      },
      link: async () => {
        if (failedStep === 'link') throw new Error('link failed')
        published = true
      },
      unlink: async (path) => {
        temporaryPaths.push(path)
        if (failedStep === 'unlink') throw new Error('unlink failed')
      },
    })
    const observer = new PlayerSnapshotV1ReplayObserver(
      '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
      async () => completeJournalEntry().verification,
      journal,
    )
    assert.equal(await observer.observe(payload, context), 'failed')
    assert.equal(published, failedStep === 'unlink')
    assert.equal(temporaryPaths.length, 1)
  }
})
