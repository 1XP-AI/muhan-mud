import { relayOnce } from './relay.js'
import { assertDatabaseUrl, PostgresManifestStore } from './store.js'
import { isAbsolute } from 'node:path'

function required(env: NodeJS.ProcessEnv, name: string): string {
  const value = env[name]
  if (!value || value.includes('\0')) throw new Error('invalid relay configuration')
  return value
}

export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2)): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once') throw new Error('invalid relay configuration')
  const outboxPath = required(env, 'M4_FILE_SNAPSHOT_OUTBOX_DIR')
  if (!isAbsolute(outboxPath)) throw new Error('invalid relay configuration')
  const store = new PostgresManifestStore(assertDatabaseUrl(env.DATABASE_URL))
  try {
    const result = await relayOnce(outboxPath, store)
    process.stdout.write(`${JSON.stringify(result)}\n`)
    return result.invalid > 0 || result.conflict > 0 || result.ioError > 0 || result.retryable > 0 || result.unknown > 0 ? 1 : 0
  } finally { await store.close?.() }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 manifest relay failed safely\n')
    process.exitCode = 1
  })
}
