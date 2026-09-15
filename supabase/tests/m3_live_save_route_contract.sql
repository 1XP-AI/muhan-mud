\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract for the additive, head-aware M3 save
-- route.  Run after bootstrap and migrations through 110, then again after
-- applying 20260912000000_m3_live_save_route.sql.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'm3 live save route contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_state(p_sqlstate text, p_sql text)
returns void language plpgsql as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then
      return;
    end if;
    raise;
  end;
  raise exception using errcode = 'P0002',
    message = format('m3 live save route contract failed: expected %s, statement succeeded', p_sqlstate);
end;
$$;

-- RED boundary: migrations through 110 intentionally do not have this v3
-- lookup.  This assertion is the expected failure before the new migration.
select pg_temp.assert_true(
  to_regprocedure('private.resolve_game_character_writer_route_v3(text,text)') is not null,
  'v3 head-aware save route function must exist'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
     where p.oid = 'private.resolve_game_character_writer_route_v3(text,text)'::regprocedure
       and n.nspname = 'private'
       and p.proname = 'resolve_game_character_writer_route_v3'
       and p.prorettype = 'record'::regtype
       and p.prosecdef
       and p.proconfig = array['search_path=pg_catalog, private']::text[]
       and p.proargnames = array[
         'p_world_id', 'p_legacy_name_key',
         'world_id', 'character_id', 'legacy_name_key', 'legacy_shard',
         'storage_format', 'lifecycle', 'imported_file_sha256',
         'head_state', 'head_sha256', 'head_revision'
       ]::text[]
       and p.proargmodes = array['i', 'i', 't', 't', 't', 't', 't', 't', 't', 't', 't', 't']::"char"[]
       and p.proallargtypes = array[
         'text'::regtype::oid, 'text'::regtype::oid,
         'text'::regtype::oid, 'uuid'::regtype::oid, 'text'::regtype::oid,
         'bpchar'::regtype::oid, 'int2'::regtype::oid,
         'public.character_lifecycle'::regtype::oid, 'text'::regtype::oid,
         'text'::regtype::oid, 'text'::regtype::oid, 'int8'::regtype::oid
       ]::oid[]
  ),
  'v3 route must be SECURITY DEFINER with the exact ordered TABLE signature and fixed search_path'
);

-- v3 is additive; the v2 interface must remain exactly where it was.
select pg_temp.assert_true(
  to_regprocedure('private.resolve_game_character_writer_route_v2(text,text)') is not null
  and (select p.proargnames from pg_proc p
        where p.oid = 'private.resolve_game_character_writer_route_v2(text,text)'::regprocedure) = array[
          'p_world_id', 'p_legacy_name_key',
          'world_id', 'character_id', 'legacy_name_key', 'legacy_shard',
          'storage_format', 'lifecycle', 'imported_file_sha256'
        ]::text[],
  'v2 route must remain unchanged'
);

do $$
declare
  v_world constant text := 'm3-v3-contract';
  v_existing constant uuid := 'a9300000-0000-0000-0000-000000000001';
  v_absent constant uuid := 'a9300000-0000-0000-0000-000000000002';
  v_imported constant uuid := 'a9300000-0000-0000-0000-000000000003';
  v_uninitialized constant uuid := 'a9300000-0000-0000-0000-000000000004';
  v_bad_shape constant uuid := 'a9300000-0000-0000-0000-000000000005';
  v_private_uninitialized constant uuid := 'a9300000-0000-0000-0000-000000000006';
  v_head_hash constant text := 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
  v_import_hash constant text := 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
  v_identity_before text;
  v_heads_before text;
  v_route record;
  v_keys text[];
