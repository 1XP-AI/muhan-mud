import { createHash, timingSafeEqual } from 'node:crypto'
import { constants as fsConstants } from 'node:fs'
import { lstat, open, readdir } from 'node:fs/promises'
import { basename, dirname, isAbsolute, resolve, sep } from 'node:path'

// The C receipt reader has a 640-byte text buffer and refuses an extra byte.
const RECEIPT_MAX_BYTES = 640
const PLAYER_FILE_MAX_BYTES = 64 * 1024 * 1024
const RPC_RESPONSE_MAX_BYTES = 16 * 1024
const RPC_PATH = '/rpc/reconcile_game_character_provisioning'
const EVIDENCE_FINALIZER_RPC_PATH = '/rpc/finalize_game_character_legacy_identity_evidence'
const HANDOFF_ACTIVATION_RPC_PATH = '/rpc/activate_game_character_onboarding_handoff'
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const SHA256_RE = /^[0-9a-f]{64}$/
const HEX_RE = /^[0-9a-f]+$/
const RECEIPT_FILE_RE = /^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.receipt$/
const JSON_CONTENT_TYPE_RE = /^application\/json(?:\s*;|$)/i
const FIELDS = [
  'version', 'state', 'actor_uuid', 'correlation_uuid', 'character_uuid',
  'canonical_name_hex', 'storage_format', 'saved_file_sha256',
] as const

type ReceiptState = 'pending' | 'saved' | 'committed'
type EntryKind = 'file' | 'directory' | 'symlink' | 'other'

export interface DirectoryEntry {
  name: string
  kind: EntryKind
}

export interface RegularFileFingerprint {
  sha256: string
  byteLength: number
  device: number
  inode: number
  modifiedAtMs: number
  changedAtMs: number
}

export interface RegularFileContents extends RegularFileFingerprint {
  bytes: Uint8Array
}

/** A narrow boundary: production verifies no-follow regular files, tests can replace it. */
export interface ReconcilerFilesystem {
  assertSafeDirectory(path: string): Promise<void>
  listReceiptEntries(path: string): Promise<ReadonlyArray<DirectoryEntry>>
  readSmallRegularFile(path: string, maxBytes: number): Promise<RegularFileContents>
  sha256RegularFile(path: string, maxBytes: number): Promise<RegularFileFingerprint>
}

export interface Clock {
  now(): number
  sleep(milliseconds: number): Promise<void>
}

export interface ReconcilerOptions {
  muhanHome: string
  postgrestUrl: string
  serviceRoleKey: string
  fetchImpl?: typeof fetch
  fs?: ReconcilerFilesystem
  clock?: Clock
  rpcAttempts?: number
  retryDelayMs?: number
  rpcRequestTimeoutMs?: number
  maxReceiptBytes?: number
  maxPlayerFileBytes?: number
  /** Disabled by default: a saved receipt otherwise keeps reconcile-only behavior. */
  recoverSavedReceiptHandoffs?: boolean
}

export type ObservationOutcome = 'pending' | 'reconciled' | 'committed' | 'rejected' | 'retry_exhausted'
export type ObservationReason =
  | 'unsafe_receipt_directory'
  | 'invalid_receipt_filename'
  | 'unsafe_receipt_file'
  | 'malformed_receipt'
  | 'receipt_filename_mismatch'
  | 'unsupported_storage_format'
  | 'invalid_canonical_name'
  | 'unsafe_player_path'
  | 'player_hash_mismatch'
  | 'rpc_failure'
  | 'rpc_response_mismatch'
  | 'rpc_retry_exhausted'

/** Deliberately contains no path, receipt content, hash, or credentials. */
export interface Observation {
  outcome: ObservationOutcome
  reason?: ObservationReason
  attempts?: number
}

export interface RunSummary {
  observations: Observation[]
}

export type OutcomeCounts = Record<ObservationOutcome, number>

interface Receipt {
  state: ReceiptState
  actorUuid: string
  correlationUuid: string
  characterUuid: string
  canonicalNameHex: string
  storageFormat: string
  savedFileSha256: string
}

interface EntryResult {
  observation: Observation
  successfulFingerprint?: string
}

type HandoffMode = 'provision' | 'claim'

interface EvidenceFinalizerResult {
  mode: HandoffMode
  lifecycle: 'handoff_pending' | 'active'
}

class UnsafeFilesystemError extends Error {}
class InvalidReceiptError extends Error {}
class InvalidCanonicalNameError extends Error {}
class InvalidRpcResponseError extends Error {}

