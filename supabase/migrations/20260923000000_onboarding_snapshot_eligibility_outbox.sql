-- 2026-09-23: an accepted onboarding handoff makes a character eligible for
-- its first durable snapshot.  This is intentionally only an eligibility
-- outbox: it neither transports nor creates a snapshot.

create table if not exists private.game_character_onboarding_snapshot_eligibility_outbox (
  correlation_id uuid primary key
    references private.game_character_onboarding_intents(correlation_id)
    on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  mode text not null,
  status text not null default 'pending',
  enqueued_at timestamptz not null default clock_timestamp(),
  constraint game_character_onboarding_snapshot_eligibility_outbox_mode
    check (mode in ('provision', 'claim')),
  constraint game_character_onboarding_snapshot_eligibility_outbox_status
    check (status = 'pending')
);

alter table private.game_character_onboarding_snapshot_eligibility_outbox
  enable row level security;

revoke all on table private.game_character_onboarding_snapshot_eligibility_outbox
  from public, anon, authenticated, service_role;

-- The handoff callback remains the only public transition.  It now also
-- creates exactly one pending eligibility row after validating the complete
-- handoff tuple.  Because this is one SECURITY DEFINER transaction, an
-- invalid callback cannot leave either activation or eligibility behind.
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
  v_activated_at timestamptz := clock_timestamp();
begin
  if p_actor_user_id is null or p_correlation_id is null or p_character_id is null
     or p_mode is null or p_mode not in ('provision', 'claim') then
    raise exception using errcode = '22023', message = 'onboarding handoff activation requires actor, correlation, character, and mode';
  end if;

  select * into v_intent
    from private.game_character_onboarding_intents
   where correlation_id = p_correlation_id
   for update;
  select * into v_handoff
    from private.game_character_onboarding_handoffs
   where correlation_id = p_correlation_id
   for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> p_mode
     or v_intent.status <> 'finalized' or v_handoff.actor_user_id <> p_actor_user_id
     or v_handoff.character_id <> p_character_id or v_handoff.mode <> p_mode then
    raise exception using errcode = 'P0001', message = 'onboarding handoff does not match exact activation tuple';
  end if;

  select * into v_character
    from public.game_characters
   where id = p_character_id
   for update;
  if not found or v_character.owner_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'onboarding handoff character is inconsistent';
  end if;

  -- Lock and verify an existing row before accepting an exact retry.  This
  -- makes a corrupt or substituted row fail closed instead of silently being
  -- treated as a completed eligibility handoff.
  select * into v_eligibility
    from private.game_character_onboarding_snapshot_eligibility_outbox
   where correlation_id = p_correlation_id
   for update;
  if found then
    if v_eligibility.actor_user_id <> p_actor_user_id
       or v_eligibility.character_id <> p_character_id
       or v_eligibility.mode <> p_mode
       or v_eligibility.status <> 'pending' then
      raise exception using errcode = 'P0001', message = 'onboarding snapshot eligibility outbox is inconsistent';
    end if;
  else
    insert into private.game_character_onboarding_snapshot_eligibility_outbox(
      correlation_id, actor_user_id, character_id, mode
    ) values (
      p_correlation_id, p_actor_user_id, p_character_id, p_mode
    );
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

  update public.game_characters
     set lifecycle = 'active'
   where id = p_character_id
  returning * into v_character;
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

comment on table private.game_character_onboarding_snapshot_eligibility_outbox is
  'Private correlation-keyed pending snapshot eligibility outbox, inserted only by exact onboarding handoff activation. A later reconciler must lock and fulfill these rows; no worker or transport is defined here.';

comment on function public.activate_game_character_onboarding_handoff(uuid, uuid, uuid, text) is
  'Gateway service_role only. Invoke only from the exact accepted C MUD1O COMMIT/CLAIMED write callback. Atomically activates only the matching pending actor/correlation/character/mode handoff, creates one pending snapshot eligibility outbox row, and treats exact activated retries as idempotent.';
