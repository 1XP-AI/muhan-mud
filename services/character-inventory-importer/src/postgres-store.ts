import { createRequire } from 'node:module'
import type { BatchIdentity } from './batch-identity.js'
import type { ExistingCharacter, ImportStore, ImportTransaction, InventoryRecord, LedgerBatch } from './inventory.js'

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

  async insertImportedUnclaimed(input: { worldId: string, record: InventoryRecord }): Promise<void> {
    const { record } = input
    await this.client.query(
      `insert into public.game_characters (
        world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
        storage_format, imported_file_sha256, owner_user_id
      ) values ($1, $2, $3, $4, 'imported_unclaimed', 1, $5, null)`,
      [input.worldId, record.name, record.canonicalNameKey, record.expectedShard, record.sha256],
    )
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
