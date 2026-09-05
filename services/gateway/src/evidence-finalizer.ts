import {
  LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT,
  LEGACY_IDENTITY_EVIDENCE_V1_VERSION,
  type LegacyIdentityEvidenceV1
} from './evidence-codec/legacy-identity-evidence-v1.js'
import type { GatewayConfig } from './config.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const SHA256_RE = /^[0-9a-f]{64}$/
const SHARD_RE = /^[0-9a-f]{2}$/
const FINALIZER_RPC = 'finalize_game_character_legacy_identity_evidence' as const
const MAX_RPC_JSON_BYTES = 64 * 1024
const MAX_RPC_TIMEOUT_MS = 5_000
const FINALIZER_ROW_KEYS = [
  'character_id', 'actor_user_id', 'mode', 'lifecycle', 'world_id',
  'canonical_legacy_name', 'legacy_shard', 'player_file_sha256',
  'evidence_version', 'storage_format', 'recorded_at'
] as const

export type EvidenceFinalizationMode = 'claim' | 'provision'

export interface FinalizeLegacyIdentityEvidenceRequest {
  actorUserId: string
  correlationId: string
  characterId: string
  mode: EvidenceFinalizationMode
  worldId: string
  evidence: LegacyIdentityEvidenceV1
}

/**
 * Narrow, injected boundary for the one shard-aware evidence RPC. The caller
 * owns authentication and HTTP details; this client cannot route table CRUD
 * or fall back to the retired eight-argument overload.
 */
export interface EvidenceFinalizerRpcTransport {
  call(name: typeof FINALIZER_RPC, parameters: Readonly<Record<string, unknown>>): Promise<unknown>
}

export class EvidenceFinalizationError extends Error {
  constructor() {
    super('legacy identity evidence finalization was refused')
    this.name = 'EvidenceFinalizationError'
  }
}

/**
 * Production-only narrow transport for the evidence finalizer. It exposes one
 * service-role RPC and deliberately has no table or generic RPC surface.
 */
export class SupabaseEvidenceFinalizerTransport implements EvidenceFinalizerRpcTransport {
  private readonly url: string
  private readonly serviceRoleKey: string
  private readonly timeoutMs: number

  constructor(config: GatewayConfig, private readonly fetchImpl: typeof fetch = fetch) {
    if (!config.supabaseInternalRestUrl || !config.supabaseServiceRoleKey) throw new EvidenceFinalizationError()
    this.url = config.supabaseInternalRestUrl
    this.serviceRoleKey = config.supabaseServiceRoleKey
    this.timeoutMs = Math.min(config.authTimeoutMs, MAX_RPC_TIMEOUT_MS)
  }

  async call(name: typeof FINALIZER_RPC, parameters: Readonly<Record<string, unknown>>): Promise<unknown> {
    if (name !== FINALIZER_RPC) throw new EvidenceFinalizationError()
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.timeoutMs)
    try {
      let response: Response
      try {
        response = await this.fetchImpl(new URL(`/rpc/${name}`, this.url), {
          method: 'POST', redirect: 'error', signal: controller.signal,
          headers: {
            authorization: `Bearer ${this.serviceRoleKey}`,
            apikey: this.serviceRoleKey,
            'content-type': 'application/json'
          },
          body: JSON.stringify(parameters)
        })
      } catch {
        throw new EvidenceFinalizationError()
      }
      if (!response.ok) throw new EvidenceFinalizationError()
      const contentType = response.headers.get('content-type')
      const contentLength = response.headers.get('content-length')
      if (!contentType || !/^application\/json(?:\s*;|$)/i.test(contentType) ||
          (contentLength !== null && (!/^\d+$/.test(contentLength) || Number(contentLength) > MAX_RPC_JSON_BYTES))) {
        throw new EvidenceFinalizationError()
      }
      const reader = response.body?.getReader()
      if (!reader) throw new EvidenceFinalizationError()
      const decoder = new TextDecoder()
      let text = ''
      let total = 0
      while (true) {
        const next = await reader.read()
        if (next.done) break
        total += next.value.byteLength
        if (total > MAX_RPC_JSON_BYTES) throw new EvidenceFinalizationError()
        text += decoder.decode(next.value, { stream: true })
      }
      text += decoder.decode()
      try {
        return JSON.parse(text)
      } catch {
        throw new EvidenceFinalizationError()
      }
    } finally {
      clearTimeout(timer)
    }
  }
}

interface FinalizerRow {
  character_id: unknown
  actor_user_id: unknown
  mode: unknown
  lifecycle: unknown
  world_id: unknown
  canonical_legacy_name: unknown
  legacy_shard: unknown
  player_file_sha256: unknown
  evidence_version: unknown
  storage_format: unknown
  recorded_at: unknown
}

function isStrictLowerUuid(value: unknown): value is string {
  return typeof value === 'string' && UUID_RE.test(value)
}

