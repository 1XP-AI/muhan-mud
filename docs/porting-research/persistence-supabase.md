# Terra 영속성 이관 연구: legacy C MUD → Supabase/Postgres

상태: 조사/설계 전용 (2026-09-01) · **제품 코드, migration, live DB, Kubernetes는 변경하지 않음**

## 결론과 경계

이 서버의 현재 게임 권위는 Postgres가 아니라 단일 Linux/amd64 C 프로세스와
`/home/muhan` 볼륨이다. 기존 `private.game_character_snapshots`는 migration의
주석대로 optional read model이며 gameplay source가 아니다
(`supabase/migrations/20260902000000_game_identity.sql:329-356`). 아래 설계는 이를
깨지 않고, 충분한 shadow/reconciliation 뒤에만 `private.mud_*`를 **새 canonical
권위**로 승격한다. 브라우저에는 어느 단계에서도 게임 상태 테이블 DML 권한을
주지 않으며, C MUD 또는 별도 trusted state service만 private RPC/직접 DB role을
사용한다.

현 상태에서 단일 writer는 이미 운영 불변조건이다. `load_rom` LRU flush와
`resave_rom`은 raw room 파일을 다시 쓰고 (`src/files2.c:64-136,229-258`),
로그아웃/주기 저장은 player file을 쓴다 (`src/player.c:741-750`,
`src/command8.c:717-805`). 같은 volume을 여러 MUD replica가 공유하면 파일도,
DB로 가는 legacy dual-write도 안전하지 않다. canonical DB cutover 전후 모두
world 당 active simulation writer는 정확히 하나여야 한다.

### 판단 기준

| 분류 | 의미 | 이관 처리 |
| --- | --- | --- |
| P0 canonical gameplay | 잃으면 캐릭터/경제/월드 결과가 바뀜 | 구조화하여 DB transaction에 포함 |
| P1 gameplay 보조 | 결과/접근권/사회 관계에 영향 | P0 뒤 동일 transaction 또는 durable event로 이관 |
| P2 운영·감사 | 게임 규칙 권위는 아니지만 보존·보안 가치 있음 | 별도 audit/config store; gameplay schema와 혼합 금지 |
| R 정적 resource | 런타임이 읽되 정상 플레이가 수정하지 않음 | checksum/versioned catalog로 import, 즉시 canonical화하지 않음 |
| T 임시 | 프로세스/세션 수명 | DB에 저장하지 않음 |

## 전수 파일 I/O와 durable state 분류

`mtype.h:42-49`가 legacy root (`ROOMPATH`, `MONPATH`, `OBJPATH`,
`PLAYERPATH`, `POSTPATH`, `LOGPATH`)를 정한다. `resource_path.c:329-394`의
`rp_fopen/open/stat/unlink`는 `MUHAN_HOME` 및 UTF-8 alias를 해석하지만,
모든 호출이 이 wrapper를 통과하지는 않는다. 따라서 아래 표의 “직접” 경계는
첫 이관 대상이다.

