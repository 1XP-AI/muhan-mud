# Live bank capture and transaction gap

## Persist newer equipped state after finishing the old request — 2026-09-08

RED source `e966f11` strengthens the independent Node DB checks to require
Peerhero revision 2 and gold 202, both durable resolved requests, the next
request's baseline revision/hash, and exactly two immutable intents. The
frozen local runner exited 1 at the intended assertion (actual revision 1,
expected 2), evidence `/tmp/muhan-newer-save-red.log`.

Source `763c5b7` extends the actual C savegame fixture after finishing the
older frozen request: save the preserved newer equipped state under the new
command, require COMMITTED revision 2 and EXACT_RETRY, refuse registry removal
while pending, then finish/release/adopt to revision 2. The original live
creature and equipment graph must remain unchanged. The independent parent
checks the old payload remains gold 201, the newer payload is gold 202 with
the same equipment bytes, bank bytes are unchanged, and a Rust deposit plan
preserves that full newer payload except for the intended gold change.

This closes the two-request save sequence in the native fixture, not the full
runtime disconnect integration. The newer state is explicitly mutated by the
fixture; real uninit/update execution, owned queued exit-state orchestration,
startup recovery and production login installation remain outstanding.

GREEN frozen source `763c5b7` completed the full isolated Linux ARM64 runner
with exit 0 observed through process handle 57930; evidence
`/tmp/muhan-newer-save-green.log`. Existing C/Rust differential, native DB,
sanitizer, onboarding and legacy restore profiles also passed. No push,
Actions execution, deployment or production authority switch was performed.

## Finish older frozen requests independently of gameplay — 2026-09-08

Source `7ee9ba4` adds `player_session_store_finish_pending`: validate the next
command, decode the exact retained pending payload into an owned clone,
recognize an already verified release when possible, retry an unconfirmed
request using only that clone, then perform native release and adoption.
The caller retains exclusive ownership of newer gameplay and must save it
under the next command after success. No newer live player is passed to this
API, and no pending payload is replaced by newer state.

The C/PG fixture sets Peerhero's newer in-memory gold to 202 while the retained
request represents 201, resets acknowledgement status to UNKNOWN, and finishes
the old request. It requires the newer creature/equipment graph to remain
unchanged, the context to advance to revision 1 and a new command with no
pending payload, and eventual registry removal to succeed. The independent
Node parent still requires DB gold 201, revision 1, the full equipment-derived
payload, and exactly one immutable intent. This is an exact-retry case, not a
new first commit or a real socket disconnect.

The RED compile ran in the retained Linux image (the host lacked libpq
headers). Startup reconstruction of a context whose release record exists but
whose acknowledgement is unknown is not implemented here. This is not a
restart loader, writer takeover coordinator, or automatic disconnect hook;
the real uninit/update integration and saving the newer exit state remain.

Frozen source `7ee9ba4` completed the full isolated Linux ARM64 runner with
exit 0 observed through process handle 86799; evidence is
`/tmp/muhan-finish-pending.log`. Existing native DB, C/Rust differential,
sanitizer, C onboarding and legacy backup/restore profiles passed. This
provides no production cutover or complete new-ledger disaster-recovery proof.

## Executable disconnect persistence boundary — 2026-09-08

Source `64e04d1` extracts the existing io.c disconnect persistence block into
`player_disconnect_persist`, called by the real disconnect path after its
socket/io and spy cleanup. No storage authority or ordering is changed:
saveable players are uninitialized before save, successful saves free the
player, failures transfer ownership to recovery, and exhausted recovery keeps
the pointer for the existing fatal server-stop path. Non-saveable players
(including claim-lane cleanup) are freed without save.

The new fixture failed to compile before this boundary existed, then passed
with ASan/UBSan. It links the actual io.c function with controlled uninit,
save, queue and free collaborators, checking order, ownership transfer and
failure retention. This is not full socket disconnect or real uninit/update
coverage. In particular it does not yet coordinate an older native pending
request with the newer post-uninit state. The extraction makes that production
boundary directly testable without reproducing disconnect code in a fixture.

Frozen Linux ARM64 source `64e04d1` completed the full local runner with exit
0 observed through process handle 79223 (`/tmp/muhan-disconnect-boundary.log`).
The claim credential lifecycle static guard also passed. Existing native DB,
C/Rust differential, onboarding and legacy restore gates remain green. No
Actions, push, deployment or production DB authority switch was performed.

## Native release authority and failure preservation — 2026-09-08

Source `34203d7` moves the two-save fixture's release decision from the Node
test parent into C. C reconciles the exact immutable request, checks its
committed revision, reselects the same character/owner under writer authority,
and compares the current DB snapshot with the pending bytes. Only then does
it invoke the trusted private-directory release recorder. The parent now
independently verifies the persisted resolved record rather than authorizing
the release itself.

Source `b489304` adds native negative cases: a fixture-owned disconnected DB
socket, a mismatched committed revision, and a mismatched captured owner.
Every rejected release must preserve the complete context; after restoring
the correct context, adoption must still fail until a real release succeeds.
These are caller-context mismatch tests, not simulated database owner turnover.

