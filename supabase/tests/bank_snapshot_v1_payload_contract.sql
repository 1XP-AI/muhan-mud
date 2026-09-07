\set ON_ERROR_STOP on
begin;
create or replace function pg_temp.assert_true(v boolean,m text) returns void language plpgsql as $$ begin if v is not true then raise exception '%',m; end if; end $$;
create or replace function pg_temp.expect_state(expected text,statement text) returns void language plpgsql as $$ begin begin execute statement; exception when others then if sqlstate=expected then return; end if; raise; end; raise exception 'expected SQLSTATE %',expected; end $$;
create function pg_temp.envelope(kind integer,body bytea) returns bytea language sql as $$
  select decode('4d55484344544f000001','hex') || int2send(kind::smallint) || int4send(octet_length(body)) || body || public.digest(body,'sha256')
$$;
select pg_temp.envelope(6,decode('00010300000004000000010002090000015d00000000ffffffff00000000','hex') || decode(repeat('00',337),'hex')) as graph \gset
select pg_temp.envelope(8,decode('000109','hex') || int4send(octet_length(:'graph'::bytea)) || :'graph'::bytea) as payload \gset
select encode(public.digest(:'payload'::bytea,'sha256'),'hex') as bank_hash \gset
select pg_temp.assert_true(private.bank_snapshot_v1_payload_nodes(:'payload'::bytea)='[{"nodeIndex":0,"parentNodeIndex":null,"siblingOrdinal":0}]'::jsonb,'payload yields its own canonical topology');
select pg_temp.expect_state('22023',format('select private.bank_snapshot_v1_payload_nodes(%L::bytea)',:'graph'));
select pg_temp.expect_state('22023','select private.bank_snapshot_v1_payload_nodes(null)');
select pg_temp.expect_state('22023',format('select private.bank_snapshot_v1_payload_nodes(%L::bytea)',:'payload'::bytea || decode('00','hex')));

insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format)
values('a9160000-0000-0000-0000-000000000001','bank-payload','Bankhero','Bankhero',substr(encode(public.digest(convert_to('Bankhero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1);
insert into private.game_character_legacy_heads(character_id,head_state,storage_format,revision) values('a9160000-0000-0000-0000-000000000001','absent',1,0);
select * from private.acquire_game_world_writer_epoch('bank-payload','b9160000-0000-0000-0000-000000000001'::uuid,clock_timestamp()+interval '3 minutes');
select private.game_character_shadow_request_sha256('bank-payload','a9160000-0000-0000-0000-000000000001'::uuid,'Bankhero',substr(encode(public.digest(convert_to('Bankhero','UTF8'),'sha1'),'hex'),1,2),'c9160000-0000-0000-0000-000000000001'::uuid,'b9160000-0000-0000-0000-000000000001'::uuid,1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint) as request_hash \gset
select private.record_legacy_published_receipt('bank-payload','Bankhero','a9160000-0000-0000-0000-000000000001'::uuid,'c9160000-0000-0000-0000-000000000001'::uuid,'b9160000-0000-0000-0000-000000000001'::uuid,:'request_hash',1::bigint,1::bigint,'absent',null::text,repeat('a',64),1::smallint);
create function pg_temp.record_payload(p_bytes bytea default :'payload',p_hash text default :'bank_hash',p_request text default :'request_hash') returns text language sql as $$
  select outcome from private.record_bank_snapshot_v1_payload_for_receipt('a9160000-0000-0000-0000-000000000001','c9160000-0000-0000-0000-000000000001',p_request,repeat('a',64),9,p_hash,p_bytes)
$$;
select pg_temp.expect_state('P0001','select pg_temp.record_payload()');
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001','select pg_temp.record_payload()');
select private.record_m4_file_snapshot_manifest_for_receipt('a9160000-0000-0000-0000-000000000001','c9160000-0000-0000-0000-000000000001',:'request_hash','legacy-file-manifest-v1',repeat('a',64),9);
select pg_temp.expect_state('22023','select pg_temp.record_payload(null)');
select pg_temp.expect_state('22023',format('select pg_temp.record_payload(%L::bytea,%L)',:'payload',repeat('0',64)));
select pg_temp.assert_true(pg_temp.record_payload()='RECORDED','complete payload is persisted');
select pg_temp.assert_true(pg_temp.record_payload()='EXACT_RETRY','complete payload exact retry');
select pg_temp.expect_state('P0001',format('select pg_temp.record_payload(%L::bytea,%L,%L)',:'payload',:'bank_hash',repeat('f',64)));
select pg_temp.expect_state('42501','select payload from private.game_character_bank_snapshot_v1_payloads');
reset role;
reset session authorization;
select pg_temp.assert_true((select payload=:'payload'::bytea and bank_sha256=:'bank_hash' from private.game_character_bank_snapshot_v1_payloads where character_id='a9160000-0000-0000-0000-000000000001'),'stored bytes match exactly');
select pg_temp.expect_state('P0001','update private.game_character_bank_snapshot_v1_payloads set payload=payload');
select pg_temp.expect_state('P0001','delete from private.game_character_bank_snapshot_v1_payloads');
rollback;
