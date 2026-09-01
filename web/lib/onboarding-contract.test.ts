import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createOnboardingSocketContract,
  shouldOpenGatewaySocket,
  shouldOpenOnboardingSocket,
} from './onboarding-contract.ts'

test('an empty roster may choose either onboarding mode', () => {
  for (const mode of ['provision', 'claim'] as const) {
    assert.equal(shouldOpenOnboardingSocket('empty', mode), true)
    assert.deepEqual(createOnboardingSocketContract('empty', mode), {
      kind: 'onboarding', path: '/onboarding', subprotocol: 'muhan.onboarding.v1', mode,
    })
  }
})

test('onboarding contract cannot become an active MUD session before ready', () => {
  assert.equal(shouldOpenOnboardingSocket('ready', 'provision'), false)
  assert.equal(createOnboardingSocketContract('ready', 'claim'), null)
  assert.equal(shouldOpenGatewaySocket('empty', null, []), false)
  assert.equal(shouldOpenGatewaySocket('empty', '11111111-1111-4111-8111-111111111111', ['11111111-1111-4111-8111-111111111111']), false)
})
