import { createHash } from 'node:crypto'
import type { Manifest } from './manifest.js'

export const PLAYER_SNAPSHOT_V1_FORMAT = 'player-snapshot-v1' as const
export const PLAYER_SNAPSHOT_V1_SUFFIX = '.player-snapshot-v1'
export const MAX_PLAYER_SNAPSHOT_V1_OCTETS = 4_194_352
export const MAX_PLAYER_SNAPSHOT_V1_HEADER_OCTETS = 2_048
export const MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS = MAX_PLAYER_SNAPSHOT_V1_HEADER_OCTETS + MAX_PLAYER_SNAPSHOT_V1_OCTETS

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/
const WORLD_RE = /^[a-z][a-z0-9_-]{0,63}$/
const NAME_HEX_RE = /^(?:[0-9a-f]{2}){1,14}$/
const POSITIVE_INT_RE = /^[1-9][0-9]{0,18}$/
const CDTO_MAGIC = Buffer.from('MUHCDTO\0', 'ascii')

export interface PlayerSnapshotV1Artifact {
  characterId: string
  commandId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  sourceOctets: string
  snapshotFormat: typeof PLAYER_SNAPSHOT_V1_FORMAT
  snapshotSha256: string
  snapshotOctets: number
  payload: Uint8Array
}

/**
 * Read-only native evidence parsed from exactly one C artifact-store file.
 * It deliberately has no filename, receipt, filesystem, relay, or mutation
 * capability; callers that need receipt authority must still use
 * `parsePlayerSnapshotV1Artifact` below.
 */
export interface PlayerSnapshotV1ArtifactEvidence {
  worldId: string
  characterId: string
  commandId: string
  canonicalNameHex: string
  requestSha256: string
  sourcePostSha256: string
  writerInstanceId: string
  writerEpoch: string
  writerRevision: string
  storageFormat: string
  snapshotFormat: typeof PLAYER_SNAPSHOT_V1_FORMAT
  sourceOctets: string
  snapshotSha256: string
  snapshotOctets: number
  payload: Uint8Array
}

export class InvalidPlayerSnapshotV1ArtifactError extends Error {
  constructor() { super('invalid PlayerSnapshotV1 artifact') }
}

export function commandFromPlayerSnapshotV1Filename(name: string): string | undefined {
  if (!name.endsWith(PLAYER_SNAPSHOT_V1_SUFFIX)) return undefined
  const commandId = name.slice(0, -PLAYER_SNAPSHOT_V1_SUFFIX.length)
  return UUID_RE.test(commandId) ? commandId : undefined
}

function u32(bytes: Uint8Array, offset: number): number {
  return bytes[offset]! * 0x1000000 + bytes[offset + 1]! * 0x10000 + bytes[offset + 2]! * 0x100 + bytes[offset + 3]!
}

function positiveInt(value: string): boolean {
  if (!POSITIVE_INT_RE.test(value)) return false
  try { return BigInt(value) <= 9_223_372_036_854_775_807n } catch { return false }
}

interface PlayerSnapshotV1Header {
  worldId: string
  characterId: string
  commandId: string
  canonicalNameHex: string
  requestSha256: string
  sourcePostSha256: string
  writerInstanceId: string
  writerEpoch: string
  writerRevision: string
  storageFormat: string
  snapshotFormat: typeof PLAYER_SNAPSHOT_V1_FORMAT
  sourceOctets: string
  snapshotSha256: string
  snapshotOctets: string
}

