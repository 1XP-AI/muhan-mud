import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

const runner = fileURLToPath(new URL('./player_snapshot_normalized_v1_replay_reader_integration.mjs', import.meta.url))
const inputs = {
  NORMALIZED_READER_ALLOW_DISPOSABLE: '1',
  NORMALIZED_READER_TEST_DATABASE_URL: 'postgresql://wrong_login:sentinel-secret@example.invalid/disposable',
  NORMALIZED_READER_TEST_WORLD_ID: 'muhan-01',
  NORMALIZED_READER_TEST_CHARACTER_ID: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
  NORMALIZED_READER_TEST_COMMAND_ID: '11111111-1111-4111-8111-111111111111',
  M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER: '/not-executed/projector',
}

// Every case exits before importing the PostgreSQL driver or running Rust.
test('requires explicit test configuration and never inherits application DATABASE_URL', () => {
  for (const missing of Object.keys(inputs)) {
    const env = { ...inputs, DATABASE_URL: 'postgresql://application:sentinel-secret@example.invalid/live' }
    delete env[missing]
    const result = spawnSync(process.execPath, [runner], { env, encoding: 'utf8', timeout: 5000 })
    assert.equal(result.status, 2, missing)
    assert.match(result.stderr, /not run/)
    assert.equal(result.stdout, '')
    assert.doesNotMatch(result.stderr, /sentinel-secret|example\.invalid/)
  }
})

test('rejects a non-dedicated login before loading or connecting the driver', () => {
  const result = spawnSync(process.execPath, [runner], { env: inputs, encoding: 'utf8', timeout: 5000 })
  assert.equal(result.status, 1)
  assert.equal(result.stdout, '')
  assert.equal(result.stderr.trim(), 'Normalized reader integration failed (reader-setup)')
})
