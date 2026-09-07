# Live bank capture and transaction gap

## Confirmed current behavior

All ordinary `load_bank` / `save_bank` calls in `src/bank.c` route through
BankStore. However, no live caller invokes `bank_snapshot_v1_artifact_store`.
`bank_evidence.c` creates canonical bytes for validation and immediately frees
them; it does not publish a bank artifact. Passing synthetic relay/restore
tests therefore does not establish live bank capture coverage.

| Mutation | Current durable-call order | Problem |
| --- | --- | --- |
| Single / bulk item deposit | player save, bank save | player receipt can precede bank publication |
| Single / bulk item withdrawal | bank save, player save | bank publication can precede player receipt |
| Money deposit / withdrawal | bank save, player save | bank save result is ignored |
| Missing bank initialization / editor | bank save only | no accompanying player command receipt |

The legacy file writer uses in-place `write_obj` with no transaction joining
the player file. Reordering two independent file writes is not an atomic fix.
The existing payload RPC binds a bank digest to a player receipt/manifest, but
does not prove both reflect one successful transfer. Never infer that binding
from a name, newest receipt, timestamp proximity or successful bank save alone.

## Executed failure evidence

`tests/unit/bank_transfer_legacy_characterization.c` links the actual C
`deposit` and `withdraw` commands, replacing only storage/output boundaries.
It runs all-money transfers with initial player 100 and bank 50:

- Successful deposit and withdrawal preserve total 150.
- Failed bank save during deposit still saves player 0, bank remains 50.
- Failed bank save during withdrawal still saves player 150, bank remains 50.
- Both failures call bank save followed by player save and do not abort.

Executed successfully in isolated Linux ARM64 Docker with read-only source,
no network and tmpfs binaries. This test **documents a known defect**; a passing
characterization is not a successful failure-safety or cutover acceptance gate.
The local runner now includes it so the gap remains visible.

## Required next implementation

1. Introduce a command-scoped transfer intent holding exact character identity,
   command UUID, expected player/bank revisions and both proposed snapshots.
   Validate funds, limits and object ownership before mutating live state.
2. Commit player and bank state together in one PostgreSQL transaction with
   compare-and-swap revisions and immutable command-result replay. A retry must
   return the original result; a changed payload for the same command fails.
3. Add crash/failure tests before commit, after commit before acknowledgement,
   concurrent writers, stale revisions and retry. Assert conservation of money
   and unique object ownership rather than merely two successful file writes.
4. Connect all six gameplay mutation paths plus initialization explicitly.
   Legacy file mirrors require a durable outbox and recovery, not success
   messages before both authoritative changes are committed.
5. Compare legacy successful-operation results with the new transaction in
   shadow mode. Failure behavior must deliberately differ from the defect
   characterized above. Only then enable a versioned DB-authority gate.

No live capture hook, authority switch or deployment was added by this audit.

## Detached transfer planner implemented

Rust `bank_transfer_v1::plan_money_transfer` now accepts full player and bank
snapshots and an explicit positive amount. It validates both schemas, checks
funds, nonnegative balances, the existing 300-million deposit ceiling and i64
overflow before returning two owned proposed snapshots. Only player gold and
bank root value change; nested objects and other fields remain identical.
The input snapshots are never mutated. Zero or negative amounts are rejected;
this intentionally does not preserve the legacy empty "all" transfer behavior.

TDD: tests first failed with the missing module, then all three new integration
tests passed. They cover both directions, rejected conditions, exact other-field
preservation and a deterministic balance/amount matrix of deposit/withdraw
roundtrips. Full default Rust crate tests passed on Linux ARM64 in local Docker:
`/tmp/muhan-bank-transfer-rust.log` (exit 0).

This is the calculation stage, not an atomic persistence implementation. It
does not authenticate the actor or bind revisions, world/character identity,
command replay or writer epoch. The future transaction must perform those
checks and compare both input versions before publishing either output.

## Internal atomic pair storage kernel

