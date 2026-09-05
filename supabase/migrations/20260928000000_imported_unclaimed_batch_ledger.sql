-- 2026-09-28: durable, private identity boundary for read-only imports of
-- imported-unclaimed characters.  No importer/store entrypoint is exposed in
-- this migration; a later transaction owns insertion and watermark advances.

create table if not exists private.game_imported_unclaimed_batches (
  world_id text not null,
  batch_id uuid not null,
  source_sha256 text not null,
  parser_version text not null,
  batch_sequence bigint not null,
  recorded_at timestamptz not null default clock_timestamp(),
  primary key (world_id, batch_id),
  constraint game_imported_unclaimed_batches_retry_identity_key
    unique (world_id, source_sha256, parser_version),
  constraint game_imported_unclaimed_batches_sequence_key
    unique (world_id, batch_sequence),
  constraint game_imported_unclaimed_batches_world_bounded
    check (char_length(world_id) between 1 and 64 and world_id !~ '[[:cntrl:]]'),
  constraint game_imported_unclaimed_batches_source_sha256_format
    check (source_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_imported_unclaimed_batches_parser_version_bounded
    check (char_length(parser_version) between 1 and 128 and parser_version !~ '[[:cntrl:]]'),
  constraint game_imported_unclaimed_batches_sequence_nonnegative
    check (batch_sequence >= 0)
);

create table if not exists private.game_imported_unclaimed_batch_watermarks (
  world_id text primary key,
  watermark_sequence bigint not null default 0,
  observed_at timestamptz not null default clock_timestamp(),
  constraint game_imported_unclaimed_batch_watermarks_world_bounded
    check (char_length(world_id) between 1 and 64 and world_id !~ '[[:cntrl:]]'),
  constraint game_imported_unclaimed_batch_watermarks_sequence_nonnegative
    check (watermark_sequence >= 0)
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
create or replace function private.enforce_game_imported_unclaimed_batch_watermark_monotone()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
begin
  if new.world_id <> old.world_id or new.watermark_sequence < old.watermark_sequence then
    raise exception using errcode = 'P0001',
      message = 'imported-unclaimed batch watermark must be monotone';
  end if;
  return new;
end;
$$;

drop trigger if exists game_imported_unclaimed_batch_watermarks_monotone
  on private.game_imported_unclaimed_batch_watermarks;
create trigger game_imported_unclaimed_batch_watermarks_monotone
before update on private.game_imported_unclaimed_batch_watermarks
for each row execute function private.enforce_game_imported_unclaimed_batch_watermark_monotone();

revoke all on function private.reject_game_imported_unclaimed_batch_mutation()
  from public, anon, authenticated, service_role;
revoke all on function private.enforce_game_imported_unclaimed_batch_watermark_monotone()
  from public, anon, authenticated, service_role;