| 우선 | 실제 파일/형식 | 상태와 정확한 근거 | 현재 write/read 경계 | DB 목적지 / 판정 |
| --- | --- | --- | --- | --- |
| P0 | `rooms/r%02d/r%05d`, raw `room` 뒤 exit/monster/object/문자열 트리 | room의 이름·flags·random slots·`perm_mon/perm_obj` timer·방문수·설명·출구와 permanent 몬스터/아이템. 구조체는 `src/mstruct.h:130-166`, serializer/parser는 `src/files1.c:282-395,538-735`. | `load_rom`, LRU flush, `reload_rom`, `resave_rom`, `resave_all_rom` (`src/files2.c:64-287`); shutdown은 `resave_all_rom(1)` (`src/update.c:836-868`). `rename(file,file~)` 방식은 room 전체에 atomic temp+fsync가 아님. | `mud_rooms`, `mud_room_exits`, room item/monster instance, respawn timer. **P0** |
| P0 | `player/<sha1-first-byte>/<name>`, raw `creature` + recursive object tree | 이름/legacy password/직업·능력치·HP/MP·XP/gold·quests·room·daily/45 timer·inventory. `creature`: `src/mstruct.h:168-214`; `write_crt/read_crt`: `src/files1.c:152-280,465-536`. ready item은 copy에서 inventory로 풀어 저장한다 (`src/command8.c:717-805`). | `PlayerStore` seam (`src/player_store.c:1-41`), atomic temp→`fsync`→rename→parent fsync (`src/file_player_store.c:58-117`); path SHA-1 shard는 `src/player_path.c:115-149`. | existing `public.game_characters`와 1:1 `private.mud_character_state`, timer, item placement. password는 import하지 않고 legacy file에만 격리하다 별도 credential migration으로 제거. **P0** |
| P0 | player/room 안 object tree | object template copy의 mutable value, shots, enchant, flags 및 container 내 임의 깊이의 아이템. `write_obj/read_obj`가 parent pointer를 버리고 재구축한다 (`src/files1.c:76-149,398-463`). | player/room/bank serializer에 중첩; room `PERMONLY` 필터에는 `OPERMT` 규칙과 container 예외가 있다 (`src/files1.c:282-395`). | `mud_item_instances` + `mud_item_locations` (container adjacency와 ordering). **P0** |
| P0 | `player/bank/<name>`, raw root object tree | 은행 보관함과 모든 중첩 물건. | `load_bank/save_bank`는 direct truncate write이며 atomic하지 않다 (`src/bank.c:16-72`). 입출금은 player save와 bank save를 별도 호출한다 (예: `src/bank.c:192-193,265-267,384-386`). | `mud_bank_accounts` + 같은 item tables. 캐릭터 gold와 item 이동을 한 DB transaction에. **P0, 가장 높은 경제 위험** |
| P0 | board directory의 `board_index` binary + `board.<n>` text body | 게시물 번호, author/date/title/line/read counter/deletion, 본문. `BOARD_INDEX`와 directory map은 `src/board.c:18-64`. | read-modify-write index offset 및 body file rename을 별도로 수행 (`src/board.c:94-171,204-319,323-455`); source는 `MUDHOME/board`를 **직접** 사용하고 resource wrapper도 없다. | `mud_boards`, `mud_board_posts`, `mud_board_post_views`/counter. **P0** (write/delete/read increment 경합) |
| P1 | `post/<recipient>` append-only-ish text; `post/DM_pad`, `post/ISSUE` | 개인 우편, DM 메모, issue text. | `postedit`가 작성 줄마다 `O_APPEND` file에 즉시 write하여 동시 송신 시 섞일 수 있음을 소스가 명시 (`src/post.c:77-123`); read/delete는 `134-199`; DM/family news editor는 `204-389`; issue editor는 `src/command11.c:203-385`. | `mud_mail_messages`, `mud_notes` (append-only message ID, delivered/read/deleted timestamps). **P1**; mail body는 log/export 금지 |
| P1 | `player/alias/<name>` text | 최대 50 alias와 title. | login `init_alias` (`src/alias.c:31-88`), `save_alias`가 direct `O_TRUNC` (`232-259`). | `mud_character_aliases`, `mud_character_profile.title`. **P1** |
| P1 | `player/family/family_list`, `family_member_<n>`, `family_news_<n>` | family definition, leader, fee/gold, membership, news. creature flags/daily fields에도 family state가 중복된다. | `load_family` (`src/command11.c:484-505`), `edit_member` rewrite (`src/command12.c:265-335`), news append/delete (`src/post.c:299-389`). | `mud_families`, `mud_family_members`, `mud_family_announcements`; character family link FK. **P1** |
| P1 | `player/fal/<name>` | memo와 failed-login notice. | memo append (`src/command12.c:77-153`), `log_fl` appends failed-login notice (`src/misc.c:600-634`), login reads/deletes it (`src/player.c:248-253`). | `mud_character_memos` and security event table split. **P1/P2** |
| P1 | `player/vote/<name>_v`, `player/invite/invite_<n>` | vote/nomination and marriage-house invitation lists. | vote read/rewrite/delete `src/command11.c:260-389`; invite read/rewrite/delete `src/command12.c:363-451`, readers `command2.c:539-550, command6.c:280-287`. | `mud_votes`, `mud_room_invites`. **P1** |
| P1 | marriage는 player record fields | marriage flags와 spouse name이 creature `key[2]`에 encoding; old `player/marriage/*` I/O는 commented out. | live mutation `src/command11.c:1146-1240`; seeded `player/marriage/list`는 legacy/static. | `mud_character_relationships` (two ordered character IDs, unique active marriage). **P1** |
| P1 | `player/suic/*` archived player/alias/bank | deletion/archive consequence. | `suicide`가 disconnect 뒤 shell `mv` 실행 (`src/command5.c:680-735`); not transactional and injection-prone. | `game_characters.lifecycle`, archive metadata + immutable storage snapshot; in-place delete 금지. **P1** |
| R | `objmon/mNN`, `objmon/oNN` raw template files | monster/object prototype data; runtime copy는 `load_crt/load_obj` (`src/files2.c:385-463,470-550`). | normally read-only; DM authoring paths may write. copies include raw pointers which loader clears. | `mud_monster_templates`, `mud_item_templates`, revisioned import. **R until authoring migration** |
| R | `objmon/talk/*`, `objmon/ddesc/*`, sign/book assets; `help/*` | NPC talk rules and text help/content. | `load_crt_tlk` (`src/files3.c:256-317`); descriptions `src/creature.c:589-596`; help `src/command4.c:178-236`. | content version/catalog or object storage, not gameplay transaction. **R** |
| P1 | room exit schedule와 global clock | exit open/close depends on compiled `time_x`; mutable exit flag는 room save에 포함. `Time`, `Shutdown` globals: `src/global.c:17-18`. | `update_game` process-local cadence (`src/update.c:40-92`); `update_time` increments `Time` but does not persist it (`730-746`); `update_exit` toggles exits and process-local `t_toggle` (`941-983`, schedule static `src/update.h:10-11`). | `mud_world_clock`, `mud_scheduled_jobs`, `mud_exit_schedule_state`. **P0 after cutover**; current startup resets clock/toggle |
| P2 | `log/log*`, `news`, `DM_news`, `teleport`, `SUICIDE`, `all_cmd`, auth lookup/logs | audit/operations, news, record-all command stream, ident artifacts. | append helpers `src/misc.c:531-850`; `all_cmd` rename/delete `src/main.c:162-185, src/update.c:987-997`; auth `src/auth.c:77-81,201-213`, `src/io.c:1515-1534`. | append-only `mud_audit_events` with retention/object storage; auth lookup is **T**, sensitive, not imported. |
| P2 | `log/lockout` text | IP/password lockout configuration loaded at process boot. | `load_lockouts` (`src/misc.c:696-734`); DM control in `src/dm3.c:729+`. | `mud_access_lockouts` with encrypted/hashed secret treatment; separate security migration. |
| T | sockets, `iobuf`, `extra`, LRU queues, active monster/enemy/follower links, update cadence statics, gateway lease | runtime links/connection identity. `extra` explicitly non-saved (`src/mstruct.h:68-81`); readers clear pointers (`src/files1.c:407-414,474-488`). | memory only; character session lease already `private.game_character_sessions`. | no generic persistence; only deliberate timer/job/audit event. |

