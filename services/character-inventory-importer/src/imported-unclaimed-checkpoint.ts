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

function closedDataObject(value: unknown, expectedKeys: readonly string[]): Record<string, unknown> | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  try {
    const actualKeys = Reflect.ownKeys(value)
    if (actualKeys.length !== expectedKeys.length || !expectedKeys.every((key) => actualKeys.includes(key))) return undefined

    const result: Record<string, unknown> = {}
    for (const key of expectedKeys) {
      const descriptor = Object.getOwnPropertyDescriptor(value, key)
      if (!descriptor || !('value' in descriptor)) return undefined
      result[key] = descriptor.value
    }
    return result
  } catch {
    return undefined
  }
}

function normalizeCandidate(candidate: unknown): ImportedUnclaimedManifestCandidate {
  const value = closedDataObject(candidate, ['legacyNameKey', 'legacyShard', 'sourceSha256', 'sourceSize'])
  if (!value || typeof value.legacyNameKey !== 'string' || typeof value.legacyShard !== 'string'
    || typeof value.sourceSha256 !== 'string' || typeof value.sourceSize !== 'number'
    || !Number.isSafeInteger(value.sourceSize)) return invalid()

  const probe: InventoryRecord = {
    name: value.legacyNameKey,
    canonicalNameKey: value.legacyNameKey,
    relativePath: `player/${value.legacyShard}/${value.legacyNameKey}`,
    observedShard: value.legacyShard,
    expectedShard: value.legacyShard,
    byteSize: value.sourceSize,
    sha256: value.sourceSha256,
  }
  if (value.legacyNameKey !== canonicalNameKey(value.legacyNameKey) || !validRecord(probe)) return invalid()

  return Object.freeze({
    legacyNameKey: value.legacyNameKey,
    legacyShard: value.legacyShard,
    sourceSha256: value.sourceSha256,
    sourceSize: value.sourceSize,
  })
}

/**
 * Public callers can structurally forge a TypeScript plan, so this boundary
 * verifies every closed candidate and preserves (rather than repairs) order.
 */
function canonicalizeImportedUnclaimedCheckpointPlan(
  plan: ImportedUnclaimedCheckpointPlan,
): ImportedUnclaimedCheckpointPlan {
  const value = closedDataObject(plan, ['sourceManifestSha256', 'candidates'])
  if (!value || typeof value.sourceManifestSha256 !== 'string' || !SHA256_RE.test(value.sourceManifestSha256)
    || !Array.isArray(value.candidates)) return invalid()

  const candidates: ImportedUnclaimedManifestCandidate[] = []
  const identities = new Set<string>()
  for (const candidate of value.candidates) {
    const normalized = normalizeCandidate(candidate)
    if (identities.has(normalized.legacyNameKey)
      || (candidates.length > 0 && compareCodePoints(candidates[candidates.length - 1]!.legacyNameKey, normalized.legacyNameKey) >= 0)) return invalid()
    identities.add(normalized.legacyNameKey)
    candidates.push(normalized)
  }

  return Object.freeze({
    sourceManifestSha256: value.sourceManifestSha256,
    candidates: Object.freeze(candidates),
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
  if (typeof legacyNameKey !== 'string') return invalid()
  const canonicalPlan = canonicalizeImportedUnclaimedCheckpointPlan(plan)
  const cursor = canonicalPlan.candidates.findIndex((candidate) => candidate.legacyNameKey === legacyNameKey)
  if (cursor < 0) return invalid()
  return Object.freeze({
    sourceManifestSha256: canonicalPlan.sourceManifestSha256,
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
  const canonicalPlan = canonicalizeImportedUnclaimedCheckpointPlan(plan)
  if (checkpoint === undefined) return canonicalPlan.candidates
  if (typeof checkpoint !== 'object' || checkpoint === null) return invalid()
  if (checkpoint.sourceManifestSha256 !== canonicalPlan.sourceManifestSha256
    || !Number.isSafeInteger(checkpoint.cursor) || checkpoint.cursor < 0
    || checkpoint.cursor >= canonicalPlan.candidates.length
    || typeof checkpoint.legacyNameKey !== 'string') return invalid()

  const persisted = canonicalPlan.candidates[checkpoint.cursor]
  if (!persisted || persisted.legacyNameKey !== checkpoint.legacyNameKey) return invalid()
  return canonicalPlan.candidates.slice(checkpoint.cursor + 1)
}
