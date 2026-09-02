# M3 journal v2: live wiring 전 계약 게이트

상태: **091a stage/hash/parser, 091b-1a PVC writer 잠금/tuple, 091b-1b
held-writer route binding C slice와 additive v2 route SQL은 CI `33579360870`에서
GREEN이고, 091b-2 local publish 경계는 CI `33586412456`에서 GNU GCC·ASan/UBSan
GREEN이다. 091b-3의 exact receipt callback과 local `DB_ACKED` marker slice는
CI `33589685559`에서 GREEN이고, immutable PREPARED 한 건을 route 없이 복구하는
`recover_one`은 CI `33591568569`에서 GREEN이고, bounded lexical backlog scanner는
CI `33594859743`에서 GREEN이다. test-only native libpq receipt transport와 disposable
PostgreSQL 17 실제 통합은 CI `33598858884`에서 GREEN이다. expired/offline successor
gate의 local/actual-PG17 contract는 추가됐고 다음 disposable PG17 CI 실행을 기다린다.
092 이후와 production transport 연결은 미구현**
(2026-09-02).
`src/character_save_journal_v2.*`는 derived stage leaf, canonical v2 wire/request
digest, descriptor walk, 누적 64 MiB hash cap, immutable `PREPARED` 생성·읽기만
검증한다. 별도 `src/character_save_journal_v2_writer.*`는 descriptor-relative PVC
root에서 exact persisted `(world_id, writer_instance_id, writer_epoch)`를 읽고,
`.m3-writer.lock`의 process-lifetime `flock`과 lock file/journal directory fsync를
검증한다. `src/character_save_journal_v2_route.*`는 exact held writer handle을 다시
검증한 뒤 mock lookup 한 번으로 DB 권위 identity/storage tuple을 묶는다. 일반·
ASan/UBSan·production static no-live-link 테스트와 additive route RPC의 PostgreSQL
17 계약은 CI `33579360870`에서 GREEN이다. 별도 publish 모듈의 expected-existing
rename과 expected-absent no-replace local publish/retry는 CI `33586412456`에서
검증됐다. ACK 모듈은 exact 12-field receipt mock, DB-offline defer, post-callback
재검증과 durable `DB_ACKED` marker retry를 검증한다. 별도 receipt transport 모듈은
exact typed `PQexecParams` 한 번과 SQLSTATE 결과 매핑을 실제 PostgreSQL 17에서
검증하지만 명시적 disposable test target에만 링크된다. publish 모듈의 route-free
`recover_one`은 held writer와 command UUID만 받고 PREPARED의 tuple·shard·name을 두 번
검증한 뒤에만 marker/live 복구를 수행한다.
production caller·DB login/impersonation 설정과 no-GC 운용은 아직 완료하지 않았으므로
091 전체나 live 연결이 완료된 상태가 아니다. v2 모듈 모두
`save_ply`, `file_player_store_save`, bank writer, Gateway/DB, production startup에는
링크되지 않는다.

기존 `src/character_save_journal.*`와
`tests/unit/character_save_journal_test.c`도 **v1 synthetic metadata journal뿐인
test-only component**다. 이 문서는 090 SQL, 091a 준비 경계, 091b 게시·복구,
092 mock integration의 순서를 고정할 뿐이며 live DB RPC/migration을 연결하거나
승인하지 않는다.

## 근거와 경계

| 코드/문서 근거 | v2 계약에 주는 제약 |
| --- | --- |
| `src/player_store.c`: `save_ply()` → `file_player_store_save()` | 향후 유일한 player facade 후보이지만 이 slice에서는 호출·수정하지 않는다. |
| `src/file_player_store.c` | temp → fsync → rename → parent fsync은 있으나 path-based temp writer다. v2는 별도 descriptor-relative stage를 증명해야 한다. |
| `public.game_characters` unique `(world_id, legacy_name_key)` | 이 route가 `character_id`를 결정한다. SHA-1-derived `legacy_shard`는 file placement hint이지 identity key가 아니다. |
| v1 journal wire | command/name/pre/post hash/epoch/revision만 있고 writer instance, character, request digest, stage binding, trusted ancestry가 없다. 따라서 live 연결 불가다. |
| `player_path_open_readonly()` + `onboarding_session_file_sha256()` | initial `fstat` cap만으로 fd 성장 경쟁을 막지 못한다. v2는 initial size와 cumulative read cap을 모두 적용한다. |
| deployment docs | replica 1 + `Recreate`는 필요하지만 writer fence가 아니다. PVC-held process lock과 DB epoch가 별도로 필요하다. |

