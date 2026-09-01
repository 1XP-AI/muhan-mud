# M3 journal v2: live wiring 전 계약 게이트

상태: 설계·RED test 계획 전용 (2026-09-02). 현재
`src/character_save_journal.*`와 `tests/unit/character_save_journal_test.c`는
**v1 synthetic metadata journal뿐인 test-only component**다. 이 문서는 090 SQL,
091 C staged artifact, 092 mock integration의 순서를 고정할 뿐이며 `save_ply`,
`file_player_store_save`, bank writer, live DB RPC/migration을 연결하거나 승인하지
않는다.

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
an `openat` descriptor, hash-read through that descriptor, then renamed into the
already-open `player/<shard>/` descriptor. It is never copied to a live path.

Before each open v2 descriptor-walks configured absolute `MUHAN_HOME` through `player`,
shard, `character-save-journal`, and `character-save-stage` using
`O_DIRECTORY|O_NOFOLLOW`; leaves use `O_NONBLOCK|O_NOFOLLOW`. Trust root and each listed
directory must be UID 10001 (the MUD runtime UID) and exact mode `0700`; live/stage/journal
regular files must be UID 10001 and exact mode `0600`. Symlink, FIFO, device, wrong owner
or mode, link count other than one, and unverified component freeze the command. This is a
deployment trust contract, not a best-effort `chmod` repair.

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
3. Reopen/hash stage, `renameat(stage → player/shard/name)`, fsync live parent, reopen/hash
   live. Only exact posthash can mark `LEGACY_PUBLISHED`; all mismatch/missing-stage/uncertain
   parent-fsync cases freeze rather than guess or consult DB.
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
| **091 C staged artifact, test-only** | `red_091_v2_wire_identity_and_leaf_binding` / fixed journal texts | missing/mismatched instance/character/digest/non-derived leaf rejects before mutation. |
| 091 | `red_091_descriptor_walk_uid_mode_and_symlink` / disposable wrong UID/mode, symlink, FIFO, device, hard-link tree | any untrusted ancestor/leaf fails closed, no path fallback/auto-chmod. |
| 091 | `red_091_hash_cap_when_open_fd_grows` / child extends stage/live fd after fstat | >64 MiB cumulative read rejects; prefix hash never accepted. |
| 091 | `red_091_prepared_recovery_matrix` / stage/live pre/post/corrupt combinations | only exact stage+pre or consumed-stage+post advances; mismatch freezes and makes no DB call. |
| 091 | `red_091_published_recovery_db_offline_backlog` / unavailable RPC mock + posthash | local publish reaches `LEGACY_PUBLISHED`; ACK defers; changed live bytes never ACK. |
| 091 | `red_091_pvc_lock_two_process_race` / `fork` + held flock same volume | one process alone prepares/publishes/reconciles; loser changes nothing. |
| 091 | `red_091_expired_offline_then_successor_fence` / A tuple, offline→renew→B mock | offline backlog needs no DB permission; successor makes A permanently freeze. |
| 091 | `red_091_no_automatic_cleanup_of_evidence` / all states + orphan stage | no automatic delete beyond explicit fixture teardown. |
| **092 mock integration only** | `red_092_synthetic_playerstore_protocol_order` / test serializer + route/epoch/receipt mocks | trace is lock → route/epoch → stage/fsync/hash → PREPARED → rename/fsync/posthash → receipt → DB_ACKED. Reordering fails. |
| 092 | `red_092_crash_cutpoints_end_to_end` / exit after every fsync/rename/RPC then restart | only approved recovered state or frozen divergence; no duplicate receipt/head advance. |
| 092 | `red_092_handoff_drain_then_successor` / queued A ACKs, seal mock, B acquire | B impossible until A ACKs and seals; then A cannot mutate DB. |
| 092 | `red_092_static_no_live_writer_linkage` / source/link-map fixture | no `save_ply`, file writer, bank, Gateway, or production startup reaches v2. Passing is not activation. |

090 is additive private schema/role/RPC plus SQL RED tests only. 091 consumes that fixed
contract through mocks and changes only test-only stage/journal code. 092 composes mocks with
a synthetic serializer. Even green 092 does **not** authorize live wiring: bank aggregate
facade, production route-cache lifecycle, PV capability evidence, divergence runbook, retention
approval, independent review, and an explicit future live-wiring decision remain blockers.

## Unresolved approvals

1. `mud_writer` transport: minimal C DB bridge versus localhost sidecar. It must expose only
   route lookup plus the four RPCs and keep credentials out of files/journal/logs. 090 keeps
   the capability role NOLOGIN with no memberships; the approved transport must define an
   auditable login/impersonation role and preserve safe migration replay before activation.
2. Production retention watermark/duration and immutable backup destination. Until approved,
   automatic deletion remains disabled.
3. Storage-class evidence for `flock`, file/directory fsync, and atomic rename on the PVC.
4. Route-binding cache invalidation for future rename/retirement. Unknown/stale binding must
   fail closed.
5. Successor handoff UX: SQL enforces `sealed_at` but cannot inspect PVC backlog; runbook/
   attestation requires review before any replacement C or Rust writer.
