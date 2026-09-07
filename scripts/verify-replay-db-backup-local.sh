#!/usr/bin/env bash
# Only accepts the disposable PG container created by the replay runner.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable ]] || exit 2
container_id="${2:-}"
profile="${3:-canonical}"
[[ "$profile" == canonical || "$profile" == tree_inventory ]] || exit 2
source_database="replay_backup_$profile"
restored_database="replay_restore_$profile"
[[ "$container_id" =~ ^[0-9a-f]{64}$ ]] || exit 2
[[ "$(docker inspect --format '{{index .Config.Labels "muhan.replay-disposable"}}' "$container_id")" == true ]] || exit 2
sql() {
  docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
    psql -X -h 127.0.0.1 -U postgres -d "$1" -v ON_ERROR_STOP=1 -At -c "$2"
}
fingerprint() {
  sql "$1" "select count(*)::text || ':' || coalesce(md5(string_agg(to_jsonb(t)::text, '' order by to_jsonb(t)::text)), '') from private.$2 t"
}
tables=(game_character_player_snapshot_v1_artifacts game_character_shadow_receipts game_character_player_snapshot_v1_level_projections game_character_m4_file_snapshot_manifests game_character_legacy_heads game_character_writer_epochs)
root="$(cd "$(dirname "$0")/.." && pwd -P)"
sql postgres "create database $source_database template template0" >/dev/null
# Copy schema only: negative comparator rows must not enter the valid restore fixture.
docker exec -e PGPASSWORD=contract-only-password "$container_id" \
  pg_dump -h 127.0.0.1 -U postgres -d postgres --schema-only |
  docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
    psql -X -q -h 127.0.0.1 -U postgres -d "$source_database" -v ON_ERROR_STOP=1
fixture_hex="$(tr -d '\r\n' < "$root/tests/fixtures/player_snapshot_v1_${profile}.hex")"
docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
  psql -X -q -h 127.0.0.1 -U postgres -d "$source_database" -v ON_ERROR_STOP=1 \
    -v "fixture_hex=$fixture_hex" < "$root/supabase/tests/replay_backup_valid_seed.sql"
before=()
for table in "${tables[@]}"; do
  value="$(fingerprint "$source_database" "$table")"
  [[ "$value" == 1:* ]] || { echo 'valid backup fixture must contain one row per evidence relation' >&2; exit 1; }
  before+=("$value")
done
sql postgres "create database $restored_database template template0" >/dev/null
# Use the server image's matching pg_dump/pg_restore, not the host client version.
docker exec -e PGPASSWORD=contract-only-password "$container_id" \
  pg_dump -h 127.0.0.1 -U postgres -d "$source_database" --format=custom |
  docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
    pg_restore -h 127.0.0.1 -U postgres -d "$restored_database" --exit-on-error
docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
  psql -X -q -h 127.0.0.1 -U postgres -d "$restored_database" -v ON_ERROR_STOP=1 \
    -v "fixture_hex=$fixture_hex" < "$root/supabase/tests/replay_backup_restored_contract.sql"
for index in "${!tables[@]}"; do
  [[ "$(fingerprint "$restored_database" "${tables[$index]}")" == "${before[$index]}" ]] || {
    echo 'replay backup restore data mismatch' >&2; exit 1;
  }
done
echo "GREEN $profile backup restored with identical full-row evidence fingerprints"