/** Parse exactly the native 15-line header plus its mandatory blank line. */
function parseHeader(bytes: Uint8Array): { header: PlayerSnapshotV1Header, payload: Uint8Array } {
  const delimiter = Buffer.from(bytes).indexOf('\n\n', 'ascii')
  if (delimiter < 0 || delimiter + 2 > MAX_PLAYER_SNAPSHOT_V1_HEADER_OCTETS || delimiter + 2 >= bytes.length) {
    throw new InvalidPlayerSnapshotV1ArtifactError()
  }
  const headerBytes = bytes.subarray(0, delimiter + 2)
  if (headerBytes.some((byte) => byte === 0 || byte > 0x7f)) throw new InvalidPlayerSnapshotV1ArtifactError()
  const lines = Buffer.from(headerBytes).toString('ascii').split('\n')
  if (lines.length !== 17 || lines[15] !== '' || lines[16] !== '') throw new InvalidPlayerSnapshotV1ArtifactError()
  const names = [
    'version', 'world_id', 'character_id', 'command_id', 'canonical_name_hex',
    'request_sha256', 'source_post_sha256', 'writer_instance_id', 'writer_epoch',
    'writer_revision', 'storage_format', 'snapshot_format', 'source_octets',
    'snapshot_sha256', 'snapshot_octets',
  ]
  const values: string[] = []
  for (let index = 0; index < names.length; index++) {
    const prefix = `${names[index]}=`
    const line = lines[index]!
    if (!line.startsWith(prefix)) throw new InvalidPlayerSnapshotV1ArtifactError()
    const value = line.slice(prefix.length)
    if (value.includes('=') || value.includes('\r') || value.includes('\n')) throw new InvalidPlayerSnapshotV1ArtifactError()
    values.push(value)
  }
  const [version, worldId, characterId, commandId, canonicalNameHex, requestSha256,
    sourcePostSha256, writerInstanceId, writerEpoch, writerRevision, storageFormat,
    snapshotFormat, sourceOctets, snapshotSha256, snapshotOctets] = values
  if (version !== '1' || !WORLD_RE.test(worldId!) || !UUID_RE.test(characterId!)
    || !UUID_RE.test(commandId!) || !NAME_HEX_RE.test(canonicalNameHex!)
    || !HASH_RE.test(requestSha256!) || !HASH_RE.test(sourcePostSha256!)
    || !UUID_RE.test(writerInstanceId!) || !positiveInt(writerEpoch!)
    || !positiveInt(writerRevision!) || !positiveInt(storageFormat!)
    || BigInt(storageFormat!) > 32_767n || snapshotFormat !== PLAYER_SNAPSHOT_V1_FORMAT
    || !positiveInt(sourceOctets!) || !HASH_RE.test(snapshotSha256!)
    || !positiveInt(snapshotOctets!) || BigInt(snapshotOctets!) > BigInt(MAX_PLAYER_SNAPSHOT_V1_OCTETS)) {
    throw new InvalidPlayerSnapshotV1ArtifactError()
  }
  const canonical = [
    'version=1', `world_id=${worldId}`, `character_id=${characterId}`,
    `command_id=${commandId}`, `canonical_name_hex=${canonicalNameHex}`,
    `request_sha256=${requestSha256}`, `source_post_sha256=${sourcePostSha256}`,
    `writer_instance_id=${writerInstanceId}`, `writer_epoch=${writerEpoch}`,
    `writer_revision=${writerRevision}`, `storage_format=${storageFormat}`,
    `snapshot_format=${PLAYER_SNAPSHOT_V1_FORMAT}`, `source_octets=${sourceOctets}`,
    `snapshot_sha256=${snapshotSha256}`, `snapshot_octets=${snapshotOctets}`, '', '',
  ].join('\n')
  if (!Buffer.from(canonical, 'ascii').equals(Buffer.from(headerBytes))) throw new InvalidPlayerSnapshotV1ArtifactError()
  return {
    header: {
      worldId: worldId!, characterId: characterId!, commandId: commandId!, canonicalNameHex: canonicalNameHex!,
      requestSha256: requestSha256!, sourcePostSha256: sourcePostSha256!, writerInstanceId: writerInstanceId!,
      writerEpoch: writerEpoch!, writerRevision: writerRevision!, storageFormat: storageFormat!,
      snapshotFormat: PLAYER_SNAPSHOT_V1_FORMAT, sourceOctets: sourceOctets!, snapshotSha256: snapshotSha256!, snapshotOctets: snapshotOctets!,
    },
    payload: bytes.subarray(delimiter + 2),
  }
}

/**
 * Parses the native header-wrapped PlayerSnapshotV1 evidence. This deliberately
 * does not call or extend the legacy text-manifest parser: its canonical receipt
 * is only used to bind the immutable header to the acknowledged save context.
 */