Frozen full local Linux ARM64 runner at `b489304` exited 0 through process
handle 24488; evidence `/tmp/muhan-native-release-negative.log`. The run
includes the native two-save/registry fixture, existing recovery and money
contracts, C/Rust differential checks, sanitizers, actual C onboarding, and
the legacy canonical/tree-inventory backup/restore profiles. It does not
prove complete restoration of the new intent ledger or production readiness.
The earlier `34203d7` log reached restore success but its terminal exit code
was unavailable; this new observed exit is the authoritative full-run result.

The helper's `--record-verified-release` mode is an internal filesystem
recorder, not a standalone DB verifier. C's DB reads precede its filesystem
lock; this is not an atomic DB/filesystem transaction. The caller must retain
per-character authority throughout release and adoption, and subsequent writes
still require the shared fence and DB CAS. This API is not installed in the
production login/disconnect path. The pending pre-disconnect versus post-uninit
state transition remains an integration gap; no production cutover is claimed.

## Never-committed queue recovery — 2026-09-08

Source `47b686c` adds the previously missing first-commit recovery case. Before
Peerhero's first actual savegame call, a separate fixture-owned DB connection
is shut down. Save returns UNKNOWN/IO_ERROR with a durably prepared request.
A healthy independent qualified read proves revision 0, gold 100 and empty
inventory still exist: this is not just a lost acknowledgement of a commit.
The real queue retains the decoded candidate while disconnected; restoring
the healthy connection produces COMMITTED revision 1 and releases its owned
clone. Later exact retries and the existing post-commit outage scenario yield
one immutable DB intent total, with the equipped-derived payload preserved.

Frozen full local ARM64 runner at `47b686c` exited 0 via its process handle;
evidence `/tmp/muhan-player-first-recovery.log`. Existing two-character save/
release/adoption, native money/recovery, sanitizer/differential, real C
onboarding and restore profiles remain passing.

Disconnect source inspection also confirms io.c frees io/extr before calling
uninit_ply and save_ply, then transfers failed players to the recovery queue.
uninit_ply invokes update_ply, which updates LT_HOURS and may expire effects.
Consequently a pending pre-disconnect payload can differ from post-uninit
state: runtime orchestration must reconcile the former before freezing a new
command, not overwrite the pending request or use a new UUID blindly. Actual
io/uninit execution has not yet been integrated in this fixture; this remains
a production cutover gap. No production grant, push, Actions or deployment.

## Actual recovery queue with DB connection loss — 2026-09-08

Source `2b19d85` links real player_recovery.c into the C/PG registry fixture.
The equipped-derived canonical clone is enqueued twice by the same pointer;
the queue owns exactly one fd-less player. The test shuts down only a separate
fixture-owned PostgreSQL socket. Retrying through the actual queue and native
adapter returns IO_ERROR/UNKNOWN, keeps the player queued, keeps the login
block, and preserves the exact pending payload. Registry removal stays refused.

After restoring the healthy borrowed connection, the same request returns
EXACT_RETRY. The actual queue removes its entry and real files1.c free_crt frees
the complete clone; ASan/UBSan reports no ownership errors. A further retry
returns NOT_FOUND. The separate context/durable pending lifecycle remains
retained and cannot be removed merely because the queue is empty.

Frozen full local ARM64 runner at `2b19d85` exited 0 through its process handle;
evidence `/tmp/muhan-player-recovery-queue.log`. Existing equipped savegame,
two-character/two-save cycles, DB recovery, C/Rust differential, actual C
onboarding and both restore profiles remain passing.

Scope: this uses a real disconnected fixture DB socket, not a crashed DB server,
and confirms an already committed request. The clone is enqueued explicitly;
actual io.c/uninit disconnect wiring, never-committed queue recovery and process
restart ownership/admission remain integration gaps. No production grant, push,
Actions execution or deployment.

## Equipped actual savegame persistence — 2026-09-08

Source `6af8a5c` exercises actual savegame_nomsg with one inventory root and
one equipped root. The fixture independently encodes the expected persisted
unequipped inventory before calling savegame, then requires byte-identical
pending data. Decoding proves Apack and Zblade appear exactly once, with no
extra root or persisted ready slot. After save and exact retry, memcmp checks
the original creature and objects, and the original inventory tag chain is
unchanged. This runs real add_obj_crt/del_obj_crt, not mocked list operations.

Node reads matching DB and durable-request bytes, unchanged bank and gold 201.
The digest-bound Rust planner accepts this equipped-derived DB snapshot and
produces precisely the same full player bytes except gold 200 for a planned
deposit. That plan is not committed in this assertion. Existing Savehero
two-save/release/adoption cycles run while Peerhero remains pending.

Full frozen local ARM64 runner at `6af8a5c` exited 0 via the process handle;
evidence `/tmp/muhan-equipped-savegame-db.log`. All native/recovery/locking,
sanitizer/differential, actual C onboarding and restore checks still pass.

This covers one equipped root plus one inventory root. Full equipment-slot,
nested-container and actual uninit/stat-normalization behavior still need
coverage. Actual disconnect and player_recovery lifecycle integration remain
uninstalled. No production grant, push, Actions run or deployment.

