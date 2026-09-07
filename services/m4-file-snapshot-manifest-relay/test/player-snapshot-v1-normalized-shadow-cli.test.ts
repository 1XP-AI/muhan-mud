import assert from 'node:assert/strict'
import { test } from 'node:test'
import { main, type NormalizedShadowCliDependencies } from '../src/player-snapshot-v1-normalized-shadow-cli.js'
import type { PlayerSnapshotV1ReceiptBoundArtifactEvidence } from '../src/player-snapshot-v1-artifact.js'

const env = {
  M4_NORMALIZED_SHADOW_OUTBOX_PATH: '/outbox',
  M4_NORMALIZED_SHADOW_DATABASE_URL: 'postgresql://mud_normalized_replay_reader_login@localhost/test',
  M4_NORMALIZED_SHADOW_PROJECTOR_PATH: '/opt/projector',
}
function fixture() {
  const calls: string[] = [], outputs: string[] = []
  const dependencies: NormalizedShadowCliDependencies = {
    loadArtifact: async () => { calls.push('load'); return {} as PlayerSnapshotV1ReceiptBoundArtifactEvidence },
    createReader: () => { calls.push('reader'); return { findByIdentity: async () => [], close: async () => { calls.push('close') } } },
    compare: async (_artifact, _reader, path) => { assert.equal(path, '/opt/projector'); calls.push('compare'); return 'MATCH' },
    writeStdout: value => outputs.push(value),
  }
  return { calls, outputs, dependencies }
}
test('requires explicit one-shot configuration before file or DB access', async () => {
  for (const args of [[], ['--watch'], ['--once', '--extra']]) {
    const f = fixture()
    assert.equal(await main(env, args, f.dependencies), 1)
    assert.deepEqual(f.calls, [])
    assert.equal(JSON.parse(f.outputs[0]!).classification, 'INVALID_INPUT')
  }
  for (const key of Object.keys(env)) {
    const f = fixture(), missing: NodeJS.ProcessEnv = { ...env }; delete missing[key]
    assert.equal(await main(missing, ['--once'], f.dependencies), 1)
    assert.deepEqual(f.calls, [])
  }
})
test('closes the reader before reporting one metadata-only comparison result', async () => {
  const f = fixture()
  assert.equal(await main(env, ['--once'], f.dependencies), 0)
  assert.deepEqual(f.calls, ['load', 'reader', 'compare', 'close'])
  assert.deepEqual(f.outputs, ['{"format":"player-snapshot-v1-normalized-shadow-comparison","version":"1","classification":"MATCH"}\n'])
})
test('never reports success after close fails or exposes raw exception text', async () => {
  const f = fixture()
  f.dependencies.createReader = () => ({ findByIdentity: async () => [], close: async () => { throw new Error('secret-db-detail') } })
  assert.equal(await main(env, ['--once'], f.dependencies), 1)
  assert.equal(JSON.parse(f.outputs[0]!).classification, 'CONNECTION_CLOSE_ERROR')
  assert.doesNotMatch(f.outputs.join(''), /secret-db-detail/)
})
test('closes on comparator errors and avoids DB construction on malformed input', async () => {
  const f = fixture()
  f.dependencies.compare = async () => { throw new Error('raw-error') }
  assert.equal(await main(env, ['--once'], f.dependencies), 1)
  assert.equal(f.calls.at(-1), 'close')
  assert.equal(JSON.parse(f.outputs[0]!).classification, 'COMPARISON_ERROR')
  const bad = fixture()
  bad.dependencies.loadArtifact = async () => { throw new Error('payload') }
  assert.equal(await main(env, ['--once'], bad.dependencies), 1)
  assert.deepEqual(bad.calls, [])
})
