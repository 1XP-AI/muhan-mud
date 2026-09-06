import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { test } from 'node:test'
import {
  loadPairedImmutablePlayerSnapshotV1Artifact,
  main as rehearsalMain,
  type PlayerSnapshotV1FullPayloadRehearsalCliDependencies,
} from '../src/player-snapshot-v1-full-payload-rehearsal-cli.js'
import type { ImmutablePlayerSnapshotV1FullPayloadEvidence } from '../src/player-snapshot-v1-full-payload-rehearsal.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const characterId = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const readerDatabaseUrl = 'postgresql://mud_full_payload_rehearsal_reader_login@localhost/postgres?options=-c%20default_transaction_read_only%3Don'
const verifierPath = '/opt/muhan/player_snapshot_v1_replay_verify'
const payload = Buffer.alloc(48)
payload.write('MUHCDTO\0', 0, 'ascii')
payload[9] = 1
payload[11] = 7
createHash('sha256').update(Buffer.alloc(0)).digest().copy(payload, 16)
const payloadDigest = createHash('sha256').update(payload).digest('hex')

const local = {
  characterId, commandId, receiptRequestSha256: 'a'.repeat(64), sourcePostSha256: 'b'.repeat(64), sourceOctets: '99',
  snapshotFormat: 'player-snapshot-v1' as const, snapshotSha256: payloadDigest, snapshotOctets: payload.length, payload,
}
const receipt = {
  characterId, commandId, worldId: 'm4-rehearsal', legacyNameKey: '4d34616c706861', requestSha256: 'a'.repeat(64),
  postSha256: 'b'.repeat(64), writerInstanceId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
  writerEpoch: '7', writerRevision: '9', storageFormat: '1',
}
const evidence: ImmutablePlayerSnapshotV1FullPayloadEvidence = {
  ...local, worldId: 'm4-rehearsal', legacyNameKey: 'M4alpha', writerInstanceId: receipt.writerInstanceId,
  writerEpoch: '7', writerRevision: '9', storageFormat: '1', receiptAcknowledgedAt: '2026-10-04 00:00:00+00',
  receipt: { ...receipt, legacyNameKey: 'M4alpha', acknowledgedAt: '2026-10-04 00:00:00+00' },
}

function manifest(): Uint8Array {
  return Buffer.from([
    'version=1', 'world_id=m4-rehearsal', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4d34616c706861', `request_sha256=${receipt.requestSha256}`, `post_sha256=${receipt.postSha256}`,
    `writer_instance_id=${receipt.writerInstanceId}`, 'snapshot_format=legacy-file-manifest-v1', 'writer_epoch=7',
    'writer_revision=9', 'storage_format=1', 'snapshot_octets=99', '',
  ].join('\n'), 'ascii')
}

function artifact(): Uint8Array {
  return Buffer.concat([Buffer.from([
    'version=1', 'world_id=m4-rehearsal', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4d34616c706861', `request_sha256=${receipt.requestSha256}`, `source_post_sha256=${receipt.postSha256}`,
    `writer_instance_id=${receipt.writerInstanceId}`, 'writer_epoch=7', 'writer_revision=9', 'storage_format=1',
    'snapshot_format=player-snapshot-v1', 'source_octets=99', `snapshot_sha256=${payloadDigest}`, 'snapshot_octets=48', '', '',
  ].join('\n'), 'ascii'), payload])
}

function verification(value: Uint8Array, inventoryNodeCount = 3) {
  const digest = createHash('sha256').update(value).digest('hex')
  return {
    format: 'player-snapshot-v1-replay-verification' as const, version: '1' as const, algorithm: 'sha-256' as const,
    inputDigest: digest, canonicalDigest: digest, canonicalOctets: value.length, inventoryNodeCount,
  }
}

function baseEnv(): NodeJS.ProcessEnv {
  return {
    M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_OUTBOX_PATH: '/secure/outbox',
    M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_DATABASE_URL: readerDatabaseUrl,
    M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_VERIFIER_PATH: verifierPath,
  }
}

