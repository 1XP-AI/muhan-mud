# 실제 Go + PostgreSQL + 브라우저 E2E 하네스

2026-09-08 · 로컬 검증용 · 운영 DB/원격/배포를 사용하지 않음

## 범위

새 `tests/browser-e2e/playwright.go-process-postgres.config.ts`는 다음 경계를
한 번에 연결한다.

1. 전용 PostgreSQL DSN에 `mud_go` 스키마를 만들고, 정확한 `browser-e2e` 월드와
   `BrowserAlice` 캐릭터만 초기화한다.
2. 실제 `server/cmd/muhan` Go 프로세스를 `-race`로 빌드·실행한다.
3. 기존 Next 개발 서버를 실제 Go WebSocket 주소로 연결한다.
4. Playwright Chromium이 터미널에서 캐릭터 생성 → 월드 입장 → `봐` → 페이지
   새로고침 → 같은 이름/암호 재로그인을 수행한다.

테스트는 가짜 WebSocket, 웹 회원가입 화면, Supabase Auth를 사용하지 않는다.
비밀번호는 브라우저 출력과 테스트 결과에 나타나지 않는지 확인한다.

## 실행 전제

`MUHAN_BROWSER_DATABASE_URL`은 이 하네스 전용의 폐기 가능한 PostgreSQL DB여야
한다. 하네스가 정확한 `browser-e2e` 월드와 `BrowserAlice` 계정만 삭제 후 다시
만들므로 운영 DB나 다른 테스트 DB를 지정하면 안 된다. 직접 config를 실행할 때는
PostgreSQL 준비/정리를 호출자가 책임진다.

Node/npm(`npx`)과 Playwright Chromium, Go 툴체인, `curl`, `pnpm`이 필요하다.

```bash
cd /Users/jjangg96/Documents/1xp/muhan-mud.nosync
MUHAN_BROWSER_DATABASE_URL='postgresql://user:password@127.0.0.1:5432/muhan_browser_e2e?sslmode=disable' \
  pnpm exec playwright test \
  --config=tests/browser-e2e/playwright.go-process-postgres.config.ts
```

포트가 충돌하면 `MUHAN_BROWSER_PORT`와 `MUHAN_GO_PORT`를 서로 다른 빈 포트로
지정한다. 반복 실행은 같은 전용 DB에서 가능하며, 캐릭터와 월드는 매번 초기화된다.

Docker가 설치된 ARM64 로컬 호스트에서는 다음 명시적 opt-in 래퍼가 로컬에 이미
있는 PostgreSQL 17-alpine 하나만 임시로 만들고 위 Playwright 실행을 연결한다.

```bash
cd /Users/jjangg96/Documents/1xp/muhan-mud.nosync
bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable
```

래퍼는 `postgres:17-alpine` 이미지가 이미 로컬에 있을 때만 실행하고 암묵적 pull을
하지 않는다. 고유한 컨테이너명·loopback 임시 포트·tmpfs를 사용하며, 종료 trap은
자신이 만든 정확한 컨테이너만 제거한다. 다른 컨테이너·볼륨·캐시·네트워크는
조회하거나 삭제하지 않는다. `--allow-disposable` 없이는 실행하지 않는다.

## 이 작업에서 실행한 검증

2026-09-08 로컬 ARM64 Docker에서 다음 명령을 실제 실행했다.

```bash
bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable
```

결과: **1 passed (10.0s)**. 실제 PostgreSQL 17 ARM64 컨테이너, Go `-race` 서버,
Next 개발 서버, Chromium을 연결해 xterm 안에서 캐릭터 생성, 한글 IME 커밋 입력
(예/남/선/봐), 새 암호, 첫 방 입장, 페이지 재로드, 같은 이름/암호 재로그인과
`봐`를 수행했고 암호가 화면에 나타나지 않는 것도 확인했다. 테스트 뒤 소유한
PostgreSQL 컨테이너가 제거됐으며 다른 Docker 자원은 건드리지 않았다.

초기 실행에서 Go 모듈이 `server/` 아래에 있다는 하네스 빌드 경로 오류를 발견해
수정했고, 같은 래퍼를 다시 실행해 위 결과를 얻었다.

새 Go 명령의 컴파일과 Playwright 설정/스펙 목록 검증은 PostgreSQL 없이도 수행할
수 있다.

```bash
cd /Users/jjangg96/Documents/1xp/muhan-mud.nosync/server
go test ./cmd/muhan-browser-e2e

cd /Users/jjangg96/Documents/1xp/muhan-mud.nosync
MUHAN_BROWSER_DATABASE_URL='postgresql://unused' \
  pnpm exec playwright test --config=tests/browser-e2e/playwright.go-process-postgres.config.ts --list
```

위 `--list`는 DSN 문자열을 파싱하거나 DB에 접속하지 않는다. 실제 PostgreSQL과
브라우저를 함께 실행한 래퍼 경로의 G2 가입·입장·재로그인 인수는 통과했지만, 이는
전체 게임 기능 인수가 아니다.

## 의도적 제한

- 전용 DB 안의 정확한 fixture ID만 삭제한다. 데이터베이스 전체를 drop/reset하지
  않는다.
- `-templates`는 빈 임시 디렉터리다. 이 스모크는 방 표시·인증·PG 영속성 경계만
  검증하며, legacy NPC/object seed나 전체 방 corpus를 인수하지 않는다.
- 테스트는 compositionstart/update/end 및 input 이벤트를 재현해 한글 IME 커밋
  경계를 검사한다. 실제 OS IME 조합기·모바일 키보드·운영 WSS/Ingress는 아직
  별도 인수 대상이다.
- `player-tick 1h`로 자동 tick 간섭을 줄였을 뿐, 전체 legacy scheduler의 검증이
  아니다.
