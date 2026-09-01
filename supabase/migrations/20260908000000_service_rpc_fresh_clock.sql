-- 2026-09-08: authorization clocks are read after every blocking lock.  `now()`
-- is transaction-start time and must not authorize an already-expired lease.

create or replace function public.begin_game_character_session(
  p_actor_user_id uuid, p_character_id uuid, p_session_id uuid,
  p_gateway_instance_id text, p_expires_at timestamptz
) returns table (character_id uuid, session_id uuid, expires_at timestamptz,
  owner_user_id uuid, lifecycle public.character_lifecycle, legacy_name_key text)
language plpgsql security definer set search_path = pg_catalog, private as $$
declare
  v_character public.game_characters%rowtype;
  v_lease private.game_character_sessions%rowtype;
  v_now timestamptz;
begin
  v_now := clock_timestamp();
  if p_actor_user_id is null or p_character_id is null or p_session_id is null
     or nullif(btrim(p_gateway_instance_id), '') is null or char_length(p_gateway_instance_id) > 128
     or p_gateway_instance_id ~ '[[:cntrl:]]' or p_expires_at is null
     or p_expires_at <= v_now or p_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'session lease arguments are invalid';
  end if;
  select * into v_character from public.game_characters where id = p_character_id for update;
  if not found or v_character.lifecycle <> 'active' or v_character.owner_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'character is not an active character owned by actor';
  end if;
  select * into v_lease from private.game_character_sessions lease where lease.character_id = p_character_id for update;
  -- Both row locks may have waited. Revalidate caller lease and retry output now.
  v_now := clock_timestamp();
  if p_expires_at <= v_now or p_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'session lease arguments are invalid';
  end if;
  if found then
    if v_lease.session_id = p_session_id then
      if v_lease.actor_user_id <> p_actor_user_id or v_lease.gateway_instance_id <> p_gateway_instance_id
         or v_lease.expires_at <= v_now then
        raise exception using errcode = 'P0001', message = 'session lease owner does not match retry';
      end if;
      return query select v_lease.character_id, v_lease.session_id, v_lease.expires_at,
        v_character.owner_user_id, v_character.lifecycle, v_character.legacy_name_key;
      return;
    end if;
    if v_lease.expires_at > v_now then
      raise exception using errcode = 'P0001', message = 'character already has an active session lease';
    end if;
    update private.game_character_sessions lease set session_id=p_session_id, actor_user_id=p_actor_user_id,
      gateway_instance_id=p_gateway_instance_id, expires_at=p_expires_at, created_at=v_now where lease.character_id=p_character_id;
  else
    insert into private.game_character_sessions(character_id,session_id,actor_user_id,gateway_instance_id,expires_at)
      values(p_character_id,p_session_id,p_actor_user_id,p_gateway_instance_id,p_expires_at);
  end if;
  return query select lease.character_id,lease.session_id,lease.expires_at,v_character.owner_user_id,
    v_character.lifecycle,v_character.legacy_name_key from private.game_character_sessions lease where lease.character_id=p_character_id;
end; $$;

create or replace function public.renew_game_character_session(
  p_session_id uuid, p_gateway_instance_id text, p_expires_at timestamptz
) returns table (character_id uuid, session_id uuid, expires_at timestamptz,
  owner_user_id uuid, lifecycle public.character_lifecycle, legacy_name_key text)
language plpgsql security definer set search_path = pg_catalog, private as $$
declare
  v_character_id uuid; v_character public.game_characters%rowtype;
  v_lease private.game_character_sessions%rowtype; v_now timestamptz;
begin
  v_now := clock_timestamp();
  if p_session_id is null or nullif(btrim(p_gateway_instance_id), '') is null
     or char_length(p_gateway_instance_id) > 128 or p_gateway_instance_id ~ '[[:cntrl:]]'
     or p_expires_at is null or p_expires_at <= v_now or p_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode='22023', message='session lease renewal arguments are invalid';
  end if;
  select lease.character_id into v_character_id from private.game_character_sessions lease where lease.session_id=p_session_id;
  if not found then raise exception using errcode='P0001', message='session lease cannot be renewed'; end if;
  select * into v_character from public.game_characters where id=v_character_id for update;
  select * into v_lease from private.game_character_sessions lease where lease.session_id=p_session_id and lease.character_id=v_character_id for update;
  v_now := clock_timestamp();
  if p_expires_at <= v_now or p_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode='22023', message='session lease renewal arguments are invalid';
  end if;
  if not found or v_character.lifecycle <> 'active' or v_character.owner_user_id is null
     or v_lease.actor_user_id <> v_character.owner_user_id or v_lease.gateway_instance_id <> p_gateway_instance_id
     or v_lease.expires_at <= v_now then
    raise exception using errcode='P0001', message='session lease cannot be renewed';
  end if;
  update private.game_character_sessions lease set expires_at=p_expires_at where lease.session_id=p_session_id;
  return query select v_lease.character_id,v_lease.session_id,p_expires_at,v_character.owner_user_id,v_character.lifecycle,v_character.legacy_name_key;