추가 direct boundary: `command1.c:785-801` reads `player/simul`,
`dm2.c:598-625` edits room files, `dm4.c:529+` views post directly. Network
`read/write` in `io.c`, `finger.c`, `auth.c` 등은 transport I/O라 durable
storage 분류에서 제외했다.

### Source-format hazards

1. Raw structs contain native sizes, padding, endianness, and stale process pointers.
   Converter must use deployed ABI, recursive framing, and drop pointers; never memcpy bytes to JSON.
   `read_crt/read_obj/read_rom` is executable specification.
2. `read_*` bounds some counts (`MAX_NESTED_OBJECTS=4096`, exits 200, mobs 4096,
   items 8192, descriptions 1 MiB) in `files1.c:10-17,398-735`; compressed
   `files3.c` parser is less defensive. Backfill uses bounded disk parser and quarantines malformed files.
3. Room persistence writes only permanent room contents on LRU/shutdown (`PERMONLY`);
   player save writes all inventory. Do not turn non-permanent spawns into durable DB rows.
4. Player identity uses exact UTF-8 bytes and SHA-1 first-byte shard, not SQL `lower()`.
   Reuse existing canonicalizer/constraints from `game_characters`.

## Target canonical schema

Create this only in a later reviewed migration. Names use non-public `private`;
`public.game_characters` remains ownership index. Every mutable aggregate has
`revision bigint not null`, `updated_at timestamptz`, and command-idempotency.
`jsonb` is allowed only as validated/versioned `legacy_extensions`, never raw C memory.

