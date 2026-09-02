# Muhan MUD 포팅 실행 계획

상태: **실행 기준선**

작성일: 2026-09-01

대상: `codex/mud-identity-foundation`, testnet-1xp

## 목표

레거시 C MUD의 영속 상태를 Supabase/Postgres 권위 모델로 단계적으로 이전하고
Rust 포팅 경계를 구축한다. TDD, 결정론적 differential test, dual-write,
shadow 검증을 통과한 기능만 전환한다. 첫 사용자 결과는 다음 두 흐름이다.

1. Supabase 로그인 뒤 xterm에서 원래 텔넷 가입 절차로 새 캐릭터를 만든다.
2. 기존 MUD 이름과 게임 비밀번호를 xterm에서 한 번 검증해 Supabase 계정에
   연결한다.

절대적인 “버그 0%”를 증명할 수는 없다. 대신 **검증되지 않은 기능은 권위
경로로 전환하지 않는다**는 규칙을 강제한다.

## 현재 실행 상태 (2026-09-02)

| 범위 | 상태 | 검증/잔여 gate |
| --- | --- | --- |
| M0 연구·계약 | 현재 검증 범위 고정 | ADR, DB/Rust/Web 연구와 공통 `MUD1O` fixture가 branch에 고정됨 |
| M1 C·Gateway·Web·SQL 구현 | 로컬 통합 GREEN, testnet 미배포 | 실제 C+Gateway+PostgREST+PostgreSQL 17 stack과 browser 8/8 시나리오가 GREEN. gateway 75/75, reconciler 18/18, importer 16 pass·2 skip, web 17/17 단위 결과도 GREEN. testnet 승격 gate는 아직 남아 있음 |
| durable onboarding receipt | 컴포넌트 GREEN | `pending → saved → committed` fsync 기록, startup 복구, sanitizer, C player `0600`/shard `0700` writer와 Helm PVC 권한 계약 GREEN |
| out-of-band reconciler | 컴포넌트 GREEN | no-follow 파일/hash와 exact service RPC, committed 무변이, process-local 중복 억제, aggregate-only polling 및 비정상 입력 회귀 GREEN |
| lock-wait TTL 회귀 | GREEN (로컬 PG17 + CI 계약) | 로컬에서 bootstrap+020..080을 두 번 적용한 기존 계약이 GREEN이고, 원격 PostgreSQL 17 CI는 020..090을 두 번 적용한다. claim/session RPC와 M3 receipt의 post-lock fresh clock, committed lifecycle 변경 경주가 모두 무변이로 GREEN |
| testnet Helm | 로컬 렌더 GREEN, 미배포 | 외부 인프라 chart render 14/14 및 `helm lint` GREEN. chart는 030~080을 순서대로 한 번 실행하고, 앱 CI는 모든 additive migration을 의도적으로 두 번 적용해 replay 안전성을 검증. cluster에는 적용하지 않음 |
| legacy inventory importer | unit GREEN, PG17 retry 계약 GREEN | unit 16 pass·2 skip·0 fail. Linux + Node 22 + disposable PostgreSQL 17의 dry-run/apply, idempotent retry, atomic failure, concurrent serialization 및 SQLSTATE `40001` bounded retry 계약은 GREEN |
| C bounded player decoder | sanitizer/unit GREEN | player-only bounded decoder의 depth 64·object 8192 예산, partial/EINTR·exact EOF·pointer scrub·문자열 NUL 경계와 allocation failure를 ASan/UBSan unit에서 GREEN; gameplay room loader는 변경하지 않음 |
| M2 CDTO/Rust | ObjectV1+CreatureV1+ObjectGraphV1 clone-only GREEN | recursive preorder graph까지 C/Rust exact-byte differential, malformed taxonomy, depth 64/node 8192, allocation faults와 Linux LeakSanitizer가 GREEN. production read/write·gameplay 경로에는 연결하지 않음 |
| M3 writer/inventory | 090 SQL + 091a + 091b-1a/1b/2/3 + 092a CI GREEN; 092b process-SIGKILL matrix CI `33628598496` GREEN | writer epoch/seal/permanent fence, immutable hash-only receipt, head CAS를 PostgreSQL 17에서 고정. 091a의 stage/hash/parser, 091b-1a의 persisted writer tuple·process-lifetime PVC lock·every-opener fsync, 091b-1b의 held-writer route binding과 route RPC가 CI `33579360870`에서 GREEN. 091b-2의 expected-existing rename, expected-absent loss-safe no-replace publish와 로컬 복구는 CI `33586412456`에서 GREEN. 091b-3의 exact receipt callback과 durable local `DB_ACKED` marker retry는 CI `33589685559`, route-free single-record `recover_one`은 CI `33591568569`, deterministic lexical backlog scanner는 CI `33594859743`, test-only native libpq adapter의 actual PostgreSQL 17 통합은 CI `33598858884`에서 GREEN. 실제 offline `DEFERRED`, 만료 A exact-renew/ACK, seal 뒤 B 설치와 A 영구 fence는 CI `33601547197`에서 GREEN. 092a의 held-root save/recovery, exact receipt replay와 process-local A→B handoff mock은 CI `33615886826`에서 GREEN. 092b는 26개 named durability cutpoint의 fresh save 41행·fresh recovery 37행과 actual PG17 outcome-unknown/retry를 CI `33628598496`에서 검증했다. 실제 host power loss/PVC 증적과 production transport/login/startup wiring은 미완료이며 `save_ply`·bank·dual-write·shadow에는 연결하지 않음 |

