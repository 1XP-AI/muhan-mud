\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract. The integration lane applies the next
-- migration after 20261005000000, then reruns this contract for GREEN.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'PlayerSnapshotNormalizedV1 projection persistence contract failed: %', p_message;
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
  raise exception using errcode = 'P0002', message = format('expected SQLSTATE %s', p_sqlstate);
end;
$$;

select pg_temp.assert_true(
  to_regprocedure(
    'private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb)'
  ) is not null,
  'the exact normalized-projection recorder signature must exist'
);

select pg_temp.assert_true(
  (select relrowsecurity from pg_class
    where oid = 'private.game_character_player_snapshot_normalized_v1_projections'::regclass)
  and (select relrowsecurity from pg_class
    where oid = 'private.game_character_player_snapshot_normalized_v1_projection_daily'::regclass)
  and (select relrowsecurity from pg_class
    where oid = 'private.game_character_player_snapshot_normalized_v1_projection_timers'::regclass)
  and (select relrowsecurity from pg_class
    where oid = 'private.game_character_player_snapshot_normalized_v1_projection_items'::regclass)
  and not exists (
    select 1 from pg_attribute
     where attrelid in (
       'private.game_character_player_snapshot_normalized_v1_projections'::regclass,
       'private.game_character_player_snapshot_normalized_v1_projection_daily'::regclass,
       'private.game_character_player_snapshot_normalized_v1_projection_timers'::regclass,
       'private.game_character_player_snapshot_normalized_v1_projection_items'::regclass
     ) and atttypid = 'bytea'::regtype and attnum > 0 and not attisdropped
  ),
  'all projection relations are private/RLS and store no raw artifact bytes'
);

select pg_temp.assert_true(
  (select prosecdef and proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc where oid =
       'private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb)'::regprocedure),
  'the writer recorder is a pinned SECURITY DEFINER function'
);

\if :{?pvi_tree_payload}
\else
  \echo pvi_tree_payload is required
  do $$ begin raise exception 'pvi_tree_payload is required' using errcode = '22023'; end $$;
\endif

select pg_temp.assert_true(
  private.player_snapshot_v1_payload_valid(:pvi_tree_payload),
  'the checked-in PlayerSnapshotV1 fixture remains valid immutable artifact evidence'
);

-- This literal is emitted by Rust PlayerSnapshotNormalizedV1::canonical_digest
-- for exactly the numeric-only projection below; Node independently verifies
-- the same vector. SQL receives only the safe projection, never artifact text
-- or bytes, and validates that canonical digest contract itself.
select jsonb_build_object(
  'format', 'player-snapshot-v1-normalized-projection',
  'version', 1,
  'algorithm', 'sha-256',
  'canonical_digest', 'ccbc64069c88e10f35089d0bbda2278112911b4294f44caf32fc101f41c3ff80',
  'player', jsonb_build_object(
    'level', 42, 'hp_max', 100, 'hp_current', 99, 'mp_max', 50, 'mp_current', 49,
    'experience', 9223372036854775807::bigint, 'gold', (-9223372036854775807::bigint - 1),
    'daily', (select jsonb_agg(jsonb_build_object(
      'max', 255, 'current', 255, 'last_used', 9223372036854775807::bigint
    ) order by i) from generate_series(1, 10) as series(i)),
    'timers', (select jsonb_agg(jsonb_build_object(
      'interval', (-9223372036854775807::bigint - 1), 'last_used', 0, 'misc', -32768
    ) order by i) from generate_series(1, 45) as series(i)),
    'items', jsonb_build_array(jsonb_build_object(
      'parent_index', null, 'child_index', 0, 'value', 9223372036854775807::bigint,
      'weight', -32768, 'type_code', -128, 'adjustment', 127, 'shots_max', 2,
      'shots_current', 2, 'ndice', -32768, 'sdice', 32767, 'pdice', 0, 'armor', -128,
      'wear_flag', 127, 'magic_power', 0, 'magic_realm', -1, 'special', 32767
    ))
  )
) as projection \gset pnp_

