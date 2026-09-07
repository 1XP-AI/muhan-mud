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
