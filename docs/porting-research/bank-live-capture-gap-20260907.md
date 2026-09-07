# Live bank capture and transaction gap

## Recovery discovers fence-only interrupted preparation — 2026-09-07

Test-first source `5110cd6` failed with zero records instead of two because the
visitor ignored reservations. `e726363` makes the bounded visitor read both
money-request and money-fence records with the same strict owner/mode/link/size,
canonical encoding and digest checks. Fence filenames must match the payload's
world/character key. Identical copies are deduplicated by command and content
digest; conflicting copies report an invalid record rather than silently winning.
No file is removed or rewritten, including invalid reservations.

`a5f6e72` exercises the real recovery CLI against PostgreSQL with only the
reservation present: it confirms the exact historical committed request after
session/writer turnover. Adding the identical request file still yields one
confirmed operation. Unknown/corrupt records remain reported; reservation and
request bytes remain unchanged across repeated fresh-process recovery. Unit
fixtures also verify two fence-only characters and no duplicate query calls.

Full frozen ARM64 suite at `a5f6e72` exited 0:
`/tmp/muhan-money-fence-recovery-pg.log`, including real C onboarding and both
backup restore profiles. This supersedes the prior visitor gap, but does not
release reservations or enforce them in the preparation CLI. Safe release,
current-state adoption and all runtime save-path ownership remain prerequisites.
No Actions run, production flag change or deployment.

## Durable per-character reservation primitive — 2026-09-07

Source `1f5745e` adds explicit `claimMoneyCharacterFence`, keyed by canonical
world/character rather than command ID. It writes/fsyncs the immutable complete
request into an exclusive temporary file, publishes a no-overwrite hard link,
removes only its own temporary link, and fsyncs the private owned directory
before returning. Competing different requests cannot replace the winning
reservation. A fresh process may acknowledge only an exact byte-for-byte retry.
Partially published/two-link state fails closed and is not auto-cleaned.

Eight independent Linux Node processes race different commands for one character:
exactly one succeeds. A new process retries the winner, a loser remains rejected,
the winning bytes remain unchanged, and another character can reserve independently.
The full frozen ARM64 suite at `1f5745e` exited 0, including existing DB/native
command, C onboarding and restore checks: `/tmp/muhan-money-fence.log`.

IMPORTANT: this primitive is not yet called by prepare CLI or the running game.
Existing preparation still deduplicates only by command ID. There is deliberately
no release API; automatic release on process exit/timeout would admit a second
transaction while the first may have committed. Before adoption, recovery must
discover fence records (current visitor only discovers money-request files),
resolve the exact immutable request against DB, and coordinate confirmed current
state plus durable reservation release. Power-loss/fsync fault injection remains
unverified. No production cutover, Actions run or deployment.

## Native command callback through live wallet dispatcher — 2026-09-07

Source `e1982b5` adds a production-source transfer callback accepting actual C
`cmd` arguments and a single-command context. Direction comes from the command
entry; amount comes from its bounded second token, not separately supplied
request text. Revision parsing is canonical/overflow checked. The callback
calls descriptor-bound read/plan/prepare/commit, then verified result conversion.
It returns a command acknowledgement only for a new confirmed current-state
result. A context is consumed on its first attempt, including validation failure;
it cannot issue a second command by accidental reuse.

Real PostgreSQL coordinate-mode tests now install this callback in the existing
bank dispatcher and assert live wallet changes only on confirmed success.
Follow-up `4d26150` requires DB confirmation and dispatcher application results
to agree, preventing a false green from merely checking the DB status. Native
mock tests cover command token/direction mapping, consumed-context refusal,
negative revision and nonterminated token. Test-first link failed before the
implementation. macOS and Linux ASan/UBSan pass.

Full frozen ARM64 suite at `4d26150` exited 0, including PG withdrawal/all-money,
state drift refusal, exact retry/recovery, real C onboarding and two restore
profiles: `/tmp/muhan-bank-command-route-final.log`. No Actions/deployment.

Only the disposable harness installs a policy; the running game does not yet
install this callback. The context is not a durable cross-command/restart fence.
Runtime must own connection lifecycle, immutable command IDs, source revisions,
per-character pending fences and every other save path before enabling authority.
The real network command-to-DB loop remains a separate acceptance requirement.

