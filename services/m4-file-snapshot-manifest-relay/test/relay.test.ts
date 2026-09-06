import assert from 'node:assert/strict'
import { chmod, link, mkdtemp, open as openFile, rename, rm, writeFile } from 'node:fs/promises'
import { test } from 'node:test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createHash } from 'node:crypto'
import { parsePlayerSnapshotV1Artifact } from '../src/player-snapshot-v1-artifact.js'
import { relayPlayerSnapshotV1ArtifactsOnce, type PlayerSnapshotV1ArtifactFilesystem } from '../src/player-snapshot-v1-artifact-relay.js'
import { MAX_MANIFEST_BYTES, isManifestFilename, parseManifest } from '../src/manifest.js'
import { NodeManifestFilesystem, relayOnce, scanImmutableOutboxFiles, type ManifestFilesystem } from '../src/relay.js'
import { NodePlayerSnapshotV1ArtifactFilesystem } from '../src/player-snapshot-v1-artifact-relay.js'
import { MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS, PLAYER_SNAPSHOT_V1_SUFFIX } from '../src/player-snapshot-v1-artifact.js'
import { PlayerSnapshotV1ReplayObserver, type PlayerSnapshotV1ReplayObserver } from '../src/player-snapshot-v1-replay-observer.js'
import { NodePlayerSnapshotV1ReplayShadowJournal } from '../src/player-snapshot-v1-replay-shadow-journal.js'
import { main as artifactRelayMain } from '../src/player-snapshot-v1-artifact-cli.js'
import { main as manifestFirstRelayMain } from '../src/player-snapshot-v1-manifest-first-cli.js'
import { relayPlayerSnapshotV1ManifestFirstOnce, type PlayerSnapshotV1ManifestFirstFilesystem } from '../src/player-snapshot-v1-manifest-first-relay.js'
import { PostgresManifestStore, PostgresPlayerSnapshotV1ArtifactStore, PostgresPlayerSnapshotV1LevelProjectionStore, assertDatabaseUrl, type ManifestStore, type PlayerSnapshotV1ArtifactFulfillmentOutcome, type PlayerSnapshotV1ArtifactFulfillmentStore, type PlayerSnapshotV1ArtifactStore, type PlayerSnapshotV1LevelProjectionStore, type PgClient, type PgPool } from '../src/store.js'

const first = '11111111-1111-4111-8111-111111111111'
const second = '22222222-2222-4222-8222-222222222222'

function body(commandId: string, revision = '1'): Uint8Array {
  const value = `version=1\nworld_id=muhan-01\ncharacter_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\ncommand_id=${commandId}\ncanonical_name_hex=4d3341\nrequest_sha256=${'a'.repeat(64)}\npost_sha256=${'b'.repeat(64)}\nwriter_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb\nsnapshot_format=legacy-file-manifest-v1\nwriter_epoch=7\nwriter_revision=${revision}\nstorage_format=1\nsnapshot_octets=128\n`
  return Buffer.from(value)
}

function u16(value: number): Buffer {
  const bytes = Buffer.alloc(2)
  bytes.writeUInt16BE(value)
  return bytes
}

function u32(value: number): Buffer {
  const bytes = Buffer.alloc(4)
  bytes.writeUInt32BE(value)
  return bytes
}

/** A test-created canonical PlayerSnapshotV1 CDTO payload with an empty object graph. */
function playerSnapshotV1(): Uint8Array {
  const types = [9, 9, 9, 9, 9, 9, 1, 5, 5, 5, 5, 6, 5, 5, 5, 5, 5, 6, 6, 6, 6, 5, 5, 8, 8, 6, 6, 6, 6, 9, 9, 9, 9, 9, 5, 9, 6, 9, 9]
  const lengths = [80, 80, 80, 20, 20, 20, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 2, 2, 2, 2, 1, 1, 8, 8, 2, 2, 2, 2, 40, 32, 16, 8, 16, 1, 20, 2, 100, 810]
  const graphBody = Buffer.concat([u16(1), Buffer.from([3]), u32(4), u32(0)])
  const graph = Buffer.concat([
    Buffer.from('MUHCDTO\0', 'ascii'), Buffer.from([0, 1, 0, 6]), u32(graphBody.length), graphBody,
    createHash('sha256').update(graphBody).digest(),
  ])
  const fields = lengths.map((length, index) => Buffer.concat([
    u16(index + 1), Buffer.from([types[index]!]), u32(length), Buffer.alloc(length),
  ]))
  fields.push(Buffer.concat([u16(40), Buffer.from([9]), u32(graph.length), graph]))
  const payload = Buffer.concat(fields)
  return Buffer.concat([
    Buffer.from('MUHCDTO\0', 'ascii'), Buffer.from([0, 1, 0, 7]), u32(payload.length), payload,
    createHash('sha256').update(payload).digest(),
  ])
}

