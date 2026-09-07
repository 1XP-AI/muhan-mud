# Self-hosted ARM64 CI migration — 2026-09-07

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

Local checks passed again:

- `tests/unit/self_hosted_ci_policy_test.py`
- `tests/unit/local_first_policy_test.py`
- `tests/unit/ci_container_cleanup_test.py`

Actual run: https://github.com/1XP-Inc/muhan-mud/actions/runs/34093192597

- Linux general and database jobs: queued, no test results yet.
- macOS: failed before tests. GitHub's annotation says the self-hosted runner
  lost communication with the server; this does not establish a source failure.
- Hosted x64 and Windows: did not start because of billing/spending limits.

The existing run has not been duplicated. CI is manual-only under the
local-first policy. The workflow is currently `disabled_manually`; re-enable
only after ensuring the default branch carries the intended manual-only
workflow so that the old workflow cannot accidentally consume hosted minutes.

## Administrator follow-up

1. Restore the macOS runner service and outbound GitHub connectivity; inspect
   its diagnostic logs and resource availability.
2. Check Linux runner online/busy state and exact labels. In the organization
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
