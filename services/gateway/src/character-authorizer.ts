import type { GatewayConfig } from './config.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const MAX_RPC_JSON_BYTES = 64 * 1024
const MAX_RPC_TIMEOUT_MS = 5_000
const LEASE_KEYS = ['character_id', 'session_id', 'expires_at', 'owner_user_id', 'lifecycle', 'legacy_name_key'] as const

export interface BeginCharacterSessionRequest {
  actorUserId: string
  characterId: string
  sessionId: string
  gatewayInstanceId: string
  expiresAt: Date
}

export interface AuthorizedCharacter {
  legacyNameKey: string
}

export interface RenewCharacterSessionRequest {
  sessionId: string
  gatewayInstanceId: string
  expiresAt: Date
}

export interface RenewedCharacterSession {
  sessionId: string
  actorUserId: string
  characterId: string
  legacyNameKey: string
  lifecycle: 'active'
  expiresAtMs: number
}

export interface CharacterAuthorizer {
  beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter>
  renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession>
  endSession(sessionId: string, gatewayInstanceId: string): Promise<void>
}

export class CharacterAuthorizationError extends Error {
  constructor(message = 'character admission was refused') {
    super(message)
    this.name = 'CharacterAuthorizationError'
  }
}

interface LeaseResponse {
  character_id: unknown
  session_id: unknown
  expires_at: unknown
  owner_user_id: unknown
  lifecycle: unknown
  legacy_name_key: unknown
}

function parseLeaseExpiry(value: unknown, expected: Date): number {
  const expiresAtMs = typeof value === 'string' ? Date.parse(value) : Number.NaN
  if (!Number.isFinite(expiresAtMs) || Math.abs(expiresAtMs - expected.getTime()) > 1_000) {
    throw new CharacterAuthorizationError()
  }
  return expiresAtMs
}

function requiredConfig(config: GatewayConfig): Required<Pick<GatewayConfig,
  'supabaseInternalRestUrl' | 'supabaseServiceRoleKey'>> {
  if (!config.supabaseInternalRestUrl || !config.supabaseServiceRoleKey) {
    throw new CharacterAuthorizationError('character authorizer is not configured')
  }
  return {
    supabaseInternalRestUrl: config.supabaseInternalRestUrl,
    supabaseServiceRoleKey: config.supabaseServiceRoleKey
  }
}

function isStrictUuid(value: unknown): value is string {
  return typeof value === 'string' && UUID_RE.test(value)
}

function validGatewayInstanceId(value: unknown): value is string {
  return typeof value === 'string' && value.trim().length > 0 && value.length <= 128 && !/[\x00-\x1f\x7f]/.test(value)
}

function validLeaseExpiryInput(value: unknown, now: number): value is Date {
  if (!(value instanceof Date)) return false
  const expiresAtMs = value.getTime()
  return Number.isFinite(expiresAtMs) && expiresAtMs > now && expiresAtMs <= now + 5 * 60_000
}

function oneLeaseRow(body: unknown): LeaseResponse {
  if (!Array.isArray(body) || body.length !== 1 || !body[0] || typeof body[0] !== 'object' || Array.isArray(body[0])) throw new CharacterAuthorizationError()
  const row = body[0] as Record<string, unknown>
  const keys = Object.keys(row)
  if (keys.length !== LEASE_KEYS.length || keys.some((key) => !LEASE_KEYS.includes(key as typeof LEASE_KEYS[number]))) throw new CharacterAuthorizationError()
  return row as unknown as LeaseResponse
}

async function jsonResponse(response: Response): Promise<unknown> {
  const contentType = response.headers.get('content-type')
  const contentLength = response.headers.get('content-length')
  if (!contentType || !/^application\/json(?:\s*;|$)/i.test(contentType) || (contentLength !== null && (!/^\d+$/.test(contentLength) || Number(contentLength) > MAX_RPC_JSON_BYTES))) throw new CharacterAuthorizationError()
  const reader = response.body?.getReader()
  if (!reader) throw new CharacterAuthorizationError()
  const chunks: Uint8Array[] = []
  let total = 0
  while (true) {
    const next = await reader.read()
    if (next.done) break
    total += next.value.byteLength
    if (total > MAX_RPC_JSON_BYTES) throw new CharacterAuthorizationError()
    chunks.push(next.value)
  }
  const bytes = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength }
  return JSON.parse(new TextDecoder().decode(bytes))
}

/**
 * Service-key-only PostgREST client. It intentionally exposes no generic CRUD
 * operation: the Gateway may obtain an owner-checked active lease and its
 * canonical C name, nothing more. The RPC returns those values from the same
 * transaction/row lock; a second SELECT would widen a time-of-check gap.
 */
export class SupabaseCharacterAuthorizer implements CharacterAuthorizer {
  private readonly internalRestUrl: string
  private readonly serviceRoleKey: string
  private readonly rpcTimeoutMs: number