const systemClock: Clock = {
  now: Date.now,
  sleep: async (milliseconds) => new Promise((done) => setTimeout(done, milliseconds)),
}

/** Node implementation used by the CLI. Every opened receipt/player file is no-follow and regular. */
export class NodeReconcilerFilesystem implements ReconcilerFilesystem {
  async assertSafeDirectory(path: string): Promise<void> {
    let directory
    try {
      directory = await this.openSafeDirectory(path)
    } catch {
      throw new UnsafeFilesystemError()
    } finally {
      await directory?.close().catch(() => undefined)
    }
  }

  async listReceiptEntries(path: string): Promise<ReadonlyArray<DirectoryEntry>> {
    let directory
    let entries
    try {
      directory = await this.openSafeDirectory(path)
      // `directory` stays open while readdir runs, so the listing is rooted at the
      // verified directory descriptor rather than a pathname an attacker can replace.
      if (process.platform === 'linux') {
        entries = await readdir(this.descriptorPath(directory), { withFileTypes: true, encoding: 'buffer' })
      } else {
        const before = await directory.stat()
        entries = await readdir(path, { withFileTypes: true, encoding: 'buffer' })
        const after = await directory.stat()
        const current = (await this.inspectDirectoryPath(path)).at(-1)
        if (!current || !this.sameIdentity(before, after) || !this.sameIdentity(before, current)) throw new UnsafeFilesystemError()
      }
    } catch {
      throw new UnsafeFilesystemError()
    } finally {
      await directory?.close().catch(() => undefined)
    }
    return entries.map((entry) => ({
      // A valid candidate filename is ASCII; malformed byte names can never pass the allowlist.
      name: Buffer.from(entry.name).toString('utf8'),
      kind: entry.isSymbolicLink() ? 'symlink' : entry.isFile() ? 'file' : entry.isDirectory() ? 'directory' : 'other',
    }))
  }

  async readSmallRegularFile(path: string, maxBytes: number): Promise<RegularFileContents> {
    return this.withRegularFile(path, maxBytes, async (handle, size, fingerprint) => {
      const result = Buffer.allocUnsafe(size)
      let offset = 0
      while (offset < result.length) {
        const { bytesRead } = await handle.read(result, offset, result.length - offset, offset)
        if (bytesRead === 0) throw new UnsafeFilesystemError()
        offset += bytesRead
      }
      const extra = Buffer.allocUnsafe(1)
      if ((await handle.read(extra, 0, 1, offset)).bytesRead !== 0) throw new UnsafeFilesystemError()
      return {
        ...fingerprint,
        sha256: createHash('sha256').update(result).digest('hex'),
        byteLength: result.byteLength,
        bytes: result,
      }
    })
  }

  async sha256RegularFile(path: string, maxBytes: number): Promise<RegularFileFingerprint> {
    return this.withRegularFile(path, maxBytes, async (handle, expectedSize, fingerprint) => {
      const digest = createHash('sha256')
      const chunk = Buffer.allocUnsafe(64 * 1024)
      let offset = 0
      for (;;) {
        const { bytesRead } = await handle.read(chunk, 0, chunk.length, offset)
        if (bytesRead === 0) break
        offset += bytesRead
        if (offset > maxBytes) throw new UnsafeFilesystemError()
        digest.update(chunk.subarray(0, bytesRead))
      }
      if (offset !== expectedSize) throw new UnsafeFilesystemError()
      return { ...fingerprint, sha256: digest.digest('hex'), byteLength: offset }
    })
  }

  private descriptorPath(directory: Awaited<ReturnType<typeof open>>, name = ''): string {
    // This helper is called only on Linux, where /proc/self/fd keeps the
    // verified directory descriptor as the lookup root.
    return `/proc/self/fd/${directory.fd}${name ? `/${name}` : ''}`
  }

