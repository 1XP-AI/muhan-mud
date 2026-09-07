# Local Docker full-stack acceptance — 2026-09-07

## Latest: JSON claims bootstrap fixed; both browser game paths pass

`7be3d7d` diagnostics showed provisioning `finalized|finalized|active` but
no selected character. The disposable `auth.uid()` only read the legacy
singular claim GUC, not PostgREST JSON claims. New SQL regression failed
before the fix and passed afterward on isolated PostgreSQL 17, including
owner-only RLS visibility, missing subject denial, and legacy compatibility.
`4addf71` updates only the disposable bootstrap and runs this regression in
both stack entrypoints. Production authentication/policies are unchanged.

Fresh frozen full run: `/tmp/muhan-local-stack.TLzSgJ/result.json`, console
`/tmp/muhan-json-claims-stack.log`. Both rendered browser flows returned from
`runWebStackAcceptance`: provision and claim each verified automatic entry,
roster reselection, and real C health-command output. Auth is still the
deterministic test boundary, not a real Supabase Auth acceptance claim.

The next failure is stack-e2e.test.ts:1093: original claimed-file SHA versus
file SHA **after** browser admission/reselection/game/disconnect. Do not remove
the assertion without proving what changed. Inspect exact changed fields and
the ordinary save/receipt boundary; preserve the separate pre-game immutable
claim proof. Full suite remains 1 PASS / 1 FAIL; no push, CI or deployment.
The isolated SQL container and full-stack containers were cleaned up by ID.

## Latest: real browser provisioning questions reached; post-password handoff next

`bb1779c` allocates the actual web port before constructing the claim Gateway
and includes only that exact browser origin alongside the existing Node-test
origin. It replaces the ReadonlySet instead of calling `.add` (first draft
`fdef6ea` had a type error, corrected before the successful test run).
Frozen bb1779c passes onboarding readiness and input, then fails looking for
the password field: `/tmp/muhan-local-stack.Q7CmBl/result.json`.

Source comparison with the passing real-C socket scenario revealed that the
browser fixture skipped name confirmation (`예`) and the subsequent Enter
prompt. `7eb72e6` adds both and waits for each real C question before advancing.
Fresh frozen run reaches actual C new-password prompt and submits through the
rendered password field, but fails the next selected-character assertion for
Webhero in web-stack-ui.ts404/caller467. Evidence:
`/tmp/muhan-local-stack.nsjGbT/result.json`, console
`/tmp/muhan-browser-provision.log`. This does not yet prove browser provisioning
committed; next inspect DB intent/provision/handoff/head and UI completion
controls after password submission. Do not weaken automatic-admission gate.

Stack TypeScript and 14 lifecycle tests passed. An initial run was stopped by
Docker ENOSPC before tests; removed eleven explicitly identified unused
task-owned images, no global cache/volume pruning. Subsequent runs cleaned all
owned containers. Full stack remains 1 PASS / 1 FAIL. No push/CI/deploy.

## Latest: real browser login/empty roster pass; onboarding socket next

Browser diagnostics established `auth-visible=true`, no roster responses,
`Failed to fetch`, and no auth-fixture interception. The web CSP permits
same-origin HTTP only, but the fixture configured a different PostgREST port.
`1f839f7` uses the browser's own origin for its Supabase URL and routes
`/rest/v1/**` via Playwright's HTTP client to actual disposable PostgREST,
preserving request headers/token and using its real response. This models
the ingress prefix without weakening production CSP or mocking roster data.
Deterministic Auth responses remain the pre-existing test boundary, not a
real Supabase Auth acceptance claim.

Fresh frozen run prints `auth-fixture method=POST`, passes sign-in, empty
roster and opens new-character wizard. It now fails the first enabled-input
assertion at web-stack-ui.ts365 / caller446 (onboarding input disabled).
Evidence `/tmp/muhan-local-stack.WCNKGp/result.json`; retained console output
`/tmp/muhan-browser-same-origin.log`. Next inspect onboarding WebSocket status:
source has fixed `origin=http://localhost:3000` in claim Gateway allowlist,
while the actual web server uses random `http://127.0.0.1:<port>`.
Use exact fixture origin, never wildcard or disabled origin validation.

