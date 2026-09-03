import { createRequire } from 'node:module'
import type { Manifest } from './manifest.js'

export type StoreOutcome = 'RECORDED' | 'EXACT_RETRY'

export interface ManifestStore {
  recordManifest(manifest: Manifest): Promise<StoreOutcome>
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
