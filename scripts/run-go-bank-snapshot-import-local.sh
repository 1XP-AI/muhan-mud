#!/usr/bin/env bash
# Explicit opt-in local bank migration lane. It owns one loopback-only
# disposable PostgreSQL container and removes only that container on exit.
set -Eeuo pipefail
[[ "${1:-}" == "--allow-disposable" ]] || {
  echo 'usage: bash scripts/run-go-bank-snapshot-import-local.sh --allow-disposable' >&2
  exit 2
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
run_id="muhan-bank-snapshot-import-$(date +%Y%m%d%H%M%S)-$$-$RANDOM"
container="${run_id}-postgres"
password='muhan-bank-snapshot-import-password'
database='muhan_bank_snapshot_import'
created=0

command -v docker >/dev/null 2>&1 || { echo 'docker is required' >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo 'go is required' >&2; exit 1; }
docker image inspect postgres:17-alpine >/dev/null 2>&1 || {
  echo 'postgres:17-alpine must already be available; refusing an implicit image pull' >&2
  exit 1
}

cleanup() {
  exit_code=$?
  trap - EXIT INT TERM
  set +e
  if [[ "$created" == 1 ]]; then
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
  echo "bank-snapshot-import-local: owned PostgreSQL container removed; exit=$exit_code"
  exit "$exit_code"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker create \
  --platform linux/arm64 \
  --name "$container" \
  --tmpfs /var/lib/postgresql/data:rw,size=256m \
  --publish 127.0.0.1::5432 \
  --env "POSTGRES_PASSWORD=$password" \
  --env "POSTGRES_DB=$database" \
  postgres:17-alpine >/dev/null
created=1
docker start "$container" >/dev/null

ready=0
for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -U postgres -d "$database" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
[[ "$ready" == 1 ]] || {
  echo 'bank-snapshot-import-local: PostgreSQL readiness failed' >&2
  exit 1
}

host_port="$(docker port "$container" 5432/tcp | sed -n '1s/.*://p')"
[[ "$host_port" =~ ^[0-9]+$ ]] || {
  echo 'bank-snapshot-import-local: could not resolve published PostgreSQL port' >&2
  exit 1
}

cd "$root/server"
MUHAN_BANK_SNAPSHOT_IMPORT_TEST_DATABASE_URL="postgresql://postgres:${password}@127.0.0.1:${host_port}/${database}?sslmode=disable" \
  go test -race ./internal/storage -run '^TestPostgres.*BankSnapshot' -count=1 -v