select jsonb_set(
  jsonb_set(:'pnp_projection'::jsonb, '{player,level}', '41'::jsonb),
  '{canonical_digest}', '"19204eefbf6ea2c4d420c46abf3d9a5d6ccc7c432e437d1cc480c891eb17591c"'::jsonb
) as projection \gset pnp_conflict_

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format
) values (
  'a9060000-0000-0000-0000-000000000001', 'pnp-projection', 'Pnphero', 'Pnphero',
  substr(encode(public.digest(convert_to('Pnphero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1
);
insert into private.game_character_legacy_heads(character_id, head_state, storage_format, revision)
values ('a9060000-0000-0000-0000-000000000001', 'absent', 1, 0);
select * from private.acquire_game_world_writer_epoch(
  'pnp-projection', 'b9060000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);
select private.game_character_shadow_request_sha256(
  'pnp-projection', 'a9060000-0000-0000-0000-000000000001'::uuid, 'Pnphero',
  substr(encode(public.digest(convert_to('Pnphero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9060000-0000-0000-0000-000000000001'::uuid,
  'b9060000-0000-0000-0000-000000000001'::uuid, 1::bigint, 1::bigint,
  'absent', null::text, repeat('a', 64), 1::smallint
) as request_sha256 \gset pnp_
select private.record_legacy_published_receipt(
  'pnp-projection', 'Pnphero', 'a9060000-0000-0000-0000-000000000001'::uuid,
  'c9060000-0000-0000-0000-000000000001'::uuid,
  'b9060000-0000-0000-0000-000000000001'::uuid, :'pnp_request_sha256',
  1::bigint, 1::bigint, 'absent', null::text, repeat('a', 64), 1::smallint
);
select octet_length(:pvi_tree_payload) as snapshot_octets,
  encode(public.digest(:pvi_tree_payload, 'sha256'), 'hex') as snapshot_sha256,
  encode(:pvi_tree_payload, 'hex') as snapshot_payload_hex
\gset pnp_

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(
  (select outcome = 'RECORDED' from private.record_m4_file_snapshot_manifest_for_receipt(
    'a9060000-0000-0000-0000-000000000001'::uuid,
    'c9060000-0000-0000-0000-000000000001'::uuid,
    :'pnp_request_sha256', 'legacy-file-manifest-v1', repeat('a', 64), 9)),
  'the acknowledged M4 source manifest records before normalized projection evidence'
);
select pg_temp.assert_true(
  (select outcome = 'RECORDED' from private.record_player_snapshot_v1_artifact_for_receipt(
    'a9060000-0000-0000-0000-000000000001'::uuid,
    'c9060000-0000-0000-0000-000000000001'::uuid,
    :'pnp_request_sha256', repeat('a', 64), 9, 'player-snapshot-v1',
    :'pnp_snapshot_sha256', :'pnp_snapshot_octets', decode(:'pnp_snapshot_payload_hex', 'hex'))),
  'the immutable receipt/source-bound artifact records before the safe projection'
);

select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_normalized_v1_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L::jsonb)',
  'a9060000-0000-0000-0000-000000000001', 'c9060000-0000-0000-0000-000000000001',
  :'pnp_request_sha256', repeat('a', 64),
  jsonb_set(:'pnp_projection'::jsonb, '{player,name}', '"must-not-cross-boundary"'::jsonb)::text
));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_normalized_v1_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L::jsonb)',
  'a9060000-0000-0000-0000-000000000001', 'c9060000-0000-0000-0000-000000000001',
  :'pnp_request_sha256', repeat('a', 64),
  jsonb_set(:'pnp_projection'::jsonb, '{player,hp_current}', '101'::jsonb)::text
));

select pg_temp.assert_true(
  (select outcome = 'RECORDED'
     from private.record_player_snapshot_normalized_v1_projection_for_receipt(
       'a9060000-0000-0000-0000-000000000001'::uuid,
       'c9060000-0000-0000-0000-000000000001'::uuid,
       :'pnp_request_sha256', repeat('a', 64), 9, :'pnp_projection'::jsonb)),
  'the writer records exactly the reviewed numeric projection after receipt and artifact metadata binding'
);
select pg_temp.assert_true(
  (select outcome = 'EXACT_RETRY'
     from private.record_player_snapshot_normalized_v1_projection_for_receipt(
       'a9060000-0000-0000-0000-000000000001'::uuid,
       'c9060000-0000-0000-0000-000000000001'::uuid,
       :'pnp_request_sha256', repeat('a', 64), 9, :'pnp_projection'::jsonb)),
  'an exact normalized-projection retry is idempotent'
);
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_normalized_v1_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L::jsonb)',
  'a9060000-0000-0000-0000-000000000001', 'c9060000-0000-0000-0000-000000000001',
  :'pnp_request_sha256', repeat('a', 64), :'pnp_conflict_projection'
));
reset role;
reset session authorization;

