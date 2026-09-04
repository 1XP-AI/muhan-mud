# Muhan MUD 포팅 핸드오프

- 최종 갱신: 2026-09-04 KST
- 브랜치: `codex/mud-identity-foundation`
- 현재 코드 기준 커밋: `d2910bd3055db480acde60f1fa42664ded5b27be`
- M4 WIP 기준 커밋: `68100aa685ed3ee2626aadcc6f788c9f0a860e0e`

## 먼저 알아야 할 상태

이 브랜치는 레거시 C MUD의 파일 영속 상태를 Supabase/PostgreSQL로 단계적으로
이전하고, Rust가 검증 가능한 canonical CDTO 경계만 사용하도록 포팅하는 작업이다.
M1부터 M3 기반과 `PlayerSnapshotV1` C/Rust 경계는 private CI를 통과했다. M4
코드는 다른 에이전트가 이어서 수정할 수 있도록 WIP 기준점으로 커밋되어 있다.

macOS의 case-insensitive filesystem에서 일반 clone을 막던
`objmon/Celduin_sign`/`objmon/celduin_sign` 충돌은 `9f7bf24`에서 해결됐다.
새 clone은 충돌 경고 없이 clean checkout되고, 서로 다른 두 역사적 blob도 보존된다.

**현재 M4 코드는 배포 가능한 완료본이 아니다.** `372b5e8`과 `d2910bd`에서
non-blocking PlayerSnapshot durable handoff, Linux PG17 링크 경계, relay의
non-Linux fail-closed 경계를 해결했고 private CI도 통과했다. 하지만 durable
consumer를 실제 게임 유휴 루프에서 호출하는 TDD 작업과 PostgreSQL의 full
PlayerSnapshot validation이 아직 남아 있으므로 live runtime 연결이나 testnet 배포를
승인하지 않는다.

이 작업은 보안 침해나 사이버 시큐리티 작업이 아니다. MUD의 계정, 캐릭터 저장,
DB 이관, 웹 xterm 연결을 안전하게 리팩터링하는 일반 소프트웨어 개발 작업이다.

## 저장소와 원격

- Private repository: `https://github.com/1XP-Inc/muhan-mud`
- Branch: `codex/mud-identity-foundation`
- 작업 디렉터리: repository root
- push 대상: remote `private`만 사용한다.
- remote `origin`은 `1XP-AI/muhan-mud`이다. 이 작업을 `origin`에 push하지 않는다.
- 커밋 메시지와 PR 설명은 한국어로 작성한다.
- 로컬의 `src/frp.new`는 사용자 소유 dirty file이다. 열기, 수정, stage, commit,
  revert하지 않는다. 원격 WIP 커밋에는 포함되지 않았다.

새 디렉터리에 받을 때는 이제 sparse checkout 없이 일반 clone을 사용해도 된다.

```sh
git clone --branch codex/mud-identity-foundation --single-branch \
  https://github.com/1XP-Inc/muhan-mud.git muhan-mud.nosync
```

다른 checkout에서 시작할 때:

```sh
git fetch private codex/mud-identity-foundation
git switch --create codex/mud-identity-foundation \
  --track private/codex/mud-identity-foundation
```

이미 같은 이름의 로컬 브랜치가 있으면 새 브랜치를 만들지 말고 해당 브랜치가
올바른 private remote를 추적하는지 먼저 확인한다.

## 장기 목표

레거시 C MUD의 영속 상태를 Supabase/Postgres 권위 모델로 단계적으로 이전하고
Rust 포팅 경계를 구축한다. TDD, 결정론적 differential test, dual-write, shadow
검증을 통과한 기능만 전환한다. 첫 사용자 결과는 다음 두 흐름이다.

1. Supabase 로그인 후 xterm에서 기존 텔넷 가입 절차로 새 캐릭터를 만든다.
2. 기존 MUD 이름과 게임 비밀번호를 xterm에서 한 번 검증해 Supabase 계정과 연결한다.

웹 로그인 계정과 게임 캐릭터는 별도 identity다. Auth 로그인만으로 기존 MUD
캐릭터가 자동 연결되지 않는다. 명시적인 claim/link 절차가 필요하다.

## macOS casefold 리소스 리팩터링 `9f7bf24`

- `objmon/Celduin_sign`은 원본 blob
  `49b3a1975def2762e68f2663351ee55ffb387e61`로 복구했다.
- 기존 소문자 리소스의 원본 blob
  `4eb75435475df4df6d4cb20050754c1ab09baefe`는 casefold-safe 물리 경로
  `objmon/celduin_sign__4eb75435`로 이동했다.
- 레거시 논리 경로 `objmon/celduin_sign`은
  `tools/revive/path-relocations.v1.tsv`와 기존 manifest에 보존된다. 이 raw 소문자
  물리 경로를 다시 만들면 macOS clone 충돌이 재발하므로 만들지 않는다.
