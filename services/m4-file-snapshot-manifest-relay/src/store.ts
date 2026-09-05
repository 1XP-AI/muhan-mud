import { createRequire } from 'node:module'
import type { Manifest } from './manifest.js'
import type { PlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import type { PlayerSnapshotV1ReplayArtifactDifferentialReader, PlayerSnapshotV1ReplayArtifactEvidence } from './player-snapshot-v1-replay-differential.js'

export type StoreOutcome = 'RECORDED' | 'EXACT_RETRY'

export interface ManifestStore {
  recordManifest(manifest: Manifest): Promise<StoreOutcome>
  close?(): Promise<void>
}

export interface PlayerSnapshotV1ArtifactStore {
  recordPlayerSnapshotV1Artifact(artifact: PlayerSnapshotV1Artifact): Promise<StoreOutcome>
  close?(): Promise<void>
}

/**
 * Raw-U8 source metadata already validated by the PlayerSnapshotV1 artifact
 * parser. This is a best-effort, non-authoritative projection input only.
 */
export interface PlayerSnapshotV1LevelProjectionInput {
  characterId: string
  commandId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  sourceOctets: string
}

export interface PlayerSnapshotV1LevelProjectionStore {
  recordPlayerSnapshotV1LevelProjection(input: PlayerSnapshotV1LevelProjectionInput): Promise<StoreOutcome>
  close?(): Promise<void>
}

export type DatabaseErrorCategory = 'invalid' | 'conflict' | 'retryable' | 'unknown'

function errorCode(error: unknown): string | undefined {
  if (typeof error !== 'object' || error === null || !('code' in error)) return undefined
  const code = (error as { code?: unknown }).code
  return typeof code === 'string' ? code : undefined
}

export function classifyDatabaseError(error: unknown): DatabaseErrorCategory {
  const code = errorCode(error)
  if (code === '22023') return 'invalid'
  if (code === 'P0001') return 'conflict'
  if (code?.startsWith('08')) return 'retryable'
  if (code === 'ETIMEDOUT' || code === 'ECONNRESET' || code === 'EPIPE' || code === 'ECONNREFUSED') return 'retryable'
  return 'unknown'
}

interface QueryResult<Row> { rows: Row[] }
export interface PgClient {
  query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<QueryResult<Row>>
  release(): void
}
export interface PgPool { connect(): Promise<PgClient>; end(): Promise<void> }
interface PgModule { Pool: new (options: { connectionString: string, max: number }) => PgPool }

const require = createRequire(import.meta.url)

/** Direct PostgreSQL adapter. It performs no retries and never writes anything except through the M4 function. */
export class PostgresManifestStore implements ManifestStore {
  private readonly pool: PgPool

  constructor(databaseUrl: string, pool?: PgPool) {
    const validatedUrl = assertDatabaseUrl(databaseUrl)
    this.pool = pool ?? new (require('pg') as PgModule).Pool({ connectionString: validatedUrl, max: 1 })
  }

  async recordManifest(manifest: Manifest): Promise<StoreOutcome> {
    const client = await this.pool.connect()
    try {
      // The URL must authenticate as mud_writer_login; this explicit SET ROLE is part of the DB contract.
      await client.query('set role mud_writer')
      const result = await client.query<{ outcome: string }>(
        `select outcome from private.record_m4_file_snapshot_manifest_for_receipt($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::bigint)`,
        [manifest.characterId, manifest.commandId, manifest.requestSha256, manifest.snapshotFormat, manifest.postSha256, manifest.snapshotOctets],
      )
      const outcome = result.rows[0]?.outcome
      if (outcome !== 'RECORDED' && outcome !== 'EXACT_RETRY') throw new Error('unexpected database outcome')
      return outcome
    } finally {
      client.release()
    }
  }

  async close(): Promise<void> { await this.pool.end() }
}

/** Direct PostgreSQL adapter for immutable PlayerSnapshotV1 evidence only. */
export class PostgresPlayerSnapshotV1ArtifactStore implements PlayerSnapshotV1ArtifactStore {
  private readonly pool: PgPool

  constructor(databaseUrl: string, pool?: PgPool) {
    const validatedUrl = assertDatabaseUrl(databaseUrl)
    this.pool = pool ?? new (require('pg') as PgModule).Pool({ connectionString: validatedUrl, max: 1 })
  }

  async recordPlayerSnapshotV1Artifact(artifact: PlayerSnapshotV1Artifact): Promise<StoreOutcome> {
    const client = await this.pool.connect()
    try {
      await client.query('set role mud_writer')
      const result = await client.query<{ outcome: string }>(
        'select outcome from private.record_player_snapshot_v1_artifact_for_receipt($1::uuid, $2::uuid, $3::text, $4::text, $5::bigint, $6::text, $7::text, $8::bigint, $9::bytea)',
        [artifact.characterId, artifact.commandId, artifact.receiptRequestSha256, artifact.sourcePostSha256,
          artifact.sourceOctets, artifact.snapshotFormat, artifact.snapshotSha256, artifact.snapshotOctets, artifact.payload],
      )
      const outcome = result.rows[0]?.outcome
      if (outcome !== 'RECORDED' && outcome !== 'EXACT_RETRY') throw new Error('unexpected database outcome')
      return outcome
    } finally { client.release() }
  }

  async close(): Promise<void> { await this.pool.end() }
}

/**
 * Direct PostgreSQL adapter for the migration-190 raw-U8 level projection.
 * It is deliberately separate from immutable artifact authority and performs
 * no policy decision beyond recording the database function's outcome.
 */
export class PostgresPlayerSnapshotV1LevelProjectionStore implements PlayerSnapshotV1LevelProjectionStore {
  private readonly pool: PgPool

  constructor(databaseUrl: string, pool?: PgPool) {
    const validatedUrl = assertDatabaseUrl(databaseUrl)
    this.pool = pool ?? new (require('pg') as PgModule).Pool({ connectionString: validatedUrl, max: 1 })
  }

  async recordPlayerSnapshotV1LevelProjection(input: PlayerSnapshotV1LevelProjectionInput): Promise<StoreOutcome> {
    const client = await this.pool.connect()
    try {
      await client.query('set role mud_writer')
      const result = await client.query<{ outcome: string }>(
        'select outcome from private.record_player_snapshot_v1_level_projection_for_receipt($1::uuid, $2::uuid, $3::text, $4::text, $5::bigint)',
        [input.characterId, input.commandId, input.receiptRequestSha256, input.sourcePostSha256, input.sourceOctets],
      )
      const outcome = result.rows[0]?.outcome
      if (outcome !== 'RECORDED' && outcome !== 'EXACT_RETRY') throw new Error('unexpected database outcome')
      return outcome
    } finally { client.release() }
  }

  async close(): Promise<void> { await this.pool.end() }
}

/**
 * Dedicated read boundary for replay reconciliation.  It intentionally does
 * not share a relay store, set a role, or expose any mutation operation.
 */
export class PostgresPlayerSnapshotV1ArtifactDifferentialReader implements PlayerSnapshotV1ReplayArtifactDifferentialReader {
  private readonly pool: PgPool

  constructor(databaseUrl: string, pool?: PgPool) {
    const validatedUrl = assertReplayDifferentialDatabaseUrl(databaseUrl)
    this.pool = pool ?? new (require('pg') as PgModule).Pool({ connectionString: validatedUrl, max: 1 })
  }

  async findByCommandId(commandId: string): Promise<readonly PlayerSnapshotV1ReplayArtifactEvidence[]> {
    const client = await this.pool.connect()
    try {
      await assertReplayDifferentialConnectionContract(client)
      const result = await client.query<Record<string, unknown>>(
        `select command_id::text as "commandId", character_id::text as "characterId",
          receipt_request_sha256 as "receiptRequestSha256", source_post_sha256 as "sourcePostSha256",
          snapshot_format as "snapshotFormat", snapshot_sha256 as "snapshotSha256",
          snapshot_octets::text as "snapshotOctets"
         from private.game_character_player_snapshot_v1_artifacts
         where command_id = $1::uuid
         order by character_id`,
        [commandId],
      )
      return result.rows.map(parseReplayDifferentialArtifactEvidence)
    } finally { client.release() }
  }

  async close(): Promise<void> { await this.pool.end() }
}

interface ReplayDifferentialConnectionCheck {
  currentUser: unknown
  sessionUser: unknown
  defaultTransactionReadOnly: unknown
  transactionReadOnly: unknown
  canInsert: unknown
  canUpdate: unknown
  canDelete: unknown
  canTruncate: unknown
  canReferences: unknown
  canTrigger: unknown
}

/**
 * The URL starts each session read-only; this SELECT-only check rejects a
 * different login, changed role/state, or any artifact mutation privilege.
 */
async function assertReplayDifferentialConnectionContract(client: PgClient): Promise<void> {
  const result = await client.query<ReplayDifferentialConnectionCheck>(
    `select current_user as "currentUser", session_user as "sessionUser",
      current_setting('default_transaction_read_only', true) as "defaultTransactionReadOnly",
      current_setting('transaction_read_only', true) as "transactionReadOnly",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'INSERT') as "canInsert",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'UPDATE') as "canUpdate",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'DELETE') as "canDelete",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'TRUNCATE') as "canTruncate",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'REFERENCES') as "canReferences",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'TRIGGER') as "canTrigger"`,
  )
  const check = result.rows[0]
  if (!check
    || check.currentUser !== 'mud_replay_reader_login' || check.sessionUser !== 'mud_replay_reader_login'
    || check.defaultTransactionReadOnly !== 'on' || check.transactionReadOnly !== 'on'
    || check.canInsert !== false || check.canUpdate !== false || check.canDelete !== false
    || check.canTruncate !== false || check.canReferences !== false || check.canTrigger !== false) {
    throw new Error('invalid replay differential database connection')
  }
}

function parseReplayDifferentialArtifactEvidence(value: Record<string, unknown>): PlayerSnapshotV1ReplayArtifactEvidence {
  const octets = typeof value.snapshotOctets === 'string' && /^\d+$/.test(value.snapshotOctets) ? Number(value.snapshotOctets) : NaN
  if (typeof value.commandId !== 'string' || typeof value.characterId !== 'string'
    || typeof value.receiptRequestSha256 !== 'string' || typeof value.sourcePostSha256 !== 'string'
    || typeof value.snapshotFormat !== 'string' || typeof value.snapshotSha256 !== 'string'
    || !Number.isSafeInteger(octets) || octets < 0) throw new Error('invalid replay differential database result')
  return {
    commandId: value.commandId, characterId: value.characterId,
    receiptRequestSha256: value.receiptRequestSha256, sourcePostSha256: value.sourcePostSha256,
    snapshotFormat: value.snapshotFormat, snapshotSha256: value.snapshotSha256, snapshotOctets: octets,
  }
}

/** Reject REST/Supabase credentials and require the dedicated direct-DB login. */
export function assertDatabaseUrl(value: string | undefined): string {
  if (!value) throw new Error('invalid relay configuration')
  let url: URL
  try { url = new URL(value) } catch { throw new Error('invalid relay configuration') }
  if ((url.protocol !== 'postgres:' && url.protocol !== 'postgresql:')
    || !url.hostname || url.username !== 'mud_writer_login'
    || /service_role|anon|authenticated/i.test(url.username)
    || /service_role|apikey|authorization|access_token|jwt/i.test(url.search)
    || /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/.test(value)) throw new Error('invalid relay configuration')
  return value
}

/** A comparison connection must use the dedicated login and start read-only. */
export function assertReplayDifferentialDatabaseUrl(value: string | undefined): string {
  if (!value) throw new Error('invalid replay differential configuration')
  let url: URL
  try { url = new URL(value) } catch { throw new Error('invalid replay differential configuration') }
  const options = url.searchParams.getAll('options')
  if ((url.protocol !== 'postgres:' && url.protocol !== 'postgresql:') || !url.hostname
    || url.username !== 'mud_replay_reader_login'
    || options.length !== 1 || options[0]!.trim() !== '-c default_transaction_read_only=on'
    || /service_role|anon|authenticated/i.test(url.username)
    || /service_role|apikey|authorization|access_token|jwt/i.test(url.search)
    || /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/.test(value)) throw new Error('invalid replay differential configuration')
  return value
}
