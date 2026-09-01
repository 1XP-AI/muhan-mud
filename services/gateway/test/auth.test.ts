import assert from 'node:assert/strict'
import test from 'node:test'
import { validateJwtClaims, AuthenticationError } from '../src/auth.js'

const config = { jwtIssuer: 'https://project.supabase.co/auth/v1', jwtAudience: 'authenticated' }

test('JWT claim boundary requires issuer audience expiration and subject', () => {
  const identity = validateJwtClaims({
    iss: config.jwtIssuer,
    aud: ['authenticated', 'other'],
    exp: 20,
    sub: 'player-id'
  }, config, 10_000)
  assert.equal(identity.sub, 'player-id')
  assert.equal(identity.expiresAtMs, 20_000)
  assert.throws(() => validateJwtClaims({ iss: config.jwtIssuer, aud: 'authenticated', exp: 10, sub: 'x' }, config, 10_000), AuthenticationError)
  assert.throws(() => validateJwtClaims({ iss: 'wrong', aud: 'authenticated', exp: 20, sub: 'x' }, config, 10_000), /issuer/)
  assert.throws(() => validateJwtClaims({ iss: config.jwtIssuer, aud: 'anon', exp: 20, sub: 'x' }, config, 10_000), /audience/)
  assert.throws(() => validateJwtClaims({ iss: config.jwtIssuer, aud: 'authenticated', exp: 20 }, config, 10_000), /subject/)
})
