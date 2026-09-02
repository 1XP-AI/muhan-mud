\set ON_ERROR_STOP on

-- Disposable PG17 contract for the additive route binding used by M3 v2.
-- It returns identity and storage metadata only; it never reads or writes
-- player bytes, credentials, sessions, or receipt state.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'm3 shadow route v2 contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_state(p_sqlstate text, p_sql text)
returns void language plpgsql as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then return; end if;
    raise;
  end;
  raise exception using errcode = 'P0002',
    message = format('m3 shadow route v2 contract failed: expected %s, statement succeeded', p_sqlstate);
end;
$$;

-- RED boundary: bootstrap+020..090 has no v2 route yet.
select pg_temp.assert_true(
  to_regprocedure('private.resolve_game_character_writer_route_v2(text,text)') is not null,
  'v2 route function must exist'
);

select pg_temp.assert_true(
  exists (
    select 1
     from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
     where p.oid = 'private.resolve_game_character_writer_route_v2(text,text)'::regprocedure
       and n.nspname = 'private'
       and p.proname = 'resolve_game_character_writer_route_v2'
       and p.prorettype = 'record'::regtype
       and p.prosecdef
       and p.proconfig = array['search_path=pg_catalog, private']::text[]
       and p.proargnames = array[
         'p_world_id', 'p_legacy_name_key',
         'world_id', 'character_id', 'legacy_name_key', 'legacy_shard',
         'storage_format', 'lifecycle', 'imported_file_sha256'
       ]::text[]
       and p.proargmodes = array['i', 'i', 't', 't', 't', 't', 't', 't', 't']::"char"[]
       and p.proallargtypes = array[
         'text'::regtype::oid,
         'text'::regtype::oid,
         'text'::regtype::oid,
         'uuid'::regtype::oid,
         'text'::regtype::oid,
         'bpchar'::regtype::oid,
         'int2'::regtype::oid,
         'public.character_lifecycle'::regtype::oid,
         'text'::regtype::oid
       ]::oid[]
  ),
  'v2 route must be SECURITY DEFINER with the exact ordered TABLE signature and fixed search_path'
);

do $$
declare
  v_world constant text := 'm3-v2-contract';
  v_character constant uuid := 'a9100000-0000-0000-0000-000000000001';
  v_provisioning constant uuid := 'a9100000-0000-0000-0000-000000000002';
  v_active constant uuid := 'a9100000-0000-0000-0000-000000000003';
  v_wrong_format constant uuid := 'a9100000-0000-0000-0000-000000000004';
  v_suspended constant uuid := 'a9100000-0000-0000-0000-000000000005';
  v_actor constant uuid := 'a9200000-0000-0000-0000-000000000001';
  v_hash constant text := '1111111111111111111111111111111111111111111111111111111111111111';
  v_active_hash constant text := '2222222222222222222222222222222222222222222222222222222222222222';
  v_route record;
  v_json jsonb;
  v_keys text[];
  v_identity_before text;
  v_receipts_before text;
  v_expected_shard text;
  v_count bigint;