begin
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
    storage_format, imported_file_sha256
  ) values
    (v_existing, v_world, 'V3existing', 'V3existing', substr(encode(public.digest(convert_to('V3existing', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, v_import_hash),
    (v_absent, v_world, 'V3absent', 'V3absent', substr(encode(public.digest(convert_to('V3absent', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_imported, v_world, 'V3imported', 'V3imported', substr(encode(public.digest(convert_to('V3imported', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, v_import_hash),
    (v_uninitialized, v_world, 'V3new', 'V3new', substr(encode(public.digest(convert_to('V3new', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, null),
    (v_bad_shape, v_world, 'V3bad', 'V3bad', substr(encode(public.digest(convert_to('V3bad', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, null),
    (v_private_uninitialized, v_world, 'V3private', 'V3private', substr(encode(public.digest(convert_to('V3private', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, null);

  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision
  ) values
    (v_existing, 'existing', v_head_hash, 1, 7),
    (v_absent, 'absent', null, 1, 4),
    -- This is shape-valid at the table level but inconsistent with the route.
    (v_bad_shape, 'absent', null, 2, 0),
    (v_private_uninitialized, 'uninitialized', null, null, 0);

  v_identity_before := (select jsonb_agg(to_jsonb(c) order by c.id)::text
                          from public.game_characters c where c.world_id = v_world);
  v_heads_before := (select jsonb_agg(to_jsonb(h) order by h.character_id)::text
                       from private.game_character_legacy_heads h
                      where h.character_id in (v_existing, v_absent, v_imported, v_uninitialized, v_bad_shape, v_private_uninitialized));

  select * into v_route from private.resolve_game_character_writer_route_v3(v_world, 'V3existing');
  select array_agg(key order by key) into v_keys from jsonb_object_keys(to_jsonb(v_route)) as key;
  perform pg_temp.assert_true(v_keys = array[
    'character_id', 'head_revision', 'head_sha256', 'head_state',
    'imported_file_sha256', 'legacy_name_key', 'legacy_shard', 'lifecycle',
    'storage_format', 'world_id'
  ], 'v3 result must have exactly the v2 and head fields');
  perform pg_temp.assert_true(
    to_jsonb(v_route) = jsonb_build_object(
      'world_id', v_world, 'character_id', v_existing,
      'legacy_name_key', 'V3existing',
      'legacy_shard', substr(encode(public.digest(convert_to('V3existing', 'UTF8'), 'sha1'), 'hex'), 1, 2),
      'storage_format', 1, 'lifecycle', 'imported_unclaimed',
      'imported_file_sha256', v_import_hash,
      'head_state', 'existing', 'head_sha256', v_head_hash, 'head_revision', 7
    ), 'an existing private head must be returned exactly');

  select * into v_route from private.resolve_game_character_writer_route_v3(v_world, 'V3absent');
  perform pg_temp.assert_true(
    v_route.head_state = 'absent' and v_route.head_sha256 is null and v_route.head_revision = 4,
    'an absent private head must be returned exactly');

  select * into v_route from private.resolve_game_character_writer_route_v3(v_world, 'V3imported');
  perform pg_temp.assert_true(
    v_route.head_state = 'existing' and v_route.head_sha256 = v_import_hash and v_route.head_revision = 0,
    'a missing private head must fall back to the imported hash at revision zero');

  select * into v_route from private.resolve_game_character_writer_route_v3(v_world, 'V3new');
  perform pg_temp.assert_true(
    v_route.head_state = 'uninitialized' and v_route.head_sha256 is null and v_route.head_revision = 0,
    'a missing private head and imported hash must be explicitly uninitialized');

  select * into v_route from private.resolve_game_character_writer_route_v3(v_world, 'V3private');
  perform pg_temp.assert_true(
    v_route.head_state = 'uninitialized' and v_route.head_sha256 is null and v_route.head_revision = 0,
    'an explicit uninitialized private head must be returned exactly');

  perform pg_temp.expect_state('P0001', format(
    'select * from private.resolve_game_character_writer_route_v3(%L, %L)', v_world, 'V3bad'));
  perform pg_temp.expect_state('22023',
    'select * from private.resolve_game_character_writer_route_v3(null::text, ''V3existing'')');
  perform pg_temp.expect_state('22023', format(
    'select * from private.resolve_game_character_writer_route_v3(%L, %L)', v_world, 'v3existing'));
  perform pg_temp.expect_state('P0001', format(
    'select * from private.resolve_game_character_writer_route_v3(%L, %L)', v_world, 'V3missing'));

  perform pg_temp.assert_true(v_identity_before = (
    select jsonb_agg(to_jsonb(c) order by c.id)::text from public.game_characters c where c.world_id = v_world
  ), 'v3 lookups and rejections must not mutate identity');
  perform pg_temp.assert_true(v_heads_before = (
    select jsonb_agg(to_jsonb(h) order by h.character_id)::text
      from private.game_character_legacy_heads h
     where h.character_id in (v_existing, v_absent, v_imported, v_uninitialized, v_bad_shape, v_private_uninitialized)
  ), 'v3 lookups and rejections must not mutate heads or create fallback rows');
end;
$$;

select pg_temp.assert_true(
  has_function_privilege('mud_writer', 'private.resolve_game_character_writer_route_v3(text,text)', 'execute')
  and not has_function_privilege('anon', 'private.resolve_game_character_writer_route_v3(text,text)', 'execute')
  and not has_function_privilege('authenticated', 'private.resolve_game_character_writer_route_v3(text,text)', 'execute')
  and not has_function_privilege('service_role', 'private.resolve_game_character_writer_route_v3(text,text)', 'execute')
  and not has_table_privilege('mud_writer', 'public.game_characters', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_legacy_heads', 'select'),
  'only mud_writer may execute v3 and it must receive no direct table SELECT'
);

rollback;
