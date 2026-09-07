import assert from 'node:assert/strict'
import test from 'node:test'
import { sqlCommand } from './sql-transport.js'

test('default SQL transport retains runner-owned Docker execution', () => {
  assert.deepEqual(sqlCommand({ STACK_E2E_PG_CONTAINER: 'owned' }, 'select 1').slice(0, 1), ['docker'])
  assert.throws(() => sqlCommand({}, 'select 1'))
})
test('local transport requires disposable opt-in and pins loopback database', () => {
  assert.throws(() => sqlCommand({ STACK_E2E_LOCAL_DISPOSABLE: '1' }, 'select 1'))
  const [command, args] = sqlCommand({ STACK_E2E_LOCAL_DISPOSABLE: '1', ADMISSION_IDENTITY_PG17_ALLOW_DISPOSABLE: '1', PGHOST: 'production' }, 'select 1')
  assert.equal(command, 'psql')
  assert.deepEqual(args.slice(0, 8), ['-h', '127.0.0.1', '-p', '5432', '-U', 'postgres', '-d', 'stack_e2e'])
})
