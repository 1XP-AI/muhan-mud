#!/usr/bin/env bash
# Explicit opt-in local lane. It owns one loopback-only disposable PostgreSQL
# container and never inspects, stops, or removes any other container.
set -Eeuo pipefail
[[ "${1:-}" == "--allow-disposable" ]] || {
  echo 'usage: bash scripts/run-go-process-postgres-browser-e2e-local.sh --allow-disposable' >&2
  exit 2
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
run_id="muhan-go-browser-e2e-$(date +%Y%m%d%H%M%S)-$$-$RANDOM"
container="${run_id}-postgres"
password="muhan-browser-e2e-password"
database="muhan_browser_e2e"
created=0

command -v docker >/dev/null 2>&1 || { echo 'docker is required' >&2; exit 1; }
command -v node >/dev/null 2>&1 || { echo 'node is required' >&2; exit 1; }

find_free_port() {
  node -e 'const net=require("net"); const s=net.createServer(); s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port)); s.close();});'
}

browser_port="${MUHAN_BROWSER_PORT:-$(find_free_port)}"
go_port="${MUHAN_GO_PORT:-$(find_free_port)}"
if [[ "$browser_port" == "$go_port" ]]; then
  go_port="$(find_free_port)"
fi

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  if [[ "$created" == 1 ]]; then
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
  echo "go-browser-e2e-local: owned PostgreSQL container removed; status=$status"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker image inspect postgres:17-alpine >/dev/null 2>&1 || {
  echo 'postgres:17-alpine must already be available; refusing an implicit image pull' >&2
  exit 1
}

docker create \
  --platform linux/arm64 \
  --name "$container" \
  --tmpfs /var/lib/postgresql/data:rw,size=256m \
  --publish 127.0.0.1::5432 \
  --env "POSTGRES_PASSWORD=${password}" \
  --env "POSTGRES_DB=${database}" \
  postgres:17-alpine >/dev/null
created=1
docker start "$container" >/dev/null

ready=0
for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -U postgres -d "$database" >/dev/null 2>&1; then
    ready=1
    break
  fi
  [[ "$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null || true)" == true ]] || break
  sleep 1
done
[[ "$ready" == 1 ]] || {
  echo 'go-browser-e2e-local: PostgreSQL readiness failed' >&2
  exit 1
}

host_port="$(docker port "$container" 5432/tcp | sed -n '1s/.*://p')"
[[ "$host_port" =~ ^[0-9]+$ ]] || {
  echo 'go-browser-e2e-local: could not resolve published PostgreSQL port' >&2
  exit 1
}

cd "$root"
MUHAN_BROWSER_DATABASE_URL="postgresql://postgres:${password}@127.0.0.1:${host_port}/${database}?sslmode=disable" \
MUHAN_BROWSER_PORT="$browser_port" \
MUHAN_GO_PORT="$go_port" \
  pnpm exec playwright test \
  --config=tests/browser-e2e/playwright.go-process-postgres.config.ts
