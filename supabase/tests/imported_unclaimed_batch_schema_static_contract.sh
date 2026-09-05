#!/usr/bin/env bash
set -euo pipefail

# This is supplemental source coverage.  The executable PostgreSQL contract
# is authoritative and owns replay, immutability, per-stream identity, and
# watermark behavior used by the importer store.
migration='supabase/migrations/20260928000000_imported_unclaimed_batch_ledger.sql'
contract='supabase/tests/imported_unclaimed_batch_ledger_contract.sql'

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
[[ -f "$contract" ]] || fail "missing executable SQL contract: $contract"

require_source 'create table if not exists private.game_imported_unclaimed_batches ('
require_source '  stream_id text not null,'
require_source '  primary key (world_id, stream_id, batch_sequence),'
require_source '  identity_key text not null,'
require_source '  source_manifest_id text not null,'
require_source '  source_byte_size bigint not null,'
require_source '  abi bigint not null,'
require_source '  start_marker text not null,'
require_source '  end_marker text not null,'
require_source '    unique (world_id, stream_id, identity_key),'
require_source '    check (batch_sequence between 0 and 9007199254740991),'
require_source 'create table if not exists private.game_imported_unclaimed_batch_watermarks ('
require_source '  primary key (world_id, stream_id),'
require_source '  committed_batch_sequence bigint not null,'
require_source '  constraint game_imported_unclaimed_batch_watermarks_sequence_nonnegative'
require_source '    check (watermark_sequence between 0 and 9007199254740991),'
require_source 'alter table private.game_imported_unclaimed_batches enable row level security;'
require_source 'alter table private.game_imported_unclaimed_batch_watermarks enable row level security;'
require_source '  from public, anon, authenticated, service_role;'
require_source 'create trigger game_imported_unclaimed_batch_watermarks_monotone'
require_source '       or new.watermark_sequence <= old.watermark_sequence'