/** Test-created native artifact: canonical 15-line header, blank line, CDTO. */
function playerSnapshotV1Artifact(payload = playerSnapshotV1(), overrides: Partial<Record<string, string>> = {}): Uint8Array {
  const values = {
    world_id: 'muhan-01',
    character_id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    command_id: first,
    canonical_name_hex: '4d3341',
    request_sha256: 'a'.repeat(64),
    source_post_sha256: 'b'.repeat(64),
    writer_instance_id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
    writer_epoch: '7',
    writer_revision: '1',
    storage_format: '1',
    snapshot_format: 'player-snapshot-v1',
    source_octets: '128',
    snapshot_sha256: createHash('sha256').update(payload).digest('hex'),
    snapshot_octets: String(payload.length),
    ...overrides,
  }
  return Buffer.concat([Buffer.from([
    'version=1', `world_id=${values.world_id}`, `character_id=${values.character_id}`,
    `command_id=${values.command_id}`, `canonical_name_hex=${values.canonical_name_hex}`,
    `request_sha256=${values.request_sha256}`, `source_post_sha256=${values.source_post_sha256}`,
    `writer_instance_id=${values.writer_instance_id}`, `writer_epoch=${values.writer_epoch}`,
    `writer_revision=${values.writer_revision}`, `storage_format=${values.storage_format}`,
    `snapshot_format=${values.snapshot_format}`, `source_octets=${values.source_octets}`,
    `snapshot_sha256=${values.snapshot_sha256}`, `snapshot_octets=${values.snapshot_octets}`, '', '',
  ].join('\n'), 'ascii'), Buffer.from(payload)])
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

test('filesystem rejects unsafe links and preserves every evidence file', { skip: process.platform !== 'linux' }, async () => {
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

test('macOS root rename/replacement race fails closed before reading replacement leaves', async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-relay-race-root-'))
  const replacement = await mkdtemp(join(tmpdir(), 'm4-relay-race-replacement-'))
  const displaced = `${root}.displaced`
  try {
    await writeFile(join(replacement, `${first}.manifest`), body(first), { mode: 0o600 })
    await rename(root, displaced)
    await rename(replacement, root)
    await assert.rejects(() => new NodeManifestFilesystem('darwin').scan(root))
  } finally {
    await rm(root, { recursive: true, force: true })
    await rm(displaced, { recursive: true, force: true })
    await rm(replacement, { recursive: true, force: true })
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

test('relays one canonical PlayerSnapshotV1 artifact separately from the legacy manifest parser', async () => {
  const manifest = body(first)
  const payload = playerSnapshotV1()
  const artifact = playerSnapshotV1Artifact(payload)
  const originalArtifact = Buffer.from(artifact)
  const originalManifest = Buffer.from(manifest)
  const calls: string[] = []
  const rows = new Map<string, ReturnType<typeof parsePlayerSnapshotV1Artifact>>()
  const legacyEvidence = { receipt: Buffer.from(manifest), head: Buffer.from('absent'), bytes: Buffer.from('legacy-player-bytes') }
  const originalLegacyEvidence = { receipt: Buffer.from(legacyEvidence.receipt), head: Buffer.from(legacyEvidence.head), bytes: Buffer.from(legacyEvidence.bytes) }
  const fs: PlayerSnapshotV1ArtifactFilesystem = { scan: async () => [
    { name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest },
    { name: `${second}.player-snapshot-v1`, bytes: Buffer.from('malformed'), receiptManifestBytes: body(second) },
  ] }
  const store: PlayerSnapshotV1ArtifactStore = { recordPlayerSnapshotV1Artifact: async (value) => {
    calls.push(value.commandId)
    assert.equal(value.snapshotFormat, 'player-snapshot-v1')
    assert.equal(value.snapshotOctets, payload.length)
    assert.deepEqual(value.payload, payload)
    const existing = rows.get(value.commandId)
    if (!existing) { rows.set(value.commandId, value); return 'RECORDED' }
    assert.deepEqual(existing, value)
    return 'EXACT_RETRY'
  } }
  assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, fs), {
    visited: 2, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 1, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
    projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  })
  assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, fs), {
    visited: 2, valid: 1, delivered: 1, recorded: 0, exactRetry: 1, invalid: 1, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
    projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  })
  assert.deepEqual(calls, [first, first])
  assert.equal(rows.size, 1)
  assert.deepEqual([...rows.values()].map(() => 'EXACT'), ['EXACT'])
  assert.deepEqual(artifact, originalArtifact)
  assert.deepEqual(manifest, originalManifest)
  assert.deepEqual(legacyEvidence, originalLegacyEvidence)
  assert.throws(() => parsePlayerSnapshotV1Artifact(`${first}.player-snapshot-v1`, Buffer.from('bad'), parseManifest(manifest)))
})

test('artifact and receipt tuple mismatches fail closed before every database side effect and preserve source evidence', async () => {
  const receipt = body(first)
  const artifact = playerSnapshotV1Artifact(playerSnapshotV1(), { request_sha256: 'c'.repeat(64) })
  const sourceEvidence = {
    artifact: Buffer.from(artifact), receipt: Buffer.from(receipt), source: Buffer.from('native-source-evidence'),
  }
  const originalEvidence = {
    artifact: Buffer.from(sourceEvidence.artifact), receipt: Buffer.from(sourceEvidence.receipt), source: Buffer.from(sourceEvidence.source),
  }
  const calls: string[] = []
  const store: PlayerSnapshotV1ArtifactStore = {
    recordPlayerSnapshotV1Artifact: async () => { calls.push('artifact'); return 'RECORDED' },
  }
  const fulfillment: PlayerSnapshotV1ArtifactFulfillmentStore = {
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => { calls.push('fulfillment'); return 'FULFILLED' },
  }
  const projection: PlayerSnapshotV1LevelProjectionStore = {
    recordPlayerSnapshotV1LevelProjection: async () => { calls.push('projection'); return 'RECORDED' },
  }
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{
      name: `${first}.player-snapshot-v1`, bytes: sourceEvidence.artifact, receiptManifestBytes: sourceEvidence.receipt,
    }],
  }

  const result = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem, undefined, fulfillment, projection)
  assert.equal(result.visited, 1)
  assert.equal(result.valid, 0)
  assert.equal(result.invalid, 1)
  assert.equal(result.delivered, 0)
  assert.equal(result.fulfillmentDelivered, 0)
  assert.equal(result.projectionDelivered, 0)
  assert.deepEqual(calls, [])
  assert.deepEqual(sourceEvidence, originalEvidence)
})