## Actual savegame command through DB registry — 2026-09-08

Source `a6b59b4` links the real command8.c savegame_nomsg and player.c object
list routines into the native session fixture. Both characters now save through
savegame_nomsg, including both Savehero commit/retry/release/adoption cycles.
This exercises the actual temporary creature allocation/copy and save_ply
dispatch rather than a hand-written call to the storage seam. A changed
pending request returns PLAYER_STORE_IO_ERROR and invokes the nonfatal game
error callback exactly once; later valid retries do not add errors. FileStore
callbacks remain fatal test guards. Production command code was unchanged.

Frozen full local ARM64 runner at `a6b59b4` exited 0 through its process handle;
evidence `/tmp/muhan-real-savegame-db.log`. Actual PG exact bytes, bank
preservation, durable request/release, cross-character independence, C/Rust
differential, ASan/UBSan, real C onboarding and both restore profiles passed.

This fixture currently has no ready-slot equipment, so it does not prove the
equipped-object branch of savegame. Interactive savegame output, actual uninit/
io disconnect, player_recovery queue ownership and production admission remain
separate integration gaps. No production grant, push, Actions run or deployment.

## Native multi-character registry — 2026-09-08

Source `20a97c7` adds a zero-initialized caller-owned registry exposing the
actual PlayerStore facade. It routes exact names to configured contexts in one
world; duplicate bindings, unknown names, inconsistent contexts and capacity
exhaustion fail closed without FileStore fallback. It uses 64 bounded slots.
Removal refuses a busy context or one with an in-memory pending request; it
does not free contexts, delete disk evidence or determine whether gameplay has
been drained. Caller-owned context lifetimes must cover the active binding.

The C/PG fixture initially failed compilation with the registry absent. It now
binds two real contexts: Peerhero commits gold 201 and leaves its request
pending while Savehero completes both save/release/adoption cycles. Exact DB
bytes, bank preservation and Peerhero's separately keyed pending payload are
read back by Node. Duplicate registration and unknown-name load are rejected;
pending Peerhero cannot be removed; drained Savehero is removed and subsequent
save through its old name is rejected. FileStore callbacks still abort.

Full frozen local ARM64 runner at `20a97c7` exited 0 through its process handle;
evidence `/tmp/muhan-player-registry.log`. Existing native bank, recovery,
cross-operation locks, sanitizer/differential, actual C onboarding and both
restore profiles remain passing.

This is not yet attached to production login or disconnect. Authentication,
registry admission/removal orchestration, actual savegame/uninit/recovery queue
integration and sizing for the configured player capacity remain. The current
test proves two contexts, not a populated-capacity stress test. No production
grant, push, Actions execution or deployment.

## Two consecutive native saves — 2026-09-08

Frozen source `0731250` extends the actual C/PG fixture beyond context-only
adoption. The same loaded context saves gold 101 at revision 1, retries,
waits for independently verified release, adopts the next command, then saves
gold 102 at revision 2 and retries again. A second verified release advances
the context baseline to revision 2. Node reads the exact second DB payload,
unchanged bank bytes and second durable request; that request carries revision
1 and the full hash of the first committed payload, not the original hash.

The subsequent CAS race now starts at revision 2 and produces one revision-3
winner. Historical first-request confirmation still returns revision 1 without
rolling the head back. Three immutable intents are present (two sequential C
saves and one concurrent SQL winner). No runtime change was needed to pass
this stronger test.

Full isolated local ARM64 runner at `0731250` exited 0, observed through its
process handle. Evidence `/tmp/muhan-player-two-saves.log`; existing native
money, cross-operation/recovery, sanitizer/differential, actual C onboarding
and both restore profiles remain passing.

This closes the second-write coverage gap, not the full port. The context is
still per-character, and no production router/admission/disconnect lifecycle
is installed. Multi-character orchestration and actual gameplay savegame/
uninit/recovery queue integration remain. No push, production grant, Actions
execution or deployment.

## Verified native baseline adoption — 2026-09-08

Source `99b597b` adds explicit native adoption into a fresh command ID. It
requires a confirmed/retried pending payload, equal normalized live memory,
current DB character/owner/revision, byte-identical decoded DB state, and exact
durable request/resolution evidence with neither player nor money reservation
remaining. Only then does it replace command/revision/hash and clear the
in-memory pending buffer. It does not delete history or mutate the creature.
Every subsequent write still needs the shared reservation and DB CAS; the
checks are not a promise that no other writer can change state afterward.

The actual C fixture initially failed compilation with the adoption API absent.
Its integration now keeps the same C process alive after save/retry: adoption
before release is rejected, an independent Node/PG connection verifies release,
then C rejects mismatched live memory and accepts the original state. The
context advances from revision 0 to 1 and receives the exact requested fresh
command ID while retained request bytes remain readable. A bounded parent
watchdog terminates only its own child if this handshake stalls.

Full frozen local ARM64 suite at `99b597b` exited 0 via its process handle;
evidence `/tmp/muhan-player-adoption.log`. Existing C/PG, money/recovery,
cross-operation, sanitizer/differential, real C onboarding and restore profiles
all remain passing.