## Confirmed result to command acknowledgement — 2026-09-07

Source `0d55a25` adds `bank_money_result_native`: only a fresh CONFIRMED result
at expected revision + 1 may become a bank command acknowledgement. It validates
bounded pair framing and both complete native codecs, normalizes the current
live player, checks wallet/bank arithmetic without overflow, and compares every
normalized player field including inventory after masking only the gold change.
No live mutation occurs. Corrupt/truncated payloads, historical RETRY, UNKNOWN,
wrong direction/revision or changed player stats expose a zero acknowledgement.
This relies on the caller's qualified coordinator provenance; it is not itself
a DB authentication/reconciliation operation.

Test-first link exposed the missing API. Actual-codec ASan/UBSan tests cover
success, all non-confirmed statuses, stale/overflow revision, wrong direction,
player level drift and corrupt/truncated frame. Real PG coordinate-mode tests
now pass each new confirmed result through this conversion before reporting
success, including withdrawal, all-deposit and normalized Korean money tokens.
The full frozen local ARM64 suite at `0d55a25` exited 0, including C onboarding
and both restoration profiles: `/tmp/muhan-bank-result.log`.

Still not a runtime route installer: production command callback wiring,
durable pending fences/recovery and other save-path ownership remain required.
No Actions run, feature enablement or deployment was performed.

## Wallet application arithmetic guard — 2026-09-07

Source `7e40787` strengthens the actual compile-gated bank command dispatcher:
capture the source wallet, require it to remain unchanged during the callback,
and require the returned wallet to equal source minus/plus the confirmed amount.
Check long bounds before arithmetic; reject impossible negative inferred bank
source on deposit and overflowing inferred bank source on withdrawal. Invalid
results leave acknowledgement zero and do not overwrite the current wallet.
An intervening callback mutation is preserved, not rolled back blindly.

Test-first actual-bank harness failed on an inconsistent acknowledgement before
the fix. Both directions now reject wrong wallet, LONG_MAX amount, intervening
wallet change and impossible bank result. Normal confirmations and legacy
selection still pass; selected failures never enter FileStore. Native macOS
ASan/UBSan passed. Full frozen Linux ARM64 suite at `7e40787` exited 0:
`/tmp/muhan-bank-route-apply.log`, including real PG, C onboarding and both restore
profiles. No production enablement, deployment or Actions execution.

This guard is necessary but not sufficient: the callback must still establish
DB identity/revision, bank provenance and current-source binding. Historical
retry acknowledgement is not permission to apply old balances. Runtime callback
installation, durable pending fences and all other save-path control remain open.

## Normalized live-state equality before bank commit — 2026-09-07

Source `15aed9c`, with fixture correction `e04da36`, captures the live player
through the real bounded legacy serializer, anonymous Linux memfd, strict
player decoder and canonical PlayerSnapshotV1 encoder. It never writes a game
file or changes the input player. The intermediate legacy credential bytes are
wiped before freeing. The checked coordinator requires exact canonical player
bytes to match the qualified DB read before planning or durable preparation.
After a positive result, the descriptor wrapper rechecks binding and normalized
state; changed state discards the result and reports UNKNOWN without applying it.

The first real-codec test failed because its password string exceeded the legacy
20-byte field, leaving no terminator in that field. The strict decoder correctly
rejected it. The fixture now uses a bounded string with an explicit size assertion;
no decoder validation or production behavior was relaxed.

Fresh full frozen-source Linux ARM64 run at `e04da36` exited 0. Evidence:
`/tmp/muhan-bank-live-state.log`. Actual codec sanitizers pass, and real PostgreSQL
tests reject independent live gold/level drift with no pending record, no output,
and unchanged DB state. Matching-state withdrawal/all-money, immutable retries,
lost acknowledgement recovery, real C onboarding and canonical/tree_inventory
backup restoration also pass. The owned test containers were removed by the
runner; no cloud build, Actions run or deployment was performed.

This compares normalized persisted bytes, not all transient runtime fields.
It is not a concurrency lock or a route installer. Remaining: apply validated
results to live state, install the actual command callback, integrate pending
fences/recovery, and govern every other player-save path before DB authority
cutover. Ordinary gameplay is not yet fully database-authoritative.

