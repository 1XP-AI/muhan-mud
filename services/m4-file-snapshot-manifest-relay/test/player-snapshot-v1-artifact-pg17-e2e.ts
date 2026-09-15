import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmod, mkdtemp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { NodePlayerSnapshotV1ArtifactFilesystem, relayPlayerSnapshotV1ArtifactsOnce } from '../src/player-snapshot-v1-artifact-relay.js'
import { PostgresPlayerSnapshotV1ArtifactStore, PostgresPlayerSnapshotV1LevelProjectionStore, type PlayerSnapshotV1LevelProjectionStore } from '../src/store.js'

const require = createRequire(import.meta.url)

interface SqlClient {
  connect(): Promise<void>
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<{ rows: Row[] }>
  end(): Promise<void>
}

interface PgModule { Client: new (options: { connectionString: string }) => SqlClient }

const databaseUrl = process.env.PLAYER_SNAPSHOT_V1_RELAY_E2E_DATABASE_URL
const superDatabaseUrl = process.env.PLAYER_SNAPSHOT_V1_RELAY_E2E_SUPER_DATABASE_URL
const requestSha256 = process.env.PLAYER_SNAPSHOT_V1_RELAY_E2E_REQUEST_SHA256
if (!databaseUrl || !superDatabaseUrl || !requestSha256) throw new Error('PlayerSnapshotV1 relay PG17 E2E configuration is missing')

const characterId = 'a9510000-0000-0000-0000-000000000001'
const commandId = 'c9510000-0000-0000-0000-000000000001'
const worldId = 'pva-relay-e2e'
const writerInstanceId = 'b9510000-0000-0000-0000-000000000001'
const sourcePostSha256 = 'a'.repeat(64)

function u16(value: number): Buffer {
  const bytes = Buffer.alloc(2)
  bytes.writeUInt16BE(value)
  return bytes
}

function u32(value: number): Buffer {
  const bytes = Buffer.alloc(4)
  bytes.writeUInt32BE(value)
  return bytes
}

/** Test-created canonical PlayerSnapshotV1 CDTO; no checked-in fixture is read. */
function payload(): Buffer {
  const types = [9, 9, 9, 9, 9, 9, 1, 5, 5, 5, 5, 6, 5, 5, 5, 5, 5, 6, 6, 6, 6, 5, 5, 8, 8, 6, 6, 6, 6, 9, 9, 9, 9, 9, 5, 9, 6, 9, 9]
  const lengths = [80, 80, 80, 20, 20, 20, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 2, 2, 2, 2, 1, 1, 8, 8, 2, 2, 2, 2, 40, 32, 16, 8, 16, 1, 20, 2, 100, 810]
  const graphBody = Buffer.concat([u16(1), Buffer.from([3]), u32(4), u32(0)])
  const graph = seal(graphBody, 6)
  const fields = lengths.map((length, index) => Buffer.concat([
    u16(index + 1), Buffer.from([types[index]!]), u32(length), index === 6 ? Buffer.from([42]) : Buffer.alloc(length),
  ]))
  fields.push(Buffer.concat([u16(40), Buffer.from([9]), u32(graph.length), graph]))
  return seal(Buffer.concat(fields), 7)
}

function seal(body: Uint8Array, kind: number): Buffer {
  return Buffer.concat([
    Buffer.from('MUHCDTO\0', 'ascii'), Buffer.from([0, 1, 0, kind]), u32(body.length), Buffer.from(body),
    createHash('sha256').update(body).digest(),
  ])
}

/** An envelope-valid kind-7 CDTO that is schema-invalid because field 40 is absent. */
function schemaInvalidPayload(valid: Uint8Array): Buffer {
  const body = Buffer.from(valid.subarray(16, valid.length - 32))
  let cursor = 0
  while (cursor < body.length) {
    const fieldLength = body.readUInt32BE(cursor + 3)
    const fieldEnd = cursor + 7 + fieldLength
    if (body.readUInt16BE(cursor) === 40) return seal(body.subarray(0, cursor), 7)
    cursor = fieldEnd
  }
  throw new Error('test fixture is missing outer field 40')
}

