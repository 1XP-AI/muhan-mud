import assert from 'node:assert/strict'
import test from 'node:test'
import { SupabaseCharacterAuthorizer, CharacterAuthorizationError } from '../src/character-authorizer.js'
import { loadConfig } from '../src/config.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const character = '123e4567-e89b-12d3-a456-426614174001'
const session = '123e4567-e89b-12d3-a456-426614174002'
const serviceKey = 'service-role-key-fixture'
const clock = () => new Date('2026-08-31T23:59:00.000Z').getTime()

function config(extra: NodeJS.ProcessEnv = {}) {
  return loadConfig({
    NODE_ENV: 'test',
    SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_INTERNAL_REST_URL: 'http://muhan-postgrest:3000',
    SUPABASE_SERVICE_ROLE_KEY: serviceKey,
    MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef',
    GATEWAY_INSTANCE_ID: 'gateway-contract',
    ALLOWED_ORIGINS: 'http://localhost:3000',
    ...extra
  })
}

function beginRequest(overrides: Partial<Record<string, unknown>> = {}) {
  return { actorUserId: actor, characterId: character, sessionId: session, gatewayInstanceId: 'gateway-contract', expiresAt: new Date('2026-09-01T00:00:00.000Z'), ...overrides } as Parameters<SupabaseCharacterAuthorizer['beginSession']>[0]
}

function leaseRow(extra: Record<string, unknown> = {}) {
  return [{ character_id: character, session_id: session, owner_user_id: actor, lifecycle: 'active', legacy_name_key: 'Contracthero', expires_at: '2026-09-01T00:00:00.000Z', ...extra }]
}

test('authorizer calls only the lease RPC and validates its owner-active canonical response', async () => {
  const requests: Array<{ url: URL, init?: RequestInit }> = []
  const authorizer = new SupabaseCharacterAuthorizer(config(), async (url, init) => {
    requests.push({ url: new URL(url), init })
    return Response.json([{
      character_id: character,
      session_id: session,
      owner_user_id: actor,
      lifecycle: 'active',
      legacy_name_key: 'Contracthero',
      expires_at: '2026-09-01T00:00:00.000Z'
    }])
  }, clock)

  const authorized = await authorizer.beginSession({
    actorUserId: actor,
    characterId: character,
    sessionId: session,
    gatewayInstanceId: 'gateway-contract',
    expiresAt: new Date('2026-09-01T00:00:00.000Z')
  })
  assert.deepEqual(authorized, { legacyNameKey: 'Contracthero' })
  assert.equal(requests.length, 1)
  assert.equal(requests[0]!.url.pathname, '/rpc/begin_game_character_session')
  assert.equal(requests[0]!.init?.headers && (requests[0]!.init.headers as Record<string, string>).authorization, `Bearer ${serviceKey}`)
  assert.equal(requests[0]!.init?.headers && (requests[0]!.init.headers as Record<string, string>).apikey, serviceKey)
  assert.equal(requests[0]!.init?.redirect, 'error')
  assert.deepEqual(JSON.parse(String(requests[0]!.init?.body)), {
    p_actor_user_id: actor,
    p_character_id: character,
    p_session_id: session,
    p_gateway_instance_id: 'gateway-contract',
    p_expires_at: '2026-09-01T00:00:00.000Z'
  })
})

test('lease RPC aborts a hanging service-role request and normalizes the failure', async () => {
  let aborted = false
  const authorizer = new SupabaseCharacterAuthorizer(config({ AUTH_TIMEOUT_MS: '100' }), async (_url, init) => new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => { aborted = true; reject(new DOMException('aborted', 'AbortError')) }, { once: true })
  }), () => new Date('2026-08-31T23:59:00.000Z').getTime())
  await assert.rejects(() => authorizer.beginSession(beginRequest()), CharacterAuthorizationError)
  assert.equal(aborted, true)
})

test('lease RPC rejects oversized, non-JSON, and unknown-column responses', async () => {
  const now = () => new Date('2026-08-31T23:59:00.000Z').getTime()
  const oversized = new SupabaseCharacterAuthorizer(config(), async () => new Response(new Uint8Array(65_537), { headers: { 'content-type': 'application/json' } }), now)
  await assert.rejects(() => oversized.beginSession(beginRequest()), CharacterAuthorizationError)
  const nonJson = new SupabaseCharacterAuthorizer(config(), async () => new Response(JSON.stringify(leaseRow()), { headers: { 'content-type': 'text/plain' } }), now)
  await assert.rejects(() => nonJson.beginSession(beginRequest()), CharacterAuthorizationError)
  const unknown = new SupabaseCharacterAuthorizer(config(), async () => Response.json(leaseRow({ unexpected: 'column' })), now)
  await assert.rejects(() => unknown.beginSession(beginRequest()), CharacterAuthorizationError)
})