현재 branch의 코드는 실험적 기능을 포함하지만 기본 활성 경로가 아니다. 현재
testnet에는 배포하지 않았으며, onboarding 및 legacy importer 기능 flag는 OFF이고
importer는 apply 없이 dry-run 기본값이다. 다음 gate를 별도로 통과하기 전에는
`onboarding.enabled`를 켜거나 testnet에 배포하지 않는다.

1. clean checkout에서 실제 C+Gateway+PostgREST+PG17 stack E2E 및 CI 재현
2. testnet PVC에서 C player 파일/샤드의 `0600`/`0700` 권한과 backup/restore 확인
3. feature-off 배포 뒤 실제 ingress에서 desktop/mobile smoke와 rollback 확인
4. 전체 ASan/UBSan, secret/artifact scan 및 공급망 검증
5. M3 선행 blocker 구현 뒤에만 dual-write shadow 관찰

### 최근 TDD 결함 수정과 회귀 증거

현재 검증에서 확인한 여러 실제 결함은 각각 RED를 먼저 고정한 뒤 최소 수정과
회귀 테스트로 GREEN을 확인했다. 이 목록은 해당 경로의 검증 결과이지 전체 시스템의
무결성을 보증하는 표현이 아니다.

- `40001` 재시도: serializable transaction이 serialization failure를 받으면 같은
  immutable batch를 새 client·새 transaction으로 최대 3회 재시도한다. 짧은 고정
  backoff를 사용하며, validation/conflict와 기타 오류는 재시도하지 않는다. 실제
  Linux + PostgreSQL 17 importer 통합 테스트가 GREEN이다.
- stale clock: advisory/row lock을 기다린 뒤 transaction-start `now()`가 만료된
  CHALLENGE/final CLAIM을 통과시키던 RED를 재현했다. 두 RPC 모두 lock 획득 뒤
  `clock_timestamp()`를 다시 읽도록 수정했고, lock-wait harness에서 만료 후
  `P0001` 거부와 무변이를 확인했다.
- password state: 비밀번호 prompt의 echo/clear·취소·재접속 상태 전이를 테스트하고
  자동 재전송과 secret artifact 기록이 없음을 확인했다.
- symlink/errno: importer scanner의 no-follow symlink, non-regular file, short
  read와 errno 경계를 TDD로 고정해 모호한 파일을 import하지 않도록 했다.
- bounded player decoder: 인터넷-facing player load에 depth 64·전체 object 8192
  budget, short/EINTR read, partial pointer scrub, PLAYER/fd invariant, 각 C-string
  NUL, exact EOF와 clean-tree 실패를 고정했다. ASan/UBSan decoder unit과 일반 C
  unit이 GREEN이며, COMPRESS의 length-less decoder 경로는 player load에서
  fail-closed다.