- `tools/revive/extract-legacy-blobs.sh`는 relocation source path, legacy path, canonical
  path와 Git blob을 함께 검증한다. 누락 relocation이나 중복 legacy path는 fail한다.
- C/Rust 경로 해석기는 이름이 실제로 바뀐 alias만 canonical-first로 연다. 동일 경로
  alias는 mutable raw file 우선순위를 유지한다. renamed canonical target이 없으면 다른
  대소문자 raw file을 잘못 여는 대신 `ENOENT`로 fail-closed한다.
- `tests/unit/resource_tree_manifest_test.py`는 전체 Git index의 NFD+casefold 유일성,
  두 manifest의 일치, canonical blob과 최소 재생성 계약을 검사한다. `src/Makefile`의
  `unit-test`에 연결돼 Ubuntu CI에서도 실행된다.

## 고정된 설계 결정

- Big-bang rewrite를 하지 않는다.
- 검증된 slice만 `legacy file → DB shadow → durable dual-write → DB authority` 순으로
  승격한다.
- C 프로세스가 레거시 ABI와 raw file의 oracle이다. Rust는 native struct나 raw file을
  직접 읽지 않고 C exporter가 만든 canonical CDTO만 decode한다.
- 권위 전환 gate가 끝날 때까지 legacy player file이 authority다.
- 신규 가입과 기존 캐릭터 claim 입력은 xterm의 C wizard 흐름을 유지한다. 웹 폼에
  게임 validation을 복제하지 않는다.
- journal, receipt, snapshot artifact, outbox manifest는 immutable evidence다. 자동
  삭제나 수정으로 수렴시키지 않는다.
- 새 런타임 경로는 exact opt-in feature flag이며 기본값은 OFF다.
- `replicas: 1`과 Kubernetes `Recreate`는 writer fence가 아니다. PVC process lock,
  DB epoch, journal/reconciliation gate가 별도로 필요하다.

## WIP 커밋 `68100aa`에 포함된 범위

### 1. M3 observer 연결

- `src/character_save_journal_v2_protocol.*`
- `src/character_save_journal_v2_recovery.*`
- `src/character_save_journal_v2_player_store.*`
- `src/character_save_journal_v2_process_owner.*`

durable `PREPARED` 뒤 observer callback과 진단 report를 추가했다. startup recovery와
live player store가 같은 observer를 전달한다. observer 실패가 legacy authority
결과를 바꾸지 않는 테스트가 있다.

### 2. PlayerSnapshotV1 artifact와 native capture

- `src/character_player_snapshot_v1_artifact.*`
- `src/character_player_snapshot_v1_capture.*`
- `src/character_player_snapshot_v1_capture_native.*`
- 관련 `tests/unit/character_player_snapshot_v1_*_test.c`

artifact store는 실제 `PlayerSnapshotV1` decoder와 re-encoder를 통해 exact canonical
bytes를 검증한다. capture는 held writer, PREPARED, stage owner/mode/link/inode/hash를
다시 확인한다. native bridge는 bounded `read_crt_player`를 사용하며 게임 비밀번호를
snapshot payload에 포함하지 않는다.

### 3. PostgreSQL artifact 계약

- `supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql`
- `supabase/tests/player_snapshot_v1_artifact_contract.sql`
- `supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh`

immutable artifact table, receipt anchor, record/reconciliation RPC와 role 경계가 있다.
현재 SQL validation은 아래 P1 때문에 완료된 계약이 아니다.

### 4. M4 manifest relay

- `services/m4-file-snapshot-manifest-relay/`

strict 13-line manifest를 lexical order로 읽고 direct PostgreSQL RPC를 호출하는 one-shot
Node service다. player payload를 읽지 않고 outbox evidence를 삭제·수정하지 않는다.

## 남은 선행 작업

### P1: PostgreSQL이 임의 kind-7 CDTO를 영구 저장할 수 있다

`supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql`은 CDTO의
magic/version/kind/length/digest만 검사한다. 현재 contract가 사용하는 48-byte 빈
kind-7 envelope도 `RECORDED`되지만 C artifact store는 이를 거부한다. 잘못된 immutable
row가 먼저 삽입되면 같은 character/command의 올바른 payload가 conflict로 막힌다.

필수 TDD 순서:

1. empty kind-7과 field count/type/length 변형을 거부하는 PG RED를 추가한다.
2. DB validation을 C `PlayerSnapshotV1` schema와 동등하게 만들거나, DB가 전체 구조를
   검증할 수 없다면 검증된 native relay만 RPC를 실행하도록 role과 deployment
   topology를 더 좁게 강제한다.
3. 실제 C fixture를 record한 뒤 C와 Rust가 exact-byte로 다시 읽는 integration을 만든다.

