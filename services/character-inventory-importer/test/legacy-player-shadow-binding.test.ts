import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import {
  bindLegacyPlayerShadowLocatorV1,
  type LegacyPlayerShadowLocatorV1,
} from '../src/legacy-player-shadow-binding.js'
import { expectedShard, type InventoryRecord } from '../src/inventory.js'

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
