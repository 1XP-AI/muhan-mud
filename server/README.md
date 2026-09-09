# Go MUD 서버 작업 영역

상태: 2026-09-08 누적 Go 수직 슬라이스 진행 중. `cmd/muhan`은 가입/로그인과
방향/함정/NPC 이동 receipt를 포함한 로컬 실행 경로가 있지만 전체 게임 명령·전투·
tick·브라우저/배포 인수는 아직 완료되지 않았다.

최신 slice: 원작 `잡담`/`잡`/`환호` 전역 명령을 terminal parser와 durable world receipt로
연결했다. daily/HP는 Supabase 권위 상태에 저장하고, descriptor cooldown과 global admission
cooldown은 runtime에만 둔다. PNOBRD/PNOBR2 수신 거부를 존중하며 첫 commit만 전역 event를
fan-out하고 replay는 재방송하지 않는다. 전체 C 명령 parity를 완료했다는 뜻은 아니다.

검증 비용도 분리했다. `fast`는 현재 작업 트리의 영향 패키지만 race 검사하며, 깨끗한
트리의 직전 커밋을 자동 반복하지 않는다(`GO_FAST_COMMIT=1`/`GO_FAST_BASE`는 명시적
재검증용). `integration`은 ARM64 build 없는 조립 gate, `main`은 기본 브랜치에서만
ARM64 cross-build를 포함한 최종 Go gate다. 실제 PG/browser/release matrix는 기능 레인에서
반복하지 않는다.
수동 GitHub `fast` job은 clean checkout에서 실수로 skip되지 않도록 depth 2와
`GO_FAST_COMMIT=1`을 사용하며, 여전히 마지막 커밋의 표적 패키지만 검사한다.
`release` 수동 scope도 기본 브랜치 전용이며, feature branch에서 잘못 선택하면
`.github/workflows/ci.yml`의 `release-scope-guard`가 DB·호환성 matrix 시작 전에
중단한다.
실행 기준은 `../docs/porting-research/go-server-execution-plan.md`다.
문서 아래쪽의 Astra/Terra 표기는 과거 조사 기록이며 현재 실행 정책은 모든 하위
작업을 Luna max로 배치하는 것이다.

최신 명령 slice: `시간`은 clock-bound read-only receipt, `정보`는 canonical player
상태의 첫 페이지 통계 receipt, `도움말`/`환영`은 UTF-8 legacy 문서 receipt,
`외쳐`·`표현`·`보아 <대상>`·bounded `action.c` 감정표현은 state/event receipt까지
`WorldConnector`에 연결했다. 모두 state purity와 동일 command ID replay를 검증했지만,
전체 C command table·continuation/info title·prefix/occurrence 출력 동등성의 완료를
의미하지 않는다.
중앙 xterm 브라우저 smoke는 별도 문서와
`pnpm test:browser`에서 검증한다. 실제 Go+PostgreSQL 게임 경계도
`bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable`로
ARM64 PostgreSQL 17, Go `-race`, Chromium을 함께 실행해 가입→월드 입장→재로그인→
`봐`, `환영`, `도움말 정보`, `표현`, `외쳐`까지 **1 passed (10.1s)**를 확인했다. 이는 전체 게임 기능·모바일/WSS·testnet
인수를 뜻하지 않는다.

## 로컬 검증

이 디렉터리에서 실행한다. Go 1.27.1 툴체인이 필요하며 `go.mod`에 고정했다.

```sh
# 병렬 레인: 변경 경로에 영향받는 패키지만 빠르게 검증
GO_FAST_RUN='Title|RangerPray' scripts/run-go-validation.sh fast
# 명령/문서만 바뀐 경우에는 자동으로 Go 검사를 건너뛴다.
# 필요할 때만 전체 런타임 표적을 명시한다.
GO_FAST_PACKAGES=all scripts/run-go-validation.sh fast
# 깨끗한 작업 트리의 마지막 커밋을 의도적으로 다시 검사할 때만 사용
GO_FAST_COMMIT=1 scripts/run-go-validation.sh fast

# 조립된 batch: 전체 Go race/vet만 필요할 때 한 번 (ARM64 cross-build 없음)
scripts/run-go-validation.sh integration

# 실제 main 병합 시에만 한 번: 위 gate + Linux ARM64 cross-build
scripts/run-go-validation.sh main

# 영속성 명령 batch: disposable PG에서 한 번만 receipt/replay 확인
MUHAN_BOUNDED_LANES_TEST_DATABASE_URL='postgresql://...' \
  go test -race ./internal/session -run TestPostgresBoundedLanesPersistAndReplay -count=1
```

레인마다 전체 race·vet·ARM64 build·PostgreSQL를 반복하지 않는다. `fast`는 world 변경 시
session/transport 소비자까지, session 변경 시 transport까지 포함하고 transport-only 변경은
transport만 검사한다. `server/cmd/muhan/` 변경은 `./cmd/muhan`도 포함해 flag·scheduler·
listener wiring을 확인하고, browser E2E helper 변경은 해당 helper 패키지만 추가한다.
영속성 변경이 포함된
batch의 PG receipt 테스트는 하나의 격리 PostgreSQL에서 한 번만 묶어 실행하고, ARM64
이미지/Helm·x64/Windows/macOS 호환·브라우저 IME/mobile·복구 검증은 승인된 통합 또는
릴리스 gate에서만 실행한다. 전체 Go 검증은 `integration`에서만 필요할 때 실행하고,
Linux ARM64 cross-build는 `main`에서만 실행한다. 예전 `merge` 이름은 모호한 고비용
실행을 막기 위해 스크립트가 거부한다. 엄격 corpus 예외 63건의 상태를 숨기지 않는다.