test('records PlayerSnapshotV1 level projections only after settled artifact evidence, including exact retries', async () => {
  const manifests = new Map([[first, body(first)], [second, body(second)]])
  const artifacts = new Map([
    [first, playerSnapshotV1Artifact(playerSnapshotV1(), { command_id: first })],
    [second, playerSnapshotV1Artifact(playerSnapshotV1(), { command_id: second })],
  ])
  const calls: string[] = []
  const artifactAttempts = new Map<string, number>()
  const projectionAttempts = new Map<string, number>()
  const projectionInputs: unknown[] = []
  const store: PlayerSnapshotV1ArtifactStore = {
    recordPlayerSnapshotV1Artifact: async (artifact) => {
      calls.push(`artifact:${artifact.commandId}`)
      const attempts = (artifactAttempts.get(artifact.commandId) ?? 0) + 1
      artifactAttempts.set(artifact.commandId, attempts)
      return attempts === 1 ? 'RECORDED' : 'EXACT_RETRY'
    },
  }
  const projection: PlayerSnapshotV1LevelProjectionStore = {
    recordPlayerSnapshotV1LevelProjection: async (input) => {
      calls.push(`projection:${input.commandId}`)
      projectionInputs.push(input)
      const attempts = (projectionAttempts.get(input.commandId) ?? 0) + 1
      projectionAttempts.set(input.commandId, attempts)
      return attempts === 1 ? 'RECORDED' : 'EXACT_RETRY'
    },
  }
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [first, second].map((commandId) => ({
      name: `${commandId}.player-snapshot-v1`, bytes: artifacts.get(commandId), receiptManifestBytes: manifests.get(commandId),
    })),
  }

  assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem, undefined, projection), {
    visited: 2, valid: 2, delivered: 2, recorded: 2, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
    replayObserved: 0, replayDisabled: 2,
    projectionDelivered: 2, projectionRecorded: 2, projectionExactRetry: 0,
    projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  })
  assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem, undefined, projection), {
    visited: 2, valid: 2, delivered: 2, recorded: 0, exactRetry: 2, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
    replayObserved: 0, replayDisabled: 2,
    projectionDelivered: 2, projectionRecorded: 0, projectionExactRetry: 2,
    projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  })
  assert.deepEqual(calls, [
    `artifact:${first}`, `projection:${first}`, `artifact:${second}`, `projection:${second}`,
    `artifact:${first}`, `projection:${first}`, `artifact:${second}`, `projection:${second}`,
  ])
  assert.deepEqual(projectionInputs, [first, second, first, second].map((commandId) => ({
    characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', commandId,
    receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64), sourceOctets: '128',
  })))
})

test('suppresses projections for unsettled artifact recording and continues after projection database failures', async () => {
  const commands = [
    first, second, '33333333-3333-4333-8333-333333333333', '44444444-4444-4444-8444-444444444444',
    '55555555-5555-4555-8555-555555555555', '66666666-6666-4666-8666-666666666666',
    '77777777-7777-4777-8777-777777777777', '88888888-8888-4888-8888-888888888888',
  ]
  const evidence = commands.map((commandId) => playerSnapshotV1Artifact(playerSnapshotV1(), { command_id: commandId }))
  const originalEvidence = evidence.map((artifact) => Buffer.from(artifact))
  const artifactCalls: string[] = []
  const projectionCalls: string[] = []
  const projectionErrors = ['22023', 'P0001', '08P01', 'ECONNRESET', undefined]
  const store: PlayerSnapshotV1ArtifactStore = {
    recordPlayerSnapshotV1Artifact: async (artifact) => {
      artifactCalls.push(artifact.commandId)
      if (artifact.commandId === first) throw Object.assign(new Error('artifact conflict'), { code: 'P0001' })
      if (artifact.commandId === second) return 'UNEXPECTED' as never
      return 'RECORDED'
    },
  }
  const projection: PlayerSnapshotV1LevelProjectionStore = {
    recordPlayerSnapshotV1LevelProjection: async (input) => {
      projectionCalls.push(input.commandId)
      const code = projectionErrors[projectionCalls.length - 1]
      if (projectionCalls.length <= projectionErrors.length) throw Object.assign(new Error('projection failed'), code ? { code } : {})
      return 'RECORDED'
    },
  }
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => commands.map((commandId, index) => ({
      name: `${commandId}.player-snapshot-v1`, bytes: evidence[index], receiptManifestBytes: body(commandId),
    })),
  }

  const result = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem, undefined, projection)
  assert.deepEqual(artifactCalls, commands)
  assert.deepEqual(projectionCalls, commands.slice(2))
  assert.deepEqual(result, {
    visited: 8, valid: 8, delivered: 6, recorded: 6, exactRetry: 0, invalid: 0, conflict: 1, retryable: 0, unknown: 1, ioError: 0,
    replayObserved: 0, replayDisabled: 8,
    projectionDelivered: 1, projectionRecorded: 1, projectionExactRetry: 0,
    projectionInvalid: 1, projectionConflict: 1, projectionRetryable: 2, projectionUnknown: 1,
  })
  assert.deepEqual(evidence, originalEvidence)
})

test('replay observation is payload-only and never changes PlayerSnapshotV1 recording', async () => {
  const manifest = body(first)
  const payload = playerSnapshotV1()
  const artifact = playerSnapshotV1Artifact(payload)
  const observed: Uint8Array[] = []
  const contexts: unknown[] = []
  const observer: PlayerSnapshotV1ReplayObserver = {
    observe: async (value, context) => { observed.push(Buffer.from(value)); contexts.push(context); throw new Error('untrusted runner failure') },
  }
  let records = 0
  const store: PlayerSnapshotV1ArtifactStore = {
    recordPlayerSnapshotV1Artifact: async () => { records++; return 'RECORDED' },
  }
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest }],
  }

  assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem, observer), {
    visited: 1, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
    replayObserved: 0, replayDisabled: 0, replayFailed: 1,
    projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  })
  assert.equal(records, 1)
  assert.deepEqual(observed, [payload])
  assert.deepEqual(contexts, [{
    commandId: first, characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64),
    snapshotSha256: createHash('sha256').update(payload).digest('hex'),
  }])
})

