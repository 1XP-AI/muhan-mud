import { createHash } from 'node:crypto'
import type { PlayerSnapshotV1ReceiptBoundArtifactEvidence } from './player-snapshot-v1-artifact.js'
import type { PlayerSnapshotV1NormalizedProjection } from './player-snapshot-v1-normalized-projection.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/
const WORLD_RE = /^[a-z][a-z0-9_-]{0,63}$/
const POSITIVE_I64_RE = /^[1-9][0-9]{0,18}$/
const MAX_I64 = 9_223_372_036_854_775_807n
const MIN_I64 = -9_223_372_036_854_775_808n
const MAX_U8 = 255
const MAX_U32 = 4_294_967_295n
const MAX_ITEMS = 8_192
const MAX_DEPTH = 64
const MAX_LIST_ITEMS = 4_096

const ARTIFACT_FIELDS = [
  'canonicalNameHex', 'characterId', 'commandId', 'payload', 'receiptRequestSha256', 'snapshotFormat', 'snapshotOctets',
  'snapshotSha256', 'sourceOctets', 'sourcePostSha256', 'storageFormat', 'worldId', 'writerEpoch', 'writerInstanceId', 'writerRevision',
]
const RECORD_FIELDS = [
  'characterId', 'commandId', 'projection', 'receiptRequestSha256', 'snapshotOctets', 'snapshotSha256', 'sourceOctets',
  'sourcePostSha256', 'worldId', 'writerEpoch', 'writerInstanceId', 'writerRevision',
]

/**
 * Exact immutable row shape available from the normalized-projection table
 * plus the artifact's world binding. The latter is held in the already
 * validated artifact/receipt relation; this pure contract does not read it.
 */
export interface ImmutablePlayerSnapshotV1NormalizedProjectionRecord {
  worldId: string
  characterId: string
  commandId: string
  receiptRequestSha256: string
  writerInstanceId: string
  writerEpoch: string
  writerRevision: string
  sourcePostSha256: string
  sourceOctets: string
  snapshotSha256: string
  snapshotOctets: number
  projection: PlayerSnapshotV1NormalizedProjection
}

/** Payload-free, read-only injection point. No database implementation belongs here. */
export interface ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader {
  findByIdentity(identity: Readonly<{ worldId: string, characterId: string, commandId: string }>): Promise<readonly unknown[]>
}

/** Injectable derivation boundary; the production adapter is the Rust-backed normalized projector. */
export interface PlayerSnapshotV1NormalizedProjectionDeriver {
  project(payload: Uint8Array, snapshotSha256: string): Promise<unknown>
}

export type PlayerSnapshotV1NormalizedProjectionShadowComparison =
  | 'MATCH'
  | 'INVALID_ARTIFACT'
  | 'PROJECTION_DERIVATION_FAILED'
  | 'RECORD_READ_ERROR'
  | 'MISSING_RECORD'
  | 'UNEXPECTED_DUPLICATE'
  | 'INVALID_RECORD'
  | 'EVIDENCE_MISMATCH'
  | 'PROJECTION_MISMATCH'

function exactlyKeys(value: Record<string, unknown>, fields: readonly string[]): boolean {
  const keys = Object.keys(value).sort()
  return keys.length === fields.length && keys.every((key, index) => key === fields[index])
}

function integer(value: unknown, min: bigint, max: bigint): value is number | bigint {
  // Match the normalized projector's runtime types before digest serialization:
  // signed i64 fields are bigint; all narrower integers are number.
  if (min === MIN_I64 && max === MAX_I64) return typeof value === 'bigint' && value >= min && value <= max
  return typeof value === 'number' && Number.isSafeInteger(value) && BigInt(value) >= min && BigInt(value) <= max
}

function u8(value: number): Buffer { return Buffer.from([value]) }
function i8(value: number): Buffer { const output = Buffer.allocUnsafe(1); output.writeInt8(value); return output }
function u16(value: number): Buffer { const output = Buffer.allocUnsafe(2); output.writeUInt16BE(value); return output }
function i16(value: number): Buffer { const output = Buffer.allocUnsafe(2); output.writeInt16BE(value); return output }
function u32(value: number): Buffer { const output = Buffer.allocUnsafe(4); output.writeUInt32BE(value); return output }
function i64(value: bigint): Buffer { const output = Buffer.allocUnsafe(8); output.writeBigInt64BE(value); return output }

