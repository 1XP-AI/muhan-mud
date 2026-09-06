import { createRequire } from 'node:module'
import type { BatchIdentity } from './batch-identity.js'
import type { BatchMemberIdentity, ExistingCharacter, ImportStore, ImportTransaction, InventoryRecord, LedgerBatch } from './inventory.js'

interface QueryResult<Row> { rows: Row[] }
interface PgClient { query<Row = Record<string, unknown>>(sql: string, values?: readonly unknown[]): Promise<QueryResult<Row>>, release(): void }
interface PgPool { connect(): Promise<PgClient>, end(): Promise<void> }
interface PgModule { Pool: new (options: { connectionString: string, max: number }) => PgPool }

interface CharacterRow {
  legacy_name: string
  legacy_name_key: string
  legacy_shard: string
  imported_file_sha256: string | null
  lifecycle: string
  owner_user_id: string | null
  storage_format: number
}

interface BatchRow {
  identity_key: string
  batch_sequence: string
  record_count: string
}

interface WatermarkRow { watermark_sequence: string }
interface InsertedCharacterRow { id: string }
interface BatchMemberRow {
  world_id: string
  stream_id: string
  batch_sequence: string
}
interface LegacyLocatorRow {
  canonical_legacy_name: string
  legacy_name_sha1: string
  legacy_shard: string
}

interface BatchMemberIdentityRow {
  canonical_name: string
  legacy_shard: string
  imported_file_sha256: string | null
  storage_format: number
}

const require = createRequire(import.meta.url)

const MAX_TRANSACTION_ATTEMPTS = 3
const SERIALIZATION_BACKOFF_MS = 25

function errorCode(error: unknown): string | undefined {
  return typeof error === 'object' && error !== null && 'code' in error
    ? String((error as { code?: unknown }).code)
    : undefined
}

/** PostgreSQL may invalidate a serializable snapshot after an advisory-lock wait. */
export function isRetryableTransactionError(error: unknown): boolean {
  const code = errorCode(error)
  return code === '40001'
}

interface RetryOptions {
  maxAttempts?: number
  sleep?: (milliseconds: number) => Promise<void>
}

/** Retry only whole transactions invalidated by PostgreSQL concurrency control. */
export async function withSerializationRetry<T>(work: () => Promise<T>, options: RetryOptions = {}): Promise<T> {
  const maxAttempts = options.maxAttempts ?? MAX_TRANSACTION_ATTEMPTS
  const sleep = options.sleep ?? ((milliseconds: number) => new Promise<void>((resolve) => setTimeout(resolve, milliseconds)))
  for (let attempt = 1; ; attempt++) {
    try {
      return await work()
    } catch (error) {
      if (!isRetryableTransactionError(error) || attempt >= maxAttempts) throw error
      await sleep(SERIALIZATION_BACKOFF_MS)
    }
  }
}

/** Reject HTTP/Supabase keys/JWTs; this CLI accepts only a direct PostgreSQL URL. */
export function assertAdminDatabaseUrl(value: string | undefined): string {
  if (!value) throw new Error('invalid import configuration')
  let url: URL
  try { url = new URL(value) } catch { throw new Error('invalid import configuration') }
  if ((url.protocol !== 'postgres:' && url.protocol !== 'postgresql:')
    || !url.hostname || !url.username || /service_role|anon|authenticated/i.test(url.username)
    || /service_role|apikey|authorization|access_token|jwt/i.test(url.search)
    || /eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/.test(value)) {
    throw new Error('invalid import configuration')
  }
  return value
}

export class PostgresImportStore implements ImportStore {
  private readonly pool: PgPool

  constructor(databaseUrl: string) {
    const pg = require('pg') as PgModule
    this.pool = new pg.Pool({ connectionString: assertAdminDatabaseUrl(databaseUrl), max: 1 })
  }

  async close(): Promise<void> { await this.pool.end() }

  async transaction<T>(work: (transaction: ImportTransaction) => Promise<T>): Promise<T> {
    return withSerializationRetry(async () => {
      // A retry must acquire a fresh client and snapshot; reusing a client
      // would retain the failed serializable transaction's view of the DB.
      const client = await this.pool.connect()
      try {
        await client.query('begin isolation level serializable')
        await client.query("set local lock_timeout = '5s'")
        await client.query("set local statement_timeout = '60s'")
        await client.query("set local idle_in_transaction_session_timeout = '60s'")
        const result = await work(new PostgresImportTransaction(client))
        await client.query('commit')
        return result
      } catch (error) {
        await client.query('rollback').catch(() => undefined)
        throw error
      } finally {
        client.release()
      }
    })
  }
}

class PostgresImportTransaction implements ImportTransaction {
  constructor(private readonly client: PgClient) {}

  async lockIdentity(worldId: string, legacyNameKey: string): Promise<void> {
    // JSON encoding creates an unambiguous stable key. A hash collision only serializes extra work.
    await this.client.query('select pg_advisory_xact_lock(hashtextextended($1, 0))', [JSON.stringify([worldId, legacyNameKey])])
  }

