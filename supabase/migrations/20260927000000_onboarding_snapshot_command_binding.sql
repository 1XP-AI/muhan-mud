-- 2026-09-27: bind one gateway-authorized snapshot command to the exact
-- activated onboarding tuple before the writer can acknowledge its receipt.
-- The binding is command-keyed rather than inferred from artifact candidates.

create table if not exists private.game_character_onboarding_snapshot_command_bindings (
  command_id uuid primary key,
  correlation_id uuid not null unique
    references private.game_character_onboarding_snapshot_eligibility_outbox(correlation_id)
    on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  mode text not null,
  bound_at timestamptz not null default clock_timestamp(),
  constraint game_character_onboarding_snapshot_command_bindings_mode
    check (mode in ('provision', 'claim'))
);

alter table private.game_character_onboarding_snapshot_command_bindings
  enable row level security;
revoke all on table private.game_character_onboarding_snapshot_command_bindings
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;

create or replace function private.game_character_onboarding_snapshot_command_bindings_immutable()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  raise exception using errcode = 'P0001',
    message = 'onboarding snapshot command bindings are immutable';
end;
$$;

drop trigger if exists game_character_onboarding_snapshot_command_bindings_immutable
  on private.game_character_onboarding_snapshot_command_bindings;
create trigger game_character_onboarding_snapshot_command_bindings_immutable
  before update or delete on private.game_character_onboarding_snapshot_command_bindings
  for each row execute function private.game_character_onboarding_snapshot_command_bindings_immutable();

create or replace function public.register_game_character_onboarding_snapshot_command_binding(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_character_id uuid,
  p_mode text,
  p_command_id uuid
)
returns table (outcome text)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
  v_character public.game_characters%rowtype;
  v_outbox private.game_character_onboarding_snapshot_eligibility_outbox%rowtype;
  v_binding private.game_character_onboarding_snapshot_command_bindings%rowtype;
  v_inserted_command_id uuid;
  v_intent_found boolean;
  v_handoff_found boolean;
  v_character_found boolean;
  v_outbox_found boolean;
begin
  if p_actor_user_id is null or p_correlation_id is null or p_character_id is null
     or p_command_id is null or p_mode is null or p_mode not in ('provision', 'claim') then
    raise exception using errcode = '22023',
      message = 'onboarding snapshot command binding requires actor, correlation, character, mode, and command';
  end if;

  -- Keep the activation lock order, then lock the command binding last.
  select * into v_intent from private.game_character_onboarding_intents
   where correlation_id = p_correlation_id for update;
  v_intent_found := found;
  select * into v_handoff from private.game_character_onboarding_handoffs
   where correlation_id = p_correlation_id for update;
  v_handoff_found := found;
  select * into v_character from public.game_characters
   where id = p_character_id for update;
  v_character_found := found;
  select * into v_outbox from private.game_character_onboarding_snapshot_eligibility_outbox
   where correlation_id = p_correlation_id for update;
  v_outbox_found := found;

  if not v_intent_found or not v_handoff_found or not v_character_found or not v_outbox_found
     or v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> p_mode
     or v_intent.status <> 'finalized'
     or v_handoff.actor_user_id <> p_actor_user_id
     or v_handoff.character_id <> p_character_id
     or v_handoff.mode <> p_mode
     or v_handoff.status <> 'activated'
     or v_handoff.activated_at is null
     or v_character.owner_user_id <> p_actor_user_id
     or v_character.lifecycle <> 'active'
     or v_outbox.actor_user_id <> p_actor_user_id
     or v_outbox.character_id <> p_character_id
     or v_outbox.mode <> p_mode
     or v_outbox.status not in ('pending', 'fulfilled')
     or (v_outbox.status = 'pending' and v_outbox.fulfilled_at is not null)
     or (v_outbox.status = 'fulfilled' and v_outbox.fulfilled_at is null) then
    raise exception using errcode = 'P0001',
      message = 'onboarding snapshot command binding does not match exact activated handoff';
  end if;

  select * into v_binding from private.game_character_onboarding_snapshot_command_bindings
   where correlation_id = p_correlation_id for share;
  if found then
    if v_binding.command_id = p_command_id
       and v_binding.actor_user_id = p_actor_user_id
       and v_binding.character_id = p_character_id
       and v_binding.mode = p_mode then
      return query select 'EXACT_RETRY'::text;
      return;
    end if;
    raise exception using errcode = 'P0001',
      message = 'onboarding snapshot command binding conflicts with immutable exact tuple';
  end if;

  -- A binding must be committed before fulfillment.  Once an unbound legacy
  -- outbox is terminal, adding a late command key would not prove that its
  -- receipt acknowledgement happened after registration.
  if v_outbox.status <> 'pending' or v_outbox.fulfilled_at is not null then
    raise exception using errcode = 'P0001',
      message = 'onboarding snapshot command binding is not pending registration';
  end if;

  insert into private.game_character_onboarding_snapshot_command_bindings(
    command_id, correlation_id, actor_user_id, character_id, mode
  ) values (
    p_command_id, p_correlation_id, p_actor_user_id, p_character_id, p_mode
  ) on conflict do nothing
  returning command_id into v_inserted_command_id;
  if v_inserted_command_id is not null then
    return query select 'BOUND'::text;
    return;
  end if;

  -- A concurrent command-key replay may have won the insert.  It is safe
  -- only when that durable command-keyed row proves this entire tuple.
  select * into v_binding from private.game_character_onboarding_snapshot_command_bindings
   where command_id = p_command_id for share;
  if found and v_binding.correlation_id = p_correlation_id
     and v_binding.actor_user_id = p_actor_user_id
     and v_binding.character_id = p_character_id
     and v_binding.mode = p_mode then
    return query select 'EXACT_RETRY'::text;
    return;
  end if;
  raise exception using errcode = 'P0001',
    message = 'onboarding snapshot command binding conflicts with immutable exact tuple';
