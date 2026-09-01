import { constants as fsConstants } from 'node:fs'
import { lstat, open } from 'node:fs/promises'
import { importInventory, importRecords, validateWorldId } from './inventory.js'
import { PostgresImportStore, assertAdminDatabaseUrl } from './postgres-store.js'
import { MAX_SCAN_RECORDS, scanMudHome } from './scanner.js'

const MAX_JSONL_INPUT_BYTES = 64 * 1024 * 1024

type Input = { kind: 'inventory', path: string } | { kind: 'mud_home', path: string }
export interface CliOptions { input: Input, worldId: string, apply: boolean }

export function parseArgs(args: readonly string[]): CliOptions {
  let inventoryPath: string | undefined
  let mudHome: string | undefined
  let worldId = 'muhan'
  let apply = false
  for (let index = 0; index < args.length; index++) {
    const argument = args[index]
    if (argument === '--apply') { apply = true; continue }
    if (argument === '--inventory' || argument === '--mud-home' || argument === '--world-id') {
      const value = args[++index]
      if (!value) throw new Error('invalid import configuration')
      if (argument === '--inventory') inventoryPath = value
      else if (argument === '--mud-home') mudHome = value
      else worldId = value
      continue
    }
    throw new Error('invalid import configuration')
  }
  if ((!inventoryPath && !mudHome) || (inventoryPath && mudHome) || !validateWorldId(worldId)) throw new Error('invalid import configuration')
  return { input: inventoryPath ? { kind: 'inventory', path: inventoryPath } : { kind: 'mud_home', path: mudHome! }, worldId, apply }
}

export async function readMetadataLines(path: string): Promise<string[]> {
  if (!fsConstants.O_NOFOLLOW) throw new Error('invalid import input')
  const before = await lstat(path)
  if (before.isSymbolicLink() || !before.isFile() || before.size > MAX_JSONL_INPUT_BYTES) throw new Error('invalid import input')
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
    const lines = text.split(/\r?\n/).filter((line) => line !== '')
    if (lines.length > MAX_SCAN_RECORDS) throw new Error('invalid import input')
    return lines
  } finally {
    await file.close().catch(() => undefined)
  }
}

/** Logs only aggregate outcomes. It deliberately never logs names, paths, hashes, or DB URLs. */
export async function main(env: NodeJS.ProcessEnv = process.env, args: readonly string[] = process.argv.slice(2)): Promise<number> {
  const options = parseArgs(args)
  const databaseUrl = assertAdminDatabaseUrl(env.DATABASE_URL)
  const store = new PostgresImportStore(databaseUrl)
  try {
    const summary = options.input.kind === 'inventory'
      ? await importInventory(store, await readMetadataLines(options.input.path), { worldId: options.worldId, apply: options.apply })
      : await (async () => {
        const scanned = await scanMudHome(options.input.path)
        return importRecords(store, scanned.records, { worldId: options.worldId, apply: options.apply }, scanned.rejected)
      })()
    process.stdout.write(`${JSON.stringify({ mode: options.apply ? 'apply' : 'dry_run', ...summary })}\n`)
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