Stack TypeScript compilation and all 14 web lifecycle tests pass. Direct
standalone web-stack-ui.ts tsc lacks Node ambient types; checking the existing
stack entrypoint resolves them and passes. One run failed before tests with
Docker ENOSPC. Removed five explicitly identified unused task-owned diagnostic
images only; no other caches/volumes/containers were pruned. Latest test cleanup
completed. Full stack remains 1 PASS / 1 FAIL. No CI dispatch/push/deploy.

## Latest: claim preservation and gameplay pass; browser empty-roster gate next

`4736e35` wires explicit CLAIM-only `player_store_save_existing` through the
same authorized V4 resolver/route/save/receipt pipeline. The serializer copies
the existing bound-head bytes instead of serializing the scrubbed creature.
Copied scratch is volatile-wiped after success/failure; pre-copy rejection
does not modify caller buffer. Provision retains its original serializer.

TDD RED was observed in both store and activation-composition tests. GREEN:
PlayerStore normal + ASan/UBSan, activation-composition normal + ASan/UBSan,
live-observer normal + ASan/UBSan, and package static gates. Test outputs:
`/tmp/muhan-claim-store-verification.log`, `/tmp/muhan-store-preserve-final.log`.
Stack TypeScript compilation passed. Astra implemented the store/test portion;
root reviewed and integrated the CLAIM caller and composition/stack gates.

Frozen actual local Docker `4736e35` passes original claim file SHA unchanged,
DB head SHA equal to that original, and increasing authoritative revision.
It then passes normal `/ws` admission and a health game command, printing
`claim-rpc-green`, `claim-mud1-ready`, `claim-game-green`.
This fixes the observed persisted-credential overwrite without bypassing saves.

Next failure is now in real Chromium UI acceptance:
`web-stack-ui.ts:322`, `signInToEmptyRoster`: expected text
`이 계정에 연결된 캐릭터가 없습니다` absent after login (5s timeout).
Caller is `runWebStackAcceptance` line405, before web provisioning/claim flow.
Evidence `/tmp/muhan-local-stack.atdcwO/result.json`. Inspect page state,
deterministic Auth fixture, roster request response and current UI text before
changing any assertion. Do not infer the browser flow passed from socket tests.
Full stack still 1 PASS / 1 FAIL; all owned containers cleaned. No cloud build,
CI dispatch, push, or deployment. Goal remains active.

## Latest: exact original-byte copy primitive implemented, not wired yet

`4a2444f` adds `character_save_journal_v2_copy_existing_at` to the existing
descriptor-rooted journal module. It borrows the root descriptor, reuses
trusted-tree traversal, rejects non-regular/unsafe-mode/multiply-linked leaves,
checks bounded size and EOF, validates the named inode, and hashes the exact
buffer returned against the wire's existing-head precondition. Failed copies
wipe bytes already copied and return length zero. Successful bytes remain
caller-owned and must be wiped by the future PlayerStore integration.

TDD: a reject-all stub failed the exact-byte preservation test before the
implementation. Fresh normal and ASan/UBSan journal suites now pass, including
exact bytes, capacity rejection, hash mismatch cleanup, hardlink/symlink/mode/
missing-file rejection, renamed held root, borrowed descriptor lifetime and
injected close failure cleanup. Production link/static check also passed.
Output: `/tmp/muhan-existing-copy-results.log`.
Concurrent truncation/mutation fault injection has not been added to this new
reader yet; runtime has short-read, size and exact-copied-hash rejection.

Next: wire explicit CLAIM-only preserve-existing PlayerStore dispatch inside
the authorized V4 serializer callback, build its precondition from the bound
route, and clear transient raw bytes on all exits. Add helper/store integration
tests proving provision unchanged and claim capability/receipt still consumed.
Do not call the new reader before V4 candidate/route validation. Then rerun
the frozen Docker original-file gate and browser acceptance. This commit
alone does NOT fix claim overwrite; no Docker run, CI dispatch, push or deploy
was performed in this step. Goal remains active.

