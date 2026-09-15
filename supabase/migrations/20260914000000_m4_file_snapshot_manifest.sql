-- 2026-09-14: receipt-anchored M4 file snapshot manifest evidence.
--
-- This migration is deliberately manifest-only. It records the identity,
-- writer tuple, digest, and length of a legacy player file that M3 already
-- acknowledged. It stores no player bytes and cannot change gameplay,
-- identity, lifecycle, M3 receipt/head state, or the legacy file.

create table if not exists private.game_character_m4_file_snapshot_manifests (
  character_id uuid not null,
  command_id uuid not null,
  world_id text not null,
  legacy_name_key text not null,
  receipt_request_sha256 text not null,
  writer_instance_id uuid not null,
  writer_epoch bigint not null,
  writer_revision bigint not null,
  file_post_sha256 text not null,
  storage_format smallint not null,
  receipt_acknowledged_at timestamptz not null,
  snapshot_format text not null,
  snapshot_sha256 text not null,
  snapshot_octets bigint not null,
  recorded_at timestamptz not null default clock_timestamp(),
  constraint game_character_m4_file_snapshot_manifests_pk
    primary key (character_id, command_id),
  constraint game_character_m4_file_snapshot_manifests_revision_unique
    unique (character_id, writer_revision),
  constraint game_character_m4_file_snapshot_manifests_receipt_fk
    foreign key (character_id, command_id)
    references private.game_character_shadow_receipts(character_id, command_id)
    on delete restrict,
  constraint game_character_m4_file_snapshot_manifests_request_hash_check
    check (receipt_request_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_m4_file_snapshot_manifests_writer_epoch_check
    check (writer_epoch between 1 and 9223372036854775807),
  constraint game_character_m4_file_snapshot_manifests_writer_revision_check
    check (writer_revision between 1 and 9223372036854775807),
  constraint game_character_m4_file_snapshot_manifests_file_hash_check
    check (file_post_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_m4_file_snapshot_manifests_storage_format_check
    check (storage_format > 0),
  constraint game_character_m4_file_snapshot_manifests_snapshot_format_check
    check (snapshot_format = 'legacy-file-manifest-v1'),
  constraint game_character_m4_file_snapshot_manifests_snapshot_hash_check
    check (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_m4_file_snapshot_manifests_same_hash_check
    check (snapshot_sha256 = file_post_sha256),
  constraint game_character_m4_file_snapshot_manifests_octets_check
    check (snapshot_octets between 1 and 67108864)
);

alter table private.game_character_m4_file_snapshot_manifests
  enable row level security;

create or replace function private.m4_file_snapshot_manifest_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = 'P0001',
    message = 'M4 file snapshot manifests are immutable';
end;
$$;

drop trigger if exists game_character_m4_file_snapshot_manifests_immutable
  on private.game_character_m4_file_snapshot_manifests;
create trigger game_character_m4_file_snapshot_manifests_immutable
  before update or delete on private.game_character_m4_file_snapshot_manifests
  for each row execute function private.m4_file_snapshot_manifest_immutable();

create or replace function private.record_m4_file_snapshot_manifest_for_receipt(
  p_character_id uuid,
  p_command_id uuid,
  p_receipt_request_sha256 text,
  p_snapshot_format text,
  p_snapshot_sha256 text,
  p_snapshot_octets bigint
)
returns table (outcome text)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
begin
  -- A nested SECURITY INVOKER call cannot use m3_assert_writer_session():
  -- SECURITY DEFINER has already changed current_user to the function owner.
  -- session_user and the actual SET ROLE value survive that boundary, so this
  -- is the same mud_writer_login -> mud_writer assertion used by M3.
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using
      errcode = 'P0001',
      message = 'M4 writer session identity is invalid';
  end if;

  if p_character_id is null
     or p_command_id is null
     or p_receipt_request_sha256 is null
     or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$'
     or p_snapshot_format is null
     or p_snapshot_format <> 'legacy-file-manifest-v1'
     or p_snapshot_sha256 is null
     or p_snapshot_sha256 !~ '^[0-9a-f]{64}$'
     or p_snapshot_octets is null
     or p_snapshot_octets not between 1 and 67108864 then
    raise exception using
      errcode = '22023',
      message = 'M4 snapshot manifest arguments are invalid';
  end if;

  -- FOR SHARE prevents a non-key receipt mutation while the complete receipt
  -- tuple is copied into immutable M4 evidence.
  select * into v_receipt
    from private.game_character_shadow_receipts
   where character_id = p_character_id
     and command_id = p_command_id
   for share;

  if not found
     or v_receipt.request_sha256 <> p_receipt_request_sha256
     or v_receipt.post_sha256 <> p_snapshot_sha256 then
    raise exception using
      errcode = 'P0001',
      message = 'M4 snapshot manifest has no matching receipt evidence';
  end if;

  -- ON CONFLICT is intentional. Two callers may both observe no manifest;
  -- one inserts, while the other waits and then verifies the committed winner
  -- in a new statement snapshot. No unique violation escapes an exact retry.
  insert into private.game_character_m4_file_snapshot_manifests (
    character_id,
    command_id,
    world_id,
    legacy_name_key,
    receipt_request_sha256,
    writer_instance_id,
    writer_epoch,
    writer_revision,
    file_post_sha256,
    storage_format,
    receipt_acknowledged_at,
    snapshot_format,
    snapshot_sha256,
    snapshot_octets
  ) values (
    p_character_id,
    p_command_id,
    v_receipt.world_id,
    v_receipt.legacy_name_key,
    v_receipt.request_sha256,
    v_receipt.writer_instance_id,
    v_receipt.writer_epoch,
    v_receipt.writer_revision,
    v_receipt.post_sha256,
    v_receipt.storage_format,
    v_receipt.acknowledged_at,
    p_snapshot_format,
    p_snapshot_sha256,
    p_snapshot_octets
  )
  on conflict do nothing
  returning * into v_manifest;

  if found then
    return query select 'RECORDED'::text;
    return;
  end if;

  select * into v_manifest
    from private.game_character_m4_file_snapshot_manifests
   where character_id = p_character_id
     and command_id = p_command_id
   for share;

  if found
     and v_manifest.world_id = v_receipt.world_id
     and v_manifest.legacy_name_key = v_receipt.legacy_name_key
     and v_manifest.receipt_request_sha256 = v_receipt.request_sha256
     and v_manifest.writer_instance_id = v_receipt.writer_instance_id
     and v_manifest.writer_epoch = v_receipt.writer_epoch
     and v_manifest.writer_revision = v_receipt.writer_revision
     and v_manifest.file_post_sha256 = v_receipt.post_sha256
     and v_manifest.storage_format = v_receipt.storage_format
     and v_manifest.receipt_acknowledged_at = v_receipt.acknowledged_at
     and v_manifest.snapshot_format = p_snapshot_format
     and v_manifest.snapshot_sha256 = p_snapshot_sha256
     and v_manifest.snapshot_octets = p_snapshot_octets then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;

  raise exception using
    errcode = 'P0001',
    message = 'M4 snapshot manifest conflicts with immutable evidence';
end;
$$;

create or replace function private.list_m4_file_snapshot_manifest_reconciliation(
  p_world_id text,
  p_limit integer default 100
)
returns table (
  character_id uuid,
  command_id uuid,
  legacy_name_key text,
  writer_instance_id uuid,
  writer_epoch bigint,
  writer_revision bigint,
  receipt_request_sha256 text,
  file_post_sha256 text,
  storage_format smallint,
  receipt_acknowledged_at timestamptz,
  manifest_state text,
  snapshot_format text,
  snapshot_sha256 text,
  snapshot_octets bigint,
  manifest_recorded_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
begin
  if p_world_id is null
     or not private.m3_shadow_valid_world(p_world_id)
     or p_limit is null
     or p_limit not between 1 and 1000 then
    raise exception using
      errcode = '22023',
      message = 'M4 reconciliation arguments are invalid';
  end if;

  return query
    select
      r.character_id,
      r.command_id,
      r.legacy_name_key,
      r.writer_instance_id,
      r.writer_epoch,
      r.writer_revision,
      r.request_sha256,
      r.post_sha256,
      r.storage_format,
      r.acknowledged_at,
      case
        when m.character_id is null then 'MISSING'
        when m.world_id = r.world_id
         and m.legacy_name_key = r.legacy_name_key
         and m.receipt_request_sha256 = r.request_sha256
         and m.writer_instance_id = r.writer_instance_id
         and m.writer_epoch = r.writer_epoch
         and m.writer_revision = r.writer_revision
         and m.file_post_sha256 = r.post_sha256
         and m.storage_format = r.storage_format
         and m.receipt_acknowledged_at = r.acknowledged_at
         and m.snapshot_format = 'legacy-file-manifest-v1'
         and m.snapshot_sha256 = r.post_sha256
         and m.snapshot_octets between 1 and 67108864
          then 'EXACT'
        else 'INCONSISTENT'
      end,
      m.snapshot_format,
      m.snapshot_sha256,
      m.snapshot_octets,
      m.recorded_at
    from private.game_character_shadow_receipts r
    left join private.game_character_m4_file_snapshot_manifests m
      on m.character_id = r.character_id
     and m.command_id = r.command_id
   where r.world_id = p_world_id
   order by r.character_id, r.writer_revision, r.command_id
   limit p_limit;
end;
$$;

-- Creating a login/capability for reconciliation is intentionally deferred
-- until a real operator process exists. PostgreSQL owner/admin may execute
-- the read-only report during this manifest-only stage.
grant usage on schema private to mud_writer;
revoke all on table private.game_character_m4_file_snapshot_manifests
  from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.m4_file_snapshot_manifest_immutable()
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.list_m4_file_snapshot_manifest_reconciliation(text,integer)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)
  to mud_writer;

comment on table private.game_character_m4_file_snapshot_manifests is
  'Immutable, receipt-anchored legacy-file-manifest-v1 metadata. No player bytes; never a gameplay or recovery authority.';
comment on function private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint) is
  'mud_writer session only. Records or exactly retries one manifest for an acknowledged M3 receipt without changing M3 or gameplay state.';
comment on function private.list_m4_file_snapshot_manifest_reconciliation(text,integer) is
  'Owner/admin-only read report of MISSING, EXACT, or INCONSISTENT receipt-to-manifest metadata; never returns player bytes or repairs state.';
