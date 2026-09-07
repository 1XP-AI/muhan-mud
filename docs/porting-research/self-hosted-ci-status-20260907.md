# Self-hosted ARM64 CI migration — 2026-09-07

## Renewed request: current verification

The latest follow-up also checks noninteractive sudo before either Linux
dependency installation, so an unprepared runner fails with an actionable
message instead of waiting for a password. Routing/isolation, manual-only,
and owned-container cleanup tests passed again. An actual dispatch was
attempted again and returned HTTP 422; **no new CI run exists**. The default
branch was read directly and still has `on: push`, `ubuntu-latest`, and
`5432:5432`. Enabling that workflow before landing the migration would restore
the old automatic hosted execution. Default-branch merge/enablement therefore
needs the user's direction; no remote success is claimed.

Latest recheck: GitHub still reports `disabled_manually`. An actual dispatch
request against `codex/self-hosted-arm64-ci` returned HTTP 422 (disabled
workflow); no new run was created. The latest run is still `34093192597`,
completed with failure. Runner-group inspection again returned HTTP 403,
which is an API permission limitation, not proof that repository access is
denied. No workflow enablement or default-branch merge was performed.

Both Linux dependency setup steps now specify apt lock timeout and download
retries on update, and fail on incomplete index updates (`--error-on=any`).
This does not remove shared lock files or guarantee that every apt lock
contention can be retried successfully. The three local CI routing,
manual-only and owned-container cleanup policy tests pass, as does
`git diff --check`. These checks are not a successful remote suite run.

Rechecked the only workflow and its complete matrix. ARM64 label routing is
already present on the working branch; there are no reusable workflows.
Added per-job temporary/cache paths for npm, pnpm and Playwright, included
the run attempt in pnpm installation paths, and removed a redundant system
package installation. Existing isolated Cargo outputs, dynamically assigned
PostgreSQL ports and owned-resource-only Docker cleanup remain in place.
The routing/isolation, local-first and Docker collision policy tests all pass.

Docker Hub's tag API confirms Linux ARM64 manifests for both CI stack images:
`postgres:17-alpine` (`dfc2780980fe…`) and
`postgrest/postgrest:v12.2.8` (`cff60f8c98d2…`). The native amd64 GCC
digest is used only by the retained x64 compatibility lane. Runtime setup
actions select the runner architecture; no x64-only binary download was
found in the workflow. This manifest check is not a full runtime test.

A fresh dispatch attempt against the existing remote migration branch was
rejected with HTTP 422: the workflow is disabled. No new run was created;
the local cache-isolation changes therefore have no remote execution result.
The last actual run remains `34093192597`, completed with failure.
The default branch still uses `on: push`, hosted Ubuntu and port 5432.
Organization runner-group inspection still returns HTTP 403. Do not infer
missing runner access solely from that API permission failure.

Before enabling CI, merge the reviewed manual-only workflow onto the default
branch; enabling it first would restore the old automatic paid workflow.
An organization administrator must allow this repository in the runner group
and, if configured, allow this workflow/ref. Linux needs apt plus noninteractive
sudo and Docker; macOS needs Xcode Command Line Tools. Then enable and dispatch
the workflow and verify every job's result. No default-branch merge, workflow
enablement, deployment, or global resource cleanup was performed here.

## Delivery

Migration branch: `codex/self-hosted-arm64-ci`, pushed to
`1XP-Inc/muhan-mud` through commit `9c606d5`.
Its changes are also present on `codex/mud-identity-foundation`.
No main-branch merge or deployment has been performed.

The repository has one workflow, `.github/workflows/ci.yml`; no reusable
workflow needs separate migration. The general matrix lane and database job
use `[self-hosted, Linux, ARM64]`. macOS portability uses
`[self-hosted, macOS, ARM64]`. Native x64 ABI / pinned amd64 named-volume
coverage and Windows compatibility remain on hosted runners intentionally.

Linux explicitly installs the compiler, libpq/client, Python, and supporting
tools, plus Node 22, pnpm 10.15.1 and Rust 1.90.0. This currently requires an
apt-based runner with noninteractive sudo. Docker is a runner prerequisite.
macOS requires preinstalled Xcode Command Line Tools; the workflow checks them
and installs Python via its setup action. PostgreSQL services use assigned
ports; build outputs and browser ports are isolated. Disposable DB harnesses
remove only container IDs successfully created by that harness, never global
containers, caches or volumes.

Local ARM64 Docker execution has built the C/Rust stack and installed Chromium
with PostgreSQL 17. This is compatibility evidence, not a passing remote CI
result or a passing complete game acceptance suite.

## Verification

### Latest terminal result (supersedes the in-progress observations below)

Run `34093192597` has completed with failure. Rechecked through the GitHub
run, job-log, annotation, workflow-state and default-branch APIs:

