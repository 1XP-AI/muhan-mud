-- 2026-10-01: immutable locator evidence for a member already admitted to an
-- imported-unclaimed batch. This is deliberately separate from membership so
-- the S5a membership relation and claim gate retain their exact contract.

create table if not exists private.game_imported_unclaimed_batch_member_legacy_locators (
  character_id uuid primary key,
  canonical_legacy_name text not null,
  legacy_name_sha1 text not null,
  legacy_shard char(2) not null,
  constraint game_imported_unclaimed_batch_member_legacy_locators_member_fkey
    foreign key (character_id)
    references private.game_imported_unclaimed_batch_members(character_id)
    on delete restrict,
  constraint game_imported_unclaimed_batch_member_legacy_locators_name_c_safe
    check (
      char_length(canonical_legacy_name) between 1 and 12
      and octet_length(canonical_legacy_name) <= 14
      and canonical_legacy_name !~ E'[[:cntrl:]/\\\\:]'
      and canonical_legacy_name = private.game_identity_canonical_legacy_name(canonical_legacy_name)
      and canonical_legacy_name not in ('.', '..')
    ),
  constraint game_imported_unclaimed_batch_member_legacy_locators_sha1_matches_name
    check (
      legacy_name_sha1 ~ '^[0-9a-f]{40}$'
      and legacy_name_sha1 = encode(digest(convert_to(canonical_legacy_name, 'UTF8'), 'sha1'), 'hex')
    ),
  constraint game_imported_unclaimed_batch_member_legacy_locators_shard_matches_sha1
    check (legacy_shard = substr(legacy_name_sha1, 1, 2))
);

alter table private.game_imported_unclaimed_batch_member_legacy_locators enable row level security;

revoke all on table private.game_imported_unclaimed_batch_member_legacy_locators
  from public, anon, authenticated, service_role;

-- A locator can only be appended while its parent is the exact freshly
-- imported row. Once present, later ownership/lifecycle transitions never
-- rewrite it and imported_file_sha256 remains solely on game_characters.
create or replace function private.require_game_imported_unclaimed_batch_member_legacy_locator_parent()
returns trigger
language plpgsql
set search_path = pg_catalog, private, public
as $$
begin
  if not exists (
    select 1
      from private.game_imported_unclaimed_batch_members as member
      join public.game_characters as character on character.id = member.character_id
     where member.character_id = new.character_id
       and character.legacy_name_key = new.canonical_legacy_name
       and character.legacy_shard = new.legacy_shard
       and character.lifecycle = 'imported_unclaimed'
       and character.owner_user_id is null
       and character.storage_format = 1
       and character.imported_file_sha256 is not null
  ) then
    raise exception using errcode = 'P0001',
      message = 'imported-unclaimed batch member locator parent is invalid';
  end if;
  return new;
end;
$$;

create or replace function private.reject_game_imported_unclaimed_batch_member_legacy_locator_mutation()
returns trigger
language plpgsql
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'imported-unclaimed batch member legacy locators are immutable';
end;
$$;

drop trigger if exists game_imported_unclaimed_batch_member_legacy_locators_parent
  on private.game_imported_unclaimed_batch_member_legacy_locators;
create trigger game_imported_unclaimed_batch_member_legacy_locators_parent
before insert on private.game_imported_unclaimed_batch_member_legacy_locators
for each row execute function private.require_game_imported_unclaimed_batch_member_legacy_locator_parent();

drop trigger if exists game_imported_unclaimed_batch_member_legacy_locators_immutable
  on private.game_imported_unclaimed_batch_member_legacy_locators;
create trigger game_imported_unclaimed_batch_member_legacy_locators_immutable
before update or delete on private.game_imported_unclaimed_batch_member_legacy_locators
for each row execute function private.reject_game_imported_unclaimed_batch_member_legacy_locator_mutation();

revoke all on function private.require_game_imported_unclaimed_batch_member_legacy_locator_parent()
  from public, anon, authenticated, service_role;
revoke all on function private.reject_game_imported_unclaimed_batch_member_legacy_locator_mutation()
  from public, anon, authenticated, service_role;
