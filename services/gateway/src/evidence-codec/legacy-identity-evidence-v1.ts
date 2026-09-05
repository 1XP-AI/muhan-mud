/**
 * Closed, metadata-only canonical wire codec for LegacyIdentityEvidenceV1.
 * This boundary neither canonicalizes names nor derives shards.
 */

export const LEGACY_IDENTITY_EVIDENCE_V1_MAGIC = new Uint8Array([
  0x4d, 0x55, 0x44, 0x4c, 0x49, 0x45, 0x00, 0x00
])
export const LEGACY_IDENTITY_EVIDENCE_V1_SCHEMA = 1
export const LEGACY_IDENTITY_EVIDENCE_V1_VERSION = 1
export const LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT = 'player-v1'
export const LEGACY_IDENTITY_EVIDENCE_V1_SHA256_HEX_LENGTH = 64
export const LEGACY_IDENTITY_EVIDENCE_V1_MAX_NAME_BYTES = 14
export const LEGACY_IDENTITY_EVIDENCE_V1_HEADER_LENGTH = 16
export const LEGACY_IDENTITY_EVIDENCE_V1_MAX_PAYLOAD_LENGTH = 95
export const LEGACY_IDENTITY_EVIDENCE_V1_MAX_WIRE_LENGTH = 111

export type LegacyIdentityEvidenceV1Outcome =
  | 'ok'
  | 'not_found'
  | 'corrupt'
  | 'io_error'
  | 'invalid_input'

export type LegacyIdentityEvidenceV1Canonicalization =
  | 'canonical'
  | 'normalized'
  | 'invalid'

export interface LegacyIdentityEvidenceV1 {
  outcome: LegacyIdentityEvidenceV1Outcome
  canonicalization: LegacyIdentityEvidenceV1Canonicalization
  canonicalName: string
  legacyShard: string
  playerFileSha256: string
  storageFormat: string
}

/** Byte and hex seams intentionally remain independent for a versioned adapter. */
export interface LegacyIdentityEvidenceBytesCodec<V> {
  encode(value: V): Uint8Array
  decode(wire: Uint8Array): V
}

export interface LegacyIdentityEvidenceHexCodec<V> {
  encodeHex(value: V): string
  decodeHex(hex: string): V
}

export class LegacyIdentityEvidenceV1WireError extends Error {
  constructor(message = 'invalid legacy identity evidence v1 wire') {
    super(message)
    this.name = 'LegacyIdentityEvidenceV1WireError'
  }
}

const encoder = new TextEncoder()
const fatalDecoder = new TextDecoder('utf-8', { fatal: true })
const outcomeBytes: Record<LegacyIdentityEvidenceV1Outcome, number> = {
  ok: 0, not_found: 1, corrupt: 2, io_error: 3, invalid_input: 4
}
const canonicalizationBytes: Record<LegacyIdentityEvidenceV1Canonicalization, number> = {
  canonical: 0, normalized: 1, invalid: 2
}
const outcomes = Object.entries(outcomeBytes).reduce<Record<number, LegacyIdentityEvidenceV1Outcome>>(
  (output, [name, byte]) => ({ ...output, [byte]: name as LegacyIdentityEvidenceV1Outcome }), {}
)
const canonicalizations = Object.entries(canonicalizationBytes).reduce<Record<number, LegacyIdentityEvidenceV1Canonicalization>>(
  (output, [name, byte]) => ({ ...output, [byte]: name as LegacyIdentityEvidenceV1Canonicalization }), {}
)

function fail(message?: string): never {
  throw new LegacyIdentityEvidenceV1WireError(message)
}

function utf8(value: string): Uint8Array {
  // TextEncoder replaces lone surrogates; reject them rather than changing input.
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index)
    if (unit >= 0xd800 && unit <= 0xdbff) {
      if (index + 1 >= value.length || value.charCodeAt(index + 1) < 0xdc00 || value.charCodeAt(index + 1) > 0xdfff) fail()
      index += 1
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {
      fail()
    }
  }
  const bytes = encoder.encode(value)
  if (bytes.includes(0)) fail()
  return bytes
}

function decodeUtf8(bytes: Uint8Array): string {
  if (bytes.includes(0)) fail()
  try {
    return fatalDecoder.decode(bytes)
  } catch {
    return fail()
  }
}

function isLowerHex(bytes: Uint8Array): boolean {
  return bytes.every((byte) => (byte >= 0x30 && byte <= 0x39) || (byte >= 0x61 && byte <= 0x66))
}

function valid(value: LegacyIdentityEvidenceV1, fields: readonly Uint8Array[]): boolean {
  const [name, shard, digest, storage] = fields
  if (value.storageFormat !== LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT ||
      !outcomeBytes.hasOwnProperty(value.outcome) ||
      !canonicalizationBytes.hasOwnProperty(value.canonicalization)) return false
  if (value.outcome === 'invalid_input') {
    return value.canonicalization === 'invalid' && name.length === 0 && shard.length === 0 && digest.length === 0
  }
  return value.canonicalization !== 'invalid' &&
    name.length > 0 && name.length <= LEGACY_IDENTITY_EVIDENCE_V1_MAX_NAME_BYTES &&
    shard.length === 2 && isLowerHex(shard) &&
    (value.outcome === 'ok'
      ? digest.length === LEGACY_IDENTITY_EVIDENCE_V1_SHA256_HEX_LENGTH && isLowerHex(digest)
      : digest.length === 0) &&
    storage.length === LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT.length
}

