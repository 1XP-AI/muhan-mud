-- 2026-09-22: C owns the final write acknowledgement.  Completion records
-- ownership and immutable C evidence, but leaves the character unavailable
-- until the same actor/correlation/character/mode tuple is acknowledged by
-- the narrow handoff RPC below.

create table if not exists private.game_character_onboarding_handoffs (
  correlation_id uuid primary key
    references private.game_character_onboarding_intents(correlation_id)
    on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null unique references public.game_characters(id) on delete restrict,
  mode text not null,
  status text not null default 'pending',
  created_at timestamptz not null default clock_timestamp(),
  activated_at timestamptz,
  constraint game_character_onboarding_handoffs_mode check (mode in ('provision', 'claim')),
  constraint game_character_onboarding_handoffs_status check (status in ('pending', 'activated')),
  constraint game_character_onboarding_handoffs_activation check (
    (status = 'activated') = (activated_at is not null)
  )
);

alter table private.game_character_onboarding_handoffs enable row level security;
revoke all on table private.game_character_onboarding_handoffs
  from public, anon, authenticated, service_role;

do $$
begin
  if not exists (
    select 1 from pg_constraint
    where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_handoff_pending_is_owned'
  ) then
    alter table public.game_characters
      add constraint game_characters_handoff_pending_is_owned check (
        lifecycle <> 'handoff_pending'
        or (owner_user_id is not null and claimed_at is not null)
      );
  end if;
end
$$;

-- Completion still seeds the M3 baseline exactly once, but admission is now
-- deliberately separate from receipt acceptance.  Reconcile follows the
-- same gate: a recovered C save cannot mint a usable Gateway session.
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

  select * into v_head from private.game_character_legacy_heads
    where character_id = v_character.id for update;
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

-- Keep the published result shape, but make a completed provisioning retry
-- report its actual non-admission lifecycle instead of manufacturing active.
create or replace function public.begin_game_character_provisioning(
  p_actor_user_id uuid, p_correlation_id uuid, p_world_id text, p_legacy_name text
)
returns table (
  character_id uuid, correlation_id uuid, actor_user_id uuid, world_id text,
  legacy_name_key text, lifecycle public.character_lifecycle, status text, expires_at timestamptz
)
language plpgsql security definer set search_path = pg_catalog, private
as $$
#variable_conflict use_column
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_request private.game_character_provisioning_requests%rowtype;
  v_character_id uuid; v_legacy_name_key text; v_legacy_shard char(2);
  v_existing_lifecycle public.character_lifecycle; v_now timestamptz;
begin
  if p_actor_user_id is null or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'provisioning requires actor and correlation id';
  end if;
  if nullif(btrim(p_world_id), '') is null or char_length(p_world_id) > 64 or p_world_id ~ '[[:cntrl:]]' then
    raise exception using errcode = '22023', message = 'provisioning world is invalid';
  end if;
  v_legacy_name_key := private.game_identity_canonical_legacy_name(p_legacy_name);
  if v_legacy_name_key is null or char_length(v_legacy_name_key) not between 1 and 12
     or octet_length(v_legacy_name_key) > 14 or v_legacy_name_key ~ E'[[:cntrl:]/\\:]'
     or v_legacy_name_key in ('.', '..') then
    raise exception using errcode = '22023', message = 'provisioning name is not C-safe';
  end if;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id = p_correlation_id for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> 'provision' then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to actor provisioning request';
  end if;
  v_now := clock_timestamp();
  if v_intent.status <> 'finalized' and v_intent.expires_at <= v_now then
    raise exception using errcode = 'P0001', message = 'onboarding intent has expired';
  end if;
  select * into v_request from private.game_character_provisioning_requests where correlation_id = p_correlation_id for update;
  if found then
    if v_request.actor_user_id <> p_actor_user_id or v_request.world_id <> p_world_id
       or v_request.legacy_name_key <> v_legacy_name_key then
      raise exception using errcode = 'P0001', message = 'correlation id belongs to a different provisioning payload';
    end if;
    select character.lifecycle into v_existing_lifecycle
      from public.game_characters as character where character.id = v_request.character_id;
    if not found then raise exception using errcode = 'P0001', message = 'provisioning character is unavailable'; end if;
    return query select v_request.character_id, v_request.correlation_id, v_request.actor_user_id,
      v_request.world_id, v_request.legacy_name_key, v_existing_lifecycle, v_request.status, v_intent.expires_at;
    return;
  end if;
  if v_intent.status <> 'started' then raise exception using errcode = 'P0001', message = 'invalid onboarding lifecycle transition to provisioning'; end if;
  v_legacy_shard := substr(encode(public.digest(convert_to(v_legacy_name_key, 'UTF8'), 'sha1'), 'hex'), 1, 2);
  insert into public.game_characters(world_id, legacy_name, legacy_name_key, legacy_shard, owner_user_id, lifecycle)
    values (p_world_id, v_legacy_name_key, v_legacy_name_key, v_legacy_shard, p_actor_user_id, 'provisioning')
    on conflict on constraint game_characters_world_name_key_key do nothing returning id into v_character_id;
  if v_character_id is null then raise exception using errcode = 'P0001', message = 'canonical legacy name is already reserved or active'; end if;
  insert into private.game_character_provisioning_requests(correlation_id, actor_user_id, character_id, world_id, legacy_name_key)
    values (p_correlation_id, p_actor_user_id, v_character_id, p_world_id, v_legacy_name_key);
  update private.game_character_onboarding_intents set status = 'provisioning' where correlation_id = p_correlation_id;
  return query select v_character_id, p_correlation_id, p_actor_user_id, p_world_id, v_legacy_name_key,
    'provisioning'::public.character_lifecycle, 'reserved'::text, v_intent.expires_at;
