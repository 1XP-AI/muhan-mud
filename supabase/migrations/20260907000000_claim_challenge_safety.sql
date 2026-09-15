-- 2026-09-07: issue a short-lived, fingerprint-bound claim challenge before
-- the Gateway can move ownership of an imported legacy character.  The C
-- process still verifies the legacy password; this ledger stores only the
-- C-observed player-file SHA-256 fingerprint and never stores passwords,
-- proofs, tokens, tickets, JWTs, terminal input, or character-name text.

create table if not exists private.game_character_claim_attempts (
  correlation_id uuid primary key
    references private.game_character_onboarding_intents(correlation_id)
    on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  imported_file_sha256 text not null,
  allowed_at timestamptz not null default now(),
  allow_expires_at timestamptz not null,
  claimed_at timestamptz,
  constraint game_character_claim_attempts_sha256_format check (
    imported_file_sha256 ~ '^[0-9a-f]{64}$'
  ),
  constraint game_character_claim_attempts_allow_window check (
    allow_expires_at = allowed_at + interval '90 seconds'
  ),
  constraint game_character_claim_attempts_lifecycle check (
    claimed_at is null
    or claimed_at >= allowed_at
  )
);

alter table private.game_character_claim_attempts enable row level security;

revoke all on table private.game_character_claim_attempts
  from public, anon, authenticated, service_role;

-- The target index supports the cross-actor short-window admission decision;
-- the correlation index is implicit in the primary key and is the retry lock.
create index if not exists game_character_claim_attempts_target_allowed_idx
  on private.game_character_claim_attempts(character_id, allowed_at);

comment on table private.game_character_claim_attempts is
  'Private 90-second fingerprint-bound claim challenge ledger. No passwords, proofs, JWTs, tickets, terminal input, or character-name text are stored here.';

comment on column private.game_character_claim_attempts.imported_file_sha256 is
  'Exact lowercase SHA-256 observed by trusted C before the password prompt; this fingerprint is not authentication proof.';

-- Preserve the pre-challenge signature for additive, rolling-schema
-- compatibility, but keep every application role unable to execute it.  Old
-- Gateway replicas therefore fail closed instead of failing schema discovery.
revoke all on function public.claim_legacy_game_character_onboarding(text, text, uuid, uuid)
  from public, anon, authenticated, service_role;

-- Keep the older direct primitive unavailable even to service_role.  It is not
-- used by the ledger-bound finalizer.
revoke all on function public.claim_legacy_game_character(text, text, uuid, uuid)
  from public, anon, authenticated, service_role;