function hasLoneSurrogate(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index)
    if (unit >= 0xd800 && unit <= 0xdbff) {
      if (index + 1 >= value.length || value.charCodeAt(index + 1) < 0xdc00 || value.charCodeAt(index + 1) > 0xdfff) return true
      index += 1
    } else if (unit >= 0xdc00 && unit <= 0xdfff) return true
  }
  return false
}

function validWorldId(value: unknown): value is string {
  return typeof value === 'string' && value.length >= 1 && value.length <= 64 &&
    !/[\x00-\x1f\x7f]/.test(value) && !hasLoneSurrogate(value)
}

function validCanonicalName(value: unknown): value is string {
  return typeof value === 'string' && value.length >= 1 && value.length <= 12 &&
    new TextEncoder().encode(value).length <= 14 && value !== '.' && value !== '..' &&
    !/[\x00-\x1f\x7f/\\:]/.test(value) && !hasLoneSurrogate(value)
}

function validEvidence(value: LegacyIdentityEvidenceV1): boolean {
  return value.outcome === 'ok' &&
    (value.canonicalization === 'canonical' || value.canonicalization === 'normalized') &&
    validCanonicalName(value.canonicalName) &&
    typeof value.legacyShard === 'string' && SHARD_RE.test(value.legacyShard) &&
    typeof value.playerFileSha256 === 'string' && SHA256_RE.test(value.playerFileSha256) &&
    value.storageFormat === LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT
}

function oneExactRow(value: unknown): FinalizerRow {
  if (!Array.isArray(value) || value.length !== 1 || !value[0] || typeof value[0] !== 'object' || Array.isArray(value[0]) ||
    Object.getPrototypeOf(value[0]) !== Object.prototype) throw new EvidenceFinalizationError()
  const row = value[0] as Record<string, unknown>
  const keys = Reflect.ownKeys(row)
  if (keys.length !== FINALIZER_ROW_KEYS.length || keys.some((key) => typeof key !== 'string' || !FINALIZER_ROW_KEYS.includes(key as typeof FINALIZER_ROW_KEYS[number]))) {
    throw new EvidenceFinalizationError()
  }
  if (Object.values(Object.getOwnPropertyDescriptors(row)).some((descriptor) => !('value' in descriptor))) {
    throw new EvidenceFinalizationError()
  }
  return row as unknown as FinalizerRow
}

function validRecordedAt(value: unknown): boolean {
  return typeof value === 'string' && value.length > 0 && Number.isFinite(Date.parse(value))
}

/**
 * Finalizes a decoded metadata-only V1 report through only the shard-aware
 * overload from the evidence binding migration.
 */
export class GatewayEvidenceFinalizer {
  constructor(private readonly transport: EvidenceFinalizerRpcTransport) {}

  async finalize(request: FinalizeLegacyIdentityEvidenceRequest): Promise<void> {
    if (!isStrictLowerUuid(request.actorUserId) || !isStrictLowerUuid(request.correlationId) ||
      !isStrictLowerUuid(request.characterId) || (request.mode !== 'claim' && request.mode !== 'provision') ||
      !validWorldId(request.worldId) || !validEvidence(request.evidence)) throw new EvidenceFinalizationError()

    let response: unknown
    try {
      response = await this.transport.call(FINALIZER_RPC, {
        p_actor_user_id: request.actorUserId,
        p_correlation_id: request.correlationId,
        p_character_id: request.characterId,
        p_outcome: 'ok',
        p_canonical_legacy_name: request.evidence.canonicalName,
        p_player_file_sha256: request.evidence.playerFileSha256,
        p_evidence_version: LEGACY_IDENTITY_EVIDENCE_V1_VERSION,
        p_storage_format: LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT,
        p_legacy_shard: request.evidence.legacyShard
      })
    } catch {
      // A failed transport is intentionally indeterminate: this inert client
      // neither exposes the cause nor retries a potentially committed RPC.
      throw new EvidenceFinalizationError()
    }

    let row: FinalizerRow
    try {
      row = oneExactRow(response)
      if (row.character_id !== request.characterId || row.actor_user_id !== request.actorUserId ||
        row.mode !== request.mode || (row.lifecycle !== 'handoff_pending' && row.lifecycle !== 'active') || row.world_id !== request.worldId ||
        row.canonical_legacy_name !== request.evidence.canonicalName || row.legacy_shard !== request.evidence.legacyShard ||
        row.player_file_sha256 !== request.evidence.playerFileSha256 || row.evidence_version !== LEGACY_IDENTITY_EVIDENCE_V1_VERSION ||
        row.storage_format !== LEGACY_IDENTITY_EVIDENCE_V1_STORAGE_FORMAT || !validRecordedAt(row.recorded_at)) {
        throw new EvidenceFinalizationError()
      }
    } catch {
      throw new EvidenceFinalizationError()
    }
  }
}