## DB-derived command amount resolution — 2026-09-07

Source `1dcee62` adds bounded Rust parsing of decimal amounts, `25냥`, leading-zero
numeric forms, `all` and `모두`. Signs, junk, zero, overflow and oversized tokens
are rejected. All-money resolves from the same digest-verified player/bank pair
used for planning: player gold for deposit, bank root value for withdrawal.
An empty balance yields no plan. This deliberately does not preserve legacy
zero-all writes or permissive atol parsing of malformed input.

The new explicit `--resolved-amount-v1` planner mode returns a positive BE i64
amount followed by the original bounded pair frame. Native transport validates
and strips the metadata, then the coordinator replaces the command token with
the canonical numeric amount before durable preparation/commit. Successful new
results also expose that amount. Original four-argument planner framing remains
unchanged. No ambiguous all-token is saved for a future retry to recalculate.

Verification includes Rust digest-bound all-deposit/all-withdraw and grammar
tests; native coordinator unit sanitizers; and real writer-login PostgreSQL
calls through the descriptor wrapper. The latter deposits all 100, confirms a
numeric pending record, retries exactly without a second transfer, rejects
empty-all without a record, and accepts `000100냥` withdrawal back to identical
initial payloads. Recovery still confirms historical revision 1 while head is 4.

Full frozen-source Linux ARM64 suite exited 0 at `1dcee62`:
`/tmp/muhan-bank-command-all.log`, including actual C onboarding and both restore
profiles. This does not install the callback into bank.c or prove live persisted
state equality/application. Session/authority selection, pending fences and other
writers remain prerequisites. No deployment, feature enablement or CI run.

## Descriptor-bound native money request — 2026-09-07

Source `00c25bb` adds `bank_money_live_native`, a caller-invoked wrapper around
the existing native coordinator. It requires the exact player pointer in its
current bounded Ply slot, a live io/extra, no onboarding mode, and complete
canonical actor/character/DB session/gateway fields. It copies these fields from
descriptor-owned extra; a name, descriptor number alone or admission nonce is
not accepted as identity. Runtime still supplies world/writer/epoch and immutable
command/revision/direction/amount; the qualified DB read/commit validates those.

After a positive coordinator result, it rechecks slot ownership, io/extra,
identity and wallet stability. A changed binding discards returned bytes and
reports UNKNOWN, retaining the original durable pending request for recovery.
It never updates the live creature. This is not full persisted-state equality:
inventory, stats and other writers must still be serialized/fenced by the live
caller. Initial live wallet equality with the DB snapshot is not established.

Test-first link failed before implementation. Unit ASan/UBSan tests now prove
exact tuple mapping, successful output, absent MUD1 session, malformed/nonterminated
identity, another player in the slot, invalid fd, onboarding mode, invalid gateway,
and changed slot/session/wallet/io during the call. The real PG withdrawal fixture
now populates a disposable Ply slot and invokes this wrapper, reaching qualified
read -> Rust -> durable preparation -> commit and exact original-request retry.
This fixture is not a full user-driven bank command in the running game.

Frozen source `00c25bb` full local ARM64 runner exited 0:
`/tmp/muhan-bank-live-binding.log`, including native sanitizers, actual C onboarding
scenario, DB recovery and both restore profiles. No production link/install,
feature enablement, deployment or CI run. Still required: command parser/all-money
handling, live current-state/revision validation and application, pending-work
fences, startup recovery and every other player-save path before authority cutover.

## Real C provision completion requires fresh admission — 2026-09-07

Source `a6817db` changes the real C provision completion path to emit its exact
ACTIVE control and disconnect, matching the one-shot claim path. It no longer
assigns the gameplay command callback to the wizard descriptor. Disconnect
removes its input buffer and transient identity; the normal admission path is
required before gameplay. The staged activation still runs once before cleanup.

The actual command1 lifecycle test first failed when required to disconnect
provision completion. After implementation, runtime-on/off tests and ASan/UBSan
pass. The real C socket scenario now checks ACTIVE followed by EOF/no game
output, including a health command coalesced after ACTIVATED. It then reconnects
with MUD2 tickets and successfully runs health/quit commands for Alice and Staged.
These tickets use test-peer session IDs, not a live DB lease: this proves C wire
and socket behavior, not the complete gateway/C/Postgres integration.