  constructor(config: GatewayConfig, private readonly fetchImpl: typeof fetch = fetch, private readonly now: () => number = Date.now) {
    const required = requiredConfig(config)
    this.internalRestUrl = required.supabaseInternalRestUrl
    this.serviceRoleKey = required.supabaseServiceRoleKey
    this.rpcTimeoutMs = Math.min(config.authTimeoutMs, MAX_RPC_TIMEOUT_MS)
  }

  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    if (!isStrictUuid(request.actorUserId) || !isStrictUuid(request.characterId) || !isStrictUuid(request.sessionId) ||
      !validGatewayInstanceId(request.gatewayInstanceId) || !validLeaseExpiryInput(request.expiresAt, this.now())) {
      throw new CharacterAuthorizationError()
    }

    const lease = await this.callLeaseRpc(request)
    if (lease.character_id !== request.characterId || lease.session_id !== request.sessionId ||
      lease.owner_user_id !== request.actorUserId || lease.lifecycle !== 'active' ||
      typeof lease.legacy_name_key !== 'string') {
      throw new CharacterAuthorizationError()
    }
    parseLeaseExpiry(lease.expires_at, request.expiresAt)
    return { legacyNameKey: lease.legacy_name_key }
  }

  async endSession(sessionId: string, gatewayInstanceId: string): Promise<void> {
    if (!isStrictUuid(sessionId) || !validGatewayInstanceId(gatewayInstanceId)) throw new CharacterAuthorizationError()
    const result = await this.rpc('end_game_character_session', { p_session_id: sessionId, p_gateway_instance_id: gatewayInstanceId })
    if (typeof result !== 'boolean') throw new CharacterAuthorizationError()
  }

  async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    if (!isStrictUuid(request.sessionId) || !validGatewayInstanceId(request.gatewayInstanceId) || !validLeaseExpiryInput(request.expiresAt, this.now())) throw new CharacterAuthorizationError()
    const row = oneLeaseRow(await this.rpc('renew_game_character_session', {
      p_session_id: request.sessionId,
      p_gateway_instance_id: request.gatewayInstanceId,
      p_expires_at: request.expiresAt.toISOString()
    }))
    if (row.session_id !== request.sessionId || !isStrictUuid(row.owner_user_id) ||
      !isStrictUuid(row.character_id) || typeof row.legacy_name_key !== 'string' ||
      row.lifecycle !== 'active') throw new CharacterAuthorizationError()
    const expiresAtMs = parseLeaseExpiry(row.expires_at, request.expiresAt)
    return {
      sessionId: row.session_id,
      actorUserId: row.owner_user_id,
      characterId: row.character_id,
      legacyNameKey: row.legacy_name_key,
      lifecycle: row.lifecycle,
      expiresAtMs
    }
  }

  private async callLeaseRpc(request: BeginCharacterSessionRequest): Promise<LeaseResponse> {
    return oneLeaseRow(await this.rpc('begin_game_character_session', {
      p_actor_user_id: request.actorUserId,
      p_character_id: request.characterId,
      p_session_id: request.sessionId,
      p_gateway_instance_id: request.gatewayInstanceId,
      p_expires_at: request.expiresAt.toISOString()
    }))
  }

  private async rpc(name: string, body: Record<string, unknown>): Promise<unknown> {
    const controller = new AbortController()
    // Keep this short deadline referenced: it is the authoritative lifecycle
    // handle when the fetch implementation has no other active socket/timer.
    const timer = setTimeout(() => controller.abort(), this.rpcTimeoutMs)
    try {
      const response = await this.fetchImpl(new URL(`/rpc/${name}`, this.internalRestUrl), {
        method: 'POST', redirect: 'error', signal: controller.signal, headers: this.headers(), body: JSON.stringify(body)
      })
      if (!response.ok) throw new CharacterAuthorizationError()
      return await jsonResponse(response)
    } catch {
      throw new CharacterAuthorizationError()
    } finally {
      clearTimeout(timer)
    }
  }

  private headers(): Record<string, string> {
    return {
      authorization: `Bearer ${this.serviceRoleKey}`,
      apikey: this.serviceRoleKey,
      'content-type': 'application/json'
    }
  }
}

/** Explicit fixture for AUTH_DISABLED tests; never constructed outside NODE_ENV=test. */
export class TestOnlyCharacterAuthorizer implements CharacterAuthorizer {
  static readonly actorUserId = '00000000-0000-4000-8000-000000000001'
  static readonly characterId = '00000000-0000-4000-8000-000000000002'

  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    if (request.actorUserId !== TestOnlyCharacterAuthorizer.actorUserId || request.characterId !== TestOnlyCharacterAuthorizer.characterId) {
      throw new CharacterAuthorizationError()
    }
    return { legacyNameKey: 'Test' }
  }

  async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    return {
      sessionId: request.sessionId,
      actorUserId: TestOnlyCharacterAuthorizer.actorUserId,
      characterId: TestOnlyCharacterAuthorizer.characterId,
      legacyNameKey: 'Test',
      lifecycle: 'active',
      expiresAtMs: request.expiresAt.getTime()
    }
  }

  async endSession(_sessionId: string, _gatewayInstanceId: string): Promise<void> {}
}

export function isStrictLowerUuid(value: unknown): value is string {
  return isStrictUuid(value)
}