end;
$$;

-- The fingerprint/challenge-bound claim finalizer retains its established lock
-- order, but records a pending handoff rather than admitting immediately.
create or replace function public.claim_legacy_game_character_onboarding(
  p_world_id text, p_legacy_name_key text, p_imported_file_sha256 text,
  p_actor_user_id uuid, p_correlation_id uuid
)
returns table (
  character_id uuid, lifecycle public.character_lifecycle, owner_user_id uuid,
  claimed_at timestamptz, onboarding_status text, imported_file_sha256 text
)
language plpgsql security definer set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character public.game_characters%rowtype;
  v_attempt private.game_character_claim_attempts%rowtype;
  v_request private.game_character_claim_requests%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
  v_now timestamptz;
begin
  if nullif(btrim(p_world_id), '') is null or nullif(btrim(p_legacy_name_key), '') is null
     or p_imported_file_sha256 is null or p_imported_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_actor_user_id is null or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'onboarding claim arguments are invalid';
  end if;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id = p_correlation_id for update;
  if not found then raise exception using errcode = 'P0001', message = 'onboarding claim unavailable'; end if;
  perform pg_advisory_xact_lock(hashtextextended(jsonb_build_array(p_world_id, p_legacy_name_key)::text, 870254061));
  select * into v_character from public.game_characters where world_id = p_world_id and legacy_name_key = p_legacy_name_key for update;
  if not found then raise exception using errcode = 'P0001', message = 'onboarding claim unavailable'; end if;
  select * into v_attempt from private.game_character_claim_attempts where correlation_id = p_correlation_id for update;
  if not found or v_attempt.actor_user_id <> p_actor_user_id or v_attempt.character_id <> v_character.id
     or v_attempt.imported_file_sha256 <> p_imported_file_sha256 then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;
  insert into private.game_character_claim_requests(correlation_id, character_id, actor_user_id)
    values (p_correlation_id, v_character.id, p_actor_user_id) on conflict (correlation_id) do nothing;
  select * into v_request from private.game_character_claim_requests where correlation_id = p_correlation_id for update;
  if not found or v_request.character_id <> v_character.id or v_request.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;
  if v_attempt.claimed_at is not null and v_intent.status = 'finalized' then
    select * into v_handoff from private.game_character_onboarding_handoffs where correlation_id = p_correlation_id for update;
    if not found or v_handoff.actor_user_id <> p_actor_user_id or v_handoff.character_id <> v_character.id
       or v_handoff.mode <> 'claim' or v_handoff.status not in ('pending', 'activated')
       or v_character.lifecycle not in ('handoff_pending', 'active') or v_character.owner_user_id <> p_actor_user_id
       or (v_handoff.status = 'pending' and v_character.lifecycle <> 'handoff_pending')
       or (v_handoff.status = 'activated' and v_character.lifecycle <> 'active')
       or v_character.claimed_at <> v_attempt.claimed_at or v_request.completed_at is null
       or v_character.imported_file_sha256 is distinct from p_imported_file_sha256 then
      raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
    end if;
    return query select v_character.id, v_character.lifecycle, v_character.owner_user_id,
      v_character.claimed_at, 'finalized'::text, v_character.imported_file_sha256;
    return;
  end if;
  v_now := clock_timestamp();
  if v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> 'claim' or v_intent.status <> 'started'
     or v_intent.expires_at <= v_now or v_attempt.claimed_at is not null or v_attempt.allow_expires_at <= v_now
     or v_request.completed_at is not null or v_character.lifecycle <> 'imported_unclaimed'
     or v_character.owner_user_id is not null or v_character.imported_file_sha256 is distinct from p_imported_file_sha256 then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;
  update public.game_characters set lifecycle = 'handoff_pending', owner_user_id = p_actor_user_id, claimed_at = v_now
    where id = v_character.id returning * into v_character;
  update private.game_character_claim_attempts set claimed_at = v_now where correlation_id = p_correlation_id;
  update private.game_character_onboarding_intents set status = 'finalized', completed_at = v_now where correlation_id = p_correlation_id;
  update private.game_character_claim_requests set completed_at = v_now where correlation_id = p_correlation_id;
  insert into private.game_character_onboarding_handoffs(correlation_id, actor_user_id, character_id, mode)
    values (p_correlation_id, p_actor_user_id, v_character.id, 'claim');
  insert into public.game_identity_events(character_id, actor_user_id, event_type, correlation_id, details)
    values (v_character.id, p_actor_user_id, 'legacy_character_claimed', p_correlation_id,
      jsonb_build_object('source', 'legacy-password-verified', 'admission', 'handoff_pending'));
  return query select v_character.id, v_character.lifecycle, v_character.owner_user_id,
    v_character.claimed_at, 'finalized'::text, v_character.imported_file_sha256;
