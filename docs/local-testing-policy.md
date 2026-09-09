# Local-first validation

GitHub-hosted CI is manual-only to avoid spending Actions budget on every push.
The remote CI workflow was also disabled on 2026-09-07; do not add an automatic
trigger. When a manual run is explicitly approved, choose the smallest scope:

| scope | purpose | expensive checks |
| --- | --- | --- |
| `fast` (default) | Go feature-lane feedback | affected Go package race tests only |
| `integration` | assembled-batch feedback before the main merge checkpoint | full Go race, `go vet`, diff check; no cross-architecture build |
| `main` | the one main-branch merge checkpoint | `integration` plus the Linux ARM64 cross-build once |
| `release` | approved release or compatibility review | legacy DB contracts, browser/stack, x64/Windows/macOS matrix |

The ARM64 build is therefore a `main` checkpoint, not a per-agent, per-commit, or
ordinary `integration` check. PostgreSQL, browser, Helm, and compatibility checks
remain release gates.
No billing/spending limit was changed.

The `release` scope is additionally guarded to the repository default branch. A
manual release request from a feature branch fails in a small self-hosted guard
before checkout, PostgreSQL startup, dependency installation, or compatibility
matrix fan-out. This keeps a release review available without letting an
accidental scope selection consume the expensive lane during development.

For a batch that changes durable game receipts, run the single opt-in PG batch
`go test -race ./internal/session -run TestPostgresBoundedLanesPersistAndReplay -count=1`
with `MUHAN_BOUNDED_LANES_TEST_DATABASE_URL` pointed at that batch's disposable
PostgreSQL. It is not part of every feature-lane run.

The fast script derives the smallest safe Go package set from the changed
`server/internal` path: world changes include session/transport consumers,
session changes include transport, and transport-only changes stay in transport.
Changes outside those runtime packages skip the Go lane; use
`GO_FAST_PACKAGES=all` for an explicit override. The pre-push hook applies the
same principle to stack checks: migration coverage runs only for migration or
stack-runner changes, and gateway/TypeScript tests run only for gateway, web,
stack-contract, or package-lock changes. A multi-commit push uses its remote tip
as one diff base so the same contract is not rerun once per commit.

`fast` reads the current worktree by default. It does not infer `HEAD^..HEAD`
when the tree is clean, so running it twice after a commit does not repeat the
same lane accidentally. To audit a committed revision explicitly, set
`GO_FAST_COMMIT=1` for `HEAD^..HEAD` or set `GO_FAST_BASE=<commit>` for a wider
range.

The old `merge` validation name is deliberately rejected by the script. This
prevents an ambiguous command from silently consuming the main-merge ARM64
budget; choose `integration` during development or `main` exactly at the
assembled main-branch checkpoint.

Install the tracked hook in each clone:

```sh
bash scripts/install-local-hooks.sh
```

The pre-push hook checks the workflow policy and only the migration/stack
contracts affected by the pushed paths. Failures block pushes. It rejects
pushes of a revision other than checked-out HEAD and dirty tracked or untracked
files in the checked paths. It is a fast regression gate, not the whole legacy C,
Rust, database or browser suite. Git hooks are local and can be bypassed; remote
workflow configuration is the separate cost control.

Full isolated C/Gateway/browser/Postgres acceptance is run locally:

```sh
bash scripts/run-stack-e2e-local-docker.sh --allow-disposable
# Or require it as part of the next push:
MUHAN_LOCAL_FULL_STACK=1 git push private HEAD
```

Docker storage must first be made available; full acceptance has not passed.
Do not substitute an Actions run when a local test is blocked. Report the
blocker instead. The existing full-stack suite stubs browser authentication;
it does not verify the real Supabase Auth provider. Never report fast checks as
full acceptance. Do not prune unrelated images or volumes to make room.
