-- Read-only reconciliation under a current writer lease. This never replays
-- an old request, refreshes its authority, or changes player/bank state.
create or replace function private.reconcile_money_transfer(
  p_character uuid,p_world text,p_actor uuid,p_session uuid,p_gateway text,p_writer uuid,p_epoch bigint,
  p_command uuid,p_expected_revision bigint,p_direction text,p_amount bigint,p_player bytea,p_bank bytea,
  p_recovery_writer uuid,p_recovery_epoch bigint)
returns table(outcome text,committed_revision bigint)
language plpgsql security definer set search_path=pg_catalog,private as $$
declare w private.game_character_writer_epochs%rowtype; c public.game_characters%rowtype;
  a private.game_character_money_transfer_authorities%rowtype;
  i private.game_character_money_transfer_intents%rowtype;
  q private.game_character_paired_snapshot_commands%rowtype;
begin
  if session_user<>'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then
    raise exception using errcode='P0001',message='money recovery writer identity invalid';
  end if;
  if p_character is null or p_world is null or p_actor is null or p_session is null or p_gateway is null
     or p_writer is null or p_epoch is null or p_command is null or p_expected_revision is null
     or p_direction is null or p_amount is null or p_player is null or p_bank is null
     or p_recovery_writer is null or p_recovery_epoch is null
     or octet_length(p_player)>4194304 or octet_length(p_bank)>4194304 then
    raise exception using errcode='22023',message='money recovery arguments invalid';
  end if;
  select * into w from private.game_character_writer_epochs where world_id=p_world for share;
  select * into c from public.game_characters where id=p_character for share;
  if w.world_id is null or w.writer_instance_id<>p_recovery_writer or w.writer_epoch<>p_recovery_epoch
     or w.sealed_at is not null or w.expires_at<=clock_timestamp()
     or c.id is null or c.world_id<>p_world or c.owner_user_id is distinct from p_actor or c.lifecycle<>'active' then
    raise exception using errcode='P0001',message='money recovery authority no longer eligible';
  end if;
  select * into a from private.game_character_money_transfer_authorities where character_id=p_character and command_id=p_command;
  if not found then
    -- Absence is not permission to resend using a new identity/command.
    return query select 'UNRESOLVED'::text,null::bigint; return;
  end if;
  select * into i from private.game_character_money_transfer_intents where character_id=p_character and command_id=p_command;
  select * into q from private.game_character_paired_snapshot_commands where character_id=p_character and command_id=p_command;
  if row(a.world_id,a.actor_user_id,a.session_id,a.gateway_instance_id,a.writer_instance_id,a.writer_epoch)
       is distinct from row(p_world,p_actor,p_session,p_gateway,p_writer,p_epoch)
     or row(i.direction,i.amount) is distinct from row(p_direction,p_amount)
     or row(q.expected_revision,q.player_payload,q.bank_payload) is distinct from row(p_expected_revision,p_player,p_bank)
     or q.committed_revision is distinct from p_expected_revision+1 then
    raise exception using errcode='P0001',message='money recovery request conflicts';
  end if;
  return query select 'CONFIRMED'::text,q.committed_revision;
end $$;
revoke all on function private.reconcile_money_transfer(uuid,text,uuid,uuid,text,uuid,bigint,uuid,bigint,text,bigint,bytea,bytea,uuid,bigint)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
