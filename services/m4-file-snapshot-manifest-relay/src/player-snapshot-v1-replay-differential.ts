import { lstat, readdir, readFile } from 'node:fs/promises'
import { isAbsolute, join } from 'node:path'
import type { PlayerSnapshotV1ReplayJournalEntry, PlayerSnapshotV1ReplayJournalV2Entry } from './player-snapshot-v1-replay-shadow-journal.js'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/
const JOURNAL_FIELDS = ['characterId', 'commandId', 'format', 'receiptRequestSha256', 'sourcePostSha256', 'verification', 'version']
const LEVEL_JOURNAL_V2_FIELDS = ['characterId', 'commandId', 'format', 'rawLevelU8', 'receiptRequestSha256', 'sourcePostSha256', 'verification', 'version']
const VERIFICATION_FIELDS = ['algorithm', 'canonicalDigest', 'canonicalOctets', 'format', 'inputDigest', 'inventoryNodeCount', 'version']

/** Maximum immutable journal entries examined by one explicit reconciliation invocation. */
export const PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES = 256

export type PlayerSnapshotV1ReplayDifferentialClassification =
  | 'MATCH' | 'MISSING_DB_ARTIFACT' | 'IDENTITY_MISMATCH' | 'DIGEST_MISMATCH' | 'OCTETS_MISMATCH'
  | 'JOURNAL_INVALID' | 'DB_READ_ERROR' | 'UNEXPECTED_DUPLICATE' | 'JOURNAL_BOUND_EXCEEDED'

/** Stable caller-facing interpretation of the detailed metadata evidence. */
export type PlayerSnapshotV1ReplayDifferentialPrimaryClassification = 'EXACT' | 'MISSING' | 'INCONSISTENT'

/** Metadata copied from the immutable artifact relation; it deliberately excludes payload bytes. */
export interface PlayerSnapshotV1ReplayArtifactEvidence {
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  snapshotFormat: string
  snapshotSha256: string
  snapshotOctets: number
}

/** A read boundary separate from relay stores: implementations may issue SELECT statements only. */
export interface PlayerSnapshotV1ReplayArtifactDifferentialReader {
  findByCommandId(commandId: string): Promise<readonly PlayerSnapshotV1ReplayArtifactEvidence[]>
}

/** Read-only journal access boundary, injectable for deterministic reconciliation checks. */
export interface PlayerSnapshotV1ReplayDifferentialJournalFileReader {
  readDirectory(directory: string): Promise<readonly Buffer[]>
  readEntry(path: string): Promise<string>
}

const defaultJournalFileReader: PlayerSnapshotV1ReplayDifferentialJournalFileReader = {
  readDirectory: async (directory) => readdir(directory, { encoding: 'buffer' }),
  readEntry: async (path) => {
    if (!(await lstat(path)).isFile()) throw new Error('invalid journal entry')
    return readFile(path, 'utf8')
  },
}

export interface PlayerSnapshotV1ReplayDifferentialJournalEvidence {
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  verificationFormat: string
  verificationVersion: string
  verificationAlgorithm: string
  snapshotSha256: string
  snapshotOctets: number
}

export interface PlayerSnapshotV1ReplayDifferentialRecord {
  index: number
  classification: PlayerSnapshotV1ReplayDifferentialClassification
  evidence?: {
    journal: PlayerSnapshotV1ReplayDifferentialJournalEvidence
    artifact?: PlayerSnapshotV1ReplayArtifactEvidence
  }
}

export interface PlayerSnapshotV1ReplayDifferentialResult {
  format: 'player-snapshot-v1-replay-differential'
  version: '1'
  classification: PlayerSnapshotV1ReplayDifferentialPrimaryClassification
  records: readonly PlayerSnapshotV1ReplayDifferentialRecord[]
}

/** A future projection reader may compare this fixed metadata without payload access. */
export interface PlayerSnapshotV1ReplayLevelDifferentialInput {
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  snapshotSha256: string
  snapshotOctets: number
  rawLevelU8: number
}