Legacy C player file은 계속 shadow 단계의 authority다. DB receipt는 이미 publish된
hash-only bytes의 idempotent acknowledgement일 뿐 ownership/lifecycle/payload/password를
변경하거나 저장하지 않는다. 기존 ADR의 allowlist lifecycle,
`imported_unclaimed|provisioning|active`만 receipt 대상이다.

## v2 persistent identity and canonical request

`writer_instance_id`는 PID나 pod UID가 아니다. PVC의 private
`character-save-journal/writer-instance.v2`에 volume provisioning 때 생성해
`0600`/fsync로 보존하는 lowercase UUID다. 같은 PVC를 쓰는 재시작은 같은 UUID를
사용하고, 새 volume 또는 cutover writer만 새 UUID를 사용한다. `(world_id,
writer_instance_id, writer_epoch)`는 `writer-epoch.v2`에도 atomically/fsync
persist한다. positive signed-64 epoch/revision 범위는 `1..INT64_MAX`다. 이
persisted tuple 없이 `PREPARED`를 만들 수 없다.

DB epoch row는 installed instance를 보존한다. 같은 persisted tuple은 TTL이 지난 뒤도
successor가 없을 때만 renew할 수 있다. 다른 instance는 predecessor가 **sealed and
expired**일 때만 acquire한다. new epoch install transaction은 predecessor `fenced_at`
을 기록한다. 이후 old tuple의 renew, seal, receipt은 항상 `P0001`이고 head/receipt는
변하지 않는다. 이것이 successor permanent fence다.

090에는 `seal_game_world_writer_epoch(world, instance, epoch)`를 추가한다. PVC lock을
가진 predecessor가 새 command 생성을 중지하고 local journal을 모두 `DB_ACKED`로 drain한
후에만 exact tuple을 seal한다. SQL은 PVC를 검사할 수 없으므로 drain을 독립 증명하지
못한다. 그러므로 successor acquire는 `sealed_at is not null`을 요구하고, 092 handoff
fixture와 운영 runbook은 seal 전 full PVC scan을 필수로 한다.

v2 journal immutable fields:

```text
version=2
writer_instance_id=<lowercase UUID>
character_id=<lowercase UUID>
request_sha256=<64 lowercase hex>
staged_leaf=<derived, not caller supplied>
```

`character_id`는 name/shard에서 추측하지 않는다. `mud_writer` role의 read-only route
lookup이 validated `(world_id, canonical legacy_name_key)`에 대해 `character_id`, canonical
name, stored shard, storage format, allowed lifecycle을 돌려준다. 결과는 private,
descriptor-relative route binding cache에 fsync한다. DB offline이면 이미 검증된 exact
binding만 쓸 수 있고 cache miss/new name은 fail closed한다. Receipt RPC는 route row를
다시 lock하여 supplied ID/name/shard/format/lifecycle을 모두 비교한다.

`request_sha256` is SHA-256 of exactly the following ASCII byte envelope, encoded as
lowercase hex. `legacy_name_key_hex` is lowercase hex of validated UTF-8 bytes; `-`
is the literal absent prehash. Every field order, LF, and final LF is mandatory.

```text
m3-shadow-receipt-v2\n
world_id=<world_id>\n
character_id=<character_id>\n
legacy_name_key_hex=<utf8 hex>\n
legacy_shard=<two lowercase hex>\n
command_uuid=<command_uuid>\n
writer_instance_id=<writer_instance_id>\n
writer_epoch=<decimal>\n
writer_revision=<decimal>\n
expected_state=existing|absent\n
expected_sha256=<64 lowercase hex|->\n
post_sha256=<64 lowercase hex>\n
storage_format=<positive decimal smallint>\n
staged_leaf=<command_uuid>.stage\n
```

