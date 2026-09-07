-- Internal baseline enrollment, not a live authority switch. Runtime grants
-- stay closed until legacy writes can be quiesced and the capture is audited.
create table if not exists private.game_character_paired_snapshot_baselines (
  character_id uuid primary key references private.game_character_paired_snapshot_states(character_id) on delete restrict,
  command_id uuid not null,
  receipt_request_sha256 text not null,
  source_revision bigint not null,
  player_sha256 text not null,
  bank_sha256 text not null,
  enrolled_at timestamptz not null default clock_timestamp(),
  foreign key(character_id,command_id) references private.game_character_player_snapshot_v1_artifacts(character_id,command_id) on delete restrict,
  foreign key(character_id,command_id) references private.game_character_bank_snapshot_v1_payloads(character_id,command_id) on delete restrict
);
alter table private.game_character_paired_snapshot_baselines enable row level security;
drop trigger if exists paired_baseline_immutable on private.game_character_paired_snapshot_baselines;
create trigger paired_baseline_immutable before update or delete on private.game_character_paired_snapshot_baselines
  for each row execute function private.bank_snapshot_v1_topology_shadow_immutable();

create or replace function private.enroll_paired_snapshot_baseline(p_character uuid,p_command uuid,p_request text)
returns text language plpgsql security invoker set search_path=pg_catalog,private as $$
declare c public.game_characters%rowtype; h private.game_character_legacy_heads%rowtype;
  r private.game_character_shadow_receipts%rowtype;
  p private.game_character_player_snapshot_v1_artifacts%rowtype;
  b private.game_character_bank_snapshot_v1_payloads%rowtype;
  t private.game_character_bank_snapshot_v1_topology_shadows%rowtype;
  prior private.game_character_paired_snapshot_baselines%rowtype;
begin
  if p_character is null or p_command is null or p_request is null or p_request !~ '^[0-9a-f]{64}$' then
    raise exception using errcode='22023',message='invalid paired baseline arguments';
  end if;
  -- Serializes first enrollment even when there is no pair row yet.
  select * into c from public.game_characters where id=p_character for update;
  select * into h from private.game_character_legacy_heads where character_id=p_character for share;
  select * into r from private.game_character_shadow_receipts where character_id=p_character and command_id=p_command;
  select * into p from private.game_character_player_snapshot_v1_artifacts where character_id=p_character and command_id=p_command;
  select * into b from private.game_character_bank_snapshot_v1_payloads where character_id=p_character and command_id=p_command;
  select * into t from private.game_character_bank_snapshot_v1_topology_shadows where character_id=p_character and command_id=p_command;
  if c.id is null or h.character_id is null or r.character_id is null or p.character_id is null or b.character_id is null or t.character_id is null
     or c.lifecycle not in ('active','imported_unclaimed') or c.storage_format<>1
     or r.request_sha256<>p_request or r.world_id<>c.world_id or r.legacy_name_key<>c.legacy_name_key
     or r.storage_format<>c.storage_format
     or substring(p.payload from 24 for 80) is distinct from
        (convert_to(c.legacy_name,'UTF8') || decode(repeat('00',greatest(0,80-octet_length(convert_to(c.legacy_name,'UTF8')))),'hex'))
     or h.head_state<>'existing' or h.head_sha256 is distinct from r.post_sha256
     or h.revision<>r.writer_revision or h.writer_epoch is distinct from r.writer_epoch or h.storage_format<>r.storage_format
     or row(p.world_id,p.legacy_name_key,p.receipt_request_sha256,p.writer_instance_id,p.writer_epoch,p.writer_revision,p.source_post_sha256,p.storage_format)
        is distinct from row(r.world_id,r.legacy_name_key,r.request_sha256,r.writer_instance_id,r.writer_epoch,r.writer_revision,r.post_sha256,r.storage_format)
     or row(t.receipt_request_sha256,t.writer_instance_id,t.writer_epoch,t.writer_revision,t.source_post_sha256,t.source_octets,t.bank_sha256,t.bank_octets)
        is distinct from row(r.request_sha256,r.writer_instance_id,r.writer_epoch,r.writer_revision,r.post_sha256,p.source_octets,b.bank_sha256,octet_length(b.payload)::bigint) then
    raise exception using errcode='P0001',message='paired baseline evidence is missing or stale';
  end if;
  select * into prior from private.game_character_paired_snapshot_baselines where character_id=p_character;
  if found then
    if row(prior.command_id,prior.receipt_request_sha256,prior.source_revision,prior.player_sha256,prior.bank_sha256)
       is distinct from row(p_command,p_request,r.writer_revision,p.snapshot_sha256,b.bank_sha256) then
      raise exception using errcode='P0001',message='paired baseline conflicts';
    end if;
    -- Never overwrite an advanced pair during a retry.
    return 'EXACT_RETRY';
  end if;
  if exists(select 1 from private.game_character_paired_snapshot_states where character_id=p_character) then
    raise exception using errcode='P0001',message='paired state already exists without matching baseline';
  end if;
  insert into private.game_character_paired_snapshot_states values(p_character,0,p.payload,b.payload);
  insert into private.game_character_paired_snapshot_baselines(character_id,command_id,receipt_request_sha256,source_revision,player_sha256,bank_sha256)
    values(p_character,p_command,p_request,r.writer_revision,p.snapshot_sha256,b.bank_sha256);
  return 'ENROLLED';
end $$;
revoke all on table private.game_character_paired_snapshot_baselines from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.enroll_paired_snapshot_baseline(uuid,uuid,text) from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
