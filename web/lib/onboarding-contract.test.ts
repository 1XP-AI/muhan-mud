import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createOnboardingAuthFrame,
  createOnboardingSocketUrl,
  createOnboardingSocketContract,
  decideOnboardingClose,
  decideOnboardingControl,
  shouldOpenGatewaySocket,
  shouldOpenOnboardingSocket,
  shouldReconnectOnboardingClose,
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

test('onboarding auth is exact and URL derives a path without leaking query data', () => {
  assert.deepEqual(createOnboardingAuthFrame('jwt', 'claim', '00000000-0000-4000-8000-000000000001'), {
    type: 'onboarding-auth', accessToken: 'jwt', mode: 'claim',
    correlationId: '00000000-0000-4000-8000-000000000001',
  })
  assert.equal(createOnboardingSocketUrl('wss://gateway.example/ws?ticket=secret'), 'wss://gateway.example/onboarding')
})

test('onboarding reconnect is bounded and never retries a normal close', () => {
  assert.equal(shouldReconnectOnboardingClose(1000, 0), false)
  assert.equal(shouldReconnectOnboardingClose(1002, 0), false)
  assert.equal(shouldReconnectOnboardingClose(1011, 0), true)
  assert.equal(shouldReconnectOnboardingClose(1011, 3), false)
})

test('provisioned control validates the UUID and records a successful completion', () => {
  assert.deepEqual(decideOnboardingControl('provision', { type: 'provisioned', characterId: '00000000-0000-4000-8000-000000000002' }), {
    kind: 'provisioned',
    characterId: '00000000-0000-4000-8000-000000000002',
  })
  assert.deepEqual(decideOnboardingControl('provision', { type: 'provisioned', characterId: 'not-a-uuid' }), {
    kind: 'failure',
  })
  assert.deepEqual(decideOnboardingControl('provision', { type: 'claimed', characterId: '00000000-0000-4000-8000-000000000002' }), {
    kind: 'failure',
  })
  assert.deepEqual(decideOnboardingControl('provision', { type: 'closed' }, 'ready'), {
    kind: 'terminated',
  })
})

test('onboarding error text is display-only and does not become a failed lifecycle', () => {
  assert.deepEqual(decideOnboardingControl('provision', {
    type: 'error',
    reason: 'temporary upstream failure',
  }, 'ready'), {
    kind: 'error',
    detail: 'temporary upstream failure',
  })
})

test('onboarding close decisions terminate completed or failed flows and reconnect transient drops', () => {
  assert.equal(decideOnboardingClose('ready', 1000, 0, false), 'terminate')
  assert.equal(decideOnboardingClose('provisioned', 1000, 0, false), 'terminate')
  assert.equal(decideOnboardingClose('provisioned', 1011, 0, false), 'terminate')
  assert.equal(decideOnboardingClose('ready', 1011, 0, false), 'reconnect')
  assert.equal(decideOnboardingClose('connecting', 1011, 3, false), 'terminate')
  assert.equal(decideOnboardingClose('provisioned', 1008, 0, true), 'terminate')
  assert.equal(decideOnboardingClose('ready', 1012, 0, false), 'reconnect')
  assert.equal(decideOnboardingClose('ready', 1013, 0, false), 'reconnect')
  assert.equal(decideOnboardingClose('ready', 1001, 0, false), 'reconnect')
  assert.equal(decideOnboardingClose('ready', 1006, 0, false), 'reconnect')
})
