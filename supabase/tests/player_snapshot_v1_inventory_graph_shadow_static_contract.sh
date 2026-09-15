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
require_source 'v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;'
require_source 'from private.game_character_m4_file_snapshot_manifests'
require_source "v_manifest.snapshot_format <> 'legacy-file-manifest-v1'"
require_source 'v_artifact.source_octets <> v_manifest.snapshot_octets'
require_source 'create or replace function private.player_snapshot_v1_inventory_graph_shadow_nodes(p_payload bytea)'
require_source 'create or replace function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt('
require_source 'create or replace function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('
require_source 'left join private.game_character_m4_file_snapshot_manifests as m'
require_source 'left join private.game_character_player_snapshot_v1_artifacts as a'
require_source "when m.character_id is null or a.character_id is null then 'INCONSISTENT'"
require_source "when s.character_id is null then 'MISSING'"
require_source 'm.snapshot_octets between 1 and 67108864'
require_source 'a.snapshot_octets between 48 and 4194352'
require_source "session_user <> 'mud_writer_login'"
require_source "current_setting('role', true) is distinct from 'mud_writer'"
require_source 'on conflict do nothing'
require_source "return query select 'EXACT_RETRY'::text;"
require_source 'before update or delete on private.game_character_player_snapshot_v1_inventory_graph_shadows'
require_source 'before update or delete on private.game_character_player_snapshot_v1_inventory_graph_shadow_items'
require_source 'grant execute on function private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)'
require_source 'grant execute on function private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)'
# The required manifest format literal is legacy-file-manifest-v1; reject
# prose/schema surface that would actually read or model a legacy file instead.
forbid_source '(^|[^[:alpha:]])(gameplay|legacy[[:space:]_-]+file[[:space:]]|policy|inventory[_-]?(name|description|weight|damage|value|flags))([^[:alpha:]]|$)'
require_contract "'0:root:0,1:0:0,2:1:0,3:0:1,4:root:1'"
require_contract "'EXACT_RETRY'"
require_contract "'P0001'"
require_contract "'service_role'"
require_contract 'a missing M4 manifest rejects with P0001 and leaves no graph shadow rows'
require_contract 'a mismatched M4 manifest rejects with P0001 and leaves no graph shadow rows'
require_contract 'a missing artifact rejects with P0001 and leaves no graph shadow rows'
require_contract 'a mismatched artifact rejects with P0001 and leaves no graph shadow rows'
require_contract 'reconciliation retains the same receipt as INCONSISTENT when M4 manifest evidence is missing'
require_contract 'reconciliation retains the same receipt as INCONSISTENT when M4 manifest evidence is mismatched'
require_contract 'reconciliation retains the same receipt as INCONSISTENT when artifact evidence is missing'
require_contract 'reconciliation retains the same receipt as INCONSISTENT when artifact evidence is mismatched'
require_contract 'a valid receipt -> M4 manifest -> PlayerSnapshotV1 artifact chain with no graph shadow row is returned exactly as MISSING'
require_contract "and shadow_state = 'INCONSISTENT'"
require_contract 'A conflicting existing'
require_contract 'the checked-in nested-inventory fixture must remain valid artifact evidence'
require_contract 'rollback;'
