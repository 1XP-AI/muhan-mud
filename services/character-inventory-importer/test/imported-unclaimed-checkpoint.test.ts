import assert from 'node:assert/strict'
import test from 'node:test'
import {
  ImportedUnclaimedCheckpointError,
  checkpointImportedUnclaimedCandidate,
  createImportedUnclaimedCheckpointPlan,
  resumeImportedUnclaimedCandidates,
} from '../src/imported-unclaimed-checkpoint.js'
import type { ImportedUnclaimedCheckpointPlan } from '../src/imported-unclaimed-checkpoint.js'
import type { ParsedImportedUnclaimedManifest } from '../src/imported-unclaimed-manifest.js'
import { expectedShard } from '../src/inventory.js'

const digest = (character: string): string => character.repeat(64)

function candidate(legacyNameKey: string, legacyShard: string, sourceSha256: string, sourceSize: number) {
  return { legacyNameKey, legacyShard, sourceSha256, sourceSize }
}

function validCandidate(legacyNameKey: string, sourceSha256: string, sourceSize: number) {
  return candidate(legacyNameKey, expectedShard(legacyNameKey), sourceSha256, sourceSize)
}

function manifest(candidates: ParsedImportedUnclaimedManifest['candidates']): ParsedImportedUnclaimedManifest {
  return { sourceManifestSha256: digest('f'), candidates }
}

function forgedPlan(candidates: readonly unknown[], extra: Record<string, unknown> = {}): ImportedUnclaimedCheckpointPlan {
  return { sourceManifestSha256: digest('f'), candidates, ...extra } as unknown as ImportedUnclaimedCheckpointPlan
}

function assertRejected(action: () => unknown): void {
  assert.throws(action, (error: unknown) => error instanceof ImportedUnclaimedCheckpointError
    && error.code === 'invalid_imported_unclaimed_checkpoint'
    && error.message === 'invalid imported-unclaimed checkpoint')
}

test('normalizes a manifest into canonical-identity order and binds cursor checkpoints to its raw fingerprint', () => {
  const plan = createImportedUnclaimedCheckpointPlan(manifest([
    validCandidate('Carol', digest('c'), 3),
    validCandidate('Alice', digest('a'), 1),
    validCandidate('Bob', digest('b'), 2),
  ]))

  assert.deepEqual(plan.candidates.map((value) => value.legacyNameKey), ['Alice', 'Bob', 'Carol'])
  assert.equal(plan.sourceManifestSha256, digest('f'))
  const checkpoint = checkpointImportedUnclaimedCandidate(plan, 'Bob')
  assert.deepEqual(checkpoint, {
    sourceManifestSha256: digest('f'),
    cursor: 1,
    legacyNameKey: 'Bob',
  })
})

test('resumes strictly after a persisted ordered cursor with neither skips nor duplicates', () => {
  const plan = createImportedUnclaimedCheckpointPlan(manifest([
    validCandidate('Carol', digest('c'), 3),
    validCandidate('Alice', digest('a'), 1),
    validCandidate('Bob', digest('b'), 2),
  ]))
  const firstPass = resumeImportedUnclaimedCandidates(plan)
  const checkpoint = checkpointImportedUnclaimedCandidate(plan, firstPass[1]!.legacyNameKey)
  const resumed = resumeImportedUnclaimedCandidates(plan, checkpoint)

  assert.deepEqual(firstPass.map((value) => value.legacyNameKey), ['Alice', 'Bob', 'Carol'])
  assert.deepEqual(resumed.map((value) => value.legacyNameKey), ['Carol'])
  assert.equal(new Set([...firstPass.slice(0, 2), ...resumed].map((value) => value.legacyNameKey)).size, 3)
})

test('rejects a fingerprint mismatch and unknown or invalid persisted cursors', () => {
  const plan = createImportedUnclaimedCheckpointPlan(manifest([
    validCandidate('Alice', digest('a'), 1),
    validCandidate('Bob', digest('b'), 2),
  ]))

  assertRejected(() => resumeImportedUnclaimedCandidates(plan, {
    sourceManifestSha256: digest('e'), cursor: 0, legacyNameKey: 'Alice',
  }))
  assertRejected(() => resumeImportedUnclaimedCandidates(plan, {
    sourceManifestSha256: digest('f'), cursor: 0, legacyNameKey: 'Bob',
  }))
  assertRejected(() => resumeImportedUnclaimedCandidates(plan, {
    sourceManifestSha256: digest('f'), cursor: 2, legacyNameKey: 'Carol',
  }))
  assertRejected(() => resumeImportedUnclaimedCandidates(plan, {
    sourceManifestSha256: digest('f'), cursor: -1, legacyNameKey: 'Alice',
  }))
  assertRejected(() => checkpointImportedUnclaimedCandidate(plan, 'Carol'))
})

test('rejects duplicate canonical identities and malformed noncanonical candidate metadata before a plan is created', () => {
  assertRejected(() => createImportedUnclaimedCheckpointPlan(manifest([
    validCandidate('Alice', digest('a'), 1),
    validCandidate('Alice', digest('b'), 2),
  ])))
  assertRejected(() => createImportedUnclaimedCheckpointPlan(manifest([
    validCandidate('alice', digest('a'), 1),
  ])))
})

test('checkpoint and resume reject forged plan-shaped candidates before using any cursor or candidate', () => {
  const sourcePathCandidate = {
    ...validCandidate('Alice', digest('a'), 1),
    sourcePath: 'player/unsafe/Alice',
  }
  const cases = [
    forgedPlan([validCandidate('Alice', digest('a'), -1)]),
    forgedPlan([validCandidate('Alice', digest('a'), 1), validCandidate('Alice', digest('b'), 2)]),
    forgedPlan([validCandidate('alice', digest('a'), 1)]),
    forgedPlan([validCandidate('../Alice', digest('a'), 1)]),
    forgedPlan([validCandidate('Bob', digest('b'), 2), validCandidate('Alice', digest('a'), 1)]),
    forgedPlan([validCandidate('Alice', digest('a'), 1)], { sourcePath: 'manifest/unsafe.json' }),
    forgedPlan([sourcePathCandidate]),
    // Validation must include candidates after the target identity as well.
    forgedPlan([validCandidate('Alice', digest('a'), 1), validCandidate('Bob', digest('b'), -1)]),
  ]

  for (const plan of cases) {
    assertRejected(() => checkpointImportedUnclaimedCandidate(plan, 'Alice'))
    assertRejected(() => resumeImportedUnclaimedCandidates(plan))
  }
})
