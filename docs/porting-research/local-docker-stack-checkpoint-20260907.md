# Local Docker full-stack acceptance — 2026-09-07

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