첫 TDD 기록: `NewInput` 미정의로 테스트 실패를 확인한 뒤 구현했다.
한글 테스트의 입력 한도를 문자 수가 아닌 바이트 수로 바로잡았다.
로컬 unit/race 통과, 3초 fuzz 468,484회 실행 통과를 확인했다.
`go vet ./...`와 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...`도
통과했다. 현재 패키지의 교차 컴파일이지 실행 서버 이미지 검증은 아니다.
이는 프레이밍 단위 검증이며 게임 기능 동등성, 실제 IME, 모바일 키보드,
DB 통합 또는 배포 검증을 의미하지 않는다.

## 입력 계약

`internal/terminal.Input`은 연결 하나의 직렬 수신 루프에서 사용한다.
UTF-8 스트림을 CR/LF/CRLF로 나누고 코드포인트 단위 Backspace를 처리한다.
길이 초과·잘못된 UTF-8·지원하지 않는 제어 문자는 해당 줄 전체를 버린다.
화살표 키 등 escape 시퀀스 편집은 아직 지원하지 않는다. 이후 UI/세션 계약에서
명시적으로 구현해야 하며 이 프레이머만으로 완성된 터미널 편집기를 주장하지 않는다.

`Feed`는 유효한 완료 줄과 일반 오류를 동시에 반환할 수 있다. 호출자는 둘 다
처리해야 한다. 입력을 echo하지 않으며 비밀번호 표시 여부는 세션 계층의 책임이다.
반환 문자열/호출자 원본 버퍼까지 자동 제거하지 않으므로 입력 로그를 남기지 않는다.
연결 종료 시 `Reset`으로 미완료 입력을 버리고 새 연결에 재사용하지 않는다.

다음 구현: 원본 가입/명령 계약 확정, 월드 로딩과 look/이동, 게임 가입/저장 및
네트워크 세션 통합. C/Rust 호출을 런타임 의존성으로 추가하지 않는다.

원본 조사 시작점: `src/command1.c`의 `login`(494행), `create_ply`(1118행),
`parse`(1497행), `process_cmd`(1581행). `resources_utf8/src/`는 현재 `src/`와
내용/행 번호가 다르므로 최신 동작의 단독 근거로 사용하지 않는다.
`tests/scenarios/create_and_relogin.json`은 가입 후 도움말/건강/저장/끝을 실행하는
기존 시나리오이며 Go E2E로 연결하는 작업은 아직 남아 있다.

## 캐릭터 생성 규칙 — 2026-09-08

`internal/game`에 5개 능력치(각 3~18, 합계 54 이하), 직업/무기 선택,
8종족의 저장 ID 매핑과 능력치 보정, 초기 방/금화/무기 숙련도를 구현했다.
`Creation`은 레벨 초기화 전 draft이며 가입·저장·월드 입장 완료본이 아니다.

TDD: 타입/함수 미정의 실패 확인 후 구현했다. unit/race/vet 통과.
`TestCreationAgainstLegacyRaceSwitch`는 현재 `src/command1.c`의 종족 보정
switch와 `src/mtype.h`의 상수를 추출해 임시 C 실행 파일을 컴파일한다.
3개 능력치 조합 × 8종족 = 24개 비교가 실제 C 실행과 일치했다.
전체 가입 흐름이나 레벨 초기화의 동등성을 증명하는 테스트는 아니다.
로컬 `cc`가 없으면 이 비교만 skip되므로 완료 판정 전에 skip 여부를 확인한다.

의도적인 입력 차이: 원본 atoi의 숫자 뒤 쓰레기 허용 및 여섯 번째 이후 능력치
무시를 복제하지 않는다. 정확한 5개 정수만 받으며 잘못된 입력은 재입력 대상이다.
종족 보정 이후 값은 기본 능력치의 3~18 범위로 다시 잘라내지 않는다.

Docker 초기 확인 기록: `docker info`가 `linux/aarch64`로 성공했다. 이후 날짜가 있는
실행 기록에서 PostgreSQL 17 격리 컨테이너 기반 통합 검증을 추가했으며, 매 테스트의
전용 컨테이너만 정리하고 기존 Docker 리소스는 변경하지 않는다.

## 터미널 생성 질문 상태 머신

`CreationWizard`는 이름 확인 이후 성별→직업→능력치→무기→성향→종족을
처리한다. 잘못된 입력은 현재 단계에 머무르고 취소 시 선택값을 비운다.
완료된 draft만 반환하며 Ready는 암호/저장을 진행할 준비 상태이지 게임
접속 성공이 아니다. 암호는 이 객체에 보관하지 않는다.

`tests/scenarios/create_and_relogin.json`의 기존 선택값을 직접 읽어 Go 흐름에
적용하는 테스트가 통과했다. 미정의 실패 후 구현했고 unit/race/vet를 재검증했다.
프롬프트는 같은 질문/선택지를 제공하되 간격과 안내 문구를 정리했으므로
바이트 단위 출력 동등성을 주장하지 않는다. 숫자 선택 뒤 임의 문자열이나
한글 접두사의 일부 바이트만 일치하는 입력은 원본과 달리 거부한다.
이름 확인·암호 검증/해싱·원자적 DB 생성·레벨 초기화·실제 WSS 연결은 남아 있다.

## 이름과 PostgreSQL draft 저장

`internal/identity.CanonicalName`은 C의 이름 길이/금지 문자 및 ASCII 대소문자
규칙을 포팅했다. trim/Unicode 정규화를 추가하지 않는다. 예약 이름 정책과
기존 계정 이관 검증은 남아 있다. Luna가 실패 테스트→구현을 담당했다.

`internal/storage.Postgres`는 이름을 canonicalize하고 계정/캐릭터 draft를 한
트랜잭션으로 삽입한다. 별도 `mud_go` 스키마이며 기존 Supabase 테이블은 수정하지
않는다. draft는 아직 플레이 가능한 캐릭터가 아니다. hash 인자는 이미 해싱된
값을 받는 내부 계약이며 암호 해싱/로그인 API는 아직 구현하지 않았다.

로컬의 기존 `postgres:17-alpine` 이미지를 이용한 전용 컨테이너에서 실제 테스트:
생성/조회 일치, 같은 이름 중복 거부, 두 번째 INSERT 실패 시 계정까지 롤백을
확인했다. 테스트는 **빈 폐기용 DB**의 `MUHAN_TEST_DATABASE_URL`을 요구한다.
설정하지 않으면 PG 테스트는 skip되며 일반 unit 성공을 DB 검증으로 보고하지 않는다.
운영/Supabase DB나 기존 데이터가 있는 DB로 테스트를 실행하지 않는다.

드라이버는 [pgx stdlib](https://pkg.go.dev/github.com/jackc/pgx/v5/stdlib)이며
`go.mod`/`go.sum`에 버전을 고정했다. 재접속 인증, 암호 해싱, 가입 요청 재시도,
동시 생성, 버전 기반 게임 저장, Supabase 역할별 권한 검증은 후속 작업이다.

## 암호와 재로그인 — 2026-09-08

`identity.HashPassword`/`CheckPassword`와 `Postgres.Register`/`Authenticate`를
추가했다. [Go bcrypt 패키지](https://pkg.go.dev/golang.org/x/crypto/bcrypt)의
기본 cost 10을 사용하며 신규 암호는 원작의 3~14 **바이트** 범위를 따른다.
제어문자·잘못된 UTF-8은 거부한다. 기존 평문 레코드를 자동으로 허용하지 않는다.
기존 데이터 이관 및 향후 hash 버전/cost 변경 경로는 별도 작업이다.

실패 테스트부터 구현했다. 해시의 salt 차이, 정상/오류 암호, 길이 경계와
손상된 해시 거부를 검증한다. 실제 PostgreSQL 테스트에는 Register 후
다른 ASCII 대소문자로 Authenticate, 틀린 암호/없는 이름의 캐릭터 반환 금지,
평문 미저장을 추가했다. 이 API의 성공은 저장된 draft의 자격 검증일 뿐
게임 입장 완료가 아니다. 레벨 초기화, 제한된 인증 시도, 세션 소유권,
미존재 계정 검증 시간 차이 완화, WSS 연결은 구현·검증이 남아 있다.

## 연결별 가입/로그인 대화

`internal/session.Login`이 이름 조회→신규 확인→Enter→원작 선택 질문→암호 저장,
또는 기존 암호 검증을 연결한다. `View.Secret`은 다음 입력의 echo 금지를 뜻하며
출력에 원본 암호를 포함하지 않는다. 세 번째 실패/빈 암호는 접속 종료,
DB 오류·저장 결과 불확실은 자동 재시도나 입장 없이 접속 종료로 처리한다.
재접속 시 저장 결과를 이름/암호로 확인한다. 정교한 가입 요청 ID 재조정은 남아 있다.

단위 테스트의 미정의 실패 후 구현했고 전체 unit/race/vet를 통과했다.
실제 PostgreSQL 17에 `MUHAN_SESSION_TEST_DATABASE_URL`을 설정한 별도 테스트에서
가입 대화→세션 종료→같은 캐릭터 재로그인과 잘못된 암호 후 재시도를 확인했다.
이 DB는 storage 테스트 DB와 분리된 빈 폐기용 DB여야 한다.

`View.Verified`는 자격 확인된 draft다. 아직 월드 입장을 허용하는 결과가 아니다.
현재 구현에는 WSS 전송, 브라우저 echo 제어, 세션 중복 소유 방지, 월드 초기화가
없으므로 사용자 플레이 가능한 링크를 제공할 단계는 아니다.

## Go WebSocket 실행 경계

`cmd/muhan`이 `/ws`와 `/healthz`를 제공한다. `DATABASE_URL`과 정확한 웹 origin을
쉼표로 나열한 `ALLOWED_ORIGINS`가 필요하다. 기본 리슨 주소는 `127.0.0.1:8081`이며
`LISTEN_ADDR`로 바꾼다. 환경 변수 설정 후 빈 개발 DB에 `go run ./cmd/muhan -migrate`,
그 뒤 `go run ./cmd/muhan`으로 실행한다. 실제 운영 DB에 이 초기 draft 스키마를
적용하는 것은 아직 승인된 배포 단계가 아니다.

입력은 완성된 한 줄 `{"type":"line","text":"..."}`이고 출력은
`{"type":"view","text":"...","secret":false,"closed":false}`다.
클라이언트는 응답을 받은 뒤 다음 줄을 보내고 `secret`에 따라 echo를 제어해야 한다.
현재 raw xterm keystroke 프레이머와 이 line 프로토콜을 혼합해서 쓰지 않는다.
최대 프레임 2KiB/입력 512바이트, 동시 접속 32개, 로그인 연결 10분/입력 대기 2분,
DB 작업 및 출력 시간 제한을 둔다. origin은 전체 문자열로 사전 확인한다.

[coder/websocket](https://pkg.go.dev/github.com/coder/websocket)의 실제 접속 테스트로
이름 프롬프트→secret 전환→암호 검증과 origin 거부를 확인했다. unit/race/vet 통과.
별도 PostgreSQL에서 실행 파일의 migration과 서버 기동도 성공했고 `/healthz` 200,
잘못된 origin의 `/ws` 403을 확인했다. 실제 WSS TLS/Ingress 및 브라우저 연동은 미검증이다.
로그인 완료 시 월드 미구현 안내를 출력하고 종료한다. 이를 플레이 완료로 세지 않는다.

## 원본 방 헤더 읽기

`internal/world.DecodeLegacyRoomHeader`는 원본 ILP32 little-endian room 480바이트,
exit 44바이트 형식에서 이름·방 번호·출구·플래그·출구 타이머를 읽는다. EUC-KR을
UTF-8로 바꾸고 저장된 포인터는 사용하지 않는다. `resources_utf8/rooms`도 확인한
시작 방 파일은 EUC-KR이며 폴더 이름만으로 인코딩을 선택하지 않는다.

실패 테스트 후 구현했고 원본 `rooms/`의 3,216개 헤더 corpus 검증이 통과했다.
시작 방 이름 무한대전, 밑 출구 목적지 1001, 본문 시작 offset 528을 검증했다.
잘린 헤더/출구와 음수/과도한 개수를 거부한다. 1024 출구 제한은 Go decoder의
방어적 상한이며 원작 gameplay 제한으로 해석하지 않는다.

이 헤더 전용 API는 **본문을 읽지 않는다.** 아래 전체 디코더와 구분한다.
해석하고 검증하기 전 월드 로더로 사용하면 안 된다. 64비트/다른 인코딩의 변형도
자동 추정하지 않는다. 원작 load_rom은 파일 경로의 번호를 권위로 사용하므로 전체
로더에서는 header ID와 파일 ID의 불일치 정책도 검증해야 한다.

## 원본 방 본문 디코더 — 진행 중, corpus 인수 실패

`DecodeLegacyRoom`은 원본 `read_rom/read_crt/read_obj` 순서를 따라 몬스터,
재귀 아이템, 길이 접두사 설명 3개와 방의 리젠 타이머를 읽는다. object 352,
creature 1184바이트 ILP32 형식이다. 소켓·비밀번호·저장된 포인터는 가져오지 않는다.
게임 상태에 아직 연결하지 않은 이관 값이며, 원작 로드 시 HP/MP/내구도 clamp는
원본 수치 비교를 위해 여기서 적용하지 않는다. byte 필드의 signed 게임 해석도
해당 기능 포팅에서 별도로 검증한다.

2026-09-08 로컬 검증:

- Luna max의 독립 `clang -m32` 정적 검증에서 little-endian, long/pointer 4바이트,
  object 352 / creature 1184 / room 480 / exit 44와 주요 필드 오프셋을 확인했다.
- 실패 테스트 → 시작 방 설명, 중첩 아이템, NPC 수치/인벤토리,
  과도한 깊이·길이·음수 개수·잘림·추가 바이트 거부 테스트 통과.
- `go vet ./...` 통과. 3초 fuzz 1,158,027회 실행, panic 없음.
- **`go test -race ./...`는 world corpus 테스트에서 실패**. 다른 패키지는 통과했지만
  이번 실행은 DB 환경 변수를 지정하지 않아 실제 DB 테스트 재실행 증거가 아니다.
- 방 3,216개 중 3,153개 디코딩, 그 안의 몬스터 887개/방 직속 아이템 344개.
  나머지 63개는 미지원 문자/추가 데이터 등의 이유로 거절됐다. 전체 이전 통과가 아니다.
- 예: r00100 설명의 `c9 a6`는 디코더에서 대체 문자로 변환되어 거절된다.
  r00173은 파싱 완료 위치 751 뒤에도 몬스터/아이템처럼 보이는 바이트가 남는다.
  오래된 저장 잔여물인지는 원본 writer와 파일 이력으로 추가 확인해야 한다.

다음 작업은 63개를 원인별로 조사해 원본을 보존하는 명시적 변환 정책과 회귀
fixture를 추가하는 것이다. 테스트를 skip하거나 입력을 무음 절삭해 통과시키지 않는다.
월드 참조 검증·look/이동·실제 캐릭터 초기화·운영 로더 연결은 아직 남아 있다.

### 조사용 읽기와 엄격한 읽기 분리

`InspectLegacyRoom`은 원본 전체 복사본, SHA-256, 실제 소비 위치, 문제 필드의
시작 offset과 종류를 반환한다. 미정의 문자는 미리보기에서 대체 문자를 쓰고,
종료 문자가 없는 고정 문자열은 필드 경계까지만 읽는다. 문제를 해결하거나 승인한
결과가 아니며 게임 입장에는 쓰지 않는다. 원본에는 과거 메모리 바이트가 있으므로
로그/웹 출력에 노출하지 않는다. 잘린 레코드, 음수 개수, 잘못된 설명 길이는 계속 거절한다.

3,216개 전체 조사 통과: 63개 방에서 `invalid-euc-kr` 80건,
`missing-text-terminator` 13건, `trailing-data` 7건. 한 방에 여러 문제가 있을 수 있다.
문제가 없는 방은 엄격한 디코더 결과와 동일함을 비교하고, 문제가 있는 방은
엄격한 API가 계속 거절함을 검사했다. 이 결과가 전체 corpus 인수 실패를 해소하지 않는다.

Luna max 소스 조사: `src/files2.c`의 `resave_all_rom`은 O_TRUNC 없이 덮어쓰고
`src/files1.c`의 `write_rom`도 파일 길이를 줄이지 않는다. `read_rom`은 세 설명을
읽은 뒤 EOF 검사 없이 반환하고 호출자는 닫는다. 따라서 잔여 tail 발생 경로가 있으며
tail을 게임 데이터로 새로 해석해서는 안 된다. 이관에서는 원본 증거로 별도 보존한다.
개별 파일이 실제 어느 저장 경로로 생성됐는지까지 증명한 것은 아니다.

## 방 표시 포팅

`view.go`의 `RenderRoomEnvironment`는 `src/room.c:display_rom`의 환경 부분을
순수 UTF-8 출력으로 옮긴다. 방 이름의 뒤 공백, 설명 표시 옵션, 06~20시 야간
경계, 조명/종족/관리자 시야, 실명, 비밀/숨김/투명 출구 및 방향 도표를 다룬다.
시간과 다른 플레이어의 광원은 인자로 공급한다. ANSI와 줄바꿈 wire 변환은
터미널 어댑터 책임이며 현재 세션에는 아직 연결하지 않았다.

원본의 설명 포인터 없음과 길이 1인 빈 문자열을 구분하기 위해 디코더에
설명 존재 여부를 보존했다. 빈 설명은 한 줄을 출력하고 없는 설명은 출력하지 않는다.
`RenderRoomMonsters`는 인접한 동일 이름 묶음, 마지막 개체의 설명/성향,
숨김/투명과 광채 표시를 옮긴다. 원본의 중립 성향→푸른 광채도 그대로 비교한다.

`RenderRoomPlayers`는 안정적인 actor ID로 자신을 제외하고, 숨김·투명·DM 투명과
설명 표시 모드를 적용한다. 이름 비교로 자기 자신을 판정하지 않는다. 월드 루프가
공급하는 순서 있는 스냅샷을 받으며 실제 세션/플레이어 목록 연결은 남아 있다.

관련 실패→구현 테스트, race, vet 통과. 아이템·전투 표시, 대상 지정 보기,
월드 명령 연결이 남아 있으며 완성된 `look`으로 보고하지 않는다.

### 실제 C 환경 출력 비교

`view_legacy_test.go`는 `display_rom`의 환경 출력 블록을 원본 소스에서 추출하고
임시 C 실행 파일로 plain UTF-8 출력을 비교한다. ANSI는 비교에서 명시적으로 제외,
시간·광원은 fixture로 주입한다. Go 실행 파일에는 C/compiler 의존성이 없다.

Luna 초안의 bool 문자열→C atoi 불일치와 출구 플래그 복사 폭 오류를 부모 검토에서
발견해 수정했다. 다른 플레이어의 빛을 양쪽에 동일하게 입력하고 출력 블록 추출을
함수 경계에 고정했다. 수정 후 18개 사례(기본 8개+야간/광원/종족/직업 경계 9개+
빈 설명 1개)에서 C/Go 출력 일치를 실제 실행으로 확인했다. 관련 race와 vet도 통과.
이는 환경 출력 비교이며 전체 C 게임 동작 비교나 63개 데이터 이관 예외 해결은 아니다.

### 아이템 표시와 장면 조합

`ListRoomObjects`는 `list_obj/obj_str`의 인접 묶음, 숨김/투명/배경 아이템 제외,
마법 감지에 따른 보정치 분리와 주문 표시를 구현한다. 원본 ILP32 보정치 byte는
signed char로 해석한다. 원본과 달리 출력 중 이름을 수정하지 않는다.
기존 1970-byte 그룹 시작 제한을 유지하고 `Truncated`로 표시 제한을 알린다.
`RenderRoomObjects`의 조사는 `under_han`의 511-byte 제한/괄호 접미사 제거와
UTF-8 종성 규칙까지 명시적으로 비교 가능한 방식으로 옮겼다.

`RenderRoomScene`은 환경→플레이어→몬스터→아이템을 조합한다. 공통 시야 검사를
통과하지 못하면 인물/아이템을 출력하지 않는다. 관련 TDD/race 및 기존 18개 C 환경
비교 회귀, vet 통과. 아이템 부분은 아직 별도 C 실행 differential 증거가 없고
소스 기반 기대값 테스트만 있다. 전투 안내·대상 보기·게임 명령/세션 연결은 남아 있다.

## 이동 명령의 출구 선택/초기 제약

`SelectExit`는 `find_ext`의 이름 접두사/양수 순번 선택과 투명·XNOSEE 제약을
옮긴다. 비밀 출구는 표시되지 않아도 이름으로 선택할 수 있다는 원작 차이를
유지한다. 빈 이름과 0 이하 순번이 엉뚱한 출구를 선택하던 경로는 명시적으로 거절한다.

`ExitRestriction`은 `go()`의 이동 불가→전투→잠금→닫힘→비행→시간 제약 순서만
구현한다. 밤 전용 출구는 원작처럼 06시와 20시 경계에서도 허용된다. 관련 TDD,
race, vet 통과. 빈 제약 결과를 이동 허가로 해석하면 안 된다. 경비/NPC, 무게/성별,
등반/추락, 대기시간, 추가 자격, 목적지 입장, 추적/동행, 상태 저장은 후속 구현이다.

`EvaluateTraversal`에서 초기 제약→경비→소지 무게→성별→등반/하강 추락까지
결정론적 상태 전이로 확장했다. 난수 범위를 호출자에게 요청하며 잘못된 난수는
부분 결과 없이 오류로 반환한다. 경비는 표시 문자열 대신 인덱스를 반환한다.
치명상은 HP 0/Dead/Stop, 등반 추락은 피해와 Stop, 하강 추락은 피해 후 다음 단계
진행으로 구분한다. `< fall` 경계와 부양 시 난수 미사용을 원작과 맞췄다.
관련 TDD/race/vet 통과. 현재 FallSkill은 입력이며 장비/민첩 보너스 산출, 피해 저장,
사망 실행, 방송, 나머지 이동 단계는 아직 연결하지 않았다. 빈 제약/Stop=false는
최종 목적지 이동 승인이 아니다.

`DestinationRestriction`은 목적지 레벨→수용 인원→패거리 가입→소유 패거리→
혼인 사유지/초대 순서를 구현했다. 원본 `count_vis_ply`처럼 숨김/투명 플레이어도
인원에 세며 DM 투명만 제외한다. 직업별 레벨 우회 경계(9/10), 소유권 우회(12),
상한 비교와 메시지의 +1 차이, signed room level byte를 명시적으로 보존했다.
관련 TDD/race/vet 통과. 초대 여부는 권위 데이터에서 공급해야 하며 아직 초대 데이터
이관/조회가 연결되지 않았다. 실제 이동 시에는 동일한 월드 상태에서 인원 검사와
위치 변경을 원자적으로 수행해야 한다. 대기시간/은신/동행/저장과 실행 루프는 남아 있다.

`PlanMovement`로 출구 선택→초기 제약/경비/추락→대기시간→은신 판정/적대 NPC→
출발 방 추적 흔적→목적지 입장 검사를 하나의 순수 변경안 계산으로 연결했다.
현재 상태를 수정하지 않고 HP/은신/위치/추적 흔적/메시지와 사망·추락 결과를 반환한다.
실제 커밋은 아니다. 인원·입장 검사와 상태 적용은 이후 월드 트랜잭션에서 묶어야 한다.

검증: 하강 추락 피해 후 대기 거절, 목적지 레벨 거절에도 남는 추적 흔적/은신 해제,
은신 성공의 동일 난수 경계, 은신 실패 시 원작의 특이한 '투명인 적대자' 차단 조건,
고정 추적 흔적 유지, 목적지 스냅샷 불일치 거절 테스트/race/vet 통과.
장비/보너스 산출·실제 actor 상태·이동 방송·입장 효과/함정·동행·원자적 저장은 미연결이다.

## 문 상태와 입장 부수 효과

`OpenDoor/CloseDoor`는 선택된 출구에 대해 성공 시만 은신을 해제하고 문 상태를
바꾼다. 열기만 LastTime을 갱신하며 닫기는 갱신하지 않는다. 실패는 상태를 보존한다.
`RefreshDoors`는 `check_exits`처럼 마감 시각을 **초과**했을 때 자동 잠금/닫힘을
적용하며 원본 슬라이스를 수정하지 않는다. 저장된 32-bit 타이머는 넓혀 더한다.
관련 TDD/race, 이동/18개 환경 출력 비교 회귀, vet 통과. OpenDoor의 시간 입력은
현재 legacy DTO의 int32 범위이며 최종 런타임 타이머의 64-bit 전환은 별도 필요하다.

입장 소스 조사: `add_ply_rom`은 위치/방문 횟수→도착 방송→영구 NPC/아이템
재생성→문 갱신→화면 출력→이름순 플레이어 삽입/첫 입장 시 NPC 활성화 순서다.
`go`는 그 뒤 동행·추격을 처리하고 마지막에 함정을 검사한다. 위치 변경만으로
입장을 완성했다고 처리하면 안 된다. 재생성/활성 목록/동행/함정 및 실제 적용은 미완료다.

`PlanPermanentSpawns`는 NPC/아이템 공통 재생성 수량 단계를 구현한다. 영구 플래그가
있는 동일 이름의 현재 개체만 세며, 슬롯 ID가 달라도 이름이 같으면 원작처럼 같은
수량에 반영한다. 첫 슬롯은 마감 <= 현재, 묶는 뒤 슬롯은 < 현재라는 원작 경계를
보존했다. 요청된 생성은 전부 성공한다는 전제의 계획이며 실제 생성 적용은 아니다.
없는 템플릿은 부분 계획을 게시하지 않고 오류로 반환하는 명시적 차이가 있다.
시각/동명/영구 개체 집계/오버플로 경계 TDD 및 race/vet 통과. NPC 소지품·금화
난수, 아이템 enchant, 템플릿 로딩, 원자적 인스턴스 추가와 활성화 연결은 남아 있다.

`TemplateCatalog`와 고정 NPC/아이템 템플릿 디코더를 추가했다. 원본의
`objmon/mNN`(100×1184)·`oNN`(100×352) 파일에서 ID/100과 ID%100으로 읽는다.
방의 재귀 레코드와 혼합하지 않고 저장된 포인터/소지품 연결은 사용하지 않는다.
명시적 filesystem root를 받으며 C 프로세스 호출이나 공유 가변 캐시가 없다.
원본 ID 1의 노동자/단도 확인, 고정 형식 길이/음수 ID/테이블 누락/포인터 바이트
테스트와 방 조사 회귀/race/vet 통과. 전체 템플릿 corpus 호환성은 아직 미검증이다.
원작 load_crt의 회복 타이머(now,60초)는 원본 디코딩을 변형하지 않고 다음 실제
개체 생성 단계에서 적용해야 한다. 랜덤 장비/금화/강화와 월드 적용도 남아 있다.

`SpawnPermanentMonster`가 템플릿에서 새 NPC 값을 생성한다. 회복 타이머(now,60),
1/2/3회 소지품 추첨 경계, 고정 금화 NPC의 0..49 희소 선택, 아이템 강화와 금화
추첨 순서, 영구 플래그를 구현했다. 카탈로그 템플릿은 가변 소지품을 포함하지 않도록
검사하며 출력 인벤토리는 독립 배열이다. UTF-8 이름/부호 있는 보정치 순 안정 정렬을
사용한다(전체 원본 EUC-KR 정렬과의 비교는 아직 미검증).
`EnchantObject`는 50/90/98 경계와 pdice 하한을 유지하고 숫자 overflow는 오류로
거절한다. 원본 노동자 템플릿 실제 생성, 난수 순서, 누락 템플릿 시 부분 생성 거절,
강화 경계 테스트/race/vet 통과. 생성된 값에 런타임 ID를 부여하고 방/활성 목록에
추가하여 저장하는 부분 및 전체 템플릿 생성 corpus 검증은 아직 남아 있다.

`RefreshRoomResources`로 영구 NPC 수량/생성→영구 아이템 수량/강화/추가→문 갱신을
연결했다. 기존 목록과 중첩 소지품을 독립 복사하고, 하나라도 실패하면 부분 방을
반환하지 않는다. 이름순 삽입은 기존 개체들의 상대 순서를 보존한다. 반복 갱신의
중복 생성 방지, NPC 생성 뒤 아이템 실패 시 부분 게시 방지, 중첩 데이터 소유권
테스트와 관련 race/vet 통과. 방의 NPC/아이템 값은 생성되지만 아직 런타임 ID,
플레이어 입장/활성 목록, 이동 결과와의 원자적 DB 커밋은 연결되지 않았다.
실패해도 주입된 RNG 호출은 소비되므로 최종 실행 계층에서 seed/명령 재실행을
관리해야 한다. 현재 시간/템플릿 DTO는 legacy int32 기반이라는 제한도 유지된다.

`PlanRoomEntry/PlanRoomDeparture`로 방 자원 갱신, 방문 횟수, 입장 전 점유자 기준
장면, 이름순 삽입 및 퇴장 목록을 결합했다. 숨김/DM 투명은 도착 방송 대상이 아니며
일반 투명은 원작처럼 방송 신호를 남긴다. 중복/누락 actor ID와 방문 횟수 overflow를
거절한다. 방 갱신 실패 시 장면·플레이어 목록·방문 횟수가 부분 게시되지 않는다.
NPC 활성화는 점유 방의 모든 NPC를 활성 상태로 맞추라는 idempotent 신호,
비활성화는 마지막 점유자 퇴장 신호다. 관련 TDD/race 및 이동/갱신 회귀, vet 통과.
실제 활성 집합과 개체 ID 부여, 출발/도착/actor 변경의 동일 DB 커밋은 미연결이다.
전투 안내와 이동 마지막 함정/동행 처리도 완료되지 않았다.

## 월드 명령 PostgreSQL 저장 경계

`Migrate`에 `mud_go.worlds`와 `world_commands`를 추가했다. `CreateWorld/LoadWorld`,
`CommitWorldCommand`는 전체 JSON 월드 스냅샷과 응답 영수증을 같은 트랜잭션으로
저장한다. 월드 행 잠금 후 명령 ID를 확인하며, 동일 ID/동일 요청 바이트는 저장된
응답·revision을 재생하고 다른 요청이면 거절한다. 신규 명령은 expected revision이
맞을 때만 상태와 영수증을 함께 갱신한다. 커밋 결과 불명 시 같은 ID/요청으로 재시도한다.

이 API는 내부 저장 계층이다. JSON은 객체 여부만 검사하며 게임 상태/권한 검증은
아직 연결되지 않은 월드 실행 계층의 책임이다. 요청에는 actor ID와 명령을 포함하고
비밀번호는 넣지 않는다. 요청 비교는 바이트 기준이므로 생성 방식과 재시도 바이트를
고정해야 한다. 캐릭터 초안 stage는 여전히 draft이며 게임 입장이 열렸다는 뜻은 아니다.

2026-09-08 격리 로컬 PostgreSQL 17에서 실제 검증:

- 상태/영수증 저장 및 재조회, 이전 revision을 가진 동일 명령의 결과 재생.
- 다른 내용의 ID 재사용 거절, 오래된 신규 명령 버전 거절.
- 영수증 insert 강제 실패 시 먼저 수행한 상태 UPDATE까지 롤백.
- 같은 ID 동시 실행은 1회 저장+1회 재생; 다른 ID의 같은 revision 경쟁은 1회 성공.
- 기존 계정/캐릭터 PostgreSQL 회귀까지 storage 패키지 전체 `-race` 통과, vet 통과.

테스트 DB는 별도 환경 변수 `MUHAN_WORLD_TEST_DATABASE_URL`을 쓰며 새 일회용 DB가
필요하다. 테스트 전용 컨테이너는 종료·제거했고 운영 데이터/공유 캐시는 변경하지 않았다.
월드 장면/이동/입장 변경안을 이 저장 경계에 전달하는 실행 루프와 복구 테스트는 남아 있다.

`engine.Execute`로 영수증 조회→상태 읽기→순수 계산→원자적 저장→응답 순서를
연결했다. 영수증이 있으면 계산하지 않고 재생하며, 커밋 오류 후 영수증으로 확인된
결과만 반환한다. 확인할 수 없으면 오류를 반환하고 같은 명령 ID/요청을 유지한다.
자동 계산 재시도는 없고 reducer의 방송/파일 쓰기 등 외부 부수 효과는 금지한다.
권한 검증과 난수/시간 고정은 호출할 월드 루프의 책임이며 WebSocket에는 미연결이다.

검증: 응답 유실 후 결과 재생, 미커밋 응답 차단, 계산 실패 시 쓰기 없음 단위 테스트.
별도 로컬 PG17에서 `PlanMovement`를 reducer로 호출해 테스트 상태의 Room 1→2와
응답을 함께 저장하고, 새 저장소 인스턴스의 동일 명령 재시도에서 계산 1회/revision 1
유지를 확인했다. engine 전체 race 및 vet 통과. 이것은 축소된 테스트 스냅샷이며
실제 캐릭터·양쪽 방·입장 효과를 모두 저장하는 게임 reducer 완성이 아니다.
실제 프로세스/DB 재시작·장애 주입 검증도 아직 남아 있다. 일회용 DB 컨테이너는 정리했다.

`PlanTransfer`는 이동 판정·출발 방 퇴장·도착 방 갱신/입장을 하나의 변경안으로
묶는다. 도착 방 준비 오류는 변경안을 전혀 반환하지 않는다. 정상적인 이동 거절은
원작 순서에서 이미 발생한 HP/은신/발자국 변화를 보존하고 출발 방에 남긴다.
입장 알림은 갱신된 은신 플래그를 사용한다. 중복 방 소속과 잘못된 시각 범위는
난수 사용 전에 거절한다. 반환된 방/재실자 목록은 입력과 분리된다.

2026-09-08 검증: 먼저 미구현 실패를 확인한 뒤 transfer 단위/race 및 관련
입장·이동·리젠 회귀, vet를 통과했다. engine의 실제 PG17 테스트를 확장하여
HP/위치/은신, 양쪽 방과 재실자 목록, 발자국, 방문 횟수까지 같은 스냅샷에 저장하고
동일 명령 재생 시 방문 횟수/revision이 1로 유지되는 것을 확인했다.
격리 컨테이너 `muhan-transfer-1119027`은 종료·자동 제거했다.
`go test ./...` 재실행은 기존 `TestRoomBodyCorpus`의 63개 실패만 보고했다.
이 reducer는 여전히 테스트용이다. 실제 세션/월드 루프, NPC 활성 상태와 방송,
추종·함정·사망 처리, 완전한 캐릭터 초기화 및 데이터 이관 연결은 남아 있다.

독립 원본 대조에서 추가 차이를 확인했다: C의 목적지 `load_rom`은 입장 제한
검사 전 일부 자원 부수 효과를 발생시킬 수 있으나, 현재 Go는 이동 허용 후에만
입장 리젠을 계산한다. 이 차이의 콜드 로드/이미 로드된 방별 differential 검증과
명시적 정책 결정이 남았으므로 원작 이동과 완전히 동등하다고 판정하지 않는다.
DB 저장 뒤 방송 순서(출발→도착→방 화면)와 NPC 활성 신호도 아직 미연결이다.

## 권위 있는 월드 상태 모델 (개발 중)

`world.State` v1은 방별 `PlayerIDs`와 단일 `Players` 맵으로 재실자 속성의
중복 저장을 없앤다. 온라인 플레이어는 정확히 한 방에 속하고 저장 위치와 일치해야
하며, 오프라인 플레이어는 위치만 보존하고 어느 방에도 재실하지 않는다.
방 키/ID 불일치, 누락·중복 소속, PLAYER(0)가 아닌 객체를 거절한다.
`DecodeState`는 버전/알 수 없는 필드/후행 데이터와 참조 무결성을 검증한다.
`RoomPlayers`는 권위 상태에서 읽기용 표시값을 생성한다.

`ApplyTransfer`는 내부 이동 변경안을 이 모델에 적용하고 HP의 int16 범위,
도착 방/이동 결과 일치, 표시값과 실제 플레이어 속성 일치 및 적용 후 전체 소속을
검사한다. 실패 시 부분 상태를 반환하지 않으며 입력과 결과의 중첩 목록을 분리한다.
이 API는 같은 revision에서 계산한 내부 변경안 전용이며 클라이언트 입력 API가 아니다.

2026-09-08: 상태/적용 테스트 미구현 실패→구현→race 통과, 이동 회귀/race 및 vet
통과. 격리 PG17 engine 테스트를 임시 `movementState` 대신 실제 `world.State`의
decode→transfer→apply→저장 경로로 변경하여 저장·재조회·동일 명령 1회 적용을
확인했다. 전용 컨테이너 `muhan-state-1226908`은 종료·자동 제거했다.

이는 전체 플레이어 저장 계약 완료가 아니다. 현재 Body는 원본 creature 의미 필드의
`LegacyMonster` 타입을 재사용하며 20개 장비 슬롯/객체 ID, 추종·전투·대화 관계,
NPC ID/활성 상태와 재시작 시 Online 재조정은 미구현이다. 자격 정보는 별도 인증
저장소에 두며 여기에 평문 비밀번호를 추가하지 않는다. 원본 대조 근거는
`src/mstruct.h:194-242`, `src/files1.c:491-498`, `src/mtype.h:117-120`이다.
실제 명령 dispatch·세션 연결·완전한 초기화/이관 인수는 아직 남아 있다.

`State.RecoverOffline`은 콜드 스타트용 변경안이다. 유효한 스냅샷에서 모든 Online과
방 PlayerIDs만 비우고 캐릭터 위치/HP/금화/중첩 인벤토리 및 방 자원은 보존한다.
원본 스냅샷과 결과는 분리되며 중복 적용은 같은 상태를 만든다. 모순된 기존 소속은
임의로 복구하지 않고 거절한다. 끊어진 ID 목록은 정렬해 결정론적으로 반환한다.
일반 재접속/읽기 경로에 호출하면 안 된다. 독점 월드 소유권을 확보하고 세션/tick을
막은 상태에서 DB 커밋을 확인한 후 새 접속을 허용해야 한다. 이 시작 절차와
lease/fencing, 세션 세대 구분은 미구현이다.

2026-09-08: recovery 미구현 실패→단위/race 통과, state/transfer 회귀와 vet 통과.
격리 PG17에서 이동 저장 후 기존 연결 풀을 닫고 새 풀로 복구 변경안을 저장했다.
동일 복구 명령 재시도는 계산 1회/revision 2를 유지하고 HP/위치/방 방문 횟수는
보존했다. 이는 연결 풀 재생성 테스트이며 실제 프로세스/DB 재시작 검증이 아니다.
전용 컨테이너 `muhan-recovery-1376888`은 종료·자동 제거했다.

`State.EnterSavedPlayer`는 `src/player.c` init_ply 중 방 배치 단계를 연결한다.
이미 온라인인 캐릭터를 거절하고 저장 방의 인원 제한(관리자 투명만 제외), 로그인
금지, 결혼방 소유 조건에 따라 1번 방으로 이동한다. 방 준비 성공 후에만 Online,
위치, 재실자/방 자원 변경안을 반환한다. 전체 init_ply가 아니며 일일 한도·패거리
플래그·타이머·장비 복원/아이템 정리·전체 로그인 방송은 남아 있다.
State.Validate는 없는 저장 방을 거절하므로 C의 '저장 방 로드 실패→1번 방' 경로는
아직 이관/복구 정책으로 해결하지 않았다. 임의로 방을 만들어 오류를 숨기지 않는다.

2026-09-08: 입장/중복/제한방/관리자 투명/실패 원자성 단위·race 테스트와 vet 통과.
실제 격리 PG17에서 이동→오프라인 복구→저장 방 재입장 및 동일 입장 명령 재생을
확인했다(revision 3, 재실자 1명, 방문 횟수 2). `muhan-entry-1390026`은 정리했다.
호출 actorID는 인증 완료 후 내부에서 해석해야 한다. 현재는 테스트 reducer에서만
연결되며, draft 가입 결과를 운영 캐릭터로 승격하거나 WebSocket 입장을 열지 않았다.

`LoginTimers`를 입장 상태 변경에 연결했다. 원작처럼 저장 타이머(22)를 현재
시각/600초로 설정한 뒤 LT_HOURS(28) 이후 경과 시간만큼 45개 타임스탬프를
옮기고 현재 시각으로 상한을 둔다. 시계 역행 시 저장 타이머도 다시 뒤로 이동하는
원작 순서를 보존한다. 계산은 int64로 넓혀 C signed overflow를 피하고 최종
int32 하한 미만이면 전체 입장 변경안을 거절한다. 이 범위 처리는 명시적 차이다.

2026-09-08: 타이머 TDD/race, 입장·복구·상태·이동 회귀/race, vet 통과.
`login_timers_legacy_test.go`가 실제 player.c의 저장 설정/시간 보정 블록과
mtype.h 상수를 가져와 테스트 전용 C oracle을 컴파일한다. 정상 경과·시계 역행·
동일 시각·초기 시각 4개 조건에서 각 45개 슬롯 전체가 Go와 일치했다.
Go 운영 런타임에는 C 의존성을 추가하지 않았다. 전체 `go test ./...` 재실행은
기존 방 corpus 63개 실패로 여전히 적색이며 이번 턴에는 PG 통합을 재실행하지 않았다.

Luna 읽기 전용 대조로 다음 장비 복원 계약을 확인했다(`player.c:134-224`,
`mtype.h:205-230`). 시간 보정 뒤 인벤토리 순서대로 처리하며 낡은 이벤트 아이템만
제거한다. 사용 횟수·방어도·피해 검증 탈락은 삭제가 아니라 인벤토리 유지다.
OWEARS(23)인 아이템의 반지는 슬롯 8..15, 목걸이는 3..4의 첫 빈 자리로 간다.
WIELD는 OWHELD(49) 유무에 따라 슬롯 19/16을 구분하고 나머지는 wear-1이다.
성공한 아이템만 인벤토리에서 제거하며 착용 플래그는 유지한다. 원본의 무검증
wear 인덱스는 Go에서 범위 검사해야 한다. 이후 compute_ac→compute_thaco→update_ply
순서를 보존해야 한다. 실제 장비 슬롯/객체 ID와 이 복원 코드 구현은 다음 작업이다.

`PlanLoginEquipment`로 원본 init_ply의 인벤토리→장비 분류를 구현했다.
결과는 기존 인벤토리 인덱스의 Ready[20](-1=빈 슬롯), Remaining, Removed다.
반지/목걸이 first-fit, WIELD/OWHELD 분기, 원본 아이템 순서와 플래그를 보존한다.
사용 횟수·방어도·피해·직업/레벨·퀘스트 조건에 탈락하면 남겨두고, OEVENT이며
ONEWEV가 아닌 아이템만 제거 대상으로 분류한다. 이 함수 자체는 데이터를 삭제하지
않으며 후속 객체 ID 트랜잭션에 연결해야 한다. eligible OWEARS의 wear가 1..20
밖이면 전체 변경안을 거절한다(C의 범위 밖 메모리 접근과 명시적으로 다르다).

2026-09-08: 미구현 실패→장비 TDD/race 및 vet 통과. 슬롯 초과 보존, 손 무기,
이벤트 분류, signed armor, 수치 경계/직업 보정과 80개 아이템이 각각 정확히 한
분류에 속하는 것을 검증했다. 입력 인벤토리는 수정하지 않는다. 원본 코드를 읽어
기대값을 작성했으며 장비 C 실행 differential은 아직 없다. 실제 PlayerState 장비
저장, AC/THAC0/update_ply 계산과 로그인 연결은 미완료다.

`ComputeArmorClass`로 compute_ac를 구현했다. 원본 bonus[64] 표를 사용하고
민첩 63 초과를 63으로 제한하며, 장비 20개 signed armor와 보호 효과를 차감한 뒤
[-127,127]로 제한한다. legacy signed dexterity가 음수인 값은 C의 음수 배열
인덱스를 재현하지 않고 거절한다. 인벤토리의 미착용 아이템은 AC에 포함하지 않는다.

2026-09-08: TDD/race, 장비 복원 결과→AC 조합 테스트 및 vet 통과.
`armor_legacy_test.go`가 player.c의 실제 compute_ac와 global.c의 bonus 표,
mtype.h 상수로 C oracle을 컴파일하여 민첩 0..127 × armor 5종 × 장비 개수
0/1/20 × 보호 유무 = 3,840개 결과가 Go와 일치함을 확인했다.
이는 AC 계산 검증이며 실제 장비 영속 저장, THAC0/update_ply, 로그인 반영과
전투 명령 완성을 뜻하지 않는다. 테스트 전용 C 실행 외 운영 C 의존성은 없다.

`WeaponProficiency`는 직업별 누적 경험치 구간을 정수 보간하고, `ComputeThaco`는
직업/레벨 표→WIELD 보정→숙련도/직업 나눗수→힘 보정→직업/레벨 하한 순서로
계산한다. 실제 C에서는 PBLESS가 저장 대입 뒤 지역 변수만 바꾸므로 저장된 THAC0에
영향이 없다. Go도 이 동작을 보존하며 축복 기능 수정은 별도 정책/검증 과제다.
이전 조사에서 '축복 3 차감'으로만 기술한 내용보다 이 실행 결과가 우선한다.

2026-09-08: TDD/race 및 C differential 통과. 실제 player.c의 compute_thaco,
mod_profic, profic 세 함수와 global.c 표를 사용한 ILP32 long 어댑터로 12직업×
7레벨×3힘×4무기 조건×축복 유무=2,016개 저장 결과가 일치했다. 숙련 경험치는
oracle에서 1,000,000~1,004,000을 사용했으며 모든 XP 범위의 동등성 주장이 아니다.
Go는 보간 곱셈을 int64로 넓혀 큰 XP에서 C의 int32 overflow를 피한다. 이 영역은
명시적 차이이고 상한 500,000,000 이상/음수 XP, 미정의 직업/레벨0/힘64 이상,
음수 무기 인덱스를 거절한다. 실제 전투/장비 저장/로그인 반영은 아직 미연결이다.

## 아이템 ID와 장비 영속 구조

`ItemCollection`은 ID→Item 맵, 인벤토리 루트 ID, 컨테이너 자식 ID, Ready[20] ID로
객체의 단일 소유 위치를 표현한다. 기존 LegacyObject.Contents 복제 저장을 거절하고
전체 도달성/중복/순환/고아 검사를 한다. `ImportItems`는 주입한 ID 할당기로 중첩
원본을 한 번 변환하며 빈/중복 ID·깊이/개수 제한 위반은 부분 결과 없이 거절한다.
재시도할 때 같은 ID를 부여하는 이관 매핑은 호출 계층에서 아직 구현해야 한다.

`RestoreEquipment`는 빈 슬롯 상태의 이관 자료에 장비 복원 계획을 적용한다.
아이템 ID를 그대로 슬롯으로 옮기고, 오래된 이벤트 컨테이너의 하위 트리 전체를
제거 대상으로 반환한다. 원본/외부 데이터는 변경하지 않는다. live wear/unwear와
canonical 상태의 재로그인 때 다시 이관하는 동작은 아니다.

PlayerState.Items를 추가했다. nil은 ID 이관 전을 의미하며, Items가 있으면
Body.Inventory의 중복 데이터를 허용하지 않는다. State.Validate는 플레이어 간
동일 아이템 ID 소유도 거절하며 이동/복구에서 중첩 ID 목록을 깊게 복사한다.
새 Items 필드는 개발 중인 v1 스냅샷에 추가했으며 배포된 운영 스키마를 바꾸지 않았다.

2026-09-08: TDD/race, JSON 왕복·잘못된 소유 관계·장비 복원·상태 복구 회귀와
vet 통과. 격리 PG17 engine에서 가방/동전/검 ID와 검 슬롯을 포함해
이동→복구→재입장→동일 명령 재생 후 3개 ID 및 소유 관계 보존을 확인했다.
전용 `muhan-items-1569253` 컨테이너는 종료·자동 제거했다. 실제 사용자의 이관,
AC/THAC0 반영, 아이템 명령과 실사용 세션 연결은 아직 남아 있다.

`ItemCollection.CombatStats`와 EnterSavedPlayer를 연결했다. canonical Ready ID만
방어도 계산에 넣고 WIELD 슬롯만 THAC0 계산에 넣는다. 재로그인에서 아이템을 다시
이관/재배치하지 않으며 ID와 소유 관계를 보존한다. 수치 계산 오류는 방 방문 횟수와
Online을 포함한 입장 변경안을 전혀 반환하지 않는다. Items=nil인 이전 경로는
여전히 이관 전 방 배치 테스트 경로이며 완전한 게임 입장으로 승인되지 않았다.

2026-09-08: 장비 수치/입장 실패 원자성 TDD 및 기존 world 관련 race/vet 통과.
격리 PG17에서 장비/아이템 상태를 가진 플레이어의 이동→복구→재입장/재시도를
실행하여 AC 93, THAC0 18과 원래 아이템 ID/슬롯이 같은 revision에 저장되는 것을
확인했다. 전용 `muhan-stats-1586203` 컨테이너는 종료·자동 제거했다.
update_ply·전체 초기화, 실제 로그인 핸들러/게임 명령 루프와 연결하는 작업은 남아 있다.

`LoginDaily`를 EnterSavedPlayer에 연결했다. CARETAKER 미만 직업의 방송/인챈트/
완전회복/추적/배설 최대 횟수만 원본 레벨 공식으로 갱신한다. Current/LastTime과
결혼·패거리 등 나머지 슬롯은 보존한다. 재로그인으로 사용 횟수를 초기화하지 않는다.
2026-09-08: TDD/race, 저/최고 레벨·관리 직업·이용 횟수 보존 및 입장 연결 테스트,
관련 장비/시간/수치 C differential 포함 회귀/race, vet 통과. 이번 단계는 원본 소스
기반 한도 테스트이며 일일 한도 자체의 C 실행 differential/PG 재실행은 하지 않았다.
update_ply의 마법 만료·회복·질병 등은 아직 이 구현에 포함되지 않는다.

`ExpireEffects`로 update_ply의 임시 효과 만료 단계(회복 시작 전)를 구현했다.
24종 플래그를 원본 순서대로 처리하고 일부 효과의 class<DM 예외, 은신 300초
특례, 엄격한 now>deadline 경계, AC/THAC0 재계산 위치를 보존한다. 출력은 ANSI를
제외한 순서 있는 이벤트이며 광원 효과 해제의 방 방송은 별도 Room 이벤트다.
이 함수는 이벤트를 전송하거나 DB/원본 데이터를 변경하지 않는다.

TDD/race: 만료 경계·중복 해제 방지·힘/축복 재계산 순서·관리자 예외·24종 동시
만료와 알림 순서·정수 범위·부분 실패 차단 통과. 기존 AC/THAC0 C differential을
포함한 관련 회귀/race와 vet 통과. 만료 함수 자체의 C 실행 differential 및 PG
통합은 아직 없다. 타이머 합산은 int64로 넓히고 음수 능력치/수치 underflow는
전체 변경안을 거절하며, 이 범위 처리는 원본의 위험한 연산과 명시적으로 다르다.

Luna 원본 분해 결과 update_ply의 남은 순서는 LT_HOURS 누적→효과 만료→
방/독/질병 기반 회복·피해→환경 피해→저장 체크포인트→광원 소모다.
현재 ExpireEffects는 이 중 한 단계만 담당한다. die()/방 소속·아이템/출력의
원자적 상태 변경, 방 플래그와 RNG, 온라인 tick 루프/접속 소유권을 함께 구현한
뒤 전체 update_ply와 로그인 마지막 단계를 연결해야 한다. 부분 만료 함수를
전체 tick으로 호출하거나 전체 초기화 완료로 해석하지 않는다.

`PlanVitals`로 만료 후 회복/질병/유해 방 단계를 구현했다. 일반/빠른 회복,
독·질병의 난수 피해와 공격 지연, 방의 중독·혼란·MP 소모, 불/물/흙/바람 보호
우선순위와 무속성 유해 방 피해를 계산한다. 환경 피해는 원본 brace 구조상
RPHARM 분기 안에 있으므로 일반 방의 회복 뒤 무조건 적용하지 않는다.
입력 시점 ill 값을 같은 회차에 보존하며, 원본의 음수 회복 interval도 임의 보정하지
않는다. 시간과 난수는 주입하고 오류는 전체 변경안을 거절한다.

Death 결과는 원본 die 호출 위치에서 멈추는 필수 전환 장벽이다. 실제 사망의
방/아이템/부활 변경을 먼저 구현해야 하며 HP만 저장하거나 사망 뒤 단계를 그대로
계속 실행하면 안 된다. 원본 die는 인라인에서 상태를 바꾸므로 그 이후 흐름은
별도 연결/비교가 남아 있다. 이 결과는 완성된 tick이나 사망 처리 완료가 아니다.

2026-09-08: TDD/race로 회복·기한 경계·독 사망 후 중단·질병 지연·새 중독 후
원본 회복 판단·환경 보호 우선순위·음수 interval·난수 실패 원자성·방 없음 검증.
기존 효과/AC/THAC0 회귀와 vet도 통과했다. PlanVitals 자체의 C 실행 differential,
실제 DB/세션/tick 연결은 아직 없다. 산술은 넓혀 계산한 뒤 범위를 검사하므로
원본 int16 wrap/정의되지 않은 인덱스 동작은 의도적으로 재현하지 않는다.

회복/피해 C 실행 비교에서 기존 Go 구현의 실제 난수 소비 차이를 발견·수정했다.
원본 MAX(1,mrand(...)-bonus)는 첫 결과가 1 이상이면 난수를 다시 뽑고 그 값을
재검사 없이 사용한다. MAX(dice(2,6,0),6)도 첫 합이 6보다 크면 주사위를 다시
굴린다. 두 번째 독 피해가 음수이거나 혼란 지연이 6 미만이 될 수 있는 원본
결함을 현재 호환 구현에서 보존한다. 이는 추후 명시적 게임 규칙 수정 후보이며,
조용히 한 번만 뽑거나 재-clamp하지 않는다.

`vitals_legacy_test.go`는 player.c의 실제 회복/피해 블록, global.c 보정표,
mtype.h MAX/LT/플래그를 사용한다. 난수는 하한/상한/교대 어댑터를 주입하고
첫 die 호출은 중단 어댑터로 처리한다. 2직업×3체질×2신앙×8방 조건×4질병 상태×
3난수 패턴×2초기 HP=2,304개에서 HP/MP/회복·공격 타이머/중독/사망/난수 소비
횟수가 일치했다. 별도 혼합 주사위 회귀도 통과했다. ANSI·메시지 출력 및 사망
이후는 비교하지 않았다. race/vet 통과; 이번 턴에는 DB/배포를 실행하지 않았다.

`PlanDeathEquipment`는 PLAYER 사망의 장비 소유 전환 변경안이다. survival/전쟁
gate로 손실 여부를 먼저 정하고, WIELD 특례→나머지 20슬롯 순서를 보존한다.
자기 자신이 공격자인 독/환경 사망도 원작에서는 AttackerPlayer=true다. 공격자가
NPC면 일반 장비는 인벤토리로, 플레이어면 바닥 전송용 컬렉션으로 분리한다.
퀘스트·ONEWEV·OCURSE 보호 장비는 그대로 착용하며, 특례 무기는 자식까지
OTEMPP/OPERM2를 설정한다. 원본 F_ISSET(item,CONTAINER)는 type 비교가 아니라
숫자 9(=OPERM2) 비트 검사라는 동작도 보존했다.

Kept와 Dropped는 같은 ID를 복제 소유하지 않는다. Dropped.Inventory는 방으로
전송할 루트 ID 목록이며 실제 방 바닥 저장과 연결해야 한다. 원본 객체나 외부
저장소를 수정/삭제하지 않았다. TDD/race로 NPC/플레이어/자기 사망 규칙 입력,
survival/전쟁 gate, 보호 장비, 중첩 임시 플래그, 모든 ID의 정확히 한 곳 소유를
검증했고 관련 아이템 회귀 및 vet도 통과했다. C 실행 differential은 아직 없다.
전체 사망의 XP/숙련도/레벨 감소, 적대·패거리 상태, 회복 수치, 방 1008 입장,
방송·저장을 합친 트랜잭션은 미완료이며 장비 분할만으로 사망을 적용하지 않는다.

Luna 조사로 후속 경험치 계약을 확인했다. self/NPC 사망은 레벨20 미만 XP/20,
그 이상 min(XP/15,100000)을 차감하며 PvP는 차감하지 않는다. 높은 레벨 보정은
원본 식 `((level+3)/4)-exp_to_lev(newXP)>1` 그대로 확인해야 한다. exp_to_lev의
고레벨 기준은 needed_exp[126]과 5,000,000 간격이다. check_war 반환1은 손실
허용이고 실제 양 패거리 전쟁 중에는 0이다. XP와 별도로 숙련도/realm 분배 손실,
down_level의 직업 HP/MP·능력치 주기·PUPDMG 해제까지 구현해야 한다. self 사망의
MP는 PLAYER 분기라 완전 회복이 아니라 최소 mpmax/10만 보장한다.

`PlanDeathProgression`은 사망 XP 차감/보정, 숙련도·realm 손실 분배, 실제로
내려갈 목표 레벨을 계산한다. 일반 PvP XP 유지와 self/NPC XP 손실을 구분한다.
SkillLoss는 호출 계층이 survival/check_war 결과로 결정해야 한다. 숙련도 손실은
9개 값에 원본 순서대로 반복 분배하고 가장 큰 무기 숙련도를 최소1024로 만든다.
동률이면 앞 슬롯을 선택한다. 합계는 int64로 넓혀 기존 long overflow를 피한다.

레벨20·XP0처럼 불일치한 저장 자료에서 원본 보정식이 XP6144를 만들 수 있음을
테스트로 명시했다. 이를 정상 게임 규칙으로 일반화하거나 XP 증가 버그를 조용히
수정하지 않는다. 이관 검토/정책 결정이 필요한 예외다.

2026-09-08: TDD/race로 PvP/self/NPC·레벨 경계·차감 상한·보정·숙련도 gate/
최소값/동률/큰 합계 검증. needed_exp 128개 전부를 실제 global.c와 대조했다.
관련 사망 장비 회귀 및 vet 통과. 사망 progression 자체의 C 실행 differential은
아직 없다. 목표 레벨만 계산하며 down_level의 HP/MP/능력치 감소, 실제 사망
통합 저장/복구는 미구현이다. 이번 턴 DB·배포는 실행하지 않았다.

`LowerPlayerLevel` 및 `ApplyDeathProgression`을 추가해 목표 레벨에 도달할 때까지
직업별 HP/MP 감소, 4레벨 단위 능력치 감소, PUPDMG의 단일 해제와 현재 HP/MP
갱신을 순수 변경안으로 구현했다. 숫자 범위 오류는 부분 변경 없이 거절하며
기존 inventory를 공유하지 않는다. 위의 down_level 미구현 기록을 대체한다.

2026-09-08: `TestLowerLevelAgainstLegacy`가 실제 player.c의 down_level과
global.c의 class_stats/level_cycle, mtype.h를 임시 C 프로그램으로 컴파일했다.
12직업 × 255시작레벨 × 4목표(유지/1하락/4하락/1까지) × 강화 여부 2종 =
24,480개에서 레벨·최대/현재 HP/MP·피해 보정·5능력치·강화 플래그가 일치했다.
낮은 레벨에서 중복되는 목표도 포함한 사례 수이며 전체 게임 완성률이 아니다.
입력은 정의된 수치 범위이며 잘못된 상태의 C wrap 동작까지 일치시킨 것은 아니다.
관련 LowerLevel/ApplyDeathProgression/Death/Experience race 검사와 world vet 통과.
XP·숙련도 분배 자체의 C 실행 비교, 사망 후 회복/상태 해제, 방 1008 입장,
장비 드롭의 방 소유권 및 저장을 합친 원자적 사망 전이는 아직 남아 있다.
이번 검증은 로컬 C/Go만 사용했으며 DB·CI·push·배포는 실행하지 않았다.

`PlanDeathCharacter`는 피해자 progression→HP/MP 회복→독/질병 해제→장비
분할→AC/THAC0 재계산을 하나의 순수 후보로 결합한다. PLAYER 공격자(자기 자신
포함)는 현재 MP와 최대 MP/10 중 큰 값을 유지하고 NPC 공격자는 MP를 완전히
회복한다. 레벨 하락 자체가 현재 MP를 최대치로 바꾸는 원본 순서도 유지한다.
canonical Items만 허용하며 raw Inventory와의 중복 소유를 거절한다.

2026-09-08: 구현 전 실패 테스트 후 NPC/PLAYER 장비 분할과 MP 회복 차이,
독/질병 해제, 장비 제거 후 AC, self/survival 입력, 입력 불변성, 마지막 능력치
계산에서 실패해도 결과 전체가 0인 것을 검증했다. 관련 사망/레벨 race 및 vet
통과. 이 후보만 저장하면 안 된다: 공격자 PK 타이머(자기 사망 포함), 적대/패거리,
바닥 소유권 병합, 기존 방 퇴장과 1008 입장을 상위 원자적 전이에 결합해야 한다.
전체 사망 C differential/실제 PG 사망 저장 검증은 미실행이다.

`TransferItemRoots`는 두 canonical 컬렉션 사이에서 루트와 모든 자식을 원래
ID로 이전한다. 양쪽 ID 중복·없는 루트·중복 요청·컨테이너 내부 항목이나 착용
슬롯의 직접 이전은 거절한다. 원본 add_obj_rom처럼 이름/부호 있는 adjustment로
삽입하고 같은 값은 기존 항목 뒤에 둔다. 결과 양쪽을 함께 저장해야 하며 단독
외부 쓰기는 없다. TDD/race로 소유 수량, 입력/자식 목록 불변성, 오류의 zero 결과,
사망 Dropped→기존 바닥 컬렉션 병합과 중첩 temporary 플래그 보존을 확인했다.
world vet 통과. 아직 RoomState 바닥 canonical 스키마 및 리젠/표시와 연결하지
않았으므로 실제 방에 드롭하거나 PG에 사망을 저장한 증거는 아니다.

RoomState.Items를 추가했다. non-nil이면 Resource.Objects는 비어 있어야 하고,
방의 Ready 슬롯은 금지된다. State.Validate는 방 간 및 방/플레이어 간 같은 ID의
중복 소유를 거절한다. State.clone/RecoverOffline은 바닥 아이템과 내부 관계를
깊은 복사로 보존한다. JSON encode/decode와 복구 왕복, 중복 소유 및 raw/canonical
혼합 거절 테스트와 관련 race/vet 통과. 실제 PostgreSQL 검증은 이번에 미실행이다.

임시 미완성 경계: 기존 PlanTransfer/PlanRoomEntry는 nested Objects 기반이므로
canonical 바닥이 있는 방에 기존 ApplyTransfer/EnterSavedPlayer를 적용하면 명시적
오류를 반환한다. ID 데이터를 버리거나 빈 바닥을 보여주는 성공 응답을 막기 위한
장치이며 최종 기능 제한이 아니다. 후속 필수 작업은 canonical 바닥을 표시하고
리젠 생성에 새 ID를 주입하는 입장/이동 adapter로 연결한 뒤 이 거절을 제거하는 것.
기존 nil Items 경로는 유지하지만 완성형 게임 실행 경로로 간주하지 않는다.

후속 진행: RefreshCanonicalRoom은 기존 리젠 계산을 공유하며 새로 생성된 바닥
객체만 관찰해 명령 소유 allocator로 ID를 부여한다. 기존 객체를 이름/값으로
추측 매칭하거나 로그인마다 ID를 재생성하지 않는다. LegacyInventory는 반복형
후위 구성으로 표시용 nested projection을 만들며 저장 권위로 사용하지 않는다.
PlanCanonicalRoomEntry는 이를 화면에 표시하고 반환 저장 후보의 Objects를 비운다.

EnterSavedPlayerWithIDs 및 기존 EnterSavedPlayer에 canonical 입장을 연결해 위의
로그인 임시 거절을 제거했다. ID allocator가 없어도 리젠이 없으면 입장 가능하고,
새 아이템이 필요하면 allocator 없이는 실패한다. 전체 State.Validate로 다른 방/
플레이어 ID와의 충돌도 거절한다. 순수 테스트/race로 신규 1회 할당, 반복 리젠의
무할당, 기존 ID/내용물 보존, 화면 표시, allocator 충돌/부재의 zero 결과, 로그인
성공/세계 전체 ID 충돌을 검증했고 world vet 통과. PG·브라우저는 이번 미실행.
ApplyTransfer의 canonical 이동 임시 거절은 아직 남아 있어 같은 처리를 이동에
연결해야 한다. WebSocket 실제 월드 입장·사망 통합 역시 미완료다.

후속: State.TransferWithIDs로 canonical 이동 경로를 추가했다. 같은 snapshot에서
출발/도착 방과 재실자, actor HP/hidden을 다시 읽고 기존 이동 규칙을 실행한다.
도착 방은 PlanCanonicalRoomEntry를 통해 리젠·표시하며 기존 출발 바닥과 새 도착
바닥을 이동/HP/재실자 후보에 함께 반영한다. 최종 State.Validate가 전역 ID 충돌을
검사한다. legacy ApplyTransfer는 ID 정보를 받지 못하므로 기존 거절을 유지하며,
canonical 호출자는 새 경로를 사용해야 한다. 다른 capability 입력은 서버가 판정한다.

2026-09-08: TDD/race로 양쪽 바닥 ID 보존·화면 표시·깊은 복사·이동 거절 시
리젠/할당 없음·새 리젠 ID의 출발 방 충돌 시 후보 전체 거절을 검증했다. 관련
canonical/transfer/state/room 회귀 및 world vet 통과. engine/session/transport
기본 테스트도 통과했으나 DB 환경 변수를 지정하지 않았으므로 실제 PG 테스트
실행 증거가 아니다. 새 이동 경로의 PG 저장/재생과 사망 통합이 다음 필수 작업이다.

2026-09-08 실제 PG17 검증: engine 통합 시나리오를 TransferWithIDs로 전환하고
출발 방 바닥 1개, 도착 방 상자/자식 2개, 신규 리젠 1개를 포함했다. 최초 ID 충돌
실패 후 revision0/위치/바닥/track 불변을 확인했으며 같은 명령 ID의 수정된 내부
할당 재실행이 replay가 아닌 revision1 신규 실행으로 성공했다. 성공 재시도는
reducer/allocator를 다시 호출하지 않는다. 풀 닫기/재생성→RecoverOffline(rev2)→
canonical 재입장(rev3) 뒤 양쪽 바닥 및 플레이어 장비 ID/중첩/슬롯/AC/THAC0
보존과 리젠 무중복을 확인했다. 출력 영수증에도 리젠검이 포함된다.

`MUHAN_ENGINE_TEST_DATABASE_URL=... go test -race ./internal/engine
-run '^TestPostgresMovementExecutionAndReplay$' -v -count=1` 실제 통과, engine vet
통과. 실제 서버 프로세스/DB 강제 종료나 브라우저 플레이 테스트는 아니다.
고유 컨테이너 muhan-floor-1970044와 새 DB floor_retry를 사용했고 해당 --rm
컨테이너만 중지/자동 제거했다. 공유 자원·CI·push·배포는 건드리지 않았다.

`PlanDeathTimers`는 원본 die의 LT_PLYKL(11) 처리를 구현한다. 피해자 ltime/
interval을 지운 뒤 survival 밖의 PLAYER 공격자에게 주입 난수 7~14일을 부여한다.
self 사망은 같은 객체라는 원본 alias 의미를 보존해 피해자에도 새 타이머가 남는다.
그 외 분기는 난수를 소비하지 않고 RemoveEnemy 신호를 반환한다. Misc는 보존한다.
TDD/race로 PLAYER/self/NPC/survival, 7/14일, 잘못된 RNG·self 문맥 거절을 검증했고
관련 사망 회귀 및 world vet 통과. 타이머 C 실행 differential과 적대 관계의 실제
삭제/세계 사망 원자적 저장은 아직 미연결이며, 신호만으로 삭제 완료를 주장하지 않는다.

FamilyWar는 AT_WAR의 base16 두 패거리 번호 및 CALLWAR1/2를 보존한다.
AllowsDeathLoss는 실제 전쟁 상대 두 패거리 사이에서만 손실을 면제하고, 무소속/
평화/다른 패거리 관계는 손실을 허용한다. AfterPlayerDeath는 전쟁 참가 패거리의
문주 사망 시 세 값을 모두 0으로 만들고 공지 필요 신호를 반환한다. 원본 die처럼
survival/공격자 종류와 독립된 조건이다. 평화 중 선전포고만 있으면 취소하지 않는다.

소스 재확인 중요 사항: 패거리 번호 DL_EXPND는 mtype.h의 daily[9].max다.
daily[8]은 기존 결혼 입장 처리에서 쓰는 다른 필드이므로 혼동하지 않는다.
TDD/race로 전쟁 양방향/무소속/제3자/같은 패거리, 문주 여부와 소속 번호,
평화 중 선언 유지 및 slot8/9 구분을 검증했고 world vet 통과했다. C 실행 비교,
전쟁 선언/승낙 명령, 월드 영속 필드와 사망 통합은 아직 미연결이다.

State.War(nil=미이관) 및 PlayerState.PlayerEnemies(ID 참조)를 추가하고 clone/
참조 검사를 연결했다. PlanPlayerDeath는 같은 방의 PLAYER 공격자 또는 self에
대해 타이머→피해자 진행/회복/장비→기존 바닥 병합→적대/전쟁→퇴장→1008 입장을
하나의 State 후보로 묶는다. 마지막 Validate까지 실패하면 State/result 전체를
거절한다. 기존 NPC를 가짜 플레이어로 대체하지 않으며 NPC 사망/공격자 경로는
고유 NPC ID/적대 관계 모델을 추가한 뒤 합쳐야 한다.

TDD/race로 self의 PK 타이머/MP/바닥 소유권/부활, 입장 visit overflow의 후기
실패, PvP 전쟁 손실 면제/문주 사망 종료, survival의 적대 제거/무난수/방송 억제,
입력 불변성을 검증했고 관련 회귀와 world vet 통과. 방송은 결과 신호이며 실제
출력/저장 후 전달은 미연결이다. 새 사망 후보의 PG 원자 저장·재시도, 전체 C
사망 differential, 웹 게임 세션 연결 및 NPC 통합은 다음 필수 작업이다.

2026-09-08: TestPostgresDeathExecutionAndReplay를 격리 PG17에서 race로 실행했다.
자기 사망의 HP/MP·PK 타이머·무기와 자식의 바닥 소유권·임시 플래그·1008 재실자/
방문 횟수가 revision1로 함께 저장됐다. DB 연결 풀을 닫고 다시 연결한 뒤 동일
사망 명령은 원래 결과를 재생했으며 reducer/RNG는 각각 총1회였다. 실제 DB에서
최종 상태와 저장 결과를 다시 읽어 확인했다. 이동/복구/재입장 PG 회귀도 함께
통과했고 engine vet 통과. 이는 self 사망 시나리오이며 PvP 전쟁/장애주입 DB
테스트나 실제 프로세스/DB 강제 종료를 검증한 것은 아니다.
고유 컨테이너 muhan-death-2005679만 중지해 --rm으로 제거했다. CI/push/배포 없음.

TickPlayerVitals는 PlanVitals의 첫 사망 barrier까지와 self PlanPlayerDeath를
단일 State 후보로 연결했다. 치명적 독 피해만 저장하지 않고 드롭/타이머/1008
입장까지 포함하며 마지막 입장 실패 시 피해 메시지와 상태 후보도 전부 거절한다.
TDD/race로 독 난수 2회→사망 PK 난수1회 순서, 독 해제/HP/드롭/부활, 일반 회복,
후기 실패의 zero 결과를 확인했고 관련 사망 회귀/world vet 통과했다.
주의: 원본 update_ply는 inline die 이후에도 실행을 계속한다. 이 API는 그 첫
barrier까지만 처리하며 완전한 tick이 아니다. 후속 실행 위치·이전 지역 변수·새
방/캐릭터 상태를 반영하는 상태 기계를 구현하기 전 전체 tick으로 스케줄하지 않는다.
실제 PG/브라우저에서 이 합성 vitals 경로는 이번에 실행하지 않았다.

PlanVitalsWithDeath를 추가해 각 inline die 위치에서 callback의 새 플레이어와
parent room으로 이어갈 수 있게 했다. 최초 ill 지역 변수는 유지하고, 레벨 하락
후 능력치 보너스는 갱신한다. 기존 PlanVitals는 callback 없이 첫 사망에서 멈추므로
기존 C differential의 비교 범위는 유지된다. 원작에서 harm 분기로 들어간 뒤
부활하면 RPHARM을 다시 분기하지 않고 새 방의 MP/hazard 처리를 이어간다.
따라서 새 방에 hazard 플래그가 없으면 일반 생명력 drain도 발생할 수 있다.
테스트로 독 사망→새 방 HP100→92, 초기 ill 때문에 MP8 유지, 마지막 heal timer
갱신과 callback 실패의 zero 결과를 확인했다. vitals race 회귀/world vet 통과.
실제 C의 사망 이후 연속 실행 differential 및 State.TickPlayerVitals의 callback
연결/복수 사망 이벤트 순서/PG 검증은 아직 남아 있다. callback은 중간 저장/방송을
하지 않고 후보만 구성해야 한다. 전체 update_ply 완료를 의미하지 않는다.

2026-09-08: TestVitalsResumeAgainstLegacy에서 실제 player.c 회복 phase를
컴파일하고 die만 고정 부활 어댑터로 대체했다. 새 방으로 이동, HP/MP 최대/현재,
CON/PTY/INT 변경과 독/질병 해제를 양쪽에 동일하게 주입한 뒤 남은 원본 C
실행과 Go를 비교했다. 2직업×3CON×2PTY×8방×4질병×3난수패턴×2HP =2,304개
전체에서 HP/MP/회복·공격 타이머/중독/사망 호출 수/난수 소비 수가 일치했다.
기존 첫 사망 중단 비교 2,304개도 race로 함께 통과했다. 실제 C die 자체와
메시지/ANSI, 모든 update_ply phase를 검증한 것은 아니다. 고정 부활 어댑터로
연속 실행 경로를 확인한 증거이며 State callback 통합은 다음 작업이다.

후속: TickPlayerVitals를 PlanVitalsWithDeath와 실제 PlanPlayerDeath callback으로
연결했다. 한 회복 phase 안의 반복 사망도 모두 같은 State 후보에 반영하며,
Deaths 배열의 AfterMessages로 사망 결과와 피해 메시지의 상대 순서를 기록한다.
기존 Death 포인터는 마지막 결과 편의값일 뿐 방송에는 전체 Deaths를 사용한다.
TDD/race로 부활 뒤 HP92까지 이어지는 피해, 최대 HP5에서 두 번 사망/부활해도
드롭은 한 번만 발생, 방문2회/메시지 순서, 두 번째 입장 실패가 첫 사망 후보와
결과까지 모두 거절하는 것을 확인했다. vitals C 비교 회귀/world vet 및 engine
기본 테스트 통과. 실제 PG에서 이 반복 사망 합성 경로는 아직 미검증이고 전체
update_ply의 다른 phase/스케줄링 및 네트워크 출력은 계속 미완료다.

2026-09-08: 실제 PG17에서 TestPostgresRepeatedVitalsDeathAndReplay 통과(race).
독→사망→부활 방 추가 피해→두 번째 사망/부활을 revision1 한 건으로 저장했다.
무기/자식의 바닥 소유는 한 번만 이전되고, 방문2회와 두 사망 결과의 메시지 위치
1/2, 최종 HP5/MP8/PK 및 회복 타이머를 DB에서 재조회했다. 연결 풀 재생성 후
같은 요청은 결과를 재생하고 reducer1회/난수4회에서 증가하지 않았다. 직접 사망
및 이동 PG 회귀도 함께 통과했고 engine vet 통과. 실제 프로세스/DB 종료나
네트워크 출력 시험은 아니다. muhan-vitals-2049781 테스트 컨테이너만 제거했다.

TickLight는 has_light와 update_ply 마지막 조명 소모를 canonical Items에 구현한다.
PLIGHT 마법이 있으면 장비를 소모하지 않는다. 앞 슬롯의 사용 가능한 OLIGHT가
우선하고 type12 LIGHTSOURCE만 shots를1 감소한다. 소진된 조명은 건너뛰며
비소모성 OLIGHT가 먼저면 뒤의 횃불도 소모하지 않는다. ExtinguishedID는 저장 후
본인/방 메시지에 사용할 신호다. TDD/race로 첫 사용 가능 슬롯·마법 우선·소진 후
다음 조명·비소모성·입력 불변성을 확인했고 vitals/death 회귀와 world vet 통과.
전체 update_ply 연결 및 실제 조명 꺼짐 출력/DB 저장·C 실행 비교는 아직 남아 있다.
소스 순서는 LT_HOURS→효과 만료→vitals(사망 후 계속)→LT_PSAVE→조명 소모다.

UpdatePlayer가 위 순서의 구현된 phase를 한 State 후보로 결합한다. LT_HOURS
산술은 int64로 계산하고 ILP32 범위 초과 시 전체 거절한다. 결과는 Effects→
Vitals(Deaths 포함)→ExtinguishedID 순서로 전달할 자료다. SaveDue는 원본 자동
저장 지점의 메타데이터이며 Go는 조명 소모까지 포함한 최종 상태를 매번 원자 저장
해야 한다. 원본 파일 저장 지점과 Go 최종 커밋 지점의 차이를 의도적으로 명시한다.
TDD/race로 조명 마법 만료 후 같은 update의 횃불 소진, 누적 시간/저장 타이머,
누적 시간 overflow의 zero 결과, 사망 중 떨어뜨린 횃불을 이후 소모하지 않는
순서를 확인했다. 관련 vitals/death/light 회귀와 world vet 통과. 전체 C 함수
차등 검증, 실제 PG의 UpdatePlayer 경로, 스케줄러와 세션 출력은 아직 미완료다.

2026-09-08 후속: TestPostgresPlayerUpdateAndReplay를 실제 격리 PG17/race로
통과했다. UpdatePlayer에서 반복 사망을 포함한 최종 State와 결과를 한 revision에
저장하고 연결 풀 재생성 후 재시도해 reducer1회/RNG4회를 유지했다. LT_HOURS
누적100/LastTime100 및 LT_PSAVE LastTime100/SaveDue도 DB/영수증에서 확인했다.
직접 사망·vitals 반복 사망·이동 PG 회귀3개도 함께 통과, engine vet 통과.
이 fixture는 조명 소진이나 개별 효과 만료의 실제 PG 검증까지 포함하지 않는다.
고유 테스트 컨테이너 muhan-update-2072812만 중지/제거했다. CI·배포 없음.

UpdatePlayers는 명시적 서버 소유 순서로 모든 Online 플레이어를 정확히1회
갱신하고 결과도 그 순서대로 반환한다. 누락/중복/없는/미이관 플레이어는 실행 전
거절하며 후행 플레이어 실패 시 앞선 변경/결과도 모두 폐기한다. 원본 update.c는
Ply descriptor 순으로 update_ply를 호출하므로 실제 런타임이 이에 대응하는 순서를
확정해야 하며 map 순서나 알파벳 정렬로 조용히 대체하지 않는다. TDD/race로 순서,
전체 갱신, 잘못된 입력의 무난수, 후기 실패 원자성을 확인했고 관련 회귀/vet 통과.
NPC/방/global 단계, 시간 구동 스케줄러, 세션 연결 및 이 배치의 PG 검증은 미완료다.

같은 원본 루프에서 class==DM(12)은 갱신 전에 건너뛰는 것을 확인했다. 따라서
위의 배치 완전성 조건은 Online 중 non-DM 전원이며, DM을 명시적으로 넣으면
거절한다. DM 제외 회귀 테스트도 통과했다. idle timeout/fd 유효성은 세션 계층에서
먼저 반영해야 하며 현재 배치가 구현한 접속 해제 기능으로 간주하지 않는다.

2026-09-08 전체 `go test ./...` 재실행: TestRoomBodyCorpus의 기존63/3216
거절만 실패했으며 다른 패키지는 통과했다. DB 환경 미지정이므로 전체 PG 검증을
뜻하지 않는다. 실패 방을 skip/삭제/자동 수정하지 않았다.

LeavePlayer는 세션 종료의 재실 상태 변경을 구현한다. 캐릭터의 위치/HP/장비와
방 바닥은 보존하고 Online=false 및 해당 방 ID만 제거한다. 마지막 재실자 여부를
NPC 비활성 신호로 돌려준다. TDD/race로 원본 불변성/깊은 복사/다른 재실자 보존/
중복 호출 거절을 확인했고 관련 회귀/vet 통과. full quit 명령은 아니며, 실제
호출 전에 세션 세대 검증이 필요하고 동일 요청 재시도는 executor receipt로 처리한다.

session.Ownership은 프로세스 내 actor별 연결 예약과 단조 증가 세대 번호를
제공한다. 이전 lease의 Release가 새 접속을 지우지 않으며 동시32개 Acquire 중
하나만 성공한다. Ordered는 독립된 접속 수락 순서 스냅샷이다. TDD/race로 중복
접속·오래된 해제·동시 경쟁·순서/복사·잘못된 입력을 확인했고 session vet 통과.
중요: DB fencing/다중 프로세스 잠금이 아니며 Owns 확인 후 비동기 쓰기만으로는
충분하지 않다. 런타임 직렬 처리 루프에서 검증과 명령 적용을 함께 해야 한다.
또한 수락 순서는 원작 descriptor 재사용 순서와 다를 수 있어 자동으로 원작
스케줄링과 같다고 주장하지 않는다. 실제 입장/명령/퇴장 연결은 다음 작업이다.

Ownership.Run은 세대 확인과 command callback을 같은 프로세스 잠금 아래 실행해
Release/새 Acquire와의 틈을 없앤다. callback은 context로 제한된 executor 저장/
영수증 복구를 수행할 자리이며 Ownership 메서드를 재호출하면 안 된다. 실패 시
예약을 유지해 불확실한 저장 결과를 해결하기 전 새 접속을 허용하지 않는다.
TDD/race로 stale 명령의 callback 미실행, 적용 중 Release 직렬화, callback 오류
보존과 예약 유지, nil callback 거절을 확인했고 session vet 통과. 실제 executor/
WebSocket 연결, 종료 저장 후 release 원자 조합, DB writer fencing은 아직 남았다.

Ownership.Finish가 종료 callback 성공 후에만 예약을 해제하도록 결합했다. 실패는
closing 상태로 남아 추가 Run/Acquire/일반 Release를 거절하고 Finish 재시도만
허용한다. 오래된 종료는 callback 자체를 실행하지 않는다. nil callback은 closing
전환도 하지 않는다. TDD/race로 실패→명령/재접속 차단→성공 재시도→새 세대
접속→과거 종료 거절을 확인했고 session vet 통과. callback에 실제 LeavePlayer/
executor receipt를 연결하는 작업과 다중 프로세스 writer fencing은 남아 있다.

Ownership.Depart를 Finish→engine.Execute→State.LeavePlayer에 연결했다. 요청에
actor/세대를 포함하며 commandID는 호출자가 부팅 간 고유하게 발급하고 재시도에
보존해야 한다. 가짜 저장소 테스트로 commit 실패 시 응답/해제 없음, closing 일반
Release 차단, 같은 명령 재시도 성공 후 Online/재실자만 제거, 새 세대 뒤 과거
Depart가 저장소에 도달하지 않음을 확인했다. session/engine race 및 session vet
통과. 이 조합의 실제 PG 검증과 WebSocket 종료 호출은 아직 미연결이다.

2026-09-08: 실제 PG17에서 TestPostgresDepartureRecoversLostReplyBeforeRelease
통과(race). DB commit은 성공시키고 응답 및 즉시 receipt 조회만 오류로 주입했다.
Online=false/재실자 제거/HP보존/revision1은 DB에 저장됐지만 연결 예약은 closing으로
유지돼 새 접속을 거절했다. 같은 종료 재시도는 receipt를 읽고 commit 총1회에서
증가하지 않은 채 예약을 해제했다. session vet 통과. 물리적 네트워크 단절이 아닌
저장소 wrapper 오류 주입이며 WebSocket 종료 호출은 아직 미연결이다.
고유 PG 컨테이너 muhan-depart-2117439만 중지/제거했다. CI·배포 없음.

Ownership.EnterWorld를 Run→engine.Execute→EnterSavedPlayerWithIDs에 연결했다.
세대/time/view를 요청에 포함하고 canonical Items 없는 캐릭터는 거절한다. 자격
확인 및 완전한 캐릭터 초기화는 호출 전 요구 조건이며 현재 생성 draft를 곧바로
입장시키는 기능이 아니다. 가짜 저장소에서 입장 commit 실패의 무응답/예약 유지,
성공 후 Online/재실자/방문1회 저장, 같은 요청 receipt 재생의 무재실행을 검증했다.
session race/vet 통과. 실제 PG 입장 wrapper 검증, pending/admitted 실행 단계 구분,
WebSocket 게임 세션·신규 캐릭터 초기화 연결은 계속 남아 있다.

Ownership의 pending/admitted 단계를 구분하고 EnterWorld는 Admit를 통해 DB
성공/receipt 확인 뒤에만 admitted가 된다. RunGame은 admitted 및 현재 세대·비종료
상태를 함께 확인한다. 입장 실패·pending·closing·재접속 새 세대는 게임 callback을
실행하지 않는다. TDD/race로 위 전이를 확인했고 session vet 통과. Run은 예약
수준의 내부 API이며 실제 게임 dispatch는 RunGame을 사용해야 한다. 네트워크
입력과 명령 저장 실행기를 여기에 연결하는 작업은 아직 남아 있다.

Ownership.ExecuteGame을 RunGame→engine.Execute에 연결했다. receipt 요청은
kind/접속 actor/세대/payload로 구성하고 reducer에는 접속 actor를 별도 인자로
전달한다. payload 안의 actor는 권위가 아니며 실제 명령 parser가 허용 필드를
검증해야 한다. 가짜 저장소로 pending 명령의 저장/reducer 미실행, 접속 actor
바인딩, 성공 영수증 재생의 무재계산을 확인했다. session/engine race와 session
vet 통과. 이것은 명령 실행 경계이며 실제 look/go parser, WebSocket loop 및
이 wrapper의 실제 PG 통합은 아직 미완료다.

State.CurrentScene은 현재 접속자의 방/바닥 ID projection/재실자를 읽고 시각 옵션을
캐릭터 플래그와 장비에서 계산한다. 다른 접속자의 조명도 검사하며 미이관 장비는
추측하지 않고 거절한다. PDSCRP는63, PBLIND는42로 연결했다. canonical 바닥 표시,
자기 자신 제외, 눈먼 상태의 바닥 숨김 테스트/race와 world vet 통과. 대상 look/
전투 안내 및 명령 parser/네트워크 호출은 아직 미완료다.

ExecuteLookLine은 원본 global.c의 정확한 대상 없는 별칭 '봐'/'보다'/'조사'를
RunGame→executor→CurrentScene에 연결한다. 대상 포함/다른 명령을 현재 방 보기로
대체하지 않고 ErrUnsupportedLookLine으로 구분한다. Hour는 서버 소유 입력이며
명령 영수증에 포함된다. 가짜 저장소에서 방 출력, 원본 State 무변경, receipt 재생,
미지원 입력 거절을 검증했고 session race/vet 통과. 실제 C 전체 parser의 축약/
인자 순서/대상 조회 및 WebSocket 호출은 아직 미완료다. 읽기 명령도 현 executor
정책상 결과 영수증과 revision을 기록하지만 게임 State 자체는 변경하지 않는다.

NewGameHandler에 검증된 storage.Character를 받는 GameConnector/Open 및
GameConnection/Submit/Close 경계를 추가했다. 로그인 뒤 연결된 게임이 있으면
소켓을 유지하고 다음 줄을 게임으로 전달한다. 종료는 소켓 context와 별도의
제한 시간 context로 정리한다. 실제 WebSocket 테스트에서 로그인→게임 화면→
명령→소켓 단절→정리 호출을 확인했고 transport/session race와 transport vet 통과.
테스트 게임은 stub이며 실제 월드/PG connector는 아직 미구현이다. 기본 NewHandler
동작은 유지하고 운영에서 draft를 입장시키지 않는다. connector는 미확인 종료의
예약/재시도 책임을 가져야 하며 현재 서버 실행 진입점에 게임을 켜지 않았다.

2026-09-08: 실제 PG17+WebSocket에서 TestWebSocketPostgresWorldAdmissionLookDeparture
통과(race). 계정 인증은 stub, 월드 캐릭터는 사전 초기화 fixture이며 입장/보기/
퇴장 계산과 영수증 저장은 실제 구현이다. 로그인→광장/검 표시→'봐'→소켓 단절→
퇴장 저장/예약 해제 후 DB revision3, Online=false, HP30, 방문1회, 바닥 ID 보존을
확인했다. transport vet 통과. adapter는 테스트 전용 단일 연결용이며 production
재시도 supervisor/신규 가입 초기화/전체 게임 플레이 검증으로 일반화하지 않는다.
고유 테스트 컨테이너 muhan-socket-2165105만 중지/제거했다. 운영 진입점 변경 없음.

RaisePlayerLevel은 원본 up_level 1회를 구현한다. 0→1에서 직업별 HP/MP/공격
주사위를 초기화하고, 이후 홀짝 증가 및 4레벨마다 능력치/HP/MP 재계산을 보존한다.
특히 4배수의 재계산은 gain*(level-1)/2 순서이며 다른 레벨에서는 현재 HP/MP를
채우지 않는 조기 return을 유지했다. 숫자 overflow는 전체 거절한다. 시작 검사/
1→4 순서/4레벨 보정/overflow 테스트와 기존 level-down race 회귀/world vet 통과.
실제 C up_level 실행 비교 및 생성 draft→완전한 캐릭터 초기화/저장은 다음 작업이다.

추가 검증: TestRaiseLevelAgainstLegacy가 원본 player.c의 up_level 함수와
global.c의 두 테이블을 직접 추출·컴파일해 Go 결과와 비교한다. 12개 직업 각각
시작 레벨 0..254에서 한 단계 상승 및 255까지 반복 상승한 6,120개 사례에서
레벨, HP/MP 최대·현재 값, 공격 주사위 세 값, 능력치 다섯 값이 일치했다.
`go test -race ./internal/world -run '^Test(RaiseLevel|LowerLevel|ApplyDeathProgression)' -count=1 -v`
통과: 기존 C 레벨 하락 24,480개 사례와 Go overflow 거절 회귀도 포함한다.
이 비교는 정상 수치 범위의 레벨 계산 증거이며 C signed overflow 동작이나
전체 가입 초기화/실제 게임 플레이를 검증한 것은 아니다. C는 테스트 시에만
실행하며 Go 운영 런타임 의존성을 추가하지 않았다. 생성 draft의 완전한 초기화와
원자적 저장·입장 연결이 다음 작업이다. CI·push·배포는 실행하지 않았다.

NewPlayer(name, CreationChoices)는 원작 생성 선택을 BuildCreation으로 검증하고
이름 정규화, 종족 보정 능력치/무기 숙련도, 1레벨 HP/MP/주사위, 500금, 시작 방1,
성별·성향·프롬프트·echo 플래그와 빈 canonical 아이템 소유권을 구성한다.
장비 없는 AC/THAC0도 계산하지만 Online=false이며 입장이나 DB 저장을 수행하지
않는다. 기존 캐릭터를 받거나 초기화하는 API가 아니며 암호도 월드에 넣지 않는다.
첫 테스트에서 미구현 함수 실패를 확인한 후 구현했고 NewPlayer/RaiseLevel race
회귀와 world vet를 통과했다. 두 캐릭터의 아이템 맵 격리와 부적절한 생성 선택
(관리자 직업 포함)의 부분 결과 없는 거절도 확인했다. 저장된 Creation 초안을
검증해 이 생성 경계로 연결하고, 계정 생성·월드 등록을 원자적으로 저장하는 작업은
아직 남아 있다. 로그인 일일치/타이머/방 입장은 별도 기존 입장 전이가 담당한다.

Creation.Choices는 저장된 초안을 원래 선택값으로 복원한 뒤 BuildCreation과
정확히 일치하는지 검사한다. 종족 보정표를 복제하지 않고 기존 생성자를 사용하며
계산 전에 수치 범위를 제한한다. 8직업×8종족×5무기×4성별/성향=1,280개 round-trip,
변조 금액/시작 방/직업/숙련도/능력치 거절 테스트를 통과했다. NewPlayerFromDraft로
검증된 초안만 레벨1 초기화에 연결하고, Postgres.Create도 트랜잭션 전에 같은 검증을
수행한다. DB 없는 테스트에서 이전 코드의 트랜잭션 접근 실패를 확인한 후 검증을
추가해 통과시켰다. 관련 game/storage/world race 및 vet 통과. 실제 PG 통합과
계정 생성·월드 등록의 원자적 연결은 이번 검증에 포함되지 않으며 다음 작업이다.

실제 PG17 재검증: 초안 사전 검증으로 기존 TestPostgresCreateIsAtomic의 잘못된
Gold 입력이 DB에 도달하지 않게 된 검증 공백을 수정했다. 정상 초안으로 account
INSERT 뒤 character INSERT의 일회성 CHECK 제약을 실패시키고 실제 PostgreSQL
오류 코드23514/제약 이름까지 확인한다. 실패 후 계정/캐릭터 수가 각각1로 유지되고
후속 정상 가입·암호 로그인도 통과했다. 고유 컨테이너 muhan-create-2235985의
동적 로컬 포트로 해당 테스트를 race 실행해 통과했다. 이 증거는 계정+초안의
원자성만 대상으로 하며 월드 등록 트랜잭션은 아직 미구현이다.

CreateInWorld를 추가했다: 같은 월드 행을 먼저 잠그고 revision을 확인한 뒤 계정,
캐릭터 초안, 해당 ID의 초기화된 offline PlayerState를 한 트랜잭션으로 저장한다.
시작 방/기존 월드 이름/상태 무결성을 확인하며 기존 캐릭터를 덮어쓰지 않는다.
실제 PG17 TestPostgresCreationIncludesWorld(race)에서 마지막 world UPDATE를
CHECK 제약으로 실패시킨 뒤 계정/캐릭터 행이 모두0인지 확인했다. 정상 재실행은
동일 ID로 계정/월드가 연결되고 revision1, level1, HP56, gold500, canonical Items,
offline 상태가 보존됐다. stale revision은 Bob 계정을 만들지 않고 거절했다.
storage vet 및 전체 패키지 컴파일 검사 통과. 고유 테스트 컨테이너
muhan-registration-2245330만 중지/제거했다. 이 API는 아직 내부 provisioning
경계다. 등록 요청 영수증/응답 유실 복구, 기존 초안의 이관, 터미널 가입 경로 연결은
남아 있으며 commit 오류를 실패 확정으로 간주해 무조건 재등록하면 안 된다.
현재 운영 진입점이나 DB 스키마의 draft stage 의미는 변경하지 않았다.

가입 요청 재생 추가: CreateInWorld는 서버 소유 commandID를 필수로 받고 기존
world_commands에 계정/캐릭터/월드와 함께 등록 영수증을 저장한다. 정규화 이름,
초안, 이미 계산한 credential hash의 digest를 포함한 요청 digest로 동일성을 확인한다.
영수증에는 최종 요청 digest와 캐릭터 ID만 저장한다. 재시도는 기존 revision보다
영수증을 우선하며 같은 commandID에 이름/초안/hash가 달라지면 ErrCommandConflict다.
실제 PG17에서 DB pool 재연결 후 같은 ID 복원, 동시8개 재생, 변경 요청3종 거절,
revision1/영수증1개 유지와 기존 rollback을 race로 검증했다. storage vet 통과.
테스트 컨테이너 muhan-registration-2255993만 정리했다. 네트워크 응답 유실 주입이나
프로세스 재시작 후 등록 조정자의 hash/요청 ID 복구는 아직 미검증이다. 호출자는
재시도마다 비밀번호를 새 salt로 hash하지 않고 동일 hash bytes를 재사용해야 한다.
터미널 가입 조정자/기존 draft 이관/실제 게임 입장 연결은 계속 남아 있다.

WorldAccounts는 기존 터미널 Accounts 인터페이스에서 Register만 월드 등록으로
대체한다. 이름/초안 검증→비밀번호 hash1회→서버 요청 ID1회 생성 후 최대3회 같은
hash/ID로 CreateInWorld를 호출하며 매번 최신 revision을 읽는다. 요청 내용 충돌은
즉시 중단하고 종료 시 hash를 지운다. 로그인/이름 조회는 기존 계정 저장소에 위임해
기존 플레이어를 초기화하지 않는다. fake 저장소에서 commit 응답 오류→revision
충돌→성공 동안 hash/ID 보존, 잘못된 초안의 IO 이전 거절을 검증했다. 실제 Login
대화에 adapter를 넣어 기존 draft-only Register 미호출, 암호 비표시, 성공 검증과
3회 실패 후 접속 종료/미입장을 확인했다. 관련 session race/vet 통과. 이번 테스트는
실제 PG나 WebSocket E2E가 아니며 cmd/muhan의 운영 wiring은 아직 변경하지 않았다.
모든 재시도 후 결과가 불명확하면 이름만으로 소유권을 추정하지 않고 다음 연결의
일반 암호 인증으로 확인한다. 프로세스 재시작/실제 응답 유실 시나리오는 다음 검증이다.

실제 PG17+WebSocket TestWebSocketPostgresRegistrationAndRelogin(race) 통과.
이번에는 빈 Players에서 실제 Postgres 계정 인증과 WorldAccounts로 터미널 생성
대화를 수행했다. 이름/성별/직업/능력치/무기/성향/종족/암호→광장→봐→종료,
새 소켓에서 ALICE/같은 암호→광장→봐→종료를 검증했다. revision4→7,
같은 캐릭터 ID, level1/HP56/gold500/canonical Items, offline/재실자0,
방 방문1→2, 계정/캐릭터 각각1개를 확인했다. 암호 프롬프트 Secret 및 암호가
응답에 포함되지 않는지도 검사했다. 게임 연결은 여전히 테스트 전용 adapter이며
세대별 입장/보기/퇴장 ID를 사용한다. 기존 사전 생성 캐릭터 소켓 테스트도 별도
빈 DB에서 race 회귀 통과했고 transport vet 통과. 고유 muhan-signup-2279698
컨테이너만 정리했다. 브라우저/xterm/모바일 검증, 운영 connector와 시작 복구,
full command loop는 이 테스트에 포함되지 않으며 cmd/muhan은 변경하지 않았다.

CancelWorldAdmission은 실패/불확실 입장의 정리 경계다. 실제 상태가 missing 또는
offline이면 변경 없는 영수증을 저장하고, 이미 online이면 LeavePlayer를 저장한
후에만 예약을 해제한다. Finish 잠금으로 입장과 직렬화하며 실패 중에는 closing을
유지해 Release/새 Acquire를 막는다. actual PG17에서 missing/offline/online 각각
정리 commit 응답과 즉시 영수증 조회를 모두 실패시킨 뒤 같은 ID 재시도로
revision1/commit1/receipt replay/예약 해제를 검증했다. 원래 HP30과 캐릭터 존재
여부를 보존하고 재실자는0이다. 기존 departure lost-reply 테스트도 race 회귀,
session vet 통과. 고유 muhan-cancel-2290017 컨테이너만 정리했다. 아직 운영
connector의 정리 재시도 루프와 cross-process fencing/startup recovery는 미연결이다.

CleanupQueue/Run을 추가했다. Enqueue 즉시 Ownership.Seal로 입장/명령/Release를
막고 세션별 단일 서버 생성 cleanup ID를 유지한다. 제한 시간 있는 Retry와 고정
주기의 Run은 실패 기록/예약을 유지하며 성공한 작업만 제거한다. Pending으로 남은
작업과 마지막 오류를 확인할 수 있다. NewWorldCleanupQueue가 실제
CancelWorldAdmission에 연결된다. 단위 테스트에서 중복 enqueue1건, 실패→성공의
동일 ID 유지/예약 해제, 취소된 worker의 IO 금지와 미완료 작업 보존을 race로
검증했고 session vet 통과. 큐는 프로세스 로컬이며 종료 시 명시적 drain과 cold-start
복구가 필요하다. 이번 검증은 실제 PG worker 통합이 아니며 운영 connector는 아직
이 큐를 사용하지 않는다. 큐의 배치 재시도 중 enqueue는 직렬화되므로 운영 연결에
앞서 느린 DB에서의 큐 대기/종료 시간 및 전체 연결 상한을 검증해야 한다.

실제 PG17 TestPostgresCleanupQueueSurvivesWorldLockTimeout(race) 통과.
별도 트랜잭션으로 월드 행을 FOR UPDATE 잠근 상태에서 실제 큐의50ms 정리 시도를
실패시켰다. 요청 ID/마지막 오류/예약이 남고 영수증0개임을 확인한 뒤 잠금을
해제하고 같은 큐를 재시도했다. 최종 revision1/동일 commandID 영수증/예약 해제,
offline/HP30 보존/재실자0을 확인했다. 기존 missing/offline/online 입장 취소와
departure 응답 유실 테스트도 같은 격리 DB에서 race 회귀 통과, session vet 통과.
고유 muhan-cleanup-2312265 컨테이너만 정리했다. 이 테스트는 실제 DB 잠금 대기
타임아웃과 수동 Retry 검증이며 worker Run의 동시 enqueue/종료 지연, 운영 connector
연결이나 프로세스 재시작 복구가 완료됐다는 뜻은 아니다.

CleanupQueue의 IO 중 metadata 잠금 유지 문제를 실패 테스트로 확인하고 수정했다.
정리 실행은 취소 가능한 단일 drain gate로 직렬화하고, Pending 복사/결과 반영 때만
metadata를 잠근다. enqueue의 seal+insert는 별도 직렬화해 완료된 lease를 뒤늦게
다시 넣는 경쟁을 막는다. DB 응답을 기다리는 동안 Pending 조회와 취소된 두 번째
Retry가 반환하는 테스트, 실행 중 worker context 취소가 작업 오류/예약을 보존하며
종료하는 테스트를 추가했다. cleanup race 회귀와 session vet 통과. Ownership 자체의
DB 직렬화와 Seal 대기는 남아 있으며 이를 무제한 동시 접속 지원으로 해석하지 않는다.
실제 PG의 변경 후 재검증과 운영 connector wiring은 다음 작업이다.

transport.WorldConnector를 추가했다. 실제 Ownership 입장/보기와 CleanupQueue를
연결하고 세션별 connection 상태, 부팅 간 고유 요청 ID, pending 예약을 포함한
MaxSessions 제한을 제공한다. Open 실패도 정리할 connection을 반환하며 Close는
큐에 봉인/등록 후 정리를 시도하고 남은 실패는 RunCleanup가 맡는다. 기존 가입+
재로그인 PG17/WebSocket 테스트를 pgGameFixture 대신 이 실제 구현으로 바꾸어
race 통과했다(테스트 wrapper는 Close 완료 알림만 관찰). 매 종료 후 Pending0,
기존 revision4→7/동일 캐릭터/HP56/gold500 보존을 확인했다. metadata 잠금 개선 후
실제 PG cleanup timeout/입장 취소/departure 응답 유실 회귀도 race 통과, transport
vet 통과. 고유 muhan-connector-2329775 컨테이너만 정리했다. 현재 dispatcher는
정확한 대상 없는 보기 별칭만 구현하며 나머지는 미구현 안내다. 서버 시작 fencing/
복구와 worker 수명·종료 drain을 cmd/muhan에 연결하기 전에는 기본 실행 경로를
전환하지 않는다. 전체 게임 플레이나 배포 완료가 아니다.

WorldConnector.Shutdown을 추가했다. 먼저 새 lease 발급을 차단하고 기존 예약을
정리 큐에 넣어 drain하며, 기한이 끝나면 실패/예약을 보존해 재호출할 수 있다.
실제 PG/WebSocket 가입·재로그인 테스트의 두 번째 온라인 접속 중 월드 행을
잠그고50ms Shutdown을 실패시켰다. Pending1과 새 Open 거절을 확인한 뒤 잠금을
풀고 Shutdown 재시도로 Pending0/offline/재실자0/revision7/HP56/gold500을
확인했다. 이후 소켓 Close에서도 저장이 중복되지 않았다. 해당 race 테스트와
transport vet 통과. 고유 muhan-shutdown-2342004 컨테이너만 정리했다. 이 API는
리스너/소켓 자체를 닫거나 cleanup worker를 취소하지 않으며 실행 호스트가 이를
조정해야 한다. cmd/muhan wiring 및 서버 시작 fencing/복구는 계속 남아 있다.

writer_epoch와 명시적 ClaimWorldWriter를 추가했다. takeover가 월드 행을 잠그고
세대를 증가시키며 반환된 불변 저장 핸들만 해당 세대로 저장한다. CommitWorldCommand와
CreateInWorld는 같은 행 잠금 안에서 epoch를 검증하므로 새 세대 이후 구 서버/미지정
핸들의 쓰기를 거절한다. 영수증 조회도 세대를 확인한다. epoch0은 아직 인계되지 않은
내부 테스트/프로비저닝 경로이며 한번 인계된 월드에는 일반 핸들로 저장할 수 없다.
PG17 race에서 A 저장→B takeover→A 명령 재생/신규 가입 거절→B 저장을 검증했다.
실제 WebSocket 가입·재접속·종료 지연 테스트도 writer 핸들로 전환해 race 통과했다.
storage vet 통과, 고유 muhan-writer-2351638 컨테이너만 정리했다. 자동 리더 선출이나
기간 lease는 아니며 takeover를 fenced 오류 재시도로 자동 호출하면 안 된다.
claim 응답 유실/시작 복구/구 서버 소켓 종료 조정과 cmd/muhan 연결은 다음 작업이다.

ClaimWorldWriter에 서버 소유 boot claim ID와 world_writer_claims 영수증을 추가했다.
월드 행 잠금 안에서 epoch 증가와 claim 기록을 한 트랜잭션으로 저장하고 동일 ID의
재시도는 현재 epoch와 일치할 때만 같은 핸들을 반환한다. 다른 boot가 인계받은 뒤
이전 claim ID 재사용은 ErrWriterFenced이며 다시 세대를 증가시키지 않는다.
실제 PG17에서 동일 claim의 epoch 보존과 A→B 이후 A claim 재사용 거절을 race
검증했고, 새 API를 사용한 실제 WebSocket 가입/재접속/종료 회귀도 통과했다.
storage/transport vet 통과, 고유 muhan-claim-2362793 컨테이너만 정리했다.
실제 네트워크 claim 응답 유실 주입은 아직 수행하지 않았으며 서로 다른 프로세스가
같은 boot ID를 공유하면 안 된다. 자동 선출/구 서버 연결 종료/시작 복구는 남아 있다.

engine.StartWorld가 명시적 writer 인계→RecoverOffline 후보→영수증 포함 저장을
연결한다. 복구 완료 전에는 writer를 반환하지 않고 오류 시 nil을 반환한다. 같은
boot ID 재시도는 같은 startup 영수증을 재생하며 플레이 중 호출해서는 안 된다.
실제 PG17 race에서 online 캐릭터의 offline/재실자 제거, HP37/gold777/아이템 kept
보존, revision1 재생, 잘못된 월드의 준비 상태 거절을 검증했다. 실제 WebSocket
가입·재접속 테스트도 StartWorld를 통과한 핸들로 전환해 race 통과했다. 시작 영수증
1회가 추가되어 최종 revision은5→8이다. engine vet 통과, 고유
muhan-startup-2370971 컨테이너만 정리했다. cmd/muhan 실행 경로 연결과 프로세스
종료/재시작 장애 주입, 전체 리소스 corpus 인수는 여전히 남아 있다.

cmd/muhan에 명시적 opt-in 월드 실행 경로를 연결했다. `-world <기존월드ID>`와
`-templates <mNN/oNN 디렉터리>` 및 `-game-hour <0..23>`가 필요하다. 이미 준비된
월드만 StartWorld로 인계/복구하며 자동 import나 테스트 월드 생성은 하지 않는다.
WorldAccounts/WorldConnector/cleanup worker를 연결하고 신호 종료 시 HTTP 종료,
월드 drain, worker 종료를 기다린 후 DB를 닫는다. 기본 옵션은 기존 draft 경로를
유지한다. 도움말 실행, cmd 컴파일/vet 통과. 실제 cmd 프로세스의 PG/WebSocket
E2E와 신호 종료 검증은 아직 수행하지 않았다. 게임 시간은 임시 명시적 고정 입력이며
지속 게임 시계/tick scheduler를 대체하지 않는다. 명령 dispatcher도 아직 보기만
구현되어 있어 운영 플레이 완성을 의미하지 않으며 배포/CI/push는 실행하지 않았다.

실제 실행 파일 TestProcessWorldSignupRestartAndSignalDrain 통과: Go 서버를 -race로
빌드해 별도 프로세스로 두 번 실행했다. PG17의 빈 월드에서 터미널 가입/봐 후
소켓을 유지한 채 SIGTERM을 보내고 정상 프로세스 종료 뒤 offline/HP56/gold500/
재실자0/revision5를 확인했다. 다음 프로세스가 새 boot로 인계/복구한 뒤 같은 계정
로그인/봐/SIGTERM 후 같은 캐릭터와 revision9를 확인했다. 고정 게임 시각12와
스폰 없는 방 fixture를 사용했으며 템플릿 읽기·전체 게임·브라우저 검증은 아니다.
실행 포트는 127.0.0.1:0 자동 할당이고 main은 실제 listener 주소를 보고한다.
cmd race 테스트/vet 통과. 테스트의 두 자식 프로세스는 종료됐고 고유 DB 컨테이너
muhan-process-2390741만 정리했다. SIGKILL/DB 중단 같은 비정상 종료 검증은 남아 있다.

TestProcessWorldRecoversAfterSIGKILL 추가 및 실제 PG17/race 통과. 첫 Go 프로세스를
가입/봐 후 실제 SIGKILL로 종료했고 종료 상태가 SIGKILL임을 확인했다. DB는 예상대로
revision4/online/재실자1을 유지했다. 두 번째 프로세스의 listener가 열린 뒤 재로그인
전에 DB offline/재실자0을 직접 확인했고, 같은 이름/암호 접속/봐/정상 종료 후
revision8/동일 ID/HP56/gold500을 검증했다. 정상 SIGTERM 2회 실행 테스트도 회귀
통과했다. 총4개 자식 서버는 모두 종료됐고 고유 muhan-crash-2399868 컨테이너만
정리했다. cmd vet 통과. 이 증거는 스폰 없는 단일 방 fixture의 프로세스 장애 복구이며
DB 서버 중단/백업 복원/전투 중 장애나 전체 데이터 이관 인수를 대신하지 않는다.

방향 이동 조사에서 중요한 분기를 확인했다: global.c의 북/8 등은 command2.c
move이고 기존 MovementInput/PlanMovement는 command6.c go 기반이다. move는
정확한 출구 이름을 찾고 XNOSEE만 제외하지만 go는 prefix/occurrence와 XINVIS
검사를 한다. move의2/4/6/8/3/9 및 한글 자모/나가 변환, 별도의 초기 거절 문구를
SelectDirectionalExit/DirectionalExitRestriction으로 분리했다. 북문/북 중 정확
일치, XNOSEE 제외/XINVIS 허용, numeric 첫 글자 변환, 거절 순서/문구 테스트가
실패→구현→race 통과했고 world vet 통과. 기존 go 계산에 방향 명령을 그대로
연결하지 않았다. move의 이후 경비/낙하/도착/추종/함정/사망 및 실제 dispatch는
아직 남아 있다. 레거시 비UTF-8 화살표 리터럴은 입력 디코딩 계약이 별도로 필요하다.

EvaluateDirectionalTraversal로 move의 초기 통과/경비/성별/휴대 무게/낙하 단계를
분리했다. move는 성별 제한 뒤 무게를 검사하며 go와 문구/비치명적 낙하 마침표가
다르다. 공통 낙하 산술은 evaluateTraversalFall로 공유하되 경로별 문구를 유지했다.
성별 우선순위, 경비 차단, rappel 손상 후 계속, 치명적 낙하 정지, 잘못된 RNG의
부분 상태 없는 거절을 TDD로 검증했다. 기존 traversal/movement 및 방향 테스트
race 회귀와 world vet 통과. 원본 C 직접 실행 differential과 낙하 사망의 실제
월드 전이, 은신/도착/추종/함정 및 터미널 방향 dispatch는 계속 남아 있다.

PlanDirectionalMovement가 move의 출구 선택→방향 traversal→은신/차단→흔적→
목적지 제한을 묶는다. 기존 go와 공통 계산은 공유하지만 방향 경로는 공격 cooldown을
적용하지 않으며 정확한 출구/없는 길/없는 지도 문구를 유지한다. move의 최대 레벨
안내는 go의 high+1이 아닌 high이며 가족방 문구도 경로별로 보존한다. 방향8의 북
정확 선택과 cooldown 무시, 기존 go cooldown 유지, 목적지 거절 시 흔적 보존,
최대 레벨/지도 없음 테스트를 TDD로 추가했다. 관련 directional/movement/
destination/traversal race 회귀 및 world vet 통과. 실제 C differential, 플레이어
입력 능력치 구성, 방 입장·추종·함정·사망 적용과 터미널 dispatch는 미완료다.

DirectionalTransferWithIDs가 방향 이동 계획을 기존 canonical 방 입장/상태 후보
적용에 연결한다. 공통 transfer는 이동 계산기를 주입받도록 분리해 go 경로를
보존한다. 실제 State에서 8→북 이동 시 위치/양쪽 재실자/흔적, 목적지 검 표시,
양쪽 바닥 ID 보존 및 입력 불변을 검증했다. 레벨 제한 거절 시 출발 흔적만 갱신하고
목적지/스폰/캐릭터 위치는 그대로임을 확인했다. TDD 실패→구현→directional/
canonical transfer/movement race 회귀 및 world vet 통과. 낙하 사망과 추종/함정이
연결되기 전의 중간 후보이며 아직 실제 명령의 저장/터미널 호출에는 사용하지 않는다.

DirectionalStep이 방향 transfer 후보에 치명적 낙하의 원작 die(player,player)를
연결한다. 결과 Transfer는 사망 전 이동 사건이고 반환 State만 최종 저장 후보다.
낙하→기존 사망/아이템 드롭→1008 부활 중 오류가 나면 부분 후보 없이 거절한다.
HP5 낙하 사망에서 난수3회(추락/손상/사망 타이머), HP100 부활, weapon ID의
출발 바닥 이동, 재실자 이전, 원래 목적지 미방문과 입력 불변을 확인했다. 부활 방
후기 오류 시 전체 후보 거절 테스트도 추가했다. TDD 및 directional/player-death/
canonical transfer race 회귀와 world vet 통과. 실제 PG 저장/재생, 추종/도착 함정,
원본 C differential 및 터미널 방향 입력 연결은 계속 남아 있다.

실제 PG17 TestPostgresDirectionalFallDeathAndReplay(race) 통과. HP5에서 북쪽
climb 낙하→자기 사망→1008 부활을 executor로 저장하고 DB pool을 닫았다가 다시
열어 같은 명령을 재생했다. reducer1회/RNG3회/revision1, HP100/MP8/사망 타이머,
weapon→gem 하위 ID/드롭 플래그 보존, 출발 재실자0/부활 재실자1/방문1을 확인했다.
사망 전 이동 사건과 부활 결과 영수증도 보존됐다. 기존 직접 사망/반복 vitals 사망/
player update PG 회귀도 race 통과, engine vet 통과. 고유 muhan-fall-2461997
컨테이너만 정리했다. 실제 명령 입력의 상태 구성과 추종/함정/방향 dispatch는 남아 있다.

MovementStats는 canonical 아이템 ID에서 원작 weight_ply/weight_obj/fall_ply의
휴대 무게·등반 장비·민첩 보너스를 계산한다. 무중량 인벤토리/하위 트리는 제외하되
ready 루트는 무중량이어도 계산한다. 지원하지 않는 민첩 인덱스와 무게 범위 초과는
부분 결과 없이 거절한다. canonical 아이템이 있는 양쪽 transfer 경로는 전달받은
무게/낙하/민첩 값을 덮어써 저장된 값을 사용한다. 아직 이관 전 Items=nil 경로는
기존 서버 입력 계약을 유지하며 이 변경만으로 전체 입력 권위 구성이 완료되진 않는다.
무게 제한 우회 실패→수정→통과, 장비로 추락 회피, 실제 민첩으로 은신 실패,
잘못된 저장 능력치의 후보 거절을 race 테스트로 확인했다. 관련 movement/
directional/canonical transfer 회귀와 world vet 통과. engine/session/transport/
cmd 로컬 race 테스트도 통과했으며 DB 환경 변수가 필요한 테스트는 별도다.
격리 PG17에서 TestPostgresDirectionalFallDeathAndReplay를 race로 다시 통과했고
이번 작업의 muhan-movement-2484523 컨테이너만 정리했다. 전체 world race 실행은
기존 TestRoomBodyCorpus의 3,216개 중 63개 이관 실패로 여전히 실패한다.

추가 원본 비교: Luna max가 작성하고 오케스트레이터가 검토·재실행한
TestMovementEquipmentAgainstLegacy는 원본 C의 weight_ply/weight_obj/fall_ply와
bonus 표를 추출해 실제 실행한다. 5개 아이템 그래프 × 민첩 0..63 = 320개에서
무게/낙하/민첩 결과가 일치했다. 무중량 루트/하위 트리, 형제 아이템, 여러 ready
등반 장비와 signed short 경계를 포함한다. 비교 입력의 형제 연결 덮어쓰기 및
32비트 범위를 넘는 shift 문제도 검토에서 수정했다. 통합 race 재실행 통과.
양/음 무게 합산 overflow 거절은 원본 C UB를 재현하지 않는 별도 Go 정책 테스트다.

이동 입력 권위 추가: canonical transfer의 go/move 공통 경로에서 Body의 class/
level, PSILNC/PFLYSP/PINVIS/PMALES/PLEVIT/PBLIND/PDINVI/PFAMIL 및
daily[9]/daily[8] 값을 사용한다. Body에 이미 있는 값은 Items=nil에서도 투영하며,
아이템 미이관 상태의 무게/낙하 계산 계약은 이전과 같다. Astra medium이 원본
move/mtype의 필드 매핑을 독립 확인했고 오케스트레이터가 구현·통합 검증했다.
기존에는 오래된 전달값으로 제한을 통과하던 14개 사례를 실패→수정→통과로 확인했다.
반대로 저장된 비행/가문/결혼/레벨 조건이 충족되면 오래된 거절값을 무시하고 이동한다.
투명 감지, 경비의 투명/직업 예외, 공중부양의 RNG 미소비, 실명 은신 상한도 race로
검증했다. 실제 PG17에서 이동/사망/방향 낙하/반복 vitals/player update 저장·재생이
통과했다. 전체 world race 실행은 여전히 기존 63개 방의 엄격한 corpus 검사로 실패한다.
이 변경은 방향 dispatch 완료가 아니다. Fighting/EnemyOfPlayer는 NPC별 순서 있는
적대 관계가 필요하고 PlayerEnemies로 대체할 수 없다. Invited는 목적지별 초대
명단 이관이 필요하다. 표시용 SceneOptions, 추종/추격/함정/방송도 아직 남아 있다.
추가로 실제 PG 시작 복구 테스트를 별도 DSN으로 실행해 통과했고 session/transport/
cmd race 회귀 및 world/engine vet도 통과했다. 이번 전용 muhan-authority-2681901
컨테이너만 정리했으며 CI/push/배포는 실행하지 않았다.

로그인/이동/부활 화면 권위: ID 장비가 있는 로그인·이동과 모든 canonical 사망은
최종 후보 State의 CurrentScene으로 Entry.Scene을 구성한다. 입력 SceneOptions의
시야/빛/ViewerID를 신뢰하지 않으며 서버 게임 Hour만 유지한다. 목적지의 다른
플레이어 빛, 저장된 실명/표시 설정, 사망 후 드롭한 발광 장비의 빛 제거를
실패→수정→race 통과로 확인했다. 표시 과정에서 필요한 조명 정보가 미이관이면
부분 이동/사망 후보 없이 거절한다. Astra medium 검토에서 과도한 조명 검사도
발견해 수정했다: 밝은 방·실명·종족/직업 시야에는 장비 검사를 하지 않으며,
필요한 어둠에서도 자기/다른 플레이어의 확정된 빛이 있으면 미이관 장비 때문에
실패하지 않는다. 실제 PG17 사망/방향 낙하 영수증의 Scene이 저장 State의 화면과
같음을 추가 검증했고 engine 전체 실제 DB 테스트 및 관련 world/session/transport/
cmd race 회귀, world/engine vet가 통과했다. 전체 world race는 기존 방 63개
corpus 실패가 남아 있다. 전체 명령 dispatch·NPC 관계·추종·함정·방송은 미완료다.
실제 Go 실행 파일(-race)과 PG17의 가입→입장→보기→SIGTERM→재시작 및
SIGKILL→복구→재로그인 두 프로세스 테스트도 통과했다. 이번 전용
muhan-scene-2778555 컨테이너만 정리했다. 브라우저 실사용/IME 인수나 배포를
수행한 것은 아니다.

사유지 초대 권위: State.Invitations는 room.Special을 키로 캐릭터 ID(최대10개)를
저장한다. nil=미이관과 빈 map=확정된 초대 없음은 구분한다. 알려진 명단이 있으면
go/move의 전달된 Invited를 목적지의 실제 ID 명단으로 덮어쓴다. 다른 사유지 초대,
잘못된 ID/중복/슬롯 초과를 거절하고 snapshot roundtrip/clone 독립성을 검증했다.
ImportInvitations는 완전한 원본 명단을 명시적으로 이름→ID 변환하는 순수 후보이며
로그인 훅이 아니다. 원본 strcmp처럼 정확히 이름을 비교하고 중복 슬롯은 합치되
미확인/동명이인/잘못된 입력이나 기존 명단 덮어쓰기는 거절한다. 캐릭터 rename 뒤에도
ID 초대가 유지된다. 실제 파일 인벤토리·디코딩·운영 명단 이관은 아직 미수행이다.
미이관 nil 경로는 기존 서버 입력 계약을 유지하며 완성형 방향 dispatch가 아니다.

다음 NPC 작업 계약(Astra medium 원본 검토, 구현 전): NPC 인스턴스 ID와 방별 순서
있는 NPCIDs를 단일 소유권으로 저장하고 Resource.Monsters는 일시적 projection으로
바꾼다. enemy는 대상 종류/ID와 signed damage를 포함한 순서 리스트여야 한다.
원본 add_enm_crt는 주석과 달리 append이며 end_enm_crt는 tail로 이동한다.
ply_is_attacking은 damage>=0, 은신 blocker는 membership, 추격은 첫 적만 검사한다.
nil 관계는 미해결이며 방 파일에 관계가 없다는 이유로 빈 적대 목록을 추정하지 않는다.
리젠은 새 인스턴스 생성 이벤트에서만 ID를 할당하고 이름/본문 동일성으로 재매칭하지
않는다. 플레이어/NPC 추종은 순서·단일 leader·cycle 검사와 재접속 정리가 필요하다.
추격 중 permanent NPC의 die_perm_crt는 타이머뿐 아니라 퀘스트/XP/출력도 바꾸므로
flag만 지우면 불완전하다. 원작 도착 Scene은 추종자가 오기 전 시점으로 유지한다.

초대 검증 결과: 관련 world race 및 session/transport/cmd 회귀, world/engine vet
통과. 실제 PG17 이동 테스트에 RONMAR 사유지와 초대 ID를 넣어 입장→저장→동일
명령 재생→offline 복구→재로그인 후에도 명단/아이템 ID가 보존됨을 확인했다.
첫 PG 실행은 기존 일반방 fixture의 재로그인 위치 기대값으로 실패했다. 원본
src/player.c:120의 비배우자 재로그인 기본방 복귀 규칙을 확인하여 기대값을 수정했고
새 격리 DB에서 engine 전체 실제 PG race 테스트가 통과했다. 초대는 거주권이 아니다.
전체 world race는 기존 63개 방 corpus 실패가 남아 있다. 전용
muhan-invites-2822634 컨테이너와 그 임시 DB만 정리했다. 운영 명단 변경/CI/push/
배포는 실행하지 않았다.

NPC ID 구현: State.NPCs/RoomState.NPCIDs와 typed enemy target+signed damage를
추가했다. 모든 인스턴스는 정확히 한 방이 소유하며 중복·누락·종류/위치 불일치·
잘못된 적대 참조를 거절한다. ImportNPCs는 방 ID 순서와 방 내부 배열 순서를
유지해 이름이 같은 개체도 개별 ID를 만들고, 기존 방의 embedded monster 소유권을
제거한다. 위치는 원작 add_crt_rom처럼 소속 방으로 정하며 적대 관계는 nil=미해결로
남긴다. 실제 적대 런타임 이관 없이 빈 목록으로 추정하지 않는다. clone/JSON에서
미해결 nil과 확인된 빈 목록을 보존한다. Luna max가 독립 테스트를 작성했고
오케스트레이터가 검토·통합 race 재실행했다.
ProjectRoom을 방 표시와 이동에 연결하고, source NPC 순서에 맞게 실제 적대
membership 및 damage>=0 전투 여부를 계산한다. 알려진 NPC 관계에서 기존 ID를
유지하는 이동·경비 차단은 동작한다. 새 NPC spawn/변형에는 명시적인 identity
event 연결이 아직 필요하며 이를 이름으로 추정하거나 일부 상태만 저장하지 않는다.
미연결 spawn은 후보 전체 거절 테스트로 확인했다. NPC가 있는 로그인/부활 방의
canonical entry 연결, 추종/추격/관계 복구·전체 명령 dispatch는 계속 남아 있다.
실제 PG17 TestPostgresNPCMovementIdentityAndReplay(race)에서 이름이 같은 두 NPC의
순서/ID/음수 damage/빈 관계를 저장하고 DB pool 재개 후 동일 명령을 재생했다.
reducer1회/revision1, 플레이어만 이동, source NPC 소유권과 관계 보존을 확인했다.
engine 전체 실제 PG race, 관련 NPC/world 및 session/transport/cmd 회귀, vet 통과.
전체 world race는 기존 63개 방 corpus 실패가 남아 있다. 이번 전용
muhan-npc-2866611 컨테이너만 정리했으며 실제 이관·CI/push/배포는 하지 않았다.

NPC 콜드 스타트 복구: RecoverOffline에서 확인된 NPC 적대 목록의 모든 player
target을 제거한다. 이미 offline으로 저장된 대상과 음수 damage도 포함하며, 아무
플레이어도 접속하지 않은 시작 시점의 세션 관계를 재조정하는 명시적 Go 복구 규칙이다.
NPC→NPC 순서/damage, NPC ID/위치/본문/인벤토리는 그대로 보존한다. nil=미해결은
빈 평화 목록으로 바꾸지 않으며, 제거 후 빈 목록은 확인된 빈 상태로 유지한다.
실패→구현→race 통과, 원본 불변성과 반복 복구 동일성을 검증했다. 실제 PG17의
StartWorld가 이 상태를 커밋한 뒤 writer를 반환하고, 동일 boot 요청을 revision1로
재생하는 테스트도 통과했다. engine 전체 실제 PG 회귀와 관련 world/session/
transport/cmd race 및 vet 통과. 일반 LeavePlayer에는 이 규칙을 적용하지 않았다:
src/update.c clear_enm_crt는 활성 NPC 중 find_enm_crt>=0만 제거하므로 원작 활성
목록 모델과 함께 따로 연결해야 한다. 이는 원본 로그아웃과의 동일성 검증이 아니다.
추가로 실제 Go 프로세스의 SIGTERM 재시작과 SIGKILL 복구 회귀 두 테스트도
PG17에서 통과했다. 전체 world race는 여전히 기존 63개 방 corpus 실패가 있다.
이번 전용 muhan-recovery-2971850 컨테이너만 정리했고 CI/push/배포는 하지 않았다.

이동 중 NPC 리젠 ID 연결: refreshRoomResourcesWithNPCEvents는 실제 새 개체의
삽입 위치와 본문을 전달한다. planCanonicalNPCEntry는 그 이벤트에서만 ID를 발급하고
확인된 빈 적대 목록으로 초기화한다. 기존 NPC는 이름/본문 매칭으로 재식별하지 않는다.
생성 delta와 방 NPCIDs/아이템/플레이어 이동을 같은 후보에 적용하고 canonical
projection을 검증한 뒤 저장 본문에서 제거한다. 이름이 같은 기존 늑대(HP77)와
새 늑대의 ID/순서 보존, 왕복 재입장 시 추가 할당 없음, 빈/중복 ID 및 후기 아이템
실패의 전체 후보 거절을 검증했다. Astra medium이 원자성/alias/순서 경계를 검토했다.
실제 PG17 NPC 이동 테스트에서도 새 NPC ID를 플레이어 이동과 revision1로 저장하고
DB pool 재개 뒤 동일 명령을 재생했다. reducer1회/ID할당1회, 기존 같은 이름 NPC의
ID/음수 적대 damage와 새 NPC의 빈 관계를 보존했다. engine 전체 실제 PG race,
관련 world/session/transport/cmd 회귀 및 vet 통과. 전체 world race는 기존 63개
방 corpus 실패가 남아 있다. NPC가 있는 로그인·부활 경로는 이 entry/delta 적용을
아직 연결해야 하며, 추종/추격/활성 목록과 터미널 방향 dispatch도 남아 있다.
이번 전용 muhan-npcspawn-2990744 컨테이너만 정리했고 CI/push/배포는 하지 않았다.

NPC 로그인/부활 연결: 두 입장 경로도 planCanonicalNPCEntry와 공통 spawn delta
적용을 사용한다. canonical floor가 없는 로그인도 NPCIDs를 덮어쓰지 않는다.
기존 같은 이름 NPC(HP77)와 새 리젠 ID/순서를 보존하고, 미해결 기존 적대 목록은
그대로 두며 새 개체만 확인된 빈 목록으로 생성한다. ID 충돌 때 접속/사망의
부분 후보를 반환하지 않는 테스트를 실패→구현→race 통과로 검증했다.
실제 PG17 직접 사망/반복 vitals 사망/player update/방향 낙하 테스트에서 부활방
NPC 리젠을 추가했다. 반복 부활도 할당1회이며 영수증 재생 후 기존/신규 ID,
원래 HP/빈 적대 목록/순서가 보존됐다. 실제 PG NPC 이동 테스트에 시작 복구→
NPC가 있는 방 재로그인→동일 로그인 재생도 추가하여 중복 리젠 없이 revision3,
login reducer1회를 확인했다. engine 전체 실제 PG race와 관련 world 및
session/transport/cmd 회귀, vet 통과. 전체 world race는 기존 방 63개 corpus
실패가 남아 있다. 활성 목록·일반 로그아웃 정리·추종/추격·함정·방향 dispatch는
미완료다. 전용 muhan-admission-3086766 컨테이너만 정리했고 CI/push/배포는 하지 않았다.

NPC 활성 순서 계약 수정: C room.c add_ply_rom은 첫 재실자 입장 때만 전체 NPC를
활성화한다. 기존 Go EnsureMonstersActive의 무조건 true를 첫 입장 조건으로
수정했다. update.c add_active는 제거 후 맨 앞 삽입이므로 추가 입장에서 전체를
재활성화하면 행동 순서가 달라진다. 점유된 방의 리젠은 새 개체만 생성 순서로
활성화해야 하므로 npcEntryDelta에 SpawnOrder를 별도로 보존한다. 방 NPCIDs의
이름 정렬이나 Spawned map 순회를 생성 순서로 사용하지 않는다.
첫/추가 입장 × 리젠 유무, 생성 순서 Zebra→Ant와 방 순서 Ant→Middle→Zebra를
구분하는 테스트를 실패 확인 후 구현하여 race 통과했다. Astra medium 읽기 전용
원작 검토와 관련 world 및 engine/session/transport/cmd race, vet를 수행했다.
이번 실행은 실제 PG 환경 변수를 제공하지 않았으므로 DB/프로세스 통합 테스트의
새 실행 증거는 아니다. 전체 world race는 기존 63개 방 corpus 실패가 남아 있다.
활성 목록의 저장·입퇴장 적용·tick 및 일반 로그아웃 적대 정리는 다음 작업이다.
이번에는 Docker/CI/push/배포를 실행하지 않았다.

활성 NPC 목록 저장/입퇴장 연결: State.ActiveNPCIDs는 전역 행동 순서를 저장한다.
nil 미이관과 알려진 빈 목록을 구분하고, 중복/없는 ID를 거절하며 clone/JSON에서
순서와 nil/empty를 보존한다. 첫 로그인/이동/부활은 방 NPC 순서로 remove→prepend,
점유 방 입장은 SpawnOrder만 적용하고 마지막 퇴장은 해당 방 NPC만 제거한다.
콜드 스타트는 알려진 활성 목록을 빈 목록으로 초기화하며 미이관 목록은 추정하지 않는다.
원본 src/update.c add_active/del_active를 추출해 컴파일한 128단계 C/Go 순서
비교가 통과했다. Luna가 작성 중 남긴 테스트는 사용량 한도 중단 후 메인에서
직접 읽고 실행했다. validation/clone/JSON/복구/첫·추가 로그인/마지막·비마지막
퇴장/이동/부활 active 순서 회귀가 race 통과했다.
격리 PG17에서 이동 활성 목록 저장·DB pool 재개 후 명령 재생·시작 복구의 빈
목록·재로그인의 재활성 및 재생을 확인했다. 실제 PG engine 전체 race, 관련 world
및 session/transport/cmd 회귀와 vet 통과. 전체 world race는 기존 63개 방 corpus
실패가 남아 있다. 새 브라우저/실제 게임 프로세스/배포 검증은 이번에 하지 않았다.
일반 로그아웃의 활성 NPC 적대 정리, NPC tick/추종/추격 및 활성 목록 이관은 아직
미완료다. 전용 muhan-active-3181616 컨테이너만 정리했고 CI/push/배포하지 않았다.

일반 로그아웃 NPC 적대 정리 연결: src/player.c uninit_ply의 del_ply_rom 뒤
clear_enm_crt 순서를 따른다. 마지막 재실자 퇴장으로 비활성화된 방 NPC는 제외하고,
남은 활성 NPC의 해당 player ID 관계 중 Damage>=0만 제거한다. 음수 관계와
다른 NPC 대상/순서는 보존한다. 알려진 활성 NPC의 Enemies가 nil이면 전체 후보를
거절하며 앞서 처리한 NPC나 접속 상태도 원본에 누출하지 않는다. ActiveNPCIDs=nil은
여전히 이전 occupancy-only 경로이며 완전한 로그아웃/이관으로 인정하지 않는다.
마지막/추가 재실자와 음수/0/양수 피해값, 다른 방 활성 NPC, 마지막 퇴장 뒤 미이관
NPC 제외, 후기 미이관 오류의 원자성 테스트를 작성하고 실패→구현→race 통과했다.
engine/session/transport/cmd 로컬 race와 vet 통과; 실제 PG/프로세스 통합은 이번에
재실행하지 않았다. 전체 world race의 기존 63개 방 corpus 실패는 계속 남아 있다.
추종 해제·quit 명령의 나머지 효과·tick·활성/적대 목록 이관은 미완료다.
이번 변경은 로컬만 수행했고 Docker/CI/push/배포를 실행하지 않았다.

Luna max 재개 후 G1 방향 dispatch 경계: C command/process_cmd는 한 줄의 첫
토큰을 command table에서 exact/약어 순서로 선택하고 move handler로 전달한다.
Go에 ParseDirectionalToken을 추가해 첫 토큰의 숫자(2/4/6/8/3/9), 한글 자모,
정식 방향, 나가기 별칭을 권위 있는 한국어 출구 이름으로 정규화했다. 뒤의
인자는 목적지나 방 ID로 해석하지 않으며, 검증되지 않은 보기/소지품/임의 토큰은
거절한다. 대각선 별칭도 direct movement token으로 보존해 authoritative exit
selector가 처리한다. 기존 DirectionalTransferWithIDs에 숫자 8→북을 연결한
TDD 테스트와 non-movement rejection, 공백/추가 인자, 모든 cardinal/up/down/
leave alias를 race 통과시켰다. ExecuteDirectionalLine을 durable command receipt
경계로 추가하고 WorldConnector.Submit의 look 실패 fallback으로 연결했다. 권위
스냅샷에서 출구/목적지를 정하고 DirectionalStep을 실행한 뒤, committed scene을
응답하며 같은 command ID는 reducer를 다시 실행하지 않는다. session 단위 재생,
connector Submit→상태 저장, 실제 PostgreSQL 17 revision1/replay를 race 통과시켰다.
실제 브라우저 WebSocket+WorldConnector 배포 환경의 방향 입력은 아직 별도 E2E가
필요하다. 다만 httptest WebSocket+WorldConnector에서 로그인→`8`→도착지 응답→
소켓 종료/퇴장을 실제 프로토콜로 통과시켰고, 비동기 종료 cleanup race를 fixture
mutex로 제거했다. session/transport/cmd 전체 race 및 vet를 재실행해 통과했다.
실제 PG directional receipt는 앞선 격리 PostgreSQL 17 revision/replay 테스트로
통과했다. 출구별 입장 후속, 추종/함정/전투 tick, 나머지 명령 dispatch와 실제
브라우저/배포 환경 검증은 미완료다. 이번 변경은 로컬 격리 PG/httptest만 사용했고
Docker/CI/push/배포를 실행하지 않았다.

2026-09-08 추가 G1 후속: `PlanArrivalTrap`이 `src/room.c:check_traps`의
도착 후 회피 순서와 함정별 결정론적 효과를 별도 순수 계약으로 고정했다. 함정이
없는 방에서는 준비 플래그만 지우고 RNG를 소비하지 않으며, 준비 회피는
민첩성/지능별 1..20/1..25 판정 뒤 일반 1..100 판정을 수행한다. 독화살·낙석·MP
피해·구덩이 피해, 주문 타이머 제거/장비 손실 표식, 구덩이 trapexit 재배치와
치명상 결과를 테스트했다. `DirectionalTransferWithIDs`는 성공한 destination에만
도착 후보를 만들고, `DirectionalStep`이 추종자 처리 뒤에만 `ArrivalTrap`을 붙인다.
따라서 거절·경비·낙하 정지에는 trap RNG나 효과가 없다. `DirectionalStep`은 그
결과를 상태와 함께 적용하고 치명상이면 기존 `PlanPlayerDeath` 원자 경계를
재사용한다. 구덩이의 비치명 재배치는 `BeenHere`·방 소속까지 함께 갱신한다.
이 문단 작성 시점에는 경보 함정의 영구 NPC 이동이 아직 구현되지 않았고, 경보는
조용히 버리지 않고 후보 전체를 거절했다. 새 순수/통합 trap
테스트와 `go test -race` 집중 실행 및 `go vet ./internal/world`가 통과했다.
전체 world race의 기존 corpus 63개 예외 실패는 그대로 남아 있다.
격리된 ARM64 PostgreSQL 17 컨테이너에서 `TestPostgresDirectionalArrivalTrapPersistsAndReplays`
도 `-race`로 통과했으며, 첫 명령의 HP/독 표식 저장과 동일 command ID 재생의
무재실행을 확인했다. `TestPostgresDirectionalFollowerPersistsAndReplays`도 같은
컨테이너에서 통과해 leader/follower 관계·양쪽 방 소속·동일 command replay를
확인했다. 컨테이너는 전용 이름으로 종료·정리했다.

추종 후속: `PlayerState`에 canonical leader ID와 C `first_fol` head-insertion 순서를
저장하고 `FollowPlayer`/`UnfollowPlayer`/`DetachPlayerRelationships`에서 self·cycle·
다른 방·비상호 edge를 원자적으로 거절한다. `DirectionalStep`은 리더 commit 뒤
현재 destination occupancy로 각 follower를 재판정하고, nested follower를 depth-first로
이동한 다음 각 follower 함정, 마지막으로 리더 함정을 적용한다. 목적지 `RONEPL`은
리더가 먼저 들어간 뒤 follower만 거절되는 회귀를 포함한다. 퇴장과 cold recovery는
관계를 정리한다. 순수 world follower/trap 회귀 및 world/session/transport/cmd
race·vet가 통과했다. 아직 follower별 방송/추적 NPC(MFOLLO)와 ALARM 영구 NPC 이동,
실제 브라우저/배포 E2E는 미완료다.
`go test -race ./internal/world -skip '^TestRoomBodyCorpus$'` 전체 회귀도
통과했으며, corpus 테스트를 포함한 전체 world 실행은 기존 63개 원본 방 예외 때문에
의도적으로 실패한다.

2026-09-08 G1 NPC chase/alarm slice: `PlanNPCFollowerChase`/`ApplyNPCFollowerChase`가
원작 `command2.c`의 이동 후 source `NPCIDs` 순회, first enemy의 canonical player ID,
MFOLLO/MDMFOL·PINVIS/PDMINV·MDINVI 가시성 조건, `1..50` 및
`15-playerDex+npcDex` 판정을 고정했다. MPERMT NPC는 명시적인
`NPCPermanentOrigin(room,slot)`으로 `die_perm_crt`의 due timer를 이동 전에 갱신하고,
목적지 NPC 정렬·ActiveNPC prepend·MPERMT 해제를 한 후보로 적용한다. 영구 원본·적대
관계·활성 순서·랜덤 결과가 미확정이면 후보 전체를 폐기한다.

`DirectionalStep`은 이제 플레이어 재귀 follower 이동 뒤 MFOLLO chase를 실행하고, 그
후 arrival trap을 적용한다. `TRAP_ALARM`은 더 이상 효과를 버리거나 단순 거절하지 않고,
`ApplyArrivalAlarmWithCatalog`에서 trapexit의 due 영구 NPC를 catalog/allocator로 먼저
원자 생성한 뒤 경보실로 이동해 MAGGRE를 켜고 MPERMT를 끄며 origin timer를 갱신한다.
필요한 catalog/allocator가 없으면 fail-closed로 후보를 거절한다. actor-local 경보
출력도 receipt에 포함한다. `WorldConnector`에는 committed movement를 다른 연결로
전달하는 비동기 room-event hub와 slow-client drop 정책을 연결했지만, 원작의 모든
분기별 정확한 formatting/event coverage와 일반 room-entry/tick spawn orchestration은
아직 별도 경계다.

`NPCFollowerIDs`/`FollowingPlayerID`와 `FollowNPCToPlayer`를 추가해 canonical
first_fol의 몬스터 관계도 보존한다. `FollowerRefs`가 import된 경우에는 플레이어와
몬스터가 섞인 C 단일 first_fol 순서를 그대로 재귀 순회하고, MDMFOL 항목은 그 위치에서
이동·ActiveNPC prepend·MPERMT 해제를 적용한다. `PlanNPCMonsterFollowers`/
`ApplyNPCMonsterFollowers`는 순서가 아직 분리된 레거시 snapshot의 호환 경로다.
`die_perm_crt` timer는 command6 경로에서 갱신하지 않는다. 이 결과도
`DirectionalStep`의 같은 receipt에 포함되고, cold recovery/logout에서 관계를 끊는다.
follower별 네트워크 fan-out 경계는 연결했지만, command6을 포함한 원작별 room 메시지
formatting과 모든 분기 event coverage는 별도 출력 경계로 남아 있다.

추가 TDD는 stale body/ID·visibility·timer·active order·alarm trapexit player 유무와
원자 실패를 검증했다. `go test -race ./internal/world -skip '^TestRoomBodyCorpus$'`,
`go test -race ./internal/session ./internal/transport ./cmd/...`, `go vet`가 통과했다.
격리 `linux/aarch64` PostgreSQL 17 컨테이너에서 기존 방향/함정/follower와 신규
`TestPostgresDirectionalNPCChasePersistsAndReplays`,
`TestPostgresDirectionalAlarmPersistsAndReplays`,
`TestPostgresDirectionalAlarmSpawnsDueNPCPersistsAndReplays`,
`TestPostgresDirectionalMDMFOLPersistsAndReplays`와 mixed `FollowerRefs`를 포함한
동일 command replay·무재실행 RNG 검증을 통과시켰고 전용 컨테이너는 정리했다.
`TestMovementEventsPreserveCommittedActorFollowerAndNPCOrder`와
`TestWebSocketForwardsAsynchronousRoomEvent`도 통과했다.
원작의 모든 follower별 broadcast/command6 room 메시지 formatting과 MFOLLO monster
follower(`command6.c`) event coverage, 일반 room-entry/tick spawn orchestration, full `die_perm_crt`
quest/XP/summon/death-description side effects, 전투 tick, 전체 명령,
실제 브라우저/배포 E2E와 63개 room corpus 예외는 여전히 남아 있다.

2026-09-08 player phase slice: `State.PlayerUpdateOrder`가 room membership 기반의
deterministic 비-DM 온라인 플레이어 순서를 만들고, `WorldConnector.RunPlayerVitalPhase`
가 `UpdatePlayer`의 effect 만료·HP/MP vitals·inline death continuation·light tick을
하나의 `engine.Execute` receipt로 저장한다. 동일 command ID replay에서는 reducer와
RNG/allocator를 재호출하지 않는다. `TestWorldConnectorPlayerVitalPhaseAndReplays`와
실제 `linux/aarch64` PostgreSQL 17의
`TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`를 `-race`로 통과했다.
이 메서드는 full legacy `update.c` scheduler가 아니다. NPC/room spawn, combat round,
전체 broadcast/formatting 및 브라우저의 주기 호출은 별도 작업이다.

2026-09-08 combat slice: `ExecuteAttackLine`은 `공격`/`공`/`쳐`/`때려 <NPC 이름>`을
canonical room NPC identity로 해석하고 `PlanNPCMeleeAttack`에서 C THAC0/armor hit
gate, damage dice, critical roll, hidden/invisible 해제와 NPC enemy relation을 원자
후보로 만든다. receipt replay에서는 RNG/상태 reducer를 재실행하지 않는다. 로컬
world/session/transport 회귀와 ARM64 PG17 `TestPostgresAttackCommandPersistsAndReplays`
를 `-race`로 통과했다. lethal NPC의 기본 `die` 전이는 후속 기록에서
drop/XP/quest/permanent timer까지 연결했으며, allocator가 없거나 summon 런타임이
필요한 경우에만 fail-closed한다. PVP·multi-swing·weapon break/flee·정확한 전체
combat broadcast는 당시 기록 시점에 미구현이었다. 이후 같은 날짜의 weapon slice
기록에서 weapon break/drop과 내구도 감소를 추가했으며, PVP·multi-swing·flee와 전체
combat broadcast는 여전히 미구현이다.

`ExecuteStatusLine`은 `건강`/`점수`를 canonical HP/MP/방어력/경험치 목표/돈 출력으로
연결하고 no-state-change receipt replay를 보장한다. ANSI title/class formatting과
전체 명령 parser는 아직 미완료다.

`ExecuteFollowLine`은 `따라 <플레이어>`와 `내보내`를 canonical same-room player identity로
해석해 `FollowPlayer`/`UnfollowPlayer` reciprocal 관계를 durable receipt에 저장하고
replay한다. `내보내`는 인자 없이 자기 leader를 떠나는 C `lose` 경로와 이름을 지정해
자기 follower를 해제하는 경로를 포함한다. NPC follower 명령 및 legacy 약어/occurrence
parser는 남아 있다. 변경 후 `go test -race ./... -skip '^TestRoomBodyCorpus$'`,
`go vet ./...`, `git diff --check`가 통과했고, ARM64 PostgreSQL 17
`TestPostgresFollowAndLoseCommandPersistsAndReplays`도 저장·replay까지 통과했다.

`ExecuteItemsLine`은 `소지품`과 `장비`/`장`을 canonical item ID·ready-slot 순서로
렌더링하고 blind/invisible 규칙을 적용한다. `TestPostgresItemsCommandPersistsAndReplays`
가 ARM64 PostgreSQL 17에서 inventory/equipment receipt 저장·replay를 통과했다.
get/drop/wear/remove/hold/ready 같은 item mutation과 전체 parser는 아직 남아 있다.

`ExecuteSayLine`은 `말`/따옴표 별칭, 침묵, local echo, 발화 시 hidden 해제를 durable
receipt로 처리한다. committed 상태 뒤 같은 방의 다른 연결에 비동기 say event를 보내며
replay에서는 재방송하지 않는다. ARM64 PostgreSQL 17
`TestPostgresSayCommandPersistsAndReplays`와 transport room-event 회귀가 통과했다.
전체 broadcast formatting, 대화/DM 및 나머지 command parser는 아직 미완료다.

`ExecuteSocialLine`은 `누구`/`그룹`을 durable read-only receipt로 처리한다. 온라인 목록은
room membership 우선 순서를 사용하고 invisibility·detect·blind를 적용하며, group은
mixed `first_fol` player/NPC 순서를 보존한다. ARM64 PostgreSQL 17
`TestPostgresSocialCommandPersistsAndReplays`가 저장·replay를 통과했다. C descriptor
order/ANSI title 완전 동등성과 group mutation은 아직 남아 있다.

`ExecuteItemMutationLine`은 `주워`/`주`/`가져`/`꺼내`와 `버려`/`넣어`를 canonical
floor/player root 간 nested subtree 이동으로 처리한다. blind/invisible 및 equipped-root
경계를 적용하며 ARM64 PostgreSQL 17 `TestPostgresItemMutationCommandPersistsAndReplays`
가 저장·replay를 통과했다. 모두 줍기, guard/weight/capacity 및 wear/remove/hold/ready는
아직 미완료다.

2026-09-08 equipment mutation slice: `ExecuteEquipmentLine`이 원작 alias `입어`, `쥐어`,
`무장`, `벗어`를 canonical inventory root와 C MAXWEAR 20개 ready slot 사이의 durable
mutation으로 연결했다. 목/손가락의 첫 빈 슬롯, 파손·저주·슬롯 충돌과 기본 C
직업/성향/크기/무기 제한을 후보 단계에서 검증하며, OWEARS/OWHELD 플래그와 AC/THAC0를
원자적으로 재계산한다. 제거는 cursed item을 보존하고 나머지를 C식 이름/보정치 순서로
inventory에 삽입한다. local unit/race/transport와 ARM64 PostgreSQL 17
`TestPostgresEquipmentCommandPersistsAndReplays`의 저장·동일 command ID replay를
통과했다. 전체 C class/race/quest/event/charge 예외, `모두` 일괄 처리, 장비 사용
효과와 전체 parser는 아직 미완료다.

아이템 이동 후속: `주워 모두`/`버려 모두`를 canonical floor/player root batch receipt로
연결했다. `ItemCollection.Weight`/`CapacityCount`가 C `weight_obj`/`weight_ply`와
`count_inv(...,-1)` 경계를 보존하며, get은 blind/invisible·경비·ONOTAK/OSCENE·무게·
소지 수·gold/quest 미구현 경계에서 fail-closed한다. drop은 일반 플레이어의
quest/event root와 nested child를 보호하고 DM만 통과시킨다. 단일/batch unit/race와
ARM64 PostgreSQL 17 replay가 통과했다. container 내부 get/drop, gold/quest 보상,
제물 방·은행·상점 연동은 별도 후속이다.

`꺼내 <가방> <물건>`/`넣어 <물건> <가방>` direct child mutation도 canonical ID와 nested
소유권·container counter를 보존하는 receipt로 연결했다. ARM64 PostgreSQL 17
`TestPostgresItemMutationCommandPersistsAndReplays`가 root/batch/container get/drop
replay를 통과했다. gold/quest 보상, 제물 방·은행·상점 연동은 남아 있다.

2026-09-08 bank slice: `잔액`/`입금`/`출금`과 `보관물`/`받아`를 C `RBANK` 플래그가
있는 방에서만 실행하고, `냥` 접미사·`모두` 금액 해석, 3억냥 계정 상한,
player gold/은행 잔액 원자 변경, canonical item-root graph 이동을 `engine.Execute`
receipt에 연결했다. 로컬 unit/session/transport와 ARM64 PostgreSQL 17
`TestPostgresBankMoneyCommandPersistsAndReplays`/`TestPostgresBankItemCommandPersistsAndReplays`의
입금·아이템 보관→동일 command ID replay→출금을 통과했다. `go test -race ./... -skip
'^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, `git diff --check`도 통과했다. 기존
bank graph 전체 이관, 상점·거래와 전체 room corpus의 원본 63개 예외는 여전히 미완료다.

