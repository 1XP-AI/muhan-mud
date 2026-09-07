import assert from 'node:assert/strict'
import { test } from 'node:test'
import { PostgresNormalizedProjectionReader } from '../src/player-snapshot-v1-normalized-projection-reader.js'
import { canonicalPlayerSnapshotV1NormalizedProjectionDigest } from '../src/player-snapshot-v1-normalized-projection-shadow-comparator.js'

const identity = { worldId: 'muhan-01', characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', commandId: '11111111-1111-4111-8111-111111111111' }
const player = {
  level: 42, hpMax: 100, hpCurrent: 99, mpMax: 50, mpCurrent: 49,
  experience: 9223372036854775807n, gold: -9223372036854775808n,
  daily: Array.from({ length: 10 }, () => ({ max: 1, current: 0, lastUsed: 0n })),
  timers: Array.from({ length: 45 }, () => ({ interval: 0n, lastUsed: 0n, misc: 0 })), items: [],
}
function wire(): string {
  return JSON.stringify({
    format: 'player-snapshot-v1-normalized-projection', version: 1, algorithm: 'sha-256',
    canonical_digest: canonicalPlayerSnapshotV1NormalizedProjectionDigest(player),
    player: { level: 42, hp_max: 100, hp_current: 99, mp_max: 50, mp_current: 49,
      experience: '9223372036854775807', gold: '-9223372036854775808',
      daily: player.daily.map(() => ({ max: 1, current: 0, last_used: 0 })),
      timers: player.timers.map(() => ({ interval: 0, last_used: 0, misc: 0 })), items: [] },
  }).replace(/"(-?\d{19})"/g, '$1')
}
const row = () => ({ ...identity, receiptRequestSha256: 'a'.repeat(64), writerInstanceId: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
  writerEpoch: '7', writerRevision: '9', sourcePostSha256: 'b'.repeat(64), sourceOctets: '128',
  snapshotSha256: 'c'.repeat(64), snapshotOctets: '48', projectionText: wire() })

test('reads one scoped SQL projection without rounding i64 values', async () => {
  const calls: Array<{ sql: string, values?: readonly string[] }> = []
  const reader = new PostgresNormalizedProjectionReader({ query: async (sql, values) => {
    calls.push({ sql, values })
    return { rows: sql.startsWith('show ') ? [{ transaction_read_only: 'on' }] : [row()] }
  } })
  const records = await reader.findByIdentity(identity)
  assert.equal(calls.length, 2)
  assert.equal(calls[0]!.sql, 'show transaction_read_only')
  assert.deepEqual(calls[1]!.values, [identity.worldId, identity.characterId, identity.commandId])
  assert.match(calls[1]!.sql, /p\.character_id = \$2::uuid and p\.command_id = \$3::uuid/)
  assert.match(calls[1]!.sql, /order by d\.slot/)
  assert.match(calls[1]!.sql, /order by t\.slot/)
  assert.match(calls[1]!.sql, /order by i\.item_index/)
  assert.doesNotMatch(calls[1]!.sql, /\b(insert|update|delete|payload)\b/i)
  assert.deepEqual(records[0]!.projection.player, player)
  assert.equal(records[0]!.snapshotOctets, 48)
})

test('rejects invalid scope or writable sessions before reading evidence', async () => {
  let calls = 0
  const reader = new PostgresNormalizedProjectionReader({ query: async () => { calls++; return { rows: [{ transaction_read_only: 'off' }] } } })
  await assert.rejects(reader.findByIdentity({ ...identity, worldId: 'bad world' }))
  assert.equal(calls, 0)
  await assert.rejects(reader.findByIdentity(identity))
  assert.equal(calls, 1)
})

test('preserves absence and rejects malformed projection or mismatched returned identity', async () => {
  const cases = [
    { rows: [], valid: true },
    { rows: [row()], valid: true },
    { rows: [row(), row()], valid: true }, // The comparator owns duplicate classification.
    { rows: [{ ...row(), projectionText: wire().replace('9223372036854775807', '9223372036854775808') }], valid: false },
    { rows: [{ ...row(), worldId: 'other' }], valid: false },
    { rows: [{ ...row(), writerRevision: '09' }], valid: false },
  ]
  for (const { rows, valid } of cases) {
    const reader = new PostgresNormalizedProjectionReader({ query: async (sql) => ({ rows: sql.startsWith('show ') ? [{ transaction_read_only: 'on' }] : rows }) })
    if (valid) assert.equal((await reader.findByIdentity(identity)).length, rows.length)
    else await assert.rejects(reader.findByIdentity(identity))
  }
})

test('propagates query failures without retries and captures identity before awaiting', async () => {
  let calls = 0
  const scope = { ...identity }
  const reader = new PostgresNormalizedProjectionReader({ query: async (sql, values) => {
    calls++
    if (sql.startsWith('show ')) {
      scope.worldId = 'changed'
      return { rows: [{ transaction_read_only: 'on' }] }
    }
    assert.deepEqual(values, [identity.worldId, identity.characterId, identity.commandId])
    throw new Error('read unavailable')
  } })
  await assert.rejects(reader.findByIdentity(scope), /read unavailable/)
  assert.equal(calls, 2)
})