## Latest: actual claim activation erases the persisted credential field

Frozen `03a6d0b` proves the field-level cause on Linux ARM64:
`size-equal=true password-equal=false password-before-nonzero=true password-after-zero=true`.
Evidence: `/tmp/muhan-local-stack.TJbt5e/result.json`.
The layout probe compiles against the running native C ABI, reports offsets
only, and the comparison emits booleans only. Neither record bytes nor password
values are printed. Stack TypeScript compilation passed. The initial probe
attempt at `831412a` could not write its executable under root-owned `/repo`;
`03a6d0b` corrected that to a unique writable temporary directory. No permission
relaxation was made. Both Docker runs cleaned their owned containers.

Root cause now observed, not merely suspected: the claim handler zeroizes its
loaded creature password, then activation_runtime_helper dispatches that same
creature through player_store_save and player_record_serialize_bounded, which
copies the creature bytes. This overwrites the original credential on disk.
Do not remove zeroization, restore credentials into the session object, skip
activation receipts, or weaken the original exact-file assertion.

Next implementation, independently reviewed by Astra:

1. Add a bounded existing-file copy primitive in character_save_journal_v2.c,
   reusing trusted descriptor traversal and leaf checks. Its hash must cover
   the exact copied bytes and match the existing bound head. Test unchanged
   bytes, bounds/hash/missing-file rejection and held-root behavior first.
2. Add explicit preserve-existing PlayerStore save dispatch, selected only
   for captured CLAIM mode. Run the same V4 resolver, writer renewal, route
   checks, stage, publish, snapshot observer and receipt path. Read within the
   authorized serializer callback; wipe temporary raw bytes after completion
   and errors. Provisioning continues normal creature serialization.
3. Prove exact original-file preservation plus activation receipt/snapshot
   hashes in the real stack, then continue normal game and browser acceptance.

No implementation of the preservation path yet. Full stack remains 1 PASS /
1 FAIL at the unchanged-file gate (line1016); browser is unreached. No CI
dispatch, push, or deployment. Goal remains active.

## Latest: claim completion EOF race fixed; legacy file preservation fails next

`c62c957` serializes onboarding TCP EOF behind the existing control queue.
C sends ACTIVE then disconnects, while Gateway awaits snapshot binding before
acknowledging the browser. Previously EOF failed the WebSocket during that
RPC. The `binding-rpc-ok` diagnostic proves ACTIVE was already parsed; earlier
localization to before C activation was incorrect. Missing-head hypothesis
was also disproved (`heads=1`).

Astra added a deterministic deferred-binding regression: RED closed with 1011
before the fix, GREEN now returns claimed/1000. Negative cases preserve errors
for rejected binding and missing ACTIVE. Fresh full Gateway suite: 144 pass,
0 fail, 4 conditional skips (148 total). Package source typecheck passed.
C activation lifecycle tests (runtime and legacy) passed for numeric-only
diagnostics committed in `77c1bea`; no control payloads/credentials are logged.

Frozen `77c1bea` reproduced the EOF failure, evidence
`/tmp/muhan-local-stack.9RkHko/result.json`. Frozen `c62c957` now prints
`claim-rpc-green`, validates finalized/active ownership, then fails the
unchanged legacy player file SHA-256 assertion at stack-e2e line1001.
Evidence: `/tmp/muhan-local-stack.PINlby/result.json`.
The C activation saver serializes a claim player whose in-memory credentials
were erased; this is a source-backed suspect, not yet a field-level proof of
which bytes changed. Preserve zeroization and the unchanged-file assertion;
next add a byte/field-preservation regression and repair the snapshot source,
not the assertion or activation gate. Browser acceptance is still unreached.

Full stack remains 1 PASS / 1 FAIL. Test containers were cleaned up by their
own runner. No remote CI dispatch, push, or deployment in this goal turn.

## Latest: confirmed expiry rejection releases intent; positive claim next

