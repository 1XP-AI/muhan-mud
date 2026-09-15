-- 2026-09-16: bind immutable PlayerSnapshotV1 source length to its receipt.
-- This replaces only the RPC definitions so pre-existing migrations remain
-- replayable; it neither changes legacy authority nor repairs stored evidence.

create or replace function private.record_player_snapshot_v1_artifact_for_receipt(
  p_character_id uuid,
  p_command_id uuid,
  p_receipt_request_sha256 text,
  p_source_post_sha256 text,
  p_source_octets bigint,
  p_snapshot_format text,
  p_snapshot_sha256 text,
  p_snapshot_octets bigint,
  p_payload bytea
)
returns table (outcome text)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
  v_artifact private.game_character_player_snapshot_v1_artifacts%rowtype;
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001', message = 'PlayerSnapshotV1 writer session identity is invalid';
  end if;
  if p_character_id is null or p_command_id is null
     or p_receipt_request_sha256 is null
     or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_post_sha256 is null
     or p_source_post_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_octets is null
     or p_source_octets not between 1 and 9223372036854775807
     or p_snapshot_format is null
     or p_snapshot_format <> 'player-snapshot-v1'
     or p_snapshot_sha256 is null
     or p_snapshot_sha256 !~ '^[0-9a-f]{64}$'
     or p_snapshot_octets is null
     or p_snapshot_octets not between 48 and 4194352
     or p_payload is null
     or octet_length(p_payload) <> p_snapshot_octets
     or not private.player_snapshot_v1_payload_valid(p_payload)
     or encode(public.digest(p_payload, 'sha256'), 'hex') <> p_snapshot_sha256 then
    raise exception using errcode = '22023', message = 'PlayerSnapshotV1 artifact arguments are invalid';
  end if;

  select * into v_receipt
    from private.game_character_shadow_receipts
   where character_id = p_character_id and command_id = p_command_id
   for share;
  if not found
     or v_receipt.request_sha256 <> p_receipt_request_sha256
     or v_receipt.post_sha256 <> p_source_post_sha256 then
    raise exception using errcode = 'P0001', message = 'PlayerSnapshotV1 artifact has no matching receipt evidence';
  end if;

  select * into v_manifest
    from private.game_character_m4_file_snapshot_manifests
   where character_id = p_character_id and command_id = p_command_id
   for share;
  if not found
     or v_manifest.world_id <> v_receipt.world_id
     or v_manifest.legacy_name_key <> v_receipt.legacy_name_key
     or v_manifest.receipt_request_sha256 <> v_receipt.request_sha256
     or v_manifest.writer_instance_id <> v_receipt.writer_instance_id
     or v_manifest.writer_epoch <> v_receipt.writer_epoch
     or v_manifest.writer_revision <> v_receipt.writer_revision
     or v_manifest.file_post_sha256 <> v_receipt.post_sha256
     or v_manifest.storage_format <> v_receipt.storage_format
     or v_manifest.receipt_acknowledged_at <> v_receipt.acknowledged_at
     or v_manifest.snapshot_format <> 'legacy-file-manifest-v1'
     or v_manifest.snapshot_sha256 <> v_receipt.post_sha256
     or v_manifest.snapshot_octets <> p_source_octets then
    raise exception using errcode = 'P0001', message = 'PlayerSnapshotV1 artifact has no matching acknowledged receipt snapshot';
  end if;

  insert into private.game_character_player_snapshot_v1_artifacts (
    character_id, command_id, world_id, legacy_name_key,
    receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision,
    source_post_sha256, source_octets, storage_format, receipt_acknowledged_at,
    snapshot_format, snapshot_sha256, snapshot_octets, payload
  ) values (
    p_character_id, p_command_id, v_receipt.world_id, v_receipt.legacy_name_key,
    v_receipt.request_sha256, v_receipt.writer_instance_id, v_receipt.writer_epoch,
    v_receipt.writer_revision, v_receipt.post_sha256, p_source_octets,
    v_receipt.storage_format, v_receipt.acknowledged_at, p_snapshot_format,
    p_snapshot_sha256, p_snapshot_octets, p_payload
  )
  on conflict do nothing
  returning * into v_artifact;
  if found then
    return query select 'RECORDED'::text;
    return;
  end if;

  select * into v_artifact
    from private.game_character_player_snapshot_v1_artifacts
   where character_id = p_character_id and command_id = p_command_id
   for share;
  if found
     and v_artifact.world_id = v_receipt.world_id
     and v_artifact.legacy_name_key = v_receipt.legacy_name_key
     and v_artifact.receipt_request_sha256 = v_receipt.request_sha256
     and v_artifact.writer_instance_id = v_receipt.writer_instance_id
     and v_artifact.writer_epoch = v_receipt.writer_epoch
     and v_artifact.writer_revision = v_receipt.writer_revision
     and v_artifact.source_post_sha256 = v_receipt.post_sha256
     and v_artifact.source_octets = v_manifest.snapshot_octets
     and v_artifact.source_octets = p_source_octets
     and v_artifact.storage_format = v_receipt.storage_format
     and v_artifact.receipt_acknowledged_at = v_receipt.acknowledged_at
     and v_artifact.snapshot_format = p_snapshot_format
     and v_artifact.snapshot_sha256 = p_snapshot_sha256
     and v_artifact.snapshot_octets = p_snapshot_octets
     and v_artifact.payload = p_payload then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;
  raise exception using errcode = 'P0001', message = 'PlayerSnapshotV1 artifact conflicts with immutable evidence';
