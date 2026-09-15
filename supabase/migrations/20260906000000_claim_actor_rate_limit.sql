-- 2026-09-06: bound fresh legacy-password claim flows per authenticated actor.
--
-- A failed C password comparison closes and cancels an unreserved intent.  If
-- cancelled rows were ignored, the same actor could mint a new correlation
-- for every guess.  Keep all outcomes in the fixed-window count while
-- preserving exact-correlation idempotency.  This is one defense layer; the
-- browser-facing onboarding ingress must also enforce a source-address limit
-- before the feature is enabled.

create index if not exists game_character_onboarding_intents_claim_rate_idx
  on private.game_character_onboarding_intents(actor_user_id, created_at)
  where mode = 'claim';

create or replace function public.begin_game_character_onboarding(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_mode text,
  p_expires_at timestamptz
)
returns table (
  correlation_id uuid,
  actor_user_id uuid,
  mode text,
  status text,
  expires_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
#variable_conflict use_column
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_now timestamptz := now();
begin
  if p_actor_user_id is null
     or p_correlation_id is null
     or p_mode not in ('provision', 'claim')
     or p_expires_at is null
     or p_expires_at <= v_now
     or p_expires_at > v_now + interval '30 minutes' then
    raise exception using errcode = '22023', message = 'onboarding requires actor, correlation, supported mode, and future expiry';
  end if;

  perform 1 from auth.users where id = p_actor_user_id;
  if not found then
    raise exception using errcode = '22023', message = 'onboarding actor does not exist';
  end if;

  -- The actor lock makes the live-intent rule and the fixed-window count one
  -- serial decision even when several Gateway replicas receive requests.
  perform pg_advisory_xact_lock(hashtextextended(p_actor_user_id::text, 717126642));

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if found then
    if v_intent.actor_user_id <> p_actor_user_id
       or v_intent.mode <> p_mode then
      raise exception using errcode = 'P0001', message = 'correlation id belongs to a different onboarding payload';
    end if;

    return query select
      v_intent.correlation_id,
      v_intent.actor_user_id,
      v_intent.mode,
      v_intent.status,
      v_intent.expires_at;
    return;
  end if;

  if p_mode = 'claim' and (
    select count(*)
    from private.game_character_onboarding_intents as recent_claim
    where recent_claim.actor_user_id = p_actor_user_id
      and recent_claim.mode = 'claim'
      and recent_claim.created_at >= v_now - interval '15 minutes'
  ) >= 5 then
    raise exception using errcode = 'P0001', message = 'claim onboarding rate limit exceeded';
  end if;

  if exists (
    select 1
    from private.game_character_onboarding_intents as active
    where active.actor_user_id = p_actor_user_id
      and (
        active.status = 'provisioning'
        or (active.status = 'started' and active.expires_at > v_now)
      )
  ) then
    raise exception using errcode = 'P0001', message = 'actor already has an active onboarding intent';
  end if;

  insert into private.game_character_onboarding_intents (
    correlation_id, actor_user_id, mode, expires_at
  ) values (
    p_correlation_id, p_actor_user_id, p_mode, p_expires_at
  ) on conflict on constraint game_character_onboarding_intents_pkey do nothing;

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found
     or v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> p_mode then
    raise exception using errcode = 'P0001', message = 'correlation id belongs to a different onboarding payload';
  end if;

  return query select
    v_intent.correlation_id,
    v_intent.actor_user_id,
    v_intent.mode,
    v_intent.status,
    v_intent.expires_at;
end;
$$;

revoke all on function public.begin_game_character_onboarding(uuid, uuid, text, timestamptz)
  from public, anon, authenticated;
grant execute on function public.begin_game_character_onboarding(uuid, uuid, text, timestamptz)
  to service_role;

comment on function public.begin_game_character_onboarding(uuid, uuid, text, timestamptz) is
  'Gateway service_role only. Idempotently reuses one correlation, rejects concurrent live onboarding, and limits one actor to five fresh claim flows per fixed 15-minute window.';