/** Mirrors the Rust v1 canonical projection digest over every allowlisted field. */
export function canonicalPlayerSnapshotV1NormalizedProjectionDigest(
  player: PlayerSnapshotV1NormalizedProjection['player'],
): string {
  const bytes: Buffer[] = [
    Buffer.from('muhan/player-snapshot-normalized-v1\0', 'ascii'), u16(1), u8(player.level),
    i16(player.hpMax), i16(player.hpCurrent), i16(player.mpMax), i16(player.mpCurrent), i64(player.experience), i64(player.gold),
  ]
  for (const daily of player.daily) bytes.push(u8(daily.max), u8(daily.current), i64(daily.lastUsed))
  for (const timer of player.timers) bytes.push(i64(timer.interval), i64(timer.lastUsed), i16(timer.misc))
  bytes.push(u32(player.items.length))
  for (const item of player.items) bytes.push(
    u32(item.parentIndex ?? 0xffff_ffff), u32(item.childIndex), i64(item.value), i16(item.weight), i8(item.typeCode), i8(item.adjustment),
    i16(item.shotsMax), i16(item.shotsCurrent), i16(item.ndice), i16(item.sdice), i16(item.pdice), i8(item.armor), i8(item.wearFlag),
    i8(item.magicPower), i8(item.magicRealm), i16(item.special),
  )
  return createHash('sha256').update(Buffer.concat(bytes)).digest('hex')
}

function positiveI64(value: unknown): value is string {
  if (typeof value !== 'string' || !POSITIVE_I64_RE.test(value)) return false
  try { return BigInt(value) <= MAX_I64 } catch { return false }
}

function closedArtifact(value: unknown): value is PlayerSnapshotV1ReceiptBoundArtifactEvidence {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const artifact = value as Record<string, unknown>
  return exactlyKeys(artifact, ARTIFACT_FIELDS)
    && typeof artifact.worldId === 'string' && WORLD_RE.test(artifact.worldId)
    && typeof artifact.characterId === 'string' && UUID_RE.test(artifact.characterId)
    && typeof artifact.commandId === 'string' && UUID_RE.test(artifact.commandId)
    && typeof artifact.canonicalNameHex === 'string' && /^(?:[0-9a-f]{2}){1,14}$/.test(artifact.canonicalNameHex)
    && typeof artifact.receiptRequestSha256 === 'string' && HASH_RE.test(artifact.receiptRequestSha256)
    && typeof artifact.sourcePostSha256 === 'string' && HASH_RE.test(artifact.sourcePostSha256)
    && typeof artifact.writerInstanceId === 'string' && UUID_RE.test(artifact.writerInstanceId)
    && positiveI64(artifact.writerEpoch) && positiveI64(artifact.writerRevision) && positiveI64(artifact.storageFormat)
    && artifact.snapshotFormat === 'player-snapshot-v1' && positiveI64(artifact.sourceOctets)
    && typeof artifact.snapshotSha256 === 'string' && HASH_RE.test(artifact.snapshotSha256)
    && typeof artifact.snapshotOctets === 'number' && Number.isSafeInteger(artifact.snapshotOctets) && artifact.snapshotOctets >= 48 && artifact.snapshotOctets <= 4_194_352
    && artifact.payload instanceof Uint8Array && artifact.payload.length === artifact.snapshotOctets
    && createHash('sha256').update(artifact.payload).digest('hex') === artifact.snapshotSha256
}

