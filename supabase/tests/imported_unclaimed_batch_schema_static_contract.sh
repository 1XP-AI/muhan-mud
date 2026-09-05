#!/usr/bin/env bash
set -euo pipefail

# Static contract for the schema-only import boundary.  It deliberately avoids
# a database: importer/store behavior is introduced by a later slice.
migration='supabase/migrations/20260928000000_imported_unclaimed_batch_ledger.sql'

fail() {
  printf 'imported-unclaimed batch schema contract failed: %s\n' "$1" >&2
  exit 1
}

require_source() {
  local needle="$1"
  grep -Fqx -- "$needle" "$migration" >/dev/null \
    || fail "missing source contract: $needle"
}

[[ -f "$migration" ]] || fail "missing migration: $migration"

require_source 'create table if not exists private.game_imported_unclaimed_batches ('
require_source '  primary key (world_id, batch_id),'
require_source '    unique (world_id, source_sha256, parser_version),'
require_source '    unique (world_id, batch_sequence),'
require_source '    check (batch_sequence >= 0)'
require_source 'create table if not exists private.game_imported_unclaimed_batch_watermarks ('
require_source '  constraint game_imported_unclaimed_batch_watermarks_sequence_nonnegative'
require_source '    check (watermark_sequence >= 0)'
require_source 'alter table private.game_imported_unclaimed_batches enable row level security;'
require_source 'alter table private.game_imported_unclaimed_batch_watermarks enable row level security;'
require_source '  from public, anon, authenticated, service_role;'
require_source 'create trigger game_imported_unclaimed_batch_watermarks_monotone'
