import { lstat, open, readdir } from 'node:fs/promises'
import { isAbsolute, join, resolve } from 'node:path'
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

class UnsafeOutboxError extends Error {}

function summary(): RelaySummary {
  return { visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0 }
}

function identity(stat: { dev: number, ino: number, size: number, mtimeMs: number, ctimeMs: number }): string {
  return `${stat.dev}:${stat.ino}:${stat.size}:${stat.mtimeMs}:${stat.ctimeMs}`
}

function descriptorPath(fd: number): string {
  return process.platform === 'linux' ? `/proc/self/fd/${fd}` : `/dev/fd/${fd}`
}

function childPath(fd: number, root: string, name: string): string {
  return process.platform === 'linux' ? `${descriptorPath(fd)}/${name}` : join(root, name)
}

function currentUid(): number | undefined {
  return typeof process.getuid === 'function' ? process.getuid() : undefined
}

function assertDirectory(stat: { isDirectory(): boolean, isSymbolicLink(): boolean, mode: number, uid: number }): void {
  if (stat.isSymbolicLink() || !stat.isDirectory() || (stat.mode & 0o777) !== 0o700 || (currentUid() !== undefined && stat.uid !== currentUid())) throw new UnsafeOutboxError()
}

function assertFile(stat: { isFile(): boolean, isSymbolicLink(): boolean, mode: number, uid: number, nlink: number, size: number }, uid: number): void {
  if (stat.isSymbolicLink() || !stat.isFile() || stat.nlink !== 1 || stat.uid !== uid || (stat.mode & 0o777) !== 0o600 || stat.size < 0 || stat.size >= MAX_MANIFEST_BYTES) throw new UnsafeOutboxError()
}

async function readStableFile(path: string, uid: number): Promise<Uint8Array> {
  const before = await lstat(path)
  assertFile(before, uid)
  const file = await open(path, noFollowFileFlags())
  try {
    const opened = await file.stat()
    assertFile(opened, uid)
    if (identity(before) !== identity(opened)) throw new UnsafeOutboxError()
    const bytes = Buffer.allocUnsafe(opened.size)
    let offset = 0
    while (offset < bytes.length) {
      const read = await file.read(bytes, offset, bytes.length - offset, null)
      if (read.bytesRead === 0) throw new UnsafeOutboxError()
      offset += read.bytesRead
    }
    const extra = Buffer.alloc(1)
    const readExtra = await file.read(extra, 0, 1, null)
    const after = await file.stat()
    if (readExtra.bytesRead !== 0 || identity(opened) !== identity(after)) throw new UnsafeOutboxError()
    return bytes
  } finally { await file.close().catch(() => undefined) }
}

/** Node filesystem implementation: descriptor-rooted on Linux and identity-checked elsewhere. */
export class NodeManifestFilesystem implements ManifestFilesystem {
  async scan(path: string): Promise<ReadonlyArray<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }>> {
    if (!isAbsolute(path) || path.includes('\0')) throw new UnsafeOutboxError()
    const rootPath = resolve(path)
    const before = await lstat(rootPath)
    assertDirectory(before)
    const root = await open(rootPath, noFollowDirectoryFlags())
    try {
      const opened = await root.stat()
      assertDirectory(opened)
      if (identity(before) !== identity(opened)) throw new UnsafeOutboxError()
      // Linux can enumerate through the open descriptor. macOS's /dev/fd/N is
      // not a directory path for all Node builds, so use the verified path and
      // retain the identity check below on that platform.
      const entries = await readdir(process.platform === 'linux' ? descriptorPath(root.fd) : rootPath, { encoding: 'buffer' })
      const rootAfter = await lstat(rootPath)
      if (process.platform !== 'linux' && identity(opened) !== identity(rootAfter)) throw new UnsafeOutboxError()
      const candidates = entries.filter((name) => isManifestFilename(name)).sort(Buffer.compare)
      if (candidates.length > MAX_OUTBOX_ENTRIES) throw new UnsafeOutboxError()
      const uid = opened.uid
      const result: Array<{ name: string, bytes?: Uint8Array, error?: 'invalid' | 'io' }> = []
      for (const rawName of candidates) {
        const name = Buffer.from(rawName).toString('utf8')
        if (!Buffer.from(name, 'utf8').equals(Buffer.from(rawName)) || name.includes('/') || name.includes('\\')) {
          result.push({ name: '<invalid>', error: 'invalid' }); continue
        }
        try { result.push({ name, bytes: await readStableFile(childPath(root.fd, rootPath, name), uid) }) }
        catch (error) {
          result.push({ name, error: error instanceof UnsafeOutboxError ? 'invalid' : 'io' })
        }
      }
      return result
    } finally { await root.close().catch(() => undefined) }
  }
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
