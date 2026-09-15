-- 2026-09-10: additive M3 v2 route binding.
-- The route exposes only DB identity/storage metadata and never mutates
-- identity, lifecycle, ownership, or shadow receipt state.

create or replace function private.resolve_game_character_writer_route_v2(
  p_world_id text,
  p_legacy_name_key text
)
returns table (
  world_id text,
  character_id uuid,
  legacy_name_key text,
  legacy_shard char(2),
  storage_format smallint,
  lifecycle public.character_lifecycle,
  imported_file_sha256 text
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_route record;
begin
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id) then
    raise exception using errcode = '22023', message = 'writer route world is invalid';
  end if;

  if p_legacy_name_key is null
     or char_length(p_legacy_name_key) not between 1 and 12
     or octet_length(p_legacy_name_key) > 14
     or p_legacy_name_key ~ E'[[:cntrl:]/\\\\:]'
     or p_legacy_name_key in ('.', '..')
     or p_legacy_name_key <> private.game_identity_canonical_legacy_name(p_legacy_name_key) then
    raise exception using errcode = '22023', message = 'writer route name is not canonical';
  end if;

  begin
    select
      c.world_id,
      c.id,
      c.legacy_name_key,
      c.legacy_shard,
      c.storage_format,
      c.lifecycle,
      c.imported_file_sha256
      into strict v_route
      from public.game_characters c
     where c.world_id = p_world_id
       and c.legacy_name_key = p_legacy_name_key
       and c.lifecycle in ('imported_unclaimed', 'provisioning', 'active')
       and c.storage_format = 1
     for share;
  exception
    when no_data_found or too_many_rows then
      raise exception using errcode = 'P0001', message = 'writer route is not eligible';
  end;

  return query select
    v_route.world_id::text,
    v_route.id::uuid,
    v_route.legacy_name_key::text,
    v_route.legacy_shard::char(2),
    v_route.storage_format::smallint,
    v_route.lifecycle::public.character_lifecycle,
    v_route.imported_file_sha256::text;
end;
$$;

revoke all on function private.resolve_game_character_writer_route_v2(text, text)
  from public, anon, authenticated, service_role, mud_writer;
grant execute on function private.resolve_game_character_writer_route_v2(text, text)
  to mud_writer;
revoke all on table public.game_characters from mud_writer;

comment on function private.resolve_game_character_writer_route_v2(text, text) is
  'mud_writer only. Resolves one canonical world/name route to authoritative character identity and legacy storage metadata; it does not mutate identity or receipts.';