The test proves context advancement, not yet a second completed write within
that context. Multi-character routing, repeated-save/release orchestration,
actual disconnect/recovery ownership, and authenticated live admission still
need integration before production authority cutover. No push, deployment,
production grant or Actions execution.

## Native per-character PlayerStore baseline — 2026-09-08

Source `ed782c9` adds a caller-owned per-character/load/command adapter using
the actual PlayerStore interface. Load resolves and revalidates DB identity,
then copies original character UUID/revision/hash into the context. It rejects
saves before load and repeated load in that lifetime. Save captures the complete
normalized player, freezes the first pending payload, and uses the prepared
native transaction with the original baseline. Different subsequent bytes are
refused; neither successful commit nor retry silently refreshes the baseline.
No fd, Ply index or creature pointer identity selects persistence authority.

The fixture initially failed compilation because the adapter was absent.
After implementation, the real PostgreSQL suite binds this adapter to actual
load_ply/save_ply. A shallow player copy with fd=-1 commits gold 101, retries
the same request, then rejects a changed payload. Original revision stays 0;
attempting FileStore load/save aborts the fixture. The managed binding restores
its prior provider on exit. Detached clone/context allocations pass ASan/UBSan.
The surrounding test reads exact DB player bytes, preserved bank and durable
request, and continues through verified release and money preparation.

Full frozen local ARM64 runner at `ed782c9` exited 0, observed via process
handle; evidence `/tmp/muhan-player-session-store.log`. All existing native bank,
cross-operation/recovery, differential, actual C onboarding and both restore
profiles still passed.

Limits: this is one character/one command lifetime, not a multi-character
runtime router. Context disposal frees memory only and never releases durable
evidence. Production still needs registry ownership, explicit confirmed
release/adoption into the next command lifetime, login credential/admission
initialization, and actual savegame/uninit/recovery-queue orchestration.
The fixture uses a shallow fd-less copy, not a complete live disconnect event.
No production installation, grant, push, Actions run or deployment.

## Verified player reservation release — 2026-09-07

Source `e8f13b8` adds serialized player release. Under the shared character
lock, it requires the exact active fence, independently confirms the immutable
request through reconciliation, then reads the current player at precisely
expected+1 using the current writer. Payload bytes and full hash must match.
Historical confirmation after the DB head advances is insufficient. The
original request and matching `.player-resolved` record are durably published
before removing only the matching fence. No gameplay memory is updated.

Claim refuses resolved command reuse. Preparation visitors skip only exact
resolved request copies; an active fence is never skipped. Recovery retains
its history-inclusive default. Tests prove unconfirmed release leaves ownership,
the release lock excludes a successor claim, history survives fence-only release,
and a stale release cannot remove a later reservation. Actual PostgreSQL tests
reject release before commit, accept after native prepared save/retry, preserve
history, and permit money preparation in the same directory afterward. They
also reject a confirmed historical request once its DB head has advanced.

Frozen full local ARM64 runner at `e8f13b8` exited 0 via its process handle;
evidence `/tmp/muhan-player-release.log`. Existing money/cross-operation fences,
native C/PG, sanitizer/differential, actual C onboarding and both backup profiles
remain passing.

Still required: the caller must own and adopt the live baseline; this API does
not attach a decoded player or update revision state across copies/disconnect.
General player authority remains uninstalled. Recovery of old confirmed requests
whose head has advanced needs a separate explicit adoption lifecycle, not blind
release or state rollback. Power-loss cut-point and player-specific lost-ack
coverage remain incomplete. No production grant, push, Actions run or deployment.

## Historical player save reconciliation — 2026-09-07

Source `2a31207` adds a closed read-only player reconciliation RPC and bounded
pending visitor consumer. The RPC locks and checks the current writer and
active character, then matches original world/name/owner/writer/epoch/source
hash, expected revision and exact payload against immutable intent/command
rows. It returns historical CONFIRMED revision or UNRESOLVED for missing
intent; it never resends, upgrades old authority, mutates the pair or releases
local ownership. The owner recorded by the intent must still own the character.

The real PostgreSQL harness proves the RPC absent before migration, applies
it twice, checks default denial and revokes disposable test grants afterward.
Tests confirm the old revision even after head advancement, reject mismatched
writer/revision/hash/payload and admin identity, and leave unknown commands
unresolved. After writer turnover, the expired writer is refused while its
successor confirms the exact original request. Fence-only player preparation
is discovered; adding its ordinary request copy still counts once. Corrupting
one copy reports invalid evidence while preserving it and the unchanged DB
head. Confirmation does not hide the invalid counter or authorize release.

Full frozen local ARM64 runner at `2a31207` exited 0, observed via its process
handle. Evidence `/tmp/muhan-player-reconcile.log`; existing cross-operation
locks, native C/PG saves, bank flows, sanitizer/differential, actual onboarding
and both restore profiles also passed.

Remaining: independently verified player reservation release/current-state
adoption, live baseline lifetime, and a deployed recovery lifecycle. The new
consumer is callable only, not an automatic daemon. Player-specific lost-ack,
lease-expiry-during-lock, and owner-turnover negative cases still need dedicated
coverage. No production grant, push, Actions run or deployment.

