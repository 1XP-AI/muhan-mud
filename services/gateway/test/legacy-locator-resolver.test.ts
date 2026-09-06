import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import { loadConfig } from '../src/config.js'
import { LegacyLocatorResolutionError, SupabaseLegacyLocatorResolver } from '../src/legacy-locator-resolver.js'

const character = '123e4567-e89b-12d3-a456-426614174000'
const serviceKey = 'service-role-key-fixture'
const worldId = 'resolver-world'
const canonicalName = '타bot'
const sha1 = createHash('sha1').update(canonicalName, 'utf8').digest('hex')

function config(extra: NodeJS.ProcessEnv = {}) {
  return loadConfig({
    NODE_ENV: 'test', SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_INTERNAL_REST_URL: 'http://postgrest:3000', SUPABASE_SERVICE_ROLE_KEY: serviceKey,
    MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef', GATEWAY_INSTANCE_ID: 'gateway-contract',
    ...extra,
  })
}

test('resolver sends the exact service RPC tuple derived from canonical UTF-8 name', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const resolver = new SupabaseLegacyLocatorResolver(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    return Response.json([{ character_id: character }])
  })

  assert.equal(await resolver.resolve({ worldId, canonicalName }), character)
  assert.equal(calls[0]!.url.pathname, '/rpc/resolve_game_imported_legacy_locator')
  assert.deepEqual(JSON.parse(String(calls[0]!.init?.body)), {
    p_world_id: worldId,
    p_canonical_legacy_name: canonicalName,
    p_legacy_name_sha1: sha1,
    p_legacy_shard: sha1.slice(0, 2),
  })
  const headers = calls[0]!.init?.headers as Record<string, string>
  assert.equal(headers.authorization, `Bearer ${serviceKey}`)
  assert.equal(headers.apikey, serviceKey)
  assert.equal(calls[0]!.init?.redirect, 'error')
})

test('resolver rejects noncanonical or unsafe identity before calling PostgREST', async () => {
  let calls = 0
  const resolver = new SupabaseLegacyLocatorResolver(config(), async () => {
    calls++
    return Response.json([{ character_id: character }])
  })
  for (const request of [
    { worldId: 'resolver-world', canonicalName: 'tAbot' },
    { worldId: 'resolver\u0000world', canonicalName },
    { worldId: 'resolver-world', canonicalName: 'a'.repeat(13) },
    { worldId: 'resolver-world', canonicalName: 'A\u0000bot' },
    { worldId: 'resolver-world', canonicalName: '   ' },
    { worldId: 'resolver-world', canonicalName: '.' },
    { worldId: 'resolver-world', canonicalName: '..' },
  ]) {
    await assert.rejects(() => resolver.resolve(request), LegacyLocatorResolutionError)
  }
  assert.equal(calls, 0)
})

test('resolver accepts only one strict UUID row and hides malformed server responses', async () => {
  const responses = [
    Response.json([{ character_id: '123e4567-e89b-12d3-a456-42661417400A' }]),
    Response.json([{ character_id: character }, { character_id: character }]),
    Response.json([{ character_id: character, secret: 'must not escape' }]),
    new Response('{"error":"service-role secret body"}', { status: 400, headers: { 'content-type': 'application/json' } }),
  ]
  for (const response of responses) {
    const resolver = new SupabaseLegacyLocatorResolver(config(), async () => response)
    await assert.rejects(
      () => resolver.resolve({ worldId, canonicalName }),
      (error: unknown) => error instanceof LegacyLocatorResolutionError && error.message === 'legacy locator resolution was refused',
    )
  }
})

test('resolver rejects oversized or non-JSON responses without reading unbounded data', async () => {
  let cancelled = false
  const oversized = new Response(new ReadableStream({
    start(controller) {
      controller.enqueue(new TextEncoder().encode('[' + ' '.repeat(65 * 1024)))
    },
    cancel() {
      cancelled = true
    },
  }), { headers: { 'content-type': 'application/json' } })
  const resolver = new SupabaseLegacyLocatorResolver(config(), async () => oversized)
  await assert.rejects(() => resolver.resolve({ worldId, canonicalName }), LegacyLocatorResolutionError)
  assert.equal(cancelled, true)

  const nonJson = new SupabaseLegacyLocatorResolver(config(), async () => new Response('not json', {
    headers: { 'content-type': 'text/plain' },
  }))
  await assert.rejects(() => nonJson.resolve({ worldId, canonicalName }), LegacyLocatorResolutionError)
})

test('resolver aborts a fetch that never resolves', async () => {
  let aborted = false
  const resolver = new SupabaseLegacyLocatorResolver(config({ AUTH_TIMEOUT_MS: '100' }), async (_url, init) => {
    const signal = init?.signal as AbortSignal
    return await new Promise<Response>((_resolve, reject) => {
      signal.addEventListener('abort', () => {
        aborted = true
        reject(new Error('aborted'))
      }, { once: true })
    })
  })
  await assert.rejects(() => resolver.resolve({ worldId, canonicalName }), LegacyLocatorResolutionError)
  assert.equal(aborted, true)
})

test('resolver deadline aborts a stalled response body read', async () => {
  let aborted = false
  const resolver = new SupabaseLegacyLocatorResolver(config({ AUTH_TIMEOUT_MS: '100' }), async (_url, init) => {
    const signal = init?.signal as AbortSignal
    signal.addEventListener('abort', () => { aborted = true }, { once: true })
    return new Response(new ReadableStream({
      pull() {
        return new Promise<void>(() => {})
      },
    }), { headers: { 'content-type': 'application/json' } })
  })
  await assert.rejects(() => resolver.resolve({ worldId, canonicalName }), LegacyLocatorResolutionError)
  assert.equal(aborted, true)
})