- save journal durability: temp `close()` 오류에서 descriptor를 두 번 닫을 수 있던
  소유권 경계와, canonical hard-link 뒤 unlink 실패 시 보존해야 할 staging을 generic
  cleanup이 지우던 경로를 fault-injection RED로 고정했다. fd는 한 번만 relinquish하고
  `RECONCILE_REQUIRED` staging hard-link는 검사할 수 있게 유지한다. 이 저널은 여전히
  synthetic/test-only이며 live save 경로에는 연결하지 않는다.
- ObjectGraph 검증: preorder subtree 재진입, depth 분류 drift, allocation-fault cleanup과
  Linux 전용 1,483-byte test buffer leak를 RED로 고정했다. C/Rust differential,
  ASan/UBSan/LeakSanitizer와 Rust 1.92/1.98 Clippy가 모두 GREEN이다.
- M3 receipt 검증: `P0001` 기대 helper가 대상 SQL 성공도 통과시키던 거짓 양성,
  `FOR KEY SHARE`가 lifecycle non-key update를 안정화하지 못하는 경주, missing/behind/
  mismatched head exact retry를 RED로 고정했다. 별도 success sentinel 자기검증,
  `FOR SHARE`, strict head consistency와 PG17 2-session race로 GREEN을 확인했다.
- M3 native transport 검증: 첫 actual PG17 run에서 `pg_roles.rolpassword`의 마스킹 값을
  NULL로 오인한 fixture 검사가 RED가 됐다. superuser fixture가 `pg_authid`의 실제 NULL과
  전체 역할 플래그를 확인하도록 교정한 뒤, native libpq callback의 revoke 대조군·ACK·
  exact retry·두 freeze 매핑과 전체 상태 무변이를 CI `33598858884`에서 GREEN으로 고정했다.
- M3 092a 조합 검증: 첫 CI `33615554934`에서 production link-map probe가 macOS 전용
  `-map`을 GNU ld에도 전달하는 RED가 재현됐다. OS별 `V2_LINK_MAP_OPTION`으로 교정해
  CI `33615886826`에서 Linux/ARM/macOS/Windows와 PostgreSQL 17 전체를 GREEN으로
  고정했다.

### 현재 검증 snapshot (2026-09-02)

실행한 검증의 숫자와 범위를 다음처럼 고정한다. 단위 테스트의 skip은 실패가
아니지만, 이를 전체 기능 통과로 확대 해석하지 않는다.

