-- 2026-09-03: private, service-only state for terminal-driven character
-- onboarding.  The C MUD remains the authority for wizard input and player
-- file writes; this migration never stores a game password, terminal input,
-- JWT, ticket, proof, or application secret.

create table if not exists private.game_character_onboarding_intents (
  correlation_id uuid primary key,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  mode text not null,
  status text not null default 'started',
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  completed_at timestamptz,
  constraint game_character_onboarding_intents_mode check (mode in ('provision', 'claim')),
  constraint game_character_onboarding_intents_status check (
    status in ('started', 'provisioning', 'finalized')
  ),
  constraint game_character_onboarding_intents_completion check (
    (status = 'finalized') = (completed_at is not null)
  )
);

create index if not exists game_character_onboarding_intents_actor_expires_idx
  on private.game_character_onboarding_intents(actor_user_id, expires_at);

create table if not exists private.game_character_provisioning_requests (
  correlation_id uuid primary key references private.game_character_onboarding_intents(correlation_id) on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  character_id uuid not null unique references public.game_characters(id) on delete restrict,
  world_id text not null,
  legacy_name_key text not null,
  status text not null default 'reserved',
  saved_file_sha256 text,
  storage_format smallint,
  finalized_at timestamptz,
  finalized_via text,
  created_at timestamptz not null default now(),
  constraint game_character_provisioning_requests_status check (status in ('reserved', 'finalized')),
  constraint game_character_provisioning_requests_save_state check (
    (status = 'reserved'
      and saved_file_sha256 is null
      and storage_format is null
      and finalized_at is null
      and finalized_via is null)
    or
    (status = 'finalized'
      and saved_file_sha256 ~ '^[0-9a-f]{64}$'
      and storage_format > 0
      and finalized_at is not null
      and finalized_via in ('finalize', 'reconcile'))
  )
);

create unique index if not exists game_character_provisioning_requests_world_name_idx
  on private.game_character_provisioning_requests(world_id, legacy_name_key);

alter table private.game_character_onboarding_intents enable row level security;
alter table private.game_character_provisioning_requests enable row level security;

revoke all on table private.game_character_onboarding_intents
  from public, anon, authenticated, service_role;
revoke all on table private.game_character_provisioning_requests
  from public, anon, authenticated, service_role;

-- Complete a reservation only after the Gateway has received structured C
-- save evidence.  `p_reconcile` is limited to the crash-window recovery RPC:
-- it permits a late receipt after intent expiry but keeps the same actor,
-- correlation, name reservation, digest, and one-way lifecycle checks.
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
  v_finalized_at timestamptz := now();
