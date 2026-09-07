-- Closed authority boundary: no runtime grant or baseline enrollment yet.
create or replace function private.lock_money_transfer_authority(
  p_character uuid,p_world text,p_actor uuid,p_session uuid,p_gateway text,p_writer uuid,p_epoch bigint)
returns void language plpgsql security definer set search_path=pg_catalog,private as $$
declare c public.game_characters%rowtype; s private.game_character_sessions%rowtype;
  w private.game_character_writer_epochs%rowtype; pair_found boolean; checked_at timestamptz;
begin
  if session_user<>'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then
    raise exception using errcode='P0001',message='money transfer writer identity invalid';
  end if;
  if p_character is null or p_world is null or p_actor is null or p_session is null
     or p_gateway is null or p_writer is null or p_epoch is null then
    raise exception using errcode='22023',message='money transfer authority arguments invalid';
  end if;
  -- Stable lock order; sample time only after every potentially blocking lock.
  select * into w from private.game_character_writer_epochs where world_id=p_world for share;
  select * into c from public.game_characters where id=p_character for share;
  select * into s from private.game_character_sessions where character_id=p_character for share;
  perform 1 from private.game_character_paired_snapshot_states where character_id=p_character for update;
  pair_found:=found;
  checked_at:=clock_timestamp();
  if not pair_found or c.id is null or c.world_id<>p_world or c.owner_user_id is distinct from p_actor
     or c.lifecycle<>'active' or c.storage_format<>1
     or s.character_id is null or s.actor_user_id<>p_actor or s.session_id<>p_session
     or s.gateway_instance_id<>p_gateway or s.expires_at<=checked_at
     or w.world_id is null or w.writer_instance_id<>p_writer or w.writer_epoch<>p_epoch
     or w.sealed_at is not null or w.expires_at<=checked_at then
    raise exception using errcode='P0001',message='money transfer authority no longer eligible';
  end if;
end $$;
revoke all on function private.lock_money_transfer_authority(uuid,text,uuid,uuid,text,uuid,bigint)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
