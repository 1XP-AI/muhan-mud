import { createHmac, timingSafeEqual } from 'node:crypto'

export const MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX = 64
export const MUD1C_ADMISSION_CONTEXT_UUID_LEN = 36
export const MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX = 28
export const MUD1C_ADMISSION_CONTEXT_NONCE_BYTES = 32
export const MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN = 64
export const MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN = 64
export const MUD1C_ADMISSION_CONTEXT_MAX_WIRE = 384
export const MUD1C_ADMISSION_CONTEXT_MAX_TTL_SECONDS = 30

export class Mud1cAdmissionContextError extends Error {}
export class Mud1cAdmissionContextDisabledError extends Mud1cAdmissionContextError {}

export interface Mud1cAdmissionContext {
  worldId: string
  actorId: string
  characterId: string
  canonicalLegacyNameKey: string
  expiresAt: number
  nonce: Uint8Array
}

export interface Mud1cAdmissionContextCodecOptions {
  /** MUD1C is deliberately opt-in; the default codec refuses every operation. */
  enabled?: boolean
}

const HEX = /^[0-9a-f]+$/
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const WORLD = /^[a-z][a-z0-9_-]{0,63}$/

function disabled(): never {
  throw new Mud1cAdmissionContextDisabledError('MUD1C admission context is disabled')
}

function assertEnabled(enabled: boolean): void {
  if (!enabled) disabled()
}

function assertSecret(secret: string): void {
  if (typeof secret !== 'string' || secret.length < 32 || secret.length > 512 || !/^[\x20-\x7e]*$/.test(secret)) {
    throw new Mud1cAdmissionContextError('invalid MUD1C secret')
  }
}

function assertContext(context: Mud1cAdmissionContext): void {
  if (!context || !WORLD.test(context.worldId) || !UUID.test(context.actorId) || !UUID.test(context.characterId)) {
    throw new Mud1cAdmissionContextError('invalid MUD1C identity field')
  }
  if (typeof context.canonicalLegacyNameKey !== 'string' || context.canonicalLegacyNameKey.length < 2 ||
      context.canonicalLegacyNameKey.length > MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX ||
      context.canonicalLegacyNameKey.length % 2 !== 0 || !HEX.test(context.canonicalLegacyNameKey)) {
    throw new Mud1cAdmissionContextError('invalid MUD1C legacy-name key')
  }
  if (!Number.isSafeInteger(context.expiresAt) || context.expiresAt < 0) {
    throw new Mud1cAdmissionContextError('invalid MUD1C expiry')
  }
  if (!(context.nonce instanceof Uint8Array) || context.nonce.length !== MUD1C_ADMISSION_CONTEXT_NONCE_BYTES) {
    throw new Mud1cAdmissionContextError('invalid MUD1C nonce')
  }
}

function hmac(secret: string, signed: Uint8Array | string): string {
  if (typeof secret !== 'string' || secret.length > 512) throw new Mud1cAdmissionContextError('invalid MUD1C secret')
  return createHmac('sha256', secret).update(signed).digest('hex')
}

function signedPart(context: Mud1cAdmissionContext): string {
  return `MUD1C|${context.worldId}|${context.actorId}|${context.characterId}|${context.canonicalLegacyNameKey}|${context.expiresAt}|${Buffer.from(context.nonce).toString('hex')}`
}

function parseWire(wire: Uint8Array | string): { context: Mud1cAdmissionContext, mac: string, signed: string } {
  if (typeof wire === 'string' && [...wire].some((char) => char < ' ' || char > '~')) {
    throw new Mud1cAdmissionContextError('invalid MUD1C wire')
  }
  const bytes = typeof wire === 'string' ? Buffer.from(wire, 'ascii') : Buffer.from(wire)
  if (!bytes.length || bytes.length > MUD1C_ADMISSION_CONTEXT_MAX_WIRE || bytes.some((b) => b < 0x20 || b > 0x7e)) {
    throw new Mud1cAdmissionContextError('invalid MUD1C wire')
  }
  const fields = bytes.toString('ascii').split('|')
  if (fields.length !== 8 || fields[0] !== 'MUD1C' || fields[1] === '' || fields[6].length !== 64 || fields[7].length !== 64 || !HEX.test(fields[6]) || !HEX.test(fields[7])) {
    throw new Mud1cAdmissionContextError('invalid MUD1C wire')
  }
  const expiresAt = Number(fields[5])
  const context: Mud1cAdmissionContext = {
    worldId: fields[1], actorId: fields[2], characterId: fields[3], canonicalLegacyNameKey: fields[4],
    expiresAt, nonce: Uint8Array.from(Buffer.from(fields[6], 'hex')),
  }
  assertContext(context)
  if (String(expiresAt) !== fields[5]) throw new Mud1cAdmissionContextError('noncanonical MUD1C expiry')
  const signed = fields.slice(0, 7).join('|')
  return { context, mac: fields[7], signed }
}

/** Pure, detached codec. No socket, replay cache, persistence, or auth side effects. */
export class Mud1cAdmissionContextCodec {
  readonly enabled: boolean
  constructor(options: Mud1cAdmissionContextCodecOptions = {}) { this.enabled = options.enabled === true }

  /**
   * Compute the raw C-compatible HMAC helper result. This intentionally does
   * not enforce the admission secret contract; format and validate do.
   */
  hmacSha256Hex(secret: string, signedBytes: Uint8Array | string): string {
    assertEnabled(this.enabled)
    const bytes = typeof signedBytes === 'string' ? Buffer.from(signedBytes, 'ascii') : Buffer.from(signedBytes)
    return hmac(secret, bytes)
  }

  format(context: Mud1cAdmissionContext, secret: string): Buffer {
    assertEnabled(this.enabled); assertContext(context); assertSecret(secret)
    const signed = signedPart(context)
    const output = Buffer.from(`${signed}|${hmac(secret, signed)}`, 'ascii')
    if (output.length > MUD1C_ADMISSION_CONTEXT_MAX_WIRE) throw new Mud1cAdmissionContextError('MUD1C wire is too large')
    return output
  }

  parse(wire: Uint8Array | string): { context: Mud1cAdmissionContext, macHex: string } {
    assertEnabled(this.enabled)
    const parsed = parseWire(wire)
    return { context: parsed.context, macHex: parsed.mac }
  }

  validate(wire: Uint8Array | string, secret: string, now: number): Mud1cAdmissionContext {
    assertEnabled(this.enabled); assertSecret(secret)
    if (!Number.isSafeInteger(now)) throw new Mud1cAdmissionContextError('invalid MUD1C current time')
    const parsed = parseWire(wire)
    if (parsed.context.expiresAt < now || parsed.context.expiresAt > now + MUD1C_ADMISSION_CONTEXT_MAX_TTL_SECONDS) throw new Mud1cAdmissionContextError('MUD1C context is outside expiry window')
    const expected = Buffer.from(hmac(secret, parsed.signed), 'ascii')
    const supplied = Buffer.from(parsed.mac, 'ascii')
    if (expected.length !== supplied.length || !timingSafeEqual(expected, supplied)) throw new Mud1cAdmissionContextError('invalid MUD1C HMAC')
    return parsed.context
  }
}

export const mud1cAdmissionContextCodec = new Mud1cAdmissionContextCodec()

export function createMud1cAdmissionContextCodec(options: Mud1cAdmissionContextCodecOptions = {}): Mud1cAdmissionContextCodec {
  return new Mud1cAdmissionContextCodec(options)
}
