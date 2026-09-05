import { createHash } from 'node:crypto'
import { isAbsolute } from 'node:path'
import {
  verifyPlayerSnapshotV2Replay,
  type PlayerSnapshotV2ReplayVerification,
  type PlayerSnapshotV2ReplayVerifierOptions,
} from './player-snapshot-v2-replay-verifier.js'
import {
  NodePlayerSnapshotV2ReplayShadowJournal,
  type PlayerSnapshotV1ReplayJournalV2Entry,
  type PlayerSnapshotV2ReplayShadowJournal,
} from './player-snapshot-v1-replay-shadow-journal.js'
import type { PlayerSnapshotV1ReplayObservationContext } from './player-snapshot-v1-replay-observer.js'

/** An explicit adapter only; the relay's default observer never imports it. */
export type PlayerSnapshotV2ReplayObservation = 'observed' | 'disabled'

export type PlayerSnapshotV2ReplayVerifier = (
  payload: Uint8Array,
  options: PlayerSnapshotV2ReplayVerifierOptions,
) => Promise<PlayerSnapshotV2ReplayVerification>

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH_RE = /^[0-9a-f]{64}$/

function configuredAbsolutePath(path: string | undefined): string | undefined {
  return path && !path.includes('\0') && isAbsolute(path) ? path : undefined
}

function isJournalContext(value: PlayerSnapshotV1ReplayObservationContext): boolean {
  return UUID_RE.test(value.commandId) && UUID_RE.test(value.characterId)
    && HASH_RE.test(value.receiptRequestSha256) && HASH_RE.test(value.sourcePostSha256)
}

function isV2Result(value: unknown, payload: Uint8Array): value is PlayerSnapshotV2ReplayVerification {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const result = value as Record<string, unknown>
  const digest = createHash('sha256').update(payload).digest('hex')
  return result.format === 'player-snapshot-v1-replay-verification'
    && result.version === '2'
    && result.algorithm === 'sha-256'
    && result.inputDigest === digest
    && result.canonicalDigest === digest
    && result.canonicalOctets === payload.length
    && Number.isSafeInteger(result.canonicalOctets)
    && typeof result.inventoryNodeCount === 'number'
    && Number.isSafeInteger(result.inventoryNodeCount)
    && result.inventoryNodeCount >= 0
    && typeof result.rawLevelU8 === 'number'
    && Number.isSafeInteger(result.rawLevelU8)
    && result.rawLevelU8 >= 0 && result.rawLevelU8 <= 255
}

function isEnabledJournal(journal: PlayerSnapshotV2ReplayShadowJournal | undefined): journal is PlayerSnapshotV2ReplayShadowJournal {
  return journal !== undefined && (!(journal instanceof NodePlayerSnapshotV2ReplayShadowJournal) || journal.isEnabled)
}

/**
 * V2's raw-U8 observation boundary is deliberately construction-only: it has
 * no environment factory and is not referenced by relay CLI or runtime code.
 */
export class PlayerSnapshotV2ReplayObserver {
  private readonly runnerPath: string | undefined

  constructor(
    runnerPath: string | undefined,
    private readonly verifier: PlayerSnapshotV2ReplayVerifier = verifyPlayerSnapshotV2Replay,
    private readonly journal: PlayerSnapshotV2ReplayShadowJournal | undefined = undefined,
  ) {
    this.runnerPath = configuredAbsolutePath(runnerPath)
  }

  async observe(
    payload: Uint8Array,
    context: PlayerSnapshotV1ReplayObservationContext,
  ): Promise<PlayerSnapshotV2ReplayObservation> {
    if (!this.runnerPath || !isEnabledJournal(this.journal) || !isJournalContext(context)) return 'disabled'
    try {
      const result: unknown = await this.verifier(payload, { runnerPath: this.runnerPath })
      if (!isV2Result(result, payload)) return 'disabled'
      const entry: PlayerSnapshotV1ReplayJournalV2Entry = {
        format: 'player-snapshot-v1-replay-shadow-journal', version: '2',
        commandId: context.commandId, characterId: context.characterId,
        receiptRequestSha256: context.receiptRequestSha256, sourcePostSha256: context.sourcePostSha256,
        rawLevelU8: result.rawLevelU8,
        verification: {
          format: result.format, version: result.version, algorithm: result.algorithm,
          inputDigest: result.inputDigest, canonicalDigest: result.canonicalDigest,
          canonicalOctets: result.canonicalOctets, inventoryNodeCount: result.inventoryNodeCount,
        },
      }
      await this.journal.append(entry)
      return 'observed'
    } catch {
      return 'disabled'
    }
  }
}
