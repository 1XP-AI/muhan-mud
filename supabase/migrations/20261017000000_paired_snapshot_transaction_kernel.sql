-- Internal transaction kernel only. No runtime role can call or seed it yet.
-- Identity/epoch/transfer-policy validation must precede any future grant.
create table if not exists private.game_character_paired_snapshot_states (
  character_id uuid primary key references public.game_characters(id) on delete restrict,
  revision bigint not null check(revision between 0 and 9223372036854775806),
  player_payload bytea not null check(private.player_snapshot_v1_payload_valid(player_payload) is true),
  bank_payload bytea not null check(private.bank_snapshot_v1_payload_nodes(bank_payload) is not null)
);
create table if not exists private.game_character_paired_snapshot_commands (
  character_id uuid not null references private.game_character_paired_snapshot_states(character_id) on delete restrict,
  command_id uuid not null,
  expected_revision bigint not null check(expected_revision between 0 and 9223372036854775805),
  committed_revision bigint not null check(committed_revision=expected_revision+1),
  player_payload bytea not null check(private.player_snapshot_v1_payload_valid(player_payload) is true),
  bank_payload bytea not null check(private.bank_snapshot_v1_payload_nodes(bank_payload) is not null),
  committed_at timestamptz not null default clock_timestamp(),
  primary key(character_id,command_id),
  unique(character_id,committed_revision)
);
alter table private.game_character_paired_snapshot_states enable row level security;
alter table private.game_character_paired_snapshot_commands enable row level security;
drop trigger if exists paired_snapshot_command_immutable on private.game_character_paired_snapshot_commands;
create trigger paired_snapshot_command_immutable before update or delete on private.game_character_paired_snapshot_commands
  for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();

create or replace function private.commit_paired_snapshot_candidate(
  p_character uuid,p_command uuid,p_expected_revision bigint,p_player bytea,p_bank bytea)
returns table(outcome text,committed_revision bigint)
language plpgsql security invoker set search_path=pg_catalog,private as $$
declare current_state private.game_character_paired_snapshot_states%rowtype;
  prior private.game_character_paired_snapshot_commands%rowtype;
begin
  if p_character is null or p_command is null or p_expected_revision is null
     or p_expected_revision not between 0 and 9223372036854775805 or p_player is null or p_bank is null then
    raise exception using errcode='22023',message='invalid paired snapshot arguments';
  end if;
  -- One pair revision serializes both snapshots; never lock/update them separately.
  select * into current_state from private.game_character_paired_snapshot_states
    where character_id=p_character for update;
  if not found then raise exception using errcode='P0001',message='paired snapshot baseline missing'; end if;
  select * into prior from private.game_character_paired_snapshot_commands
    where character_id=p_character and command_id=p_command;
  if found then
    if prior.expected_revision=p_expected_revision and prior.player_payload=p_player and prior.bank_payload=p_bank then
      return query select 'EXACT_RETRY'::text,prior.committed_revision; return;
    end if;
    raise exception using errcode='P0001',message='paired snapshot command conflict';
  end if;
  if current_state.revision<>p_expected_revision then
    raise exception using errcode='40001',message='paired snapshot revision changed';
  end if;
  update private.game_character_paired_snapshot_states set revision=p_expected_revision+1,
    player_payload=p_player,bank_payload=p_bank where character_id=p_character;
  insert into private.game_character_paired_snapshot_commands(character_id,command_id,expected_revision,committed_revision,player_payload,bank_payload)
    values(p_character,p_command,p_expected_revision,p_expected_revision+1,p_player,p_bank);
  return query select 'COMMITTED'::text,p_expected_revision+1;
end $$;
revoke all on table private.game_character_paired_snapshot_states,private.game_character_paired_snapshot_commands
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.commit_paired_snapshot_candidate(uuid,uuid,bigint,bytea,bytea)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