test('every replay journal filesystem failure exposes its temporary and publish state while PlayerSnapshotV1 recording remains intact', async () => {
  const manifest = body(first)
  const payload = playerSnapshotV1()
  const artifact = playerSnapshotV1Artifact(payload)
  const digest = createHash('sha256').update(payload).digest('hex')
  for (const failedStep of ['open', 'write', 'sync', 'close', 'link', 'unlink'] as const) {
    const temporaryPaths: string[] = []
    let published = false
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
      async () => ({
        format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
        inputDigest: digest, canonicalDigest: digest, canonicalOctets: payload.length, inventoryNodeCount: 0,
      }),
      journal,
    )
    let recorded = 0
    const result = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', {
      recordPlayerSnapshotV1Artifact: async () => { recorded++; return 'RECORDED' },
    }, {
      scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest }],
    }, observer)
    assert.equal(recorded, 1)
    assert.equal(temporaryPaths.length, 1)
    assert.equal(published, failedStep === 'unlink')
    assert.deepEqual(result, {
      visited: 1, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
      replayObserved: 0, replayDisabled: 0, replayFailed: 1,
      projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
    })
  }
})

test('a successful replay observation preserves recorder input and established relay aggregates', async () => {
  const manifest = body(first)
  const payload = playerSnapshotV1()
  const artifact = playerSnapshotV1Artifact(payload)
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest }],
  }
  const recordedArtifacts: ReturnType<typeof parsePlayerSnapshotV1Artifact>[] = []
  const store: PlayerSnapshotV1ArtifactStore = {
    recordPlayerSnapshotV1Artifact: async (value) => {
      recordedArtifacts.push(value)
      return 'RECORDED'
    },
  }
  const observer: PlayerSnapshotV1ReplayObserver = { observe: async (value) => {
    assert.deepEqual(value, payload)
    return 'observed'
  } }

  const baseline = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem)
  const observed = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', store, filesystem, observer)

  assert.deepEqual(recordedArtifacts, [
    parsePlayerSnapshotV1Artifact(`${first}.player-snapshot-v1`, artifact, parseManifest(manifest)),
    parsePlayerSnapshotV1Artifact(`${first}.player-snapshot-v1`, artifact, parseManifest(manifest)),
  ])
  const { replayObserved: baselineObserved, replayDisabled: baselineDisabled, ...baselineAggregate } = baseline
  const { replayObserved: observedCount, replayDisabled: observedDisabled, ...observedAggregate } = observed
  assert.deepEqual(observedAggregate, baselineAggregate)
  assert.deepEqual({ baselineObserved, baselineDisabled, observedCount, observedDisabled }, {
    baselineObserved: 0, baselineDisabled: 1, observedCount: 1, observedDisabled: 0,
  })
  assert.deepEqual(artifact, playerSnapshotV1Artifact(payload))
})

test('PlayerSnapshotV1 artifact requires the exact native header and receipt agreement', () => {
  const receipt = parseManifest(body(first))
  const payload = playerSnapshotV1()
  const artifact = playerSnapshotV1Artifact(payload)
  const parsed = parsePlayerSnapshotV1Artifact(`${first}.player-snapshot-v1`, artifact, receipt)
  assert.deepEqual(parsed.payload, payload)
  assert.equal(parsed.snapshotSha256, createHash('sha256').update(payload).digest('hex'))
  assert.throws(() => parsePlayerSnapshotV1Artifact(`${first}.player-snapshot-v1`, payload, receipt))
  assert.throws(() => parsePlayerSnapshotV1Artifact(
    `${first}.player-snapshot-v1`, playerSnapshotV1Artifact(payload, { writer_revision: '2' }), receipt,
  ))
  assert.throws(() => parsePlayerSnapshotV1Artifact(
    `${first}.player-snapshot-v1`, playerSnapshotV1Artifact(payload, { source_post_sha256: 'c'.repeat(64) }), receipt,
  ))
  assert.throws(() => parsePlayerSnapshotV1Artifact(
    `${first}.player-snapshot-v1`, Buffer.from(artifact.toString('ascii').replace('writer_epoch=7', 'writer_epoch=07'), 'ascii'), receipt,
  ))
  assert.throws(() => parsePlayerSnapshotV1Artifact(
    `${first}.player-snapshot-v1`, playerSnapshotV1Artifact(payload, { snapshot_sha256: 'd'.repeat(64) }), receipt,
  ))
})

test('artifact filesystem pairs one immutable PlayerSnapshotV1 file with its canonical receipt', { skip: process.platform !== 'linux' }, async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-'))
  const manifest = Buffer.from(body(first))
  const artifact = Buffer.from(playerSnapshotV1Artifact())
  try {
    await chmod(root, 0o700)
    await writeFile(join(root, `${first}.manifest`), manifest, { mode: 0o600 })
    await writeFile(join(root, `${first}.player-snapshot-v1`), artifact, { mode: 0o600 })
    const files = await new NodePlayerSnapshotV1ArtifactFilesystem().scan(root)
    assert.deepEqual(files, [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest }])
    assert.deepEqual(await import('node:fs/promises').then(({ readFile }) => readFile(join(root, `${first}.manifest`))), manifest)
    assert.deepEqual(await import('node:fs/promises').then(({ readFile }) => readFile(join(root, `${first}.player-snapshot-v1`))), artifact)
  } finally {
    await rm(root, { recursive: true, force: true })
  }
})

