import assert from 'node:assert/strict'
import test from 'node:test'
import { SupabaseCharacterAuthorizer, CharacterAuthorizationError } from '../src/character-authorizer.js'
import { loadConfig } from '../src/config.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const character = '123e4567-e89b-12d3-a456-426614174001'
const session = '123e4567-e89b-12d3-a456-426614174002'
const serviceKey = 'service-role-key-fixture'

function config() {
  return loadConfig({
    NODE_ENV: 'test',
    SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_INTERNAL_REST_URL: 'http://muhan-postgrest:3000',
    SUPABASE_SERVICE_ROLE_KEY: serviceKey,
    MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef',
    GATEWAY_INSTANCE_ID: 'gateway-contract',
    ALLOWED_ORIGINS: 'http://localhost:3000'
  })
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
  })

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
  assert.deepEqual(JSON.parse(String(requests[0]!.init?.body)), {
    p_actor_user_id: actor,
    p_character_id: character,
    p_session_id: session,
    p_gateway_instance_id: 'gateway-contract',
    p_expires_at: '2026-09-01T00:00:00.000Z'
  })
})

test('authorizer fails closed without exposing a PostgREST response', async () => {
  const authorizer = new SupabaseCharacterAuthorizer(config(), async () => Response.json([{
    character_id: character,
    session_id: session,
    owner_user_id: actor,
    lifecycle: 'suspended',
    legacy_name_key: 'Contracthero'
  }]))
  await assert.rejects(() => authorizer.beginSession({
    actorUserId: actor,
    characterId: character,
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
  }]))
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
  })
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
  }]))
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
})
