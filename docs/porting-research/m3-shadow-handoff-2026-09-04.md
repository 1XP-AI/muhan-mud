# M3 PlayerSnapshotV1 handoff

## 목적

레거시 C MUD의 저장 파일과 게임 런타임을 계속 권위로 유지한 채,
`PlayerSnapshotV1`을 PostgreSQL에 불변 증거로 기록하는 M3 shadow 경계를
검증한다. 이 문서의 작업은 DB에서 게임 상태를 읽거나 게임 플레이를
전환하지 않는다.

## 현재 기준점

- 브랜치: `codex/mud-identity-foundation`
- 기준 커밋: `8fffce1` (`private` 원격 저장소에 푸시됨)
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
- `make -C src character-player-snapshot-v1-capture-native-test`
- `make -C src character-player-snapshot-v1-handoff-test`
- `make -C src character-player-snapshot-v1-handoff-sanitizer-test`
- `bash -n supabase/tests/m3_runtime_shadow_pg17_integration.sh`
- `PLAYER_SNAPSHOT_V1_ARTIFACT_ALLOW_DISPOSABLE=1 supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh`

위 PostgreSQL 17 계약은 migration 140에서 RED, 150 적용 후 GREEN을
확인한다. 새 runtime-shadow handoff E2E는 Linux/libpq/Docker 대상이라
macOS에서는 의도적으로 실행하지 않으며, private branch CI가 full GREEN
환경이다. CI의 Linux native test는 `PROBE_ONLY` 없이 운영 source를 test
seam으로 포함하여 opt-in 및 ABI mismatch 경로를 실행한다.

## 완료한 단일-save 통합 증거

`8fffce1`은 Linux 전용 disposable PostgreSQL 17 runtime-shadow harness에
다음 흐름을 추가했다.

1. 기본 OFF `fault`/`recover` run은 PlayerSnapshotV1 outbox/handoff
   directory를 만들지 않는다.
2. `MUD_M3_PLAYER_SNAPSHOT_V1=handoff` opt-in run은 production
   `save_ply`를 한 번 호출하고, real `files1.c` player decoder를 사용해
   bounded native tick 한 번으로 immutable artifact 한 건을 기록한다.
3. harness는 filename만 보지 않고 production artifact-load와
   PlayerSnapshotV1 clone decode로 command, character, request hash,
   source hash, writer tuple, snapshot bytes를 PREPARED evidence와 대조한다.
4. 두 번째 tick은 artifact를 추가로 만들지 않으며, legacy player bytes와
   PostgreSQL receipt/head revision은 저장 결과 그대로 남는다.

레거시 파일과 M3 receipt가 계속 권위이고 artifact는 local immutable
evidence다. 이 slice는 artifact upload, DB readback, bank, reconnect,
ownership/lifecycle 전환을 하지 않는다.

## 다음 수직 단계

Linux CI에서 위 disposable PostgreSQL 17 harness의 full GREEN을 확인한 뒤,
별도 M4 relay 경계에서 이 immutable artifact를 PostgreSQL validator로
전달하는 단일-record 계약을 추가한다. relay는 snapshot을 gameplay read
source로 승격하지 않으며, upload 실패는 artifact를 남긴 채 재시도 가능한
diagnostic 상태로 끝나야 한다.

## 작업 원칙

- 기본 MUD build 또는 gameplay authority를 건드리지 않는다.
- `private` 원격 저장소만 사용하고 `origin`에는 푸시하지 않는다.
- 저장 경계 변경마다 deterministic C/Rust/PG17 증거를 추가한다.
- 실제 배포는 이 단일-save shadow 단계와 reconciliation 기준을 통과한 뒤
  별도로 판단한다.
