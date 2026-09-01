import assert from 'node:assert/strict'
import test from 'node:test'
import { createAdmissionTicket } from '../src/admission-ticket.js'
import { CharacterAuthorizationError } from '../src/character-authorizer.js'

const secret = '0123456789abcdef0123456789abcdef'
const actor = '123e4567-e89b-12d3-a456-426614174000'
const character = '123e4567-e89b-12d3-a456-426614174001'

test('creates the C-compatible ASCII HMAC admission line byte-for-byte', () => {
  const line = createAdmissionTicket({
    actorUserId: actor,
    characterId: character,
    legacyNameKey: 'Contracthero',
    nowMs: 1_700_000_000_000,
    jwtExpiresAtMs: 1_700_000_020_000
  }, secret, { randomBytes: () => Buffer.from([...Array(16).keys()]) })

  assert.equal(line.toString('ascii'),
    'MUD1|1700000015|000102030405060708090a0b0c0d0e0f|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|436f6e74726163746865726f|efeb13872f4540861fbc80512f9a55e5367f88308bc7a3702a2a4fefe6cf135c\n')
})

test('fails closed at the JWT ticket-expiry boundary and for noncanonical names', () => {
  assert.throws(() => createAdmissionTicket({
    actorUserId: actor,
    characterId: character,
    legacyNameKey: 'Contracthero',
    nowMs: 1_700_000_000_900,
    jwtExpiresAtMs: 1_700_000_000_950
  }, secret, { randomBytes: () => Buffer.alloc(16) }), CharacterAuthorizationError)
  assert.throws(() => createAdmissionTicket({
    actorUserId: actor,
    characterId: character,
    legacyNameKey: 'contracthero',
    nowMs: 1_700_000_000_000,
    jwtExpiresAtMs: 1_700_000_020_000
  }, secret, { randomBytes: () => Buffer.alloc(16) }), CharacterAuthorizationError)
})