function dependencies(rows: readonly unknown[] | Error = [evidence], calls: string[] = [], output: string[] = []): PlayerSnapshotV1FullPayloadRehearsalCliDependencies {
  return {
    loadArtifact: async (path) => { calls.push(`load:${path}`); return local },
    createReader: (url) => ({
      findByCommandId: async (id) => { calls.push(`read:${id}`); if (rows instanceof Error) throw rows; return rows },
      close: async () => { calls.push('close') },
    }),
    verify: async (value, options) => { calls.push(`verify:${options.runnerPath}`); return verification(value) },
    writeStdout: (line) => { output.push(line) },
  }
}

function diagnostic(output: readonly string[]) {
  assert.equal(output.length, 1)
  assert.equal(output[0]!.split('\n').filter(Boolean).length, 1)
  return JSON.parse(output[0]!) as Record<string, unknown>
}

test('immutable paired loader selects exactly one canonical artifact and paired manifest', async () => {
  const filesystem = {
    scan: async () => [{ name: `${commandId}.player-snapshot-v1`, bytes: artifact(), receiptManifestBytes: manifest() }],
  }
  assert.deepEqual(await loadPairedImmutablePlayerSnapshotV1Artifact('/secure/outbox', filesystem), local)
  for (const files of [
    [],
    [{ name: `${commandId}.player-snapshot-v1`, bytes: artifact() }],
    [{ name: `${commandId}.player-snapshot-v1`, bytes: artifact(), receiptManifestBytes: manifest() }, { name: '22222222-2222-4222-8222-222222222222.player-snapshot-v1', bytes: artifact(), receiptManifestBytes: manifest() }],
  ]) await assert.rejects(() => loadPairedImmutablePlayerSnapshotV1Artifact('/secure/outbox', { scan: async () => files }))
})

test('one-shot full-payload CLI validates --once and environment as INVALID_INPUT before loading or database access', async () => {
  for (const [args, env] of [
    [[], baseEnv()],
    [['--once', '--again'], baseEnv()],
    [['--once'], { ...baseEnv(), M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_OUTBOX_PATH: 'relative' }],
    [['--once'], { ...baseEnv(), M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_DATABASE_URL: undefined }],
    [['--once'], { ...baseEnv(), M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_VERIFIER_PATH: 'relative' }],
  ] as const) {
    const calls: string[] = []; const output: string[] = []
    assert.equal(await rehearsalMain(env, args, dependencies([evidence], calls, output)), 1)
    assert.deepEqual(calls, [])
    assert.deepEqual(diagnostic(output), {
      format: 'player-snapshot-v1-full-payload-rehearsal', version: '1', classification: 'INVALID_INPUT', commandId: null, characterId: null,
    })
  }
})

test('one-shot full-payload CLI emits MATCH exactly once after local load, reader, and verifier ordering', async () => {
  const calls: string[] = []; const output: string[] = []
  assert.equal(await rehearsalMain(baseEnv(), ['--once'], dependencies([evidence], calls, output)), 0)
  assert.deepEqual(calls, [`load:/secure/outbox`, `read:${commandId}`, `verify:${verifierPath}`, `verify:${verifierPath}`, 'close'])
  assert.deepEqual(diagnostic(output), {
    format: 'player-snapshot-v1-full-payload-rehearsal', version: '1', classification: 'MATCH', commandId, characterId,
  })
})

test('one-shot full-payload CLI closes successful reader results and exits one for every closed non-match class', async () => {
  const cases: Array<[string, readonly unknown[] | Error, (deps: PlayerSnapshotV1FullPayloadRehearsalCliDependencies) => void]> = [
    ['MISSING_ARTIFACT', [], () => undefined],
    ['UNEXPECTED_DUPLICATE', [evidence, evidence], () => undefined],
    ['DB_READ_ERROR', new Error('raw database error'), () => undefined],
    ['RECEIPT_MISMATCH', [{ ...evidence, receipt: { ...evidence.receipt, worldId: 'other' } }], () => undefined],
    ['ARTIFACT_MISMATCH', [{ ...evidence, sourceOctets: '100' }], () => undefined],
    ['DECODE_MISMATCH', [evidence], (deps) => { deps.verify = async () => { throw new Error('raw verifier error') } }],
    ['SEMANTIC_MISMATCH', [evidence], (deps) => { let count = 0; deps.verify = async (value) => verification(value, ++count === 1 ? 3 : 4) }],
    ['INVALID_INPUT', [{ ...evidence, snapshotSha256: 'c'.repeat(64) }], () => undefined],
  ]
  for (const [classification, rows, configure] of cases) {
    const calls: string[] = []; const output: string[] = []; const deps = dependencies(rows, calls, output)
    configure(deps)
    assert.equal(await rehearsalMain(baseEnv(), ['--once'], deps), 1, classification)
    assert.equal(calls.at(-1), 'close', classification)
    const result = diagnostic(output)
    assert.equal(result.classification, classification)
    assert.equal(result.commandId, commandId)
    assert.equal(result.characterId, characterId)
    assert.equal(JSON.stringify(result).includes('raw database error'), false)
    assert.equal(JSON.stringify(result).includes('raw verifier error'), false)
  }
})