  private async openSafeDirectory(path: string): Promise<Awaited<ReturnType<typeof open>>> {
    if (!isAbsolute(path) || path.includes('\0') || !fsConstants.O_NOFOLLOW || !fsConstants.O_DIRECTORY) throw new UnsafeFilesystemError()
    if (process.platform !== 'linux') return this.openSafeDirectoryByPath(path)
    const components = resolve(path).split(sep).filter(Boolean)
    let directory: Awaited<ReturnType<typeof open>> | undefined
    try {
      directory = await open(sep, fsConstants.O_RDONLY | fsConstants.O_DIRECTORY | fsConstants.O_NOFOLLOW)
      for (const component of components) {
        const next = await open(this.descriptorPath(directory, component), fsConstants.O_RDONLY | fsConstants.O_DIRECTORY | fsConstants.O_NOFOLLOW)
        await directory.close()
        directory = next
      }
      const status = await directory.stat()
      if (!status.isDirectory() || (status.mode & 0o077) !== 0) throw new UnsafeFilesystemError()
      return directory
    } catch (error) {
      await directory?.close().catch(() => undefined)
      if (error instanceof UnsafeFilesystemError) throw error
      throw new UnsafeFilesystemError()
    }
  }

  private async openSafeDirectoryByPath(path: string): Promise<Awaited<ReturnType<typeof open>>> {
    let directory: Awaited<ReturnType<typeof open>> | undefined
    try {
      const before = await this.inspectDirectoryPath(path)
      directory = await open(path, fsConstants.O_RDONLY | fsConstants.O_DIRECTORY | fsConstants.O_NOFOLLOW)
      const opened = await directory.stat()
      const after = await this.inspectDirectoryPath(path)
      const expected = before.at(-1)
      const actual = after.at(-1)
      if (!expected || !actual || !this.sameIdentity(expected, actual) || !this.sameIdentity(expected, opened)) throw new UnsafeFilesystemError()
      return directory
    } catch (error) {
      await directory?.close().catch(() => undefined)
      if (error instanceof UnsafeFilesystemError) throw error
      throw new UnsafeFilesystemError()
    }
  }

  private async inspectDirectoryPath(path: string): Promise<Array<{ dev: number, ino: number }>> {
    const components = resolve(path).split(sep).filter(Boolean)
    const identities: Array<{ dev: number, ino: number }> = []
    let current: string = sep
    for (const component of components) {
      current = resolve(current, component)
      const status = await lstat(current)
      if (status.isSymbolicLink() || !status.isDirectory()) throw new UnsafeFilesystemError()
      identities.push({ dev: status.dev, ino: status.ino })
    }
    const final = await lstat(path)
    if ((final.mode & 0o077) !== 0) throw new UnsafeFilesystemError()
    return identities
  }

  private sameIdentity(left: { dev: number, ino: number }, right: { dev: number, ino: number }): boolean {
    return left.dev === right.dev && left.ino === right.ino
  }

  private async withRegularFile<T>(path: string, maxBytes: number, operation: (handle: Awaited<ReturnType<typeof open>>, size: number, fingerprint: Omit<RegularFileFingerprint, 'sha256' | 'byteLength'>) => Promise<T>): Promise<T> {
    let parent: Awaited<ReturnType<typeof open>> | undefined
    let handle: Awaited<ReturnType<typeof open>> | undefined
    try {
      parent = await this.openSafeDirectory(dirname(path))
      const name = basename(path)
      if (!name || name === '.' || name === '..') throw new UnsafeFilesystemError()
      // Every parent was opened with O_NOFOLLOW, then this final component is
      // opened relative to the retained parent descriptor with O_NOFOLLOW.
      // fstat before and after the read binds the bytes to one regular inode.
      const parentBefore = await parent.stat()
      handle = await open(
        process.platform === 'linux' ? this.descriptorPath(parent, name) : path,
        fsConstants.O_RDONLY | fsConstants.O_NONBLOCK | fsConstants.O_NOFOLLOW,
      )
      if (process.platform !== 'linux') {
        const currentParent = (await this.inspectDirectoryPath(dirname(path))).at(-1)
        if (!currentParent || !this.sameIdentity(parentBefore, currentParent)) throw new UnsafeFilesystemError()
      }
      const before = await handle.stat()
      if (!before.isFile() || (before.mode & 0o077) !== 0 || !Number.isSafeInteger(before.size) || before.size < 0 || before.size > maxBytes
        || !Number.isSafeInteger(before.dev) || before.dev < 0 || !Number.isSafeInteger(before.ino) || before.ino < 0
        || !Number.isFinite(before.mtimeMs) || !Number.isFinite(before.ctimeMs)) throw new UnsafeFilesystemError()
      const result = await operation(handle, before.size, {
        device: before.dev,
        inode: before.ino,
        modifiedAtMs: before.mtimeMs,
        changedAtMs: before.ctimeMs,
      })
      const after = await handle.stat()
      if (!after.isFile() || (after.mode & 0o077) !== 0 || after.dev !== before.dev || after.ino !== before.ino || after.size !== before.size || after.mtimeMs !== before.mtimeMs || after.ctimeMs !== before.ctimeMs) throw new UnsafeFilesystemError()
      return result
    } catch (error) {
      if (error instanceof UnsafeFilesystemError) throw error
      throw new UnsafeFilesystemError()
    } finally {
      if (handle) await handle.close().catch(() => undefined)
      if (parent) await parent.close().catch(() => undefined)
    }
  }
}

