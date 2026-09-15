-- 2026-09-26: make the relay-safe fulfillment writer's terminal states
-- explicit.  The relay still supplies only a character and one immutable
-- artifact command identity; this migration never scans for a candidate.

create or replace function private.fulfill_game_character_onboarding_snapshot_eligibility(
  p_character_id uuid,
  p_artifact_command_id uuid
)
returns table (outcome text)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_correlation_id uuid;
  v_intent private.game_character_onboarding_intents%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
  v_character public.game_characters%rowtype;
  v_outbox private.game_character_onboarding_snapshot_eligibility_outbox%rowtype;
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
  v_artifact private.game_character_player_snapshot_v1_artifacts%rowtype;
  v_fulfillment private.game_character_onboarding_snapshot_fulfillments%rowtype;
  v_intent_found boolean;
  v_handoff_found boolean;
  v_character_found boolean;
  v_outbox_found boolean;
  v_artifact_found boolean;
  v_receipt_found boolean;
  v_manifest_found boolean;
  v_fulfillment_found boolean;
  v_fulfilled_at timestamptz := clock_timestamp();
begin
  if session_user <> 'mud_writer_login'
     or current_setting('role', true) is distinct from 'mud_writer' then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot fulfillment writer session identity is invalid';
  end if;
  if p_character_id is null or p_artifact_command_id is null then
    raise exception using errcode = '22023', message = 'onboarding snapshot fulfillment requires character and artifact command';
  end if;

  -- This resolves one durable active relation, not a candidate selected from
  -- artifact timing, names, or hashes.  Its absence is normal delivery state.
  select h.correlation_id into v_correlation_id
    from private.game_character_onboarding_handoffs h
    join private.game_character_onboarding_snapshot_eligibility_outbox o
      on o.correlation_id = h.correlation_id
   where h.character_id = p_character_id
     and o.character_id = p_character_id
     and h.status = 'activated'
     and h.activated_at is not null
     and o.status in ('pending', 'fulfilled');
  if v_correlation_id is null then
    return query select 'NOT_ELIGIBLE'::text;
    return;
  end if;

  -- Keep the activation lock order: intent -> handoff -> character -> outbox.
  select * into v_intent from private.game_character_onboarding_intents
   where correlation_id = v_correlation_id for share;
  v_intent_found := found;
  select * into v_handoff from private.game_character_onboarding_handoffs
   where correlation_id = v_correlation_id for share;
  v_handoff_found := found;
  select * into v_character from public.game_characters
   where id = p_character_id for share;
  v_character_found := found;
  select * into v_outbox from private.game_character_onboarding_snapshot_eligibility_outbox
   where correlation_id = v_correlation_id for update;
  v_outbox_found := found;

  if not v_intent_found or not v_handoff_found or not v_character_found or not v_outbox_found
     or v_handoff.actor_user_id <> v_outbox.actor_user_id
     or v_handoff.character_id <> v_outbox.character_id
     or v_handoff.mode <> v_outbox.mode
     or v_intent.actor_user_id <> v_outbox.actor_user_id
     or v_intent.mode <> v_outbox.mode
     or v_intent.status <> 'finalized'
     or v_handoff.status <> 'activated'
     or v_handoff.activated_at is null
     or v_outbox.character_id <> p_character_id then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility relation is inconsistent';
  end if;
  if v_character.owner_user_id <> v_outbox.actor_user_id
     or v_character.lifecycle <> 'active'
     or v_character.legacy_name <> private.game_identity_canonical_legacy_name(v_character.legacy_name)
     or v_character.legacy_name_key <> v_character.legacy_name
     or v_character.world_id is null
     or v_character.storage_format is null
     or v_character.storage_format <= 0 then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility character is inconsistent';
  end if;

  -- The supplied command is only a candidate.  Missing, stale, or
  -- nonmatching candidate evidence is an ordinary NOT_ELIGIBLE result.
  select * into v_artifact from private.game_character_player_snapshot_v1_artifacts
   where character_id = v_outbox.character_id and command_id = p_artifact_command_id for share;
  v_artifact_found := found;
  select * into v_receipt from private.game_character_shadow_receipts
   where character_id = v_outbox.character_id and command_id = p_artifact_command_id for share;
  v_receipt_found := found;
  select * into v_manifest from private.game_character_m4_file_snapshot_manifests
   where character_id = v_outbox.character_id and command_id = p_artifact_command_id for share;
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
    return query select 'NOT_ELIGIBLE'::text;
    return;
  end if;

  select * into v_fulfillment from private.game_character_onboarding_snapshot_fulfillments
   where correlation_id = v_correlation_id for share;
  v_fulfillment_found := found;

  if v_outbox.status = 'pending' and v_outbox.fulfilled_at is null then
    if v_fulfillment_found then
      raise exception using errcode = 'P0001', message = 'pending onboarding snapshot eligibility has an immutable fulfillment receipt';
    end if;
    insert into private.game_character_onboarding_snapshot_fulfillments(
      correlation_id, actor_user_id, character_id, mode, world_id, legacy_name_key,
      storage_format, artifact_command_id, receipt_request_sha256,
      receipt_acknowledged_at, snapshot_sha256, snapshot_octets, fulfilled_at
    ) values (
      v_correlation_id, v_outbox.actor_user_id, v_outbox.character_id, v_outbox.mode,
      v_character.world_id, v_character.legacy_name_key, v_character.storage_format,
      p_artifact_command_id, v_receipt.request_sha256, v_receipt.acknowledged_at,
      v_artifact.snapshot_sha256, v_artifact.snapshot_octets, v_fulfilled_at
    );
    update private.game_character_onboarding_snapshot_eligibility_outbox
       set status = 'fulfilled', fulfilled_at = v_fulfilled_at
     where correlation_id = v_correlation_id;
    return query select 'FULFILLED'::text;
    return;
  end if;

  if v_outbox.status <> 'fulfilled' or v_outbox.fulfilled_at is null
     or not v_fulfillment_found
     or v_fulfillment.actor_user_id <> v_outbox.actor_user_id
     or v_fulfillment.character_id <> v_outbox.character_id
     or v_fulfillment.mode <> v_outbox.mode
     or v_fulfillment.world_id <> v_character.world_id
     or v_fulfillment.legacy_name_key <> v_character.legacy_name_key
     or v_fulfillment.storage_format <> v_character.storage_format
     or v_fulfillment.fulfilled_at <> v_outbox.fulfilled_at then
    raise exception using errcode = 'P0001', message = 'fulfilled onboarding snapshot eligibility is inconsistent';
  end if;
  if v_fulfillment.artifact_command_id = p_artifact_command_id
     and v_fulfillment.receipt_request_sha256 = v_receipt.request_sha256
     and v_fulfillment.receipt_acknowledged_at = v_receipt.acknowledged_at
     and v_fulfillment.snapshot_sha256 = v_artifact.snapshot_sha256
     and v_fulfillment.snapshot_octets = v_artifact.snapshot_octets then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;
  return query select 'ALREADY_FULFILLED'::text;
