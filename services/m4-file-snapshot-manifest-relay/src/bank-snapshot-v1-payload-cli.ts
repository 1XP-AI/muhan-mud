import { isAbsolute } from 'node:path'
import { NodeBankSnapshotV1ArtifactFilesystem } from './bank-snapshot-v1-artifact-relay.js'
import { relayBankSnapshotV1PayloadsOnce, type BankSnapshotV1PayloadStore } from './bank-snapshot-v1-payload-relay.js'
import { assertDatabaseUrl, PostgresBankSnapshotV1PayloadStore } from './store.js'

export interface Dependencies {
  createStore(url: string): BankSnapshotV1PayloadStore
  relay: typeof relayBankSnapshotV1PayloadsOnce
  write(value: string): void
}
const production: Dependencies = {
  createStore: (url) => new PostgresBankSnapshotV1PayloadStore(url),
  relay: relayBankSnapshotV1PayloadsOnce,
  write: (value) => { process.stdout.write(value) },
}
export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2), dependencies: Dependencies = production): Promise<number> {
  const path = env.M4_BANK_SNAPSHOT_V1_PAYLOAD_OUTBOX_DIR
  if (env.M4_BANK_SNAPSHOT_V1_PAYLOAD_ENABLED !== 'true' || args.length !== 1 || args[0] !== '--once'
      || !path || !isAbsolute(path) || path.includes('\0')) throw new Error('invalid bank payload configuration')
  const store = dependencies.createStore(assertDatabaseUrl(env.M4_BANK_SNAPSHOT_V1_PAYLOAD_DATABASE_URL))
  try {
    const result = await dependencies.relay(path,store,new NodeBankSnapshotV1ArtifactFilesystem())
    dependencies.write(`${JSON.stringify(result)}\n`)
    return result.invalid || result.conflict || result.retryable || result.unknown || result.ioError ? 1 : 0
  } finally { await store.close?.() }
}
if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('bank payload relay failed safely\n')
    process.exitCode = 1
  })
}
