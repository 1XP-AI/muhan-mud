-- 2026-09-12: additive M3 head-aware save route.
--
-- v2 remains the compatibility route.  This v3 lookup binds the one fenced
-- writer to the same authoritative route plus a read-only effective head.

create or replace function private.resolve_game_character_writer_route_v3(
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
  imported_file_sha256 text,
  head_state text,
  head_sha256 text,
  head_revision bigint
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

  -- The character row and any private head are read and locked in one
  -- statement/snapshot.  FOR SHARE blocks a concurrently fenced receipt from
  -- advancing an existing head until this lookup has returned, while never
  -- inserting an absent/uninitialized fallback row.
  begin
    select
      c.world_id, c.id, c.legacy_name_key, c.legacy_shard, c.storage_format,
      c.lifecycle, c.imported_file_sha256,
      h.head_state, h.head_sha256, h.revision as head_revision,
      h.storage_format as head_storage_format
      into strict v_route
      from public.game_characters c
      left join lateral (
        select x.head_state, x.head_sha256, x.revision, x.storage_format
          from private.game_character_legacy_heads x
         where x.character_id = c.id
         for share
      ) h on true
     where c.world_id = p_world_id
       and c.legacy_name_key = p_legacy_name_key
       and c.lifecycle in ('imported_unclaimed', 'provisioning', 'active')
       and c.storage_format = 1
     for share of c;
  exception
    when no_data_found or too_many_rows then
      raise exception using errcode = 'P0001', message = 'writer route is not eligible';
  end;

  if v_route.head_state is not null then
    if v_route.head_state not in ('uninitialized', 'absent', 'existing')
       or v_route.head_revision < 0
       or (v_route.head_state in ('existing', 'absent') and
           v_route.head_storage_format is distinct from v_route.storage_format)
       or (v_route.head_state = 'existing' and
           (v_route.head_sha256 is null or v_route.head_sha256 !~ '^[0-9a-f]{64}$'))
       or (v_route.head_state = 'absent' and v_route.head_sha256 is not null)
       or (v_route.head_state = 'uninitialized' and
           (v_route.head_sha256 is not null or v_route.head_storage_format is not null
            or v_route.head_revision <> 0)) then
      raise exception using errcode = 'P0001', message = 'writer head is not eligible';
    end if;

    return query select
      v_route.world_id::text,
      v_route.id::uuid,
      v_route.legacy_name_key::text,
      v_route.legacy_shard::char(2),
      v_route.storage_format::smallint,
      v_route.lifecycle::public.character_lifecycle,
      v_route.imported_file_sha256::text,
      v_route.head_state::text,
      v_route.head_sha256::text,
      v_route.head_revision::bigint;
    return;
  end if;

  if v_route.imported_file_sha256 is not null then
    return query select
      v_route.world_id::text,
      v_route.id::uuid,
      v_route.legacy_name_key::text,
      v_route.legacy_shard::char(2),
      v_route.storage_format::smallint,
      v_route.lifecycle::public.character_lifecycle,
      v_route.imported_file_sha256::text,
      'existing'::text,
      v_route.imported_file_sha256::text,
      0::bigint;
    return;
  end if;

  return query select
    v_route.world_id::text,
    v_route.id::uuid,
    v_route.legacy_name_key::text,
    v_route.legacy_shard::char(2),
    v_route.storage_format::smallint,
    v_route.lifecycle::public.character_lifecycle,
    v_route.imported_file_sha256::text,
    'uninitialized'::text,
    null::text,
    0::bigint;
end;
$$;

revoke all on function private.resolve_game_character_writer_route_v3(text, text)
  from public, anon, authenticated, service_role, mud_writer;
grant execute on function private.resolve_game_character_writer_route_v3(text, text)
  to mud_writer;
revoke all on table public.game_characters from mud_writer;
revoke all on table private.game_character_legacy_heads from mud_writer;

comment on function private.resolve_game_character_writer_route_v3(text, text) is
  'mud_writer only. Resolves one canonical world/name route and a consistent read-only effective legacy head; it never creates or mutates head, identity, lifecycle, or receipts.';
