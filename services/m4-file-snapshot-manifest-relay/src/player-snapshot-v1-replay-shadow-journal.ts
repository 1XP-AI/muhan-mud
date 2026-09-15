import { createHash, randomUUID } from 'node:crypto'
import { link, open, unlink } from 'node:fs/promises'
import { isAbsolute, join } from 'node:path'
import type { PlayerSnapshotV1ReplayVerification } from './player-snapshot-v1-replay-verifier.js'
/** The journal has a closed metadata schema; verifier extras are never durable. */
export type PlayerSnapshotV1ReplayJournalVerification = Pick<PlayerSnapshotV1ReplayVerification,
  'format' | 'version' | 'algorithm' | 'inputDigest' | 'canonicalDigest' | 'canonicalOctets' | 'inventoryNodeCount'>

/** V2 keeps the same proof fields and identifies the v2 verifier report. */
export interface PlayerSnapshotV1ReplayJournalV2Verification {
  format: 'player-snapshot-v1-replay-verification'
  version: '2'
  algorithm: 'sha-256'
  inputDigest: string
  canonicalDigest: string
  canonicalOctets: number
  inventoryNodeCount: number
}

export interface PlayerSnapshotV1ReplayJournalEntry {
  format: 'player-snapshot-v1-replay-shadow-journal'
  version: '1'
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  verification: PlayerSnapshotV1ReplayJournalVerification
}

/**
 * Closed level-capable journal schema. Its raw U8 is supplied by the v2 Rust
 * verifier's existing canonical decode, never by a Node payload re-read.
 */
export interface PlayerSnapshotV1ReplayJournalV2Entry {
  format: 'player-snapshot-v1-replay-shadow-journal'
  version: '2'
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  rawLevelU8: number
  verification: PlayerSnapshotV1ReplayJournalV2Verification
}

export type PlayerSnapshotV1ReplayJournalAppend = 'appended' | 'duplicate'

/** A local journal is strictly a shadow sink, never a source of relay state. */
export interface PlayerSnapshotV1ReplayShadowJournal {
  append(entry: PlayerSnapshotV1ReplayJournalEntry): Promise<PlayerSnapshotV1ReplayJournalAppend>
}

/** An explicit v2 sink, kept separate from the established v1 journal API. */
export interface PlayerSnapshotV2ReplayShadowJournal {
  append(entry: PlayerSnapshotV1ReplayJournalV2Entry): Promise<PlayerSnapshotV1ReplayJournalAppend>
}

export interface PlayerSnapshotV1ReplayShadowJournalFile {
  writeFile(data: Uint8Array): Promise<void>
  sync(): Promise<void>
  close(): Promise<void>
}

/** Injectable only to prove that a failed partial write is never published. */
export interface PlayerSnapshotV1ReplayShadowJournalFilesystem {
  open(path: string, flags: 'wx', mode: number): Promise<PlayerSnapshotV1ReplayShadowJournalFile>
  link(existingPath: string, newPath: string): Promise<void>
  unlink(path: string): Promise<void>
}

const nodeFilesystem: PlayerSnapshotV1ReplayShadowJournalFilesystem = { open, link, unlink }

function configuredAbsolutePath(path: string): string | undefined {
  return path.length > 0 && !path.includes('\0') && isAbsolute(path) ? path : undefined
}

/** Canonical field order makes both bytes and entry identity independent of caller object shape. */
function normalizedEntry(entry: PlayerSnapshotV1ReplayJournalEntry): PlayerSnapshotV1ReplayJournalEntry {
  return {
    format: 'player-snapshot-v1-replay-shadow-journal',
    version: '1',
    commandId: entry.commandId,
    characterId: entry.characterId,
    receiptRequestSha256: entry.receiptRequestSha256,
    sourcePostSha256: entry.sourcePostSha256,
    verification: {
      format: entry.verification.format,
      version: entry.verification.version,
      algorithm: entry.verification.algorithm,
      inputDigest: entry.verification.inputDigest,
      canonicalDigest: entry.verification.canonicalDigest,
      canonicalOctets: entry.verification.canonicalOctets,
      inventoryNodeCount: entry.verification.inventoryNodeCount,
    },
  }
}

function normalizedV2Entry(entry: PlayerSnapshotV1ReplayJournalV2Entry): PlayerSnapshotV1ReplayJournalV2Entry {
  return {
    format: 'player-snapshot-v1-replay-shadow-journal',
    version: '2',
    commandId: entry.commandId,
    characterId: entry.characterId,
    receiptRequestSha256: entry.receiptRequestSha256,
    sourcePostSha256: entry.sourcePostSha256,
    rawLevelU8: entry.rawLevelU8,
    verification: {
      format: entry.verification.format,
      version: entry.verification.version,
      algorithm: entry.verification.algorithm,
      inputDigest: entry.verification.inputDigest,
      canonicalDigest: entry.verification.canonicalDigest,
      canonicalOctets: entry.verification.canonicalOctets,
      inventoryNodeCount: entry.verification.inventoryNodeCount,
    },
  }
}

function encodedEntry(entry: PlayerSnapshotV1ReplayJournalEntry): Uint8Array {
  return Buffer.from(JSON.stringify(normalizedEntry(entry)), 'utf8')
}

