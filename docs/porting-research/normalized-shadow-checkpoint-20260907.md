# Normalized shadow comparator 검증 체크포인트

2026-09-07, 기반 커밋 `7699993`.

## 이번 수정과 증거

- 숫자 범위 검사만으로는 normalized projection의 런타임 타입을 보장하지 못했다. `level: 42n`을 넣는 회귀 테스트가 digest 직렬화에서 `TypeError`로 실패하는 것을 먼저 확인했다.
- signed i64는 `bigint`, 좁은 정수는 `number`라는 기존 Rust-backed Node projector 계약을 비교기에서도 검사한다. 필드 타입이 뒤바뀐 derivation은 조회 전에 `PROJECTION_DERIVATION_FAILED`, 저장 행은 `INVALID_RECORD`로 분류한다.
- scalar, daily, timer, item의 숫자 필드마다 타입을 바꿔 양쪽 입력을 검사한다. receipt의 canonical name 또는 storage format 불일치도 실제 receipt-bound parser가 거부하는 테스트를 추가했다.
- 패키지 typecheck 통과. C oracle → Rust CLI → Node bridge 실행 결과: 145 tests, 140 pass, 0 fail, 5 기존 조건부 skip. 이 결과는 실제 PostgreSQL 또는 배포 환경 검증이 아니다.

## 남은 목표

현재 comparator는 주입된 조회 결과만 비교한다. 독립 리뷰, SQL 조회 adapter의 실제 세션 연결, PostgreSQL 계약 실행, 저장·재시작·웹 가입/기존 계정 연결의 배포 환경 검증이 남아 있다. legacy 파일은 여전히 gameplay 저장 권위이며 이번 변경은 DB 권위 전환을 수행하지 않는다.

다음 조회 adapter는 receipt/artifact/projection을 `(character_id, command_id)`로 결합하고 world를 함께 확인해야 한다. normalized root와 순서가 고정된 daily/timer/item 행을 읽고 i64를 손실 없이 복원해야 한다. 전용 조회 역할 계약은 실제 migration 및 PostgreSQL 검증이 필요하다.

## 조회 identity 후속 수정

비교기의 주입 조회 계약을 `findByIdentity({ worldId, characterId, commandId })`로 변경했다. DB 기본키는 `(character_id, command_id)`이므로 command 단독 조회는 다른 캐릭터의 유효 행까지 중복으로 오인할 수 있다. 회귀 테스트는 같은 command를 사용하는 두 캐릭터를 각각 선택하고 world도 구분한다. 조회기가 다른 캐릭터를 반환할 때는 결과의 identity 재검증으로 `EVIDENCE_MISMATCH`를 유지한다. 조회 계약을 변경하기 전 새 테스트는 `RECORD_READ_ERROR`로 실패했고 변경 후 통과했다. 이는 주입 조회 계약의 증거이며 실제 SQL adapter나 PostgreSQL 실행 증거는 아니다.

## SQL 조회 adapter 구현

`PostgresNormalizedProjectionReader`는 caller가 제공하는 전용 read-only session에서 현재 transaction의 read-only 여부를 확인한 뒤 parameterized SELECT 한 번을 실행한다. root/artifact/receipt/manifest를 복합키와 writer·hash·format·octet 정보로 결합한다. daily/timer/item은 slot/index 순으로 집계하며 item count와 마지막 index도 확인한다. projection JSON은 SQL `text`로 받아 기존 Rust 출력 파서를 공유하므로 signed i64의 최댓값과 최솟값을 반올림 없이 복원한다. 조회 결과는 비교기와 같은 닫힌 행 검증을 거친다.

테스트는 scope 파라미터, 읽기 전용 전제, i64 극값, 부재·중복, 잘못된 projection/metadata, 조회 실패 무재시도, await 도중 caller identity 변경을 검증한다. typecheck와 C→Rust→Node 브리지는 150 tests 중 145 pass, 0 fail, 5 기존 skip이다. SQL은 실제 PostgreSQL에서 실행하지 않았으므로 SQL 실행·grant·RLS·runtime consumer 연결은 여전히 미검증/미완료다. 현재 adapter는 세션이나 역할을 생성하지 않는다. caller가 connection을 독점하고 read-only 상태를 유지해야 한다.

서브 에이전트 배정: 핵심 설계·복잡한 구현·독립 리뷰는 Astra/medium, 범위가 명확한 테스트·문서·정적 점검은 Luna/max. 직전 Orca Astra worker는 모델 호환성 오류로 리뷰를 수행하지 못했으므로 독립 리뷰 완료로 계산하지 않는다.

## C·Rust·조회 adapter 연결 검증

실제 C inventory fixture를 receipt-bound artifact로 읽고 Rust CLI의 실제 JSON 출력을 SQL 결과 행으로 주입해, 조회 adapter와 shadow comparator를 함께 통과시키는 테스트를 추가했다. 정상 일치, 행 부재, 중복, writer revision 불일치, 손상된 projection JSON을 각각 검증한다. 조회 파라미터가 world/character/command 복합 식별자인지와 조회 횟수도 확인한다.

2026-09-07 재실행: typecheck 통과, C oracle·Rust CLI·Node bridge는 151 tests, 146 pass, 0 fail, 5 기존 조건부 skip. 이 테스트의 DB 경계는 주입된 행이므로 실제 PostgreSQL SQL 실행, 역할 권한, runtime 연결 또는 배포 검증을 대신하지 않는다.

## 정적 점검 및 전용 조회 계정 후보

Luna/max의 `ctx_cce041432b02` 점검은 `9d8f6ed`의 SQL을 20260909/14/15 및 20261006 migration과 대조했다. 컬럼, 복합 결합, 집계 순서/count, 손실 없는 숫자 복원에서 범위 내 결함을 발견하지 못했다. 세션의 계정 identity와 권한을 보장하는 것은 여전히 caller의 책임이다. 이는 제한된 정적 점검이며 전체 독립 리뷰나 PostgreSQL 실행 증거는 아니다. 결과를 회수하고 worker-release가 `released`와 transcript 보존을 확인했다.