function requireBoundedInteger(value: number | undefined, fallback: number, minimum: number, maximum: number): number {
  const candidate = value ?? fallback
  if (!Number.isInteger(candidate) || candidate < minimum || candidate > maximum) throw new Error('reconciler configuration rejected')
  return candidate
}

function normalizeHome(value: string): string {
  if (!isAbsolute(value) || value.includes('\0')) throw new Error('reconciler configuration rejected')
  return resolve(value)
}

function normalizePostgrestUrl(value: string): string {
  let url: URL
  try {
    url = new URL(value)
  } catch {
    throw new Error('reconciler configuration rejected')
  }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash || url.pathname !== '/') {
    throw new Error('reconciler configuration rejected')
  }
  return url.origin
}

function childPath(base: string, ...components: string[]): string {
  const result = resolve(base, ...components)
  if (!result.startsWith(`${base}${sep}`)) throw new UnsafeFilesystemError()
  return result
}

function parseReceipt(bytes: Uint8Array): Receipt {
  let text: string
  try {
    text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
  } catch {
    throw new InvalidReceiptError()
  }
  const lines = text.split('\n')
  if (lines.length !== FIELDS.length + 1 || lines.at(-1) !== '') throw new InvalidReceiptError()
  const values = new Map<string, string>()
  for (const [index, field] of FIELDS.entries()) {
    const line = lines[index]!
    const prefix = `${field}=`
    if (!line.startsWith(prefix) || values.has(field)) throw new InvalidReceiptError()
    values.set(field, line.slice(prefix.length))
  }
  const version = values.get('version')!
  const state = values.get('state')!
  const actorUuid = values.get('actor_uuid')!
  const correlationUuid = values.get('correlation_uuid')!
  const characterUuid = values.get('character_uuid')!
  const canonicalNameHex = values.get('canonical_name_hex')!
  const storageFormat = values.get('storage_format')!
  const savedFileSha256 = values.get('saved_file_sha256')!
  if (version !== '1' || !['pending', 'saved', 'committed'].includes(state) || !UUID_RE.test(actorUuid) || !UUID_RE.test(correlationUuid) || !UUID_RE.test(characterUuid)) throw new InvalidReceiptError()
  // C's onboarding receipt v1 limits the UTF-8 player name to 14 bytes, encoded as lower hex.
  if (canonicalNameHex.length < 2 || canonicalNameHex.length > 28 || canonicalNameHex.length % 2 !== 0 || !HEX_RE.test(canonicalNameHex)) throw new InvalidReceiptError()
  if (!/^[a-z][a-z0-9_.-]{0,31}$/.test(storageFormat)) throw new InvalidReceiptError()
  if (state === 'pending' ? savedFileSha256 !== '' : !SHA256_RE.test(savedFileSha256)) throw new InvalidReceiptError()
  return { state: state as ReceiptState, actorUuid, correlationUuid, characterUuid, canonicalNameHex, storageFormat, savedFileSha256 }
}

function decodeCanonicalName(hex: string): { name: string, bytes: Buffer } {
  const bytes = Buffer.from(hex, 'hex')
  let name: string
  try {
    name = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
  } catch {
    throw new InvalidCanonicalNameError()
  }
  const codepoints = Array.from(name).length
  if (bytes.length < 1 || bytes.length > 14 || codepoints < 1 || codepoints > 12 || name === '.' || name === '..' || /[\x00-\x1f\x7f/\\:]/.test(name)) throw new InvalidCanonicalNameError()
  // Mirror the C admission boundary exactly: ASCII letters are lowercase
  // except for an ASCII first character, which is uppercase. Non-ASCII UTF-8
  // remains byte-for-byte canonical because the C code does not case-fold it.
  const canonical = Buffer.from(bytes)
  for (let index = 0; index < canonical.length; index++) {
    if (canonical[index]! >= 0x41 && canonical[index]! <= 0x5a) canonical[index] += 0x20
  }
  if (canonical[0]! >= 0x61 && canonical[0]! <= 0x7a) canonical[0] -= 0x20
  if (!canonical.equals(bytes)) throw new InvalidCanonicalNameError()
  return { name, bytes }
}