## Shared player/money character reservation — 2026-09-07

Source `b1cfdba` moves the existing kernel flock implementation into shared
pending storage without changing its stable `.money-lock` inode or world/UUID
key. Money claim now rejects a matching player fence under that lock; player
claim rejects a matching money fence and publishes the complete immutable
player request as `.player-fence` before ordinary request publication.
Player publication holds the same directory capability throughout.

Both preparation CLIs now scan pending evidence from both operation types,
reject invalid/truncated discovery, then claim under the shared lock. The scan
protects adoption of existing request-only records; it is not the race arbiter.
Old unfenced senders must be stopped and both paths must share one private
pending directory. A bounded player visitor reads both fence and request forms,
validates the character key and deduplicates byte-identical command copies.

The new mixed-process test races eight distinct money/player commands for one
character and observes exactly one winner. Fresh-process exact retry preserves
the winner's bytes; the opposite operation is refused. Separate characters
test both blocking directions deterministically. Existing money release,
SIGKILL/restart tests and native prepared-save integration remain passing.
Full frozen ARM64 runner at `b1cfdba` exited 0, observed via its process handle;
evidence `/tmp/muhan-cross-player-money.log`. C onboarding, differential,
sanitizer and both restore profiles also ran successfully.

Remaining: player historical DB reconciliation and independently verified
release are absent. Player reservations intentionally remain blocking; this
must not be installed as ordinary gameplay persistence until release/recovery
and baseline lifetime are complete. The new visitor still needs dedicated
player fence-only crash/corrupt-copy coverage beyond preparation preflight.
No runtime authority switch, production grant, push or deployment.

## Prepared native player save — 2026-09-07

Source `3e9eda2` adds an internal prepared-save wrapper: a trusted absolute
helper must return the exact payload after durable publication before the
wrapper invokes SQL. Missing directory, conflicting stored request, helper
failure or mismatched echo returns NOT_SENT with zero revision. NOT_SENT does
not mean no local record exists; interrupted preparation must retain evidence.

The initial real PostgreSQL test failed on the valid request with NOT_SENT.
Investigation traced this to bank_money_process_native enforcing a paired
player/bank frame before spawning. A 1778-byte canonical player is not that
frame. Source `50b2466` adds an independently bounded single-player mode while
preserving every existing bank pair validation. The exact previously failing
test now passes: missing/conflicting preparation changes neither DB state nor
intent ledger; successful native save retains the full original request; a
fresh process retries with identical bytes and receives historical revision 1.

Frozen full local ARM64 suite at `50b2466` exited 0, observed from the process
handle; evidence `/tmp/muhan-player-prepared-fixed.log`. The prior RED is
`/tmp/muhan-player-prepared.log`. Existing bank/native/sanitizer/differential,
actual C onboarding and both backup profiles also pass. Investigate skill
guided tracing the failure to the protocol boundary before changing code.
Its separate learning logger failed because its jsonl-store.ts was missing;
this document is the retained investigation record.

Still internal and not installed as PlayerStore. Raw transport remains available
for focused tests; production integration must choose the prepared path and
also enforce per-character cross-operation fencing, load-baseline lifetime,
pending discovery/reconciliation and verified release. No claim that this
wrapper alone prevents two different command IDs or completes crash recovery.
No production grant, push, Actions run or deployment.

## Immutable player pending records — 2026-09-07

Source `2e3accd` stores the complete player-save request: original world/name,
writer/epoch, character/command, revision, full-source hash and payload. A typed
canonical envelope binds all these bytes with a digest. The helper echoes the
payload only after file fsync, exclusive publication and directory fsync;
existing records are never overwritten. Read validates private owned directory,
file mode/type/link count, size, digest, canonical encoding and command binding.

The directory capability, bounded byte reader and immutable publication were
extracted from money storage into `pending-record-store.ts`, so both transports
use the same implementation. Money envelope formats and fence protocol remain
unchanged. The player test initially failed with its implementation absent;
after implementation it proves fresh-process exact retry with byte-identical
history, conflicting baseline refusal, one winner for conflicting concurrent
publication, and corrupted record refusal with no CLI echo.

Full frozen local ARM64 suite at `2e3accd` exited 0, observed from its process
handle. Evidence: `/tmp/muhan-player-pending.log`. Existing money fence/recovery,
native C/PG save, sanitizer/differential, actual C onboarding and both restore
profiles still pass after the shared storage extraction.

This is durable transport storage, not character-level ownership. It does not
block two different player commands or coordinate with a money reservation;
the native player save does not yet require this helper. Player-specific pending
discovery, exact historical reconciliation after writer turnover, release and
process-death/power-loss fault injection remain required before installation.
The envelope digest is not gameplay validation (native codec and SQL do that).
No automatic retry, record deletion, production grant, push or deployment.

## Native player save transport — 2026-09-07

Source `7a412a7` adds a bounded libpq adapter for the closed general player
save RPC. Its explicit request includes the original loaded revision/hash;
it never looks up a new revision or changes a live creature. It bounds inputs,
decodes the canonical payload before sending, checks the name, and distinguishes
local invalid input, explicit DB rejection, unknown outcome, new commit and
historical retry. Revision output is zero except for a strictly checked binary
acknowledgement at expected+1. Unknown results require keeping the exact request
and discarding the borrowed connection.

