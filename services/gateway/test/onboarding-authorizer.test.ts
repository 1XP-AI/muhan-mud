import assert from 'node:assert/strict'
import test from 'node:test'
import { OnboardingAuthorizationError, SupabaseOnboardingAuthorizer } from '../src/onboarding-authorizer.js'
import { loadConfig } from '../src/config.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const correlation = '123e4567-e89b-12d3-a456-426614174001'
const character = '123e4567-e89b-12d3-a456-426614174002'
const serviceKey = 'service-role-key-fixture'

function config(extra: NodeJS.ProcessEnv = {}) {
  return loadConfig({ NODE_ENV: 'test', SUPABASE_URL: 'https://mud.example.com', SUPABASE_INTERNAL_REST_URL: 'http://postgrest:3000', SUPABASE_SERVICE_ROLE_KEY: serviceKey, MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef', GATEWAY_INSTANCE_ID: 'gateway-contract', ...extra })
}

function beginRequest() {
  return { actorUserId: actor, correlationId: correlation, mode: 'provision' as const, expiresAt: new Date('2026-09-02T00:00:00.000Z') }
}

function beginRow(extra: Record<string, unknown> = {}) {
  return [{ correlation_id: correlation, actor_user_id: actor, mode: 'provision', status: 'started', expires_at: '2026-09-02T00:00:00.000Z', ...extra }]
}

test('onboarding client exposes only exact service RPCs and validates idempotent intent/provision responses', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const client = new SupabaseOnboardingAuthorizer(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    const path = new URL(url).pathname
    if (path.endsWith('begin_game_character_onboarding')) return Response.json([{ correlation_id: correlation, actor_user_id: actor, mode: 'provision', status: 'started', expires_at: '2026-09-02T00:00:00.000Z' }])
    return Response.json([{ character_id: character, correlation_id: correlation, actor_user_id: actor, world_id: 'muhan', legacy_name_key: 'hero', lifecycle: 'provisioning', status: 'reserved', expires_at: '2026-09-02T00:00:00.000Z' }])
  }, () => new Date('2026-09-01T23:50:00.000Z').getTime())
  await client.begin({ actorUserId: actor, correlationId: correlation, mode: 'provision', expiresAt: new Date('2026-09-02T00:00:00.000Z') })
  const reservation = await client.reserve({ actorUserId: actor, correlationId: correlation, worldId: 'muhan', legacyName: 'hero' })
  assert.deepEqual(reservation, { characterId: character, legacyNameKey: 'hero' })
  assert.deepEqual(calls.map(({ url }) => url.pathname), ['/rpc/begin_game_character_onboarding', '/rpc/begin_game_character_provisioning'])
  assert.deepEqual(JSON.parse(String(calls[1]!.init?.body)), { p_actor_user_id: actor, p_correlation_id: correlation, p_world_id: 'muhan', p_legacy_name: 'hero' })
  assert.equal((calls[0]!.init?.headers as Record<string, string>).authorization, `Bearer ${serviceKey}`)
  assert.equal(calls[0]!.init?.redirect, 'error')
})

test('onboarding RPC aborts a hanging service-role request and normalizes the failure', async () => {
  let aborted = false
  const client = new SupabaseOnboardingAuthorizer(config({ AUTH_TIMEOUT_MS: '100' }), async (_url, init) => new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => { aborted = true; reject(new DOMException('aborted', 'AbortError')) }, { once: true })
  }), () => new Date('2026-09-01T23:50:00.000Z').getTime())
  await assert.rejects(() => client.begin(beginRequest()), OnboardingAuthorizationError)
  assert.equal(aborted, true)
})

test('handoff activation invokes only the exact service RPC and validates its active row', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const client = new SupabaseOnboardingAuthorizer(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    return Response.json([{
      character_id: character,
      actor_user_id: actor,
      correlation_id: correlation,
      lifecycle: 'active',
      onboarding_status: 'finalized',
    }])
  })

  await client.activateHandoff({ actorUserId: actor, correlationId: correlation, characterId: character, mode: 'provision' })
  assert.equal(calls[0]!.url.pathname, '/rpc/activate_game_character_onboarding_handoff')
  assert.deepEqual(JSON.parse(String(calls[0]!.init?.body)), {
    p_actor_user_id: actor,
    p_correlation_id: correlation,
    p_character_id: character,
    p_mode: 'provision',
  })

  const unexpectedColumn = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{
    character_id: character,
    actor_user_id: actor,
    correlation_id: correlation,
    lifecycle: 'active',
    onboarding_status: 'finalized',
    mode: 'provision',
  }]))
  await assert.rejects(() => unexpectedColumn.activateHandoff({ actorUserId: actor, correlationId: correlation, characterId: character, mode: 'provision' }), OnboardingAuthorizationError)
})