기존 `mud_replay_reader_login`은 메타데이터 컬럼의 닫힌 권한 계약을 가진다. 이를 넓히지 않도록 `20261014000000_player_snapshot_normalized_v1_replay_reader.sql`에 별도 `mud_normalized_replay_reader_login` 후보를 추가했다. 7개 관계의 비교용 컬럼만 SELECT하고 raw artifact payload는 제외한다. 기본 read-only 세션과 SELECT용 RLS 정책을 설정하며 기존 운영 비밀번호는 replay 시 유지한다. 게임 권위나 consumer를 활성화하지 않는다.

먼저 작성한 `supabase/tests/player_snapshot_normalized_v1_replay_reader_contract.sql`은 전용 계정 속성, 역할 membership 부재, 정확한 조회 컬럼, RLS, 쓰기 권한 부재 및 payload 미노출을 검사한다. **이 SQL 계약 테스트와 migration은 아직 PostgreSQL에서 실행하지 않았다. RED/GREEN 통과로 계산하지 않는다.** 로컬 정적 검사는 adapter의 143개 컬럼 참조가 7개 관계의 grant 목록에 포함되는지만 확인했다. 다음 단계는 disposable PostgreSQL에서 migration·계약·실제 adapter SELECT를 함께 실행하고, 검증된 전용 세션을 runtime 비교기에 연결하는 것이다.

## 실제 조회 integration 실행기

`supabase/tests/player_snapshot_normalized_v1_replay_reader_integration.mjs`는 이미 migration과 fixture 저장이 끝난 disposable DB를 대상으로 한다. 데이터나 계정을 만들지 않으며 `DATABASE_URL`로 fallback하지 않는다. 전용 login의 current/session user와 기본 read-only 설정을 확인하고 독점 connection의 read-only transaction에서 실제 adapter를 실행한다. 저장된 projection을 tree inventory fixture의 실제 Rust 변환 결과와 비교하고 snapshot hash/크기, 다른 world 조회 부재, raw payload SELECT 거부, 오류 후 재조회까지 검사한다. 종료 시 rollback과 connection 종료를 시도한다.

실행 전 relay의 `dist`와 Rust normalized projector가 빌드되어 있어야 한다. `NORMALIZED_READER_ALLOW_DISPOSABLE=1`, `NORMALIZED_READER_TEST_DATABASE_URL`(전용 login), `NORMALIZED_READER_TEST_WORLD_ID`, `NORMALIZED_READER_TEST_CHARACTER_ID`, `NORMALIZED_READER_TEST_COMMAND_ID`, `M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER`(절대 경로)를 명시한 뒤 Node로 실행한다. 해당 identity에는 checked-in tree fixture 및 **그 fixture에서 Rust가 실제로 계산한 projection**이 receipt/manifest/artifact/normalized recorder 경로로 미리 저장되어 있어야 한다. 기존 persistence SQL 계약의 인위적인 i64 극값 projection은 이 positive fixture를 대신할 수 없다.

검증: Node 문법 검사 통과, DB에 연결하지 않는 guard tests 2개 통과(필수 설정별 누락 및 잘못된 계정 거부). **실제 integration은 아직 실행하지 않았다.** disposable fixture seed 연결, PostgreSQL 실행 증거, production runtime consumer와 배포 검증은 남아 있다.

## 별도 조회 세션용 seed SQL 후보

`supabase/tests/player_snapshot_normalized_v1_replay_reader_seed.sql`은 disposable DB 소유자가 실제 tree fixture 표현식(`pvi_tree_payload`)과 그 fixture에서 Rust가 출력한 JSON(`normalized_projection`)을 전달해 실행할 후보다. receipt → manifest → artifact → normalized recorder 순서로 저장하고 하나의 transaction을 commit하므로 이후 별도의 전용 reader 세션에서 조회할 수 있다. fixed test identity를 새로 INSERT하며 기존 행을 삭제·덮어쓰지 않는다. 각 recorder의 결과는 정확히 한 행의 `RECORDED`여야 한다. source post hash/크기는 합성 fixture metadata이며 실제 legacy save 증거가 아니다.

조회 실행기에 전달할 identity: world `normalized-reader-test`, character `a9140000-0000-0000-0000-000000000001`, command `c9140000-0000-0000-0000-000000000001`. 기존 persistence 계약의 rollback fixture와는 별개다. SQL은 아직 미실행이며 Rust 출력 준비와 seed/계약/reader의 자동 연결도 남아 있다. 2026-09-07 Docker 도구 경로만 확인했으며 daemon이나 DB에는 접속하지 않았다. 사용자에게 로컬 임시 PostgreSQL 생성·검증·정리 실행 허용을 질문했고 답변을 기다리는 중이다.

## 사용자 허용 후 실제 PostgreSQL 17 검증 — 2026-09-07

위의 미실행/허용 대기 기록 이후 사용자가 로컬 Docker 임시 DB 생성·테스트·정리를 허용했다. Docker `desktop-linux`의 로컬 Unix socket을 확인하고 `postgres:17-alpine` 임시 인스턴스에서 실행했다. 운영 DB, Supabase 운영 서비스, k8s에는 접속하지 않았다.

실행에서 두 결함을 재현하고 수정했다.

- 긴 constraint 이름이 63바이트로 잘려 20261003의 inventory graph migration에서 중복 이름으로 실패했다. 7개 migration의 긴 constraint 이름을 48자 prefix와 원본 이름 SHA-256의 10자리 suffix로 바꾸고 관련 SQL 테스트의 이름 참조도 맞췄다. 새 이름 길이 회귀 테스트의 실패 후 통과를 확인했다. 기존 설치 DB의 constraint를 rename하는 작업은 하지 않았으며, 이 수정은 새 설치 SQL의 선언과 참조를 정정한다.
- normalized recorder가 `WITH ORDINALITY`의 `ordinality` 컬럼을 `ordinal`로 참조해 실제 seed 저장에서 실패했다. validation/insert/exact-retry 비교 경로의 참조를 고쳤다. 실제 fixture seed 저장과 기존 persistence SQL 계약이 이후 통과했다.

확인된 결과:

- relay TypeScript build 및 Rust normalized projector build 통과.
- 실제 tree fixture → Rust JSON → receipt/manifest/artifact/normalized recorder 저장 commit 통과.
- 전용 login으로 실제 Node SQL adapter 실행: Rust projection과 정확한 일치, snapshot hash/크기, 다른 world 조회 부재, payload SELECT 거부 및 재조회 통과.
- 역할까지 비어 있는 별도 새 PostgreSQL 인스턴스에서 20260902부터 20261014까지 게임 migration 전체 적용, normalized persistence SQL 계약, normalized reader SQL 계약 통과.
- Node의 constraint 이름 테스트와 integration guard tests: 3 pass, 0 fail.

범위 제한: 20260901 lobby migration은 Supabase Realtime 스키마를 요구하므로 기존 PG 계약 lane처럼 제외했다. 일반 PostgreSQL에 적용하면 Realtime 스키마 부재로 실패한다. 같은 클러스터의 다른 DB로 전체 migration을 반복하면 공유 역할의 membership 전제로 실패하므로 fresh-chain 검증은 별도 인스턴스로 수행했다. 실제 Supabase 전체 설치·웹 가입/계정 연동·게임 저장 권위 전환·k8s 배포 검증은 여전히 남아 있다.

두 임시 컨테이너 `muhan-normalized-contract-7f913c`, `muhan-normalized-contract-7f913d`는 테스트 라벨/ID를 확인한 뒤 종료했고 auto-remove 및 컨테이너 목록 부재를 확인했다. tmpfs의 합성 테스트 데이터도 제거되었다. 재현용 전체 자동 orchestration script와 독립 변경 리뷰는 후속 작업이다.

## 전용 연결 풀 adapter — 2026-09-07

`PostgresNormalizedProjectionSessionReader`를 추가했다. 별도 전용 login URL만 허용하고 writer pool과 공유하지 않는다. 조회마다 연결을 독점해 `BEGIN READ ONLY` 후 current/session user 및 기본 read-only 설정을 검사하고 기존 SQL adapter를 호출한다. 성공·실패 모두 rollback 후 release하며 실패한 연결은 재사용하지 않도록 destroy한다. `close()`는 소유한 pool을 종료한다. 이 클래스는 아직 운영 CLI/worker에 자동 연결되지 않았다.

테스트를 먼저 작성해 모듈 부재로 실패하는 것을 확인한 뒤 구현했다. 정상 정리, 계정/기본 설정 불일치, BEGIN/조회/ROLLBACK 실패, 잘못된 URL 거부의 4개 테스트가 통과했다. typecheck/build 및 전체 C→Rust→Node bridge는 155 tests, 150 pass, 0 fail, 5 기존 조건부 skip이다.

별도 새 PostgreSQL 17 컨테이너에서 게임 migration, 실제 Rust fixture seed, 전용 login을 준비하고 integration 실행기에 이 연결 pool 경로를 추가해 실행했다. 기존 직접 SQL 검증과 함께 pool 재사용 2회 모두 기존 행 전체와 일치했다. `muhan-normalized-session-834be1`은 라벨/ID 확인 후 종료했고 auto-remove와 목록 부재를 확인했다. 운영 DB/k8s는 변경하지 않았다. 다음은 운영 comparison job의 입력·종료·결과 계약을 이 연결 경로에 붙이고 배포 전 검증하는 단계다.

## 단발 normalized shadow CLI

`services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-normalized-shadow-cli.ts`를 추가했다. 빌드 후 `node dist/player-snapshot-v1-normalized-shadow-cli.js --once`로 실행한다. 명시적인 `M4_NORMALIZED_SHADOW_OUTBOX_PATH`, `M4_NORMALIZED_SHADOW_DATABASE_URL`, `M4_NORMALIZED_SHADOW_PROJECTOR_PATH`가 필요하다. 경로는 절대 경로이며 DB URL은 기존 session reader와 같은 전용 login 검증을 공유한다. 일반 `DATABASE_URL` fallback이나 기본 relay에서의 자동 활성화는 없다.

입력은 기존 읽기 전용 filesystem scanner를 이용한 정확히 한 artifact/receipt 쌍이다. receipt-bound parser → 실제 Rust projector → normalized comparator → 전용 session reader를 기본 의존성으로 연결했다. stdout은 format/version/classification만 포함하는 JSON 한 줄이며 MATCH만 exit 0이다. reader 생성 실패·비교 예외·연결 종료 실패는 각각 실패로 출력하고 원시 에러는 출력하지 않는다. 종료 실패 후 MATCH로 보고하지 않는다.

CLI 테스트를 먼저 작성해 모듈 부재 실패 후 구현했다. 설정 누락과 --once 검사, 실행·정리 순서, close 실패, 비교 예외, 잘못된 입력 전 DB 생성 방지 및 실제 tree fixture의 receipt pairing을 검증했다. build와 C→Rust→Node bridge는 160 tests, 155 pass, 0 fail, 5 기존 skip이다. **이번 CLI 프로세스 전체를 실제 filesystem outbox와 PostgreSQL에 동시에 연결한 E2E는 아직 미실행이다.** 앞 절의 PostgreSQL 검증은 조회 adapter/session의 증거이지 새 CLI 전체의 증거가 아니다. 독립 리뷰, 실제 CLI E2E, 배포 job/chart 연결이 다음 단계다.

## Linux CLI E2E 준비 및 저장 공간 차단

integration 실행기에 실제 mode 0700 outbox와 mode 0600 artifact/receipt 파일을 생성하고 CLI 자식 프로세스를 실행하는 MATCH/INVALID_INPUT/MISSING_RECORD 사례를 추가했다. stdout·exit code와 입력 파일이 변경되지 않았는지도 검사한다. 임시 파일은 finally에서 제거한다. 이미지 안의 `/app` 빌드 산출물로 실행할 때 `NORMALIZED_READER_TEST_RELAY_ROOT=/app`을 지정할 수 있다.

macOS 실행에서는 CLI MATCH가 INVALID_INPUT으로 실패했다. 기존 filesystem scanner는 실제 Linux에서만 동작하도록 설계되어 있고, 테스트 생성 receipt의 필드 순서도 canonical 형식과 달랐다. 순서는 수정했고 Linux-only 조건은 유지했다. 실제 파일 E2E는 Linux 이미지에서 다시 실행해야 한다. reader/session 단독 경로의 앞선 통과 결과와 구분한다.