select pg_temp.assert_true(
  (select level = 42 and hp_current = 99 and item_count = 1
     and canonical_digest = 'ccbc64069c88e10f35089d0bbda2278112911b4294f44caf32fc101f41c3ff80'
     and snapshot_sha256 = :'pnp_snapshot_sha256' and snapshot_octets = :'pnp_snapshot_octets'
     from private.game_character_player_snapshot_normalized_v1_projections
    where character_id = 'a9060000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9060000-0000-0000-0000-000000000001'::uuid)
  and (select count(*) = 10 from private.game_character_player_snapshot_normalized_v1_projection_daily)
  and (select count(*) = 45 from private.game_character_player_snapshot_normalized_v1_projection_timers)
  and (select count(*) = 1 from private.game_character_player_snapshot_normalized_v1_projection_items),
  'only format metadata and reviewed numeric scalar, daily, timer, and inventory values persist'
);

select pg_temp.expect_state('P0001', $$
  update private.game_character_player_snapshot_normalized_v1_projections set level = 1;
$$);
select pg_temp.expect_state('P0001', $$
  update private.game_character_player_snapshot_normalized_v1_projection_items set value = 1;
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_character_player_snapshot_normalized_v1_projection_daily;
$$);

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('42501', $$
  select * from private.game_character_player_snapshot_normalized_v1_projections;
$$);
select pg_temp.expect_state('42501', $$
  insert into private.game_character_player_snapshot_normalized_v1_projections(character_id, command_id)
  values ('a9060000-0000-0000-0000-000000000001', 'c9060000-0000-0000-0000-000000000001');
$$);
reset role;
reset session authorization;

select pg_temp.assert_true(
  has_function_privilege('mud_writer',
    'private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb)', 'execute')
  and not has_function_privilege('service_role',
    'private.record_player_snapshot_normalized_v1_projection_for_receipt(uuid,uuid,text,text,bigint,jsonb)', 'execute')
  and not has_table_privilege('service_role',
    'private.game_character_player_snapshot_normalized_v1_projections', 'select,insert,update,delete'),
  'only the writer role can execute the recorder and direct table access is denied'
);
set local session authorization service_role;
select pg_temp.expect_state('42501', format(
  'select * from private.record_player_snapshot_normalized_v1_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L::jsonb)',
  'a9060000-0000-0000-0000-000000000001', 'c9060000-0000-0000-0000-000000000001',
  :'pnp_request_sha256', repeat('a', 64), :'pnp_projection'
));
select pg_temp.expect_state('42501', $$
  select * from private.game_character_player_snapshot_normalized_v1_projections;
$$);
reset session authorization;

-- A privileged repair can bypass row triggers. A changed immutable row must
-- fail closed rather than becoming a false exact retry.
set local session_replication_role = replica;
update private.game_character_player_snapshot_normalized_v1_projections
   set canonical_digest = repeat('0', 64)
 where character_id = 'a9060000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9060000-0000-0000-0000-000000000001'::uuid;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_normalized_v1_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L::jsonb)',
  'a9060000-0000-0000-0000-000000000001', 'c9060000-0000-0000-0000-000000000001',
  :'pnp_request_sha256', repeat('a', 64), :'pnp_projection'
));
reset role;
reset session authorization;

rollback;
