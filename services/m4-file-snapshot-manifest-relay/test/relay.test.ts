import assert from 'node:assert/strict'
import { chmod, link, mkdtemp, open as openFile, rename, rm, writeFile } from 'node:fs/promises'
import { test } from 'node:test'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createHash } from 'node:crypto'
import { parsePlayerSnapshotV1Artifact } from '../src/player-snapshot-v1-artifact.js'
import { relayPlayerSnapshotV1ArtifactsOnce, type PlayerSnapshotV1ArtifactFilesystem } from '../src/player-snapshot-v1-artifact-relay.js'
import { parseManifest } from '../src/manifest.js'
import { NodeManifestFilesystem, relayOnce, type ManifestFilesystem } from '../src/relay.js'
import { NodePlayerSnapshotV1ArtifactFilesystem } from '../src/player-snapshot-v1-artifact-relay.js'
import { PlayerSnapshotV1ReplayObserver, type PlayerSnapshotV1ReplayObserver } from '../src/player-snapshot-v1-replay-observer.js'
import { NodePlayerSnapshotV1ReplayShadowJournal } from '../src/player-snapshot-v1-replay-shadow-journal.js'
import { PostgresManifestStore, PostgresPlayerSnapshotV1ArtifactStore, PostgresPlayerSnapshotV1LevelProjectionStore, assertDatabaseUrl, type ManifestStore, type PlayerSnapshotV1ArtifactStore, type PlayerSnapshotV1LevelProjectionStore, type PgClient, type PgPool } from '../src/store.js'

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
    replayObserved: 0, replayDisabled: 1,
    projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  })
  assert.equal(records, 1)
  assert.deepEqual(observed, [payload])
  assert.deepEqual(contexts, [{
    commandId: first, characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
    receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64),
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
      replayObserved: 0, replayDisabled: 1,
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
