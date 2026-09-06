import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import {
  bindLegacyPlayerShadowEvidenceV1,
  bindLegacyPlayerShadowLocatorV1,
  type LegacyIdentityEvidenceV1Shape,
  type LegacyPlayerShadowLocatorV1,
} from '../src/legacy-player-shadow-binding.js'
import { expectedShard, type InventoryRecord } from '../src/inventory.js'
import { importerBindingFixture } from './legacy-identity-evidence-fixture.js'

function admittedRecord(name = 'Alice'): InventoryRecord {
  const shard = expectedShard(name)
  return {
    name,
    canonicalNameKey: name,
    relativePath: `player/${shard}/${name}`,
    observedShard: shard,
    expectedShard: shard,
    byteSize: 1,
    sha256: 'a'.repeat(64),
  }
}

function locator(name = 'Alice'): LegacyPlayerShadowLocatorV1 {
  return {
    canonicalName: name,
    nameSha1: createHash('sha1').update(name, 'utf8').digest('hex'),
    shard: expectedShard(name),
  }
}

function evidence(record = admittedRecord()): LegacyIdentityEvidenceV1Shape {
  return {
    outcome: 'ok',
    canonicalization: 'canonical',
    canonicalName: record.name,
    legacyShard: record.expectedShard,
    playerFileSha256: record.sha256,
    storageFormat: 'player-v1',
  }
}

test('binds an admitted inventory record to an exact closed legacy shadow locator', () => {
  const result = bindLegacyPlayerShadowLocatorV1(admittedRecord(), locator())
  assert.deepEqual(result, {
    canonicalName: 'Alice',
    nameSha1: '35318264c9a98faf79965c270ac80c5606774df1',
    shard: '35',
  })
  assert.deepEqual(Object.keys(result ?? {}).sort(), ['canonicalName', 'nameSha1', 'shard'])
})

test('rejects canonical-name, SHA-1, and shard disagreement without a partial binding result', () => {
  const record = admittedRecord()
  const valid = locator()
  for (const invalid of [
    locator('Bob'),
    { ...valid, nameSha1: 'b'.repeat(40) },
    { ...valid, shard: '00' },
    { ...valid, canonicalName: 'Alice', nameSha1: valid.nameSha1, shard: expectedShard('Bob') },
  ]) {
    assert.equal(bindLegacyPlayerShadowLocatorV1(record, invalid), undefined)
  }
})

test('rejects the full four-field Rust identity rather than stripping its level', () => {
  assert.equal(bindLegacyPlayerShadowLocatorV1(admittedRecord(), { ...locator(), level: 17 }), undefined)
})

test('rejects extras, symbols, accessors, and non-enumerable fields', () => {
  const valid = locator()
  const withSymbol = { ...valid, [Symbol('extra')]: true }
  const withAccessor = Object.defineProperty({ ...valid }, 'nameSha1', {
    enumerable: true,
    get: () => valid.nameSha1,
  })
  const withNonEnumerable = Object.defineProperty({ ...valid }, 'hidden', {
    enumerable: false,
    value: true,
  })
  const coreNonEnumerable = Object.defineProperty({ ...valid }, 'shard', {
    enumerable: false,
    value: valid.shard,
  })
  for (const invalid of [
    { ...valid, extra: true },
    withSymbol,
    withAccessor,
    withNonEnumerable,
    coreNonEnumerable,
  ]) {
    assert.equal(bindLegacyPlayerShadowLocatorV1(admittedRecord(), invalid), undefined)
  }
})

test('binds canonical and normalized strict identity evidence to the existing locator shape', () => {
  const record = admittedRecord()
  for (const canonicalization of ['canonical', 'normalized']) {
    const result = bindLegacyPlayerShadowEvidenceV1(record, { ...evidence(record), canonicalization })
    assert.deepEqual(result, locator())
  }
})

test('binds the shared strict Rust-decoded evidence contract to its matching inventory record', () => {
  const { evidence, record } = importerBindingFixture()
  assert.deepEqual(bindLegacyPlayerShadowEvidenceV1(record, evidence), {
    canonicalName: evidence.canonicalName,
    nameSha1: createHash('sha1').update(evidence.canonicalName, 'utf8').digest('hex'),
    shard: evidence.legacyShard,
  })
})

test('accepts and binds valid null-prototype identity evidence', () => {
  const record = admittedRecord()
  const nullPrototypeEvidence = Object.create(null, Object.getOwnPropertyDescriptors(evidence(record)))

  assert.deepEqual(bindLegacyPlayerShadowEvidenceV1(record, nullPrototypeEvidence), locator())
})

test('rejects every evidence identity mismatch and non-success outcome', () => {
  const record = admittedRecord()
  const valid = evidence(record)
  const invalid = [
    { ...valid, outcome: 'not_found' },
    { ...valid, canonicalization: 'invalid' },
    { ...valid, storageFormat: 'legacy-v1' },
    { ...valid, canonicalName: 'Bob' },
    { ...valid, legacyShard: expectedShard('Bob') },
    { ...valid, playerFileSha256: 'b'.repeat(64) },
    { ...valid, playerFileSha256: 'A'.repeat(64) },
    { ...valid, playerFileSha256: 'f'.repeat(63) },
  ]
  for (const candidate of invalid) assert.equal(bindLegacyPlayerShadowEvidenceV1(record, candidate), undefined)
})

test('rejects malformed evidence containers, extra fields, symbols, and accessors', () => {
  const valid = evidence()
  const withSymbol = { ...valid, [Symbol('extra')]: true }
  const withAccessor = Object.defineProperty({ ...valid }, 'legacyShard', {
    enumerable: true,
    get: () => valid.legacyShard,
  })
  const withNonEnumerable = Object.defineProperty({ ...valid }, 'hidden', {
    enumerable: false,
    value: true,
  })
  const coreNonEnumerable = Object.defineProperty({ ...valid }, 'legacyShard', {
    enumerable: false,
    value: valid.legacyShard,
  })
  for (const candidate of [
    { ...valid, extra: true }, withSymbol, withAccessor, withNonEnumerable, coreNonEnumerable,
    null, [],
  ]) assert.equal(bindLegacyPlayerShadowEvidenceV1(admittedRecord(), candidate), undefined)
})

test('does not mutate the validated record or strict evidence on rejection or acceptance', () => {
  const record = admittedRecord()
  const valid = evidence(record)
  const recordBefore = structuredClone(record)
  const evidenceBefore = structuredClone(valid)
  assert.deepEqual(bindLegacyPlayerShadowEvidenceV1(record, valid), locator())
  assert.equal(bindLegacyPlayerShadowEvidenceV1(record, { ...valid, canonicalName: 'Bob' }), undefined)
  assert.deepEqual(record, recordBefore)
  assert.deepEqual(valid, evidenceBefore)
})
