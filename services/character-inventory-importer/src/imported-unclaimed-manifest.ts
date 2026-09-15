import { createHash } from 'node:crypto'
import { canonicalNameKey, validRecord, type InventoryRecord } from './inventory.js'

const FORMAT = 'muhan.imported_unclaimed_manifest'
const FORMAT_VERSION = 1
const MAX_CANDIDATES = 100_000
// Match the importer's existing JSONL and scanned-file input ceiling.
const MAX_MANIFEST_BYTES = 64 * 1024 * 1024
const MAX_JSON_NESTING = 256

export interface ImportedUnclaimedManifestCandidate {
  readonly legacyNameKey: string
  readonly legacyShard: string
  readonly sourceSha256: string
  readonly sourceSize: number
}

export interface ParsedImportedUnclaimedManifest {
  /** SHA-256 of the exact manifest bytes supplied to this parser. */
  readonly sourceManifestSha256: string
  /** Closed candidate metadata; it intentionally excludes paths and player data. */
  readonly candidates: readonly ImportedUnclaimedManifestCandidate[]
}

/** The only observable failure detail; never attach source manifest data to it. */
export class ImportedUnclaimedManifestError extends Error {
  readonly code = 'invalid_imported_unclaimed_manifest'

  constructor() {
    super('invalid imported-unclaimed manifest')
    this.name = 'ImportedUnclaimedManifestError'
  }
}

function invalid(): never {
  throw new ImportedUnclaimedManifestError()
}

/**
 * JSON.parse intentionally keeps the last duplicate member, so scan the
 * grammar first and reject duplicate decoded member names at every depth.
 */
class StrictJsonStructureValidator {
  private index = 0

  constructor(private readonly text: string) {}

  validate(): boolean {
    this.skipWhitespace()
    if (!this.value(0)) return false
    this.skipWhitespace()
    return this.index === this.text.length
  }

  private value(depth: number): boolean {
    if (depth > MAX_JSON_NESTING) return false
    switch (this.text[this.index]) {
      case '{': return this.object(depth + 1)
      case '[': return this.array(depth + 1)
      case '"': return this.string(false) === true
      case 't': return this.literal('true')
      case 'f': return this.literal('false')
      case 'n': return this.literal('null')
      default: return this.number()
    }
  }

  private object(depth: number): boolean {
    this.index++
    this.skipWhitespace()
    if (this.text[this.index] === '}') {
      this.index++
      return true
    }

    const keys = new Set<string>()
    while (true) {
      const key = this.string(true)
      if (typeof key !== 'string' || keys.has(key)) return false
      keys.add(key)

      this.skipWhitespace()
      if (this.text[this.index] !== ':') return false
      this.index++
      this.skipWhitespace()
      if (!this.value(depth)) return false
      this.skipWhitespace()

      if (this.text[this.index] === '}') {
        this.index++
        return true
      }
      if (this.text[this.index] !== ',') return false
      this.index++
      this.skipWhitespace()
    }
  }

  private array(depth: number): boolean {
    this.index++
    this.skipWhitespace()
    if (this.text[this.index] === ']') {
      this.index++
      return true
    }

    while (true) {
      if (!this.value(depth)) return false
      this.skipWhitespace()
      if (this.text[this.index] === ']') {
        this.index++
        return true
      }
      if (this.text[this.index] !== ',') return false
      this.index++
      this.skipWhitespace()
    }
  }

  /**
   * Values only need syntactic validation. Decode member names alone so the
   * duplicate check observes JSON-decoded keys without retaining arbitrary
   * string values from the untrusted manifest.
   */
  private string(decodeMemberName: boolean): string | true | undefined {
    if (this.text[this.index] !== '"') return undefined
    this.index++
    const decoded = decodeMemberName ? [] as string[] : undefined

    while (this.index < this.text.length) {
      const character = this.text[this.index++]!
      if (character === '"') return decoded?.join('') ?? true
      if (character.charCodeAt(0) <= 0x1f) return undefined
      if (character !== '\\') {
        decoded?.push(character)
        continue
      }

      const escape = this.text[this.index++]
      switch (escape) {
        case '"': decoded?.push('"'); break
        case '\\': decoded?.push('\\'); break
        case '/': decoded?.push('/'); break
        case 'b': decoded?.push('\b'); break
        case 'f': decoded?.push('\f'); break
        case 'n': decoded?.push('\n'); break
        case 'r': decoded?.push('\r'); break
        case 't': decoded?.push('\t'); break
        case 'u': {
          const hex = this.text.slice(this.index, this.index + 4)
          if (hex.length !== 4 || !/^[0-9a-fA-F]{4}$/.test(hex)) return undefined
          decoded?.push(String.fromCharCode(Number.parseInt(hex, 16)))
          this.index += 4
          break
        }
        default: return undefined
      }
    }
    return undefined
  }

  private number(): boolean {
    const start = this.index
    if (this.text[this.index] === '-') this.index++

    if (this.text[this.index] === '0') {
      this.index++
    } else if (this.isDigitOneToNine(this.text[this.index])) {
      this.index++
      while (this.isDigit(this.text[this.index])) this.index++
    } else {
      this.index = start
      return false
    }

    if (this.text[this.index] === '.') {
      this.index++
      if (!this.isDigit(this.text[this.index])) return false
      while (this.isDigit(this.text[this.index])) this.index++
    }
    if (this.text[this.index] === 'e' || this.text[this.index] === 'E') {
      this.index++
      if (this.text[this.index] === '+' || this.text[this.index] === '-') this.index++
      if (!this.isDigit(this.text[this.index])) return false
      while (this.isDigit(this.text[this.index])) this.index++
    }
    return true
  }

