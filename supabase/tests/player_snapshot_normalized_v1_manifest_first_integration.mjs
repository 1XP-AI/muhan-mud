import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile, mkdtemp, writeFile, rm } from 'node:fs/promises'
import { spawnSync } from 'node:child_process'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { isAbsolute, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

// Mutating test lane: use ONLY a disposable database prepared with receipt_only.
const env = process.env
const keys = ['NORMALIZED_READER_TEST_DATABASE_URL', 'NORMALIZED_MANIFEST_FIRST_WRITER_DATABASE_URL',
  'M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER']
if (env.NORMALIZED_READER_ALLOW_DISPOSABLE !== '1' || keys.some(key => !env[key])) {
  console.error('Normalized manifest-first integration not run: disposable opt-in and explicit reader/writer inputs required')
  process.exit(2)
}
let client, directory
let stage = 'setup'
try {
  assert.equal(process.platform, 'linux')
  const readerUrl = new URL(env.NORMALIZED_READER_TEST_DATABASE_URL)
  const writerUrl = new URL(env.NORMALIZED_MANIFEST_FIRST_WRITER_DATABASE_URL)
  for (const url of [readerUrl, writerUrl]) assert.ok(['postgres:', 'postgresql:'].includes(url.protocol))
  assert.equal(decodeURIComponent(readerUrl.username), 'mud_normalized_replay_reader_login')
  assert.equal(decodeURIComponent(writerUrl.username), 'mud_writer_login')
  assert.deepEqual([writerUrl.hostname, writerUrl.port, writerUrl.pathname], [readerUrl.hostname, readerUrl.port, readerUrl.pathname])
  assert.ok(isAbsolute(env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER))
  if (env.NORMALIZED_READER_TEST_RELAY_ROOT) assert.ok(isAbsolute(env.NORMALIZED_READER_TEST_RELAY_ROOT))
  const base = env.NORMALIZED_READER_TEST_RELAY_ROOT
    ? pathToFileURL(env.NORMALIZED_READER_TEST_RELAY_ROOT.replace(/\/$/, '') + '/')
    : new URL('../../services/m4-file-snapshot-manifest-relay/', import.meta.url)
  const { Client } = createRequire(new URL('package.json', base))('pg')
  const identity = ['a9140000-0000-0000-0000-000000000001', 'c9140000-0000-0000-0000-000000000001']
  client = new Client({ connectionString: readerUrl.toString(), connectionTimeoutMillis: 5000 })
  await client.connect()
  const session = await client.query('select current_user, session_user')
  assert.deepEqual(session.rows, [{ current_user: 'mud_normalized_replay_reader_login', session_user: 'mud_normalized_replay_reader_login' }])
  for (const table of ['game_character_m4_file_snapshot_manifests', 'game_character_player_snapshot_v1_artifacts',
    'game_character_player_snapshot_normalized_v1_projections']) {
    const count = await client.query(`select count(character_id)::text as count from private.${table} where character_id=$1::uuid and command_id=$2::uuid`, identity)
    assert.equal(count.rows[0].count, '0', 'no prerequisite output may be preseeded')
  }
  const receiptRow = await client.query(`select world_id, legacy_name_key, request_sha256, post_sha256,
    writer_instance_id::text, writer_epoch::text, writer_revision::text, storage_format
    from private.game_character_shadow_receipts where character_id=$1::uuid and command_id=$2::uuid`, identity)
  assert.equal(receiptRow.rows.length, 1)
  const row = receiptRow.rows[0]
  assert.equal(row.world_id, 'normalized-reader-test'); assert.equal(row.legacy_name_key, 'Nprhero')
  assert.equal(row.post_sha256, 'a'.repeat(64)); assert.equal(row.storage_format, 1)
  const payload = Buffer.from((await readFile(new URL('../../tests/fixtures/player_snapshot_v1_tree_inventory.hex', import.meta.url), 'utf8')).trim(), 'hex')
  const common = ['version=1', `world_id=${row.world_id}`, `character_id=${identity[0]}`, `command_id=${identity[1]}`,
    `canonical_name_hex=${Buffer.from(row.legacy_name_key).toString('hex')}`, `request_sha256=${row.request_sha256}`]
  const writer = [`writer_epoch=${row.writer_epoch}`, `writer_revision=${row.writer_revision}`, 'storage_format=1']
  const receipt = [...common, `post_sha256=${row.post_sha256}`, `writer_instance_id=${row.writer_instance_id}`,
    'snapshot_format=legacy-file-manifest-v1', ...writer, 'snapshot_octets=9', ''].join('\n')
  const artifact = Buffer.concat([Buffer.from([...common, `source_post_sha256=${row.post_sha256}`,
    `writer_instance_id=${row.writer_instance_id}`, ...writer, 'snapshot_format=player-snapshot-v1', 'source_octets=9',
    `snapshot_sha256=${createHash('sha256').update(payload).digest('hex')}`, `snapshot_octets=${payload.length}`, '', ''].join('\n')), payload])
  directory = await mkdtemp(join(tmpdir(), 'normalized-manifest-first-'))
  const receiptPath = join(directory, `${identity[1]}.manifest`), artifactPath = join(directory, `${identity[1]}.player-snapshot-v1`)
  await writeFile(receiptPath, receipt, { mode: 0o600, flag: 'wx' })
  await writeFile(artifactPath, artifact, { mode: 0o600, flag: 'wx' })
  for (const attempt of ['Recorded', 'ExactRetry']) {
    stage = `writer-${attempt}`
    const result = spawnSync(process.execPath, [fileURLToPath(new URL('dist/player-snapshot-v1-manifest-first-cli.js', base)), '--once'], {
      encoding: 'utf8', timeout: 30000, env: { PATH: env.PATH, DATABASE_URL: writerUrl.toString(),
        M4_FILE_SNAPSHOT_OUTBOX_DIR: directory, M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ENABLED: 'true',
        M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_RUNNER: env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER },
    })
    assert.equal(result.status, 0); assert.equal(result.stderr, '')
    const summary = JSON.parse(result.stdout)
    assert.equal(summary.delivered, 1)
    for (const prefix of ['manifest', 'artifact', 'normalizedProjection']) assert.equal(summary[`${prefix}${attempt}`], 1)
    for (const key of ['invalid', 'conflict', 'retryable', 'unknown', 'ioError']) assert.equal(summary[key], 0)
    assert.equal(await readFile(receiptPath, 'utf8'), receipt)
    assert.deepEqual(await readFile(artifactPath), artifact)
  }
  stage = 'reader-comparison'
  const comparison = spawnSync(process.execPath, [fileURLToPath(new URL('./player_snapshot_normalized_v1_replay_reader_integration.mjs', import.meta.url))], {
    encoding: 'utf8', timeout: 90000, env: { PATH: env.PATH, NORMALIZED_READER_ALLOW_DISPOSABLE: '1',
      NORMALIZED_READER_TEST_DATABASE_URL: readerUrl.toString(), NORMALIZED_READER_TEST_WORLD_ID: row.world_id,
      NORMALIZED_READER_TEST_CHARACTER_ID: identity[0], NORMALIZED_READER_TEST_COMMAND_ID: identity[1],
      ...(env.NORMALIZED_READER_TEST_RELAY_ROOT ? { NORMALIZED_READER_TEST_RELAY_ROOT: env.NORMALIZED_READER_TEST_RELAY_ROOT } : {}),
      M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER: env.M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER },
  })
  assert.equal(comparison.status, 0); assert.equal(comparison.stderr, '')
  assert.match(comparison.stdout, /Normalized reader integration passed:/)
} catch {
  console.error(`Normalized manifest-first integration failed (${stage})`)
  process.exitCode = 1
} finally {
  if (client) try { await client.end() } catch { process.exitCode = 1 }
  if (directory) await rm(directory, { recursive: true, force: true })
}
if (process.exitCode !== 1) console.log('Normalized manifest-first integration passed: new records, exact retry, reader comparison, unchanged evidence')
