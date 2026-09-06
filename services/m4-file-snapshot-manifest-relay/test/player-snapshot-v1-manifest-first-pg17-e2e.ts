import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { chmod, mkdtemp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { isAbsolute, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)

interface SqlClient {
  connect(): Promise<void>
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<{ rows: Row[] }>
  end(): Promise<void>
}

interface PgModule { Client: new (options: { connectionString: string }) => SqlClient }

interface ManifestFirstSummary {
  visited: number
  valid: number
  delivered: number
  recorded: number
  exactRetry: number
  invalid: number
  conflict: number
  retryable: number
  unknown: number
  ioError: number
  manifestDelivered: number
  manifestRecorded: number
  manifestExactRetry: number
  artifactDelivered: number
  artifactRecorded: number
  artifactExactRetry: number
}

interface CliResult { code: number | null, stdout: string, stderr: string }
interface EvidenceState {
  names: string[]
  outboxMode: string
  files: Record<string, { mode: string, mtimeNs: string, ctimeNs: string, bytes: string }>
}
interface DatabaseState { manifests: string, artifacts: string, paired: string, legacy: string, capabilities: string }

function required(name: string): string {
  const value = process.env[name]
  if (!value || value.includes('\0')) throw new Error(`missing ${name}`)
  return value
}

const databaseUrl = required('PLAYER_SNAPSHOT_V1_RELAY_E2E_DATABASE_URL')
const superDatabaseUrl = required('PLAYER_SNAPSHOT_V1_RELAY_E2E_SUPER_DATABASE_URL')
const requestSha256 = required('PLAYER_SNAPSHOT_V1_RELAY_E2E_REQUEST_SHA256')
const cStyleSnapshotFixture = required('PLAYER_SNAPSHOT_V1_RELAY_E2E_C_STYLE_SNAPSHOT_FIXTURE')
if (!isAbsolute(cStyleSnapshotFixture)) throw new Error('C-style snapshot fixture path must be absolute')

const characterId = 'a9510000-0000-0000-0000-000000000001'
const commandId = 'c9510000-0000-0000-0000-000000000001'
const worldId = 'pva-relay-e2e'
const writerInstanceId = 'b9510000-0000-0000-0000-000000000001'
const sourcePostSha256 = 'a'.repeat(64)
const serviceRoot = resolve(fileURLToPath(new URL('..', import.meta.url)))
const cliPath = join(serviceRoot, 'dist', 'player-snapshot-v1-manifest-first-cli.js')

async function canonicalCStyleSnapshot(): Promise<Buffer> {
  const hex = (await readFile(cStyleSnapshotFixture, 'ascii')).trim()
  assert.match(hex, /^(?:[0-9a-f]{2})+$/)
  const payload = Buffer.from(hex, 'hex')
  assert.equal(payload.toString('ascii', 0, 8), 'MUHCDTO\0')
  return payload
}

function receipt(): Buffer {
  return Buffer.from([
    'version=1', `world_id=${worldId}`, `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4532656865726f', `request_sha256=${requestSha256}`,
    `post_sha256=${sourcePostSha256}`, `writer_instance_id=${writerInstanceId}`,
    'snapshot_format=legacy-file-manifest-v1', 'writer_epoch=1', 'writer_revision=1',
    'storage_format=1', 'snapshot_octets=9', '',
  ].join('\n'), 'ascii')
}

function artifact(payload: Uint8Array): Buffer {
  return Buffer.concat([Buffer.from([
    'version=1', `world_id=${worldId}`, `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4532656865726f', `request_sha256=${requestSha256}`,
    `source_post_sha256=${sourcePostSha256}`, `writer_instance_id=${writerInstanceId}`,
    'writer_epoch=1', 'writer_revision=1', 'storage_format=1',
    'snapshot_format=player-snapshot-v1', 'source_octets=9',
    `snapshot_sha256=${createHash('sha256').update(payload).digest('hex')}`,
    `snapshot_octets=${payload.length}`, '', '',
  ].join('\n'), 'ascii'), Buffer.from(payload)])
}

async function createOutbox(payload: Uint8Array): Promise<string> {
  let outboxPath: string | undefined
  try {
    outboxPath = await mkdtemp(join(tmpdir(), 'pva-manifest-first-cli-e2e-'))
    await chmod(outboxPath, 0o700)
    await writeFile(join(outboxPath, `${commandId}.manifest`), receipt(), { flag: 'wx', mode: 0o600 })
    await writeFile(join(outboxPath, `${commandId}.player-snapshot-v1`), artifact(payload), { flag: 'wx', mode: 0o600 })
    await Promise.all([
      chmod(join(outboxPath, `${commandId}.manifest`), 0o600),
      chmod(join(outboxPath, `${commandId}.player-snapshot-v1`), 0o600),
    ])
    return outboxPath
  } catch (error) {
    if (outboxPath) await rm(outboxPath, { recursive: true, force: true })
    throw error
  }
}

async function evidenceState(outboxPath: string): Promise<EvidenceState> {
  const names = (await readdir(outboxPath)).sort()
  const files = Object.fromEntries(await Promise.all(names.map(async (name) => {
    const [metadata, bytes] = await Promise.all([stat(join(outboxPath, name), { bigint: true }), readFile(join(outboxPath, name))])
    return [name, {
      mode: (metadata.mode & 0o777n).toString(8), mtimeNs: metadata.mtimeNs.toString(),
      ctimeNs: metadata.ctimeNs.toString(), bytes: bytes.toString('hex'),
    }]
  })))
  const outbox = await stat(outboxPath, { bigint: true })
  return { names, outboxMode: (outbox.mode & 0o777n).toString(8), files }
}

