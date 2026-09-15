import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createMud1cAdmissionContextCodec,
  mud1cAdmissionContextCodec,
  Mud1cAdmissionContextDisabledError,
  Mud1cAdmissionContextError,
  type Mud1cAdmissionContext,
} from '../src/mud1c-admission-context.js'

const secret = '0123456789abcdef0123456789abcdef'
const context: Mud1cAdmissionContext = {
  worldId: 'muhan',
  actorId: '123e4567-e89b-12d3-a456-426614174000',
  characterId: '123e4567-e89b-12d3-a456-426614174001',
  canonicalLegacyNameKey: '416c696365',
  expiresAt: 1000,
  nonce: Uint8Array.from({ length: 32 }, (_, index) => index),
}
const wire = 'MUD1C|muhan|123e4567-e89b-12d3-a456-426614174000|123e4567-e89b-12d3-a456-426614174001|416c696365|1000|000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f|c5429c2f0a9ef7a155c1a89987611c54aa9470aa924e02762672f9421ca01a19'

test('defaults to feature-off and refuses every operation', () => {
  assert.equal(mud1cAdmissionContextCodec.enabled, false)
  assert.throws(() => mud1cAdmissionContextCodec.format(context, secret), Mud1cAdmissionContextDisabledError)
  assert.throws(() => mud1cAdmissionContextCodec.parse(wire), Mud1cAdmissionContextDisabledError)
  assert.throws(() => mud1cAdmissionContextCodec.validate(wire, secret, 1000), Mud1cAdmissionContextDisabledError)
})

test('matches the C format, parse, and raw HMAC vector exactly', () => {
  const codec = createMud1cAdmissionContextCodec({ enabled: true })
  // The exported C helper deliberately supports this standard short-key vector;
  // the >=32 printable secret contract applies only to format and validate.
  assert.equal(codec.hmacSha256Hex('Jefe', 'what do ya want for nothing?'), '5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843')
  assert.equal(codec.hmacSha256Hex(secret, 'MUD1C|vector'), 'ebf774ea200bbedfa06a2f3e425bcd41d877b09f44993d38e2d8374ef4567efc')
  assert.equal(codec.format(context, secret).toString('ascii'), wire)
  assert.deepEqual(codec.parse(Buffer.from(wire, 'ascii')), { context, macHex: 'c5429c2f0a9ef7a155c1a89987611c54aa9470aa924e02762672f9421ca01a19' })
  assert.deepEqual(codec.validate(wire, secret, 1000), context)
})

test('rejects malformed, noncanonical, mismatched, and expired contexts', () => {
  const codec = createMud1cAdmissionContextCodec({ enabled: true })
  const rejects = [
    wire.replace('muhan', 'Muhan'),
    wire.replace('416c696365', '416C696365'),
    wire.replace('|1000|', '|01000|'),
    `${wire}|`,
    wire.replace('123e4567-e89b-12d3-a456-426614174000', 'muhan'),
    `${wire.slice(0, -64)}${'x'.repeat(64)}`,
  ]
  for (const malformed of rejects) assert.throws(() => codec.parse(malformed), Mud1cAdmissionContextError)
  assert.throws(() => codec.validate(wire.replace(/.$/, '0'), secret, 1000), Mud1cAdmissionContextError)
  assert.throws(() => codec.validate(wire, 'x'.repeat(32), 1000), Mud1cAdmissionContextError)
  assert.throws(() => codec.validate(codec.format({ ...context, expiresAt: 999 }, secret), secret, 1000), Mud1cAdmissionContextError)
  assert.throws(() => codec.validate(codec.format({ ...context, expiresAt: 1031 }, secret), secret, 1000), Mud1cAdmissionContextError)
})

test('enforces identity, nonce, secret, and wire bounds before encoding', () => {
  const codec = createMud1cAdmissionContextCodec({ enabled: true })
  for (const invalid of [
    { ...context, worldId: 'muhan!' },
    { ...context, actorId: context.actorId.toUpperCase() },
    { ...context, canonicalLegacyNameKey: '4g' },
    { ...context, nonce: new Uint8Array(31) },
    { ...context, expiresAt: -1 },
  ]) assert.throws(() => codec.format(invalid, secret), Mud1cAdmissionContextError)
  assert.throws(() => codec.format(context, 'short'), Mud1cAdmissionContextError)
  assert.throws(() => codec.format(context, `${secret}\n`), Mud1cAdmissionContextError)
  assert.throws(() => codec.validate(wire, 'short', 1000), Mud1cAdmissionContextError)
  assert.throws(() => codec.validate(wire, `${secret}\n`, 1000), Mud1cAdmissionContextError)
})
