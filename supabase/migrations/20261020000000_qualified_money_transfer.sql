-- Qualification and commit must run in one statement/transaction. Still closed
-- until audited baseline enrollment and live command wiring are ready.
create table if not exists private.game_character_money_transfer_authorities (
  character_id uuid not null,command_id uuid not null,
  world_id text not null,actor_user_id uuid not null references auth.users(id) on delete restrict,
  session_id uuid not null,gateway_instance_id text not null,
  writer_instance_id uuid not null,writer_epoch bigint not null check(writer_epoch>0),
  primary key(character_id,command_id),
  foreign key(character_id,command_id) references private.game_character_money_transfer_intents(character_id,command_id) on delete restrict
);
alter table private.game_character_money_transfer_authorities enable row level security;
drop trigger if exists money_transfer_authority_immutable on private.game_character_money_transfer_authorities;
create trigger money_transfer_authority_immutable before update or delete on private.game_character_money_transfer_authorities
  for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();
create or replace function private.commit_qualified_money_transfer(
  p_character uuid,p_world text,p_actor uuid,p_session uuid,p_gateway text,p_writer uuid,p_epoch bigint,
  p_command uuid,p_expected_revision bigint,p_direction text,p_amount bigint,p_player bytea,p_bank bytea)
returns table(outcome text,committed_revision bigint)
language plpgsql security definer set search_path=pg_catalog,private as $$
declare prior private.game_character_money_transfer_authorities%rowtype;
begin
  perform private.lock_money_transfer_authority(p_character,p_world,p_actor,p_session,p_gateway,p_writer,p_epoch);
  -- The qualifier holds writer/character/session/pair locks until this entire
  -- transaction ends. All later kernel row locks are already held.
  select * into prior from private.game_character_money_transfer_authorities
    where character_id=p_character and command_id=p_command;
  if found then
    if prior.world_id<>p_world or prior.actor_user_id<>p_actor or prior.session_id<>p_session
       or prior.gateway_instance_id<>p_gateway or prior.writer_instance_id<>p_writer or prior.writer_epoch<>p_epoch then
      raise exception using errcode='P0001',message='money transfer command authority conflict';
    end if;
    return query select * from private.commit_money_transfer_candidate(p_character,p_command,p_expected_revision,p_direction,p_amount,p_player,p_bank);
    return;
  end if;
  if exists(select 1 from private.game_character_money_transfer_intents where character_id=p_character and command_id=p_command) then
    raise exception using errcode='P0001',message='money transfer command lacks matching authority';
  end if;
  return query select * from private.commit_money_transfer_candidate(p_character,p_command,p_expected_revision,p_direction,p_amount,p_player,p_bank);
  insert into private.game_character_money_transfer_authorities values(p_character,p_command,p_world,p_actor,p_session,p_gateway,p_writer,p_epoch);
end $$;
revoke all on table private.game_character_money_transfer_authorities from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.commit_qualified_money_transfer(uuid,text,uuid,uuid,text,uuid,bigint,uuid,bigint,text,bigint,bytea,bytea)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
