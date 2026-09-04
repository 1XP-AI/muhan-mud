import { isAbsolute } from 'node:path'
import { createHash } from 'node:crypto'
import {
  verifyPlayerSnapshotV1Replay,
  type PlayerSnapshotV1ReplayVerification,
  type PlayerSnapshotV1ReplayVerifierOptions,
} from './player-snapshot-v1-replay-verifier.js'

export type PlayerSnapshotV1ReplayObservation = 'observed' | 'disabled'

/** The relay deliberately consumes only this binary observation outcome. */
export interface PlayerSnapshotV1ReplayObserver {
  observe(payload: Uint8Array): Promise<PlayerSnapshotV1ReplayObservation>
}

export type PlayerSnapshotV1ReplayVerifier = (
  payload: Uint8Array,
  options: PlayerSnapshotV1ReplayVerifierOptions,
) => Promise<PlayerSnapshotV1ReplayVerification>

function configuredRunnerPath(env: NodeJS.ProcessEnv): string | undefined {
  const path = env.M4_PLAYER_SNAPSHOT_V1_REPLAY_VERIFY_PATH
  return path && !path.includes('\0') && isAbsolute(path) ? path : undefined
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

/**
 * A best-effort, metadata-only replay check. Any unavailable runner or invalid
 * report is explicitly disabled so it cannot alter the established relay flow.
 */
export class PlayerSnapshotV1ReplayObserver implements PlayerSnapshotV1ReplayObserver {
  constructor(
    private readonly runnerPath: string | undefined,
    private readonly verifier: PlayerSnapshotV1ReplayVerifier = verifyPlayerSnapshotV1Replay,
  ) {}

  async observe(payload: Uint8Array): Promise<PlayerSnapshotV1ReplayObservation> {
    if (!this.runnerPath) return 'disabled'
    try {
      const result: unknown = await this.verifier(payload, { runnerPath: this.runnerPath })
      return isVerifiedReplayResult(result, payload) ? 'observed' : 'disabled'
    } catch {
      return 'disabled'
    }
  }
}

/** Runtime configuration is opt-in: only a directly injected absolute path enables observation. */
export function playerSnapshotV1ReplayObserverFromEnvironment(
  env: NodeJS.ProcessEnv,
  verifier: PlayerSnapshotV1ReplayVerifier = verifyPlayerSnapshotV1Replay,
): PlayerSnapshotV1ReplayObserver {
  return new PlayerSnapshotV1ReplayObserver(configuredRunnerPath(env), verifier)
}