Migration `20261017000000_paired_snapshot_transaction_kernel.sql` introduces
a private pair-state row and immutable command journal. One shared revision
identifies both full snapshots. `commit_paired_snapshot_candidate` locks that
row, checks the expected revision, updates both snapshots, then journals the
command in the same PostgreSQL transaction. Exact old-command retries return
the original committed revision; changed command inputs conflict. Stale new
commands return SQLSTATE 40001. Payload schemas remain database-validated.

Source `dbda233` passed local PG17 execution with **both payloads changed**:
injected journal INSERT failure rolls back both byte arrays and revision;
successful execution stores both proposed arrays; exact retry succeeds; stale
revision, changed retry revision, malformed bank payload and journal deletion
are rejected. Full local runner exit 0, including prior C/Rust differential,
real bank file relay and restore checks:
`/tmp/muhan-paired-snapshot-changed.log`.

This is an unexposed storage kernel, NOT a gameplay transfer API. Its function
is SECURITY INVOKER; runtime/web roles have no EXECUTE or table access, and no
baseline seeding API or live read route exists. It deliberately does not yet
validate actor/world/writer epoch, money-transfer semantics or item ownership.
The changed test snapshots demonstrate storage atomicity, not legal gameplay.
Next: actual concurrent-session tests and transaction rollback/retry; then bind
the Rust planner's exact input/output digests and implement the authority and
transfer-policy checks before granting any runtime access. No testnet migration
or authority switch was performed.

## Real concurrent PostgreSQL sessions verified

Source `9086ddb`, full local runner exit 0:
`/tmp/muhan-paired-concurrency.log`.
`paired-snapshot-concurrency-local-pg.mjs` opens two real transaction clients
and a separate observer in the disposable PG17 database. It waits until
`pg_blocking_pids` proves the second request is blocked by the first, rather
than assuming overlap from sleeps or simultaneous Promise dispatch.

Four cases passed:

- First transaction commits: competing different command gets 40001; only the
  first snapshot pair and command revision exist.
- Same command concurrently retries: waiter returns EXACT_RETRY at revision 1;
  no duplicate journal row or extra state advance.
- First transaction explicitly rolls back: waiter commits its own pair and
  command at revision 1; aborted command is absent.
- First connection closes before commit: server rollback releases the lock;
  waiter commits, with no partial pair or orphan command from the closed client.

Every case compares both persisted byte arrays and exact command identity/count.
Statement timeouts and a bounded observation deadline prevent indefinite waits.
The client-close case is not a database-host crash test. These checks exercise
the unexposed internal kernel as a disposable administrator, not production
actor/writer permissions. All earlier relay, C/Rust comparison and bank/player
restore gates also passed. Runtime grants and authority remain unchanged.

## DB / Rust / atomic pair integration

Source `ea0d5aa`, full local Linux runner exit 0:
`/tmp/muhan-rust-bank-db-final.log`.
The bounded `bank_money_transfer_plan` executable reads a length-framed pair
on stdin plus explicit direction/positive amount and both expected SHA-256
values. It validates canonical input bytes and digests, calls the pure Rust
planner, and emits a complete length-framed canonical result pair. It has no
database or live-runtime access; rejected requests emit no payload.

The real PG integration reads pair bytes, their hashes and revision together,
invokes the actual Rust binary, compares outputs against independently patched
expected bytes, then commits with that exact revision. Starting at wallet 100
and bank 50, deposit 25 produces 75/75, withdrawal 25 returns both entire byte
arrays to their original values. Other player fields and nested bank objects
are preserved. Exact command retry, bad input digest, insufficient funds and
stale revision are checked against unchanged database state. The planner's
malformed-argument/framing unit tests are also run by the PG harness.

Still unexposed: the database kernel does not itself enforce that a caller used
this planner. A future gameplay entrypoint must bind actor/character/world,
writer epoch, command intent (direction/amount), exact baseline digests and
revision, and enforce transfer-only changes before obtaining runtime grants.
This integration is an administrator-only test of the calculation/persistence
path, not production authorization or authority cutover.

## Database-enforced money semantics