end;
$$;

-- An activation replay may see a fulfilled outbox, but only if the one
-- immutable receipt still proves the entire completed handoff tuple.
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

  select * into v_intent from private.game_character_onboarding_intents
   where correlation_id = p_correlation_id for update;
  v_intent_found := found;
  select * into v_handoff from private.game_character_onboarding_handoffs
   where correlation_id = p_correlation_id for update;
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

  select * into v_eligibility from private.game_character_onboarding_snapshot_eligibility_outbox
   where correlation_id = p_correlation_id for update;
  v_eligibility_found := found;
  if v_eligibility_found then
    select * into v_fulfillment from private.game_character_onboarding_snapshot_fulfillments
     where correlation_id = p_correlation_id for share;
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
      select * into v_artifact from private.game_character_player_snapshot_v1_artifacts
       where character_id = p_character_id and command_id = v_fulfillment.artifact_command_id for share;
      v_artifact_found := found;
      select * into v_receipt from private.game_character_shadow_receipts
       where character_id = p_character_id and command_id = v_fulfillment.artifact_command_id for share;
      v_receipt_found := found;
      select * into v_manifest from private.game_character_m4_file_snapshot_manifests
       where character_id = p_character_id and command_id = v_fulfillment.artifact_command_id for share;
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
  update private.game_character_onboarding_handoffs
     set status = 'activated', activated_at = v_activated_at
   where correlation_id = p_correlation_id;
  insert into public.game_identity_events(character_id, actor_user_id, event_type, correlation_id, details)
  values (p_character_id, p_actor_user_id, 'game_character_onboarding_activated', p_correlation_id,
    jsonb_build_object('mode', p_mode, 'source', 'c-write-callback'));
  return query select v_character.id, v_character.owner_user_id, p_correlation_id,
    v_character.lifecycle, 'finalized'::text;
end;
$$;

revoke all on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid, uuid)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid, uuid)
  to mud_writer;

comment on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid, uuid) is
  'mud_writer_login SET ROLE mud_writer only. Uses the supplied exact immutable artifact identity without candidate inference and returns FULFILLED, EXACT_RETRY, ALREADY_FULFILLED, or NOT_ELIGIBLE; inconsistent durable state raises.';
comment on function public.activate_game_character_onboarding_handoff(uuid, uuid, uuid, text) is
  'Gateway service_role only. Exact activation retries validate the pending outbox or, when fulfilled, its one immutable receipt and artifact/receipt facts before returning the active handoff.';