There is no JSON reserialization, trimming, SQL `lower()`, locale conversion, or
optional field. C computes it before `PREPARED`; 090 recomputes it from trusted route
data plus validated arguments and rejects mismatch with `22023`. The receipt stores the
digest, so same `(character_id, command_uuid)` is read-only only for the exact envelope and
the currently installed exact, unsealed, unexpired writer tuple. Expiry requires exact renew;
a sealed tuple or installed successor permanently fences even an otherwise exact retry.

## Descriptor walk, stage leaf, and byte cap

The only staged leaf is `<command_uuid>.stage` under `character-save-stage/`; callers
cannot pass a path or suffix. It is a regular `0600` file, written and fsynced through
an `openat` descriptor and hash-read through that descriptor. Expected-existing uses
same-filesystem `renameat` into the already-open `player/<shard>/` descriptor. Expected-
absent uses a portable loss-safe no-replace sequence: `linkat`, destination-directory
fsync, source unlink, then source-directory fsync. That sequence intentionally exposes
a recoverable two-name state and is not described as an atomic move.

Before each open v2 descriptor-walks configured absolute `MUHAN_HOME` through `player`,
shard, `character-save-journal`, and `character-save-stage` using
`O_DIRECTORY|O_NOFOLLOW`; leaves use `O_NONBLOCK|O_NOFOLLOW`. Trust root and each listed
directory must be UID 10001 (the MUD runtime UID) and exact mode `0700`; live/stage/journal
regular files must be UID 10001 and exact mode `0600`. Symlink, FIFO, device, wrong owner
or mode, unverified component, or a link count other than one freezes the command, except
for the exact same-inode two-name pair created by an interrupted local promotion. Recovery
hash-verifies that pair before removing a name. This is a deployment trust contract, not a
best-effort `chmod` repair.

Every stage/live hash reader first rejects initial regular-file size over
`PLAYER_PATH_READ_MAX_BYTES` (64 MiB), then rejects a successful read when
`n > CAP - total`. It handles `EINTR`, verifies EOF and close, and hashes the exact
opened fd. Thus a file that grows after `fstat` cannot yield an accepted prefix hash.

## Exact local/RPC ordering

The process first opens descriptor-relative `.m3-writer.lock` on the same PVC, validates
it `0600`, and holds `flock(LOCK_EX|LOCK_NB)` for its complete lifetime. Lock failure means
startup/recovery fails closed: no writer or reconciler may mutate a file. Selected PV
semantics must prove cross-process advisory locks, fsync, directory fsync, and atomic
same-filesystem rename. `replicas: 1`/`Recreate` remain mandatory but are not substitutes.

With the lock held, each command does exactly this:

1. Open trusted descriptors; resolve/load exact route binding and persisted writer tuple.
   Serialize into derived stage, fsync stage + stage directory, and cumulative-hash it.
2. Descriptor-open/cumulative-hash live bytes. Require explicit `existing + expected hash`
   or `absent`; derive request digest; write+fsync v2 `PREPARED` only after these facts hold.
3. Reopen/hash stage. For expected-existing, `renameat(stage → player/shard/name)`; for
   expected-absent, no-replace `linkat` → live-parent fsync → stage unlink → stage-parent
   fsync. Reopen/hash live before writing the marker. Only exact posthash can mark
   `LEGACY_PUBLISHED`; mismatch or uncertain durability freezes rather than guessing or
   consulting DB. A consumed stage plus exact posthash is reconciled by fsyncing both
   parents before marking.
4. Best-effort `record_legacy_published_receipt`; only exact DB ACK can fsync
   `DB_ACKED`. Timeout/offline leaves `LEGACY_PUBLISHED` durable.

All authorizing decisions reread `clock_timestamp()` after every blocking lock.