| 계층 | 결과 | 근거 |
| --- | --- | --- |
| Browser E2E | 8/8 pass (13.6s) | Playwright desktop/mobile, claim password prompt·clear·retry·normal close 포함 |
| Gateway | 75/75 pass, 0 cancelled | `pnpm --filter @muhan/gateway test` |
| Reconciler | 18/18 pass | `pnpm --filter @muhan/onboarding-reconciler test` |
| Importer | 16 pass, 2 skip, 0 fail | `pnpm --filter @muhan/character-inventory-importer test`; PG17 retry는 disposable CI 계약 |
| Web | 17/17 pass | `pnpm --filter @muhan/web test` |
| C bounded decoder | pass | `make -C src files1-decoder-test CC=gcc` (ASan/UBSan) 및 C unit |
| Credential lifecycle | pass | `tests/unit/onboarding_credential_lifecycle_test.py` |
| M2 CDTO/Rust graph | pass, clone-only | ObjectGraph C unit/sanitizer, 12 Rust unit+2 differential, fixed/random corpus와 Linux LeakSanitizer |
| M3 journal v1 / 090 SQL / 091a·091b-3 C / 092a / 092b process matrix | v1 test-only + 090/100 PG17 + 091a·091b-1a CI `33573456858` + 091b-1b CI `33579360870` + 091b-2 CI `33586412456` + 091b-3 ACK CI `33589685559` + recover_one CI `33591568569` + backlog scanner CI `33594859743` + native PG CI `33598858884` + expiry/successor CI `33601547197` + 092a CI `33615886826` + 092b CI `33628598496` GREEN | 091b-1b의 opaque held-writer handle과 exact DB identity route mock은 GNU GCC/PostgreSQL 17에서 GREEN. 091b-2의 no-clobber local publish, exact two-name crash recovery, fsync/unlink 재시도와 immutable marker evidence는 전체 CI에서 GREEN. 091b-3 ACK slice는 exact absent/existing 12-field callback, offline defer, post-callback writer/live 재검증, marker cutpoint retry와 static no-live-link가 GREEN. route-free `recover_one`은 caller route/path/payload 없이 immutable PREPARED와 held tuple만으로 한 건을 복구한다. Bounded backlog scanner는 전체 candidate snapshot을 먼저 검사하고 bytewise UUID 순으로 recover+ACK하며 deferred/freeze/changed-live 방문, capability-loss 중단, OOM/cap/close 무변경, no-GC와 report 합계를 GNU GCC 일반·ASan/UBSan 및 전체 CI에서 검증했다. Test-only native libpq adapter는 PostgreSQL 17에서 startup role isolation, revoke 대조군, ACK/idempotent retry 및 SQLSTATE freeze 매핑을 실제 실행했고, 실제 offline/expiry/exact-renew/successor permanent fence까지 통과했다. 092a는 held-root stage/precondition/PREPARED, 대표 fresh-child 복구, current-command exact receipt replay, local-incomplete 상세 결과, mock drain/seal/B와 production no-live-link를 검증했다. 092b는 41 save-process + 37 recovery-process SIGKILL 행과 actual PG17 pre-send/post-commit/protocol/RPC retry를 CI `33628598496`에서 검증한다. 이는 process crash 증거이며 host power loss/PVC 보증은 아니다. production transport/login/startup과 live writer 연결은 미완료 |
| PG migration 090 | PostgreSQL 17 CI contract GREEN | bootstrap+020..090 두 번 적용, identity/onboarding/M3 SQL과 세 lock-expiry script 통과 |
| PG migration 100 route v2 | PostgreSQL 17 CI contract GREEN (`33579360870`) | exact seven-field route signature, `storage_format=1`, 세 allowed lifecycle, nullable imported hash, exact regprocedure identity, mud_writer-only execute 및 전체 fixture identity/receipt non-mutation을 disposable PostgreSQL 17에서 검증 |
| Helm | 14/14 render + lint GREEN | 별도 인프라 chart 검증; chart/cluster는 이 저장소·실행 범위 밖 |

## 연구 근거

- [영속성·Supabase 이관 연구](persistence-supabase.md)
- [C → Rust 차등 포팅 연구](rust-differential.md)
- [웹 온보딩·E2E 연구](web-onboarding-e2e.md)
- 현재 권위 분리: [MUD identity·저장 구조](../web-mud/game-identity-refactor.md)
- 현재 private 입장 계약: [Trusted admission](../web-mud/trusted-admission.md)

## 결정 사항

### 1. Big-bang rewrite를 하지 않는다

C 프로세스는 각 slice가 승격될 때까지 게임 규칙과 legacy 파일의 oracle이다.
Rust가 raw `creature`, `object`, `room` bytes를 직접 해석하지 않는다. raw struct는
native pointer, padding, `long` 폭, compiler ABI를 포함하므로 원래 ABI의 C
exporter만 읽을 수 있다.

```text
legacy raw file / C heap
          │ C exporter: runtime pointer 제거 + invariant 검사
          ▼
  CDTO v1 canonical bytes ── digest ── golden/replay evidence
          │                              ▲
          ├─ Rust decode/pure transition ┤
          └─ C importer clone ───────────┘

권위 전환 전에는 source file, live Ply[], socket을 변경하지 않는다.
```

### 2. Supabase는 Auth만이 아니라 최종 gameplay DB가 된다

단, 다음 순서로 권위를 옮긴다.

```text
A. files canonical
   read-only export → DB shadow → reconciliation

B. files canonical + durable dual-write
   fsynced local journal → legacy atomic projection → idempotent DB receipt

C. DB canonical
   command/state/outbox one transaction → legacy file is derived projection
```

POSIX 파일과 Postgres를 하나의 원자 transaction처럼 취급하지 않는다. B 단계의
local journal과 C 단계의 DB outbox가 crash window를 복구한다.

### 3. 가입·claim 입력은 실제 xterm 흐름을 유지한다

웹 HTML form이 C wizard를 복제하면 validation과 prompt가 drift한다. onboarding은
별도 WSS subprotocol과 private C control을 사용하되, 이름·성별·직업·능력치·무기·
성향·종족·게임 비밀번호는 xterm에서 입력한다.

