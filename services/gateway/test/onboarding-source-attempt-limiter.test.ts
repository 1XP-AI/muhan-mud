import assert from 'node:assert/strict'
import test from 'node:test'
import { authoritativeOnboardingSourceAddress, OnboardingSourceAttemptLimiter } from '../src/onboarding-source-attempt-limiter.js'

test('limits each source only within its configured attempt window', () => {
  let now = 1_000
  const limiter = new OnboardingSourceAttemptLimiter({ maxAttempts: 2, windowMs: 100, maxKeys: 3, now: () => now })

  assert.equal(limiter.allow('203.0.113.20'), true)
  assert.equal(limiter.allow('203.0.113.20'), true)
  assert.equal(limiter.allow('203.0.113.20'), false)
  now += 100
  assert.equal(limiter.allow('203.0.113.20'), true, 'the expired window must allow a new attempt')
})

test('keeps source keys bounded and recovers capacity after expiry', () => {
  let now = 1_000
  const limiter = new OnboardingSourceAttemptLimiter({ maxAttempts: 1, windowMs: 100, maxKeys: 2, now: () => now })

  assert.equal(limiter.allow('203.0.113.1'), true)
  assert.equal(limiter.allow('203.0.113.2'), true)
  assert.equal(limiter.allow('203.0.113.3'), false, 'a new active key must not evict another active key')
  assert.equal(limiter.size, 2)
  now += 100
  assert.equal(limiter.allow('203.0.113.3'), true)
  assert.equal(limiter.size, 1)
})

test('a zero limit disables tracking and allows every source', () => {
  const limiter = new OnboardingSourceAttemptLimiter({ maxAttempts: 0, windowMs: 100, maxKeys: 1 })

  for (let index = 0; index < 10; index += 1) assert.equal(limiter.allow(`203.0.113.${index}`), true)
  assert.equal(limiter.allow(undefined), true)
  assert.equal(limiter.size, 0)
})

test('uses only a normalized direct TCP peer address as the authority', () => {
  assert.equal(authoritativeOnboardingSourceAddress(undefined), undefined)
  assert.equal(authoritativeOnboardingSourceAddress('  '), undefined)
  assert.equal(authoritativeOnboardingSourceAddress('not-an-address'), undefined)
  assert.equal(authoritativeOnboardingSourceAddress('::ffff:203.0.113.7'), '203.0.113.7')
  assert.equal(authoritativeOnboardingSourceAddress('2001:DB8::7'), '2001:db8::7')
})