export interface PlayerSnapshotV1ReplayLevelDifferentialInputRecord {
  index: number
  classification: 'INPUT' | 'JOURNAL_INVALID' | 'JOURNAL_BOUND_EXCEEDED'
  input?: PlayerSnapshotV1ReplayLevelDifferentialInput
}

export interface PlayerSnapshotV1ReplayLevelDifferentialInputResult {
  format: 'player-snapshot-v1-replay-level-differential-input'
  version: '1'
  classification: 'READY' | 'INCONSISTENT'
  records: readonly PlayerSnapshotV1ReplayLevelDifferentialInputRecord[]
}

function hasExactlyKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const actual = Object.keys(value).sort()
  return actual.length === expected.length && actual.every((key, index) => key === expected[index])
}

function isJournalV1Entry(value: unknown): value is PlayerSnapshotV1ReplayJournalEntry {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const entry = value as Record<string, unknown>
  if (!hasExactlyKeys(entry, JOURNAL_FIELDS) || typeof entry.verification !== 'object' || entry.verification === null || Array.isArray(entry.verification)) return false
  const verification = entry.verification as Record<string, unknown>
  return hasExactlyKeys(verification, VERIFICATION_FIELDS)
    && entry.format === 'player-snapshot-v1-replay-shadow-journal' && entry.version === '1'
    && typeof entry.commandId === 'string' && UUID_RE.test(entry.commandId)
    && typeof entry.characterId === 'string' && UUID_RE.test(entry.characterId)
    && typeof entry.receiptRequestSha256 === 'string' && HASH_RE.test(entry.receiptRequestSha256)
    && typeof entry.sourcePostSha256 === 'string' && HASH_RE.test(entry.sourcePostSha256)
    && verification.format === 'player-snapshot-v1-replay-verification' && verification.version === '1' && verification.algorithm === 'sha-256'
    && typeof verification.inputDigest === 'string' && HASH_RE.test(verification.inputDigest)
    && typeof verification.canonicalDigest === 'string' && HASH_RE.test(verification.canonicalDigest)
    && verification.inputDigest === verification.canonicalDigest
    && typeof verification.canonicalOctets === 'number' && Number.isSafeInteger(verification.canonicalOctets) && verification.canonicalOctets >= 0
    && typeof verification.inventoryNodeCount === 'number' && Number.isSafeInteger(verification.inventoryNodeCount) && verification.inventoryNodeCount >= 0
}

/** V2 is intentionally separate: a v1 record never becomes a level source. */
function isLevelJournalV2Entry(value: unknown): value is PlayerSnapshotV1ReplayJournalV2Entry {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const entry = value as Record<string, unknown>
  if (!hasExactlyKeys(entry, LEVEL_JOURNAL_V2_FIELDS) || typeof entry.verification !== 'object' || entry.verification === null || Array.isArray(entry.verification)) return false
  const verification = entry.verification as Record<string, unknown>
  return hasExactlyKeys(verification, VERIFICATION_FIELDS)
    && entry.format === 'player-snapshot-v1-replay-shadow-journal' && entry.version === '2'
    && typeof entry.commandId === 'string' && UUID_RE.test(entry.commandId)
    && typeof entry.characterId === 'string' && UUID_RE.test(entry.characterId)
    && typeof entry.receiptRequestSha256 === 'string' && HASH_RE.test(entry.receiptRequestSha256)
    && typeof entry.sourcePostSha256 === 'string' && HASH_RE.test(entry.sourcePostSha256)
    && typeof entry.rawLevelU8 === 'number' && Number.isSafeInteger(entry.rawLevelU8)
    && entry.rawLevelU8 >= 0 && entry.rawLevelU8 <= 255
    && verification.format === 'player-snapshot-v1-replay-verification' && verification.version === '2' && verification.algorithm === 'sha-256'
    && typeof verification.inputDigest === 'string' && HASH_RE.test(verification.inputDigest)
    && typeof verification.canonicalDigest === 'string' && HASH_RE.test(verification.canonicalDigest)
    && verification.inputDigest === verification.canonicalDigest
    && typeof verification.canonicalOctets === 'number' && Number.isSafeInteger(verification.canonicalOctets) && verification.canonicalOctets >= 0
    && typeof verification.inventoryNodeCount === 'number' && Number.isSafeInteger(verification.inventoryNodeCount) && verification.inventoryNodeCount >= 0
}