기존 `MUD1` active-character 입장 계약은 변경하지 않는다. 새 흐름은 버전이 다른
`MUD1O` 계약으로 격리하고 feature flag 기본값을 off로 둔다.

### 4. 저장과 계정 연결은 두 단계로 확정한다

DB가 active가 되기 전에는 게임 명령을 열지 않는다. C 저장 성공 뒤 DB finalize가
실패하면 파일을 삭제하거나 wizard를 자동 재실행하지 않는다. 같은 correlation과
file digest로 reconcile한다.

## 첫 수직 슬라이스: xterm onboarding

### 공통 연결

```text
Browser                  Gateway                    Supabase                C MUD
   │ JWT + mode              │                          │                       │
   ├────────────────────────>│ verify JWT               │                       │
   │                         ├─ begin intent RPC ──────>│ private, TTL          │
   │                         │<──── intent result ──────┤                       │
   │                         ├─ signed MUD1O ticket ───────────────────────────>│
   │                         │<────────────────────────────── MUD1O OK          │
   │<──── onboarding-ready ──┤                          │                       │
   │                         │                          │       original prompt │
   │<══════════════════════════════════════════════════════════════════════════│
```

첫 WSS frame은 strict JSON이며 unknown key를 거부한다.

```json
{"type":"onboarding-auth","accessToken":"<JWT>","mode":"provision|claim","correlationId":"<uuid>"}
```

`correlationId`는 retry identity일 뿐 권한이 아니다. JWT subject와 private intent의
actor가 같아야 하고, Gateway가 service-only RPC 결과를 검증한 뒤에만 C TCP를 연다.

### 신규 provisioning

```text
xterm name input
  → C UTF-8/path/canonical 검사 + local NOT_FOUND 확인
  → C: MUD1O RESERVE|<name_hex>
  → Gateway: begin_game_character_provisioning RPC
  → Gateway: MUD1O RESERVED|<character_uuid>
  → C: 기존 create_ply wizard
  → PlayerStore atomic save
  → C: MUD1O SAVED|<character_uuid>|<sha256>|<format>
  → Gateway: finalize_game_character_provisioning RPC
  → Gateway: MUD1O COMMIT
  → C command state + Browser ready
```

이름은 xterm에서 입력한 뒤 예약한다. 예약 응답 전에는 추가 browser input을 C로
보내지 않는다. `PLAYER_STORE_OK`만 SAVED를 만들며, partial write/fsync/rename/disk
full/recovery queue는 active로 전환하지 않는다.

### 기존 캐릭터 claim

```text
xterm legacy name
  → C canonical load + player-file SHA-256
  → C: MUD1O CHALLENGE|<name_hex>|<file_sha256>
  → Gateway: service-only challenge RPC
  → DB: actor/correlation/character/SHA exact binding, target-wide 3회/15분,
        90초 allow ledger
  → Gateway: MUD1O ALLOW
  → xterm old game password, Telnet echo off (ALLOW 전에는 prompt 금지)
  → C가 기존 player password를 정확히 한 번 비교
  → C가 player file SHA-256을 다시 계산해 challenge와 exact match
  → C: MUD1O VERIFIED|<name_hex>|<file_sha256>
  → Gateway: ledger-bound final claim RPC, same actor/correlation/character/SHA
  → DB: challenge 미사용·미만료와 imported_file_sha256를 row lock 안에서 exact match
  → Gateway: MUD1O CLAIMED|<character_uuid>
  → C claim socket close
  → Browser roster refresh → existing MUD1 admission
```

wrong password, missing/corrupt/IO error, already owned는 browser에 같은 일반 오류를
보낸다. 비밀번호는 DB/RPC/audit/log/evidence에 넣지 않고 자동 재전송하지 않는다.
현재 raw player file에는 plaintext game password가 남으므로 웹 계정 비밀번호와
다른 값을 쓰라는 안내를 표시한다. KDF 전환은 versioned player format 뒤에 한다.

### Supabase 후속 migration

기존 migration을 수정하지 않고 additive migration을 만든다.

- `private.game_character_onboarding_intents`
  - `correlation_id`, actor, mode, status, expiry, consumed/finalized timestamps
