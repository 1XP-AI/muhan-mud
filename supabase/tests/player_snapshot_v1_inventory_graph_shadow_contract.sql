\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract. The integration lane applies the
-- migration after this RED boundary, then reruns the contract for GREEN.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'PlayerSnapshotV1 inventory graph shadow contract failed: %', p_message;
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
    'private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)'
  ) is not null
  and to_regprocedure(
    'private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)'
  ) is not null,
  'exact recorder and reconciliation signatures must exist'
);

select pg_temp.assert_true(
  (select relrowsecurity from pg_class
    where oid = 'private.game_character_player_snapshot_v1_inventory_graph_shadows'::regclass)
  and (select relrowsecurity from pg_class
    where oid = 'private.game_character_player_snapshot_v1_inventory_graph_shadow_items'::regclass)
  and not exists (
    select 1 from pg_attribute
     where attrelid in (
       'private.game_character_player_snapshot_v1_inventory_graph_shadows'::regclass,
       'private.game_character_player_snapshot_v1_inventory_graph_shadow_items'::regclass
     ) and atttypid = 'bytea'::regtype and attnum > 0 and not attisdropped
  ),
  'both private relations are RLS-protected and contain no copied payload'
);

select pg_temp.assert_true(
  (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p where p.oid =
       'private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)'::regprocedure)
  and (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p where p.oid =
       'private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)'::regprocedure),
  'writer-only RPCs are security definer with pinned search paths'
);

\if :{?pvi_tree_payload}
\else
  \echo pvi_tree_payload is required
  do $$ begin raise exception 'pvi_tree_payload is required' using errcode = '22023'; end $$;
\endif

