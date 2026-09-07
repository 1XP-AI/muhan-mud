import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, isAbsolute, join } from 'node:path'
import { test } from 'node:test'
import { relayPlayerSnapshotV1ArtifactsOnce, type PlayerSnapshotV1ArtifactFilesystem } from '../src/player-snapshot-v1-artifact-relay.js'
import { projectPlayerSnapshotV1Normalized, type PlayerSnapshotV1NormalizedProjection } from '../src/player-snapshot-v1-normalized-projection.js'
import { comparePlayerSnapshotV1NormalizedProjectionShadow } from '../src/player-snapshot-v1-normalized-projection-shadow-comparator.js'
import { parsePlayerSnapshotV1ArtifactEvidence } from '../src/player-snapshot-v1-artifact.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const characterId = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const requestSha256 = 'a'.repeat(64)
const sourcePostSha256 = 'b'.repeat(64)
const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '../../..')
const treeFixture = join(repoRoot, 'tests/fixtures/player_snapshot_v1_tree_inventory.hex')
const TREE_CANONICAL_DIGEST = '96df4bf87d1012fbef2043f215b95b6bf0790780b731546b1fcd6a257ee2b76c'

function receipt(): Uint8Array {
  return Buffer.from([
    'version=1', 'world_id=muhan-01', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4d3341', `request_sha256=${requestSha256}`, `post_sha256=${sourcePostSha256}`,
    'writer_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'snapshot_format=legacy-file-manifest-v1',
    'writer_epoch=7', 'writer_revision=1', 'storage_format=1', 'snapshot_octets=128', '',
  ].join('\n'), 'ascii')
}

function artifact(payload: Uint8Array, snapshotSha256: string): Uint8Array {
  return Buffer.concat([Buffer.from([
    'version=1', 'world_id=muhan-01', `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4d3341', `request_sha256=${requestSha256}`, `source_post_sha256=${sourcePostSha256}`,
    'writer_instance_id=bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'writer_epoch=7', 'writer_revision=1',
    'storage_format=1', 'snapshot_format=player-snapshot-v1', 'source_octets=128',
    `snapshot_sha256=${snapshotSha256}`, `snapshot_octets=${payload.length}`, '', '',
  ].join('\n'), 'ascii'), Buffer.from(payload)])
}

test('hermetic C -> Rust -> Node bridge preserves the checked-in tree projection and remains default-off', async () => {
  const runnerPath = process.env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER
  assert.ok(runnerPath && isAbsolute(runnerPath), 'bridge runner supplies the real Rust projection binary')

  const payload = Buffer.from((await readFile(treeFixture, 'utf8')).trim(), 'hex')
  const snapshotSha256 = createHash('sha256').update(payload).digest('hex')
  const parsed = await projectPlayerSnapshotV1Normalized(payload, { runnerPath, snapshotSha256 })
  assert.equal(parsed.canonicalDigest, TREE_CANONICAL_DIGEST)
  assert.deepEqual(parsed.player.items.map(({ parentIndex, childIndex }) => ({ parentIndex, childIndex })), [
    { parentIndex: null, childIndex: 0 }, { parentIndex: 0, childIndex: 0 }, { parentIndex: 1, childIndex: 0 },
    { parentIndex: 0, childIndex: 1 }, { parentIndex: null, childIndex: 1 },
  ])

  const wrappedArtifact = artifact(payload, snapshotSha256)
  const filesystem: PlayerSnapshotV1ArtifactFilesystem = {
    scan: async () => [{ name: `${commandId}.player-snapshot-v1`, bytes: wrappedArtifact, receiptManifestBytes: receipt() }],
  }
  let artifactCalls = 0
  const artifactStore = { recordPlayerSnapshotV1Artifact: async () => { artifactCalls++; return 'RECORDED' as const } }

  const off = await relayPlayerSnapshotV1ArtifactsOnce('/hermetic/unused', artifactStore, filesystem)
  assert.equal(artifactCalls, 1)
  assert.equal(off.normalizedProjectionDelivered, undefined, 'no normalized persistence seam is present by default')

  const persisted: Array<Record<string, unknown>> = []
  let projectCalls = 0
  const persistence = {
    project: async (receivedPayload: Uint8Array, receivedSha256: string): Promise<PlayerSnapshotV1NormalizedProjection> => {
      projectCalls++
      assert.deepEqual(receivedPayload, payload, 'relay preserves exact artifact payload bytes')
      assert.equal(receivedSha256, snapshotSha256, 'relay preserves exact artifact SHA-256')
      return parsed
    },
    store: { recordPlayerSnapshotNormalizedV1Projection: async (input: Record<string, unknown>) => { persisted.push(input); return 'RECORDED' as const } },
  }
  const enabled = await relayPlayerSnapshotV1ArtifactsOnce('/hermetic/unused', artifactStore, filesystem, undefined, undefined, undefined, persistence)
  assert.equal(projectCalls, 1)
  assert.equal(enabled.normalizedProjectionRecorded, 1)
  assert.equal(enabled.normalizedProjectionDelivered, 1)
  assert.deepEqual(persisted, [{
    characterId, commandId, receiptRequestSha256: requestSha256, sourcePostSha256, sourceOctets: '128', projection: parsed,
  }], 'the actual relay persists the exact Node-parsed Rust projection and only receipt-bound metadata')
})

test('hermetic post-save shadow proof binds the C artifact projection to one injected normalized record', async () => {
  const runnerPath = process.env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER
  assert.ok(runnerPath && isAbsolute(runnerPath), 'bridge runner supplies the real Rust projection binary')
  const payload = Buffer.from((await readFile(treeFixture, 'utf8')).trim(), 'hex')
  const snapshotSha256 = createHash('sha256').update(payload).digest('hex')
  const derived = await projectPlayerSnapshotV1Normalized(payload, { runnerPath, snapshotSha256 })
  const evidence = parsePlayerSnapshotV1ArtifactEvidence(artifact(payload, snapshotSha256))
  const record = {
    worldId: evidence.worldId, characterId, commandId, receiptRequestSha256: requestSha256,
    writerInstanceId: evidence.writerInstanceId, writerEpoch: evidence.writerEpoch, writerRevision: evidence.writerRevision,
    sourcePostSha256, sourceOctets: evidence.sourceOctets, snapshotSha256, snapshotOctets: payload.length, projection: derived,
  }
  assert.equal(await comparePlayerSnapshotV1NormalizedProjectionShadow(evidence, {
    findByCommandId: async () => [record],
  }, {
    project: (receivedPayload, receivedSha256) => projectPlayerSnapshotV1Normalized(receivedPayload, { runnerPath, snapshotSha256: receivedSha256 }),
  }), 'MATCH')
})