function sameHash(left: string, right: string): boolean {
  if (!SHA256_RE.test(left) || !SHA256_RE.test(right)) return false
  return timingSafeEqual(Buffer.from(left, 'ascii'), Buffer.from(right, 'ascii'))
}

function isTransientStatus(status: number): boolean {
  return status === 408 || status === 429 || status >= 500
}

function oneExactObject(value: unknown, keys: readonly string[]): Record<string, unknown> | undefined {
  if (!Array.isArray(value) || value.length !== 1 || !value[0] || typeof value[0] !== 'object' || Array.isArray(value[0]) || Object.getPrototypeOf(value[0]) !== Object.prototype) return undefined
  const row = value[0] as Record<string, unknown>
  const actualKeys = Reflect.ownKeys(row)
  if (actualKeys.length !== keys.length || actualKeys.some((key) => typeof key !== 'string' || !keys.includes(key)) ||
      Object.values(Object.getOwnPropertyDescriptors(row)).some((descriptor) => !('value' in descriptor))) return undefined
  return row
}

/**
 * The legacy reconcile RPC finalizes persisted provisioning evidence but does
 * not admit the character. Its successful result is therefore handoff_pending.
 */
function matchesPostSaveReconciliationResponse(value: unknown, receipt: Receipt): boolean {
  const requiredColumns = ['actor_user_id', 'character_id', 'lifecycle', 'saved_file_sha256', 'status', 'storage_format']
  const row = oneExactObject(value, requiredColumns)
  if (!row) return false
  return row.character_id === receipt.characterUuid
    && row.actor_user_id === receipt.actorUuid
    && row.lifecycle === 'handoff_pending'
    && row.status === 'finalized'
    && row.saved_file_sha256 === receipt.savedFileSha256
    && row.storage_format === 1
}

function matchesEvidenceFinalizerResponse(value: unknown, receipt: Receipt, name: string, shard: string): EvidenceFinalizerResult | undefined {
  const requiredColumns = [
    'character_id', 'actor_user_id', 'mode', 'lifecycle', 'world_id', 'canonical_legacy_name', 'legacy_shard',
    'player_file_sha256', 'evidence_version', 'storage_format', 'recorded_at',
  ]
  const row = oneExactObject(value, requiredColumns)
  if (!row || row.character_id !== receipt.characterUuid || row.actor_user_id !== receipt.actorUuid ||
      (row.mode !== 'provision' && row.mode !== 'claim') || (row.lifecycle !== 'handoff_pending' && row.lifecycle !== 'active') ||
      typeof row.world_id !== 'string' || row.world_id.length < 1 || row.world_id.length > 64 || /[\x00-\x1f\x7f]/.test(row.world_id) ||
      row.canonical_legacy_name !== name || row.legacy_shard !== shard || row.player_file_sha256 !== receipt.savedFileSha256 ||
      row.evidence_version !== 1 || row.storage_format !== 'player-v1' || typeof row.recorded_at !== 'string' || !Number.isFinite(Date.parse(row.recorded_at))) return undefined
  return { mode: row.mode, lifecycle: row.lifecycle }
}

/** The separate activation RPC is the only path whose successful result is playable. */
function matchesActiveHandoffActivationResponse(value: unknown, receipt: Receipt): boolean {
  const requiredColumns = ['character_id', 'actor_user_id', 'correlation_id', 'lifecycle', 'onboarding_status']
  const row = oneExactObject(value, requiredColumns)
  return !!row && row.character_id === receipt.characterUuid && row.actor_user_id === receipt.actorUuid &&
    row.correlation_id === receipt.correlationUuid && row.lifecycle === 'active' && row.onboarding_status === 'finalized'
}

async function parseRpcResponse(response: Response): Promise<unknown> {
  if (!JSON_CONTENT_TYPE_RE.test(response.headers.get('content-type') ?? '') || !response.body) throw new InvalidRpcResponseError()
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let length = 0
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      if (!value) continue
      length += value.byteLength
      if (length > RPC_RESPONSE_MAX_BYTES) throw new InvalidRpcResponseError()
      chunks.push(value)
    }
  } finally {
    reader.releaseLock()
  }
  const bytes = Buffer.concat(chunks.map((chunk) => Buffer.from(chunk)), length)
  let text: string
  try {
    text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
    return JSON.parse(text)
  } catch {
    throw new InvalidRpcResponseError()
  }
}

