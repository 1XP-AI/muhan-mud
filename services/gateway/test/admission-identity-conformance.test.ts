import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import fixture from '../../../tests/fixtures/admission_identity_conformance_v1.json' with { type: 'json' }
import { createAdmissionTicket } from '../src/admission-ticket.js'
import { CharacterAuthorizationError } from '../src/character-authorizer.js'
import { GatewayEvidenceFinalizer, EvidenceFinalizationError, type EvidenceFinalizerRpcTransport } from '../src/evidence-finalizer.js'
import { OnboardingControlLineParser, formatOnboardingEvidenceControl } from '../src/onboarding-protocol.js'
import { decodeLegacyIdentityEvidenceV1, lowerHexToBytes, type LegacyIdentityEvidenceV1 } from '../src/evidence-codec/legacy-identity-evidence-v1.js'

const oracle = process.env.ADMISSION_IDENTITY_CONFORMANCE_C_ORACLE

function runOracle(...args: string[]): { status: number | null, stdout: string } {
  assert.ok(oracle, 'the C conformance oracle must be supplied by the harness')
  const result = spawnSync(oracle, args, { encoding: 'utf8' })
  assert.equal(result.error, undefined)
  return { status: result.status, stdout: result.stdout.trimEnd() }
}

function request(evidence: LegacyIdentityEvidenceV1) {
  return {
    actorUserId: fixture.actorUserId,
    correlationId: fixture.correlationId,
    characterId: fixture.characterId,
    mode: 'claim' as const,
    worldId: fixture.worldId,
    evidence,
  }
}

test('session-bound ticket preserves the DB session and gateway in C', {
  skip: !oracle && 'requires C oracle',
}, () => {
  const binding={sessionId:'123e4567-e89b-12d3-a456-426614174088',gatewayInstanceId:'gateway-test-1'}
  const input={actorUserId:fixture.actorUserId,characterId:fixture.characterId,
    legacyNameKey:fixture.canonicalName,nowMs:fixture.nowMs,jwtExpiresAtMs:fixture.nowMs+20000,...binding}
  const wire=createAdmissionTicket(input,fixture.secret,{randomBytes:()=>Buffer.from(fixture.nonceHex,'hex')}).toString('ascii')
  assert.ok(wire.startsWith('MUD2|'))
  assert.deepEqual(runOracle('ticket-bound',fixture.secret,wire),{status:0,stdout:`bound|${binding.sessionId}|${binding.gatewayInstanceId}`})
  assert.deepEqual(runOracle('ticket-twice',fixture.secret,wire),{status:0,stdout:'accepted|rejected'})
  for(const changed of [wire.replace(binding.sessionId,binding.sessionId.slice(0,-1)+'9'),wire.replace(binding.gatewayInstanceId,'gateway-test-2')])
    assert.deepEqual(runOracle('ticket',fixture.secret,changed),{status:0,stdout:'rejected'})
  assert.throws(()=>createAdmissionTicket({...input,gatewayInstanceId:undefined},fixture.secret))
  assert.throws(()=>createAdmissionTicket({...input,gatewayInstanceId:'bad|gateway'},fixture.secret))
  const maximum=createAdmissionTicket({...input,legacyNameKey:'Abcdefghijkl',gatewayInstanceId:'g'.repeat(128)},fixture.secret,{randomBytes:()=>Buffer.alloc(16,3)}).toString('ascii')
  assert.ok(Buffer.byteLength(maximum)>256 && Buffer.byteLength(maximum)<=384)
  assert.deepEqual(runOracle('ticket-bound',fixture.secret,maximum),{status:0,stdout:`bound|${binding.sessionId}|${'g'.repeat(128)}`})
  assert.throws(()=>createAdmissionTicket({...input,gatewayInstanceId:'g'.repeat(129)},fixture.secret))
  const legacy=createAdmissionTicket({...input,sessionId:undefined,gatewayInstanceId:undefined},fixture.secret).toString('ascii')
  assert.deepEqual(runOracle('ticket-bound',fixture.secret,legacy),{status:0,stdout:'bound||'})
})

function row(evidence: LegacyIdentityEvidenceV1, extra: Record<string, unknown> = {}) {
  return [{
    character_id: fixture.characterId,
    actor_user_id: fixture.actorUserId,
    mode: 'claim',
    lifecycle: 'handoff_pending',
    world_id: fixture.worldId,
    canonical_legacy_name: evidence.canonicalName,
    legacy_shard: evidence.legacyShard,
    player_file_sha256: evidence.playerFileSha256,
    evidence_version: fixture.version,
    storage_format: evidence.storageFormat,
    recorded_at: '2026-09-21T00:00:00.000Z',
    ...extra,
  }]
}

test('CI invokes admission identity conformance through the C-oracle harness', async () => {
  const [workflow, harness] = await Promise.all([
    readFile(new URL('../../../.github/workflows/ci.yml', import.meta.url), 'utf8'),
    readFile(new URL('../../../scripts/run-admission-identity-conformance.sh', import.meta.url), 'utf8'),
  ])

  assert.match(workflow,
    /- name: Admission identity C\/Gateway conformance[\s\S]*?\.\/scripts\/run-admission-identity-conformance\.sh/)
  assert.match(harness,
    /ADMISSION_IDENTITY_CONFORMANCE_C_ORACLE="\$tmp\/oracle"\s*\\\n\s*pnpm --dir "\$root" --filter @muhan\/gateway exec tsx --test\s*\\\n\s*test\/admission-identity-conformance\.test\.ts/)
})

