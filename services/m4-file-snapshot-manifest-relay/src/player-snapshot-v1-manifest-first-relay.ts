import { parseManifest } from './manifest.js'
import {
  NodePlayerSnapshotV1ArtifactFilesystem,
  type PlayerSnapshotV1ArtifactFilesystem,
} from './player-snapshot-v1-artifact-relay.js'
import { parsePlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import { classifyDatabaseError, type ManifestStore, type PlayerSnapshotV1ArtifactStore } from './store.js'

/**
 * The artifact filesystem already provides descriptor-rooted, immutable paired
 * evidence reads. This relay deliberately adds no file mutation or acknowledgement.
 */
export type PlayerSnapshotV1ManifestFirstFilesystem = PlayerSnapshotV1ArtifactFilesystem

export interface PlayerSnapshotV1ManifestFirstSummary {
  visited: number
  valid: number
  delivered: number
  recorded: number
  exactRetry: number
  invalid: number
  conflict: number
  retryable: number
  unknown: number
  ioError: number
  manifestDelivered: number
  manifestRecorded: number
  manifestExactRetry: number
  artifactDelivered: number
  artifactRecorded: number
  artifactExactRetry: number
}

function summary(): PlayerSnapshotV1ManifestFirstSummary {
  return {
    visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0,
    invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
    manifestDelivered: 0, manifestRecorded: 0, manifestExactRetry: 0,
    artifactDelivered: 0, artifactRecorded: 0, artifactExactRetry: 0,
  }
}

/**
 * Record each complete immutable M3 pair in its database precondition order:
 * canonical legacy-file manifest first, then PlayerSnapshotV1 artifact. A
 * failed manifest stage deliberately prevents its artifact RPC for that pass.
 * Fulfillment, replay, and projection are not capabilities of this entrypoint.
 */
export async function relayPlayerSnapshotV1ManifestFirstOnce(
  outboxPath: string,
  store: ManifestStore & PlayerSnapshotV1ArtifactStore,
  filesystem: PlayerSnapshotV1ManifestFirstFilesystem = new NodePlayerSnapshotV1ArtifactFilesystem(),
): Promise<PlayerSnapshotV1ManifestFirstSummary> {
  const result = summary()
  let files: Awaited<ReturnType<PlayerSnapshotV1ManifestFirstFilesystem['scan']>>
  try { files = await filesystem.scan(outboxPath) }
  catch { result.ioError++; return result }
  result.visited = files.length

  const ordered = [...files].sort((left, right) => Buffer.compare(Buffer.from(left.name), Buffer.from(right.name)))
  for (const file of ordered) {
    if (file.error === 'invalid') { result.invalid++; continue }
    if (file.error === 'io' || !file.bytes) { result.ioError++; continue }
    if (!file.receiptManifestBytes) { result.invalid++; continue }

    let manifest
    let artifact
    try {
      manifest = parseManifest(file.receiptManifestBytes)
      artifact = parsePlayerSnapshotV1Artifact(file.name, file.bytes, manifest)
    } catch { result.invalid++; continue }
    result.valid++

    try {
      const outcome = await store.recordManifest(manifest)
      if (outcome === 'RECORDED') {
        result.manifestRecorded++
        result.manifestDelivered++
      } else if (outcome === 'EXACT_RETRY') {
        result.manifestExactRetry++
        result.manifestDelivered++
      } else {
        result.unknown++
        continue
      }
    } catch (error) {
      result[classifyDatabaseError(error)]++
      continue
    }

    try {
      const outcome = await store.recordPlayerSnapshotV1Artifact(artifact)
      if (outcome === 'RECORDED') {
        result.artifactRecorded++
        result.artifactDelivered++
        result.recorded++
        result.delivered++
      } else if (outcome === 'EXACT_RETRY') {
        result.artifactExactRetry++
        result.artifactDelivered++
        result.exactRetry++
        result.delivered++
      } else result.unknown++
    } catch (error) { result[classifyDatabaseError(error)]++ }
  }
  return result
}