### P1: durable handoff consumer의 실제 유휴 lifecycle 연결

`MUD_M3_PLAYER_SNAPSHOT_V1=handoff`는 default OFF인 명시적 opt-in이다. enabled일 때
startup recovery와 live save는 immutable private handoff를 남기지만,
`character_save_journal_v2_runtime_native_snapshot_tick()`을 실제 게임 프로세스가 아직
호출하지 않으므로 artifact consumer는 자동으로 drain되지 않는다. legacy save/publish/ACK
authority와 결과는 바꾸지 않지만, 이 상태를 실사용 기능으로 선언하거나 배포하면 안 된다.

필수 TDD 순서:

1. `USE_M3_RUNTIME` guarded main-to-`sock_loop()` idle hook을 만들고, 한 게임 turn의
   `output_buf`·command 처리·`update_game` 뒤에만 호출한다.
2. 1초마다 최대 1 token만 소비하고 OFF/BUSY/NOT_READY는 no-op, handoff 오류는
   rate-limited diagnostic으로 처리한다.
3. default OFF, synchronous save/publish/ACK/startup/shutdown 경로에서 tick이 호출되지
   않는 테스트와 idle ordering·busy suppression·retry 테스트를 먼저 만든다.
4. SIGKILL은 final drain 대상이 아니라 existing durable recovery boundary로 유지한다.

### 해결된 scoped 항목 (다시 열지 말 것)

- **P1b–P1e durable handoff:** legacy stage의 hard-link 대신 command별 private source
  copy를 만들고, hash-verified rename-only promotion을 사용한다. consumer-visible source는
  항상 `nlink==1`이며 source/source.tmp 2-link 또는 불명 leaf는 evidence를 보존한 poison
  경로로 처리한다. `MAX_PENDING=128` identity(최대 8 GiB payload ceiling)에는 poison도
  포함되고, unsafe A가 `limit=1`에서 뒤의 valid B를 막지 않는다.
- **publish independence:** stage가 publish 뒤 사라진 partial source는 game save가 아니라
  shadow snapshot만 명시적으로 drop한다. publish/ACK는 capture consumer를 기다리지 않는다.
- **P2 relay:** `de80115`에서 Linux descriptor-rooted scan을 사용하고 non-Linux는
  fail-closed로 바꿨다. root pathname fallback을 다시 추가하지 않는다.
- **Linux CI graph:** `d2910bd`에서 PG17 process-owner/runtime-shadow harness에만 required
  handoff/capture closure와 fail-closed fixture decoder를 더했다. default legacy Makefile
  graph에는 handoff/libpq가 추가되지 않았다.

## 병렬 위임 권장안

서로 다른 checkout/worktree에서 다음 세 묶음을 병렬화할 수 있다.

1. **Terra/고난도:** `sock_loop()` idle pump의 TDD 구현, opt-in/idle/Busy/shutdown lifecycle
   경계와 deterministic cadence 검증.
2. **Terra/고난도:** PostgreSQL full PlayerSnapshot validation과 C/Rust/PG fixture 연동.
3. **Luna/중간 난도:** Linux PG17 harness와 idle-pump diff의 독립 리뷰, default-off 및
   legacy link graph 회귀 검사.

각 에이전트는 자기 묶음만 수정하고, 커밋하지 않은 다른 에이전트 파일을 정리하거나
덮어쓰지 않는다. 결과 회수 후 실행 세션과 Orca terminal을 0개로 정리한다.

## 현재 검증 증거

### 리소스 경로 리팩터링 `9f7bf24`

2026-09-04 KST에 다음 로컬 검증이 통과했다.

```sh
python3 tests/unit/resource_tree_manifest_test.py
make -C src unit-test CC=cc
cargo test --workspace --manifest-path rust/Cargo.toml
```

- manifest 검사: Git index 7,877개 경로, alias 3,824개, NFD+casefold 충돌 0건.
- C-only와 `USE_RUST_RESOLVER` runtime alias 회귀 테스트 모두 통과.
- fresh macOS 일반 clone: 충돌 warning 0건, dirty path 0건, 두 canonical blob 일치.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33828127616>
  (`macOS`, `Windows`, `Ubuntu ARM`, `Ubuntu`, `Supabase ownership contract` 모두 GREEN)
- staged credential scan: HIGH/MEDIUM/LOW/WARN 모두 0건.

2026-09-04 KST, WIP 커밋 직전에 다음이 통과했다.

