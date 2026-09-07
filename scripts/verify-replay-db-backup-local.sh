#!/usr/bin/env bash
# Only accepts the disposable PG container created by the replay runner.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable ]] || exit 2
container_id="${2:-}"
[[ "$container_id" =~ ^[0-9a-f]{64}$ ]] || exit 2
[[ "$(docker inspect --format '{{index .Config.Labels "muhan.replay-disposable"}}' "$container_id")" == true ]] || exit 2
sql() {
  docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
    psql -X -h 127.0.0.1 -U postgres -d "$1" -v ON_ERROR_STOP=1 -At -c "$2"
}
fingerprint() {
  sql "$1" "select count(*)::text || ':' || coalesce(md5(string_agg(to_jsonb(t)::text, '' order by character_id, command_id)), '') from private.$2 t"
}
tables=(game_character_player_snapshot_v1_artifacts game_character_shadow_receipts game_character_player_snapshot_v1_level_projections)
before=()
for table in "${tables[@]}"; do before+=("$(fingerprint postgres "$table")"); done
sql postgres 'create database replay_restore_check template template0' >/dev/null
# Use the server image's matching pg_dump/pg_restore, not the host client version.
docker exec -e PGPASSWORD=contract-only-password "$container_id" \
  pg_dump -h 127.0.0.1 -U postgres -d postgres --format=custom |
  docker exec -i -e PGPASSWORD=contract-only-password "$container_id" \
    pg_restore -h 127.0.0.1 -U postgres -d replay_restore_check --exit-on-error
for index in "${!tables[@]}"; do
  [[ "$(fingerprint replay_restore_check "${tables[$index]}")" == "${before[$index]}" ]] || {
    echo 'replay backup restore data mismatch' >&2; exit 1;
  }
done
echo 'GREEN replay backup restored with identical full-row evidence fingerprints'
