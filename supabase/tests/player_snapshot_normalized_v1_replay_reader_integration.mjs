import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile, mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises'
import { spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { createRequire } from 'node:module'
import { isAbsolute, join } from 'node:path'

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
let outboxRoot
let stage = 'reader-setup'
try {
  const url = new URL(env.NORMALIZED_READER_TEST_DATABASE_URL)
  assert.ok(['postgres:', 'postgresql:'].includes(url.protocol))
  assert.equal(decodeURIComponent(url.username), 'mud_normalized_replay_reader_login')
  assert.ok(isAbsolute(env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER))
  if (env.NORMALIZED_READER_TEST_RELAY_ROOT) assert.ok(isAbsolute(env.NORMALIZED_READER_TEST_RELAY_ROOT))
  const relayBase = env.NORMALIZED_READER_TEST_RELAY_ROOT
    ? pathToFileURL(env.NORMALIZED_READER_TEST_RELAY_ROOT.replace(/\/$/, '') + '/')
    : new URL('../../services/m4-file-snapshot-manifest-relay/', import.meta.url)
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
  const { projectPlayerSnapshotV1Normalized } = await import(new URL('dist/player-snapshot-v1-normalized-projection.js', relayBase))
  const { PostgresNormalizedProjectionReader } = await import(new URL('dist/player-snapshot-v1-normalized-projection-reader.js', relayBase))
  const expected = await projectPlayerSnapshotV1Normalized(fixture, {
    runnerPath: env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER, snapshotSha256,
  })
  const requireRelay = createRequire(new URL('package.json', relayBase))
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
  const { PostgresNormalizedProjectionSessionReader } = await import(new URL('dist/player-snapshot-v1-normalized-projection-session.js', relayBase))
  const sessions = new PostgresNormalizedProjectionSessionReader(url.toString())
  try {
    assert.deepEqual(await sessions.findByIdentity(identity), records)
    // Reusing the pool must not inherit an unfinished transaction.
    assert.deepEqual(await sessions.findByIdentity(identity), records)
  } finally { await sessions.close() }
  const metadata = await client.query(`select legacy_name_key, storage_format::text as storage_format
    from private.game_character_player_snapshot_v1_artifacts
    where character_id = $1::uuid and command_id = $2::uuid`, [identity.characterId, identity.commandId])
  assert.equal(metadata.rows.length, 1)
  await client.query('rollback')
  const row = records[0], canonicalNameHex = Buffer.from(metadata.rows[0].legacy_name_key, 'utf8').toString('hex')
  outboxRoot = await mkdtemp(join(tmpdir(), 'normalized-cli-e2e-'))
  const cli = fileURLToPath(new URL('dist/player-snapshot-v1-normalized-shadow-cli.js', relayBase))
  const cases = [
    { name: 'match', commandId: identity.commandId, requestHash: row.receiptRequestSha256, classification: 'MATCH', code: 0 },
    { name: 'bad-receipt', commandId: identity.commandId, requestHash: row.receiptRequestSha256 === '0'.repeat(64) ? '1'.repeat(64) : '0'.repeat(64), classification: 'INVALID_INPUT', code: 1 },
    { name: 'missing', commandId: 'c9140000-0000-0000-0000-000000000099', requestHash: row.receiptRequestSha256, classification: 'MISSING_RECORD', code: 1 },
  ]
  for (const scenario of cases) {
    stage = `cli-${scenario.name}`
    const directory = join(outboxRoot, scenario.name)
    await mkdir(directory, { mode: 0o700 })
    const common = ['version=1', `world_id=${identity.worldId}`, `character_id=${identity.characterId}`,
      `command_id=${scenario.commandId}`, `canonical_name_hex=${canonicalNameHex}`]
    const writer = [`writer_epoch=${row.writerEpoch}`, `writer_revision=${row.writerRevision}`, `storage_format=${metadata.rows[0].storage_format}`]
    const receipt = [...common, `request_sha256=${scenario.requestHash}`, `post_sha256=${row.sourcePostSha256}`,
      `writer_instance_id=${row.writerInstanceId}`, 'snapshot_format=legacy-file-manifest-v1', ...writer, `snapshot_octets=${row.sourceOctets}`, ''].join('\n')
    const header = [...common, `request_sha256=${row.receiptRequestSha256}`, `source_post_sha256=${row.sourcePostSha256}`,
      `writer_instance_id=${row.writerInstanceId}`, ...writer, 'snapshot_format=player-snapshot-v1', `source_octets=${row.sourceOctets}`,
      `snapshot_sha256=${snapshotSha256}`, `snapshot_octets=${fixture.length}`, '', ''].join('\n')
    const artifact = Buffer.concat([Buffer.from(header), fixture])
    const receiptPath = join(directory, `${scenario.commandId}.manifest`)
    const artifactPath = join(directory, `${scenario.commandId}.player-snapshot-v1`)
    await writeFile(receiptPath, receipt, { mode: 0o600, flag: 'wx' })
    await writeFile(artifactPath, artifact, { mode: 0o600, flag: 'wx' })
    const result = spawnSync(process.execPath, [cli, '--once'], { encoding: 'utf8', timeout: 20000, env: {
      PATH: env.PATH,
      M4_NORMALIZED_SHADOW_OUTBOX_PATH: directory,
      M4_NORMALIZED_SHADOW_DATABASE_URL: url.toString(),
      M4_NORMALIZED_SHADOW_PROJECTOR_PATH: env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER,
    } })
    if (result.status !== scenario.code) {
      const actual = (() => { try { return JSON.parse(result.stdout).classification } catch { return undefined } })()
      const allowed = ['MATCH', 'INVALID_INPUT', 'INVALID_ARTIFACT', 'PROJECTION_DERIVATION_FAILED', 'RECORD_READ_ERROR', 'MISSING_RECORD', 'UNEXPECTED_DUPLICATE', 'INVALID_RECORD', 'EVIDENCE_MISMATCH', 'PROJECTION_MISMATCH', 'COMPARISON_ERROR', 'CONNECTION_CLOSE_ERROR']
      stage += allowed.includes(actual) ? `-${actual}` : '-unexpected-process-result'
    }
    assert.equal(result.status, scenario.code, scenario.name)
    assert.equal(result.stderr, '')
    assert.equal(result.stdout, JSON.stringify({ format: 'player-snapshot-v1-normalized-shadow-comparison', version: '1', classification: scenario.classification }) + '\n')
    assert.equal(await readFile(receiptPath, 'utf8'), receipt)
    assert.deepEqual(await readFile(artifactPath), artifact)
  }
} catch {
  // Do not print connection strings, fixture bytes, or raw PostgreSQL errors.
  console.error(`Normalized reader integration failed (${stage})`)
  process.exitCode = 1
} finally {
  if (client) {
    if (connected) {
      try { await client.query('rollback') } catch { process.exitCode = 1 }
    }
    try { await client.end() } catch { process.exitCode = 1 }
  }
  if (outboxRoot) await rm(outboxRoot, { recursive: true, force: true })
}
if (process.exitCode !== 1) {
  console.log('Normalized reader integration passed: SQL, pooled sessions, CLI MATCH/INVALID_INPUT/MISSING_RECORD, and unchanged input files')
}