- `private.game_character_provisioning_requests`
  - character, canonical name, expected/saved digest, format, reconcile state
- `private.game_character_claim_attempts`
  - correlation, actor, character, exact imported-file digest, 90초 allow/claimed timestamps
  - 같은 target은 actor가 달라도 15분에 세 번만 password prompt까지 진입
- service-only RPC
  - `begin_game_character_onboarding`
  - `begin_game_character_provisioning`
  - `finalize_game_character_provisioning`
  - `reconcile_game_character_provisioning`
  - `challenge_legacy_game_character_onboarding`
- `claim_legacy_game_character_onboarding`은 선행 challenge와 C가 재검증한 exact
  player SHA-256을 `imported_file_sha256`과 같은 row lock 안에서 비교한 뒤에만
  ownership을 옮긴다.
- 이전 4인자 `claim_legacy_game_character` primitive는 service role execute를 회수해
  fingerprint 검사를 우회할 수 없게 한다.

브라우저는 onboarding/private row와 RPC를 읽거나 호출하지 못한다. service role도
table CRUD를 받지 않고 narrow RPC execute만 받는다. 같은 correlation+payload retry는
같은 결과를 반환하고, 같은 correlation+다른 payload는 거부한다.

## Canonical gameplay schema 순서

| 순서 | Aggregate | 이유 |
| --- | --- | --- |
| P0-1 | character + nested inventory + bank | 계정 가치와 경제 split-write 위험이 가장 큼 |
| P0-2 | room + exits + permanent mobs/items | 월드 권위와 respawn/timer를 고정해야 함 |
| P1-1 | board + mail/post | 현재 index/body 및 append가 경합과 partial write에 취약 |
| P1-2 | alias + family + vote/invite + relationship | player file과 별도 파일의 중복 관계를 정규화 |
| P0-3 | world clock + scheduled jobs | 재시작 drift와 중복 toggle/spawn 방지 |
| R | item/monster/help/talk catalogs | revisioned static resource로 먼저 import |

모든 mutable aggregate에는 `revision`, `command_id`, `updated_at`이 있고 item은
character/room/monster/bank/container 중 정확히 한 owner만 갖는다. 브라우저는
gameplay table DML을 받지 않는다.

## TDD와 지속 검증 규칙

### RED → GREEN → REFACTOR 증거

각 task는 다음 증거를 남긴다.

1. 새 실패 test와 실패 이유
2. 최소 구현 뒤 해당 test 통과
3. 관련 전체 suite 통과
4. secret/redaction scan
5. 변경된 계약과 rollback 방법

테스트를 나중에 추가하는 task는 완료로 처리하지 않는다.

### 검증 계층

```text
pure unit
  ├─ C/TS/SQL canonical-name vectors
  ├─ protocol parser/state transitions
  ├─ CDTO codecs/properties
  └─ rate-limit/redaction
        │
database + C component contracts
  ├─ RLS/RPC/idempotency/race
  ├─ C ticket/create/claim/save failures
  └─ Rust/C round-trip clone
        │
real integration
  ├─ disposable PostgreSQL/Supabase
  ├─ actual C binary + Gateway
  ├─ create/save/restart/relogin
  └─ DB outage/crash-window reconcile
        │
Playwright desktop + mobile
        │
testnet shadow/canary + rollback drill
```

### Cutover gate

한 slice는 아래를 모두 만족해야 active execution/read source가 될 수 있다.

- pinned Linux/amd64 oracle의 state digest, output bytes, status, clock/RNG trace zero diff
- 10,000개 이상의 결정론적 transition, 100 seed, 모든 failure fixture zero diff
- C ↔ CDTO ↔ Rust ↔ C clone round-trip과 malformed corpus rejection 통과
- ASan/UBSan, property, fuzz에서 crash/leak/UB/allocation-cap 우회 없음
- representative shadow traffic 7일간 unexplained mismatch 0
- journal/outbox backlog 0, 두 번의 독립 full reconciliation에서 P0 mismatch 0
- backup/restore와 reverse-cutover drill 통과
- feature flag 한 번으로 C 권위 경로로 돌아갈 수 있음

의도적 차이는 generic allowlist에 숨기지 않는다. versioned fixture, owner, 이유,
사용자 영향, 재검토 날짜가 있어야 한다.