`272bbb5` distinguishes only canonical bounded claim RPC HTTP400 PostgreSQL
P0001/22023 rejection via OnboardingClaimRejectedError. Only a confirmed FIRST
request rejection restores cancellation. Network/abort/5xx/malformed replies
do not; an uncertain first request followed by a rejected exact retry also
remains non-cancellable. This retains the indeterminate ownership boundary.

Frozen full-stack now passes expired-claim cancellation and next normal claim
begin, but fails waiting for the positive claim's `claimed` event at line970.
Evidence `/tmp/muhan-local-stack.u8gLQS/result.json`; root must next inspect
claim RPC, C CLAIMED/activation acknowledgement and DB lifecycle separately.
Do not weaken the completion assertion. Browser phase remains unexecuted.

Fresh Gateway suite: 141 pass, 0 fail, 4 conditional skips. Added tests cover
initial confirmed rejection versus uncertainty followed by rejection. Fixed
undefined test-only characterId shorthand that previously made delayed
challenge throw; delayed callback test now waits for server cancellation,
then drains Gateway operations rather than assuming client close means server
closure. Direct strict compilation of the entire test file exposes older
RawData/Buffer.from overload errors; package source typecheck passes, so do
not claim full test-file typecheck success.

Remote CI run34093192597: macOS failed; check annotation says runner lost
communication. Linux remains queued; hosted exceptions were billing-blocked.
No duplicate run dispatched. No production rollout or push this turn.

## Latest: pre-completion claim cancellation fixed; expired finalize next

`89e50b4` keeps unfinished claim cancellation eligible through challenge and
password input, then preserves indeterminate work before VERIFIED claim RPC
or EVIDENCE completion control. Existing cancel SQL only changes the intent;
no migration or ledger deletion was needed. New transactional SQL contract
passed against actual isolated PostgreSQL 17/full current schema: same-actor
retries, full-row allowance preservation, old challenge/claim denial,
idempotent cancellation, target/actor rate caps and reserved/finalized guards.
Owned tmpfs DB container was removed after rollback; no other containers touched.

Gateway suite: 137 passed, 0 failed, 4 conditional skips after test-only
evidence-finalizer injection. Gateway and stack TypeScript checks passed.
Concurrent two-DB-session cancel-vs-claim ordering remains to be tested.

Frozen `89e50b4` full stack passes missing-member cancellation and wrong-password
cancellation (including retained allowance) and proceeds to expired claim.
It fails at stack-e2e line 963: expired claim remains started, expected cancelled.
Evidence `/tmp/muhan-local-stack.a8R76A/result.json`. Expiry is injected in DB
after challenge returns, so C verifies password then claim RPC rejects; Gateway
has already disabled cancellation before sending that RPC. Next distinguish
known deterministic DB rejection from indeterminate ownership completion and
cover both with TDD; do not cancel an uncertain committed claim blindly.
Full acceptance still 1 PASS / 1 FAIL; browser phase not reached. No push/deploy.

## Latest diagnosis: rejected claim permanently skips cancellation

Frozen `fe3edfc` reproduces `missing-member-intent-after-close=started`, then
the next claim returns an error frame and closed socket instead of readiness.
Evidence: `/tmp/muhan-local-stack.qFxEZQ/result.json`. Gateway clears
`unreservedIntentMayExist` before awaiting challenge (gateway.ts:932), but
the membership-gate SQL rejects before writing a claim attempt. Therefore
finish never schedules cancellation; this is not an asynchronous cleanup lag.
Latest begin rejects a new correlation while that started intent is unexpired.

Next implementation must preserve durable claim evidence: guarded cancellation
shares the existing intent row lock with challenge. A rejected/no-ledger
challenge should release its intent; an indeterminate committed challenge
must not erase its ledger or allow an invalid ownership transition. A separate
explicit abort policy is needed for allowed-but-unclaimed attempts (wrong
password/disconnect), retaining attempt history and rate limits. Do not hide
the retry bug by using different actors for each negative E2E case.

Luna added exact receipt-scalar, three-generation, multiple staged-tail, and
callback-time live/published-marker mutation regression tests. Root reran
normal recovery and ASan/UBSan targets successfully. Astra and Luna finished;
no worker remains assigned work. No deployment or remote CI dispatch this turn.

