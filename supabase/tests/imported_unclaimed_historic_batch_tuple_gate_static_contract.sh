#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20261009000000_imported_unclaimed_historic_batch_tuple_gate.sql'
contract='supabase/tests/imported_unclaimed_historic_batch_tuple_gate_contract.sql'
workflow='.github/workflows/ci.yml'

fail() {
  printf 'historic batch tuple gate static contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fq -- "$1" "$migration" >/dev/null || fail "missing migration contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" >/dev/null || fail "missing SQL coverage: $1"
}

forbid_source() {
  ! grep -Fq -- "$1" "$migration" >/dev/null || fail "unexpected lifecycle gate: $1"
}

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
[[ -f "$workflow" ]] || fail 'missing CI workflow'
require_source 'with qualified_batches as ('
require_source 'create table if not exists private.game_imported_unclaimed_batch_tuple_policy ('
require_source 'on conflict (policy_key) do nothing;'
require_source 'where not policy.backfill_complete'
require_source 'and batch.recorded_at < policy.historic_cutoff'
require_source 'set backfill_complete = true'
require_source 'and count(distinct character.legacy_name_key) = batch.record_count'
require_source "and character.lifecycle = 'imported_unclaimed'"
require_source 'and character.owner_user_id is null'
require_source 'and character.claimed_at is null'
forbid_source 'require_game_imported_unclaimed_batch_tuple_before_lifecycle_mutation'
forbid_source 'before update of lifecycle, owner_user_id, claimed_at on public.game_characters'
require_contract '\ir ../migrations/20261008000000_imported_unclaimed_batch_member_identity.sql'
require_contract '\ir ../migrations/20261009000000_imported_unclaimed_historic_batch_tuple_gate.sql'
require_contract 'batch_sequence in (1, 2)'
require_contract "set lifecycle = 'active'"
require_contract "set lifecycle = 'handoff_pending',"
require_contract "'ordinary lifecycle mutation stays outside historic replay policy'"
require_contract 'rollback;'

contract_lines=( $(grep -nF -- "--file=${contract}" "$workflow" | cut -d: -f1) )
migration_lines=( $(grep -nF -- "--file=${migration}" "$workflow" | cut -d: -f1) )
[[ "${#contract_lines[@]}" -eq 2 && "${#migration_lines[@]}" -eq 2 ]] || \
  fail 'expected both CI contract lanes to invoke the historic tuple migration and contract'
(( contract_lines[0] < migration_lines[0] && contract_lines[1] < migration_lines[1] )) || \
  fail 'historic tuple migration must be permanently applied after its contract in both CI lanes'
