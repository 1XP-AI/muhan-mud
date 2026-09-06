import { lstat, open, readdir } from 'node:fs/promises'
import { isAbsolute, resolve } from 'node:path'
import {
  MAX_MANIFEST_BYTES, MAX_OUTBOX_ENTRIES, commandFromFilename, isManifestFilename,
  noFollowDirectoryFlags, noFollowFileFlags, parseManifest, type Manifest,
} from './manifest.js'
import { classifyDatabaseError, type ManifestStore } from './store.js'

export interface RelaySummary {
  visited: number
  valid: number
  delivered: number
  recorded: number
  exactRetry: number
  invalid: number
  conflict: number
  retryable: number
  unknown: number
  ioError: number
}

export interface ManifestFilesystem {
  scan(path: string): Promise<ReadonlyArray<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }>>
}

/** One filename class and its independent immutable-evidence size bound. */
export interface ImmutableOutboxFilePolicy {
  isCandidateFilename(name: Uint8Array): boolean
  maximumBytes: number
}

/** Test-only synchronization points for deterministic immutable-outbox race tests. */
export interface ImmutableOutboxScanHooks {
  beforeRootOpen?: () => void | Promise<void>
  afterFileRead?: (name: string) => void | Promise<void>
}

class UnsafeOutboxError extends Error {}

function summary(): RelaySummary {
  return { visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0 }
}

interface BigIntStat {
  dev: bigint
  ino: bigint
  size: bigint
  mtimeNs: bigint
  ctimeNs: bigint
  mode: bigint
  uid: bigint
  nlink: bigint
  isDirectory(): boolean
  isFile(): boolean
  isSymbolicLink(): boolean
}

function identity(stat: BigIntStat): string {
  // Preserve both the content-bearing identity fields and every metadata
  // invariant enforced by assertFile. ctime alone is not a substitute: the
  // post-read check must explicitly fail if a platform reports changed mode,
  // owner, or link count without a distinguishable timestamp change.
  return `${stat.dev}:${stat.ino}:${stat.size}:${stat.mtimeNs}:${stat.ctimeNs}:${stat.mode}:${stat.uid}:${stat.nlink}`
}

function descriptorPath(fd: number): string {
  return `/proc/self/fd/${fd}`
}

function currentUid(): number | undefined {
  return typeof process.getuid === 'function' ? process.getuid() : undefined
}

function assertDirectory(stat: BigIntStat): void {
  const uid = currentUid()
  if (stat.isSymbolicLink() || !stat.isDirectory() || (stat.mode & 0o777n) !== 0o700n || (uid !== undefined && stat.uid !== BigInt(uid))) throw new UnsafeOutboxError()
}

function assertFile(stat: BigIntStat, uid: bigint, maximumBytes: number): void {
  if (stat.isSymbolicLink() || !stat.isFile() || stat.nlink !== 1n || stat.uid !== uid
    || (stat.mode & 0o777n) !== 0o600n || stat.size < 0n || stat.size >= BigInt(maximumBytes)) throw new UnsafeOutboxError()
}

async function readStableFile(path: string, owner: bigint, maximumBytes: number): Promise<Uint8Array> {
  const before = await lstat(path, { bigint: true })
  assertFile(before, owner, maximumBytes)
  const file = await open(path, noFollowFileFlags())
  try {
    const opened = await file.stat({ bigint: true })
    assertFile(opened, owner, maximumBytes)
    if (identity(before) !== identity(opened)) throw new UnsafeOutboxError()
    const bytes = Buffer.allocUnsafe(Number(opened.size))
    let offset = 0
    while (offset < bytes.length) {
      const read = await file.read(bytes, offset, bytes.length - offset, null)
      if (read.bytesRead === 0) throw new UnsafeOutboxError()
      offset += read.bytesRead
    }
    const extra = Buffer.alloc(1)
    const readExtra = await file.read(extra, 0, 1, null)
    const after = await file.stat({ bigint: true })
    if (readExtra.bytesRead !== 0 || identity(opened) !== identity(after)) throw new UnsafeOutboxError()
    return bytes
  } finally { await file.close() }
}

/** Node filesystem implementation: descriptor-rooted on Linux and fail-closed elsewhere. */
export class NodeManifestFilesystem implements ManifestFilesystem {
  constructor(private readonly platform: NodeJS.Platform = process.platform) {}

  async scan(path: string): Promise<ReadonlyArray<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }>> {
    return scanImmutableOutboxFiles(path, isManifestFilename, MAX_MANIFEST_BYTES, this.platform)
  }
}

