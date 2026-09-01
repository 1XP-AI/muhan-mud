import { createHmac, randomBytes as nodeRandomBytes } from 'node:crypto'
import { isStrictLowerUuid } from './character-authorizer.js'

const MAX_AUTH_FRAME_BYTES = 16 * 1024
const MAX_ACCESS_TOKEN_BYTES = 8 * 1024
const MAX_TICKET_TTL_MS = 15_000
const MAX_TICKET_LINE_BYTES = 256
const MAX_CONTROL_LINE_BYTES = 256
const MAX_STORAGE_FORMAT_BYTES = 32
const UUID_KEYS = ['type', 'accessToken', 'mode', 'correlationId'] as const

export class OnboardingProtocolError extends Error {
  constructor() {
    super('invalid onboarding protocol message')
    this.name = 'OnboardingProtocolError'
  }
}

export type OnboardingMode = 'provision' | 'claim'

export interface OnboardingAuthFrame {
  type: 'onboarding-auth'
  accessToken: string
  mode: OnboardingMode
  correlationId: string
}

function fail(): never {
  throw new OnboardingProtocolError()
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function countJsonKey(frame: string, key: string): number {
  const escaped = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return (frame.match(new RegExp(`"${escaped}"\\s*:`, 'g')) ?? []).length
}

/** Parse the only legal browser first frame. Binary frames are intentionally refused. */
export function parseOnboardingAuthFrame(frame: string | Uint8Array): OnboardingAuthFrame {
  if (typeof frame !== 'string') fail()
  if (Buffer.byteLength(frame, 'utf8') > MAX_AUTH_FRAME_BYTES) fail()

  let parsed: unknown
  try {
    parsed = JSON.parse(frame)
  } catch {
    fail()
  }
  if (!isPlainObject(parsed)) fail()

  const keys = Object.keys(parsed)
  if (keys.length !== UUID_KEYS.length || keys.some((key) => !UUID_KEYS.includes(key as typeof UUID_KEYS[number]))) fail()
  if (UUID_KEYS.some((key) => countJsonKey(frame, key) !== 1)) fail()
  if (parsed.type !== 'onboarding-auth') fail()
  if (typeof parsed.accessToken !== 'string' || parsed.accessToken.length < 1 ||
      Buffer.byteLength(parsed.accessToken, 'utf8') > MAX_ACCESS_TOKEN_BYTES || /[\x00-\x1f\x7f]/.test(parsed.accessToken)) fail()
  if (parsed.mode !== 'provision' && parsed.mode !== 'claim') fail()
  if (!isStrictLowerUuid(parsed.correlationId)) fail()
  return {
    type: 'onboarding-auth',
    accessToken: parsed.accessToken,
    mode: parsed.mode,
    correlationId: parsed.correlationId,
  }
}

export interface OnboardingTicketInput {
  mode: OnboardingMode
  userId: string
  correlationId: string
}

export interface OnboardingTicketDependencies {
  nowMs?: number | (() => number)
  ttlMs?: number
  nonce?: string | (() => string)
  randomBytes?: (size: number) => Buffer
}

function resolveNow(value: number | (() => number) | undefined): number {
  const now = typeof value === 'function' ? value() : (value ?? Date.now())
  if (!Number.isFinite(now)) fail()
  return now
}

function resolveNonce(dependencies: OnboardingTicketDependencies): string {
  if (typeof dependencies.nonce === 'string') return dependencies.nonce
  if (typeof dependencies.nonce === 'function') return dependencies.nonce()
  const randomBytes = dependencies.randomBytes ?? nodeRandomBytes
  const bytes = randomBytes(16)
  if (!Buffer.isBuffer(bytes) || bytes.length !== 16) fail()
  return bytes.toString('hex')
}

/** Create the exact ASCII MUD1O private ticket consumed by the C adapter. */
export function createOnboardingTicket(
  input: OnboardingTicketInput,
  secret: string,
  dependencies: OnboardingTicketDependencies = {},
): Buffer {
  if ((input.mode !== 'provision' && input.mode !== 'claim') ||
      !isStrictLowerUuid(input.userId) || !isStrictLowerUuid(input.correlationId) ||
      typeof secret !== 'string' || secret.length < 32 || secret.length > 512 ||
      !/^[\x20-\x7e]+$/.test(secret)) fail()

  const nowMs = resolveNow(dependencies.nowMs)
  const ttlMs = dependencies.ttlMs ?? MAX_TICKET_TTL_MS
  if (!Number.isFinite(ttlMs) || ttlMs <= 0 || ttlMs > MAX_TICKET_TTL_MS) fail()
  const expiresUnix = Math.floor((nowMs + ttlMs) / 1_000)
  if (expiresUnix <= Math.floor(nowMs / 1_000) || expiresUnix > 2_147_483_647) fail()

  const nonce = resolveNonce(dependencies)
  if (!/^[0-9a-f]{32}$/.test(nonce)) fail()
  const mode = input.mode === 'provision' ? 'P' : 'C'
  const signed = `MUD1O|${mode}|${expiresUnix}|${nonce}|${input.userId}|${input.correlationId}`
  const hmac = createHmac('sha256', secret).update(signed, 'ascii').digest('hex')
  const wire = Buffer.from(`${signed}|${hmac}\n`, 'ascii')
  if (wire.length > MAX_TICKET_LINE_BYTES) fail()
  return wire
}

type HexNameControl =
  | { type: 'RESERVE'; nameHex: string }
  | { type: 'CHALLENGE'; nameHex: string; fileSha256: string }
  | { type: 'VERIFIED'; nameHex: string; fileSha256: string }
type CharacterControl = { type: 'RESERVED' | 'CLAIMED'; characterId: string }
type SavedControl = { type: 'SAVED'; characterId: string; fileSha256: string; storageFormat: string }
export type OnboardingControl =
  | { type: 'OK' | 'ALLOW' | 'ERR' | 'COMMIT' | 'ABORT' }
  | HexNameControl
  | CharacterControl
  | SavedControl

function isLowerHex(value: string, min: number, max: number): boolean {
  return value.length >= min && value.length <= max && value.length % 2 === 0 && /^[0-9a-f]+$/.test(value)
}

function parseControlLine(line: Buffer): OnboardingControl {
  if (line.length < 1 || line.length > MAX_CONTROL_LINE_BYTES || line.some((byte) => byte < 0x20 || byte > 0x7e)) fail()
  const text = line.toString('ascii')
  if (!text.startsWith('MUD1O ')) fail()
  const body = text.slice(6)
  if (body === 'OK') return { type: 'OK' }
  if (body === 'ALLOW') return { type: 'ALLOW' }
  if (body === 'ERR') return { type: 'ERR' }
  if (body === 'COMMIT') return { type: 'COMMIT' }
  if (body === 'ABORT') return { type: 'ABORT' }

  const parts = body.split('|')
  if (parts[0] === 'RESERVE' && parts.length === 2 && isLowerHex(parts[1]!, 2, 28)) {
    return { type: 'RESERVE', nameHex: parts[1]! }
  }
  if (parts[0] === 'CHALLENGE' && parts.length === 3 && isLowerHex(parts[1]!, 2, 28) && /^[0-9a-f]{64}$/.test(parts[2]!)) {
    return { type: 'CHALLENGE', nameHex: parts[1]!, fileSha256: parts[2]! }
  }
  if (parts[0] === 'RESERVED' && parts.length === 2 && isStrictLowerUuid(parts[1])) {
    return { type: 'RESERVED', characterId: parts[1]! }
  }
  if (parts[0] === 'VERIFIED' && parts.length === 3 && isLowerHex(parts[1]!, 2, 28) && /^[0-9a-f]{64}$/.test(parts[2]!)) {
    return { type: 'VERIFIED', nameHex: parts[1]!, fileSha256: parts[2]! }
  }
  if (parts[0] === 'CLAIMED' && parts.length === 2 && isStrictLowerUuid(parts[1])) {
    return { type: 'CLAIMED', characterId: parts[1]! }
  }
  if (parts[0] === 'SAVED' && parts.length === 4 && isStrictLowerUuid(parts[1]) &&
      /^[0-9a-f]{64}$/.test(parts[2]!) && Buffer.byteLength(parts[3]!, 'ascii') > 0 &&
      Buffer.byteLength(parts[3]!, 'ascii') <= MAX_STORAGE_FORMAT_BYTES && /^[\x20-\x7e]+$/.test(parts[3]!)) {
    return { type: 'SAVED', characterId: parts[1]!, fileSha256: parts[2]!, storageFormat: parts[3]! }
  }
  fail()
}

/** Incremental parser for private C control lines; it supports coalesced output. */
export class OnboardingControlLineParser {
  private pending = Buffer.alloc(0)

  push(chunk: string | Uint8Array): OnboardingControl[] {
    const bytes = typeof chunk === 'string' ? Buffer.from(chunk, 'utf8') : Buffer.from(chunk)
    if (bytes.some((byte) => byte === 0 || byte > 0x7f)) fail()
    this.pending = Buffer.concat([this.pending, bytes])
    const output: OnboardingControl[] = []
    while (true) {
      const newline = this.pending.indexOf(0x0a)
      if (newline < 0) {
        if (this.pending.length > MAX_CONTROL_LINE_BYTES) fail()
        break
      }
      if (newline > MAX_CONTROL_LINE_BYTES || (newline > 0 && this.pending[newline - 1] === 0x0d)) fail()
      output.push(parseControlLine(this.pending.subarray(0, newline)))
      this.pending = this.pending.subarray(newline + 1)
    }
    return output
  }
}

/** Splits only reserved MUD1O lines from arbitrary Telnet/game bytes, including fragmented prefixes. */
export class OnboardingControlDemultiplexer {
  private pending = Buffer.alloc(0)
  private readonly controls = new OnboardingControlLineParser()

  push(chunk: Uint8Array): { controls: OnboardingControl[], game: Buffer[] } {
    this.pending = Buffer.concat([this.pending, Buffer.from(chunk)])
    const output: { controls: OnboardingControl[], game: Buffer[] } = { controls: [], game: [] }
    while (this.pending.length > 0) {
      const index = this.pending.indexOf('MUD1O ', 0, 'ascii')
      if (index < 0) {
        let retain = 0
        for (let size = Math.min(5, this.pending.length); size > 0; size--) {
          if (this.pending.subarray(this.pending.length - size).equals(Buffer.from('MUD1O '.slice(0, size), 'ascii'))) { retain = size; break }
        }
        const game = this.pending.subarray(0, this.pending.length - retain)
        if (game.length) output.game.push(game)
        this.pending = this.pending.subarray(this.pending.length - retain)
        break
      }
      if (index > 0) { output.game.push(this.pending.subarray(0, index)); this.pending = this.pending.subarray(index); continue }
      const newline = this.pending.indexOf(0x0a)
      if (newline < 0) { if (this.pending.length > MAX_CONTROL_LINE_BYTES) fail(); break }
      const line = this.pending.subarray(0, newline + 1)
      output.controls.push(...this.controls.push(line))
      this.pending = this.pending.subarray(newline + 1)
    }
    return output
  }

  /** Once the provisioning transaction is committed, retained prefix bytes are ordinary game output. */
  drainGame(): Buffer {
    const pending = this.pending
    this.pending = Buffer.alloc(0)
    return pending
  }
}

export type OnboardingProtocolState = 'RESERVE' | 'RESERVED' | 'CHALLENGE' | 'ALLOW' | 'VERIFIED' | 'CLAIMED' | 'SAVED' | 'COMMIT' | 'ERR' | 'ABORT'
const LEGAL_TRANSITIONS: Readonly<Record<OnboardingProtocolState, readonly OnboardingProtocolState[]>> = {
  RESERVE: ['RESERVED', 'ERR'], RESERVED: ['SAVED', 'ERR'], CHALLENGE: ['ALLOW', 'ERR'], ALLOW: ['VERIFIED', 'ERR'], VERIFIED: ['CLAIMED', 'ERR'],
  CLAIMED: [], SAVED: ['COMMIT', 'ERR'], COMMIT: [], ERR: ['ABORT'], ABORT: [],
}

export function canTransitionOnboardingState(from: OnboardingProtocolState, to: OnboardingProtocolState): boolean {
  return LEGAL_TRANSITIONS[from]?.includes(to) ?? false
}

export function assertLegalOnboardingTransition(from: OnboardingProtocolState, to: OnboardingProtocolState): void {
  if (!canTransitionOnboardingState(from, to)) fail()
}

const SAFE_LOG_VALUES: Readonly<Record<string, ReadonlySet<string>>> = {
  mode: new Set(['provision', 'claim']),
  phase: new Set(['authenticating', 'beginning', 'connecting', 'reserving', 'verifying', 'saving', 'finalizing', 'reconciling', 'ready', 'closed']),
  state: new Set(['RESERVE', 'RESERVED', 'CHALLENGE', 'ALLOW', 'VERIFIED', 'CLAIMED', 'SAVED', 'COMMIT', 'ERR', 'ABORT']),
  controlType: new Set(['OK', 'CHALLENGE', 'ALLOW', 'RESERVE', 'RESERVED', 'VERIFIED', 'CLAIMED', 'SAVED', 'COMMIT', 'ERR', 'ABORT']),
  reasonCode: new Set(['invalid-frame', 'authentication-failed', 'authorization-failed', 'protocol-error', 'timeout', 'upstream-unavailable', 'conflict', 'aborted', 'internal']),
}
export function sanitizeOnboardingLogMetadata(input: Record<string, unknown>): Record<string, string> {
  const output: Record<string, string> = {}
  for (const [key, value] of Object.entries(input)) {
    if (typeof value !== 'string') continue
    if (key === 'correlationId') {
      if (isStrictLowerUuid(value)) output[key] = value
      continue
    }
    if (SAFE_LOG_VALUES[key]?.has(value)) output[key] = value
  }
  return output
}

export const onboardingProtocolLimits = {
  maxAuthFrameBytes: MAX_AUTH_FRAME_BYTES,
  maxAccessTokenBytes: MAX_ACCESS_TOKEN_BYTES,
  maxTicketTtlMs: MAX_TICKET_TTL_MS,
  maxTicketLineBytes: MAX_TICKET_LINE_BYTES,
  maxControlLineBytes: MAX_CONTROL_LINE_BYTES,
  maxStorageFormatBytes: MAX_STORAGE_FORMAT_BYTES,
} as const
