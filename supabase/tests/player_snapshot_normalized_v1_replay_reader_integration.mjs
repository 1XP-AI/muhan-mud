import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { isAbsolute } from 'node:path'

// Operator-only, read-only integration lane. Migrations and fixture persistence
// must already be complete in a disposable database. This runner neither seeds
// data nor provisions a login. In particular, it never accepts DATABASE_URL as
// an implicit fallback to an application or writer connection.
const env = process.env
const required = [
  'NORMALIZED_READER_TEST_DATABASE_URL', 'NORMALIZED_READER_TEST_WORLD_ID',
  'NORMALIZED_READER_TEST_CHARACTER_ID', 'NORMALIZED_READER_TEST_COMMAND_ID',
  'M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER',
]
if (env.NORMALIZED_READER_ALLOW_DISPOSABLE !== '1' || required.some(key => !env[key])) {
  console.error('Normalized reader integration not run: disposable opt-in and all test inputs are required')
  process.exit(2)
}

let client
let connected = false
try {
  const url = new URL(env.NORMALIZED_READER_TEST_DATABASE_URL)
  assert.ok(['postgres:', 'postgresql:'].includes(url.protocol))
  assert.equal(decodeURIComponent(url.username), 'mud_normalized_replay_reader_login')
  assert.ok(isAbsolute(env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER))
  const identity = {
    worldId: env.NORMALIZED_READER_TEST_WORLD_ID,
    characterId: env.NORMALIZED_READER_TEST_CHARACTER_ID,
    commandId: env.NORMALIZED_READER_TEST_COMMAND_ID,
  }
  assert.match(identity.worldId, /^[a-z][a-z0-9_-]{0,63}$/)
  for (const value of [identity.characterId, identity.commandId]) {
    assert.match(value, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
  }
  const fixture = Buffer.from((await readFile(new URL('../../tests/fixtures/player_snapshot_v1_tree_inventory.hex', import.meta.url), 'utf8')).trim(), 'hex')
  const snapshotSha256 = createHash('sha256').update(fixture).digest('hex')
  const { projectPlayerSnapshotV1Normalized } = await import('../../services/m4-file-snapshot-manifest-relay/dist/player-snapshot-v1-normalized-projection.js')
  const { PostgresNormalizedProjectionReader } = await import('../../services/m4-file-snapshot-manifest-relay/dist/player-snapshot-v1-normalized-projection-reader.js')
  const expected = await projectPlayerSnapshotV1Normalized(fixture, {
    runnerPath: env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER, snapshotSha256,
  })
  const requireRelay = createRequire(new URL('../../services/m4-file-snapshot-manifest-relay/package.json', import.meta.url))
  const { Client } = requireRelay('pg')
  client = new Client({ connectionString: url.toString(), connectionTimeoutMillis: 5000 })
  await client.connect()
  connected = true
  const session = await client.query('select current_user, session_user, current_setting(\'default_transaction_read_only\') as default_read_only')
  assert.deepEqual(session.rows, [{ current_user: 'mud_normalized_replay_reader_login', session_user: 'mud_normalized_replay_reader_login', default_read_only: 'on' }])
  await client.query('begin read only')
  const reader = new PostgresNormalizedProjectionReader(client)
  const records = await reader.findByIdentity(identity)
  assert.equal(records.length, 1, 'preseeded fixture must be present')
  assert.equal(records[0].snapshotSha256, snapshotSha256)
  assert.equal(records[0].snapshotOctets, fixture.length)
  assert.deepEqual(records[0].projection, expected)

  // The same character/command must not be visible under a different world.
  const otherWorld = identity.worldId === 'normalized-other' ? 'normalized-different' : 'normalized-other'
  assert.deepEqual(await reader.findByIdentity({ ...identity, worldId: otherWorld }), [])
  // Check effective column grants, not just a catalog policy's existence.
  await client.query('savepoint denied_payload')
  await assert.rejects(client.query('select payload from private.game_character_player_snapshot_v1_artifacts limit 0'), { code: '42501' })
  await client.query('rollback to savepoint denied_payload')
  assert.equal((await reader.findByIdentity(identity)).length, 1)
  const { PostgresNormalizedProjectionSessionReader } = await import('../../services/m4-file-snapshot-manifest-relay/dist/player-snapshot-v1-normalized-projection-session.js')
  const sessions = new PostgresNormalizedProjectionSessionReader(url.toString())
  try {
    assert.deepEqual(await sessions.findByIdentity(identity), records)
    // Reusing the pool must not inherit an unfinished transaction.
    assert.deepEqual(await sessions.findByIdentity(identity), records)
  } finally { await sessions.close() }
} catch {
  // Do not print connection strings, fixture bytes, or raw PostgreSQL errors.
  console.error('Normalized reader integration failed')
  process.exitCode = 1
} finally {
  if (client) {
    if (connected) {
      try { await client.query('rollback') } catch { process.exitCode = 1 }
    }
    try { await client.end() } catch { process.exitCode = 1 }
  }
}
if (process.exitCode !== 1) {
  console.log('Normalized reader integration passed: real SQL, fixture projection, world scope, payload exclusion, and pooled sessions')
}