test('one-shot full-payload CLI rejects incomplete verifier records without emitting MATCH', async () => {
  for (const malformed of [
    undefined,
    { format: 'player-snapshot-v1-replay-verification', version: '1' },
    { ...verification(payload), canonicalOctets: payload.length - 1 },
  ]) {
    const calls: string[] = []; const output: string[] = []; const deps = dependencies([evidence], calls, output)
    deps.verify = async (value, options) => {
      calls.push(`verify:${options.runnerPath}`)
      return malformed as never
    }
    assert.equal(await rehearsalMain(baseEnv(), ['--once'], deps), 1)
    assert.equal(calls.filter((call) => call.startsWith('verify:')).length, 2)
    const result = diagnostic(output)
    assert.equal(result.classification, 'DECODE_MISMATCH')
    assert.notEqual(result.classification, 'MATCH')
  }
})

test('one-shot full-payload CLI reports reader construction failure as one closed DB_READ_ERROR record', async () => {
  const output: string[] = []
  const deps = dependencies([evidence], [], output)
  deps.createReader = () => { throw new Error('raw connection details must not escape') }
  assert.equal(await rehearsalMain(baseEnv(), ['--once'], deps), 1)
  assert.deepEqual(diagnostic(output), {
    format: 'player-snapshot-v1-full-payload-rehearsal', version: '1', classification: 'DB_READ_ERROR', commandId, characterId,
  })
  assert.equal(JSON.stringify(output).includes('raw connection details must not escape'), false)
})

test('local loader failures close as INVALID_INPUT before reader creation, while close failure leaves stdout empty', async () => {
  const output: string[] = []; let readerCreated = false
  const invalidLoader = dependencies([evidence], [], output)
  invalidLoader.loadArtifact = async () => { throw new Error('path /secret must not escape') }
  invalidLoader.createReader = () => { readerCreated = true; throw new Error('must not create') }
  assert.equal(await rehearsalMain(baseEnv(), ['--once'], invalidLoader), 1)
  assert.equal(readerCreated, false)
  assert.equal(diagnostic(output).classification, 'INVALID_INPUT')

  const closeOutput: string[] = []
  const closeFailure = dependencies([evidence], [], closeOutput)
  closeFailure.createReader = () => ({ findByCommandId: async () => [evidence], close: async () => { throw new Error('unrecoverable close') } })
  await assert.rejects(() => rehearsalMain(baseEnv(), ['--once'], closeFailure))
  assert.deepEqual(closeOutput, [])
})

test('default relay and image remain detached from the full-payload rehearsal authority', async () => {
  const [relayCli, image, rehearsalCli] = await Promise.all([
    readFile(new URL('../src/cli.ts', import.meta.url), 'utf8'),
    readFile(new URL('../Dockerfile', import.meta.url), 'utf8'),
    readFile(new URL('../src/player-snapshot-v1-full-payload-rehearsal-cli.ts', import.meta.url), 'utf8'),
  ])
  assert.doesNotMatch(relayCli, /full-payload-rehearsal/i)
  assert.doesNotMatch(image, /full-payload-rehearsal/i)
  assert.doesNotMatch(rehearsalCli, /env\.DATABASE_URL|mud_writer|PostgresPlayerSnapshotV1ArtifactStore/)
})
