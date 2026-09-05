-- 2026-10-02: service-only lookup of a character already bound to immutable
-- imported-batch locator evidence. This is intentionally a read-only identity
-- resolver: it neither authorizes nor changes a character lifecycle.

create or replace function public.resolve_game_imported_legacy_locator(
  p_world_id text,
  p_canonical_legacy_name text,
  p_legacy_name_sha1 text,
  p_legacy_shard char(2)
)
returns table (character_id uuid)
language plpgsql
security definer
set search_path = pg_catalog, private, public
as $$
declare
  v_character_id uuid;
begin
  if p_world_id is null
     or char_length(p_world_id) not between 1 and 64
     or p_world_id ~ '[[:cntrl:]]'
     or p_canonical_legacy_name is null
     or p_canonical_legacy_name <> private.game_identity_canonical_legacy_name(p_canonical_legacy_name)
     or p_legacy_name_sha1 is null
     or p_legacy_name_sha1 !~ '^[0-9a-f]{40}$'
     or p_legacy_name_sha1 <> encode(digest(convert_to(p_canonical_legacy_name, 'UTF8'), 'sha1'), 'hex')
     or p_legacy_shard is null
     or p_legacy_shard !~ '^[0-9a-f]{2}$'
     or p_legacy_shard <> substr(p_legacy_name_sha1, 1, 2) then
    raise exception using errcode = '22023',
      message = 'legacy locator arguments must be one exact canonical identity tuple';
  end if;

  select character.id
    into v_character_id
    from private.game_imported_unclaimed_batch_member_legacy_locators as locator
    join private.game_imported_unclaimed_batch_members as member
      on member.character_id = locator.character_id
    join public.game_characters as character
      on character.id = member.character_id
   where member.world_id = p_world_id
     and locator.canonical_legacy_name = p_canonical_legacy_name
     and locator.legacy_name_sha1 = p_legacy_name_sha1
     and locator.legacy_shard = p_legacy_shard
     and character.world_id = p_world_id
     and character.legacy_name = p_canonical_legacy_name
     and character.legacy_name_key = p_canonical_legacy_name
     and character.legacy_shard = p_legacy_shard;

  if not found then
    raise exception using errcode = 'P0001',
      message = 'legacy locator is unavailable';
  end if;

  return query select v_character_id;
end;
$$;

revoke all on function public.resolve_game_imported_legacy_locator(text, text, text, char(2))
  from public, anon, authenticated, service_role;
grant execute on function public.resolve_game_imported_legacy_locator(text, text, text, char(2))
  to service_role;

comment on function public.resolve_game_imported_legacy_locator(text, text, text, char(2)) is
  'Gateway service_role only. Reads one exact canonical world/name/SHA-1/shard locator tuple through immutable importer member evidence and character identity; returns only character_id and performs no mutation.';
