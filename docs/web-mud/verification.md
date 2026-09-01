# Web MUD MVP 검증 기록

검증일: 2026-09-01  
범위: 로컬 코드·프로토콜·레거시 런타임 경계  
결과: 통과(실제 testnet-1xp 배포 검증은 별도 인프라 저장소에서 수행)

## 자동 검사

저장소 루트의 `pnpm check`가 통과했다.

- web/gateway TypeScript 검사: 통과
- gateway 단위·통합 테스트: 8/8 통과
- Next.js 16.3.4 production build: 통과, `/`, `/_not-found`, `/icon.svg` 정적 생성
- gateway production TypeScript build: 통과

게이트웨이 테스트는 JWT claim 경계, production 설정 거부 조건, exact origin, auth-first relay, 분할 UTF-8, Telnet echo 협상, 알 수 없는 옵션 거절, subnegotiation 제거를 포함한다.

`docker compose config --quiet`와 두 컨테이너 shell script의 `sh -n` 검사도 통과했다. 이 워크스테이션에는 Docker API socket이 없어 이미지 build/up과 컨테이너 내부 healthcheck는 실행하지 못했다.

## 실제 프로세스 검사

게이트웨이를 `NODE_ENV=test`, test-only auth bypass로 실제 기동하고 다음 응답을 확인했다.

```json
{"status":"ok","activeConnections":0}
```

기존 C 서버는 체크아웃의 플레이어·방 파일을 오염시키지 않도록 `/tmp`의 일회용 복제본에서 Apple clang으로 빌드하고 실행했다. 기존 `session_smoke.py`가 새 캐릭터 생성, UTF-8 코드포인트 백스페이스, 저장, 두 번째 접속의 재로그인을 통과했다. 레거시 선언 관련 컴파일 warning은 남지만 link와 실행은 성공했다. 빌드가 다시 쓴 추적 바이너리 `src/frp.new`는 검증 뒤 원래 Git 버전으로 복구했다.

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

1. 실제 Supabase 프로젝트에 migration 적용 및 private Realtime RLS 확인
2. Supabase 이메일 확인/Site URL과 Vercel production URL 연동
3. `web/` Vercel production 배포와 CSP/WSS 확인
4. 영속 디스크 호스트에서 Docker image build, volume seed, backup/restore 검증
5. production TLS proxy 뒤 exact origin과 `X-Forwarded-Proto` 신뢰 경계 확인
6. 재시작 후 플레이어 보존, JWT 만료, 느린 브라우저, drain canary

절차와 rollback은 각각 `supabase-setup.md`, `vercel-deployment.md`, `runtime-deployment.md`에 기록되어 있다.
