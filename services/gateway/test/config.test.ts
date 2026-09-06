import assert from 'node:assert/strict'
import test from 'node:test'
import { ConfigError, loadConfig } from '../src/config.js'

test('production requires exact origins and active authentication', () => {
  assert.throws(
    () => loadConfig({ NODE_ENV: 'production', SUPABASE_URL: 'https://project.supabase.co' }),
    ConfigError
  )
  assert.throws(
    () => loadConfig({ NODE_ENV: 'production', AUTH_DISABLED: 'true', ALLOWED_ORIGINS: 'https://mud.example.com' }),
    /AUTH_DISABLED/
  )
})

test('test-only disabled authentication produces a bounded gateway config', () => {
  const config = loadConfig({
    NODE_ENV: 'test',
    AUTH_DISABLED: 'true',
    ALLOWED_ORIGINS: 'http://localhost:3000,https://preview.example.com',
    MAX_CONNECTIONS: '2',
    MUD_PORT: '4100'
  })
  assert.equal(config.authDisabled, true)
  assert.equal(config.mudPort, 4100)
  assert.equal(config.maxConnections, 2)
  assert.deepEqual([...config.allowedOrigins], ['http://localhost:3000', 'https://preview.example.com'])
  assert.equal(config.mudOnboardingEnabled, false)
  assert.equal(config.onboardingSourceAttemptLimit, 0)
  assert.equal(config.onboardingSourceAttemptWindowMs, 60_000)
  assert.equal(config.onboardingSourceAttemptMaxKeys, 10_000)
})

test('onboarding source attempt limiting is explicit, bounded, and can stay disabled', () => {
  const enabled = loadConfig({
    NODE_ENV: 'test', AUTH_DISABLED: 'true',
    MUD_ONBOARDING_SOURCE_ATTEMPT_LIMIT: '3',
    MUD_ONBOARDING_SOURCE_ATTEMPT_WINDOW_MS: '500',
    MUD_ONBOARDING_SOURCE_ATTEMPT_MAX_KEYS: '20'
  })
  assert.equal(enabled.onboardingSourceAttemptLimit, 3)
  assert.equal(enabled.onboardingSourceAttemptWindowMs, 500)
  assert.equal(enabled.onboardingSourceAttemptMaxKeys, 20)
  assert.throws(() => loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_SOURCE_ATTEMPT_LIMIT: '-1' }), /MUD_ONBOARDING_SOURCE_ATTEMPT_LIMIT/)
  assert.throws(() => loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_SOURCE_ATTEMPT_MAX_KEYS: '0' }), /MUD_ONBOARDING_SOURCE_ATTEMPT_MAX_KEYS/)
})

test('onboarding is an opt-in strict true or false flag', () => {
  assert.equal(loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true' }).mudOnboardingEnabled, true)
  assert.throws(() => loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: '1' }), /MUD_ONBOARDING_ENABLED must be true or false/)
})

test('evidence onboarding mirrors C: only exact 1 enables the control lane', () => {
  assert.equal(loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ENABLE_ONBOARDING_EVIDENCE: '1' }).mudOnboardingEvidenceEnabled, true)
  assert.equal(loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ENABLE_ONBOARDING_EVIDENCE: 'true' }).mudOnboardingEvidenceEnabled, false)
})

test('origins cannot contain paths or wildcard-like values', () => {
  assert.throws(
    () => loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', ALLOWED_ORIGINS: 'https://mud.example.com/ws' }),
    /exact origins/
  )
})

test('an internal Auth URL does not change the public JWT issuer', () => {
  const config = loadConfig({
    NODE_ENV: 'production',
    SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_AUTH_URL: 'http://muhan-auth:9999',
    SUPABASE_INTERNAL_REST_URL: 'http://muhan-kong:8000',
    SUPABASE_SERVICE_ROLE_KEY: 'service-role-key-fixture',
    MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef',
    GATEWAY_INSTANCE_ID: 'gateway-contract',
    ALLOWED_ORIGINS: 'https://mud.example.com'
  })

  assert.equal(config.supabaseAuthUrl, 'http://muhan-auth:9999')
  assert.equal(config.jwtIssuer, 'https://mud.example.com/auth/v1')
})

test('authenticated Gateway configuration requires internal service credentials and C-compatible secret', () => {
  assert.throws(() => loadConfig({
    NODE_ENV: 'production',
    SUPABASE_URL: 'https://mud.example.com',
    ALLOWED_ORIGINS: 'https://mud.example.com'
  }), /authenticated Gateway requires/)
  assert.throws(() => loadConfig({
    NODE_ENV: 'production',
    SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_INTERNAL_REST_URL: 'http://muhan-kong:8000',
    SUPABASE_SERVICE_ROLE_KEY: 'service-role-key-fixture',
    MUD_ADMISSION_SECRET: 'not-long-enough',
    GATEWAY_INSTANCE_ID: 'gateway-contract',
    ALLOWED_ORIGINS: 'https://mud.example.com'
  }), /MUD_ADMISSION_SECRET/)
  assert.throws(() => loadConfig({
    NODE_ENV: 'production',
    SUPABASE_URL: 'https://mud.example.com',
    SUPABASE_INTERNAL_REST_URL: 'http://muhan-postgrest:3000',
    SUPABASE_SERVICE_ROLE_KEY: 'service-role-key-fixture',
    MUD_ADMISSION_SECRET: 'x'.repeat(513),
    GATEWAY_INSTANCE_ID: 'gateway-contract',
    ALLOWED_ORIGINS: 'https://mud.example.com'
  }), /MUD_ADMISSION_SECRET/)
})
