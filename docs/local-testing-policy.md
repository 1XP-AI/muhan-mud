# Local-first validation

GitHub-hosted CI is manual-only to avoid spending Actions budget on every push.
The remote CI workflow was also disabled on 2026-09-07; do not add an automatic
trigger. When a manual run is explicitly approved, choose the smallest scope:

| scope | purpose | expensive checks |
| --- | --- | --- |
| `fast` (default) | Go feature-lane feedback | changed Go package race tests only |
| `integration` | main-branch/merge checkpoint after parallel lanes are integrated | full Go race, `go vet`, Linux ARM64 build, diff check once |
| `release` | approved release or compatibility review | legacy DB contracts, browser/stack, x64/Windows/macOS matrix |

The ARM64 build is therefore a merge checkpoint, not a per-agent or per-commit
check. PostgreSQL, browser, Helm, and compatibility checks remain release gates.
No billing/spending limit was changed.

Install the tracked hook in each clone:

```sh
bash scripts/install-local-hooks.sh
```

The pre-push hook checks the workflow policy, migration coverage, strict stack
TypeScript compilation and 19 SQL-transport/normalized-reader/lifecycle tests.
Failures block pushes. It rejects pushes of a revision other than checked-out
HEAD and dirty tracked files in the checked paths. It is a fast regression gate,
not the whole legacy C, Rust, database or browser suite. Git hooks are local and
can be bypassed; remote workflow configuration is the separate cost control.

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
