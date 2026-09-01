-- 2026-09-04: operational safety for self-service character onboarding.
--
-- A user may retry the same correlation idempotently, but may not fan out
-- multiple live wizards. A provisioning reservation remains blocking even
-- after its browser intent expires because a C player file may already exist
-- and must be reconciled before another flow is opened.

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_provisioning_reserved_admin_names'
  ) then
    alter table public.game_characters
      add constraint game_characters_provisioning_reserved_admin_names check (
        lifecycle <> 'provisioning'
        or legacy_name_key not in ('테스토스', '꽃', '뽀뽀')
      );
  end if;
end
$$;

alter table private.game_character_onboarding_intents
  drop constraint if exists game_character_onboarding_intents_status,
  drop constraint if exists game_character_onboarding_intents_completion;

alter table private.game_character_onboarding_intents
  add constraint game_character_onboarding_intents_status check (
    status in ('started', 'provisioning', 'finalized', 'cancelled')
  ),
  add constraint game_character_onboarding_intents_completion check (
    (status in ('finalized', 'cancelled')) = (completed_at is not null)
  );

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

  -- Serialize new correlations for one actor. The 64-bit seeded hash keeps
  -- this advisory-lock namespace separate from unrelated application locks.
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
  'Gateway service_role only. Idempotently reuses one correlation and rejects concurrent live onboarding intents for the same actor; a provisioning intent remains blocked until finalized or explicitly reconciled.';

create or replace function public.cancel_unreserved_game_character_onboarding(
  p_actor_user_id uuid,
  p_correlation_id uuid
)
returns table (
  correlation_id uuid,
  actor_user_id uuid,
  status text,
  completed_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
#variable_conflict use_column
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_completed_at timestamptz := now();
begin
  if p_actor_user_id is null or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'onboarding cancellation requires actor and correlation id';
  end if;

  perform pg_advisory_xact_lock(hashtextextended(p_actor_user_id::text, 717126642));
  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to cancellation actor';
  end if;

  if v_intent.status = 'cancelled' then
    return query select
      v_intent.correlation_id,
      v_intent.actor_user_id,
      v_intent.status,
      v_intent.completed_at;
    return;
  end if;
  if v_intent.status <> 'started'
     or exists (
       select 1
       from private.game_character_provisioning_requests as request
       where request.correlation_id = p_correlation_id
     ) then
    raise exception using errcode = 'P0001', message = 'reserved or completed onboarding cannot be cancelled by the unreserved path';
  end if;

  update private.game_character_onboarding_intents
    set status = 'cancelled', completed_at = v_completed_at
    where correlation_id = p_correlation_id
    returning * into v_intent;

  return query select
    v_intent.correlation_id,
    v_intent.actor_user_id,
    v_intent.status,
    v_intent.completed_at;
end;
$$;

revoke all on function public.cancel_unreserved_game_character_onboarding(uuid, uuid)
  from public, anon, authenticated;
grant execute on function public.cancel_unreserved_game_character_onboarding(uuid, uuid)
  to service_role;

comment on function public.cancel_unreserved_game_character_onboarding(uuid, uuid) is
  'Gateway service_role only. Cancels only a started intent that has no provisioning reservation; it cannot delete or release a character name or saved-file claim.';
