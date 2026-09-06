import { createHash } from 'node:crypto'
import type { GatewayConfig } from './config.js'

const SAFE_WORLD_RE = /^(?=.{1,64}$)[^\x00-\x1f\x7f]+$/u
const SAFE_NAME_RE = /^(?=.{1,12}$)[^\x00-\x1f\x7f/\\:]+$/u
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const MAX_NAME_BYTES = 14
const MAX_RPC_JSON_BYTES = 64 * 1024
const MAX_RPC_TIMEOUT_MS = 5_000
const RESULT_KEYS = ['character_id'] as const

export interface LegacyLocatorRequest {
  worldId: string
  canonicalName: string
}

export interface LegacyLocatorResolver {
  resolve(request: LegacyLocatorRequest): Promise<string>
}

export class LegacyLocatorResolutionError extends Error {
  constructor() {
    super('legacy locator resolution was refused')
    this.name = 'LegacyLocatorResolutionError'
  }
}

function hasUnpairedSurrogate(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index)
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1)
      if (next < 0xdc00 || next > 0xdfff) return true
      index++
    } else if (code >= 0xdc00 && code <= 0xdfff) return true
  }
  return false
}

/** Match C lowercize(name, 1): fold ASCII uppercase, then capitalize ASCII first byte. */
function canonicalNameKey(name: string): string {
  let folded = ''
  for (const character of name) {
    folded += character >= 'A' && character <= 'Z'
      ? String.fromCharCode(character.charCodeAt(0) + 32)
      : character
  }
  const first = folded[0]
  return first !== undefined && first >= 'a' && first <= 'z'
    ? String.fromCharCode(first.charCodeAt(0) - 32) + folded.slice(1)
    : folded
}

function validRequest(request: LegacyLocatorRequest): boolean {
  if (typeof request?.worldId !== 'string' || typeof request.canonicalName !== 'string') return false
  if (hasUnpairedSurrogate(request.worldId) || hasUnpairedSurrogate(request.canonicalName)) return false
  if (!SAFE_WORLD_RE.test(request.worldId) || !SAFE_NAME_RE.test(request.canonicalName)) return false
  if (Buffer.byteLength(request.canonicalName, 'utf8') > MAX_NAME_BYTES) return false
  return canonicalNameKey(request.canonicalName) === request.canonicalName
}

function oneRow(body: unknown): string {
  if (!Array.isArray(body) || body.length !== 1 || !body[0] || typeof body[0] !== 'object' || Array.isArray(body[0])) {
    throw new LegacyLocatorResolutionError()
  }
  const row = body[0] as Record<string, unknown>
  const keys = Object.keys(row)
  if (keys.length !== RESULT_KEYS.length || keys.some((key) => !RESULT_KEYS.includes(key as typeof RESULT_KEYS[number]))) {
    throw new LegacyLocatorResolutionError()
  }
  const characterId = row.character_id
  if (typeof characterId !== 'string' || !UUID_RE.test(characterId)) throw new LegacyLocatorResolutionError()
  return characterId
}

async function boundedJson(response: Response): Promise<unknown> {
  const contentType = response.headers.get('content-type')
  const contentLength = response.headers.get('content-length')
  if (!contentType || !/^application\/json(?:\s*;|$)/i.test(contentType) ||
      (contentLength !== null && (!/^\d+$/.test(contentLength) || Number(contentLength) > MAX_RPC_JSON_BYTES))) {
    throw new LegacyLocatorResolutionError()
  }
  const reader = response.body?.getReader()
  if (!reader) throw new LegacyLocatorResolutionError()
  const chunks: Uint8Array[] = []
  let total = 0
  while (true) {
    const next = await reader.read()
    if (next.done) break
    total += next.value.byteLength
    if (total > MAX_RPC_JSON_BYTES) throw new LegacyLocatorResolutionError()
    chunks.push(next.value)
  }
  const bytes = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.byteLength
  }
  try {
    return JSON.parse(new TextDecoder().decode(bytes))
  } catch {
    throw new LegacyLocatorResolutionError()
  }
}

function requiredConfig(config: GatewayConfig): { url: string, key: string } {
  if (!config.supabaseInternalRestUrl || !config.supabaseServiceRoleKey) throw new LegacyLocatorResolutionError()
  return { url: config.supabaseInternalRestUrl, key: config.supabaseServiceRoleKey }
}

/** Service-role-only, read-only adapter for the imported legacy locator RPC. */
export class SupabaseLegacyLocatorResolver implements LegacyLocatorResolver {
  private readonly internalRestUrl: string
  private readonly serviceRoleKey: string
  private readonly rpcTimeoutMs: number

  constructor(config: GatewayConfig, private readonly fetchImpl: typeof fetch = fetch) {
    const required = requiredConfig(config)
    this.internalRestUrl = required.url
    this.serviceRoleKey = required.key
    this.rpcTimeoutMs = Math.min(config.authTimeoutMs, MAX_RPC_TIMEOUT_MS)
  }

  async resolve(request: LegacyLocatorRequest): Promise<string> {
    if (!validRequest(request)) throw new LegacyLocatorResolutionError()
    const digest = createHash('sha1').update(request.canonicalName, 'utf8').digest('hex')
    try {
      const body = await this.rpc({
        p_world_id: request.worldId,
        p_canonical_legacy_name: request.canonicalName,
        p_legacy_name_sha1: digest,
        p_legacy_shard: digest.slice(0, 2),
      })
      return oneRow(body)
    } catch (error) {
      if (error instanceof LegacyLocatorResolutionError) throw error
      throw new LegacyLocatorResolutionError()
    }
  }

  private async rpc(body: Record<string, string>): Promise<unknown> {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), this.rpcTimeoutMs)
    try {
      let response: Response
      try {
        response = await this.fetchImpl(new URL('/rpc/resolve_game_imported_legacy_locator', this.internalRestUrl), {
          method: 'POST',
          redirect: 'error',
          signal: controller.signal,
          headers: {
            authorization: `Bearer ${this.serviceRoleKey}`,
            apikey: this.serviceRoleKey,
            'content-type': 'application/json',
          },
          body: JSON.stringify(body),
        })
      } catch {
        throw new LegacyLocatorResolutionError()
      }
      if (!response.ok) throw new LegacyLocatorResolutionError()
      return await boundedJson(response)
    } finally {
      clearTimeout(timer)
    }
  }
}
