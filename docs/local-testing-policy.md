# Local-first validation

GitHub-hosted CI is manual-only to avoid spending Actions budget on every push.
The remote CI workflow was also disabled on 2026-09-07; do not re-enable or run it
without explicit user approval. No billing/spending limit was changed.

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