test('artifact filesystem never emits a cross-root receipt/artifact pair during a scanner-stage root replacement', { skip: process.platform !== 'linux', concurrency: false }, async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-pair-race-'))
  const replacement = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-pair-race-replacement-'))
  const displaced = `${root}.displaced`
  const receiptA = Buffer.from(body(first, '1'))
  const artifactA = Buffer.from(playerSnapshotV1Artifact(playerSnapshotV1(), { writer_revision: '1' }))
  const receiptB = Buffer.from(body(first, '2'))
  const artifactB = Buffer.from(playerSnapshotV1Artifact(playerSnapshotV1(), { writer_revision: '2' }))
  const probe = await openFile(join(root, 'probe'), 'w+')
  const handlePrototype = Object.getPrototypeOf(probe) as {
    read: (...args: unknown[]) => Promise<{ bytesRead: number }>
  }
  const originalRead = handlePrototype.read
  let replaced = false
  try {
    await chmod(root, 0o700)
    await chmod(replacement, 0o700)
    await probe.close()
    await rm(join(root, 'probe'))
    await writeFile(join(root, `${first}.manifest`), receiptA, { mode: 0o600 })
    await writeFile(join(root, `${first}.player-snapshot-v1`), artifactA, { mode: 0o600 })
    await writeFile(join(replacement, `${first}.manifest`), receiptB, { mode: 0o600 })
    await writeFile(join(replacement, `${first}.player-snapshot-v1`), artifactB, { mode: 0o600 })
    handlePrototype.read = async function (this: object, ...args: unknown[]) {
      const result = await originalRead.apply(this, args)
      // The receipt is lexically first. Replace the pathname between evidence
      // reads; the one-root scanner must keep reading A through its root fd.
      if (!replaced) {
        replaced = true
        await rename(root, displaced)
        await rename(replacement, root)
      }
      return result
    }
    const files = await new NodePlayerSnapshotV1ArtifactFilesystem().scan(root)
    assert.equal(replaced, true)
    assert.deepEqual(files, [{
      name: `${first}.player-snapshot-v1`, bytes: artifactA, receiptManifestBytes: receiptA,
    }])
    assert.deepEqual(await import('node:fs/promises').then(({ readFile }) => readFile(join(root, `${first}.manifest`))), receiptB)
    assert.deepEqual(await import('node:fs/promises').then(({ readFile }) => readFile(join(root, `${first}.player-snapshot-v1`))), artifactB)

    // This test-local model preserves the historical two-root arrangement:
    // independent root scans with a controlled replacement between them.
    await rename(root, replacement)
    await rename(displaced, root)
    const receipts = await scanImmutableOutboxFiles(root, isManifestFilename, MAX_MANIFEST_BYTES, process.platform)
    await rename(root, displaced)
    await rename(replacement, root)
    const artifacts = await scanImmutableOutboxFiles(
      root,
      (name) => Buffer.from(name).toString('utf8').endsWith(PLAYER_SNAPSHOT_V1_SUFFIX),
      MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS + 1,
      process.platform,
    )
    assert.deepEqual(receipts, [{ name: `${first}.manifest`, bytes: receiptA }])
    assert.deepEqual(artifacts, [{ name: `${first}.player-snapshot-v1`, bytes: artifactB }])
  } finally {
    handlePrototype.read = originalRead
    await probe.close().catch(() => undefined)
    await rm(root, { recursive: true, force: true })
    await rm(displaced, { recursive: true, force: true })
    await rm(replacement, { recursive: true, force: true })
  }
})

test('artifact filesystem rejects immutable evidence metadata changed while immutable evidence is read', { skip: process.platform !== 'linux', concurrency: false }, async () => {
  const root = await mkdtemp(join(tmpdir(), 'm4-player-snapshot-v1-metadata-race-'))
  const manifest = Buffer.from(body(first))
  const artifact = Buffer.from(playerSnapshotV1Artifact())
  const manifestPath = join(root, `${first}.manifest`)
  const probe = await openFile(manifestPath, 'w+')
  const handlePrototype = Object.getPrototypeOf(probe) as {
    stat: (...args: unknown[]) => Promise<{ mode: bigint }>
  }
  const originalStat = handlePrototype.stat
  const statCalls = new WeakMap<object, number>()
  try {
    await chmod(root, 0o700)
    await probe.close()
    await writeFile(manifestPath, manifest, { mode: 0o600 })
    await chmod(manifestPath, 0o600)
    await writeFile(join(root, `${first}.player-snapshot-v1`), artifact, { mode: 0o600 })
    handlePrototype.stat = async function (this: object, ...args: unknown[]) {
      const stat = await originalStat.apply(this, args)
      const calls = (statCalls.get(this) ?? 0) + 1
      statCalls.set(this, calls)
      // Simulate an otherwise-safe mode-bit change after the read. Keeping the
      // device/inode/timestamps fixed isolates the evidence-metadata invariant.
      return calls === 2 ? { ...stat, mode: stat.mode | 0o4000n } : stat
    }
    const files = await new NodePlayerSnapshotV1ArtifactFilesystem().scan(root)
    assert.deepEqual(files, [{ name: `${first}.player-snapshot-v1`, error: 'invalid' }])
  } finally {
    handlePrototype.stat = originalStat
    await probe.close().catch(() => undefined)
    await rm(root, { recursive: true, force: true })
  }
})

test('PlayerSnapshotV1 postgres adapter parameterizes the immutable artifact recorder', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const client: PgClient = {
    query: async <Row>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      return { rows: sql.startsWith('select outcome') ? [{ outcome: 'EXACT_RETRY' } as Row] : [] }
    },
    release: () => undefined,
  }
  const pool: PgPool = { connect: async () => client, end: async () => undefined }
  const parsed = parsePlayerSnapshotV1Artifact(`${first}.player-snapshot-v1`, playerSnapshotV1Artifact(), parseManifest(body(first)))
  const store = new PostgresPlayerSnapshotV1ArtifactStore('postgresql://mud_writer_login@localhost/postgres', pool)
  assert.equal(await store.recordPlayerSnapshotV1Artifact(parsed), 'EXACT_RETRY')
  assert.deepEqual(queries.map((query) => query.sql), [
    'set role mud_writer',
    'select outcome from private.record_player_snapshot_v1_artifact_for_receipt($1::uuid, $2::uuid, $3::text, $4::text, $5::bigint, $6::text, $7::text, $8::bigint, $9::bytea)',
  ])
  assert.deepEqual(queries[1]?.values, [
    parsed.characterId, parsed.commandId, parsed.receiptRequestSha256, parsed.sourcePostSha256,
    parsed.sourceOctets, parsed.snapshotFormat, parsed.snapshotSha256, parsed.snapshotOctets, parsed.payload,
  ])
})

