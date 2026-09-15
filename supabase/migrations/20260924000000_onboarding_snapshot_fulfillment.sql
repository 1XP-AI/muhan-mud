-- 2026-09-24: fulfill a pending onboarding snapshot eligibility row only
-- from one already-recorded immutable PlayerSnapshotV1 artifact.  This is a
-- database-side acknowledgement of existing evidence; it never transports,
-- creates, or selects a snapshot candidate.

alter table private.game_character_onboarding_snapshot_eligibility_outbox
  add column if not exists fulfilled_at timestamptz;

alter table private.game_character_onboarding_snapshot_eligibility_outbox
  drop constraint if exists game_character_onboarding_snapshot_eligibility_outbox_status;
alter table private.game_character_onboarding_snapshot_eligibility_outbox
  add constraint game_character_onboarding_snapshot_eligibility_outbox_status
    check (
      (status = 'pending' and fulfilled_at is null)
      or (status = 'fulfilled' and fulfilled_at is not null)
    );

create table if not exists private.game_character_onboarding_snapshot_fulfillments (
  correlation_id uuid primary key
    references private.game_character_onboarding_snapshot_eligibility_outbox(correlation_id)
    on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  mode text not null,
  world_id text not null,
  legacy_name_key text not null,
  storage_format smallint not null,
  artifact_command_id uuid not null,
  receipt_request_sha256 text not null,
  receipt_acknowledged_at timestamptz not null,
  snapshot_sha256 text not null,
  snapshot_octets bigint not null,
  fulfilled_at timestamptz not null default clock_timestamp(),
  constraint game_character_onboarding_snapshot_fulfillments_mode
    check (mode in ('provision', 'claim')),
  constraint game_character_onboarding_snapshot_fulfillments_storage_format
    check (storage_format > 0),
  constraint game_character_onboarding_snapshot_fulfillments_request_sha256
    check (receipt_request_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_onboarding_snapshot_fulfillments_snapshot_sha256
    check (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
  constraint game_character_onboarding_snapshot_fulfillments_snapshot_octets
    check (snapshot_octets between 48 and 4194352),
  constraint game_character_onboarding_snapshot_fulfillments_artifact_fk
    foreign key (character_id, artifact_command_id)
    references private.game_character_player_snapshot_v1_artifacts(character_id, command_id)
    on delete restrict
);

alter table private.game_character_onboarding_snapshot_fulfillments
  enable row level security;

create or replace function private.game_character_onboarding_snapshot_fulfillment_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'onboarding snapshot fulfillment receipts are immutable';
end;
$$;

drop trigger if exists game_character_onboarding_snapshot_fulfillments_immutable
  on private.game_character_onboarding_snapshot_fulfillments;
create trigger game_character_onboarding_snapshot_fulfillments_immutable
  before update or delete on private.game_character_onboarding_snapshot_fulfillments
  for each row execute function private.game_character_onboarding_snapshot_fulfillment_immutable();

create or replace function private.fulfill_game_character_onboarding_snapshot_eligibility(
  p_correlation_id uuid,
  p_character_id uuid,
  p_artifact_command_id uuid
)
returns table (outcome text)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_outbox private.game_character_onboarding_snapshot_eligibility_outbox%rowtype;
  v_intent private.game_character_onboarding_intents%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
  v_character public.game_characters%rowtype;
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
  v_artifact private.game_character_player_snapshot_v1_artifacts%rowtype;
  v_fulfillment private.game_character_onboarding_snapshot_fulfillments%rowtype;
  v_handoff_found boolean;
  v_outbox_found boolean;
  v_intent_found boolean;
  v_character_found boolean;
  v_artifact_found boolean;
  v_receipt_found boolean;
  v_manifest_found boolean;
  v_fulfilled_at timestamptz := clock_timestamp();
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot fulfillment writer session identity is invalid';
  end if;
  if p_correlation_id is null or p_character_id is null or p_artifact_command_id is null then
    raise exception using errcode = '22023', message = 'onboarding snapshot fulfillment requires correlation, character, and artifact command';
  end if;

  -- Correlation is resolved through the durable handoff relation, never from
  -- candidate timing, names, hashes, or an artifact scan.
  select * into v_handoff
    from private.game_character_onboarding_handoffs
   where correlation_id = p_correlation_id
   for share;
  v_handoff_found := found;
  select * into v_outbox
    from private.game_character_onboarding_snapshot_eligibility_outbox
   where correlation_id = p_correlation_id
   for update;
  v_outbox_found := found;
  select * into v_intent
    from private.game_character_onboarding_intents
   where correlation_id = p_correlation_id
   for share;
  v_intent_found := found;
  if not v_handoff_found or not v_outbox_found or not v_intent_found
     or v_handoff.actor_user_id <> v_outbox.actor_user_id
     or v_handoff.character_id <> v_outbox.character_id
     or v_handoff.mode <> v_outbox.mode
     or v_intent.actor_user_id <> v_outbox.actor_user_id
     or v_intent.mode <> v_outbox.mode
     or v_intent.status <> 'finalized'
     or v_handoff.status <> 'activated'
     or v_handoff.activated_at is null
     or v_outbox.character_id <> p_character_id then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility does not match an active exact handoff';
  end if;

  select * into v_character
    from public.game_characters
   where id = v_outbox.character_id
   for share;
  v_character_found := found;
  if not v_character_found
     or v_character.owner_user_id <> v_outbox.actor_user_id
     or v_character.lifecycle <> 'active'
     or v_character.legacy_name <> private.game_identity_canonical_legacy_name(v_character.legacy_name)
     or v_character.legacy_name_key <> v_character.legacy_name
     or v_character.world_id is null
     or v_character.storage_format is null
     or v_character.storage_format <= 0 then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility character is inconsistent';
  end if;

  -- The caller supplies one artifact identity.  This lookup is deliberately
  -- exact; no latest/first/hash/name/time candidate selection is permitted.
  select * into v_artifact
    from private.game_character_player_snapshot_v1_artifacts
   where character_id = v_outbox.character_id
     and command_id = p_artifact_command_id
   for share;
  v_artifact_found := found;
  select * into v_receipt
    from private.game_character_shadow_receipts
   where character_id = v_outbox.character_id
     and command_id = p_artifact_command_id
   for share;
  v_receipt_found := found;
  select * into v_manifest
    from private.game_character_m4_file_snapshot_manifests
   where character_id = v_outbox.character_id
     and command_id = p_artifact_command_id
   for share;
  v_manifest_found := found;
  if not v_artifact_found or not v_receipt_found or not v_manifest_found
     or v_artifact.character_id <> v_outbox.character_id
     or v_artifact.command_id <> p_artifact_command_id
     or v_artifact.world_id <> v_character.world_id
     or v_artifact.legacy_name_key <> v_character.legacy_name_key
     or v_artifact.storage_format <> v_character.storage_format
     or v_artifact.snapshot_format <> 'player-snapshot-v1'
     or v_artifact.snapshot_octets <> octet_length(v_artifact.payload)
     or not private.player_snapshot_v1_payload_valid(v_artifact.payload)
     or v_artifact.snapshot_sha256 <> encode(public.digest(v_artifact.payload, 'sha256'), 'hex')
     or v_receipt.world_id <> v_character.world_id
     or v_receipt.legacy_name_key <> v_character.legacy_name_key
     or v_receipt.storage_format <> v_character.storage_format
     or v_artifact.receipt_request_sha256 <> v_receipt.request_sha256
     or v_artifact.writer_instance_id <> v_receipt.writer_instance_id
     or v_artifact.writer_epoch <> v_receipt.writer_epoch
     or v_artifact.writer_revision <> v_receipt.writer_revision
     or v_artifact.source_post_sha256 <> v_receipt.post_sha256
     or v_artifact.receipt_acknowledged_at <> v_receipt.acknowledged_at
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
     or v_manifest.snapshot_octets <> v_artifact.source_octets
     or v_receipt.acknowledged_at < v_outbox.enqueued_at then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility has no exact current artifact receipt binding';
  end if;

  select * into v_fulfillment
    from private.game_character_onboarding_snapshot_fulfillments
   where correlation_id = p_correlation_id
   for share;
  if found then
    if v_outbox.status = 'fulfilled'
       and v_outbox.fulfilled_at is not null
       and v_fulfillment.actor_user_id = v_outbox.actor_user_id
       and v_fulfillment.character_id = v_outbox.character_id
       and v_fulfillment.mode = v_outbox.mode
       and v_fulfillment.world_id = v_character.world_id
       and v_fulfillment.legacy_name_key = v_character.legacy_name_key
       and v_fulfillment.storage_format = v_character.storage_format
       and v_fulfillment.artifact_command_id = p_artifact_command_id
       and v_fulfillment.receipt_request_sha256 = v_receipt.request_sha256
       and v_fulfillment.receipt_acknowledged_at = v_receipt.acknowledged_at
       and v_fulfillment.snapshot_sha256 = v_artifact.snapshot_sha256
       and v_fulfillment.snapshot_octets = v_artifact.snapshot_octets then
      return query select 'EXACT_RETRY'::text;
      return;
    end if;
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility fulfillment conflicts with immutable receipt';
  end if;

  if v_outbox.status <> 'pending' or v_outbox.fulfilled_at is not null then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility is not pending fulfillment';
  end if;

  insert into private.game_character_onboarding_snapshot_fulfillments(
    correlation_id, actor_user_id, character_id, mode, world_id, legacy_name_key,
    storage_format, artifact_command_id, receipt_request_sha256,
    receipt_acknowledged_at, snapshot_sha256, snapshot_octets, fulfilled_at
  ) values (
    p_correlation_id, v_outbox.actor_user_id, v_outbox.character_id, v_outbox.mode,
    v_character.world_id, v_character.legacy_name_key, v_character.storage_format,
    p_artifact_command_id, v_receipt.request_sha256, v_receipt.acknowledged_at,
    v_artifact.snapshot_sha256, v_artifact.snapshot_octets, v_fulfilled_at
  );
  update private.game_character_onboarding_snapshot_eligibility_outbox
     set status = 'fulfilled', fulfilled_at = v_fulfilled_at
   where correlation_id = p_correlation_id;

  return query select 'FULFILLED'::text;
end;
$$;

grant usage on schema private to mud_writer;
revoke all on table private.game_character_onboarding_snapshot_fulfillments
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.game_character_onboarding_snapshot_fulfillment_immutable()
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
revoke all on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid,uuid)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid,uuid)
  to mud_writer;

comment on table private.game_character_onboarding_snapshot_fulfillments is
  'Immutable correlation-keyed receipt binding one active onboarding handoff to one already-recorded PlayerSnapshotV1 artifact; never selects or creates a snapshot.';
comment on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid,uuid) is
  'mud_writer_login SET ROLE mud_writer only. Fulfills exactly one pending active onboarding eligibility row from the supplied immutable PlayerSnapshotV1 artifact/receipt identity, or exactly retries that same fulfillment.';