`끝` 명령도 explicit quit receipt로 연결했다. 응답을 먼저 전송한 뒤 WebSocket이
`Closed`를 관찰해 기존 `Depart`/cleanup 경계를 실행하므로 정상 종료와 소켓 단절이
같은 durable departure 경로를 공유한다. 전체 quit alias·자동저장·재접속 UX는 아직
별도 인수 범위다.

2026-09-08 read-only time slice: `시간`을 중앙 parser와 `WorldConnector`에 연결했다.
C `prt_time`의 게임 시각(오전/오후·12시간 변환)과 PST wall-clock을 request에 포함해
`ExecuteReadLine` durable receipt로 렌더링하며, reducer는 world snapshot bytes를 그대로
반환한다. 같은 command ID replay는 새 시계를 읽거나 reducer를 재실행하지 않는다.
`도움말`과 인자가 붙은 시간 입력은 아직 continuation/info 경계가 없어 receipt 없이
fail-closed한다. `WallClock` 주입을 사용한 local unit/transport 회귀, session/transport
race와 `go vet`가 통과했다. 전체 C 출력/ANSI 포맷과 도움말·정보 명령은 별도 후속이다.

2026-09-08 NPC lethal combat slice: `PlanNPCMeleeAttackWithOptions`가 치명타를
`PlanNPCDeath`와 같은 후보에 묶는다. canonical NPC를 room/active 순서에서 제거하고,
다른 NPC의 적대 참조와 monster follower edge를 정리하며, C의 damage 비례 XP/성향,
quest bit/proficiency 보상, MPERMT origin timer를 적용한다. legacy NPC inventory와
금화는 host allocator로 canonical room item graph에 이동하며 allocator가 없거나
`MSUMMO` 소환 side effect가 필요한 경우 fail-closed한다. `TestPostgresLethalAttackCommandPersistsNPCDeath`와
`TestPostgresLethalAttackCommandPersistsNPCDrops`가 ARM64 PostgreSQL 17에서 저장·동일
command ID replay와 canonical floor graph를 확인한다. PVP,
summon 생성·death description broadcast와 전체 NPC 전투 tick은 아직 구현·인수되지
않았다.