function closedProjection(value: unknown): value is PlayerSnapshotV1NormalizedProjection {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const projection = value as Record<string, unknown>
  if (!exactlyKeys(projection, ['algorithm', 'canonicalDigest', 'format', 'player', 'version'])
    || projection.format !== 'player-snapshot-v1-normalized-projection' || projection.version !== 1
    || projection.algorithm !== 'sha-256' || typeof projection.canonicalDigest !== 'string' || !HASH_RE.test(projection.canonicalDigest)
    || typeof projection.player !== 'object' || projection.player === null || Array.isArray(projection.player)) return false
  const player = projection.player as Record<string, unknown>
  if (!exactlyKeys(player, ['daily', 'experience', 'gold', 'hpCurrent', 'hpMax', 'items', 'level', 'mpCurrent', 'mpMax', 'timers'])
    || !integer(player.level, 0n, 255n) || !integer(player.hpMax, -32768n, 32767n) || !integer(player.hpCurrent, -32768n, 32767n)
    || !integer(player.mpMax, -32768n, 32767n) || !integer(player.mpCurrent, -32768n, 32767n)
    || !integer(player.experience, MIN_I64, MAX_I64) || !integer(player.gold, MIN_I64, MAX_I64)
    || BigInt(player.hpCurrent) > BigInt(player.hpMax) || BigInt(player.mpCurrent) > BigInt(player.mpMax)
    || !Array.isArray(player.daily) || player.daily.length !== 10 || !Array.isArray(player.timers) || player.timers.length !== 45
    || !Array.isArray(player.items) || player.items.length > MAX_ITEMS) return false
  for (const daily of player.daily) {
    if (typeof daily !== 'object' || daily === null || Array.isArray(daily)) return false
    const value = daily as Record<string, unknown>
    if (!exactlyKeys(value, ['current', 'lastUsed', 'max']) || !integer(value.max, 0n, 255n)
      || !integer(value.current, 0n, 255n) || BigInt(value.current) > BigInt(value.max) || !integer(value.lastUsed, MIN_I64, MAX_I64)) return false
  }
  for (const timer of player.timers) {
    if (typeof timer !== 'object' || timer === null || Array.isArray(timer)) return false
    const value = timer as Record<string, unknown>
    if (!exactlyKeys(value, ['interval', 'lastUsed', 'misc']) || !integer(value.interval, MIN_I64, MAX_I64)
      || !integer(value.lastUsed, MIN_I64, MAX_I64) || !integer(value.misc, -32768n, 32767n)) return false
  }
  const childCounts = new Uint16Array(player.items.length)
  const depth = new Uint8Array(player.items.length)
  const ancestors: number[] = []
  let roots = 0
  for (const [index, item] of player.items.entries()) {
    if (typeof item !== 'object' || item === null || Array.isArray(item)) return false
    const value = item as Record<string, unknown>
    if (!exactlyKeys(value, ['adjustment', 'armor', 'childIndex', 'magicPower', 'magicRealm', 'ndice', 'parentIndex', 'pdice', 'sdice', 'shotsCurrent', 'shotsMax', 'special', 'typeCode', 'value', 'wearFlag', 'weight'])
      || !integer(value.parentIndex === null ? 0 : value.parentIndex, 0n, MAX_U32) || !integer(value.childIndex, 0n, MAX_U32)
      || !integer(value.value, MIN_I64, MAX_I64) || !integer(value.weight, -32768n, 32767n) || !integer(value.typeCode, -128n, 127n)
      || !integer(value.adjustment, -128n, 127n) || !integer(value.shotsMax, -32768n, 32767n) || !integer(value.shotsCurrent, -32768n, 32767n)
      || BigInt(value.shotsCurrent) > BigInt(value.shotsMax) || !integer(value.ndice, -32768n, 32767n) || !integer(value.sdice, -32768n, 32767n)
      || !integer(value.pdice, -32768n, 32767n) || !integer(value.armor, -128n, 127n) || !integer(value.wearFlag, -128n, 127n)
      || !integer(value.magicPower, -128n, 127n) || !integer(value.magicRealm, -128n, 127n) || !integer(value.special, -32768n, 32767n)) return false
    let expected: number
    if (value.parentIndex === null) {
      ancestors.length = 0
      expected = roots++
      depth[index] = 1
      if (roots > MAX_LIST_ITEMS) return false
    }
    else {
      if (typeof value.parentIndex !== 'number' || !Number.isSafeInteger(value.parentIndex) || value.parentIndex >= index) return false
      const ancestor = ancestors.lastIndexOf(value.parentIndex)
      if (ancestor < 0) return false
      ancestors.length = ancestor + 1
      depth[index] = depth[value.parentIndex]! + 1
      expected = childCounts[value.parentIndex]!
      childCounts[value.parentIndex]++
      if (childCounts[value.parentIndex]! > MAX_LIST_ITEMS) return false
    }
    if (typeof value.childIndex !== 'number' || value.childIndex !== expected || depth[index]! > MAX_DEPTH) return false
    ancestors.push(index)
  }
  return canonicalPlayerSnapshotV1NormalizedProjectionDigest(player as PlayerSnapshotV1NormalizedProjection['player']) === projection.canonicalDigest
}

function closedRecord(value: unknown): value is ImmutablePlayerSnapshotV1NormalizedProjectionRecord {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const record = value as Record<string, unknown>
  return exactlyKeys(record, RECORD_FIELDS)
    && typeof record.worldId === 'string' && WORLD_RE.test(record.worldId)
    && typeof record.characterId === 'string' && UUID_RE.test(record.characterId)
    && typeof record.commandId === 'string' && UUID_RE.test(record.commandId)
    && typeof record.receiptRequestSha256 === 'string' && HASH_RE.test(record.receiptRequestSha256)
    && typeof record.writerInstanceId === 'string' && UUID_RE.test(record.writerInstanceId)
    && positiveI64(record.writerEpoch) && positiveI64(record.writerRevision)
    && typeof record.sourcePostSha256 === 'string' && HASH_RE.test(record.sourcePostSha256)
    && positiveI64(record.sourceOctets) && typeof record.snapshotSha256 === 'string' && HASH_RE.test(record.snapshotSha256)
    && typeof record.snapshotOctets === 'number' && Number.isSafeInteger(record.snapshotOctets) && record.snapshotOctets >= 48 && record.snapshotOctets <= 4_194_352
    && closedProjection(record.projection)
}

