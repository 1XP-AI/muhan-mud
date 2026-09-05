import { canonicalNameKey, SHA256_RE, validRecord, type InventoryRecord } from './inventory.js'
import type {
  ImportedUnclaimedManifestCandidate,
  ParsedImportedUnclaimedManifest,
} from './imported-unclaimed-manifest.js'

export interface ImportedUnclaimedCheckpoint {
  /** SHA-256 of the exact raw manifest bytes, as admitted by the parser. */
  readonly sourceManifestSha256: string
  /** Zero-based position of the last completed canonical identity. */
  readonly cursor: number
  /** The identity at cursor; it prevents an ordinal from being reused ambiguously. */
  readonly legacyNameKey: string
}

export interface ImportedUnclaimedCheckpointPlan {
  readonly sourceManifestSha256: string
  readonly candidates: readonly ImportedUnclaimedManifestCandidate[]
}

/** Checkpoints are local operator state only; this module does not persist them. */
export class ImportedUnclaimedCheckpointError extends Error {
  readonly code = 'invalid_imported_unclaimed_checkpoint'

  constructor() {
    super('invalid imported-unclaimed checkpoint')
    this.name = 'ImportedUnclaimedCheckpointError'
  }
}

function invalid(): never {
  throw new ImportedUnclaimedCheckpointError()
}

function compareCodePoints(left: string, right: string): number {
  const leftPoints = Array.from(left)
  const rightPoints = Array.from(right)
  for (let index = 0; index < leftPoints.length && index < rightPoints.length; index++) {
    const difference = leftPoints[index]!.codePointAt(0)! - rightPoints[index]!.codePointAt(0)!
    if (difference !== 0) return difference
  }
  return leftPoints.length - rightPoints.length
}

function normalizeCandidate(candidate: ImportedUnclaimedManifestCandidate): ImportedUnclaimedManifestCandidate {
  if (!candidate || typeof candidate.legacyNameKey !== 'string' || typeof candidate.legacyShard !== 'string'
    || typeof candidate.sourceSha256 !== 'string' || typeof candidate.sourceSize !== 'number'
    || !Number.isSafeInteger(candidate.sourceSize)) return invalid()

  const probe: InventoryRecord = {
    name: candidate.legacyNameKey,
    canonicalNameKey: candidate.legacyNameKey,
    relativePath: `player/${candidate.legacyShard}/${candidate.legacyNameKey}`,
    observedShard: candidate.legacyShard,
    expectedShard: candidate.legacyShard,
    byteSize: candidate.sourceSize,
    sha256: candidate.sourceSha256,
  }
  if (candidate.legacyNameKey !== canonicalNameKey(candidate.legacyNameKey) || !validRecord(probe)) return invalid()

  return Object.freeze({
    legacyNameKey: candidate.legacyNameKey,
    legacyShard: candidate.legacyShard,
    sourceSha256: candidate.sourceSha256,
    sourceSize: candidate.sourceSize,
  })
}

/**
 * Normalizes parser-admitted candidates into one deterministic canonical
 * identity order. Duplicate identities are rejected before a cursor exists.
 */
export function createImportedUnclaimedCheckpointPlan(
  manifest: ParsedImportedUnclaimedManifest,
): ImportedUnclaimedCheckpointPlan {
  if (!manifest || !SHA256_RE.test(manifest.sourceManifestSha256) || !Array.isArray(manifest.candidates)) return invalid()

  const candidates = manifest.candidates.map(normalizeCandidate)
  candidates.sort((left, right) => compareCodePoints(left.legacyNameKey, right.legacyNameKey))
  for (let index = 1; index < candidates.length; index++) {
    if (candidates[index - 1]!.legacyNameKey === candidates[index]!.legacyNameKey) return invalid()
  }

  return Object.freeze({
    sourceManifestSha256: manifest.sourceManifestSha256,
    candidates: Object.freeze(candidates),
  })
}

/** Create a durable cursor only for an identity in this exact ordered plan. */
export function checkpointImportedUnclaimedCandidate(
  plan: ImportedUnclaimedCheckpointPlan,
  legacyNameKey: string,
): ImportedUnclaimedCheckpoint {
  if (!plan || typeof legacyNameKey !== 'string') return invalid()
  const cursor = plan.candidates.findIndex((candidate) => candidate.legacyNameKey === legacyNameKey)
  if (cursor < 0) return invalid()
  return Object.freeze({
    sourceManifestSha256: plan.sourceManifestSha256,
    cursor,
    legacyNameKey,
  })
}

/**
 * Return only the candidates strictly after a verified persisted cursor.
 * Undefined checkpoints deliberately begin at the first ordered candidate.
 */
export function resumeImportedUnclaimedCandidates(
  plan: ImportedUnclaimedCheckpointPlan,
  checkpoint?: ImportedUnclaimedCheckpoint,
): readonly ImportedUnclaimedManifestCandidate[] {
  if (!plan || !SHA256_RE.test(plan.sourceManifestSha256) || !Array.isArray(plan.candidates)) return invalid()
  if (!checkpoint) return plan.candidates
  if (checkpoint.sourceManifestSha256 !== plan.sourceManifestSha256
    || !Number.isSafeInteger(checkpoint.cursor) || checkpoint.cursor < 0
    || checkpoint.cursor >= plan.candidates.length
    || typeof checkpoint.legacyNameKey !== 'string') return invalid()

  const persisted = plan.candidates[checkpoint.cursor]
  if (!persisted || persisted.legacyNameKey !== checkpoint.legacyNameKey) return invalid()
  return plan.candidates.slice(checkpoint.cursor + 1)
}