function fieldsFor(value: LegacyIdentityEvidenceV1): Uint8Array[] {
  return [utf8(value.canonicalName), utf8(value.legacyShard), utf8(value.playerFileSha256), utf8(value.storageFormat)]
}

export function bytesToLowerHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

/** Strict canonical lowercase hex; callers may trim fixture file whitespace themselves. */
export function lowerHexToBytes(hex: string): Uint8Array {
  if (hex.length % 2 !== 0 || !/^(?:[0-9a-f]{2})*$/.test(hex)) fail('invalid lowercase hex')
  const bytes = new Uint8Array(hex.length / 2)
  for (let index = 0; index < bytes.length; index += 1) bytes[index] = Number.parseInt(hex.slice(index * 2, index * 2 + 2), 16)
  return bytes
}

export function encodeLegacyIdentityEvidenceV1(value: LegacyIdentityEvidenceV1): Uint8Array {
  const fields = fieldsFor(value)
  if (!valid(value, fields) || fields.some((field) => field.length > 0xff)) fail()
  const payloadLength = 2 + fields.reduce((length, field) => length + 1 + field.length, 0)
  if (payloadLength > LEGACY_IDENTITY_EVIDENCE_V1_MAX_PAYLOAD_LENGTH) fail()
  const wire = new Uint8Array(LEGACY_IDENTITY_EVIDENCE_V1_HEADER_LENGTH + payloadLength)
  wire.set(LEGACY_IDENTITY_EVIDENCE_V1_MAGIC, 0)
  wire[9] = LEGACY_IDENTITY_EVIDENCE_V1_SCHEMA
  wire[11] = LEGACY_IDENTITY_EVIDENCE_V1_VERSION
  new DataView(wire.buffer, wire.byteOffset, wire.byteLength).setUint32(12, payloadLength, false)
  let at = LEGACY_IDENTITY_EVIDENCE_V1_HEADER_LENGTH
  wire[at++] = outcomeBytes[value.outcome]
  wire[at++] = canonicalizationBytes[value.canonicalization]
  for (const field of fields) {
    wire[at++] = field.length
    wire.set(field, at)
    at += field.length
  }
  return wire
}

export function decodeLegacyIdentityEvidenceV1(wire: Uint8Array): LegacyIdentityEvidenceV1 {
  if (wire.length < LEGACY_IDENTITY_EVIDENCE_V1_HEADER_LENGTH) fail('truncated header')
  if (!LEGACY_IDENTITY_EVIDENCE_V1_MAGIC.every((byte, index) => wire[index] === byte)) fail('invalid magic')
  const view = new DataView(wire.buffer, wire.byteOffset, wire.byteLength)
  if (view.getUint16(8, false) !== LEGACY_IDENTITY_EVIDENCE_V1_SCHEMA) fail('unsupported schema')
  if (view.getUint16(10, false) !== LEGACY_IDENTITY_EVIDENCE_V1_VERSION) fail('unsupported version')
  const payloadLength = view.getUint32(12, false)
  if (payloadLength > LEGACY_IDENTITY_EVIDENCE_V1_MAX_PAYLOAD_LENGTH) fail('payload too large')
  if (wire.length !== LEGACY_IDENTITY_EVIDENCE_V1_HEADER_LENGTH + payloadLength) fail('truncated or trailing bytes')
  let at = LEGACY_IDENTITY_EVIDENCE_V1_HEADER_LENGTH
  const outcome = outcomes[wire[at++]]
  const canonicalization = canonicalizations[wire[at++]]
  if (outcome === undefined || canonicalization === undefined) fail('invalid outcome or canonicalization')
  const take = (): Uint8Array => {
    if (at >= wire.length) fail('truncated field length')
    const length = wire[at++]
    if (length > wire.length - at) fail('truncated field')
    const result = wire.subarray(at, at + length)
    at += length
    return result
  }
  const fields = [take(), take(), take(), take()]
  if (at !== wire.length) fail('trailing payload bytes')
  const [canonicalName, legacyShard, playerFileSha256, storageFormat] = fields.map(decodeUtf8)
  const value = { outcome, canonicalization, canonicalName, legacyShard, playerFileSha256, storageFormat }
  if (!valid(value, fields)) fail('noncanonical evidence')
  return value
}

export const legacyIdentityEvidenceV1Codec: LegacyIdentityEvidenceBytesCodec<LegacyIdentityEvidenceV1> & LegacyIdentityEvidenceHexCodec<LegacyIdentityEvidenceV1> = {
  encode: encodeLegacyIdentityEvidenceV1,
  decode: decodeLegacyIdentityEvidenceV1,
  encodeHex: (value) => bytesToLowerHex(encodeLegacyIdentityEvidenceV1(value)),
  decodeHex: (hex) => decodeLegacyIdentityEvidenceV1(lowerHexToBytes(hex))
}