begin
  insert into auth.users(id, email)
  values (v_actor, 'm3-v2-contract@example.invalid');

  v_expected_shard := substr(
    encode(public.digest(convert_to('V2hero', 'UTF8'), 'sha1'), 'hex'), 1, 2
  );
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard,
    lifecycle, storage_format, imported_file_sha256
  ) values (
    v_character, v_world, 'V2hero', 'V2hero', v_expected_shard,
    'imported_unclaimed', 1, v_hash
  );

  v_expected_shard := substr(
    encode(public.digest(convert_to('V2provision', 'UTF8'), 'sha1'), 'hex'), 1, 2
  );
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard,
    lifecycle, storage_format, imported_file_sha256
  ) values (
    v_provisioning, v_world, 'V2provision', 'V2provision', v_expected_shard,
    'provisioning', 1, null
  );

  v_expected_shard := substr(
    encode(public.digest(convert_to('V2active', 'UTF8'), 'sha1'), 'hex'), 1, 2
  );
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard,
    owner_user_id, lifecycle, storage_format, imported_file_sha256, claimed_at
  ) values (
    v_active, v_world, 'V2active', 'V2active', v_expected_shard,
    v_actor, 'active', 1, v_active_hash, clock_timestamp()
  );

  v_expected_shard := substr(
    encode(public.digest(convert_to('V2format', 'UTF8'), 'sha1'), 'hex'), 1, 2
  );
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard,
    lifecycle, storage_format, imported_file_sha256
  ) values (
    v_wrong_format, v_world, 'V2format', 'V2format', v_expected_shard,
    'imported_unclaimed', 2, null
  );

  v_expected_shard := substr(
    encode(public.digest(convert_to('V2suspended', 'UTF8'), 'sha1'), 'hex'), 1, 2
  );
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard,
    lifecycle, storage_format, imported_file_sha256
  ) values (
    v_suspended, v_world, 'V2suspended', 'V2suspended', v_expected_shard,
    'suspended', 1, null
  );

  v_identity_before := (
    select jsonb_agg(to_jsonb(c) order by c.id)::text
      from public.game_characters c
     where c.id in (
       v_character, v_provisioning, v_active, v_wrong_format, v_suspended
     )
  );
  v_receipts_before := (
    select coalesce(
      jsonb_agg(to_jsonb(r) order by r.character_id::text, r.command_id::text),
      '[]'::jsonb
    )::text
    from private.game_character_shadow_receipts r
    where r.character_id in (v_character, v_provisioning, v_active, v_wrong_format, v_suspended)
  );

  select * into v_route
    from private.resolve_game_character_writer_route_v2(v_world, 'V2hero');
  v_json := to_jsonb(v_route);
  select array_agg(key order by key) into v_keys from jsonb_object_keys(v_json) as key;
  perform pg_temp.assert_true(
    v_keys = array[
      'character_id', 'imported_file_sha256', 'legacy_name_key', 'legacy_shard',
      'lifecycle', 'storage_format', 'world_id'
    ],
    'result JSON key set must contain exactly the seven route fields'
  );
  perform pg_temp.assert_true(
    v_json = jsonb_build_object(
      'world_id', v_world,
      'character_id', v_character,
      'legacy_name_key', 'V2hero',
      'legacy_shard', substr(encode(public.digest(convert_to('V2hero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
      'storage_format', 1,
      'lifecycle', 'imported_unclaimed',
      'imported_file_sha256', v_hash
    ),
    'route must echo authoritative identity, shard, format, lifecycle, and imported hash'
  );
  perform pg_temp.assert_true(v_route.world_id = v_world, 'world id must be echoed from the row');
  perform pg_temp.assert_true(v_route.character_id = v_character, 'character UUID must be DB authoritative');
  perform pg_temp.assert_true(v_route.legacy_name_key = 'V2hero', 'canonical name key must be DB authoritative');
  perform pg_temp.assert_true(
    v_route.legacy_shard = substr(encode(public.digest(convert_to('V2hero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'shard must be the stored SHA-1-derived two-byte value'
  );
  perform pg_temp.assert_true(v_route.storage_format = 1, 'storage format must be exact legacy-c-abi-v1');
  perform pg_temp.assert_true(v_route.lifecycle = 'imported_unclaimed', 'lifecycle must be DB authoritative');
  perform pg_temp.assert_true(v_route.imported_file_sha256 = v_hash, 'imported SHA-256 must be returned exactly');

  select * into v_route
    from private.resolve_game_character_writer_route_v2(v_world, 'V2provision');
  perform pg_temp.assert_true(
    v_route.lifecycle = 'provisioning' and v_route.imported_file_sha256 is null,
    'provisioning route must be eligible and preserve a nullable imported hash'
  );
  select * into v_route
    from private.resolve_game_character_writer_route_v2(v_world, 'V2active');
  perform pg_temp.assert_true(
    v_route.lifecycle = 'active' and v_route.imported_file_sha256 = v_active_hash,
    'active route must be eligible and preserve its exact imported hash'
  );
  select count(*) into v_count
    from private.resolve_game_character_writer_route_v2(v_world, 'V2hero');
  perform pg_temp.assert_true(v_count = 1, 'a valid unique route must return exactly one row');

  perform pg_temp.expect_state('22023',
    'select * from private.resolve_game_character_writer_route_v2(null::text, ''V2hero'')');
  perform pg_temp.expect_state('22023',
    format('select * from private.resolve_game_character_writer_route_v2(%L, ''V2hero'')', 'M3-v2-contract'));
  perform pg_temp.expect_state('22023',
    format('select * from private.resolve_game_character_writer_route_v2(%L, ''V2hero'')', '1m3-v2-contract'));
  perform pg_temp.expect_state('22023',
    format('select * from private.resolve_game_character_writer_route_v2(%L, null::text)', v_world));
  perform pg_temp.expect_state('22023',
    format('select * from private.resolve_game_character_writer_route_v2(%L, %L)', v_world, 'v2Hero'));
  perform pg_temp.expect_state('22023',
    format('select * from private.resolve_game_character_writer_route_v2(%L, %L)', v_world, 'V2/Hero'));
  perform pg_temp.expect_state('22023',
    format('select * from private.resolve_game_character_writer_route_v2(%L, %L)', v_world, ''));

  perform pg_temp.expect_state('P0001',
    format('select * from private.resolve_game_character_writer_route_v2(%L, %L)', v_world, 'V2missing'));
  perform pg_temp.expect_state('P0001',
    format('select * from private.resolve_game_character_writer_route_v2(%L, %L)', v_world, 'V2format'));
  perform pg_temp.expect_state('P0001',
    format('select * from private.resolve_game_character_writer_route_v2(%L, %L)', v_world, 'V2suspended'));

  perform pg_temp.assert_true(
    v_identity_before = (
      select jsonb_agg(to_jsonb(c) order by c.id)::text
        from public.game_characters c
       where c.id in (
         v_character, v_provisioning, v_active, v_wrong_format, v_suspended
       )
    ),
    'route lookup and rejection paths must not mutate any fixture identity row'
  );
  perform pg_temp.assert_true(
    v_receipts_before = (
      select coalesce(
        jsonb_agg(to_jsonb(r) order by r.character_id::text, r.command_id::text),
        '[]'::jsonb
      )::text
      from private.game_character_shadow_receipts r
      where r.character_id in (v_character, v_provisioning, v_active, v_wrong_format, v_suspended)
    ),
    'route lookup must not mutate receipt rows'
  );
end;
$$;

select pg_temp.assert_true(
  has_function_privilege('mud_writer', 'private.resolve_game_character_writer_route_v2(text,text)', 'execute')
  and not has_function_privilege('anon', 'private.resolve_game_character_writer_route_v2(text,text)', 'execute')
  and not has_function_privilege('authenticated', 'private.resolve_game_character_writer_route_v2(text,text)', 'execute')
  and not has_function_privilege('service_role', 'private.resolve_game_character_writer_route_v2(text,text)', 'execute')
  and not exists (
    select 1
      from pg_proc p,
           lateral aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) acl
     where p.oid = 'private.resolve_game_character_writer_route_v2(text,text)'::regprocedure
       and acl.grantee = 0
       and acl.privilege_type = 'EXECUTE'
  )
  and not has_table_privilege('mud_writer', 'public.game_characters', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_shadow_receipts', 'select'),
  'only mud_writer may execute the route and it must have no table SELECT privilege'
);

rollback;