Migration `20261018000000_money_transfer_semantics.sql` adds an unexposed
money-specific entrypoint on top of the atomic pair kernel. It validates both
canonical payload schemas, then compares every non-money field byte-for-byte
(excluding only envelope digests that must be recomputed). SQL independently
checks exact direction/amount arithmetic, nonnegative balances and the legacy
deposit ceiling using numeric arithmetic to avoid i64 overflow during checks.
The immutable intent row binds direction and amount to the command; changing
either on an otherwise identical retry is rejected.

Source `d9d593f` full local runner exit 0:
`/tmp/muhan-money-policy-rollback.log`.
The DB/Rust E2E now commits through this money-specific entrypoint. Tests reject
canonical player-name and bank-root-name substitutions, unrequested gold,
changed retry amount/direction, cap overflow, insufficient funds, wrapped i64
overflow, zero amount and null direction. They accept the exact cap boundary.
An injected failure on the final intent INSERT rolls back the already-executed
pair update and command INSERT. Retrying afterward commits normally.

No runtime grants were added. Actor ownership, current lease/world/writer-epoch
qualification, audited baseline enrollment and gameplay wiring are still needed.
The internal generic pair kernel remains inaccessible to runtime roles so it
cannot bypass this money-only policy. Item transfer semantics remain separate.

## Locked ownership/session/writer qualifier

Migration `20261019000000_money_transfer_authority.sql` adds a closed internal
qualifier requiring actual `mud_writer_login` plus `SET ROLE mud_writer`. It
locks the writer lease, character, character session and paired state before
sampling `clock_timestamp()`. It checks active ownership, exact world/session/
gateway identity, storage format, live session expiry and matching unsealed,
unexpired writer instance/epoch. The pair lock prevents a later wait between
qualification and mutation when composed in the same database transaction.

Source `2540992` full local runner exit 0:
`/tmp/muhan-money-authority-closed.log`. Valid identity succeeds; wrong actor,
session, gateway, world, epoch, suspended lifecycle, expired session and sealed
writer are rejected. Admin identity is rejected as a runtime caller. The test
grants function access only inside its rollback-only disposable transaction;
a post-rollback assertion proves the runtime role still lacks EXECUTE.

The qualifier is now composed inside `commit_qualified_money_transfer`
(migration 200), not a separate autocommit preflight. The wrapper records the
actor/session/gateway/writer/epoch tuple atomically with both snapshots and the
command intent; exact retries must retain that tuple. Runtime grants remain
closed. Baseline eligibility/enrollment, cross-operation lock-order compatibility
and eventual live command activation remain outstanding.

### Qualified transaction and lock-wait expiry evidence

Source `ac387d5` passed the full isolated Linux ARM64 replay runner (exit 0),
including both backup/restore profiles and C/Rust byte verification. Log:
`/tmp/muhan-qualified-lock-expiry.log`. The actual writer login exercises
DB snapshots -> Rust planner -> qualified atomic commit -> exact retry and
full-byte deposit/withdraw roundtrip. The disposable-only function grant is
explicitly revoked and checked absent at teardown; no production grant exists.

An independent administrator connection holds the paired snapshot row lock.
The writer starts with a live session; `pg_blocking_pids` proves it is waiting
on that administrator before the session expires. Releasing the lock after
expiry rejects the command with P0001, preserves both snapshots/revision and
leaves all three command/intent/authority tables empty for that character.
Refreshing the session then permits the normal commit and exact retry.

The first expiry probe instead reached the role's existing lock timeout
(55P03), so the test connection alone now uses a bounded five-second lock
timeout and ten-second statement timeout around the three-second expiry
window. This is not a production timeout change. This evidence covers session
expiry at the paired-state lock; writer-lease expiry and competing ownership
changes across other operation lock orders still need dedicated coverage.

### Writer expiry and concurrent renewal follow-up

Source `5c85cba` extends the real-connection test to expire the writer lease
while the qualified command waits on the paired-state lock. Like session expiry,
it returns P0001 without changing either snapshot or any command journal.
Both probes establish that the lease is live after the wait is observed, before
waiting for its expiry. No production lease settings are changed.

