import { createHmac, randomBytes as nodeRandomBytes } from 'node:crypto'
import { CharacterAuthorizationError, isStrictLowerUuid } from './character-authorizer.js'

const MAX_TICKET_TTL_MS = 15_000
const MAX_TICKET_LINE_BYTES = 256

export interface AdmissionTicketInput {
  actorUserId: string
  characterId: string
  legacyNameKey: string
  jwtExpiresAtMs: number
  nowMs: number
}

export interface AdmissionTicketDependencies {
  randomBytes?: (size: number) => Buffer
}

function isValidLegacyName(value: string): boolean {
  const encoded = Buffer.from(value, 'utf8')
  if (encoded.toString('utf8') !== value) return false
  const codepoints = Array.from(value)
  if (codepoints.length < 1 || codepoints.length > 12 || encoded.length > 14) return false
  if (value === '.' || value === '..' || /[\x00-\x1f\x7f/\\:]/.test(value)) return false

  // This mirrors trusted_admission.c's final canonical form check for ASCII
  // names. Non-ASCII bytes remain untouched and are validated again by C.
  let canonical = value.replace(/[A-Z]/g, (letter) => letter.toLowerCase())
  if (/^[a-z]/.test(canonical)) canonical = canonical[0]!.toUpperCase() + canonical.slice(1)
  return canonical === value
}

export function createAdmissionTicket(
  input: AdmissionTicketInput,
  secret: string,
  dependencies: AdmissionTicketDependencies = {}
): Buffer {
  if (!isStrictLowerUuid(input.actorUserId) || !isStrictLowerUuid(input.characterId) || !isValidLegacyName(input.legacyNameKey)) {
    throw new CharacterAuthorizationError()
  }
  if (!Number.isFinite(input.nowMs) || !Number.isFinite(input.jwtExpiresAtMs) || input.jwtExpiresAtMs <= input.nowMs) {
    throw new CharacterAuthorizationError('token expiration is invalid for admission')
  }

  const expiresAtMs = Math.min(input.jwtExpiresAtMs, input.nowMs + MAX_TICKET_TTL_MS)
  const expiresUnix = Math.floor(expiresAtMs / 1_000)
  if (expiresUnix <= Math.floor(input.nowMs / 1_000) || expiresUnix > 2_147_483_647) {
    throw new CharacterAuthorizationError('token expiration is too close for admission')
  }

  const randomBytes = dependencies.randomBytes ?? nodeRandomBytes
  const nonceBytes = randomBytes(16)
  if (!Buffer.isBuffer(nonceBytes) || nonceBytes.length !== 16) throw new CharacterAuthorizationError('invalid admission nonce source')
  const nonce = nonceBytes.toString('hex')
  const nameHex = Buffer.from(input.legacyNameKey, 'utf8').toString('hex')
  const signed = `MUD1|${expiresUnix}|${nonce}|${input.actorUserId}|${input.characterId}|${nameHex}`
  const hmac = createHmac('sha256', secret).update(signed, 'ascii').digest('hex')
  const wire = Buffer.from(`${signed}|${hmac}\n`, 'ascii')
  if (wire.length > MAX_TICKET_LINE_BYTES) throw new CharacterAuthorizationError('admission ticket is too large')
  return wire
}

export const admissionTicketLimits = {
  maxTicketTtlMs: MAX_TICKET_TTL_MS,
  maxTicketLineBytes: MAX_TICKET_LINE_BYTES
} as const
