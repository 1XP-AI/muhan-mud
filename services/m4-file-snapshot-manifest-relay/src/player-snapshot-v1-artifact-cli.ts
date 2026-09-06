import { isAbsolute } from 'node:path'
import { relayPlayerSnapshotV1ArtifactsOnce, type PlayerSnapshotV1NormalizedProjectionPersistence } from './player-snapshot-v1-artifact-relay.js'
import { playerSnapshotV1ReplayObserverFromEnvironment } from './player-snapshot-v1-replay-observer.js'
import { projectPlayerSnapshotV1Normalized } from './player-snapshot-v1-normalized-projection.js'
import {
  assertDatabaseUrl,
  PostgresPlayerSnapshotNormalizedV1ProjectionStore,
  PostgresPlayerSnapshotV1ArtifactStore,
  type PlayerSnapshotNormalizedV1ProjectionStore,
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
  createNormalizedProjectionStore(databaseUrl: string): PlayerSnapshotNormalizedV1ProjectionStore
  projectNormalized(payload: Uint8Array, runnerPath: string, snapshotSha256: string): ReturnType<typeof projectPlayerSnapshotV1Normalized>
  relay: typeof relayPlayerSnapshotV1ArtifactsOnce
  replayObserverFromEnvironment: typeof playerSnapshotV1ReplayObserverFromEnvironment
  writeStdout(value: string): void
}

const productionDependencies: PlayerSnapshotV1ArtifactCliDependencies = {
  createStore: (databaseUrl) => new PostgresPlayerSnapshotV1ArtifactStore(databaseUrl),
  createNormalizedProjectionStore: (databaseUrl) => new PostgresPlayerSnapshotNormalizedV1ProjectionStore(databaseUrl),
  projectNormalized: (payload, runnerPath, snapshotSha256) => projectPlayerSnapshotV1Normalized(payload, { runnerPath, snapshotSha256 }),
  relay: relayPlayerSnapshotV1ArtifactsOnce,
  replayObserverFromEnvironment: playerSnapshotV1ReplayObserverFromEnvironment,
  writeStdout: (value) => process.stdout.write(value),
}

/** Fulfillment is a separately deployed authority step and remains default-off. */
export function isPlayerSnapshotV1ArtifactFulfillmentEnabled(env: NodeJS.ProcessEnv): boolean {
  return env.M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED === 'true'
}

/** Normalized projection persistence is detached unless deliberately enabled. */
export function isPlayerSnapshotNormalizedV1ProjectionPersistenceEnabled(env: NodeJS.ProcessEnv): boolean {
  return env.M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ENABLED === 'true'
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
  let normalizedProjectionStore: PlayerSnapshotNormalizedV1ProjectionStore | undefined
  try {
    let normalizedProjectionPersistence: PlayerSnapshotV1NormalizedProjectionPersistence | undefined
    if (isPlayerSnapshotNormalizedV1ProjectionPersistenceEnabled(env)) {
      const runnerPath = required(env, 'M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_RUNNER')
      if (!isAbsolute(runnerPath)) throw new Error('invalid relay configuration')
      normalizedProjectionStore = dependencies.createNormalizedProjectionStore(assertDatabaseUrl(env.DATABASE_URL))
      normalizedProjectionPersistence = {
        project: (payload, snapshotSha256) => dependencies.projectNormalized(payload, runnerPath, snapshotSha256),
        store: normalizedProjectionStore,
      }
    }
    const result = await dependencies.relay(
      outboxPath, store, undefined, dependencies.replayObserverFromEnvironment(env),
      isPlayerSnapshotV1ArtifactFulfillmentEnabled(env) ? store : undefined,
      undefined,
      normalizedProjectionPersistence,
    )
    dependencies.writeStdout(`${JSON.stringify(result)}\n`)
    const failed = result.invalid > 0 || result.conflict > 0 || result.ioError > 0 || result.retryable > 0 || result.unknown > 0
      || (result.fulfillmentInvalid ?? 0) > 0 || (result.fulfillmentConflict ?? 0) > 0
      || (result.fulfillmentRetryable ?? 0) > 0 || (result.fulfillmentUnknown ?? 0) > 0
      || (result.normalizedProjectionInvalid ?? 0) > 0 || (result.normalizedProjectionConflict ?? 0) > 0
      || (result.normalizedProjectionRetryable ?? 0) > 0 || (result.normalizedProjectionUnknown ?? 0) > 0
    return failed ? 1 : 0
  } finally {
    await normalizedProjectionStore?.close?.()
    await store.close?.()
  }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 PlayerSnapshotV1 artifact relay failed safely\n')
    process.exitCode = 1
  })
}