2026-09-08 NPC attack weapon/multi-swing slice: command5의 `shotscur < 1` 선행 경계를 canonical
ready/inventory graph로 옮기고, 치명타의 `OALCRT`/`ONSHAT`/`ONEWEV` 파괴 규칙, 일반
명중의 숙련도 기반 드롭 확률, 명중 후 `mrand(0,3)` 내구도 감소를 원래 RNG 순서로
처리한다. 파괴 시 root와 nested child를 함께 삭제하고, 드롭 시 C식 이름/보정치 순서로
inventory에 되돌리며, NPC 피해 비례 proficiency award도 같은 후보에 저장한다. 결과
응답은 부서짐/드롭을 terminal receipt에 포함하고, 동일 command ID 재생은 난수·장비
mutation을 재실행하지 않는다. `TestPlanNPCMeleeAttackMovesAlreadyBrokenWeapon`,
`TestPlanNPCMeleeAttackDropsWeaponOnNonCriticalHit`,
`TestPlanNPCMeleeAttackAwardsProficiencyAndConsumesWeaponShot`,
`TestPlanNPCMeleeAttackShattersCriticalWeaponAndRemovesSubtree`,
`TestExecuteAttackLineCommitsWeaponDropAndReplays`, 그리고 ARM64 PostgreSQL 17
`TestPostgresAttackCommandPersistsWeaponDrop`를 통과했다. PUPDMG의 추가 swing 수와
lethal/무기 소진 시 중단을 `TestPlanNPCMeleeAttackPowerDamageAddsDeterministicExtraSwing`,
`TestPlanNPCMeleeAttackPowerDamageStopsAfterLethalSwing`,
`TestPostgresAttackCommandPersistsPowerDamageSequence`로 unit/race/ARM64 PG replay
검증했다. PVP/도망, summon 생성·death description broadcast와 전체 NPC 전투 tick은
여전히 미완료다.

