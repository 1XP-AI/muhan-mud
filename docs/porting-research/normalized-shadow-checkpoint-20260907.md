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