- Linux ARM64 general: checkout, explicit dependency installation, Node,
  pnpm, Rust and the C build passed. The native lifecycle sanitizer then
  reported `stack-use-after-return` in
  `test_native_read_rehearsal_is_exact_default_off_and_diagnostic_only`.
  Its referenced stack object is `rehearsal` in `runtime_native_file_load`.
  This is an actionable test/code failure, not a runner-access failure;
  sanitizers must not be disabled to obtain a green run.
- Linux database and macOS: terminal failure; both annotations report that
  the self-hosted runner lost communication with GitHub. Neither suite passed.
- Hosted x64/Windows: remain blocked by billing/spending limits.
- The three local CI policy tests still pass.
- A new dispatch on `codex/self-hosted-arm64-ci` was attempted and rejected
  with HTTP 422 because the workflow is disabled. No new run was created.
- Default branch `main` still contains `on: push`, hosted Ubuntu routing and
  fixed PostgreSQL host port 5432. Re-enabling the repository workflow now
  would also reactivate that older automatic workflow, so it was left disabled
  to respect the local-first/budget constraint. Merge the reviewed migration
  into the default branch before re-enabling and dispatching it.
- Organization runner-group inspection still returns HTTP 403: an org admin
  or a token with runner/runner-group permission must verify repository and
  workflow access. Existing Linux execution already proves access for that run.

Remaining acceptance: fix the sanitizer failure, restore runner connectivity,
land the manual-only migration on the default branch, then enable and rerun.
No successful remote CI completion is claimed.

### Local sanitizer follow-up

The native lifecycle failure was reproduced in the retained local Linux ARM64
test image with `detect_stack_use_after_return=1` (exit 2, same ASan stack).
The test double retained the address of `runtime_native_file_load`'s local
`rehearsal` descriptor and dereferenced it after the call returned. It now
copies that descriptor during the call; the existing writer/artifact identity
assertions remain unchanged. Production code was not modified.

The native lifecycle Make target now explicitly enables this ASan check so
different compiler defaults cannot hide the regression. Fresh local results:

- Native lifecycle test and ASan/UBSan: exit 0, including the default Make target.
- Production-object/no-live-link guards: both passed.
- Read-rehearsal unit and ASan/UBSan targets: exit 0 with the same strict option.

These runs used a read-only source mount, no network, no host ports, tmpfs
outputs and only self-removing task containers. No Actions run or deployment
was started. The full remote suite and broader database migration acceptance
remain unverified; the sanitizer item above is locally fixed, not remotely green.

### Earlier in-progress observations

Local checks passed again:

- `tests/unit/self_hosted_ci_policy_test.py`
- `tests/unit/local_first_policy_test.py`
- `tests/unit/ci_container_cleanup_test.py`

Actual run: https://github.com/1XP-Inc/muhan-mud/actions/runs/34093192597

- Linux general job: queued, no test results yet.
- Linux database job: assigned to the organization default runner group with
  `[self-hosted, Linux, ARM64]`. Container initialization, checkout, and ARM64
  verification passed; dependency installation is in progress. This proves
  runner access for this job, not a passing database test suite.
- macOS: failed before tests. GitHub's annotation says the self-hosted runner
  lost communication with the server; this does not establish a source failure.
- Hosted x64 and Windows: did not start because of billing/spending limits.

The existing run has not been duplicated. CI is manual-only under the
local-first policy. The workflow is currently `disabled_manually`; re-enable
only after ensuring the default branch carries the intended manual-only
workflow so that the old workflow cannot accidentally consume hosted minutes.

## Administrator follow-up

Rechecked on 2026-09-07 for the renewed migration request: all three local
policy tests above pass. The database job has now started on Linux ARM64,
while the general Linux job remains queued; macOS has the runner-communication failure annotation, and the hosted
x64 job has the billing/spending-limit annotation. The workflow remains
disabled manually. No duplicate run was dispatched and no runner-group
permissions were changed. The organization runner-group API explicitly
returned HTTP 403 (organization administrator or runner-group permission
required); repository runner discovery returned zero accessible runners.
These observations do not establish that the tests pass on the new runners.

1. Restore the macOS runner service and outbound GitHub connectivity; inspect
   its diagnostic logs and resource availability.
2. Check Linux runner capacity and the in-progress dependency installation.
   This run demonstrates that at least one Linux job has organization runner
   access; do not treat the other queued job as proof of an access denial.
   In the organization
   runner group's repository access settings, allow `1XP-Inc/muhan-mud`;
   if workflow access is restricted, allow this repository's CI workflow/ref.
3. The repository runner-list API returned an empty list, which alone does not
   prove an organization access denial. An earlier run assigned organization
   runners; the current queue needs an administrator's live runner-group view.
4. Resolve billing or provide separately approved Windows/x64 capacity for
   the intentionally retained compatibility lanes.
5. Once infrastructure is healthy, finish or cancel the existing queued run
   deliberately, then re-enable and dispatch the migration workflow as needed.
   A successful self-hosted CI result remains unverified.
