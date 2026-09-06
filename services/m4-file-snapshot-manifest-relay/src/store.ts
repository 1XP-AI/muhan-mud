import { createRequire } from 'node:module'
import type { Manifest } from './manifest.js'
import type { PlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import type { PlayerSnapshotV1ReplayArtifactDifferentialReader, PlayerSnapshotV1ReplayArtifactEvidence } from './player-snapshot-v1-replay-differential.js'
import type { ImmutablePlayerSnapshotLevelProjectionEvidence, ImmutablePlayerSnapshotLevelProjectionReader } from './player-snapshot-v1-level-comparator.js'
import type { ImmutablePlayerSnapshotV1FullPayloadEvidence, ImmutablePlayerSnapshotV1FullPayloadReader } from './player-snapshot-v1-full-payload-rehearsal.js'

export type StoreOutcome = 'RECORDED' | 'EXACT_RETRY'

export interface ManifestStore {
  recordManifest(manifest: Manifest): Promise<StoreOutcome>
  close?(): Promise<void>
}

export interface PlayerSnapshotV1ArtifactStore {
  recordPlayerSnapshotV1Artifact(artifact: PlayerSnapshotV1Artifact): Promise<StoreOutcome>
  close?(): Promise<void>
}

export type PlayerSnapshotV1ArtifactFulfillmentOutcome =
  | 'FULFILLED'
  | 'EXACT_RETRY'
  | 'ALREADY_FULFILLED'
  | 'NOT_ELIGIBLE'

/**
 * The relay supplies only the immutable artifact's character and command
 * identities. The database resolves correlation, actor, and mode itself.
 */
export interface PlayerSnapshotV1ArtifactFulfillmentStore {
  fulfillGameCharacterOnboardingSnapshotEligibility(
    characterId: string,
    artifactCommandId: string,
  ): Promise<PlayerSnapshotV1ArtifactFulfillmentOutcome>
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
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const SHA256_RE = /^[0-9a-f]{64}$/

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
export class PostgresPlayerSnapshotV1ArtifactStore implements PlayerSnapshotV1ArtifactStore, PlayerSnapshotV1ArtifactFulfillmentStore {
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

  async fulfillGameCharacterOnboardingSnapshotEligibility(
    characterId: string,
    artifactCommandId: string,
  ): Promise<PlayerSnapshotV1ArtifactFulfillmentOutcome> {
    const client = await this.pool.connect()
    try {
      await client.query('set role mud_writer')
      const result = await client.query<{ outcome: string }>(
        'select outcome from private.fulfill_game_character_onboarding_snapshot_eligibility($1::uuid, $2::uuid)',
        [characterId, artifactCommandId],
      )
      const outcome = result.rows[0]?.outcome
      if (outcome !== 'FULFILLED' && outcome !== 'EXACT_RETRY'
        && outcome !== 'ALREADY_FULFILLED' && outcome !== 'NOT_ELIGIBLE') {
        throw new Error('unexpected database fulfillment outcome')
      }
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

/**
 * Dedicated PostgreSQL read boundary for the pure v2 level comparator.  It
 * shares only the dedicated replay-reader login and read-only contract; it
 * exposes the closed level-projection metadata and no mutation operation.
 */
export class PostgresPlayerSnapshotV1LevelProjectionReader implements ImmutablePlayerSnapshotLevelProjectionReader {
  private readonly pool: PgPool

  constructor(databaseUrl: string, pool?: PgPool) {
    const validatedUrl = assertReplayDifferentialDatabaseUrl(databaseUrl)
    this.pool = pool ?? new (require('pg') as PgModule).Pool({ connectionString: validatedUrl, max: 1 })
  }

  async findByCommandId(commandId: string): Promise<readonly ImmutablePlayerSnapshotLevelProjectionEvidence[]> {
    const client = await this.pool.connect()
    try {
      await assertReplayLevelProjectionConnectionContract(client)
      const result = await client.query<Record<string, unknown>>(
        `select command_id::text as "commandId", character_id::text as "characterId",
          receipt_request_sha256 as "receiptRequestSha256", source_post_sha256 as "sourcePostSha256",
          snapshot_sha256 as "snapshotSha256", snapshot_octets::text as "snapshotOctets",
          raw_level_u8::text as "rawLevelU8"
         from private.game_character_player_snapshot_v1_level_projections
         where command_id = $1::uuid
         order by character_id`,
        [commandId],
      )
      return result.rows.map(parseReplayLevelProjectionEvidence)
    } finally { client.release() }
  }

  async close(): Promise<void> { await this.pool.end() }
}

/**
 * Explicit full-payload rehearsal reader. It is intentionally a separate
 * login from the metadata differential reader, has no write API, and is not
 * constructed by a relay or default CLI.
 */
export class PostgresPlayerSnapshotV1FullPayloadRehearsalReader implements ImmutablePlayerSnapshotV1FullPayloadReader {
  private readonly pool: PgPool

  constructor(databaseUrl: string, pool?: PgPool) {
    const validatedUrl = assertFullPayloadRehearsalDatabaseUrl(databaseUrl)
    this.pool = pool ?? new (require('pg') as PgModule).Pool({ connectionString: validatedUrl, max: 1 })
  }

  async findByCommandId(commandId: string): Promise<readonly ImmutablePlayerSnapshotV1FullPayloadEvidence[]> {
    const client = await this.pool.connect()
    try {
      await assertFullPayloadRehearsalConnectionContract(client)
      const result = await client.query<Record<string, unknown>>(
        `select a.character_id::text as "characterId", a.command_id::text as "commandId",
          a.world_id as "worldId", a.legacy_name_key as "legacyNameKey",
          a.receipt_request_sha256 as "receiptRequestSha256",
          a.writer_instance_id::text as "writerInstanceId", a.writer_epoch::text as "writerEpoch",
          a.writer_revision::text as "writerRevision", a.source_post_sha256 as "sourcePostSha256",
          a.source_octets::text as "sourceOctets", a.storage_format::text as "storageFormat",
          a.receipt_acknowledged_at::text as "receiptAcknowledgedAt",
          a.snapshot_format as "snapshotFormat", a.snapshot_sha256 as "snapshotSha256",
          a.snapshot_octets::text as "snapshotOctets", a.payload as "payload",
          json_build_object(
            'characterId', r.character_id::text, 'commandId', r.command_id::text,
            'worldId', r.world_id, 'legacyNameKey', r.legacy_name_key,
            'requestSha256', r.request_sha256, 'writerInstanceId', r.writer_instance_id::text,
            'writerEpoch', r.writer_epoch::text, 'writerRevision', r.writer_revision::text,
            'postSha256', r.post_sha256, 'storageFormat', r.storage_format::text,
            'acknowledgedAt', r.acknowledged_at::text
          ) as "receipt"
         from private.game_character_player_snapshot_v1_artifacts a
         join private.game_character_shadow_receipts r
           on r.character_id = a.character_id and r.command_id = a.command_id
         where a.command_id = $1::uuid
         order by a.character_id`,
        [commandId],
      )
      return result.rows.map(parseFullPayloadRehearsalEvidence)
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

/** Enforce the same dedicated reader contract against the level projection table. */
async function assertReplayLevelProjectionConnectionContract(client: PgClient): Promise<void> {
  const result = await client.query<ReplayDifferentialConnectionCheck>(
    `select current_user as "currentUser", session_user as "sessionUser",
      current_setting('default_transaction_read_only', true) as "defaultTransactionReadOnly",
      current_setting('transaction_read_only', true) as "transactionReadOnly",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_level_projections', 'INSERT') as "canInsert",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_level_projections', 'UPDATE') as "canUpdate",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_level_projections', 'DELETE') as "canDelete",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_level_projections', 'TRUNCATE') as "canTruncate",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_level_projections', 'REFERENCES') as "canReferences",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_level_projections', 'TRIGGER') as "canTrigger"`,
  )
  const check = result.rows[0]
  if (!check
    || check.currentUser !== 'mud_replay_reader_login' || check.sessionUser !== 'mud_replay_reader_login'
    || check.defaultTransactionReadOnly !== 'on' || check.transactionReadOnly !== 'on'
    || check.canInsert !== false || check.canUpdate !== false || check.canDelete !== false
    || check.canTruncate !== false || check.canReferences !== false || check.canTrigger !== false) {
    throw new Error('invalid replay level projection database connection')
  }
}

interface FullPayloadRehearsalConnectionCheck {
  currentUser: unknown
  sessionUser: unknown
  defaultTransactionReadOnly: unknown
  transactionReadOnly: unknown
  canArtifactInsert: unknown
  canArtifactUpdate: unknown
  canArtifactDelete: unknown
  canArtifactTruncate: unknown
  canArtifactReferences: unknown
  canArtifactTrigger: unknown
  canReceiptInsert: unknown
  canReceiptUpdate: unknown
  canReceiptDelete: unknown
  canReceiptTruncate: unknown
  canReceiptReferences: unknown
  canReceiptTrigger: unknown
}

/** The full payload is only readable through its distinct read-only login. */
async function assertFullPayloadRehearsalConnectionContract(client: PgClient): Promise<void> {
  const result = await client.query<FullPayloadRehearsalConnectionCheck>(
    `select current_user as "currentUser", session_user as "sessionUser",
      current_setting('default_transaction_read_only', true) as "defaultTransactionReadOnly",
      current_setting('transaction_read_only', true) as "transactionReadOnly",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'INSERT') as "canArtifactInsert",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'UPDATE') as "canArtifactUpdate",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'DELETE') as "canArtifactDelete",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'TRUNCATE') as "canArtifactTruncate",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'REFERENCES') as "canArtifactReferences",
      has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'TRIGGER') as "canArtifactTrigger",
      has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'INSERT') as "canReceiptInsert",
      has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'UPDATE') as "canReceiptUpdate",
      has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'DELETE') as "canReceiptDelete",
      has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'TRUNCATE') as "canReceiptTruncate",
      has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'REFERENCES') as "canReceiptReferences",
      has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'TRIGGER') as "canReceiptTrigger"`,
  )
  const check = result.rows[0]
  if (!check || check.currentUser !== 'mud_full_payload_rehearsal_reader_login'
    || check.sessionUser !== 'mud_full_payload_rehearsal_reader_login'
    || check.defaultTransactionReadOnly !== 'on' || check.transactionReadOnly !== 'on'
    || check.canArtifactInsert !== false || check.canArtifactUpdate !== false || check.canArtifactDelete !== false
    || check.canArtifactTruncate !== false || check.canArtifactReferences !== false || check.canArtifactTrigger !== false
    || check.canReceiptInsert !== false || check.canReceiptUpdate !== false || check.canReceiptDelete !== false
    || check.canReceiptTruncate !== false || check.canReceiptReferences !== false || check.canReceiptTrigger !== false) {
    throw new Error('invalid full payload rehearsal database connection')
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

function parseReplayLevelProjectionEvidence(value: Record<string, unknown>): ImmutablePlayerSnapshotLevelProjectionEvidence {
  const snapshotOctets = typeof value.snapshotOctets === 'string' && /^\d+$/.test(value.snapshotOctets) ? Number(value.snapshotOctets) : NaN
  const rawLevelU8 = typeof value.rawLevelU8 === 'string' && /^\d+$/.test(value.rawLevelU8) ? Number(value.rawLevelU8) : NaN
  if (typeof value.commandId !== 'string' || !UUID_RE.test(value.commandId)
    || typeof value.characterId !== 'string' || !UUID_RE.test(value.characterId)
    || typeof value.receiptRequestSha256 !== 'string' || !SHA256_RE.test(value.receiptRequestSha256)
    || typeof value.sourcePostSha256 !== 'string' || !SHA256_RE.test(value.sourcePostSha256)
    || typeof value.snapshotSha256 !== 'string' || !SHA256_RE.test(value.snapshotSha256)
    || !Number.isSafeInteger(snapshotOctets) || snapshotOctets < 0
    || !Number.isSafeInteger(rawLevelU8) || rawLevelU8 < 0 || rawLevelU8 > 255) {
    throw new Error('invalid replay level projection database result')
  }
  return {
    commandId: value.commandId, characterId: value.characterId,
    receiptRequestSha256: value.receiptRequestSha256, sourcePostSha256: value.sourcePostSha256,
    snapshotSha256: value.snapshotSha256, snapshotOctets, rawLevelU8,
  }
}

function parseFullPayloadRehearsalEvidence(value: Record<string, unknown>): ImmutablePlayerSnapshotV1FullPayloadEvidence {
  const snapshotOctets = typeof value.snapshotOctets === 'string' && /^\d+$/.test(value.snapshotOctets) ? Number(value.snapshotOctets) : NaN
  const payload = value.payload instanceof Uint8Array ? Buffer.from(value.payload) : undefined
  if (typeof value.characterId !== 'string' || !UUID_RE.test(value.characterId)
    || typeof value.commandId !== 'string' || !UUID_RE.test(value.commandId)
    || typeof value.worldId !== 'string' || !/^[a-z][a-z0-9_-]{0,63}$/.test(value.worldId)
    || typeof value.legacyNameKey !== 'string' || value.legacyNameKey.length < 1 || value.legacyNameKey.length > 14
    || typeof value.receiptRequestSha256 !== 'string' || !SHA256_RE.test(value.receiptRequestSha256)
    || typeof value.writerInstanceId !== 'string' || !UUID_RE.test(value.writerInstanceId)
    || !positiveDbInteger(value.writerEpoch) || !positiveDbInteger(value.writerRevision)
    || typeof value.sourcePostSha256 !== 'string' || !SHA256_RE.test(value.sourcePostSha256)
    || !positiveDbInteger(value.sourceOctets) || !positiveDbInteger(value.storageFormat)
    || BigInt(value.storageFormat) > 32_767n || typeof value.receiptAcknowledgedAt !== 'string' || !value.receiptAcknowledgedAt
    || value.snapshotFormat !== 'player-snapshot-v1' || typeof value.snapshotSha256 !== 'string' || !SHA256_RE.test(value.snapshotSha256)
    || !Number.isSafeInteger(snapshotOctets) || snapshotOctets < 48 || snapshotOctets > 4_194_352
    || !payload || payload.length !== snapshotOctets || !isFullPayloadReceipt(value.receipt)) {
    throw new Error('invalid full payload rehearsal database result')
  }
  return {
    characterId: value.characterId, commandId: value.commandId, worldId: value.worldId,
    legacyNameKey: value.legacyNameKey, receiptRequestSha256: value.receiptRequestSha256,
    writerInstanceId: value.writerInstanceId, writerEpoch: value.writerEpoch, writerRevision: value.writerRevision,
    sourcePostSha256: value.sourcePostSha256, sourceOctets: value.sourceOctets, storageFormat: value.storageFormat,
    receiptAcknowledgedAt: value.receiptAcknowledgedAt, snapshotFormat: 'player-snapshot-v1',
    snapshotSha256: value.snapshotSha256, snapshotOctets, payload, receipt: value.receipt,
  }
}

function positiveDbInteger(value: unknown): value is string {
  if (typeof value !== 'string' || !/^[1-9][0-9]{0,18}$/.test(value)) return false
  try { return BigInt(value) <= 9_223_372_036_854_775_807n } catch { return false }
}

function isFullPayloadReceipt(value: unknown): value is ImmutablePlayerSnapshotV1FullPayloadEvidence['receipt'] {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const receipt = value as Record<string, unknown>
  const keys = Object.keys(receipt).sort()
  return keys.length === 11 && keys.every((key, index) => key === [
    'acknowledgedAt', 'characterId', 'commandId', 'legacyNameKey', 'postSha256', 'requestSha256',
    'storageFormat', 'worldId', 'writerEpoch', 'writerInstanceId', 'writerRevision',
  ][index]) && typeof receipt.characterId === 'string' && UUID_RE.test(receipt.characterId)
    && typeof receipt.commandId === 'string' && UUID_RE.test(receipt.commandId)
    && typeof receipt.worldId === 'string' && /^[a-z][a-z0-9_-]{0,63}$/.test(receipt.worldId)
    && typeof receipt.legacyNameKey === 'string' && receipt.legacyNameKey.length >= 1 && receipt.legacyNameKey.length <= 14
    && typeof receipt.requestSha256 === 'string' && SHA256_RE.test(receipt.requestSha256)
    && typeof receipt.writerInstanceId === 'string' && UUID_RE.test(receipt.writerInstanceId)
    && positiveDbInteger(receipt.writerEpoch) && positiveDbInteger(receipt.writerRevision)
    && typeof receipt.postSha256 === 'string' && SHA256_RE.test(receipt.postSha256)
    && positiveDbInteger(receipt.storageFormat) && BigInt(receipt.storageFormat) <= 32_767n
    && typeof receipt.acknowledgedAt === 'string' && receipt.acknowledgedAt.length > 0
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

/** Full payload rehearsal has a distinct least-privilege direct reader login. */
export function assertFullPayloadRehearsalDatabaseUrl(value: string | undefined): string {
  if (!value) throw new Error('invalid full payload rehearsal configuration')
  let url: URL
  try { url = new URL(value) } catch { throw new Error('invalid full payload rehearsal configuration') }
  const options = url.searchParams.getAll('options')
  if ((url.protocol !== 'postgres:' && url.protocol !== 'postgresql:') || !url.hostname
    || url.username !== 'mud_full_payload_rehearsal_reader_login'
    || options.length !== 1 || options[0]!.trim() !== '-c default_transaction_read_only=on'
    || /service_role|anon|authenticated/i.test(url.username)
    || /service_role|apikey|authorization|access_token|jwt/i.test(url.search)
    || /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/.test(value)) throw new Error('invalid full payload rehearsal configuration')
  return value
}