  async findCharacter(worldId: string, legacyNameKey: string): Promise<ExistingCharacter | undefined> {
    const result = await this.client.query<CharacterRow>(
      'select legacy_name, legacy_name_key, legacy_shard, imported_file_sha256, lifecycle::text, owner_user_id::text, storage_format from public.game_characters where world_id = $1 and legacy_name_key = $2 for update',
      [worldId, legacyNameKey],
    )
    const row = result.rows[0]
    return row && {
      legacyName: row.legacy_name,
      legacyNameKey: row.legacy_name_key,
      legacyShard: row.legacy_shard,
      importedFileSha256: row.imported_file_sha256,
      lifecycle: row.lifecycle,
      ownerUserId: row.owner_user_id,
      storageFormat: row.storage_format,
    }
  }

  async insertImportedUnclaimed(input: { worldId: string, record: InventoryRecord }): Promise<string> {
    const { record } = input
    const result = await this.client.query<InsertedCharacterRow>(
      `insert into public.game_characters (
        world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
        storage_format, imported_file_sha256, owner_user_id
      ) values ($1, $2, $3, $4, 'imported_unclaimed', 1, $5, null)
        returning id::text as id`,
      [input.worldId, record.name, record.canonicalNameKey, record.expectedShard, record.sha256],
    )
    const id = result.rows[0]?.id
    if (typeof id !== 'string' || id === '') throw new Error('character insert did not return an identifier')
    return id
  }

  async lockBatchStream(worldId: string, streamId: string): Promise<void> {
    // This serializes creation and progression of one durable stream. The
    // character locks remain separate so independent streams do not share a
    // broad world-level mutex.
    await this.client.query('select pg_advisory_xact_lock(hashtextextended($1, 0))', [JSON.stringify(['imported-unclaimed-batch', worldId, streamId])])
  }

  async findBatchBySequence(worldId: string, streamId: string, sequence: number): Promise<LedgerBatch | undefined> {
    const result = await this.client.query<BatchRow>(
      `select identity_key, batch_sequence::text, record_count::text
         from private.game_imported_unclaimed_batches
        where world_id = $1 and stream_id = $2 and batch_sequence = $3`,
      [worldId, streamId, sequence],
    )
    return this.batch(result.rows[0])
  }

  async findBatchByIdentity(worldId: string, streamId: string, stableKey: string): Promise<LedgerBatch | undefined> {
    const result = await this.client.query<BatchRow>(
      `select identity_key, batch_sequence::text, record_count::text
         from private.game_imported_unclaimed_batches
        where world_id = $1 and stream_id = $2 and identity_key = $3`,
      [worldId, streamId, stableKey],
    )
    return this.batch(result.rows[0])
  }

  async findBatchMemberIdentities(worldId: string, streamId: string, sequence: number): Promise<readonly BatchMemberIdentity[]> {
    const result = await this.client.query<BatchMemberIdentityRow>(
      `select canonical_legacy_name as canonical_name, legacy_shard,
              imported_file_sha256, storage_format
         from private.game_imported_unclaimed_batch_member_identities
        where world_id = $1 and stream_id = $2 and batch_sequence = $3`,
      [worldId, streamId, sequence],
    )
    return result.rows.map((row) => ({
      canonicalName: row.canonical_name,
      shard: row.legacy_shard,
      sha256: row.imported_file_sha256 ?? '',
      storageFormat: row.storage_format,
    }))
  }