## 병렬 실행 계획

| Lane | 모델 | 첫 작업 | 독립 범위 | 합류 gate |
| --- | --- | --- | --- | --- |
| A | Terra | onboarding DB migration + SQL race/RLS contract | `supabase/` | RPC wire/type 계약 고정 |
| B | Terra | C `MUD1O` parser/state + claim/provision save ACK tests | `src/`, `tests/` | C real scenario 통과 |
| C | Luna | Gateway onboarding parser/state tests와 Web empty-roster UX contract | `services/gateway/`, `web/` | A/B contract fixtures 소비 |
| O | Root | ADR, protocol fixture, integration review, CI, merge, 운영 안전 | cross-cutting | 모든 lane evidence 검토 |

실행 순서:

```text
Wave 1: A(DB RED) + B(C RED) + C(Gateway/Web RED) 병렬
             │ root가 shared vectors/wire fixture 고정
             ▼
Wave 2: A/B/C GREEN 구현 병렬
             ▼
Wave 3: actual C + Gateway + disposable DB integration
             ▼
Wave 4: Web + Playwright desktop/mobile
             ▼
Wave 5: testnet feature flag off 배포 → shadow → canary
```

같은 파일을 두 lane이 수정하지 않는다. shared contract 변경은 root 승인 뒤 모든
lane fixture를 동시에 갱신한다. 연구 에이전트의 문서를 곧바로 production truth로
간주하지 않고 root가 source/test와 대조한다.

## Milestone과 완료 조건

### M0 연구·계약

- 세 연구 문서와 이 실행 계획 merge
- shared name/protocol/lifecycle vectors
- feature flag 및 rollback 경계

### M1 웹 onboarding

- 상태: 로컬 component와 실제 C/Gateway/PostgREST/PostgreSQL 17 stack GREEN
- browser 8/8 시나리오와 lock-wait TTL regression GREEN
- gateway 75/75, reconciler 18/18, importer 15 pass·2 skip, web 17/17 단위 결과 GREEN
- 신규 provisioning과 기존 claim은 저장/claim DB 확정 전 gameplay를 차단
- testnet에는 아직 배포하지 않았고 `onboarding.enabled`는 OFF
- 남은 gate는 clean-checkout 재검증, backup/rollback, testnet shadow 관찰

### M2 CDTO player/inventory clone-only

- 상태: `ObjectV1`+`CreatureV1`+recursive `ObjectGraphV1`와 envelope unit GREEN,
  clone-only 경계 유지
- `rust/muhan-core-dto` canonical codec, exact C↔Rust bytes, sanitized golden/property/
  malformed/fixed-seed differential 범위
- preorder closure, depth 64, total node 8192, pointer-free padding과 allocation failure 고정
- production read/write 변화 없음

### M3 character/bank dual-write shadow

- 상태: 090 PostgreSQL writer epoch/seal/fence, immutable receipt와 head CAS가 GREEN;
  live 연결은 없음
- exact unsealed/unexpired writer, canonical request digest, consistent head와 strict next
  revision만 ACK하며 successor 뒤 old writer는 영구 fence
- 091 C journal v2의 `writer_instance_id`/`character_id`/`request_sha256`/derived staged
  leaf, descriptor ancestry, cumulative 64 MiB hash cap, immutable `PREPARED`와 raw-byte/
  write/fsync/close 회귀는 091a test-only GREEN
- 091b-1a의 PVC process-lifetime lock, exact persisted writer tuple, first-create 경쟁과
  재시도 fsync 회귀, production static no-live-link는 test-only GREEN
- 091b-1b의 trusted route binding C seam과 additive v2 route RPC는 CI
  `33579360870`에서 GNU GCC/ASan·UBSan/PostgreSQL 17 GREEN
- 091b-2의 expected-existing rename, expected-absent `linkat → destination fsync →
  source unlink → source fsync`, exact two-name crash recovery와 marker retry는 test-only
  CI `33586412456`에서 GNU GCC 일반·ASan/UBSan GREEN이며 atomic move로 간주하지 않음