| RPC | exact lock order and rule |
| --- | --- |
| `acquire_game_world_writer_epoch(world, instance, expiry)` | world xact advisory → epoch row `FOR UPDATE` → fresh clock. Same unsealed instance returns/renews its tuple; different instance requires sealed+expired predecessor and then fences it. |
| `renew_game_world_writer_epoch(world, instance, epoch, expiry)` | world advisory → epoch `FOR UPDATE` → fresh clock. Exact instance/epoch, unsealed row, and future requested expiry required. Expiry alone does not prohibit same-writer renewal. |
| `seal_game_world_writer_epoch(world, instance, epoch)` | world advisory → epoch `FOR UPDATE` → fresh clock. Exact unsealed tuple writes `sealed_at`; it permits no new local command. |
| `record_legacy_published_receipt(...)` | world advisory → route row `FOR SHARE` → epoch `FOR UPDATE` → head `FOR UPDATE` → receipt PK lookup/insert → fresh clock/CAS. `FOR SHARE` stabilizes non-key authority fields such as lifecycle, storage format, and imported hash. Validate route/lifecycle/digest, then immutable receipt insert and head advance commit together. |

`record` has no DB `PREPARED` state. An exact existing receipt is read-only only while its
exact writer tuple is the currently installed, unsealed, unexpired tuple and no successor has
replaced it. Every new record has the same lease requirement plus expected head (`existing`
exact hash or `absent`),
strictly next revision, and posthash. Stale epoch/revision/prehash, UUID payload mismatch,
fence, or route mismatch is `P0001` with no mutation. Malformed lease/envelope/digest is
`22023`. Only non-login `mud_writer` has execute; browser roles, `service_role`, and Gateway
are revoked.

A missing head may bootstrap revision zero only for `existing` when the trusted route's
lowercase `imported_file_sha256` exactly equals the observed prehash. An `absent` observation
or any hash mismatch is `P0001` with no row. Production absent-head seeding remains a future
file-authority protocol and is intentionally not inferred by this receipt RPC.

## Recovery, DB outage, and cleanup

Recovery obtains the PVC lock and validates ancestry first, then scans lexical command UUID
leaves, never mtimes.

| state | required recovery | forbidden |
| --- | --- | --- |
| `PREPARED` | stage must exist and hash post; live expected pre/absent allows one rename. Consumed stage + live post permits mark-published. Any other combination freezes divergence. | DB receipt, regenerated stage, blind overwrite, deletion. |
| `LEGACY_PUBLISHED` | stage must be absent; reread live cumulative posthash, then exact record retry. ACK marks `DB_ACKED`. | DB publication/rollback or acceptance of changed live bytes. |
| `DB_ACKED` | exact read-only retry only while the same exact unsealed, unexpired writer tuple remains current; an expired tuple must exact-renew first. Retain immutable evidence. | Retry after seal/successor, DB mutation, or backwards transition. |

With DB offline, PVC-lock holder with a persisted tuple may create/publish local journals;
DB is not a pre-publish authorizer. An expired tuple may continue local file-authority work,
but cannot ACK until the same instance exact-renews once DB returns. If a successor exists,
old renew/receipt rejects permanently and the local situation freezes for operator recovery.
The sealed predecessor acquire rule prevents planned successor issuance while backlog remains.

091/092 intentionally implement **no automatic production GC**. `PREPARED` stage+journal
remain; successful rename consumes the stage; a leftover stage is incident evidence.
`LEGACY_PUBLISHED` and `DB_ACKED` journals remain `0600` until separately approved retention
watermark, reconciliation export, and verified backup/PVC snapshot. Only disposable fixture
teardown may remove an exact descriptor leaf. A cleaner must not erase crash/fence evidence.

## RED test matrix and implementation sequence

Fixed UUIDs, synthetic hashes, disposable `MUHAN_HOME`, PG17 disposable DB, and redacted
fixtures only. No fixture contains player payload, password, JWT, ticket, or production data.