The new native fixture first failed compilation because this API was absent.
After implementation the actual PostgreSQL test performs its initial player
save through C, retries through fresh C processes, rejects a stale hash,
noncanonical/overflow revision and corrupted payload, and checks historical
retry after subsequent concurrent saves without head rollback. The full frozen
local ARM64 runner at `7a412a7` exited 0, including ASan/UBSan, native bank
commands, actual C onboarding and both backup profiles. Evidence:
`/tmp/muhan-player-save-native.log`.

This is an internal transport, not an installed PlayerStore callback. Durable
player-request preparation, per-character save ownership across copies and
disconnect, pending-money coordination, and live baseline lifecycle remain
required. Player-save-specific lost-ack and malformed-server-response injection
are not yet covered; money transport tests do not prove those cases for this
adapter. No production grant, push, deployment or Actions run.

## General player save CAS verified — 2026-09-07

Source `19d1a5f` adds closed `commit_player_snapshot` with an immutable,
operation-specific intent ledger. It revalidates the writer/name/character
route after acquiring the paired-state update lock, requires the caller's
original revision and player hash, checks the canonical payload/name, and
commits player bytes while preserving bank bytes under the shared revision.
An exact historical retry returns its original revision without rolling the
current head back; a conflicting command, stale baseline or invalid payload
cannot write. This is C snapshot persistence, not a Rust gameplay port.

Frozen source `f46c123` additionally tests wrong name, character ID and writer
epoch, plus a valid-digest payload with a different player name. Each rejects
without changing the pair or creating a save intent. The actual PostgreSQL
tests prove one winner for concurrent CAS saves, preserved bank bytes,
immutable intents, and historical retry after the head advances. Grants are
test-only and revoked afterward.

The full isolated local ARM64 runner at `f46c123` exited **0**, observed through
the process handle; evidence: `/tmp/muhan-player-save-identity.log`. This also
ran native bank commands, C onboarding, sanitizer/differential checks, and
both canonical/tree-inventory backup restore profiles. The latter fingerprints
do not yet prove complete recovery of the new player-save intent ledger.

Remaining cutover blockers: no native save provider is installed; the original
load baseline must survive savegame copies, disconnect and recovery queues.
A fresh route lookup must never label stale in-memory state with a newer DB
revision. General saves also need durable pending-command ownership and
coordination with money operations before DB authority is enabled. Credential
verification and live player initialization remain separate from DTO decoding.
No production grants, deployment, push or Actions execution were performed in
this goal continuation; the pending CI changes remain separate.

## Revalidated detached native DB player load — 2026-09-07

Source `db05b82` adds closed read_player_paired_snapshot and a PlayerStore-shaped
native load callback. The SQL re-resolves the fenced writer/name route within
the same transaction and requires exact expected character ID and paired revision
before returning payload/hash. C validates bounded binary columns and the selected
payload hash, then decodes the complete canonical player and requires exact name
agreement before publishing the owned clone. Failure returns no player and never
calls FileStore. This does not attach the clone to Ply or mutate live state.

The harness proves the RPC absent before migration and replays migration twice.
Actual C/PG tests reject an intentionally wrong revision with null output, then
load the expected player name/gold at initial and post-session-expiry successor
revisions. Test grants are revoked afterward. Full frozen ARM64 suite at
`db05b82` exited 0: `/tmp/muhan-player-native-load.log`, including actual bank
commands, C onboarding and both backup profiles.

Limits: canonical DTO intentionally excludes credentials and runtime/session
links. This callback is not installed in gameplay login and cannot substitute
for legacy password/account verification. Rich inventory load, negative payload
injection through this RPC, runtime initialization/admission integration, general
DB player saves and recovery ownership remain required before authority cutover.
No production grant, deployment or Actions execution.

## Native paired route selector — 2026-09-07

Source `bb81b08` provides player_paired_route_select with the PlayerAuthorityStore
selector signature. It borrows the caller's idle authenticated connection and
held world/writer/epoch, calls the closed paired-route RPC through the bounded
libpq exchange, and validates one binary row's field count/types/nullability,
UUID lengths, nonnegative revision and lowercase 64-byte hashes. Last result is
cleared on every attempt; only a complete valid result returns DB selection 1.
All failure returns -1, never legacy or a cached route.

The real PostgreSQL native executable pre-fills last with stale bytes and asserts
zero output state on failure. Tests compare exact ID/owner/revision/hashes before
gameplay and after session expiry/writer succession. A held paired-state DB lock
forces native timeout within the watchdog with no output. Full frozen ARM64
suite at `bb81b08` exited 0: `/tmp/muhan-player-native-route.log`, including real
bank commands, C onboarding and both restore profiles.

This callable selector is not globally installed. The actual DB player provider
must revalidate identity/revision during save/load and own connection/pending
lifecycle; the selector alone does not grant mutation authority. Malformed
PGresult injection and the new SQL lookup's lock-wait lease-expiry race remain
additional tests. No production RPC grant, deployment or Actions execution.

