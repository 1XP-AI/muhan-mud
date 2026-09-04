\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract for the M3 file-authoritative absent
-- first seed. C must have already proved file absence beneath its held root;
-- this SQL never reads, writes, or publishes a legacy file.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'm3 absent head seed contract failed: %', p_message;
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
    message = format('m3 absent head seed contract failed: expected %s, statement succeeded', p_sqlstate);
end;
$$;

-- RED boundary: migrations through 130 deliberately have no file-absence
-- seed RPC. The runnable integration harness applies 180 after this fails.
select pg_temp.assert_true(
  to_regprocedure('private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)') is not null,
  'absent-head seed RPC must exist'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
     where p.oid = 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)'::regprocedure
       and n.nspname = 'private'
       and p.proname = 'seed_game_character_absent_head'
       and p.prorettype = 'void'::regtype
       and p.prosecdef
       and p.proconfig = array['search_path=pg_catalog, private']::text[]
       and p.proargnames = array[
         'p_world_id', 'p_legacy_name_key', 'p_character_id',
         'p_writer_instance_id', 'p_writer_epoch', 'p_storage_format'
       ]::text[]
  ),
  'absent-head seed must have the fixed SECURITY DEFINER signature and search_path'
);

do $$
declare
  v_world constant text := 'm3-absent-seed';
  v_seed constant uuid := 'a9800000-0000-0000-0000-000000000001';
  v_imported constant uuid := 'a9800000-0000-0000-0000-000000000002';
  v_other constant uuid := 'a9800000-0000-0000-0000-000000000003';
  v_existing constant uuid := 'a9800000-0000-0000-0000-000000000004';
  v_advanced constant uuid := 'a9800000-0000-0000-0000-000000000005';
  v_different constant uuid := 'a9800000-0000-0000-0000-000000000006';
  v_malformed constant uuid := 'a9800000-0000-0000-0000-000000000007';
  v_sealed constant uuid := 'a9800000-0000-0000-0000-000000000008';
  v_successor constant uuid := 'a9800000-0000-0000-0000-000000000009';
  v_writer_a constant uuid := 'b9800000-0000-0000-0000-000000000001';
  v_writer_b constant uuid := 'b9800000-0000-0000-0000-000000000002';
  v_import_hash constant text := repeat('a', 64);
  v_head_before text;
  v_character_before text;
  v_receipt_before text;
  v_epoch bigint;
