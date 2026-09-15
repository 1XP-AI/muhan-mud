import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createOnboardingAuthFrame,
  createOnboardingSocketUrl,
  createOnboardingSocketContract,
  decideOnboardingClose,
  decideOnboardingControl,
  recoveryFromOnboardingClose,
  recoveryFromOnboardingControl,
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
  assert.equal(shouldOpenGatewaySocket('empty', '11111111-1111-4111-8111-111111111111', [{ id: '11111111-1111-4111-8111-111111111111', lifecycle: 'active' }]), false)
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

test('browser-visible claim failure maps to credential recovery without retaining Gateway text', () => {
  assert.deepEqual(decideOnboardingControl('provision', {
    type: 'error',
    reason: 'onboarding authentication failed',
  }, 'ready'), {
    kind: 'error',
    recovery: {
      category: 'session',
      title: '웹 로그인 확인이 필요합니다.',
      detail: '웹 로그인 세션을 다시 확인한 뒤 캐릭터 목록을 새로고침하고 다시 시도해 주세요.',
    },
  })

  const claim = recoveryFromOnboardingControl('claim', {
    type: 'error', reason: 'onboarding failed',
  })
  assert.equal(claim.category, 'legacy-credentials')
  assert.match(claim.detail, /기존 캐릭터 이름과 게임 비밀번호/)
})

test('state, transport, and unknown recovery categories never echo raw diagnostics', () => {
  const stateChanged = recoveryFromOnboardingControl('provision', {
    type: 'error', reason: 'onboarding failed',
  })
  assert.equal(stateChanged.category, 'state-changed')

  assert.equal(recoveryFromOnboardingClose(4001).category, 'session')
  assert.equal(recoveryFromOnboardingClose(1012).category, 'unavailable')

  const secret = 'password=not-for-display ticket=abc sha256=deadbeef internal-id=42'
  const unknown = recoveryFromOnboardingControl('claim', {
    type: 'error', reason: secret,
  })
  assert.equal(unknown.category, 'unknown')
  assert.doesNotMatch(`${unknown.title} ${unknown.detail}`, /password=|ticket=|sha256=|internal-id=/)
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
