import assert from 'node:assert/strict'
import test from 'node:test'

import { decideClaimEntry } from '../../../web/lib/claim-transparency.ts'
import { readPublicConfig } from '../../../web/lib/config.ts'
import { shouldOpenGatewaySocket } from '../../../web/lib/gateway-contract.ts'
import {
  completeOnboardingHandoff,
  resolvePlayAdmission,
} from '../../../web/lib/play-admission.ts'
import { loadConfig } from '../src/config.js'

const webCoordinates = {
  SUPABASE_PUBLIC_URL: 'https://supabase.example',
  SUPABASE_PUBLISHABLE_KEY: 'public-key',
  MUD_GATEWAY_URL: 'wss://gateway.example/ws',
}

const authenticatedGatewayCoordinates = {
  NODE_ENV: 'test',
  SUPABASE_URL: 'https://supabase.example',
  SUPABASE_INTERNAL_REST_URL: 'http://postgrest.internal:3000',
  SUPABASE_SERVICE_ROLE_KEY: 'service-role-test-fixture',
  MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef',
  GATEWAY_INSTANCE_ID: 'onboarding-configuration-contract',
  ALLOWED_ORIGINS: 'https://web.example',
}

/**
 * This is a no-deploy contract: C verifies its exact 0/1 + trusted-admission
 * mode in onboarding_admission_test, while this test uses the public web and
 * Gateway parsers to reject a browser/Gateway partial rollout before it can
 * be treated as a supported configuration.
 */
function requireCoordinatedOnboarding(
  webEnabled: boolean,
  gatewayEnabled: boolean,
): void {
  if (!webEnabled || !gatewayEnabled) {
    throw new Error('onboarding needs both web and Gateway enabled before C MUD1O is selected')
  }
}

function webOnboardingEnabled(value?: 'true' | 'false'): boolean {
  return readPublicConfig({
    ...webCoordinates,
    ...(value === undefined ? {} : { MUD_ONBOARDING_ENABLED: value }),
  }).config?.onboardingEnabled === true
}

function gatewayOnboardingEnabled(value?: 'true' | 'false'): boolean {
  return loadConfig({
    ...authenticatedGatewayCoordinates,
    ...(value === undefined ? {} : { MUD_ONBOARDING_ENABLED: value }),
  }).mudOnboardingEnabled
}

test('onboarding defaults stay all-off across browser and authenticated Gateway configuration', () => {
  assert.equal(webOnboardingEnabled(), false)
  assert.equal(gatewayOnboardingEnabled(), false)
  assert.throws(
    () => requireCoordinatedOnboarding(webOnboardingEnabled(), gatewayOnboardingEnabled()),
    /both web and Gateway enabled/,
  )
})

test('only the coordinated enabled test configuration exposes create or claim then normal admission', () => {
  const webEnabled = webOnboardingEnabled('true')
  const gatewayEnabled = gatewayOnboardingEnabled('true')
  requireCoordinatedOnboarding(webEnabled, gatewayEnabled)

  assert.equal(decideClaimEntry('empty', webEnabled).kind, 'claim-start')

  const ownerUserId = '11111111-1111-4111-8111-111111111111'
  const characterId = '22222222-2222-4222-8222-222222222222'
  for (const completion of ['provisioned', 'claimed'] as const) {
    const handoff = completeOnboardingHandoff(ownerUserId, characterId, completion)
    assert.deepEqual(
      resolvePlayAdmission(ownerUserId, 'ready', [{ id: characterId, lifecycle: 'active' }], handoff),
      handoff,
    )
    assert.equal(shouldOpenGatewaySocket('ready', characterId, [characterId]), true)
  }
})

test('a partial browser/Gateway enablement is not a supported onboarding configuration', () => {
  for (const [webValue, gatewayValue] of [
    ['true', 'false'],
    ['false', 'true'],
  ] as const) {
    const webEnabled = webOnboardingEnabled(webValue)
    const gatewayEnabled = gatewayOnboardingEnabled(gatewayValue)
    assert.throws(
      () => requireCoordinatedOnboarding(webEnabled, gatewayEnabled),
      /both web and Gateway enabled/,
    )
  }
})
