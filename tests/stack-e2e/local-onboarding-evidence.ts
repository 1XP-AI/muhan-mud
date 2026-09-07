import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'

export interface LocalOnboardingIdentity {
  actorUserId: string
  correlationId: string
  characterId: string
  commandId: string
  mode: 'provision' | 'claim'
}

export async function readLocalOnboardingEvidence(root: string, identity: LocalOnboardingIdentity,
  read: (path: string) => Promise<string> = path => readFile(path, 'utf8')): Promise<string[]> {
  for (const id of [identity.actorUserId, identity.correlationId, identity.characterId, identity.commandId])
    assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
  assert(identity.mode === 'provision' || identity.mode === 'claim')
  const binding = await read(join(root, 'onboarding-activation-bindings', `${identity.commandId}.binding`))
  const expected = `actor_user_id=${identity.actorUserId}\ncorrelation_id=${identity.correlationId}\ncharacter_id=${identity.characterId}\nmode=${identity.mode}\ncommand_id=${identity.commandId}\n`
  assert(binding === expected, 'local activation binding must match the exact database identity')
  // onboarding_receipt.h defines the reconciliation receipt for provision
  // only; both modes durably bind ACTIVATED in onboarding_activation_binding.c.
  if (identity.mode === 'claim') return [binding]
  const receipt = await read(join(root, 'onboarding-receipts', `${identity.correlationId}.receipt`))
  return [binding, receipt]
}
