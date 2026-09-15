#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20261002000000_legacy_locator_resolver.sql'
contract='supabase/tests/legacy_locator_resolver_contract.sql'

fail() {
  printf 'legacy locator resolver static contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fq -- "$1" "$migration" >/dev/null || fail "missing migration contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" >/dev/null || fail "missing executable SQL coverage: $1"
}

forbid_source() {
  if grep -Eiq -- "$1" "$migration"; then
    fail "forbidden mutable or generic resolver surface: $1"
  fi
}

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
require_source 'create or replace function public.resolve_game_imported_legacy_locator('
require_source '  p_world_id text,'
require_source '  p_canonical_legacy_name text,'
require_source '  p_legacy_name_sha1 text,'
require_source '  p_legacy_shard char(2)'
require_source 'returns table (character_id uuid)'
require_source 'security definer'
require_source 'set search_path = pg_catalog, private, public'
require_source 'p_canonical_legacy_name <> private.game_identity_canonical_legacy_name(p_canonical_legacy_name)'
require_source "p_legacy_name_sha1 <> encode(digest(convert_to(p_canonical_legacy_name, 'UTF8'), 'sha1'), 'hex')"
require_source 'from private.game_imported_unclaimed_batch_member_legacy_locators as locator'
require_source 'join private.game_imported_unclaimed_batch_members as member'
require_source 'join public.game_characters as character'
require_source 'and character.legacy_name = p_canonical_legacy_name'
require_source 'and character.legacy_name_key = p_canonical_legacy_name'
require_source "message = 'legacy locator is unavailable'"
require_source 'revoke all on function public.resolve_game_imported_legacy_locator(text, text, text, char(2))'
require_source 'grant execute on function public.resolve_game_imported_legacy_locator(text, text, text, char(2))'
require_source '  to service_role;'
forbid_source '(^|[^[:alpha:]])(insert|update|delete|merge|create[[:space:]]+table|alter[[:space:]]+table|drop[[:space:]]+table)([^[:alpha:]]|$)'
require_contract "\\ir ../migrations/20261002000000_legacy_locator_resolver.sql"
require_contract "'TABLE(character_id uuid)'"
require_contract "'service_role'"
require_contract "'anon'"
require_contract "'authenticated'"
require_contract "'22023'"
require_contract "'P0001'"
require_contract 'exact immutable locator tuple returns one UUID and no additional output'
require_contract 'leave character, member, and locator state unchanged'
require_contract 'rollback;'
