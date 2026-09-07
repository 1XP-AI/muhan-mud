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
