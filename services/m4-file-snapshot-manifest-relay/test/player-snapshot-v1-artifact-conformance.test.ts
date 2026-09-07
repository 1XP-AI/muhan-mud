import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { parseManifest } from '../src/manifest.js'
import { parsePlayerSnapshotV1Artifact, parsePlayerSnapshotV1ArtifactEvidence } from '../src/player-snapshot-v1-artifact.js'
import { relayPlayerSnapshotV1ArtifactsOnce } from '../src/player-snapshot-v1-artifact-relay.js'

const producer = process.env.PLAYER_SNAPSHOT_V1_ARTIFACT_C_PRODUCER
const rustVerifier = process.env.PLAYER_SNAPSHOT_V1_ARTIFACT_RUST_VERIFIER

function report(bytes: Uint8Array): Record<string, string> {
  const text = Buffer.from(bytes).toString('utf8')
  assert.ok(text.endsWith('\n'), 'the Rust report is line-oriented')
  const values: Record<string, string> = {}
  for (const line of text.trimEnd().split('\n')) {
    const separator = line.indexOf('=')
    assert.ok(separator > 0, `report line must be key=value: ${line}`)
    const key = line.slice(0, separator)
    assert.equal(values[key], undefined, `report key must be unique: ${key}`)
    values[key] = line.slice(separator + 1)
  }
  return values
}

function pairedReceipt(evidence: ReturnType<typeof parsePlayerSnapshotV1ArtifactEvidence>): Uint8Array {
  // The production relay intentionally requires the independently persisted
  // legacy receipt.  This test derives a matching in-memory receipt from the
  // C artifact only to reach that existing relay boundary; it never claims the
  // C artifact-store fixture produced a PlayerStore receipt.
  return Buffer.from([
    'version=1', `world_id=${evidence.worldId}`, `character_id=${evidence.characterId}`,
    `command_id=${evidence.commandId}`, `canonical_name_hex=${evidence.canonicalNameHex}`,
    `request_sha256=${evidence.requestSha256}`, `post_sha256=${evidence.sourcePostSha256}`,
    `writer_instance_id=${evidence.writerInstanceId}`, 'snapshot_format=legacy-file-manifest-v1',
    `writer_epoch=${evidence.writerEpoch}`, `writer_revision=${evidence.writerRevision}`,
    `storage_format=${evidence.storageFormat}`, `snapshot_octets=${evidence.sourceOctets}`, '',
  ].join('\n'), 'ascii')
}

function malformedHeader(artifact: Uint8Array): Uint8Array {
  return Buffer.concat([Buffer.from('ignored=x\n', 'ascii'), artifact])
}

function malformedPayload(artifact: Uint8Array): Uint8Array {
  const result = Buffer.from(artifact)
  const payloadOffset = result.indexOf('\n\n', 'ascii') + 2
  assert.ok(payloadOffset > 1, 'C artifact has a header delimiter')
  result[payloadOffset] ^= 1
  return result
}