relay Dockerfile이 기존 replay verifier만 포함하고 normalized projector를 포함하지 않는 것도 확인했다. 같은 Rust build에 `player_snapshot_v1_normalized_project`를 추가하고 최종 이미지의 `/usr/local/libexec/muhan/`으로 복사하도록 수정했다. 로컬 이미지 `muhan-normalized-e2e:local` 빌드를 시도했으나 Rust base image unpack 단계에서 `no space left on device`로 실패했다. 이미지 빌드/CLI E2E 통과나 배포 완료로 계산하지 않는다.

Docker 사용량은 이미지 21.01GB(회수 가능 10.84GB), build cache 12.17GB(회수 가능 630.9MB)로 보고됐다. 다른 프로젝트 리소스와 캐시는 삭제하지 않았다. 이번 임시 DB `muhan-normalized-cli-392a41`만 라벨/ID 확인 후 종료·auto-remove·목록 부재를 확인했다. 최신 문법 검사와 guard tests 2개는 통과했다. Linux E2E 재시도에는 Docker 공간 확보가 필요하다.

## 공간 확보 후 Linux CLI E2E 통과

사용자의 공간 정리 완료 응답 후 Docker 이미지 사용량이 9.977GB로 줄어든 것을 확인했다. 추가 사용자 이미지를 삭제하지 않고 빌드를 재시도했다. Rust 1.85 image에서는 SHA-256 코드의 `slice::as_chunks`가 아직 안정화되지 않아 컴파일에 실패했다. 동일한 64/4 byte 분할과 big-endian word 구성을 `chunks_exact`로 표현해 고정된 toolchain을 유지했다. 기존 SHA 표준 벡터를 포함한 Rust library tests 25개가 통과하고 Rust 1.85 기반 Docker release build도 통과했다.

검증 이미지: 로컬 `muhan-normalized-e2e:local`, build manifest list `sha256:1fa44554e48f226366075e2552658856a9aa44234d474f9e36456aecd851cfc2`. 이 이미지는 registry에 push하거나 k8s에 배포하지 않았다.

새 PostgreSQL 17 인스턴스에 게임 migration과 실제 tree fixture projection을 저장했다. 최종 이미지의 기본 `node` 사용자로, read-only root filesystem과 임시 `/tmp`를 사용해 integration 실행기를 실행했다. 테스트 파일/fixture만 read-only mount하고 별도 이미지 안의 `/app` 산출물과 Linux Rust projector를 사용했다. DB 컨테이너 network namespace에 연결하여 다음이 모두 통과했다:

- 실제 SQL reader, 전용 연결 pool 재사용, Rust와 저장 projection의 정확한 일치.
- 실제 파일 scanner → receipt-bound parser → Rust → DB → CLI JSON/exit code: MATCH(0), INVALID_INPUT(1), MISSING_RECORD(1).
- 각 실행 후 artifact/receipt 파일 byte 불변 확인 및 raw payload SELECT 거부.

테스트 runner는 auto-remove되었으며 임시 DB `muhan-linux-e2e-db-72c54a`도 ID/label 확인 후 종료·auto-remove·목록 부재를 확인했다. 합성 데이터는 제거되었고 로컬 검증 이미지는 후속 검증을 위해 남겨 두었다. 기존 공간 부족 및 Linux CLI E2E 차단은 해소됐다. 독립 리뷰, chart/job 연결, 실제 게임 onboarding 및 DB 저장 권위 전환은 여전히 미완료다.

## 배포 저장소 선행 수정

`/Users/jjangg96/Documents/1xp/tesnet-1xp.nosync`의 `codex/muhan-onboarding-safety`에서 기존 chart 구성을 확인했다. 이 chart는 아직 normalized recorder/reader migration과 Job을 포함하지 않는다. 기존 복사 SQL 5개(20260915/19/28, 20261001/03)에 소스에서 수정한 constraint 이름 충돌이 남아 있었고, 배포용 통합 Dockerfile도 normalized projector를 빌드·복사하지 않았다.

배포 저장소 커밋 `673754d5`: 위 SQL은 이름 변경 외 차이가 없음을 확인한 뒤 소스와 byte 일치하도록 맞췄다. 원본 SQL hash를 고정하는 기존 렌더링 테스트도 해당 5개 hash만 갱신했다. 통합 Dockerfile에 normalized projector build/COPY/root-owned 0555 설정을 추가했다. 새 prerequisite 테스트 2개를 먼저 실패시키고 수정 후 기존 chart tests 44개와 함께 46 pass, 0 fail을 확인했다. 통합 이미지 자체의 새 빌드나 배포 실행 증거는 아니다.

새 normalized 비교 Job, 전용 secret 및 DB migration 연결, network policy, 운영 입력 쌍 준비, amd64 통합 이미지 검증은 여전히 다음 작업이다. 두 저장소의 변경은 로컬 커밋이며 registry/Git 원격 push 및 k8s 작업은 하지 않았다.

## normalized shadow chart 연결

배포 저장소 커밋 `c3857ea6`에 기본 비활성 normalizedShadow 설정을 추가했다. `enabled`는 20261006/14 스키마 및 종속 artifact migration 준비만 켜고, 별도 `run`이 실제 Job을 렌더링한다. migration은 검증된 소스와 byte 일치하며 checksum에도 반영된다. run에는 짧은 manualRunId, 별도 PVC/단일 subPath, 전용 기존 Secret이 필수다. Job은 10001 UID, 읽기 전용 PVC 및 root filesystem, /tmp memory volume, backoff 0, 120초 deadline, 서비스 계정 token 없이 CLI --once를 실행한다.

전용 Secret key는 `normalized-reader-db-uri`이며 계정 비밀번호 설정과 Secret 생성은 자동화하지 않았다. 기존 migration hook의 완료를 먼저 확인하고 credential/input을 준비한 다음 run을 켜는 2단계 운영 절차를 `muhan-mud/NORMALIZED-SHADOW.md`에 기록했다. Job은 일반 retained Job이며 hook/TTL 자동 삭제를 사용하지 않는다. 동일 ID는 기존 Job이 유지되는 동안 재실행되지 않으며 새 실행에는 새 ID가 필요하다.

