#!/usr/bin/env bash
# Explicit opt-in local G4 lane. It owns one loopback-only disposable
# PostgreSQL container and exercises a real pg_dump/pg_restore of the Go
# world schema before checking receipt replay and writer fencing.
set -Eeuo pipefail
[[ "${1:-}" == "--allow-disposable" ]] || {
  echo 'usage: bash scripts/run-go-backup-restore-local.sh --allow-disposable' >&2
  exit 2
}

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
run_id="muhan-go-backup-$(date +%Y%m%d%H%M%S)-$$-$RANDOM"
container="${run_id}-postgres"
password="muhan-go-backup-password"
source_database="muhan_go_backup_source_$$_$RANDOM"
restored_database="muhan_go_backup_restored_$$_$RANDOM"
world_id="go-backup-${run_id}"
command_one="go-backup-command-one-${run_id}"
command_two="go-backup-command-two-${run_id}"
claim_id="go-backup-claim-${run_id}"
created=0
dump_file=""

command -v docker >/dev/null 2>&1 || { echo 'docker is required' >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo 'go is required' >&2; exit 1; }
docker image inspect postgres:17-alpine >/dev/null 2>&1 || {
  echo 'postgres:17-alpine must already be available; refusing an implicit image pull' >&2
  exit 1
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  if [[ -n "$dump_file" ]]; then
    rm -f -- "$dump_file"
  fi
  if [[ "$created" == 1 ]]; then
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
  echo "go-backup-restore-local: owned PostgreSQL container removed; status=$status"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker create \
  --platform linux/arm64 \
  --name "$container" \
  --label muhan.go-backup-disposable=true \
  --tmpfs /var/lib/postgresql/data:rw,size=256m \
  --publish 127.0.0.1::5432 \
  --env "POSTGRES_PASSWORD=${password}" \
  --env POSTGRES_DB=postgres \
  postgres:17-alpine >/dev/null
created=1
docker start "$container" >/dev/null

ready=0
for _ in $(seq 1 60); do
  if docker exec -e PGPASSWORD="$password" "$container" \
      psql -X -U postgres -d postgres -v ON_ERROR_STOP=1 -c 'SELECT 1' >/dev/null 2>&1; then
    # initdb accepts one connection burst then restarts; require a second
    # query so CREATE DATABASE does not hit a vanished unix socket.
    sleep 1
    if docker exec -e PGPASSWORD="$password" "$container" \
        psql -X -U postgres -d postgres -v ON_ERROR_STOP=1 -c 'SELECT 1' >/dev/null 2>&1; then
      ready=1
      break
    fi
  fi
  [[ "$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null || true)" == true ]] || break
  sleep 1
done
[[ "$ready" == 1 ]] || {
  echo 'go-backup-restore-local: PostgreSQL readiness failed' >&2
  exit 1
}

host_port="$(docker port "$container" 5432/tcp | sed -n '1s/.*://p')"
[[ "$host_port" =~ ^[0-9]+$ ]] || {
  echo 'go-backup-restore-local: could not resolve published PostgreSQL port' >&2
  exit 1
}

docker exec -e PGPASSWORD="$password" "$container" \
  psql -X -U postgres -d postgres -v ON_ERROR_STOP=1 \
  -c "CREATE DATABASE $source_database TEMPLATE template0" >/dev/null
docker exec -e PGPASSWORD="$password" "$container" \
  psql -X -U postgres -d postgres -v ON_ERROR_STOP=1 \
  -c "CREATE DATABASE $restored_database TEMPLATE template0" >/dev/null

source_dsn="postgresql://postgres:${password}@127.0.0.1:${host_port}/${source_database}?sslmode=disable"
restored_dsn="postgresql://postgres:${password}@127.0.0.1:${host_port}/${restored_database}?sslmode=disable"

(
  cd "$root/server"
  MUHAN_BACKUP_PHYSICAL_RESTORE_ROLE=seed \
  MUHAN_BACKUP_PHYSICAL_RESTORE_ALLOW_DISPOSABLE=1 \
  MUHAN_BACKUP_PHYSICAL_RESTORE_DATABASE_URL="$source_dsn" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_WORLD_ID="$world_id" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_COMMAND_ONE="$command_one" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_COMMAND_TWO="$command_two" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_CLAIM_ID="$claim_id" \
    go test -race ./internal/storage -run '^TestPostgresWorldBackupPhysicalRestore$' -count=1 -v
)

(
  cd "$root/server"
  MUHAN_BACKUP_TEST_DATABASE_URL="$source_dsn" \
    go test -race ./internal/storage -run '^TestPostgresWorldBackupRestoreFencesReceipts$' -count=1 -v
)

dump_file="$(mktemp "${TMPDIR:-/tmp}/muhan-go-backup.XXXXXX")"
docker exec -e PGPASSWORD="$password" "$container" \
  pg_dump -h 127.0.0.1 -U postgres -d "$source_database" --format=custom > "$dump_file"
[[ -s "$dump_file" ]] || {
  echo 'go-backup-restore-local: pg_dump produced an empty archive' >&2
  exit 1
}
docker exec -i -e PGPASSWORD="$password" "$container" \
  pg_restore -h 127.0.0.1 -U postgres -d "$restored_database" --exit-on-error < "$dump_file"

(
  cd "$root/server"
  MUHAN_BACKUP_PHYSICAL_RESTORE_ROLE=verify \
  MUHAN_BACKUP_PHYSICAL_RESTORE_ALLOW_DISPOSABLE=1 \
  MUHAN_BACKUP_PHYSICAL_RESTORE_DATABASE_URL="$restored_dsn" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_WORLD_ID="$world_id" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_COMMAND_ONE="$command_one" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_COMMAND_TWO="$command_two" \
  MUHAN_BACKUP_PHYSICAL_RESTORE_CLAIM_ID="$claim_id" \
    go test -race ./internal/storage -run '^TestPostgresWorldBackupPhysicalRestore$' -count=1 -v
)

echo "GREEN Go world pg_dump/pg_restore preserved receipts and revision fencing ($world_id)"
