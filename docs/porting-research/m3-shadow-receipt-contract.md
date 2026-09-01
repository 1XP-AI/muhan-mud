# ADR: M3 DB shadow receipt contract

Status: superseded research snapshot. The executable, test-only 090 contract and all
current gates are maintained in [m3-journal-v2-gates.md](m3-journal-v2-gates.md).
This file preserves the earlier proposal and is not an activation runbook. No C/Rust
writer or live deployment is authorized; fixtures contain no password, raw player
payload, token, or terminal input.

## Authority, routing, and writer model

The **C PlayerStore is the only player-file writer** for normal Telnet and web
commands. Gateway is only an auth/session relay: it never holds epochs, writes
snapshots, or invokes receipt mutation RPCs. Both paths route the existing
unique `(world_id, legacy_name_key)` mapping to `character_id`; `legacy_shard`
is only a derived file-placement hint, never a second identity key. Shadow
covers exactly `imported_unclaimed`, `provisioning`, and `active`; it changes
neither ownership nor lifecycle and rejects `claiming`, `suspended`, `retired`,
and any future unlisted lifecycle.

One C PlayerStore instance per world has a world writer lease. Multiple C
replicas are deployment-invalid. Fencing is between a replacement C instance
and a future Rust cutover writer—not between Gateway and C.

## Local-first protocol

DB is not a publish authorizer before cutover. C does:

1. Read current legacy file/head, write a command-UUID-named staged artifact
   below a trusted PlayerStore staging descriptor, fsync it and directory, then
   write/fsync local journal `PREPARED`.
2. Atomically publish legacy file and fsync parent directory; journal becomes
   `LEGACY_PUBLISHED`.
3. Best-effort submit hash-only DB receipt; journal becomes `DB_ACKED`.

DB outage creates durable local backlog and never blocks step 2. The local
journal contains command UUID, route, positive signed-64 writer epoch/revision,
explicit expected state/hash, posthash, storage format and state—never payload.
The staging leaf is derived from the validated command UUID; the current journal
wire has no filename field, and the next slice must add that derivation contract.
Only the C reconciler with that journal and staged artifact may publish a
prepared file. DB reconciler only observes/acks a known published hash. Missing
or corrupt staged artifact freezes local divergence; DB cannot publish/rollback.
The next C slice defines descriptor-safe staged retention and DB_ACKED cleanup.
The current test-only journal also lacks `writer_instance_id`, `character_id`,
and `request_sha256`; therefore it cannot reconstruct same-writer renewal or an
atomic receipt after restart and must not be wired to `save_ply`. The next slice
must persist or unambiguously derive all four identities, descriptor-walk the
trusted ancestry, and verify the staged/live posthash before changing the local
state to `LEGACY_PUBLISHED`.

## Proposed 090 private schema

`game_character_legacy_heads(character_id PK, head_state, head_sha256,
storage_format, revision bigint, writer_epoch bigint, updated_at)` provides CAS.
Epoch/revision are `1..INT64_MAX`. `head_state` is explicit
`existing|absent|uninitialized`; first observation initializes from
`imported_file_sha256` only when it equals the currently observed legacy hash.
Otherwise it remains uninitialized/divergent and is never guessed or advanced.

`game_character_writer_epochs(world_id PK, epoch bigint, writer_instance_id uuid, issued_at,
expires_at, fenced_at)` has one live epoch. New C/Rust writer acquisition fences
an expired predecessor and installs the successor; live other writer is `P0001`.
The installed row remains identifiable after its lease expires, so the same local
writer can renew it while no successor has been issued. A successor installation
is the permanent fence: an old epoch can never be renewed or used for a receipt.

`game_character_shadow_receipts(character_id,command_id PK, request_sha256,
writer_epoch bigint, writer_revision bigint, expected_state, expected_sha256,
post_sha256, storage_format,timestamps)` is an immutable acknowledged receipt;
it has no DB state column. Insert plus head CAS/advance occurs in one transaction,
so a successful record is already the DB acknowledgement.
`expected_state=existing` requires exact lowercase 64-hex prehash; `absent`
requires NULL prehash. There is no DB `prepared` state or implicit NULL meaning.

## RPCs, privileges, locks

All are SECURITY DEFINER, `search_path=pg_catalog,private`, executable only by
a dedicated non-login `mud_writer` DB role used by the local C bridge. Revoke
EXECUTE from `service_role`, `anon`, and `authenticated`; Gateway receives none.

| Signature | Lock order | policy |
|---|---|---|
| `acquire_game_world_writer_epoch(world_id text, writer_instance_id uuid, lease_expires_at timestamptz)` | world advisory → epoch row | positive bigint epoch; malformed/expired `22023`, live other writer `P0001` |
| `renew_game_world_writer_epoch(world_id text, writer_instance_id uuid, epoch bigint, lease_expires_at timestamptz)` | world advisory → epoch row | only the exact persisted positive epoch and `writer_instance_id` may extend the currently installed, unfenced row; requested expiry must be future at the post-lock `clock_timestamp()` (`22023`), identity/epoch mismatch or successor fence is `P0001` |
| `record_legacy_published_receipt(world_id text, legacy_name_key text, character_id uuid, command_id uuid, request_sha256 text, writer_epoch bigint, writer_revision bigint, expected_state text, expected_sha256 text, post_sha256 text, storage_format smallint)` | world advisory → route row → epoch → head → receipt PK | atomic receipt insert + head CAS/advance + ACK; exact retry only; stale epoch/head/revision or UUID payload mismatch `P0001`; malformed `22023` |

All CAS/lease decisions re-read `clock_timestamp()` after blocking locks. Record
is notification of an already-published file and only advances DB head when the
explicit expected condition matches; failure never mutates a file. `request_sha256`
requires a next-slice journal field, unless 090 instead defines canonical
allowlisted envelope fields and computes the digest server-side.

## Crash table and test matrix

| Local state | recovery |
|---|---|
| PREPARED | C alone opens journal-named staged artifact and publishes; DB is not permission |
| LEGACY_PUBLISHED | C rereads live posthash and retries atomic record/ACK |
| DB_ACKED | exact retry read-only; later local collection |
| artifact missing/corrupt | freeze/divergence; never DB publish/rollback |

With DB offline, a cold restart may continue shadow only with its persisted local
epoch and exclusive PVC writer lock. If that epoch's TTL elapsed, the same writer
first renews its exact installed `(writer_instance_id, epoch)` and then drains the
backlog; renewal is for DB acknowledgement, never permission for local publish.
It must drain predecessor backlog before a successor epoch is issued. After
successor issuance, old epoch renewal and receipts reject permanently. Epoch TTL
is fencing for DB acknowledgement, never permission for local publish.

PG17 RED/GREEN snapshots must cover different-payload replay, stale epoch,
existing/absent prehash CAS, lock wait, exact retry, C-versus-Rust-cutover writer,
and no mutation. Also prove DB outage does not block legacy publish/backlog,
browser/Gateway has no RPC permission, and ownership/lifecycle never change.
Use fixed synthetic fixtures, pg_locks/PgSleep markers, and bootstrap+migrations
twice on disposable PostgreSQL 17.

090 is schema/acquire/renew/atomic-record-ACK RED tests only; 091 is test-only C staged
artifact+journal recovery. Production waits for one-writer guard, fence proof,
divergence runbook, and retention approval.
