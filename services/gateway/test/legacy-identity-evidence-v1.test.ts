import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import {
  LEGACY_IDENTITY_EVIDENCE_V1_MAX_WIRE_LENGTH,
  LegacyIdentityEvidenceV1WireError,
  decodeLegacyIdentityEvidenceV1,
  encodeLegacyIdentityEvidenceV1,
  legacyIdentityEvidenceV1Codec,
  lowerHexToBytes
} from '../src/evidence-codec/legacy-identity-evidence-v1.js'

const testDirectory = fileURLToPath(new URL('.', import.meta.url))
const fixturePath = (name: string): string => resolve(testDirectory, '../../../tests/fixtures', name)
const fixture = async (name: string): Promise<Uint8Array> => lowerHexToBytes((await readFile(fixturePath(name), 'utf8')).trim())
const valid = {
  outcome: 'ok', canonicalization: 'normalized', canonicalName: 'Alice', legacyShard: '35',
  playerFileSha256: '18f8d2eb4a387bbc1e37ec099a7326805739bc9c99ecf0f14b808a5bcb65bf49', storageFormat: 'player-v1'
} as const

test('reproduces every accepted shared V1 fixture byte-for-byte', async () => {
  const ok = await fixture('legacy_identity_evidence_wire_v1_ok.hex')
  const invalidInput = await fixture('legacy_identity_evidence_wire_v1_invalid_input.hex')
  assert.deepEqual(encodeLegacyIdentityEvidenceV1(valid), ok)
  assert.deepEqual(decodeLegacyIdentityEvidenceV1(ok), valid)
  const decodedInvalid = decodeLegacyIdentityEvidenceV1(invalidInput)
  assert.deepEqual(decodedInvalid, { outcome: 'invalid_input', canonicalization: 'invalid', canonicalName: '', legacyShard: '', playerFileSha256: '', storageFormat: 'player-v1' })
  assert.deepEqual(encodeLegacyIdentityEvidenceV1(decodedInvalid), invalidInput)
  assert.equal(legacyIdentityEvidenceV1Codec.encodeHex(valid), (await readFile(fixturePath('legacy_identity_evidence_wire_v1_ok.hex'), 'utf8')).trim())
})

test('accepts a 14-byte name at the maximum V1 wire length', () => {
  const maximumLength = { ...valid, canonicalName: 'FourteenByteID' }
  const wire = encodeLegacyIdentityEvidenceV1(maximumLength)
  assert.equal(maximumLength.canonicalName.length, 14)
  assert.equal(wire.length, LEGACY_IDENTITY_EVIDENCE_V1_MAX_WIRE_LENGTH)
  assert.deepEqual(decodeLegacyIdentityEvidenceV1(wire), maximumLength)
})

test('rejects all shared NUL-invalid fixtures before accepting text', async () => {
  for (const name of ['legacy_identity_evidence_wire_v1_nul_name.hex', 'legacy_identity_evidence_wire_v1_nul_shard.hex', 'legacy_identity_evidence_wire_v1_nul_digest.hex', 'legacy_identity_evidence_wire_v1_nul_storage.hex']) {
    const nulInvalid = await fixture(name)
    assert.throws(() => decodeLegacyIdentityEvidenceV1(nulInvalid), LegacyIdentityEvidenceV1WireError)
  }
})

test('rejects malformed header, length, trailing bytes, and contradictory outcomes', () => {
  const cases: Uint8Array[] = []
  const wire = encodeLegacyIdentityEvidenceV1(valid)
  const magic = wire.slice(); magic[0] ^= 1; cases.push(magic)
  const schema = wire.slice(); schema[9] = 2; cases.push(schema)
  const version = wire.slice(); version[11] = 2; cases.push(version)
  const truncated = wire.slice(0, -1); cases.push(truncated)
  const trailing = new Uint8Array([...wire, 0]); cases.push(trailing)
  const badOutcome = wire.slice(); badOutcome[16] = 99; cases.push(badOutcome)
  const contradictory = wire.slice(); contradictory[16] = 1; cases.push(contradictory)
  for (const malformed of cases) assert.throws(() => decodeLegacyIdentityEvidenceV1(malformed), LegacyIdentityEvidenceV1WireError)
  assert.throws(() => encodeLegacyIdentityEvidenceV1({ ...valid, legacyShard: '3A' }), LegacyIdentityEvidenceV1WireError)
  assert.throws(() => encodeLegacyIdentityEvidenceV1({ ...valid, canonicalName: 'FifteenByteID!!' }), LegacyIdentityEvidenceV1WireError)
  assert.throws(() => encodeLegacyIdentityEvidenceV1({ ...valid, canonicalName: 'Al\0ice' }), LegacyIdentityEvidenceV1WireError)
  assert.throws(() => legacyIdentityEvidenceV1Codec.decodeHex('AA'), LegacyIdentityEvidenceV1WireError)
})
