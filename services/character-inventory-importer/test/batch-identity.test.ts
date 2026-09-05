import assert from 'node:assert/strict'
import test from 'node:test'
import {
  BatchIdentityError,
  createBatchIdentity,
} from '../src/batch-identity.js'

const validInput = () => ({
  worldId: 'celduin-prod',
  sourceManifestId: 'inventory-manifest:2026-09-06',
  sourceSha256: 'a'.repeat(64),
  sourceByteSize: 4096,
  parserVersion: '1.2.3',
  abi: 1,
  startMarker: 'player:000001',
  endMarker: 'player:000250',
})

function assertRejected(value: unknown): void {
  assert.throws(
    () => createBatchIdentity(value),
    (error: unknown) => error instanceof BatchIdentityError
      && error.code === 'invalid_batch_identity'
      && error.message === 'invalid batch identity',
  )
}

test('constructs an immutable canonical batch identity and stable key', () => {
  const identity = createBatchIdentity(validInput())
  const expected = '{"worldId":"celduin-prod","sourceManifestId":"inventory-manifest:2026-09-06","sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","sourceByteSize":4096,"parserVersion":"1.2.3","abi":1,"startMarker":"player:000001","endMarker":"player:000250"}'

  assert.deepEqual(identity, {
    worldId: 'celduin-prod',
    sourceManifestId: 'inventory-manifest:2026-09-06',
    sourceSha256: 'a'.repeat(64),
    sourceByteSize: 4096,
    parserVersion: '1.2.3',
    abi: 1,
    startMarker: 'player:000001',
    endMarker: 'player:000250',
    canonicalSerialization: expected,
    stableKey: expected,
  })
  assert.equal(identity.canonicalSerialization, identity.stableKey)
  assert.ok(Object.isFrozen(identity))
  assert.throws(() => {
    ;(identity as { worldId: string }).worldId = 'other-world'
  }, TypeError)
})

test('uses the canonical field order rather than caller member order', () => {
  const reversed = {
    endMarker: 'player:000250',
    startMarker: 'player:000001',
    abi: 1,
    parserVersion: '1.2.3',
    sourceByteSize: 4096,
    sourceSha256: 'a'.repeat(64),
    sourceManifestId: 'inventory-manifest:2026-09-06',
    worldId: 'celduin-prod',
  }

  assert.equal(createBatchIdentity(reversed).stableKey, createBatchIdentity(validInput()).stableKey)
})

test('accepts strict SemVer prerelease and build metadata', () => {
  for (const parserVersion of ['1.2.3-RC.1', '1.2.3+build.7']) {
    assert.equal(createBatchIdentity({ ...validInput(), parserVersion }).parserVersion, parserVersion)
  }
})

test('rejects unknown enumerable Symbol own keys', () => {
  const input = validInput()
  input[Symbol('unknown')] = true

  assertRejected(input)
})

test('rejects missing, unknown, malformed, and noncanonical fields', () => {
  const missing = validInput() as Record<string, unknown>
  delete missing.endMarker
  assertRejected(missing)
  assertRejected({ ...validInput(), extra: true })
  assertRejected(null)
  assertRejected([])

  const invalidFields: ReadonlyArray<Readonly<Record<string, unknown>>> = [
    { ...validInput(), worldId: 'Celduin' },
    { ...validInput(), worldId: 'celduin/' },
    { ...validInput(), sourceManifestId: 'Inventory-manifest:2026-09-06' },
    { ...validInput(), sourceManifestId: 'inventory-manifest:2026-09-06/' },
    { ...validInput(), sourceSha256: 'A'.repeat(64) },
    { ...validInput(), sourceSha256: 'a'.repeat(63) },
    { ...validInput(), sourceByteSize: -1 },
    { ...validInput(), sourceByteSize: 1.5 },
    { ...validInput(), sourceByteSize: Number.MAX_SAFE_INTEGER + 1 },
    { ...validInput(), parserVersion: 'v1.2.3' },
    { ...validInput(), parserVersion: '01.2.3' },
    { ...validInput(), parserVersion: '1.2' },
    { ...validInput(), parserVersion: '1.2.3-01' },
    { ...validInput(), parserVersion: '1.2.3+' },
    { ...validInput(), parserVersion: '1.2.3+build..7' },
    { ...validInput(), abi: 0 },
    { ...validInput(), abi: 1.5 },
    { ...validInput(), startMarker: 'Player:000001' },
    { ...validInput(), startMarker: 'player/000001' },
    { ...validInput(), endMarker: 'player:000001' },
  ]

  for (const value of invalidFields) assertRejected(value)
})

test('does not retain or mutate the caller input object', () => {
  const input = validInput()
  const identity = createBatchIdentity(input)
  input.worldId = 'other-world'
  input.sourceSha256 = 'b'.repeat(64)

  assert.equal(identity.worldId, 'celduin-prod')
  assert.equal(identity.sourceSha256, 'a'.repeat(64))
})