export function parsePlayerSnapshotV1ArtifactEvidence(bytes: Uint8Array): PlayerSnapshotV1ArtifactEvidence {
  if (bytes.length < 51 || bytes.length > MAX_PLAYER_SNAPSHOT_V1_ARTIFACT_OCTETS) {
    throw new InvalidPlayerSnapshotV1ArtifactError()
  }
  const { header, payload } = parseHeader(bytes)
  if (payload.length < 48 || payload.length > MAX_PLAYER_SNAPSHOT_V1_OCTETS
    || BigInt(header.snapshotOctets) !== BigInt(payload.length)
    || !Buffer.from(payload.subarray(0, 8)).equals(CDTO_MAGIC)
    || payload[8] !== 0 || payload[9] !== 1 || payload[10] !== 0 || payload[11] !== 7) {
    throw new InvalidPlayerSnapshotV1ArtifactError()
  }
  const payloadLength = u32(payload, 12)
  if (payloadLength > MAX_PLAYER_SNAPSHOT_V1_OCTETS - 48 || payloadLength + 48 !== payload.length) {
    throw new InvalidPlayerSnapshotV1ArtifactError()
  }
  const payloadEnd = 16 + payloadLength
  const expectedDigest = payload.subarray(payloadEnd)
  const actualDigest = createHash('sha256').update(payload.subarray(16, payloadEnd)).digest()
  if (expectedDigest.length !== 32 || !actualDigest.equals(expectedDigest)) throw new InvalidPlayerSnapshotV1ArtifactError()
  if (createHash('sha256').update(payload).digest('hex') !== header.snapshotSha256) throw new InvalidPlayerSnapshotV1ArtifactError()
  return {
    worldId: header.worldId,
    characterId: header.characterId,
    commandId: header.commandId,
    canonicalNameHex: header.canonicalNameHex,
    requestSha256: header.requestSha256,
    sourcePostSha256: header.sourcePostSha256,
    writerInstanceId: header.writerInstanceId,
    writerEpoch: header.writerEpoch,
    writerRevision: header.writerRevision,
    storageFormat: header.storageFormat,
    snapshotFormat: header.snapshotFormat,
    sourceOctets: header.sourceOctets,
    snapshotSha256: header.snapshotSha256,
    snapshotOctets: payload.length,
    payload: Buffer.from(payload),
  }
}

/**
 * Parses native evidence and then binds it to the independently persisted
 * legacy receipt selected by the immutable artifact filename.
 */
export function parsePlayerSnapshotV1Artifact(
  filename: string,
  bytes: Uint8Array,
  receipt: Manifest,
): PlayerSnapshotV1Artifact {
  const commandId = commandFromPlayerSnapshotV1Filename(filename)
  if (!commandId) throw new InvalidPlayerSnapshotV1ArtifactError()
  const evidence = parsePlayerSnapshotV1ArtifactEvidence(bytes)
  if (commandId !== receipt.commandId || evidence.worldId !== receipt.worldId
    || evidence.characterId !== receipt.characterId || evidence.commandId !== receipt.commandId
    || evidence.canonicalNameHex !== receipt.canonicalNameHex || evidence.requestSha256 !== receipt.requestSha256
    || evidence.sourcePostSha256 !== receipt.postSha256 || evidence.writerInstanceId !== receipt.writerInstanceId
    || evidence.writerEpoch !== receipt.writerEpoch || evidence.writerRevision !== receipt.writerRevision
    || evidence.storageFormat !== String(receipt.storageFormat) || evidence.sourceOctets !== receipt.snapshotOctets) {
    throw new InvalidPlayerSnapshotV1ArtifactError()
  }
  return {
    characterId: evidence.characterId,
    commandId: evidence.commandId,
    receiptRequestSha256: evidence.requestSha256,
    sourcePostSha256: evidence.sourcePostSha256,
    sourceOctets: evidence.sourceOctets,
    snapshotFormat: evidence.snapshotFormat,
    snapshotSha256: evidence.snapshotSha256,
    snapshotOctets: evidence.snapshotOctets,
    payload: evidence.payload,
  }
}
