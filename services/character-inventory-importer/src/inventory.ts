import { createHash } from 'node:crypto'
import { createBatchIdentity, isCanonicalBatchIdentifier, type BatchIdentity } from './batch-identity.js'

export const SHA256_RE = /^[0-9a-f]{64}$/
const SHA1_SHARD_RE = /^[0-9a-f]{2}$/
const MAX_NAME_CODEPOINTS = 12
const MAX_NAME_BYTES = 14
const MAX_WORLD_CODEPOINTS = 64
const MAX_FILE_BYTES = 64 * 1024 * 1024

export interface InventoryRecord {
  name: string
  canonicalNameKey: string
  relativePath: string
  observedShard: string
  expectedShard: string
  byteSize: number
  sha256: string
}

export type QuarantineReason =
  | 'invalid_metadata'
  | 'duplicate_input_identity'
  | 'owned_row'
  | 'lifecycle_conflict'
  | 'hash_conflict'
  | 'identity_conflict'

export interface QuarantinedRecord {
  reason: QuarantineReason
}

export interface ExistingCharacter {
  legacyName: string
  legacyNameKey: string
  legacyShard: string
  importedFileSha256: string | null
  lifecycle: string
  ownerUserId: string | null
  storageFormat: number
}

export interface ImportTransaction {
  lockIdentity(worldId: string, legacyNameKey: string): Promise<void>
  findCharacter(worldId: string, legacyNameKey: string): Promise<ExistingCharacter | undefined>
  /** Returns the opaque UUID assigned to this newly inserted character only. */
  insertImportedUnclaimed(input: { worldId: string, record: InventoryRecord }): Promise<string>
  lockBatchStream(worldId: string, streamId: string): Promise<void>
  findBatchBySequence(worldId: string, streamId: string, sequence: number): Promise<LedgerBatch | undefined>
  findBatchByIdentity(worldId: string, streamId: string, stableKey: string): Promise<LedgerBatch | undefined>
  createBatch(input: { identity: BatchIdentity, streamId: string, sequence: number, recordCount: number }): Promise<void>
  /** Appends a permanent, private link from a new character to its creating batch. */
  recordBatchMember(input: {
    worldId: string, streamId: string, sequence: number, characterId: string
    /** Optional additive evidence for a newly linked member; no player file digest belongs here. */
    legacyLocator?: { canonicalName: string, legacyNameSha1: string, legacyShard: string }
  }): Promise<void>
  readWatermark(worldId: string, streamId: string): Promise<number | undefined>
  advanceWatermark(worldId: string, streamId: string, sequence: number): Promise<void>
}

export interface ImportStore {
  transaction<T>(work: (transaction: ImportTransaction) => Promise<T>): Promise<T>
}

export interface ImportOptions {
  worldId: string
  apply: boolean
}

export interface ImportSummary {
  wouldInsert: number
  inserted: number
  idempotent: number
  quarantined: Record<QuarantineReason, number>
}

/** Immutable, committed ledger evidence returned by the transactional store. */
export interface LedgerBatch {
  stableKey: string
  sequence: number
  recordCount: number
}

export interface BatchImportOptions {
  /** Must be constructed with createBatchIdentity; arbitrary source paths are never identity. */
  identity: BatchIdentity
  streamId: string
  sequence: number
  apply: boolean
}

export interface BatchImportSummary extends ImportSummary {
  streamId: string
  sequence: number
  ledger: 'dry_run' | 'committed' | 'idempotent'
}

/** Deliberately non-specific errors: rejected identity/sequence values are not logged. */
export class BatchImportError extends Error {
  constructor(readonly code: 'batch_sequence_identity_conflict' | 'batch_identity_sequence_conflict' | 'batch_sequence_out_of_order') {
    super('invalid batch import')
    this.name = 'BatchImportError'
  }
}

const initialSummary = (): ImportSummary => ({
  wouldInsert: 0,
  inserted: 0,
  idempotent: 0,
  quarantined: {
    invalid_metadata: 0,
    duplicate_input_identity: 0,
    owned_row: 0,
    lifecycle_conflict: 0,
    hash_conflict: 0,
    identity_conflict: 0,
  },
})

