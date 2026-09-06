#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)"
harness="$repo_root/supabase/tests/bank_snapshot_v1_root_value_shadow_disposable_contract.sh"
contract="$repo_root/supabase/tests/bank_snapshot_v1_root_value_shadow_contract.sql"

fail() {
  printf 'BankSnapshotV1 root-value shadow disposable harness test failed: %s\n' "$1" >&2
  exit 1
}

[[ -x "$harness" ]] || fail 'harness must be executable'

umask 077
test_root="$(mktemp -d "${TMPDIR:-/tmp}/bank-root-value-shadow-harness.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT
fake_bin="$test_root/bin"
psql_args="$test_root/psql-args"
mkdir "$fake_bin"

cat >"$fake_bin/psql" <<'SH'
#!/usr/bin/env bash
printf '%s\n' '---' >>"$HARNESS_PSQL_ARGS"
printf '%s\n' "$@" >>"$HARNESS_PSQL_ARGS"
exit "${HARNESS_PSQL_EXIT:-0}"
SH
chmod 0700 "$fake_bin/psql"

run_harness() {
  local label="$1"
  shift
  : >"$psql_args"
  set +e
  "$@" >"$test_root/$label.output" 2>&1
  local result=$?
  set -e
  printf '%s' "$result"
}

assert_closed_without() {
  local label="$1"
  local result="$2"
  local required_message="$3"
  [[ "$result" == 2 ]] || fail "$label must fail closed with exit 2"
  [[ ! -s "$psql_args" ]] || fail "$label must not invoke psql"
  rg -q --fixed-strings -- "$required_message" "$test_root/$label.output" ||
    fail "$label must report only the missing safe prerequisite"
}

path_value="$fake_bin:/usr/bin:/bin"
result="$(run_harness no-opt-in env -i PATH="$path_value" bash "$harness")"
assert_closed_without no-opt-in "$result" 'ALLOW_DISPOSABLE=1 is required'

result="$(run_harness acknowledgement-only env -i PATH="$path_value" \
  BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 bash "$harness")"
assert_closed_without acknowledgement-only "$result" 'DATABASE_URL is required'

result="$(run_harness database-url-only env -i PATH="$path_value" \
  DATABASE_URL='postgresql://example.invalid/contract' bash "$harness")"
[[ "$result" == 2 ]] || fail 'database URL without acknowledgement must fail closed with exit 2'
[[ ! -s "$psql_args" ]] || fail 'database URL without acknowledgement must not invoke psql'
rg -q --fixed-strings -- 'ALLOW_DISPOSABLE=1 is required' "$test_root/database-url-only.output" ||
  fail 'database URL without acknowledgement must report only the missing safe prerequisite'

result="$(run_harness no-psql env -i PATH="$test_root/empty-bin" \
  DATABASE_URL='postgresql://example.invalid/contract' \
  BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 /bin/bash "$harness")"
assert_closed_without no-psql "$result" 'psql is required'

result="$(run_harness allowed env -i PATH="$path_value" HARNESS_PSQL_ARGS="$psql_args" \
  DATABASE_URL='postgresql://example.invalid/contract' \
  BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 bash "$harness")"
[[ "$result" == 0 ]] || fail 'both explicit opt-ins must construct the contract invocation'
rg -q --fixed-strings -- 'BankSnapshotV1 root-value shadow disposable contract passed' \
  "$test_root/allowed.output" || fail 'successful run must emit the aggregate success outcome'

rg -Fx -- '--no-password' "$psql_args" >/dev/null || fail 'psql must never prompt for credentials'
rg -Fx -- '--no-psqlrc' "$psql_args" >/dev/null || fail 'psql must disable user configuration'
rg -Fx -- '--quiet' "$psql_args" >/dev/null || fail 'psql must be quiet'
rg -Fx -- '--set=ON_ERROR_STOP=1' "$psql_args" >/dev/null || fail 'psql must stop on contract errors'
rg -Fx -- '--dbname=postgresql://example.invalid/contract' "$psql_args" >/dev/null ||
  fail 'psql must receive only the operator-provided database URL'
rg -Fx -- "--file=$contract" "$psql_args" >/dev/null ||
  fail 'psql must execute only the approved root-value contract'
[[ "$(rg -c --fixed-strings -- '---' "$psql_args")" == 1 ]] ||
  fail 'the harness must execute exactly one psql contract invocation'
if rg -q --fixed-strings -- 'postgresql://example.invalid/contract' "$test_root/allowed.output"; then
  fail 'operator output must not expose connection data'
fi

result="$(run_harness psql-failure env -i PATH="$path_value" HARNESS_PSQL_ARGS="$psql_args" HARNESS_PSQL_EXIT=1 \
  DATABASE_URL='postgresql://example.invalid/contract' \
  BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 bash "$harness")"
[[ "$result" == 1 ]] || fail 'a psql failure must return an aggregate contract failure'
rg -q --fixed-strings -- 'BankSnapshotV1 root-value shadow disposable contract failed' \
  "$test_root/psql-failure.output" || fail 'psql failure must emit only the aggregate failure outcome'
if rg -q --fixed-strings -- 'postgresql://example.invalid/contract' "$test_root/psql-failure.output"; then
  fail 'failed operator output must not expose connection data'
fi

if rg -q --fixed-strings -- 'docker' "$harness" \
  || rg -q --fixed-strings -- 'createdb' "$harness" \
  || rg -q --fixed-strings -- 'dropdb' "$harness" \
  || rg -q --fixed-strings -- '/supabase/migrations/' "$harness"; then
  fail 'the harness must neither provision a disposable database nor apply migrations'
fi

printf 'BankSnapshotV1 root-value shadow disposable harness hermetic tests passed\n'