create or replace function public.challenge_legacy_game_character_onboarding(
  p_world_id text,
  p_legacy_name_key text,
  p_imported_file_sha256 text,
  p_actor_user_id uuid,
  p_correlation_id uuid
)
returns table (
  character_id uuid,
  legacy_name_key text,
  imported_file_sha256 text,
  challenge_status text,
  allow_expires_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character public.game_characters%rowtype;
  v_attempt private.game_character_claim_attempts%rowtype;
  v_now timestamptz := now();
begin
  if nullif(btrim(p_world_id), '') is null
     or nullif(btrim(p_legacy_name_key), '') is null
     or p_imported_file_sha256 is null
     or p_imported_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_actor_user_id is null
     or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'claim challenge arguments are invalid';
  end if;

  -- Lock in the documented order.  The target advisory lock makes the
  -- count-and-insert decision serial across actors and Gateway replicas.
  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found then
    raise exception using errcode = 'P0001', message = 'claim challenge unavailable';
  end if;

  perform pg_advisory_xact_lock(
    hashtextextended(
      jsonb_build_array(p_world_id, p_legacy_name_key)::text,
      870254061
    )
  );

  select * into v_character
    from public.game_characters as character
    where character.world_id = p_world_id
      and character.legacy_name_key = p_legacy_name_key
    for update;
  if not found then
    raise exception using errcode = 'P0001', message = 'claim challenge unavailable';
  end if;

  -- now() is fixed at transaction start.  Refresh the authorization clock
  -- after the advisory/row locks so a long lock wait cannot revive an expired
  -- challenge or mint a challenge whose 90-second window is already stale.
  v_now := clock_timestamp();

  select * into v_attempt
    from private.game_character_claim_attempts
    where correlation_id = p_correlation_id
    for update;
  -- A pre-existing attempt row can itself be locked by a concurrent retry;
  -- refresh again after that final authorization-relevant lock wait.
  v_now := clock_timestamp();
  if found then
    if v_attempt.actor_user_id <> p_actor_user_id
       or v_attempt.character_id <> v_character.id
       or v_attempt.imported_file_sha256 <> p_imported_file_sha256
       or (
         v_attempt.claimed_at is null
         and (
           v_intent.actor_user_id <> p_actor_user_id
           or v_intent.mode <> 'claim'
           or v_intent.status <> 'started'
           or v_intent.expires_at <= v_now
           or v_attempt.allow_expires_at <= v_now
           or v_character.lifecycle <> 'imported_unclaimed'
           or v_character.owner_user_id is not null
           or v_character.imported_file_sha256 is distinct from p_imported_file_sha256
         )
       ) then
      raise exception using errcode = 'P0001', message = 'claim challenge unavailable';
    end if;

    return query select
      v_attempt.character_id,
      v_character.legacy_name_key,
      v_attempt.imported_file_sha256,
      'allowed'::text,
      v_attempt.allow_expires_at;
    return;
  end if;

  if v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> 'claim'
     or v_intent.status <> 'started'
     or v_intent.expires_at <= v_now
     or v_character.lifecycle <> 'imported_unclaimed'
     or v_character.owner_user_id is not null
     or v_character.imported_file_sha256 is distinct from p_imported_file_sha256 then
    raise exception using errcode = 'P0001', message = 'claim challenge unavailable';
  end if;

  if (
    select count(*)
    from private.game_character_claim_attempts as recent
    where recent.character_id = v_character.id
      and recent.allowed_at >= v_now - interval '15 minutes'
  ) >= 3 then
    raise exception using errcode = 'P0001', message = 'claim challenge unavailable';
  end if;

  insert into private.game_character_claim_attempts (
    correlation_id, actor_user_id, character_id, imported_file_sha256,
    allowed_at, allow_expires_at
  ) values (
    p_correlation_id, p_actor_user_id, v_character.id, p_imported_file_sha256,
    v_now, v_now + interval '90 seconds'
  ) returning * into v_attempt;

  return query select
    v_attempt.character_id,
    v_character.legacy_name_key,
    v_attempt.imported_file_sha256,
    'allowed'::text,
    v_attempt.allow_expires_at;
end;
$$;

create or replace function public.claim_legacy_game_character_onboarding(
  p_world_id text,
  p_legacy_name_key text,
  p_imported_file_sha256 text,
  p_actor_user_id uuid,
  p_correlation_id uuid
)
returns table (
  character_id uuid,
  lifecycle public.character_lifecycle,
  owner_user_id uuid,
  claimed_at timestamptz,
  onboarding_status text,
  imported_file_sha256 text
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character public.game_characters%rowtype;
  v_attempt private.game_character_claim_attempts%rowtype;
  v_request private.game_character_claim_requests%rowtype;
  v_now timestamptz := now();
begin
  if nullif(btrim(p_world_id), '') is null
     or nullif(btrim(p_legacy_name_key), '') is null
     or p_imported_file_sha256 is null
     or p_imported_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_actor_user_id is null
     or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'onboarding claim arguments are invalid';
  end if;

  -- Match challenge lock order so retries and competing Gateway replicas do
  -- not form a lock cycle: intent, target advisory lock, then character.
  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;

  perform pg_advisory_xact_lock(
    hashtextextended(
      jsonb_build_array(p_world_id, p_legacy_name_key)::text,
      870254061
    )
  );

  select * into v_character
    from public.game_characters as character
    where character.world_id = p_world_id
      and character.legacy_name_key = p_legacy_name_key
    for update;
  if not found then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;

  -- The expiry decision must use wall-clock time after all lock waits, not the
  -- transaction-start snapshot captured in the declaration above.
  v_now := clock_timestamp();

  select * into v_attempt
    from private.game_character_claim_attempts
    where correlation_id = p_correlation_id
    for update;
  -- Do not let a wait on the challenge row revive an expired authorization.
  v_now := clock_timestamp();
  if not found
     or v_attempt.actor_user_id <> p_actor_user_id
     or v_attempt.character_id <> v_character.id
     or v_attempt.imported_file_sha256 <> p_imported_file_sha256 then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;

  -- Preserve the original private claim-request audit contract while the
  -- challenge ledger becomes the authorization boundary.  The correlation
  -- row is bound to the same actor and character and completed atomically.
  insert into private.game_character_claim_requests (
    correlation_id, character_id, actor_user_id
  ) values (
    p_correlation_id, v_character.id, p_actor_user_id
  ) on conflict (correlation_id) do nothing;

  select * into v_request
    from private.game_character_claim_requests
    where correlation_id = p_correlation_id
    for update;
  if not found
     or v_request.character_id <> v_character.id
     or v_request.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;

  -- A committed exact retry is safe forever.  It is deliberately checked
  -- before the challenge/intent expiry gates so a lost Gateway response does
  -- not turn a completed ownership transition into an apparent failure.
  if v_attempt.claimed_at is not null and v_intent.status = 'finalized' then
    if v_character.lifecycle <> 'active'
       or v_character.owner_user_id <> p_actor_user_id
       or v_character.claimed_at <> v_attempt.claimed_at
       or v_request.completed_at is null
       or v_character.imported_file_sha256 <> p_imported_file_sha256 then
      raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
    end if;

    return query select
      v_character.id,
      v_character.lifecycle,
      v_character.owner_user_id,
      v_character.claimed_at,
      'finalized'::text,
      v_character.imported_file_sha256;
    return;
  end if;

  if v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> 'claim'
     or v_intent.status <> 'started'
     or v_intent.expires_at <= v_now
     or v_attempt.claimed_at is not null
     or v_attempt.allow_expires_at <= v_now
     or v_request.completed_at is not null
     or v_character.lifecycle <> 'imported_unclaimed'
     or v_character.owner_user_id is not null
     or v_character.imported_file_sha256 is distinct from p_imported_file_sha256 then
    raise exception using errcode = 'P0001', message = 'onboarding claim unavailable';
  end if;

  update public.game_characters
    set lifecycle = 'claiming', owner_user_id = p_actor_user_id
    where id = v_character.id;

  update public.game_characters
    set lifecycle = 'active', claimed_at = v_now
    where id = v_character.id
    returning * into v_character;

  update private.game_character_claim_attempts
    set claimed_at = v_now
    where correlation_id = p_correlation_id;

  update private.game_character_onboarding_intents
    set status = 'finalized', completed_at = v_now
    where correlation_id = p_correlation_id;

  update private.game_character_claim_requests
    set completed_at = v_now
    where correlation_id = p_correlation_id;

  insert into public.game_identity_events (
    character_id, actor_user_id, event_type, correlation_id, details
  ) values (
    v_character.id,
    p_actor_user_id,
    'legacy_character_claimed',
    p_correlation_id,
    jsonb_build_object('source', 'legacy-password-verified')
  );

  return query select
    v_character.id,
    v_character.lifecycle,
    v_character.owner_user_id,
    v_character.claimed_at,
    'finalized'::text,
    v_character.imported_file_sha256;
end;
$$;

revoke all on function public.challenge_legacy_game_character_onboarding(text, text, text, uuid, uuid)
  from public, anon, authenticated, service_role;
revoke all on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid)
  from public, anon, authenticated, service_role;
grant execute on function public.challenge_legacy_game_character_onboarding(text, text, text, uuid, uuid)
  to service_role;
grant execute on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid)
  to service_role;

comment on function public.challenge_legacy_game_character_onboarding(text, text, text, uuid, uuid) is
  'Gateway service_role only. Records one exact C-observed imported-file SHA-256 challenge for 90 seconds before password entry; the fingerprint is not authentication proof, and target attempts are capped at three per 15 minutes across actors and replicas.';

comment on function public.claim_legacy_game_character_onboarding(text, text, text, uuid, uuid) is
  'Gateway service_role only. Finalizes only an exact unexpired allowed challenge, atomically binding actor, correlation, character, and imported-file SHA-256; no password, proof, JWT, ticket, or terminal input is accepted.';