| Aggregate / table | Key columns and invariants |
| --- | --- |
| `private.mud_worlds`, `mud_world_state` | `world_id`; one state row with `game_hour`, `clock_anchor_at`, catalog version, revision. |
| `private.mud_rooms` | `(world_id, room_num)` PK; all room scalar/descriptive/respawn fields, revision. Player list is not stored. |
| `private.mud_room_exits` | UUID PK; unique source/name/destination tuple; flags/key/timer, revision; deferred room FKs. |
| `private.mud_monster_templates`, `mud_item_templates` | immutable `(world, legacy_index, catalog_revision)` source catalog with checksum/current revision. |
| `private.mud_monster_instances` | UUID, template ref, room FK, mutable creature fields/timers, `persistence_class` (`ephemeral`, `permanent`, `respawned`), revision. |
| `private.mud_character_state` | `character_id` PK/FK; normalized creature state, location, gold, flags/quests, `legacy_password_present boolean` only. `mud_character_daily_limits` and `mud_character_timers` use `(character_id, slot)` to round-trip 10/45 slots. |
| `private.mud_item_instances` | UUID instance, template ref, mutable attributes/flags, revision. |
| `private.mud_item_locations` | `item_id` PK and exactly one owner: character, room, monster, bank, or container item; `position` preserves list order; `equipped_slot` only with character. Deferred trigger rejects no/multiple owner, self/cyclic containers, invalid slot/world mismatch. |
| `private.mud_bank_accounts` | unique `(world_id, character_id)`, revision; items are location-owned by account. Currency stays character gold unless legacy proves otherwise. |
| `private.mud_boards`, `mud_board_posts`, `mud_board_post_views` | stable board key; post UUID and unique legacy sequence; body/title/tombstone; views idempotent by post/viewer. Counter is cache, not sole evidence. |
| `private.mud_mail_messages`, `mud_notes` | sender/recipient FK where known, body, created/read/deleted time, source checksum. Delete is tombstone/retention, not unlink. |
| `private.mud_families`, `mud_family_members`, `mud_family_announcements`, `mud_room_invites`, `mud_votes`, `mud_character_relationships` | FKs and unique active membership/relationship; family leader must be member. |
| `private.mud_scheduled_jobs`, `mud_job_runs` | unique `(world, job_key)`, next due, lease/run token/attempt, immutable run/event ID. Covers exit toggles/spawn/reset/shutdown intent. |
| `private.mud_commands`, `mud_state_events`, `mud_outbox`, `mud_projection_receipts`, `mud_import_ledger`, `mud_reconciliation_runs` | immutable command/event/control plane; unique `(world, command_id)`, receipt `(target,event_id)`, import source path/hash/ABI/quarantine reason. |

Enable RLS for all tables and revoke all from `anon`, `authenticated`, and
`service_role` by default. Browser retains only existing active-character ownership
read; it cannot directly select/mutate gameplay tables. A narrow state role/SECURITY
DEFINER RPC validates server-issued character lease and exposes operation-specific
functions (`move_item`, `bank_deposit`, `append_mail`, `save_character_snapshot`),
not table writes.

## Transaction, concurrency, and locking contract

A user command receives one durable UUID; a retry repeats it. A successful reply
means canonical transaction commit, not an async projector acceptance.

1. Validate session, command payload/version and expected revisions; insert
   `mud_commands(world_id,command_id,character_id,payload_hash,status)`. Same ID/hash
   returns recorded result; same ID/different hash rejects.
2. In a short transaction lock resources in this fixed order: character UUIDs sorted →
   family → bank UUIDs sorted → rooms `(world,room_num)` sorted → items UUID sorted →
   board post. Use `SELECT ... FOR UPDATE`. Trade locks both characters sorted.
   Never hold DB tx across editor/network/filesystem/long tick.
3. Validate item graph/capacity/equipment in deferred trigger; debit/credit and every
   item move atomically; increment revisions; append `mud_state_events` and
   `mud_outbox` before COMMIT. Thus bank withdraw cannot split bank and player save.