test('PlayerSnapshotV1 postgres adapter fulfills eligibility with only character and artifact command identities', async () => {
  const outcomes: Array<string | undefined> = ['FULFILLED', 'EXACT_RETRY', 'ALREADY_FULFILLED', 'NOT_ELIGIBLE', 'UNKNOWN', undefined]
  for (const expected of outcomes) {
    const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
    const client: PgClient = {
      query: async <Row>(sql: string, values?: readonly unknown[]) => {
        queries.push({ sql, values })
        return { rows: sql.startsWith('select outcome') && expected ? [{ outcome: expected } as Row] : [] }
      },
      release: () => undefined,
    }
    const pool: PgPool = { connect: async () => client, end: async () => undefined }
    const store = new PostgresPlayerSnapshotV1ArtifactStore('postgresql://mud_writer_login@localhost/postgres', pool)
    const operation = store as PlayerSnapshotV1ArtifactFulfillmentStore
    if (expected && expected !== 'UNKNOWN') {
      assert.equal(await operation.fulfillGameCharacterOnboardingSnapshotEligibility(
        'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', first,
      ), expected as PlayerSnapshotV1ArtifactFulfillmentOutcome)
    } else {
      await assert.rejects(() => operation.fulfillGameCharacterOnboardingSnapshotEligibility(
        'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', first,
      ), /unexpected database fulfillment outcome/)
    }
    assert.deepEqual(queries.map((query) => query.sql), [
      'set role mud_writer',
      'select outcome from private.fulfill_game_character_onboarding_snapshot_eligibility($1::uuid, $2::uuid)',
    ])
    assert.deepEqual(queries[1]?.values, ['aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', first])
  }
})

test('fulfillment runs only after a recorded artifact and accepts every terminal outcome', async () => {
  const manifest = body(first)
  const artifact = playerSnapshotV1Artifact()
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest }],
  }
  const nonRecordCalls: string[] = []
  const nonRecordFulfillment: PlayerSnapshotV1ArtifactFulfillmentStore = {
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => { nonRecordCalls.push('fulfillment'); return 'FULFILLED' },
  }
  const nonRecordResult = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', {
    recordPlayerSnapshotV1Artifact: async () => { nonRecordCalls.push('artifact'); throw Object.assign(new Error('conflict'), { code: 'P0001' }) },
  }, filesystem, undefined, nonRecordFulfillment)
  assert.deepEqual(nonRecordCalls, ['artifact'])
  assert.equal(nonRecordResult.fulfillmentDelivered, 0)

  for (const outcome of ['FULFILLED', 'EXACT_RETRY', 'ALREADY_FULFILLED', 'NOT_ELIGIBLE'] as const) {
    const calls: string[] = []
    const fulfillment: PlayerSnapshotV1ArtifactFulfillmentStore = {
      fulfillGameCharacterOnboardingSnapshotEligibility: async (characterId, commandId) => {
        calls.push(`${characterId}:${commandId}`)
        return outcome
      },
    }
    const projection: PlayerSnapshotV1LevelProjectionStore = {
      recordPlayerSnapshotV1LevelProjection: async () => { calls.push('projection'); return 'RECORDED' },
    }
    const result = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', {
      recordPlayerSnapshotV1Artifact: async () => { calls.unshift('artifact'); return 'RECORDED' },
    }, filesystem, undefined, fulfillment, projection)
    assert.deepEqual(calls, ['artifact', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa:' + first, 'projection'])
    assert.equal(result.fulfillmentDelivered, 1)
    assert.equal(result.fulfillmentFulfilled, outcome === 'FULFILLED' ? 1 : 0)
    assert.equal(result.fulfillmentExactRetry, outcome === 'EXACT_RETRY' ? 1 : 0)
    assert.equal(result.fulfillmentAlreadyFulfilled, outcome === 'ALREADY_FULFILLED' ? 1 : 0)
    assert.equal(result.fulfillmentNotEligible, outcome === 'NOT_ELIGIBLE' ? 1 : 0)
  }
})

test('repeat artifact delivery remains idempotent while retrying fulfillment failures', async () => {
  const manifest = body(first)
  const artifact = playerSnapshotV1Artifact()
  const originalArtifact = Buffer.from(artifact)
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: manifest }],
  }
  let artifactAttempts = 0
  let fulfillmentAttempts = 0
  const calls: string[] = []
  const resultStore: PlayerSnapshotV1ArtifactStore = {
    recordPlayerSnapshotV1Artifact: async () => {
      artifactAttempts++
      calls.push(`artifact:${artifactAttempts}`)
      return artifactAttempts === 1 ? 'RECORDED' : 'EXACT_RETRY'
    },
  }
  const fulfillment: PlayerSnapshotV1ArtifactFulfillmentStore = {
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => {
      fulfillmentAttempts++
      calls.push(`fulfillment:${fulfillmentAttempts}`)
      if (fulfillmentAttempts === 1) throw Object.assign(new Error('connection reset'), { code: 'ECONNRESET' })
      return 'ALREADY_FULFILLED'
    },
  }
  const firstResult = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', resultStore, filesystem, undefined, fulfillment)
  const secondResult = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', resultStore, filesystem, undefined, fulfillment)
  assert.deepEqual(calls, ['artifact:1', 'fulfillment:1', 'artifact:2', 'fulfillment:2'])
  assert.equal(firstResult.delivered, 1)
  assert.equal(firstResult.fulfillmentRetryable, 1)
  assert.equal(secondResult.exactRetry, 1)
  assert.equal(secondResult.fulfillmentAlreadyFulfilled, 1)
  assert.deepEqual(artifact, originalArtifact)
})