begin
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
    storage_format, imported_file_sha256
  ) values
    (v_seed, v_world, 'Seedone', 'Seedone', substr(encode(public.digest(convert_to('Seedone', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, null),
    (v_imported, v_world, 'Imported', 'Imported', substr(encode(public.digest(convert_to('Imported', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1, v_import_hash),
    (v_other, v_world, 'Seedother', 'Seedother', substr(encode(public.digest(convert_to('Seedother', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_existing, v_world, 'Existing', 'Existing', substr(encode(public.digest(convert_to('Existing', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_advanced, v_world, 'Advanced', 'Advanced', substr(encode(public.digest(convert_to('Advanced', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_different, v_world, 'Different', 'Different', substr(encode(public.digest(convert_to('Different', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_malformed, v_world, 'Malformed', 'Malformed', substr(encode(public.digest(convert_to('Malformed', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_sealed, v_world, 'Sealedone', 'Sealedone', substr(encode(public.digest(convert_to('Sealedone', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null),
    (v_successor, v_world, 'Successor', 'Successor', substr(encode(public.digest(convert_to('Successor', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'provisioning', 1, null);

  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision, writer_epoch
  ) values
    (v_existing, 'existing', repeat('b', 64), 1, 0, null),
    (v_advanced, 'absent', null, 1, 1, 1),
    (v_different, 'absent', null, 1, 0, 1),
    (v_malformed, 'uninitialized', null, null, 0, null);

  select writer_epoch into v_epoch from private.acquire_game_world_writer_epoch(
    v_world, v_writer_a, clock_timestamp() + interval '3 minutes'
  );
  perform pg_temp.assert_true(v_epoch = 1, 'test writer must acquire epoch one');

  perform private.seed_game_character_absent_head(
    v_world, 'Seedone', v_seed, v_writer_a, 1::bigint, 1::smallint
  );
  perform pg_temp.assert_true(
    (select h.head_state = 'absent' and h.head_sha256 is null
            and h.storage_format = 1 and h.revision = 0 and h.writer_epoch is null
       from private.game_character_legacy_heads h where h.character_id = v_seed),
    'a valid C-proved absence must seed only the exact revision-zero absent head'
  );
  perform pg_temp.assert_true(
    (select count(*) = 0 from private.game_character_shadow_receipts where character_id = v_seed),
    'a seed must not write a receipt'
  );
  v_head_before := (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_seed);
  v_character_before := (select jsonb_agg(to_jsonb(c) order by c.id)::text from public.game_characters c where c.world_id = v_world);
  v_receipt_before := (select coalesce(jsonb_agg(to_jsonb(r) order by r.character_id, r.command_id)::text, '[]') from private.game_character_shadow_receipts r);
  perform private.seed_game_character_absent_head(
    v_world, 'Seedone', v_seed, v_writer_a, 1::bigint, 1::smallint
  );
  perform pg_temp.assert_true(
    v_head_before = (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_seed)
    and v_character_before = (select jsonb_agg(to_jsonb(c) order by c.id)::text from public.game_characters c where c.world_id = v_world)
    and v_receipt_before = (select coalesce(jsonb_agg(to_jsonb(r) order by r.character_id, r.command_id)::text, '[]') from private.game_character_shadow_receipts r),
    'the exact absent baseline retry must be fully read-only'
  );

  v_head_before := (select coalesce(jsonb_agg(to_jsonb(h) order by h.character_id)::text, '[]') from private.game_character_legacy_heads h);
  v_character_before := (select jsonb_agg(to_jsonb(c) order by c.id)::text from public.game_characters c where c.world_id = v_world);
  v_receipt_before := (select coalesce(jsonb_agg(to_jsonb(r) order by r.character_id, r.command_id)::text, '[]') from private.game_character_shadow_receipts r);

  perform pg_temp.expect_state('22023', format(
    'select private.seed_game_character_absent_head(null::text, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    'Seedone', v_seed, v_writer_a));
  perform pg_temp.expect_state('22023', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'seedone', v_seed, v_writer_a));
  perform pg_temp.expect_state('22023', format(
    'select private.seed_game_character_absent_head(%L, %L, null::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Seedone', v_writer_a));
  perform pg_temp.expect_state('22023', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, null::uuid, 1::bigint, 1::smallint)',
    v_world, 'Seedone', v_seed));
  perform pg_temp.expect_state('22023', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 0::bigint, 1::smallint)',
    v_world, 'Seedone', v_seed, v_writer_a));
  perform pg_temp.expect_state('22023', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 0::smallint)',
    v_world, 'Seedone', v_seed, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Seedone', v_other, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Seedother', v_seed, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 2::smallint)',
    v_world, 'Seedother', v_other, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Imported', v_imported, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Existing', v_existing, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Advanced', v_advanced, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Different', v_different, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Malformed', v_malformed, v_writer_a));
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Sealedone', v_sealed, 'b9800000-0000-0000-0000-000000000099'));
  perform pg_temp.assert_true(
    v_head_before = (select coalesce(jsonb_agg(to_jsonb(h) order by h.character_id)::text, '[]') from private.game_character_legacy_heads h)
    and v_character_before = (select jsonb_agg(to_jsonb(c) order by c.id)::text from public.game_characters c where c.world_id = v_world)
    and v_receipt_before = (select coalesce(jsonb_agg(to_jsonb(r) order by r.character_id, r.command_id)::text, '[]') from private.game_character_shadow_receipts r),
    'invalid arguments, route/import conflicts, and all non-exact heads must not mutate head, character, or receipt rows'
  );

  update private.game_character_writer_epochs
     set issued_at = clock_timestamp() - interval '2 seconds',
         expires_at = clock_timestamp() - interval '1 second'
   where world_id = v_world;
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Sealedone', v_sealed, v_writer_a));
  perform private.renew_game_world_writer_epoch(
    v_world, v_writer_a, 1::bigint, clock_timestamp() + interval '3 minutes'
  );
  perform private.seal_game_world_writer_epoch(v_world, v_writer_a, 1::bigint);
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Sealedone', v_sealed, v_writer_a));
  update private.game_character_writer_epochs
     set issued_at = clock_timestamp() - interval '2 seconds',
         expires_at = clock_timestamp() - interval '1 second'
   where world_id = v_world;
  perform private.acquire_game_world_writer_epoch(
    v_world, v_writer_b, clock_timestamp() + interval '3 minutes'
  );
  perform pg_temp.expect_state('P0001', format(
    'select private.seed_game_character_absent_head(%L, %L, %L::uuid, %L::uuid, 1::bigint, 1::smallint)',
    v_world, 'Successor', v_successor, v_writer_a));
  perform pg_temp.assert_true(
    (select count(*) = 0 from private.game_character_legacy_heads where character_id in (v_sealed, v_successor))
    and v_character_before = (select jsonb_agg(to_jsonb(c) order by c.id)::text from public.game_characters c where c.world_id = v_world)
    and v_receipt_before = (select coalesce(jsonb_agg(to_jsonb(r) order by r.character_id, r.command_id)::text, '[]') from private.game_character_shadow_receipts r),
    'stale, expired, sealed, and successor-fenced tuples must leave character, receipt, and target heads unchanged'
  );
  perform private.seed_game_character_absent_head(
    v_world, 'Successor', v_successor, v_writer_b, 2::bigint, 1::smallint
  );
  perform pg_temp.assert_true(
    (select head_state = 'absent' and head_sha256 is null and storage_format = 1
            and revision = 0 and writer_epoch is null
       from private.game_character_legacy_heads where character_id = v_successor),
    'the exact live successor tuple may seed a separate missing head'
  );
end;
$$;

select pg_temp.assert_true(
  has_function_privilege('mud_writer', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute')
  and not has_function_privilege('anon', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute')
  and not has_function_privilege('authenticated', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute')
  and not has_function_privilege('service_role', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute')
  and not has_function_privilege('mud_writer_login', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute')
  and not has_table_privilege('mud_writer', 'public.game_characters', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_legacy_heads', 'select'),
  'only mud_writer may execute the seed RPC and it gains no direct identity or head access'
);

rollback;

-- Replay is part of this disposable contract: the migration creates no seed
-- rows and repeated application preserves its fixed function/privilege shape.
\ir ../migrations/20260918000000_m3_absent_head_seed.sql
\ir ../migrations/20260918000000_m3_absent_head_seed.sql

do $$
begin
  if to_regprocedure('private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)') is null
     or not has_function_privilege('mud_writer', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute')
     or has_function_privilege('anon', 'private.seed_game_character_absent_head(text,text,uuid,uuid,bigint,smallint)', 'execute') then
    raise exception 'm3 absent head seed contract failed: migration replay changed the seed RPC authority';
  end if;
end;
$$;
