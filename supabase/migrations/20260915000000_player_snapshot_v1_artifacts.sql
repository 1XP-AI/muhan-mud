-- 2026-09-15: immutable, receipt-anchored PlayerSnapshotV1 CDTO evidence.
-- This is additive storage evidence only.  It never becomes gameplay or
-- legacy-file authority and it exposes no browser/service-role path.

-- The validator is deliberately structural: it checks the CDTO envelope
-- prefix, player kind, declared payload length, exact total length, and the
-- payload digest.  Game-specific field decoding remains outside PostgreSQL.
create or replace function private.player_snapshot_v1_payload_valid(p_payload bytea)
returns boolean
language plpgsql
immutable
set search_path = pg_catalog, public, private
as $$
declare
  v_payload_length bigint;
  v_total bigint;
begin
  if p_payload is null then return false; end if;
  v_total := octet_length(p_payload);
  if v_total < 48 or v_total > 4194352 then return false; end if;
  if substring(p_payload from 1 for 8) <> decode('4d55484344544f00', 'hex') then return false; end if;
  if get_byte(p_payload, 8) <> 0 or get_byte(p_payload, 9) <> 1 then return false; end if;
  if get_byte(p_payload, 10) <> 0 or get_byte(p_payload, 11) <> 7 then return false; end if;
  v_payload_length :=
      get_byte(p_payload, 12)::bigint * 16777216
    + get_byte(p_payload, 13)::bigint * 65536
    + get_byte(p_payload, 14)::bigint * 256
    + get_byte(p_payload, 15)::bigint;
  if v_payload_length > 4194304 or v_payload_length + 48 <> v_total then return false; end if;
  if public.digest(substring(p_payload from 17 for v_payload_length::integer), 'sha256')
     <> substring(p_payload from (v_total - 31)::integer for 32) then return false; end if;
  return true;
end;
$$;

create table if not exists private.game_character_player_snapshot_v1_artifacts (
  character_id uuid not null,
  command_id uuid not null,
  world_id text not null,
  legacy_name_key text not null,
  receipt_request_sha256 text not null,
  writer_instance_id uuid not null,
  writer_epoch bigint not null,
  writer_revision bigint not null,
  source_post_sha256 text not null,
  source_octets bigint not null,
  storage_format smallint not null,
  receipt_acknowledged_at timestamptz not null,
  snapshot_format text not null,
  snapshot_sha256 text not null,
  snapshot_octets bigint not null,
  payload bytea not null,
  recorded_at timestamptz not null default clock_timestamp(),
  constraint game_character_player_snapshot_v1_artifacts_pk
    primary key (character_id, command_id),
  constraint game_character_player_snapshot_v1_artifacts_revision_unique
    unique (character_id, writer_revision),
  constraint game_character_player_snapshot_v1_artifacts_receipt_fk
    foreign key (character_id, command_id)
    references private.game_character_shadow_receipts(character_id, command_id)
    on delete restrict,
  constraint game_character_player_snapshot_v1_artifacts_request_hash_check
    check (receipt_request_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_artifacts_writer_epoch_check
    check (writer_epoch between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_artifacts_writer_revision_check
    check (writer_revision between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_artifacts_source_hash_check
    check (source_post_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_artifacts_source_octets_check
    check (source_octets between 1 and 9223372036854775807),
  constraint game_character_player_snapshot_v1_artifacts_storage_format_check
    check (storage_format > 0),
  constraint game_character_player_snapshot_v1_artifacts_snapshot_format_check
    check (snapshot_format = 'player-snapshot-v1'),
  constraint game_character_player_snapshot_v1_artifacts_snapshot_hash_check
    check (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_player_snapshot_v1_artifacts_snapshot_octets_check
    check (snapshot_octets between 48 and 4194352),
  constraint game_character_player_snapshot_v1_artifacts_payload_octets_check
    check (octet_length(payload) = snapshot_octets),
  constraint game_character_player_snapshot_v1_artifacts_payload_valid_check
    check (private.player_snapshot_v1_payload_valid(payload))
);

alter table private.game_character_player_snapshot_v1_artifacts enable row level security;

create or replace function private.player_snapshot_v1_artifact_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001', message = 'PlayerSnapshotV1 artifacts are immutable';
end;
$$;

drop trigger if exists game_character_player_snapshot_v1_artifacts_immutable
  on private.game_character_player_snapshot_v1_artifacts;
create trigger game_character_player_snapshot_v1_artifacts_immutable
  before update or delete on private.game_character_player_snapshot_v1_artifacts
  for each row execute function private.player_snapshot_v1_artifact_immutable();

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
    left join private.game_character_player_snapshot_v1_artifacts a
      on a.character_id = r.character_id and a.command_id = r.command_id
   where r.world_id = p_world_id
   order by r.character_id, r.writer_revision, r.command_id
   limit p_limit;
end;
$$;

grant usage on schema private to mud_writer;
revoke all on table private.game_character_player_snapshot_v1_artifacts
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.player_snapshot_v1_payload_valid(bytea)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.player_snapshot_v1_artifact_immutable()
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.list_player_snapshot_v1_artifact_reconciliation(text,integer)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)
  to mud_writer;

comment on table private.game_character_player_snapshot_v1_artifacts is
  'Immutable receipt-anchored PlayerSnapshotV1 CDTO bytes; not gameplay or legacy-file authority.';
comment on function private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea) is
  'mud_writer_login SET ROLE mud_writer only. Records or exactly retries one PlayerSnapshotV1 artifact.';
comment on function private.list_player_snapshot_v1_artifact_reconciliation(text,integer) is
  'Owner/admin-only report of MISSING, EXACT, or INCONSISTENT metadata; never repairs state.';
