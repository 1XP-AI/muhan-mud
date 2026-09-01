# Stack E2E harness

`../../scripts/run-stack-e2e.sh` creates a uniquely named, internal-only
Docker network, disposable PostgreSQL 17 container, and PostgREST container.
It applies `bootstrap_contract.sql`, migrations 020/030/040/050/060/070, then runs the
real C binary and Gateway against that PostgREST instance. No host volume,
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

Run from the repository root:

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