Two test-environment assumptions were corrected without deleting the gates:
- SAVED file SHA is checked before COMMIT. Immediate disconnect performs the
  existing uninit/save transition, so its later file need not be byte-identical
  to SAVED. That later output must settle and pass real C re-admission. The
  immutable SAVED/committed receipt assertions remain; this does not establish
  that legacy disconnect writes are safe under a future DB authority cutover.
- The chmod-based FileStore failure test must run unprivileged. The Docker
  runner now creates an owned private source copy on tmpfs and drops to uid 1000
  for this scenario. It does not relax host source permissions or skip the test.

Frozen source `3aca42d`: complete local Linux ARM64 runner exited 0, including
real C scenario, lifecycle sanitizers, qualified money/recovery and both backup
restore profiles. Evidence: `/tmp/muhan-onboarding-c-close-final.log`.
Gateway regression suite also passed (146 passed, 5 skipped), and the activation
static gate passed. No deployment, CI run or production feature flag change.
Still required: bind the live bank coordinator and all save paths to verified
session/current DB state and close remaining file-authority gaps.

## Onboarding-to-session path and completion input barrier — 2026-09-07

Correction to the earlier handoff prerequisite: the existing browser and gateway
intentionally close the one-shot onboarding socket after provisioned/claimed.
`onboarding-terminal.tsx` hands the character back to the roster/admission flow;
ordinary `/ws` admission then acquires its own DB lease. Onboarding must not
invent or retain a parallel gameplay lease. The new MUD2 ordinary-admission
path can therefore serve freshly provisioned characters through that reconnect.

The existing provision-to-ordinary-socket integration test now runs with binding
both off and on. It proves onboarding acquired no gameplay lease, then the fresh
socket acquired one and emitted its exact session/gateway IDs, with matching
actor and character (not the onboarding correlation ID). The test uses the real
gateway with a fake MUD and recording authorizer; it does not prove the entire
browser/real-C/real-DB scenario or all claim reconnect cases.

A concrete completion race was reproduced: while claim snapshot binding was
pending, a synthetic socket `drain` unpaused queued browser input and sent it to
the MUD. The test waits for WebSocket pong to prove input arrived before drain.
`resumeInput` now refuses to reopen the completion-only barrier. The regression
covers successful/rejected binding and missing ACTIVE, retaining queued input
until teardown rather than allowing gameplay without fresh ordinary admission.

Verification: gateway typecheck passed; full gateway suite 146 passed/5 skipped;
onboarding integration suite 35/35 passed in each of five repeated runs.
Evidence: `/tmp/muhan-onboarding-drain-red.log`,
`/tmp/muhan-onboarding-drain-green.log`, and
`/tmp/muhan-onboarding-drain-repeat-1.log` through `-5.log`.
No CI, Docker build, deployment, or production configuration change. Still
needed: real C socket/queued-input completion verification and live bank
coordinator/authority binding; this does not establish full gameplay cutover.

## Opt-in ordinary-admission DB session handoff — 2026-09-07

Source `1d78559` introduces an optional MUD2 ticket:
`MUD2|expiry|nonce|actor|character|nameHex|session|gateway|hmac`.
The HMAC covers the entire prefix, including the exact DB lease session and
gateway instance ID. `MUD_SESSION_BINDING_ENABLED=true` makes ordinary gateway
admission emit this format; the default remains MUD1. A bound gateway identifier
is restricted to 1–128 ASCII letters/digits/dot/dash/underscore. Invalid or
partial binding is rejected, never silently downgraded. The existing MUD1 OK/ERR
response is unchanged; deploy the accepting C runtime before enabling emission.

C keeps the old MUD1 limit at 256 and permits at most 384 bytes for MUD2.
The actual admission callback receives the separate 1,025-byte `buf` in
`handle_commands` (IBUFSIZE 1024), not the 256-byte alias command array.
Ordinary login copies the verified session/gateway into descriptor-owned `extra`,
which is freed on disconnect and is not serialized with `creature`. MUD1 leaves
these fields empty. MUD1O activation explicitly clears them: onboarding lease
handoff still needs implementation and must not reuse a nonce/correlation ID.

