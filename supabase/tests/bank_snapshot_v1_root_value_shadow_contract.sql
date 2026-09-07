\set ON_ERROR_STOP on
-- PG17 disposable contract: apply through 20261007000000, then run.
begin;
create or replace function pg_temp.assert_true(v boolean, m text) returns void language plpgsql as $$ begin if v is not true then raise exception '%',m; end if; end $$;
create or replace function pg_temp.expect_state(expected text, statement text) returns void language plpgsql as $$ begin begin execute statement; exception when others then if sqlstate=expected then return; end if; raise; end; raise exception using errcode='P0002',message=format('expected SQLSTATE %s',expected); end $$;
select pg_temp.assert_true(to_regprocedure('private.record_bank_snapshot_v1_root_value_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,bigint)') is not null,'bank root-value recorder signature exists');
select pg_temp.assert_true((select relrowsecurity from pg_class where oid='private.game_character_bank_snapshot_v1_root_value_shadows'::regclass),'root-value relation is private RLS evidence');
select pg_temp.assert_true(not exists(select 1 from pg_attribute where attrelid='private.game_character_bank_snapshot_v1_root_value_shadows'::regclass and atttypid='bytea'::regtype and attnum>0 and not attisdropped),'root-value shadow stores no bank payload');
select pg_temp.assert_true((select prosecdef and proconfig=array['search_path=pg_catalog, private']::text[] from pg_proc where oid='private.record_bank_snapshot_v1_root_value_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,bigint)'::regprocedure),'root-value writer is a pinned security definer');

insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values ('a9070000-0000-0000-0000-000000000001','bank-root','Roothero','Roothero',substr(encode(public.digest(convert_to('Roothero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1);
insert into private.game_character_legacy_heads(character_id,head_state,storage_format,revision) values ('a9070000-0000-0000-0000-000000000001','absent',1,0);
select * from private.acquire_game_world_writer_epoch('bank-root','b9070000-0000-0000-0000-000000000001'::uuid,clock_timestamp()+interval '3 minutes');
select private.game_character_shadow_request_sha256('bank-root','a9070000-0000-0000-0000-000000000001'::uuid,'Roothero',substr(encode(public.digest(convert_to('Roothero','UTF8'),'sha1'),'hex'),1,2),'c9070000-0000-0000-0000-000000000001'::uuid,'b9070000-0000-0000-0000-000000000001'::uuid,1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint) as request_sha256 \gset bsv_
select private.record_legacy_published_receipt('bank-root','Roothero','a9070000-0000-0000-0000-000000000001'::uuid,'c9070000-0000-0000-0000-000000000001'::uuid,'b9070000-0000-0000-0000-000000000001'::uuid,:'bsv_request_sha256',1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint);
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001',format('select outcome from private.record_bank_snapshot_v1_root_value_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,48,-1)', 'a9070000-0000-0000-0000-000000000001','c9070000-0000-0000-0000-000000000001',:'bsv_request_sha256',repeat('a',64),repeat('b',64)));
select pg_temp.assert_true((select outcome='RECORDED' from private.record_m4_file_snapshot_manifest_for_receipt('a9070000-0000-0000-0000-000000000001'::uuid,'c9070000-0000-0000-0000-000000000001'::uuid,:'bsv_request_sha256','legacy-file-manifest-v1',repeat('a',64),9)),'matching M4 manifest settles before root-value evidence');
-- Bind the psql variable before entering the dollar-quoted function body.
create or replace function pg_temp.record_root(p_value bigint, p_request_sha256 text default :'bsv_request_sha256') returns text language sql as $$ select outcome from private.record_bank_snapshot_v1_root_value_shadow_for_receipt('a9070000-0000-0000-0000-000000000001'::uuid,'c9070000-0000-0000-0000-000000000001'::uuid,p_request_sha256,repeat('a',64),9,repeat('b',64),48,p_value) $$;
select pg_temp.assert_true(pg_temp.record_root(-9223372036854775808::bigint)='RECORDED','canonical signed-i64 minimum root value records');
select pg_temp.assert_true(pg_temp.record_root(-9223372036854775808::bigint)='EXACT_RETRY','same command and value exact-retry without mutation');
select pg_temp.expect_state('P0001','select pg_temp.record_root(7)');
select pg_temp.expect_state('P0001',format('select outcome from private.record_bank_snapshot_v1_root_value_shadow_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,48,-1)', 'a9070000-0000-0000-0000-000000000001','c9070000-0000-0000-0000-000000000002',:'bsv_request_sha256',repeat('a',64),repeat('b',64)));
reset role;
reset session authorization;
select pg_temp.assert_true((select root_value=-9223372036854775808::bigint from private.game_character_bank_snapshot_v1_root_value_shadows where character_id='a9070000-0000-0000-0000-000000000001'::uuid and command_id='c9070000-0000-0000-0000-000000000001'::uuid),'changed-value retry and topology-key mismatch leave immutable root evidence untouched');
reset role;
reset session authorization;
rollback;
