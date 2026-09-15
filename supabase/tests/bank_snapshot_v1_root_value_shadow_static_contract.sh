#!/usr/bin/env bash
set -euo pipefail
migration='supabase/migrations/20261007000000_bank_snapshot_v1_root_value_shadow.sql'
contract='supabase/tests/bank_snapshot_v1_root_value_shadow_contract.sql'
harness='supabase/tests/bank_snapshot_v1_root_value_shadow_disposable_contract.sh'
fail() { printf 'BankSnapshotV1 root-value shadow static contract failed: %s\n' "$1" >&2; exit 1; }
require_source() { grep -Fq -- "$1" "$migration" || fail "missing migration contract: $1"; }
require_contract() { grep -Fq -- "$1" "$contract" || fail "missing SQL coverage: $1"; }
require_harness() { grep -Fq -- "$1" "$harness" || fail "missing disposable harness contract: $1"; }
[[ -f "$migration" && -f "$contract" && -x "$harness" ]] || fail 'missing migration, executable SQL contract, or executable harness'
require_source 'root_value bigint not null'
require_source 'p_root_value bigint'
require_source 'r.character_id is null or m.character_id is null'
require_source 'BankSnapshotV1 root-value shadow has no matching M3 receipt and M4 manifest evidence'
require_source 'BankSnapshotV1 root-value shadow conflicts with immutable evidence'
require_source 'root_value=p_root_value'
require_contract 'matching M4 manifest settles before root-value evidence'
require_contract 'canonical signed-i64 minimum root value records'
require_contract 'same command and value exact-retry without mutation'
require_contract "expect_state('P0001','select pg_temp.record_root(7)')"
require_contract 'set local session authorization mud_writer_login'
require_contract 'set local role mud_writer'
require_contract "'a9070000-0000-0000-0000-000000000001'"
require_contract "'b9070000-0000-0000-0000-000000000001'"
require_contract "'c9070000-0000-0000-0000-000000000001'"
require_contract 'changed-value retry and topology-key mismatch leave immutable root evidence untouched'
require_harness 'BANK_SNAPSHOT_V1_ROOT_VALUE_SHADOW_ALLOW_DISPOSABLE=1 is required'
require_harness 'DATABASE_URL is required'
require_harness '--no-password --no-psqlrc --quiet --set=ON_ERROR_STOP=1'
require_harness 'bank_snapshot_v1_root_value_shadow_contract.sql'
if grep -Fq -- 'docker' "$harness" || grep -Fq -- 'createdb' "$harness" \
  || grep -Fq -- 'dropdb' "$harness" || grep -Fq -- '/supabase/migrations/' "$harness"; then
  fail 'disposable harness must not provision a database or apply migrations'
fi
