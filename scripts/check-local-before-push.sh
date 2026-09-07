#!/usr/bin/env bash
set -Eeuo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
# Do not validate different worktree code from the commit being pushed.
# Known user-owned generated files are excluded and are never read here.
scope=(.githooks .github scripts tests services web supabase package.json pnpm-lock.yaml pnpm-workspace.yaml ':!web/next-env.d.ts')
git diff --quiet HEAD -- "${scope[@]}" || {
  echo 'local-first: commit or separate changes in the tested paths before pushing.' >&2
  exit 1
}
python3 tests/unit/local_first_policy_test.py
python3 tests/unit/stack_e2e_migration_coverage_test.py
pnpm --dir services/gateway exec tsc --noEmit --strict --skipLibCheck --target ES2022 --module ESNext --moduleResolution Bundler --esModuleInterop --jsx react-jsx ../../tests/stack-e2e/stack-e2e.test.ts
pnpm --dir services/gateway exec tsx --test ../../tests/stack-e2e/sql-transport.test.ts ../../tests/stack-e2e/normalized-snapshot-check.test.ts ../../tests/stack-e2e/web-stack-ui.lifecycle.test.ts
echo 'Local fast checks passed. This is NOT full C/DB/browser acceptance.'
if [[ "${MUHAN_LOCAL_FULL_STACK:-}" == 1 ]]; then
  bash scripts/run-stack-e2e-local-docker.sh --allow-disposable
else
  echo 'Full isolated acceptance: bash scripts/run-stack-e2e-local-docker.sh --allow-disposable'
fi
