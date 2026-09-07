-- Closed detached-load boundary, revalidating the route in this transaction.
create or replace function private.read_player_paired_snapshot(
  p_world text,p_name text,p_writer uuid,p_epoch bigint,p_character uuid,p_revision bigint)
returns table(payload bytea,payload_hash text)
language plpgsql security definer set search_path=pg_catalog,private as $$
declare route record;
begin
  if p_character is null or p_revision is null or p_revision<0 then
    raise exception using errcode='22023',message='invalid player read expectation';
  end if;
  select * into strict route from private.resolve_player_paired_route(p_world,p_name,p_writer,p_epoch);
  if route.character_id<>p_character or route.revision<>p_revision then
    raise exception using errcode='40001',message='player read route changed';
  end if;
  return query select s.player_payload,route.player_hash::text
    from private.game_character_paired_snapshot_states s where s.character_id=p_character;
end $$;
revoke all on function private.read_player_paired_snapshot(text,text,uuid,bigint,uuid,bigint)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
