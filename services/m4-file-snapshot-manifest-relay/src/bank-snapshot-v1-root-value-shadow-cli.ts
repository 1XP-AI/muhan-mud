import { isAbsolute } from 'node:path'
import { NodeBankSnapshotV1ArtifactFilesystem } from './bank-snapshot-v1-artifact-relay.js'
import { relayBankSnapshotV1RootValueShadowsOnce } from './bank-snapshot-v1-root-value-shadow-relay.js'
import { assertDatabaseUrl, PostgresBankSnapshotV1RootValueShadowStore, type BankSnapshotV1RootValueShadowStore } from './store.js'

const ENABLED_ENV = 'M4_BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ENABLED'
const OUTBOX_ENV = 'M4_BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_OUTBOX_DIR'
const DATABASE_ENV = 'M4_BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_DATABASE_URL'

function configuredAbsolutePath(value: string | undefined): string {
  if (!value || value.includes('\0') || !isAbsolute(value)) throw new Error('invalid bank root-value shadow configuration')
  return value
}

export interface BankSnapshotV1RootValueShadowCliDependencies {
  createStore(databaseUrl: string): BankSnapshotV1RootValueShadowStore
  relay: typeof relayBankSnapshotV1RootValueShadowsOnce
  writeStdout(value: string): void
}
const productionDependencies: BankSnapshotV1RootValueShadowCliDependencies = {
  createStore: (databaseUrl) => new PostgresBankSnapshotV1RootValueShadowStore(databaseUrl),
  relay: relayBankSnapshotV1RootValueShadowsOnce,
  writeStdout: (value) => process.stdout.write(value),
}

/** Explicit one-shot, default-OFF root-value writer. */
export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2), dependencies: BankSnapshotV1RootValueShadowCliDependencies = productionDependencies): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once' || env[ENABLED_ENV] !== 'true') throw new Error('invalid bank root-value shadow configuration')
  const outboxPath = configuredAbsolutePath(env[OUTBOX_ENV])
  const store = dependencies.createStore(assertDatabaseUrl(env[DATABASE_ENV]))
  try {
    const result = await dependencies.relay(outboxPath, store, new NodeBankSnapshotV1ArtifactFilesystem())
    dependencies.writeStdout(`${JSON.stringify(result)}\n`)
    return result.invalid > 0 || result.conflict > 0 || result.ioError > 0 || result.retryable > 0 || result.unknown > 0 ? 1 : 0
  } finally { await store.close?.() }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => { process.stderr.write('m4 BankSnapshotV1 root-value shadow relay failed safely\n'); process.exitCode = 1 })
}
