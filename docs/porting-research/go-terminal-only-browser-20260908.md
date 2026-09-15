# Go 중앙 xterm 브라우저 경계 — 2026-09-08

## 이번 검증

현재 웹 진입점은 별도 웹 회원가입/로그인 UI 없이 `ClassicTerminal` 하나만 렌더링한다.
기본 focus는 xterm 입력 textarea에 두고, window focus 복귀 때 선택/한글 조합 중이
아니면 다시 터미널로 돌린다. `MUD_GO_GATEWAY_URL`이 없으면 주소 미설정 문구만
표시하며 Supabase Auth/REST를 호출하지 않는다.

로컬에서 다음 명령을 실행했다.

```text
pnpm test:browser
```

결과는 기존 브라우저 설정을 현재 UI에 맞춘 두 case 모두 GREEN이다.

- `classic-terminal.spec.ts`: fake Gateway WebSocket에 `{"type":"line","text":"Alice"}`를
  보내고 view 응답을 렌더링하는 line protocol/focus 회귀.
- `mud-portal.feature-off.spec.ts`: 중앙 terminal 단일 화면, web account input/button 부재,
  `/auth/v1`·`/rest/v1` 요청 0건, 서버 주소 미설정 화면.
- 각 case는 Chromium 1 worker에서 통과했다.

Playwright CLI headed 세션에서도 `http://127.0.0.1:3312`를 열어 중앙 blue xterm과
active `Terminal input`을 snapshot/screenshot으로 확인했다. 산출물은
`output/playwright/terminal-only-local.png`에 있다.

## 이 증거가 보장하지 않는 것

- fake WebSocket case는 실제 Go 서버·PostgreSQL·가입/로그인 데이터를 사용하지 않는다.
- 실제 Go+PG 가입→캐릭터 생성→게임 명령→재접속 브라우저 E2E는 아직 남아 있다.
- Playwright Chromium은 실제 iOS/Android IME가 아니다. 한글 조합, 모바일 키보드,
  회전/visual viewport, 선택·복사·스크롤백 인수는 실제 기기에서 추가 검증해야 한다.
- 이전 Supabase 웹 계정/온보딩 테스트 파일은 역사적 자산으로 남아 있지만,
  active Playwright config는 중앙 xterm case만 선택한다. 이것은 기존 웹 계정을
  게임 계정으로 자동 연동했다는 뜻이 아니다.