test('C artifact-store fixture is receipt-bound through Node relay and malformed bytes stop before side effects', { skip: !producer || !rustVerifier }, async () => {
  // Provenance is intentionally narrow: this is C artifact-store fixture
  // production, not invocation of the live PlayerStore runtime.
  const artifact = execFileSync(producer!, [], { encoding: 'buffer' })
  assert.ok(artifact.length > 0, 'C artifact-store fixture emitted bytes')
  const original = Buffer.from(artifact)
  const node = parsePlayerSnapshotV1ArtifactEvidence(artifact)

  const verified = spawnSync(rustVerifier!, [], { input: artifact, encoding: 'buffer' })
  assert.equal(verified.status, 0, Buffer.from(verified.stderr).toString('utf8'))
  assert.deepEqual(report(verified.stdout), {
    format: 'player-snapshot-v1-artifact-shadow-verification', version: '1', algorithm: 'sha-256',
    world_id: node.worldId, character_id: node.characterId, command_id: node.commandId,
    canonical_name_hex: node.canonicalNameHex, request_sha256: node.requestSha256,
    source_post_sha256: node.sourcePostSha256, writer_instance_id: node.writerInstanceId,
    writer_epoch: node.writerEpoch, writer_revision: node.writerRevision,
    storage_format: node.storageFormat, snapshot_format: node.snapshotFormat,
    source_octets: node.sourceOctets, snapshot_sha256: node.snapshotSha256,
    snapshot_octets: String(node.snapshotOctets), canonical_octets: String(node.snapshotOctets),
    inventory_node_count: '0',
  }, 'Rust and Node must expose the same immutable C artifact facts')

  const receipt = pairedReceipt(node)
  const manifest = parseManifest(receipt)
  const filename = `${node.commandId}.player-snapshot-v1`
  const parsed = parsePlayerSnapshotV1Artifact(filename, artifact, manifest)
  assert.deepEqual({
    characterId: parsed.characterId,
    commandId: parsed.commandId,
    receiptRequestSha256: parsed.receiptRequestSha256,
    sourcePostSha256: parsed.sourcePostSha256,
    sourceOctets: parsed.sourceOctets,
    snapshotFormat: parsed.snapshotFormat,
    snapshotSha256: parsed.snapshotSha256,
    snapshotOctets: parsed.snapshotOctets,
  }, {
    characterId: node.characterId,
    commandId: node.commandId,
    receiptRequestSha256: node.requestSha256,
    sourcePostSha256: node.sourcePostSha256,
    sourceOctets: node.sourceOctets,
    snapshotFormat: node.snapshotFormat,
    snapshotSha256: node.snapshotSha256,
    snapshotOctets: node.snapshotOctets,
  }, 'the receipt-bound parser preserves the immutable C artifact identity')
  assert.deepEqual(parsed.payload, node.payload, 'the receipt-bound parser preserves the exact C payload bytes')

  const mismatchedReceipt = parseManifest(Buffer.from(
    Buffer.from(receipt).toString('ascii').replace(
      `writer_revision=${node.writerRevision}`,
      `writer_revision=${BigInt(node.writerRevision) + 1n}`,
    ),
    'ascii',
  ))
  assert.throws(
    () => parsePlayerSnapshotV1Artifact(filename, artifact, mismatchedReceipt),
    'a valid but mismatched receipt cannot be bound to C artifact evidence',
  )

  const calls: string[] = []
  const delivered = await relayPlayerSnapshotV1ArtifactsOnce('/hermetic/c-artifact', {
    recordPlayerSnapshotV1Artifact: async (value) => {
      calls.push('artifact')
      assert.deepEqual(value, parsed, 'relay passes the receipt-bound immutable artifact to its store')
      return 'RECORDED' as const
    },
  }, {
    scan: async () => [{ name: filename, bytes: artifact, receiptManifestBytes: receipt }],
  }, undefined, {
    fulfillGameCharacterOnboardingSnapshotEligibility: async (characterId, commandId) => {
      calls.push('fulfillment')
      assert.equal(characterId, parsed.characterId)
      assert.equal(commandId, parsed.commandId)
      return 'FULFILLED' as const
    },
  }, {
    recordPlayerSnapshotV1LevelProjection: async (value) => {
      calls.push('projection')
      assert.deepEqual(value, {
        characterId: parsed.characterId,
        commandId: parsed.commandId,
        receiptRequestSha256: parsed.receiptRequestSha256,
        sourcePostSha256: parsed.sourcePostSha256,
        sourceOctets: parsed.sourceOctets,
      })
      return 'RECORDED' as const
    },
  })
  assert.equal(delivered.valid, 1)
  assert.equal(delivered.delivered, 1)
  assert.equal(delivered.recorded, 1)
  assert.equal(delivered.fulfillmentDelivered, 1)
  assert.equal(delivered.fulfillmentFulfilled, 1)
  assert.equal(delivered.projectionDelivered, 1)
  assert.equal(delivered.projectionRecorded, 1)
  assert.deepEqual(calls, ['artifact', 'fulfillment', 'projection'], 'side effects run only after artifact recording settles')

  for (const malformed of [malformedHeader(artifact), malformedPayload(artifact)]) {
    assert.throws(() => parsePlayerSnapshotV1ArtifactEvidence(malformed), 'Node rejects malformed C artifact bytes')
    assert.throws(() => parsePlayerSnapshotV1Artifact(filename, malformed, manifest), 'receipt-bound Node parser rejects malformed C artifact bytes')
    const rejected = spawnSync(rustVerifier!, [], { input: malformed, encoding: 'buffer' })
    assert.equal(rejected.status, 1)
    assert.deepEqual(rejected.stdout, Buffer.alloc(0))
    assert.equal(Buffer.from(rejected.stderr).toString('utf8'), 'rejected: invalid player snapshot artifact\n')

    const calls: string[] = []
    const result = await relayPlayerSnapshotV1ArtifactsOnce('/hermetic/c-artifact', {
      recordPlayerSnapshotV1Artifact: async () => { calls.push('artifact'); return 'RECORDED' as const },
    }, {
      scan: async () => [{
        name: filename, bytes: malformed, receiptManifestBytes: receipt,
      }],
    }, undefined, {
      fulfillGameCharacterOnboardingSnapshotEligibility: async () => { calls.push('fulfillment'); return 'FULFILLED' as const },
    }, {
      recordPlayerSnapshotV1LevelProjection: async () => { calls.push('projection'); return 'RECORDED' as const },
    })
    assert.equal(result.invalid, 1)
    assert.equal(result.delivered, 0)
    assert.equal(result.projectionDelivered, 0)
    assert.equal(result.fulfillmentDelivered, 0)
    assert.deepEqual(calls, [], 'malformed bytes must fail before relay or projection mutation')
  }
  assert.deepEqual(artifact, original, 'the conformance harness never mutates C evidence')
})