end;
$$;

-- Evidence finalization remains an immutable receipt path.  A receipt can be
-- retried while pending, but neither its first write nor its retry activates.
create or replace function public.finalize_game_character_legacy_identity_evidence(
  p_actor_user_id uuid, p_correlation_id uuid, p_character_id uuid, p_outcome text,
  p_canonical_legacy_name text, p_player_file_sha256 text, p_evidence_version smallint,
  p_storage_format text, p_legacy_shard text
)
returns table (
  character_id uuid, actor_user_id uuid, mode text, lifecycle public.character_lifecycle,
  world_id text, canonical_legacy_name text, legacy_shard char(2), player_file_sha256 text,
  evidence_version smallint, storage_format text, recorded_at timestamptz
)
language plpgsql security definer set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character public.game_characters%rowtype;
  v_evidence private.game_character_legacy_identity_evidence%rowtype;
  v_provisioning private.game_character_provisioning_requests%rowtype;
  v_handoff private.game_character_onboarding_handoffs%rowtype;
begin
  if p_actor_user_id is null or p_correlation_id is null or p_character_id is null
     or p_outcome is distinct from 'ok' or p_evidence_version is distinct from 1::smallint
     or p_storage_format is distinct from 'player-v1' or p_player_file_sha256 is null
     or p_player_file_sha256 !~ '^[0-9a-f]{64}$' or p_canonical_legacy_name is null
     or p_legacy_shard is null or p_legacy_shard !~ '^[0-9a-f]{2}$'
     or p_canonical_legacy_name <> private.game_identity_canonical_legacy_name(p_canonical_legacy_name) then
    raise exception using errcode = '22023', message = 'legacy identity evidence must be an exact successful V1 player-v1 tuple';
  end if;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id = p_correlation_id for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence correlation is not owned by actor';
  end if;
  if v_intent.mode = 'claim' then
    select * into v_character from public.game_characters where id = p_character_id;
    if not found or v_character.legacy_name_key <> p_canonical_legacy_name or v_character.legacy_name <> p_canonical_legacy_name
       or v_character.legacy_shard <> p_legacy_shard then
      raise exception using errcode = 'P0001', message = 'legacy identity evidence does not match canonical character';
    end if;
    perform pg_advisory_xact_lock(hashtextextended(jsonb_build_array(v_character.world_id, p_canonical_legacy_name)::text, 870254061));
  end if;
  select * into v_character from public.game_characters where id = p_character_id for update;
  if not found or v_character.legacy_name_key <> p_canonical_legacy_name or v_character.legacy_name <> p_canonical_legacy_name
     or v_character.legacy_shard <> p_legacy_shard or v_character.storage_format <> 1 then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence does not match canonical character';
  end if;
  select * into v_evidence from private.game_character_legacy_identity_evidence where correlation_id = p_correlation_id for update;
  if found then
    if v_evidence.actor_user_id <> p_actor_user_id or v_evidence.character_id <> p_character_id
       or v_evidence.mode <> v_intent.mode or v_evidence.world_id <> v_character.world_id
       or v_evidence.canonical_legacy_name <> p_canonical_legacy_name or v_evidence.legacy_shard <> p_legacy_shard
       or v_evidence.player_file_sha256 <> p_player_file_sha256 or v_evidence.outcome <> p_outcome
       or v_evidence.evidence_version <> p_evidence_version or v_evidence.storage_format <> p_storage_format
       or v_character.lifecycle not in ('handoff_pending', 'active') or v_character.owner_user_id <> p_actor_user_id
       or v_intent.status <> 'finalized'
       or (v_intent.mode = 'claim' and v_character.imported_file_sha256 is distinct from p_player_file_sha256) then
      raise exception using errcode = 'P0001', message = 'finalized evidence handoff state is inconsistent';
    end if;
    select * into v_handoff from private.game_character_onboarding_handoffs
      where correlation_id = p_correlation_id for update;
    if not found or v_handoff.actor_user_id <> p_actor_user_id or v_handoff.character_id <> p_character_id
       or v_handoff.mode <> v_intent.mode
       or (v_handoff.status = 'pending' and v_character.lifecycle <> 'handoff_pending')
       or (v_handoff.status = 'activated' and v_character.lifecycle <> 'active') then
      raise exception using errcode = 'P0001', message = 'finalized evidence handoff binding is inconsistent';
    end if;
    if v_evidence.mode = 'provision' then
      select * into v_provisioning from private.game_character_provisioning_requests where correlation_id = p_correlation_id for update;
      if not found or v_provisioning.character_id <> p_character_id or v_provisioning.actor_user_id <> p_actor_user_id
         or v_provisioning.status <> 'finalized' or v_provisioning.saved_file_sha256 <> p_player_file_sha256
         or v_provisioning.storage_format <> 1 then
        raise exception using errcode = 'P0001', message = 'finalized provision evidence state is inconsistent';
      end if;
    end if;
    return query select v_evidence.character_id, v_evidence.actor_user_id, v_evidence.mode,
      v_character.lifecycle, v_evidence.world_id, v_evidence.canonical_legacy_name, v_evidence.legacy_shard,
      v_evidence.player_file_sha256, v_evidence.evidence_version, v_evidence.storage_format, v_evidence.recorded_at;
    return;
  end if;
  if v_intent.mode = 'provision' then
    select * into v_provisioning from private.game_character_provisioning_requests where correlation_id = p_correlation_id for update;
    if not found or v_intent.status <> 'provisioning' or v_intent.expires_at <= clock_timestamp()
       or v_provisioning.actor_user_id <> p_actor_user_id or v_provisioning.character_id <> p_character_id
       or v_provisioning.world_id <> v_character.world_id or v_provisioning.legacy_name_key <> p_canonical_legacy_name
       or v_provisioning.status <> 'reserved' or v_character.lifecycle <> 'provisioning'
       or v_character.owner_user_id <> p_actor_user_id then
      raise exception using errcode = 'P0001', message = 'provision evidence is not authorized for current lifecycle';
    end if;
    perform 1 from private.complete_game_character_provisioning(
      p_actor_user_id, p_correlation_id, p_player_file_sha256, 1::smallint, false
    );
  elsif v_intent.mode = 'claim' then
    if v_intent.status <> 'started' or v_intent.expires_at <= clock_timestamp()
       or v_character.lifecycle <> 'imported_unclaimed' or v_character.owner_user_id is not null
       or v_character.imported_file_sha256 is distinct from p_player_file_sha256 then
      raise exception using errcode = 'P0001', message = 'claim evidence is not authorized for imported character';
    end if;
    perform 1 from public.claim_legacy_game_character_onboarding(
      v_character.world_id, p_canonical_legacy_name, p_player_file_sha256, p_actor_user_id, p_correlation_id
    );
  else
    raise exception using errcode = 'P0001', message = 'legacy identity evidence mode is inconsistent';
  end if;
  select * into v_character from public.game_characters where id = p_character_id for update;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id = p_correlation_id;
  if not found or v_character.lifecycle <> 'handoff_pending' or v_character.owner_user_id <> p_actor_user_id
     or v_intent.status <> 'finalized'
     or (v_intent.mode = 'claim' and v_character.imported_file_sha256 is distinct from p_player_file_sha256) then
    raise exception using errcode = 'P0001', message = 'legacy identity evidence lifecycle transition is inconsistent';
  end if;
  insert into private.game_character_legacy_identity_evidence(
    correlation_id, actor_user_id, character_id, mode, world_id, canonical_legacy_name,
    legacy_shard, player_file_sha256, outcome, evidence_version, storage_format, recorded_at
  ) values (p_correlation_id, p_actor_user_id, p_character_id, v_intent.mode, v_character.world_id,
    p_canonical_legacy_name, p_legacy_shard, p_player_file_sha256, p_outcome, p_evidence_version,
    p_storage_format, clock_timestamp()) returning * into v_evidence;
  return query select v_evidence.character_id, v_evidence.actor_user_id, v_evidence.mode,
    v_character.lifecycle, v_evidence.world_id, v_evidence.canonical_legacy_name, v_evidence.legacy_shard,
    v_evidence.player_file_sha256, v_evidence.evidence_version, v_evidence.storage_format, v_evidence.recorded_at;