test('lease RPC validates gateway instance and expiry before fetch', async () => {
  let calls = 0
  const authorizer = new SupabaseCharacterAuthorizer(config(), async () => { calls++; return Response.json(leaseRow()) })
  await assert.rejects(() => authorizer.beginSession(beginRequest({ gatewayInstanceId: '\u0000bad' })), CharacterAuthorizationError)
  await assert.rejects(() => authorizer.beginSession(beginRequest({ expiresAt: new Date('invalid') })), CharacterAuthorizationError)
  await assert.rejects(() => authorizer.renewSession({ sessionId: session, gatewayInstanceId: '   ', expiresAt: new Date('2026-09-01T00:00:00.000Z') }), CharacterAuthorizationError)
  await assert.rejects(() => authorizer.endSession(session, '\u0000bad'), CharacterAuthorizationError)
  assert.equal(calls, 0)
})

test('authorizer fails closed without exposing a PostgREST response', async () => {
  const authorizer = new SupabaseCharacterAuthorizer(config(), async () => Response.json([{
    character_id: character,
    session_id: session,
    owner_user_id: actor,
    lifecycle: 'suspended',
    legacy_name_key: 'Contracthero'
  }]), clock)
  await assert.rejects(() => authorizer.beginSession({
    actorUserId: actor,
    characterId: character,
    sessionId: session,
    gatewayInstanceId: 'gateway-contract',
    expiresAt: new Date('2026-09-01T00:00:00.000Z')
  }), CharacterAuthorizationError)
})

test('handoff_pending is never accepted as normal character admission', async () => {
  const authorizer = new SupabaseCharacterAuthorizer(config(), async () => Response.json(leaseRow({ lifecycle: 'handoff_pending' })), clock)

  await assert.rejects(() => authorizer.beginSession(beginRequest()), CharacterAuthorizationError)
  await assert.rejects(() => authorizer.renewSession({
    sessionId: session,
    gatewayInstanceId: 'gateway-contract',
    expiresAt: new Date('2026-09-01T00:00:00.000Z')
  }), CharacterAuthorizationError)
})

test('authorizer rejects a lease expiry that differs from the requested transaction result', async () => {
  const authorizer = new SupabaseCharacterAuthorizer(config(), async () => Response.json([{
    character_id: character,
    session_id: session,
    owner_user_id: actor,
    lifecycle: 'active',
    legacy_name_key: 'Contracthero',
    expires_at: '2026-09-01T00:00:05.000Z'
  }]), clock)
  await assert.rejects(() => authorizer.beginSession({
    actorUserId: actor,
    characterId: character,
    sessionId: session,
    gatewayInstanceId: 'gateway-contract',
    expiresAt: new Date('2026-09-01T00:00:00.000Z')
  }), CharacterAuthorizationError)
})

test('renewal calls the service-only RPC and rejects a mismatched response', async () => {
  const requests: Array<{ url: URL, init?: RequestInit }> = []
  const authorizer = new SupabaseCharacterAuthorizer(config(), async (url, init) => {
    requests.push({ url: new URL(url), init })
    return Response.json([{
      character_id: character,
      session_id: session,
      owner_user_id: actor,
      lifecycle: 'active',
      legacy_name_key: 'Contracthero',
      expires_at: '2026-09-01T00:02:00.000Z'
    }])
  }, clock)
  const renewed = await authorizer.renewSession({
    sessionId: session,
    gatewayInstanceId: 'gateway-contract',
    expiresAt: new Date('2026-09-01T00:02:00.000Z')
  })
  assert.equal(renewed.sessionId, session)
  assert.equal(renewed.expiresAtMs, Date.parse('2026-09-01T00:02:00.000Z'))
  assert.equal(requests[0]!.url.pathname, '/rpc/renew_game_character_session')
  assert.deepEqual(JSON.parse(String(requests[0]!.init?.body)), {
    p_session_id: session,
    p_gateway_instance_id: 'gateway-contract',
    p_expires_at: '2026-09-01T00:02:00.000Z'
  })

  const mismatch = new SupabaseCharacterAuthorizer(config(), async () => Response.json([{
    character_id: character,
    session_id: '123e4567-e89b-12d3-a456-426614174099',
    owner_user_id: actor,
    lifecycle: 'active',
    legacy_name_key: 'Contracthero',
    expires_at: '2026-09-01T00:02:00.000Z'
  }]), clock)
  await assert.rejects(() => mismatch.renewSession({
    sessionId: session,
    gatewayInstanceId: 'gateway-contract',
    expiresAt: new Date('2026-09-01T00:02:00.000Z')
  }), CharacterAuthorizationError)
})

test('release scopes the exact session to the owning gateway instance', async () => {
  const requests: Array<{ url: URL, init?: RequestInit }> = []
  const authorizer = new SupabaseCharacterAuthorizer(config(), async (url, init) => {
    requests.push({ url: new URL(url), init })
    return Response.json(true)
  })

  await authorizer.endSession(session, 'gateway-contract')
  assert.equal(requests[0]!.url.pathname, '/rpc/end_game_character_session')
  assert.deepEqual(JSON.parse(String(requests[0]!.init?.body)), {
    p_session_id: session,
    p_gateway_instance_id: 'gateway-contract'
  })

  const malformed = new SupabaseCharacterAuthorizer(config(), async () => Response.json([true]))
  await assert.rejects(() => malformed.endSession(session, 'gateway-contract'), CharacterAuthorizationError)

  const alreadyReleased = new SupabaseCharacterAuthorizer(config(), async () => Response.json(false))
  await alreadyReleased.endSession(session, 'gateway-contract')
})
