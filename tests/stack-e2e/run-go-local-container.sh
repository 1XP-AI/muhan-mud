#!/usr/bin/env bash
set -Eeuo pipefail
[[ "${STACK_E2E_LOCAL_DISPOSABLE:-}" == 1 ]] || exit 2
[[ "$(id -u)" == 10001 ]] || { echo 'local-stack: Go lane production-equivalent UID 10001 required' >&2; exit 2; }
[[ ! -S /var/run/docker.sock ]] || exit 2

artifact_dir=/tmp/go-terminal-stack-artifacts
mkdir -p "$artifact_dir"
export PLAYWRIGHT_BROWSERS_PATH=/opt/playwright
export MUHAN_BROWSER_DATABASE_URL='postgresql://postgres:stack-e2e-postgres-password@127.0.0.1:5432/stack_e2e?sslmode=disable'
export MUHAN_BROWSER_PORT="${MUHAN_BROWSER_PORT:-3125}"
export MUHAN_GO_PORT="${MUHAN_GO_PORT:-3181}"

ready=0
for attempt in $(seq 1 60); do
  if pg_isready -h 127.0.0.1 -p 5432 -U postgres -d stack_e2e >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
[[ "$ready" == 1 ]] || {
  echo 'local-stack: Go lane PostgreSQL readiness failed' >&2
  printf '%s\n' 'status=1' > "$artifact_dir/status"
  exit 1
}

set +e
pnpm exec playwright test \
  --config=tests/browser-e2e/playwright.go-process-postgres.config.ts \
  2>&1 | tee "$artifact_dir/playwright.log"
status=${PIPESTATUS[0]}
set -e

if [[ -d /repo/output/playwright ]]; then
  cp -R /repo/output/playwright "$artifact_dir/playwright"
fi
printf 'status=%s\nuid=%s\n' "$status" "$(id -u)" > "$artifact_dir/status"
exit "$status"