Evidence:
- Test-first cross-language test failed because old output was MUD1.
- C/Gateway conformance: all 3 tests passed, including unchanged IDs, tampered
  binding rejection, replay rejection, maximum gateway length (>256-byte wire),
  partial/invalid binding and legacy empty session fields.
- Gateway suite: 145 passed, 5 intentionally skipped; typecheck passed. The new
  real gateway/socket test compares emitted session/gateway to the acquired lease.
- Linux frozen-source `e49238a` full runner exited 0:
  `/tmp/muhan-session-admission-linux.log`. Includes actual command1.c compilation,
  existing admission ASan/UBSan, DB money/recovery and both restore profiles.
  The new MUD2 conformance cases ran on the local C oracle; they are not yet a
  full live-login C socket scenario or a sanitizer test of every MUD2 boundary.

No deployment, authority switch, or production config change. Remaining work:
onboarding handoff, actual selector/coordinator binding to these transient IDs,
live state/revision checks, pending recovery, and all non-bank player writers.

## Selected-authority legacy bank fence — 2026-09-07

Source `bc6db47` closes the remaining file-bank entry points when the existing
compile-gated routing policy selects DB authority or reports a selection error.
`bank_inv` (including its implicit missing-file initialization), `bank`,
`input_bank`, `output_bank`, `drop_all_bank` and `get_all_bank` now return before
legacy storage or inventory handling. They explicitly report that DB-backed
inventory/query support is not ready, rather than reading a stale file mirror.
No installed policy, or an explicit legacy selection, preserves legacy mode.

The actual bank.c characterization test reproduced the bypass (exit 134 at a
forbidden inventory helper) before the fix. After the fix, native ASan/UBSan
tests pass for selected/error policies and six entry points; file load/save and
player-save counters remain zero. Full frozen-source local Linux ARM64 runner
exited 0: `/tmp/muhan-bank-legacy-fence.log`, including paired money transactions,
recovery and both restore profiles. This is a cutover prerequisite, not completed
DB item transfer functionality. The production compile gate remains off.

### Confirmed session-binding prerequisite

`services/gateway/src/gateway.ts` creates `sessionId` and obtains the DB lease,
but `createAdmissionTicket` currently signs only expiry, nonce, actor, character
and legacy-name bytes. `src/trusted_admission.h` and `src/mstruct.h` retain no
DB session ID or gateway instance ID. Both ordinary admission and onboarding
activation populate actor/character identity, not that qualified DB tuple.
Never substitute `admission_nonce` or a newly invented ID for the DB session.

Next connection work requires a versioned, authenticated session-binding handoff
and transient C session storage, covering both existing-character admission and
post-onboarding activation, including disconnect cleanup. Wire-size limits and
the socket command buffer must be checked together before expanding the current
256-byte admission protocol. This turn does not change the protocol or install
a live bank selector. Other player-file writers still require separate fences.

## Native fresh-command coordinator — 2026-09-07

Source `9997fd4` composes qualified native read, Rust subprocess planning,
durable preparation and qualified commit into `bank_money_coordinate_native`.
The supplied expected revision must equal the DB-read revision. There is no
automatic replan or new command UUID on failure. Only a new CONFIRMED commit
returns owned planned bytes; all failures and historical RETRY clear the output
so the caller cannot accidentally apply an old retry snapshot as a new wallet.
The caller still owns live-state serialization and applying validated snapshots.

Test-first evidence: the new coordinator contract failed to compile before the
API existed. Unit tests now pass with ASan/UBSan, checking the full argument and
payload chain, every commit status, stopped read/plan phases, stale revision and
invalid input. In the real PostgreSQL fixture, the withdrawal now uses this
single C call rather than Node sequencing the adapters. It rejects a stale
revision before creating a pending record, commits the exact expected pair,
then retries using the original immutable request via the recovery transport.

The frozen-source Linux ARM64 suite exited 0 with this source:
`/tmp/muhan-native-coordinate.log`, including both database restore profiles.
The coordinator is not installed in the live bank route and is not linked into
the deployed runtime. Still required: runtime session/identity binding, command
parsing including all-money commands, pending-work fences and startup recovery,
safe application to current live state, and fencing/migrating other writers.
Each phase is bounded separately; this synchronous API is not a nonblocking
game-loop integration. No deployment or production privilege changes occurred.