export class OnboardingReconciler {
  private readonly home: string
  private readonly postgrestUrl: string
  private readonly serviceRoleKey: string
  private readonly fetchImpl: typeof fetch
  private readonly fs: ReconcilerFilesystem
  private readonly clock: Clock
  private readonly rpcAttempts: number
  private readonly retryDelayMs: number
  private readonly rpcRequestTimeoutMs: number
  private readonly maxReceiptBytes: number
  private readonly maxPlayerFileBytes: number
  private readonly recoverSavedReceiptHandoffs: boolean
  // Kept only for the receipt files observed in the immediately preceding
  // successful scan. It suppresses duplicate idempotent RPCs, never validation.
  private successfulSavedReceipts = new Map<string, string>()

  constructor(options: ReconcilerOptions) {
    this.home = normalizeHome(options.muhanHome)
    this.postgrestUrl = normalizePostgrestUrl(options.postgrestUrl)
    if (!options.serviceRoleKey || /[\r\n]/.test(options.serviceRoleKey)) throw new Error('reconciler configuration rejected')
    this.serviceRoleKey = options.serviceRoleKey
    this.fetchImpl = options.fetchImpl ?? fetch
    this.fs = options.fs ?? new NodeReconcilerFilesystem()
    this.clock = options.clock ?? systemClock
    this.rpcAttempts = requireBoundedInteger(options.rpcAttempts, 3, 1, 10)
    this.retryDelayMs = requireBoundedInteger(options.retryDelayMs, 250, 0, 60_000)
    this.rpcRequestTimeoutMs = requireBoundedInteger(options.rpcRequestTimeoutMs, 10_000, 1, 60_000)
    this.maxReceiptBytes = requireBoundedInteger(options.maxReceiptBytes, RECEIPT_MAX_BYTES, 1, RECEIPT_MAX_BYTES)
    this.maxPlayerFileBytes = requireBoundedInteger(options.maxPlayerFileBytes, PLAYER_FILE_MAX_BYTES, 1, PLAYER_FILE_MAX_BYTES)
    if (options.recoverSavedReceiptHandoffs !== undefined && typeof options.recoverSavedReceiptHandoffs !== 'boolean') throw new Error('reconciler configuration rejected')
    this.recoverSavedReceiptHandoffs = options.recoverSavedReceiptHandoffs ?? false
  }

  async runOnce(): Promise<RunSummary> {
    const receiptDirectory = childPath(this.home, 'onboarding-receipts')
    let entries: ReadonlyArray<DirectoryEntry>
    try {
      await this.fs.assertSafeDirectory(this.home)
      entries = await this.fs.listReceiptEntries(receiptDirectory)
    } catch {
      this.successfulSavedReceipts.clear()
      return { observations: [{ outcome: 'rejected', reason: 'unsafe_receipt_directory' }] }
    }
    const observations: Observation[] = []
    const successfulSavedReceipts = new Map<string, string>()
    for (const entry of [...entries].filter((item) => item.name.endsWith('.receipt')).sort((left, right) => left.name.localeCompare(right.name))) {
      const result = await this.reconcileEntry(receiptDirectory, entry, this.successfulSavedReceipts.get(entry.name))
      observations.push(result.observation)
      if (result.successfulFingerprint) successfulSavedReceipts.set(entry.name, result.successfulFingerprint)
    }
    // Receipt deletions, malformed replacements, and failures evict their cache
    // entries. Memory is therefore bounded by valid saved receipts in one scan.
    this.successfulSavedReceipts = successfulSavedReceipts
    return { observations }
  }

