import { constants as fsConstants } from 'node:fs'
import { lstat, open } from 'node:fs/promises'
import { createBatchIdentity } from './batch-identity.js'
import { importBatch, importInventory, importRecords, parseInventoryLine, validateWorldId } from './inventory.js'
import { PostgresImportStore, assertAdminDatabaseUrl } from './postgres-store.js'
import { MAX_SCAN_RECORDS, scanMudHome } from './scanner.js'

const MAX_JSONL_INPUT_BYTES = 64 * 1024 * 1024

type Input = { kind: 'inventory', path: string } | { kind: 'mud_home', path: string }
type BatchMode = { identityPath: string, streamId: string, sequence: number }
export interface CliOptions { input: Input, worldId: string, apply: boolean, batch?: BatchMode }

export function parseArgs(args: readonly string[]): CliOptions {
  let inventoryPath: string | undefined
  let mudHome: string | undefined
  let worldId = 'muhan'
  let worldIdProvided = false
  let apply = false
  let identityPath: string | undefined
  let streamId: string | undefined
  let sequence: number | undefined
  for (let index = 0; index < args.length; index++) {
    const argument = args[index]
    if (argument === '--apply') { apply = true; continue }
    if (argument === '--inventory' || argument === '--mud-home' || argument === '--world-id' || argument === '--batch-identity' || argument === '--batch-stream' || argument === '--batch-sequence') {
      const value = args[++index]
      if (!value) throw new Error('invalid import configuration')
      if (argument === '--inventory') inventoryPath = value
      else if (argument === '--mud-home') mudHome = value
      else if (argument === '--world-id') { worldId = value; worldIdProvided = true }
      else if (argument === '--batch-identity') identityPath = value
      else if (argument === '--batch-stream') streamId = value
      else {
        if (!/^(?:0|[1-9]\d*)$/.test(value)) throw new Error('invalid import configuration')
        sequence = Number(value)
        if (!Number.isSafeInteger(sequence)) throw new Error('invalid import configuration')
      }
      continue
    }
    throw new Error('invalid import configuration')
  }
  const hasAnyBatchOption = identityPath !== undefined || streamId !== undefined || sequence !== undefined
  const batch = identityPath !== undefined && streamId !== undefined && sequence !== undefined
    ? { identityPath, streamId, sequence }
    : undefined
  if ((!inventoryPath && !mudHome) || (inventoryPath && mudHome) || !validateWorldId(worldId)
    || (hasAnyBatchOption && !batch) || (batch && worldIdProvided)) throw new Error('invalid import configuration')
  return { input: inventoryPath ? { kind: 'inventory', path: inventoryPath } : { kind: 'mud_home', path: mudHome! }, worldId, apply, ...(batch ? { batch } : {}) }
}

async function readBoundedUtf8File(path: string, maxBytes: number): Promise<string> {
  if (!fsConstants.O_NOFOLLOW) throw new Error('invalid import input')
  const before = await lstat(path)
  if (before.isSymbolicLink() || !before.isFile() || before.size > maxBytes) throw new Error('invalid import input')
  const file = await open(path, fsConstants.O_RDONLY | fsConstants.O_NOFOLLOW | fsConstants.O_NONBLOCK)
  try {
    const opened = await file.stat()
    if (!opened.isFile() || opened.size !== before.size || opened.dev !== before.dev || opened.ino !== before.ino) throw new Error('invalid import input')
    const bytes = Buffer.allocUnsafe(opened.size)
    let offset = 0
    while (offset < bytes.length) {
      const { bytesRead } = await file.read(bytes, offset, bytes.length - offset, offset)
      if (bytesRead === 0) throw new Error('invalid import input')
      offset += bytesRead
    }
    const after = await file.stat()
    if (after.size !== opened.size || after.dev !== opened.dev || after.ino !== opened.ino || after.mtimeMs !== opened.mtimeMs || after.ctimeMs !== opened.ctimeMs) throw new Error('invalid import input')
    const text = bytes.toString('utf8')
    if (!Buffer.from(text, 'utf8').equals(bytes)) throw new Error('invalid import input')
    return text
  } finally {
    await file.close().catch(() => undefined)
  }
}

export async function readMetadataLines(path: string): Promise<string[]> {
  const text = await readBoundedUtf8File(path, MAX_JSONL_INPUT_BYTES)
  const lines = text.split(/\r?\n/).filter((line) => line !== '')
  if (lines.length > MAX_SCAN_RECORDS) throw new Error('invalid import input')
  return lines
}

async function readBatchIdentity(path: string) {
  const text = await readBoundedUtf8File(path, 16 * 1024)
  let value: unknown
  try { value = JSON.parse(text) } catch { throw new Error('invalid import input') }
  return createBatchIdentity(value)
}

/** Logs only aggregate outcomes. It deliberately never logs names, paths, hashes, or DB URLs. */
export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2)): Promise<number> {
  const options = parseArgs(args)
  const databaseUrl = assertAdminDatabaseUrl(env.DATABASE_URL)
  const store = new PostgresImportStore(databaseUrl)
  try {
    const loaded = options.input.kind === 'inventory'
      ? { records: undefined, lines: await readMetadataLines(options.input.path), rejected: 0 }
      : await (async () => {
        const scanned = await scanMudHome(options.input.path)
        return { records: scanned.records, lines: undefined, rejected: scanned.rejected }
      })()
    const batch = options.batch
    const summary = batch
      ? await (async () => {
        const parsed = loaded.records ?? loaded.lines!.flatMap((line) => {
          const record = parseInventoryLine(line)
          return record === undefined ? [] : [record]
        })
        const rejected = loaded.rejected + (loaded.lines?.filter((line) => parseInventoryLine(line) === undefined).length ?? 0)
        return importBatch(store, parsed,
          { identity: await readBatchIdentity(batch.identityPath), streamId: batch.streamId, sequence: batch.sequence, apply: options.apply },
          rejected)
      })()
      : loaded.records
        ? await importRecords(store, loaded.records, { worldId: options.worldId, apply: options.apply }, loaded.rejected)
        : await importInventory(store, loaded.lines!, { worldId: options.worldId, apply: options.apply })
    process.stdout.write(`${JSON.stringify({ mode: options.apply ? 'apply' : 'dry_run', ledger: options.batch ? 'batch' : 'none', ...summary })}\n`)
    return Object.values(summary.quarantined).some((count) => count > 0) ? 1 : 0
  } finally {
    await store.close()
  }
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((exitCode) => { process.exitCode = exitCode }).catch(() => {
    process.stderr.write('character inventory import failed safely\n')
    process.exitCode = 1
  })
}
