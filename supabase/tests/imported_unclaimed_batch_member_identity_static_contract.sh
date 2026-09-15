#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20261008000000_imported_unclaimed_batch_member_identity.sql'
contract='supabase/tests/imported_unclaimed_batch_member_identity_contract.sql'

fail() {
  printf 'batch member identity schema contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  grep -Fq -- "$1" "$migration" >/dev/null || fail "missing migration contract: $1"
}

require_contract() {
  grep -Fq -- "$1" "$contract" >/dev/null || fail "missing SQL coverage: $1"
}

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
require_source 'create table if not exists private.game_imported_unclaimed_batch_member_identities ('
require_source '  canonical_legacy_name text not null,'
require_source '  legacy_shard char(2) not null,'
require_source '  imported_file_sha256 text not null,'
require_source '  storage_format integer not null,'
require_source '  primary key (world_id, stream_id, batch_sequence, canonical_legacy_name),'
require_source '    references private.game_imported_unclaimed_batches(world_id, stream_id, batch_sequence)'
require_source '    on delete restrict,'
require_source "imported_file_sha256 ~ '^[0-9a-f]{64}\$'"
require_source 'check (storage_format = 1)'
require_source 'alter table private.game_imported_unclaimed_batch_member_identities enable row level security;'
require_source 'before update or delete on private.game_imported_unclaimed_batch_member_identities'
require_source '  from public, anon, authenticated, service_role;'
require_contract '\ir bootstrap_contract.sql'
require_contract '\ir ../migrations/20261008000000_imported_unclaimed_batch_member_identity.sql'
require_contract "'23505'"
require_contract "'23514'"
require_contract "'P0001'"
require_contract 'rollback;'