## Latest full-stack: restart and recovered gameplay pass; claim readiness next

Frozen `e8926aa` actually passed the previously failing C restart. The next
assertion failed at recovered lifecycle: evidence-only CLI reconciliation
correctly returned handoff_pending, while the test expected active. The
existing saved-handoff opt-in was not exposed by CLI. `934ef08` adds strict
`ONBOARDING_RECONCILER_RECOVER_SAVED_HANDOFFS=true|false` (default false),
with CLI RED/GREEN tests and explicit true in the full-stack recovery lane.
All 32 reconciler tests and the strict stack TypeScript check passed.

Frozen `934ef08` then passed recovery-rpc-green, recovery-ws-ready and
recovery-game-green: actual fresh C/Gateway admission plus Korean health
command after finalize outage recovery. It proceeded through the first claim
negative case (missing batch membership) and failed on the next wrong-password
case's onboarding-ready wait, before a password was sent. Current evidence:
`/tmp/muhan-local-stack.WYff58/result.json`; failing caller line 940, helper
line 392. Investigate successive claim intent/session lifecycle and returned
Gateway outcome; do not weaken readiness or relabel DB lifecycle to bypass it.
Full run remains 1 PASS / 1 FAIL, actual browser phase not yet reached.

Docker was confirmed at zero free space. Removed only four old, unused,
rebuildable task image tags ending 1788758197-59781, 1788758473-68867,
1788758637-72181 and 1788758850-77565; no broad cache/image/volume prune.
This recovered 3.2GB. Both runs used the local default Docker driver, and
task containers/networks were cleaned on exit.

## Latest: historical recovery regression green locally; full-stack pending

Implemented a separate historical receipt replay path. Ordinary publish/ACK
still requires its own live post-hash. Recovery selects the latest existing
published successor, validates the complete canonical contiguous chain and
historical ACK, and replays the original DB receipt without publishing old
bytes. Evidence and the held writer generation are checked again after ACK.
Malformed candidates cannot fall back to an older anchor. Historical calls
count as ACK attempts only, not successful publications.

Fresh local verification: recovery, ACK, and publish static/unit targets and
all three AddressSanitizer/UndefinedBehaviorSanitizer targets passed. The
previous two-save replay assertion was observed failing before integration.
Nine added scenarios cover published-but-unacknowledged anchor, staged tail,
deferred/invalid/rejected receipt, writer reopen, corrupt live file, malformed
published marker, and malformed historical ACK. Ordinary historical ACK stays
strict; replay preserves the live inode/bytes when no new save is pending.

This does not yet prove the original full Docker restart scenario. Next add
callback-time evidence mutation and longer-chain coverage, review the helper,
then rebuild the committed source and rerun isolated full-stack acceptance
after scoped Docker artifact space recovery. No deployment or push this turn.
The Astra worker completed and is idle. CI run 34093192597 remains live:
macOS in progress, Linux queued, hosted Windows/x64 billing-blocked; no duplicate
CI run was dispatched.

## Prior restart investigation: failing local regression reproduced

Frozen `77cf875` narrows restart failure to owner startup 7 / recovery 8
(RECOVERY / INCOMPLETE), not DB transport or writer bootstrap. Evidence:
`/tmp/muhan-local-stack.VpRxMC/result.json`.

Added an intentionally RED assertion, currently uncommitted, to
`test_same_character_revision_order` in the recovery unit test: after replaying
and acknowledging a two-generation same-character chain, replay that complete
history again. `make -C src character-save-journal-v2-recovery-test` fails only
this new assertion. The old test checked the first pass but never replayed
history after the successor had replaced the current file. Next inspect the
publish/ACK historical evidence rules and implement a verified replay path;
do not skip history merely because an `.acked` filename exists.

Frozen `35a5609` adds numeric publish/ACK diagnostics but did not reach restart:
fixture copy failed. A read-only local Docker `df` confirms overlay 32G / 30G,
5.1M available, 100%. Runner-specific images accumulated (all are rebuildable);
no broad prune or unrelated volume/image deletion was performed. Stop heavy
Docker reruns until scoped test-artifact cleanup, and continue the lightweight
host unit regression. This is progress, not a goal-level impasse.