test('artifact CLI keeps onboarding fulfillment absent until its explicit feature flag is enabled', async () => {
  const calls: unknown[][] = []
  const writes: string[] = []
  let created = 0
  let closed = 0
  const store: PlayerSnapshotV1ArtifactStore & PlayerSnapshotV1ArtifactFulfillmentStore = {
    recordPlayerSnapshotV1Artifact: async () => 'RECORDED',
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => 'FULFILLED',
    close: async () => { closed++ },
  }
  const dependencies = {
    createStore: () => { created++; return store },
    relay: async (...args: Parameters<typeof relayPlayerSnapshotV1ArtifactsOnce>) => {
      calls.push(args)
      return {
        visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
        replayObserved: 0, replayDisabled: 0,
        projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0,
        projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
      }
    },
    replayObserverFromEnvironment: () => ({ observe: async () => 'disabled' as const }),
    writeStdout: (value: string) => { writes.push(value) },
  }
  const environment = {
    M4_FILE_SNAPSHOT_OUTBOX_DIR: '/immutable/outbox',
    DATABASE_URL: 'postgresql://mud_writer_login@localhost/postgres',
  }

  assert.equal(await artifactRelayMain(environment, ['--once'], dependencies), 0)
  assert.equal(created, 1)
  assert.equal(closed, 1)
  assert.equal(calls.length, 1)
  assert.equal(calls[0]?.[4], undefined)
  assert.equal(writes.length, 1)

  assert.equal(await artifactRelayMain({ ...environment, M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED: 'false' }, ['--once'], dependencies), 0)
  assert.equal(created, 2)
  assert.equal(closed, 2)
  assert.equal(calls[1]?.[4], undefined)
  assert.equal(writes.length, 2)

  assert.equal(await artifactRelayMain({ ...environment, M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED: 'true' }, ['--once'], dependencies), 0)
  assert.equal(created, 3)
  assert.equal(closed, 3)
  assert.equal(calls[2]?.[4], store)
  assert.equal(writes.length, 3)
})

test('a dual-interface side-effect store preserves projection retry after fulfillment', async () => {
  const receipt = body(first)
  const artifact = playerSnapshotV1Artifact()
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: artifact, receiptManifestBytes: receipt }],
  }
  const calls: string[] = []
  let attempts = 0
  const dualStore: PlayerSnapshotV1ArtifactStore & PlayerSnapshotV1ArtifactFulfillmentStore & PlayerSnapshotV1LevelProjectionStore = {
    recordPlayerSnapshotV1Artifact: async () => {
      attempts++
      calls.push(`artifact:${attempts}`)
      return attempts === 1 ? 'RECORDED' : 'EXACT_RETRY'
    },
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => {
      calls.push(`fulfillment:${attempts}`)
      return attempts === 1 ? 'FULFILLED' : 'EXACT_RETRY'
    },
    recordPlayerSnapshotV1LevelProjection: async () => {
      calls.push(`projection:${attempts}`)
      return attempts === 1 ? 'RECORDED' : 'EXACT_RETRY'
    },
  }

  const firstResult = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', dualStore, filesystem, undefined, dualStore)
  const retryResult = await relayPlayerSnapshotV1ArtifactsOnce('/ignored', dualStore, filesystem, undefined, dualStore)
  assert.deepEqual(calls, ['artifact:1', 'fulfillment:1', 'projection:1', 'artifact:2', 'fulfillment:2', 'projection:2'])
  assert.equal(firstResult.fulfillmentFulfilled, 1)
  assert.equal(firstResult.projectionRecorded, 1)
  assert.equal(retryResult.fulfillmentExactRetry, 1)
  assert.equal(retryResult.projectionExactRetry, 1)
})

test('PlayerSnapshotV1 raw-U8 level projection adapter uses SET ROLE and one parameterized migration-190 call', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const client: PgClient = {
    query: async <Row>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      return { rows: sql.startsWith('select outcome') ? [{ outcome: 'RECORDED' } as Row] : [] }
    },
    release: () => undefined,
  }
  const pool: PgPool = { connect: async () => client, end: async () => undefined }
  const store = new PostgresPlayerSnapshotV1LevelProjectionStore('postgresql://mud_writer_login@localhost/postgres', pool)
  const input = {
    characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', commandId: first,
    receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64), sourceOctets: '128',
  }
  assert.equal(await store.recordPlayerSnapshotV1LevelProjection(input), 'RECORDED')
  assert.deepEqual(queries.map((query) => query.sql), [
    'set role mud_writer',
    'select outcome from private.record_player_snapshot_v1_level_projection_for_receipt($1::uuid, $2::uuid, $3::text, $4::text, $5::bigint)',
  ])
  assert.deepEqual(queries[1]?.values, [
    input.characterId, input.commandId, input.receiptRequestSha256, input.sourcePostSha256, input.sourceOctets,
  ])
})

test('paired shadow relay records the manifest before its PlayerSnapshotV1 artifact and exactly retries both', async () => {
  const manifest = body(first)
  const artifact = playerSnapshotV1Artifact()
  const sourceEvidence = { manifest: Buffer.from(manifest), artifact: Buffer.from(artifact) }
  const originalEvidence = { manifest: Buffer.from(manifest), artifact: Buffer.from(artifact) }
  const calls: string[] = []
  let attempt = 0
  const filesystem: PlayerSnapshotV1ManifestFirstFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: sourceEvidence.artifact, receiptManifestBytes: sourceEvidence.manifest }],
  }
  const store: ManifestStore & PlayerSnapshotV1ArtifactStore = {
    recordManifest: async (value) => { calls.push(`manifest:${value.commandId}`); return attempt === 0 ? 'RECORDED' : 'EXACT_RETRY' },
    recordPlayerSnapshotV1Artifact: async (value) => { calls.push(`artifact:${value.commandId}`); return attempt++ === 0 ? 'RECORDED' : 'EXACT_RETRY' },
  }

  const recorded = await relayPlayerSnapshotV1ManifestFirstOnce('/ignored', store, filesystem)
  const retried = await relayPlayerSnapshotV1ManifestFirstOnce('/ignored', store, filesystem)

  assert.deepEqual(calls, [`manifest:${first}`, `artifact:${first}`, `manifest:${first}`, `artifact:${first}`])
  assert.equal(recorded.manifestRecorded, 1)
  assert.equal(recorded.artifactRecorded, 1)
  assert.equal(retried.manifestExactRetry, 1)
  assert.equal(retried.artifactExactRetry, 1)
  assert.deepEqual(sourceEvidence, originalEvidence)
})

