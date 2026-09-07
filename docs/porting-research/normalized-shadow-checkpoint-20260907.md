# Normalized shadow comparator 검증 체크포인트

2026-09-07, 기반 커밋 `7699993`.

## 이번 수정과 증거

- 숫자 범위 검사만으로는 normalized projection의 런타임 타입을 보장하지 못했다. `level: 42n`을 넣는 회귀 테스트가 digest 직렬화에서 `TypeError`로 실패하는 것을 먼저 확인했다.
- signed i64는 `bigint`, 좁은 정수는 `number`라는 기존 Rust-backed Node projector 계약을 비교기에서도 검사한다. 필드 타입이 뒤바뀐 derivation은 조회 전에 `PROJECTION_DERIVATION_FAILED`, 저장 행은 `INVALID_RECORD`로 분류한다.
- scalar, daily, timer, item의 숫자 필드마다 타입을 바꿔 양쪽 입력을 검사한다. receipt의 canonical name 또는 storage format 불일치도 실제 receipt-bound parser가 거부하는 테스트를 추가했다.
- 패키지 typecheck 통과. C oracle → Rust CLI → Node bridge 실행 결과: 145 tests, 140 pass, 0 fail, 5 기존 조건부 skip. 이 결과는 실제 PostgreSQL 또는 배포 환경 검증이 아니다.

## 남은 목표

현재 comparator는 주입된 조회 결과만 비교한다. 독립 리뷰, 실제 normalized projection 조회 adapter, PostgreSQL 계약 실행, 저장·재시작·웹 가입/기존 계정 연결의 배포 환경 검증이 남아 있다. legacy 파일은 여전히 gameplay 저장 권위이며 이번 변경은 DB 권위 전환을 수행하지 않는다.

다음 조회 adapter는 receipt/artifact/projection을 `(character_id, command_id)`로 결합하고 world를 함께 확인해야 한다. normalized root와 순서가 고정된 daily/timer/item 행을 읽고 i64를 손실 없이 복원해야 한다. 전용 조회 역할 계약은 실제 migration 및 PostgreSQL 검증이 필요하다. 현재 `findByCommandId` 인터페이스를 실제 SQL 연결 전에 복합 identity 입력으로 보강할지 검토한다.

서브 에이전트는 Astra/medium 선호를 유지한다. 직전 Orca worker는 모델 호환성 오류로 리뷰를 수행하지 못했으므로 독립 리뷰 완료로 계산하지 않는다.
