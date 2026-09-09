#!/usr/bin/env bash
set -Eeuo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
# Do not validate different worktree code from the commit being pushed.
# Known user-owned generated files are excluded and are never read here.
scope=(.githooks .github scripts tests server services web supabase package.json pnpm-lock.yaml pnpm-workspace.yaml ':!web/next-env.d.ts')
scoped_status="$(git status --porcelain --untracked-files=all -- "${scope[@]}")"
[[ -z "$scoped_status" ]] || {
  echo 'local-first: commit or separate changes in the tested paths before pushing.' >&2
  exit 1
}
python3 tests/unit/local_first_policy_test.py

diff_base="${MUHAN_DIFF_BASE:-}"
if [[ -n "$diff_base" ]] && git rev-parse --verify "$diff_base^{commit}" >/dev/null 2>&1; then
  changed_files="$(git diff --name-only "$diff_base"...HEAD)"
elif git rev-parse --verify origin/main^{commit} >/dev/null 2>&1; then
  changed_files="$(git diff --name-only "$(git merge-base HEAD origin/main)"...HEAD)"
elif git rev-parse --verify origin/master^{commit} >/dev/null 2>&1; then
  changed_files="$(git diff --name-only "$(git merge-base HEAD origin/master)"...HEAD)"
elif git rev-parse --verify HEAD^ >/dev/null 2>&1; then
  changed_files="$(git diff --name-only HEAD^ HEAD)"
else
  changed_files="$(git diff-tree --no-commit-id --name-only -r HEAD)"
fi

run_stack_contracts=0
run_migration_contract=0
while IFS= read -r changed; do
  [[ -n "$changed" ]] || continue
  case "$changed" in
    supabase/*|scripts/run-stack-e2e.sh|tests/unit/stack_e2e_migration_coverage_test.py)
      run_migration_contract=1
      ;;
  esac
  case "$changed" in
    services/gateway/*|tests/stack-e2e/*|web/*|package.json|pnpm-lock.yaml|pnpm-workspace.yaml|supabase/*)
      run_stack_contracts=1
      ;;
  esac
done <<<"$changed_files"

if [[ "$run_migration_contract" == 1 ]]; then
  python3 tests/unit/stack_e2e_migration_coverage_test.py
else
  echo 'Skipped migration-order contract (no migration/stack runner change).'
fi

if [[ "$run_stack_contracts" == 1 ]]; then
  pnpm --dir services/gateway exec tsc --noEmit --strict --skipLibCheck --target ES2022 --module ESNext --moduleResolution Bundler --esModuleInterop --jsx react-jsx ../../tests/stack-e2e/stack-e2e.test.ts
  pnpm --dir services/gateway exec tsx --test ../../tests/stack-e2e/sql-transport.test.ts ../../tests/stack-e2e/normalized-snapshot-check.test.ts ../../tests/stack-e2e/web-stack-ui.lifecycle.test.ts
else
  echo 'Skipped gateway/stack TypeScript checks (no gateway, web, or stack contract change).'
fi

echo 'Local fast checks passed. This is NOT full C/DB/browser acceptance.'
if [[ "${MUHAN_LOCAL_FULL_STACK:-}" == 1 ]]; then
  bash scripts/run-stack-e2e-local-docker.sh --allow-disposable
else
  echo 'Full isolated acceptance: bash scripts/run-stack-e2e-local-docker.sh --allow-disposable'
fi
