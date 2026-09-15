import { isAbsolute } from 'node:path'
import { pathToFileURL } from 'node:url'
import { parseManifest } from './manifest.js'
import { NodePlayerSnapshotV1ArtifactFilesystem, type PlayerSnapshotV1ArtifactFilesystem } from './player-snapshot-v1-artifact-relay.js'
import { parsePlayerSnapshotV1ReceiptBoundArtifactEvidence, type PlayerSnapshotV1ReceiptBoundArtifactEvidence } from './player-snapshot-v1-artifact.js'
import { projectPlayerSnapshotV1Normalized } from './player-snapshot-v1-normalized-projection.js'
import { assertNormalizedProjectionSessionDatabaseUrl, PostgresNormalizedProjectionSessionReader } from './player-snapshot-v1-normalized-projection-session.js'
import { comparePlayerSnapshotV1NormalizedProjectionShadow, type ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader, type PlayerSnapshotV1NormalizedProjectionShadowComparison } from './player-snapshot-v1-normalized-projection-shadow-comparator.js'

interface ClosableReader extends ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader { close(): Promise<void> }
export interface NormalizedShadowCliDependencies {
  loadArtifact(path: string): Promise<PlayerSnapshotV1ReceiptBoundArtifactEvidence>
  createReader(url: string): ClosableReader
  compare(artifact: PlayerSnapshotV1ReceiptBoundArtifactEvidence, reader: ClosableReader, projectorPath: string): Promise<PlayerSnapshotV1NormalizedProjectionShadowComparison>
  writeStdout(value: string): void
}

export async function loadNormalizedShadowArtifact(path: string, filesystem: PlayerSnapshotV1ArtifactFilesystem = new NodePlayerSnapshotV1ArtifactFilesystem()) {
  const files = await filesystem.scan(path)
  const file = files[0]
  if (files.length !== 1 || !file || file.error || !file.bytes || !file.receiptManifestBytes) throw new Error('invalid input')
  return parsePlayerSnapshotV1ReceiptBoundArtifactEvidence(file.name, file.bytes, parseManifest(file.receiptManifestBytes))
}
const defaults: NormalizedShadowCliDependencies = {
  loadArtifact: loadNormalizedShadowArtifact,
  createReader: url => new PostgresNormalizedProjectionSessionReader(url),
  compare: (artifact, reader, runnerPath) => comparePlayerSnapshotV1NormalizedProjectionShadow(artifact, reader, {
    project: (payload, snapshotSha256) => projectPlayerSnapshotV1Normalized(payload, { runnerPath, snapshotSha256 }),
  }),
  writeStdout: value => { process.stdout.write(value) },
}
function absolute(value: string | undefined): string {
  if (!value || !isAbsolute(value) || value.includes('\0')) throw new Error('invalid path')
  return value
}

/** Explicit one-shot shadow diagnostic, separate from every writer entrypoint. */
export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2), dependencies: NormalizedShadowCliDependencies = defaults): Promise<number> {
  const emit = (classification: string) => {
    dependencies.writeStdout(JSON.stringify({ format: 'player-snapshot-v1-normalized-shadow-comparison', version: '1', classification }) + '\n')
    return classification === 'MATCH' ? 0 : 1
  }
  let outbox: string, database: string, projector: string
  try {
    if (args.length !== 1 || args[0] !== '--once') throw new Error('invalid arguments')
    outbox = absolute(env.M4_NORMALIZED_SHADOW_OUTBOX_PATH)
    database = assertNormalizedProjectionSessionDatabaseUrl(env.M4_NORMALIZED_SHADOW_DATABASE_URL)
    projector = absolute(env.M4_NORMALIZED_SHADOW_PROJECTOR_PATH)
  } catch { return emit('INVALID_INPUT') }
  let artifact: PlayerSnapshotV1ReceiptBoundArtifactEvidence
  try { artifact = await dependencies.loadArtifact(outbox) }
  catch { return emit('INVALID_INPUT') }
  let reader: ClosableReader
  try { reader = dependencies.createReader(database) }
  catch { return emit('RECORD_READ_ERROR') }
  let classification: string
  try { classification = await dependencies.compare(artifact, reader, projector) }
  catch { classification = 'COMPARISON_ERROR' }
  try { await reader.close() }
  catch { classification = 'CONNECTION_CLOSE_ERROR' }
  return emit(classification)
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().then(code => { process.exitCode = code }).catch(() => { process.exitCode = 1 })
}