The real `renew_game_character_session` and `renew_game_world_writer_epoch`
functions also run in separate held transactions. A concurrent qualified writer
is observed blocked via `pg_blocking_pids`; committing the renewal releases it
and the money operation succeeds. Its outer test transaction is then rolled
back, proving both snapshots and all three journals return to the baseline.
These two renewal-before-command schedules passed without deadlock. This does
not prove all possible lock orders: reverse schedules, ownership takeover and
writer fencing during other persistence operations remain untested here.

Full isolated Linux runner, including both restored player/bank profiles,
passed exit 0: `/tmp/muhan-qualified-renewal.log`. No live grants, CI runs or
deployments were performed. Next live-transition prerequisites remain an
audited consistent player/bank baseline, a single runtime authority choice,
and actual command wiring with restart/differential verification.

### Receipt-bound baseline enrollment

Migration 210 now enrolls both payloads into paired revision zero from one
existing character/command receipt. It locks the character and current legacy
head, checks world/name/storage, request hash, writer epoch/revision and source
hash/size across player and bank evidence, and checks the embedded player name
against the canonical character name. An immutable baseline ledger references
both source artifacts and records their digests. Existing untracked paired
state is not adopted. A matching retry never resets an advanced pair.

Source `29ef0ce` passed the full local Linux replay runner, including both
backup/restore profiles: `/tmp/muhan-paired-baseline-rollback.log` (exit 0).
The contract fails before the migration, then passes after two applications.
Wrong request, missing command and stale head reject enrollment. An injected
failure on the final baseline-ledger insert rolls back the pair insert too.
Successful enrollment compares both complete byte payloads to source evidence;
retry preserves a deliberately advanced test revision. Runtime grants remain
absent. The fixture uses a separate identity namespace after the first run
exposed a collision with intentionally inconsistent historical reader fixtures.

This is an internal enrollment prerequisite, NOT proof that separate legacy
files were captured atomically or that live writes have stopped. No production
grant or authority switch is enabled. Live activation still requires a bounded
quiescence/capture protocol, exclusive runtime authority, and command integration
with restart tests. This turn's backup checks retain their existing scope; they
do not yet exercise restoration of an enrolled baseline ledger.

### Enrolled baseline backup/restore follow-up

Source `6c1dca5` adds paired state, baseline ledger and paired command rows to
the full-row backup fingerprints (12 relations total). Both canonical and tree
inventory fixtures enroll through the real internal function, then advance to
revision one through the internal paired kernel before pg_dump/pg_restore.
That kernel probe preserves payload bytes and is not a money-transfer claim.

After restoration, baseline enrollment returns EXACT_RETRY without resetting
revision one; retrying the saved paired command returns its original revision.
Both payloads, the baseline provenance, command and timestamps are covered by
unchanged full-row fingerprints. Browser/service/writer roles retain no baseline
enrollment or paired-state write grants. C byte roundtrips and Rust digest-bound
replay still verify both player and bank payloads for both profiles.

Full isolated Linux runner passed exit 0: `/tmp/muhan-baseline-backup.log`.
This closes the enrolled-baseline restore gap above, not live capture or runtime
authority activation. Qualified money-transfer intent/authority restoration is
still outside this particular backup fixture.

### All-or-nothing native C planner input

Source `4783104` adds `bank_transfer_snapshot_v1_encode`: synchronously encode
one normalized player and detached bank graph, then publish a single frame
with two big-endian lengths and both complete CDTO payloads. This is the actual
Rust money planner's input format. Both payloads are capped at 4 MiB including
digests. A failed bank encoder or final allocation frees the player payload and
returns no frame. The API performs no callbacks, IO or source mutation.

The new C test first failed for the absent API. Normal and ASan/UBSan targets
now pass, checking deterministic bytes, decoded balances, unchanged sources,
invalid-bank failure after player encoding, and injected final allocation
failure. A separate executable integration takes the real C frame into the
Rust planner, deposits and withdraws 25, and compares the complete returned
frame to the original. A wrong player digest returns no output. The full local
Linux runner passed exit 0: `/tmp/muhan-c-rust-paired-capture.log`, including
the existing DB and both backup/restore profiles.

