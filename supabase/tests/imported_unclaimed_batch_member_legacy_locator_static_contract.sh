#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20261001000000_imported_unclaimed_batch_member_legacy_locator.sql'
contract='supabase/tests/imported_unclaimed_batch_member_legacy_locator_contract.sql'
identity_migration='supabase/migrations/20260902000000_game_identity.sql'

fail() {
  printf 'batch member legacy locator static contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fq -- "$1" "$migration" >/dev/null || fail "missing migration contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" >/dev/null || fail "missing SQL coverage: $1"
}

require_identity_source() {
  grep -Fq -- "$1" "$identity_migration" >/dev/null || fail "missing canonical game-identity rule: $1"
}

[[ -f "$migration" && -f "$contract" && -f "$identity_migration" ]] || fail 'missing migration, identity rule, or executable SQL contract'
require_source 'create table if not exists private.game_imported_unclaimed_batch_member_legacy_locators ('
require_source '  character_id uuid primary key,'
require_source '  canonical_legacy_name text not null,'
require_source '  legacy_name_sha1 text not null,'
require_source '  legacy_shard char(2) not null,'
require_source '    references private.game_imported_unclaimed_batch_members(character_id)'
require_source '    on delete restrict,'
require_source "legacy_name_sha1 ~ '^[0-9a-f]{40}\$'"
require_source "legacy_name_sha1 = encode(digest(convert_to(canonical_legacy_name, 'UTF8'), 'sha1'), 'hex')"
require_source 'check (legacy_shard = substr(legacy_name_sha1, 1, 2))'
require_identity_source "    and legacy_name !~ E'[[:cntrl:]/\\\\\\\\:]'"
require_source "      and canonical_legacy_name !~ E'[[:cntrl:]/\\\\\\\\:]'"
require_source 'alter table private.game_imported_unclaimed_batch_member_legacy_locators enable row level security;'
require_source 'before insert on private.game_imported_unclaimed_batch_member_legacy_locators'
require_source 'before update or delete on private.game_imported_unclaimed_batch_member_legacy_locators'
require_source 'and character.lifecycle = '\''imported_unclaimed'\'''
require_source 'and character.owner_user_id is null'
require_source 'and character.imported_file_sha256 is not null'
require_source '  from public, anon, authenticated, service_role;'
require_contract '\ir bootstrap_contract.sql'
require_contract '\ir ../migrations/20260902000000_game_identity.sql'
require_contract "\\ir ../migrations/20261001000000_imported_unclaimed_batch_member_legacy_locator.sql"
require_contract "array['character_id', 'canonical_legacy_name', 'legacy_name_sha1', 'legacy_shard']::text[]"
require_contract "'23514'"
require_contract "'P0001'"
require_contract 'locator C-safe regex must match the game-identity rule and reject backslash'
require_contract 'on conflict (character_id) do nothing;'
require_contract 'rollback;'