function validateBatchOptions(options: BatchImportOptions): BatchImportOptions {
  // Reconstruct from the public fields instead of trusting a caller-provided
  // stableKey. The CLI supplies this value directly from createBatchIdentity.
  const identity = createBatchIdentity({
    worldId: options.identity.worldId,
    sourceManifestId: options.identity.sourceManifestId,
    sourceSha256: options.identity.sourceSha256,
    sourceByteSize: options.identity.sourceByteSize,
    parserVersion: options.identity.parserVersion,
    abi: options.identity.abi,
    startMarker: options.identity.startMarker,
    endMarker: options.identity.endMarker,
  })
  if (!isCanonicalBatchIdentifier(options.streamId)
    || !Number.isSafeInteger(options.sequence) || options.sequence < 0) {
    throw new Error('invalid import configuration')
  }
  return { ...options, identity }
}

/** Match the C lowercize(name, 1) behavior used by the database constraints. */
export function canonicalNameKey(name: string): string {
  let folded = ''
  for (const character of name) {
    folded += character >= 'A' && character <= 'Z' ? String.fromCharCode(character.charCodeAt(0) + 32) : character
  }
  const first = folded[0]
  return first !== undefined && first >= 'a' && first <= 'z'
    ? String.fromCharCode(first.charCodeAt(0) - 32) + folded.slice(1)
    : folded
}

export function expectedShard(name: string): string {
  return createHash('sha1').update(canonicalNameKey(name), 'utf8').digest('hex').slice(0, 2)
}

export function validateWorldId(worldId: string): boolean {
  return !hasUnpairedSurrogate(worldId)
    && Array.from(worldId).length <= MAX_WORLD_CODEPOINTS && worldId.length >= 1 && !/[\x00-\x1f\x7f]/.test(worldId)
}

function hasUnpairedSurrogate(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index)
    if (code >= 0xd800 && code <= 0xdbff) {
      if (index + 1 >= value.length || value.charCodeAt(index + 1) < 0xdc00 || value.charCodeAt(index + 1) > 0xdfff) return true
      index++
    } else if (code >= 0xdc00 && code <= 0xdfff) return true
  }
  return false
}

function validName(name: string): boolean {
  return !hasUnpairedSurrogate(name)
    && Array.from(name).length >= 1
    && Array.from(name).length <= MAX_NAME_CODEPOINTS
    && Buffer.byteLength(name, 'utf8') <= MAX_NAME_BYTES
    && name !== '.' && name !== '..'
    && !/[\x00-\x1f\x7f/\\:]/.test(name)
}

