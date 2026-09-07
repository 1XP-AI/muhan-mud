import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import {
  canonicalPlayerSnapshotV1NormalizedProjectionDigest,
  comparePlayerSnapshotV1NormalizedProjectionShadow,
  type ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader,
} from '../src/player-snapshot-v1-normalized-projection-shadow-comparator.js'
import type { PlayerSnapshotV1ReceiptBoundArtifactEvidence } from '../src/player-snapshot-v1-artifact.js'
import type { PlayerSnapshotV1NormalizedProjection } from '../src/player-snapshot-v1-normalized-projection.js'

const commandId = '11111111-1111-4111-8111-111111111111'
const characterId = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const player: PlayerSnapshotV1NormalizedProjection['player'] = {
    level: 42, hpMax: 100, hpCurrent: 99, mpMax: 80, mpCurrent: 79, experience: 123n, gold: 456n,
    daily: Array.from({ length: 10 }, (_, index) => ({ max: 2, current: 1, lastUsed: BigInt(index + 3) })),
    timers: Array.from({ length: 45 }, (_, index) => ({ interval: BigInt(index + 4), lastUsed: BigInt(index + 5), misc: 6 })),
    items: [
      { parentIndex: null, childIndex: 0, value: 7n, weight: 8, typeCode: 9, adjustment: 10, shotsMax: 11, shotsCurrent: 10, ndice: 12, sdice: 13, pdice: 14, armor: 15, wearFlag: 16, magicPower: 17, magicRealm: 18, special: 19 },
      { parentIndex: 0, childIndex: 0, value: 20n, weight: 21, typeCode: 22, adjustment: 23, shotsMax: 24, shotsCurrent: 23, ndice: 25, sdice: 26, pdice: 27, armor: 28, wearFlag: 29, magicPower: 30, magicRealm: 31, special: 32 },
    ],
}
const projection: PlayerSnapshotV1NormalizedProjection = {
  format: 'player-snapshot-v1-normalized-projection', version: 1, algorithm: 'sha-256',
  canonicalDigest: canonicalPlayerSnapshotV1NormalizedProjectionDigest(player), player,
}

function artifact(): PlayerSnapshotV1ReceiptBoundArtifactEvidence {
  const payload = Buffer.alloc(48, 1)
  return {
    worldId: 'muhan-01', characterId, commandId, canonicalNameHex: '4d3341', receiptRequestSha256: 'a'.repeat(64),
    sourcePostSha256: 'b'.repeat(64), writerInstanceId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
    writerEpoch: '7', writerRevision: '9', storageFormat: '1', snapshotFormat: 'player-snapshot-v1',
    sourceOctets: '123', snapshotSha256: createHash('sha256').update(payload).digest('hex'), snapshotOctets: payload.length, payload,
  }
}

function record(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  const source = artifact()
  return {
    worldId: source.worldId, characterId: source.characterId, commandId: source.commandId,
    receiptRequestSha256: source.receiptRequestSha256, writerInstanceId: source.writerInstanceId,
    writerEpoch: source.writerEpoch, writerRevision: source.writerRevision,
    sourcePostSha256: source.sourcePostSha256, sourceOctets: source.sourceOctets,
    snapshotSha256: source.snapshotSha256, snapshotOctets: source.snapshotOctets,
    projection, ...overrides,
  }
}

function projected(player: PlayerSnapshotV1NormalizedProjection['player']): PlayerSnapshotV1NormalizedProjection {
  return { ...projection, canonicalDigest: canonicalPlayerSnapshotV1NormalizedProjectionDigest(player), player }
}

function reader(rows: readonly unknown[]): ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader {
  return { findByCommandId: async () => rows }
}

test('derives every normalized field from immutable C artifact evidence and matches one closed injected record', async () => {
  const seen: Array<[Uint8Array, string]> = []
  const result = await comparePlayerSnapshotV1NormalizedProjectionShadow(artifact(), reader([record()]), {
    project: async (payload, snapshotSha256) => { seen.push([payload, snapshotSha256]); return projection },
  })
  assert.equal(result, 'MATCH')
  assert.deepEqual(seen, [[artifact().payload, artifact().snapshotSha256]])
})