## Offline-capable paired-state identity lookup — 2026-09-07

Source `7bb5494` adds closed read-only resolve_player_paired_route(world,name,
writer,epoch). Unlike M3's legacy file head revision, it returns the actual
paired-state revision, character/owner UUID and both payload hashes. It requires
the real mud_writer_login/role, locks writer -> character -> pair, samples lease
time after locks, and requires active owned format-1 character plus exact stored
name and a paired state. Missing/ineligible lookup is an error, never a legacy
selection. This lookup deliberately does not require a live character session;
it is not permission to execute a money command or arbitrary player save.

The disposable harness proves the function absent before migration, applies it
twice, grants only inside the test and verifies revocation. Real login queries
verify initial UUID/revision/hash, missing name, wrong epoch and wrong login
rejection. After session expiry/writer succession, old writer lookup is rejected
and successor lookup returns revision 4 without reviving a session. Full frozen
ARM64 suite at `7bb5494` exited 0: `/tmp/muhan-player-paired-route.log`, including
bank, onboarding and both restore profiles.

Native C resolver/provider wiring, complete player mutation transactions and
runtime authority installation remain open. No production grant, deployment or
Actions run. Lock-wait expiry behavior of this new lookup has not yet received
its own observed-lock concurrency test; existing money lock tests are not a
substitute for that coverage.

## Socket-independent player-store dispatch boundary — 2026-09-07

Source `6934fb5` adds a caller-owned composition adapter for the existing
PlayerStore facade. It validates the bounded player-name lookup key, requires
save-key/player-name agreement and asks an explicit selector for legacy vs DB.
Only explicit legacy selection invokes the supplied legacy provider. DB error,
unknown mapping, missing selector/provider or reentrancy cannot fall back.
The adapter does not use fd, Ply or creature pointer identity; a copied save
view and an offline/recovery player can use the same authority lookup.

Unit coverage binds the real save_ply/load_ply facade to this adapter and uses
counted providers: live and copied fd=-1 saves reach DB, DB save/load failure
never invokes legacy, mismatched name is refused, explicit legacy still works,
and managed unbinding restores the previous facade. Test-first link failed on
the missing adapter. Full frozen ARM64 suite at `6934fb5` exited 0:
`/tmp/muhan-player-authority.log`, including existing real DB bank entry-point,
onboarding and backup/restore checks.

Scope: the new selector and DB provider are still callback contracts with mock
providers in this test, not a durable mapping implementation or DB player-save
transaction. Name is only a lookup key, not proof of character identity. Runtime
must supply held-world UUID/revision mapping and independently fenced DB save/
load, including disconnect/recovery, before installing this adapter. Existing
M3 adapter remains file-authoritative after PUBLISHED and is not relabeled DB
authority. No runtime policy, production feature flag or deployment changed.

## Equipped inventory normalization and save-path audit — 2026-09-07

Tracing command8.c savegame/savegame_nomsg exposed a missing normalization step:
legacy save temporarily adds ready-slot equipment to inventory before save_ply.
The initial live snapshot helper serialized only first_obj, so equipped objects
could disappear from the canonical comparison. The corrected fixture at f3982bd
reproduced an unequal canonical image. Source `8e05bc1` constructs a private
root-tag view and inserts equipped roots using legacy name/adjustment ordering,
clearing ready slots only on the view. It never updates live object parents or
list tags. Duplicate root pointers and over-200 roots fail closed instead of
silently following the legacy serializer's 200-root truncation.

The actual serializer/decoder/codec test compares equipped live state with its
expected unequipped inventory snapshot and asserts the live creature, equipped
object and original list link remain unchanged. Full frozen ARM64 suite at
`8e05bc1` exited 0: `/tmp/muhan-equipped-snapshot.log`, including real bank entry
points, qualified DB transactions, onboarding and both restore profiles. The
test covers a simple equipped root; richer ready-slot/nested-container and live
uninit stat changes remain acceptance work, not implicitly covered.

Save-path findings for the runtime adapter: command8 save functions pass a
different shallow creature copy to save_ply; io.c disconnect frees io/extra
before uninit_ply/save_ply; recovery queue retries later through save_ply. Thus a
bank policy keyed only by original creature pointer or a live Ply session cannot
govern all saves. A durable character authority/revision binding must survive
disconnect and copied save views before global DB player-store installation.
No current file save route was disabled or deployed by this change.

## Actual C deposit/withdraw functions with qualified PostgreSQL — 2026-09-07

Sources `0adaf72`/`535a43c` extend the native PostgreSQL executable to call the
real bank.c deposit/withdraw entry points, not the dispatcher directly. A bank
room fixture and actual cmd feed the native callback, qualified DB transaction,
result decoder, wallet application and command success formatting. Successful
message count must agree with CONFIRMED; printed amount must agree with the
wallet delta; failed commands leave the wallet unchanged. Legacy bank load/save,
player save and legacy amount-parser calls abort this selected-authority test.

The first integration attempt failed linking a duplicate free_obj test stub;
the real files1.c implementation remains linked and that duplicate was removed.
The isolated native bank compile explicitly enables MUHAN_BANK_MONEY_ROUTING.
No production compilation default was changed.

