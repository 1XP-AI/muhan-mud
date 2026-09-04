# M3 PlayerSnapshotV1 handoff

## 목적

레거시 C MUD의 저장 파일과 게임 런타임을 계속 권위로 유지한 채,
`PlayerSnapshotV1`을 PostgreSQL에 불변 증거로 기록하는 M3 shadow 경계를
검증한다. 이 문서의 작업은 DB에서 게임 상태를 읽거나 게임 플레이를
전환하지 않는다.

## 현재 기준점

- 브랜치: `codex/mud-identity-foundation`
- 기준 커밋: `1d80e72` (`private` 원격 저장소에 푸시됨)
- `PlayerSnapshotV1`은 40개 필드의 pointer-free CDTO와 bounded inventory
  graph를 사용한다.
- PostgreSQL 17 계약은 canonical payload, receipt tuple, immutable replay,
  `RECORDED`/`EXACT_RETRY`, 그리고 reconciliation 상태를 검증한다.
- M3 native runtime은 opt-in handoff를 idle turn에서 초당 최대 한 건씩
  drain한다. 저장·publish·ACK 경로는 이 drain을 기다리지 않는다.

## 이번 ABI 경계

다음 변경은 아직 커밋 전이다.

- `player_snapshot_v1_native_abi_supported()`가 durable handoff가 요구하는
  ABI를 명시한다: 8-bit char, 16-bit short, signed i64 전체를 담는 64-bit
  long, PLAYER wire value 0.
- `MUD_M3_PLAYER_SNAPSHOT_V1=handoff`여도 이 조건이 성립하지 않으면
  observer/capture를 만들지 않고 조용히 비활성으로 남는다.
- 코덱 자체는 portable test 대상으로 남는다. 즉 Windows의 snapshot
  codec 검증은 유지하고, production native handoff만 ABI에 따라 열린다.

## 확인한 증거

- `make -C src player-snapshot-v1-test`
- `make -C src player-snapshot-v1-sanitizer-test`
- `make -C src character-save-journal-v2-runtime-static-test`
- `PLAYER_SNAPSHOT_V1_ARTIFACT_ALLOW_DISPOSABLE=1 supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh`

위 PostgreSQL 17 계약은 migration 140에서 RED, 150 적용 후 GREEN을
확인한다. native runtime 실행 테스트는 Linux/libpq 대상이며, macOS에서는
의도적으로 skip된다. CI의 Linux native test는 `PROBE_ONLY` 없이 운영
source를 test seam으로 포함하여 opt-in 및 ABI mismatch 경로를 실행한다.

## 다음 수직 단계

실제 레거시 save call-site 하나를 명시적으로 선택해, 단일
character/save에 한해 다음을 feature-off 기본값으로 연결한다.

1. 기존 legacy save가 PREPARED와 receipt를 완성한다.
2. 성공/실패 결과와 gameplay authority는 그대로 legacy 경로가 결정한다.
3. native shadow owner의 bounded idle drain이 동일 source/receipt tuple로
   immutable PlayerSnapshotV1 artifact를 기록한다.
4. DB readback, bank, reconnect, ownership/lifecycle 전환은 하지 않는다.

필요한 테스트는 성공, DB offline deferred retry, exact replay, receipt 또는
epoch mismatch, source mutation, crash/restart cleanup, 그리고 legacy
file/player bytes가 변하지 않았음을 포함해야 한다. rollback은 opt-in과
idle invocation을 끄는 것뿐이며, 기존 legacy 저장 경로와 이미 기록된
불변 증거는 삭제하지 않는다.

## 작업 원칙

- 기본 MUD build 또는 gameplay authority를 건드리지 않는다.
- `private` 원격 저장소만 사용하고 `origin`에는 푸시하지 않는다.
- 저장 경계 변경마다 deterministic C/Rust/PG17 증거를 추가한다.
- 실제 배포는 이 단일-save shadow 단계와 reconciliation 기준을 통과한 뒤
  별도로 판단한다.