- 091b-3의 exact absent/existing 12-field receipt callback, DB-offline defer, post-callback
  writer/live 재검증과 durable local `DB_ACKED` marker cutpoint retry는 test-only
  경계로 CI `33589685559`에서 GREEN이며 live MUD `OBJECTS` 밖에 유지
- 091b-3의 route-free single-record `recover_one`은 held writer+command UUID만 받아
  immutable PREPARED가 정한 shard/name/precondition으로 복구하며 CI `33591568569`에서
  GNU GCC 일반·ASan/UBSan, Windows/ARM/macOS와 static no-live-link가 GREEN
- 091b-3의 bounded lexical backlog scanner는 canonical PREPARED snapshot을 bytewise
  정렬해 recover+ACK하고 deferred/freeze/changed-live를 계속 방문하며 capability-loss는
  중단한다. OOM·cap·close·unsafe leaf 무변경과 no-GC/static no-live-link는 CI
  `33594859743`에서 GREEN
- test-only native libpq receipt adapter는 exact typed parameters/SQLSTATE 매핑, startup
  `mud_writer` role isolation, revoke 대조군, ACK/exact retry/freeze 무변이를 actual
  PostgreSQL 17 CI `33598858884`에서 GREEN으로 고정
- actual offline `DEFERRED`, 만료된 A의 receipt freeze, exact A renew/ACK, seal 뒤 B
  epoch 2 설치와 이후 A renew/seal/receipt 영구 fence는 PostgreSQL 17 CI
  `33601547197`에서 head·receipt·epoch·fence·identity 무변이와 함께 GREEN
- 092a test-only protocol은 held root에서 stage와 live precondition을 완료한 뒤에만
  PREPARED를 남기고, publish 뒤 current command의 durable PREPARED와 동일한 전체
  receipt snapshot을 재전송한다. malformed/conflicting local ACK 증거는 보존하면서
  `DB_ACKED_LOCAL_INCOMPLETE`를 보고하고, 대표 fresh-child PREPARED/PUBLISHED 재시작과
  process-local drain/attest/seal/B mock, static no-live-link를 CI `33615886826`에서 검증
- 092b의 26개 named durability cutpoint를 41개 fresh save process와 37개 fresh
  recovery process에서 SIGKILL한 뒤 재실행하는 matrix는 로컬 일반·ASan/UBSan에서
  GREEN이다. 별도 actual PostgreSQL 17 lane은 pre-send loss, committed-but-unread
  receipt, protocol recovery, acquire/renew/seal exact retry와 successor fence를
  검증했고 private CI `33628598496`도 GREEN이다. 실제 host power-loss/PVC 증적은
  아직 남아 있다.
- production transport 권한·설정·호출·startup 연결은 아직 구현하지 않았으므로
  091 전체는 미완료
- production absent-head seed, `mud_writer` transport, PVC flock/fsync 증적과 retention은
  승인 전 blocker
- shadow 관찰과 cutover 조건은 아직 시작하지 않음

### M4 room/social/timer 도메인 확장

- domain별 같은 gate 반복
- world clock/job fencing과 restart test

### M5 testnet DB canonical canary

- 새 backup + DB PITR marker + final watermark
- one-world single-writer canary
- reverse-cutover drill
- 관찰 기간 뒤에만 다음 domain 승격

## 운영 안전 규칙

- testnet에서도 적용 직전 새 Postgres dump와 MUD PVC snapshot을 만들고 checksum을
  검증한다.
- additive migration만 사용한다. `DROP`, `TRUNCATE`, unclaim/delete rollback을 하지
  않는다.
- MUD replica는 shared fencing이 생기기 전까지 1, `Recreate`다.
- rollout rollback은 image/chart revision과 권위 flag를 되돌리되 ownership/audit/
  recovery row는 보존한다.
- 기존 검증 배포와 domain `muhan.1xp.vc`는 M1 전체 gate 전까지 현재 active-character
  경로를 유지한다.

## 지금 하지 않는 것

- raw C 파일을 Rust `repr(C)`로 직접 decode
- browser가 gameplay table을 직접 수정
- C network/select loop를 먼저 Rust async runtime으로 교체
- legacy password를 검증 없이 Supabase Auth password와 통합
- 여러 MUD writer를 같은 world/PVC에 실행
- P0 mismatch, unresolved quarantine, recovery backlog가 있는 상태의 cutover