end;
$$;

create or replace function public.finalize_game_character_legacy_identity_evidence(
  p_actor_user_id uuid, p_correlation_id uuid, p_character_id uuid, p_outcome text,
  p_canonical_legacy_name text, p_player_file_sha256 text, p_evidence_version smallint,
  p_storage_format text
)
returns table (
  character_id uuid, actor_user_id uuid, mode text, lifecycle public.character_lifecycle,
  canonical_legacy_name text, player_file_sha256 text, evidence_version smallint,
  storage_format text, recorded_at timestamptz
)
language plpgsql security definer set search_path = pg_catalog, private
as $$
declare v_legacy_shard char(2);
begin
  select legacy_shard into v_legacy_shard from public.game_characters where id = p_character_id;
  if not found then raise exception using errcode = 'P0001', message = 'legacy identity evidence character is unavailable'; end if;
  return query select finalized.character_id, finalized.actor_user_id, finalized.mode, finalized.lifecycle,
    finalized.canonical_legacy_name, finalized.player_file_sha256, finalized.evidence_version,
    finalized.storage_format, finalized.recorded_at
  from public.finalize_game_character_legacy_identity_evidence(
    p_actor_user_id, p_correlation_id, p_character_id, p_outcome, p_canonical_legacy_name,
    p_player_file_sha256, p_evidence_version, p_storage_format, v_legacy_shard::text
  ) as finalized;
