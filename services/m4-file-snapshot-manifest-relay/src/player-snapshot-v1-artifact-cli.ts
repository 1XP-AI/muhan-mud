import { isAbsolute } from 'node:path'
import { relayPlayerSnapshotV1ArtifactsOnce } from './player-snapshot-v1-artifact-relay.js'
import { playerSnapshotV1ReplayObserverFromEnvironment } from './player-snapshot-v1-replay-observer.js'
import {
  assertDatabaseUrl,
  PostgresPlayerSnapshotV1ArtifactStore,
  type PlayerSnapshotV1ArtifactFulfillmentStore,
  type PlayerSnapshotV1ArtifactStore,
} from './store.js'

function required(env: NodeJS.ProcessEnv, name: string): string {
  const value = env[name]
  if (!value || value.includes('\0')) throw new Error('invalid relay configuration')
  return value
}

type PlayerSnapshotV1ArtifactRelayStore = PlayerSnapshotV1ArtifactStore & PlayerSnapshotV1ArtifactFulfillmentStore

export interface PlayerSnapshotV1ArtifactCliDependencies {
  createStore(databaseUrl: string): PlayerSnapshotV1ArtifactRelayStore
  relay: typeof relayPlayerSnapshotV1ArtifactsOnce
  replayObserverFromEnvironment: typeof playerSnapshotV1ReplayObserverFromEnvironment
  writeStdout(value: string): void
}

const productionDependencies: PlayerSnapshotV1ArtifactCliDependencies = {
  createStore: (databaseUrl) => new PostgresPlayerSnapshotV1ArtifactStore(databaseUrl),
  relay: relayPlayerSnapshotV1ArtifactsOnce,
  replayObserverFromEnvironment: playerSnapshotV1ReplayObserverFromEnvironment,
  writeStdout: (value) => process.stdout.write(value),
}

/** Fulfillment is a separately deployed authority step and remains default-off. */
export function isPlayerSnapshotV1ArtifactFulfillmentEnabled(env: NodeJS.ProcessEnv): boolean {
  return env.M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED === 'true'
}

/** Dedicated entrypoint: the legacy manifest relay remains unchanged. */
export async function main(
  env: NodeJS.ProcessEnv = process.env,
  args: readonly string[] = process.argv.slice(2),
  dependencies: PlayerSnapshotV1ArtifactCliDependencies = productionDependencies,
): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once') throw new Error('invalid relay configuration')
  const outboxPath = required(env, 'M4_FILE_SNAPSHOT_OUTBOX_DIR')
  if (!isAbsolute(outboxPath)) throw new Error('invalid relay configuration')
  const store = dependencies.createStore(assertDatabaseUrl(env.DATABASE_URL))
  try {
    const result = await dependencies.relay(
      outboxPath, store, undefined, dependencies.replayObserverFromEnvironment(env),
      isPlayerSnapshotV1ArtifactFulfillmentEnabled(env) ? store : undefined,
    )
    dependencies.writeStdout(`${JSON.stringify(result)}\n`)
    return result.invalid > 0 || result.conflict > 0 || result.ioError > 0 || result.retryable > 0 || result.unknown > 0
      || (result.fulfillmentInvalid ?? 0) > 0 || (result.fulfillmentConflict ?? 0) > 0
      || (result.fulfillmentRetryable ?? 0) > 0 || (result.fulfillmentUnknown ?? 0) > 0 ? 1 : 0
  } finally { await store.close?.() }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 PlayerSnapshotV1 artifact relay failed safely\n')
    process.exitCode = 1
  })
}
