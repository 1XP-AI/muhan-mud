import { parseManifest, type Manifest } from './manifest.js'
import { MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS, PLAYER_SNAPSHOT_V1_SUFFIX, commandFromPlayerSnapshotV1Filename, parsePlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import { MAX_MANIFEST_BYTES, isManifestFilename } from './manifest.js'
import { scanImmutableOutboxFiles } from './relay.js'
import {
  classifyDatabaseError,
  type PlayerSnapshotV1ArtifactFulfillmentOutcome,
  type PlayerSnapshotV1ArtifactFulfillmentStore,
  type PlayerSnapshotV1ArtifactStore,
  type PlayerSnapshotV1LevelProjectionStore,
} from './store.js'
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
  projectionDelivered: number
  projectionRecorded: number
  projectionExactRetry: number
  projectionInvalid: number
  projectionConflict: number
  projectionRetryable: number
  projectionUnknown: number
  fulfillmentDelivered?: number
  fulfillmentFulfilled?: number
  fulfillmentExactRetry?: number
  fulfillmentAlreadyFulfilled?: number
  fulfillmentNotEligible?: number
  fulfillmentInvalid?: number
  fulfillmentConflict?: number
  fulfillmentRetryable?: number
  fulfillmentUnknown?: number
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
    projectionDelivered: 0, projectionRecorded: 0, projectionExactRetry: 0,
    projectionInvalid: 0, projectionConflict: 0, projectionRetryable: 0, projectionUnknown: 0,
  }
}

function withFulfillmentCounters(result: PlayerSnapshotV1ArtifactRelaySummary): void {
  result.fulfillmentDelivered = 0
  result.fulfillmentFulfilled = 0
  result.fulfillmentExactRetry = 0
  result.fulfillmentAlreadyFulfilled = 0
  result.fulfillmentNotEligible = 0
  result.fulfillmentInvalid = 0
  result.fulfillmentConflict = 0
  result.fulfillmentRetryable = 0
  result.fulfillmentUnknown = 0
}

function incrementFulfillmentOutcome(
  result: PlayerSnapshotV1ArtifactRelaySummary,
  outcome: PlayerSnapshotV1ArtifactFulfillmentOutcome,
): void {
  result.fulfillmentDelivered = (result.fulfillmentDelivered ?? 0) + 1
  if (outcome === 'FULFILLED') result.fulfillmentFulfilled = (result.fulfillmentFulfilled ?? 0) + 1
  else if (outcome === 'EXACT_RETRY') result.fulfillmentExactRetry = (result.fulfillmentExactRetry ?? 0) + 1
  else if (outcome === 'ALREADY_FULFILLED') result.fulfillmentAlreadyFulfilled = (result.fulfillmentAlreadyFulfilled ?? 0) + 1
  else result.fulfillmentNotEligible = (result.fulfillmentNotEligible ?? 0) + 1
}

function isFulfillmentOutcome(value: unknown): value is PlayerSnapshotV1ArtifactFulfillmentOutcome {
  return value === 'FULFILLED' || value === 'EXACT_RETRY'
    || value === 'ALREADY_FULFILLED' || value === 'NOT_ELIGIBLE'
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
  fulfillmentStoreOrProjection?: PlayerSnapshotV1ArtifactFulfillmentStore | PlayerSnapshotV1LevelProjectionStore,
  projectionStoreOrFulfillment?: PlayerSnapshotV1LevelProjectionStore | PlayerSnapshotV1ArtifactFulfillmentStore,
): Promise<PlayerSnapshotV1ArtifactRelaySummary> {
  const result = summary()
  // Accept either side-effect ordering so existing projection callers remain
  // source-compatible while fulfillment can be inserted before projection.
  let fulfillmentStore: PlayerSnapshotV1ArtifactFulfillmentStore | undefined
  let projectionStore: PlayerSnapshotV1LevelProjectionStore | undefined
  for (const sideEffect of [fulfillmentStoreOrProjection, projectionStoreOrFulfillment]) {
    if (!sideEffect) continue
    if ('fulfillGameCharacterOnboardingSnapshotEligibility' in sideEffect) fulfillmentStore = sideEffect
    if ('recordPlayerSnapshotV1LevelProjection' in sideEffect) projectionStore = sideEffect
  }
  if (fulfillmentStore) withFulfillmentCounters(result)
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
      if (await replayObserver.observe(artifact.payload, {
        commandId: artifact.commandId,
        characterId: artifact.characterId,
        receiptRequestSha256: artifact.receiptRequestSha256,
        sourcePostSha256: artifact.sourcePostSha256,
      }) === 'observed') result.replayObserved++
      else result.replayDisabled++
    } catch { result.replayDisabled++ }
    let artifactSettled = false
    try {
      const outcome = await store.recordPlayerSnapshotV1Artifact(artifact)
      if (outcome === 'RECORDED') { result.recorded++; result.delivered++; artifactSettled = true }
      else if (outcome === 'EXACT_RETRY') { result.exactRetry++; result.delivered++; artifactSettled = true }
      else result.unknown++
    } catch (error) { result[classifyDatabaseError(error)]++ }
    if (artifactSettled && fulfillmentStore) {
      try {
        const outcome = await fulfillmentStore.fulfillGameCharacterOnboardingSnapshotEligibility(
          artifact.characterId, artifact.commandId,
        )
        if (!isFulfillmentOutcome(outcome)) throw new Error('unexpected database fulfillment outcome')
        incrementFulfillmentOutcome(result, outcome)
      } catch (error) {
        const category = classifyDatabaseError(error)
        if (category === 'invalid') result.fulfillmentInvalid = (result.fulfillmentInvalid ?? 0) + 1
        else if (category === 'conflict') result.fulfillmentConflict = (result.fulfillmentConflict ?? 0) + 1
        else if (category === 'retryable') result.fulfillmentRetryable = (result.fulfillmentRetryable ?? 0) + 1
        else result.fulfillmentUnknown = (result.fulfillmentUnknown ?? 0) + 1
      }
    }
    // Immutable artifact evidence is authoritative; this projection is only a
    // best-effort migration-190 side effect after that evidence has settled.
    if (!artifactSettled || !projectionStore) continue
    try {
      const outcome = await projectionStore.recordPlayerSnapshotV1LevelProjection({
        characterId: artifact.characterId,
        commandId: artifact.commandId,
        receiptRequestSha256: artifact.receiptRequestSha256,
        sourcePostSha256: artifact.sourcePostSha256,
        sourceOctets: artifact.sourceOctets,
      })
      if (outcome === 'RECORDED') { result.projectionRecorded++; result.projectionDelivered++ }
      else if (outcome === 'EXACT_RETRY') { result.projectionExactRetry++; result.projectionDelivered++ }
      else result.projectionUnknown++
    } catch (error) {
      const category = classifyDatabaseError(error)
      if (category === 'invalid') result.projectionInvalid++
      else if (category === 'conflict') result.projectionConflict++
      else if (category === 'retryable') result.projectionRetryable++
      else result.projectionUnknown++
    }
  }
  return result
}
