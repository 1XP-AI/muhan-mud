import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import {
  PlayerSnapshotV1ReplayObserver,
  playerSnapshotV1ReplayObserverFromEnvironment,
  type PlayerSnapshotV1ReplayVerifier,
} from '../src/player-snapshot-v1-replay-observer.js'

const payload = Buffer.from('player snapshot payload')

test('replay observer injects only an absolute runner path and forwards only the payload', async () => {
  const calls: Array<{ payload: Uint8Array, runnerPath?: string }> = []
  const digest = createHash('sha256').update(payload).digest('hex')
  const verifier: PlayerSnapshotV1ReplayVerifier = async (value, options) => {
    calls.push({ payload: Buffer.from(value), runnerPath: options.runnerPath })
    return {
      format: 'player-snapshot-v1-replay-verification', version: '1', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: value.length, inventoryNodeCount: 0,
    }
  }
  const observer = playerSnapshotV1ReplayObserverFromEnvironment({
    M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
  }, verifier)

  assert.equal(await observer.observe(payload), 'observed')
  assert.deepEqual(calls, [{
    payload,
    runnerPath: '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
  }])
})

test('replay observer remains explicitly disabled without an absolute configured runner', async () => {
  let called = false
  const verifier: PlayerSnapshotV1ReplayVerifier = async () => {
    called = true
    throw new Error('must not run')
  }
  for (const env of [{}, { M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: 'player_snapshot_v1_replay_verify' }, { M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH: '/bad\0path' }]) {
    const observer = playerSnapshotV1ReplayObserverFromEnvironment(env, verifier)
    assert.equal(await observer.observe(payload), 'disabled')
  }
  assert.equal(called, false)
})

test('replay observer treats runner and report failures as disabled without exposing their details', async () => {
  const observer = new PlayerSnapshotV1ReplayObserver(
    '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
    async () => { throw new Error('untrusted runner output') },
  )

  assert.equal(await observer.observe(payload), 'disabled')
})

test('replay observer disables malformed runtime results from an injected verifier', async () => {
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
    const observer = new PlayerSnapshotV1ReplayObserver(
      '/usr/local/libexec/muhan/player_snapshot_v1_replay_verify',
      async () => result as never,
    )
    assert.equal(await observer.observe(payload), 'disabled')
  }
})
