import { parseManifest, type Manifest } from './manifest.js'
import { MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS, PLAYER_SNAPSHOT_V1_SUFFIX, commandFromPlayerSnapshotV1Filename, parsePlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import { MAX_MANIFEST_BYTES, isManifestFilename } from './manifest.js'
import { scanImmutableOutboxFilesWithPolicies } from './relay.js'
import {
  classifyDatabaseError,
  type PlayerSnapshotV1ArtifactFulfillmentOutcome,
  type PlayerSnapshotV1ArtifactFulfillmentStore,
  type PlayerSnapshotV1ArtifactStore,
  type PlayerSnapshotV1InventoryGraphShadowStore,
  type PlayerSnapshotV1LevelProjectionStore,
  type PlayerSnapshotNormalizedV1ProjectionStore,
} from './store.js'
import { PlayerSnapshotV1ReplayObserver, type PlayerSnapshotV1ReplayObserver as PlayerSnapshotV1ReplayObserverContract } from './player-snapshot-v1-replay-observer.js'
import type { PlayerSnapshotV1NormalizedProjection } from './player-snapshot-v1-normalized-projection.js'

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
  /** Present only when an enabled diagnostic verifier failed. */
  replayFailed?: number
  projectionDelivered: number
  projectionRecorded: number
  projectionExactRetry: number
  projectionInvalid: number
  projectionConflict: number
  projectionRetryable: number
  projectionUnknown: number
  normalizedProjectionDelivered?: number
  normalizedProjectionRecorded?: number
  normalizedProjectionExactRetry?: number
  normalizedProjectionInvalid?: number
  normalizedProjectionConflict?: number
  normalizedProjectionRetryable?: number
  normalizedProjectionUnknown?: number
  inventoryGraphShadowDelivered?: number
  inventoryGraphShadowRecorded?: number
  inventoryGraphShadowExactRetry?: number
  inventoryGraphShadowInvalid?: number
  inventoryGraphShadowConflict?: number
  inventoryGraphShadowRetryable?: number
  inventoryGraphShadowUnknown?: number
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

/**
 * Explicit default-off persistence seam. The projector may read the immutable
 * artifact, but the store only receives the already-validated numeric allowlist.
 */
export interface PlayerSnapshotV1NormalizedProjectionPersistence {
  project(payload: Uint8Array, snapshotSha256: string): Promise<PlayerSnapshotV1NormalizedProjection>
  store: PlayerSnapshotNormalizedV1ProjectionStore
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
    // Both members of every pair are read through this one root descriptor.
    // Reopening `path` for receipts and artifacts would allow a root rename to
    // splice together evidence that was independently verified under two roots.
    const scanned = await scanImmutableOutboxFilesWithPolicies(path, [
      { isCandidateFilename: isManifestFilename, maximumBytes: MAX_MANIFEST_BYTES },
      { isCandidateFilename: isPlayerSnapshotV1Filename, maximumBytes: MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS + 1 },
    ], this.platform)
    const receipts = scanned.filter((file) => isManifestFilename(Buffer.from(file.name)))
    const artifacts = scanned.filter((file) => isPlayerSnapshotV1Filename(Buffer.from(file.name)))
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

function withNormalizedProjectionCounters(result: PlayerSnapshotV1ArtifactRelaySummary): void {
  result.normalizedProjectionDelivered = 0
  result.normalizedProjectionRecorded = 0
  result.normalizedProjectionExactRetry = 0
  result.normalizedProjectionInvalid = 0
  result.normalizedProjectionConflict = 0
  result.normalizedProjectionRetryable = 0
  result.normalizedProjectionUnknown = 0
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

function withInventoryGraphShadowCounters(result: PlayerSnapshotV1ArtifactRelaySummary): void {
  result.inventoryGraphShadowDelivered = 0
  result.inventoryGraphShadowRecorded = 0
  result.inventoryGraphShadowExactRetry = 0
  result.inventoryGraphShadowInvalid = 0
  result.inventoryGraphShadowConflict = 0
  result.inventoryGraphShadowRetryable = 0
  result.inventoryGraphShadowUnknown = 0
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
  normalizedProjectionPersistence?: PlayerSnapshotV1NormalizedProjectionPersistence,
  inventoryGraphShadowStore?: PlayerSnapshotV1InventoryGraphShadowStore,
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
  if (normalizedProjectionPersistence) withNormalizedProjectionCounters(result)
  if (inventoryGraphShadowStore) withInventoryGraphShadowCounters(result)
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
      const replayOutcome = await replayObserver.observe(artifact.payload, {
        commandId: artifact.commandId,
        characterId: artifact.characterId,
        receiptRequestSha256: artifact.receiptRequestSha256,
        sourcePostSha256: artifact.sourcePostSha256,
        snapshotSha256: artifact.snapshotSha256,
      })
      if (replayOutcome === 'observed') result.replayObserved++
      else if (replayOutcome === 'failed') result.replayFailed = (result.replayFailed ?? 0) + 1
      else result.replayDisabled++
    } catch { result.replayFailed = (result.replayFailed ?? 0) + 1 }
    let artifactSettled = false
    try {
      const outcome = await store.recordPlayerSnapshotV1Artifact(artifact)
      if (outcome === 'RECORDED') { result.recorded++; result.delivered++; artifactSettled = true }
      else if (outcome === 'EXACT_RETRY') { result.exactRetry++; result.delivered++; artifactSettled = true }
      else result.unknown++
    } catch (error) { result[classifyDatabaseError(error)]++ }
    // Migration 20261003000000 is an explicit, default-off shadow writer. It
    // receives only settled immutable receipt/artifact metadata; the database
    // function resolves the durable artifact evidence itself.
    if (artifactSettled && inventoryGraphShadowStore) {
      try {
        const outcome = await inventoryGraphShadowStore.recordPlayerSnapshotV1InventoryGraphShadow({
          characterId: artifact.characterId,
          commandId: artifact.commandId,
          receiptRequestSha256: artifact.receiptRequestSha256,
          sourcePostSha256: artifact.sourcePostSha256,
          sourceOctets: artifact.sourceOctets,
        })
        if (outcome === 'RECORDED') {
          result.inventoryGraphShadowRecorded = (result.inventoryGraphShadowRecorded ?? 0) + 1
          result.inventoryGraphShadowDelivered = (result.inventoryGraphShadowDelivered ?? 0) + 1
        } else if (outcome === 'EXACT_RETRY') {
          result.inventoryGraphShadowExactRetry = (result.inventoryGraphShadowExactRetry ?? 0) + 1
          result.inventoryGraphShadowDelivered = (result.inventoryGraphShadowDelivered ?? 0) + 1
        } else result.inventoryGraphShadowUnknown = (result.inventoryGraphShadowUnknown ?? 0) + 1
      } catch (error) {
        const category = classifyDatabaseError(error)
        if (category === 'invalid') result.inventoryGraphShadowInvalid = (result.inventoryGraphShadowInvalid ?? 0) + 1
        else if (category === 'conflict') result.inventoryGraphShadowConflict = (result.inventoryGraphShadowConflict ?? 0) + 1
        else if (category === 'retryable') result.inventoryGraphShadowRetryable = (result.inventoryGraphShadowRetryable ?? 0) + 1
        else result.inventoryGraphShadowUnknown = (result.inventoryGraphShadowUnknown ?? 0) + 1
      }
    }
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
    if (artifactSettled && projectionStore) {
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
    // Migration 20261006000000 is an explicit, default-off projection side
    // effect. Its database adapter receives neither this payload nor the raw
    // runner text: only the adapter-validated numeric projection below.
    if (!artifactSettled || !normalizedProjectionPersistence) continue
    let normalizedProjection: PlayerSnapshotV1NormalizedProjection
    try {
      normalizedProjection = await normalizedProjectionPersistence.project(artifact.payload, artifact.snapshotSha256)
    } catch {
      result.normalizedProjectionInvalid = (result.normalizedProjectionInvalid ?? 0) + 1
      continue
    }
    try {
      const outcome = await normalizedProjectionPersistence.store.recordPlayerSnapshotNormalizedV1Projection({
        characterId: artifact.characterId,
        commandId: artifact.commandId,
        receiptRequestSha256: artifact.receiptRequestSha256,
        sourcePostSha256: artifact.sourcePostSha256,
        sourceOctets: artifact.sourceOctets,
        projection: normalizedProjection,
      })
      if (outcome === 'RECORDED') { result.normalizedProjectionRecorded = (result.normalizedProjectionRecorded ?? 0) + 1; result.normalizedProjectionDelivered = (result.normalizedProjectionDelivered ?? 0) + 1 }
      else if (outcome === 'EXACT_RETRY') { result.normalizedProjectionExactRetry = (result.normalizedProjectionExactRetry ?? 0) + 1; result.normalizedProjectionDelivered = (result.normalizedProjectionDelivered ?? 0) + 1 }
      else result.normalizedProjectionUnknown = (result.normalizedProjectionUnknown ?? 0) + 1
    } catch (error) {
      const category = classifyDatabaseError(error)
      if (category === 'invalid') result.normalizedProjectionInvalid = (result.normalizedProjectionInvalid ?? 0) + 1
      else if (category === 'conflict') result.normalizedProjectionConflict = (result.normalizedProjectionConflict ?? 0) + 1
      else if (category === 'retryable') result.normalizedProjectionRetryable = (result.normalizedProjectionRetryable ?? 0) + 1
      else result.normalizedProjectionUnknown = (result.normalizedProjectionUnknown ?? 0) + 1
    }
  }
  return result
}
