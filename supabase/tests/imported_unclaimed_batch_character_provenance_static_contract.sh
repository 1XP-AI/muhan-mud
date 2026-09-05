#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql'
contract='supabase/tests/imported_unclaimed_batch_character_provenance_contract.sql'

fail() {
  printf 'batch character provenance schema contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fqx -- "$1" "$migration" >/dev/null || fail "missing source contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" >/dev/null || fail "missing SQL contract: $1"
}

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
require_source 'create table if not exists private.game_imported_unclaimed_batch_members ('
require_source '  primary key (character_id),'
require_source '    references private.game_imported_unclaimed_batches(world_id, stream_id, batch_sequence)'
require_source '    on delete restrict,'
require_source '    references public.game_characters(id)'
require_source '    on delete restrict'
require_source 'alter table private.game_imported_unclaimed_batch_members enable row level security;'
require_source 'create trigger game_imported_unclaimed_batch_members_immutable'
require_source 'before update or delete on private.game_imported_unclaimed_batch_members'
require_source '  from public, anon, authenticated, service_role;'
require_contract "\\ir ../migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql"
require_contract "'23505'"
require_contract "'P0001'"
require_contract "'23503'"
require_contract 'rollback;'