select pg_temp.assert_true(
  private.player_snapshot_v1_payload_valid(:pvi_tree_payload),
  'the checked-in nested-inventory fixture must remain valid artifact evidence'
);

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format
) values (
  'a9030000-0000-0000-0000-000000000001', 'pvi-shadow', 'Pvihero', 'Pvihero',
  substr(encode(public.digest(convert_to('Pvihero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1
);
insert into private.game_character_legacy_heads(character_id, head_state, storage_format, revision)
values ('a9030000-0000-0000-0000-000000000001', 'absent', 1, 0);

select * from private.acquire_game_world_writer_epoch(
  'pvi-shadow', 'b9030000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);
select private.game_character_shadow_request_sha256(
  'pvi-shadow', 'a9030000-0000-0000-0000-000000000001'::uuid, 'Pvihero',
  substr(encode(public.digest(convert_to('Pvihero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9030000-0000-0000-0000-000000000001'::uuid,
  'b9030000-0000-0000-0000-000000000001'::uuid, 1::bigint, 1::bigint,
  'absent', null::text, repeat('a', 64), 1::smallint
) as request_sha256 \gset pvi_
select private.record_legacy_published_receipt(
  'pvi-shadow', 'Pvihero', 'a9030000-0000-0000-0000-000000000001'::uuid,
  'c9030000-0000-0000-0000-000000000001'::uuid,
  'b9030000-0000-0000-0000-000000000001'::uuid, :'pvi_request_sha256',
  1::bigint, 1::bigint, 'absent', null::text, repeat('a', 64), 1::smallint
);
select octet_length(:pvi_tree_payload) as snapshot_octets,
  encode(public.digest(:pvi_tree_payload, 'sha256'), 'hex') as snapshot_sha256,
  encode(:pvi_tree_payload, 'hex') as snapshot_payload_hex
\gset pvi_

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(private.m3_assert_writer_session(),
  'setup uses the actual writer login plus SET ROLE');
select pg_temp.assert_true(
  (select outcome = 'RECORDED' from private.record_m4_file_snapshot_manifest_for_receipt(
    'a9030000-0000-0000-0000-000000000001'::uuid,
    'c9030000-0000-0000-0000-000000000001'::uuid,
    :'pvi_request_sha256', 'legacy-file-manifest-v1', repeat('a', 64), 9)),
  'the acknowledged source manifest records before its receipt-bound artifact'
);
select pg_temp.assert_true(
  (select outcome = 'RECORDED' from private.record_player_snapshot_v1_artifact_for_receipt(
    'a9030000-0000-0000-0000-000000000001'::uuid,
    'c9030000-0000-0000-0000-000000000001'::uuid,
    :'pvi_request_sha256', repeat('a', 64), 9, 'player-snapshot-v1',
    :'pvi_snapshot_sha256', :'pvi_snapshot_octets', decode(:'pvi_snapshot_payload_hex', 'hex'))),
  'the immutable receipt/source-octet-bound artifact records before the graph shadow'
);
reset role;
reset session authorization;

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(
  (select count(*) = 1 and min(shadow_state) = 'MISSING' and max(shadow_state) = 'MISSING'
     from private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('pvi-shadow', 10)
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
      and receipt_request_sha256 = :'pvi_request_sha256')
  and not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadows
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ),
  'a valid receipt -> M4 manifest -> PlayerSnapshotV1 artifact chain with no graph shadow row is returned exactly as MISSING'
);
reset role;
reset session authorization;

-- Start each negative case with the production-valid immutable chain, then
-- emulate an owner-side repair defect without permanently changing its setup.
savepoint pvi_missing_manifest;
set local session_replication_role = replica;
delete from private.game_character_m4_file_snapshot_manifests
 where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000001',
  :'pvi_request_sha256', repeat('a', 64)));
select pg_temp.assert_true(
  (select count(*) = 1
     from private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('pvi-shadow', 10)
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
      and receipt_request_sha256 = :'pvi_request_sha256'
      and shadow_state = 'INCONSISTENT'),
  'reconciliation retains the same receipt as INCONSISTENT when M4 manifest evidence is missing'
);
reset role;
reset session authorization;
select pg_temp.assert_true(
  not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadows
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ) and not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ),
  'a missing M4 manifest rejects with P0001 and leaves no graph shadow rows'
);
rollback to savepoint pvi_missing_manifest;
release savepoint pvi_missing_manifest;

savepoint pvi_mismatched_manifest;
set local session_replication_role = replica;
update private.game_character_m4_file_snapshot_manifests
   set snapshot_octets = 10
 where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000001',
  :'pvi_request_sha256', repeat('a', 64)));
select pg_temp.assert_true(
  (select count(*) = 1
     from private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('pvi-shadow', 10)
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
      and receipt_request_sha256 = :'pvi_request_sha256'
      and shadow_state = 'INCONSISTENT'),
  'reconciliation retains the same receipt as INCONSISTENT when M4 manifest evidence is mismatched'
);
reset role;
reset session authorization;
select pg_temp.assert_true(
  not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadows
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ) and not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ),
  'a mismatched M4 manifest rejects with P0001 and leaves no graph shadow rows'
);
rollback to savepoint pvi_mismatched_manifest;
release savepoint pvi_mismatched_manifest;

savepoint pvi_missing_artifact;
set local session_replication_role = replica;
delete from private.game_character_player_snapshot_v1_artifacts
 where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000001',
  :'pvi_request_sha256', repeat('a', 64)));
select pg_temp.assert_true(
  (select count(*) = 1
     from private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('pvi-shadow', 10)
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
      and receipt_request_sha256 = :'pvi_request_sha256'
      and shadow_state = 'INCONSISTENT'),
  'reconciliation retains the same receipt as INCONSISTENT when artifact evidence is missing'
);
reset role;
reset session authorization;
select pg_temp.assert_true(
  not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadows
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ) and not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ),
  'a missing artifact rejects with P0001 and leaves no graph shadow rows'
);
rollback to savepoint pvi_missing_artifact;
release savepoint pvi_missing_artifact;

savepoint pvi_mismatched_artifact;
set local session_replication_role = replica;
update private.game_character_player_snapshot_v1_artifacts
   set source_octets = 10
 where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000001',
  :'pvi_request_sha256', repeat('a', 64)));
