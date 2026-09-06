import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmod, mkdtemp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { isAbsolute, join } from 'node:path'
import { NodePlayerSnapshotV1ArtifactFilesystem } from '../src/player-snapshot-v1-artifact-relay.js'
import { relayPlayerSnapshotV1ManifestFirstOnce } from '../src/player-snapshot-v1-manifest-first-relay.js'
import { PostgresManifestStore, PostgresPlayerSnapshotV1ArtifactStore, type ManifestStore, type PlayerSnapshotV1ArtifactStore } from '../src/store.js'

const require = createRequire(import.meta.url)

interface SqlClient {
  connect(): Promise<void>
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<{ rows: Row[] }>
  end(): Promise<void>
}

interface PgModule { Client: new (options: { connectionString: string }) => SqlClient }

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

interface EvidenceState {
  names: string[]
  outboxMode: string
  files: Record<string, { mode: string, mtimeNs: string, ctimeNs: string, bytes: string }>
}

interface DatabaseState { manifests: string, artifacts: string, legacy: string }

async function canonicalCStyleSnapshot(): Promise<Buffer> {
  // The source tree has a canonical C PlayerSnapshotV1 payload fixture, but
  // no production C runtime target that emits a reusable paired outbox.  This
  // closed seam preserves C FileStore authority while exercising its exact
  // immutable wire payload through the real PostgreSQL relay stores.
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

async function createOutbox(files: Readonly<Record<string, Uint8Array>>): Promise<string> {
  let outboxPath: string | undefined
  try {
    outboxPath = await mkdtemp(join(tmpdir(), 'pva-manifest-first-e2e-'))
    await chmod(outboxPath, 0o700)
    for (const [name, bytes] of Object.entries(files)) {
      await writeFile(join(outboxPath, name), bytes, { flag: 'wx', mode: 0o600 })
      await chmod(join(outboxPath, name), 0o600)
    }
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
      jsonb_build_object(
        'receipts', coalesce((select jsonb_agg(to_jsonb(r) order by r.command_id)
          from private.game_character_shadow_receipts r where r.character_id = $1::uuid), '[]'::jsonb),
        'head', (select to_jsonb(h) from private.game_character_legacy_heads h where h.character_id = $1::uuid),
        'legacy_snapshot', coalesce((select jsonb_agg(to_jsonb(s) order by s.revision)
          from private.game_character_snapshots s where s.character_id = $1::uuid), '[]'::jsonb)
      )::text as legacy
  `, [characterId, commandId])
  return result.rows[0]!
}

async function closeAll(paths: readonly string[], stores: readonly { close(): Promise<void> }[], client: SqlClient): Promise<void> {
  await Promise.all([...stores.map((store) => store.close()), client.end(), ...paths.map((path) => rm(path, { recursive: true, force: true }))])
}

async function main(): Promise<void> {
  assert.equal(process.platform, 'linux', 'the descriptor-rooted filesystem E2E must run in Linux')
  const payload = await canonicalCStyleSnapshot()
  const manifestBytes = receipt()
  const artifactBytes = artifact(payload)
  const validOutbox = await createOutbox({
    [`${commandId}.manifest`]: manifestBytes,
    [`${commandId}.player-snapshot-v1`]: artifactBytes,
  })
  const malformedOutbox = await createOutbox({
    [`${commandId}.manifest`]: manifestBytes,
    [`${commandId}.player-snapshot-v1`]: Buffer.from('not-a-c-player-snapshot-v1-artifact'),
  })
  const missingOutbox = await createOutbox({ [`${commandId}.manifest`]: manifestBytes })
  const manifestStore = new PostgresManifestStore(databaseUrl)
  const artifactStore = new PostgresPlayerSnapshotV1ArtifactStore(databaseUrl)
  const superClient = new (require('pg') as PgModule).Client({ connectionString: superDatabaseUrl })

  try {
    await superClient.connect()
    const sourceBefore = await evidenceState(validOutbox)
    const malformedBefore = await evidenceState(malformedOutbox)
    const missingBefore = await evidenceState(missingOutbox)
    assert.deepEqual(sourceBefore.names, [`${commandId}.manifest`, `${commandId}.player-snapshot-v1`])
    assert.equal(sourceBefore.outboxMode, '700')
    assert.deepEqual(Object.values(sourceBefore.files).map((file) => file.mode), ['600', '600'])

    const before = await databaseState(superClient)
    assert.deepEqual(before, { manifests: '0', artifacts: '0', legacy: before.legacy })
    const calls: string[] = []
    const realManifestFirstStore: ManifestStore & PlayerSnapshotV1ArtifactStore = {
      recordManifest: async (value) => {
        calls.push('manifest')
        return manifestStore.recordManifest(value)
      },
      recordPlayerSnapshotV1Artifact: async (value) => {
        calls.push('artifact')
        if (calls.length === 2) {
          const staged = await databaseState(superClient)
          assert.equal(staged.manifests, '1', 'the real manifest record must exist before the artifact store call')
          assert.equal(staged.artifacts, '0', 'the artifact must not be inserted before manifest-first delivery')
        }
        return artifactStore.recordPlayerSnapshotV1Artifact(value)
      },
    }
    assert.equal('fulfillGameCharacterOnboardingSnapshotEligibility' in realManifestFirstStore, false)
    assert.equal('recordPlayerSnapshotV1LevelProjection' in realManifestFirstStore, false)
    const filesystem = new NodePlayerSnapshotV1ArtifactFilesystem()

    assert.deepEqual(await relayPlayerSnapshotV1ManifestFirstOnce(validOutbox, realManifestFirstStore, filesystem), {
      visited: 1, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
      manifestDelivered: 1, manifestRecorded: 1, manifestExactRetry: 0,
      artifactDelivered: 1, artifactRecorded: 1, artifactExactRetry: 0,
    })
    assert.deepEqual(calls, ['manifest', 'artifact'])
    const recorded = await databaseState(superClient)
    assert.deepEqual(recorded, { manifests: '1', artifacts: '1', legacy: before.legacy })
    assert.deepEqual(await evidenceState(validOutbox), sourceBefore, 'manifest-first delivery must not mutate C-style source evidence')

    assert.deepEqual(await relayPlayerSnapshotV1ManifestFirstOnce(validOutbox, realManifestFirstStore, filesystem), {
      visited: 1, valid: 1, delivered: 1, recorded: 0, exactRetry: 1, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
      manifestDelivered: 1, manifestRecorded: 0, manifestExactRetry: 1,
      artifactDelivered: 1, artifactRecorded: 0, artifactExactRetry: 1,
    })
    assert.deepEqual(calls, ['manifest', 'artifact', 'manifest', 'artifact'])
    assert.deepEqual(await databaseState(superClient), recorded, 'an exact rerun must be database-idempotent')
    assert.deepEqual(await evidenceState(validOutbox), sourceBefore, 'an exact rerun must not mutate C-style source evidence')

    assert.deepEqual(await relayPlayerSnapshotV1ManifestFirstOnce(malformedOutbox, realManifestFirstStore, filesystem).then((result) => ({
      visited: result.visited, valid: result.valid, delivered: result.delivered, invalid: result.invalid,
    })), { visited: 1, valid: 0, delivered: 0, invalid: 1 })
    assert.deepEqual(await databaseState(superClient), recorded, 'a malformed pair must add no rows')
    assert.deepEqual(await evidenceState(malformedOutbox), malformedBefore, 'a malformed pair must remain immutable')

    assert.deepEqual(await relayPlayerSnapshotV1ManifestFirstOnce(missingOutbox, realManifestFirstStore, filesystem).then((result) => ({
      visited: result.visited, valid: result.valid, delivered: result.delivered, invalid: result.invalid,
    })), { visited: 0, valid: 0, delivered: 0, invalid: 0 })
    assert.deepEqual(await databaseState(superClient), recorded, 'a missing pair member must add no rows')
    assert.deepEqual(await evidenceState(missingOutbox), missingBefore, 'a missing pair member must remain immutable')
    console.log('GREEN PostgreSQL 17 Linux: manifest-first relay records the immutable C-style pair before its artifact, retries exactly, and leaves malformed/missing evidence inert')
  } finally {
    await closeAll([validOutbox, malformedOutbox, missingOutbox], [manifestStore, artifactStore], superClient)
  }
}

await main()
