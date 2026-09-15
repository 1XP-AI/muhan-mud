-- 2026-09-19: immutable, receipt-bound raw-U8 level evidence.
--
-- This is a derived observation of already validated PlayerSnapshotV1
-- artifacts. It is not gameplay authority, never reads a legacy file, and
-- deliberately applies no level policy beyond preserving the serialized U8.

create table if not exists private.game_character_player_snapshot_v1_level_projections (
  character_id uuid not null,
  command_id uuid not null,
  receipt_request_sha256 text not null,
  writer_instance_id uuid not null,
  writer_epoch bigint not null,
  writer_revision bigint not null,
  source_post_sha256 text not null,
  source_octets bigint not null,
  snapshot_sha256 text not null,
  snapshot_octets bigint not null,
  raw_level_u8 smallint not null,
  recorded_at timestamptz not null default clock_timestamp(),
  constraint game_character_player_snapshot_v1_level_projections_pk
    primary key (character_id, command_id),
  constraint game_character_player_snapshot_v1_level_projecti_7d49ec2c3c
    unique (character_id, writer_revision),
  constraint game_character_player_snapshot_v1_level_projections_artifact_fk
    foreign key (character_id, command_id)
    references private.game_character_player_snapshot_v1_artifacts(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_v1_level_projecti_10acdd79ba
    check (receipt_request_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_level_projecti_2d9b4db49c
    check (writer_epoch between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_level_projecti_38f0fdfccb
    check (writer_revision between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_level_projecti_8a87e5d688
    check (source_post_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_level_projecti_21c88333c6
    check (source_octets between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_level_projecti_d6bfb95736
    check (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_level_projecti_65b1a51934
    check (snapshot_octets between 48 and 4194352),
  constraint game_character_player_snapshot_v1_level_projecti_da595e506a
    check (raw_level_u8 between 0 and 255)
);

alter table private.game_character_player_snapshot_v1_level_projections
  enable row level security;

create or replace function private.player_snapshot_v1_level_projection_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'PlayerSnapshotV1 level projections are immutable';
end;
$$;

drop trigger if exists game_character_player_snapshot_v1_level_projections_immutable
  on private.game_character_player_snapshot_v1_level_projections;
create trigger game_character_player_snapshot_v1_level_projections_immutable
  before update or delete on private.game_character_player_snapshot_v1_level_projections
  for each row execute function private.player_snapshot_v1_level_projection_immutable();

create or replace function private.record_player_snapshot_v1_level_projection_for_receipt(
  p_character_id uuid,
  p_command_id uuid,
  p_receipt_request_sha256 text,
  p_source_post_sha256 text,
  p_source_octets bigint
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
  v_projection private.game_character_player_snapshot_v1_level_projections%rowtype;
  v_raw_level_u8 smallint;
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotV1 level projection writer session identity is invalid';
  end if;
  if p_character_id is null or p_command_id is null
     or p_receipt_request_sha256 is null
     or p_receipt_request_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_post_sha256 is null
     or p_source_post_sha256 !~ '^[0-9a-f]{64}$'
     or p_source_octets is null
     or p_source_octets not between 1 and 9223372036854775807 then
    raise exception using errcode = '22023',
      message = 'PlayerSnapshotV1 level projection arguments are invalid';
  end if;

  -- Read and lock the complete evidence chain. The artifact is immutable and
  -- validated at insertion, but all receipt and source-octet bindings are
  -- checked again before a derived row is persisted.
  select * into v_receipt
    from private.game_character_shadow_receipts
   where character_id = p_character_id and command_id = p_command_id
   for share;
  select * into v_manifest
    from private.game_character_m4_file_snapshot_manifests
   where character_id = p_character_id and command_id = p_command_id
   for share;
  select * into v_artifact
    from private.game_character_player_snapshot_v1_artifacts
   where character_id = p_character_id and command_id = p_command_id
   for share;

  if v_receipt.character_id is null
     or v_manifest.character_id is null
     or v_artifact.character_id is null
     or v_receipt.request_sha256 <> p_receipt_request_sha256
     or v_receipt.post_sha256 <> p_source_post_sha256
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
     or v_manifest.snapshot_octets <> p_source_octets
     or v_artifact.world_id <> v_receipt.world_id
     or v_artifact.legacy_name_key <> v_receipt.legacy_name_key
     or v_artifact.receipt_request_sha256 <> v_receipt.request_sha256
     or v_artifact.writer_instance_id <> v_receipt.writer_instance_id
     or v_artifact.writer_epoch <> v_receipt.writer_epoch
     or v_artifact.writer_revision <> v_receipt.writer_revision
     or v_artifact.source_post_sha256 <> v_receipt.post_sha256
     or v_artifact.source_octets <> v_manifest.snapshot_octets
     or v_artifact.source_octets <> p_source_octets
     or v_artifact.storage_format <> v_receipt.storage_format
     or v_artifact.receipt_acknowledged_at <> v_receipt.acknowledged_at
     or v_artifact.snapshot_format <> 'player-snapshot-v1'
     or v_artifact.snapshot_octets <> octet_length(v_artifact.payload)
     or not private.player_snapshot_v1_payload_valid(v_artifact.payload)
     or v_artifact.snapshot_sha256 <> encode(public.digest(v_artifact.payload, 'sha256'), 'hex') then
    raise exception using errcode = 'P0001',
      message = 'PlayerSnapshotV1 level projection has no matching immutable artifact evidence';
  end if;

  -- PlayerSnapshotV1 is a canonical CDTO envelope: after its 16-byte prefix,
  -- fields 1..6 each have a 7-byte header and values of
  -- 80, 80, 80, 20, 20, and 20 bytes. Field 7 then has its own 7-byte header,
  -- so its U8 value is at zero-based offset 365. get_byte is zero-based.
  -- No policy maps, clamps, or otherwise interprets this serialized value.
  v_raw_level_u8 := get_byte(v_artifact.payload, 365)::smallint;

  insert into private.game_character_player_snapshot_v1_level_projections (
    character_id, command_id, receipt_request_sha256,
    writer_instance_id, writer_epoch, writer_revision,
    source_post_sha256, source_octets, snapshot_sha256, snapshot_octets,
    raw_level_u8
  ) values (
    p_character_id, p_command_id, v_receipt.request_sha256,
    v_receipt.writer_instance_id, v_receipt.writer_epoch, v_receipt.writer_revision,
    v_receipt.post_sha256, v_artifact.source_octets,
    v_artifact.snapshot_sha256, v_artifact.snapshot_octets,
    v_raw_level_u8
  )
  on conflict do nothing
  returning * into v_projection;
  if found then
    return query select 'RECORDED'::text;
    return;
  end if;

  select * into v_projection
    from private.game_character_player_snapshot_v1_level_projections
   where character_id = p_character_id and command_id = p_command_id
   for share;
  if found
     and v_projection.receipt_request_sha256 = v_receipt.request_sha256
     and v_projection.writer_instance_id = v_receipt.writer_instance_id
     and v_projection.writer_epoch = v_receipt.writer_epoch
     and v_projection.writer_revision = v_receipt.writer_revision
     and v_projection.source_post_sha256 = v_receipt.post_sha256
     and v_projection.source_octets = v_artifact.source_octets
     and v_projection.snapshot_sha256 = v_artifact.snapshot_sha256
     and v_projection.snapshot_octets = v_artifact.snapshot_octets
     and v_projection.raw_level_u8 = v_raw_level_u8 then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;

  raise exception using errcode = 'P0001',
    message = 'PlayerSnapshotV1 level projection conflicts with immutable evidence';
end;
$$;

grant usage on schema private to mud_writer;
revoke all on table private.game_character_player_snapshot_v1_level_projections
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
revoke all on function private.player_snapshot_v1_level_projection_immutable()
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
revoke all on function private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
    mud_replay_reader_login;
grant execute on function private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)
  to mud_writer;

comment on table private.game_character_player_snapshot_v1_level_projections is
  'Immutable receipt-bound raw-U8 level projection from validated PlayerSnapshotV1 artifacts; never gameplay or legacy-file authority.';
comment on function private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint) is
  'mud_writer_login SET ROLE mud_writer only. Derives or exactly retries one raw-U8 level projection from receipt/source-octet-bound PlayerSnapshotV1 evidence.';
