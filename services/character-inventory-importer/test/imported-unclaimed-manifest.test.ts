import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import test from 'node:test'
import {
  ImportedUnclaimedManifestError,
  parseImportedUnclaimedManifest,
} from '../src/imported-unclaimed-manifest.js'

const digest = (value: Uint8Array): string => createHash('sha256').update(value).digest('hex')
const MAX_MANIFEST_BYTES = 64 * 1024 * 1024

function candidate(name: string, shard: string, sourceSha256: string, sourceSize: number) {
  return {
    legacy_name_key: name,
    legacy_shard: shard,
    source_sha256: sourceSha256,
    source_size: sourceSize,
  }
}

function manifest(overrides: Record<string, unknown> = {}): Buffer {
  return Buffer.from(JSON.stringify({
    format: 'muhan.imported_unclaimed_manifest',
    format_version: 1,
    dry_run: true,
    candidates: [
      candidate('Alice', '35', 'a'.repeat(64), 5),
      candidate('Bob', 'da', 'b'.repeat(64), 7),
    ],
    rejections: [],
    ...overrides,
  }))
}

function assertRejected(raw: Uint8Array): void {
  assert.throws(
    () => parseImportedUnclaimedManifest(raw),
    (error: unknown) => error instanceof ImportedUnclaimedManifestError
      && error.code === 'invalid_imported_unclaimed_manifest'
      && error.message === 'invalid imported-unclaimed manifest',
  )
}

function oversizedWhitespaceManifest(): Buffer {
  const raw = manifest()
  return Buffer.concat([raw, Buffer.alloc(MAX_MANIFEST_BYTES + 1 - raw.byteLength, 0x20)])
}

test('accepts the closed v1 review fixture and fingerprints its exact raw bytes', () => {
  const raw = manifest()
  const parsed = parseImportedUnclaimedManifest(raw)

  assert.deepEqual(parsed, {
    sourceManifestSha256: digest(raw),
    candidates: [
      { legacyNameKey: 'Alice', legacyShard: '35', sourceSha256: 'a'.repeat(64), sourceSize: 5 },
      { legacyNameKey: 'Bob', legacyShard: 'da', sourceSha256: 'b'.repeat(64), sourceSize: 7 },
    ],
  })
  assert.deepEqual(Object.keys(parsed).sort(), ['candidates', 'sourceManifestSha256'])
  assert.deepEqual(Object.keys(parsed.candidates[0]!).sort(), ['legacyNameKey', 'legacyShard', 'sourceSha256', 'sourceSize'])
  assert.doesNotMatch(JSON.stringify(parsed), /player\/|relative_path|password|raw inventory/i)

  const sameMeaningDifferentBytes = Buffer.from(`${raw.toString('utf8')}\n`)
  assert.notEqual(parseImportedUnclaimedManifest(sameMeaningDifferentBytes).sourceManifestSha256, parsed.sourceManifestSha256)
})

test('rejects oversized Buffer and Uint8Array inputs before copying or parsing', () => {
  const oversizedBuffer = oversizedWhitespaceManifest()
  const oversizedUint8Array = new Uint8Array(
    oversizedBuffer.buffer,
    oversizedBuffer.byteOffset,
    oversizedBuffer.byteLength,
  )
  const originalBufferFrom = Buffer.from
  const originalJsonParse = JSON.parse

  try {
    Object.defineProperty(Buffer, 'from', {
      configurable: true,
      value: () => { throw new Error('unexpected raw input copy') },
      writable: true,
    })
    Object.defineProperty(JSON, 'parse', {
      configurable: true,
      value: () => { throw new Error('unexpected JSON parse') },
      writable: true,
    })

    assertRejected(oversizedBuffer)
    assertRejected(oversizedUint8Array)
  } finally {
    Object.defineProperty(Buffer, 'from', {
      configurable: true,
      value: originalBufferFrom,
      writable: true,
    })
    Object.defineProperty(JSON, 'parse', {
      configurable: true,
      value: originalJsonParse,
      writable: true,
    })
  }
})

test('accepts valid JSON whitespace and escaped closed-schema member names', () => {
  const raw = Buffer.from(manifest().toString('utf8')
    .replace('"format"', '"f\\u006frmat"')
    .replace('"legacy_name_key"', '"legacy_\\u006eame_key"')
    .replaceAll(':', ' :\t')
    .replaceAll(',', ',\r\n  ')
    .replace('{', '\r\n {\n  ')
    .replace('}', '\n }\r\n'))

  const parsed = parseImportedUnclaimedManifest(raw)
  assert.equal(parsed.sourceManifestSha256, digest(raw))
  assert.deepEqual(parsed.candidates, [
    { legacyNameKey: 'Alice', legacyShard: '35', sourceSha256: 'a'.repeat(64), sourceSize: 5 },
    { legacyNameKey: 'Bob', legacyShard: 'da', sourceSha256: 'b'.repeat(64), sourceSize: 7 },
  ])
})

