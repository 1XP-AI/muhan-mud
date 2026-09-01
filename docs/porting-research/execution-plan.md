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
  → C canonical load
  → xterm old game password, Telnet echo off
  → C가 기존 player password를 정확히 한 번 비교
  → C: MUD1O VERIFIED|<name_hex>|<file_sha256>
  → Gateway: claim_legacy_game_character RPC, same correlation
  → Gateway: MUD1O CLAIMED|<character_uuid>
  → C authoritative reload/init
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
- service-only RPC
  - `begin_game_character_onboarding`
  - `begin_game_character_provisioning`
  - `finalize_game_character_provisioning`
  - `reconcile_game_character_provisioning`
- 기존 `claim_legacy_game_character`는 C 검증 뒤에만 호출한다.

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

- 신규 provisioning과 기존 claim을 실제 xterm에서 수행
- 저장/claim DB 확정 전 gameplay 차단
- 실제 C/Gateway/Postgres/Playwright desktop+mobile 통과
- password/token/ticket artifact leak 0

### M2 CDTO player/inventory clone-only

- ABI fingerprint와 C exporter/importer
- `rust/muhan-core-dto` canonical codec
- sanitized golden/property/fuzz/differential suite
- production read/write 변화 없음

### M3 character/bank dual-write shadow

- direct writer facade coverage
- fsynced intent journal + idempotent DB receipt
- economy concurrency/failure injection
- 7일 zero mismatch

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
