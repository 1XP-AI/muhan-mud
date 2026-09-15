import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import fixture from '../../../tests/fixtures/mud1c_admission_context_conformance_v1.json' with { type: 'json' }
import {
  createMud1cAdmissionContextCodec,
  Mud1cAdmissionContextError,
  type Mud1cAdmissionContext,
} from '../src/mud1c-admission-context.js'

const oracle = process.env.MUD1C_ADMISSION_CONTEXT_C_ORACLE

function c(...args: string[]): string {
  assert.ok(oracle, 'the MUD1C C oracle must be supplied by the harness')
  const result = spawnSync(oracle, args, { encoding: 'utf8' })
  assert.equal(result.error, undefined)
  assert.equal(result.status, 0, result.stderr)
  return result.stdout.trimEnd()
}

function context(): Mud1cAdmissionContext {
  return {
    worldId: fixture.context.worldId,
    actorId: fixture.context.actorId,
    characterId: fixture.context.characterId,
    canonicalLegacyNameKey: fixture.context.canonicalLegacyNameKey,
    expiresAt: fixture.context.expiresAt,
    nonce: Uint8Array.from(Buffer.from(fixture.context.nonceHex, 'hex')),
  }
}

test('C and Gateway format the canonical fixture byte-for-byte', {
  skip: oracle ? false : 'run scripts/run-mud1c-admission-context-conformance.sh to build the C oracle',
}, () => {
  const codec = createMud1cAdmissionContextCodec({ enabled: true })
  const tsWire = codec.format(context(), fixture.secret).toString('ascii')
  const cWire = c('format', fixture.secret, fixture.context.worldId, fixture.context.actorId,
    fixture.context.characterId, fixture.context.canonicalLegacyNameKey,
    String(fixture.context.expiresAt), fixture.context.nonceHex)

  assert.equal(tsWire, fixture.canonicalWire)
  assert.equal(cWire, fixture.canonicalWire)
  assert.deepEqual(codec.validate(cWire, fixture.secret, fixture.now), context())
  assert.equal(c('validate', fixture.secret, String(fixture.now), tsWire), 'accepted')
})

test('C and Gateway reject every fixed malformed parse vector', {
  skip: oracle ? false : 'run scripts/run-mud1c-admission-context-conformance.sh to build the C oracle',
}, () => {
  const codec = createMud1cAdmissionContextCodec({ enabled: true })
  for (const vector of fixture.parseRejects) {
    assert.equal(c('parse', vector.wire), 'rejected', vector.name)
    assert.throws(() => codec.parse(vector.wire), Mud1cAdmissionContextError, vector.name)
  }
})

test('C and Gateway reject every fixed invalid validation vector', {
  skip: oracle ? false : 'run scripts/run-mud1c-admission-context-conformance.sh to build the C oracle',
}, () => {
  const codec = createMud1cAdmissionContextCodec({ enabled: true })
  for (const vector of fixture.validationRejects) {
    assert.equal(c('validate', fixture.secret, String(fixture.now), vector.wire), 'rejected', vector.name)
    assert.throws(() => codec.validate(vector.wire, fixture.secret, fixture.now), Mud1cAdmissionContextError, vector.name)
  }
})