2026-09-08 G1 room admission/catalog slice: 엄격한 `DecodeLegacyRoom`은 malformed
text와 trailing bytes를 계속 거절하고, `AdmitLegacyRoom`만 명시적인
`LegacyRoomAdmissionPolicy`를 받아 알려진 compatibility issue를 승인한다. 허용
가능한 issue는 invalid EUC-KR, bounded fixed-text NUL 누락, trailing data, C
`load_rom`의 path-ID 우선 mismatch이며, 잘림·invalid count·과도한 depth/size는
정책과 무관하게 거절한다. admission 결과는 원본 Source/SHA-256/issue offset을
보존하므로 replacement preview가 원본을 덮지 않는다.

`LoadLegacyRoomCatalog`은 C의 `rooms/r%02d/r%05d`만 canonical path로 읽고,
현재 3,216개 트리에서 2,341개를 catalog에 넣으며 path가 맞지 않는 875개 역사적
artifact는 `Ignored()`에 기록한다. `r09/r09000`은 path가 runtime identity를
소유한다는 C 동작에 따라 ID를 9000으로 정규화하고 header mismatch를 evidence로
남긴다. `LegacyRoomCatalog.NewState`는 빈 플레이어의 검증 가능한 초기 State를
생성하지만 legacy monsters/objects를 canonical NPC/item graph로 자동 이관하지는
않는다. 다음 단계에서 이 변환, PG seed, room graph 참조 검증을 별도로 구현한다.

