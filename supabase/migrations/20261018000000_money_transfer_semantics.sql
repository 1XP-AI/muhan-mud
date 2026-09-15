-- Internal money-only entrypoint. Deliberately no runtime grants yet.
create or replace function private.snapshot_i64(p bytea,off integer) returns numeric
language plpgsql immutable strict set search_path=pg_catalog as $$
declare n numeric:=0; i integer;
begin
  if off<0 or octet_length(p)<off+8 then raise exception using errcode='22023',message='invalid integer offset'; end if;
  for i in 0..7 loop n:=n*256+get_byte(p,off+i); end loop;
  if n>=9223372036854775808 then n:=n-18446744073709551616; end if;
  return n;
end $$;
create or replace function private.money_transfer_pair_valid(
  old_player bytea,old_bank bytea,new_player bytea,new_bank bytea,direction text,amount bigint)
returns boolean language plpgsql immutable set search_path=pg_catalog,private as $$
declare cursor integer:=16; gold_offset integer:=0; old_gold numeric; new_gold numeric;
  old_balance numeric; new_balance numeric; old_graph bytea; new_graph bytea;
begin
  if direction is null or direction not in ('deposit','withdraw') or amount is null or amount<=0
     or private.player_snapshot_v1_payload_valid(old_player) is not true
     or private.player_snapshot_v1_payload_valid(new_player) is not true
     or old_bank is null or new_bank is null then return false; end if;
  perform private.bank_snapshot_v1_payload_nodes(old_bank);
  perform private.bank_snapshot_v1_payload_nodes(new_bank);
  if octet_length(old_player)<>octet_length(new_player) or octet_length(old_bank)<>octet_length(new_bank) then return false; end if;
  while cursor<octet_length(old_player)-32 loop
    if private.player_snapshot_v1_u16(old_player,cursor)=25 then gold_offset:=cursor+7; exit; end if;
    cursor:=cursor+7+private.player_snapshot_v1_u32(old_player,cursor+3)::integer;
  end loop;
  if gold_offset=0 then return false; end if;
  -- Canonical envelopes validated above: ignore only the money bytes and their
  -- recomputed envelope digests. Every other field must remain byte-identical.
  if overlay(substring(old_player from 1 for octet_length(old_player)-32) placing decode(repeat('00',8),'hex') from gold_offset+1 for 8)
    <> overlay(substring(new_player from 1 for octet_length(new_player)-32) placing decode(repeat('00',8),'hex') from gold_offset+1 for 8) then return false; end if;
  old_graph:=substring(old_bank from 24 for octet_length(old_bank)-55);
  new_graph:=substring(new_bank from 24 for octet_length(new_bank)-55);
  if overlay(substring(old_graph from 1 for octet_length(old_graph)-32) placing decode(repeat('00',8),'hex') from 347 for 8)
    <> overlay(substring(new_graph from 1 for octet_length(new_graph)-32) placing decode(repeat('00',8),'hex') from 347 for 8) then return false; end if;
  old_gold:=private.snapshot_i64(old_player,gold_offset); new_gold:=private.snapshot_i64(new_player,gold_offset);
  old_balance:=private.snapshot_i64(old_graph,346); new_balance:=private.snapshot_i64(new_graph,346);
  if least(old_gold,new_gold,old_balance,new_balance)<0 then return false; end if;
  if direction='deposit' then
    return new_gold=old_gold-amount and new_balance=old_balance+amount and new_balance<=300000000;
  end if;
  return new_gold=old_gold+amount and new_balance=old_balance-amount;
end $$;

create table if not exists private.game_character_money_transfer_intents (
  character_id uuid not null,command_id uuid not null,
  direction text not null check(direction in ('deposit','withdraw')),
  amount bigint not null check(amount>0),
  primary key(character_id,command_id),
  foreign key(character_id,command_id) references private.game_character_paired_snapshot_commands(character_id,command_id) on delete restrict
);
alter table private.game_character_money_transfer_intents enable row level security;
drop trigger if exists money_transfer_intent_immutable on private.game_character_money_transfer_intents;
create trigger money_transfer_intent_immutable before update or delete on private.game_character_money_transfer_intents
  for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();
create or replace function private.commit_money_transfer_candidate(
  p_character uuid,p_command uuid,p_expected_revision bigint,p_direction text,p_amount bigint,p_player bytea,p_bank bytea)
returns table(outcome text,committed_revision bigint)
language plpgsql security invoker set search_path=pg_catalog,private as $$
declare state private.game_character_paired_snapshot_states%rowtype;
  intent private.game_character_money_transfer_intents%rowtype;
begin
  if p_character is null or p_command is null or p_direction is null or p_direction not in ('deposit','withdraw') or p_amount is null or p_amount<=0 then
    raise exception using errcode='22023',message='invalid money transfer intent';
  end if;
  select * into state from private.game_character_paired_snapshot_states where character_id=p_character for update;
  if not found then raise exception using errcode='P0001',message='money transfer baseline missing'; end if;
  select * into intent from private.game_character_money_transfer_intents where character_id=p_character and command_id=p_command;
  if found then
    if intent.direction<>p_direction or intent.amount<>p_amount then raise exception using errcode='P0001',message='money transfer intent conflict'; end if;
    return query select * from private.commit_paired_snapshot_candidate(p_character,p_command,p_expected_revision,p_player,p_bank); return;
  end if;
  if exists(select 1 from private.game_character_paired_snapshot_commands where character_id=p_character and command_id=p_command) then
    raise exception using errcode='P0001',message='command belongs to another operation';
  end if;
  if state.revision is distinct from p_expected_revision then raise exception using errcode='40001',message='money transfer revision changed'; end if;
  if private.money_transfer_pair_valid(state.player_payload,state.bank_payload,p_player,p_bank,p_direction,p_amount) is not true then
    raise exception using errcode='22023',message='money transfer changes unsupported state';
  end if;
  return query select * from private.commit_paired_snapshot_candidate(p_character,p_command,p_expected_revision,p_player,p_bank);
  insert into private.game_character_money_transfer_intents values(p_character,p_command,p_direction,p_amount);
end $$;
revoke all on table private.game_character_money_transfer_intents from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.snapshot_i64(bytea,integer),private.money_transfer_pair_valid(bytea,bytea,bytea,bytea,text,bigint),private.commit_money_transfer_candidate(uuid,uuid,bigint,text,bigint,bytea,bytea)
  from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