## Latest: provision-to-gameplay path verified; restart remains

Frozen `0aa8ae7` now passes real C character creation, first-save evidence,
completion/activation, a new normal `/ws` session, the Korean health command,
duplicate-session rejection and lease release on close. Two stale test
assumptions were corrected without weakening data checks:

- Capture the actual player hash before finalize and require it to equal both
  the SAVED control request and saved/committed onboarding receipt. Activation
  performs another explicit save, so independently require the current file to
  match the DB head with revision >= 2. Both generations passed (`f5d0332`).
- The Gateway intentionally closes onboarding after ACTIVE/binding. Reconnect
  through normal `/ws` for gameplay, matching the web client; do not send game
  commands on the completed onboarding socket (`0aa8ae7`).

Full acceptance remains 1 PASS / 1 FAIL. It has advanced to restarting C for
the injected-finalize-outage scenario: the second process exits 78 with
`M3 runtime startup failed` at stack-e2e.test.ts:787. Investigate persisted
writer lease/epoch/recovery state across graceful restart; do not delete its
journal or bypass startup checks to make the fixture pass. Evidence:
`/tmp/muhan-local-stack.8F1oTb/result.json`. The first process's graceful stop
completed. No runtime containers remain after exact-name cleanup.

20 local fast tests, strict TypeScript, 45-migration coverage and manual-only
workflow policy pass. Real Chromium, remaining claim/normalized scenarios and
production acceptance remain unverified. No cloud build, Actions or rollout.

## Latest correction: completion and activation now succeed

Read-only disposable RPC diagnostics reproduced SQLSTATE 42702 for
`character_id` in completion (`d03d945`) and then `correlation_id` in activation
(`93d4cd6`). Both names collided with PL/pgSQL TABLE output variables.
Forward migration `20261015000000_provisioning_completion_head_qualification.sql`
replaces the current function bodies with qualified column lookups only;
ownership, lifecycle, evidence and exact-retry checks remain unchanged.

Frozen `18a38d9` passes both previously failing boundaries: a `provisioned`
response arrives and DB state is `finalized|finalized|active`. Full acceptance
still FAILS at stack-e2e.test.ts's subsequent hash assertion: the onboarding
request's saved-file hash differs from the now-current player file hash. Next
trace the activation save/snapshot timing and compare evidence from the correct
save generation; do not overwrite immutable onboarding evidence or remove hash
validation. Real browser and later claim/normalized acceptance remain unproven.

Evidence: `/tmp/muhan-local-stack.Tt2rgW/result.json`. Local fast checks pass:
20 tests, strict TypeScript, all 45 game migrations and manual-only CI policy.
All executions used the local default Docker driver. No cloud build, Actions
run, remote push or production rollout occurred. Disposable containers and
network were cleaned; frozen evidence and local image/cache were retained.

## Latest rerun: storage resolved, first save acknowledged

Frozen source `24cd12d` ran on local Docker Desktop with the pinned default
builder. No Actions job, cloud build, production deployment or remote push.
Local fast checks passed: strict TypeScript, 44-migration coverage, manual-only
workflow policy, and 20 tests. Full acceptance remains 1 PASS / 1 FAIL.

The new cleanup-time diagnostics rule out an absent/unacknowledged first-save
head: the provisioning character has head state `existing`, revision 1,
storage format 1 and a writer epoch; the save journal contains `.prepared`,
`.published` and `.acked`. The earlier revision-zero baseline-conflict
hypothesis is therefore not supported. Finalize/reconcile still fails, leaving
intent `provisioning`, request `reserved`, character `provisioning` and no
handoff. Next capture the disposable RPC error at finalize/reconcile and trace
the installed function, without weakening lifecycle or evidence gates. This
does not establish complete browser acceptance or real Supabase Auth.

User explicitly authorized isolated local Docker full-stack testing.

## Implemented

