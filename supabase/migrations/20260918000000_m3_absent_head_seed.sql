-- 2026-09-18: additive M3 absent-head first seed.
--
-- C may call this only after it has confirmed that the canonical legacy file
-- is absent beneath its held trusted root.  The database neither reads nor
-- writes legacy files, and this RPC is not a publish authorization.

create or replace function private.seed_game_character_absent_head(
  p_world_id text,
  p_legacy_name_key text,
  p_character_id uuid,
  p_writer_instance_id uuid,
  p_writer_epoch bigint,
  p_storage_format smallint
)
returns void
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_route public.game_characters%rowtype;
  v_epoch private.game_character_writer_epochs%rowtype;
  v_head private.game_character_legacy_heads%rowtype;
  v_now timestamptz;
begin
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_legacy_name_key is null
     or char_length(p_legacy_name_key) not between 1 and 12
     or octet_length(p_legacy_name_key) > 14
     or p_legacy_name_key ~ E'[[:cntrl:]/\\\\:]'
     or p_legacy_name_key in ('.', '..')
     or p_legacy_name_key <> private.game_identity_canonical_legacy_name(p_legacy_name_key)
     or p_character_id is null
     or p_writer_instance_id is null
     or p_writer_epoch is null
     or p_writer_epoch not between 1 and 9223372036854775807
     or p_storage_format is null
     or p_storage_format <= 0 then
    raise exception using errcode = '22023', message = 'absent head seed arguments are invalid';
  end if;

  -- Keep the receipt contract's lock order: world fence, route, writer,
  -- then per-character head.  This serializes a seed with any receipt CAS.
  perform pg_advisory_xact_lock(hashtextextended(p_world_id, 900090));
  select * into v_route
    from public.game_characters
   where world_id = p_world_id
     and legacy_name_key = p_legacy_name_key
   for share;
  if not found
     or v_route.id <> p_character_id
     or v_route.lifecycle not in ('imported_unclaimed', 'provisioning', 'active')
     or v_route.storage_format <> p_storage_format then
    raise exception using errcode = 'P0001', message = 'absent head seed route is not eligible';
  end if;

  select * into v_epoch
    from private.game_character_writer_epochs
   where world_id = p_world_id
   for update;
  select * into v_head
    from private.game_character_legacy_heads
   where character_id = p_character_id
   for update;
  v_now := clock_timestamp();
  if v_epoch.world_id is null
     or v_epoch.writer_instance_id <> p_writer_instance_id
     or v_epoch.writer_epoch <> p_writer_epoch
     or v_epoch.sealed_at is not null
     or v_epoch.expires_at <= v_now then
    raise exception using errcode = 'P0001', message = 'absent head seed writer epoch is stale or fenced';
  end if;

  -- Imported evidence already fixes an existing-file route.  It is never
  -- reinterpreted as permission to claim an absent file.
  if v_route.imported_file_sha256 is not null then
    raise exception using errcode = 'P0001', message = 'absent head seed conflicts with imported file evidence';
  end if;

  if v_head.character_id is not null then
    if v_head.head_state = 'absent'
       and v_head.head_sha256 is null
       and v_head.storage_format = p_storage_format
       and v_head.revision = 0
       and v_head.writer_epoch is null then
      -- Exact retry: preserve updated_at and leave every row unchanged.
      return;
    end if;
    raise exception using errcode = 'P0001', message = 'absent head seed conflicts with existing head';
  end if;

  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision, writer_epoch, updated_at
  ) values (
    p_character_id, 'absent', null, p_storage_format, 0, null, v_now
  );
end;
$$;

revoke all on function private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)
  from public, anon, authenticated, service_role, mud_writer_login, mud_writer;
grant execute on function private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)
  to mud_writer;

comment on function private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint) is
  'mud_writer only. C calls only after it has proved the canonical file absent beneath its held trusted root. Revalidates route and active writer tuple, inserts only a revision-zero absent head, and never reads, writes, or publishes a legacy file.';