4. Standard commands use `READ COMMITTED` + locks/revisions. Multi-row settlement/
   transfer uses `SERIALIZABLE`, bounded retry, and same command ID. Lock timeout returns
   retriable busy, not partial action.
5. Job workers claim due rows with `FOR UPDATE SKIP LOCKED`, set bounded lease/run
   UUID and next due atomically, and record effect keyed by run UUID. A crash can retry
   but not duplicate logical effect.

Existing `game_character_sessions` is admission/single-login coordination, not
gameplay mutex. Multiple MUD writers need separate writer-epoch fencing and shared job
leases; that is out of scope.

## Phased implementation and dual-write/outbox

### A. Contract/exporter — files canonical

Freeze ABI/parser. A read-only exporter invokes safe legacy decode, assigns stable
UUID mapping from `(world,kind,legacy path,traversal path)`, and writes only
`mud_import_ledger` and shadow rows. Stream room/player/object/bank/board/mail/family/
alias/vote/invite/timer separately and resumably. Match `game_characters` only by
canonical name+world+shard; conflict/unmatched rows quarantine, never auto-claim.

Every batch records source SHA-256/size/mtime, parser version, start/end marker and
row counts. Byte-identical rerun is idempotent; changed source opens new revision.
No command reads shadow DB.

### B. Legacy-authoritative dual write — repair journal

Before a class is dual-written, introduce one C persistence facade for **every**
writer: PlayerStore, room save/resave, bank, board, post/mail, alias, family/member,
vote/invite and archive. Do not bridge player files while leaving `bank.c` or
`board.c` direct writes.

POSIX and Postgres do not share a transaction; never call this atomic. The facade
writes a local fsynced intent journal with command ID/pre-post source hashes before
atomic legacy replacement, then marks committed after success. A relay replays it to
idempotent DB RPC. For append/truncate files first implement deterministic
temp+fsync+rename legacy projection; otherwise replay cannot prove partial results.
Shadow DB failure leaves file authoritative and visible repair backlog; economy/item
commands fail closed once repair SLA is exceeded. The journal, not filesystem watch,
is the outbox. Retain records until DB receipt and reconciliation watermark.

### C. DB canonical — files derived projection

After gates pass: stop admissions, drain commands, take named volume snapshot and DB
PITR marker, reconcile final watermark, then audited feature flag `db_canonical`.
State RPC commits command + state event + `mud_outbox` together. C reads DB-derived
state and returns success only after commit.

Projector materializes deterministic legacy snapshot per event through
temp→file fsync→rename→parent fsync and stores unique receipt. Receipt loss/retry is
harmless because same event projects same revision. File projection delay is a
derived-data incident, never a gameplay read source. This avoids “DB then
best-effort file write” split authority.

## Backfill and reconciliation

1. Snapshot/freeze source volume and manifest every file class/checksum/ABI/runtime root.
   Never backfill a changing directory as consistent input.
2. Decode with bounds; validate UTF-8/name/shard, depth, ownership, room range, flags,
   timers. Corrupt/truncated/ambiguous source is ledger quarantine—never synthesize empty
   player/item. Never export password, ticket, ident lookup or raw pointer bytes.
3. Load worlds/catalog → rooms/exits → identity mapping → character/monster/item graph →
   bank locations → social/message/board → schedule; defer cyclic room FKs. Preserve
   traversal order as item position.
4. Compare source/DB per aggregate: canonical serialization checksum, item node count/
   depth/owner/equipped slot, room/exit/permanent count, player gold/XP/stats/timer slots,
   bank tree/value, board body/sequence, mail/family/invite/vote set. Globally require one
   owner per item, no cycles, no duplicate identity; only documented ephemeral drops vary.
5. During B compare per command ID and post-hash after high-risk commands. Mismatch blocks
   that aggregate’s promotion and emits redacted incident artifact; do not auto-overwrite either side.

Required threshold: zero P0 mismatch across two independent full exports and seven days
of production-like shadow traffic; zero unresolved journal/outbox; 100% parsable P0
or individually approved quarantine with character/world disabled; all projection
receipts through final watermark. One happy-path import is not a cutover gate.

## TDD-first failure tests and acceptance gates

Write these tests **red first**. Fixture binaries are ABI-pinned and contain no
production password/message content.