- `scripts/run-stack-e2e-local-docker.sh --allow-disposable` freezes committed HEAD via Git archive, builds C/Rust/Node/Chromium inside Docker, creates a uniquely named internal network and disposable PostgreSQL 17/PostgREST containers.
- The runner shares only the disposable PostgreSQL network namespace. No published ports, host source mounts, Docker socket, production credentials, cluster operations or external DB configuration.
- Existing CI-only runner remains unchanged. SQL test transport now supports explicitly opted-in loopback psql execution without Docker-in-Docker.
- Browser authentication remains a deterministic stub; this lane does not prove real Supabase Auth or production Next build behavior.
- Exact created container/network names are cleaned up. Frozen source/evidence and image/build cache are retained; there is no global prune.

## Verification and current blocker

- SQL transport unit tests: 2 passed.
- Full stack TypeScript strict typecheck: passed.
- CI migration coverage: all 44 game migrations, passed.
- Shell syntax: passed; empty cleanup array regression tested on macOS Bash.
- First frozen build source: `f291caed1f844bac2b19816d5463a64187165240`.
- Image build failed during apt repository verification. Subsequent read-only disk diagnosis confirmed Docker overlay at 100%, 32 GB total / 31 GB used / 0 available. Do not disable package signature verification.
- Host filesystem also has only ~14 GiB available. Docker reports ~22 GB build cache, ~9.7 GB images, ~2 GB volumes. These include unrelated user resources; none were pruned.
- No `muhan-local-stack` containers remain. The full C/Gateway/browser/normalized DB acceptance has NOT run or passed.
- Follow-up fixes: macOS Bash empty-array cleanup and PostgREST schema reload/readiness after migration. These require a new frozen build once storage is available.

Next: obtain authority for a specifically agreed storage cleanup or have the user provide Docker disk space, then rerun the committed local runner and fix real acceptance failures. This is not completion of the broader DB authority/porting goal.

## Resumed after user-provided Docker space

The storage blocker is resolved. All runs used the local `default` Docker
driver, not Docker Build Cloud or GitHub Actions. The host launcher now pins
`buildx --builder default --load` (d19a073). No remote workflow was triggered.

- Actual Linux arm64 C and Rust builds, Chromium installation, PostgreSQL 17
  startup, all 44 migrations, and PostgREST readiness succeeded.
- New psql transport incorrectly set PGSERVICE to an empty string; PostgreSQL
  interpreted that as a service name. Added regression test and removed the
  environment key instead (5bf8c66). Three transport tests and strict typecheck
  passed. Dependency/browser build cache now precedes source COPY.
- The legacy Gateway/RPC fixture lacked importer membership and expected the
  obsolete immediate-active lifecycle. It now uses the real importBatch path
  with its explicitly synthetic tuple and asserts handoff_pending plus pending
  handoff, without bypassing C activation (a4e7000).
- Latest full-stack frozen source: a4e7000be86d9651526cffa6e9933d89a2a0cd3b.
  Result: Gateway/RPC contract PASS; complete stack scenario FAIL. Evidence:
  `/tmp/muhan-local-stack.8lcNUp/result.json`.
- Full scenario currently stops while creating the disposable legacy player:
  welcome `[엔터]` is observed, newline is sent, but no name prompt arrives
  within 15 seconds. The test now consumes each observed prompt and handles
  the welcome step. Do not add blind repeated input or weaken assertions;
  investigate the real C login state/fixture next.
- All runner-owned containers/networks were cleaned by the exact-name trap.
  Local images/caches and frozen-source evidence remain for follow-up.

This proves neither complete browser gameplay nor real Supabase Auth. Do not
mark the porting goal complete. No production deployment or DB changes occurred.

## Continued: ARM welcome fixed; first shadow save still fails

Previous turn classified as progress. This continuation also made verified
progress; there is no external blocker and the overall goal remains active.

Root cause of the welcome hang: `iobuf.fnparam` was plain char. On Linux ARM64
its default unsigned interpretation promoted the login welcome sentinel -1
to 255. A temporary diagnostic image logged both dispatch/login `param=255`,
while the process remained alive. Changed this runtime-only one-byte field to
`signed char` (49ab1b8), without changing player persistence layout. The new
source-bound test failed with the old declaration under `-funsigned-char` and
passed all five values under both char defaults after the change. Linux Docker
also passed this test and the seven-case ASan/UBSan info alignment regression.

