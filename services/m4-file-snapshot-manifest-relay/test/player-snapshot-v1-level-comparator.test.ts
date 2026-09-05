import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  comparePlayerSnapshotV2JournalLevel,
  type ImmutablePlayerSnapshotLevelProjectionEvidence,
  type ImmutablePlayerSnapshotLevelProjectionReader,
} from '../src/player-snapshot-v1-level-comparator.js'
import type { PlayerSnapshotV1ReplayLevelDifferentialInput } from '../src/player-snapshot-v1-replay-differential.js'

const input: PlayerSnapshotV1ReplayLevelDifferentialInput = {
  commandId: '11111111-1111-4111-8111-111111111111',
  characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  receiptRequestSha256: 'a'.repeat(64),
  sourcePostSha256: 'b'.repeat(64),
  snapshotSha256: 'c'.repeat(64),
  snapshotOctets: 42,
  rawLevelU8: 42,
}

function projection(
  value: Partial<ImmutablePlayerSnapshotLevelProjectionEvidence> = {},
): ImmutablePlayerSnapshotLevelProjectionEvidence {
  return { ...input, ...value }
}

function reader(
  result: readonly ImmutablePlayerSnapshotLevelProjectionEvidence[] | Error,
): ImmutablePlayerSnapshotLevelProjectionReader {
  return {
    findByCommandId: async () => {
      if (result instanceof Error) throw result
      return result
    },
  }
}

test('compares raw-U8 levels exactly at 0, 42, and 255 without mapping or clamping', async () => {
  for (const rawLevelU8 of [0, 42, 255]) {
    const journal = { ...input, rawLevelU8 }
    assert.equal(await comparePlayerSnapshotV2JournalLevel(journal, reader([projection({ rawLevelU8 })])), 'MATCH')
  }
  assert.equal(await comparePlayerSnapshotV2JournalLevel(
    { ...input, rawLevelU8: 0 }, reader([projection({ rawLevelU8: 1 })]),
  ), 'MISMATCH_LEVEL')
  assert.equal(await comparePlayerSnapshotV2JournalLevel(
    { ...input, rawLevelU8: 255 }, reader([projection({ rawLevelU8: 254 })]),
  ), 'MISMATCH_LEVEL')
})

test('returns identity mismatch before a differing raw level and compares all receipt-bound bindings', async () => {
  for (const [field, value] of [
    ['characterId', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'],
    ['commandId', '22222222-2222-4222-8222-222222222222'],
    ['receiptRequestSha256', 'd'.repeat(64)],
    ['sourcePostSha256', 'e'.repeat(64)],
    ['snapshotSha256', 'f'.repeat(64)],
    ['snapshotOctets', 43],
  ] as const) {
    assert.equal(await comparePlayerSnapshotV2JournalLevel(
      input, reader([projection({ [field]: value, rawLevelU8: 0 })]),
    ), 'IDENTITY_MISMATCH', field)
  }
})

test('separates missing evidence, duplicate evidence, and reader failure from decision outcomes', async () => {
  assert.equal(await comparePlayerSnapshotV2JournalLevel(input, reader([])), 'MISSING_PROJECTION')
  assert.equal(await comparePlayerSnapshotV2JournalLevel(input, reader([projection(), projection()])), 'UNEXPECTED_DUPLICATE')
  assert.equal(await comparePlayerSnapshotV2JournalLevel(
    input, reader(new Error('projection reader failure must not escape')),
  ), 'PROJECTION_READ_ERROR')
  assert.equal(await comparePlayerSnapshotV2JournalLevel(input, {
    findByCommandId: async () => undefined as never,
  }), 'PROJECTION_READ_ERROR')
})

test('rejects malformed journal metadata or projection evidence before any level comparison', async () => {
  let reads = 0
  const countingReader: ImmutablePlayerSnapshotLevelProjectionReader = {
    findByCommandId: async () => { reads++; return [projection()] },
  }
  assert.equal(await comparePlayerSnapshotV2JournalLevel({ ...input, rawLevelU8: 256 }, countingReader), 'INVALID_INPUT')
  assert.equal(await comparePlayerSnapshotV2JournalLevel({ ...input, payload: 'forbidden' }, countingReader), 'INVALID_INPUT')
  assert.equal(reads, 0)

  assert.equal(await comparePlayerSnapshotV2JournalLevel(input, {
    findByCommandId: async () => [{ ...projection(), snapshotOctets: -1 }],
  }), 'INVALID_INPUT')
  assert.equal(await comparePlayerSnapshotV2JournalLevel(input, {
    findByCommandId: async () => [{ ...projection(), extra: true }],
  }), 'INVALID_INPUT')
})