## Latest native durable preparation verification — 2026-09-07

Source `e2855b1` adds the C prepared-commit wrapper: the trusted Node helper
persists the exact eleven arguments and framed payload using the existing
fsynced pending store, then echoes the frame. C sends the database commit only
after successful preparation and byte-identical echo. Missing directory or
conflicting same-command content returns NOT_SENT without changing DB state.
Preparation failure can still leave a durable record; it must be retained.

Source `ce341fc` removes parent-side preparation from the acknowledgement-loss
integration test. The pending record is initially absent; the real C sender
creates it, commits to PostgreSQL, loses its reply and reports UNKNOWN. A new
process reads that record and receives EXACT_RETRY revision 1. Changed request
content and corrupted records are rejected; there is exactly one intent.

The complete frozen-source local Linux ARM64 runner exited 0:
`/tmp/muhan-native-prepared-ack-loss.log`. This includes native preparation
failure/conflict probes, acknowledgement-loss recovery, C/Rust differential
tests and both canonical/tree-inventory database restore profiles with C/Rust
byte verification. No GitHub Actions, image build or deployment was performed.

This is not a live authority cutover: the actual bank command still needs a
coordinator binding current player/session identity, read/plan/prepare/commit,
pending recovery and other legacy writers. Item transfers and startup recovery
installation remain incomplete. A historical EXACT_RETRY must not overwrite a
newer live wallet. Power-loss/fsync fault coverage is not established here.

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

### Native C qualified reader

Source `58a0f0c` adds an isolated libpq C reader, borrowing an already connected,
idle writer session. It sends the seven authority parameters separately and
uses nonblocking libpq plus monotonic poll deadlines (1..10000 ms) for the query
exchange. It requires exactly one binary-format row with the expected field
types, nonnull revision/payload/digests, bounded payload sizes and lowercase
digests. It copies owned data into the same paired Rust input frame and publishes
nothing on failure. A failed connection must be discarded by the caller; the
adapter does not claim to recover an unfinished query or establish connections.

The actual native executable now supplies both payloads to the real Rust
deposit/withdraw -> qualified DB commit integration. Its returned revision,
digests and full bytes match independent database observations. Wrong-actor
native reads produce no frame. Holding the pair lock with server lock and
statement timeouts disabled proves the two-second native deadline returns
failure with no output before the five-second process watchdog (test bounds
1.5..4.5 seconds). The test's separate connection setup has a three-second limit.

ASan/UBSan instrument the reader and its real PostgreSQL harness. The entire
local runner passed exit 0, including both restores:
`/tmp/muhan-native-money-read-final.log`. No libpq dependency was added to the
default runtime object list. Native commit transport, command identity and
uncertain-result recovery, live session binding and exclusive write fencing
remain required before installing this into the actual game coordinator.

### Native C qualified commit transport

Source `e8fd6c0` adds the native commit adapter and shares the bounded
nonblocking exchange with the native reader. Eleven text parameters and two
binary bytea payloads call the qualified commit RPC. A successful response must
have the exact two-column binary shape, COMMITTED or EXACT_RETRY, and expected
revision + 1. Only those outcomes publish a revision. Local malformed frames
are invalid; explicit recognized server rollback/error states are rejected;
transport timeout, absent/malformed acknowledgement and unrecognized errors
remain UNKNOWN. Unknown outcomes require retaining the same command/payload
and discarding the connection, never generating a replacement command.

The actual integration now runs native C read -> Rust planner -> native C
commit against the disposable DB for deposit and withdrawal. A fresh native
process reconnects with the same arguments and gets EXACT_RETRY, with two total
intents and matching authority records after the two operations. Wrong actor
gets rejection and no success output. With server timeouts disabled and the
pair locked, the native two-second deadline reports UNKNOWN with no output,
before the process watchdog. That timeout probe deliberately uses a wrong
actor to prevent a late commit; it proves classification and bounded waiting,
NOT post-commit acknowledgement-loss recovery.

