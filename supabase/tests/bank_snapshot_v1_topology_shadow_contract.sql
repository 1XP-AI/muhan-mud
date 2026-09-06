\set ON_ERROR_STOP on
-- PG17 disposable contract: apply through 20261005000000, then run.
begin;
create or replace function pg_temp.assert_true(v boolean, m text) returns void language plpgsql as $$ begin if v is not true then raise exception '%',m; end if; end $$;
create or replace function pg_temp.expect_state(expected text, statement text) returns void language plpgsql as $$ begin begin execute statement; exception when others then if sqlstate=expected then return; end if; raise; end; raise exception using errcode='P0002',message=format('expected SQLSTATE %s',expected); end $$;

select pg_temp.assert_true(to_regprocedure('private.record_bank_snapshot_v1_topology_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,jsonb)') is not null,'bank topology recorder signature exists');
select pg_temp.assert_true((select relrowsecurity from pg_class where oid='private.game_character_bank_snapshot_v1_topology_shadows'::regclass) and (select relrowsecurity from pg_class where oid='private.game_character_bank_snapshot_v1_topology_shadow_items'::regclass),'bank topology relations are private RLS evidence');
select pg_temp.assert_true(not exists(select 1 from pg_attribute where attrelid in ('private.game_character_bank_snapshot_v1_topology_shadows'::regclass,'private.game_character_bank_snapshot_v1_topology_shadow_items'::regclass) and atttypid='bytea'::regtype and attnum>0 and not attisdropped),'bank topology shadow stores no bank payload');
select pg_temp.assert_true((select prosecdef and proconfig=array['search_path=pg_catalog, private']::text[] from pg_proc where oid='private.record_bank_snapshot_v1_topology_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,jsonb)'::regprocedure),'bank topology writer is a pinned security definer');

insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values ('a9050000-0000-0000-0000-000000000001','bank-shadow','Bankhero','Bankhero',substr(encode(public.digest(convert_to('Bankhero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1);
insert into private.game_character_legacy_heads(character_id,head_state,storage_format,revision) values ('a9050000-0000-0000-0000-000000000001','absent',1,0);
select * from private.acquire_game_world_writer_epoch('bank-shadow','b9050000-0000-0000-0000-000000000001'::uuid,clock_timestamp()+interval '3 minutes');
select private.game_character_shadow_request_sha256('bank-shadow','a9050000-0000-0000-0000-000000000001'::uuid,'Bankhero',substr(encode(public.digest(convert_to('Bankhero','UTF8'),'sha1'),'hex'),1,2),'c9050000-0000-0000-0000-000000000001'::uuid,'b9050000-0000-0000-0000-000000000001'::uuid,1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint) as request_sha256 \gset bsv_
select private.record_legacy_published_receipt('bank-shadow','Bankhero','a9050000-0000-0000-0000-000000000001'::uuid,'c9050000-0000-0000-0000-000000000001'::uuid,'b9050000-0000-0000-0000-000000000001'::uuid,:'bsv_request_sha256',1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint);

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true((select outcome='RECORDED' from private.record_m4_file_snapshot_manifest_for_receipt('a9050000-0000-0000-0000-000000000001'::uuid,'c9050000-0000-0000-0000-000000000001'::uuid,:'bsv_request_sha256','legacy-file-manifest-v1',repeat('a',64),9)),'the valid receipt-bound M4 manifest records before bank topology evidence');
create or replace function pg_temp.record_bank_nodes(p_nodes jsonb) returns text language sql as $$ select outcome from private.record_bank_snapshot_v1_topology_shadow_for_receipt('a9050000-0000-0000-0000-000000000001'::uuid,'c9050000-0000-0000-0000-000000000001'::uuid,:'bsv_request_sha256',repeat('a',64),9,repeat('b',64),48,p_nodes) $$;

select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(null::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"parentNodeIndex":null,"siblingOrdinal":0} ]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"nodeIndex":0,"siblingOrdinal":0} ]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"nodeIndex":0,"parentNodeIndex":null} ]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"nodeIndex":0,"parentNodeIndex":null,"siblingOrdinal":0}, {"nodeIndex":1,"parentNodeIndex":null,"siblingOrdinal":1} ]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"nodeIndex":0,"parentNodeIndex":0,"siblingOrdinal":0} ]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"nodeIndex":"0","parentNodeIndex":null,"siblingOrdinal":0} ]''::jsonb)');
select pg_temp.expect_state('22023','select pg_temp.record_bank_nodes(''[ {"nodeIndex":0,"parentNodeIndex":null,"siblingOrdinal":1,"extra":true} ]''::jsonb)');
select jsonb_agg(jsonb_build_object('nodeIndex',i,'parentNodeIndex',case when i=0 then null::integer else i-1 end,'siblingOrdinal',0) order by i)::text as depth_nodes from generate_series(0,64) as series(i) \gset bsv_
select pg_temp.expect_state('22023',format('select pg_temp.record_bank_nodes(%L::jsonb)',:'bsv_depth_nodes'));
select pg_temp.assert_true(not exists(select 1 from private.game_character_bank_snapshot_v1_topology_shadows where character_id='a9050000-0000-0000-0000-000000000001'::uuid and command_id='c9050000-0000-0000-0000-000000000001'::uuid),'every rejected topology leaves no shadow evidence');

select pg_temp.assert_true(pg_temp.record_bank_nodes('[{"nodeIndex":0,"parentNodeIndex":null,"siblingOrdinal":0},{"nodeIndex":1,"parentNodeIndex":0,"siblingOrdinal":0},{"nodeIndex":2,"parentNodeIndex":1,"siblingOrdinal":0}]'::jsonb)='RECORDED','a canonical one-root depth-three topology with explicit null root records');
select pg_temp.assert_true(pg_temp.record_bank_nodes('[{"nodeIndex":0,"parentNodeIndex":null,"siblingOrdinal":0},{"nodeIndex":1,"parentNodeIndex":0,"siblingOrdinal":0},{"nodeIndex":2,"parentNodeIndex":1,"siblingOrdinal":0}]'::jsonb)='EXACT_RETRY','a canonical topology exact-retries without mutation');
select pg_temp.assert_true((select item_count=3 from private.game_character_bank_snapshot_v1_topology_shadows where character_id='a9050000-0000-0000-0000-000000000001'::uuid and command_id='c9050000-0000-0000-0000-000000000001'::uuid) and (select count(*)=3 from private.game_character_bank_snapshot_v1_topology_shadow_items where character_id='a9050000-0000-0000-0000-000000000001'::uuid and command_id='c9050000-0000-0000-0000-000000000001'::uuid),'only canonical topology rows are recorded');
reset role;
reset session authorization;
rollback;
