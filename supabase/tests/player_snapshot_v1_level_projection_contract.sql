\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract for the non-authoritative raw-U8 level
-- projection. The integration harness runs this before migration 190 for RED,
-- then applies migration 190 twice and reruns it for GREEN.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'PlayerSnapshotV1 level projection contract failed: %', p_message;
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
    message = format('expected SQLSTATE %s', p_sqlstate);
end;
$$;

select pg_temp.assert_true(
  to_regprocedure(
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)'
  ) is not null,
  'the exact level-projection RPC signature must exist'
);

select pg_temp.assert_true(
  (select c.relrowsecurity
     from pg_class c
    where c.oid = 'private.game_character_player_snapshot_v1_level_projections'::regclass)
  and not exists (
    select 1
      from pg_attribute a
     where a.attrelid = 'private.game_character_player_snapshot_v1_level_projections'::regclass
       and a.atttypid = 'bytea'::regtype
       and a.attnum > 0
       and not a.attisdropped
  )
  and (select count(*) = 1
         from pg_constraint
        where conrelid = 'private.game_character_player_snapshot_v1_level_projections'::regclass
          and contype = 'p')
  and (select count(*) = 1
         from pg_constraint
        where conrelid = 'private.game_character_player_snapshot_v1_level_projections'::regclass
          and contype = 'u'),
  'the projection is private/RLS, payload-free, and unique by receipt key plus revision'
);

select pg_temp.assert_true(
  (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p
    where p.oid =
      'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)'::regprocedure),
  'the projection RPC is SECURITY DEFINER with a pinned search path'
);

\if :{?pvl_payload}
\else
  \echo pvl_payload is required
  do $$ begin raise exception 'pvl_payload is required' using errcode = '22023'; end $$;
\endif

select pg_temp.assert_true(
  private.player_snapshot_v1_payload_valid(:pvl_payload),
  'the checked-in C PlayerSnapshotV1 fixture must remain valid evidence'
);

-- Make field 7 nonzero without changing any non-digest byte except its
-- zero-based value offset. The terminal CDTO digest covers bytes 16 through
-- total-33, so reseal it after changing byte 365. This prevents the level
-- projection assertion from passing when an unrelated fixture byte happens
-- to equal the level.
create temporary table pvl_level_payload (
  payload bytea not null
);
insert into pvl_level_payload (payload)
with changed as (
  select set_byte(:pvl_payload, 365, 42) as payload
)
select overlay(
  payload placing public.digest(
    substring(payload from 17 for octet_length(payload) - 48), 'sha256'
  ) from octet_length(payload) - 31
)
from changed;