async function databaseState(client: SqlClient): Promise<DatabaseState> {
  const result = await client.query<DatabaseState>(`
    select
      (select count(*)::text from private.game_character_m4_file_snapshot_manifests
        where character_id = $1::uuid and command_id = $2::uuid) as manifests,
      (select count(*)::text from private.game_character_player_snapshot_v1_artifacts
        where character_id = $1::uuid and command_id = $2::uuid) as artifacts,
      (select count(*)::text from private.game_character_player_snapshot_v1_artifacts a
        join private.game_character_m4_file_snapshot_manifests m
          on m.character_id = a.character_id and m.command_id = a.command_id
        where a.character_id = $1::uuid and a.command_id = $2::uuid
          and m.request_sha256 = a.receipt_request_sha256
          and m.writer_instance_id = a.writer_instance_id
          and m.writer_epoch = a.writer_epoch and m.writer_revision = a.writer_revision
          and m.post_sha256 = a.source_post_sha256 and m.storage_format = a.storage_format) as paired,
      jsonb_build_object(
        'receipts', coalesce((select jsonb_agg(to_jsonb(r) order by r.command_id)
          from private.game_character_shadow_receipts r where r.character_id = $1::uuid), '[]'::jsonb),
        'head', (select to_jsonb(h) from private.game_character_legacy_heads h where h.character_id = $1::uuid),
        'legacy_snapshot', coalesce((select jsonb_agg(to_jsonb(s) order by s.revision)
          from private.game_character_snapshots s where s.character_id = $1::uuid), '[]'::jsonb)
      )::text as legacy,
      (select count(*)::text from pg_proc p join pg_namespace n on n.oid = p.pronamespace
        where n.nspname = 'private' and p.proname in (
          'fulfill_game_character_onboarding_snapshot_eligibility',
          'record_player_snapshot_v1_level_projection_for_receipt')) as capabilities
  `, [characterId, commandId])
  return result.rows[0]!
}

function runCli(outboxPath: string): Promise<CliResult> {
  return new Promise((resolveResult, reject) => {
    const child = spawn(process.execPath, [cliPath, '--once'], {
      cwd: serviceRoot,
      env: { ...process.env, DATABASE_URL: databaseUrl, M4_FILE_SNAPSHOT_OUTBOX_DIR: outboxPath },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let stdout = ''
    let stderr = ''
    child.stdout.setEncoding('utf8').on('data', (chunk: string) => { stdout += chunk })
    child.stderr.setEncoding('utf8').on('data', (chunk: string) => { stderr += chunk })
    child.once('error', reject)
    child.once('close', (code) => { resolveResult({ code, stdout, stderr }) })
  })
}

function parsedSummary(result: CliResult): ManifestFirstSummary {
  assert.equal(result.code, 0, 'the production CLI must exit successfully')
  assert.equal(result.stderr, '', 'the production CLI must not report an error')
  assert.match(result.stdout, /^\{[^\n]+\}\n$/)
  const summary: unknown = JSON.parse(result.stdout)
  if (typeof summary !== 'object' || summary === null || Array.isArray(summary)) throw new Error('the production CLI must emit an object summary')
  assert.equal('fulfillmentDelivered' in summary, false)
  assert.equal('projectionDelivered' in summary, false)
  return summary as ManifestFirstSummary
}

async function main(): Promise<void> {
  assert.equal(process.platform, 'linux', 'the descriptor-rooted filesystem E2E must run in Linux')
  await stat(cliPath)
  const outboxPath = await createOutbox(await canonicalCStyleSnapshot())
  const superClient = new (require('pg') as PgModule).Client({ connectionString: superDatabaseUrl })
  try {
    await superClient.connect()
    const sourceBefore = await evidenceState(outboxPath)
    assert.deepEqual(sourceBefore.names, [`${commandId}.manifest`, `${commandId}.player-snapshot-v1`])
    assert.equal(sourceBefore.outboxMode, '700')
    assert.deepEqual(Object.values(sourceBefore.files).map((file) => file.mode), ['600', '600'])
    const before = await databaseState(superClient)
    assert.deepEqual(before, { manifests: '0', artifacts: '0', paired: '0', legacy: before.legacy, capabilities: '0' })

    assert.deepEqual(parsedSummary(await runCli(outboxPath)), {
      visited: 1, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
      manifestDelivered: 1, manifestRecorded: 1, manifestExactRetry: 0,
      artifactDelivered: 1, artifactRecorded: 1, artifactExactRetry: 0,
    })
    const recorded = await databaseState(superClient)
    assert.deepEqual(recorded, { manifests: '1', artifacts: '1', paired: '1', legacy: before.legacy, capabilities: '0' })
    assert.deepEqual(await evidenceState(outboxPath), sourceBefore, 'the production CLI must not mutate source evidence')

    const retry = parsedSummary(await runCli(outboxPath))
    assert.deepEqual(retry, {
      visited: 1, valid: 1, delivered: 1, recorded: 0, exactRetry: 1, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
      manifestDelivered: 1, manifestRecorded: 0, manifestExactRetry: 1,
      artifactDelivered: 1, artifactRecorded: 0, artifactExactRetry: 1,
    })
    assert.deepEqual(await databaseState(superClient), recorded, 'the production CLI exact retry must add no rows')
    assert.deepEqual(await evidenceState(outboxPath), sourceBefore, 'the production CLI exact retry must not mutate source evidence')
    console.log('GREEN PostgreSQL 17 Linux: compiled manifest-first CLI records the immutable pair manifest-before-artifact, retries exactly, and leaves source evidence inert')
  } finally {
    await Promise.all([superClient.end(), rm(outboxPath, { recursive: true, force: true })])
  }
}

await main()