begin
  if p_actor_user_id is null or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'provisioning completion requires actor and correlation id';
  end if;
  if p_saved_file_sha256 is null
     or p_saved_file_sha256 !~ '^[0-9a-f]{64}$'
     or p_storage_format is null
     or p_storage_format <= 0 then
    raise exception using errcode = '22023', message = 'provisioning completion requires lowercase SHA-256 and positive storage format';
  end if;

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found or v_intent.actor_user_id <> p_actor_user_id or v_intent.mode <> 'provision' then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to actor provisioning request';
  end if;
  if not p_reconcile and v_intent.expires_at <= v_finalized_at then
    raise exception using errcode = 'P0001', message = 'onboarding intent has expired before finalize';
  end if;

  select * into v_request
    from private.game_character_provisioning_requests
    where correlation_id = p_correlation_id
    for update;
  if not found or v_request.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'provisioning request does not belong to actor';
  end if;

  if v_request.status = 'finalized' then
    if v_request.saved_file_sha256 <> p_saved_file_sha256
       or v_request.storage_format <> p_storage_format then
      raise exception using errcode = 'P0001', message = 'correlation id belongs to a different saved file payload';
    end if;

    select * into v_character
      from public.game_characters
      where id = v_request.character_id;
    if not found
       or v_character.owner_user_id <> p_actor_user_id
       or v_character.lifecycle <> 'active'
       or v_intent.status <> 'finalized' then
      raise exception using errcode = 'P0001', message = 'finalized provisioning state is inconsistent';
    end if;

    return query select
      v_character.id,
      v_character.owner_user_id,
      v_character.lifecycle,
      v_request.status,
      v_request.saved_file_sha256,
      v_request.storage_format;
    return;
  end if;

  if v_request.status <> 'reserved' or v_intent.status <> 'provisioning' then
    raise exception using errcode = 'P0001', message = 'invalid provisioning lifecycle transition';
  end if;

  select * into v_character
    from public.game_characters
    where id = v_request.character_id
    for update;
  if not found
     or v_character.owner_user_id <> p_actor_user_id
     or v_character.lifecycle <> 'provisioning'
     or v_character.world_id <> v_request.world_id
     or v_character.legacy_name_key <> v_request.legacy_name_key then
    raise exception using errcode = 'P0001', message = 'provisioning reservation is inconsistent';
  end if;

  update private.game_character_provisioning_requests
    set status = 'finalized',
        saved_file_sha256 = p_saved_file_sha256,
        storage_format = p_storage_format,
        finalized_at = v_finalized_at,
        finalized_via = case when p_reconcile then 'reconcile' else 'finalize' end
    where correlation_id = p_correlation_id;

  update public.game_characters
    set lifecycle = 'active',
        storage_format = p_storage_format,
        claimed_at = v_finalized_at
    where id = v_character.id;

  update private.game_character_onboarding_intents
    set status = 'finalized', completed_at = v_finalized_at
    where correlation_id = p_correlation_id;

  insert into public.game_identity_events (
    character_id, actor_user_id, event_type, correlation_id, details
  ) values (
    v_character.id,
    p_actor_user_id,
    'game_character_provisioned',
    p_correlation_id,
    jsonb_build_object(
      'storage_format', p_storage_format,
      'completion_path', case when p_reconcile then 'reconcile' else 'finalize' end
    )
  );

  return query select
    v_character.id,
    p_actor_user_id,
    'active'::public.character_lifecycle,
    'finalized'::text,
    p_saved_file_sha256,
    p_storage_format;
end;
$$;

revoke all on function private.complete_game_character_provisioning(uuid, uuid, text, smallint, boolean)
  from public, anon, authenticated, service_role;

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
begin
  if p_actor_user_id is null
     or p_correlation_id is null
     or p_mode not in ('provision', 'claim')
     or p_expires_at is null
     or p_expires_at <= now()
     or p_expires_at > now() + interval '30 minutes' then
    raise exception using errcode = '22023', message = 'onboarding requires actor, correlation, supported mode, and future expiry';
  end if;

  perform 1 from auth.users where id = p_actor_user_id;
  if not found then
    raise exception using errcode = '22023', message = 'onboarding actor does not exist';
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
end;
$$;

