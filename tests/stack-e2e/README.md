# Stack E2E harness

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
that normalized persistence is enabled in the game scenario.