| slice | RED test / fixture | failure invariant and GREEN boundary |
| --- | --- | --- |
| **090 SQL tests only** | `red_090_epoch_same_instance_restart_renewal` / `m3_epoch_seed.sql` (A epoch 7, expired clock) | persisted A can renew; B cannot. No C file changes. |
| 090 | `red_090_successor_requires_sealed_expired_predecessor` / A+B UUIDs, `pg_sleep` lock marker | B before seal/expiry is `P0001`; after B acquire, A renew/record/seal are permanent `P0001`, head unchanged. |
| 090 | `red_090_route_is_character_id_not_shard` / two characters sharing derived-shard fixture | wrong ID/name/shard, format, or lifecycle inserts no receipt. |
| 090 | `red_090_canonical_request_sha256_golden_and_tamper` / exact envelope bytes above | C/SQL golden agrees; newline/order/case/prehash/UUID/leaf tamper is `22023`, never normalization. |
| 090 | `red_090_receipt_exact_retry_and_head_cas` / existing, absent, receipt rows | missing-head `existing` bootstraps only from the exact imported hash while mismatched/absent inference rejects; exact retry is read-only only for the current exact unsealed, unexpired writer tuple and a consistent head at or beyond its revision. Changed digest/stale prehash/revision/epoch, missing/behind/mismatched head, or retry after expiry/seal/successor leaves receipt and head unchanged. |
| 090 | `red_090_lock_wait_rechecks_clock` / concurrent PG sessions + `pg_locks` | post-wait expired lease rejects; a committed non-key lifecycle change is observed after route-lock wait and also rejects. No deadlock or partial head. |
| 090 | `red_090_writer_privileges_and_no_identity_mutation` / mud_writer, Gateway, browser roles | only mud_writer executes; no ownership/lifecycle/name/shard mutation. |
| **091a C prepare boundary, test-only — GREEN** | `red_091a_v2_wire_identity_leaf_and_raw_bytes` / fixed journal texts | missing/mismatched identity/digest/non-derived leaf, overlong field, embedded NUL, extra bytes reject before mutation and zero parser output. |
| 091a — GREEN | `red_091a_descriptor_walk_and_file_contract` / disposable wrong UID/mode, symlink, FIFO, device, hard-link and component-swap tree | invalid ancestor/leaf rejects with no path fallback or auto-chmod. |
| 091a — GREEN | `red_091a_hash_cap_when_open_fd_grows` / child extends fd after fstat | exactly 64 MiB accepts; initial or cumulative +1 rejects; prefix hash is never accepted. |
| 091a — GREEN | `red_091a_write_fsync_close_durability` / EINTR, short/zero/EIO write, four close points, four fsync points | retryable writes finish exactly; uncertain durability reports failure, preserves available stage/journal evidence, and never reuses a consumed fd. |
| 091a — GREEN | `red_091a_static_no_live_linkage` / fresh production object, `nm`, link map, Make `OBJECTS` | no player writer, bank, onboarding, Gateway or DB edge; test hooks absent; v2 absent from live MUD objects. |
| **091b-1a PVC writer lifetime boundary, test-only — GREEN** | `red_091b_writer_persisted_tuple_and_lifetime_lock` / exact tuple leaves, two processes, first-create pause | only an exact persisted world/instance/epoch tuple opens; one process holds the lock for the context lifetime and a later process can acquire only after close. |
| 091b-1a — GREEN | `red_091b_first_create_sync_race_and_retry` / creator paused after `O_EXCL`, existing opener, injected fsync failure | every successful opener fsyncs the lock file and journal directory while holding `flock`; a failed creator followed by retry repeats both durability operations. |
| 091b-1a — GREEN | `red_091b_writer_static_no_live_linkage` / fresh production object, `nm`, Make `OBJECTS` | no player writer, bank, onboarding, Gateway or DB dependency; test hooks absent and the writer object remains outside live MUD objects. |
| **091b-1b held-writer route binding, test-only — GREEN (`33579360870`)** | `red_091b_bound_route_owner_and_identity` / opaque held writer handle + route mock + additive route RPC | forged, copied, zero, garbage and fork-child handles cannot validate, close, or reach the callback; the exact owner gets one lookup and binds only the DB-returned UUID/name/shard/format/lifecycle/imported hash. Same-shard names retain distinct character IDs and all failures preserve output. GNU GCC sanitizer와 PostgreSQL 17 migration replay/contract도 GREEN. |
| **091b-2 local publish/recovery, test-only — GREEN (`33586412456`)** | `red_091b_prepared_recovery_matrix` / stage/live pre/post/corrupt combinations | exact stage+pre publishes through expected-existing rename or expected-absent loss-safe no-replace ordering; consumed-stage+exact-post and exact interrupted two-link pairs converge. Mismatch, alias, race, partial marker, close/fsync/unlink ambiguity freezes without DB calls. GNU GCC 일반·ASan/UBSan과 전체 unit도 GREEN. |
| 091b-2 — GREEN (`33586412456`) | `red_091b_local_marker_retry_and_no_live_linkage` / exact·partial·conflicting `.published.tmp`, destination races, `nm`/Make | only an exact command-owned temporary is narrowly reusable; it is removed only after the exact target is proven durable. Partial, conflicting, aliased, or orphan evidence is retained. Publish test hooks are absent from its production object and the object remains outside live MUD `OBJECTS`. |
| **091b-3 receipt ACK/local marker, test-only — GREEN (`33589685559`)** | `red_091b_exact_receipt_and_db_acked_marker_retry` / absent+existing receipt mocks, unavailable callback, every marker close/fsync/link/unlink cutpoint | only exact `LEGACY_PUBLISHED`, absent stage, exact live posthash and held writer tuple reach the exact 12-field callback. Deferred/invalid/rejected outcomes retain evidence. ACK then revalidates writer/live and fsyncs `DB_ACKED`; temp-only and exact two-name retries re-fsync and repeat the idempotent callback. Partial, conflicting, different-inode and nlink3 evidence remains byte-for-byte unchanged. ACK object stays outside live MUD `OBJECTS`. GNU GCC 일반·ASan/UBSan과 전체 CI도 GREEN. |
| **091b-3 route-free single-record recovery, test-only — GREEN (`33591568569`)** | `red_091b_prepared_route_free_recover_one` / absent·existing·consumed-stage·two-name·tuple mismatch·malformed/unsafe evidence | held writer와 command UUID 외의 path, payload, name, route 입력 없이 immutable PREPARED가 정한 shard/name/precondition만 사용한다. 두 번의 tuple/wire 검증 전에 marker를 변경하지 않으며, FD 소유권은 모든 open/read/identity 종료 경로에서 정확히 한 번만 닫힌다. GNU GCC 일반·ASan/UBSan, production static no-live-link와 전체 CI가 GREEN. |
| **091b-3 lexical recovery/backlog scan, test-only — GREEN (`33594859743`)** | `red_091b_published_recovery_db_offline_backlog` / scrambled lexical PREPARED scan + deferred/freeze mocks | held writer로 최대 1024개의 canonical lowercase UUID PREPARED를 먼저 안전성 검사해 bounded snapshot으로 고정하고 bytewise 정렬한다. 각 command를 한 번씩 route-free recover한 뒤 성공 건만 exact ACK에 넘긴다. Deferred·invalid/rejected freeze·changed-live는 증거를 보존한 채 다음 command를 방문하고, writer capability 상실이나 DB-ACKed/local-incomplete는 즉시 중단한다. OOM·cap·directory/root close·unsafe leaf는 callback 전에 all-zero report로 종료한다. GNU GCC 일반·ASan/UBSan, 전체 unit, static no-live-link와 전체 CI가 GREEN. |
| **091b-3 native PostgreSQL receipt transport, test-only — GREEN (`33598858884`)** | `red_091b_native_receipt_transport_pg17` / disposable PostgreSQL 17 + actual libpq harness | `PGOPTIONS` startup role이 `current_user=mud_writer`, `session_user=postgres`를 만들고 private table 직접 SELECT는 `42501`로 거부됨을 먼저 증명한다. 함수 EXECUTE revoke 대조군은 실제 callback을 `DEFERRED`로 만들며 state를 바꾸지 않는다. 정상 ACK, exact retry timestamp 무변경, `22023` invalid freeze와 `P0001` rejected freeze를 actual typed `PQexecParams`로 검증하고 head·receipt·writer epoch/fence·identity snapshot을 비교한다. 일반 unit·ASan/UBSan과 Linux/ARM/macOS/Windows 전체 CI도 GREEN이며 production `OBJECTS`에는 추가되지 않는다. |
| **091b-3 — PG17 CI pending** | `red_091b_expired_offline_then_successor_fence` / A tuple, offline→exact renew→seal→B | local ACK boundary verifies durable `LEGACY_PUBLISHED`/`DB_ACKED` evidence is unchanged on DEFERRED, expired, and successor-rejected receipt outcomes; the disposable native libpq contract verifies actual expiry rejection, exact A renewal, and permanent A renew/seal/receipt fence after B install. |
| **091b-3 unclassified evidence — GREEN (`33594859743`)** | `red_091b_no_automatic_cleanup_of_unclassified_evidence` / final markers, unrelated temp, tuple, lock, malformed/unsafe leaves | scanner는 `.prepared` candidate 외의 marker·tuple·lock·unrelated temp를 권위 입력으로 보지 않고 삭제하지 않는다. malformed canonical candidate, symlink, FIFO, hard-link는 전체 scan을 무변경으로 중단하며 기존 publish/ACK의 partial/conflicting/alias 회귀도 함께 GREEN이다. Production 자동 GC는 여전히 없다. |
| **092 mock integration only** | `red_092_synthetic_playerstore_protocol_order` / test serializer + route/epoch/receipt mocks | trace is lock → route/epoch → stage/fsync/hash → PREPARED → rename/fsync/posthash → receipt → DB_ACKED. Reordering fails. |
| 092 | `red_092_crash_cutpoints_end_to_end` / exit after every fsync/rename/RPC then restart | only approved recovered state or frozen divergence; no duplicate receipt/head advance. |
| 092 | `red_092_handoff_drain_then_successor` / queued A ACKs, seal mock, B acquire | B impossible until A ACKs and seals; then A cannot mutate DB. |
| 092 | `red_092_static_no_live_writer_linkage` / source/link-map fixture | no `save_ply`, file writer, bank, Gateway, or production startup reaches v2. Passing is not activation. |