```sh
pnpm --filter @muhan/m4-file-snapshot-manifest-relay test
pnpm --filter @muhan/m4-file-snapshot-manifest-relay typecheck
pnpm --filter @muhan/m4-file-snapshot-manifest-relay build

make -C src \
  character-player-snapshot-v1-artifact-test \
  character-player-snapshot-v1-capture-test \
  character-player-snapshot-v1-capture-native-test \
  character-save-journal-v2-player-store-test \
  character-save-journal-v2-process-owner-test \
  character-save-journal-v2-protocol-test \
  character-save-journal-v2-recovery-test CC=cc

make -C src \
  character-player-snapshot-v1-artifact-sanitizer-test \
  character-player-snapshot-v1-capture-sanitizer-test \
  character-player-snapshot-v1-capture-native-sanitizer-test CC=cc

PLAYER_SNAPSHOT_V1_ARTIFACT_ALLOW_DISPOSABLE=1 \
  supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh
```

- relay: 5/5 pass, typecheck/build pass.
- C target과 ASan/UBSan target: pass.
- disposable PostgreSQL 17 migration replay: pass.
- staged diff credential scan: 0 findings.
- `files1.c` native capture build는 레거시 non-prototype warning 67개를 출력하지만 pass.
- 위 GREEN은 당시 WIP 기준 증거다. durable handoff/relay 수정의 최신 CI 증거는 아래 별도 항목을 따른다.

### Durable handoff P1e 및 Linux CI 복구 `372b5e8`, `d2910bd`

2026-09-04 KST에 다음 검증이 통과했다.

```sh
make -C src \
  character-player-snapshot-v1-handoff-static-test \
  character-player-snapshot-v1-handoff-test \
  character-player-snapshot-v1-handoff-sanitizer-test \
  character-player-snapshot-v1-capture-native-test \
  character-save-journal-v2-process-owner-test CC=cc
make -C src unit-test CC=cc
bash -n supabase/tests/m3_process_owner_pg17_integration.sh \
  supabase/tests/m3_runtime_shadow_pg17_integration.sh
```

- focused handoff/capture/process-owner tests, ASan/UBSan, 전체 C unit: pass.
- 독립 Luna review: source rename/poison 경계와 Linux PG17 closure에 P0/P1/P2 없음.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33849595161>
  (`Supabase ownership contract`, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN).
- Linux PG17 E2E는 process-owner/runtime-shadow, full M3 runtime link, artifact contract,
  named-volume restart까지 통과했다. macOS의 native lifecycle target은 Linux-only이므로
  local에서 skip되는 것이 정상이다.

아직 필요한 검증:

- idle pump P1의 RED/GREEN과 full Linux CI
- PostgreSQL full PlayerSnapshot schema validation P1
- live runtime opt-in, PVC, rollback 및 browser smoke

## Kubernetes 현황

- context: `testnet-1xp`
- release: `muhan-mud-testnet`
- URL: <https://muhan.1xp.vc>
- chart label: `muhan-mud-0.1.5`
- web, gateway, MUD, Auth, PostgREST, Realtime와 PostgreSQL은 2026-09-04 확인 시
  모두 Ready 1/1이었다.
- running app digest:
  `sha256:117baa939a909f5005a872a355f9e714a5c6cde692af16dff0652f97c7218c70`
- chart repo: sibling checkout `../tesnet-1xp.nosync/muhan-mud/charts`
- live image에는 `d2910bd` durable handoff/CI 복구나 `68100aa`의 M4 WIP가 포함되지 않았다.
- 현재 웹 로그인은 가능하지만 연결된 캐릭터가 없으면 게임 진입이 막힌다. 신규 가입과
  기존 캐릭터 claim/link 흐름은 아직 사용자에게 열지 않았다.

## 다음 완료 순서

1. P1 idle pump를 RED 테스트부터 구현한다. runtime flag는 exact
   `MUD_M3_PLAYER_SNAPSHOT_V1=handoff`만 허용하며 absent/OFF는 기존 startup을 유지한다.
2. PostgreSQL full PlayerSnapshot schema validation을 C/Rust fixture와 동등하게 만든다.
3. targeted/full unit, sanitizer, PG17, workspace test/typecheck/build와 fresh private CI를
   다시 실행한다.
4. `src/frp.new`를 제외한 의도된 파일만 stage한다. `git add -A`를 쓰지 않는다.
5. `private`에만 push하고 CI 완료까지 확인한다.
6. CI GREEN 뒤에만 testnet chart를 별도 커밋한다. feature OFF 배포와 rollback/smoke를
   먼저 확인한 뒤 shadow 기능만 opt-in한다.
7. reconciliation 증거가 안정된 뒤에만 xterm 신규 가입과 기존 계정 claim/link를 연다.

## 먼저 읽을 문서

- `docs/porting-research/execution-plan.md`
- `docs/porting-research/m3-journal-v2-gates.md`
- `docs/porting-research/persistence-supabase.md`
- `docs/porting-research/rust-differential.md`
- `docs/web-mud/game-identity-refactor.md`
- `docs/web-mud/trusted-admission.md`

완료율을 과장하지 않는다. 검증된 gate와 아직 검증하지 않은 경계를 분리해 보고한다.