Full frozen ARM64 suite at `535a43c` exited 0:
`/tmp/muhan-bank-actual-command.log`. Actual entry-point tests include matching
withdrawal, all-deposit, state drift refusal, empty-all refusal and same-directory
withdrawal after verified release; recovery and both backup profiles also pass.
The separate real C onboarding socket scenario passes. These are not yet one
network-to-DB game process: room/descriptor setup and output capture are fixtures.
Runtime policy installation, network command acceptance and all save-path
ownership remain open. No Actions execution or deployment.

## Process-death-safe kernel money locks — 2026-09-07

Test-first `4baa214` reproduced a stale directory lock after SIGKILL: the next
process could not reacquire the same immutable reservation. Source `8f71f37`
uses Linux flock on a validated, owned, mode-0600, zero-length, single-link
stable file. A bounded nonblocking `/usr/bin/flock` child receives only the
explicit inherited descriptor; the parent FileHandle retains the shared open-file
description lock after the child exits. Closing the handle or process death
releases ownership. The stable inode is never unlinked or reclaimed by age/PID.
Existing legacy directory locks fail closed and require offline migration.

The independent-process test acquires a release lock, blocks another process,
SIGKILLs the holder, waits for actual exit, then successfully reacquires the same
reservation without deleting its bytes. Existing concurrency, old-release/new-
reservation, DB confirmation and same-directory sequential transaction gates
still pass. Full frozen ARM64 suite at `8f71f37` exited 0:
`/tmp/muhan-money-kernel-lock.log`, including C onboarding and both restore profiles.

Relay Dockerfile now explicitly installs util-linux and checks `/usr/bin/flock`.
The tested retained image has util-linux 2.38.1; the changed deployment Dockerfile
was not rebuilt, and no image was pushed/deployed. Full filesystem power-loss
fault injection and production runtime/source-adoption wiring remain incomplete.
No Actions run or broad resource cleanup was performed.

## Verified serialized release restores sequential money commands — 2026-09-07

Source `432f3f1` resolves the preceding real-PG regression without changing
directories or deleting test reservations manually. Claim and release share an
exclusive per-character directory lock. Release compares the exact reservation,
then calls the read-only DB reconciliation RPC and qualified current pair reader.
Both must confirm the expected next revision and exact resulting player/bank
bytes. UNKNOWN or changed current state cannot release. Original request history
is retained, and a separate exact-byte resolution record is synced before only
the matching active fence is unlinked and the directory synced.

Preparation skips only exact validated resolved history; an active fence remains
blocking. A resolved command cannot reacquire the reservation. A late old release
cannot remove a newer reservation because verification/unlink and claim are
serialized. Tests hold a release open while a competing process claims, reject
an unconfirmed verifier, preserve history, and reject stale release after a new
reservation. Real PG all-deposit -> verified release -> withdrawal in the SAME
directory now passes, as do exact retry and empty-all refusal.

Full frozen Linux ARM64 suite at `432f3f1` exited 0:
`/tmp/muhan-money-fence-release.log`, including C onboarding and both backup
restores. This supersedes the previous RED outcome. No production deployment or
Actions execution; only disposable fixture reservations were released, with
immutable requests/completion records retained until fixture cleanup.

Remaining lifecycle limits: process death while holding the directory lock leaves
a fail-closed stale lock; there is deliberately no unsafe time-based reclamation.
Crash/fdatasync fault injection, supervised stale-lock recovery and restart source
adoption remain unverified. The running C server does not yet invoke the release
service or install the policy. Qualified leases, memory adoption, pending recovery
and all other save-path ownership must be wired by the runtime before cutover.

## Reservation enforcement exposes missing release lifecycle — 2026-09-07

Source `304dbf6` first reproduced that the preparation CLI accepted another
command despite an existing character reservation. `2de3309` adds a bounded
legacy-request/conflict preflight followed by exclusive durable reservation,
then request preparation. No frame is echoed on conflict, invalid discovery,
or scan truncation. Old unfenced senders must be stopped before adopting this
protocol; preflight alone cannot serialize them.

Verification is INCOMPLETE/RED, not a passing full suite. The reservation CLI
regression passes, as do qualified normal transactions and lost-ack retry.
The real all-money sequence then fails at bank-transfer-rust-pg.mjs's second
new transaction (native status 4 instead of success): the first confirmed
transaction's reservation intentionally remains. Evidence:
`/tmp/muhan-money-fence-enforce.log`, terminal exit 1 at `2de3309`.
Do not evade this by changing directories, clearing reservations in tests, or
loosening the reservation check. Sequential normal transfers must work again
through an explicit verified release lifecycle before this is accepted.

Next requirement: serialize claim/release for the same character, verify exact
reservation identity plus authoritative terminal DB outcome/current-state
adoption, preserve the immutable historical request, then durably release only
that reservation. A read-then-unlink without serialization can delete a newer
reservation when two recovery workers race; it is not an acceptable release.
Unknown outcomes and partial release failures must stay fenced. No deployment
or Actions run was performed; runtime policy installation remains disabled.

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
