import { isAbsolute } from 'node:path'
import type { ImmutablePlayerSnapshotLevelProjectionReader } from './player-snapshot-v1-level-comparator.js'
import { comparePlayerSnapshotV2JournalLevelShadowJournal } from './player-snapshot-v2-journal-level-shadow-comparator.js'
import { assertReplayDifferentialDatabaseUrl, PostgresPlayerSnapshotV1LevelProjectionReader } from './store.js'

const JOURNAL_ENV = 'M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH'
const DATABASE_ENV = 'M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL'

interface ShadowComparatorDependencies {
  createReader(databaseUrl: string): ImmutablePlayerSnapshotLevelProjectionReader & { close?: () => Promise<void> }
  writeStdout(value: string): void
}

const nodeDependencies: ShadowComparatorDependencies = {
  createReader: (databaseUrl) => new PostgresPlayerSnapshotV1LevelProjectionReader(databaseUrl),
  writeStdout: (value) => process.stdout.write(value),
}

function configuredJournalDirectory(value: string | undefined): string {
  if (!value || value.includes('\0') || !isAbsolute(value)) throw new Error('invalid v2 journal level shadow comparator configuration')
  return value
}

/** A comparison-only identity for detecting an accidental writer connection. */
function canonicalPostgresConnectionUrl(value: string | undefined): string | undefined {
  if (!value) return undefined
  try {
    const url = new URL(value)
    if (url.protocol !== 'postgres:' && url.protocol !== 'postgresql:') return undefined
    const decode = (component: string) => decodeURIComponent(component)
    const query = Array.from(url.searchParams.entries())
      .sort(([leftKey, leftValue], [rightKey, rightValue]) => leftKey.localeCompare(rightKey) || leftValue.localeCompare(rightValue))
    return JSON.stringify([
      'postgresql:', decode(url.username), decode(url.password), url.hostname.toLowerCase(),
      url.port === '5432' ? '' : url.port, decode(url.pathname), query,
    ])
  } catch { return undefined }
}

/**
 * A standalone, explicit opt-in reconciliation command. It has no relay
 * writer URL input and its injected reader has no write API.
 */
export async function main(
  env: NodeJS.ProcessEnv = process.env,
  args: readonly string[] = process.argv.slice(2),
  dependencies: ShadowComparatorDependencies = nodeDependencies,
): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once') throw new Error('invalid v2 journal level shadow comparator configuration')
  const journalDirectory = configuredJournalDirectory(env[JOURNAL_ENV])
  const databaseUrl = assertReplayDifferentialDatabaseUrl(env[DATABASE_ENV])
  const readerConnectionUrl = canonicalPostgresConnectionUrl(databaseUrl)
  if (readerConnectionUrl === undefined || readerConnectionUrl === canonicalPostgresConnectionUrl(env.DATABASE_URL)) {
    throw new Error('invalid v2 journal level shadow comparator configuration')
  }
  const reader = dependencies.createReader(databaseUrl)
  let result
  try {
    result = await comparePlayerSnapshotV2JournalLevelShadowJournal(journalDirectory, reader)
  } finally { await reader.close?.() }
  dependencies.writeStdout(`${JSON.stringify(result)}\n`)
  return result.classification === 'MATCH' ? 0 : 1
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 PlayerSnapshotV2 journal level shadow comparator failed safely\n')
    process.exitCode = 1
  })
}