Actual full-stack execution now creates three legacy C player files and passes
real importBatch membership/locator/exact-retry checks. The separate real
Gateway/Postgres admission evidence contract continues to pass.

Local fixture/runtime corrections discovered by executing the full lane:

- Run as production-equivalent UID 10001, not root; the writer explicitly
  requires UID 10001 ownership. Chromium is installed in /opt/playwright and
  the Next working directory is writable by this user (d6aebbe).
- Prepare private character-save-journal and character-save-stage directories;
  the writer/publisher open these existing components rather than creating them.
- Set copied player root to 0700 before first save, not only after save.
- Include actual name confirmation and enter stages in both WebSocket provision
  scenarios (ed88d92). No server-side gates or activation checks were removed.
- Expose redacted server diagnostics when startup/save assertions fail.

Latest executed source: db65bac77a7b69799b5ab6d751967175d17e921a.
Image: muhan-local-stack-1788761102-18156:local.
Evidence: /tmp/muhan-local-stack.XzZMMR/result.json.
Result remains 1 PASS / 1 FAIL: the full scenario reaches the new-password
prompt, but its receipt remains pending after password submission. C stderr
is empty. Startup now succeeds. Directory corrections are necessary native
preconditions, but have NOT proven the remaining save failure fixed.

Next: instrument the disposable native PlayerStore save/handoff outcome and
inspect the test WebSocket close/error frames plus writer journal state before
teardown. Do not guess another directory change or weaken pending-to-saved
assertions. Also audit other plain-char runtime sentinels (e.g. commands=-1)
as a separate portability concern; fnparam alone is not a complete ARM audit.
The real browser phase has still not been reached, and no production rollout,
remote push, Actions job, or cloud build was performed.

## First shadow save now crosses the saved-receipt gate

Verified progress in this continuation:

- Added redacted test failure evidence (control frames and journal/stage names)
  and fixed-label native save diagnostics. Runtime reported
  `step=absent-head-bootstrap cutpoint=0`; the target shard did not exist.
- The file-only PlayerStore calls player_path_ensure_dir before saving. The
  M3 adapter instead reached descriptor-based absent observation without
  preparing the first character's shard. Existing tests always supplied it.
- Added a missing-shard regression, demonstrated RED, then implemented
  character_save_journal_v2_prepare_absent_shard_at (b94ad9d). It validates the
  canonical absent wire and existing private root/player/journal/stage tree,
  creates only the route's shard through the held root descriptor, validates
  ownership/mode/no-follow, and fsyncs shard and parent. Existing entries are
  never chmod'd or replaced. No player bytes or DB head are created by this
  preparation; the original absent-file and exact-rebind gates still run.
- Added existing-shard symlink and permissive-mode rejection cases. Bootstrap
  tests and ASan/UBSan tests passed. Existing v2 journal, v2 sanitizer,
  PlayerStore and protocol test targets also passed with exit 0.
- Actual C full-stack run crossed `state=(saved|committed)` after submitting
  the provision password, without fixture pre-creation of the target shard.
  The earlier pending-receipt defect is fixed in the exercised scenario.

Latest run source: 78fa283a65ad0349b0cbd8ea3bc5dc6d0a72d1e7.
Evidence: /tmp/muhan-local-stack.FM5ETN/result.json.
The standalone Gateway/Postgres contract passes. Full stack still fails at
the next `browser.json('provisioned')` gate. C stderr is empty and the Gateway
returns its generic `onboarding failed` control after saving.

Next: determine which post-save boundary rejects (finalize/reconcile, snapshot
command binding, or C/DB handoff activation). Inspect actual DB lifecycle and
the exact authorizer failure phase in the disposable run. Do not treat a
saved file as active gameplay, weaken handoff requirements, or mark the full
browser/normalized acceptance complete. No deployment or external CI occurred.
