# Web MUD MVP 검증 기록

검증일: 2026-09-01  
범위: 로컬 코드·프로토콜·레거시 런타임 경계  
결과: 통과(실제 testnet-1xp 배포 검증은 별도 인프라 저장소에서 수행)

## 자동 검사

저장소 루트의 `pnpm check`가 통과했다.

- web/gateway TypeScript 검사: 통과
- gateway 단위·통합 테스트: 9/9 통과
- Next.js 16.3.4 production build: 통과, `/`, `/_not-found`, `/icon.svg` 정적 생성
- gateway production TypeScript build: 통과

게이트웨이 테스트는 JWT claim 경계, production 설정 거부 조건, exact origin, auth-first relay, 분할 UTF-8, Telnet echo 협상, 알 수 없는 옵션 거절, subnegotiation 제거를 포함한다.

`docker compose config --quiet`와 두 컨테이너 shell script의 `sh -n` 검사도 통과했다. 이 워크스테이션에는 Docker API socket이 없어 이미지 build/up과 컨테이너 내부 healthcheck는 실행하지 못했다.

## 실제 프로세스 검사

게이트웨이를 `NODE_ENV=test`, test-only auth bypass로 실제 기동하고 다음 응답을 확인했다.

```json
{"status":"ok","activeConnections":0}
```

기존 `session_smoke.py`는 첫 `[엔터]`를 보내지 않아 입력 단계가 한 칸씩
밀렸고, 출력 길이만 확인해 실제 캐릭터 생성·재로그인 없이도 통과할 수 있음을
확인했다. 이 검사는 호환성·기동을 보는 약한 legacy smoke gate로 CI에 유지한다.
릴리스 증거는 실제 C 서버를 disposable `MUHAN_HOME`에서 실행하는
`tests/harness/run_scenario.py` deterministic scenario를 사용한다.
wrapper는 기본적으로 현재 소스에서 `/tmp` 전용 바이너리를 강제 재빌드하며,
결과 JSON에 실행 바이너리의 절대경로·SHA-256·`current-source` provenance를
기록한다. 별도 sanitizer/전용 바이너리를 시험할 때만
`AI_SCENARIO_ALLOW_EXTERNAL_BINARY=1 FRP_BIN=/path/to/frp.new`를 명시적으로
지정할 수 있고, 이 경우 provenance는 `external`로 기록된다.

새 시나리오는 prompt 기반으로 첫 Enter, 고정 캐릭터 `타봇` 생성, 도움말,
건강, 저장, 종료와 재로그인을 검증했다. 저장 결과는 `player/04/타봇`, 1956
bytes였으며 SHA-256을 JSON 증거에 기록했다. 이후 파일을 truncate한 세 번째
접속은 신규 생성으로 빠지지 않고 `corrupt_player_rejected=true`로 차단됐다.
scenario 결과 JSON은 transcript event와 raw base64뿐 아니라 실패 진단의 server
log tail까지 중앙 redaction하고, 업로드 직전에 전체 artifact에 비밀번호가 없는지
self-check한다. 따라서 CI가 업로드하는 릴리스 증거는 이 deterministic scenario
JSON 하나이며 테스트 비밀번호가 남지 않는다.
PlayerStore unit, FileStore contract, runtime path, serializer failure-injection
test도 모두 통과했다. disconnect 저장 실패 snapshot은 bounded recovery queue가
소유하고 성공할 때까지 초당 재시도하며, 그동안 신규 캐릭터 로그인과 stale
duplicate reload를 차단하는 contract도 검증했다. 이 queue는 프로세스 생존
동안의 RAM 복구 경계이며 crash-safe durable spool은 후속 단계다. 레거시
non-prototype warning은 남지만 full link와 실행은 성공했다.

같은 일회용 C 서버에 실제 gateway를 연결한 뒤 `muhan.v1` WebSocket 클라이언트가 다음을 확인했다.

- 첫 text auth frame 전송
- gateway `ready` control 수신
- 실제 C MUD 환영 출력 136 bytes를 binary frame으로 수신
- 세션 종료 뒤 gateway drain

## 브라우저 검사

Playwright로 1440×1000 desktop과 390×844 mobile에서 로그인 관문과 인증 후 xterm.js 셸을 직접 렌더링했다. 확인 항목은 인증 tab/label 접근성, responsive 재배치, 상태 계기판, 터미널 focus/input, 재접속 상태였다.

첫 검사에서 xterm.js의 부모 높이가 제한되지 않아 full page가 약 7,000px까지 늘어나는 결함을 발견했다. `game-shell`/`gate-layout`/`terminal-stack`에 명시적인 viewport 높이와 overflow 경계를 적용한 뒤 두 해상도 모두 한 화면 안에 터미널과 명령 입력줄이 유지되는 것을 재확인했다. favicon 404도 `app/icon.svg`로 제거했다.

가짜 Supabase 좌표와 의도적으로 닫힌 gateway를 사용한 인증 후 UI 검사의 DNS/WebSocket 오류는 실패 상태 표시를 보기 위한 테스트 조건이며 production build 오류가 아니다.

## 실제 배포에서 남은 확인

외부 자격 증명과 프로젝트 설정이 필요한 아래 항목은 이 로컬 검증에 포함되지 않는다.

1. self-hosted Supabase에 `game_characters`/claim migration과 RLS 구현·검증
2. Gateway ownership 검사와 Gateway→MUD trusted admission 구현·검증
3. 영속 볼륨의 player inventory/backfill, snapshot과 backup/restore rehearsal
4. `testnet-1xp` chart 배포 뒤 private MUD port와 network policy 확인
5. production TLS proxy 뒤 exact origin과 `X-Forwarded-Proto` 신뢰 경계 확인
6. 재시작 후 플레이어 보존, JWT 만료, 느린 브라우저, drain canary

절차와 rollback은 각각 `supabase-setup.md`, `vercel-deployment.md`, `runtime-deployment.md`에 기록되어 있다.
