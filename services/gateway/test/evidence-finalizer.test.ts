import assert from 'node:assert/strict'
import test from 'node:test'
import { readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { GatewayEvidenceFinalizer, EvidenceFinalizationError, type EvidenceFinalizerRpcTransport } from '../src/evidence-finalizer.js'
import { decodeLegacyIdentityEvidenceV1, encodeLegacyIdentityEvidenceV1, type LegacyIdentityEvidenceV1 } from '../src/evidence-codec/legacy-identity-evidence-v1.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const correlation = '123e4567-e89b-12d3-a456-426614174001'
const character = '123e4567-e89b-12d3-a456-426614174002'
const evidence = (): LegacyIdentityEvidenceV1 => decodeLegacyIdentityEvidenceV1(encodeLegacyIdentityEvidenceV1({
  outcome: 'ok', canonicalization: 'normalized', canonicalName: 'Legacyhero', legacyShard: '35',
  playerFileSha256: 'f'.repeat(64), storageFormat: 'player-v1'
}))
const request = () => ({ actorUserId: actor, correlationId: correlation, characterId: character, mode: 'claim' as const, worldId: 'muhan', evidence: evidence() })
const row = (extra: Record<string, unknown> = {}) => [{
  character_id: character, actor_user_id: actor, mode: 'claim', lifecycle: 'active', world_id: 'muhan',
  canonical_legacy_name: 'Legacyhero', legacy_shard: '35', player_file_sha256: 'f'.repeat(64),
  evidence_version: 1, storage_format: 'player-v1', recorded_at: '2026-09-21T00:00:00.000Z', ...extra
}]

test('uses only the full shard-aware RPC with the exact decoded V1 metadata tuple', async () => {
  const calls: Array<{ name: string, parameters: Readonly<Record<string, unknown>> }> = []
  const transport: EvidenceFinalizerRpcTransport = {
    async call(name, parameters) { calls.push({ name, parameters }); return row() }
  }

  await new GatewayEvidenceFinalizer(transport).finalize(request())

  assert.deepEqual(calls, [{
    name: 'finalize_game_character_legacy_identity_evidence',
    parameters: {
      p_actor_user_id: actor,
      p_correlation_id: correlation,
      p_character_id: character,
      p_outcome: 'ok',
      p_canonical_legacy_name: 'Legacyhero',
      p_player_file_sha256: 'f'.repeat(64),
      p_evidence_version: 1,
      p_storage_format: 'player-v1',
      p_legacy_shard: '35'
    }
  }])
})

test('rejects invalid actor, correlation, or character UUIDs before calling transport', async () => {
  const invalidRequests = [
    { actorUserId: 'not-a-uuid' },
    { correlationId: '123E4567-e89b-12d3-a456-426614174001' },
    { characterId: '123e4567-e89b-12d3-a456-42661417400' }
  ]
  let calls = 0
  const transport: EvidenceFinalizerRpcTransport = {
    async call() { calls += 1; return row() }
  }
  const finalizer = new GatewayEvidenceFinalizer(transport)

  for (const invalidRequest of invalidRequests) {
    await assert.rejects(() => finalizer.finalize({ ...request(), ...invalidRequest }), EvidenceFinalizationError)
  }
  assert.equal(calls, 0)
})

test('rejects a decoded non-ok evidence object before calling transport', async () => {
  const decodedNonOkEvidence = decodeLegacyIdentityEvidenceV1(encodeLegacyIdentityEvidenceV1({
    outcome: 'not_found', canonicalization: 'canonical', canonicalName: 'Legacyhero', legacyShard: '35',
    playerFileSha256: '', storageFormat: 'player-v1'
  }))
  let calls = 0
  const transport: EvidenceFinalizerRpcTransport = {
    async call() { calls += 1; return row() }
  }

  await assert.rejects(
    () => new GatewayEvidenceFinalizer(transport).finalize({ ...request(), evidence: decodedNonOkEvidence }),
    EvidenceFinalizationError
  )
  assert.equal(calls, 0)
})

test('rejects malformed, indeterminate, extra, and mismatched full-RPC responses', async () => {
  const cases: unknown[] = [
    undefined,
    [],
    [row()[0], row()[0]],
    [{ ...row()[0], unexpected: true }],
    [{ character_id: character }],
    row({ actor_user_id: '123e4567-e89b-12d3-a456-426614174099' }),
    row({ character_id: '123e4567-e89b-12d3-a456-426614174099' }),
    row({ mode: 'provision' }),
    row({ lifecycle: 'provisioning' }),
    row({ world_id: 'other-world' }),
    row({ canonical_legacy_name: 'Otherhero' }),
    row({ legacy_shard: 'aa' }),
    row({ player_file_sha256: 'e'.repeat(64) }),
    row({ evidence_version: 2 }),
    row({ storage_format: 'player-v2' }),
    row({ recorded_at: 'not-a-timestamp' })
  ]
  for (const response of cases) {
    const transport: EvidenceFinalizerRpcTransport = { async call() { return response } }
    await assert.rejects(() => new GatewayEvidenceFinalizer(transport).finalize(request()), EvidenceFinalizationError)
  }
  const rejectedTransport: EvidenceFinalizerRpcTransport = { async call() { throw new Error('network unknown') } }
  await assert.rejects(() => new GatewayEvidenceFinalizer(rejectedTransport).finalize(request()), EvidenceFinalizationError)
})

test('sends exactly decoded metadata, never raw evidence or credential and terminal payload fields', async () => {
  const source = await readFile(fileURLToPath(new URL('../src/evidence-finalizer.ts', import.meta.url)), 'utf8')
  assert.doesNotMatch(source, /\b(?:Uint8Array|Buffer|process\.stdout|console\.)\b/)
  const calls: Array<Readonly<Record<string, unknown>>> = []
  const transport: EvidenceFinalizerRpcTransport = { async call(_name, parameters) { calls.push(parameters); return row() } }
  const requestWithSensitiveExtras = Object.assign(request(), {
    rawEvidenceWire: 'raw-evidence-wire', password: 'password', ticket: 'ticket', jwt: 'jwt',
    terminalPayload: 'terminal-payload', gamePayload: 'game-payload'
  })
  await new GatewayEvidenceFinalizer(transport).finalize(requestWithSensitiveExtras)
  assert.deepEqual(Object.keys(calls[0]!).sort(), [
    'p_actor_user_id', 'p_canonical_legacy_name', 'p_character_id', 'p_correlation_id', 'p_evidence_version',
    'p_legacy_shard', 'p_outcome', 'p_player_file_sha256', 'p_storage_format'
  ])
  for (const forbiddenKey of ['rawEvidenceWire', 'password', 'ticket', 'jwt', 'terminalPayload', 'gamePayload']) {
    assert.equal(forbiddenKey in calls[0]!, false)
  }
})