function isJournalEntry(value: unknown): value is PlayerSnapshotV1ReplayJournalEntry | PlayerSnapshotV1ReplayJournalV2Entry {
  return isJournalV1Entry(value) || isLevelJournalV2Entry(value)
}

function journalEvidence(entry: PlayerSnapshotV1ReplayJournalEntry | PlayerSnapshotV1ReplayJournalV2Entry): PlayerSnapshotV1ReplayDifferentialJournalEvidence {
  return {
    commandId: entry.commandId, characterId: entry.characterId,
    receiptRequestSha256: entry.receiptRequestSha256, sourcePostSha256: entry.sourcePostSha256,
    verificationFormat: entry.verification.format, verificationVersion: entry.verification.version,
    verificationAlgorithm: entry.verification.algorithm, snapshotSha256: entry.verification.canonicalDigest,
    snapshotOctets: entry.verification.canonicalOctets,
  }
}

function classify(entry: PlayerSnapshotV1ReplayJournalEntry | PlayerSnapshotV1ReplayJournalV2Entry, artifact: PlayerSnapshotV1ReplayArtifactEvidence): PlayerSnapshotV1ReplayDifferentialClassification {
  if (artifact.commandId !== entry.commandId || artifact.characterId !== entry.characterId
    || artifact.receiptRequestSha256 !== entry.receiptRequestSha256 || artifact.sourcePostSha256 !== entry.sourcePostSha256
    || artifact.snapshotFormat !== 'player-snapshot-v1') return 'IDENTITY_MISMATCH'
  if (artifact.snapshotSha256 !== entry.verification.canonicalDigest) return 'DIGEST_MISMATCH'
  if (artifact.snapshotOctets !== entry.verification.canonicalOctets) return 'OCTETS_MISMATCH'
  return 'MATCH'
}

function configuredAbsoluteDirectory(directory: string): string | undefined {
  return directory.length > 0 && !directory.includes('\0') && isAbsolute(directory) ? directory : undefined
}

function primaryClassification(records: readonly PlayerSnapshotV1ReplayDifferentialRecord[]): PlayerSnapshotV1ReplayDifferentialPrimaryClassification {
  if (records.some((record) => record.classification !== 'MATCH' && record.classification !== 'MISSING_DB_ARTIFACT')) return 'INCONSISTENT'
  if (records.some((record) => record.classification === 'MISSING_DB_ARTIFACT')) return 'MISSING'
  return 'EXACT'
}

function resultFor(records: readonly PlayerSnapshotV1ReplayDifferentialRecord[]): PlayerSnapshotV1ReplayDifferentialResult {
  return { format: 'player-snapshot-v1-replay-differential', version: '1', classification: primaryClassification(records), records }
}

function levelInputResult(records: readonly PlayerSnapshotV1ReplayLevelDifferentialInputRecord[]): PlayerSnapshotV1ReplayLevelDifferentialInputResult {
  return {
    format: 'player-snapshot-v1-replay-level-differential-input', version: '1',
    classification: records.some((record) => record.classification !== 'INPUT') ? 'INCONSISTENT' : 'READY',
    records,
  }
}

function levelInput(entry: PlayerSnapshotV1ReplayJournalV2Entry): PlayerSnapshotV1ReplayLevelDifferentialInput {
  return {
    commandId: entry.commandId, characterId: entry.characterId,
    receiptRequestSha256: entry.receiptRequestSha256, sourcePostSha256: entry.sourcePostSha256,
    snapshotSha256: entry.verification.canonicalDigest, snapshotOctets: entry.verification.canonicalOctets,
    rawLevelU8: entry.rawLevelU8,
  }
}

/**
 * Reads only immutable v2 journal metadata for a future level projection
 * comparison. It never opens an artifact payload, a legacy player file, or a
 * database connection; v1 entries fail closed because they contain no level.
 */
