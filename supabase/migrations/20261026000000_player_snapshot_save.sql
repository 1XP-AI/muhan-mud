-- Closed C-gameplay snapshot persistence boundary. Does not port command logic.
create table if not exists private.game_character_player_save_intents (
  character_id uuid not null,
  command_id uuid not null,
  world_id text not null,
  legacy_name text not null,
  owner_user_id uuid not null,
  writer_instance_id uuid not null,
  writer_epoch bigint not null check(writer_epoch>0),
  expected_player_hash text not null check(expected_player_hash ~ '^[0-9a-f]{64}$'),
  primary key(character_id,command_id),
  foreign key(character_id,command_id) references private.game_character_paired_snapshot_commands(character_id,command_id) on delete restrict
);
alter table private.game_character_player_save_intents enable row level security;
drop trigger if exists player_save_intent_immutable on private.game_character_player_save_intents;
create trigger player_save_intent_immutable before update or delete on private.game_character_player_save_intents
  for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();

create or replace function private.commit_player_snapshot(
  p_world text,p_name text,p_writer uuid,p_epoch bigint,p_character uuid,p_command uuid,
  p_revision bigint,p_source_hash text,p_player bytea)
returns table(outcome text,committed_revision bigint)
language plpgsql security definer set search_path=pg_catalog,private as $$
declare route record;
  state private.game_character_paired_snapshot_states%rowtype;
  prior private.game_character_paired_snapshot_commands%rowtype;
  intent private.game_character_player_save_intents%rowtype;
begin
  if session_user<>'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then
    raise exception using errcode='P0001',message='player save writer identity invalid';
  end if;
  if p_character is null or p_command is null or p_revision is null or p_revision not between 0 and 9223372036854775805
     or p_source_hash is null or p_source_hash !~ '^[0-9a-f]{64}$'
     or p_player is null or octet_length(p_player)>4194304
     or private.player_snapshot_v1_payload_valid(p_player) is not true then
    raise exception using errcode='22023',message='invalid player save request';
  end if;
  -- Obtain update ownership before calling the shared route lookup: two
  -- concurrent saves must not both take SHARE and then deadlock upgrading it.
  perform 1 from private.game_character_writer_epochs where world_id=p_world for share;
  perform 1 from public.game_characters where id=p_character and world_id=p_world and legacy_name=p_name for share;
  if not found then raise exception using errcode='P0001',message='player save identity mismatch';end if;
  select * into state from private.game_character_paired_snapshot_states where character_id=p_character for update;
  select * into strict route from private.resolve_player_paired_route(p_world,p_name,p_writer,p_epoch);
  if route.character_id<>p_character or state.character_id is null then
    raise exception using errcode='P0001',message='player save identity mismatch';
  end if;
  if substring(p_player from 24 for 80) <>
     (convert_to(p_name,'UTF8')||decode(repeat('00',80-octet_length(convert_to(p_name,'UTF8'))),'hex')) then
    raise exception using errcode='22023',message='player save payload name mismatch';
  end if;
  select * into prior from private.game_character_paired_snapshot_commands where character_id=p_character and command_id=p_command;
  if found then
    select * into intent from private.game_character_player_save_intents where character_id=p_character and command_id=p_command;
    if not found or row(intent.world_id,intent.legacy_name,intent.owner_user_id,intent.writer_instance_id,intent.writer_epoch,intent.expected_player_hash)
       is distinct from row(p_world,p_name,route.owner_user_id,p_writer,p_epoch,p_source_hash)
       or prior.expected_revision<>p_revision or prior.player_payload<>p_player then
      raise exception using errcode='P0001',message='player save command conflict';
    end if;
    return query select 'EXACT_RETRY'::text,prior.committed_revision;return;
  end if;
  if state.revision<>p_revision or encode(public.digest(state.player_payload,'sha256'),'hex')<>p_source_hash then
    raise exception using errcode='40001',message='player save source changed';
  end if;
  -- Preserve bank bytes atomically under the same pair revision.
  return query select * from private.commit_paired_snapshot_candidate(p_character,p_command,p_revision,p_player,state.bank_payload);
  insert into private.game_character_player_save_intents
    values(p_character,p_command,p_world,p_name,route.owner_user_id,p_writer,p_epoch,p_source_hash);
end $$;
revoke all on table private.game_character_player_save_intents from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.commit_player_snapshot(text,text,uuid,bigint,uuid,uuid,bigint,text,bytea)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
