const SHA256_RE = /^[0-9a-f]{64}$/
const CANONICAL_IDENTIFIER_RE = /^[a-z0-9](?:[a-z0-9._:-]{0,254}[a-z0-9])?$/
const SEMVER_RE = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:(?:0|[1-9]\d*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:(?:0|[1-9]\d*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/

const INPUT_KEYS = [
  'worldId',
  'sourceManifestId',
  'sourceSha256',
  'sourceByteSize',
  'parserVersion',
  'abi',
  'startMarker',
  'endMarker',
] as const

/**
 * A closed, replayable description of exactly one source range. Identifier
 * values are canonical lowercase ASCII tokens so spelling variants cannot
 * create distinct ledger batches for the same reviewed source.
 */
export interface BatchIdentityInput {
  readonly worldId: string
  readonly sourceManifestId: string
  readonly sourceSha256: string
  readonly sourceByteSize: number
  readonly parserVersion: string
  readonly abi: number
  readonly startMarker: string
  readonly endMarker: string
}

export interface BatchIdentity extends BatchIdentityInput {
  /** Deterministic, field-ordered serialization of all identity components. */
  readonly canonicalSerialization: string
  /** Alias used as the durable identity key by later ledger integration. */
  readonly stableKey: string
}

/** The only observable validation detail; input values are never echoed. */
export class BatchIdentityError extends Error {
  readonly code = 'invalid_batch_identity'

  constructor() {
    super('invalid batch identity')
    this.name = 'BatchIdentityError'
  }
}

function invalid(): never {
  throw new BatchIdentityError()
}

function object(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function hasExactKeys(value: Record<string, unknown>): boolean {
  const keys = Object.keys(value)
  return keys.length === INPUT_KEYS.length
    && keys.every((key) => (INPUT_KEYS as readonly string[]).includes(key))
    && !Object.getOwnPropertySymbols(value).some(
      (key) => Object.prototype.propertyIsEnumerable.call(value, key),
    )
}

function canonicalIdentifier(value: unknown): value is string {
  return typeof value === 'string' && CANONICAL_IDENTIFIER_RE.test(value)
}

function canonicalParserVersion(value: unknown): value is string {
  return typeof value === 'string' && SEMVER_RE.test(value)
}

function safeNonNegativeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function positiveSafeInteger(value: unknown): value is number {
  return safeNonNegativeInteger(value) && value > 0
}

function canonicalSerialization(input: BatchIdentityInput): string {
  // The literal order is the serialization contract. All admitted strings are
  // ASCII-only, eliminating alternate Unicode normalization representations.
  return JSON.stringify({
    worldId: input.worldId,
    sourceManifestId: input.sourceManifestId,
    sourceSha256: input.sourceSha256,
    sourceByteSize: input.sourceByteSize,
    parserVersion: input.parserVersion,
    abi: input.abi,
    startMarker: input.startMarker,
    endMarker: input.endMarker,
  })
}

/**
 * Construct the immutable identity value for one importer ledger batch.
 * This is deliberately pure: it reads no source bytes, opens no files, and
 * performs no database or import-flow work.
 */
export function createBatchIdentity(value: unknown): BatchIdentity {
  const input = object(value)
  if (!input || !hasExactKeys(input)
    || !canonicalIdentifier(input.worldId)
    || !canonicalIdentifier(input.sourceManifestId)
    || typeof input.sourceSha256 !== 'string' || !SHA256_RE.test(input.sourceSha256)
    || !safeNonNegativeInteger(input.sourceByteSize)
    || !canonicalParserVersion(input.parserVersion)
    || !positiveSafeInteger(input.abi)
    || !canonicalIdentifier(input.startMarker)
    || !canonicalIdentifier(input.endMarker)
    || input.startMarker === input.endMarker) return invalid()

  const canonicalInput: BatchIdentityInput = {
    worldId: input.worldId,
    sourceManifestId: input.sourceManifestId,
    sourceSha256: input.sourceSha256,
    sourceByteSize: input.sourceByteSize,
    parserVersion: input.parserVersion,
    abi: input.abi,
    startMarker: input.startMarker,
    endMarker: input.endMarker,
  }
  const serialized = canonicalSerialization(canonicalInput)
  return Object.freeze({
    ...canonicalInput,
    canonicalSerialization: serialized,
    stableKey: serialized,
  })
}