This is a synchronous encoding boundary, not yet a live command hook. Its
caller must exclusively own/freeze both normalized graphs for the call. It
does not establish cross-file atomic capture, normalize live runtime fields,
write evidence, stop legacy saves or activate DB authority. Those prerequisites
must be provided by the eventual runtime coordinator, not inferred from a
passing codec test.

### Actual bank command routing seam

Source `5b46efb` inserts a compile-gated route into actual `deposit` and
`withdraw` after room/argument-count checks, before any legacy bank load or
wallet mutation. Authority selection is separate from transaction outcome:
only explicit legacy selection continues the old path. Selected DB routing
with an unavailable handler, rejection, unknown result or selection error
returns without any legacy read/save. Only confirmed commit status with valid
nonnegative returned balances updates the in-memory wallet and prints success.

The real `bank.c` command characterization harness now runs both old default
scenarios and selected-route scenarios under ASan/UBSan. Its injected
coordinator checks both directions and status -1/0/1/2, missing handler and
selection error, asserting zero bank loads, bank saves and player saves after
DB selection. Failed results leave the wallet unchanged. The original known
legacy failure baseline remains explicitly characterized, not silently fixed.

Full local replay runner passed exit 0:
`/tmp/muhan-bank-command-route-final.log`, including C/Rust and restored DB
profiles. The production object list includes the route module, but
`MUHAN_BANK_MONEY_ROUTING` is NOT enabled by default and no production
coordinator is installed. The handler in this test is a test double, not an
actual database connection. Parsing, command/receipt identity, uncertain-commit
reconciliation, deadlines and session/writer binding belong to the next real
coordinator implementation. Other character saves and item-bank commands must
also be fenced before enabling any DB-authoritative character in live play.

### Qualified writer read -> Rust -> qualified commit

Migration 220 adds `read_qualified_money_transfer_state`, returning one shared
revision, both payloads and their whole-payload SHA-256 digests after the same
owner/session/world-writer qualifier used by the commit. The held locks keep
that returned pair consistent. A separate later commit must still recheck
authority and expected revision; this read is not a reservation after its
transaction ends. All production EXECUTE grants remain revoked.

Source `85eb97d` passed the full local Linux runner, exit 0:
`/tmp/muhan-qualified-read.log`. The actual writer-login integration now obtains
its Rust planner input from this RPC instead of the administrator's direct
table query. The independent admin observation compares all returned fields;
direct writer table SELECT fails, a different actor is rejected, and calling
the RPC as the administrator fails its login-identity check. The real Rust
deposit/withdraw results commit through the qualified writer RPC with exact
retry and full-byte roundtrip preserved. Test-only read and commit grants are
both explicitly revoked and checked absent in teardown.

This completes the tested DB-side read/plan/commit access path, not the native
C coordinator transport. Command identity/reconnect reconciliation, bounded
transport, live normalization and legacy-write fencing remain required before
the bank command compile gate can be enabled.

## Actual C / Rust command differential verified

Source `7db0146` extends the C characterization harness with a bounded numeric
command interface. Rust launches the binary linked with actual `src/bank.c`
deposit/withdraw code for 686 deterministic combinations (two directions,
seven wallet balances, seven bank balances, seven amounts). Compared results
include accepted final balances and rejected unchanged durable balances, with
zero amounts, insufficient funds and values around the 300-million deposit cap.
All 686 agree with the Rust planner. This replaces the command-differential
gap above for the tested explicit-amount money-transfer domain only.

The test requires an explicit C oracle and is ignored by default cargo runs;
the local Linux runner compiles the oracle and explicitly runs `--ignored`.
Full local runner passed exit 0, including bank file/PG E2E and both player/bank
restore profiles: `/tmp/muhan-bank-command-differential.log`.

Not covered by this comparison: item transfers, "all" parsing, i64 overflow
behavior in legacy C, storage failure recovery or a DB transaction. The known
legacy failed-save defect is still separately characterized and not reproduced
as desired Rust behavior. Next persistence work must bind both snapshot input
revisions and command identity, atomically commit both outputs, and prove retry
and crash behavior before any live switch.
