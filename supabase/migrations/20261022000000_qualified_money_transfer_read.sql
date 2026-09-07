-- The real writer may eventually read one qualified, consistent pair without
-- direct table SELECT. Still closed pending runtime authority activation.
create or replace function private.read_qualified_money_transfer_state(
  p_character uuid,p_world text,p_actor uuid,p_session uuid,p_gateway text,p_writer uuid,p_epoch bigint)
returns table(revision bigint,player_payload bytea,bank_payload bytea,player_hash text,bank_hash text)
language plpgsql security definer set search_path=pg_catalog,private as $$
begin
  perform private.lock_money_transfer_authority(p_character,p_world,p_actor,p_session,p_gateway,p_writer,p_epoch);
  return query select s.revision,s.player_payload,s.bank_payload,
    encode(public.digest(s.player_payload,'sha256'),'hex'),encode(public.digest(s.bank_payload,'sha256'),'hex')
    from private.game_character_paired_snapshot_states s where s.character_id=p_character;
end $$;
revoke all on function private.read_qualified_money_transfer_state(uuid,text,uuid,uuid,text,uuid,bigint)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