Both native adapters and real-PG harnesses are ASan/UBSan instrumented. Full
local runner passed exit 0: `/tmp/muhan-native-money-commit.log`, including all
existing C/Rust and restored-DB profiles. The native transport is still not
installed into the live command coordinator. Durable pending-request recovery,
actual acknowledgement loss, native planner invocation, command/session
binding and legacy write fencing remain before activation.

### Actual committed acknowledgement loss

Source `e746f14` adds a disposable loopback relay around the actual native
commit executable. Authentication and query submission reach the local PG
server; server replies are withheld after the qualified commit query begins.
While the C process still awaits its reply, an independent DB connection proves
revision one and both intended payloads are already committed. The test also
asserts response bytes were actually withheld. The native client reaches its
own deadline, returns UNKNOWN and emits no success output.

A fresh native process reconnects directly to the disposable DB with the exact
same command, authority tuple, expected revision and planned bytes. It receives
EXACT_RETRY, including a second retry, before the ordinary withdrawal proceeds.
Final full-byte roundtrip and two total intents/authority rows prove no duplicate
deposit. The relay uses an ephemeral loopback port inside the isolated test
container, no external service or published host port, and closes only its own
sockets/process. This is test-only failure injection, not application routing.

Full local runner passed exit 0: `/tmp/muhan-lost-money-ack.log`, with the existing
native sanitizers and both restored profiles. This closes actual post-commit
reply-loss classification/reconnect retry for a still-valid authority tuple.
The test retains command bytes in its parent process; durable recovery after
the entire runtime restarts, expired session/writer reconciliation, live command
installation and exclusive legacy-write fencing remain outstanding.

### Durable request and fresh-process replay

Source `96448e2` adds a Linux pending-request store containing the exact eleven
commit arguments and proposed paired frame in a canonical digest-bound record.
It checks bounded identity/numeric/frame shape; gameplay semantics remain the
Rust/DB responsibility. An owned mode-0700 directory is held by descriptor;
operations use its `/proc/self/fd` capability, final symlinks are refused, and
records must be regular owned mode-0600 single-link files. Publication writes
and fsyncs a private temporary file, links without replacing an existing command,
removes its own temporary link, then fsyncs the directory. Identical prepare is
an exact retry; changed command content conflicts. No credential is recorded,
and no production delete/acknowledge lifecycle is implemented yet.

The actual lost-ack integration prepares the request before sending. After
the native sender exits UNKNOWN, a fresh Node process receives only the private
directory and command filename, reloads the recorded arguments/bytes, launches
a new native commit process, and gets EXACT_RETRY. It is not supplied the old
frame or authority tuple by the parent. Changed intent is rejected without
overwrite; appending corruption prevents replay and emits no success output.
The database still has one deposit intent, followed by the normal withdrawal.

Full local runner passed exit 0: `/tmp/muhan-money-pending-recovery.log`. This
proves file-backed recovery across sender/replayer process lifetimes while the
test coordinator and original authority remain available. It does not yet
prove machine/power-loss recovery at every fsync boundary, discovery/draining
after complete service restart, expired-authority reconciliation, or wiring
into live C command handling. The existing legacy journal was not reused
because it encodes legacy-file promotion rather than a two-payload DB command.

### Reconciliation after old authority expires

Migration 230 adds a read-only reconciliation RPC. It requires the actual
writer login, a current unsealed/unexpired recovery writer lease and unchanged
active character ownership. It compares the complete original authority tuple,
command, expected revision, direction, amount and both proposed byte payloads
against immutable command/intent/authority records. Matching history returns
CONFIRMED with its original committed revision; missing history is UNRESOLVED,
not permission to issue a new command. The function never refreshes the old
session, replays a write or mutates player/bank data. Production grants stay off.

Source `8bde360` passed the full local runner, exit 0:
`/tmp/muhan-money-reconcile.log`. After both money operations the test expires
the old session, seals/expires writer epoch one and acquires epoch two for a
successor. Old commit replay now fails, while the successor can confirm the
exact epoch-one deposit. Changed original session/amount/actor and wrong recovery
epoch are rejected; a missing command remains UNRESOLVED. Calling as admin
fails the login check. Snapshot revision/bytes and the two intent rows remain
unchanged. The disposable recovery grant is revoked and verified absent.

