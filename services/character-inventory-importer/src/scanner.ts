import { constants as fsConstants } from 'node:fs'
import { lstat, open, opendir } from 'node:fs/promises'
import { isAbsolute, join, resolve } from 'node:path'
import { createHash } from 'node:crypto'
import { expectedShard, type InventoryRecord, validRecord } from './inventory.js'

export const MAX_SCAN_FILE_BYTES = 64 * 1024 * 1024
export const MAX_SCAN_RECORDS = 100_000

const SHARD_RE = /^[0-9a-f]{2}$/
const KNOWN_NON_CHARACTER_DIRECTORIES = new Set(['alias', 'bank', 'fal', 'family', 'invite', 'marriage', 'simul', 'suic', 'vote'])
const KNOWN_NON_CHARACTER_FILES = new Set(['README'])
const MAX_PLAYER_ROOT_ENTRIES = 512

export interface ScannerOptions {
  /** Test-only reduction; production cannot exceed the conservative hard cap. */
  maxRecords?: number
  maxFileBytes?: number
}

export interface ScanResult {
  records: InventoryRecord[]
  /** Aggregate only: no rejected identity, path, hash, or payload is retained. */
  rejected: number
}

interface FileIdentity {
  dev: number
  ino: number
  size: number
  mtimeMs: number
  ctimeMs: number
}

class UnsafeFilesystemError extends Error {}
class ScanLimitError extends Error {}

function boundedLimit(value: number | undefined, defaultValue: number, maximum: number): number {
  if (value === undefined) return defaultValue
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) throw new Error('invalid scanner configuration')
  return value
}

function identity(stat: { dev: number, ino: number, size: number, mtimeMs: number, ctimeMs: number }): FileIdentity {
  return { dev: stat.dev, ino: stat.ino, size: stat.size, mtimeMs: stat.mtimeMs, ctimeMs: stat.ctimeMs }
}

function sameIdentity(left: FileIdentity, right: FileIdentity): boolean {
  return left.dev === right.dev && left.ino === right.ino && left.size === right.size
    && left.mtimeMs === right.mtimeMs && left.ctimeMs === right.ctimeMs
}

function descriptorPath(directoryFd: number, name = ''): string {
  return `/proc/self/fd/${directoryFd}${name === '' ? '' : `/${name}`}`
}

function decodeEntryName(name: string | Buffer): string | undefined {
  const bytes = Buffer.isBuffer(name) ? name : Buffer.from(name, 'utf8')
  const decoded = bytes.toString('utf8')
  return Buffer.from(decoded, 'utf8').equals(bytes) ? decoded : undefined
}

function pathForChild(parentFd: number, fallbackParent: string, name: string): string {
  return process.platform === 'linux' ? descriptorPath(parentFd, name) : join(fallbackParent, name)
}

function noFollowDirectoryFlags(): number {
  if (!fsConstants.O_NOFOLLOW || !fsConstants.O_DIRECTORY) throw new UnsafeFilesystemError()
  return fsConstants.O_RDONLY | fsConstants.O_NONBLOCK | fsConstants.O_NOFOLLOW | fsConstants.O_DIRECTORY
}

function noFollowFileFlags(): number {
  if (!fsConstants.O_NOFOLLOW) throw new UnsafeFilesystemError()
  return fsConstants.O_RDONLY | fsConstants.O_NONBLOCK | fsConstants.O_NOFOLLOW
}

async function openSafeDirectory(path: string) {
  const before = await lstat(path)
  if (before.isSymbolicLink() || !before.isDirectory()) throw new UnsafeFilesystemError()
  const directory = await open(path, noFollowDirectoryFlags())
  try {
    const opened = await directory.stat()
    const after = await lstat(path)
    if (!opened.isDirectory() || after.isSymbolicLink() || !sameIdentity(identity(before), identity(opened)) || !sameIdentity(identity(before), identity(after))) throw new UnsafeFilesystemError()
    return directory
  } catch (error) {
    await directory.close().catch(() => undefined)
    throw error
  }
}

interface DirectoryEntry { name: string | Buffer }

async function listEntries(directory: Awaited<ReturnType<typeof open>>, fallbackPath: string, maximumEntries: number): Promise<DirectoryEntry[]> {
  const path = process.platform === 'linux' ? descriptorPath(directory.fd) : fallbackPath
  const before = process.platform === 'linux' ? undefined : identity(await directory.stat())
  // Node supports `buffer` here at runtime (and returns raw directory-entry
  // bytes), although the current @types/node opendir overload omits it.
  const stream = await opendir(path, { encoding: 'buffer' as BufferEncoding })
  const entries: DirectoryEntry[] = []
  try {
    for await (const entry of stream) {
      if (entries.length >= maximumEntries) throw new ScanLimitError()
      entries.push(entry)
    }
  } finally {
    await stream.close().catch(() => undefined)
  }
  if (before) {
    const after = identity(await directory.stat())
    const current = await lstat(fallbackPath)
    if (current.isSymbolicLink() || !sameIdentity(before, after) || !sameIdentity(before, identity(current))) throw new UnsafeFilesystemError()
  }
  return entries
}

