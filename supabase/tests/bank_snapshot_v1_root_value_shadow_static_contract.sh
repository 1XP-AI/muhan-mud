#!/usr/bin/env bash
set -euo pipefail
migration='supabase/migrations/20261007000000_bank_snapshot_v1_root_value_shadow.sql'
contract='supabase/tests/bank_snapshot_v1_root_value_shadow_contract.sql'
fail() { printf 'BankSnapshotV1 root-value shadow static contract failed: %s\n' "$1" >&2; exit 1; }
require_source() { grep -Fq -- "$1" "$migration" || fail "missing migration contract: $1"; }
require_contract() { grep -Fq -- "$1" "$contract" || fail "missing SQL coverage: $1"; }
[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
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