NetworkPolicy enabled 시 새 Job의 ingress를 닫고 같은 release Postgres/DNS egress만 허용하며 DB ingress 허용 대상도 추가한다. RED 3개 확인 후 chart 구현을 추가해 신규 3개 및 기존 46개 테스트, 총 49 pass/0 fail을 확인했다. 이후 read-only mount 수, service-account token, writer credential 미참조, hook 부재 assertion도 추가해 신규 3개를 다시 통과했다.

실제 k8s Job, amd64 통합 이미지, 운영 credential/input provisioning은 아직 미검증/미완료다. Git/registry push 또는 운영 배포는 하지 않았다. 다음은 독립 리뷰와 배포 artifact/source revision 검증이다.

## 재개 후 배포 검증 범위 확대

목표 active 상태를 확인하고 배포 저장소의 chart 검사 외에 release wrapper와 Docker source-path 검사까지 실행했다. 58개 중 57개가 통과했고 source-path 검사 1개가 실패했다. 해당 검사는 검토 기준 `d4d70643ba9d21ae537417c1d82d5e1482b43fd0`와 소스 checkout HEAD가 같아야 하는데, 실행 시 HEAD는 `98ed194b55d95b5da90f46f6dcc8d728b44a978b`였다. 새 소스 검토 없이 상수만 갱신하거나 검사를 제거하지 않았다. 이 실패가 해결되기 전 전체 packaging 검증 통과로 보고하지 않는다.

배포 `muhan-mud/build.sh`는 private Git에서 정확한 원격 source SHA를 가져오며, 호출하는 루트 `build.sh`는 cloud builder의 `--push`까지 실행한다. 따라서 이를 로컬 전용 이미지 테스트로 실행하지 않았다. 원격에 존재하는 검토 완료 source SHA 확정, source-path 검증 기준 갱신, 실제 amd64 통합 이미지 검증이 배포 전 선행 작업이다.

배포 커밋 `778a30e3`은 회귀 테스트만 보강한다. 실행 입력 각각의 누락·긴 ID·하위 경로 거부, default/schema-only/network-off 시 비교 정책 부재, 실행 시 같은 release 선택과 PostgreSQL/DNS 포트, DB ingress의 비교 Job 연결을 검사한다. 최초 테스트의 리소스 선택이 PostgREST 이름까지 부분 일치하는 결함을 바로잡아 정확한 PostgreSQL 정책 이름으로 검사한다. 이는 테스트 구현 수정이지 chart 런타임 결함 수정은 아니다. 보강 후 prerequisite/normalized/chart 50개가 모두 통과했다. 위의 별도 source-path 검사는 여전히 미해결이다. 커밋은 로컬이며 운영 변경은 없다.

Luna/max의 제한적 독립 정적 점검 `task_1416f14fae81` / `ctx_b7da02f59d62` 결과를 회수했다. 검토 대상은 배포 `c3857ea6`이며 이후 테스트 보강은 대상이 아니다. (1) 일반 Job이 post-install migration보다 먼저 실행될 수 있어 문서의 2단계 절차가 코드로 강제되지 않는 점, (2) 기존 Postgres ingress의 component-only allowlist에 normalized Job을 추가해 다른 release의 같은 component label도 허용되는 점을 지적했다. 후자는 비교 Job 자체의 egress가 같은 release로 제한되는 것과 별개다. 두 항목은 다음 수정/검증 대상으로 남겼다. retained Job의 이미지 변경 upgrade 동작도 미검증이다. 정적 검토를 운영 동작 보증으로 계산하지 않는다. 결과 보존과 `worker-release`의 `released / closed_agent_terminal / captured`를 확인하고 완료 메시지를 처리했다.

## DB ingress 범위 수정

배포 커밋 `e9ffe418`에서 normalized comparison을 기존 component-only DB allowlist에서 제거하고 같은 앱·release·component를 모두 요구하는 별도 TCP 5432 ingress 항목으로 옮겼다. 다른 기존 consumer 규칙은 변경하지 않았다. 비교 Job의 network policy가 활성화되는 기존 조건(enabled + run)을 그대로 따른다. 회귀 테스트를 먼저 변경해 broad allowlist가 남아 있는 실패를 확인하고 구현 후 prerequisite/normalized/chart 50개 통과를 확인했다. default/schema-only에는 새 ingress가 없으며 같은 release selector와 5432 포트가 렌더링되는 것을 검사한다. 실제 CNI 트래픽 검증은 수행하지 않았다.

실행 순서 문제는 여전히 남아 있다. migration Job은 post-install/post-upgrade hook이면서 성공 즉시 삭제되므로, 후속 도구가 단순히 이름으로 완료 Job을 기다리는 방식은 사용할 수 없다. 비교 Job만 무조건 post-hook으로 바꾸면 일상적인 upgrade마다 실행될 수 있어 기존 명시적 단발 실행 계약도 검토해야 한다. 스키마 준비 성공을 실제로 확인한 뒤 별도 실행을 허용하는 수명주기 설계와 retained Job의 upgrade 검증이 다음 작업이다. 소스 커밋 검증 기준 불일치, amd64 통합 이미지 및 운영 검증도 여전히 미완료다.

## 수동 Helm test로 실행 분리

배포 커밋 `0bd8dd80`은 비교 Job을 일반 release 리소스에서 `helm.sh/hook: test`로 바꿨다. `normalizedShadow.run=true`는 이제 수동 test 등록만 한다. 설치/업그레이드가 비교 작업을 자동 실행하지 않으며 일반 리소스의 immutable Job template 갱신 대상에서도 빠진다. 자동 성공/실패 삭제나 TTL은 없다. 단, 직접 Helm test를 재실행하면 before-hook-creation 정책으로 같은 이름의 이전 Job이 교체될 수 있다.

이를 방지하는 `muhan-mud/scripts/run-normalized-shadow.mjs` 실행기를 추가했다. 명시적인 context/namespace/release/job/source/digest를 받고, 기본은 원격 조회 preflight만 수행하며 `--execute`일 때 선택한 test 하나만 실행한다. Helm release JSON의 deployed 상태·identity·설정, 이번 last_deployed 이후 완료된 migration 성공 기록 및 로컬 normalized SQL checksum, 미실행 test를 확인한다. 기존 Job이 있으면 거부하고 실행 직전 release revision을 재검사한다. 원시 Helm 출력/오류는 노출하지 않는다. 이 실행기를 실제 클러스터에 호출하지는 않았다.

