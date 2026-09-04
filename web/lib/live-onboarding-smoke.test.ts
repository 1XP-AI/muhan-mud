import assert from 'node:assert/strict'
import test from 'node:test'

import {
  LIVE_ONBOARDING_SMOKE_APPROVAL,
  readLiveOnboardingSmokeConfig,
} from './live-onboarding-smoke.ts'

const fixtures = {
  MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL: 'https://muhan.example.test',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL: 'provision@smoke.test',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD: 'provision-web-password',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME: 'Provisionone',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER: '남',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS: '4',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS: '12 10 12 10 10',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON: '1',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT: '선',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE: '7',
  MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD: 'provision-game-password',
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL: 'claim@smoke.test',
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD: 'claim-web-password',
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME: 'Claimone',
  MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD: 'claim-legacy-password',
}

const approvals = {
  MUHAN_LIVE_ONBOARDING_SMOKE_DURABLE_DATA_APPROVAL: LIVE_ONBOARDING_SMOKE_APPROVAL,
  MUHAN_LIVE_ONBOARDING_SMOKE_GLOBAL_UNIQUENESS_APPROVAL:
    'confirmed-global-uniqueness',
}

test('live onboarding smoke is skipped by default', () => {
  const result = readLiveOnboardingSmokeConfig({})

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.match(result.skipReason, /MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED=true/)
})

test('live onboarding smoke stays skipped when enabled without every pre-created fixture', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL: fixtures.MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL,
  })

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.deepEqual(result.missing, [
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_EMAIL',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEB_PASSWORD',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CHARACTER_NAME',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GENDER',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_CLASS',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_WEAPON',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_ALIGNMENT',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_RACE',
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD',
    'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL',
    'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_WEB_PASSWORD',
    'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME',
    'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_LEGACY_PASSWORD',
  ])
  assert.match(result.skipReason, /pre-created fixture input/i)
})

test('live onboarding smoke requires a distinct operator approval before durable work', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...fixtures,
  })

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.match(result.skipReason, /operator approval/i)
})

test('live onboarding smoke requires an explicit operator attestation that global fixture uniqueness was checked', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    MUHAN_LIVE_ONBOARDING_SMOKE_DURABLE_DATA_APPROVAL: LIVE_ONBOARDING_SMOKE_APPROVAL,
    ...fixtures,
  })

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.match(result.skipReason, /global.*uniqueness/i)
})

test('live onboarding smoke accepts only explicit enablement, fixtures, and approval', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...approvals,
    ...fixtures,
  })

  assert.equal(result.shouldRun, true)
  assert.deepEqual(result.missing, [])
  assert.deepEqual(result.config, {
    baseUrl: 'https://muhan.example.test',
    provision: {
      email: 'provision@smoke.test',
      webPassword: 'provision-web-password',
      characterName: 'Provisionone',
      gender: '남',
      characterClass: '4',
      stats: '12 10 12 10 10',
      weapon: '1',
      alignment: '선',
      race: '7',
      gamePassword: 'provision-game-password',
    },
    claim: {
      email: 'claim@smoke.test',
      webPassword: 'claim-web-password',
      characterName: 'Claimone',
      legacyPassword: 'claim-legacy-password',
    },
  })
})

test('live onboarding smoke rejects non-HTTPS targets and does not become runnable', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...approvals,
    ...fixtures,
    MUHAN_LIVE_ONBOARDING_SMOKE_BASE_URL: 'http://muhan.example.test',
  })

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.match(result.skipReason, /https/i)
})

test('live onboarding smoke rejects provision and claim fixtures that reuse the same web account', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...approvals,
    ...fixtures,
    MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL: 'PROVISION@SMOKE.TEST',
  })

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.deepEqual(result.invalid, [
    'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_EMAIL',
  ])
  assert.match(result.skipReason, /different web accounts/i)
})

test('live onboarding smoke rejects provision and claim fixtures that reuse the same character name', () => {
  const result = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...approvals,
    ...fixtures,
    MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME: 'Provisionone',
  })

  assert.equal(result.config, null)
  assert.equal(result.shouldRun, false)
  assert.deepEqual(result.invalid, [
    'MUHAN_LIVE_ONBOARDING_SMOKE_CLAIM_CHARACTER_NAME',
  ])
  assert.match(result.skipReason, /different character names/i)
})

test('live onboarding smoke rejects placeholder and malformed operator fixture inputs without exposing values', () => {
  const placeholder = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...approvals,
    ...fixtures,
    MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD: '<operator-supplied-secret>',
  })
  const malformed = readLiveOnboardingSmokeConfig({
    MUHAN_LIVE_ONBOARDING_SMOKE_ENABLED: 'true',
    ...approvals,
    ...fixtures,
    MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS: '12 10\n12 10 10',
  })

  assert.equal(placeholder.config, null)
  assert.deepEqual(placeholder.invalid, [
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_GAME_PASSWORD',
  ])
  assert.equal(malformed.config, null)
  assert.deepEqual(malformed.invalid, [
    'MUHAN_LIVE_ONBOARDING_SMOKE_PROVISION_STATS',
  ])
  assert.match(placeholder.skipReason, /invalid fixture value/i)
  assert.doesNotMatch(placeholder.skipReason, /operator-supplied-secret/)
})