end; $$;

create or replace function public.begin_game_character_onboarding(
  p_actor_user_id uuid, p_correlation_id uuid, p_mode text, p_expires_at timestamptz
) returns table(correlation_id uuid, actor_user_id uuid, mode text, status text, expires_at timestamptz)
language plpgsql security definer set search_path = pg_catalog, private as $$
#variable_conflict use_column
declare v_intent private.game_character_onboarding_intents%rowtype; v_now timestamptz;
begin
  v_now := clock_timestamp();
  if p_actor_user_id is null or p_correlation_id is null or p_mode not in ('provision','claim') or p_expires_at is null
     or p_expires_at <= v_now or p_expires_at > v_now + interval '30 minutes' then
    raise exception using errcode='22023', message='onboarding requires actor, correlation, supported mode, and future expiry';
  end if;
  perform 1 from auth.users where id=p_actor_user_id;
  if not found then raise exception using errcode='22023', message='onboarding actor does not exist'; end if;
  perform pg_advisory_xact_lock(hashtextextended(p_actor_user_id::text,717126642));
  v_now := clock_timestamp();
  if p_expires_at <= v_now or p_expires_at > v_now + interval '30 minutes' then
    raise exception using errcode='22023', message='onboarding requires actor, correlation, supported mode, and future expiry';
  end if;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id=p_correlation_id for update;
  v_now := clock_timestamp();
  if p_expires_at <= v_now or p_expires_at > v_now + interval '30 minutes' then
    raise exception using errcode='22023', message='onboarding requires actor, correlation, supported mode, and future expiry';
  end if;
  if found then
    if v_intent.actor_user_id<>p_actor_user_id or v_intent.mode<>p_mode
       or (v_intent.status='started' and v_intent.expires_at<=v_now) then
      raise exception using errcode='P0001', message='correlation id belongs to a different onboarding payload';
    end if;
    return query select v_intent.correlation_id,v_intent.actor_user_id,v_intent.mode,v_intent.status,v_intent.expires_at; return;
  end if;
  if p_mode='claim' and (select count(*) from private.game_character_onboarding_intents recent_claim
    where recent_claim.actor_user_id=p_actor_user_id and recent_claim.mode='claim' and recent_claim.created_at>=v_now-interval '15 minutes')>=5 then
    raise exception using errcode='P0001',message='claim onboarding rate limit exceeded';
  end if;
  if exists(select 1 from private.game_character_onboarding_intents active where active.actor_user_id=p_actor_user_id and
    (active.status='provisioning' or (active.status='started' and active.expires_at>v_now))) then
    raise exception using errcode='P0001',message='actor already has an active onboarding intent';
  end if;
  insert into private.game_character_onboarding_intents(correlation_id,actor_user_id,mode,expires_at)
    values(p_correlation_id,p_actor_user_id,p_mode,p_expires_at) on conflict on constraint game_character_onboarding_intents_pkey do nothing;
  select * into v_intent from private.game_character_onboarding_intents where correlation_id=p_correlation_id for update;
  if not found or v_intent.actor_user_id<>p_actor_user_id or v_intent.mode<>p_mode then
    raise exception using errcode='P0001',message='correlation id belongs to a different onboarding payload';
  end if;
  return query select v_intent.correlation_id,v_intent.actor_user_id,v_intent.mode,v_intent.status,v_intent.expires_at;
end; $$;

comment on function public.begin_game_character_session(uuid,uuid,uuid,text,timestamptz) is
  'Gateway service_role only. Rechecks caller and retry lease expiry with clock_timestamp() after character and lease lock waits.';
comment on function public.renew_game_character_session(uuid,text,timestamptz) is
  'Gateway service_role only. Rechecks requested and existing lease expiry with clock_timestamp() after character and lease lock waits.';
comment on function public.begin_game_character_onboarding(uuid,uuid,text,timestamptz) is
  'Gateway service_role only. Rechecks requested expiry after the actor advisory lock; an expired started correlation is not a successful retry.';