  private literal(value: string): boolean {
    if (!this.text.startsWith(value, this.index)) return false
    this.index += value.length
    return true
  }

  private skipWhitespace(): void {
    while (this.text[this.index] === ' ' || this.text[this.index] === '\n'
      || this.text[this.index] === '\r' || this.text[this.index] === '\t') this.index++
  }

  private isDigit(character: string | undefined): boolean {
    return character !== undefined && character >= '0' && character <= '9'
  }

  private isDigitOneToNine(character: string | undefined): boolean {
    return character !== undefined && character >= '1' && character <= '9'
  }
}

function hasUniqueJsonObjectMembers(text: string): boolean {
  return new StrictJsonStructureValidator(text).validate()
}

function object(value: unknown): Record<string, unknown> | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const actual = Object.keys(value)
  return actual.length === expected.length && actual.every((key) => expected.includes(key))
}

function compareCodePoints(left: string, right: string): number {
  const leftPoints = Array.from(left)
  const rightPoints = Array.from(right)
  for (let index = 0; index < leftPoints.length && index < rightPoints.length; index++) {
    const difference = leftPoints[index]!.codePointAt(0)! - rightPoints[index]!.codePointAt(0)!
    if (difference !== 0) return difference
  }
  return leftPoints.length - rightPoints.length
}

function compareCandidates(left: ImportedUnclaimedManifestCandidate, right: ImportedUnclaimedManifestCandidate): number {
  return compareCodePoints(left.legacyNameKey, right.legacyNameKey)
    || compareCodePoints(left.legacyShard, right.legacyShard)
    || compareCodePoints(left.sourceSha256, right.sourceSha256)
    || left.sourceSize - right.sourceSize
}

function parseCandidate(value: unknown): ImportedUnclaimedManifestCandidate {
  const candidate = object(value)
  if (!candidate || !hasExactKeys(candidate, ['legacy_name_key', 'legacy_shard', 'source_sha256', 'source_size'])) return invalid()
  if (typeof candidate.legacy_name_key !== 'string' || typeof candidate.legacy_shard !== 'string'
    || typeof candidate.source_sha256 !== 'string' || typeof candidate.source_size !== 'number'
    || !Number.isSafeInteger(candidate.source_size)) return invalid()

  // Reuse the importer's independently established identity derivations and
  // size/digest bounds without retaining its path-shaped record.
  const probe: InventoryRecord = {
    name: candidate.legacy_name_key,
    canonicalNameKey: candidate.legacy_name_key,
    relativePath: `player/${candidate.legacy_shard}/${candidate.legacy_name_key}`,
    observedShard: candidate.legacy_shard,
    expectedShard: candidate.legacy_shard,
    byteSize: candidate.source_size,
    sha256: candidate.source_sha256,
  }
  if (candidate.legacy_name_key !== canonicalNameKey(candidate.legacy_name_key) || !validRecord(probe)) return invalid()

  return Object.freeze({
    legacyNameKey: candidate.legacy_name_key,
    legacyShard: candidate.legacy_shard,
    sourceSha256: candidate.source_sha256,
    sourceSize: candidate.source_size,
  })
}

/**
 * Strictly admit the v1 review-only manifest generated by
 * build-imported-unclaimed-manifest.py. This boundary is pure: it opens no
 * files, connects to no database, and exposes neither source paths nor raw
 * player/inventory data on success or failure.
 */
export function parseImportedUnclaimedManifest(rawManifest: Uint8Array): ParsedImportedUnclaimedManifest {
  // Check the caller-provided view before Buffer.from can duplicate it or any
  // decoding/scanning/parsing work can retain manifest-derived data.
  if (rawManifest.byteLength > MAX_MANIFEST_BYTES) return invalid()
  const raw = Buffer.from(rawManifest)
  const text = raw.toString('utf8')
  if (!Buffer.from(text, 'utf8').equals(raw)) return invalid()
  if (!hasUniqueJsonObjectMembers(text)) return invalid()

  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return invalid()
  }
  const manifest = object(parsed)
  if (!manifest || !hasExactKeys(manifest, ['format', 'format_version', 'dry_run', 'candidates', 'rejections'])) return invalid()
  if (manifest.format !== FORMAT || manifest.format_version !== FORMAT_VERSION || manifest.dry_run !== true
    || !Array.isArray(manifest.candidates) || manifest.candidates.length > MAX_CANDIDATES
    || !Array.isArray(manifest.rejections) || manifest.rejections.length !== 0) return invalid()

  const candidates = manifest.candidates.map(parseCandidate)
  const identities = new Set<string>()
  for (let index = 0; index < candidates.length; index++) {
    const candidate = candidates[index]!
    if (identities.has(candidate.legacyNameKey)) return invalid()
    identities.add(candidate.legacyNameKey)
    if (index > 0 && compareCandidates(candidates[index - 1]!, candidate) >= 0) return invalid()
  }

  return Object.freeze({
    sourceManifestSha256: createHash('sha256').update(raw).digest('hex'),
    candidates: Object.freeze(candidates),
  })
}
