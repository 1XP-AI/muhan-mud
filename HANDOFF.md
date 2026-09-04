# Muhan MUD 포팅 핸드오프

- 최종 갱신: 2026-09-05 KST
- 브랜치: `codex/mud-identity-foundation`
- 포팅 기능 기준 커밋: `dfcaacec541015dc1c0a42d4ec87e2da1e400394`
- 최신 검증 커밋: `d9415508566a188c15829ed20aa7bdad03c82d65`

## 먼저 알아야 할 상태

이 브랜치는 레거시 C MUD의 파일 영속 상태를 Supabase/PostgreSQL로 단계적으로
이전하고, Rust가 검증 가능한 canonical CDTO 경계만 사용하도록 포팅하는 작업이다.
현재 branch에는 M3 observer/durable handoff, `PlayerSnapshotV1`의 C/Rust/PG17
검증 경계, M4 manifest relay, 그리고 C↔Rust helper wake 프로토콜의 첫 계약이
들어 있다.

macOS의 case-insensitive filesystem에서 일반 clone을 막던
`objmon/Celduin_sign`/`objmon/celduin_sign` 충돌은 `9f7bf24`에서 해결됐다.
새 clone은 충돌 경고 없이 clean checkout되고, 서로 다른 두 역사적 blob도 보존된다.

**현재 코드는 DB 권위 전환 완료본이 아니다.** PostgreSQL full
`PlayerSnapshotV1` validation과 opt-in idle consumer는 구현·계약 검증이 끝났지만,
기본 경로는 여전히 OFF이고 legacy player file이 authority다. Rust helper wake는
전송·helper process·DB 작업·배포를 전혀 포함하지 않는 16-byte 무상태 신호 계약일
뿐이다. 별도 TDD/PG17/운영 gate 없이 M3 runtime을 켜거나 game save를 DB 권위로
바꾸지 않는다.

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
  revert하지 않는다. 원격 branch에는 포함되지 않았다.

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

## 현재 포팅 경계

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

### 3. PostgreSQL full artifact 계약

- `supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql`
- `supabase/tests/player_snapshot_v1_artifact_contract.sql`
- `supabase/tests/player_snapshot_v1_artifact_pg17_integration.sh`

immutable artifact table, receipt anchor, record/reconciliation RPC와 role 경계가 있다.
`20260915000000`은 canonical envelope, 정확히 40개 field의 id/type/고정 길이,
`PLAYER`, HP/MP 범위, fixed string과 `ObjectGraphV1`의 node/depth/list 예산을
PostgreSQL에서 검사한다. `20260916000000`은 source octets를 acknowledged receipt에
정확히 묶는다. PG17 계약은 migration 140의 RPC 부재를 RED로 확인한 뒤 150/160 적용과
C/Rust exact-byte 재읽기를 GREEN으로 고정한다.

### 4. M4 manifest relay

- `services/m4-file-snapshot-manifest-relay/`

strict 13-line manifest를 lexical order로 읽고 direct PostgreSQL RPC를 호출하는 one-shot
Node service다. player payload를 읽지 않고 outbox evidence를 삭제·수정하지 않는다.

### 5. Durable handoff consumer의 idle lifecycle

`USE_M3_RUNTIME` build에서만 `main.c`가 optional native runtime을 시작한다. exact
`MUD_M3_PLAYER_SNAPSHOT_V1=handoff` opt-in일 때 `io.c`의 serialized game loop가
`output_buf`·command 처리·`update_game` 뒤에 idle hook을 호출한다. cadence는 1초마다
최대 한 token이고 OFF/BUSY/NOT_READY는 no-op이며 실패 진단은 rate-limited다. 일반 종료는
idempotent teardown을 거치고 SIGKILL은 기존 durable recovery 경계로 남는다. 기본 build와
기본 환경은 runtime symbol을 link하거나 I/O를 하지 않는다.

### 6. M3 helper wake protocol v1

`dfcaace`의 `m3_wake_v1.*`와 `rust/muhan-m3-wake-protocol/`은 identity나 durable-state
참조가 전혀 없는 exact 16-byte wake frame을 C/Rust differential test로 고정한다. 이는
미래 helper가 durable artifact/outbox를 다시 스캔하라는 best-effort 힌트일 뿐이며,
loss/duplicate/reorder에 의존하지 않는다. production transport, Unix socket, helper
process, database work, chart와 MUD runtime linkage는 이 slice에 포함되지 않는다.

## 남은 선행 작업

1. helper transport/process supervision은 별도 설계와 RED 테스트로 시작한다. wake frame은
   identity, path, credential, payload, acknowledgement 또는 authority 신호로 확장하지
   않는다.
2. M3 shadow opt-in을 검토하려면 feature-OFF testnet 배포, PVC/restart/rollback, PG17
   reconciliation과 실제 browser/xterm smoke를 별도 증거로 축적한다. DB authority 전환은
   그보다 뒤의 명시적 gate다.

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

## 다음 위임 권장안

서로 다른 checkout/worktree에서 다음 세 묶음을 병렬화할 수 있다.

1. **Terra/고난도:** 미래 helper transport/process supervision의 설계·RED 테스트만 맡긴다.
   wake가 유실돼도 durable scan/recovery가 독립적으로 정확한지 증명하고, DB authority와
   legacy save 결과는 바꾸지 않는다.
2. **Terra/고난도:** M3 shadow opt-in 전의 PG17/PVC/restart/reconciliation E2E gate를
   별도 worktree에서 보강한다. 활성화·배포는 이 작업의 권한이 아니다.
