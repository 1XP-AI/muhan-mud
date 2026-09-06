import assert from 'node:assert/strict'
import test from 'node:test'

import { LEGACY_GAME_PASSWORD_DISCLOSURE } from './onboarding-disclosure.ts'

test('the onboarding disclosure states the legacy game-password storage and web-password boundary exactly', () => {
  assert.equal(
    LEGACY_GAME_PASSWORD_DISCLOSURE,
    '레거시 게임 비밀번호는 현재 레거시 플레이어 파일에 저장되며, 웹 로그인 비밀번호와 반드시 달라야 합니다.',
  )
})