select pg_temp.assert_true(
  (select count(*) = 1
     from private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('pvi-shadow', 10)
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
      and receipt_request_sha256 = :'pvi_request_sha256'
      and shadow_state = 'INCONSISTENT'),
  'reconciliation retains the same receipt as INCONSISTENT when artifact evidence is mismatched'
);
reset role;
reset session authorization;
select pg_temp.assert_true(
  not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadows
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ) and not exists (
    select 1 from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
     where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
       and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid
  ),
  'a mismatched artifact rejects with P0001 and leaves no graph shadow rows'
);
rollback to savepoint pvi_mismatched_artifact;
release savepoint pvi_mismatched_artifact;

set local session authorization mud_writer_login;
set local role mud_writer;

select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000099',
  :'pvi_request_sha256', repeat('a', 64)));
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,10)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000001',
  :'pvi_request_sha256', repeat('a', 64)));

select pg_temp.assert_true(
  (select outcome = 'RECORDED'
     from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(
       'a9030000-0000-0000-0000-000000000001'::uuid,
       'c9030000-0000-0000-0000-000000000001'::uuid,
       :'pvi_request_sha256', repeat('a', 64), 9)),
  'the graph topology is atomically derived from immutable artifact evidence'
);
select pg_temp.assert_true(
  (select outcome = 'EXACT_RETRY'
     from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(
       'a9030000-0000-0000-0000-000000000001'::uuid,
       'c9030000-0000-0000-0000-000000000001'::uuid,
       :'pvi_request_sha256', repeat('a', 64), 9)),
  'an exact graph-shadow retry is idempotent'
);
select pg_temp.assert_true(
  (select shadow_state = 'EXACT' and item_count = 5
     from private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation('pvi-shadow', 10)
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid),
  'writer reconciliation reports an exact five-node immutable graph shadow'
);

reset role;
reset session authorization;

select pg_temp.assert_true(
  (select item_count = 5 and receipt_request_sha256 = :'pvi_request_sha256'
     and source_post_sha256 = repeat('a', 64) and source_octets = 9
     from private.game_character_player_snapshot_v1_inventory_graph_shadows
    where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
      and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid)
  and (select string_agg(node_index || ':' || coalesce(parent_node_index::text, 'root') || ':' || sibling_ordinal, ',' order by node_index)
         = '0:root:0,1:0:0,2:1:0,3:0:1,4:root:1'
       from private.game_character_player_snapshot_v1_inventory_graph_shadow_items
      where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
        and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid),
  'only canonical node index, parent index, and sibling ordinal are stored from the nested fixture'
);

select pg_temp.expect_state('P0001', $$
  update private.game_character_player_snapshot_v1_inventory_graph_shadows set item_count = 1;
$$);
select pg_temp.expect_state('P0001', $$
  update private.game_character_player_snapshot_v1_inventory_graph_shadow_items set sibling_ordinal = 9;
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_character_player_snapshot_v1_inventory_graph_shadow_items;
$$);

-- A privileged repair path can bypass immutability. A conflicting existing
-- shadow must still fail closed rather than becoming a silent retry.
set local session_replication_role = replica;
update private.game_character_player_snapshot_v1_inventory_graph_shadows
   set item_count = 4
 where character_id = 'a9030000-0000-0000-0000-000000000001'::uuid
   and command_id = 'c9030000-0000-0000-0000-000000000001'::uuid;
set local session_replication_role = origin;
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9)',
  'a9030000-0000-0000-0000-000000000001', 'c9030000-0000-0000-0000-000000000001',
  :'pvi_request_sha256', repeat('a', 64)));
reset role;
reset session authorization;

select pg_temp.assert_true(
  not has_function_privilege('service_role',
    'private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and not has_function_privilege('service_role',
    'private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)', 'execute')
  and has_function_privilege('mud_writer',
    'private.record_player_snapshot_v1_inventory_graph_shadow_for_receipt(uuid,uuid,text,text,bigint)', 'execute')
  and has_function_privilege('mud_writer',
    'private.list_player_snapshot_v1_inventory_graph_shadow_reconciliation(text,integer)', 'execute'),
  'recorder and reconciliation are executable only by mud_writer'
);

rollback;