function object(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

/**
 * Accept only one `status: ok` line produced by export-player-inventory.py.
 * The allowlist intentionally excludes raw player data, passwords, and opaque payload fields.
 */
export function parseInventoryLine(line: string): InventoryRecord | undefined {
  let parsed: unknown
  try { parsed = JSON.parse(line) } catch { return undefined }
  const value = object(parsed)
  if (!value) return undefined
  const allowed = new Set(['name', 'canonical_name_key', 'name_not_canonical', 'relative_path', 'observed_shard', 'expected_shard', 'shard_match', 'valid_name', 'byte_size', 'sha256', 'status', 'duplicate_name'])
  if (Object.keys(value).some((key) => !allowed.has(key))) return undefined
  if (value.status !== 'ok' || value.valid_name !== true || value.name_not_canonical !== false || value.shard_match !== true || value.duplicate_name !== false) return undefined
  if (typeof value.name !== 'string' || typeof value.canonical_name_key !== 'string' || typeof value.relative_path !== 'string'
    || typeof value.observed_shard !== 'string' || typeof value.expected_shard !== 'string' || typeof value.sha256 !== 'string'
    || typeof value.byte_size !== 'number' || !Number.isSafeInteger(value.byte_size)) return undefined
  const record: InventoryRecord = {
    name: value.name,
    canonicalNameKey: value.canonical_name_key,
    relativePath: value.relative_path,
    observedShard: value.observed_shard,
    expectedShard: value.expected_shard,
    byteSize: value.byte_size,
    sha256: value.sha256,
  }
  return validRecord(record) ? record : undefined
}

export function validRecord(record: InventoryRecord): boolean {
  return validName(record.name)
    && record.name === canonicalNameKey(record.name)
    && record.canonicalNameKey === record.name
    && SHA1_SHARD_RE.test(record.observedShard)
    && record.expectedShard === record.observedShard
    && record.expectedShard === expectedShard(record.name)
    && record.relativePath === `player/${record.observedShard}/${record.name}`
    && record.byteSize >= 0 && record.byteSize <= MAX_FILE_BYTES
    && SHA256_RE.test(record.sha256)
}

function sameImportedIdentity(record: InventoryRecord, existing: ExistingCharacter): QuarantineReason | undefined {
  if (existing.ownerUserId !== null) return 'owned_row'
  if (existing.lifecycle !== 'imported_unclaimed') return 'lifecycle_conflict'
  if (existing.importedFileSha256 !== record.sha256) return 'hash_conflict'
  if (existing.legacyName !== record.name || existing.legacyNameKey !== record.canonicalNameKey || existing.legacyShard !== record.expectedShard || existing.storageFormat !== 1) return 'identity_conflict'
  return undefined
}

function legacyNameSha1(canonicalName: string): string {
  return createHash('sha1').update(canonicalName, 'utf8').digest('hex')
}

/**
 * Legacy non-ledger import: it has no batch evidence or watermark behavior.
 * Every candidate is locked and inspected before insertion; this routine
 * never updates or deletes a row.
 */
export async function importRecords(store: ImportStore, records: readonly InventoryRecord[], options: ImportOptions, initialRejected = 0): Promise<ImportSummary> {
  if (!validateWorldId(options.worldId)) throw new Error('invalid import configuration')
  const summary = initialSummary()
  summary.quarantined.invalid_metadata = initialRejected
  const candidates: InventoryRecord[] = []
  for (const record of records) {
    if (!validRecord(record)) summary.quarantined.invalid_metadata++
    else candidates.push(record)
  }
  const duplicateKeys = new Set<string>()
  const seenKeys = new Set<string>()
  for (const candidate of candidates) {
    if (seenKeys.has(candidate.canonicalNameKey)) duplicateKeys.add(candidate.canonicalNameKey)
    seenKeys.add(candidate.canonicalNameKey)
  }
  for (const record of candidates) {
    if (duplicateKeys.has(record.canonicalNameKey)) {
      summary.quarantined.duplicate_input_identity++
    }
  }
  // Metadata is a batch admission boundary: malformed or ambiguous input must
  // never produce a partial backfill, even when other rows look valid.
  if (summary.quarantined.invalid_metadata > 0 || summary.quarantined.duplicate_input_identity > 0) return summary

  const ordered = [...candidates].sort((left, right) => left.canonicalNameKey < right.canonicalNameKey ? -1 : left.canonicalNameKey > right.canonicalNameKey ? 1 : 0)
  const committed = await store.transaction(async (transaction) => {
    // Keep counters local to this transaction attempt. PostgreSQL may retry
    // the whole callback after a serialization failure; merging only after a
    // successful commit prevents rolled-back work from being counted twice.
    const attemptSummary = initialSummary()
    // A stable global order prevents two importer processes from deadlocking
    // while preserving one atomic commit for the whole reviewed inventory.
    for (const record of ordered) await transaction.lockIdentity(options.worldId, record.canonicalNameKey)

    const absent: InventoryRecord[] = []
    for (const record of ordered) {
      const existing = await transaction.findCharacter(options.worldId, record.canonicalNameKey)
      if (!existing) {
        absent.push(record)
        continue
      }
      const reason = sameImportedIdentity(record, existing)
      if (reason) attemptSummary.quarantined[reason]++
      else attemptSummary.idempotent++
    }

    // Database drift discovered after operator review is also fail-closed. No
    // absent row is inserted alongside an owned, changed, or incompatible row.
    if (Object.values(attemptSummary.quarantined).some((count) => count > 0)) return attemptSummary
    if (!options.apply) {
      attemptSummary.wouldInsert = absent.length
      return attemptSummary
    }
    for (const record of absent) {
      await transaction.insertImportedUnclaimed({ worldId: options.worldId, record })
      attemptSummary.inserted++
    }
    return attemptSummary
  })
  summary.wouldInsert += committed.wouldInsert
  summary.inserted += committed.inserted
  summary.idempotent += committed.idempotent
  for (const reason of Object.keys(committed.quarantined) as QuarantineReason[]) {
    summary.quarantined[reason] += committed.quarantined[reason]
  }
  return summary
}

function admissionSummary(records: readonly InventoryRecord[], initialRejected: number): { summary: ImportSummary, ordered: InventoryRecord[] } {
  const summary = initialSummary()
  summary.quarantined.invalid_metadata = initialRejected
  const candidates: InventoryRecord[] = []
  for (const record of records) {
    if (!validRecord(record)) summary.quarantined.invalid_metadata++
    else candidates.push(record)
  }
  const duplicateKeys = new Set<string>()
  const seenKeys = new Set<string>()
  for (const candidate of candidates) {
    if (seenKeys.has(candidate.canonicalNameKey)) duplicateKeys.add(candidate.canonicalNameKey)
    seenKeys.add(candidate.canonicalNameKey)
  }
  for (const record of candidates) if (duplicateKeys.has(record.canonicalNameKey)) summary.quarantined.duplicate_input_identity++
  return {
    summary,
    ordered: [...candidates].sort((left, right) => left.canonicalNameKey < right.canonicalNameKey ? -1 : left.canonicalNameKey > right.canonicalNameKey ? 1 : 0),
  }
}

async function inspectRecords(transaction: ImportTransaction, records: readonly InventoryRecord[], worldId: string): Promise<{ summary: ImportSummary, absent: InventoryRecord[] }> {
  const summary = initialSummary()
  for (const record of records) await transaction.lockIdentity(worldId, record.canonicalNameKey)
  const absent: InventoryRecord[] = []
  for (const record of records) {
    const existing = await transaction.findCharacter(worldId, record.canonicalNameKey)
    if (!existing) absent.push(record)
    else {
      const reason = sameImportedIdentity(record, existing)
      if (reason) summary.quarantined[reason]++
      else summary.idempotent++
    }
  }
  return { summary, absent }
}

/**
 * Ledger-backed import. Batch identity, all character inserts, and the
 * per-world/stream contiguous watermark advance share one serializable
 * transaction. Unlike importRecords, this is an auditable ledger mode.
 */
export async function importBatch(store: ImportStore, records: readonly InventoryRecord[], options: BatchImportOptions, initialRejected = 0): Promise<BatchImportSummary> {
  const validated = validateBatchOptions(options)
  const admitted = admissionSummary(records, initialRejected)
  const base = (): BatchImportSummary => ({ ...initialSummary(), streamId: validated.streamId, sequence: validated.sequence, ledger: 'dry_run' })
  if (admitted.summary.quarantined.invalid_metadata > 0 || admitted.summary.quarantined.duplicate_input_identity > 0) {
    return { ...base(), ...admitted.summary }
  }
  return store.transaction(async (transaction) => {
    await transaction.lockBatchStream(validated.identity.worldId, validated.streamId)
    const atSequence = await transaction.findBatchBySequence(validated.identity.worldId, validated.streamId, validated.sequence)
    if (atSequence) {
      if (atSequence.stableKey !== validated.identity.stableKey) throw new BatchImportError('batch_sequence_identity_conflict')
      return { ...base(), idempotent: atSequence.recordCount, ledger: 'idempotent' }
    }
    if (await transaction.findBatchByIdentity(validated.identity.worldId, validated.streamId, validated.identity.stableKey)) {
      throw new BatchImportError('batch_identity_sequence_conflict')
    }
    const watermark = await transaction.readWatermark(validated.identity.worldId, validated.streamId)
    const expected = watermark === undefined ? 0 : watermark + 1
    if (validated.sequence !== expected) throw new BatchImportError('batch_sequence_out_of_order')

    const inspected = await inspectRecords(transaction, admitted.ordered, validated.identity.worldId)
    if (Object.values(inspected.summary.quarantined).some((count) => count > 0)) return { ...base(), ...inspected.summary }
    if (!validated.apply) return { ...base(), ...inspected.summary, wouldInsert: inspected.absent.length }

    await transaction.createBatch({ identity: validated.identity, streamId: validated.streamId, sequence: validated.sequence, recordCount: admitted.ordered.length })
    for (const record of inspected.absent) {
      const characterId = await transaction.insertImportedUnclaimed({ worldId: validated.identity.worldId, record })
      await transaction.recordBatchMember({
        worldId: validated.identity.worldId,
        streamId: validated.streamId,
        sequence: validated.sequence,
        characterId,
        legacyLocator: {
          canonicalName: record.canonicalNameKey,
          legacyNameSha1: legacyNameSha1(record.canonicalNameKey),
          legacyShard: record.expectedShard,
        },
      })
    }
    await transaction.advanceWatermark(validated.identity.worldId, validated.streamId, validated.sequence)
    return { ...base(), ...inspected.summary, inserted: inspected.absent.length, ledger: 'committed' }
  })
}

/** Preserve the reviewed JSONL operator mode while applying the shared record contract. */
export async function importInventory(store: ImportStore, lines: readonly string[], options: ImportOptions): Promise<ImportSummary> {
  const records: InventoryRecord[] = []
  let rejected = 0
  for (const line of lines) {
    const record = parseInventoryLine(line)
    if (record) records.push(record)
    else rejected++
  }
  return importRecords(store, records, options, rejected)
}