test('handoff activation rejects pending or non-finalized evidence before normal admission can be eligible', async () => {
  const activation = { actorUserId: actor, correlationId: correlation, characterId: character, mode: 'provision' as const }
  const exactRow = {
    character_id: character,
    actor_user_id: actor,
    correlation_id: correlation,
    lifecycle: 'active',
    onboarding_status: 'finalized',
  }

  for (const response of [
    { ...exactRow, lifecycle: 'handoff_pending' },
    { ...exactRow, onboarding_status: 'provisioning' },
  ]) {
    const client = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([response]))
    await assert.rejects(() => client.activateHandoff(activation), OnboardingAuthorizationError)
  }
})

test('snapshot command binding uses the migration RPC contract and accepts only BOUND outcomes', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const request = {
    actorUserId: actor,
    correlationId: correlation,
    characterId: character,
    mode: 'provision' as const,
    commandId: '123e4567-e89b-12d3-a456-426614174003',
  }
  const client = new SupabaseOnboardingAuthorizer(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    return Response.json([{ outcome: 'BOUND' }])
  })

  await client.bindSnapshotCommand(request)
  assert.equal(calls[0]!.url.pathname, '/rpc/register_game_character_onboarding_snapshot_command_binding')
  assert.deepEqual(JSON.parse(String(calls[0]!.init?.body)), {
    p_actor_user_id: actor,
    p_correlation_id: correlation,
    p_character_id: character,
    p_mode: 'provision',
    p_command_id: request.commandId,
  })

  const exactRetry = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ outcome: 'EXACT_RETRY' }]))
  await exactRetry.bindSnapshotCommand(request)

  const unknownOutcome = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ outcome: 'ALREADY_BOUND' }]))
  await assert.rejects(() => unknownOutcome.bindSnapshotCommand(request), OnboardingAuthorizationError)

  const unknownColumn = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ outcome: 'BOUND', extra: true }]))
  await assert.rejects(() => unknownColumn.bindSnapshotCommand(request), OnboardingAuthorizationError)
})

test('onboarding RPC rejects oversized, non-JSON, and unknown-column responses', async () => {
  const now = () => new Date('2026-09-01T23:50:00.000Z').getTime()
  const oversized = new SupabaseOnboardingAuthorizer(config(), async () => new Response(new Uint8Array(65_537), { headers: { 'content-type': 'application/json' } }), now)
  await assert.rejects(() => oversized.begin(beginRequest()), OnboardingAuthorizationError)
  const nonJson = new SupabaseOnboardingAuthorizer(config(), async () => new Response(JSON.stringify(beginRow()), { headers: { 'content-type': 'text/plain' } }), now)
  await assert.rejects(() => nonJson.begin(beginRequest()), OnboardingAuthorizationError)
  const unknownColumn = new SupabaseOnboardingAuthorizer(config(), async () => Response.json(beginRow({ unexpected: 'column' })), now)
  await assert.rejects(() => unknownColumn.begin(beginRequest()), OnboardingAuthorizationError)
})

test('onboarding client maps only player-v1 to storage format one and requires a pending handoff result', async () => {
  const client = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ character_id: character, actor_user_id: actor, lifecycle: 'handoff_pending', status: 'finalized', saved_file_sha256: 'a'.repeat(64), storage_format: 2 }]))
  await assert.rejects(() => client.finalize({ actorUserId: actor, correlationId: correlation, characterId: character, fileSha256: 'a'.repeat(64), storageFormat: 'player-v1' }), OnboardingAuthorizationError)
  await assert.rejects(() => client.reserve({ actorUserId: actor, correlationId: correlation, worldId: 'muhan', legacyName: 'hero' }), OnboardingAuthorizationError)
})

test('same-correlation reconnect accepts the original still-live intent expiry', async () => {
  const now = new Date('2026-09-02T00:00:00.000Z').getTime()
  const client = new SupabaseOnboardingAuthorizer(
    config(),
    async () => Response.json([{
      correlation_id: correlation,
      actor_user_id: actor,
      mode: 'claim',
      status: 'started',
      expires_at: '2026-09-02T00:05:00.000Z',
    }]),
    () => now,
  )
  await client.begin({
    actorUserId: actor,
    correlationId: correlation,
    mode: 'claim',
    expiresAt: new Date('2026-09-02T00:10:00.000Z'),
  })
})

test('unreserved cancellation uses the exact actor and correlation service RPC and validates completion', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const client = new SupabaseOnboardingAuthorizer(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    return Response.json([{
      correlation_id: correlation,
      actor_user_id: actor,
      status: 'cancelled',
      completed_at: '2026-09-02T00:00:00.000Z',
    }])
  })

  await client.cancelUnreserved({ actorUserId: actor, correlationId: correlation })
  assert.equal(calls[0]!.url.pathname, '/rpc/cancel_unreserved_game_character_onboarding')
  assert.deepEqual(JSON.parse(String(calls[0]!.init?.body)), {
    p_actor_user_id: actor,
    p_correlation_id: correlation,
  })
})

test('unreserved cancellation fails closed on a mismatched or incomplete RPC response', async () => {
  const client = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{
    correlation_id: correlation,
    actor_user_id: '123e4567-e89b-12d3-a456-426614174099',
    status: 'cancelled',
    completed_at: null,
  }]))
  await assert.rejects(() => client.cancelUnreserved({ actorUserId: actor, correlationId: correlation }), OnboardingAuthorizationError)
})

