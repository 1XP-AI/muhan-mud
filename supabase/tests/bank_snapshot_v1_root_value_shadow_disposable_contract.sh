#!/usr/bin/env bash
set -euo pipefail

# Explicit operator-only contract lane. The supplied DATABASE_URL must name an
# already-migrated disposable PostgreSQL 17 database; this script neither
# creates a database nor applies migrations. Invoke only after ordered
# migrations through 20261007000000 have been applied:
#   BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 \
#   DATABASE_URL='<operator-supplied disposable PostgreSQL 17 database URL>' \
#   ./supabase/tests/bank_snapshot_v1_root_value_shadow_disposable_contract.sh

fail_closed() {
  printf 'BankSnapshotV1 root-value shadow disposable contract not run: %s\n' "$1" >&2
  exit 2
}

[[ "${BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE:-}" == 1 ]] ||
  fail_closed 'BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 is required'
[[ -n "${DATABASE_URL:-}" ]] || fail_closed 'DATABASE_URL is required'
command -v psql >/dev/null || fail_closed 'psql is required'

repo_root="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
contract_path="$repo_root/supabase/tests/bank_snapshot_v1_root_value_shadow_contract.sql"

[[ -f "$contract_path" ]] || fail_closed 'approved root-value contract is missing'

umask 077
psql_output="$(mktemp "${TMPDIR:-/tmp}/bank-root-value-shadow-contract.XXXXXX")"
cleanup() {
  rm -f -- "$psql_output"
}
trap cleanup EXIT

# Keep psql diagnostics in a protected temporary file. The contract itself
# establishes session_user mud_writer_login and SET ROLE mud_writer before it
# records synthetic immutable receipt evidence.
if psql --dbname="$DATABASE_URL" --no-password --no-psqlrc --quiet --set=ON_ERROR_STOP=1 \
  --file="$contract_path" >"$psql_output" 2>&1; then
  printf 'BankSnapshotV1 root-value shadow disposable contract passed\n'
else
  printf 'BankSnapshotV1 root-value shadow disposable contract failed\n' >&2
  exit 1
fi