테스트부터 작성해 기존 hook 부재와 새 모듈 부재로 실패한 뒤 구현했다. readiness/runner mock과 chart/prerequisite 검사 55개가 통과했다. 실제 Helm v3.16.4의 install/upgrade `--no-hooks` 렌더링에서 비교 Job이 일반 리소스 목록에 없는 것도 확인했다. 공식 Helm hook 문서와 v3.16.4 hook JSON 구조를 대조했으나 실제 release JSON 및 cluster test 실행 검증은 아직 없다. release 작업 직렬 실행이 필요하고 revision 재검사는 분산 잠금을 대체하지 않는다. 기존 일반 Job 버전에서 전환 시 결과 보관 필요사항도 운영 문서에 명시했다.

다음 검증은 실제 Helm 형식과 checksum 연결, CLI 프로세스 경계, 독립 리뷰 및 amd64 통합 이미지다. 소스 검토 기준 SHA 불일치는 여전히 미해결이며 전체 포팅·웹 게임 onboarding·DB 권위 전환·운영 배포가 완료된 것은 아니다. 두 저장소의 변경은 로컬 커밋이다.

## CLI 프로세스 검증 및 다음 실제 데이터 경로 확인

`normalized-shadow-cli-process.test.mjs`는 실제 로컬 Helm 렌더링의 migration/test manifest를 합성 release JSON에 넣고 실행기를 별도 Node 프로세스로 실행한다. 하위 Helm/kubectl은 임시 디렉터리의 대역만 PATH에 제공하며 사용자 환경/자격증명은 상속하지 않는다. preflight/execute, 기존 Job, revision 변경, 외부 명령 실패, 잘못된 JSON, test 실패, 필수 인자 누락의 8개 분기가 통과했다. 실제 CLI가 계산한 SQL checksum과 chart 출력이 연결됐고 정확한 context/namespace/test filter, 종료 코드·한 줄 출력, 원시 명령 출력 미노출을 검증했다. 임시 fixture는 finally로 정리했다. 실제 클러스터의 release JSON이나 실제 Helm test 실행 증거는 아니다.

전체 데이터 경로를 다시 확인한 결과, normalized DB 저장 구현은 소스 `player-snapshot-v1-artifact-cli.ts`의 별도 opt-in에 이미 있다. 하지만 배포 chart의 artifact relay Job은 `player-snapshot-v1-manifest-first-cli.js`를 실행하고, 이 entrypoint는 현재 manifest/artifact만 저장하며 normalized projector/store를 연결하지 않는다. 따라서 비교 Job만 등록해도 실제 게임 outbox의 normalized 행이 자동으로 생성되는 것은 아니다. 다음 핵심 작업은 이 manifest-first 저장 경로에 검증된 normalized persistence를 연결하고 실제 C receipt-bound outbox → Rust → PostgreSQL → 비교를 E2E로 검증하는 것이다. 별도 기존 artifact CLI와 운영 receipt-bound 입력 형식 호환성부터 확인해야 하며, 기존 게임 파일 권위는 그대로 유지한다.

## manifest-first 정규화 저장 연결

소스 커밋 `9a8dee4`에서 운영 manifest-first relay에 선택적 normalized persistence를 연결했다. 기존 receipt-bound parser/paired filesystem을 그대로 사용해 manifest와 artifact RPC가 RECORDED 또는 EXACT_RETRY로 확정된 뒤에만 Rust projector와 normalized store를 호출한다. 기존 numeric allowlist 재구성 함수를 공유해 projector의 추가 필드는 DB 입력으로 전달하지 않는다. normalized 단계 실패 시 generic error counter와 미전달로 집계하며 앞 단계의 저장 증거는 유지한다. 다음 실행은 선행 exact retry 후 마지막 저장을 다시 시도한다. 전체 delivered/recorded/exactRetry는 마지막 활성 단계가 성공한 쌍만 계산하고 각 단계별 counter는 따로 남긴다.

manifest-first CLI는 `M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ENABLED=true`와 절대 경로 `M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_RUNNER`가 있을 때만 이를 연결한다. writer URL 검증과 기존 전용 store를 재사용한다. 잘못된 runner 설정은 base store 생성 전 거부하며 실패 시 두 store 모두 닫는다. 기본 off의 호출과 결과 구조는 유지한다. chart는 아직 이 설정을 제공하지 않으므로 자동 활성화되지 않는다.

선행 실패/부분 전달·exact retry/필드 allowlist/CLI opt-in 및 정리의 테스트 3개를 먼저 실패시킨 후 구현했다. 최초 retry fixture는 기존 분류기에 없는 SQLSTATE 40001을 사용했으므로 기존 정책을 바꾸지 않고 연결 중단 ECONNRESET fixture로 수정했다. 타입 검사·빌드 통과, relay 단위 34 pass/4 기존 skip, 전체 C→Rust→Node bridge 163 tests 중 158 pass/0 fail/5 기존 skip이다. 새 manifest-first normalized 조합을 실제 PostgreSQL에 저장하고 조회하는 E2E는 아직 없다. 다음은 그 E2E와 chart opt-in 연결이며, 정규화 저장이 게임 DB 권위 전환 완료를 의미하지 않는다.

## 실제 manifest-first 신규 저장 E2E 통과

소스 커밋 `34e0449`에 신규 E2E 실행기를 추가했다. 기존 seed에 psql `receipt_only` 변수의 존재로 선택하는 별도 경로를 추가해 identity/head/receipt까지만 commit한다. 이 모드에서는 manifest/artifact/normalized 행을 만들지 않는다. 기존 기본 seed 경로는 유지한다. 신규 실행기는 세 출력 테이블의 행 부재를 먼저 확인한 뒤 합성 tree CDTO fixture와 receipt metadata로 0700/0600 파일 쌍을 만든다. 실제 manifest-first CLI가 신규 3단계 저장과 exact retry를 수행하고, 기존 reader integration을 호출해 SQL/pool 조회 및 CLI MATCH/INVALID_INPUT/MISSING_RECORD와 입력 파일 불변을 확인한다.

