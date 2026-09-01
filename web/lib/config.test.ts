import assert from 'node:assert/strict'
import test from 'node:test'

import { readPublicConfig } from './config.ts'

const base = {
  SUPABASE_PUBLIC_URL: 'https://supabase.example',
  SUPABASE_PUBLISHABLE_KEY: 'public-key',
  MUD_GATEWAY_URL: 'wss://gateway.example/ws',
}

test('onboarding is disabled by default and parses strict true', () => {
  assert.equal(readPublicConfig(base).config?.onboardingEnabled, false)
  assert.equal(readPublicConfig({ ...base, MUD_ONBOARDING_ENABLED: 'true' }).config?.onboardingEnabled, true)
})

test('onboarding rejects values other than literal true or false', () => {
  const result = readPublicConfig({ ...base, MUD_ONBOARDING_ENABLED: '1' })
  assert.equal(result.config, null)
  assert.match(result.error ?? '', /true|false/)
})