관련 TDD는 `TestAdmitLegacyRoomRequiresExplicitPolicyForNoncanonicalText`,
`TestAdmitLegacyRoomPreservesSourceEvidence`,
`TestAdmitLegacyRoomNeverRelaxesStructuralValidation`,
`TestLegacyRoomCatalogFollowsCPathAndRecordsHistoricalArtifacts`다. 이 slice는
엄격한 `TestRoomBodyCorpus`의 기존 63개 실패를 숨기거나 전체 게임 완료로 승격하지
않는다.

`engine.SeedWorldFromCatalog`와 `cmd/muhan -seed-world <id> -seed-rooms <rooms-dir>`는
검토된 catalog를 `mud_go.worlds`에 최초 snapshot으로 넣는 명시적 provisioning 경로다.
기존 world를 덮어쓰지 않으며 `-migrate` 후 별도로 실행한다. ARM64 PostgreSQL 17의
`TestPostgresSeedLegacyRoomCatalog`와 실제 CLI seed가 2,341개 room을 저장·재로드하는
것을 확인했다. 이 snapshot은 아직 legacy monster/object를 canonical NPC/item graph로
변환하지 않으므로, seed 성공만으로 플레이 가능 또는 전체 이관 완료로 해석하지 않는다.

2026-09-08 G1/G4 resource graph admission 후속: 이름이 비어 있는 zeroed creature
record는 `empty-monster-placeholder` evidence로 격리한다. 기본 compatibility policy에서만
런타임 방 목록에서 제외하며 원본 바이트·issue는 `LegacyInspection`에 남긴다. strict
policy는 이 의미 이상을 자동 추측하지 않고 거절한다. `ImportNPCs`는 방 ID와 원본
monster slice 순서로 stable NPC ID를 만들고, `ImportRoomItems`는 방 ID·root·nested
preorder로 stable item ID를 만든다. `ImportNPCItems`도 같은 NPC 순서로 nested inventory를
ID graph로 옮기며, 어느 단계든 allocator/검증 오류가 나면 부분 snapshot을 반환하지
않는다. NPC projection/death는 canonical graph를 legacy view로만 투영하고, 사망 drop은
기존 item ID를 재발급하지 않고 room graph로 이동한다.