  async createBatch(input: { identity: BatchIdentity, streamId: string, sequence: number, recordCount: number }): Promise<void> {
    const { identity } = input
    await this.client.query(
      `insert into private.game_imported_unclaimed_batches (
        world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
        source_sha256, source_byte_size, parser_version, abi, start_marker,
        end_marker, record_count
      ) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
      [identity.worldId, input.streamId, input.sequence, identity.stableKey,
        identity.sourceManifestId, identity.sourceSha256, identity.sourceByteSize,
        identity.parserVersion, identity.abi, identity.startMarker, identity.endMarker,
        input.recordCount],
    )
  }

  async recordBatchMemberIdentity(input: {
    worldId: string, streamId: string, sequence: number, identity: BatchMemberIdentity
  }): Promise<void> {
    const inserted = await this.client.query<BatchMemberIdentityRow>(
      `insert into private.game_imported_unclaimed_batch_member_identities (
        world_id, stream_id, batch_sequence, canonical_legacy_name,
        legacy_shard, imported_file_sha256, storage_format
      ) values ($1, $2, $3, $4, $5, $6, $7)
      on conflict (world_id, stream_id, batch_sequence, canonical_legacy_name) do nothing
      returning canonical_legacy_name as canonical_name, legacy_shard,
        imported_file_sha256, storage_format`,
      [input.worldId, input.streamId, input.sequence, input.identity.canonicalName,
        input.identity.shard, input.identity.sha256, input.identity.storageFormat],
    )
    if (inserted.rows.length === 1) return

    const existing = await this.client.query<BatchMemberIdentityRow>(
      `select canonical_legacy_name as canonical_name, legacy_shard,
              imported_file_sha256, storage_format
         from private.game_imported_unclaimed_batch_member_identities
        where world_id = $1 and stream_id = $2 and batch_sequence = $3
          and canonical_legacy_name = $4`,
      [input.worldId, input.streamId, input.sequence, input.identity.canonicalName],
    )
    const row = existing.rows[0]
    if (!row || row.canonical_name !== input.identity.canonicalName
      || row.legacy_shard !== input.identity.shard
      || row.imported_file_sha256 !== input.identity.sha256
      || row.storage_format !== input.identity.storageFormat) {
      throw new Error('batch member identity conflict')
    }
  }

  async recordBatchMember(input: {
    worldId: string, streamId: string, sequence: number, characterId: string
    legacyLocator?: { canonicalName: string, legacyNameSha1: string, legacyShard: string }
  }): Promise<void> {
    const inserted = await this.client.query<BatchMemberRow>(
      `insert into private.game_imported_unclaimed_batch_members (
        world_id, stream_id, batch_sequence, character_id
      ) values ($1, $2, $3, $4::uuid)
      on conflict (character_id) do nothing
      returning world_id, stream_id, batch_sequence::text`,
      [input.worldId, input.streamId, input.sequence, input.characterId],
    )
    if (inserted.rows.length === 0) {
      const existing = await this.client.query<BatchMemberRow>(
        `select world_id, stream_id, batch_sequence::text
           from private.game_imported_unclaimed_batch_members
          where character_id = $1::uuid`,
        [input.characterId],
      )
      const row = existing.rows[0]
      if (!row || row.world_id !== input.worldId || row.stream_id !== input.streamId
        || row.batch_sequence !== String(input.sequence)) throw new Error('batch member conflict')
    }
    if (input.legacyLocator) await this.recordBatchMemberLegacyLocator({ characterId: input.characterId, ...input.legacyLocator })
  }

  private async recordBatchMemberLegacyLocator(input: { characterId: string, canonicalName: string, legacyNameSha1: string, legacyShard: string }): Promise<void> {
    const inserted = await this.client.query<LegacyLocatorRow>(
      `insert into private.game_imported_unclaimed_batch_member_legacy_locators (
        character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
      ) values ($1::uuid, $2, $3, $4)
      on conflict (character_id) do nothing
      returning canonical_legacy_name, legacy_name_sha1, legacy_shard`,
      [input.characterId, input.canonicalName, input.legacyNameSha1, input.legacyShard],
    )
    if (inserted.rows.length === 1) return

    // The immutable relation can suppress only an exact retry. A missing or
    // different existing row is a database-integrity conflict, never a repair.
    const existing = await this.client.query<LegacyLocatorRow>(
      `select canonical_legacy_name, legacy_name_sha1, legacy_shard
         from private.game_imported_unclaimed_batch_member_legacy_locators
        where character_id = $1::uuid`,
      [input.characterId],
    )
    const row = existing.rows[0]
    if (!row || row.canonical_legacy_name !== input.canonicalName
      || row.legacy_name_sha1 !== input.legacyNameSha1 || row.legacy_shard !== input.legacyShard) {
      throw new Error('legacy locator conflict')
    }
  }

  async readWatermark(worldId: string, streamId: string): Promise<number | undefined> {
    const result = await this.client.query<WatermarkRow>(
      `select watermark_sequence::text from private.game_imported_unclaimed_batch_watermarks
        where world_id = $1 and stream_id = $2`,
      [worldId, streamId],
    )
    const row = result.rows[0]
    return row === undefined ? undefined : Number(row.watermark_sequence)
  }

  async advanceWatermark(worldId: string, streamId: string, sequence: number): Promise<void> {
    await this.client.query(
      `insert into private.game_imported_unclaimed_batch_watermarks (
        world_id, stream_id, watermark_sequence, committed_batch_sequence
      ) values ($1, $2, $3, $3)
      on conflict (world_id, stream_id) do update
        set watermark_sequence = excluded.watermark_sequence,
            committed_batch_sequence = excluded.committed_batch_sequence,
            observed_at = clock_timestamp()`,
      [worldId, streamId, sequence],
    )
  }

  private batch(row: BatchRow | undefined): LedgerBatch | undefined {
    if (!row) return undefined
    // Rows can only be inserted through the validated store API, but decoding
    // through the shared constructor makes corrupt/manual rows fail closed.
    const sequence = Number(row.batch_sequence)
    const recordCount = Number(row.record_count)
    if (!Number.isSafeInteger(sequence) || sequence < 0 || !Number.isSafeInteger(recordCount) || recordCount < 0) {
      throw new Error('invalid batch ledger state')
    }
    return { stableKey: row.identity_key, sequence, recordCount }
  }
}
