#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${1:-}" != "--allow-disposable" ]]; then
	cat >&2 <<'EOF'
Usage:
  scripts/run-go-social-import-local.sh --allow-disposable

The flag is required because this check creates and removes one isolated,
temporary PostgreSQL container owned by this script.
EOF
	exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd -P)"
server_dir="$root/server"
run_id="muhan-social-import-${RANDOM}-$$"
container="${run_id}-postgres"
password="muhan-social-import-password"
database="muhan_social_import"

cleanup() {
	docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

arch="$(docker image inspect --format '{{.Architecture}}' postgres:17-alpine)"
if [[ "$arch" != "arm64" ]]; then
	echo "postgres:17-alpine must be ARM64; found $arch" >&2
	exit 1
fi

docker create \
	--name "$container" \
	--label "muhan.social-import-disposable=$run_id" \
	-p 127.0.0.1::5432 \
	--tmpfs /var/lib/postgresql/data:rw,size=384m \
	-e POSTGRES_PASSWORD="$password" \
	-e POSTGRES_DB="$database" \
	postgres:17-alpine >/dev/null
docker start "$container" >/dev/null

ready=0
for attempt in $(seq 1 60); do
	if docker exec "$container" pg_isready -U postgres -d "$database" >/dev/null 2>&1; then
		ready=1
		break
	fi
	if [[ "$attempt" -eq 60 ]]; then
		docker logs --tail 80 "$container" >&2 || true
		exit 1
	fi
	sleep 1
done
if [[ "$ready" -ne 1 ]]; then
	echo "temporary PostgreSQL did not become ready" >&2
	exit 1
fi

port="$(docker port "$container" 5432/tcp | sed -n '1s/.*://p')"
if [[ -z "$port" ]]; then
	echo "temporary PostgreSQL has no loopback port" >&2
	exit 1
fi
dsn="postgresql://postgres:${password}@127.0.0.1:${port}/${database}?sslmode=disable"

(
	cd "$server_dir"
	MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL="$dsn" go test -race ./cmd/muhan -run '^TestSocialManifestApplyAgainstPostgres$' -count=1
	MUHAN_SOCIAL_IMPORT_TEST_DATABASE_URL="$dsn" go test -race ./internal/storage -run '^TestPostgres(SocialAggregatesAndReplays|SocialImportRollsBackSnapshotAndEvidence|SocialEvidenceReadAndRestore|SocialRestoreHonorsWriterFence)$' -count=1
)

echo "Go social import/restore checks passed on ARM64 postgres:17-alpine"