select pg_temp.assert_true(
  (select private.player_snapshot_v1_payload_valid(payload)
        and get_byte(payload, 365) = 42
        and get_byte(payload, 527) <> 42
        and substring(payload from 1 for octet_length(payload) - 32) =
            set_byte(
              substring(:pvl_payload from 1 for octet_length(:pvl_payload) - 32),
              365, 42
            )
     from pvl_level_payload),
  'the level-42 payload changes only field 7 byte 365 before resealing its terminal digest'
);

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format
) values (
  'a9900000-0000-0000-0000-000000000001', 'pvl-projection', 'Pvlhero', 'Pvlhero',
  substr(encode(public.digest(convert_to('Pvlhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1
);

insert into private.game_character_legacy_heads(character_id, head_state, storage_format, revision)
values ('a9900000-0000-0000-0000-000000000001', 'absent', 1, 0);

select * from private.acquire_game_world_writer_epoch(
  'pvl-projection', 'b9900000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);

select private.game_character_shadow_request_sha256(
  'pvl-projection', 'a9900000-0000-0000-0000-000000000001'::uuid, 'Pvlhero',
  substr(encode(public.digest(convert_to('Pvlhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9900000-0000-0000-0000-000000000001'::uuid,
  'b9900000-0000-0000-0000-000000000001'::uuid, 1::bigint, 1::bigint,
  'absent', null::text, repeat('a', 64), 1::smallint
) as request_sha256 \gset pvl_first_

select private.record_legacy_published_receipt(
  'pvl-projection', 'Pvlhero', 'a9900000-0000-0000-0000-000000000001'::uuid,
  'c9900000-0000-0000-0000-000000000001'::uuid,
  'b9900000-0000-0000-0000-000000000001'::uuid, :'pvl_first_request_sha256',
  1::bigint, 1::bigint, 'absent', null::text, repeat('a', 64), 1::smallint
);

select octet_length(payload) as snapshot_octets,
  encode(public.digest(payload, 'sha256'), 'hex') as snapshot_sha256,
  encode(payload, 'hex') as snapshot_payload_hex
from pvl_level_payload
\gset pvl_

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(private.m3_assert_writer_session(),
  'projection setup uses the actual writer login plus SET ROLE');
select pg_temp.assert_true(
  (select outcome = 'RECORDED'
     from private.record_m4_file_snapshot_manifest_for_receipt(
       'a9900000-0000-0000-0000-000000000001'::uuid,
       'c9900000-0000-0000-0000-000000000001'::uuid,
       :'pvl_first_request_sha256', 'legacy-file-manifest-v1', repeat('a', 64), 9)),
  'the source-octet receipt manifest records before projection'
);
select pg_temp.assert_true(
  (select outcome = 'RECORDED'
     from private.record_player_snapshot_v1_artifact_for_receipt(
       'a9900000-0000-0000-0000-000000000001'::uuid,
       'c9900000-0000-0000-0000-000000000001'::uuid,
       :'pvl_first_request_sha256', repeat('a', 64), 9, 'player-snapshot-v1',
       :'pvl_snapshot_sha256', :'pvl_snapshot_octets',
       decode(:'pvl_snapshot_payload_hex', 'hex'))),
  'the validated receipt/source-octet-bound artifact records before projection'
);

select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_level_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9900000-0000-0000-0000-000000000001', 'c9900000-0000-0000-0000-000000000099',
  :'pvl_first_request_sha256', repeat('a', 64)));
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_level_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,10)',
  'a9900000-0000-0000-0000-000000000001', 'c9900000-0000-0000-0000-000000000001',
  :'pvl_first_request_sha256', repeat('a', 64)));
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_level_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9900000-0000-0000-0000-000000000001', 'c9900000-0000-0000-0000-000000000001',
  repeat('b', 64), repeat('a', 64)));

select pg_temp.assert_true(
  (select outcome = 'RECORDED'
     from private.record_player_snapshot_v1_level_projection_for_receipt(
       'a9900000-0000-0000-0000-000000000001'::uuid,
       'c9900000-0000-0000-0000-000000000001'::uuid,
       :'pvl_first_request_sha256', repeat('a', 64), 9)),
  'the projection is atomically derived from the already recorded artifact'
);
select pg_temp.assert_true(
  (select outcome = 'EXACT_RETRY'
     from private.record_player_snapshot_v1_level_projection_for_receipt(
       'a9900000-0000-0000-0000-000000000001'::uuid,
       'c9900000-0000-0000-0000-000000000001'::uuid,
       :'pvl_first_request_sha256', repeat('a', 64), 9)),
  'an exact projection retry is idempotent'
);

reset role;
reset session authorization;

select pg_temp.assert_true(
  (select raw_level_u8 = 42
       and raw_level_u8 between 0 and 255
       and receipt_request_sha256 = :'pvl_first_request_sha256'
       and source_post_sha256 = repeat('a', 64)
       and source_octets = 9
     from private.game_character_player_snapshot_v1_level_projections
    where character_id = 'a9900000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9900000-0000-0000-0000-000000000001'::uuid),
  'the persisted value is the level-42 field 7 raw U8 at zero-based offset 365, with no gameplay policy transformation'
);

select pg_temp.expect_state('P0001', $$
  update private.game_character_player_snapshot_v1_level_projections set raw_level_u8 = 1;
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_character_player_snapshot_v1_level_projections;
$$);

-- A privileged repair tool could bypass triggers. A conflicting stored row
-- must still fail closed rather than turning a retry into a silent overwrite.
set local session_replication_role = replica;
update private.game_character_player_snapshot_v1_level_projections set raw_level_u8 = 1;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_level_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9900000-0000-0000-0000-000000000001', 'c9900000-0000-0000-0000-000000000001',
  :'pvl_first_request_sha256', repeat('a', 64)));
reset role;
reset session authorization;

-- A different key using the same character revision is likewise a conflict,
-- even if an owner-side repair path has bypassed its foreign-key trigger.
set local session_replication_role = replica;
delete from private.game_character_player_snapshot_v1_level_projections
 where character_id = 'a9900000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9900000-0000-0000-0000-000000000001'::uuid;
insert into private.game_character_player_snapshot_v1_level_projections (
  character_id, command_id, receipt_request_sha256,
  writer_instance_id, writer_epoch, writer_revision,
  source_post_sha256, source_octets, snapshot_sha256, snapshot_octets,
  raw_level_u8
) values (
  'a9900000-0000-0000-0000-000000000001', 'c9900000-0000-0000-0000-000000000099',
  :'pvl_first_request_sha256', 'b9900000-0000-0000-0000-000000000001', 1, 1,
  repeat('a', 64), 9, :'pvl_snapshot_sha256', :'pvl_snapshot_octets', 0
);
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_level_projection_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9900000-0000-0000-0000-000000000001', 'c9900000-0000-0000-0000-000000000001',
  :'pvl_first_request_sha256', repeat('a', 64)));
reset role;
reset session authorization;

select pg_temp.assert_true(
  has_function_privilege('mud_writer',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_function_privilege('mud_writer_login',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_function_privilege('anon',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_function_privilege('authenticated',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_function_privilege('service_role',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_function_privilege('mud_replay_reader_login',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_table_privilege('mud_writer',
    'private.game_character_player_snapshot_v1_level_projections', 'select,insert,update,delete')
  and not has_table_privilege('service_role',
    'private.game_character_player_snapshot_v1_level_projections', 'select,insert,update,delete')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'select,insert,update,delete'),
  'least privilege prevents direct projection mutation or browser/service payload paths'
);

rollback;
