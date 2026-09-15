import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

export function sqlCommand(env: NodeJS.ProcessEnv, query: string): [string, string[]] {
  const args = ['-U', 'postgres', '-d', 'stack_e2e', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query]
  if (env.STACK_E2E_LOCAL_DISPOSABLE === '1') {
    assert.equal(env.ADMISSION_IDENTITY_PG17_ALLOW_DISPOSABLE, '1')
    // Runner shares only the disposable PostgreSQL network namespace. No
    // configurable host/URL and no Docker socket are accepted in this lane.
    return ['psql', ['-h', '127.0.0.1', '-p', '5432', ...args]]
  }
  assert.ok(env.STACK_E2E_PG_CONTAINER, 'runner-owned PostgreSQL container required')
  return ['docker', ['exec', env.STACK_E2E_PG_CONTAINER, 'psql', ...args]]
}

export function sqlEnvironment(source: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...source, PGPASSWORD: source.STACK_E2E_PG_PASSWORD, PSQLRC: '/dev/null' }
  delete env.PGSERVICE
  delete env.PGSERVICEFILE
  return env
}

export async function sql(query: string): Promise<string> {
  const [command, args] = sqlCommand(process.env, query)
  const result = await promisify(execFile)(command, args, {
    maxBuffer: 1024 * 1024,
    env: sqlEnvironment(process.env),
  })
  return result.stdout.trim()
}