test('rejects unknown or missing closed top-level and candidate fields', () => {
  assertRejected(manifest({ unexpected: true }))
  const missingTopLevel = JSON.parse(manifest().toString('utf8')) as Record<string, unknown>
  delete missingTopLevel.rejections
  assertRejected(Buffer.from(JSON.stringify(missingTopLevel)))

  const unknownCandidate = JSON.parse(manifest().toString('utf8')) as { candidates: Array<Record<string, unknown>> }
  unknownCandidate.candidates[0]!.relative_path = 'player/35/Alice'
  assertRejected(Buffer.from(JSON.stringify(unknownCandidate)))
  const missingCandidate = JSON.parse(manifest().toString('utf8')) as { candidates: Array<Record<string, unknown>> }
  delete missingCandidate.candidates[0]!.source_size
  assertRejected(Buffer.from(JSON.stringify(missingCandidate)))
})

test('rejects duplicate JSON members before their final values can satisfy the closed schema', () => {
  const topLevelDuplicate = manifest().toString('utf8').replace(
    '"format":"muhan.imported_unclaimed_manifest"',
    '"f\\u006frmat":"muhan.imported_unclaimed_manifest","format":"muhan.imported_unclaimed_manifest"',
  )
  assertRejected(Buffer.from(topLevelDuplicate))

  const candidateDuplicate = manifest().toString('utf8').replace(
    '"source_size":5',
    '"source_\\u0073ize":5,"source_size":5',
  )
  assertRejected(Buffer.from(candidateDuplicate))
})

test('rejects decoded duplicate member names in nested objects and arrays', () => {
  const nestedObjectDuplicate = manifest().toString('utf8').replace(
    '"rejections":[]',
    '"rejections":[{"nested":{"key":1,"\\u006bey":2}}]',
  )
  assertRejected(Buffer.from(nestedObjectDuplicate))

  const nestedArrayDuplicate = manifest().toString('utf8').replace(
    '"rejections":[]',
    '"rejections":[[{"escaped":1,"\\u0065scaped":2}]]',
  )
  assertRejected(Buffer.from(nestedArrayDuplicate))
})

test('rejects malformed JSON strings, control characters, and nesting over budget generically', () => {
  const malformedEscape = manifest().toString('utf8').replace('"Alice"', '"Ali\\x63e"')
  const unescapedControl = manifest().toString('utf8').replace('"Alice"', '"Ali\u0001ce"')
  const unterminatedString = manifest().toString('utf8').replace('"Alice"', '"Alice')
  const overBudgetNesting = `${'['.repeat(257)}${manifest().toString('utf8')}${']'.repeat(257)}`

  for (const raw of [malformedEscape, unescapedControl, unterminatedString, overBudgetNesting]) {
    assertRejected(Buffer.from(raw))
  }
})

test('rejects malformed UTF-8 and unsafe source sizes with the generic manifest error', () => {
  assertRejected(Buffer.from([0x7b, 0xff, 0x7d]))

  const unsafeSourceSize = manifest().toString('utf8').replace(
    '"source_size":5',
    '"source_size":9007199254740993',
  )
  assertRejected(Buffer.from(unsafeSourceSize))
})

test('rejects format, version, dry-run, and unresolved-rejection mismatches', () => {
  assertRejected(manifest({ format: 'other' }))
  assertRejected(manifest({ format_version: 2 }))
  assertRejected(manifest({ dry_run: false }))
  assertRejected(manifest({ rejections: [{ reasons: ['invalid_entry'] }] }))
})

test('rejects noncanonical, duplicate, and unsorted candidate identities', () => {
  assertRejected(manifest({ candidates: [candidate('alice', '35', 'a'.repeat(64), 5)] }))
  assertRejected(manifest({ candidates: [
    candidate('Alice', '35', 'a'.repeat(64), 5),
    candidate('Alice', '35', 'b'.repeat(64), 7),
  ] }))
  assertRejected(manifest({ candidates: [
    candidate('Bob', 'da', 'b'.repeat(64), 7),
    candidate('Alice', '35', 'a'.repeat(64), 5),
  ] }))
})

test('rejects invalid candidate derivations without exposing input data in errors', () => {
  const cases = [
    candidate('Alice', '00', 'a'.repeat(64), 5),
    candidate('Alice', '35', 'A'.repeat(64), 5),
    candidate('Alice', '35', 'a'.repeat(64), -1),
    candidate('Alice', '35', 'a'.repeat(64), 64 * 1024 * 1024 + 1),
  ]
  for (const invalid of cases) assertRejected(manifest({ candidates: [invalid] }))

  const raw = Buffer.from('{"password":"must-not-appear"}')
  assert.throws(() => parseImportedUnclaimedManifest(raw), (error: unknown) => {
    assert.ok(error instanceof ImportedUnclaimedManifestError)
    assert.equal(error.code, 'invalid_imported_unclaimed_manifest')
    assert.doesNotMatch(error.message, /password|must-not-appear/)
    return true
  })
})
