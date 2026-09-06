import { createHash } from 'node:crypto'
import type { PlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import type { PlayerSnapshotV1ReplayVerification } from './player-snapshot-v1-replay-verifier.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/
const WORLD_RE = /^[a-z][a-z0-9_-]{0,63}$/
const NAME_RE = /^.{1,14}$/s
const POSITIVE_INTEGER_RE = /^[1-9][0-9]{0,18}$/

/** The immutable artifact row plus the exact receipt tuple read with it. */
export interface ImmutablePlayerSnapshotV1FullPayloadEvidence {
  characterId: string
  commandId: string
  worldId: string
  legacyNameKey: string
  receiptRequestSha256: string
  writerInstanceId: string
  writerEpoch: string
  writerRevision: string
  sourcePostSha256: string
  sourceOctets: string
  storageFormat: string
  receiptAcknowledgedAt: string
  snapshotFormat: 'player-snapshot-v1'
  snapshotSha256: string
  snapshotOctets: number
  payload: Uint8Array
  receipt: {
    characterId: string
    commandId: string
    worldId: string
    legacyNameKey: string
    requestSha256: string
    writerInstanceId: string
    writerEpoch: string
    writerRevision: string
    postSha256: string
    storageFormat: string
    acknowledgedAt: string
  }
}

/** No default wiring owns this reader; callers must explicitly inject it. */
export interface ImmutablePlayerSnapshotV1FullPayloadReader {
  findByCommandId(commandId: string): Promise<readonly unknown[]>
}

export type PlayerSnapshotV1FullPayloadRehearsal =
  | 'MATCH'
  | 'MISSING_ARTIFACT'
  | 'UNEXPECTED_DUPLICATE'
  | 'RECEIPT_MISMATCH'
  | 'ARTIFACT_MISMATCH'
  | 'DECODE_MISMATCH'
  | 'SEMANTIC_MISMATCH'
  | 'INVALID_INPUT'
  | 'DB_READ_ERROR'

export type PlayerSnapshotV1SemanticDecoder = (payload: Uint8Array) => Promise<PlayerSnapshotV1ReplayVerification>

function hasExactlyKeys(value: Record<string, unknown>, fields: readonly string[]): boolean {
  const keys = Object.keys(value).sort()
  return keys.length === fields.length && keys.every((key, index) => key === fields[index])
}

const RECEIPT_FIELDS = [
  'acknowledgedAt', 'characterId', 'commandId', 'legacyNameKey', 'postSha256',
  'requestSha256', 'storageFormat', 'worldId', 'writerEpoch', 'writerInstanceId', 'writerRevision',
]
const EVIDENCE_FIELDS = [
  'characterId', 'commandId', 'legacyNameKey', 'payload', 'receipt', 'receiptAcknowledgedAt',
  'receiptRequestSha256', 'snapshotFormat', 'snapshotOctets', 'snapshotSha256', 'sourceOctets',
  'sourcePostSha256', 'storageFormat', 'worldId', 'writerEpoch', 'writerInstanceId', 'writerRevision',
]

function validPositive(value: unknown): value is string {
  if (typeof value !== 'string' || !POSITIVE_INTEGER_RE.test(value)) return false
  try { return BigInt(value) <= 9_223_372_036_854_775_807n } catch { return false }
}

function isReceipt(value: unknown): value is ImmutablePlayerSnapshotV1FullPayloadEvidence['receipt'] {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const receipt = value as Record<string, unknown>
  return hasExactlyKeys(receipt, RECEIPT_FIELDS)
    && typeof receipt.characterId === 'string' && UUID_RE.test(receipt.characterId)
    && typeof receipt.commandId === 'string' && UUID_RE.test(receipt.commandId)
    && typeof receipt.worldId === 'string' && WORLD_RE.test(receipt.worldId)
    && typeof receipt.legacyNameKey === 'string' && NAME_RE.test(receipt.legacyNameKey)
    && typeof receipt.requestSha256 === 'string' && HASH_RE.test(receipt.requestSha256)
    && typeof receipt.writerInstanceId === 'string' && UUID_RE.test(receipt.writerInstanceId)
    && validPositive(receipt.writerEpoch) && validPositive(receipt.writerRevision)
    && typeof receipt.postSha256 === 'string' && HASH_RE.test(receipt.postSha256)
    && validPositive(receipt.storageFormat) && BigInt(receipt.storageFormat) <= 32_767n
    && typeof receipt.acknowledgedAt === 'string' && receipt.acknowledgedAt.length > 0
}

function isEvidence(value: unknown): value is ImmutablePlayerSnapshotV1FullPayloadEvidence {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const evidence = value as Record<string, unknown>
  return hasExactlyKeys(evidence, EVIDENCE_FIELDS)
    && typeof evidence.characterId === 'string' && UUID_RE.test(evidence.characterId)
    && typeof evidence.commandId === 'string' && UUID_RE.test(evidence.commandId)
    && typeof evidence.worldId === 'string' && WORLD_RE.test(evidence.worldId)
    && typeof evidence.legacyNameKey === 'string' && NAME_RE.test(evidence.legacyNameKey)
    && typeof evidence.receiptRequestSha256 === 'string' && HASH_RE.test(evidence.receiptRequestSha256)
    && typeof evidence.writerInstanceId === 'string' && UUID_RE.test(evidence.writerInstanceId)
    && validPositive(evidence.writerEpoch) && validPositive(evidence.writerRevision)
    && typeof evidence.sourcePostSha256 === 'string' && HASH_RE.test(evidence.sourcePostSha256)
    && validPositive(evidence.sourceOctets) && validPositive(evidence.storageFormat)
    && BigInt(evidence.storageFormat) <= 32_767n
    && typeof evidence.receiptAcknowledgedAt === 'string' && evidence.receiptAcknowledgedAt.length > 0
    && evidence.snapshotFormat === 'player-snapshot-v1'
    && typeof evidence.snapshotSha256 === 'string' && HASH_RE.test(evidence.snapshotSha256)
    && typeof evidence.snapshotOctets === 'number' && Number.isSafeInteger(evidence.snapshotOctets)
    && evidence.snapshotOctets >= 48 && evidence.snapshotOctets <= 4_194_352
    && evidence.payload instanceof Uint8Array && evidence.payload.length === evidence.snapshotOctets
    && createHash('sha256').update(evidence.payload).digest('hex') === evidence.snapshotSha256
    && isReceipt(evidence.receipt)
}

function receiptMatchesArtifact(evidence: ImmutablePlayerSnapshotV1FullPayloadEvidence): boolean {
  const receipt = evidence.receipt
  return receipt.characterId === evidence.characterId && receipt.commandId === evidence.commandId
    && receipt.worldId === evidence.worldId && receipt.legacyNameKey === evidence.legacyNameKey
    && receipt.requestSha256 === evidence.receiptRequestSha256
    && receipt.writerInstanceId === evidence.writerInstanceId && receipt.writerEpoch === evidence.writerEpoch
    && receipt.writerRevision === evidence.writerRevision && receipt.postSha256 === evidence.sourcePostSha256
    && receipt.storageFormat === evidence.storageFormat && receipt.acknowledgedAt === evidence.receiptAcknowledgedAt
}

function localMatchesArtifact(local: PlayerSnapshotV1Artifact, evidence: ImmutablePlayerSnapshotV1FullPayloadEvidence): boolean {
  return local.characterId === evidence.characterId && local.commandId === evidence.commandId
    && local.receiptRequestSha256 === evidence.receiptRequestSha256
    && local.sourcePostSha256 === evidence.sourcePostSha256 && local.sourceOctets === evidence.sourceOctets
    && local.snapshotFormat === evidence.snapshotFormat && local.snapshotSha256 === evidence.snapshotSha256
    && local.snapshotOctets === evidence.snapshotOctets
}

function validVerification(value: unknown, payload: Uint8Array): value is PlayerSnapshotV1ReplayVerification {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const report = value as Record<string, unknown>
  const digest = createHash('sha256').update(payload).digest('hex')
  return hasExactlyKeys(report, [
    'algorithm', 'canonicalDigest', 'canonicalOctets', 'format', 'inputDigest', 'inventoryNodeCount', 'version',
  ]) && report.format === 'player-snapshot-v1-replay-verification' && report.version === '1' && report.algorithm === 'sha-256'
    && report.inputDigest === digest && report.canonicalDigest === digest && report.canonicalOctets === payload.length
    && typeof report.inventoryNodeCount === 'number' && Number.isSafeInteger(report.inventoryNodeCount) && report.inventoryNodeCount >= 0
}

/**
 * Explicit diagnostic only: first prove DB receipt binding, then decode both
 * immutable snapshots and compare their closed persisted semantics.  No
 * result is returned to gameplay and no reader/write capability is installed.
 */
export async function rehearsePlayerSnapshotV1FullPayload(
  localArtifact: unknown,
  reader: ImmutablePlayerSnapshotV1FullPayloadReader,
  decoder: PlayerSnapshotV1SemanticDecoder,
): Promise<PlayerSnapshotV1FullPayloadRehearsal> {
  if (!isLocalArtifact(localArtifact)) return 'INVALID_INPUT'
  let rows: readonly unknown[]
  try { rows = await reader.findByCommandId(localArtifact.commandId) } catch { return 'DB_READ_ERROR' }
  if (!Array.isArray(rows)) return 'DB_READ_ERROR'
  if (rows.length === 0) return 'MISSING_ARTIFACT'
  if (rows.length !== 1) return 'UNEXPECTED_DUPLICATE'
  if (!isEvidence(rows[0])) return 'INVALID_INPUT'
  const evidence = rows[0]
  if (!receiptMatchesArtifact(evidence)) return 'RECEIPT_MISMATCH'
  if (!localMatchesArtifact(localArtifact, evidence)) return 'ARTIFACT_MISMATCH'
  try {
    const [local, database] = await Promise.all([decoder(localArtifact.payload), decoder(evidence.payload)])
    if (!validVerification(local, localArtifact.payload) || !validVerification(database, evidence.payload)) return 'DECODE_MISMATCH'
    return local.canonicalDigest === database.canonicalDigest
      && local.canonicalOctets === database.canonicalOctets
      && local.inventoryNodeCount === database.inventoryNodeCount ? 'MATCH' : 'SEMANTIC_MISMATCH'
  } catch { return 'DECODE_MISMATCH' }
}

function isLocalArtifact(value: unknown): value is PlayerSnapshotV1Artifact {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const local = value as Record<string, unknown>
  return hasExactlyKeys(local, [
    'characterId', 'commandId', 'payload', 'receiptRequestSha256', 'snapshotFormat', 'snapshotOctets',
    'snapshotSha256', 'sourceOctets', 'sourcePostSha256',
  ]) && typeof local.characterId === 'string' && UUID_RE.test(local.characterId)
    && typeof local.commandId === 'string' && UUID_RE.test(local.commandId)
    && typeof local.receiptRequestSha256 === 'string' && HASH_RE.test(local.receiptRequestSha256)
    && typeof local.sourcePostSha256 === 'string' && HASH_RE.test(local.sourcePostSha256)
    && validPositive(local.sourceOctets) && local.snapshotFormat === 'player-snapshot-v1'
    && typeof local.snapshotSha256 === 'string' && HASH_RE.test(local.snapshotSha256)
    && typeof local.snapshotOctets === 'number' && Number.isSafeInteger(local.snapshotOctets)
    && local.snapshotOctets >= 48 && local.snapshotOctets <= 4_194_352
    && local.payload instanceof Uint8Array && local.payload.length === local.snapshotOctets
    && createHash('sha256').update(local.payload).digest('hex') === local.snapshotSha256
}