This is historical confirmation, NOT current wallet state: a confirmed revision
one may coexist with current revision two, and must not overwrite live memory.
The native recovery transport and restarted coordinator still need to consume
this result, drain durable requests and obtain current state under a fresh
game session before resuming commands. Live authority remains disabled.

### Fresh coordinator request discovery and reconciliation

Source `a37621b` adds an explicitly enabled, single-pass recovery CLI. It opens
the private pending directory by capability, discovers request files without
receiving command identifiers, validates and processes one request at a time,
and calls only the read-only reconciliation RPC under the configured current
writer lease. Connection/query/server-lock deadlines are explicit. Reports
contain counts only: confirmed, unresolved, invalid, errors and truncation.
Discovery stops after 1000 directory entries and reports truncation rather
than claiming a full scan. Requests are never replayed, deleted or rewritten.

The real PostgreSQL integration starts this CLI as a fresh process after the
old session/epoch are expired and a successor holds epoch two. A directory
with one historical request confirms successfully. Adding a missing command
and a corrupted record produces one confirmed, one unresolved and one invalid;
two fresh invocations return the same report, preserve every file byte and
leave the paired state and intent count unchanged. No old authority tuple or
frame is passed to these processes outside the durable records.

Full local runner passed exit 0: `/tmp/muhan-money-recovery-startup.log`.
This is an executable recovery entrypoint, not an installed startup service.
Confirmed-request acknowledgement/retention, paginated draining, live C
coordinator installation, current-session state refresh and legacy write
fencing remain. No K8s deployment, production grant or CI run was performed.

### Native C invocation of the Rust planner

Source `6d07856` replaces direct Node->Rust invocation in the qualified
integration with a native C process bridge. It executes one explicit trusted
absolute binary without a shell, passes only the four planner arguments and a
minimal locale environment, and closes inherited descriptors above stderr.
Concurrent nonblocking local socket IO prevents input/output pipe deadlock;
complete frames are bounded to two 4-MiB payloads plus headers. Failure,
oversize/malformed framing, nonzero child exit or deadline emits no result.
The bridge reaps its child and does not mutate game or database state.

The real C read -> C-launched Rust plan -> C commit flow passes along with
wrong-digest/funds rejection. A deliberately stalled child reaches the native
two-second deadline, is killed/reaped, and yields no output before the outer
watchdog; database state remains unchanged. Native bridge ASan/UBSan and the
full local replay/restore suite passed exit 0:
`/tmp/muhan-native-rust-bridge-final.log`.

Concrete runtime integration prerequisite found in `src/io.c`: SIGCHLD
increments `Deadchildren`, and `reap_children` currently uses blocking `wait`
based on that counter. Independently reaping the planner could leave a stale
counter and block the game while another child is alive. The bridge is therefore
NOT installed into the live coordinator yet. Reaper ownership/nonblocking
collection must be tested and adjusted before activation, along with the
existing pending-request integration and legacy-write fencing requirements.

### Nonblocking legacy child collection

The stale SIGCHLD failure was reproduced by linking the actual `src/io.c`
reaper with an already-reaped child and another still-running child. Before
the fix, the one-second watchdog fired (exit 90; it cleaned its own sleeper):
`/tmp/muhan-child-reaper-red.log`. No real auth files were accessed.

Source `ef6e0a0` treats `Deadchildren` as a volatile signal-safe hint rather
than a count. The reaper clears it before draining `waitpid(-1,WNOHANG)`, retries
EINTR, and processes every returned PID through the existing auth path. It no
longer blocks on a live child or silently discards one result in a final wait4.
Signals during draining remain hints for another harmless nonblocking pass.

The actual reaper regression now passes under ASan/UBSan: stale notification
with a live child, multiple waitable children behind one notification, no-child
notification, and the existing recognized authentication result update. Auth
file operations are replaced only in the test, using an anonymous fixture file.
The full local replay/restore runner passed exit 0:
`/tmp/muhan-child-reaper-final.log`. This resolves the observed reaper blocking
prerequisite; it does not install the native money coordinator or prove a full
live session. Durable preparation integration and legacy-write fencing remain.

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
