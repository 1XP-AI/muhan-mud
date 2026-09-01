import assert from 'node:assert/strict'
import test from 'node:test'
import fixture from '../../../tests/fixtures/onboarding_protocol_v1.json' with { type: 'json' }
import {
  OnboardingProtocolError,
  OnboardingControlLineParser,
  assertLegalOnboardingTransition,
  canTransitionOnboardingState,
  createOnboardingTicket,
  onboardingProtocolLimits,
  parseOnboardingAuthFrame,
  sanitizeOnboardingLogMetadata,
} from '../src/onboarding-protocol.js'

const actor = '11111111-1111-4111-8111-111111111111'
const correlation = '22222222-2222-4222-8222-222222222222'
const token = 'jwt-token-that-must-never-appear-in-an-error'

test('parses the strict first onboarding auth frame', () => {
  assert.deepEqual(parseOnboardingAuthFrame(JSON.stringify({
    type: 'onboarding-auth', accessToken: token, mode: 'provision', correlationId: correlation,
  })), { type: 'onboarding-auth', accessToken: token, mode: 'provision', correlationId: correlation })
})

test('rejects unknown keys, noncanonical UUIDs, oversized tokens, and binary first frames', () => {
  const valid = { type: 'onboarding-auth', accessToken: token, mode: 'claim', correlationId: correlation }
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, extra: true })), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, correlationId: correlation.replace('2', 'A') })), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, accessToken: 'x'.repeat(8193) })), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(Buffer.from(JSON.stringify(valid))), OnboardingProtocolError)
  assert.throws(() => parseOnboardingAuthFrame(JSON.stringify({ ...valid, mode: 'other' })), OnboardingProtocolError)
})

test('creates both exact MUD1O fixture tickets with configurable clock, TTL, and nonce', () => {
  for (const vector of fixture.vectors) {
    const ticket = createOnboardingTicket({
      mode: vector.mode === 'P' ? 'provision' : 'claim', userId: vector.userId, correlationId: vector.correlationId,
    }, fixture.syntheticSecret, {
      nowMs: (vector.expiresAt * 1000) - 15000,
      ttlMs: 15000,
      nonce: vector.nonce,
    })
    assert.equal(ticket.toString('ascii'), vector.ticket)
  }
})

test('rejects invalid ticket inputs without reflecting secrets', () => {
  assert.throws(() => createOnboardingTicket({
    mode: 'provision', userId: actor, correlationId: correlation,
  }, token, { nowMs: 1_700_000_000_000, ttlMs: 15_000, nonce: 'bad' }), (error: unknown) => {
    assert.ok(error instanceof OnboardingProtocolError)
    assert.ok(!String(error).includes(token))
    return true
  })
})

test('parses fragmented and coalesced bounded C control lines', () => {
  const parser = new OnboardingControlLineParser()
  assert.deepEqual(parser.push('MUD1O RES'), [])
  assert.deepEqual(parser.push('ERVE|6162\nMUD1O SAVED|11111111-1111-4111-8111-111111111111|'.replace('11111111-1111-4111-8111-111111111111', actor)), [
    { type: 'RESERVE', nameHex: '6162' },
  ])
  assert.deepEqual(parser.push('a'.repeat(64) + '|legacy-v1\nMUD1O COMMIT\n'), [
    { type: 'SAVED', characterId: actor, fileSha256: 'a'.repeat(64), storageFormat: 'legacy-v1' },
    { type: 'COMMIT' },
  ])
  assert.throws(() => parser.push('MUD1O WHAT\n'), OnboardingProtocolError)
  assert.throws(() => parser.push('MUD1O ' + 'x'.repeat(2048)), OnboardingProtocolError)
})

test('enforces explicit onboarding state transitions', () => {
  assert.equal(canTransitionOnboardingState('RESERVE', 'RESERVED'), true)
  assert.equal(canTransitionOnboardingState('VERIFIED', 'CLAIMED'), true)
  assert.equal(canTransitionOnboardingState('SAVED', 'COMMIT'), true)
  assert.equal(canTransitionOnboardingState('ERR', 'ABORT'), true)
  assert.equal(canTransitionOnboardingState('RESERVED', 'COMMIT'), false)
  assert.equal(canTransitionOnboardingState('RESERVED', 'VERIFIED'), false)
  assert.equal(canTransitionOnboardingState('RESERVED', 'CLAIMED'), false)
  assert.equal(canTransitionOnboardingState('CLAIMED', 'SAVED'), false)
  assert.throws(() => assertLegalOnboardingTransition('RESERVED', 'COMMIT'), OnboardingProtocolError)
})

test('shares the C ticket and control bounds', () => {
  assert.equal(onboardingProtocolLimits.maxTicketLineBytes, 256)
  assert.equal(onboardingProtocolLimits.maxControlLineBytes, 256)
  assert.equal(onboardingProtocolLimits.maxStorageFormatBytes, 32)
})

test('log metadata is allowlisted and never returns secrets', () => {
  const safe = sanitizeOnboardingLogMetadata({
    mode: 'claim', correlationId: correlation, phase: 'verifying', accessToken: token,
    ticket: 'MUD1O|secret', hmacSecret: 'hmac', gamePassword: 'password', nested: { token },
    reasonCode: token,
  })
  assert.deepEqual(safe, { mode: 'claim', correlationId: correlation, phase: 'verifying' })
  assert.ok(!JSON.stringify(safe).includes(token))
})