async function hashRegularFile(path: string, maximumBytes: number): Promise<{ byteSize: number, sha256: string }> {
  const before = await lstat(path)
  if (before.isSymbolicLink() || !before.isFile() || before.size < 0 || before.size > maximumBytes) throw new UnsafeFilesystemError()
  const file = await open(path, noFollowFileFlags())
  try {
    const opened = await file.stat()
    if (!opened.isFile() || !sameIdentity(identity(before), identity(opened)) || opened.size > maximumBytes) throw new UnsafeFilesystemError()
    const digest = createHash('sha256')
    const chunk = Buffer.allocUnsafe(64 * 1024)
    let length = 0
    for (;;) {
      const { bytesRead } = await file.read(chunk, 0, chunk.length, null)
      if (bytesRead === 0) break
      length += bytesRead
      if (length > maximumBytes) throw new UnsafeFilesystemError()
      digest.update(chunk.subarray(0, bytesRead))
    }
    const after = await file.stat()
    if (length !== opened.size || !sameIdentity(identity(opened), identity(after))) throw new UnsafeFilesystemError()
    return { byteSize: length, sha256: digest.digest('hex') }
  } finally {
    await file.close().catch(() => undefined)
  }
}

function deterministicSort<T extends DirectoryEntry>(entries: readonly T[]): T[] {
  return [...entries].sort((left, right) => {
    const leftBytes = Buffer.isBuffer(left.name) ? left.name : Buffer.from(left.name, 'utf8')
    const rightBytes = Buffer.isBuffer(right.name) ? right.name : Buffer.from(right.name, 'utf8')
    return Buffer.compare(leftBytes, rightBytes)
  })
}

/**
 * Scan a mounted legacy tree without following symlinks or reading a file into
 * memory. Returned records are the same metadata shape accepted by the importer.
 */
export async function scanMudHome(mudHome: string, options: ScannerOptions = {}): Promise<ScanResult> {
  const maxRecords = boundedLimit(options.maxRecords, MAX_SCAN_RECORDS, MAX_SCAN_RECORDS)
  const maxFileBytes = boundedLimit(options.maxFileBytes, MAX_SCAN_FILE_BYTES, MAX_SCAN_FILE_BYTES)
  if (!isAbsolute(mudHome) || mudHome.includes('\0')) return { records: [], rejected: 1 }
  const rootPath = resolve(mudHome)
  const playerPath = join(rootPath, 'player')
  const records: InventoryRecord[] = []
  let rejected = 0
  let considered = 0
  let root: Awaited<ReturnType<typeof open>> | undefined
  let player: Awaited<ReturnType<typeof open>> | undefined
  try {
    root = await openSafeDirectory(rootPath)
    player = await openSafeDirectory(pathForChild(root.fd, rootPath, 'player'))
    const shardEntries = deterministicSort(await listEntries(player, playerPath, MAX_PLAYER_ROOT_ENTRIES))
    shardLoop:
    for (const shardEntry of shardEntries) {
      const shard = decodeEntryName(shardEntry.name)
      if (!shard) { rejected++; continue }
      if (KNOWN_NON_CHARACTER_DIRECTORIES.has(shard) || KNOWN_NON_CHARACTER_FILES.has(shard)) continue
      if (!SHARD_RE.test(shard)) { rejected++; continue }
      const shardPath = join(playerPath, shard)
      let shardDirectory: Awaited<ReturnType<typeof open>> | undefined
      try {
        shardDirectory = await openSafeDirectory(pathForChild(player.fd, playerPath, shard))
        const remaining = maxRecords - considered
        const entries = deterministicSort(await listEntries(shardDirectory, shardPath, remaining + KNOWN_NON_CHARACTER_FILES.size))
        for (const entry of entries) {
          const name = decodeEntryName(entry.name)
          if (name !== undefined && KNOWN_NON_CHARACTER_FILES.has(name)) continue
          considered++
          if (!name) { rejected++; continue }
          try {
            const fingerprint = await hashRegularFile(pathForChild(shardDirectory.fd, shardPath, name), maxFileBytes)
            const record: InventoryRecord = {
              name,
              canonicalNameKey: name,
              relativePath: `player/${shard}/${name}`,
              observedShard: shard,
              expectedShard: expectedShard(name),
              byteSize: fingerprint.byteSize,
              sha256: fingerprint.sha256,
            }
            if (!validRecord(record)) { rejected++; continue }
            records.push(record)
          } catch {
            rejected++
          }
        }
      } catch (error) {
        rejected++
        if (error instanceof ScanLimitError) {
          records.length = 0
          break shardLoop
        }
      } finally {
        await shardDirectory?.close().catch(() => undefined)
      }
    }
  } catch {
    rejected++
  } finally {
    await player?.close().catch(() => undefined)
    await root?.close().catch(() => undefined)
  }
  records.sort((left, right) => left.name === right.name
    ? (left.relativePath < right.relativePath ? -1 : left.relativePath > right.relativePath ? 1 : 0)
    : (left.name < right.name ? -1 : 1))
  // Never leave an ambiguous canonical identity for the importer to choose.
  const duplicateNames = new Set<string>()
  const seenNames = new Set<string>()
  for (const record of records) {
    if (seenNames.has(record.canonicalNameKey)) duplicateNames.add(record.canonicalNameKey)
    seenNames.add(record.canonicalNameKey)
  }
  if (duplicateNames.size === 0) return { records, rejected }
  const uniqueRecords = records.filter((record) => {
    if (!duplicateNames.has(record.canonicalNameKey)) return true
    rejected++
    return false
  })
  return { records: uniqueRecords, rejected }
}
