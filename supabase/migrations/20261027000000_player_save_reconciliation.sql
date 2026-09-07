-- Read-only historical confirmation. Never resends or upgrades an old request.
create or replace function private.reconcile_player_snapshot(
  p_world text,p_name text,p_writer uuid,p_epoch bigint,p_character uuid,p_command uuid,
  p_revision bigint,p_source_hash text,p_player bytea,p_recovery_writer uuid,p_recovery_epoch bigint)
returns table(outcome text,committed_revision bigint)
language plpgsql security definer set search_path=pg_catalog,private as $$
declare w private.game_character_writer_epochs%rowtype;
  c public.game_characters%rowtype;
  i private.game_character_player_save_intents%rowtype;
  q private.game_character_paired_snapshot_commands%rowtype;
begin
  if session_user<>'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then
    raise exception using errcode='P0001',message='player recovery writer identity invalid';
  end if;
  if p_world is null or not private.m3_shadow_valid_world(p_world) or p_name is null
     or octet_length(p_name) not between 1 and 14 or p_writer is null or p_epoch is null or p_epoch<1
     or p_character is null or p_command is null or p_revision is null or p_revision not between 0 and 9223372036854775805
     or p_source_hash is null or p_source_hash !~ '^[0-9a-f]{64}$' or p_player is null
     or octet_length(p_player) not between 48 and 4194304
     or p_recovery_writer is null or p_recovery_epoch is null or p_recovery_epoch<1 then
    raise exception using errcode='22023',message='player recovery arguments invalid';
  end if;
  select * into w from private.game_character_writer_epochs where world_id=p_world for share;
  select * into c from public.game_characters where id=p_character for share;
  if w.world_id is null or w.writer_instance_id<>p_recovery_writer or w.writer_epoch<>p_recovery_epoch
     or w.sealed_at is not null or w.expires_at<=clock_timestamp()
     or c.id is null or c.world_id<>p_world or c.legacy_name<>p_name
     or c.lifecycle<>'active' or c.owner_user_id is null or c.storage_format<>1 then
    raise exception using errcode='P0001',message='player recovery authority no longer eligible';
  end if;
  select * into i from private.game_character_player_save_intents where character_id=p_character and command_id=p_command;
  if not found then
    return query select 'UNRESOLVED'::text,null::bigint;return;
  end if;
  select * into q from private.game_character_paired_snapshot_commands where character_id=p_character and command_id=p_command;
  if row(i.world_id,i.legacy_name,i.owner_user_id,i.writer_instance_id,i.writer_epoch,i.expected_player_hash)
       is distinct from row(p_world,p_name,c.owner_user_id,p_writer,p_epoch,p_source_hash)
     or row(q.expected_revision,q.player_payload,q.committed_revision)
       is distinct from row(p_revision,p_player,p_revision+1) then
    raise exception using errcode='P0001',message='player recovery request conflicts';
  end if;
  return query select 'CONFIRMED'::text,q.committed_revision;
end $$;
revoke all on function private.reconcile_player_snapshot(text,text,uuid,bigint,uuid,uuid,bigint,text,bytea,uuid,bigint)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
