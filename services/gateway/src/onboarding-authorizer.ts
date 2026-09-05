import type { GatewayConfig } from './config.js'
import { isStrictLowerUuid } from './character-authorizer.js'
import type { OnboardingMode } from './onboarding-protocol.js'

const SHA256_RE = /^[0-9a-f]{64}$/
const SAFE_WORLD_RE = /^(?=.{1,64}$)[^\x00-\x1f\x7f]+$/
const SAFE_NAME_RE = /^(?=.{1,12}$)[^\x00-\x1f\x7f/\\:]+$/
const MAX_RPC_JSON_BYTES = 64 * 1024
const MAX_RPC_TIMEOUT_MS = 5_000
const CLAIM_ALLOW_TTL_MS = 90_000

export class OnboardingAuthorizationError extends Error {
  constructor(public readonly indeterminate = false) { super('onboarding authorization was refused'); this.name = 'OnboardingAuthorizationError' }
}

export interface BeginOnboardingRequest { actorUserId: string; correlationId: string; mode: OnboardingMode; expiresAt: Date }
export interface CancelUnreservedOnboardingRequest { actorUserId: string; correlationId: string }
export interface ReserveOnboardingRequest { actorUserId: string; correlationId: string; worldId: string; legacyName: string }
export interface FinalizeOnboardingRequest { actorUserId: string; correlationId: string; characterId: string; fileSha256: string; storageFormat: string }
export interface ClaimOnboardingRequest { actorUserId: string; correlationId: string; worldId: string; legacyNameKey: string; fileSha256: string }
export interface ActivateOnboardingHandoffRequest { actorUserId: string; correlationId: string; characterId: string; mode: OnboardingMode }
export interface BindSnapshotCommandRequest { actorUserId: string; correlationId: string; characterId: string; mode: OnboardingMode; commandId: string }
export interface ChallengeOnboardingRequest { actorUserId: string; correlationId: string; worldId: string; legacyNameKey: string; fileSha256: string }
export interface ChallengeOnboardingResult { characterId: string; legacyNameKey: string; fileSha256: string; allowExpiresAtMs: number }
export interface OnboardingAuthorizer {
  begin(request: BeginOnboardingRequest): Promise<void>
  cancelUnreserved(request: CancelUnreservedOnboardingRequest): Promise<void>
  reserve(request: ReserveOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string }>
  finalize(request: FinalizeOnboardingRequest): Promise<{ characterId: string }>
  reconcile(request: FinalizeOnboardingRequest): Promise<{ characterId: string }>
  challenge(request: ChallengeOnboardingRequest): Promise<ChallengeOnboardingResult>
  claim(request: ClaimOnboardingRequest): Promise<{ characterId: string }>
  activateHandoff(request: ActivateOnboardingHandoffRequest): Promise<{ characterId: string }>
  bindSnapshotCommand(request: BindSnapshotCommandRequest): Promise<void>
}

function configured(config: GatewayConfig): { url: string; key: string } {
  if (!config.supabaseInternalRestUrl || !config.supabaseServiceRoleKey) throw new OnboardingAuthorizationError()
  return { url: config.supabaseInternalRestUrl, key: config.supabaseServiceRoleKey }
}
function oneRow(body: unknown, expectedKeys: readonly string[]): Record<string, unknown> {
  if (!Array.isArray(body) || body.length !== 1 || !body[0] || typeof body[0] !== 'object' || Array.isArray(body[0])) throw new OnboardingAuthorizationError()
  const row = body[0] as Record<string, unknown>
  const keys = Object.keys(row)
  if (keys.length !== expectedKeys.length || keys.some((key) => !expectedKeys.includes(key))) throw new OnboardingAuthorizationError()
  return row
}
async function jsonResponse(response: Response): Promise<unknown> {
  const contentType = response.headers.get('content-type')
  const contentLength = response.headers.get('content-length')
  if (!contentType || !/^application\/json(?:\s*;|$)/i.test(contentType) || (contentLength !== null && (!/^\d+$/.test(contentLength) || Number(contentLength) > MAX_RPC_JSON_BYTES))) throw new OnboardingAuthorizationError()
  const reader = response.body?.getReader()
  if (!reader) throw new OnboardingAuthorizationError()
  const chunks: Uint8Array[] = []
  let total = 0
  while (true) {
    const next = await reader.read()
    if (next.done) break
    total += next.value.byteLength
    if (total > MAX_RPC_JSON_BYTES) throw new OnboardingAuthorizationError()
    chunks.push(next.value)
  }
  const bytes = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength }
  return JSON.parse(new TextDecoder().decode(bytes))
}
function validIntentExpiry(value: unknown, requested: Date, now: number): boolean {
  if (typeof value !== 'string') return false
  const returned = Date.parse(value)
  return Number.isFinite(returned) && returned > now && returned <= requested.getTime() + 1000
}
function validBase(actor: string, correlation: string): boolean { return isStrictLowerUuid(actor) && isStrictLowerUuid(correlation) }
function validAllowExpiry(value: unknown, now: number): number | undefined {
  if (typeof value !== 'string') return undefined
  const expiresAtMs = Date.parse(value)
  if (!Number.isFinite(expiresAtMs) || expiresAtMs <= now || expiresAtMs > now + CLAIM_ALLOW_TTL_MS) return undefined
  return expiresAtMs
}