090 is additive private schema/role/RPC plus SQL RED tests only. 091a fixes the test-only
stage/hash/parser boundary, 091b-1a fixes the persisted writer tuple plus PVC lifetime lock
boundary, 091b-1b adds the held-writer route binding seam, and 091b-2 adds local publish plus
durability recovery without live linkage. 091b-3의 exact receipt callback과 local
`DB_ACKED` marker retry는 CI `33589685559`에서 GREEN이고 route-free single-record
`recover_one`은 CI `33591568569`에서 GREEN이고 bounded lexical backlog scanner는
CI `33594859743`에서 GREEN이다. test-only native DB adapter의 actual PostgreSQL 17
통합은 CI `33598858884`에서 GREEN이다. expired/offline successor의 새 disposable
PG17 contract는 CI 실행 대기이며, production transport 권한·설정·호출 연결은 남아 있다. 이 경계까지 통과해야 091을 complete로
부를 수 있다.
092 composes those mocks with
a synthetic serializer. Even green 092 does **not** authorize live wiring: bank aggregate
facade, production route-cache lifecycle, PV capability evidence, divergence runbook, retention
approval, independent review, and an explicit future live-wiring decision remain blockers.

## Unresolved approvals

1. `mud_writer` production transport: test-only minimal C libpq bridge는 실제 PG17에서
   검증됐지만 이를 승격할지 localhost sidecar를 채택할지는 미결정이다. production
   transport는 route lookup과 네 RPC만 노출하고 credentials를 file/journal/log에 남기지
   않아야 한다. 090의 capability role은 NOLOGIN/no-membership이며, 활성화 전에 auditable
   login/impersonation role과 safe migration replay를 별도로 고정해야 한다.
2. Production retention watermark/duration and immutable backup destination. Until approved,
   automatic deletion remains disabled.
3. Storage-class evidence for `flock`, file/directory fsync, and atomic rename on the PVC.
4. Route-binding cache invalidation for future rename/retirement. Unknown/stale binding must
   fail closed.
5. Successor handoff UX: SQL enforces `sealed_at` but cannot inspect PVC backlog; runbook/
   attestation requires review before any replacement C or Rust writer.