end;
$$;

revoke all on function public.register_game_character_onboarding_snapshot_command_binding(uuid, uuid, uuid, text, uuid)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function public.register_game_character_onboarding_snapshot_command_binding(uuid, uuid, uuid, text, uuid)
  to service_role;

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
  v_binding private.game_character_onboarding_snapshot_command_bindings%rowtype;
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_manifest private.game_character_m4_file_snapshot_manifests%rowtype;
  v_artifact private.game_character_player_snapshot_v1_artifacts%rowtype;
  v_fulfillment private.game_character_onboarding_snapshot_fulfillments%rowtype;
  v_intent_found boolean;
  v_handoff_found boolean;
  v_character_found boolean;
  v_outbox_found boolean;
  v_binding_found boolean;
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

  -- Resolve eligibility from the immutable command key only.  This never
  -- searches artifact candidates by name, hash, or acknowledgement time.
  select correlation_id into v_correlation_id
    from private.game_character_onboarding_snapshot_command_bindings
   where command_id = p_artifact_command_id
     and character_id = p_character_id;
  if v_correlation_id is null then
    return query select 'NOT_ELIGIBLE'::text;
    return;
  end if;

  -- Keep the activation lock order and lock the binding only after outbox.
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
  select * into v_binding from private.game_character_onboarding_snapshot_command_bindings
   where command_id = p_artifact_command_id for share;
  v_binding_found := found;

  if not v_intent_found or not v_handoff_found or not v_character_found or not v_outbox_found or not v_binding_found
     or v_binding.correlation_id <> v_correlation_id
     or v_binding.actor_user_id <> v_outbox.actor_user_id
     or v_binding.character_id <> v_outbox.character_id
     or v_binding.mode <> v_outbox.mode
     or v_handoff.actor_user_id <> v_outbox.actor_user_id
     or v_handoff.character_id <> v_outbox.character_id
     or v_handoff.mode <> v_outbox.mode
     or v_intent.actor_user_id <> v_outbox.actor_user_id
     or v_intent.mode <> v_outbox.mode
     or v_intent.status <> 'finalized'
     or v_handoff.status <> 'activated'
     or v_handoff.activated_at is null
     or v_outbox.character_id <> p_character_id then
    raise exception using errcode = 'P0001', message = 'onboarding snapshot command binding relation is inconsistent';
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
     or v_receipt.acknowledged_at <= v_binding.bound_at then
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
  raise exception using errcode = 'P0001', message = 'fulfilled onboarding snapshot command binding is inconsistent';
end;
$$;

revoke all on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid, uuid)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login;
grant execute on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid, uuid)
  to mud_writer;

comment on table private.game_character_onboarding_snapshot_command_bindings is
  'Private immutable command-keyed proof that one service-registered command belongs to one exact activated onboarding actor/correlation/character/mode tuple.';
comment on function public.register_game_character_onboarding_snapshot_command_binding(uuid, uuid, uuid, text, uuid) is
  'Gateway service_role only. After exact activated onboarding, immutably binds one command id to the supplied actor/correlation/character/mode tuple; exact replay returns EXACT_RETRY and every substitution fails closed.';
comment on function private.fulfill_game_character_onboarding_snapshot_eligibility(uuid, uuid) is
  'mud_writer_login SET ROLE mud_writer only. Fulfills only the supplied command-key binding after a receipt acknowledgement strictly later than bound_at; no candidate scan is performed.';