  private async reconcileEntry(receiptDirectory: string, entry: DirectoryEntry, cachedFingerprint: string | undefined): Promise<EntryResult> {
    const match = RECEIPT_FILE_RE.exec(entry.name)
    if (!match) return { observation: { outcome: 'rejected', reason: 'invalid_receipt_filename' } }
    if (entry.kind !== 'file') return { observation: { outcome: 'rejected', reason: 'unsafe_receipt_file' } }
    let receipt: Receipt
    let receiptFile: RegularFileContents
    try {
      receiptFile = await this.fs.readSmallRegularFile(childPath(receiptDirectory, entry.name), this.maxReceiptBytes)
      receipt = parseReceipt(receiptFile.bytes)
    } catch (error) {
      return { observation: { outcome: 'rejected', reason: error instanceof InvalidReceiptError ? 'malformed_receipt' : 'unsafe_receipt_file' } }
    }
    if (receipt.correlationUuid !== match[1]) return { observation: { outcome: 'rejected', reason: 'receipt_filename_mismatch' } }
    if (receipt.state === 'pending') return { observation: { outcome: 'pending' } }
    if (receipt.storageFormat !== 'player-v1') return { observation: { outcome: 'rejected', reason: 'unsupported_storage_format' } }

    let name: string
    let shard: string
    let playerPath: string
    try {
      const decoded = decodeCanonicalName(receipt.canonicalNameHex)
      name = decoded.name
      shard = createHash('sha1').update(decoded.bytes).digest('hex').slice(0, 2)
      const playerRoot = childPath(this.home, 'player')
      const shardDirectory = childPath(playerRoot, shard)
      await this.fs.assertSafeDirectory(playerRoot)
      await this.fs.assertSafeDirectory(shardDirectory)
      playerPath = childPath(shardDirectory, name)
    } catch (error) {
      return { observation: { outcome: 'rejected', reason: error instanceof InvalidCanonicalNameError ? 'invalid_canonical_name' : 'unsafe_player_path' } }
    }

    let playerFile: RegularFileFingerprint
    try {
      playerFile = await this.fs.sha256RegularFile(playerPath, this.maxPlayerFileBytes)
    } catch {
      return { observation: { outcome: 'rejected', reason: 'unsafe_player_path' } }
    }
    if (!sameHash(playerFile.sha256, receipt.savedFileSha256)) return { observation: { outcome: 'rejected', reason: 'player_hash_mismatch' } }
    // A committed receipt is durable proof that C has already sent its COMMIT
    // after DB finalization. Validate every byte/path/hash, then deliberately
    // avoid a service-role mutation request.
    if (receipt.state === 'committed') return { observation: { outcome: 'committed' } }

    const fingerprint = [
      receiptFile.device, receiptFile.inode, receiptFile.byteLength, receiptFile.modifiedAtMs, receiptFile.changedAtMs, receiptFile.sha256,
      playerFile.device, playerFile.inode, playerFile.byteLength, playerFile.modifiedAtMs, playerFile.changedAtMs, playerFile.sha256,
    ].join(':')
    if (cachedFingerprint === fingerprint) return { observation: { outcome: 'reconciled' }, successfulFingerprint: fingerprint }
    const observation = this.recoverSavedReceiptHandoffs
      ? await this.recoverSavedReceiptHandoffRpc(receipt, name, shard)
      : await this.reconcileRpc(receipt)
    return observation.outcome === 'reconciled'
      ? { observation, successfulFingerprint: fingerprint }
      : { observation }
  }

