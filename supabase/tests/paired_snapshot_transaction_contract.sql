\set ON_ERROR_STOP on
begin;
create or replace function pg_temp.assert_true(v boolean,m text) returns void language plpgsql as $$ begin if v is not true then raise exception '%',m; end if; end $$;
create or replace function pg_temp.expect_state(expected text,statement text) returns void language plpgsql as $$ begin begin execute statement; exception when others then if sqlstate=expected then return; end if; raise; end; raise exception 'expected SQLSTATE %',expected; end $$;
select payload as bank_payload from private.game_character_bank_snapshot_v1_payloads
where character_id='a9530000-0000-0000-0000-000000000001' \gset
create function pg_temp.pair_envelope(kind integer,body bytea) returns bytea language sql as $$
  select decode('4d55484344544f000001','hex')||int2send(kind::smallint)||int4send(octet_length(body))||body||public.digest(body,'sha256')
$$;
select pg_temp.pair_envelope(6,set_byte(substring(:'bank_payload'::bytea from 40 for octet_length(:'bank_payload'::bytea)-103),30,65)) as next_graph \gset
select pg_temp.pair_envelope(8,decode('000109','hex')||int4send(octet_length(:'next_graph'::bytea))||:'next_graph'::bytea) as next_bank \gset
select pg_temp.assert_true(:'next_bank'::bytea<>:'bank_payload'::bytea and private.bank_snapshot_v1_payload_nodes(:'next_bank'::bytea) is not null,'next bank differs and is canonical');
insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format)
values('a9170000-0000-0000-0000-000000000001','pair-contract','Pvahero','Pvahero',substr(encode(public.digest(convert_to('Pvahero','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1);
insert into private.game_character_paired_snapshot_states values('a9170000-0000-0000-0000-000000000001',0,decode(:'fixture_hex','hex'),:'bank_payload'::bytea);
create function pg_temp.commit_pair(command uuid,revision bigint,bank bytea default :'next_bank',player bytea default decode(:'next_fixture_hex','hex')) returns text language sql as $$
  select outcome from private.commit_paired_snapshot_candidate('a9170000-0000-0000-0000-000000000001',command,revision,player,bank)
$$;
select pg_temp.assert_true(not has_function_privilege('mud_writer','private.commit_paired_snapshot_candidate(uuid,uuid,bigint,bytea,bytea)','EXECUTE'),'kernel not exposed to runtime');
-- Force the second write to fail after the state update: both must roll back.
create function pg_temp.reject_command() returns trigger language plpgsql as $$ begin raise exception using errcode='P0001',message='injected command journal failure'; end $$;
create trigger injected_pair_failure before insert on private.game_character_paired_snapshot_commands for each row execute function pg_temp.reject_command();
select pg_temp.expect_state('P0001','select pg_temp.commit_pair(''c9170000-0000-0000-0000-000000000001'',0)');
select pg_temp.assert_true((select revision=0 and bank_payload=:'bank_payload'::bytea and player_payload=decode(:'fixture_hex','hex') from private.game_character_paired_snapshot_states where character_id='a9170000-0000-0000-0000-000000000001'),'journal failure rolls back both snapshots and revision');
select pg_temp.assert_true(not exists(select 1 from private.game_character_paired_snapshot_commands),'failed transaction leaves no command');
drop trigger injected_pair_failure on private.game_character_paired_snapshot_commands;
select pg_temp.assert_true(pg_temp.commit_pair('c9170000-0000-0000-0000-000000000001',0)='COMMITTED','pair committed');
select pg_temp.assert_true((select player_payload=decode(:'next_fixture_hex','hex') and bank_payload=:'next_bank'::bytea from private.game_character_paired_snapshot_states where character_id='a9170000-0000-0000-0000-000000000001'),'both changed snapshots committed together');
select pg_temp.assert_true(pg_temp.commit_pair('c9170000-0000-0000-0000-000000000001',0)='EXACT_RETRY','lost acknowledgement retry is exact');
select pg_temp.expect_state('40001','select pg_temp.commit_pair(''c9170000-0000-0000-0000-000000000002'',0)');
select pg_temp.expect_state('P0001','select pg_temp.commit_pair(''c9170000-0000-0000-0000-000000000001'',1)');
select pg_temp.expect_state('22023','select pg_temp.commit_pair(''c9170000-0000-0000-0000-000000000002'',1,decode(''00'',''hex''))');
select pg_temp.assert_true((select revision=1 from private.game_character_paired_snapshot_states where character_id='a9170000-0000-0000-0000-000000000001') and (select count(*)=1 from private.game_character_paired_snapshot_commands),'rejected calls never partially advance state');
select pg_temp.expect_state('P0001','delete from private.game_character_paired_snapshot_commands');
rollback;
