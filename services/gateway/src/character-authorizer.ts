import type { GatewayConfig } from './config.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

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
  character_id?: unknown
  session_id?: unknown
  expires_at?: unknown
  owner_user_id?: unknown
  lifecycle?: unknown
  legacy_name_key?: unknown
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

async function parseArray(response: Response): Promise<unknown[]> {
  let body: unknown
  try {
    body = await response.json()
  } catch {
    throw new CharacterAuthorizationError()
  }
  if (!Array.isArray(body)) throw new CharacterAuthorizationError()
  return body
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

  constructor(config: GatewayConfig, private readonly fetchImpl: typeof fetch = fetch) {
    const required = requiredConfig(config)
    this.internalRestUrl = required.supabaseInternalRestUrl
    this.serviceRoleKey = required.supabaseServiceRoleKey
  }

  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    if (!isStrictUuid(request.actorUserId) || !isStrictUuid(request.characterId) || !isStrictUuid(request.sessionId)) {
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
    if (!isStrictUuid(sessionId) || gatewayInstanceId.trim().length === 0 || gatewayInstanceId.length > 128 ||
        /[\x00-\x1f\x7f]/.test(gatewayInstanceId)) return
    let response: Response
    try {
      response = await this.fetchImpl(new URL('/rpc/end_game_character_session', this.internalRestUrl), {
        method: 'POST',
        headers: this.headers(),
        body: JSON.stringify({
          p_session_id: sessionId,
          p_gateway_instance_id: gatewayInstanceId
        })
      })
    } catch {
      throw new CharacterAuthorizationError()
    }
    if (!response.ok) throw new CharacterAuthorizationError()
  }

  async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    if (!isStrictUuid(request.sessionId)) throw new CharacterAuthorizationError()
    let response: Response
    try {
      response = await this.fetchImpl(new URL('/rpc/renew_game_character_session', this.internalRestUrl), {
        method: 'POST',
        headers: this.headers(),
        body: JSON.stringify({
          p_session_id: request.sessionId,
          p_gateway_instance_id: request.gatewayInstanceId,
          p_expires_at: request.expiresAt.toISOString()
        })
      })
    } catch {
      throw new CharacterAuthorizationError()
    }
    if (!response.ok) throw new CharacterAuthorizationError()
    const rows = await parseArray(response)
    if (rows.length !== 1 || !rows[0] || typeof rows[0] !== 'object') throw new CharacterAuthorizationError()
    const row = rows[0] as LeaseResponse
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
    let response: Response
    try {
      response = await this.fetchImpl(new URL('/rpc/begin_game_character_session', this.internalRestUrl), {
        method: 'POST',
        headers: this.headers(),
        body: JSON.stringify({
          p_actor_user_id: request.actorUserId,
          p_character_id: request.characterId,
          p_session_id: request.sessionId,
          p_gateway_instance_id: request.gatewayInstanceId,
          p_expires_at: request.expiresAt.toISOString()
        })
      })
    } catch {
      throw new CharacterAuthorizationError()
    }
    if (!response.ok) throw new CharacterAuthorizationError()
    const leases = await parseArray(response)
    if (leases.length !== 1 || !leases[0] || typeof leases[0] !== 'object') throw new CharacterAuthorizationError()
    return leases[0] as LeaseResponse
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