새 로컬 이미지 `muhan-normalized-manifest-first:local`을 빌드했다(manifest list `sha256:2018009f5ebe845e97bcce04c400a65b9401e56be582b94e7a4ec6b3323c39ea`). PostgreSQL 17 임시 인스턴스에 bootstrap 및 20260902~20261014 migration을 적용했다. 앞선 PG lane과 같이 Supabase Realtime을 필요로 하는 20260901은 제외했다. seed는 `-v receipt_only=1 -v pvi_tree_payload=NULL`로 실행했으며 payload 변수는 이 경로에서 사용되지 않는다.

read-only root filesystem, tmpfs /tmp, 이미지의 기본 node UID, 테스트 파일 read-only mount, DB 컨테이너 network namespace에서 실제 실행이 통과했다: `Normalized manifest-first integration passed: new records, exact retry, reader comparison, unchanged evidence`. DB는 외부 포트를 게시하지 않은 disposable trust 인증 환경이므로 비밀번호 인증을 새로 검증한 결과는 아니다. reader/writer는 각각 전용 DB login을 사용했다. CDTO와 source post metadata는 합성 fixture이며 살아 있는 C 게임 프로세스의 save/power-loss/PVC 증적은 아니다.

careful 절차에 따라 `muhan-normalized-write-db-92f1`의 ID/label/auto-remove를 확인하고 종료했다. DB와 runner 컨테이너 모두 목록 부재를 확인했으며 합성 데이터는 제거됐다. 로컬 이미지는 후속 검증용으로 보존했다. 운영 DB·k8s·registry push는 없었다. 다음은 chart opt-in 연결과 독립 리뷰, 검토된 source SHA/amd64 통합 이미지 준비다.

## 정규화 저장 chart opt-in 연결

배포 커밋 `c1b08a4e`에 `playerSnapshotV1ArtifactRelay.normalizedProjection.enabled=false` 기본값을 추가했다. 활성화하려면 artifact relay, normalizedShadow 스키마 준비, migrations가 함께 켜져 있어야 하며 기존 M3 shadow/paired outbox 조건도 유지한다. 이 옵션을 켠 artifact relay만 test-only hook으로 바꾸고 고정된 Rust projector 경로와 literal true persistence env를 manifest-first CLI에 전달한다. 옵션이 꺼져 있으면 기존 artifact relay 동작은 그대로다.

준비 검사기/실행기에 `--persist` 모드를 추가했다. 비교 등록 여부와 별개로 실제 writer capability와 artifact relay test 경로를 검사하고, 기존 migration 성공·checksum·source/image·기존 Job 거부·revision 재검사를 그대로 수행한다. 저장 모드는 기본 Job 300초를 기다릴 수 있도록 Helm timeout 6분/자식 프로세스 370초, 비교 모드는 기존 3분/190초다. 실행기는 두 Job을 한꺼번에 호출하지 않는다.

필수 조건 및 writer readiness 테스트 실패 후 구현했고 chart/prerequisite/readiness/CLI/기존 render 58개가 통과했다. 이후 실제 CLI 프로세스 대역 테스트에 실제 writer chart manifest와 --persist 전달을 추가해 9개 시나리오가 통과했다. 실제 cluster 호출이나 새 배포는 없다. 운영 가이드에 저장 → 비교 순서와 실패 시 앞 단계 commit 가능성을 기록했다. source SHA 검증 기준 불일치와 amd64 통합 이미지, 독립 검토가 다음 선행 작업이다.

## 고정 소스의 로컬 amd64 통합 빌드 시도

원격 read-only `git ls-remote private refs/heads/codex/mud-identity-foundation`에서 여전히 `c1b016e811879f6c1515cec2092b1daf5e0c8cd7`을 확인했다. 최신 로컬 기능은 원격 브랜치에 없다. 기존 source-path 테스트의 고정 SHA/HEAD 불일치는 그대로 유지했고, 검토 없이 상수나 검증을 제거하지 않았다.

