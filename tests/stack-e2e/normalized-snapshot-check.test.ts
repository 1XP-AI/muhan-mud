import assert from 'node:assert/strict'
import test from 'node:test'
import { assertOnboardingNormalizedSnapshot } from './normalized-snapshot-check.js'
import type { PlayerSnapshotV1ReceiptBoundArtifactEvidence } from '../../services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-artifact.js'

// Dependency tests cover orchestration/cleanup, not production SQL or parsing.
const artifact = {} as PlayerSnapshotV1ReceiptBoundArtifactEvidence
for (const outcome of ['MATCH', 'MISSING_RECORD', 'throw']) {
  test(`onboarding normalized comparison closes its reader after ${outcome}`, async () => {
    const calls: string[] = []
    const reader = { findByIdentity: async () => [], close: async () => { calls.push('close') } }
    const operation = assertOnboardingNormalizedSnapshot(artifact, 'reader-url', '/projector', {
      createReader(url) { assert.equal(url, 'reader-url'); calls.push('open'); return reader },
      async compare(value, actualReader, runnerPath) {
        assert.equal(value, artifact); assert.equal(actualReader, reader); assert.equal(runnerPath, '/projector')
        calls.push('compare')
        if (outcome === 'throw') throw new Error('comparison failed')
        return outcome
      },
    })
    if (outcome === 'MATCH') await operation
    else await assert.rejects(operation)
    assert.deepEqual(calls, ['open', 'compare', 'close'])
  })
}