test('paired shadow relay does not invoke the artifact RPC when manifest delivery fails and retries unchanged evidence', async () => {
  const manifest = body(first)
  const artifact = playerSnapshotV1Artifact()
  const sourceEvidence = { manifest: Buffer.from(manifest), artifact: Buffer.from(artifact) }
  const originalEvidence = { manifest: Buffer.from(manifest), artifact: Buffer.from(artifact) }
  const calls: string[] = []
  let manifestAttempts = 0
  const filesystem: PlayerSnapshotV1ManifestFirstFilesystem = {
    scan: async () => [{ name: `${first}.player-snapshot-v1`, bytes: sourceEvidence.artifact, receiptManifestBytes: sourceEvidence.manifest }],
  }
  const store: ManifestStore & PlayerSnapshotV1ArtifactStore = {
    recordManifest: async () => {
      calls.push('manifest')
      if (manifestAttempts++ === 0) throw Object.assign(new Error('connection reset'), { code: 'ECONNRESET' })
      return 'RECORDED'
    },
    recordPlayerSnapshotV1Artifact: async () => { calls.push('artifact'); return 'RECORDED' },
  }

  const failed = await relayPlayerSnapshotV1ManifestFirstOnce('/ignored', store, filesystem)
  assert.equal(failed.retryable, 1)
  assert.equal(failed.artifactDelivered, 0)
  assert.deepEqual(calls, ['manifest'])
  assert.deepEqual(sourceEvidence, originalEvidence)

  const retried = await relayPlayerSnapshotV1ManifestFirstOnce('/ignored', store, filesystem)
  assert.equal(retried.manifestRecorded, 1)
  assert.equal(retried.artifactRecorded, 1)
  assert.deepEqual(calls, ['manifest', 'manifest', 'artifact'])
  assert.deepEqual(sourceEvidence, originalEvidence)
})

test('paired shadow relay preserves malformed and initially missing pair evidence without side effects', async () => {
  const manifest = body(first)
  const artifact = playerSnapshotV1Artifact()
  const sourceEvidence = { manifest: Buffer.from(manifest), artifact: Buffer.from(artifact), malformed: Buffer.from('not-a-manifest') }
  const originalEvidence = { manifest: Buffer.from(manifest), artifact: Buffer.from(artifact), malformed: Buffer.from('not-a-manifest') }
  const calls: string[] = []
  let scan = 0
  const filesystem: PlayerSnapshotV1ManifestFirstFilesystem = {
    scan: async () => {
      scan++
      if (scan === 1) return [
        { name: `${first}.player-snapshot-v1`, bytes: sourceEvidence.artifact, error: 'invalid' },
        { name: `${second}.player-snapshot-v1`, bytes: sourceEvidence.artifact, receiptManifestBytes: sourceEvidence.malformed },
      ]
      return [{ name: `${first}.player-snapshot-v1`, bytes: sourceEvidence.artifact, receiptManifestBytes: sourceEvidence.manifest }]
    },
  }
  const store: ManifestStore & PlayerSnapshotV1ArtifactStore = {
    recordManifest: async () => { calls.push('manifest'); return 'RECORDED' },
    recordPlayerSnapshotV1Artifact: async () => { calls.push('artifact'); return 'RECORDED' },
  }

  const incomplete = await relayPlayerSnapshotV1ManifestFirstOnce('/ignored', store, filesystem)
  assert.equal(incomplete.invalid, 2)
  assert.deepEqual(calls, [])
  assert.deepEqual(sourceEvidence, originalEvidence)

  const retried = await relayPlayerSnapshotV1ManifestFirstOnce('/ignored', store, filesystem)
  assert.equal(retried.delivered, 1)
  assert.deepEqual(calls, ['manifest', 'artifact'])
  assert.deepEqual(sourceEvidence, originalEvidence)
})

test('combined entrypoint supplies one manifest-first store in manifest/artifact order without side-effect capabilities', async () => {
  const calls: unknown[][] = []
  const storeCalls: string[] = []
  const writes: string[] = []
  let closed = 0
  const store: ManifestStore & PlayerSnapshotV1ArtifactStore = {
    recordManifest: async () => { storeCalls.push('manifest'); return 'RECORDED' },
    recordPlayerSnapshotV1Artifact: async () => { storeCalls.push('artifact'); return 'RECORDED' },
    close: async () => { closed++ },
  }
  const dependencies = {
    createStore: () => store,
    relay: async (...args: Parameters<typeof relayPlayerSnapshotV1ManifestFirstOnce>) => {
      calls.push(args)
      await args[1].recordManifest(parseManifest(body(first)))
      await args[1].recordPlayerSnapshotV1Artifact(parsePlayerSnapshotV1Artifact(
        `${first}.player-snapshot-v1`, playerSnapshotV1Artifact(), parseManifest(body(first)),
      ))
      return {
        visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
        manifestDelivered: 0, manifestRecorded: 0, manifestExactRetry: 0,
        artifactDelivered: 0, artifactRecorded: 0, artifactExactRetry: 0,
      }
    },
    writeStdout: (value: string) => { writes.push(value) },
  }
  const environment = {
    M4_FILE_SNAPSHOT_OUTBOX_DIR: '/immutable/outbox',
    DATABASE_URL: 'postgresql://mud_writer_login@localhost/postgres',
    M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED: 'true',
  }

  assert.equal(await manifestFirstRelayMain(environment, ['--once'], dependencies), 0)
  assert.equal(calls.length, 1)
  assert.equal(calls[0]?.length, 2)
  assert.equal(calls[0]?.[1], store)
  assert.deepEqual(storeCalls, ['manifest', 'artifact'])
  assert.equal(closed, 1)
  assert.equal(writes.length, 1)
})
