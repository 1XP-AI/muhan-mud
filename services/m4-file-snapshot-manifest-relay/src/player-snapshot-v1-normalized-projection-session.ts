import { createRequire } from 'node:module'
import { PostgresNormalizedProjectionReader, type NormalizedProjectionQueryClient } from './player-snapshot-v1-normalized-projection-reader.js'
import type { ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader } from './player-snapshot-v1-normalized-projection-shadow-comparator.js'

interface SessionClient extends NormalizedProjectionQueryClient { release(destroy?: boolean): void }
export interface NormalizedProjectionSessionPool {
  connect(): Promise<SessionClient>
  end(): Promise<void>
}
const require = createRequire(import.meta.url)
const login = 'mud_normalized_replay_reader_login'

/** Owns a separate pool; never shares a writer connection or changes role. */
export class PostgresNormalizedProjectionSessionReader implements ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader {
  private readonly pool: NormalizedProjectionSessionPool

  constructor(databaseUrl: string, pool?: NormalizedProjectionSessionPool) {
    try {
      const url = new URL(databaseUrl)
      if (!['postgres:', 'postgresql:'].includes(url.protocol) || !url.hostname
        || decodeURIComponent(url.username) !== login || url.pathname.length < 2 || url.hash
        || [...url.searchParams.keys()].some(key => key !== 'sslmode')
        || url.searchParams.getAll('sslmode').length > 1) throw new Error()
    } catch { throw new Error('invalid normalized projection session configuration') }
    this.pool = pool ?? new (require('pg').Pool)({ connectionString: databaseUrl, max: 1, connectionTimeoutMillis: 5000 })
  }

  async findByIdentity(identity: Readonly<{ worldId: string, characterId: string, commandId: string }>) {
    const scope = { ...identity }
    const client = await this.pool.connect()
    let destroy = false
    try {
      await client.query('begin read only')
      const contract = await client.query(`select current_user as "currentUser", session_user as "sessionUser",
        current_setting('default_transaction_read_only') as "defaultReadOnly"`)
      const row = contract.rows[0]
      if (contract.rows.length !== 1 || row?.currentUser !== login || row?.sessionUser !== login
        || row?.defaultReadOnly !== 'on') throw new Error('invalid session')
      return await new PostgresNormalizedProjectionReader(client).findByIdentity(scope)
    } catch {
      destroy = true
      throw new Error('normalized projection session read failed')
    } finally {
      try { await client.query('rollback') }
      catch {
        destroy = true
        throw new Error('normalized projection session rollback failed')
      } finally { client.release(destroy) }
    }
  }

  async close(): Promise<void> { await this.pool.end() }
}
