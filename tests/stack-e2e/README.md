# Stack E2E harness

For the explicitly authorized **local Docker** lane, use
`STACK_E2E_REAL_AUTH=1 bash scripts/run-stack-e2e-local-docker.sh --allow-disposable`
from the repository root with the local Docker endpoint configured. It freezes
HEAD and uses the local default builder, never the cloud release wrapper.
GoTrue v2.189.0 performs real browser login; Gateway uses its production token
verifier and PostgREST enforces real ownership. All users/data are disposable.
Absent/zero REAL_AUTH retains the deterministic Auth boundary for isolated
protocol regression tests. Values other than zero/one are rejected.
The local lane is separate from the CI-only script below; it publishes no
host ports, mounts no host files/socket, and cleans only its created resources.

## Go terminal-only local lane

The same disposable wrapper has a bounded Go lane for the shipped central xterm
page and the existing real Go+PostgreSQL browser assertions:

```bash
STACK_E2E_LOCAL_LANE=go bash scripts/run-stack-e2e-local-docker.sh --allow-disposable
```

This command archives the committed `HEAD` before building
`tests/stack-e2e/Dockerfile.go-local`; working-tree files are never copied into
the image. The image and runner compile `server/cmd/muhan` and
`server/cmd/muhan-browser-e2e` as UID/GID `10001`, then the runner starts the
real Go WebSocket process and Next page in one disposable container. PostgreSQL
is a fresh tmpfs-backed `postgres:17-alpine` container, and the runner shares
only its private network namespace, so the lane publishes no host ports, uses
no host mounts or Docker socket, and does not start a relay, fake WebSocket, or
Auth service.

The lane executes
`tests/browser-e2e/go-process-postgres.spec.ts` through its existing
`playwright.go-process-postgres.config.ts`, including signup/game-password
login, existing-character admission and duplicate-session denial,
reconnect/persistence, movement, Korean composition, and mobile viewport
coverage. After cleanup the wrapper prints a retained scratch directory whose
`go-terminal-stack-artifacts` subdirectory contains the redacted Playwright log,
JSON report, and failure traces when produced. This lane is opt-in; omitting
`STACK_E2E_LOCAL_LANE` preserves the default legacy C/Node lane and its tests.

`../../scripts/run-stack-e2e.sh` creates a uniquely named, internal-only
Docker network, disposable PostgreSQL 17 container, and PostgREST container.
It applies `bootstrap_contract.sql` and the identity, handoff, snapshot-eligibility,
fulfillment, command-binding, importer, and normalized projection migrations through
2026-10-14, then runs the
real C binary and Gateway against that PostgREST instance. Before the broader
onboarding scenario, a separately gated PostgreSQL 17 contract sends the shared
admission identity fixture through the real Gateway finalizer HTTP transport and
its shard-aware RPC. No host volume,
named volume, existing container, or checked-in data is used; cleanup is
installed before the first Docker object is created.

The test injects only the Gateway authenticator (a deterministic identity).
`SupabaseOnboardingAuthorizer` and `SupabaseCharacterAuthorizer` use their
normal service-role PostgREST RPCs. The scenario covers flag-off/no-C-connect,
cancel-and-retry, the full wizard/save SHA/receipt/game-command path, duplicate
lease, lease release, exact C `SIGTERM` → exit 0 handling, and a wrong-hash
finalize/reconcile rejection. The successful claim exercises C `CHALLENGE`,
Gateway `ALLOW`, C password verification, and exact `VERIFIED`/final-claim
binding. A subsequent challenge denial against the active target verifies
that no password prompt or ownership mutation occurs. The successful and failed paths assert that the
DB name key and player filename match C `lowercize(name, 1)` (`Stackhero`),
while the failed path retains the saved file and `state=saved` receipt but
leaves DB state `provisioning|reserved|provisioning` with no `COMMIT`.

The last lane starts the real Next application and Chromium against the same
disposable Gateway, C MUD, and PostgREST resources. It stubs only the absent
Supabase Auth endpoint with signed deterministic fixtures, then proves the
actual UI can provision and claim, refresh each active roster, and enter the
unchanged normal MUD socket. Chromium must already be installed (CI installs
it immediately before this runner). The acceptance explicitly compiles the
M3 runtime, starts its M3 lane in `shadow`/`handoff` mode, and enables the M4
artifact relay's fulfillment flag only inside this disposable run; ordinary
runtime configuration remains default-off.

This is a CI-only disposable gate, not a local development command: without
`CI=true` the runner exits before creating Docker resources. Its CI job needs
Docker with space for `postgres:17-alpine`, Node, pnpm, make, curl, `gcc`, a
working `pg_config`/libpq toolchain, installed workspace dependencies, and a
Playwright Chromium binary. The CI job invokes it from the repository root:

```sh
./scripts/run-stack-e2e.sh
```

To retain the redacted evidence JSON after cleanup, set
`STACK_E2E_OUTPUT=/tmp/stack-e2e-result.json`.

The runner prints RED/GREEN preflight evidence and only emits stage-level
failures. Access tokens, service-role JWTs, admission HMACs, and game
passwords are redacted from the JSON evidence assembled by the test. Docker
containers and the network are uniquely named, tracked as created, and removed
only by this invocation's trap; the fixture is also inside the disposable run
directory. A Docker VM with enough free space for `postgres:17-alpine` is
required; if it cannot initialize, the runner prints read-only Docker space
diagnostics, reports `BLOCKED`, and never runs prune or deletes existing user
resources.

The local, non-mutating `python3 tests/unit/stack_e2e_migration_coverage_test.py`
checks that the explicit application list contains every game migration exactly
once in chronological order. Only the Realtime lobby migration is excluded.
This guards schema coverage; it does not prove that a stack run has passed or
that the end-to-end comparison has passed.

The runner also builds the locked Rust normalized projector into its disposable
directory and provisions a separate normalized-reader login in its temporary DB.
The manifest-first pass persists the real C-produced snapshots; the fulfillment
pass remains separate. Provision and claim assertions bind the actual outbox
bytes to their receipt, compare them through the production reader and Rust
projector, require MATCH, and require normalized exact retry for both browser
flows. `normalized-snapshot-check.test.ts` separately verifies reader cleanup
on match, mismatch, and exceptions without connecting to a DB. These assertions
are CI acceptance requirements, not a claim that the current CI run passed.