end;
$$;

create or replace function private.list_player_snapshot_v1_artifact_reconciliation(
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
  source_post_sha256 text,
  source_octets bigint,
  storage_format smallint,
  receipt_acknowledged_at timestamptz,
  artifact_state text,
  snapshot_format text,
  snapshot_sha256 text,
  snapshot_octets bigint,
  artifact_recorded_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
begin
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_limit is null or p_limit not between 1 and 1000 then
    raise exception using errcode = '22023', message = 'PlayerSnapshotV1 reconciliation arguments are invalid';
  end if;
  return query
    select r.character_id, r.command_id, r.legacy_name_key,
      r.writer_instance_id, r.writer_epoch, r.writer_revision,
      r.request_sha256, r.post_sha256, a.source_octets, r.storage_format,
      r.acknowledged_at,
      case
        when a.character_id is null then 'MISSING'
        when a.world_id = r.world_id
         and a.legacy_name_key = r.legacy_name_key
         and a.receipt_request_sha256 = r.request_sha256
         and a.writer_instance_id = r.writer_instance_id
         and a.writer_epoch = r.writer_epoch
         and a.writer_revision = r.writer_revision
         and a.source_post_sha256 = r.post_sha256
         and m.world_id = r.world_id
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
         and a.source_octets = m.snapshot_octets
         and a.storage_format = r.storage_format
         and a.receipt_acknowledged_at = r.acknowledged_at
         and a.snapshot_format = 'player-snapshot-v1'
         and a.snapshot_octets = octet_length(a.payload)
         and a.snapshot_octets between 48 and 4194352
         and private.player_snapshot_v1_payload_valid(a.payload)
         and a.snapshot_sha256 = encode(public.digest(a.payload, 'sha256'), 'hex')
          then 'EXACT'
        else 'INCONSISTENT'
      end,
      a.snapshot_format, a.snapshot_sha256, a.snapshot_octets, a.recorded_at
    from private.game_character_shadow_receipts r
    left join private.game_character_m4_file_snapshot_manifests m
      on m.character_id = r.character_id and m.command_id = r.command_id
    left join private.game_character_player_snapshot_v1_artifacts a
      on a.character_id = r.character_id and a.command_id = r.command_id
   where r.world_id = p_world_id
   order by r.character_id, r.writer_revision, r.command_id
   limit p_limit;
end;
$$;

comment on function private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea) is
  'mud_writer_login SET ROLE mud_writer only. Records or exactly retries one PlayerSnapshotV1 artifact bound to its acknowledged receipt octets.';
comment on function private.list_player_snapshot_v1_artifact_reconciliation(text,integer) is
  'Owner/admin-only report of MISSING, EXACT, or INCONSISTENT metadata including receipt-bound source octets; never repairs state.';
