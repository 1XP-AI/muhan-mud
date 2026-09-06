import { isAbsolute } from 'node:path'
import { parseManifest } from './manifest.js'
import { NodePlayerSnapshotV1ArtifactFilesystem, type PlayerSnapshotV1ArtifactFilesystem } from './player-snapshot-v1-artifact-relay.js'
import { parsePlayerSnapshotV1Artifact, type PlayerSnapshotV1Artifact } from './player-snapshot-v1-artifact.js'
import {
  rehearsePlayerSnapshotV1FullPayload,
  type ImmutablePlayerSnapshotV1FullPayloadReader,
  type PlayerSnapshotV1FullPayloadRehearsal,
} from './player-snapshot-v1-full-payload-rehearsal.js'
import { verifyPlayerSnapshotV1Replay, type PlayerSnapshotV1ReplayVerifierOptions } from './player-snapshot-v1-replay-verifier.js'
import { assertFullPayloadRehearsalDatabaseUrl, PostgresPlayerSnapshotV1FullPayloadRehearsalReader } from './store.js'

const OUTBOX_ENV = 'M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_OUTBOX_PATH'
const DATABASE_ENV = 'M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_DATABASE_URL'
const VERIFIER_ENV = 'M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_VERIFIER_PATH'
const DIAGNOSTIC_FORMAT = 'player-snapshot-v1-full-payload-rehearsal' as const
const DIAGNOSTIC_VERSION = '1' as const
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

interface ClosableFullPayloadReader extends ImmutablePlayerSnapshotV1FullPayloadReader {
  close?: () => Promise<void>
}

export interface PlayerSnapshotV1FullPayloadRehearsalCliDependencies {
  loadArtifact(path: string): Promise<PlayerSnapshotV1Artifact>
  createReader(databaseUrl: string): ClosableFullPayloadReader
  verify(payload: Uint8Array, options: PlayerSnapshotV1ReplayVerifierOptions): ReturnType<typeof verifyPlayerSnapshotV1Replay>
  writeStdout(value: string): void
}

const nodeDependencies: PlayerSnapshotV1FullPayloadRehearsalCliDependencies = {
  loadArtifact: (path) => loadPairedImmutablePlayerSnapshotV1Artifact(path),
  createReader: (databaseUrl) => new PostgresPlayerSnapshotV1FullPayloadRehearsalReader(databaseUrl),
  verify: verifyPlayerSnapshotV1Replay,
  writeStdout: (value) => process.stdout.write(value),
}

function invalidConfiguration(): never { throw new Error('invalid full payload rehearsal configuration') }

function requiredAbsolutePath(value: string | undefined): string {
  if (!value || value.includes('\0') || !isAbsolute(value)) invalidConfiguration()
  return value
}

function diagnostic(
  classification: PlayerSnapshotV1FullPayloadRehearsal,
  artifact?: Pick<PlayerSnapshotV1Artifact, 'commandId' | 'characterId'>,
): string {
  const commandId = typeof artifact?.commandId === 'string' && UUID_RE.test(artifact.commandId) ? artifact.commandId : null
  const characterId = typeof artifact?.characterId === 'string' && UUID_RE.test(artifact.characterId) ? artifact.characterId : null
  return `${JSON.stringify({
    format: DIAGNOSTIC_FORMAT, version: DIAGNOSTIC_VERSION, classification,
    commandId, characterId,
  })}\n`
}

/** Select one immutable artifact/receipt pair and parse it without changing either file. */
export async function loadPairedImmutablePlayerSnapshotV1Artifact(
  outboxPath: string,
  filesystem: PlayerSnapshotV1ArtifactFilesystem = new NodePlayerSnapshotV1ArtifactFilesystem(),
): Promise<PlayerSnapshotV1Artifact> {
  const files = await filesystem.scan(outboxPath)
  if (files.length !== 1) invalidConfiguration()
  const file = files[0]
  if (!file || file.error || !file.bytes || !file.receiptManifestBytes) invalidConfiguration()
  return parsePlayerSnapshotV1Artifact(file.name, file.bytes, parseManifest(file.receiptManifestBytes))
}

/**
 * Default-off, one-shot diagnostic. Its only database capability is the
 * dedicated full-payload reader, and no normal relay entrypoint imports it.
 */
export async function main(
  env: NodeJS.ProcessEnv = process.env,
  args: readonly string[] = process.argv.slice(2),
  dependencies: PlayerSnapshotV1FullPayloadRehearsalCliDependencies = nodeDependencies,
): Promise<number> {
  let outboxPath: string
  let databaseUrl: string
  let verifierPath: string
  try {
    if (args.length !== 1 || args[0] !== '--once') invalidConfiguration()
    outboxPath = requiredAbsolutePath(env[OUTBOX_ENV])
    databaseUrl = assertFullPayloadRehearsalDatabaseUrl(env[DATABASE_ENV])
    verifierPath = requiredAbsolutePath(env[VERIFIER_ENV])
  } catch {
    dependencies.writeStdout(diagnostic('INVALID_INPUT'))
    return 1
  }

  let artifact: PlayerSnapshotV1Artifact
  try { artifact = await dependencies.loadArtifact(outboxPath) }
  catch {
    dependencies.writeStdout(diagnostic('INVALID_INPUT'))
    return 1
  }

  let reader: ClosableFullPayloadReader
  try { reader = dependencies.createReader(databaseUrl) }
  catch {
    dependencies.writeStdout(diagnostic('DB_READ_ERROR', artifact))
    return 1
  }
  let classification: PlayerSnapshotV1FullPayloadRehearsal
  try {
    classification = await rehearsePlayerSnapshotV1FullPayload(
      artifact,
      reader,
      (payload) => dependencies.verify(payload, { runnerPath: verifierPath }),
    )
  } finally {
    await reader.close?.()
  }
  dependencies.writeStdout(diagnostic(classification, artifact))
  return classification === 'MATCH' ? 0 : 1
}

if (import.meta.url === new URL(process.argv[1]!, 'file:').href) {
  main().then((code) => { process.exitCode = code }).catch(() => {
    process.exitCode = 1
  })
}