create or replace function public.begin_game_character_provisioning(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_world_id text,
  p_legacy_name text
)
returns table (
  character_id uuid,
  correlation_id uuid,
  actor_user_id uuid,
  world_id text,
  legacy_name_key text,
  lifecycle public.character_lifecycle,
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
  v_request private.game_character_provisioning_requests%rowtype;
  v_character_id uuid;
  v_legacy_name_key text;
  v_legacy_shard char(2);
begin
  if p_actor_user_id is null or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'provisioning requires actor and correlation id';
  end if;
  if nullif(btrim(p_world_id), '') is null
     or char_length(p_world_id) > 64
     or p_world_id ~ '[[:cntrl:]]' then
    raise exception using errcode = '22023', message = 'provisioning world is invalid';
  end if;

  v_legacy_name_key := private.game_identity_canonical_legacy_name(p_legacy_name);
  if v_legacy_name_key is null
     or char_length(v_legacy_name_key) not between 1 and 12
     or octet_length(v_legacy_name_key) > 14
     or v_legacy_name_key ~ E'[[:cntrl:]/\\:]'
     or v_legacy_name_key in ('.', '..') then
    raise exception using errcode = '22023', message = 'provisioning name is not C-safe';
  end if;

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found
     or v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> 'provision' then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to actor provisioning request';
  end if;
  if v_intent.expires_at <= now() then
    raise exception using errcode = 'P0001', message = 'onboarding intent has expired';
  end if;

  select * into v_request
    from private.game_character_provisioning_requests
    where correlation_id = p_correlation_id
    for update;
  if found then
    if v_request.actor_user_id <> p_actor_user_id
       or v_request.world_id <> p_world_id
       or v_request.legacy_name_key <> v_legacy_name_key then
      raise exception using errcode = 'P0001', message = 'correlation id belongs to a different provisioning payload';
    end if;

    return query select
      v_request.character_id,
      v_request.correlation_id,
      v_request.actor_user_id,
      v_request.world_id,
      v_request.legacy_name_key,
      case when v_request.status = 'finalized' then 'active'::public.character_lifecycle else 'provisioning'::public.character_lifecycle end,
      v_request.status,
      v_intent.expires_at;
    return;
  end if;

  if v_intent.status <> 'started' then
    raise exception using errcode = 'P0001', message = 'invalid onboarding lifecycle transition to provisioning';
  end if;

  v_legacy_shard := substr(
    encode(public.digest(convert_to(v_legacy_name_key, 'UTF8'), 'sha1'), 'hex'), 1, 2
  );
  insert into public.game_characters (
    world_id, legacy_name, legacy_name_key, legacy_shard, owner_user_id, lifecycle
  ) values (
    p_world_id, v_legacy_name_key, v_legacy_name_key, v_legacy_shard, p_actor_user_id, 'provisioning'
  ) on conflict on constraint game_characters_world_name_key_key do nothing
  returning id into v_character_id;
  if v_character_id is null then
    raise exception using errcode = 'P0001', message = 'canonical legacy name is already reserved or active';
  end if;

  insert into private.game_character_provisioning_requests (
    correlation_id, actor_user_id, character_id, world_id, legacy_name_key
  ) values (
    p_correlation_id, p_actor_user_id, v_character_id, p_world_id, v_legacy_name_key
  );

  update private.game_character_onboarding_intents
    set status = 'provisioning'
    where correlation_id = p_correlation_id;

  return query select
    v_character_id,
    p_correlation_id,
    p_actor_user_id,
    p_world_id,
    v_legacy_name_key,
    'provisioning'::public.character_lifecycle,
    'reserved'::text,
    v_intent.expires_at;
end;
$$;

create or replace function public.finalize_game_character_provisioning(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_saved_file_sha256 text,
  p_storage_format smallint
)
returns table (
  character_id uuid,
  actor_user_id uuid,
  lifecycle public.character_lifecycle,
  status text,
  saved_file_sha256 text,
  storage_format smallint
)
language sql
security definer
set search_path = pg_catalog, private
as $$
  select *
  from private.complete_game_character_provisioning(
    p_actor_user_id,
    p_correlation_id,
    p_saved_file_sha256,
    p_storage_format,
    false
  );
$$;

create or replace function public.reconcile_game_character_provisioning(
  p_actor_user_id uuid,
  p_correlation_id uuid,
  p_saved_file_sha256 text,
  p_storage_format smallint
)
returns table (
  character_id uuid,
  actor_user_id uuid,
  lifecycle public.character_lifecycle,
  status text,
  saved_file_sha256 text,
  storage_format smallint
)
language sql
security definer
set search_path = pg_catalog, private
as $$
  select *
  from private.complete_game_character_provisioning(
    p_actor_user_id,
    p_correlation_id,
    p_saved_file_sha256,
    p_storage_format,
    true
  );
$$;

-- The legacy password remains C-only.  This wrapper binds the already
-- existing claim transaction to the actor/mode/correlation intent and marks
-- that intent complete in the same database transaction.
create or replace function public.claim_legacy_game_character_onboarding(
  p_world_id text,
  p_legacy_name_key text,
  p_actor_user_id uuid,
  p_correlation_id uuid
)
returns table (
  character_id uuid,
  lifecycle public.character_lifecycle,
  owner_user_id uuid,
  claimed_at timestamptz,
  onboarding_status text
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
#variable_conflict use_column
declare
  v_intent private.game_character_onboarding_intents%rowtype;
  v_character_id uuid;
  v_lifecycle public.character_lifecycle;
  v_owner_user_id uuid;
  v_claimed_at timestamptz;
  v_completed_at timestamptz := now();
begin
  if nullif(btrim(p_world_id), '') is null
     or nullif(btrim(p_legacy_name_key), '') is null
     or p_actor_user_id is null
     or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'onboarding claim requires world, canonical name, actor, and correlation id';
  end if;

  select * into v_intent
    from private.game_character_onboarding_intents
    where correlation_id = p_correlation_id
    for update;
  if not found
     or v_intent.actor_user_id <> p_actor_user_id
     or v_intent.mode <> 'claim' then
    raise exception using errcode = 'P0001', message = 'onboarding intent does not belong to actor claim request';
  end if;
  if v_intent.status not in ('started', 'finalized') then
    raise exception using errcode = 'P0001', message = 'invalid onboarding lifecycle transition to claim';
  end if;
  if v_intent.status = 'started' and v_intent.expires_at <= v_completed_at then
    raise exception using errcode = 'P0001', message = 'onboarding intent has expired before claim';
  end if;

  select claimed.character_id, claimed.lifecycle,
         claimed.owner_user_id, claimed.claimed_at
    into v_character_id, v_lifecycle, v_owner_user_id, v_claimed_at
    from public.claim_legacy_game_character(
      p_world_id, p_legacy_name_key, p_actor_user_id, p_correlation_id
    ) as claimed;
  if not found
     or v_owner_user_id <> p_actor_user_id
     or v_lifecycle <> 'active'
     or v_claimed_at is null then
    raise exception using errcode = 'P0001', message = 'claimed character result is inconsistent';
  end if;

  if v_intent.status = 'started' then
    update private.game_character_onboarding_intents
      set status = 'finalized', completed_at = v_completed_at
      where correlation_id = p_correlation_id;
  end if;

  return query select
    v_character_id,
    v_lifecycle,
    v_owner_user_id,
    v_claimed_at,
    'finalized'::text;
end;
$$;

revoke all on function public.begin_game_character_onboarding(uuid, uuid, text, timestamptz)
  from public, anon, authenticated;
revoke all on function public.begin_game_character_provisioning(uuid, uuid, text, text)
  from public, anon, authenticated;
revoke all on function public.finalize_game_character_provisioning(uuid, uuid, text, smallint)
  from public, anon, authenticated;
revoke all on function public.reconcile_game_character_provisioning(uuid, uuid, text, smallint)
  from public, anon, authenticated;
revoke all on function public.claim_legacy_game_character_onboarding(text, text, uuid, uuid)
  from public, anon, authenticated;

grant execute on function public.begin_game_character_onboarding(uuid, uuid, text, timestamptz)
  to service_role;
grant execute on function public.begin_game_character_provisioning(uuid, uuid, text, text)
  to service_role;
grant execute on function public.finalize_game_character_provisioning(uuid, uuid, text, smallint)
  to service_role;
grant execute on function public.reconcile_game_character_provisioning(uuid, uuid, text, smallint)
  to service_role;
grant execute on function public.claim_legacy_game_character_onboarding(text, text, uuid, uuid)
  to service_role;

comment on function public.begin_game_character_onboarding(uuid, uuid, text, timestamptz) is
  'Gateway service_role only. Binds an actor UUID and retry correlation to a provision or claim onboarding intent; never pass a JWT, ticket, proof, password, or terminal input.';
comment on function public.begin_game_character_provisioning(uuid, uuid, text, text) is
  'Gateway service_role only. Reserves a canonical C-safe name for a provision onboarding intent; same correlation and payload is idempotent.';
comment on function public.finalize_game_character_provisioning(uuid, uuid, text, smallint) is
  'Gateway service_role only. Call only after the C PlayerStore reports a successful save with SHA-256 and storage format.';
comment on function public.reconcile_game_character_provisioning(uuid, uuid, text, smallint) is
  'Gateway service_role only. Recovers the C-save/DB-finalize crash window from the same actor, correlation, SHA-256, and storage format.';
comment on function public.claim_legacy_game_character_onboarding(text, text, uuid, uuid) is
  'Gateway service_role only. Call only after C verifies the legacy game password; atomically binds the claim result to the matching actor claim intent and never accepts a password or proof.';