test('legacy claim binds the exact C-verified player fingerprint to the service RPC', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const fingerprint = 'f'.repeat(64)
  const client = new SupabaseOnboardingAuthorizer(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    return Response.json([{
      character_id: character,
      lifecycle: 'handoff_pending',
      owner_user_id: actor,
      claimed_at: '2026-09-02T00:00:00.000Z',
      onboarding_status: 'finalized',
      imported_file_sha256: fingerprint,
    }])
  })

  await client.claim({
    actorUserId: actor,
    correlationId: correlation,
    worldId: 'muhan',
    legacyNameKey: 'Legacyhero',
    fileSha256: fingerprint,
  })

  assert.equal(calls[0]!.url.pathname, '/rpc/claim_legacy_game_character_onboarding')
  assert.deepEqual(JSON.parse(String(calls[0]!.init?.body)), {
    p_world_id: 'muhan',
    p_legacy_name_key: 'Legacyhero',
    p_imported_file_sha256: fingerprint,
    p_actor_user_id: actor,
    p_correlation_id: correlation,
  })

  const mismatched = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{
    character_id: character,
    lifecycle: 'handoff_pending',
    owner_user_id: actor,
    claimed_at: '2026-09-02T00:00:00.000Z',
    onboarding_status: 'finalized',
    imported_file_sha256: 'e'.repeat(64),
  }]))
  await assert.rejects(() => mismatched.claim({
    actorUserId: actor,
    correlationId: correlation,
    worldId: 'muhan',
    legacyNameKey: 'Legacyhero',
    fileSha256: fingerprint,
  }), OnboardingAuthorizationError)
})

test('legacy claim challenge binds the exact C-verified fingerprint and returns the bounded allow window', async () => {
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const fingerprint = 'f'.repeat(64)
  const client = new SupabaseOnboardingAuthorizer(config(), async (url, init) => {
    calls.push({ url: new URL(url), init })
    return Response.json([{
      character_id: character,
      legacy_name_key: 'Legacyhero',
      imported_file_sha256: fingerprint,
      challenge_status: 'allowed',
      allow_expires_at: '2026-09-02T00:01:30.000Z',
    }])
  }, () => new Date('2026-09-02T00:00:00.000Z').getTime())

  const result = await client.challenge({
    actorUserId: actor,
    correlationId: correlation,
    worldId: 'muhan',
    legacyNameKey: 'Legacyhero',
    fileSha256: fingerprint,
  })

  assert.deepEqual(result, {
    characterId: character,
    legacyNameKey: 'Legacyhero',
    fileSha256: fingerprint,
    allowExpiresAtMs: new Date('2026-09-02T00:01:30.000Z').getTime(),
  })
  assert.equal(calls[0]!.url.pathname, '/rpc/challenge_legacy_game_character_onboarding')
  assert.deepEqual(JSON.parse(String(calls[0]!.init?.body)), {
    p_world_id: 'muhan',
    p_legacy_name_key: 'Legacyhero',
    p_imported_file_sha256: fingerprint,
    p_actor_user_id: actor,
    p_correlation_id: correlation,
  })
})

test('legacy claim challenge fails closed on stale, overlong, or mismatched allow responses', async () => {
  const fingerprint = 'f'.repeat(64)
  const request = { actorUserId: actor, correlationId: correlation, worldId: 'muhan', legacyNameKey: 'Legacyhero', fileSha256: fingerprint }
  const row = { character_id: character, legacy_name_key: 'Legacyhero', imported_file_sha256: fingerprint, challenge_status: 'allowed', allow_expires_at: '2026-09-02T00:01:30.000Z' }
  const stale = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ ...row, allow_expires_at: '2026-09-01T23:59:59.000Z' }]), () => new Date('2026-09-02T00:00:00.000Z').getTime())
  await assert.rejects(() => stale.challenge(request), OnboardingAuthorizationError)
  const overlong = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ ...row, allow_expires_at: '2026-09-02T00:10:00.000Z' }]), () => new Date('2026-09-02T00:00:00.000Z').getTime())
  await assert.rejects(() => overlong.challenge(request), OnboardingAuthorizationError)
  const mismatched = new SupabaseOnboardingAuthorizer(config(), async () => Response.json([{ ...row, imported_file_sha256: 'e'.repeat(64) }]), () => new Date('2026-09-02T00:00:00.000Z').getTime())
  await assert.rejects(() => mismatched.challenge(request), OnboardingAuthorizationError)
})

test('legacy claim challenge aborts a hanging service-role request within the RPC bound', async () => {
  let aborted = false
  const client = new SupabaseOnboardingAuthorizer(config({ AUTH_TIMEOUT_MS: '100' }), async (_url, init) => new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => { aborted = true; reject(new DOMException('aborted', 'AbortError')) }, { once: true })
  }))
  await assert.rejects(() => client.challenge({ actorUserId: actor, correlationId: correlation, worldId: 'muhan', legacyNameKey: 'Legacyhero', fileSha256: 'f'.repeat(64) }), OnboardingAuthorizationError)
  assert.equal(aborted, true)
})