| Layer | Required failure test | Green acceptance gate |
| --- | --- | --- |
| Legacy decoder | truncated struct, invalid count, stale pointer-looking bytes, 4097 nested objects, invalid UTF-8/shard, oversize desc reject | known room/player/bank fixtures become stable DTO/checksum; no pointer/password leak |
| Schema/import | duplicate world/name, two locations/item, orphan/cyclic container, invalid equipped slot, ephemeral drop as permanent reject | deferred constraints + report prove one owner/item and exact counts |
| Character | retry same command ID; same ID/different hash; stale revision | save/relogin/move preserves stats, timers, nested/equipped inventory |
| Economy | inject failure after debit/move/commit; concurrent deposit/withdraw/trade | atomic no lost/duplicate gold/item; deterministic retry and lock-order deadlock test |
| Board/mail | two writers; reader/delete race; body projector crash | no body interleave/sequence collision/lost counter/duplicate message |
| Room/jobs | two job workers; crash claim/effect/ack; restart clock drift | one logical toggle/spawn event and persistent observable due state |
| Dual write | crash before journal commit, after file replacement, after shadow DB, duplicate relay | reconciliation identifies each state; idempotent relay; no silent divergence |
| Canonical outbox | DB commit then projector crash, duplicate event, receipt failure, file failure | DB authoritative, deterministic eventual projection/alert |
| Security | browser/anon/service direct DML/select; forged lease/cross-owner command | only trusted RPC works; no plaintext secret in DB/log fixture |
| Rollback | cutover N, mutate N+k, halt projector, request rollback | verified watermark export/restore, no admission during switch, invariant-matching relog |

Promotion order: (1) parser/property/schema/RLS contracts; (2) disposable C E2E:
create → save → bank → mail → board → timer → shutdown/restart; (3) injected-failure and
concurrency; (4) full backfill/reconciliation threshold; (5) isolated restore and reverse
cutover drill; (6) observability runbook/alert review, then production change approval.
This research authorizes no live `supabase db push`, cluster change, or feature flag.

## Rollback and risk register

Before DB canonical, disable shadow/relay and retain files/journals; do not delete either.
After canonical cutover, code rollback to file readers is unsafe because files can lag DB.
Required reverse: stop admissions → drain/record last DB event N → wait/replay projection
through N → checksum reconcile selected aggregates → snapshot both sides → switch one writer
to verified legacy projection. If it cannot complete, restore DB service/read path and use
PITR; never silently select older file.

| Risk | Evidence | Mitigation / stop condition |
| --- | --- | --- |
| Raw ABI/parser mismatch | `sizeof(struct)` persistence throughout `files1.c`; deployment requires Linux/amd64 | ABI-pinned converter/fixtures; mismatch quarantines and blocks affected cutover |
| Split bank/player wealth | separate `savegame_nomsg/save_bank` calls in `bank.c` | one DB economic transaction before cutover |
| Room dynamic loss | `PERMONLY` and non-atomic room replacement | explicit persistence class, deterministic projector, room durability test |
| Board/mail partial/race | immediate mail append; index RMW | append-only rows, unique sequence, locks/idempotency, body checksum |
| Timer drift/double event | `Time`, `t_toggle`, `last_*` process-local | persisted clock/job run token; restart test |
| Direct I/O bypass | disparate bank/board/post/alias/family/suicide writes | facade coverage checklist and static analysis rejection of new direct writers |
| Secret exposure | raw creature password, auth/log artifacts | never backfill sensitive bytes; private least-privilege RPC/redaction scan |
| Multi-writer | LRU files and timers lack distributed fencing | one active world writer; block rollout otherwise |

## Recommended delivery order

1. P0 parser fixtures + read-only import ledger/reconciliation: player/item graph,
   rooms/exits/permanent entities, bank, timer source.
2. P1 social/content: board, mail, aliases, families, votes/invites, relationships;
   replace unsafe direct I/O with facades under legacy authority.
3. Transactional state RPC + command/event/outbox and B repair journal, first for
   character+bank+item moves.
4. Persisted world clock/jobs and room projector; shadow traffic/failure drills.
5. One-world drained watermark-verified canonical cutover, only after reverse-cutover
   drill passes.

The high-priority work is not a new Postgres table alone: it is proving one writer, a
lossless object-location graph, idempotent commands, and a reversible boundary for every
current direct file mutation.
