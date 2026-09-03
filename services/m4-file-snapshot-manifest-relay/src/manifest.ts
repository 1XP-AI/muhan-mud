import { constants as fsConstants } from 'node:fs'

export const SNAPSHOT_FORMAT = 'legacy-file-manifest-v1' as const
export const MAX_OUTBOX_ENTRIES = 256
export const MAX_MANIFEST_BYTES = 2048
export const MAX_SNAPSHOT_OCTETS = 64 * 1024 * 1024

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/
const WORLD_RE = /^[a-z][a-z0-9_-]{0,63}$/
const NAME_HEX_RE = /^(?:[0-9a-f]{2}){1,14}$/
const POSITIVE_INT_RE = /^[1-9][0-9]{0,18}$/

export interface Manifest {
  worldId: string
  characterId: string
  commandId: string
  canonicalNameHex: string
  requestSha256: string
  postSha256: string
  writerInstanceId: string
  snapshotFormat: typeof SNAPSHOT_FORMAT
  writerEpoch: string
  writerRevision: string
  storageFormat: number
  snapshotOctets: string
}

export class InvalidManifestError extends Error {
  constructor() { super('invalid manifest') }
}

function positiveInt(value: string): boolean {
  if (!POSITIVE_INT_RE.test(value)) return false
  try { return BigInt(value) <= BigInt('9223372036854775807') } catch { return false }
}

function ascii(bytes: Uint8Array): string | undefined {
  for (const byte of bytes) if (byte > 0x7f || byte === 0) return undefined
  return Buffer.from(bytes).toString('ascii')
}

/** Parse exactly the canonical C outbox encoding, without accepting aliases or whitespace. */
export function parseManifest(bytes: Uint8Array): Manifest {
  if (bytes.length === 0 || bytes.length >= MAX_MANIFEST_BYTES || bytes[bytes.length - 1] !== 0x0a) throw new InvalidManifestError()
  const text = ascii(bytes)
  if (text === undefined) throw new InvalidManifestError()
  const lines = text.split('\n')
  if (lines.length !== 14 || lines[13] !== '') throw new InvalidManifestError()
  const names = [
    'version', 'world_id', 'character_id', 'command_id', 'canonical_name_hex',
    'request_sha256', 'post_sha256', 'writer_instance_id', 'snapshot_format',
    'writer_epoch', 'writer_revision', 'storage_format', 'snapshot_octets',
  ]
  const values: string[] = []
  for (let index = 0; index < names.length; index++) {
    const line = lines[index]!
    const prefix = `${names[index]}=`
    if (!line.startsWith(prefix)) throw new InvalidManifestError()
    const value = line.slice(prefix.length)
    if (value.includes('=') || value.includes('\r') || value.includes('\n')) throw new InvalidManifestError()
    values.push(value)
  }
  if (values[0] !== '1') throw new InvalidManifestError()
  const [, worldId, characterId, commandId, canonicalNameHex, requestSha256,
    postSha256, writerInstanceId, snapshotFormat, writerEpoch, writerRevision,
    storageFormatText, snapshotOctets] = values
  if (!WORLD_RE.test(worldId!) || !UUID_RE.test(characterId!) || !UUID_RE.test(commandId!)
    || !NAME_HEX_RE.test(canonicalNameHex!) || !HASH_RE.test(requestSha256!)
    || !HASH_RE.test(postSha256!) || !UUID_RE.test(writerInstanceId!)
    || snapshotFormat !== SNAPSHOT_FORMAT || !positiveInt(writerEpoch!)
    || !positiveInt(writerRevision!) || !positiveInt(storageFormatText!)
    || !positiveInt(snapshotOctets!) || BigInt(storageFormatText!) > 32767
    || BigInt(snapshotOctets!) > BigInt(MAX_SNAPSHOT_OCTETS)) throw new InvalidManifestError()
  const canonical = [
    `version=1`, `world_id=${worldId}`, `character_id=${characterId}`,
    `command_id=${commandId}`, `canonical_name_hex=${canonicalNameHex}`,
    `request_sha256=${requestSha256}`, `post_sha256=${postSha256}`,
    `writer_instance_id=${writerInstanceId}`, `snapshot_format=${SNAPSHOT_FORMAT}`,
    `writer_epoch=${writerEpoch}`, `writer_revision=${writerRevision}`,
    `storage_format=${storageFormatText}`, `snapshot_octets=${snapshotOctets}`,
  ].join('\n') + '\n'
  if (Buffer.byteLength(canonical, 'ascii') !== bytes.length || !Buffer.from(canonical, 'ascii').equals(Buffer.from(bytes))) throw new InvalidManifestError()
  return {
    worldId: worldId!, characterId: characterId!, commandId: commandId!, canonicalNameHex: canonicalNameHex!,
    requestSha256: requestSha256!, postSha256: postSha256!, writerInstanceId: writerInstanceId!,
    snapshotFormat: SNAPSHOT_FORMAT, writerEpoch: writerEpoch!, writerRevision: writerRevision!,
    storageFormat: Number(storageFormatText), snapshotOctets: snapshotOctets!,
  }
}

export function isManifestFilename(name: Uint8Array): boolean {
  const suffix = Buffer.from('.manifest')
  return name.length >= suffix.length && Buffer.from(name).subarray(-suffix.length).equals(suffix)
}

export function commandFromFilename(name: string): string | undefined {
  if (!name.endsWith('.manifest')) return undefined
  const command = name.slice(0, -'.manifest'.length)
  return UUID_RE.test(command) ? command : undefined
}

export function noFollowDirectoryFlags(): number {
  if (!fsConstants.O_NOFOLLOW || !fsConstants.O_DIRECTORY) throw new Error('unsafe outbox')
  return fsConstants.O_RDONLY | fsConstants.O_NONBLOCK | fsConstants.O_NOFOLLOW | fsConstants.O_DIRECTORY
}

export function noFollowFileFlags(): number {
  if (!fsConstants.O_NOFOLLOW) throw new Error('unsafe outbox')
  return fsConstants.O_RDONLY | fsConstants.O_NONBLOCK | fsConstants.O_NOFOLLOW
}
