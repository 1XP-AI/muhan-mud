-- 2026-09-29: immutable membership from each newly inserted imported-unclaimed
-- character to the exact committed batch that created it. This deliberately
-- stores only durable identifiers; it never stores player data, credentials,
-- or source paths.

create table if not exists private.game_imported_unclaimed_batch_members (
  world_id text not null,
  stream_id text not null,
  batch_sequence bigint not null,
  character_id uuid not null,
  primary key (character_id),
  constraint game_imported_unclaimed_batch_members_batch_fkey
    foreign key (world_id, stream_id, batch_sequence)
    references private.game_imported_unclaimed_batches(world_id, stream_id, batch_sequence)
    on delete restrict,
  constraint game_imported_unclaimed_batch_members_character_fkey
    foreign key (character_id)
    references public.game_characters(id)
    on delete restrict
);

alter table private.game_imported_unclaimed_batch_members enable row level security;

revoke all on table private.game_imported_unclaimed_batch_members
  from public, anon, authenticated, service_role;

create or replace function private.reject_game_imported_unclaimed_batch_member_mutation()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'imported-unclaimed batch members are immutable';
end;
$$;

drop trigger if exists game_imported_unclaimed_batch_members_immutable
  on private.game_imported_unclaimed_batch_members;
create trigger game_imported_unclaimed_batch_members_immutable
before update or delete on private.game_imported_unclaimed_batch_members
for each row execute function private.reject_game_imported_unclaimed_batch_member_mutation();

revoke all on function private.reject_game_imported_unclaimed_batch_member_mutation()
  from public, anon, authenticated, service_role;