test('fails closed on a missing, duplicate, malformed, incomplete, mismatched, or out-of-order projection record', async () => {
  const cases: Array<[string, readonly unknown[], string]> = [
    ['missing', [], 'MISSING_RECORD'],
    ['duplicate', [record(), record()], 'UNEXPECTED_DUPLICATE'],
    ['extra field', [{ ...record(), extra: true }], 'INVALID_RECORD'],
    ['incomplete field', [(() => { const value = record(); delete value.writerRevision; return value })()], 'INVALID_RECORD'],
    ['bound revision', [record({ writerRevision: '10' })], 'EVIDENCE_MISMATCH'],
    ['bound world', [record({ worldId: 'other' })], 'EVIDENCE_MISMATCH'],
    ['bound snapshot hash', [record({ snapshotSha256: 'e'.repeat(64) })], 'EVIDENCE_MISMATCH'],
    ['projection digest', [record({ projection: { ...projection, canonicalDigest: 'c'.repeat(64) } })], 'INVALID_RECORD'],
    ['projection scalar', [record({ projection: projected({ ...projection.player, gold: 457n }) })], 'PROJECTION_MISMATCH'],
    ['projection item order', [record({ projection: projected({ ...projection.player, items: [...projection.player.items].reverse() }) })], 'INVALID_RECORD'],
    ['projection daily order', [record({ projection: projected({ ...projection.player, daily: [...projection.player.daily].reverse() }) })], 'PROJECTION_MISMATCH'],
  ]
  for (const [name, rows, expected] of cases) {
    const result = await comparePlayerSnapshotV1NormalizedProjectionShadow(artifact(), reader(rows), { project: async () => projection })
    assert.equal(result, expected, name)
  }
})

test('fails closed when the immutable evidence or derived projection is invalid, and never opens a reader first', async () => {
  let reads = 0
  const invalidArtifact = { ...artifact(), writerRevision: '09' }
  assert.equal(await comparePlayerSnapshotV1NormalizedProjectionShadow(invalidArtifact, {
    findByCommandId: async () => { reads++; return [record()] },
  }, { project: async () => projection }), 'INVALID_ARTIFACT')
  assert.equal(reads, 0)

  assert.equal(await comparePlayerSnapshotV1NormalizedProjectionShadow(artifact(), reader([record()]), {
    project: async () => ({ ...projection, player: { ...projection.player, daily: projection.player.daily.slice(1) } }),
  }), 'PROJECTION_DERIVATION_FAILED')
  assert.equal(await comparePlayerSnapshotV1NormalizedProjectionShadow(artifact(), {
    findByCommandId: async () => { throw new Error('reader failure') },
  }, { project: async () => projection }), 'RECORD_READ_ERROR')
})

function item(parentIndex: number | null, childIndex: number): PlayerSnapshotV1NormalizedProjection['player']['items'][number] {
  return {
    parentIndex, childIndex, value: 7n, weight: 8, typeCode: 9, adjustment: 10,
    shotsMax: 11, shotsCurrent: 10, ndice: 12, sdice: 13, pdice: 14, armor: 15,
    wearFlag: 16, magicPower: 17, magicRealm: 18, special: 19,
  }
}

function topologyProjection(items: PlayerSnapshotV1NormalizedProjection['player']['items']): PlayerSnapshotV1NormalizedProjection {
  const value = { ...player, items }
  return projected(value)
}

test('rejects Rust-invalid depth-65 and 4,097-item root or child lists before a comparison can match', async () => {
  const cases: Array<[string, PlayerSnapshotV1NormalizedProjection]> = [
    ['depth 65', topologyProjection(Array.from({ length: 65 }, (_, index) => item(index === 0 ? null : index - 1, 0)))],
    ['4,097 roots', topologyProjection(Array.from({ length: 4097 }, (_, index) => item(null, index)))],
    ['4,097 children', topologyProjection([item(null, 0), ...Array.from({ length: 4097 }, (_, index) => item(0, index))])],
  ]
  for (const [name, malformed] of cases) {
    let reads = 0
    const result = await comparePlayerSnapshotV1NormalizedProjectionShadow(artifact(), {
      findByCommandId: async () => { reads++; return [record({ projection: malformed })] },
    }, { project: async () => malformed })
    assert.equal(result, 'PROJECTION_DERIVATION_FAILED', name)
    assert.equal(reads, 0, `${name} must fail before comparison can reach MATCH`)
  }
})

test('preserves valid Rust topology boundaries: depth 64 and 4,096 roots or children', async () => {
  const cases: Array<[string, PlayerSnapshotV1NormalizedProjection]> = [
    ['depth 64', topologyProjection(Array.from({ length: 64 }, (_, index) => item(index === 0 ? null : index - 1, 0)))],
    ['4,096 roots', topologyProjection(Array.from({ length: 4096 }, (_, index) => item(null, index)))],
    ['4,096 children', topologyProjection([item(null, 0), ...Array.from({ length: 4096 }, (_, index) => item(0, index))])],
  ]
  for (const [name, bounded] of cases) {
    const result = await comparePlayerSnapshotV1NormalizedProjectionShadow(artifact(), reader([record({ projection: bounded })]), {
      project: async () => bounded,
    })
    assert.equal(result, 'MATCH', name)
  }
})