function encodedV2Entry(entry: PlayerSnapshotV1ReplayJournalV2Entry): Uint8Array {
  return Buffer.from(JSON.stringify(normalizedV2Entry(entry)), 'utf8')
}

/**
 * The configured path is a pre-existing local directory. Each observation is
 * a single immutable entry linked into place only after its private temporary
 * file was completely written and synced. An interrupted temporary file is not
 * a journal entry and retries use the same deterministic final entry name.
 */
export class NodePlayerSnapshotV1ReplayShadowJournal implements PlayerSnapshotV1ReplayShadowJournal {
  private readonly directory: string | undefined

  constructor(
    directory: string,
    private readonly filesystem: PlayerSnapshotV1ReplayShadowJournalFilesystem = nodeFilesystem,
  ) {
    this.directory = configuredAbsolutePath(directory)
  }

  /** Lets the observer fail closed before an invalid configured journal can run a verifier. */
  get isEnabled(): boolean { return this.directory !== undefined }

  async append(entry: PlayerSnapshotV1ReplayJournalEntry): Promise<PlayerSnapshotV1ReplayJournalAppend> {
    const directory = this.directory
    if (!directory) throw new Error('invalid replay shadow journal path')
    const bytes = encodedEntry(entry)
    const filename = `${createHash('sha256').update(bytes).digest('hex')}.json`
    const target = join(directory, filename)
    const temporary = join(directory, `.${filename}.${process.pid}.${randomUUID()}.tmp`)
    let handle: PlayerSnapshotV1ReplayShadowJournalFile | undefined
    let outcome: PlayerSnapshotV1ReplayJournalAppend | undefined
    let appendFailure: unknown
    let appendFailed = false
    try {
      handle = await this.filesystem.open(temporary, 'wx', 0o600)
      await handle.writeFile(bytes)
      await handle.sync()
      await handle.close()
      handle = undefined
      try {
        await this.filesystem.link(temporary, target)
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code === 'EEXIST') outcome = 'duplicate'
        else throw error
      }
      if (!outcome) {
        outcome = 'appended'
      }
    } catch (error) {
      appendFailed = true
      appendFailure = error
    }

    const cleanupFailures: unknown[] = []
    if (handle) {
      try {
        await handle.close()
      } catch (error) {
        cleanupFailures.push(error)
      }
    }
    try {
      await this.filesystem.unlink(temporary)
    } catch (error) {
      cleanupFailures.push(error)
    }

    if (appendFailed) {
      if (cleanupFailures.length > 0) {
        throw new AggregateError([appendFailure, ...cleanupFailures], 'replay shadow journal append and cleanup failed')
      }
      throw appendFailure
    }
    if (cleanupFailures.length > 0) {
      throw new AggregateError(cleanupFailures, 'replay shadow journal temporary cleanup failed')
    }
    if (!outcome) {
      throw new Error('replay shadow journal did not produce an append outcome')
    }
    return outcome
  }
}

/** Explicit v2 writer. It has no environment or default relay integration. */
export class NodePlayerSnapshotV2ReplayShadowJournal implements PlayerSnapshotV2ReplayShadowJournal {
  private readonly directory: string | undefined

  constructor(
    directory: string,
    private readonly filesystem: PlayerSnapshotV1ReplayShadowJournalFilesystem = nodeFilesystem,
  ) {
    this.directory = configuredAbsolutePath(directory)
  }

  get isEnabled(): boolean { return this.directory !== undefined }

  async append(entry: PlayerSnapshotV1ReplayJournalV2Entry): Promise<PlayerSnapshotV1ReplayJournalAppend> {
    const directory = this.directory
    if (!directory) throw new Error('invalid replay shadow journal path')
    const bytes = encodedV2Entry(entry)
    const filename = `${createHash('sha256').update(bytes).digest('hex')}.json`
    const target = join(directory, filename)
    const temporary = join(directory, `.${filename}.${process.pid}.${randomUUID()}.tmp`)
    let handle: PlayerSnapshotV1ReplayShadowJournalFile | undefined
    let outcome: PlayerSnapshotV1ReplayJournalAppend | undefined
    let appendFailure: unknown
    let appendFailed = false
    try {
      handle = await this.filesystem.open(temporary, 'wx', 0o600)
      await handle.writeFile(bytes)
      await handle.sync()
      await handle.close()
      handle = undefined
      try { await this.filesystem.link(temporary, target) }
      catch (error) {
        if ((error as NodeJS.ErrnoException).code === 'EEXIST') outcome = 'duplicate'
        else throw error
      }
      if (!outcome) outcome = 'appended'
    } catch (error) {
      appendFailed = true
      appendFailure = error
    }
    const cleanupFailures: unknown[] = []
    if (handle) {
      try { await handle.close() }
      catch (error) { cleanupFailures.push(error) }
    }
    try { await this.filesystem.unlink(temporary) }
    catch (error) { cleanupFailures.push(error) }
    if (appendFailed) {
      if (cleanupFailures.length > 0) throw new AggregateError([appendFailure, ...cleanupFailures], 'replay shadow journal append and cleanup failed')
      throw appendFailure
    }
    if (cleanupFailures.length > 0) throw new AggregateError(cleanupFailures, 'replay shadow journal temporary cleanup failed')
    if (!outcome) throw new Error('replay shadow journal did not produce an append outcome')
    return outcome
  }
}
