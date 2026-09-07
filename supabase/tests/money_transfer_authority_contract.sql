\set ON_ERROR_STOP on
begin;
create or replace function pg_temp.assert_true(v boolean,m text) returns void language plpgsql as $$ begin if v is not true then raise exception '%',m; end if; end $$;
create or replace function pg_temp.expect_state(expected text,statement text) returns void language plpgsql as $$ begin begin execute statement; exception when others then if sqlstate=expected then return; end if; raise; end; raise exception 'expected SQLSTATE %',expected; end $$;
select pg_temp.assert_true(not has_function_privilege('mud_writer','private.lock_money_transfer_authority(uuid,text,uuid,uuid,text,uuid,bigint)','EXECUTE'),'migration leaves authority boundary closed');
insert into auth.users(id) values('e9200000-0000-0000-0000-000000000001');
update public.game_characters set owner_user_id='e9200000-0000-0000-0000-000000000001',lifecycle='active',claimed_at=clock_timestamp()
where id='a9190000-0000-0000-0000-000000000001';
insert into private.game_character_sessions(character_id,session_id,actor_user_id,gateway_instance_id,created_at,expires_at)
values('a9190000-0000-0000-0000-000000000001','f9200000-0000-0000-0000-000000000001','e9200000-0000-0000-0000-000000000001','test-gateway',clock_timestamp()-interval '5 minutes',clock_timestamp()+interval '3 minutes');
select * from private.acquire_game_world_writer_epoch('rust-pair','b9200000-0000-0000-0000-000000000001',clock_timestamp()+interval '3 minutes');
create function pg_temp.authority(actor uuid default 'e9200000-0000-0000-0000-000000000001',session uuid default 'f9200000-0000-0000-0000-000000000001',gateway text default 'test-gateway',epoch bigint default 1,world text default 'rust-pair') returns void language sql as $$
  select private.lock_money_transfer_authority('a9190000-0000-0000-0000-000000000001',world,actor,session,gateway,'b9200000-0000-0000-0000-000000000001',epoch)
$$;
-- Test-only grant in a transaction, always rolled back. No deployed grant.
grant execute on function private.lock_money_transfer_authority(uuid,text,uuid,uuid,text,uuid,bigint) to mud_writer;
select pg_temp.expect_state('P0001','select pg_temp.authority()');
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.authority();
select pg_temp.expect_state('P0001','select pg_temp.authority(actor=>''e9200000-0000-0000-0000-000000000002'')');
select pg_temp.expect_state('P0001','select pg_temp.authority(session=>''f9200000-0000-0000-0000-000000000002'')');
select pg_temp.expect_state('P0001','select pg_temp.authority(gateway=>''another-gateway'')');
select pg_temp.expect_state('P0001','select pg_temp.authority(epoch=>2)');
select pg_temp.expect_state('P0001','select pg_temp.authority(world=>''another-world'')');
reset role;
reset session authorization;
update public.game_characters set lifecycle='suspended' where id='a9190000-0000-0000-0000-000000000001';
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001','select pg_temp.authority()');
reset role;
reset session authorization;
update public.game_characters set lifecycle='active' where id='a9190000-0000-0000-0000-000000000001';
update private.game_character_sessions set expires_at=clock_timestamp()-interval '1 second' where character_id='a9190000-0000-0000-0000-000000000001';
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001','select pg_temp.authority()');
reset role;
reset session authorization;
update private.game_character_sessions set expires_at=clock_timestamp()+interval '3 minutes' where character_id='a9190000-0000-0000-0000-000000000001';
update private.game_character_writer_epochs set sealed_at=clock_timestamp() where world_id='rust-pair';
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.expect_state('P0001','select pg_temp.authority()');
reset role;
reset session authorization;
rollback;
do $$ begin
  if has_function_privilege('mud_writer','private.lock_money_transfer_authority(uuid,text,uuid,uuid,text,uuid,bigint)','EXECUTE') then
    raise exception 'runtime authority grant leaked from disposable test';
  end if;
end $$;