function sameProjection(left: PlayerSnapshotV1NormalizedProjection, right: PlayerSnapshotV1NormalizedProjection): boolean {
  if (left.format !== right.format || left.version !== right.version || left.algorithm !== right.algorithm || left.canonicalDigest !== right.canonicalDigest) return false
  const a = left.player; const b = right.player
  if (a.level !== b.level || a.hpMax !== b.hpMax || a.hpCurrent !== b.hpCurrent || a.mpMax !== b.mpMax || a.mpCurrent !== b.mpCurrent || a.experience !== b.experience || a.gold !== b.gold
    || a.daily.length !== b.daily.length || a.timers.length !== b.timers.length || a.items.length !== b.items.length) return false
  for (let index = 0; index < a.daily.length; index++) { const x = a.daily[index]!; const y = b.daily[index]!; if (x.max !== y.max || x.current !== y.current || x.lastUsed !== y.lastUsed) return false }
  for (let index = 0; index < a.timers.length; index++) { const x = a.timers[index]!; const y = b.timers[index]!; if (x.interval !== y.interval || x.lastUsed !== y.lastUsed || x.misc !== y.misc) return false }
  for (let index = 0; index < a.items.length; index++) {
    const x = a.items[index]!; const y = b.items[index]!
    if (x.parentIndex !== y.parentIndex || x.childIndex !== y.childIndex || x.value !== y.value || x.weight !== y.weight || x.typeCode !== y.typeCode || x.adjustment !== y.adjustment || x.shotsMax !== y.shotsMax || x.shotsCurrent !== y.shotsCurrent || x.ndice !== y.ndice || x.sdice !== y.sdice || x.pdice !== y.pdice || x.armor !== y.armor || x.wearFlag !== y.wearFlag || x.magicPower !== y.magicPower || x.magicRealm !== y.magicRealm || x.special !== y.special) return false
  }
  return true
}

/**
 * Hermetic post-save comparison only: callers supply parser-validated immutable
 * C artifact evidence, a Rust-backed projector, and one injected normalized
 * record. It opens no file or database, makes no writes, and is unwired from
 * every relay/gameplay path.
 */
export async function comparePlayerSnapshotV1NormalizedProjectionShadow(
  artifact: unknown,
  reader: ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader,
  deriver: PlayerSnapshotV1NormalizedProjectionDeriver,
): Promise<PlayerSnapshotV1NormalizedProjectionShadowComparison> {
  if (!closedArtifact(artifact)) return 'INVALID_ARTIFACT'
  let derived: unknown
  try { derived = await deriver.project(artifact.payload, artifact.snapshotSha256) } catch { return 'PROJECTION_DERIVATION_FAILED' }
  if (!closedProjection(derived)) return 'PROJECTION_DERIVATION_FAILED'
  let rows: readonly unknown[]
  try {
    rows = await reader.findByIdentity({ worldId: artifact.worldId, characterId: artifact.characterId, commandId: artifact.commandId })
  } catch { return 'RECORD_READ_ERROR' }
  if (!Array.isArray(rows)) return 'RECORD_READ_ERROR'
  if (rows.length === 0) return 'MISSING_RECORD'
  if (rows.length !== 1) return 'UNEXPECTED_DUPLICATE'
  const record = rows[0]
  if (!closedRecord(record)) return 'INVALID_RECORD'
  if (record.worldId !== artifact.worldId || record.characterId !== artifact.characterId || record.commandId !== artifact.commandId
    || record.receiptRequestSha256 !== artifact.receiptRequestSha256 || record.writerInstanceId !== artifact.writerInstanceId
    || record.writerEpoch !== artifact.writerEpoch || record.writerRevision !== artifact.writerRevision
    || record.sourcePostSha256 !== artifact.sourcePostSha256 || record.sourceOctets !== artifact.sourceOctets
    || record.snapshotSha256 !== artifact.snapshotSha256 || record.snapshotOctets !== artifact.snapshotOctets) return 'EVIDENCE_MISMATCH'
  return sameProjection(derived, record.projection) ? 'MATCH' : 'PROJECTION_MISMATCH'
}
