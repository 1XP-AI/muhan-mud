import type { PlayerSnapshotV1ReplayLevelDifferentialInput } from './player-snapshot-v1-replay-differential.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/
const LEVEL_INPUT_FIELDS = [
  'characterId', 'commandId', 'rawLevelU8', 'receiptRequestSha256', 'snapshotOctets', 'snapshotSha256', 'sourcePostSha256',
]

/** The closed metadata emitted by the v2 journal level-input reader. */
export type PlayerSnapshotV2JournalLevelInput = PlayerSnapshotV1ReplayLevelDifferentialInput

/** Immutable, payload-free projection evidence supplied by an injected read boundary. */
export interface ImmutablePlayerSnapshotLevelProjectionEvidence {
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  snapshotSha256: string
  snapshotOctets: number
  rawLevelU8: number
}

/** Read-only injection point; this decision contract owns no database adapter. */
export interface ImmutablePlayerSnapshotLevelProjectionReader {
  findByCommandId(commandId: string): Promise<readonly unknown[]>
}

export type PlayerSnapshotV2JournalLevelComparison =
  | 'MATCH'
  | 'MISMATCH_LEVEL'
  | 'MISSING_PROJECTION'
  | 'IDENTITY_MISMATCH'
  | 'INVALID_INPUT'
  | 'UNEXPECTED_DUPLICATE'
  | 'PROJECTION_READ_ERROR'

function hasExactlyKeys(value: Record<string, unknown>): boolean {
  const actual = Object.keys(value).sort()
  return actual.length === LEVEL_INPUT_FIELDS.length && actual.every((key, index) => key === LEVEL_INPUT_FIELDS[index])
}

function isRawLevelU8(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= 255
}

function isClosedMetadata(value: unknown): value is ImmutablePlayerSnapshotLevelProjectionEvidence {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const input = value as Record<string, unknown>
  return hasExactlyKeys(input)
    && typeof input.commandId === 'string' && UUID_RE.test(input.commandId)
    && typeof input.characterId === 'string' && UUID_RE.test(input.characterId)
    && typeof input.receiptRequestSha256 === 'string' && HASH_RE.test(input.receiptRequestSha256)
    && typeof input.sourcePostSha256 === 'string' && HASH_RE.test(input.sourcePostSha256)
    && typeof input.snapshotSha256 === 'string' && HASH_RE.test(input.snapshotSha256)
    && typeof input.snapshotOctets === 'number' && Number.isSafeInteger(input.snapshotOctets) && input.snapshotOctets >= 0
    && isRawLevelU8(input.rawLevelU8)
}

function hasMatchingIdentity(
  journal: PlayerSnapshotV2JournalLevelInput,
  projection: ImmutablePlayerSnapshotLevelProjectionEvidence,
): boolean {
  return projection.characterId === journal.characterId
    && projection.commandId === journal.commandId
    && projection.receiptRequestSha256 === journal.receiptRequestSha256
    && projection.sourcePostSha256 === journal.sourcePostSha256
    && projection.snapshotSha256 === journal.snapshotSha256
    && projection.snapshotOctets === journal.snapshotOctets
}

/**
 * Compares only v2 closed journal metadata with injected immutable projection
 * metadata. It deliberately neither reads payloads/files nor activates a
 * database, and identity must match before the raw U8 values are compared.
 */
export async function comparePlayerSnapshotV2JournalLevel(
  journal: unknown,
  reader: ImmutablePlayerSnapshotLevelProjectionReader,
): Promise<PlayerSnapshotV2JournalLevelComparison> {
  if (!isClosedMetadata(journal)) return 'INVALID_INPUT'

  let projections: readonly unknown[]
  try {
    projections = await reader.findByCommandId(journal.commandId)
  } catch {
    return 'PROJECTION_READ_ERROR'
  }

  if (!Array.isArray(projections)) return 'PROJECTION_READ_ERROR'
  if (projections.length === 0) return 'MISSING_PROJECTION'
  if (projections.length !== 1) return 'UNEXPECTED_DUPLICATE'
  const projection = projections[0]
  if (!isClosedMetadata(projection)) return 'INVALID_INPUT'
  if (!hasMatchingIdentity(journal, projection)) return 'IDENTITY_MISMATCH'
  return journal.rawLevelU8 === projection.rawLevelU8 ? 'MATCH' : 'MISMATCH_LEVEL'
}
