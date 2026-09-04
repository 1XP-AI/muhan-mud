import { parseManifest, type Manifest } from './manifest.js'
import { MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS, PLAYER_SNAPSHOT_V1_SUFFIX, commandFromPlayerSnapshotV1Filename, parsePlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import { MAX_MANIFEST_BYTES, isManifestFilename } from './manifest.js'
import { scanImmutableOutboxFiles } from './relay.js'
import { classifyDatabaseError, type PlayerSnapshotV1ArtifactStore } from './store.js'
import { PlayerSnapshotV1ReplayObserver, type PlayerSnapshotV1ReplayObserver as PlayerSnapshotV1ReplayObserverContract } from './player-snapshot-v1-replay-observer.js'

export interface PlayerSnapshotV1ArtifactRelaySummary {
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
  replayObserved: number
  replayDisabled: number
}

export interface PlayerSnapshotV1ArtifactFilesystem {
  scan(path: string): Promise<ReadonlyArray<{
    name: string
    bytes?: Uint8Array
    receiptManifestBytes?: Uint8Array
    error?: 'invalid' | 'io'
  }>>
}

function isPlayerSnapshotV1Filename(name: Uint8Array): boolean {
  const suffix = Buffer.from(PLAYER_SNAPSHOT_V1_SUFFIX)
  return name.length >= suffix.length && Buffer.from(name).subarray(-suffix.length).equals(suffix)
}

/**
 * Linux descriptor-rooted adapter for artifact/receipt pairs.  It reads both
 * immutable files without invoking the legacy relay or changing either file.
 */
export class NodePlayerSnapshotV1ArtifactFilesystem implements PlayerSnapshotV1ArtifactFilesystem {
  constructor(private readonly platform: NodeJS.Platform = process.platform) {}

  async scan(path: string): Promise<ReadonlyArray<{
    name: string
    bytes?: Uint8Array
    receiptManifestBytes?: Uint8Array
    error?: 'invalid' | 'io'
  }>> {
    const [receipts, artifacts] = await Promise.all([
      scanImmutableOutboxFiles(path, isManifestFilename, MAX_MANIFEST_BYTES, this.platform),
      scanImmutableOutboxFiles(path, isPlayerSnapshotV1Filename, MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS + 1, this.platform),
    ])
    const byName = new Map(receipts.map((file) => [file.name, file]))
    return artifacts.map((artifact) => {
      const commandId = commandFromPlayerSnapshotV1Filename(artifact.name)
      const receipt = commandId ? byName.get(`${commandId}.manifest`) : undefined
      if (artifact.error) return artifact
      if (!receipt || receipt.error || !receipt.bytes) return { name: artifact.name, bytes: artifact.bytes, error: receipt?.error ?? 'invalid' }
      return { name: artifact.name, bytes: artifact.bytes, receiptManifestBytes: receipt.bytes }
    })
  }
}

function summary(): PlayerSnapshotV1ArtifactRelaySummary {
  return {
    visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0,
    replayObserved: 0, replayDisabled: 0,
  }
}

/**
 * Relay header-wrapped .player-snapshot-v1 evidence using an artifact-aware
 * filesystem adapter. The paired legacy manifest is read only as canonical
 * receipt metadata; this path never calls the legacy receipt recorder or mutates files.
 */
export async function relayPlayerSnapshotV1ArtifactsOnce(
  outboxPath: string,
  store: PlayerSnapshotV1ArtifactStore,
  filesystem: PlayerSnapshotV1ArtifactFilesystem = new NodePlayerSnapshotV1ArtifactFilesystem(),
  replayObserver: PlayerSnapshotV1ReplayObserverContract = new PlayerSnapshotV1ReplayObserver(undefined),
): Promise<PlayerSnapshotV1ArtifactRelaySummary> {
  const result = summary()
  let files: ReadonlyArray<{ name: string, bytes?: Uint8Array, receiptManifestBytes?: Uint8Array, error?: 'invalid' | 'io' }>
  try { files = await filesystem.scan(outboxPath) }
  catch { result.ioError++; return result }
  result.visited = files.length
  const ordered = [...files].sort((left, right) => Buffer.compare(Buffer.from(left.name), Buffer.from(right.name)))
  for (const file of ordered) {
    if (file.error === 'invalid') { result.invalid++; continue }
    if (file.error === 'io' || !file.bytes || !file.receiptManifestBytes) { result.ioError++; continue }
    let receipt: Manifest
    try { receipt = parseManifest(file.receiptManifestBytes) }
    catch { result.invalid++; continue }
    let artifact
    try { artifact = parsePlayerSnapshotV1Artifact(file.name, file.bytes, receipt) }
    catch { result.invalid++; continue }
    result.valid++
    try {
      if (await replayObserver.observe(artifact.payload) === 'observed') result.replayObserved++
      else result.replayDisabled++
    } catch { result.replayDisabled++ }
    try {
      const outcome = await store.recordPlayerSnapshotV1Artifact(artifact)
      if (outcome === 'RECORDED') { result.recorded++; result.delivered++ }
      else if (outcome === 'EXACT_RETRY') { result.exactRetry++; result.delivered++ }
      else result.unknown++
    } catch (error) { result[classifyDatabaseError(error)]++ }
  }
  return result
}
