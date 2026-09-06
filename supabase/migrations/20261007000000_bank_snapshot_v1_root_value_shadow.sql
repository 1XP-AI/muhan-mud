-- Detached audit projection only. This does not load, persist, or authorize a
-- bank; the root value is accepted only after M3 receipt and M4 manifest evidence.
create table if not exists private.game_character_bank_snapshot_v1_root_value_shadows (
  character_id uuid not null, command_id uuid not null, receipt_request_sha256 text not null,
  writer_instance_id uuid not null, writer_epoch bigint not null, writer_revision bigint not null,
  source_post_sha256 text not null, source_octets bigint not null, bank_sha256 text not null,
  bank_octets bigint not null, root_value bigint not null, recorded_at timestamptz not null default clock_timestamp(),
  primary key(character_id, command_id), unique(character_id, writer_revision),
  foreign key(character_id, command_id) references private.game_character_m4_file_snapshot_manifests(character_id, command_id) on delete restrict,
  check(receipt_request_sha256 ~ '^[0-9a-f]{64}$'), check(writer_epoch between 1 and 9223372036854775807), check(writer_revision between 1 and 9223372036854775807),
  check(source_post_sha256 ~ '^[0-9a-f]{64}$'), check(source_octets between 1 and 9223372036854775807),
  check(bank_sha256 ~ '^[0-9a-f]{64}$'), check(bank_octets between 48 and 4194304)
);
alter table private.game_character_bank_snapshot_v1_root_value_shadows enable row level security;
create or replace function private.bank_snapshot_v1_root_value_shadow_immutable() returns trigger language plpgsql security invoker set search_path=pg_catalog as $$ begin raise exception using errcode='P0001',message='BankSnapshotV1 root-value shadows are immutable'; end $$;
drop trigger if exists game_character_bank_snapshot_v1_root_value_shadows_immutable on private.game_character_bank_snapshot_v1_root_value_shadows;
create trigger game_character_bank_snapshot_v1_root_value_shadows_immutable before update or delete on private.game_character_bank_snapshot_v1_root_value_shadows for each row execute function private.bank_snapshot_v1_root_value_shadow_immutable();

create or replace function private.record_bank_snapshot_v1_root_value_shadow_for_receipt(p_character_id uuid,p_command_id uuid,p_receipt_request_sha256 text,p_source_post_sha256 text,p_source_octets bigint,p_bank_sha256 text,p_bank_octets bigint,p_root_value bigint)
returns table(outcome text) language plpgsql security definer set search_path=pg_catalog,private as $$
declare r private.game_character_shadow_receipts%rowtype; m private.game_character_m4_file_snapshot_manifests%rowtype; s private.game_character_bank_snapshot_v1_root_value_shadows%rowtype;
begin
  if session_user <> 'mud_writer_login' or current_setting('role',true) is distinct from 'mud_writer' then raise exception using errcode='P0001',message='BankSnapshotV1 root-value writer session identity is invalid'; end if;
  if p_character_id is null or p_command_id is null or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$' or p_source_post_sha256 !~ '^[0-9a-f]{64}$' or p_bank_sha256 !~ '^[0-9a-f]{64}$' or p_source_octets not between 1 and 9223372036854775807 or p_bank_octets not between 48 and 4194304 then raise exception using errcode='22023',message='BankSnapshotV1 root-value arguments are invalid'; end if;
  select * into r from private.game_character_shadow_receipts where character_id=p_character_id and command_id=p_command_id for share;
  select * into m from private.game_character_m4_file_snapshot_manifests where character_id=p_character_id and command_id=p_command_id for share;
  if r.character_id is null or m.character_id is null or r.request_sha256<>p_receipt_request_sha256 or r.post_sha256<>p_source_post_sha256 or m.world_id<>r.world_id or m.legacy_name_key<>r.legacy_name_key or m.receipt_request_sha256<>r.request_sha256 or m.writer_instance_id<>r.writer_instance_id or m.writer_epoch<>r.writer_epoch or m.writer_revision<>r.writer_revision or m.file_post_sha256<>r.post_sha256 or m.storage_format<>r.storage_format or m.receipt_acknowledged_at<>r.acknowledged_at or m.snapshot_format<>'legacy-file-manifest-v1' or m.snapshot_sha256<>r.post_sha256 or m.snapshot_octets<>p_source_octets then raise exception using errcode='P0001',message='BankSnapshotV1 root-value shadow has no matching M3 receipt and M4 manifest evidence'; end if;
  insert into private.game_character_bank_snapshot_v1_root_value_shadows(character_id,command_id,receipt_request_sha256,writer_instance_id,writer_epoch,writer_revision,source_post_sha256,source_octets,bank_sha256,bank_octets,root_value) values(p_character_id,p_command_id,r.request_sha256,r.writer_instance_id,r.writer_epoch,r.writer_revision,r.post_sha256,p_source_octets,p_bank_sha256,p_bank_octets,p_root_value) on conflict do nothing returning * into s;
  if found then return query select 'RECORDED'::text; return; end if;
  select * into s from private.game_character_bank_snapshot_v1_root_value_shadows where character_id=p_character_id and command_id=p_command_id for share;
  if s.receipt_request_sha256=r.request_sha256 and s.writer_instance_id=r.writer_instance_id and s.writer_epoch=r.writer_epoch and s.writer_revision=r.writer_revision and s.source_post_sha256=r.post_sha256 and s.source_octets=p_source_octets and s.bank_sha256=p_bank_sha256 and s.bank_octets=p_bank_octets and s.root_value=p_root_value then return query select 'EXACT_RETRY'::text; return; end if;
  raise exception using errcode='P0001',message='BankSnapshotV1 root-value shadow conflicts with immutable evidence';
end $$;
revoke all on table private.game_character_bank_snapshot_v1_root_value_shadows from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
revoke all on function private.record_bank_snapshot_v1_root_value_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,bigint) from public,anon,authenticated,service_role,mud_writer,mud_writer_login;
grant execute on function private.record_bank_snapshot_v1_root_value_shadow_for_receipt(uuid,uuid,text,text,bigint,text,bigint,bigint) to mud_writer;
