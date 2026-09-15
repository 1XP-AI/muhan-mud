-- Correct SQLSTATE 42702: the TABLE output character_id shadows the head column.
-- Preserve all evidence, ownership, lifecycle and retry gates. Existing databases
-- need a forward migration; CREATE OR REPLACE retains function ownership/grants.
create or replace function private.complete_game_character_provisioning(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_saved_file_sha256 text,
  p_storage_format smallint,
  p_reconcile boolean
)
returns table (
  character_id uuid,
  actor_user_id uuid,
  lifecycle public.character_lifecycle,
  status text,
  saved_file_sha256 text,
  storage_format smallint
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_request private.game_character_provisioning_requests%rowtype;
  v_character public.game_characters%rowtype;
  v_head private.game_character_legacy_heads%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
  v_finalized_at timestamptz := clock_timestamp();
begin
  if p_actor_user_id is null or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'provisioning completion requires actor and correlation id';
  end if;
  if p_saved_file_sha256 is null or p_saved_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_storage_format is null or p_storage_format <= 0 then
    raise exception using errcode = '22023', message = 'provisioning completion requires lowercase SHA-256 and positive storage format';
  end if;

  select * into v_intent from private.game_character_onboarding_intents
   where correlation_id = p_correlation_id for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> 'provision' then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to actor provisioning request';
  end if;
  if not p_reconcile and v_intent.expires_at <= v_finalized_at then
    raise exception using errcode = 'P0001', message = 'onboarding intent has expired before finalize';
  end if;

  select * into v_request from private.game_character_provisioning_requests
   where correlation_id = p_correlation_id for update;
  if not found or v_request.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'provisioning request does not belong to actor';
  end if;

  if v_request.status = 'finalized' then
    if v_request.saved_file_sha256 <> p_saved_file_sha256
       or v_request.storage_format <> p_storage_format then
      raise exception using errcode = 'P0001', message = 'correlation id belongs to a different saved file payload';
    end if;
    select * into v_character from public.game_characters where id = v_request.character_id for update;
    select * into v_handoff from private.game_character_onboarding_handoffs
      where correlation_id = p_correlation_id for update;
    if not found
       or v_handoff.actor_user_id <> p_actor_user_id
       or v_handoff.character_id <> v_request.character_id
       or v_handoff.mode <> 'provision'
       or v_handoff.status not in ('pending', 'activated')
       or v_character.owner_user_id <> p_actor_user_id
       or v_character.lifecycle not in ('handoff_pending', 'active')
       or (v_handoff.status = 'pending' and v_character.lifecycle <> 'handoff_pending')
       or (v_handoff.status = 'activated' and v_character.lifecycle <> 'active')
       or v_intent.status <> 'finalized' then
      raise exception using errcode = 'P0001', message = 'finalized provisioning handoff state is inconsistent';
    end if;
    return query select v_character.id, v_character.owner_user_id, v_character.lifecycle,
      v_request.status, v_request.saved_file_sha256, v_request.storage_format;
    return;
  end if;

  if v_request.status <> 'reserved' or v_intent.status <> 'provisioning' then
    raise exception using errcode = 'P0001', message = 'invalid provisioning lifecycle transition';
  end if;
  select * into v_character from public.game_characters where id = v_request.character_id for update;
  if not found or v_character.owner_user_id <> p_actor_user_id
     or v_character.lifecycle <> 'provisioning' or v_character.world_id <> v_request.world_id
     or v_character.legacy_name_key <> v_request.legacy_name_key then
    raise exception using errcode = 'P0001', message = 'provisioning reservation is inconsistent';
  end if;

  select * into v_head from private.game_character_legacy_heads as head
    where head.character_id = v_character.id for update;
  if found then
    if v_head.revision = 0 and (v_head.head_state <> 'existing'
        or v_head.head_sha256 <> p_saved_file_sha256
        or v_head.storage_format <> p_storage_format or v_head.writer_epoch is not null) then
      raise exception using errcode = 'P0001', message = 'provisioning baseline head conflicts with saved file evidence';
    end if;
  else
    insert into private.game_character_legacy_heads(
      character_id, head_state, head_sha256, storage_format, revision, writer_epoch
    ) values (v_character.id, 'existing', p_saved_file_sha256, p_storage_format, 0, null);
  end if;

  update private.game_character_provisioning_requests
    set status = 'finalized', saved_file_sha256 = p_saved_file_sha256,
        storage_format = p_storage_format, finalized_at = v_finalized_at,
        finalized_via = case when p_reconcile then 'reconcile' else 'finalize' end
    where correlation_id = p_correlation_id;
  update public.game_characters
    set lifecycle = 'handoff_pending', storage_format = p_storage_format, claimed_at = v_finalized_at
    where id = v_character.id;
  update private.game_character_onboarding_intents
    set status = 'finalized', completed_at = v_finalized_at where correlation_id = p_correlation_id;
  insert into private.game_character_onboarding_handoffs(
    correlation_id, actor_user_id, character_id, mode
  ) values (p_correlation_id, p_actor_user_id, v_character.id, 'provision');
  insert into public.game_identity_events(character_id, actor_user_id, event_type, correlation_id, details)
  values (v_character.id, p_actor_user_id, 'game_character_provisioned', p_correlation_id,
    jsonb_build_object('storage_format', p_storage_format,
      'completion_path', case when p_reconcile then 'reconcile' else 'finalize' end,
      'admission', 'handoff_pending'));

  return query select v_character.id, p_actor_user_id, 'handoff_pending'::public.character_lifecycle,
    'finalized'::text, p_saved_file_sha256, p_storage_format;
end;
$$;

-- Activation has the same TABLE-output collision for correlation_id and
-- character_id. Qualify every affected lookup, including exact retry paths.
create or replace function public.activate_game_character_onboarding_handoff(
  p_actor_user_id uuid, p_correlation_id uuid, p_character_id uuid, p_mode text
)
returns table (
  character_id uuid, actor_user_id uuid, correlation_id uuid,
  lifecycle public.character_lifecycle, onboarding_status text
)
language plpgsql security definer set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
  v_character public.game_characters%rowtype;
  v_eligibility private.game_character_onboarding_snapshot_eligibility_outbox%rowtype;
  v_fulfillment private.game_character_onboarding_snapshot_fulfillments%rowtype;
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
  v_artifact private.game_character_player_snapshot_v1_artifacts%rowtype;
  v_intent_found boolean;
  v_handoff_found boolean;
  v_character_found boolean;
  v_eligibility_found boolean;
  v_fulfillment_found boolean;
  v_receipt_found boolean;
  v_manifest_found boolean;
  v_artifact_found boolean;
  v_activated_at timestamptz := clock_timestamp();
begin
  if p_actor_user_id is null or p_correlation_id is null or p_character_id is null
     or p_mode is null or p_mode not in ('provision', 'claim') then
    raise exception using errcode = '22023', message = 'onboarding handoff activation requires actor, correlation, character, and mode';
  end if;

  select * into v_intent from private.game_character_onboarding_intents as row_source
   where row_source.correlation_id = p_correlation_id for update;
  v_intent_found := found;
  select * into v_handoff from private.game_character_onboarding_handoffs as row_source
   where row_source.correlation_id = p_correlation_id for update;
  v_handoff_found := found;
  if not v_intent_found or not v_handoff_found
     or v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> p_mode
     or v_intent.status <> 'finalized' or v_handoff.actor_user_id <> p_actor_user_id
     or v_handoff.character_id <> p_character_id or v_handoff.mode <> p_mode then
    raise exception using errcode = 'P0001', message = 'onboarding handoff does not match exact activation tuple';
  end if;

  select * into v_character from public.game_characters where id = p_character_id for update;
  v_character_found := found;
  if not v_character_found or v_character.owner_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'onboarding handoff character is inconsistent';
  end if;

  select * into v_eligibility from private.game_character_onboarding_snapshot_eligibility_outbox as row_source
   where row_source.correlation_id = p_correlation_id for update;
  v_eligibility_found := found;
  if v_eligibility_found then
    select * into v_fulfillment from private.game_character_onboarding_snapshot_fulfillments as row_source
     where row_source.correlation_id = p_correlation_id for share;
    v_fulfillment_found := found;
    if v_eligibility.actor_user_id <> p_actor_user_id
       or v_eligibility.character_id <> p_character_id
       or v_eligibility.mode <> p_mode then
      raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility outbox is inconsistent';
    end if;
    if v_eligibility.status = 'pending' and v_eligibility.fulfilled_at is null then
      if v_fulfillment_found then
        raise exception using errcode = 'P0001', message = 'pending onboarding snapshot eligibility has an immutable fulfillment receipt';
      end if;
    elsif v_eligibility.status = 'fulfilled' and v_eligibility.fulfilled_at is not null then
      if not v_fulfillment_found
         or v_handoff.status <> 'activated' or v_handoff.activated_at is null
         or v_character.lifecycle <> 'active'
         or v_fulfillment.correlation_id <> p_correlation_id
         or v_fulfillment.actor_user_id <> p_actor_user_id
         or v_fulfillment.character_id <> p_character_id
         or v_fulfillment.mode <> p_mode
         or v_fulfillment.world_id <> v_character.world_id
         or v_fulfillment.legacy_name_key <> v_character.legacy_name_key
         or v_fulfillment.storage_format <> v_character.storage_format
         or v_fulfillment.fulfilled_at <> v_eligibility.fulfilled_at then
        raise exception using errcode = 'P0001', message = 'fulfilled onboarding snapshot eligibility is inconsistent';
      end if;
      select * into v_artifact from private.game_character_player_snapshot_v1_artifacts as row_source
       where row_source.character_id = p_character_id and row_source.command_id = v_fulfillment.artifact_command_id for share;
      v_artifact_found := found;
      select * into v_receipt from private.game_character_shadow_receipts as row_source
       where row_source.character_id = p_character_id and row_source.command_id = v_fulfillment.artifact_command_id for share;
      v_receipt_found := found;
      select * into v_manifest from private.game_character_m4_file_snapshot_manifests as row_source
       where row_source.character_id = p_character_id and row_source.command_id = v_fulfillment.artifact_command_id for share;
      v_manifest_found := found;
      if not v_artifact_found or not v_receipt_found or not v_manifest_found
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
         or v_receipt.acknowledged_at < v_eligibility.enqueued_at
         or v_fulfillment.artifact_command_id <> v_artifact.command_id
         or v_fulfillment.receipt_request_sha256 <> v_receipt.request_sha256
         or v_fulfillment.receipt_acknowledged_at <> v_receipt.acknowledged_at
         or v_fulfillment.snapshot_sha256 <> v_artifact.snapshot_sha256
         or v_fulfillment.snapshot_octets <> v_artifact.snapshot_octets then
        raise exception using errcode = 'P0001', message = 'fulfilled onboarding snapshot eligibility receipt is inconsistent';
      end if;
    else
      raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility outbox is inconsistent';
    end if;
  else
    insert into private.game_character_onboarding_snapshot_eligibility_outbox(
      correlation_id, actor_user_id, character_id, mode
    ) values (p_correlation_id, p_actor_user_id, p_character_id, p_mode);
  end if;

  if v_handoff.status = 'activated' then
    if v_character.lifecycle <> 'active' or v_handoff.activated_at is null then
      raise exception using errcode = 'P0001', message = 'activated onboarding handoff is inconsistent';
    end if;
    return query select v_character.id, v_character.owner_user_id, p_correlation_id,
      v_character.lifecycle, 'finalized'::text;
    return;
  end if;
  if v_handoff.status <> 'pending' or v_character.lifecycle <> 'handoff_pending' then
    raise exception using errcode = 'P0001', message = 'onboarding handoff is not pending activation';
  end if;

  update public.game_characters set lifecycle = 'active' where id = p_character_id returning * into v_character;
  update private.game_character_onboarding_handoffs as row_source
     set status = 'activated', activated_at = v_activated_at
   where row_source.correlation_id = p_correlation_id;
  insert into public.game_identity_events(character_id, actor_user_id, event_type, correlation_id, details)
  values (p_character_id, p_actor_user_id, 'game_character_onboarding_activated', p_correlation_id,
    jsonb_build_object('mode', p_mode, 'source', 'c-write-callback'));
  return query select v_character.id, v_character.owner_user_id, p_correlation_id,
    v_character.lifecycle, 'finalized'::text;
end;
$$;
