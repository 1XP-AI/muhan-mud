-- 2026-10-08: private batch-time file tuples for every admitted record. This
-- relation deliberately does not reference public.game_characters: lifecycle,
-- ownership, storage, and source-file changes there must not rewrite ledger
-- evidence used to authenticate an exact reviewed-batch replay.

create table if not exists private.game_imported_unclaimed_batch_member_identities (
  world_id text not null,
  stream_id text not null,
  batch_sequence bigint not null,
  canonical_legacy_name text not null,
  legacy_shard char(2) not null,
  imported_file_sha256 text not null,
  storage_format integer not null,
  primary key (world_id, stream_id, batch_sequence, canonical_legacy_name),
  constraint game_imported_unclaimed_batch_member_identities_batch_fkey
    foreign key (world_id, stream_id, batch_sequence)
    references private.game_imported_unclaimed_batches(world_id, stream_id, batch_sequence)
    on delete restrict,
  constraint game_imported_unclaimed_batch_member_identities_name_c_safe
    check (
      char_length(canonical_legacy_name) between 1 and 12
      and octet_length(canonical_legacy_name) <= 14
      and canonical_legacy_name !~ E'[[:cntrl:]/\\\\:]'
      and canonical_legacy_name = private.game_identity_canonical_legacy_name(canonical_legacy_name)
      and canonical_legacy_name not in ('.', '..')
    ),
  constraint game_imported_unclaimed_batch_member_identities__ea393e73d4
    check (
      legacy_shard = substr(
        encode(digest(convert_to(canonical_legacy_name, 'UTF8'), 'sha1'), 'hex'), 1, 2
      )
    ),
  constraint game_imported_unclaimed_batch_member_identities_sha256_format
    check (imported_file_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_imported_unclaimed_batch_member_identities_storage_format
    check (storage_format = 1)
);

alter table private.game_imported_unclaimed_batch_member_identities enable row level security;

revoke all on table private.game_imported_unclaimed_batch_member_identities
  from public, anon, authenticated, service_role;

create or replace function private.reject_game_imported_unclaimed_batch_member_identity_mutation()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'imported-unclaimed batch member identities are immutable';
end;
$$;

drop trigger if exists game_imported_unclaimed_batch_member_identities_immutable
  on private.game_imported_unclaimed_batch_member_identities;
create trigger game_imported_unclaimed_batch_member_identities_immutable
before update or delete on private.game_imported_unclaimed_batch_member_identities
for each row execute function private.reject_game_imported_unclaimed_batch_member_identity_mutation();

revoke all on function private.reject_game_imported_unclaimed_batch_member_identity_mutation()
  from public, anon, authenticated, service_role;
