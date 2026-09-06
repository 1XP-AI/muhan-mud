#!/usr/bin/env bash
set -euo pipefail

# Explicit operator-only contract lane. The supplied DATABASE_URL must name an
# already-migrated disposable PostgreSQL database; this script never creates a
# database and never applies migrations. Invoke only after the ordered
# migrations through 20261006000000 have been applied:
#   PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ALLOW_DISPOSABLE=1 \
#   DATABASE_URL='<operator-supplied disposable database URL>' \
#   ./supabase/tests/player_snapshot_normalized_v1_projection_persistence_disposable_contract.sh

fail_closed() {
  printf 'PlayerSnapshotNormalizedV1 projection persistence disposable contract not run: %s\n' "$1" >&2
  exit 2
}

[[ "${PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ALLOW_DISPOSABLE:-}" == 1 ]] ||
  fail_closed 'PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ALLOW_DISPOSABLE=1 is required'
[[ -n "${DATABASE_URL:-}" ]] || fail_closed 'DATABASE_URL is required'
command -v psql >/dev/null || fail_closed 'psql is required'

repo_root="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
fixture_path="$repo_root/tests/fixtures/player_snapshot_v1_tree_inventory.hex"
contract_path="$repo_root/supabase/tests/player_snapshot_normalized_v1_projection_persistence_contract.sql"

[[ -f "$fixture_path" && -f "$contract_path" ]] || fail_closed 'approved fixture or contract is missing'

tree_fixture_hex="$(tr -d '\r\n' < "$fixture_path")"
[[ "$tree_fixture_hex" =~ ^[0-9a-f]+$ && "${#tree_fixture_hex}" -eq 7116 ]] ||
  fail_closed 'checked-in tree fixture is not canonical lowercase hex'
tree_fixture_sql="decode('$tree_fixture_hex', 'hex')"

umask 077
psql_output="$(mktemp "${TMPDIR:-/tmp}/pnp-projection-contract.XXXXXX")"
cleanup() {
  rm -f -- "$psql_output"
}
trap cleanup EXIT

# Keep all psql diagnostics in a protected temporary file. The operator sees
# only the aggregate result, preventing raw payload or connection data leaks.
if psql --dbname="$DATABASE_URL" --no-password --no-psqlrc --quiet --set=ON_ERROR_STOP=1 \
  --set="pvi_tree_payload=$tree_fixture_sql" --file="$contract_path" \
  >"$psql_output" 2>&1; then
  printf 'PlayerSnapshotNormalizedV1 projection persistence disposable contract passed\n'
else
  printf 'PlayerSnapshotNormalizedV1 projection persistence disposable contract failed\n' >&2
  exit 1
fi
