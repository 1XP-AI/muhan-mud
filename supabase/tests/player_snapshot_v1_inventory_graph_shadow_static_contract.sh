#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20261003000000_player_snapshot_v1_inventory_graph_shadow.sql'
contract='supabase/tests/player_snapshot_v1_inventory_graph_shadow_contract.sql'

fail() {
  printf 'PlayerSnapshotV1 inventory graph shadow static contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fq -- "$1" "$migration" || fail "missing migration contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" || fail "missing executable SQL coverage: $1"
}

forbid_source() {
  if grep -Eiq -- "$1" "$migration"; then
    fail "forbidden authority or speculative schema surface: $1"
  fi
}

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
require_source 'create table if not exists private.game_character_player_snapshot_v1_inventory_graph_shadows ('
require_source 'create table if not exists private.game_character_player_snapshot_v1_inventory_graph_shadow_items ('
require_source 'references private.game_character_player_snapshot_v1_artifacts(character_id, command_id)'
require_source 'references private.game_character_player_snapshot_v1_inventory_graph_shadows(character_id, command_id)'
require_source 'create or replace function private.player_snapshot_v1_inventory_graph_shadow_nodes(p_payload bytea)'
require_source 'create or replace function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt('
require_source 'create or replace function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('
require_source "session_user <> 'mud_writer_login'"
require_source "current_setting('role', true) is distinct from 'mud_writer'"
require_source 'on conflict do nothing'
require_source "return query select 'EXACT_RETRY'::text;"
require_source 'before update or delete on private.game_character_player_snapshot_v1_inventory_graph_shadows'
require_source 'before update or delete on private.game_character_player_snapshot_v1_inventory_graph_shadow_items'
require_source 'grant execute on function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)'
require_source 'grant execute on function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)'
forbid_source '(^|[^[:alpha:]])(gameplay|legacy[_-]?file|policy|inventory[_-]?(name|description|weight|damage|value|flags))([^[:alpha:]]|$)'
require_contract "'0:root:0,1:0:0,2:1:0,3:0:1,4:root:1'"
require_contract "'EXACT_RETRY'"
require_contract "'P0001'"
require_contract "'service_role'"
require_contract 'the checked-in nested-inventory fixture must remain valid artifact evidence'
require_contract 'rollback;'
