-- Read-only offline-capable identity lookup; NOT a player save RPC or cutover.
create or replace function private.resolve_player_paired_route(
  p_world text,p_name text,p_writer uuid,p_epoch bigint)
returns table(character_id uuid,owner_user_id uuid,revision bigint,player_hash text,bank_hash text)
language plpgsql security definer set search_path=pg_catalog,private as $$
declare w private.game_character_writer_epochs%rowtype;
  c public.game_characters%rowtype;
  s private.game_character_paired_snapshot_states%rowtype;
  checked_at timestamptz;
begin
  if session_user<>'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then
    raise exception using errcode='P0001',message='player route writer identity invalid';
  end if;
  if p_world is null or not private.m3_shadow_valid_world(p_world)
     or p_name is null or octet_length(p_name) not between 1 and 14
     or p_writer is null or p_epoch is null or p_epoch<1 then
    raise exception using errcode='22023',message='player route arguments invalid';
  end if;
  -- Same writer -> character -> pair order as the qualified money path.
  select * into w from private.game_character_writer_epochs where world_id=p_world for share;
  select * into c from public.game_characters
    where world_id=p_world and legacy_name_key=private.game_identity_canonical_legacy_name(p_name) for share;
  select * into s from private.game_character_paired_snapshot_states where game_character_paired_snapshot_states.character_id=c.id for share;
  checked_at:=clock_timestamp();
  if w.world_id is null or w.writer_instance_id<>p_writer or w.writer_epoch<>p_epoch
     or w.expires_at<=checked_at or w.sealed_at is not null
     or c.id is null or c.legacy_name<>p_name or c.lifecycle<>'active' or c.owner_user_id is null
     or c.storage_format<>1 or s.character_id is null then
    raise exception using errcode='P0001',message='player route not eligible';
  end if;
  return query select c.id,c.owner_user_id,s.revision,
    encode(public.digest(s.player_payload,'sha256'),'hex'),encode(public.digest(s.bank_payload,'sha256'),'hex');
end $$;
revoke all on function private.resolve_player_paired_route(text,text,uuid,bigint)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