test('C trusted admission and Gateway evidence finalization conform to the shared identity fixture', {
  skip: oracle ? false : 'run scripts/run-admission-identity-conformance.sh to build the C oracle',
}, async () => {
  const ticket = createAdmissionTicket({
    actorUserId: fixture.actorUserId,
    characterId: fixture.characterId,
    legacyNameKey: fixture.canonicalName,
    nowMs: fixture.nowMs,
    jwtExpiresAtMs: fixture.nowMs + 20_000,
  }, fixture.secret, { randomBytes: () => Buffer.from(fixture.nonceHex, 'hex') })

  assert.deepEqual(runOracle('ticket', fixture.secret, ticket.toString('ascii')), {
    status: 0,
    stdout: `accepted|${fixture.canonicalName}`,
  }, 'the C trusted-admission parser accepts Gateway canonical ticket bytes')
  assert.deepEqual(runOracle('ticket-twice', fixture.secret, ticket.toString('ascii')), {
    status: 0,
    stdout: 'accepted|rejected',
  }, 'C consumes the Gateway nonce once and rejects a replay')
  assert.deepEqual(runOracle('ticket-name', fixture.secret, fixture.aliasNameHex), {
    status: 0,
    stdout: 'rejected',
  }, 'C rejects a correctly signed alias ticket')
  assert.deepEqual(runOracle('ticket', fixture.secret, 'MUD1|not-a-ticket'), {
    status: 0,
    stdout: 'rejected',
  }, 'C rejects malformed trusted-admission input')
  assert.throws(() => createAdmissionTicket({
    actorUserId: fixture.actorUserId,
    characterId: fixture.characterId,
    legacyNameKey: fixture.aliasName,
    nowMs: fixture.nowMs,
    jwtExpiresAtMs: fixture.nowMs + 20_000,
  }, fixture.secret, { randomBytes: () => Buffer.from(fixture.nonceHex, 'hex') }), CharacterAuthorizationError)

  const emitted = runOracle('emit-evidence', fixture.canonicalName, fixture.playerFileSha256)
  assert.equal(emitted.status, 0, 'C must emit evidence only for the durable file SHA')
  const parser = new OnboardingControlLineParser({ evidenceEnabled: true })
  const controls = parser.push(`${emitted.stdout}\n`)
  assert.equal(controls.length, 1)
  assert.equal(controls[0]?.type, 'EVIDENCE')
  if (controls[0]?.type !== 'EVIDENCE') throw new Error('unreachable')
  const evidence = controls[0].evidence
  assert.deepEqual(evidence, {
    outcome: 'ok', canonicalization: 'canonical', canonicalName: fixture.canonicalName,
    legacyShard: fixture.legacyShard, playerFileSha256: fixture.playerFileSha256,
    storageFormat: fixture.storageFormat,
  }, 'Gateway decodes the exact C canonical name, shard, and legacy-file SHA tuple')
  assert.equal(formatOnboardingEvidenceControl(evidence).toString('ascii').trimEnd(), emitted.stdout,
    'Gateway re-encodes the checked C evidence record byte-for-byte')
  assert.deepEqual(runOracle('evidence', fixture.malformedEvidenceWireHex), { status: 1, stdout: '' },
    'C rejects malformed evidence wire bytes')
  assert.throws(() => new OnboardingControlLineParser({ evidenceEnabled: true }).push('MUD1O EVIDENCE|1|00\n'),
    /invalid onboarding protocol message/)
  assert.deepEqual(runOracle('emit-evidence', fixture.canonicalName, fixture.mismatchPlayerFileSha256), { status: 1, stdout: '' },
    'C refuses to emit evidence when the known SHA conflicts with the durable player file')

  const calls: Readonly<Record<string, unknown>>[] = []
  const matchedTransport: EvidenceFinalizerRpcTransport = {
    async call(_name, parameters) { calls.push(parameters); return row(evidence) }
  }
  await new GatewayEvidenceFinalizer(matchedTransport).finalize(request(evidence))
  assert.deepEqual(calls, [{
    p_actor_user_id: fixture.actorUserId,
    p_correlation_id: fixture.correlationId,
    p_character_id: fixture.characterId,
    p_outcome: 'ok',
    p_canonical_legacy_name: fixture.canonicalName,
    p_player_file_sha256: fixture.playerFileSha256,
    p_evidence_version: fixture.version,
    p_storage_format: fixture.storageFormat,
    p_legacy_shard: fixture.legacyShard,
  }], 'Gateway passes only the C-decoded identity tuple to its authorizer boundary')

  const activeRetry: EvidenceFinalizerRpcTransport = {
    async call() { return row(evidence, { lifecycle: 'active' }) }
  }
  await new GatewayEvidenceFinalizer(activeRetry).finalize(request(evidence))
  for (const conflict of [
    { canonical_legacy_name: fixture.aliasName },
    { player_file_sha256: fixture.mismatchPlayerFileSha256 },
  ]) {
    const conflictingTransport: EvidenceFinalizerRpcTransport = {
      async call() { return row(evidence, conflict) }
    }
    await assert.rejects(() => new GatewayEvidenceFinalizer(conflictingTransport).finalize(request(evidence)), EvidenceFinalizationError)
  }

  assert.throws(() => decodeLegacyIdentityEvidenceV1(lowerHexToBytes(fixture.malformedEvidenceWireHex)))
})
