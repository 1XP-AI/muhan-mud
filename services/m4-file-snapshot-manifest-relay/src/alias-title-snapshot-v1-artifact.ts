import { createHash } from 'node:crypto'

/**
 * Detached, raw-CDTO-only evidence boundary.  Nothing in the normal M4 relay,
 * image, or runtime imports this module.
 */
export const ALIAS_TITLE_SNAPSHOT_V1_KIND = 9
export const MAX_ALIAS_TITLE_SNAPSHOT_V1_OCTETS = 64 * 1024 + 48

const CDTO_MAGIC = Buffer.from('MUHCDTO\0', 'ascii')
const CDTO_PREFIX_OCTETS = 16
const CDTO_DIGEST_OCTETS = 32
const CDTO_FIELD_HEADER_OCTETS = 7
const CDTO_U16 = 2
const CDTO_BYTES = 9
const CDTO_BOOL = 11
const SCHEMA = 1
const MAX_ALIASES = 50
const ALIAS_MAX_OCTETS = 13
const PROCESS_MAX_OCTETS = 253
const TITLE_MAX_OCTETS = 78

export interface AliasTitleSnapshotV1Alias {
  alias: Uint8Array
  process: Uint8Array
}

/** The terminal CDTO digest authenticates these exact ordered bytes. */
export interface AliasTitleSnapshotV1Artifact {
  canonicalDigest: string
  canonicalOctets: number
  aliases: readonly AliasTitleSnapshotV1Alias[]
  /** undefined is distinct from a present, empty title. */
  title: Uint8Array | undefined
}

export class InvalidAliasTitleSnapshotV1ArtifactError extends Error {
  constructor() { super('invalid AliasTitleSnapshotV1 artifact') }
}

function u16(bytes: Uint8Array, offset: number): number {
  return bytes[offset]! * 0x100 + bytes[offset + 1]!
}

function u32(bytes: Uint8Array, offset: number): number {
  return bytes[offset]! * 0x1000000 + bytes[offset + 1]! * 0x10000 + bytes[offset + 2]! * 0x100 + bytes[offset + 3]!
}

function invalid(): never { throw new InvalidAliasTitleSnapshotV1ArtifactError() }

function fields(body: Uint8Array): readonly { id: number, type: number, value: Uint8Array }[] {
  const output: Array<{ id: number, type: number, value: Uint8Array }> = []
  for (let at = 0; at < body.length;) {
    if (body.length - at < CDTO_FIELD_HEADER_OCTETS) invalid()
    const id = u16(body, at)
    const type = body[at + 2]!
    const length = u32(body, at + 3)
    at += CDTO_FIELD_HEADER_OCTETS
    if (length > body.length - at) invalid()
    output.push({ id, type, value: body.subarray(at, at + length) })
    at += length
  }
  return output
}

function aliases(list: Uint8Array): readonly AliasTitleSnapshotV1Alias[] {
  const output: AliasTitleSnapshotV1Alias[] = []
  for (let at = 0; at < list.length;) {
    if (output.length === MAX_ALIASES || list.length - at < 3) invalid()
    const aliasLength = list[at++]!
    if (aliasLength === 0 || aliasLength > ALIAS_MAX_OCTETS || list.length - at < aliasLength + 2) invalid()
    const alias = Buffer.from(list.subarray(at, at + aliasLength))
    at += aliasLength
    const processLength = u16(list, at)
    at += 2
    if (processLength > PROCESS_MAX_OCTETS || list.length - at < processLength) invalid()
    const process = Buffer.from(list.subarray(at, at + processLength))
    at += processLength
    if (output.some((entry) => Buffer.from(entry.alias).equals(alias))) invalid()
    output.push({ alias, process })
  }
  return output
}

/**
 * Accepts only the closed AliasTitleSnapshotV1 CDTO schema: a valid kind-9
 * envelope, exact four ordered fields, and bounded ordered alias/title bytes.
 * It deliberately accepts no filename, manifest, database, or legacy file
 * syntax, so it remains an unconnected M4 parser boundary.
 */
export function parseAliasTitleSnapshotV1Artifact(bytes: Uint8Array): AliasTitleSnapshotV1Artifact {
  if (bytes.length < CDTO_PREFIX_OCTETS + CDTO_DIGEST_OCTETS || bytes.length > MAX_ALIAS_TITLE_SNAPSHOT_V1_OCTETS
    || !Buffer.from(bytes.subarray(0, 8)).equals(CDTO_MAGIC)
    || bytes[8] !== 0 || bytes[9] !== 1 || bytes[10] !== 0 || bytes[11] !== ALIAS_TITLE_SNAPSHOT_V1_KIND) invalid()
  const bodyLength = u32(bytes, 12)
  if (bodyLength + CDTO_PREFIX_OCTETS + CDTO_DIGEST_OCTETS !== bytes.length) invalid()
  const body = bytes.subarray(CDTO_PREFIX_OCTETS, CDTO_PREFIX_OCTETS + bodyLength)
  const digest = bytes.subarray(CDTO_PREFIX_OCTETS + bodyLength)
  if (!createHash('sha256').update(body).digest().equals(digest)) invalid()

  const decoded = fields(body)
  const expected = [[1, CDTO_U16, 2], [2, CDTO_BYTES], [3, CDTO_BOOL, 1], [4, CDTO_BYTES]] as const
  if (decoded.length !== expected.length || decoded.some((field, index) => {
    const [id, type, length] = expected[index]!
    return field.id !== id || field.type !== type || (length !== undefined && field.value.length !== length)
  })) invalid()
  if (u16(decoded[0]!.value, 0) !== SCHEMA) invalid()
  const titlePresent = decoded[2]!.value[0]
  if (titlePresent !== 0 && titlePresent !== 1 || decoded[3]!.value.length > TITLE_MAX_OCTETS
    || titlePresent === 0 && decoded[3]!.value.length !== 0) invalid()

  return {
    canonicalDigest: Buffer.from(digest).toString('hex'),
    canonicalOctets: bytes.length,
    aliases: aliases(decoded[1]!.value),
    title: titlePresent === 1 ? Buffer.from(decoded[3]!.value) : undefined,
  }
}
