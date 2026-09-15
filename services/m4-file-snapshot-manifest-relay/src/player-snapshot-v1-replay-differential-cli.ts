import { isAbsolute } from 'node:path'
import {
  comparePlayerSnapshotV1ReplayShadowJournal,
  type PlayerSnapshotV1ReplayArtifactDifferentialReader,
  type PlayerSnapshotV1ReplayDifferentialResult,
} from './player-snapshot-v1-replay-differential.js'
import { assertReplayDifferentialDatabaseUrl, PostgresPlayerSnapshotV1ArtifactDifferentialReader } from './store.js'

const JOURNAL_ENV = 'M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_JOURNAL_PATH'
const DATABASE_ENV = 'M4_PLAYER_SNAPSHOT_V1_REPLAY_DIFFERENTIAL_DATABASE_URL'

interface DifferentialDependencies {
  createReader(databaseUrl: string): PlayerSnapshotV1ReplayArtifactDifferentialReader & { close?: () => Promise<void> }
  writeStdout(value: string): void
}

const nodeDependencies: DifferentialDependencies = {
  createReader: (databaseUrl) => new PostgresPlayerSnapshotV1ArtifactDifferentialReader(databaseUrl),
  writeStdout: (value) => process.stdout.write(value),
}

function configuredJournalDirectory(value: string | undefined): string {
  if (!value || value.includes('\0') || !isAbsolute(value)) throw new Error('invalid replay differential configuration')
  return value
}

/**
 * Standalone, explicit opt-in reconciliation command. It never reads a relay
 * writer URL and the supplied reader has no write API.
 */
export async function main(
  env: NodeJS.ProcessEnv = process.env,
  args: readonly string[] = process.argv.slice(2),
  dependencies: DifferentialDependencies = nodeDependencies,
): Promise<number> {
  if (args.length !== 1 || args[0] !== '--once') throw new Error('invalid replay differential configuration')
  const journalDirectory = configuredJournalDirectory(env[JOURNAL_ENV])
  const databaseUrl = assertReplayDifferentialDatabaseUrl(env[DATABASE_ENV])
  if (databaseUrl === env.DATABASE_URL) throw new Error('invalid replay differential configuration')
  const reader = dependencies.createReader(databaseUrl)
  let result: PlayerSnapshotV1ReplayDifferentialResult
  try {
    result = await comparePlayerSnapshotV1ReplayShadowJournal(journalDirectory, reader)
  } finally { await reader.close?.() }
  dependencies.writeStdout(`${JSON.stringify(result)}\n`)
  return result.classification === 'EXACT' ? 0 : 1
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.stderr.write('m4 PlayerSnapshotV1 replay differential failed safely\n')
    process.exitCode = 1
  })
}
