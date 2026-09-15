import { isAbsolute } from 'node:path'
import { relayPlayerSnapshotV1ManifestFirstOnce } from './player-snapshot-v1-manifest-first-relay.js'
import type { PlayerSnapshotV1NormalizedProjectionPersistence } from './player-snapshot-v1-artifact-relay.js'
import { projectPlayerSnapshotV1Normalized } from './player-snapshot-v1-normalized-projection.js'
import {
  assertDatabaseUrl,
  PostgresManifestStore,
  PostgresPlayerSnapshotV1ArtifactStore,
  PostgresPlayerSnapshotNormalizedV1ProjectionStore,
  type ManifestStore,
  type PlayerSnapshotV1ArtifactStore,
} from './store.js'

function required(env: NodeJS.ProcessEnv, name: string): string {
  const value = env[name]
  if (!value || value.includes('\0')) throw new Error('invalid relay configuration')
  return value
}

type ManifestFirstStore = ManifestStore & PlayerSnapshotV1ArtifactStore

export interface PlayerSnapshotV1ManifestFirstCliDependencies {
  createStore(databaseUrl: string): ManifestFirstStore
  createNormalizedPersistence?(databaseUrl: string, runnerPath: string): PlayerSnapshotV1NormalizedProjectionPersistence
  relay: typeof relayPlayerSnapshotV1ManifestFirstOnce
  writeStdout(value: string): void
}

function createStore(databaseUrl: string): ManifestFirstStore {
  const manifests = new PostgresManifestStore(databaseUrl)
  const artifacts = new PostgresPlayerSnapshotV1ArtifactStore(databaseUrl)
  return {
    recordManifest: (manifest) => manifests.recordManifest(manifest),
    recordPlayerSnapshotV1Artifact: (artifact) => artifacts.recordPlayerSnapshotV1Artifact(artifact),
    close: async () => { await Promise.all([manifests.close(), artifacts.close()]) },
  }
}

const productionDependencies: PlayerSnapshotV1ManifestFirstCliDependencies = {
  createStore,
  createNormalizedPersistence: (databaseUrl, runnerPath) => ({
    project: (payload, snapshotSha256) => projectPlayerSnapshotV1Normalized(payload, { runnerPath, snapshotSha256 }),
    store: new PostgresPlayerSnapshotNormalizedV1ProjectionStore(databaseUrl),
  }),
  relay: relayPlayerSnapshotV1ManifestFirstOnce,
  writeStdout: (value) => process.stdout.write(value),
}

/** Dedicated operational entrypoint for the paired M3 shadow outbox. */
export async function main(
  env: NodeJS.ProcessEnv = process.env,
  args: readonly string[] = process.argv.slice(2),
  dependencies: PlayerSnapshotV1ManifestFirstCliDependencies = productionDependencies,
): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once') throw new Error('invalid relay configuration')
  const outboxPath = required(env, 'M4_FILE_SNAPSHOT_OUTBOX_DIR')
  if (!isAbsolute(outboxPath)) throw new Error('invalid relay configuration')
  const enabled = env.M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ENABLED === 'true'
  let runnerPath: string | undefined
  if (enabled) {
    runnerPath = required(env, 'M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_RUNNER')
    if (!isAbsolute(runnerPath) || !dependencies.createNormalizedPersistence) throw new Error('invalid relay configuration')
  }
  const databaseUrl = assertDatabaseUrl(env.DATABASE_URL)
  const store = dependencies.createStore(databaseUrl)
  let normalizedPersistence: PlayerSnapshotV1NormalizedProjectionPersistence | undefined
  try {
    if (enabled) normalizedPersistence = dependencies.createNormalizedPersistence!(databaseUrl, runnerPath!)
    const result = normalizedPersistence
      ? await dependencies.relay(outboxPath, store, undefined, normalizedPersistence)
      : await dependencies.relay(outboxPath, store)
    dependencies.writeStdout(`${JSON.stringify(result)}\n`)
    return result.invalid > 0 || result.conflict > 0 || result.ioError > 0 || result.retryable > 0 || result.unknown > 0 ? 1 : 0
  } finally {
    try { await normalizedPersistence?.store.close?.() }
    finally { await store.close?.() }
  }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 PlayerSnapshotV1 manifest-first relay failed safely\n')
    process.exitCode = 1
  })
}
