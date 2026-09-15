import assert from 'node:assert/strict'
import { test } from 'node:test'
import { PostgresNormalizedProjectionSessionReader } from '../src/player-snapshot-v1-normalized-projection-session.js'

const url = 'postgresql://mud_normalized_replay_reader_login@localhost/test'
const identity = { worldId: 'muhan-01', characterId: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', commandId: '11111111-1111-4111-8111-111111111111' }
const contract = { currentUser: 'mud_normalized_replay_reader_login', sessionUser: 'mud_normalized_replay_reader_login', defaultReadOnly: 'on' }
function fixture(failAt?: string, check: unknown = contract) {
  const calls: string[] = []
  const releases: boolean[] = []
  const pool = {
    connect: async () => ({
      query: async (sql: string) => {
        calls.push(sql)
        if (sql === failAt || (failAt === 'data' && sql.startsWith('select a.world_id'))) throw new Error('injected query failure')
        return { rows: sql.includes('current_user') ? [check] as Record<string, unknown>[]
          : sql === 'show transaction_read_only' ? [{ transaction_read_only: 'on' }] : [] }
      },
      release: (destroy = false) => { releases.push(destroy) },
    }),
    end: async () => { calls.push('end') },
  }
  return { reader: new PostgresNormalizedProjectionSessionReader(url, pool), calls, releases }
}

test('owns a read-only transaction and releases the connection after reading', async () => {
  const f = fixture()
  assert.deepEqual(await f.reader.findByIdentity(identity), [])
  assert.equal(f.calls[0], 'begin read only')
  assert.equal(f.calls.at(-1), 'rollback')
  assert.deepEqual(f.releases, [false])
  await f.reader.close()
  assert.equal(f.calls.at(-1), 'end')
})

test('rejects a changed login or writable defaults before reading projection rows', async () => {
  for (const changed of [{ currentUser: 'mud_writer' }, { sessionUser: 'postgres' }, { defaultReadOnly: 'off' }]) {
    const f = fixture(undefined, { ...contract, ...changed })
    await assert.rejects(f.reader.findByIdentity(identity))
    assert.equal(f.calls.some(sql => sql.startsWith('select a.world_id')), false)
    assert.equal(f.calls.at(-1), 'rollback')
    assert.deepEqual(f.releases, [true])
  }
})

test('rolls back query failures and destroys connections when rollback fails', async () => {
  for (const failAt of ['begin read only', 'data', 'rollback']) {
    const f = fixture(failAt)
    await assert.rejects(f.reader.findByIdentity(identity))
    assert.equal(f.calls.at(-1), 'rollback')
    assert.deepEqual(f.releases, [true])
  }
})

test('rejects application URLs before constructing a database pool', () => {
  for (const bad of ['', 'https://example.invalid', 'postgresql://mud_writer_login@localhost/test', url + '?user=postgres']) {
    assert.throws(() => new PostgresNormalizedProjectionSessionReader(bad), /configuration/)
  }
})
