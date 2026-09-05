-- 2026-09-28: durable, private identity boundary for read-only imports of
-- imported-unclaimed characters.  No importer/store entrypoint is exposed in
-- this migration; a later transaction owns insertion and watermark advances.

create table if not exists private.game_imported_unclaimed_batches (
  world_id text not null,
  batch_id uuid not null,
  source_manifest_id text not null,
  source_sha256 text not null,
  source_byte_size bigint not null,
  parser_version text not null,
  abi bigint not null,
  start_marker text not null,
  end_marker text not null,
  batch_sequence bigint not null,
  recorded_at timestamptz not null default clock_timestamp(),
  primary key (world_id, batch_id),
  -- This is the full BatchIdentityInput tuple in its canonical field order.
  -- batch_id and batch_sequence are audit references, never retry identity.
  constraint game_imported_unclaimed_batches_durable_identity_key
    unique (world_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker),
  constraint game_imported_unclaimed_batches_world_bounded
    check (char_length(world_id) between 1 and 256
      and world_id ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batches_source_manifest_id_canonical
    check (char_length(source_manifest_id) between 1 and 256
      and source_manifest_id ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batches_source_sha256_format
    check (source_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_imported_unclaimed_batches_source_byte_size_safe
    check (source_byte_size between 0 and 9007199254740991),
  constraint game_imported_unclaimed_batches_parser_version_semver
    check (parser_version ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.((0|[1-9][0-9]*)|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'),
  constraint game_imported_unclaimed_batches_abi_safe_positive
    check (abi between 1 and 9007199254740991),
  constraint game_imported_unclaimed_batches_start_marker_canonical
    check (char_length(start_marker) between 1 and 256
      and start_marker ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batches_end_marker_canonical
    check (char_length(end_marker) between 1 and 256
      and end_marker ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batches_range_nonempty
    check (start_marker <> end_marker),
  constraint game_imported_unclaimed_batches_sequence_nonnegative
    check (batch_sequence >= 0)
);

create table if not exists private.game_imported_unclaimed_batch_watermarks (
  world_id text primary key,
  watermark_sequence bigint not null default 0,
  committed_batch_id uuid not null,
  observed_at timestamptz not null default clock_timestamp(),
  constraint game_imported_unclaimed_batch_watermarks_world_bounded
    check (char_length(world_id) between 1 and 256
      and world_id ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batch_watermarks_sequence_nonnegative
    check (watermark_sequence >= 0),
  constraint game_imported_unclaimed_batch_watermarks_committed_batch_fkey
    foreign key (world_id, committed_batch_id)
    references private.game_imported_unclaimed_batches(world_id, batch_id)
    on delete restrict
);

alter table private.game_imported_unclaimed_batches enable row level security;
alter table private.game_imported_unclaimed_batch_watermarks enable row level security;

revoke all on table private.game_imported_unclaimed_batches,
  private.game_imported_unclaimed_batch_watermarks
  from public, anon, authenticated, service_role;

-- Imported batch rows are immutable evidence.  The future importer first
-- resolves this exact identity, then either accepts the retry or inserts it.
create or replace function private.reject_game_imported_unclaimed_batch_mutation()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'imported-unclaimed batch identities are immutable';
end;
$$;

drop trigger if exists game_imported_unclaimed_batches_immutable
  on private.game_imported_unclaimed_batches;
create trigger game_imported_unclaimed_batches_immutable
before update or delete on private.game_imported_unclaimed_batches
for each row execute function private.reject_game_imported_unclaimed_batch_mutation();

-- Equal assignments make an exact transaction retry harmless; a lower value
-- is rejected so no later importer path can move a world's watermark back.
-- The watermark must also name the immutable batch that committed that exact
-- sequence, making its operational state independently auditable.
create or replace function private.enforce_game_imported_unclaimed_batch_watermark_monotone()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
declare
  v_batch_sequence bigint;
begin
  if TG_OP = 'UPDATE'
     and (new.world_id <> old.world_id or new.watermark_sequence < old.watermark_sequence) then
    raise exception using errcode = 'P0001',
      message = 'imported-unclaimed batch watermark must be monotone';
  end if;
  select batch_sequence into v_batch_sequence
    from private.game_imported_unclaimed_batches
   where world_id = new.world_id and batch_id = new.committed_batch_id;
  if v_batch_sequence is null or v_batch_sequence <> new.watermark_sequence then
    raise exception using errcode = 'P0001',
      message = 'imported-unclaimed batch watermark must name its committed batch';
  end if;
  return new;
end;
$$;

drop trigger if exists game_imported_unclaimed_batch_watermarks_monotone
  on private.game_imported_unclaimed_batch_watermarks;
create trigger game_imported_unclaimed_batch_watermarks_monotone
before insert or update on private.game_imported_unclaimed_batch_watermarks
for each row execute function private.enforce_game_imported_unclaimed_batch_watermark_monotone();

revoke all on function private.reject_game_imported_unclaimed_batch_mutation()
  from public, anon, authenticated, service_role;
revoke all on function private.enforce_game_imported_unclaimed_batch_watermark_monotone()
  from public, anon, authenticated, service_role;
