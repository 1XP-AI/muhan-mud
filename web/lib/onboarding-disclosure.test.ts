import assert from 'node:assert/strict'
import test from 'node:test'

import { LEGACY_GAME_PASSWORD_DISCLOSURE } from './onboarding-disclosure.ts'

test('the onboarding disclosure states the legacy game-password storage and web-password boundary exactly', () => {
  assert.equal(
    LEGACY_GAME_PASSWORD_DISCLOSURE,
    'The legacy game password is currently stored in the legacy player file and must differ from the web-login password.',
  )
})