/** A service-only PostgREST boundary. No table routes or generic CRUD are exposed. */
export class SupabaseOnboardingAuthorizer implements OnboardingAuthorizer {
  private readonly internalRestUrl: string
  private readonly serviceRoleKey: string
  private readonly rpcTimeoutMs: number
  constructor(config: GatewayConfig, private readonly fetchImpl: typeof fetch = fetch, private readonly now: () => number = Date.now) {
    const values = configured(config); this.internalRestUrl = values.url; this.serviceRoleKey = values.key
    this.rpcTimeoutMs = Math.min(config.authTimeoutMs, MAX_RPC_TIMEOUT_MS)
  }
  async begin(request: BeginOnboardingRequest): Promise<void> {
    const now = this.now()
    if (!validBase(request.actorUserId, request.correlationId) || (request.mode !== 'provision' && request.mode !== 'claim') || !Number.isFinite(request.expiresAt.getTime()) || request.expiresAt.getTime() <= now || request.expiresAt.getTime() > now + 30 * 60_000) throw new OnboardingAuthorizationError()
    const row = await this.rpc('begin_game_character_onboarding', { p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId, p_mode: request.mode, p_expires_at: request.expiresAt.toISOString() }, ['correlation_id', 'actor_user_id', 'mode', 'status', 'expires_at'])
    if (row.correlation_id !== request.correlationId || row.actor_user_id !== request.actorUserId || row.mode !== request.mode || !['started', 'provisioning', 'finalized'].includes(String(row.status)) || !validIntentExpiry(row.expires_at, request.expiresAt, now)) throw new OnboardingAuthorizationError()
  }
  async cancelUnreserved(request: CancelUnreservedOnboardingRequest): Promise<void> {
    if (!validBase(request.actorUserId, request.correlationId)) throw new OnboardingAuthorizationError()
    const row = await this.rpc('cancel_unreserved_game_character_onboarding', { p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId }, ['correlation_id', 'actor_user_id', 'status', 'completed_at'])
    if (row.correlation_id !== request.correlationId || row.actor_user_id !== request.actorUserId || row.status !== 'cancelled' || typeof row.completed_at !== 'string' || !Number.isFinite(Date.parse(row.completed_at))) throw new OnboardingAuthorizationError()
  }
  async reserve(request: ReserveOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string }> {
    if (!validBase(request.actorUserId, request.correlationId) || !SAFE_WORLD_RE.test(request.worldId) || !SAFE_NAME_RE.test(request.legacyName)) throw new OnboardingAuthorizationError()
    const row = await this.rpc('begin_game_character_provisioning', { p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId, p_world_id: request.worldId, p_legacy_name: request.legacyName }, ['character_id', 'correlation_id', 'actor_user_id', 'world_id', 'legacy_name_key', 'lifecycle', 'status', 'expires_at'])
    if (!isStrictLowerUuid(row.character_id) || row.correlation_id !== request.correlationId || row.actor_user_id !== request.actorUserId || row.world_id !== request.worldId || row.legacy_name_key !== request.legacyName || row.lifecycle !== 'provisioning' || row.status !== 'reserved') throw new OnboardingAuthorizationError()
    return { characterId: row.character_id, legacyNameKey: row.legacy_name_key }
  }
  async finalize(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> { return this.complete('finalize_game_character_provisioning', request) }
  async reconcile(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> { return this.complete('reconcile_game_character_provisioning', request) }
  async challenge(request: ChallengeOnboardingRequest): Promise<ChallengeOnboardingResult> {
    if (!validBase(request.actorUserId, request.correlationId) || !SAFE_WORLD_RE.test(request.worldId) || !SAFE_NAME_RE.test(request.legacyNameKey) || !SHA256_RE.test(request.fileSha256)) throw new OnboardingAuthorizationError()
    const row = await this.rpc('challenge_legacy_game_character_onboarding', { p_world_id: request.worldId, p_legacy_name_key: request.legacyNameKey, p_imported_file_sha256: request.fileSha256, p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId }, ['character_id', 'legacy_name_key', 'imported_file_sha256', 'challenge_status', 'allow_expires_at'])
    const allowExpiresAtMs = validAllowExpiry(row.allow_expires_at, this.now())
    if (!isStrictLowerUuid(row.character_id) || row.legacy_name_key !== request.legacyNameKey || row.imported_file_sha256 !== request.fileSha256 || row.challenge_status !== 'allowed' || allowExpiresAtMs === undefined) throw new OnboardingAuthorizationError()
    return { characterId: row.character_id, legacyNameKey: request.legacyNameKey, fileSha256: request.fileSha256, allowExpiresAtMs }
  }
  async claim(request: ClaimOnboardingRequest): Promise<{ characterId: string }> {
    if (!validBase(request.actorUserId, request.correlationId) || !SAFE_WORLD_RE.test(request.worldId) || !SAFE_NAME_RE.test(request.legacyNameKey) || !SHA256_RE.test(request.fileSha256)) throw new OnboardingAuthorizationError()
    const row = await this.rpc('claim_legacy_game_character_onboarding', { p_world_id: request.worldId, p_legacy_name_key: request.legacyNameKey, p_imported_file_sha256: request.fileSha256, p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId }, ['character_id', 'lifecycle', 'owner_user_id', 'claimed_at', 'onboarding_status', 'imported_file_sha256'])
    if (!isStrictLowerUuid(row.character_id) || row.lifecycle !== 'handoff_pending' || row.owner_user_id !== request.actorUserId || row.onboarding_status !== 'finalized' || row.imported_file_sha256 !== request.fileSha256 || typeof row.claimed_at !== 'string' || !Number.isFinite(Date.parse(row.claimed_at))) throw new OnboardingAuthorizationError()
    return { characterId: row.character_id }
  }
  async activateHandoff(request: ActivateOnboardingHandoffRequest): Promise<{ characterId: string }> {
    if (!validBase(request.actorUserId, request.correlationId) || !isStrictLowerUuid(request.characterId) || (request.mode !== 'provision' && request.mode !== 'claim')) throw new OnboardingAuthorizationError()
    const row = await this.rpc('activate_game_character_onboarding_handoff', {
      p_actor_user_id: request.actorUserId,
      p_correlation_id: request.correlationId,
      p_character_id: request.characterId,
      p_mode: request.mode,
    }, ['character_id', 'actor_user_id', 'correlation_id', 'lifecycle', 'onboarding_status'])
    if (row.character_id !== request.characterId || row.actor_user_id !== request.actorUserId || row.correlation_id !== request.correlationId || row.lifecycle !== 'active' || row.onboarding_status !== 'finalized') throw new OnboardingAuthorizationError()
    return { characterId: request.characterId }
  }
  async bindSnapshotCommand(request: BindSnapshotCommandRequest): Promise<void> {
    if (!validBase(request.actorUserId, request.correlationId) || !isStrictLowerUuid(request.characterId) ||
      !isStrictLowerUuid(request.commandId) || (request.mode !== 'provision' && request.mode !== 'claim')) throw new OnboardingAuthorizationError()
    const row = await this.rpc('register_game_character_onboarding_snapshot_command_binding', {
      p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId,
      p_character_id: request.characterId, p_mode: request.mode, p_command_id: request.commandId,
    }, ['outcome'])
    if (row.outcome !== 'BOUND' && row.outcome !== 'EXACT_RETRY') throw new OnboardingAuthorizationError()
  }
  private async complete(name: 'finalize_game_character_provisioning' | 'reconcile_game_character_provisioning', request: FinalizeOnboardingRequest): Promise<{ characterId: string }> {
    if (!validBase(request.actorUserId, request.correlationId) || !isStrictLowerUuid(request.characterId) || !SHA256_RE.test(request.fileSha256) || request.storageFormat !== 'player-v1') throw new OnboardingAuthorizationError()
    const row = await this.rpc(name, { p_actor_user_id: request.actorUserId, p_correlation_id: request.correlationId, p_saved_file_sha256: request.fileSha256, p_storage_format: 1 }, ['character_id', 'actor_user_id', 'lifecycle', 'status', 'saved_file_sha256', 'storage_format'])
    if (row.character_id !== request.characterId || row.actor_user_id !== request.actorUserId || row.lifecycle !== 'handoff_pending' || row.status !== 'finalized' || row.saved_file_sha256 !== request.fileSha256 || row.storage_format !== 1) throw new OnboardingAuthorizationError()
    return { characterId: request.characterId }
  }
  private async rpc(name: string, body: Record<string, unknown>, expectedKeys: readonly string[]): Promise<Record<string, unknown>> {
    const controller = new AbortController()
    // Keep this short deadline referenced so a pending RPC cannot let its
    // worker exit before the request is deterministically aborted.
    const timer = setTimeout(() => controller.abort(), this.rpcTimeoutMs)
    try {
      let response: Response
      try {
        response = await this.fetchImpl(new URL(`/rpc/${name}`, this.internalRestUrl), { method: 'POST', redirect: 'error', signal: controller.signal, headers: { authorization: `Bearer ${this.serviceRoleKey}`, apikey: this.serviceRoleKey, 'content-type': 'application/json' }, body: JSON.stringify(body) })
      } catch {
        // A network/abort failure may have committed the RPC. The caller may
        // perform the one exact-correlation retry allowed for final claims.
        throw new OnboardingAuthorizationError(true)
      }
      if (!response.ok) throw new OnboardingAuthorizationError()
      return oneRow(await jsonResponse(response), expectedKeys)
    } catch (error) {
      if (error instanceof OnboardingAuthorizationError) throw error
      throw new OnboardingAuthorizationError()
    } finally {
      clearTimeout(timer)
    }
  }
}

/** Explicitly limited fixture for isolated AUTH_DISABLED gateway tests. */
export class TestOnlyOnboardingAuthorizer implements OnboardingAuthorizer {
  async begin(_request: BeginOnboardingRequest): Promise<void> {}
  async cancelUnreserved(_request: CancelUnreservedOnboardingRequest): Promise<void> {}
  async reserve(request: ReserveOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string }> {
    return { characterId: '00000000-0000-4000-8000-000000000002', legacyNameKey: request.legacyName }
  }
  async finalize(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> { return { characterId: request.characterId } }
  async reconcile(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> { return { characterId: request.characterId } }
  async challenge(request: ChallengeOnboardingRequest): Promise<ChallengeOnboardingResult> {
    return { characterId: '00000000-0000-4000-8000-000000000002', legacyNameKey: request.legacyNameKey, fileSha256: request.fileSha256, allowExpiresAtMs: Date.now() + CLAIM_ALLOW_TTL_MS }
  }
  async claim(_request: ClaimOnboardingRequest): Promise<{ characterId: string }> { return { characterId: '00000000-0000-4000-8000-000000000002' } }
  async activateHandoff(request: ActivateOnboardingHandoffRequest): Promise<{ characterId: string }> { return { characterId: request.characterId } }
  async bindSnapshotCommand(_request: BindSnapshotCommandRequest): Promise<void> {}
}