end;
$$;

-- This is the sole database transition from a completed C write to admission.
-- The C-side callback must submit exactly the tuple bound in the private
-- handoff row; no Gateway instance, actor, or correlation substitution works.
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
  v_activated_at timestamptz := clock_timestamp();
begin
  if p_actor_user_id is null or p_correlation_id is null or p_character_id is null
     or p_mode is null or p_mode not in ('provision', 'claim') then
    raise exception using errcode = '22023', message = 'onboarding handoff activation requires actor, correlation, character, and mode';
  end if;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id = p_correlation_id for update;
  select * into v_handoff from private.game_character_onboarding_handoffs where correlation_id = p_correlation_id for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> p_mode
     or v_intent.status <> 'finalized' or v_handoff.actor_user_id <> p_actor_user_id
     or v_handoff.character_id <> p_character_id or v_handoff.mode <> p_mode then
    raise exception using errcode = 'P0001', message = 'onboarding handoff does not match exact activation tuple';
  end if;
  select * into v_character from public.game_characters where id = p_character_id for update;
  if not found or v_character.owner_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'onboarding handoff character is inconsistent';
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
  update private.game_character_onboarding_handoffs set status = 'activated', activated_at = v_activated_at
    where correlation_id = p_correlation_id;
  insert into public.game_identity_events(character_id, actor_user_id, event_type, correlation_id, details)
    values (p_character_id, p_actor_user_id, 'game_character_onboarding_activated', p_correlation_id,
      jsonb_build_object('mode', p_mode, 'source', 'c-write-callback'));
  return query select v_character.id, v_character.owner_user_id, p_correlation_id,
    v_character.lifecycle, 'finalized'::text;
end;
$$;

revoke all on function public.activate_game_character_onboarding_handoff(uuid, uuid, uuid, text)
  from public, anon, authenticated;
grant execute on function public.activate_game_character_onboarding_handoff(uuid, uuid, uuid, text)
  to service_role;

comment on table private.game_character_onboarding_handoffs is
  'Private correlation-keyed C write handoff ledger. It binds actor, character, and mode; service_role has no direct table CRUD.';
comment on function public.activate_game_character_onboarding_handoff(uuid, uuid, uuid, text) is
  'Gateway service_role only. Invoke only from the exact accepted C MUD1O COMMIT/CLAIMED write callback. Atomically activates only the matching pending actor/correlation/character/mode handoff; exact activated retries are idempotent.';