  private async reconcileRpc(receipt: Receipt): Promise<Observation> {
    const request = {
      p_actor_user_id: receipt.actorUuid,
      p_correlation_id: receipt.correlationUuid,
      p_saved_file_sha256: receipt.savedFileSha256,
      p_storage_format: 1,
    }
    for (let attempt = 1; attempt <= this.rpcAttempts; attempt++) {
      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), this.rpcRequestTimeoutMs)
      try {
        const response = await this.fetchImpl(new URL(RPC_PATH, this.postgrestUrl), {
          method: 'POST',
          headers: {
            authorization: `Bearer ${this.serviceRoleKey}`,
            apikey: this.serviceRoleKey,
            accept: 'application/json',
            'content-type': 'application/json',
          },
          body: JSON.stringify(request),
          // A redirect can forward bearer credentials to a different origin.
          // Treat it as a retryable transport failure instead of following it.
          redirect: 'error',
          signal: controller.signal,
        })
        if (!response.ok) {
          if (isTransientStatus(response.status)) {
            if (attempt < this.rpcAttempts) {
              await this.clock.sleep(this.retryDelayMs)
              continue
            }
            return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: attempt }
          }
          return { outcome: 'rejected', reason: 'rpc_failure', attempts: attempt }
        }
        const body = await parseRpcResponse(response)
        if (!matchesPostSaveReconciliationResponse(body, receipt)) return { outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: attempt }
        return { outcome: 'reconciled', attempts: attempt }
      } catch (error) {
        if (error instanceof InvalidRpcResponseError) return { outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: attempt }
        if (attempt < this.rpcAttempts) {
          await this.clock.sleep(this.retryDelayMs)
          continue
        }
        return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: attempt }
      } finally {
        clearTimeout(timeout)
      }
    }
    return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: this.rpcAttempts }
  }

  /**
   * Opt-in recovery mirrors the post-save gateway boundary exactly: evidence
   * finalization is one atomic DB transition, followed only by the activation
   * tuple whose mode was returned by that finalizer. A retry repeats the
   * complete ordered pair, never a legacy reconcile RPC and never a guessed mode.
   */
  private async recoverSavedReceiptHandoffRpc(receipt: Receipt, name: string, shard: string): Promise<Observation> {
    const evidenceRequest = {
      p_actor_user_id: receipt.actorUuid,
      p_correlation_id: receipt.correlationUuid,
      p_character_id: receipt.characterUuid,
      p_outcome: 'ok',
      p_canonical_legacy_name: name,
      p_player_file_sha256: receipt.savedFileSha256,
      p_evidence_version: 1,
      p_storage_format: 'player-v1',
      p_legacy_shard: shard,
    }
    for (let attempt = 1; attempt <= this.rpcAttempts; attempt++) {
      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), this.rpcRequestTimeoutMs)
      try {
        const evidenceResponse = await this.fetchImpl(new URL(EVIDENCE_FINALIZER_RPC_PATH, this.postgrestUrl), {
          method: 'POST', headers: this.rpcHeaders(), body: JSON.stringify(evidenceRequest), redirect: 'error', signal: controller.signal,
        })
        if (!evidenceResponse.ok) {
          if (isTransientStatus(evidenceResponse.status)) {
            if (attempt < this.rpcAttempts) { await this.clock.sleep(this.retryDelayMs); continue }
            return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: attempt }
          }
          return { outcome: 'rejected', reason: 'rpc_failure', attempts: attempt }
        }
        const finalized = matchesEvidenceFinalizerResponse(await parseRpcResponse(evidenceResponse), receipt, name, shard)
        if (!finalized) return { outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: attempt }

        const activationResponse = await this.fetchImpl(new URL(HANDOFF_ACTIVATION_RPC_PATH, this.postgrestUrl), {
          method: 'POST', headers: this.rpcHeaders(),
          body: JSON.stringify({
            p_actor_user_id: receipt.actorUuid,
            p_correlation_id: receipt.correlationUuid,
            p_character_id: receipt.characterUuid,
            p_mode: finalized.mode,
          }),
          redirect: 'error', signal: controller.signal,
        })
        if (!activationResponse.ok) {
          if (isTransientStatus(activationResponse.status)) {
            if (attempt < this.rpcAttempts) { await this.clock.sleep(this.retryDelayMs); continue }
            return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: attempt }
          }
          return { outcome: 'rejected', reason: 'rpc_failure', attempts: attempt }
        }
        if (!matchesActiveHandoffActivationResponse(await parseRpcResponse(activationResponse), receipt)) return { outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: attempt }
        return { outcome: 'reconciled', attempts: attempt }
      } catch (error) {
        if (error instanceof InvalidRpcResponseError) return { outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: attempt }
        if (attempt < this.rpcAttempts) { await this.clock.sleep(this.retryDelayMs); continue }
        return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: attempt }
      } finally {
        clearTimeout(timeout)
      }
    }
    return { outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: this.rpcAttempts }
  }

  private rpcHeaders(): Record<string, string> {
    return {
      authorization: `Bearer ${this.serviceRoleKey}`,
      apikey: this.serviceRoleKey,
      accept: 'application/json',
      'content-type': 'application/json',
    }
  }
}

export interface PollOptions {
  runs: number
  intervalMs: number
}

/** Executes a finite number of scans. It is intentionally not an unbounded daemon loop. */
export function emptyOutcomeCounts(): OutcomeCounts {
  return { pending: 0, reconciled: 0, committed: 0, rejected: 0, retry_exhausted: 0 }
}

/**
 * Executes a finite number of scans while retaining only aggregate counters.
 * A run's per-receipt observations become collectible before the next run.
 */
export async function runPolling(service: Pick<OnboardingReconciler, 'runOnce'>, clock: Clock, options: PollOptions): Promise<OutcomeCounts> {
  const runs = requireBoundedInteger(options.runs, 1, 1, 10_000)
  const intervalMs = requireBoundedInteger(options.intervalMs, 0, 0, 60_000)
  const totals = emptyOutcomeCounts()
  for (let run = 0; run < runs; run++) {
    const summary = await service.runOnce()
    for (const observation of summary.observations) totals[observation.outcome]++
    if (run + 1 < runs) await clock.sleep(intervalMs)
  }
  return totals
}
