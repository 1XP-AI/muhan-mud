-- 2026-09-28: immutable importer ledger evidence. The TypeScript importer
-- owns batch, character, and per-world/stream watermark writes in one
-- serializable transaction.

create table if not exists private.game_imported_unclaimed_batches (
  world_id text not null,
  stream_id text not null,
  batch_sequence bigint not null,
  identity_key text not null,
  source_manifest_id text not null,
  source_sha256 text not null,
  source_byte_size bigint not null,
  parser_version text not null,
  abi bigint not null,
  start_marker text not null,
  end_marker text not null,
  record_count bigint not null,
  recorded_at timestamptz not null default clock_timestamp(),
  primary key (world_id, stream_id, batch_sequence),
  constraint game_imported_unclaimed_batches_stream_identity_key
    unique (world_id, stream_id, identity_key),
  constraint game_imported_unclaimed_batches_world_bounded
    check (char_length(world_id) between 1 and 64
      and world_id ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batches_stream_bounded
    check (char_length(stream_id) between 1 and 256
      and stream_id ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
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
    check (batch_sequence between 0 and 9007199254740991),
  constraint game_imported_unclaimed_batches_record_count_safe
    check (record_count between 0 and 9007199254740991)
);

create table if not exists private.game_imported_unclaimed_batch_watermarks (
  world_id text not null,
  stream_id text not null,
  watermark_sequence bigint not null,
  committed_batch_sequence bigint not null,
  observed_at timestamptz not null default clock_timestamp(),
  primary key (world_id, stream_id),
  constraint game_imported_unclaimed_batch_watermarks_world_bounded
    check (char_length(world_id) between 1 and 64
      and world_id ~ '^[a-z0-9]([a-z0-9._:-]{0,254}[a-z0-9])?$'),
  constraint game_imported_unclaimed_batch_watermarks_sequence_nonnegative
    check (watermark_sequence between 0 and 9007199254740991),
  constraint game_imported_unclaimed_batch_watermarks_committed_sequence_matches
    check (committed_batch_sequence = watermark_sequence),
  constraint game_imported_unclaimed_batch_watermarks_committed_batch_fkey
    foreign key (world_id, stream_id, committed_batch_sequence)
    references private.game_imported_unclaimed_batches(world_id, stream_id, batch_sequence)
    on delete restrict
);

alter table private.game_imported_unclaimed_batches enable row level security;
alter table private.game_imported_unclaimed_batch_watermarks enable row level security;

revoke all on table private.game_imported_unclaimed_batches,
  private.game_imported_unclaimed_batch_watermarks
  from public, anon, authenticated, service_role;

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

-- Exact retry is a read-only store decision. Strictly increasing writes here
-- ensure an equal sequence can never retarget a durable watermark.
create or replace function private.enforce_game_imported_unclaimed_batch_watermark_monotone()
returns trigger
language plpgsql
set search_path = pg_catalog, private
as $$
begin
  if TG_OP = 'UPDATE'
     and (new.world_id <> old.world_id
       or new.stream_id <> old.stream_id
       or new.watermark_sequence <= old.watermark_sequence
       or new.committed_batch_sequence <> new.watermark_sequence) then
    raise exception using errcode = 'P0001',
      message = 'imported-unclaimed batch watermark must strictly advance';
  end if;
  if new.committed_batch_sequence <> new.watermark_sequence then
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
