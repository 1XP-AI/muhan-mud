# Local Docker full-stack acceptance — 2026-09-07

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