`engine.SeedCanonicalWorldFromCatalog`와 CLI `-seed-world <id> -seed-canonical
-seed-rooms <rooms-dir>`는 NPC와 room/NPC item graph를 한 번에 명시적으로 seed한다.
namespace별 deterministic ID allocator와 `State.Validate`의 cross-owner 검사를 사용한다.
실제 ARM64 PostgreSQL 17에서 `TestPostgresSeedCanonicalRoomGraphs` 및 실제 CLI canonical seed가
2,341개 room, canonical NPC, floor/NPC item graph를 저장·재로드했다. 이는 전체 명령,
플레이어/bank 이관, tick·브라우저·배포 인수를 의미하지 않는다.

## 2026-09-08 player-vital scheduler 연결

`WorldConnector.RunPlayerVitalScheduler`를 추가해 기존에 수동 호출만 가능하던
`RunPlayerVitalPhase`를 실제 world-mode worker에 연결했다. 기본 cadence는 CLI의
`-player-tick 20s`이며, 현재는 이미 구현·검증된 player effect expiry, HP/MP vital,
inline death continuation, light tick만 실행한다. NPC active combat, room/random spawn,
persistent game clock, full broadcast는 이 scheduler가 처리하지 않는다.

각 cadence는 `player-vitals-<unix-slot>` command ID를 사용한다. 저장 결과가 불확실하면
slot의 timestamp/hour와 request를 메모리에 보존해 다음 cadence에서 동일 command를
재시도하고, 같은 slot의 중복 호출은 건너뛴다. WebSocket/수동 명령과 scheduler는
connector 단일 writer mutex를 공유한다. 종료 시 HTTP server를 먼저 drain하고
scheduler/cleanup worker를 취소한 다음 session cleanup을 마지막으로 재시도한다.

TDD 증거: `world_tick_test.go`의 deterministic slot skip, exact pending retry,
context cancellation 테스트와 `TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays`,
`TestWorldConnectorPlayerVitalTickReplaysAcrossConnectorRestart`의 ARM64 PostgreSQL 17
검증을 통과했다. 전체 회귀는 `go test ./... -skip '^TestRoomBodyCorpus$' -count=1`,
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`로 다시
검증한다. 이는 full legacy `update.c` scheduler 인수가 아니다.

## 2026-09-08 central command parser 연결

`session.ParseCommand`가 WebSocket world connector의 기존 순차 fallback을 대체했다.
현재 durable handler가 있는 look/direction/attack/status/follow/items/say/social/item
mutation/equipment/bank/quit alias를 하나의 명시적 registry에서 분류하고, 원본 line은
receipt identity용으로 보존한다. C 호환 7-token 제한과 quoted token 경계를 추가했으며,
미등록 명령은 구현된 것으로 추측하지 않고 안내 응답으로 남긴다. 약어/occurrence 전체
정책과 나머지 C command table은 아직 후속 범위다.

`command_parser_test.go`의 alias/quote/seven-token/오인 분류 회귀와 기존 transport
dispatch 테스트를 통과했다. 이는 command table 전체 이관이나 full parser parity를
의미하지 않는다.

## 2026-09-08 document help receipt 연결

`session.ExecuteHelpLine`을 추가해 C `help()`의 문서 경계를 Go receipt에 연결했다.
`도움말`/`?`는 `helpfile`, `도움말 주술`은 `spellfile`, `도움말 정책`은 `policy`,
현재 Go handler가 있는 명령 주제는 원본 `help.<cmdno>`를 읽는다. 문서 디렉터리는
프로세스가 주입한 `fs.FS`만 사용하며, 파일이 없거나 UTF-8이 아니면 추정 응답을 만들지
않고 실패한다. 미지원 주제는 C의 고정된 `그 명령어에 대한 도움말은 없습니다.`를
receipt로 저장한다. `WorldConnector`는 `-help-dir`(배포 기본 `/home/muhan/help`)로
이 경계를 구성한다.

TDD는 `help_command_test.go`, `world_connector_help_test.go`에서 문서 읽기·unknown
topic·누락 문서 fail-closed·동일 command ID replay·world state purity를 고정했다.
실제 ARM64 PostgreSQL 17 + Go `-race` + Chromium E2E도 `도움말 정보`를 포함해
**1 passed (11.0s)**였다. 이는 전체 C help alias/약어, continuation prompt, info title,
전체 command table 인수를 의미하지 않는다.

## 2026-09-08 `환영`·`외쳐`·감정표현 receipt 연결

`환영`은 프로세스가 주입한 `fs.FS`의 고정 `welcome` 문서를 읽는 read-only receipt로
연결했다. 문서가 없거나 UTF-8이 아니면 추정 응답을 만들지 않고 실패하며, 동일 command
ID replay에서는 파일을 다시 읽거나 commit하지 않는다. 실제 browser E2E는 원본 welcome
문서의 `레벨 5가 넘으면 많은 제약이 따릅니다.` 문장을 확인한다.

`외쳐`는 `command6.c:yell`의 빈 입력·침묵·은신 해제 순서를 원자 reducer로 옮겼다.
commit 뒤 같은 방에는 발화자 이름이 포함된 메시지를, 연결된 출구 방에는 익명 메시지를
출구 순서대로 fan-out하며, 본인과 replay에는 재방송하지 않는다. 알 수 없는 출구는
부분 상태를 저장하지 않고 fail-closed한다. `TestPostgresYellCommandPersistsAndReplays`
가 ARM64 PostgreSQL 17에서 저장·동일 command ID replay를 확인한다.

감정표현은 `src/action.c`의 일반 플레이어 alias 중 현재 출력 계약이 확보된 bounded
집합(`감정표현`, `노려봐`, `끄덕`/`응`, `감`/`감사`, `미소`, `청혼`, `떨어`, `해`,
`하품`, `웃어`, `미안`, `악수`, `하이파이브`, `박수`, `흡연`/`담배`, `절`, `찔러`,
`춤`, `노래`, `울어`, `달래`, `당황`, `생각`, `부끄러`, `놀려`, `설레`, `바이`/`잘가`,
`안녕`, `뽀뽀`, `윙크`, `구걸`, `구박`, `안아`/`껴안아`)만 exact alias로 인정한다.
PHIDDN 해제와 PSILNC 출력 순서를 보존하고, 대상은 exact same-room online player만
허용한다. 대상에게는 target-specific 출력, 같은 방의 다른 연결에는 room 출력,
본인에게는 비동기 event를 보내지 않는다. NPC/prefix/occurrence/미검증 target은
부분 receipt 없이 거부한다. `TestPostgresEmoteCommandPersistsAndReplays`와
`TestWorldConnectorSubmitDispatchesEmoteWithTargetAndRoomProjection`가 저장·replay와
fan-out 경계를 확인한다.

검증 명령:

```sh
go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1
go vet ./...
bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable
pnpm test:browser
```

실행 결과는 각각 Go 전체 race 통과, vet 통과, 실제 ARM64 PostgreSQL 17 + Go
`-race` + Chromium **1 passed (10.4s)**, 표준 xterm/feature-off **각 1 passed**다.
`TestRoomBodyCorpus`의 기존 63개 strict legacy 예외, 전체 action alias/명령 table,
모바일 IME/WSS/Ingress 및 testnet 배포 인수는 여전히 남아 있다. `src/frp.new`는
사용자 변경으로 계속 보존하며 이 slice에서 수정하지 않았다.

## 2026-09-08 `표현`·`보아 <대상>` 수직 슬라이스

원본 `command11.c:emote`의 `표현` 명령은 free-form UTF-8 payload를 최대 255바이트로
제한하고 제어문자·잘못된 UTF-8·oversize를 receipt 전에 거부한다. 빈 입력은
`무슨말을 표현하시려구요?`, 침묵 상태는 `당신은 지금당장 그것을 할 수 없습니다.`를
반환하며 snapshot을 바꾸지 않는다. 비침묵 성공만 PHIDDN을 해제하고, `PLECHO`가
있어도 서버가 임의 formatting을 실행하지 않는다. commit 뒤 같은 방에
`:이름님이 <text>.`를 fan-out하고 본인/replay에는 중복 event를 보내지 않는다.
임의 payload는 receipt에 저장하지 않고 actor 응답만 저장한다.

원본 `action.c:보아`의 bounded explicit target도 연결했다. exact same-room canonical
identity만 허용하며 NPC를 player보다 먼저 탐색하고, invisible/DM-invisible 및 detect
경계를 source에 맞춰 확인한다. player target은 대상자에게 `당신을 봅니다` projection,
같은 방의 다른 연결에는 room projection을 보내고, NPC target은 검증된 room projection만
보낸다. bare `보아`, prefix/occurrence, object inspection, 전체 `조사` parity는 아직
별도 범위다. PHIDDN 해제는 source처럼 PSILNC 검사보다 먼저 적용한다.

TDD/검증: `express_command_test.go`, `look_at_target_command_test.go`, world reducer
tests, transport fan-out tests 및 실제 ARM64 PostgreSQL의
`TestPostgres(Emote|Express|LookAtTarget|Yell)CommandPersistsAndReplays`가 통과했다.
전체 `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...`도 통과했으며 실제 Go+PG+Chromium
E2E는 **1 passed (10.1s)**다. strict room corpus의 기존 63개 예외와 전체 명령/배포
인수는 그대로 미완료다.

## 2026-09-08 `검색`·`찾아` same-room hidden target 수직 슬라이스

`src/command5.c:search`의 확정된 플레이어/NPC 경계만 Go에 연결했다. 순수
`PlanSearch`/`ApplySearch`는 C의 piety·level·class·blind 확률, ranger/caretaker
override 순서, LT_SERCH(7) cooldown, actor PHIDDN 해제와 player-then-NPC room
identity 순서를 보존한다. DM-invisible·invisible 대상은 C의 조건 순서대로 처리하며
객체/출구 검색과 prefix/occurrence는 durable 계약이 없어 이번 slice에서 제외했다.
canonical identity, random source, 이름/수치 범위를 확인할 수 없으면 부분 상태 없이
fail-closed한다.

`검색`/`찾아`는 중앙 parser와 WebSocket connector에 연결했다. receipt response는
actor 문자열과 탐지된 canonical target ID를 함께 보존하므로 동일 command ID replay는
RNG/reducer/event fan-out을 다시 실행하지 않는다. 최초 commit 뒤 같은 방의 다른 연결에
검색 행동 및 발견 힌트를 순서대로 fan-out하고 actor는 자신의 receipt만 받는다.

검증은 `search_command_test.go`, `search_command_pg_test.go`(환경 변수
`MUHAN_SEARCH_TEST_DATABASE_URL`가 있을 때 ARM64 PostgreSQL 17 저장/replay),
`world_connector_search_test.go`로 고정했다. 로컬에서
`go test ./... -skip '^TestRoomBodyCorpus$' -count=1`,
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`가 통과했다.
PG 통합 테스트는 이 실행에 격리 DB 환경 변수가 없어 skip했으며, 기존 strict room corpus
63개 예외와 전체 C search(객체/출구 포함) parity는 여전히 미완료다.

## 2026-09-08 `추적`·`숨겨`/`숨어` stealth command slices

`추적`은 `src/command4.c:track`의 bare ranger/관리자 branch를 중앙 parser와
`WorldConnector`에 연결했다. `PlanTrack`/`ApplyTrack`이 `LT_TRACK` cooldown, PHIDDN
해제, DEX·level chance, blind/empty-trace/found response와 committed room event를
원자적으로 처리한다. 방향·대상 인자는 받지 않으며 객체·출구 graph가 준비될 때까지
fail-closed한다.

`숨겨`/`숨어`는 `src/command5.c:hide`의 bare player branch를 연결했다. class별 chance와
5/15초 interval, blind 20 cap, 단일 `1..100` RNG, `LT_HIDES`, 성공/실패 PHIDDN 및
room broadcast를 durable result로 저장한다. 객체 hide는 C의 ONOTAK/object inventory
권위가 없어 `ErrHideObjectUnsupported`로 거절하고, 추가 토큰은 receipt를 만들지 않는다.

session receipt는 응답·broadcast outcome을 함께 저장하므로 command ID replay가 RNG,
state reducer, async event를 재실행하지 않는다. `hide_command_test.go`/`track_command_test.go`,
transport fan-out 회귀와 환경 변수 `MUHAN_HIDE_TEST_DATABASE_URL`/
`MUHAN_TRACK_TEST_DATABASE_URL`를 사용하는 ARM64 PostgreSQL replay가 통과했다.
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...`, 실제 Go+PG+Chromium E2E
**1 passed (10.9s)**도 통과했다. 기존 strict room corpus 63개 예외와 객체/출구
stealth, 전체 C command parity·tick·경제·Ingress 인수는 미완료다.

## 2026-09-08 `엿봐 <대상>` peek command slice

`command4.c:peek`의 bounded player/NPC branch를 추가했다. `엿봐 <정확한 이름>`만
받고 NPC를 먼저 찾으며, 도둑/무적 이상 권한·blind·invisible/DM-invisible와 보호
대상 flag를 committed snapshot에서 판정한다. `LT_PEEKS` 5초 timer, 레벨 차이 기반
성공률, 성공 후 두 번째 발각 RNG의 순서를 유지하고, 성공 시 canonical item graph의
보이는 inventory root만 actor receipt에 렌더링한다. 보호 대상도 C처럼 timer를 기록하고
응답만 반환한다.

발각 시 receipt가 대상 ID/kind와 대상 개인 메시지·room 메시지를 함께 보유하며,
`WorldConnector`는 target을 제외한 room fan-out과 target private event를 최초 commit에만
보낸다. 동일 command ID replay는 item read나 난수를 다시 실행하지 않는다.
`peek_command_test.go`, transport 회귀, `MUHAN_PEEK_TEST_DATABASE_URL` ARM64
PostgreSQL replay와 browser E2E의 fighter 권한 거절을 통과했다. prefix/occurrence,
object branch, 전체 C `list_obj`/ANSI parity는 아직 별도 범위다.

## 2026-09-08 `설정`·`해제` settings command slice

`설정`/`해제`를 `command5.c:set`/`clear`의 source-backed player flag 경계로 연결했다.
설정 flag-list, 일반 toggle, `도망수치`, `패거리귀환`, `hexline`, `eavesdropper`,
`~robot~`, `수동공격`과 해제 응답을 canonical `LegacyMonster.Flags`/`WimpyValue`에
원자적으로 적용한다. flag 번호는 클라이언트가 제출할 수 없고, stale flag/value·잘못된
숫자·미확인 actor는 receipt 전에 거절한다. `설정` unknown key는 C처럼 flag-list를
반환하고, `해제` unknown key는 C의 오류 문구를 반환한다.

검증:

- world/session/transport settings TDD 및 parser 회귀 통과
- `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, Linux ARM64 cross-build 통과
- 격리 `postgres:17-alpine`에서 `TestPostgresSettingsCommandPersistsAndReplays` 통과 후 컨테이너 제거
- 실제 Go + PostgreSQL + Chromium 가입→`설정 색`→재로그인 E2E **1 passed (11.2s)**

전체 C settings alias/ANSI formatting, 관리자 전용 옵션, 나머지 명령·tick·경제·배포
인수는 아직 완료되지 않았다.

## 2026-09-08 `열어`·`닫아` door command slice

`command6.c:openexit`/`closeexit`의 bounded same-room 출구 전이를 연결했다. 방의
권위 snapshot에서 첫 prefix match를 선택하고 `XLOCKD`/`XCLOSD`/`XCLOSS` 경계를
확인한 뒤 exit flags, open timestamp, actor `PHIDDN`을 함께 저장한다. 실패·stale
room/exit는 상태를 바꾸지 않고, 성공한 command receipt만 다른 방 연결에 event를
fan-out한다.

검증:

- world/session/transport door TDD 및 parser 회귀 통과
- `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, Linux ARM64 cross-build 통과
- 격리 `postgres:17-alpine`에서 `TestPostgresDoorCommandPersistsAndReplays` 통과 후 컨테이너 제거
- 실제 Go + PostgreSQL + Chromium에서 `열어 __missing_door__` fail-safe 경로 **1 passed (10.8s)**

`풀어`/`잠궈`/`따`의 열쇠·내구도·picklock 확률과 전체 occurrence/ANSI formatting은
별도 미완료 slice다.

## 2026-09-08 `풀어`·`잠궈`·`따` door-key slice

원본 `command6.c:unlock`/`lock`/`picklock`의 bounded same-room 경계를 Go에 연결했다.
`풀어`와 `잠궈`는 열쇠 object type·`ndice`/exit key 일치·내구도·잠금 가능/닫힘
순서를 확인하고, unlock 성공 때만 열쇠 사용 횟수와 `ltime`을 갱신한다. `따`는
도둑/무적 이상 권한, blind, `XLOCKD`, `LT_PICKL=6` 10초 cooldown, source chance와
`XUNPCK`를 적용하며, eligible 시 단일 `1..100` RNG 결과를 receipt-bound outcome으로
저장한다. cooldown에서도 C처럼 actor `PHIDDN`을 먼저 해제하고, pick 시도와 성공
room event는 원래 순서를 보존한다.

canonical ID item graph와 아직 legacy inventory인 snapshot을 모두 지원하되, 이 slice의
key lookup은 root inventory의 exact case-insensitive 이름으로 제한한다. nested/equipped
occurrence와 전체 `find_obj` prefix 정책은 item identity 계약이 확정될 때까지
추측하지 않고 별도 범위로 남겼다. stale exit·timer·inventory identity, 잘못된 RNG와
replay 재실행은 fail-closed한다.

검증:

- `door_keys_test.go`, session receipt/replay, transport fan-out 및 parser TDD 통과
- 격리 `linux/arm64` `postgres:17-alpine`에서 `TestPostgresDoorKeyCommandPersistsAndReplays` 통과
- `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, Linux ARM64 cross-build 통과
- 실제 Go + PostgreSQL + Chromium 가입→월드 입장→`따 __missing_door__` 권한 경계 **1 passed (11.3s)**

전체 C key lookup/occurrence·ANSI formatting, 나머지 command table·NPC/tick·경제·전체
배포 인수는 계속 미완료다.

## 2026-09-08 canonical search/inspection follow-up

`검색`/`찾아`는 원본 `command5.c:search`의 출구→방 객체→플레이어→NPC 순서를
canonical identity로 확장했다. secret exit는 방 ID와 ordered exit index를, 방 객체는
`RoomState.Items`의 root item ID를 receipt에 저장하며, 각 대상의 visibility와 단일
RNG 소비 순서를 Apply/room event에서 재검증한다. legacy linked-list room object,
nested object, prefix/occurrence는 identity가 확정되지 않아 fail-closed한다.

`보아 <대상>`도 기존 player/NPC branch 뒤에 canonical floor root와 exact visible exit만
허용한다. proposal/apply에서 actor 은신 해제와 target identity를 원자적으로 재검증하고,
object/exit room event는 최초 commit에서만 생성한다. 전체 C ANSI object description,
bare `보아`, prefix/occurrence와 legacy fallback은 여전히 별도 범위다.

검증: search/look world·session·transport TDD와 receipt/replay 회귀, 전체
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, Linux ARM64
cross-build가 통과했다. PostgreSQL 환경 변수가 없는 로컬에서는 새 PG 컨테이너를 만들지
않아 canonical object/exit PG replay는 아직 실행 증거가 아니다. 기존 strict room corpus
63개 예외와 전체 command/tick/경제/배포 인수는 계속 남아 있다.

## 2026-09-08 병렬 데이터·웹·부하 검증 후속

데이터 감사 fixture(`6631e5c`)는 3,216개 원본 방의 strict 거부 63개를 SHA-256,
소비 위치, issue kind별로 고정하며 runtime 변환 allow-list를 만들지 않는다. 웹
중앙 xterm 보강(`9b49c55`)은 reconnect/resize/submit 포커스, 한글 IME, 모바일
visualViewport, secret echo와 pending input 폐기를 TDD로 확인한다. 로컬 load harness
(`500aee6`)는 운영 REST endpoint가 아니라 httptest 어댑터로 REST-like와 persistent
connector 비용을 비교하며 staged target 100/250/500/1000과 MaxSessions=32,
cleanup/namespace/loopback 충돌을 검증한다.

통합 검증은 Go exception audit, 전체 Go race(기존 corpus test 제외), vet, ARM64
build, web typecheck/42 tests/build, classic-terminal Playwright 2 tests, load harness
race와 100/1000 smoke에서 통과했다. persistent 결과의 32-session 경계와 REST-like
수치는 메모리 fixture의 방향성 자료일 뿐 PostgreSQL/gateway 운영 동접 보증이 아니다.

bounded reconnect 후속(`6e86daa`)에서 유효 view 수신마다 retry budget을 초기화하던
경로를 제거했다. 반복 transient close에서도 최대 재연결 횟수가 유지되며, classic
terminal Playwright 3 tests와 web typecheck/build가 통과했다.

