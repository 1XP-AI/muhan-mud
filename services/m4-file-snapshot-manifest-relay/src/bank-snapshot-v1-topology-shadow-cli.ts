import { isAbsolute } from 'node:path'
import {
  NodeBankSnapshotV1ArtifactFilesystem,
  relayBankSnapshotV1ArtifactsOnce,
} from './bank-snapshot-v1-artifact-relay.js'
import {
  assertDatabaseUrl,
  PostgresBankSnapshotV1TopologyShadowStore,
  type BankSnapshotV1TopologyShadowStore,
} from './store.js'

const ENABLED_ENV = 'M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ENABLED'
const OUTBOX_ENV = 'M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_OUTBOX_DIR'
const DATABASE_ENV = 'M4_BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_DATABASE_URL'

function configuredAbsolutePath(value: string | undefined): string {
  if (!value || value.includes('\0') || !isAbsolute(value)) throw new Error('invalid bank topology shadow configuration')
  return value
}

export interface BankSnapshotV1TopologyShadowCliDependencies {
  createStore(databaseUrl: string): BankSnapshotV1TopologyShadowStore
  relay: typeof relayBankSnapshotV1ArtifactsOnce
  writeStdout(value: string): void
}

const productionDependencies: BankSnapshotV1TopologyShadowCliDependencies = {
  createStore: (databaseUrl) => new PostgresBankSnapshotV1TopologyShadowStore(databaseUrl),
  relay: relayBankSnapshotV1ArtifactsOnce,
  writeStdout: (value) => process.stdout.write(value),
}

/**
 * Explicit default-OFF writer: an operator must select this entrypoint,
 * supply the exact enable token, and request one scan.
 */
export async function main(
  env: NodeJS.ProcessEnv = process.env,
  args: readonly string[] = process.argv.slice(2),
  dependencies: BankSnapshotV1TopologyShadowCliDependencies = productionDependencies,
): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once' || env[ENABLED_ENV] !== 'true') {
    throw new Error('invalid bank topology shadow configuration')
  }
  const outboxPath = configuredAbsolutePath(env[OUTBOX_ENV])
  const store = dependencies.createStore(assertDatabaseUrl(env[DATABASE_ENV]))
  try {
    const result = await dependencies.relay(outboxPath, store, new NodeBankSnapshotV1ArtifactFilesystem())
    dependencies.writeStdout(`${JSON.stringify(result)}\n`)
    return result.invalid > 0 || result.conflict > 0 || result.ioError > 0 || result.retryable > 0 || result.unknown > 0 ? 1 : 0
  } finally { await store.close?.() }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 BankSnapshotV1 topology shadow relay failed safely\n')
    process.exitCode = 1
  })
}