function receipt(): Uint8Array {
  return Buffer.from([
    'version=1', `world_id=${worldId}`, `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4532656865726f', `request_sha256=${requestSha256}`,
    `post_sha256=${sourcePostSha256}`, `writer_instance_id=${writerInstanceId}`,
    'snapshot_format=legacy-file-manifest-v1', 'writer_epoch=1', 'writer_revision=1',
    'storage_format=1', 'snapshot_octets=9', '',
  ].join('\n'), 'ascii')
}

function artifact(value: Uint8Array): Uint8Array {
  return Buffer.concat([Buffer.from([
    'version=1', `world_id=${worldId}`, `character_id=${characterId}`, `command_id=${commandId}`,
    'canonical_name_hex=4532656865726f', `request_sha256=${requestSha256}`,
    `source_post_sha256=${sourcePostSha256}`, `writer_instance_id=${writerInstanceId}`,
    'writer_epoch=1', 'writer_revision=1', 'storage_format=1',
    'snapshot_format=player-snapshot-v1', 'source_octets=9',
    `snapshot_sha256=${createHash('sha256').update(value).digest('hex')}`,
    `snapshot_octets=${value.length}`, '', '',
  ].join('\n'), 'ascii'), Buffer.from(value)])
}

interface EvidenceState {
  names: string[]
  outboxMode: string
  receiptMode: string
  artifactMode: string
  receiptMtimeNs: string
  artifactMtimeNs: string
  receiptCtimeNs: string
  artifactCtimeNs: string
  receipt: string
  artifact: string
}

interface CreateOutboxOptions {
  beforeArtifactWrite?: (outboxPath: string) => Promise<void>
}

interface OwnedTestResources {
  outboxPath?: string
  store?: Pick<PostgresPlayerSnapshotV1ArtifactStore, 'close'>
  projectionStore?: Pick<PostgresPlayerSnapshotV1LevelProjectionStore, 'close'>
  superClient?: Pick<SqlClient, 'end'>
}

async function evidenceState(outboxPath: string): Promise<EvidenceState> {
  const receiptPath = join(outboxPath, `${commandId}.manifest`)
  const artifactPath = join(outboxPath, `${commandId}.player-snapshot-v1`)
  const [outbox, receiptFile, artifactFile, receiptBytes, artifactBytes, names] = await Promise.all([
    stat(outboxPath, { bigint: true }), stat(receiptPath, { bigint: true }), stat(artifactPath, { bigint: true }),
    readFile(receiptPath), readFile(artifactPath), readdir(outboxPath),
  ])
  return {
    names: names.sort(), outboxMode: (outbox.mode & 0o777n).toString(8),
    receiptMode: (receiptFile.mode & 0o777n).toString(8), artifactMode: (artifactFile.mode & 0o777n).toString(8),
    receiptMtimeNs: receiptFile.mtimeNs.toString(), artifactMtimeNs: artifactFile.mtimeNs.toString(),
    receiptCtimeNs: receiptFile.ctimeNs.toString(), artifactCtimeNs: artifactFile.ctimeNs.toString(),
    receipt: receiptBytes.toString('hex'), artifact: artifactBytes.toString('hex'),
  }
}

async function createOutbox(receiptBytes: Uint8Array, artifactBytes: Uint8Array, options: CreateOutboxOptions = {}): Promise<string> {
  let outboxPath: string | undefined
  try {
    outboxPath = await mkdtemp(join(tmpdir(), 'pva-relay-e2e-'))
    const receiptPath = join(outboxPath, `${commandId}.manifest`)
    const artifactPath = join(outboxPath, `${commandId}.player-snapshot-v1`)
    await chmod(outboxPath, 0o700)
    await writeFile(receiptPath, receiptBytes, { flag: 'wx', mode: 0o600 })
    await options.beforeArtifactWrite?.(outboxPath)
    await writeFile(artifactPath, artifactBytes, { flag: 'wx', mode: 0o600 })
    await Promise.all([chmod(receiptPath, 0o600), chmod(artifactPath, 0o600)])
    assert.deepEqual((await evidenceState(outboxPath)).names, [`${commandId}.manifest`, `${commandId}.player-snapshot-v1`])
    assert.equal((await evidenceState(outboxPath)).outboxMode, '700')
    assert.equal((await evidenceState(outboxPath)).receiptMode, '600')
    assert.equal((await evidenceState(outboxPath)).artifactMode, '600')
    return outboxPath
  } catch (error) {
    if (outboxPath) await rm(outboxPath, { recursive: true, force: true })
    throw error
  }
}

async function assertCreateOutboxFailureRemovesPartialDirectory(): Promise<void> {
  let partialOutboxPath: string | undefined
  await assert.rejects(
    createOutbox(receipt(), artifact(payload()), {
      beforeArtifactWrite: async (outboxPath: string): Promise<void> => {
        partialOutboxPath = outboxPath
        throw new Error('simulate outbox setup failure')
      },
    }),
    /simulate outbox setup failure/,
  )
  assert.ok(partialOutboxPath, 'the failure path must have created its owned outbox')
  await assert.rejects(stat(partialOutboxPath), { code: 'ENOENT' }, 'a failed outbox setup must remove its partial directory')
}

async function closeOwnedTestResources(resources: OwnedTestResources): Promise<void> {
  const results = await Promise.allSettled([
    resources.store?.close(),
    resources.projectionStore?.close(),
    resources.superClient?.end(),
    resources.outboxPath ? rm(resources.outboxPath, { recursive: true, force: true }) : undefined,
  ])
  const failed = results.find((result): result is PromiseRejectedResult => result.status === 'rejected')
  if (failed) throw failed.reason
}

async function withOwnedTestResources<T>(setup: (resources: OwnedTestResources) => Promise<T>): Promise<T> {
  const resources: OwnedTestResources = {}
  let setupFailed = false
  try {
    return await setup(resources)
  } catch (error) {
    setupFailed = true
    throw error
  } finally {
    try {
      await closeOwnedTestResources(resources)
    } catch (cleanupError) {
      if (!setupFailed) throw cleanupError
    }
  }
}

async function assertSetupFailureCleansOwnedResources(): Promise<void> {
  let outboxPath: string | undefined
  let storeClosed = false
  let projectionStoreClosed = false
  let superClientEnded = false
  await assert.rejects(
    withOwnedTestResources(async (resources) => {
      const createdOutboxPath = await createOutbox(receipt(), artifact(payload()))
      resources.outboxPath = createdOutboxPath
      outboxPath = createdOutboxPath
      resources.store = { close: async () => { storeClosed = true } }
      resources.projectionStore = { close: async () => { projectionStoreClosed = true } }
      resources.superClient = { end: async () => { superClientEnded = true } }
      throw new Error('simulate resource setup failure')
    }),
    /simulate resource setup failure/,
  )
  assert.equal(storeClosed, true, 'a setup failure must close an initialized store')
  assert.equal(projectionStoreClosed, true, 'a setup failure must close an initialized projection store')
  assert.equal(superClientEnded, true, 'a setup failure must end an initialized PostgreSQL client')
  assert.ok(outboxPath, 'the setup failure must own an outbox before failing')
  await assert.rejects(stat(outboxPath), { code: 'ENOENT' }, 'a setup failure must remove its owned outbox')
}

async function authorityState(client: SqlClient): Promise<string> {
  const result = await client.query<{ state: string }>(`
    select jsonb_build_object(
      'artifacts', coalesce((select jsonb_agg(to_jsonb(a) order by a.command_id)
        from private.game_character_player_snapshot_v1_artifacts a where a.character_id = $1::uuid), '[]'::jsonb),
      'receipts', coalesce((select jsonb_agg(to_jsonb(r) order by r.command_id)
        from private.game_character_shadow_receipts r where r.character_id = $1::uuid), '[]'::jsonb),
      'head', (select to_jsonb(h) from private.game_character_legacy_heads h where h.character_id = $1::uuid),
      'legacy_snapshot', coalesce((select jsonb_agg(to_jsonb(s) order by s.revision)
        from private.game_character_snapshots s where s.character_id = $1::uuid), '[]'::jsonb)
    )::text as state
  `, [characterId])
  return result.rows[0]!.state
}

async function legacyAuthorityState(client: SqlClient): Promise<string> {
  const result = await client.query<{ state: string }>(`
    select jsonb_build_object(
      'receipts', coalesce((select jsonb_agg(to_jsonb(r) order by r.command_id)
        from private.game_character_shadow_receipts r where r.character_id = $1::uuid), '[]'::jsonb),
      'head', (select to_jsonb(h) from private.game_character_legacy_heads h where h.character_id = $1::uuid),
      'legacy_snapshot', coalesce((select jsonb_agg(to_jsonb(s) order by s.revision)
        from private.game_character_snapshots s where s.character_id = $1::uuid), '[]'::jsonb)
    )::text as state
  `, [characterId])
  return result.rows[0]!.state
}

interface ProjectionRow {
  character_id: string
  command_id: string
  receipt_request_sha256: string
  source_post_sha256: string
  source_octets: string
  snapshot_sha256: string
  snapshot_octets: string
  raw_level_u8: string
}

async function projectionRow(client: SqlClient): Promise<ProjectionRow> {
  const result = await client.query<ProjectionRow>(`
    select character_id::text, command_id::text, receipt_request_sha256,
      source_post_sha256, source_octets::text, snapshot_sha256,
      snapshot_octets::text, raw_level_u8::text
    from private.game_character_player_snapshot_v1_level_projections
    where character_id = $1::uuid and command_id = $2::uuid
  `, [characterId, commandId])
  assert.equal(result.rows.length, 1, 'the relay must persist one receipt-bound level projection')
  return result.rows[0]!
}

async function main(): Promise<void> {
  assert.equal(process.platform, 'linux', 'the descriptor-rooted filesystem E2E must run in Linux')
  await assertCreateOutboxFailureRemovesPartialDirectory()
  await assertSetupFailureCleansOwnedResources()
  await withOwnedTestResources(async (resources) => {
    const validPayload = payload()
    const malformedPayload = schemaInvalidPayload(validPayload)
    const receiptBytes = receipt()
    const outboxPath = await createOutbox(receiptBytes, artifact(validPayload))
    resources.outboxPath = outboxPath
    const artifactPath = join(outboxPath, `${commandId}.player-snapshot-v1`)
    const filesystem = new NodePlayerSnapshotV1ArtifactFilesystem()
    const store = new PostgresPlayerSnapshotV1ArtifactStore(databaseUrl)
    resources.store = store
    const projectionStore = new PostgresPlayerSnapshotV1LevelProjectionStore(databaseUrl)
    resources.projectionStore = projectionStore
    const superClient = new (require('pg') as PgModule).Client({ connectionString: superDatabaseUrl })
    resources.superClient = superClient
    await superClient.connect()
    const initialFiles = await evidenceState(outboxPath)
    const legacyBefore = await legacyAuthorityState(superClient)
    assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce(outboxPath, store, filesystem, undefined, projectionStore), {
      visited: 1, valid: 1, delivered: 1, recorded: 1, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
      projectionDelivered: 1, projectionRecorded: 1, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
    })
    assert.deepEqual(await evidenceState(outboxPath), initialFiles, 'the first relay must not alter test-owned receipt or artifact files')
    assert.equal(await legacyAuthorityState(superClient), legacyBefore, 'the first relay must not alter legacy authority')
    const expectedProjection: ProjectionRow = {
      character_id: characterId,
      command_id: commandId,
      receipt_request_sha256: requestSha256,
      source_post_sha256: sourcePostSha256,
      source_octets: '9',
      snapshot_sha256: createHash('sha256').update(validPayload).digest('hex'),
      snapshot_octets: String(validPayload.length),
      raw_level_u8: '42',
    }
    const persistedProjection = await projectionRow(superClient)
    assert.deepEqual(persistedProjection, expectedProjection, 'the projection must preserve the field-7 raw U8 and receipt/source/snapshot bindings')
    await assert.rejects(
      superClient.query(
        'update private.game_character_player_snapshot_v1_level_projections set raw_level_u8 = 1 where character_id = $1::uuid and command_id = $2::uuid',
        [characterId, commandId],
      ),
      (error: { code?: unknown }) => error.code === 'P0001',
      'a persisted projection must reject UPDATE with P0001',
    )
    assert.deepEqual(await projectionRow(superClient), persistedProjection, 'a rejected projection UPDATE must leave the row unchanged')
    assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce(outboxPath, store, filesystem, undefined, projectionStore), {
      visited: 1, valid: 1, delivered: 1, recorded: 0, exactRetry: 1, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
      projectionDelivered: 1, projectionRecorded: 0, projectionExactRetry: 1, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
    })
    assert.deepEqual(await evidenceState(outboxPath), initialFiles, 'the exact retry must not alter test-owned receipt or artifact files')
    assert.equal(await legacyAuthorityState(superClient), legacyBefore, 'the exact retry must not alter legacy authority')
    const throwingProjectionStore: PlayerSnapshotV1LevelProjectionStore = {
      async recordPlayerSnapshotV1LevelProjection(): Promise<'RECORDED'> {
        throw Object.assign(new Error('simulate projection failure after immutable artifact evidence'), { code: 'P0001' })
      },
    }
    assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce(outboxPath, store, filesystem, undefined, throwingProjectionStore), {
      visited: 1, valid: 1, delivered: 1, recorded: 0, exactRetry: 1, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
      projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 1, projectionRetryable: 0, projectionUnknown: 0,
    })
    assert.deepEqual(await evidenceState(outboxPath), initialFiles, 'a projection failure must not alter test-owned receipt or artifact files')
    assert.equal(await legacyAuthorityState(superClient), legacyBefore, 'a projection failure must not alter legacy authority')
    assert.deepEqual(await projectionRow(superClient), persistedProjection, 'a projection failure must not alter immutable projection evidence')
    assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce(outboxPath, store, filesystem, undefined, projectionStore), {
      visited: 1, valid: 1, delivered: 1, recorded: 0, exactRetry: 1, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
      projectionDelivered: 1, projectionRecorded: 0, projectionExactRetry: 1, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
    })
    assert.deepEqual(await evidenceState(outboxPath), initialFiles, 'the post-failure exact retry must not alter test-owned receipt or artifact files')
    assert.equal(await legacyAuthorityState(superClient), legacyBefore, 'the post-failure exact retry must not alter legacy authority')
    assert.deepEqual(await projectionRow(superClient), persistedProjection, 'the post-failure real projection retry must remain exact')
    const before = await authorityState(superClient)
    await writeFile(artifactPath, artifact(malformedPayload))
    await chmod(artifactPath, 0o600)
    const malformedFiles = await evidenceState(outboxPath)
    assert.deepEqual(await relayPlayerSnapshotV1ArtifactsOnce(outboxPath, store, filesystem, undefined, projectionStore), {
      visited: 1, valid: 1, delivered: 0, recorded: 0, exactRetry: 0, invalid: 1, conflict: 0, retryable: 0, unknown: 0, ioError: 0, replayObserved: 0, replayDisabled: 1,
      projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0, projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
    })
    assert.deepEqual(await evidenceState(outboxPath), malformedFiles, 'the rejected relay must not alter test-owned receipt or artifact files')
    assert.equal(await authorityState(superClient), before, 'malformed CDTO must not change artifact, receipt, head, or legacy snapshot bytes')
    const reconciliation = await superClient.query<{ artifact_state: string }>(
      'select artifact_state from private.list_player_snapshot_v1_artifact_reconciliation($1::text, 10)', [worldId],
    )
    assert.deepEqual(reconciliation.rows.map((row) => row.artifact_state), ['EXACT'])
    const count = await superClient.query<{ count: string }>(
      'select count(*)::text as count from private.game_character_player_snapshot_v1_artifacts where character_id = $1::uuid', [characterId],
    )
    assert.equal(count.rows[0]!.count, '1')
    console.log('GREEN PostgreSQL 17 Linux: descriptor-rooted PlayerSnapshotV1 relay recorded immutable level-42 projection, exactly retried artifact/projection, and preserved files plus legacy authority')
  })
}

await main()