별도 로컬 검증으로 고정 소스 `03c677fa660c3e2c21214aa2728ebbe21223dfa9`를 git archive의 `src/` prefix로 `/tmp/muhan-amd64-source.DRusdR`에 추출했다. 임시 경로에는 해당 archive와 추출본만 있으며 작업 폴더 변경을 복사하지 않았다. Docker의 named build context로 `source` stage를 대체해 나머지 기존 통합 Dockerfile을 linux/amd64로 빌드하려 했다. 이는 private Git fetch/BuildKit credential 검증을 생략한 로컬 패키징 검증이며 운영 이미지 provenance 승인과 구분한다. [Docker build context 동작](https://docs.docker.com/reference/cli/docker/buildx/build/).

`docker buildx build --builder desktop-linux --load --platform linux/amd64 --build-context source=/tmp/muhan-amd64-source.DRusdR --build-arg SOURCE_REVISION=03c677fa660c3e2c21214aa2728ebbe21223dfa9 -t muhan-integrated-amd64:local -f muhan-mud/Dockerfile muhan-mud`는 Docker frontend `docker/dockerfile:1.7` metadata 취득의 DeadlineExceeded로 종료했다. 소스 컴파일까지 진행하지 않았으므로 통합 이미지 통과/실패로 해석하지 않는다.

대안으로 같은 frontend 이미지의 `docker pull docker/dockerfile:1.7`을 실행했고 현재 exec session `64431`이 실행 중이며 마지막 30초 wait에서도 종료되지 않았다. 새 pull을 시작하지 말고 이 세션을 먼저 재확인한다. 이전 build session `9481`은 exit 1로 끝났다. 임시 소스 폴더는 재시도를 위해 보존했고 다른 이미지/캐시는 삭제하지 않았다. push/배포는 없다.

## 내장 frontend 대안도 registry metadata에서 중단

다음 턴에 pull session `64431`을 재확인했고 여전히 실행 중이었다. 중복 pull은 시작하지 않았다. 운영 Dockerfile을 수정하지 않고 입력의 첫 syntax 지시문만 제외해 내장 frontend로 같은 amd64 검증을 시도했다. exec session `65830`은 named source context 로딩까지 진행했지만 `gcc:14` metadata 취득에서 DeadlineExceeded로 종료했고 node/rust metadata 요청은 함께 취소됐다. 따라서 외부 frontend 이미지만의 문제로 좁힐 수 없고, 아직 C/Rust/Node 컴파일 증거는 없다.

호스트의 bounded HTTPS 확인은 Docker registry `/v2/`에서 0.6초 내 HTTP 401 응답을 받았다(익명 요청에 대한 인증 요구 응답). 이것은 호스트 경로 도달성 증거일 뿐 Docker 내부 DNS/proxy/credential 경로의 정상 증거는 아니다. 원인을 단정하거나 Docker 설정·로그인·daemon을 변경하지 않았다. 마지막 pull 재조회도 session `64431` 실행 중/새 출력 없음이었다. 두 build 시도는 모두 terminal failure이므로 자동으로 새 build를 반복하지 않는다. 임시 소스는 여전히 `/tmp/muhan-amd64-source.DRusdR`에 보존되어 있다.

## 임시 익명 설정에서 registry 정체 우회 확인

목표 active를 확인하고 investigate 절차로 읽기 전용 진단했다. Docker 엔진은 29.6.1/linux/aarch64로 즉시 응답한다. 기존 pull session `64431`은 여전히 출력 없이 실행 중이며, 해당 docker PID 69017의 자식 docker-credential-desktop PID 69058은 10분 이상 생존했다. 1초 stack sampling에서는 대기 스레드가 보였지만 심볼이 충분하지 않아 Keychain이나 내부 원인을 확정하지 않았다. 다른 buildx 자식 credential helper도 장시간 남아 있었고 임의 종료하지 않았다.

별도 빈 임시 설정 `/tmp/muhan-registry-check.UIqvJ7`으로 공개 `gcc:14` manifest만 조회한 session `91482`는 exit 0, schemaVersion 2, manifest 12개, amd64 존재를 반환했다. 원래 Docker 설정/로그인/daemon은 변경하지 않았다. 임시 설정에는 기존 buildx 플러그인을 찾기 위한 cliPluginsExtraDirs만 추가했다. 인증 정보는 복사하지 않았다.

동일 고정 소스와 Dockerfile을 이 임시 설정 및 명시적인 로컬 socket으로 빌드한 exec session **24794**가 진행 중이다. default docker driver를 사용하며 --load만 지정했다. 이번에는 dockerfile frontend와 gcc/node/rust metadata가 모두 성공하고 실제 base image layer 다운로드로 진행했다. 따라서 기존 인증 도우미/CLI 설정 경로의 관여를 강하게 시사하나 builder 선택도 달라 엄밀한 단일 변수 실험이나 영구 원인 수정으로 보고하지 않는다. 아직 컴파일/이미지 완성 증거는 없다. 다음 턴은 24794를 먼저 이어받고 중복 빌드를 시작하지 않는다. 기존 64431도 미종료 상태로 별도 추적한다. Git/registry push 및 k8s 변경은 없다.

## 통합 amd64 이미지 빌드 및 projector 실행 통과

24794는 실제 Rust 컴파일 단계에서 exit 1로 종료했다. 배포 Dockerfile이 muhan-core-dto 디렉터리만 복사해 상위 rust/Cargo.toml 및 Cargo.lock을 누락했고, --locked가 없는 lockfile 생성을 거부한 것이 원인이다. 기존 단독 relay Dockerfile은 이미 전체 Rust workspace를 사용하고 있었다. investigate 절차로 원인을 확인한 뒤 --locked를 유지하고 전체 workspace를 복사하며 -p muhan-core-dto로 두 binary만 빌드하도록 수정했다. runtime COPY도 workspace target 경로로 맞췄다.

배포 커밋 `3cf3503d`: 신규 regression test가 기존 Dockerfile에서 실패한 뒤 수정 후 prerequisite/chart/readiness/CLI 검사 **59 pass, 0 fail**을 확인했다. 기존 source-path 검사의 필수 복사 경로만 /src/rust/로 맞췄고 reviewed SHA 및 HEAD 일치 gate는 제거하거나 갱신하지 않았다. 따라서 별도의 reviewed-source gate 불일치는 여전히 남는다.

동일 고정 source `03c677fa660c3e2c21214aa2728ebbe21223dfa9`와 임시 익명 설정으로 재빌드한 session **99662**는 exit 0이다. `muhan-integrated-amd64:local` 이미지 ID/manifest list는 `sha256:4b8d40d2e37edd046958ac6082969ff62fcefc89059f689af085d78f2c040d06`, architecture amd64, 기본 사용자 muhan:muhan이다. C 게임/인증, Rust 두 binary, Node 서비스 및 Next 웹 빌드를 모두 지나 최종 이미지가 생성됐다. 단, C command4.c:253의 alstr[16]에 한국어 문자열 복사 시 overflow warning 2개가 관찰되어 후속 수정 대상으로 남겼다. 아직 코드 수정이나 무경고 검증 증거는 없다.

최종 이미지를 --rm, --read-only, --network none으로 실행해 C 게임 및 normalized projector의 ldd 의존성이 해소됨을 확인했다. 같은 제한과 기본 사용자에서 실제 amd64 projector에 tree fixture를 stdin으로 전달했다. 정상 digest는 exit 0/빈 stderr/5개 item 및 canonical digest `96df4bf87d1012fbef2043f215b95b6bf0790780b731546b1fcd6a257ee2b76c`, 잘못된 digest는 exit 1/빈 stdout/고정 거부 문구를 검증했다. 단발 컨테이너는 auto-remove됐다.

이 결과는 고정 로컬 archive를 source stage 대신 사용한 패키징·projector smoke 증거다. private remote fetch, DB 연결, 실제 게임 가입/계정 연동, k8s 배포 또는 DB 권위 전환 완료의 증거가 아니다. 원격 게시/검토 기준 SHA 정리, 통합 이미지의 DB E2E, 실제 C save 및 onboarding 검증이 남는다. 기존 pull 64431은 이전 확인에서 살아 있었으며 종료 확인 전 재실행하지 않는다.