## 2026-09-08 직접 Luna max 후속 slice

root가 Orca 없이 직접 병렬 배치한 세 lane을 통합했다.

- `153efb7`: 실제 help 문서가 존재하는 `help.21`, `22`, `24–29`, `32–35`, `61`, `100`
  alias를 source-backed `도움말` projection에 추가했다. `help.31`처럼 없는 문서는
  연결하지 않으며 missing/invalid UTF-8과 receipt replay를 검증한다.
- `f09b0a4`: `보아 <prefix> <positive occurrence>`를 canonical NPC-first/player
  order와 display-name/three-key prefix로 제한적으로 지원한다. object/exit occurrence,
  bare `보아`, 전체 ANSI inspection은 아직 미지원이며 ambiguous/unresolved identity는
  receipt 전에 거부한다. WebSocket recipient/room projection과 replay suppression도
  회귀 테스트했다.
- `cbf6b8b`: player-vital scheduler가 NPC/room refresh를 암묵적으로 실행하지 않는
  경계를 고정했다. NPC spawn origin·active-order 권위가 준비되기 전에는 scheduler를
  확장하지 않는다.

이번 통합에서 다음 검증이 통과했다.

```text
go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...
```

추가로 격리 `postgres:17-alpine`에서 `TestPostgresLookAtTargetOccurrencePersistsAndReplays`
를 실행해 prefix/occurrence receipt 저장과 동일 command ID replay를 확인했다. 이 결과는
전체 PostgreSQL+Chromium E2E나 운영 동시성 인수를 대체하지 않는다.

strict room corpus 기존 63개 예외, 전체 C 명령/tick/경제, 실제 PostgreSQL/Chromium 재실행,
WSS/Ingress와 testnet 배포 인수는 계속 남아 있다.

## 2026-09-09 기존 캐릭터 이관·룸 자원 스케줄러 후속

`LinkExistingWorldCharacter`를 추가해 운영자가 정확한 world/player ID와 기대 revision을
제시한 경우에만 이미 canonical `PlayerState`인 캐릭터를 게임 이름/비밀번호 계정에
연결한다. 이름만으로 claim하지 않으며, bcrypt hash 형식·중복 이름·중복 player link·
명령 ID 재사용 충돌을 거절한다. 계정/character row, world revision, immutable receipt는
한 PostgreSQL transaction으로 저장하고, 인증 결과는 linked world player ID를 반환한다.
신규 `CreateInWorld`도 동일한 linked 메타데이터를 기록한다.

`WorldConnector.RunRoomResourceTick`과 `-room-resource-tick` worker는 canonical floor
object respawn과 자동 door refresh만 durable receipt로 실행한다. due permanent NPC가
있는 방은 익명 생성하지 않고 receipt의 `npc_pending_rooms`에 남기며, 아직 canonical
item graph가 아닌 방은 `unmigrated_rooms`로 건너뛴다. NPC identity/active-order phase는
아래 durable scheduler 경계로 연결했으며, NPC 전투 AI는 별도 포팅 경계다.

검증: 실제 격리 PostgreSQL 17에서 기존 캐릭터 link/replay·중복/이름 충돌·신규 world
creation을 통과했고, Go + PostgreSQL + Chromium 브라우저에서 terminal signup/relogin과
실제 linked character 로그인·동시 중복 세션 거부를 **2 passed**로 확인했다. 전체
`go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`, Linux ARM64
cross-build, web typecheck/test(42/42)도 통과했다. strict room corpus 63개 예외, 전체
NPC/tick/command parity, IME/mobile 실기기, backup/restore와 testnet 배포 인수는 아직
미완료다.

## 2026-09-09 레거시 원장·NPC identity·`도망` 후속

레거시 방 로더에 source-backed admission manifest를 추가했다. 경로·원본 SHA-256·
inspection issue·소비 길이를 정렬된 증거로 고정하고, 검토된 corpus의 3,216개 파일
(canonical 2,341, 비정규 artifact 875, body exception 63)와 digest가 달라지면
`LoadReviewedLegacyRoomCatalog`가 fail-closed한다. 비정규 경로는 runtime room으로
승격하지 않으며, strict `TestRoomBodyCorpus`의 63개 예외를 녹색으로 위장하지 않는다.

영구 NPC는 `NPCPermanentOrigin(room, slot)` identity를 기준으로 due respawn을
순수 plan/apply한다. 이름으로 점유 여부를 추측하지 않고, active order·allocator·RNG·
stale/tamper를 검증하며 부분 생성은 저장하지 않는다. transport scheduler는 아래
`RunNPCResourceTick` 경계에서 같은 identity 계약을 durable receipt로 실행한다.

원작 `도망`의 작은 수직 슬라이스도 Go session/transport receipt에 연결했다. 전투
cooldown, 출구 필터 순서, guard/chance, 추적·은신 해제, destination 제한, paladin
숙련도 손실과 성공·저지 room event를 검증하며, legacy 관계가 해석되지 않는 trap/NPC
부수효과는 fail-closed한다.

검증: `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
Linux ARM64 cross-build, web typecheck/test(42/42), 실제 PostgreSQL 17 + Go process
Chromium 가입→월드 입장→재로그인·기존 캐릭터 중복 세션 **2 passed**, infra source
pin test **2 passed**. 일반 `TestRoomBodyCorpus`는 기존 unsupported body 63건으로
계속 실패하며, 전체 기능·NPC scheduler·IME/mobile 실기기·backup/restore·WSS/Ingress와
testnet 배포 인수는 아직 남아 있다.

## 2026-09-09 NPC identity scheduler durable 연결

`RunNPCResourceTick`/`RunNPCResourceScheduler`와 `-npc-resource-tick` worker를
추가해 canonical room의 영구 NPC respawn을 engine receipt에 연결했다. cadence slot과
`npc-resources-<slot>` command ID를 고정하고, 저장 결과가 불확실하거나 실패하면 같은
slot·timestamp·request를 재사용한다. canonical NPC/item graph가 아닌 방은
`unmigrated_rooms`로 증거만 남기며 익명 NPC를 만들지 않는다. identity order와
allocator/RNG 호출 순서, cancellation·invalid interval을 race 테스트로 검증했다.

이 단계는 NPC 전투 AI·room broadcast·전체 update scheduler를 완료했다는 뜻이 아니며,
실제 격리 PostgreSQL 17에서 `TestWorldConnectorNPCResourceTickPostgresPersistsAndReplays`
를 실행해 spawn receipt·revision·재시작 replay를 확인했다. 전체 NPC 전투 AI·room
broadcast·update scheduler parity는 다음 인수 조건으로 남아 있다.

`PlanNPCCombatRound`/`ApplyNPCCombatRound`는 canonical NPC가 exact player enemy
identity를 대상으로 수행하는 한 번의 source-backed melee round를 순수 전이로 고정한다.
`update_active`의 armor/THAC0 hit gate, `mdice` 피해, class별 `mod_profic` critical과
stale/tamper·RNG·room membership 검증을 포함한다. player death continuation은 아직
별도 reducer 조합 전이라 fail-closed하며, 이를 전체 NPC AI나 전투 tick 완료로 해석하지
않는다.

## 2026-09-09 NPC 전투 tick·플레이어 사망·상점 구매 후속

`RunNPCCombatTick`/`RunNPCCombatPhase`를 player-vital/resource scheduler와 분리된
durable receipt phase로 추가했다. C `update_active`의 canonical active-NPC 순서, room
membership, 첫 enemy/player identity를 결정적으로 선택하고 `npc-combat-<slot>` command
ID·slot timestamp·pending retry를 고정한다. 여러 non-lethal `PlanNPCCombatRound`를
하나의 후보에 적용하며 lethal PLAYER는 아직 사망 continuation과 합쳐지지 않아 receipt
summary에 fail-closed로 남긴다. 새 connector는 동일 receipt를 replay해 RNG와 공격을
재실행하지 않는다. 실제 ARM64 PostgreSQL 17 test에서 저장·재생·request conflict·
rollback·RNG 미재실행을 확인했다.

`PlanNPCPlayerDeath`는 C `creature.c:die`의 NPC attacker PLAYER branch를 별도 순수
후보로 만들었다. victim progression/장비·HP/MP/timer, NPC enemy 제거, source floor drop,
room 1008 respawn, war 결과를 원자 후보에 묶고 NPC `MSUMMO`도 이 branch에서는 허용한다.
death description/broadcast/savegame/summon side effect와 combat tick 조합은 아직
fail-closed 경계다.

`QuoteShopPurchase`/`BuyShopItem` 및 `RunShopPurchase`는 `RSHOPP` + 다음 방 `RNOTEL`
저장고의 exact stock ID/value를 검증하고 nested item subtree를 새 canonical ID로
복사한다. gold/weight/capacity/duplicate ID/temporary flag와 성공 구매 시 원작의
`PHIDDN` 해제를 원자적으로 적용하며, 재고 원본은 변경하지 않는다. 동일 command ID
replay는 allocator와 reducer를 재호출하지 않는다. parser/list/sell/trade/merchant NPC와
실제 shop PostgreSQL replay는 남아 있다.

검증 커밋: `2449963`, `84692d7`, `574d67c`, `9061745`, `387db25`, `958f2f7`,
`e0aa717`. 전체 race/vet/ARM64 build 통과. strict room corpus 63개 예외, lethal NPC
tick 통합, 전체 C command/economy, IME/mobile 실기기, backup/restore, WSS/Ingress와
testnet 배포 인수는 여전히 미완료다.

## 2026-09-09 병렬 리뷰 후속: C 패리티·PG receipt·웹 터미널

리뷰 대기 중 독립 파일 경계를 나눠 세 lane을 병렬 처리했다. `fe8408e`는 C
`update_active`의 NPC→PLAYER 직접 공격에 맞춰 hit `mrand(1,20)` 뒤 `mdice - armor/5`
clamp만 소비하도록 고쳤고, player→NPC 전용 critical RNG는 보존했다. NPC 직접 공격의
PHIDDN/PINVIS도 명중·빗나감에서 보존한다. `1e00ed8`의 lethal continuation은 이제
`PlanNPCPlayerDeath`를 같은 durable candidate에 원자적으로 연결하고, C의 사망 후
`first_active` 재시작 경계로 tick을 중단한다.

`f759f49`는 실제 ARM64 PostgreSQL 17에서 상점 구매 receipt 생성·재생·request conflict와
receipt INSERT 실패 rollback을 검증한다. `5992018`은 중앙 xterm의 기본 포커스 정책을
IME composition, 선택/붙여넣기, 모바일 키보드 resize/복귀 경계와 함께 보강했다.

검증: `go test -race ./... -skip '^TestRoomBodyCorpus$' -count=1`, `go vet ./...`,
Linux ARM64 cross-build, web typecheck 및 **44/44** 테스트, 실제 PostgreSQL 17 전투·상점
receipt race test, 실제 Go+PostgreSQL+Chromium 가입→월드 입장→재로그인·중복 세션
**2 passed**. strict room corpus의 기존 unsupported body 63건, 전체 C 명령/경제 parity,
IME/mobile 실기기, backup/restore, WSS/Ingress와 testnet 배포 인수는 여전히 남아 있다.

## 2026-09-09 G3/G4 병렬 후속: 상점·NPC scheduler·백업

`41e0a47`은 원작 `품목`/`팔아`의 bounded marketplace slice를 추가했다. `RSHOPP`/
`RPAWNS`와 다음 `RNOTEL` 저장고를 canonical room/item graph로 확인하고, list는
read-only deterministic receipt, sell은 직접 소지 root·정확한 occurrence·`value/2`
상한·품질/visibility/중첩/flag/weight/gold 검증과 원자 소유권 이동으로 처리한다.
prefix/key selection, 이중지급 RNG branch, merchant/repair는 source fixture가 없어
fail-closed한다. 실제 PG17 receipt replay/conflict를 통과했다.

`451bec4`는 기존 `RunNPCCombatTick`을 바꾸지 않고 `NPCCombatScheduler` lifecycle을
추가했다. `RunOnce`/`Start`/`Run`/`Wait`/`Stop`/`Shutdown`을 제공하며 고정 slot·now·
command ID, pending retry, receipt replay, 중복 worker와 cancellation을 보장한다.
`07f63cb`에서 `-npc-combat-tick`과 기존 worker WaitGroup/shutdown에 실제 연결했지만,
전체 update cadence와 room broadcast는 아직 별도 gate다.

`9d8d95c`/`f73b5b1`은 checksum·format version·world revision을 포함한 deterministic
backup envelope와 fail-closed restore를 추가했다. 기본 복구는 expected revision 및
receipt 없는 대상만 허용하고, `Force`는 기존 receipt를 제거하고 writer epoch을 올려
구 writer를 fencing한다. `b9b1abf`는 0600·64 MiB·atomic file export/restore CLI를
추가했고, `c2a0fa5`는 JSONB compacting에 따른 checksum mismatch를 canonical JSON으로
고쳤다. 실제 PG17 API/CLI 복구 테스트를 통과했다.

통합 검증은 race/vet/ARM64 build, web 44/44, PG17 combat/shop/backup receipt를
통과했다. 전체 command/economy parity, NPC full cadence/broadcast, strict room corpus
63개, IME/mobile 실기기, 백업 파일 운영 보관·복원 연습, WSS/Ingress/testnet 배포는
여전히 미완료다.

## 2026-09-09 터미널 상점 구매 연결

원작 상점 구매 별칭 `사`/`구입`을 중앙 parser와 live WebSocket connector에 연결했다.
터미널은 stock ID를 제출하지 않고 정확한 canonical 상품명과 양수 occurrence만 제출하며,
서버가 권위 저장고 snapshot에서 stock ID를 해석한다. prefix/key/merchant 구매는
추측하지 않고 fail-closed한다. 기존 `BuyShopItem`의 nested graph deep-copy, gold/weight/
capacity/duplicate/temporary flag/`PHIDDN` 규칙과 durable receipt/replay를 그대로 재사용하고,
live connection은 `Ownership.RunGame` admission 경계를 통과한다.

검증: session/world/transport TDD·race·vet, live connector output/admission/occurrence,
실제 PostgreSQL 17의 name purchase receipt/replay/request conflict 및 nested allocator를
통과했다. 전체 merchant/trade/value/수리와 나머지 C 경제 parity는 미완료다.

## 2026-09-09 `교환` NPC 거래 수직 슬라이스

원작 `command10.c:trade`의 접미 명령 형식인 `물건 괴물이름 교환`을 중앙 parser와
실제 WebSocket connector에 연결했다. `NPCState.TradeOffers`는 C `carry[0..4]`/
`carry[5..9]` 쌍을 명시적으로 이관한 canonical 템플릿이며, 미이관 `Body.Carry`를
실행 시 추측하지 않는다. 같은 방 `MTRADE` NPC와 플레이어의 직접 inventory root를
정확한 이름·양수 occurrence로 선택하고, `ONAMED`·손상 물건·key[0] 불일치·중복
NPC를 거부한다. 교환한 root subtree는 제거하고 보상 subtree는 command ID 기반의
결정적 canonical ID로 새로 만들어 하나의 durable receipt에 저장한다. 퀘스트 보상,
숙련도, 보상 없음 경로와 room broadcast를 포함하며 receipt replay에서는 reducer·ID
할당·방송을 다시 실행하지 않는다.

검증: world/session/transport TDD·race·vet, C carry-pair import, live connector dispatch,
실제 ARM64 PostgreSQL 17의 저장·동일 command replay·request conflict, Linux ARM64
cross-build를 통과했다. 원본 prefix/key `find_obj`와 merchant/repair/전체 경제 parity,
full NPC tick/broadcast는 별도 인수 조건으로 남아 있다.

## 2026-09-09 가치·수리·개인 메시지 명령 연결

직접 관리한 Luna max 병렬 레인 세 개를 통합해 `가치`/`가격`, `수리`, `얘기`/`이야기`를
중앙 parser와 WebSocket connector에 연결했다. 가치 조회는 전당포/수리점의 canonical
직접 소지 root를 읽기 전용 receipt로 저장하고, 수리는 주입 RNG·piety를 포함한 원자
repair candidate로 파손/환불/삭제 또는 shots 복구를 수행한다. 개인 메시지는 정확한
online player를 선택해 receipt의 deterministic recipient event를 대상 연결에만 보낸다.

`go test -race ./... -skip '^TestRoomBodyCorpus$'`, `go vet ./...`, Linux ARM64 build,
live connector 회귀와 ARM64 PostgreSQL 17 세 명령 receipt/replay 통합 테스트를 통과했다.
전체 C prefix/key/ANSI parity, merchant full behavior, strict corpus 63건, 실기기
IME/mobile, WSS/Ingress 및 testnet 배포는 아직 인수하지 않았다.

## NPC 대화·그룹말·상인 구입

현재 Go connector는 원작 터미널 흐름을 유지한 세 bounded command를 추가로 받는다.

- `대화 <NPC> [topic]`: same-room exact NPC 선택, MTALKS topic 계약 부재 시 fail-closed,
  receipt 기반 actor/observer projection
- `그룹말 <메시지>` 또는 `<메시지> 그룹말`(`무리말`, `=` 별칭 포함): mixed follower
  순서와 canonical flag 경계, exact recipient event
- `<NPC> <item> 구입`: `MerchantOffers` server-owned catalog, nested reward copy,
  gold/weight/capacity 검증

모든 command는 `ExecuteGame` receipt/replay 경계를 사용하며, replay에서는 reducer·ID
allocator·fan-out을 다시 실행하지 않는다. merchant catalog가 없거나 legacy `Carry`가
명시적으로 이관되지 않은 경우 실행을 거부한다. 이 기능들은 전체 C 명령/경제 parity,
strict room corpus 63건, NPC full tick/broadcast, 실제 IME/mobile 및 testnet 배포의
완료를 의미하지 않는다.

## 묘사·사용자 조회·귀환

현재 connector는 다음 원작 터미널 명령의 bounded Go slice를 지원한다.

- `<설명> 묘사`/`묘사`: 31바이트·UTF-8 경계와 trailing-space canonical 저장
- `사용자검색 <이름>`/`사용자정보 <이름>`: online canonical exact 조회와 가시성
  fail-closed; offline legacy file metadata는 사용하지 않음
- `귀환`/`귀`: 전투·그룹 거부, PFRTUN 목적지, 고레벨 도력 소진, 원자적 이동과
  source/destination room event

세 명령 모두 `ExecuteGame` receipt/replay를 사용하고, replay에서는 room fan-out을
반복하지 않는다. race/vet/ARM64 build 및 실제 ARM64 PostgreSQL 17 receipt 검증을
통과했지만 전체 C parity, strict corpus 63건, IME/mobile 실기기, WSS/Ingress와
testnet 배포는 별도 게이트다.

## 비교·감정·명명

터미널은 다음 아이템 명령을 canonical Go world/session 경계로 처리한다.

- `비교 [물건] [occurrence]`: 무기·방어구만 원작 직업/레벨 판정으로 비교한다.
- `감정 <물건> [occurrence]`: 도둑 또는 INVINCIBLE 이상만 직접 소지품을 감정한다.
- `<물건> [occurrence] <새 이름> 명명`: OCNAME 아이템을 80-byte bounded name으로
  원자적으로 변경하고 ONAMED를 설정한다.

세 명령은 exact canonical direct root만 선택하며 nested/equipped/legacy/unmigrated 자료는
추측하지 않고 거부한다. receipt replay는 상태 변경과 room announcement를 반복하지 않는다.
race/vet/ARM64 build와 실제 ARM64 PostgreSQL 17 저장·재생 검증을 통과했지만 전체 C
명령 parity, strict room corpus, IME/mobile 실기기, WSS/Ingress와 testnet 배포는 남아 있다.

## 능력 명령과 칭호

현재 터미널 connector는 다음 bounded Go slice도 처리한다.

- `활보법`/`신원법`: 직업·확률·cooldown·효과 stat과 legacy flag/timer를 atomic하게 반영
- `경계`/`잠력격발`: 준비·잠력 효과와 원작 cooldown/실패 경계를 typed receipt로 저장
- `칭호`/`칭호삭제`: 웹 계정과 무관하게 xterm에서 78-byte title을 저장·조회·삭제

모든 명령은 `ExecuteGame` receipt/replay를 사용한다. 성공한 첫 실행만 room observer event를
전달하고, replay에서는 RNG·state mutation·fan-out을 재실행하지 않는다. race/vet/ARM64
build와 실제 ARM64 PostgreSQL 17 저장·재생 검증을 통과했지만 전체 C 명령 parity, strict
room corpus, NPC full cadence, 실기기 IME/mobile, WSS/Ingress 및 testnet 배포는 별도 인수
조건으로 남아 있다.

## 기공집결·살기충전·참선

터미널 connector는 원작 `command9.c`의 세 self-ability bounded slice도 처리한다.

- `기공집결`: 검사 계열 권한, 600초 cooldown, 성공 힘 +3/`PPOWER` 효과와 실패 cooldown
- `살기충전`: 자객·도둑 계열 권한, canonical WIELD gate, 성공 THACO -3/`PSLAYE` 효과
- `참선`: 무사·불제자 계열 권한, 700초 cooldown, 성공 지능 +3/`PMEDIT` 효과

모두 exact bare alias만 받고, clock/RNG를 주입한 proposal/apply와 typed receipt/event를
사용한다. 최초 commit만 room observer에 전파하며 동일 command ID replay에서는 RNG·상태
변경·방송을 재실행하지 않는다. 이 batch는 레인별 targeted race/gofmt만 반복하고 통합
경계에서 전체 gate와 격리 PostgreSQL을 한 번 실행하는 cadence를 따른다. 전체 C alias/
prefix/key parity, strict room corpus 63건, NPC full cadence, 실기기 IME/mobile,
WSS/Ingress와 testnet 배포는 아직 별도 인수 조건이다.

## 검증 scope와 중복 실행 방지

Go 기능 레인은 담당 패키지의 `gofmt`와 targeted `go test -race`만 실행한다. 전체 race·
`go vet`·diff는 필요할 때 `scripts/run-go-validation.sh integration`에서 실행하고,
Linux ARM64 cross-build는 실제 main 병합 시 `scripts/run-go-validation.sh main`에서 한
번만 실행한다. 영속성 변경 batch의 PostgreSQL receipt/replay도 고유 격리 DB에서 한 번만
실행한다. `scripts/run-go-validation.sh fast`는 world/session/transport 표적 회귀를 위한
기본 경로다.

수동 GitHub CI는 `.github/workflows/ci.yml`의 `validation_scope`로 같은 경계를 따른다.
`fast`는 Go 표적 race, `integration`은 전체 Go gate, `main`은 ARM64 cross-build를 포함한
main 병합 gate, `release`만 기존 Supabase/DB,
브라우저/stack 및 x64·Windows·macOS 호환 matrix를 실행한다. 자동 push/PR workflow는 없으며,
호환성·차트·실기기 검증을 삭제하지 않고 release 시점으로 이동했다.

## 원작 `!` 명령 재실행

`WorldConnector`는 C `command()`의 연결별 `lastcommand`를 순수 입력 경계로 유지한다.
`!`은 직전 명령을 재실행하고 `!suffix`는 직전 명령에 suffix를 붙여 기존 parser로
보낸다. 저장된 줄임말은 `$1..$16`/`$*`의 단일 명령 치환까지 확장하며 `;` queue와
잘못된 문법은 fail-closed한다. history/확장 결과는 `State`, 계정, PostgreSQL receipt에
별도 필드로 저장하지 않고 실제 실행 명령의 기존 receipt/replay 규칙을 따른다. pure
session 테스트와 connector 회귀가 race 검사를 통과했다. 전체 C 약어 우선순위·다중
command queue·출력 parity는 아직 별도 범위다.

브라우저 경계도 `run-go-process-postgres-browser-e2e-local.sh --allow-disposable`로
재검증했다. 전용 ARM64 PostgreSQL 17에서 가입→월드 입장→줄임말 등록·실행→`!` 재실행→
재로그인과 기존 캐릭터 중복 세션 거절을 포함한 Chromium 테스트 2개가 통과했다(16.0초).
이 증거는 전체 명령 parity·실기기 IME/mobile·운영 배포 인수를 의미하지 않는다.

## 2026-09-09 뇌물·숨기기·도망 함정

세 Luna max 병렬 레인을 통합해 `뇌물`, `숨겨`/`숨어`, arrival trap이 있는 `도망`을
parser→world reducer→receipt/replay→WebSocket room event 경계에 연결했다. targeted race,
fast/merge gate와 ARM64 PostgreSQL 17 `TestPostgresBribeCommandPersistsAndReplays`를
통과했다. 전체 C parity, strict room corpus, NPC cadence, IME/mobile, WSS/Ingress와 testnet
배포는 남아 있다.

## 2026-09-09 NPC phase ordering·저장 명령·대화 자산 provenance

`NPCWorldScheduler`를 프로세스 경계에 연결했다. 기존 maintenance/resource/combat
worker를 따로 실행하지 않고 하나의 worker가 매 wake-up마다 maintenance → resource →
combat 순서를 지킨다. 기본 phase cadence는 각각 1초/20초/1초로 유지하며 최소 cadence로
깨운 뒤 각 tick의 durable slot suppression을 적용한다. maintenance 또는 resource가
오류를 반환하면 후속 phase를 실행하지 않고 다음 cadence에 같은 pending request를 재시도한다.
시작 로그에는 wake/phase cadence와 `ordered`가 표시되고, 종료는 이 worker를 먼저 drain한다.

원작 `src/global.c` cmdno 52의 `저장`/`src/command8.c:savegame`도 연결했다. Go에서는
mutation 자체가 canonical state receipt로 저장되므로 별도 파일 저장을 중복하지 않는
no-state receipt로 응답한다. xterm 입력 `저장`과 명시적 호환 alias `save`만 허용하며,
offline actor·추가 인자·제어문자·잘못된 UTF-8은 receipt 전에 거절한다. 동일 command ID
재생은 state/응답을 그대로 돌려주고 commit/RNG를 반복하지 않는다.

`resources_utf8/objmon/talk/아파트_수위_아저씨-127`는 manifest와 Git blob이 동일하지만
CP949 offset 216의 standalone `0xBA`로 strict decode에 실패한다. 복구 원본을 확인하지
못한 상태에서 임의 보정하지 않고 provenance 문서와 regression으로 전체 talk catalog의
fail-closed 동작을 고정했다.

새 변경 검증: scheduler/session/live connector targeted race, cmd 프로세스 테스트(격리 DB
환경이 없으면 의도적 skip). 전체 race/vet/integration, ARM64 cross-build, 실제 DB·browser·
release matrix는 기능 레인마다 반복하지 않고 승인된 cadence에서만 실행한다.