/** Shared no-follow scanner for immutable evidence files; parsers remain format-specific. */
export async function scanImmutableOutboxFiles(
  path: string,
  isCandidateFilename: (name: Uint8Array) => boolean,
  maximumBytes: number,
  platform: NodeJS.Platform = process.platform,
  hooks?: ImmutableOutboxScanHooks,
): Promise<ReadonlyArray<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }>> {
  return scanImmutableOutboxFilesWithPolicies(path, [{ isCandidateFilename, maximumBytes }], platform, hooks)
}

/**
 * Scan several evidence classes from one descriptor-pinned immutable root.
 * A caller pairing different classes must use this instead of independently
 * opening the mutable root pathname for each class.
 */
export async function scanImmutableOutboxFilesWithPolicies(
  path: string,
  policies: readonly ImmutableOutboxFilePolicy[],
  platform: NodeJS.Platform = process.platform,
  hooks?: ImmutableOutboxScanHooks,
): Promise<ReadonlyArray<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }>> {
  if (policies.length === 0 || policies.some(({ maximumBytes }) => !Number.isSafeInteger(maximumBytes) || maximumBytes < 1)) throw new UnsafeOutboxError()
  // Node has no portable openat(2) binding. Rejoining a verified root pathname
  // on macOS leaves a root rename/replacement TOCTOU, so non-Linux is denied.
  if (process.platform !== 'linux' || platform !== 'linux') throw new UnsafeOutboxError()
  if (!isAbsolute(path) || path.includes('\0')) throw new UnsafeOutboxError()
  await hooks?.beforeRootOpen?.()
  const rootPath = resolve(path)
  const before = await lstat(rootPath, { bigint: true })
  assertDirectory(before)
  const root = await open(rootPath, noFollowDirectoryFlags())
  try {
    const opened = await root.stat({ bigint: true })
    assertDirectory(opened)
    if (identity(before) !== identity(opened)) throw new UnsafeOutboxError()
    const entries = await readdir(descriptorPath(root.fd), { encoding: 'buffer' })
    const candidates = entries.map((name) => ({
      name,
      policies: policies.filter((policy) => policy.isCandidateFilename(name)),
    })).filter(({ policies: matches }) => matches.length > 0).sort((left, right) => Buffer.compare(left.name, right.name))
    if (candidates.length > MAX_OUTBOX_ENTRIES) throw new UnsafeOutboxError()
    const uid = opened.uid
    const result: Array<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }> = []
    for (const { name: rawName, policies: matches } of candidates) {
      // Overlapping policies would make a pair's maximum size ambiguous. The
      // safe response is to reject the entire descriptor-pinned scan.
      if (matches.length !== 1) throw new UnsafeOutboxError()
      const name = Buffer.from(rawName).toString('utf8')
      if (!Buffer.from(name, 'utf8').equals(Buffer.from(rawName)) || name.includes('/') || name.includes('\\')) {
        result.push({ name: '<invalid>', error: 'invalid' }); continue
      }
      try { result.push({ name, bytes: await readStableFile(`${descriptorPath(root.fd)}/${name}`, uid, matches[0]!.maximumBytes) }) }
      catch (error) {
        result.push({ name, error: error instanceof UnsafeOutboxError ? 'invalid' : 'io' })
        continue
      }
      await hooks?.afterFileRead?.(name)
    }
    return result
  } finally { await root.close() }
}

/** Deliver every valid immutable outbox evidence file once, in deterministic filename order. */
export async function relayOnce(outboxPath: string, store: ManifestStore, filesystem: ManifestFilesystem = new NodeManifestFilesystem()): Promise<RelaySummary> {
  const result = summary()
  let files: ReadonlyArray<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }>
  try { files = await filesystem.scan(outboxPath) }
  catch { result.ioError++; return result }
  result.visited = files.length
  const ordered = [...files].sort((left, right) => Buffer.compare(Buffer.from(left.name), Buffer.from(right.name)))
  for (const file of ordered) {
    if (file.error === 'invalid') { result.invalid++; continue }
    if (file.error === 'io' || !file.bytes) { result.ioError++; continue }
    const command = commandFromFilename(file.name)
    if (!command) { result.invalid++; continue }
    let manifest: Manifest
    try { manifest = parseManifest(file.bytes) }
    catch { result.invalid++; continue }
    if (manifest.commandId !== command) { result.invalid++; continue }
    result.valid++
    try {
      const outcome = await store.recordManifest(manifest)
      if (outcome === 'RECORDED') { result.recorded++; result.delivered++ }
      else if (outcome === 'EXACT_RETRY') { result.exactRetry++; result.delivered++ }
      else result.unknown++
    } catch (error) {
      const category = classifyDatabaseError(error)
      result[category]++
    }
  }
  return result
}

export const relay = relayOnce
export const relayOutboxOnce = relayOnce
