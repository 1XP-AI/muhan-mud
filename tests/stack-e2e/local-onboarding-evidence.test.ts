import assert from 'node:assert/strict'
import test from 'node:test'
import { readLocalOnboardingEvidence, type LocalOnboardingIdentity } from './local-onboarding-evidence.js'

const identity: LocalOnboardingIdentity = {
  actorUserId: '10000000-0000-4000-8000-000000000001',
  correlationId: '20000000-0000-4000-8000-000000000001',
  characterId: '30000000-0000-4000-8000-000000000001',
  commandId: '40000000-0000-4000-8000-000000000001', mode: 'claim',
}
const text = (id: LocalOnboardingIdentity) => `actor_user_id=${id.actorUserId}\ncorrelation_id=${id.correlationId}\ncharacter_id=${id.characterId}\nmode=${id.mode}\ncommand_id=${id.commandId}\n`
test('claim requires exact command binding, not a provision-only receipt', async () => {
  const paths: string[] = []
  const result = await readLocalOnboardingEvidence('/fixture', identity, async path => {
    paths.push(path)
    assert.equal(path, `/fixture/onboarding-activation-bindings/${identity.commandId}.binding`)
    return text(identity)
  })
  assert.deepEqual(result, [text(identity)])
  assert.equal(paths.length, 1)
})
test('provision still requires its separate reconciliation receipt', async () => {
  const id = { ...identity, mode: 'provision' as const }
  const result = await readLocalOnboardingEvidence('/fixture', id, async path => path.endsWith('.binding') ? text(id) : 'receipt')
  assert.deepEqual(result, [text(id), 'receipt'])
  await assert.rejects(readLocalOnboardingEvidence('/fixture', id, async path => {
    if (path.endsWith('.binding')) return text(id)
    throw new Error('missing receipt')
  }), /missing receipt/)
})
for (const field of ['actorUserId', 'correlationId', 'characterId', 'commandId', 'mode'] as const)
  test(`rejects substituted ${field}`, async () => {
    const altered = { ...identity, [field]: field === 'mode' ? 'provision' : 'ffffffff-ffff-4fff-8fff-ffffffffffff' } as LocalOnboardingIdentity
    await assert.rejects(readLocalOnboardingEvidence('/fixture', identity, async () => text(altered)), /exact database identity/)
  })
test('rejects extra content, missing binding, and unsafe file identity', async () => {
  await assert.rejects(readLocalOnboardingEvidence('/fixture', identity, async () => text(identity) + 'unexpected\n'))
  await assert.rejects(readLocalOnboardingEvidence('/fixture', identity, async () => { throw new Error('missing binding') }), /missing binding/)
  await assert.rejects(readLocalOnboardingEvidence('/fixture', { ...identity, commandId: '../outside' }, async () => { throw new Error('must not read') }), error => error instanceof assert.AssertionError)
})