export async function readPlayerSnapshotV1ReplayLevelDifferentialInputs(
  directory: string,
  journalFiles: PlayerSnapshotV1ReplayDifferentialJournalFileReader = defaultJournalFileReader,
): Promise<PlayerSnapshotV1ReplayLevelDifferentialInputResult> {
  const configured = configuredAbsoluteDirectory(directory)
  if (!configured) throw new Error('invalid replay differential configuration')
  let names: Buffer[]
  try {
    names = (await journalFiles.readDirectory(configured))
      .filter((name) => name.subarray(-5).equals(Buffer.from('.json')))
      .sort(Buffer.compare)
  } catch {
    return levelInputResult([{ index: 0, classification: 'JOURNAL_INVALID' }])
  }
  if (names.length > PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES) {
    return levelInputResult([{
      index: PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES,
      classification: 'JOURNAL_BOUND_EXCEEDED',
    }])
  }
  const records: PlayerSnapshotV1ReplayLevelDifferentialInputRecord[] = []
  for (const [index, name] of names.entries()) {
    let parsed: unknown
    try { parsed = JSON.parse(await journalFiles.readEntry(join(configured, name.toString('utf8')))) }
    catch { records.push({ index, classification: 'JOURNAL_INVALID' }); continue }
    if (!isLevelJournalV2Entry(parsed)) { records.push({ index, classification: 'JOURNAL_INVALID' }); continue }
    records.push({ index, classification: 'INPUT', input: levelInput(parsed) })
  }
  return levelInputResult(records)
}

/**
 * Reads immutable journal JSON entries in bytewise lexical filename order. The
 * returned result contains only fixed metadata and never file paths, payloads, or parse errors.
 */
export async function comparePlayerSnapshotV1ReplayShadowJournal(
  directory: string,
  reader: PlayerSnapshotV1ReplayArtifactDifferentialReader,
  journalFiles: PlayerSnapshotV1ReplayDifferentialJournalFileReader = defaultJournalFileReader,
): Promise<PlayerSnapshotV1ReplayDifferentialResult> {
  const configured = configuredAbsoluteDirectory(directory)
  if (!configured) throw new Error('invalid replay differential configuration')
  let names: Buffer[]
  try {
    names = (await journalFiles.readDirectory(configured))
      .filter((name) => name.subarray(-5).equals(Buffer.from('.json')))
      .sort(Buffer.compare)
  } catch {
    return resultFor([{ index: 0, classification: 'JOURNAL_INVALID' }])
  }
  if (names.length > PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES) {
    return resultFor([{
      index: PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_MAX_JOURNAL_ENTRIES,
      classification: 'JOURNAL_BOUND_EXCEEDED',
    }])
  }
  const records: PlayerSnapshotV1ReplayDifferentialRecord[] = []
  for (const [index, name] of names.entries()) {
    let parsed: unknown
    try { parsed = JSON.parse(await journalFiles.readEntry(join(configured, name.toString('utf8')))) }
    catch { records.push({ index, classification: 'JOURNAL_INVALID' }); continue }
    if (!isJournalEntry(parsed)) { records.push({ index, classification: 'JOURNAL_INVALID' }); continue }
    const evidence = journalEvidence(parsed)
    try {
      const artifacts = await reader.findByCommandId(parsed.commandId)
      if (artifacts.length === 0) records.push({ index, classification: 'MISSING_DB_ARTIFACT', evidence: { journal: evidence } })
      else if (artifacts.length !== 1) records.push({ index, classification: 'UNEXPECTED_DUPLICATE', evidence: { journal: evidence } })
      else records.push({ index, classification: classify(parsed, artifacts[0]!), evidence: { journal: evidence, artifact: artifacts[0]! } })
    } catch { records.push({ index, classification: 'DB_READ_ERROR', evidence: { journal: evidence } }) }
  }
  return resultFor(records)
}

export function replayDifferentialHasNonMatch(result: PlayerSnapshotV1ReplayDifferentialResult): boolean {
  return result.classification !== 'EXACT'
}
