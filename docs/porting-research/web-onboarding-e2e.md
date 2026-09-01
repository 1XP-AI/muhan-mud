# 웹 온보딩·E2E 포팅 연구

작성일: 2026-09-01

상태: 조사·설계안, 제품 코드와 live 환경은 변경하지 않음
범위: Supabase 로그인 뒤의 신규 캐릭터 provisioning, 기존 캐릭터 claim, Gateway·trusted admission·C `login/create_ply`, 모바일 UX, TDD/E2E 검증

이 문서는 현재 구현을 기준으로 다음 단계의 온보딩 계약을 고정한다. 지금 배포된 경로는 **Supabase 계정 로그인 → 이미 `active`인 소유 캐릭터 선택 → Gateway lease → C trusted admission**까지만 지원한다. 신규 provisioning과 기존 캐릭터 claim은 아직 구현하지 않으며, 아래의 `추가 구현` 항목을 완료한 뒤에만 운영으로 연다.

## 1. 현재 구현의 사실과 경계

### 1.1 웹과 Supabase

현재 [AuthGate](../../web/components/auth-gate.tsx#L12)는 Supabase email/password `signInWithPassword`와 `signUp`을 호출한다. 회원가입 결과에 session이 없으면 확인 메일을 안내하며, 이 단계의 비밀번호는 브라우저의 게임 터미널로 보내지 않는다. [Supabase client](../../web/lib/supabase.ts#L7)는 `persistSession`과 token auto-refresh를 켜지만 service-role key를 사용하지 않는다.

[Character roster](../../web/lib/character-roster.ts#L109)는 `game_characters`에서 `owner_user_id = userId`와 `lifecycle = active`만 조회한다. 브라우저는 `select`만 하며, `characterId`를 선택했을 때에도 소유권을 증명하지 못한다. Gateway가 같은 transaction의 service-only begin RPC 결과를 다시 검증해야 한다. 목록이 비어 있으면 현재 UI는 “운영자가 캐릭터를 연결하거나 claim 단계를 열어야 한다”고 안내하고, 신규 캐릭터 버튼은 없다([CharacterRoster](../../web/components/character-roster.tsx#L46)).

현재 migration의 권위 분리는 다음과 같다([game identity migration](../../supabase/migrations/20260902000000_game_identity.sql#L55)).

| 대상 | 권위 | 현재 브라우저 권한 |
|---|---|---|
| 웹 사람 계정·JWT | `auth.users` | Supabase Auth API |
| 계정 ↔ 캐릭터 소유권·lifecycle | `public.game_characters` | 자기 `active` row SELECT |
| online lease | `private.game_character_sessions` | 없음 |
| 레벨·장비·위치·세계 | C의 legacy player/world 파일 | 없음 |
| audit·claim request | `game_identity_events`, private request row | 없음 |

현재 migration에는 `imported_unclaimed`, `claiming`, `provisioning`, `active`, `suspended`, `retired` lifecycle enum이 있지만 provisioning intent 생성·예약·finalize RPC는 없다. 현재 존재하는 `claim_legacy_game_character(world, name_key, actor, correlation)`은 **C가 이미 검증한 뒤** Gateway가 호출하는 좁은 RPC이며, 인자로 password/proof/JWT/ticket을 받지 않는다([claim RPC](../../supabase/migrations/20260902000000_game_identity.sql#L397)).

### 1.2 Gateway와 현재 trusted admission

현재 브라우저는 선택한 캐릭터가 있을 때에만 `muhan.v1` WebSocket을 열고, 첫 text frame으로 정확히 다음 객체를 보낸다([Gateway contract](../../web/lib/gateway-contract.ts#L1), [terminal](../../web/components/mud-terminal.tsx#L263)).

```json
{"type":"auth","accessToken":"<Supabase JWT>","characterId":"<lowercase UUID>"}
```

Gateway state는 다음 순서다.

```text
awaiting-auth
  → connecting
  → awaiting-admission
  → ready
  → closed
```

`awaiting-auth`에서는 JWT의 서명, issuer, audience, expiry, subject를 확인하고, service-role 전용 `begin_game_character_session`으로 owner/active/canonical name을 한 번에 확인한다. 이때까지 C TCP 연결을 만들지 않는다. 성공하면 동일한 Gateway instance와 session UUID로 최대 120초 lease를 열고, 15초 이내 HMAC ticket을 만들어 C에 보낸다([Gateway session](../../services/gateway/src/gateway.ts#L280)).

Gateway는 C의 `MUD1 OK\n`을 완전히 받은 뒤에만 `{"type":"ready"}`를 브라우저로 보내고, 그 뒤의 C output만 binary WebSocket frame으로 relay한다([admission preface](../../services/gateway/src/gateway.ts#L398)). Telnet `WILL/WONT ECHO`는 Gateway가 소비하여 `echo` control로 바꾼다([Telnet parser](../../services/gateway/src/telnet.ts#L1)).

현재 입력·수명 제한은 다음과 같다.

- WebSocket max frame: 기본 16 KiB
- 입력 byte rate: 기본 4,096 B/s
- browser output buffer: 기본 1 MiB, 초과 시 slow consumer 종료
- 인증 timeout: 기본 10초
- C TCP connect/admission timeout: 기본 각 5초
- lease: 120초, 60초마다 갱신, 최소 30초가 남지 않으면 종료
- 연결 종료 시 exact session/Gateway만 release하고 50ms, 200ms의 bounded retry
- retry 가능한 WebSocket close에는 750ms 지수 backoff와 최대 30초, 15% jitter를 사용

### 1.3 C의 legacy login/create 경로

`src/io.c`의 trusted mode에서는 banner, name prompt, site password prompt를 보내지 않고 곧바로 `trusted_admission_login`에 들어간다([init_connect](../../src/io.c#L431)). HMAC ticket 검증 뒤 C는 canonical player 파일을 preflight하고, 같은 이름의 기존 session을 disconnect한 다음 authoritative 재load → `init_ply` → non-persisted `extra`에 user/character/nonce 기록 → `MUD1 OK\n` 순으로 진행한다([trusted admission login](../../src/command1.c#L246)).

legacy mode의 신규 가입은 다음 prompt state를 가진다([`login`](../../src/command1.c#L41), [`create_ply`](../../src/command1.c#L320)).

```text
banner
  → [엔터]
  → 이름
  → "생성하시겠습니까? (예/아니오)"
  → [엔터]
  → 성별: 남자/여자
  → 직업: 1..8
  → 능력치: 5개, 각 3..18, 합계 ≤ 54
  → 익숙한 무기: 1..5
  → 성향: 선함/악함
  → 종족: 1..8
  → 새 암호: 3..14 bytes
  → save_ply
  → command
```

성별·성향·종족·이름은 한국어 UTF-8 prefix를 비교하고, 이름은 최대 12 code point/14 bytes 및 `/`, `\\`, `:`, control, `.`/`..`를 거부한다([player path validation](../../src/player_path.c#L170)). 이름 파일은 canonical name의 UTF-8 bytes SHA-1 첫 byte를 lowercase hex 두 글자로 한 shard에 저장한다([path](../../src/player_path.c#L113)). 최종 player struct에는 `char password[15]`가 있고, 기존 저장 포맷은 이 값을 plaintext로 보존한다([creature](../../src/mstruct.h#L155)). 이 사실은 “웹이 비밀번호를 영속화하지 않는다”와 별개로 사용자에게 명시해야 한다.

### 1.4 trusted admission wire와 저장되지 않는 값

C trusted admission의 private line은 다음과 같다([wire contract](../web-mud/trusted-admission.md), [parser](../../src/trusted_admission.c#L366)).

```text
MUD1|<exp>|<nonce32hex>|<user_uuid>|<character_uuid>|<name_utf8_hex>|<hmac64hex>\n
```

secret은 32~512 printable ASCII bytes이고, C는 missing/invalid secret 또는 `MUD_REQUIRE_TRUSTED_ADMISSION=1` 위반 시 listener bind 전에 종료한다. nonce는 process-local replay cache에서 성공 검증 후 소비되며, ticket은 최대 15초다. user UUID, character UUID, nonce는 `extra`에만 두고 `creature` 파일에는 기록하지 않는다. password, proof, JWT, ticket 전문은 DB column/event, Gateway log, test evidence에 넣지 않는다([migration safety check](../../supabase/migrations/20260902000000_game_identity.sql#L191)).

## 2. 목표 아키텍처와 불변조건

### 2.1 소유권과 게임 상태를 분리한다

```text
Supabase Auth JWT
       │
       ▼
Gateway: actor/intent/lease/rate-limit
       │ private control
       ├─ C claim verifier: legacy file password compare once
       └─ C provision session: real login/create_ply wizard
       │
       ├─ Supabase ownership RPC: reservation/claim/finalize
       └─ C PlayerStore: legacy game-state authority
```

다음은 반드시 지킨다.

1. browser는 Supabase table write, claim RPC, private schema, MUD TCP에 접근하지 않는다.
2. `characterId`/`intentId`만으로 권한을 만들지 않는다. JWT subject와 DB RPC의 actor가 일치해야 한다.
3. provisioning은 이름 reservation을 먼저 확보한 뒤 C가 실제 wizard를 실행한다. DB active 전에는 roster에 노출하지 않는다.
4. C save 성공과 DB active 전환은 각각 idempotent correlation ID로 묶는다. DB finalize 실패가 새 캐릭터 재생성으로 이어지지 않는다.
5. 기존 claim password는 C process memory에서 한 번 비교하고 폐기한다. Gateway는 retry 시 password를 자동 재전송하지 않는다.
6. 새 게임 password는 현재 legacy raw struct 포맷 때문에 C player 파일에 저장된다. 이것을 “비영속”이라고 표시하지 않으며, hash/암호화 저장은 별도 저장 포맷 migration으로 분리한다.
7. 모든 reconnect는 fresh JWT와 fresh private admission ticket을 사용한다. URL query, localStorage, telemetry, analytics, error, audit에 token/password를 넣지 않는다.

### 2.2 UI가 보여 주는 상태와 내부 protocol state를 분리한다

사용자는 “계정 확인 → 캐릭터 연결 → 세계 입장” 세 단계만 이해하면 된다. 상세 state는 control frame과 test log에만 둔다. 오류는 이름 존재 여부나 소유 여부를 추측할 수 없도록 claim 실패를 한 문장으로 합친다.

```text
signed-out
  → auth-pending
  → authenticated
      ├─ active roster → character-selected → game-ready
      ├─ empty roster → provisioning-intent → provisioning-terminal
      └─ unclaimed legacy → claim-intent → claim-verify → claimed → roster refresh
```

## 3. 신규 provisioning 설계

### 3.1 사용자 경험

로그인 직후 roster가 비어 있으면 `새 캐릭터 만들기`와 `기존 캐릭터 연결` 두 action을 보여 준다. 둘 다 별도 설명 화면을 한 번 거친 뒤 xterm을 열며, 빈 roster 상태에서 바로 기존 `muhan.v1` active-character socket을 만들지 않는다.

안내 문구는 다음 사실을 포함한다.

- 생성 과정은 기존 텔넷 가입 화면을 그대로 사용한다.
- 게임 내 이름, 성별, 직업, 능력치, 무기, 성향, 종족은 xterm prompt에 답한다.
- 새 게임 password는 C의 기존 player 파일 포맷에 저장되며, 웹 계정 password와 다른 값으로 사용할 것을 권한다.
- 브라우저 새로고침/네트워크 단절 때 password prompt는 자동 재전송하지 않는다.
- 최종 저장 확인 전에는 캐릭터가 계정 목록에 나타나지 않는다.

### 3.2 DB 상태와 RPC

기존 migration을 수정하지 말고 후속 migration에서 다음 private/public API를 추가한다. 이름은 제안이며 구현 전에 eng review로 확정한다.

| RPC | 권한 | 핵심 입력 | 결과·전이 |
|---|---|---|---|
| `begin_game_character_provisioning` | service_role only | actor, world, canonical name, correlation, expiry | 이름을 row-lock/unique 검사 후 `provisioning` 예약 |
| `renew_game_character_provisioning` | service_role only | intent, actor, correlation, expiry | 같은 intent만 expiry 연장 |
| `finalize_game_character_provisioning` | service_role only | intent, actor, correlation, saved file SHA-256/format | `provisioning → active`, audit 1회 |
| `abort_game_character_provisioning` | service_role only | intent, actor, correlation, reason code | 파일이 확실히 없을 때만 reservation 만료/해제 |
| `reconcile_game_character_provisioning` | service/operator only | intent/name/file fingerprint | crash window의 saved file을 active로 finalize하거나 quarantine |

`game_characters` row에는 `provisioning_intent_id`, `reservation_expires_at`, `provisioning_correlation_id`, `saved_file_sha256` 같은 metadata만 둔다. password, terminal transcript, JWT, HMAC ticket은 절대 두지 않는다. `provisioning` row는 browser SELECT 대상이 아니며, owner가 있더라도 `active`가 될 때까지 roster 정책에서 제외한다.

예약 RPC는 같은 `(world_id, legacy_name_key)`를 `FOR UPDATE`로 직렬화한다. canonicalizer는 C의 ASCII-only `lowercize(name, 1)`와 동일해야 하며, non-ASCII bytes는 그대로 둔다. reservation expiry는 예를 들어 10분으로 시작하되, active session/long-running create에는 의존하지 않는다. intent가 만료되면 C socket을 닫고 새 입력을 받지 않는다.

### 3.3 provisioning protocol

현재 `muhan.v1` auth frame은 `characterId`가 필수이므로 provisioning에 재사용하지 않는다. 새 subprotocol을 `muhan.onboarding.v1`로 만들고 public WebSocket path는 `/onboarding`으로 분리한다.

첫 frame:

```json
{"type":"provision-auth","accessToken":"<Supabase JWT>","intentId":"<lowercase UUID>"}
```

Gateway state machine:

```text
awaiting-provision-auth
  → verifying-jwt
  → reserving/validating-intent
  → connecting-c
  → awaiting-provision-admission
  → legacy-create
  → awaiting-save-ack
  → finalizing-ownership
  → ready
```

필수 guard:

- 첫 frame은 text JSON 하나이고 unknown key를 거부한다.
- JWT subject가 intent owner와 다르면 C 연결을 만들지 않는다.
- intent의 `world`, canonical name, expiry, correlation을 service RPC 결과와 다시 비교한다.
- C 연결 전용 ticket은 `MUD1` active admission과 별개인 `MUD1P` private ticket으로 만든다. actor/intent/name/expiry를 서명하되 password/JWT 전문은 넣지 않는다.
- C가 `MUD1P OK\n`을 보낸 뒤에만 기존 banner와 prompt를 relay한다. 이 ACK 전에는 browser game bytes를 보내지 않는다.
- C가 보내는 기존 Telnet echo 협상은 현재 parser와 같은 방식으로 소비한다. password prompt에서 browser local echo를 끈다.
- C가 create flow를 끝내고 **내부 structured save ACK**를 보내야 한다. 한국어 문장이나 prompt substring을 파싱해 저장 성공으로 추측하지 않는다. 권장 control은 `MUD1P SAVED|<intent>|<sha256>|1\n`이며 private C↔Gateway control로만 처리한다.

### 3.4 C provisioning adapter

현재 C는 trusted mode에서 `create_ply`로 진입하지 않고, `create_ply`에도 provisioning intent/DB callback이 없다. 따라서 다음을 추가 구현해야 한다.

1. private ticket 검증 후에만 provisioning session을 만든다. public MUD port나 legacy fallback을 다시 열지 않는다.
2. reservation의 canonical name을 C create context에 고정한다. browser가 이후 name prompt를 바꾸거나 다른 파일을 만들지 못하게 한다.
3. `login`의 `[엔터]`와 “예” 분기를 그대로 사용해 `create_ply(1)`로 들어간다. wizard 자체는 gender → class → stats → weapon → alignment → race → password 순서를 유지한다.
4. create 완료 직전 `save_ply`가 `PLAYER_STORE_OK`인지 확인하고, 성공 시에만 file hash/format을 내부 ACK로 보낸다. 현재 save 실패 시 command state로 돌아가는 동작은 provisioning에는 부족하다. 실패하면 `provisioning`을 유지한 채 socket을 닫는다.
5. C가 내부적으로 intent ID를 player struct에 쓰지 않는다. 현재 `extra`는 session memory이고 `creature`는 raw file 포맷이므로, correlation은 private control/journal/DB metadata로만 관리한다.
6. save 성공 뒤 browser에는 일반 command prompt를 보낼 수 있지만, Gateway는 DB finalize가 성공하기 전 `ready`를 보내지 않는다. finalize 실패 시 해당 연결을 닫고 recovery state를 표시한다.

### 3.5 crash window와 recovery

가장 위험한 순서는 C file save와 DB finalize 사이의 process crash다. 이를 “새로 만들기 재시도”로 처리하면 같은 이름/두 파일/소유권 mismatch가 생긴다.

```text
reservation ── C save 실패 ──> provisioning 유지 또는 만료
reservation ── C save 성공 ── DB finalize 성공 ──> active
reservation ── C save 성공 ── Gateway/DB crash ──> reconcile by intent + sha256
```

reconcile worker는 `provisioning` row, intent correlation, exact canonical file path, file SHA-256, storage format을 확인한다. 일치하면 같은 finalize correlation을 재시도하고, 불일치하거나 이름이 이미 active이면 파일을 quarantine하여 운영자가 판단한다. reservation을 해제하기 전에 파일 존재를 확인하며, 무조건 delete하지 않는다. legacy player file은 파괴적 rollback 대상이 아니다.

현재 player save는 atomic rename과 bounded RAM recovery queue를 사용하지만 crash-safe durable spool은 없다([identity refactor](../web-mud/game-identity-refactor.md#플레이어-저장-경계)). 따라서 provisioning release 전에 서버 restart·save failure·disk full 시나리오를 실제 C로 검증해야 한다.

## 4. 기존 캐릭터 claim 설계

### 4.1 claim의 목적과 노출 경계

claim은 이미 `player/<shard>/<canonical name>` 파일이 있지만 `game_characters.owner_user_id`가 비어 있는 캐릭터를 현재 Supabase user에 한 번 연결하는 흐름이다. 성공 뒤에도 C player file의 level/inventory/password는 그대로이며, DB는 ownership index만 만든다.

Claim UI는 “기존 텔넷 이름과 게임 password를 한 번 입력해 연결”이라고 설명한다. 웹 계정 password와 게임 password를 혼동시키지 않는다. 이름 존재 여부, 다른 계정 소유 여부, wrong password 여부를 서로 다른 error로 표시하지 않는다.

### 4.2 C가 한 번 검증하는 private verifier

현재 `trusted_admission_login`은 owner-checked active character를 password 없이 여는 경로이고, `login`의 password compare는 public legacy login state에 있다([password compare](../../src/command1.c#L168)). 운영에서는 `MUD_REQUIRE_TRUSTED_ADMISSION=1` 때문에 public legacy login으로 claim을 처리할 수 없다. 새 claim verifier는 별도의 private Unix socket 또는 Gateway↔C private control channel이어야 한다.

제안 sequence:

```text
Browser: claim name + old game password (memory only, password no-echo)
  → Gateway: verify Supabase JWT, validate name syntax, make claim correlation
  → C private verifier: canonicalize/load file/strcmp password exactly once
  → C: CLAIM_OK or CLAIM_DENIED, then wipe input buffers and close
  → Gateway: consume one-time result, call claim_legacy_game_character RPC
  → Supabase: imported_unclaimed → claiming → active + one audit event
  → Browser: roster refresh, no password replay
```

C response는 browser에 relay하지 않는다. `CLAIM_OK`는 private request ID, canonical name, file fingerprint, one-time nonce로 Gateway에만 반환한다. C는 verifier process-local replay set에서 성공 request를 소비하고, Gateway도 correlation을 consumed 상태로 바꾼다. DB RPC는 password/proof를 받지 않고 `world`, `name_key`, JWT subject, same correlation만 받는다.

검증 규칙:

- 이름은 C canonicalizer와 shared test vector로 일치시킨다. 입력 이름을 DB lookup에 그대로 쓰지 않는다.
- file load 결과 `NOT_FOUND`, `CORRUPT`, `IO_ERROR`, wrong password를 모두 사용자에게 같은 “연결할 수 없습니다”로 축약한다. 운영 audit에는 reason code만 남기고 password는 남기지 않는다.
- `strcmp`는 C가 정확히 한 번 수행한다. Gateway나 Supabase는 player file을 읽거나 password를 비교하지 않는다.
- 성공한 verifier nonce는 재사용하지 않는다. DB retry는 password 재검증이 아니라 같은 correlation으로 claim RPC만 재시도한다.
- claim RPC는 현재 구현처럼 row lock과 request PK로 경쟁 claim을 직렬화한다([claim lock](../../supabase/migrations/20260902000000_game_identity.sql#L429)). 먼저 성공한 계정만 active가 되고 다른 계정은 generic conflict를 받는다.
- claim 성공 전까지 `begin_game_character_session`과 trusted admission을 열지 않는다. 성공 후 active roster refresh 뒤 현재 normal WSS path를 사용한다.

### 4.3 claim state machine

```text
idle
  → claim-authenticated
  → awaiting-legacy-name
  → awaiting-legacy-password
  → c-verifying
  → c-verified
  → db-claiming
  → claimed

wrong/expired/rate-limited/conflict/db-timeout
  → failed (password cleared, no automatic password retry)
```

`c-verified`는 짧은 memory-only state이며 최대 30초 안에 DB claim을 끝내지 못하면 폐기한다. browser reconnect 뒤에는 새 claim attempt를 만들고 password를 다시 직접 입력하게 한다. 성공 claim 뒤 같은 name은 다시 claim action이 아니라 roster에 나타나는 active character다.

## 5. rate limit, retry, recovery

아래 수치는 초기 운영 제안이며 구현 시 abuse test로 조정한다. 모든 bucket은 actor UUID, canonical name hash, source IP를 분리해 갖고, raw name/password를 key나 log에 넣지 않는다.

| 대상 | 시작 제한 | 초과 시 |
|---|---:|---|
| Supabase auth 실패 | IP당 10회/분, actor당 10회/15분 | 429와 점진적 지연 |
| claim password attempt | actor당 5회/15분, name당 5회/15분, IP당 20회/시간 | generic failure, password 폐기 |
| provisioning intent | actor당 3회/시간, active intent 1개 | 기존 intent resume 또는 generic reject |
| onboarding frame | 기존 Gateway 16 KiB/4,096 B/s | close 1008/1013, 입력 폐기 |
| C create idle | prompt 간 5분, 전체 20분 | reservation 유지 후 recovery/release |
| active Gateway | config 기본 200 connections, output 1 MiB | slow consumer 종료 |

retry 정책:

- JWT/Auth malformed, name invalid, wrong password, ownership conflict은 자동 retry하지 않는다.
- Supabase transient 5xx/network error는 Gateway가 동일 correlation으로 250ms, 1s, 3s, 최대 3회만 RPC retry한다. password와 C verifier를 재시도하지 않는다.
- C TCP connect/ACK failure는 새 private ticket으로 한 번 재연결할 수 있으나, provisioning wizard의 입력 replay는 하지 않는다. 사용자가 현재 prompt를 확인하고 다시 입력한다.
- lease renew는 현재 60초 interval을 유지한다. renew 실패 시 active game을 fail closed하고 exact release를 bounded retry한다.
- browser close 1011/1012/1013은 jitter backoff로 reconnect하되, 1000/1008/4001/440x는 자동 reconnect하지 않는다([close policy](../../web/lib/gateway-contract.ts#L24)). provisioning intent가 아직 유효하면 `intentId`만 resume에 쓰고 JWT는 새로 얻는다.

recovery UX:

- `reservation expired`: “생성 시간이 지나 캐릭터를 예약 해제했습니다. 처음부터 다시 시작하세요.”
- `save pending`: “캐릭터 저장 확인 중입니다. 같은 이름으로 다시 만들지 마세요.”
- `manual recovery`: “저장은 확인됐지만 계정 연결이 지연되고 있습니다. 잠시 후 캐릭터 목록을 다시 불러오세요.”
- `claim conflict`: “이 캐릭터는 연결할 수 없습니다. 다른 캐릭터를 선택하세요.”
- `session reconnect`: “입력한 비밀번호는 다시 보내지 않았습니다. 현재 화면의 질문부터 이어 가세요.”

운영자는 `intent`, `correlation`, owner UUID, lifecycle, file hash, reason code만 조회한다. password, proof, JWT, HMAC ticket, terminal transcript를 복구 자료로 요구하지 않는다.

## 6. secret non-persistence 계약

### 6.1 claim old password

- browser React state와 DOM input에 머무는 동안만 존재하며 `localStorage`, `sessionStorage`, IndexedDB, URL, cookie, query, telemetry에 쓰지 않는다.
- Gateway는 request scope의 bounded buffer 하나로만 받으며 C response 직후 zeroize한다. access log, structured log, error message, metrics label, tracing attribute에 넣지 않는다.
- C verifier는 compare 후 input buffer와 temporary copy를 zeroize하고 process 종료/close 시에도 clear한다. C standard library나 core dump가 값을 남길 수 있으므로 onboarding/verifier process는 core dump를 끄고 secret-bearing memory를 dump에서 제외한다.
- browser가 연결을 잃으면 old password를 다시 보내지 않는다. retry는 새 사용자 입력이다.
- test harness transcript는 streaming redactor로 split secret도 가리고, artifact write 전 `assert_artifact_safe`를 수행한다. 기존 harness의 패턴([run_scenario](../../tests/harness/run_scenario.py#L120), [admission scenario](../../tests/harness/run_admission_scenario.py#L48))을 재사용한다.

### 6.2 Supabase/web password와 game password

Supabase Auth password는 Supabase Auth가 관리하며 Gateway/C로 전달하지 않는다. claim old password와 달리 신규 provisioning에서 사용자가 C prompt에 입력하는 **게임 password는 현재 C raw `creature` 파일의 `password[15]`에 저장된다**. 따라서 다음 중 하나를 선택하기 전까지는 보안 약속을 과장하지 않는다.

1. 단기: 게임 password가 legacy file에 저장된다는 경고와 웹 계정 password 재사용 금지 안내를 제공한다.
2. 후속: versioned player format, password hash/KDF, C login migration, recovery/reset path를 함께 설계한다.

Admission secret은 deployment secret mount에만 두고 브라우저 env/build/static bundle에 넣지 않는다. HMAC ticket은 private hop에서만 사용하며, ticket 전문을 browser control, DB, logs, reconnect storage에 남기지 않는다([runtime policy](../web-mud/runtime-deployment.md#network-and-secret-policy)).

## 7. 모바일 UX 설계

현재 [MudTerminal](../../web/components/mud-terminal.tsx#L350)은 xterm 아래에 mobile command bar를 고정하고, `localEcho=false`일 때 input `type=password`를 사용한다. 이 기반을 provisioning/claim에도 적용한다.

- 390×844에서 terminal viewport와 command bar가 한 화면 안에 있고, `env(safe-area-inset-bottom)`과 soft keyboard resize를 반영한다.
- terminal을 탭해도 focus를 회복하며, 별도 command input은 IME 조합 중 Enter를 submit으로 가로채지 않는다. `compositionstart/compositionend`를 추적한 뒤 조합 완료 Enter만 전송한다.
- prompt를 보고 답하는 기존 흐름을 유지하되, 상단에 `성별 1/7`, `직업 2/7` 같은 비밀 없는 진행 상태를 표시한다. 실제 C prompt가 authoritative이므로 progress가 prompt를 대신하지 않는다.
- 직업/무기/종족은 quick chip을 제공할 수 있지만, chip은 동일한 ASCII 입력을 보내는 convenience layer이며 C validation을 우회하지 않는다. 능력치는 숫자 keyboard와 합계 preview를 제공하되 최종 검증은 C가 한다.
- password prompt에서는 터미널 local echo와 mobile input echo를 모두 끄고, 입력 text를 React debug/log/error boundary에 넣지 않는다. submit 뒤 input value를 즉시 clear한다.
- 화면 회전과 백그라운드 전환은 password를 보존하지 않는다. provisioning intent만 살아 있으면 reconnect 후 현재 단계의 prompt를 새로 표시하고 사용자가 다시 입력한다.
- `screenReaderMode: true`, 명시적 label, `aria-live` status를 유지한다. 색만으로 연결/저장/실패를 구분하지 않는다.
- 키보드가 terminal을 가릴 때는 “터미널로 돌아가기” 버튼을 제공하고, 자동 scroll은 사용자가 과거 transcript를 읽고 있을 때 끈다.
- `prefers-reduced-motion`에서는 reconnect spinner를 정지형 상태 문구로 보여 준다. 네트워크 단절 뒤 countdown이 끝나기 전에 중복 submit을 받지 않는다.

데스크톱은 xterm 중심의 단일 흐름을 유지하고, 모바일은 xterm + 고정 command bar를 primary interaction으로 삼는다. onboarding을 일반 HTML form으로 재작성하면 C의 prompt/error 경로와 drift하므로 사용자가 실제 텔넷 가입 경험을 보도록 한다.

## 8. TDD 실행 순서

각 단계는 앞 단계의 계약이 통과한 뒤에만 다음 단계로 진행한다. 테스트에는 deterministic clock, fixed UUID/nonce, disposable `MUHAN_HOME`, redacted artifacts를 사용한다.

### 8.1 순수 unit

1. `canonicalLegacyName`: ASCII fold, first-byte capitalization, UTF-8 byte/codepoint limits, reserved/path/control rejection, C/TypeScript/SQL 동일 vector.
2. lifecycle transition table: `imported_unclaimed → claiming → active`, `provisioning → active`, expiry/abort/reconcile의 허용·금지 전이.
3. protocol parser: exact first frame keys, max bytes, malformed JSON, unknown type, fragmented `MUD1P OK/SAVED`, binary-before-ready rejection.
4. rate limiter: actor/name/IP buckets, clock refill, retry-after, no raw secret in key/log.
5. secret scrubber/redactor: whole and split password/secret/token matches, error/JSON/transcript artifact self-check.
6. mobile input adapter: IME composition, Enter, backspace, password clear, no automatic replay.

### 8.2 C unit

1. private provisioning ticket HMAC/expiry/nonce/replay parser, including missing/invalid secret fail-closed.
2. claim verifier: valid password exactly once, wrong password, missing/corrupt/IO error all generic, buffers cleared after return.
3. create adapter: canonical reserved name cannot change, each `create_ply` step reaches the next state, invalid stats/job/race/password remains in the same step.
4. save ACK: only `PLAYER_STORE_OK` emits `MUD1P SAVED`; partial write, fsync, rename, disk full, recovery queue produce no active ACK.
5. real player file remains compatible and new session metadata is not serialized into `creature`.

기존 C unit은 [Makefile unit targets](../../src/Makefile#L65)의 naming/build pattern을 따르되, 새 test binary에 production database/network secret을 주입하지 않는다.

### 8.3 Gateway unit/integration

1. auth-first: no C TCP before valid JWT + owner-checked intent/claim result.
2. provisioning auth rejects active-character frame confusion, wrong actor, expired intent, duplicate intent, unknown fields.
3. claim input is sent only to private verifier; DB RPC body has world/name/actor/correlation and never password/proof/JWT/ticket.
4. one verifier result → one claim RPC; same correlation retry is idempotent, second actor/correlation is rejected.
5. provisioning waits for fragmented/coalesced `MUD1P OK`, `MUD1P SAVED`; game bytes before required ACK are rejected.
6. finalization failure closes browser session, retains recovery metadata, and never starts a duplicate C create.
7. lease begin/renew/end exact owner/session/Gateway checks; stale release cannot delete replacement lease.
8. input/output limits, slow consumer, auth timeout, C timeout, DB timeout, close-code retry matrix, bounded release/finalize retries.

기존 계약 테스트([gateway integration](../../services/gateway/test/gateway.integration.test.ts#L137), [authorizer](../../services/gateway/test/character-authorizer.test.ts#L23), [Telnet](../../services/gateway/test/telnet.test.ts#L5))를 깨지 않고 onboarding subprotocol 테스트를 별도 suite로 둔다.

### 8.4 PostgreSQL contract/integration

disposable PostgreSQL에서 migration과 contract를 실행한다.

- provisioning reservation은 같은 world/name 경쟁에서 정확히 한 row만 성공한다.
- owner가 아닌 actor, expired intent, replayed correlation, duplicate finalize는 실패한다.
- finalize 동일 correlation 재시도는 같은 character/active row/audit 1건을 반환한다.
- crash window의 provisioning + saved hash만 reconcile 가능하고, 다른 hash/name은 quarantine된다.
- browser role은 provisioning/private table/RPC를 보지 못한다. active owner만 roster SELECT가 된다.
- service_role은 직접 table CRUD 없이 narrow RPC만 실행한다.
- claim A/B 경쟁은 exact one winner, one audit, one active owner를 만든다.
- audit details의 nested key에 password/proof/JWT/token/ticket/secret/authorization/bearer가 있으면 거부한다.
- rollback은 rows를 unclaim/delete하지 않고 image/chart 이전 버전과 recovery/reconcile로 처리한다.

기존 [game identity contract](../../supabase/tests/game_identity_contract.sql#L297)를 fixture seed와 보완하고, request/correlation 및 file fingerprint 검증을 같은 one-shot DB에서 확인한다.

### 8.5 실제 C integration

`MUHAN_HOME` disposable fixture에서 current-source binary를 강제 재빌드한다.

1. private provisioning ticket으로 C에 접속한다. public banner/legacy site-password fallback이 노출되지 않는지 확인한다.
2. Gateway/C private ACK 뒤 실제 banner, `[엔터]`, 고정 이름, “예”, gender/class/stats/weapon/alignment/race/password를 순서대로 전송한다.
3. `MUD1P SAVED` 뒤 player path, shard, size, SHA-256을 확인하고 password는 transcript/log/artifact에 없는지 확인한다.
4. DB finalize 후 active trusted admission으로 같은 character를 relogin하고 `건강`, `끝` 명령의 binary relay와 lease release를 확인한다.
5. 각 create step의 invalid input, UTF-8 boundary, duplicate reserved name, disk full/rename/fsync failure를 확인한다.
6. C kill을 save와 finalize 사이 각각 주입하고 reconcile이 duplicate create 없이 같은 file을 finalize하는지 확인한다.
7. claim fixture는 실제 legacy player file을 만들고 private verifier에 old password를 한 번 전송한다. wrong password, replayed nonce, concurrent actor, DB failure 뒤 password 재전송 금지를 확인한다.

기존 [admission harness](../../tests/harness/run_admission_scenario.py#L1)와 [scenario harness](../../tests/harness/run_scenario.py#L640)의 redaction/provenance 규칙을 재사용한다. 테스트 password를 command line, environment snapshot, retained log에 넣지 않는다.

### 8.6 실제 Gateway + C integration

Node Gateway를 실제로 띄우고 disposable C와 연결한다.

- provisioning `muhan.onboarding.v1`과 active `muhan.v1`를 구분한다.
- C output을 split byte/Telnet echo로 보내고, `MUD1P OK/SAVED`와 game output coalescing을 검증한다.
- Gateway가 browser에 ready를 보내는 시점이 C save ACK와 DB finalize 뒤인지 확인한다.
- JWT expiry, lease renewal, C restart, Gateway drain, slow browser, DB unavailable 동안 password/secret/ticket이 어디에도 남지 않는지 확인한다.
- MUD `:4000`은 Gateway 외 source에서 연결되지 않는지 NetworkPolicy와 socket probe로 확인한다.

### 8.7 Playwright E2E

Playwright는 desktop 1440×1000, mobile 390×844를 모두 실행한다.

**신규 provisioning happy path**

1. 새 Supabase fixture account로 sign up/sign in한다.
2. empty roster와 `새 캐릭터 만들기` action이 보이고, active character WebSocket이 아직 열리지 않았는지 확인한다.
3. provisioning intent를 만들고 xterm에 실제 C banner/prompt가 나타나는지 확인한다.
4. mobile command bar/IME로 모든 create step을 입력하고 password no-echo를 확인한다.
5. 저장 ACK와 DB finalize 후 roster에 정확히 한 active row가 나타나는지 확인한다.
6. 게임 입장으로 전환한 뒤 `건강` binary output, `끝` normal close, exact lease release를 확인한다.

**claim happy/negative path**

1. unclaimed imported fixture가 empty roster에서는 숨겨지고 `기존 캐릭터 연결`만 보이는지 확인한다.
2. 기존 name/old password를 입력하고 성공 뒤 active roster 한 건과 game admission을 확인한다.
3. wrong password, expired attempt, competing account, duplicate click은 generic error를 보이고 password를 재전송하지 않는지 확인한다.

**failure/recovery path**

- C가 prompt 중 끊기면 retry 문구와 no-secret-replay를 확인한다.
- DB finalize가 5xx이면 duplicate create action이 잠기고 recovery 문구가 보인다.
- reload/back-forward/second tab은 같은 intent의 duplicate save를 만들지 않는다.
- JWT expiry는 4001/재로그인으로 처리하며 stale terminal input은 C로 보내지 않는다.
- slow output은 1013 또는 bounded close 후 reconnect 정책을 따른다.

**보안/접근성 assertions**

- `localStorage`, `sessionStorage`, URL, console, page error, network URL/body, Gateway test log, evidence JSON에 old/new password, service key, admission secret, full ticket이 없다.
- form label, xterm label, focus order, screen reader status, keyboard-only submit, contrast, reduced motion을 검사한다.
- viewport 높이가 1 화면을 넘어 늘어나지 않고 terminal/command bar가 soft keyboard와 겹치지 않는다.

## 9. 구현 순서와 출시 gate

다음 순서를 지키면 현재 active-character 경로를 보존한 채 점진적으로 포팅할 수 있다.

1. C canonicalizer와 private claim/provision control의 unit/contract를 먼저 만든다.
2. Supabase reservation/finalize/reconcile migration과 disposable PostgreSQL contract를 만든다.
3. Gateway onboarding subprotocol, memory-only secret handling, rate-limit/retry를 구현한다.
4. C `create_ply` adapter와 structured save ACK를 구현하고 실제 C integration을 통과시킨다.
5. 웹 empty roster action, claim/provision screens, mobile input/ARIA를 구현한다.
6. 실제 Gateway+C integration 후 Playwright desktop/mobile E2E를 실행한다.
7. staging에서 crash/restart/disk full/DB outage/lease expiry/competing claim을 재현한다.
8. 운영 enablement은 provisioning/claim feature flag를 켜고, 기존 active roster/admission 경로가 별도 회귀 suite를 통과한 뒤 진행한다.

출시 gate는 “signup 성공”이 아니다. 다음을 모두 만족해야 한다.

- 동일 이름을 두 계정이 동시에 provision/claim할 때 정확히 한 owner만 active다.
- C가 저장하기 전과 DB finalize 전에는 웹 roster와 trusted admission이 열리지 않는다.
- C/DB/Gateway crash 뒤 duplicate player file이나 orphan active owner가 없다.
- claim old password는 C가 한 번만 비교하고 retry/recovery에서 재전송되지 않는다.
- 새 game password가 legacy file에 저장된다는 안내와 웹 password 분리가 구현되어 있다.
- 실제 C binary, 실제 Gateway, disposable Supabase/Postgres, Playwright desktop/mobile이 모두 통과한다.
- 모든 증거 artifact가 redacted이며 source checkout이나 live 환경을 이 연구 단계에서 변경하지 않았다.

## 10. 현재 구현과 이 설계 사이의 gap 목록

| Gap | 현재 상태 | 필요한 변경 |
|---|---|---|
| empty roster에서 신규 action | 없음 | provisioning intent API/UI |
| provisioning DB RPC | lifecycle enum만 있음 | reservation/finalize/reconcile migration |
| C provisioning admission | trusted active load만 지원 | private `MUD1P` + create adapter + save ACK |
| C one-time claim verifier | 없음 | private control, exact compare, nonce consume, zeroize |
| Gateway onboarding state | active `muhan.v1`만 있음 | `/onboarding`, `muhan.onboarding.v1`, state/retry limits |
| claim UX | “운영자가 연결” 안내만 있음 | one-time name/password flow와 generic errors |
| recovery spool | process RAM bounded queue | provisioning journal/reconcile 정책 |
| legacy password storage | raw `creature.password[15]` | 단기 경고, 장기 KDF/versioned format |
| real Playwright onboarding | active roster/admission만 검증 | provisioning/claim/negative/mobile E2E |

이 gap이 해소되기 전에는 현재의 [MUD identity refactor](../web-mud/game-identity-refactor.md)가 정한 “단계 2는 후속” 판단을 유지한다. 이 문서는 그 단계의 구현 계약과 검증 순서를 제안하는 연구 기록이며 제품 코드나 live 환경 변경 기록이 아니다.
