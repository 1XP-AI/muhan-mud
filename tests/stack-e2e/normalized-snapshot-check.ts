import assert from 'node:assert/strict'
import type { PlayerSnapshotV1ReceiptBoundArtifactEvidence } from '../../services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-artifact.js'
import { projectPlayerSnapshotV1Normalized } from '../../services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-normalized-projection.js'
import { PostgresNormalizedProjectionSessionReader } from '../../services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-normalized-projection-session.js'
import { comparePlayerSnapshotV1NormalizedProjectionShadow, type ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader } from '../../services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-normalized-projection-shadow-comparator.js'

interface Reader extends ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader { close(): Promise<void> }
interface Dependencies {
  createReader(url: string): Reader
  compare(artifact: PlayerSnapshotV1ReceiptBoundArtifactEvidence, reader: Reader, runnerPath: string): Promise<string>
}
const production: Dependencies = {
  createReader: url => new PostgresNormalizedProjectionSessionReader(url),
  compare: (artifact, reader, runnerPath) => comparePlayerSnapshotV1NormalizedProjectionShadow(artifact, reader, {
    project: (payload, snapshotSha256) => projectPlayerSnapshotV1Normalized(payload, { runnerPath, snapshotSha256 }),
  }),
}

/** Consume the real C receipt-bound artifact, never a separately seeded projection. */
export async function assertOnboardingNormalizedSnapshot(
  artifact: PlayerSnapshotV1ReceiptBoundArtifactEvidence,
  readerUrl: string,
  runnerPath: string,
  dependencies: Dependencies = production,
): Promise<void> {
  const reader = dependencies.createReader(readerUrl)
  try {
    assert.equal(await dependencies.compare(artifact, reader, runnerPath), 'MATCH',
      'C onboarding snapshot must exactly match its persisted normalized projection')
  } finally { await reader.close() }
}
