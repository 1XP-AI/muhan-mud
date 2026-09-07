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