3. **Luna/중간 난도:** Linux CI, C↔Rust differential, feature-OFF chart 값과 browser/xterm
   smoke의 독립 재현을 맡긴다.

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

### 2026-09-05 현재 검증

다음은 `dfcaace` 기준으로 로컬에서 다시 확인했다.

```sh
make -C src character-save-journal-v2-bootstrap-test CC=cc
make -C src unit-test CC=cc
./scripts/run-m3-wake-differential.sh
pnpm --filter @muhan/m4-file-snapshot-manifest-relay test
pnpm --filter @muhan/m4-file-snapshot-manifest-relay typecheck
pnpm --filter @muhan/m4-file-snapshot-manifest-relay build
```

- focused bootstrap과 전체 C unit: pass.
- C ASan/UBSan wake oracle, Rust unit, C↔Rust malformed corpus differential: pass.
- relay: 49 pass, 3 expected skip; typecheck/build: pass.
- `PlayerSnapshotV1` full DB contract의 독립 Terra 검토는 P0/P1 구현 누락 없음으로
  판정했다. 선택적 P2 negative fixture 증강은 다음 별도 slice다.
- 이 handoff와 함께 들어가는 bounded bootstrap fixture 수정은 GitHub Ubuntu GCC의
  `-Werror=format-truncation` 경고를 해소하기 위한 것이다.

### 2026-09-05 CI 전체 복구 `67b6bc6`, `72e4099`, `06d10f0`, `d941550`

- `67b6bc6`은 Linux GCC bootstrap fixture의 bounded pathname 경고만 고쳤다.
- `72e4099`은 M3 RPC expiry fixture가 `expires_at > issued_at` 계약을 항상 만족하도록
  두 timestamp를 한 statement에서 재기준화했다.
- `06d10f0`은 successor writer epoch 2에 맞춰 M3 receipt exact-retry golden digest 두 개만
  동기화했다.
- `d941550`은 `player_store`가 쓰는
  `character_save_journal_v2_bootstrap_absent_head` 구현을 PG17 process-owner와
  runtime-shadow E2E harness의 명시 링크 목록에 각각 추가했다. 제품 runtime, SQL migration,
  DB authority, chart와 UI는 바꾸지 않았다.
- 로컬에서는 두 harness의 shell syntax/diff check 및 `player_store`·`bootstrap` 정적 compile와
  bootstrap unit test가 통과했다. PostgreSQL 17 실통합은 GitHub Actions에서 확인했다.
- private GitHub Actions: <https://github.com/1XP-Inc/muhan-mud/actions/runs/33915490900>
  (`Supabase ownership contract`, Ubuntu ARM, Ubuntu, Windows, macOS 모두 GREEN). 이 run에는
  M3 RPC receipt/login/startup, PlayerSnapshot/relay, process-owner+runtime-shadow, named-volume
  restart와 importer integration이 포함된다.
- 사용자 소유 `src/frp.new`는 이번 네 개 커밋 어디에도 포함되지 않았다.

## Kubernetes 현황

- context: `testnet-1xp`
- release: `muhan-mud-testnet`
- URL: <https://muhan.1xp.vc>
- Helm revision: `9` (`deployed`, 2026-09-05 KST 확인)
- web, gateway, MUD, Auth, PostgREST, Realtime와 PostgreSQL은 모두 Ready였다.
- running app digest:
  `sha256:5f025ee1732981b2cd6d9aaef18ffc858b90905f6cc1e80a9bbf8d141ce174ec`
- chart repo: sibling checkout `../tesnet-1xp.nosync/muhan-mud/charts`
- onboarding과 reconciler는 enabled다. `m3.mode=off`, replay differential도 OFF다.
- 웹 Auth 계정은 MUD 캐릭터를 자동 생성하거나 자동 연결하지 않는다. 연결된 캐릭터가
  없는 계정이 게임 진입 전 안내를 보는 것은 정상이며, 실제 신규 가입/기존 캐릭터 link는
  xterm의 원래 wizard/claim 흐름으로 검증해야 한다.
- 영속 QA 계정이나 캐릭터는 아직 만들지 않았다. 사용자 데이터가 생기는 실제 smoke는
  명시적으로 기록하고 정리 가능한 test account만 사용한다.

## 다음 완료 순서

1. M3는 OFF인 채로 testnet의 xterm 신규 가입과 기존 캐릭터 claim/link를 실제 browser
   smoke로 검증한다. game account와 Auth account의 분리는 유지한다.
2. 다음 M3 slice는 helper transport/process supervision 또는 shadow reconciliation 중
   하나만 선택해 RED→GREEN으로 시작한다. wake protocol 자체에는 runtime transport를
   덧붙이지 않는다.
3. feature-OFF 배포, PVC/restart, rollback, PG17 reconciliation과 충분한 shadow evidence가
   모두 쌓인 뒤에만 별도 승인으로 M3 opt-in을 검토한다. DB authority 전환은 그 이후다.

## 먼저 읽을 문서

- `docs/porting-research/execution-plan.md`
- `docs/porting-research/m3-helper-wake-protocol-v1.md`
- `docs/porting-research/m3-journal-v2-gates.md`
- `docs/porting-research/persistence-supabase.md`
- `docs/porting-research/rust-differential.md`
- `docs/web-mud/game-identity-refactor.md`
- `docs/web-mud/trusted-admission.md`

`execution-plan.md`의 날짜가 있는 snapshot은 당시의 역사적 근거다. 현재 상태는 이
handoff와 이후 GitHub CI 결과를 우선한다. 완료율을 과장하지 않고, 검증된 gate와 아직
검증하지 않은 경계를 분리해 보고한다.
