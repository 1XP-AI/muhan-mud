import { isAbsolute } from 'node:path'
import { createHash } from 'node:crypto'
import {
  verifyPlayerSnapshotV1Replay,
  type PlayerSnapshotV1ReplayVerification,
  type PlayerSnapshotV1ReplayVerifierOptions,
} from './player-snapshot-v1-replay-verifier.js'
import {
  NodePlayerSnapshotV1ReplayShadowJournal,
  type PlayerSnapshotV1ReplayJournalEntry,
  type PlayerSnapshotV1ReplayShadowJournal,
} from './player-snapshot-v1-replay-shadow-journal.js'

export type PlayerSnapshotV1ReplayObservation = 'observed' | 'disabled' | 'failed'

/** Identifiers are copied from already parsed artifact and receipt metadata. */
export interface PlayerSnapshotV1ReplayObservationContext {
  commandId: string
  characterId: string
  receiptRequestSha256: string
  sourcePostSha256: string
  /** SHA-256 from the already-validated immutable artifact header. */
  snapshotSha256: string
}

/** The relay deliberately consumes only this binary observation outcome. */
export interface PlayerSnapshotV1ReplayObserver {
  observe(payload: Uint8Array, context: PlayerSnapshotV1ReplayObservationContext): Promise<PlayerSnapshotV1ReplayObservation>
}

export type PlayerSnapshotV1ReplayVerifier = (
  payload: Uint8Array,
  options: PlayerSnapshotV1ReplayVerifierOptions,
) => Promise<PlayerSnapshotV1ReplayVerification>

function configuredRunnerPath(env: NodeJS.ProcessEnv): string | undefined {
  return configuredAbsolutePath(env.M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH)
}

function configuredJournalPath(env: NodeJS.ProcessEnv): string | undefined {
  return configuredAbsolutePath(env.M4_PLAYER_SNAPSHOT_V1_REPLAY_JOURNAL_PATH)
}

function configuredAbsolutePath(path: string | undefined): string | undefined {
  return path && !path.includes('\0') && isAbsolute(path) ? path : undefined
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/

function isJournalContext(value: PlayerSnapshotV1ReplayObservationContext): boolean {
  return UUID_RE.test(value.commandId) && UUID_RE.test(value.characterId)
    && HASH_RE.test(value.receiptRequestSha256) && HASH_RE.test(value.sourcePostSha256)
    && HASH_RE.test(value.snapshotSha256)
}

/**
 * The verifier is injectable for tests and deployment wiring, so its TypeScript
 * return annotation cannot be treated as a runtime boundary.  Keep this
 * observer metadata-only by accepting only the same complete report contract
 * validated by the production verifier.
 */
function isVerifiedReplayResult(value: unknown, payload: Uint8Array): value is PlayerSnapshotV1ReplayVerification {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const result = value as Record<string, unknown>
  const expectedDigest = createHash('sha256').update(payload).digest('hex')
  return result.format === 'player-snapshot-v1-replay-verification'
    && result.version === '1'
    && result.algorithm === 'sha-256'
    && result.inputDigest === expectedDigest
    && result.canonicalDigest === expectedDigest
    && result.canonicalOctets === payload.length
    && Number.isSafeInteger(result.canonicalOctets)
    && typeof result.inventoryNodeCount === 'number'
    && Number.isSafeInteger(result.inventoryNodeCount)
    && result.inventoryNodeCount >= 0
}

function projectedVerification(result: PlayerSnapshotV1ReplayVerification): PlayerSnapshotV1ReplayJournalEntry['verification'] {
  return {
    format: result.format,
    version: result.version,
    algorithm: result.algorithm,
    inputDigest: result.inputDigest,
    canonicalDigest: result.canonicalDigest,
    canonicalOctets: result.canonicalOctets,
    inventoryNodeCount: result.inventoryNodeCount,
  }
}

function isEnabledJournal(journal: PlayerSnapshotV1ReplayShadowJournal | undefined): journal is PlayerSnapshotV1ReplayShadowJournal {
  return journal !== undefined && (!(journal instanceof NodePlayerSnapshotV1ReplayShadowJournal) || journal.isEnabled)
}

/**
 * A best-effort, metadata-only replay check. Any unavailable runner or invalid
 * report is explicitly disabled so it cannot alter the established relay flow.
 */
export class PlayerSnapshotV1ReplayObserver implements PlayerSnapshotV1ReplayObserver {
  private readonly runnerPath: string | undefined
  private readonly verifier: PlayerSnapshotV1ReplayVerifier
  private readonly journal: PlayerSnapshotV1ReplayShadowJournal | undefined
  /** In-process dedupe prevents replayed artifacts from rerunning diagnostics. */
  private readonly observations = new Map<string, Promise<PlayerSnapshotV1ReplayObservation>>()

  constructor(
    runnerPath: string | undefined,
    verifier: PlayerSnapshotV1ReplayVerifier = verifyPlayerSnapshotV1Replay,
    journal: PlayerSnapshotV1ReplayShadowJournal | undefined = undefined,
  ) {
    this.runnerPath = configuredAbsolutePath(runnerPath)
    this.verifier = verifier
    this.journal = journal
  }

  async observe(payload: Uint8Array, context: PlayerSnapshotV1ReplayObservationContext): Promise<PlayerSnapshotV1ReplayObservation> {
    const journal = this.journal
    const runnerPath = this.runnerPath
    if (!runnerPath || !isEnabledJournal(journal) || !isJournalContext(context)
      || createHash('sha256').update(payload).digest('hex') !== context.snapshotSha256) return 'disabled'
    const key = `${context.commandId}:${context.characterId}:${context.receiptRequestSha256}:${context.sourcePostSha256}:${context.snapshotSha256}`
    const existing = this.observations.get(key)
    if (existing) return existing
    const observation = this.observeOnce(payload, context, runnerPath, journal)
    this.observations.set(key, observation)
    return observation
  }

  private async observeOnce(
    payload: Uint8Array,
    context: PlayerSnapshotV1ReplayObservationContext,
    runnerPath: string,
    journal: PlayerSnapshotV1ReplayShadowJournal,
  ): Promise<PlayerSnapshotV1ReplayObservation> {
    try {
      const result: unknown = await this.verifier(payload, { runnerPath, snapshotSha256: context.snapshotSha256 })
      if (!isVerifiedReplayResult(result, payload)) return 'failed'
      const entry: PlayerSnapshotV1ReplayJournalEntry = {
        format: 'player-snapshot-v1-replay-shadow-journal', version: '1',
        commandId: context.commandId, characterId: context.characterId,
        receiptRequestSha256: context.receiptRequestSha256, sourcePostSha256: context.sourcePostSha256,
        verification: projectedVerification(result),
      }
      await journal.append(entry)
      return 'observed'
    } catch {
      return 'failed'
    }
  }
}

/** Runtime configuration is opt-in: only a directly injected absolute path enables observation. */
export function playerSnapshotV1ReplayObserverFromEnvironment(
  env: NodeJS.ProcessEnv,
  verifier: PlayerSnapshotV1ReplayVerifier = verifyPlayerSnapshotV1Replay,
): PlayerSnapshotV1ReplayObserver {
  const journalPath = configuredJournalPath(env)
  return new PlayerSnapshotV1ReplayObserver(
    configuredRunnerPath(env), verifier,
    journalPath ? new NodePlayerSnapshotV1ReplayShadowJournal(journalPath) : undefined,
  )
}
