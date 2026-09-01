-- 2026-09-02: account-to-character ownership index and service-only claim RPC.
--
-- This migration deliberately does not import player files, inspect legacy
-- passwords, or make Postgres the authority for MUD game state.  A trusted C
-- control must verify the legacy password/proof before the Gateway invokes the
-- RPC below.

create extension if not exists pgcrypto;

do $$
begin
  if not exists (
    select 1 from pg_type where typnamespace = 'public'::regnamespace
      and typname = 'character_lifecycle'
  ) then
    create type public.character_lifecycle as enum (
      'imported_unclaimed', 'claiming', 'provisioning',
      'active', 'suspended', 'retired'
    );
  end if;

  if not exists (select 1 from pg_roles where rolname = 'service_role') then
    raise exception 'self-hosted Supabase service_role database role is required';
  end if;
end
$$;

create schema if not exists private;

-- Match legacy C lowercize(name, 1) without locale-dependent lower()/upper():
-- fold every ASCII uppercase byte, then capitalize only an ASCII first byte.
create or replace function private.game_identity_canonical_legacy_name(p_name text)
returns text
language sql
immutable
strict
parallel safe
set search_path = pg_catalog
as $$
  with folded(value) as (
    select translate(
      p_name,
      'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
      'abcdefghijklmnopqrstuvwxyz'
    )
  )
  select case
    when ascii(nullif(left(value, 1), '')) between 97 and 122
      then overlay(value placing chr(ascii(left(value, 1)) - 32) from 1 for 1)
    else value
  end
  from folded;
$$;

create table if not exists public.game_characters (
  id uuid primary key default gen_random_uuid(),
  world_id text not null default 'muhan',
  -- This must be produced by the shared C/Gateway canonicalizer, not lower().
  legacy_name text not null,
  legacy_name_key text not null,
  legacy_shard char(2) not null,
  owner_user_id uuid references auth.users(id) on delete restrict,
  lifecycle public.character_lifecycle not null default 'imported_unclaimed',
  storage_format smallint not null default 1,
  imported_file_sha256 text,
  claimed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint game_characters_world_name_key_key unique (world_id, legacy_name_key),
  constraint game_characters_world_id_bounded check (
    char_length(world_id) between 1 and 64 and world_id !~ '[[:cntrl:]]'
  ),
  constraint game_characters_legacy_names_c_safe check (
    char_length(legacy_name) between 1 and 12
    and octet_length(legacy_name) <= 14
    and legacy_name !~ E'[[:cntrl:]/\\\\:]'
    and char_length(legacy_name_key) between 1 and 12
    and octet_length(legacy_name_key) <= 14
    and legacy_name_key !~ E'[[:cntrl:]/\\\\:]'
    and legacy_name = private.game_identity_canonical_legacy_name(legacy_name)
    and legacy_name_key = legacy_name
  ),
  constraint game_characters_reserved_names check (
    legacy_name not in ('.', '..') and legacy_name_key not in ('.', '..')
  )
);

alter table public.game_characters
  add column if not exists owner_user_id uuid references auth.users(id) on delete restrict,
  add column if not exists lifecycle public.character_lifecycle not null default 'imported_unclaimed',
  add column if not exists storage_format smallint not null default 1,
  add column if not exists imported_file_sha256 text,
  add column if not exists claimed_at timestamptz,
  add column if not exists created_at timestamptz not null default now(),
  add column if not exists updated_at timestamptz not null default now();

do $$
begin
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_world_name_key_key'
  ) then
    alter table public.game_characters
      add constraint game_characters_world_name_key_key
      unique (world_id, legacy_name_key);
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_world_id_bounded'
  ) then
    alter table public.game_characters
      add constraint game_characters_world_id_bounded
      check (char_length(world_id) between 1 and 64 and world_id !~ '[[:cntrl:]]');
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_legacy_names_c_safe'
  ) then
    alter table public.game_characters
      add constraint game_characters_legacy_names_c_safe
      check (
        char_length(legacy_name) between 1 and 12
        and octet_length(legacy_name) <= 14
        and legacy_name !~ E'[[:cntrl:]/\\\\:]'
        and char_length(legacy_name_key) between 1 and 12
        and octet_length(legacy_name_key) <= 14
        and legacy_name_key !~ E'[[:cntrl:]/\\\\:]'
        and legacy_name = private.game_identity_canonical_legacy_name(legacy_name)
        and legacy_name_key = legacy_name
      );
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_reserved_names'
  ) then
    alter table public.game_characters
      add constraint game_characters_reserved_names
      check (legacy_name not in ('.', '..') and legacy_name_key not in ('.', '..'));
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_name_not_blank'
  ) then
    alter table public.game_characters
      add constraint game_characters_name_not_blank
      check (length(btrim(legacy_name)) > 0 and length(btrim(legacy_name_key)) > 0);
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_shard_matches_key'
  ) then
    alter table public.game_characters
      add constraint game_characters_shard_matches_key
      check (
        legacy_shard = substr(
          encode(digest(convert_to(legacy_name_key, 'UTF8'), 'sha1'), 'hex'), 1, 2
        )
      );
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_imported_has_no_owner'
  ) then
    alter table public.game_characters
      add constraint game_characters_imported_has_no_owner
      check (lifecycle <> 'imported_unclaimed' or owner_user_id is null);
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_active_is_owned'
  ) then
    alter table public.game_characters
      add constraint game_characters_active_is_owned
      check (lifecycle <> 'active' or (owner_user_id is not null and claimed_at is not null));
  end if;
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_characters'::regclass
      and conname = 'game_characters_import_sha256_format'
  ) then
    alter table public.game_characters
      add constraint game_characters_import_sha256_format
      check (imported_file_sha256 is null or imported_file_sha256 ~ '^[0-9a-f]{64}$');
  end if;
end
$$;

create index if not exists game_characters_owner_active_idx
  on public.game_characters(owner_user_id)
  where lifecycle = 'active';

-- There are intentionally no password, proof, JWT, or ticket columns.  Audit
-- payloads are also checked recursively so a future nested object cannot
-- smuggle one of those values into the extensible details field.
create or replace function private.game_identity_json_is_safe(p_value jsonb)
returns boolean
language plpgsql
immutable
strict
set search_path = pg_catalog
as $$
declare
  v_key text;
  v_normalized_key text;
  v_child jsonb;
begin
  case jsonb_typeof(p_value)
    when 'object' then
      for v_key, v_child in select entry.key, entry.value from jsonb_each(p_value) as entry loop
        v_normalized_key := regexp_replace(lower(v_key), '[^[:alnum:]]', '', 'g');
        if v_normalized_key ~ '(password|passwd|proof|jwt|token|ticket|authorization|bearer|secret)'
           or not private.game_identity_json_is_safe(v_child) then
          return false;
        end if;
      end loop;
    when 'array' then
      for v_child in select entry.value from jsonb_array_elements(p_value) as entry loop
        if not private.game_identity_json_is_safe(v_child) then
          return false;
        end if;
      end loop;
  end case;
  return true;
end;
$$;

create or replace function private.game_identity_event_details_are_safe(details jsonb)
returns boolean
language sql
immutable
strict
set search_path = pg_catalog, private
as $$
  select jsonb_typeof(details) = 'object'
     and private.game_identity_json_is_safe(details);
$$;

create table if not exists public.game_identity_events (
  id bigint generated always as identity primary key,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  actor_user_id uuid references auth.users(id) on delete set null,
  event_type text not null,
  correlation_id uuid not null,
  occurred_at timestamptz not null default now(),
  details jsonb not null default '{}'::jsonb,
  constraint game_identity_events_event_type_bounded
    check (char_length(event_type) between 1 and 64 and event_type !~ '[[:cntrl:]]'),
  constraint game_identity_events_details_safe
    check (private.game_identity_event_details_are_safe(details)),
  constraint game_identity_events_claim_correlation_key
    unique (event_type, correlation_id)
);

do $$
begin
  if not exists (
    select 1 from pg_constraint where conrelid = 'public.game_identity_events'::regclass
      and conname = 'game_identity_events_event_type_bounded'
  ) then
    alter table public.game_identity_events
      add constraint game_identity_events_event_type_bounded
      check (char_length(event_type) between 1 and 64 and event_type !~ '[[:cntrl:]]');
  end if;
end
$$;

create index if not exists game_identity_events_character_occurred_idx
  on public.game_identity_events(character_id, occurred_at desc);

-- This private request row is the correlation-id lock.  It makes retrying one
-- verified claim safe and prevents the same correlation id from claiming two
-- characters concurrently.
create table if not exists private.game_character_claim_requests (
  correlation_id uuid primary key,
  character_id uuid not null references public.game_characters(id) on delete restrict,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  created_at timestamptz not null default now(),
  completed_at timestamptz
);

create table if not exists private.game_character_sessions (
  character_id uuid primary key references public.game_characters(id) on delete restrict,
  session_id uuid not null unique,
  actor_user_id uuid not null references auth.users(id) on delete restrict,
  gateway_instance_id text not null,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  constraint game_character_sessions_gateway_instance_bounded check (
    char_length(gateway_instance_id) between 1 and 128
    and gateway_instance_id !~ '[[:cntrl:]]'
  ),
  constraint game_character_sessions_expiry_after_create check (expires_at > created_at)
);

alter table private.game_character_sessions
  add column if not exists actor_user_id uuid references auth.users(id) on delete restrict;

update private.game_character_sessions lease
  set actor_user_id = character.owner_user_id
  from public.game_characters character
  where lease.character_id = character.id
    and lease.actor_user_id is null;

alter table private.game_character_sessions
  alter column actor_user_id set not null;

do $$
begin
  if not exists (
    select 1 from pg_constraint where conrelid = 'private.game_character_sessions'::regclass
      and conname = 'game_character_sessions_gateway_instance_bounded'
  ) then
    alter table private.game_character_sessions
      add constraint game_character_sessions_gateway_instance_bounded
      check (
        char_length(gateway_instance_id) between 1 and 128
        and gateway_instance_id !~ '[[:cntrl:]]'
      );
  end if;
end
$$;

create index if not exists game_character_sessions_expires_at_idx
  on private.game_character_sessions(expires_at);

-- Snapshot data is an optional read model only.  No browser role is granted
-- access and this migration does not publish any snapshot.
create table if not exists private.game_character_snapshots (
  character_id uuid not null references public.game_characters(id) on delete restrict,
  revision bigint not null check (revision >= 0),
  storage_format smallint not null check (storage_format > 0),
  blob_ref text,
  sha256 text not null check (sha256 ~ '^[0-9a-f]{64}$'),
  saved_at timestamptz not null default now(),
  primary key (character_id, revision)
);

create or replace function private.touch_game_character_updated_at()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  new.updated_at := now();
  return new;
end;
$$;

drop trigger if exists game_characters_touch_updated_at on public.game_characters;
create trigger game_characters_touch_updated_at
  before update on public.game_characters
  for each row execute function private.touch_game_character_updated_at();

alter table public.game_characters enable row level security;
alter table public.game_identity_events enable row level security;
alter table private.game_character_claim_requests enable row level security;
alter table private.game_character_sessions enable row level security;
alter table private.game_character_snapshots enable row level security;

revoke all on schema private from public, anon, authenticated;
revoke all on function private.game_identity_canonical_legacy_name(text)
  from public, anon, authenticated, service_role;
revoke all on table public.game_characters from public, anon, authenticated;
revoke all on table public.game_identity_events from public, anon, authenticated;
revoke all on table private.game_character_claim_requests from public, anon, authenticated;
revoke all on table private.game_character_sessions from public, anon, authenticated;
revoke all on table private.game_character_snapshots from public, anon, authenticated;
revoke all on sequence public.game_identity_events_id_seq
  from public, anon, authenticated;

grant select on table public.game_characters to authenticated;

drop policy if exists game_characters_select_own_active on public.game_characters;
create policy game_characters_select_own_active
  on public.game_characters
  for select to authenticated
  using (
    owner_user_id = (select auth.uid())
    and lifecycle = 'active'
  );

-- service_role is the Gateway's DB identity.  Browser clients never receive
-- this role or its key.  Its write access is only through narrowly scoped
-- SECURITY DEFINER RPCs; future snapshot work needs another such RPC.
revoke all on schema private from service_role;
revoke all on table public.game_characters from service_role;
revoke all on table public.game_identity_events from service_role;
revoke all on table private.game_character_claim_requests from service_role;
revoke all on table private.game_character_sessions from service_role;
revoke all on table private.game_character_snapshots from service_role;
revoke all on sequence public.game_identity_events_id_seq from service_role;

create or replace function public.claim_legacy_game_character(
  p_world_id text,
  p_legacy_name_key text,
  p_actor_user_id uuid,
  p_correlation_id uuid
)
returns table (
  character_id uuid,
  lifecycle public.character_lifecycle,
  owner_user_id uuid,
  claimed_at timestamptz
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_character public.game_characters%rowtype;
  v_request private.game_character_claim_requests%rowtype;
begin
  if nullif(btrim(p_world_id), '') is null
     or nullif(btrim(p_legacy_name_key), '') is null
     or p_actor_user_id is null
     or p_correlation_id is null then
    raise exception using errcode = '22023', message = 'claim requires world, canonical name key, actor, and correlation id';
  end if;

  perform 1 from auth.users where id = p_actor_user_id;
  if not found then
    raise exception using errcode = '22023', message = 'claim actor does not exist';
  end if;

  -- The character lock serializes competing correlation ids for one name.
  select * into v_character
    from public.game_characters
    where world_id = p_world_id and legacy_name_key = p_legacy_name_key
    for update;
  if not found then
    raise exception using errcode = 'P0001', message = 'legacy character is not claimable';
  end if;

  -- The request PK serializes a retried correlation id across all characters.
  insert into private.game_character_claim_requests (
    correlation_id, character_id, actor_user_id
  ) values (
    p_correlation_id, v_character.id, p_actor_user_id
  ) on conflict (correlation_id) do nothing;

  select * into v_request
    from private.game_character_claim_requests
    where correlation_id = p_correlation_id
    for update;

  if v_request.character_id <> v_character.id
     or v_request.actor_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'correlation id belongs to a different claim';
  end if;

  if v_request.completed_at is not null then
    return query
      select c.id, c.lifecycle, c.owner_user_id, c.claimed_at
      from public.game_characters c where c.id = v_character.id;
    return;
  end if;

  if v_character.lifecycle <> 'imported_unclaimed' or v_character.owner_user_id is not null then
    raise exception using errcode = 'P0001', message = 'legacy character is already claimed or unavailable';
  end if;

  -- Keep the declared lifecycle transition explicit even though this one
  -- transaction exposes only the committed active state to other sessions.
  update public.game_characters
    set lifecycle = 'claiming', owner_user_id = p_actor_user_id
    where id = v_character.id;

  update public.game_characters
    set lifecycle = 'active', claimed_at = now()
    where id = v_character.id;

  insert into public.game_identity_events (
    character_id, actor_user_id, event_type, correlation_id, details
  ) values (
    v_character.id,
    p_actor_user_id,
    'legacy_character_claimed',
    p_correlation_id,
    jsonb_build_object('source', 'legacy-password-verified')
  );

  update private.game_character_claim_requests
    set completed_at = now()
    where correlation_id = p_correlation_id;

  return query
    select c.id, c.lifecycle, c.owner_user_id, c.claimed_at
    from public.game_characters c where c.id = v_character.id;
end;
$$;

revoke all on function public.claim_legacy_game_character(text, text, uuid, uuid)
  from public, anon, authenticated;
grant execute on function public.claim_legacy_game_character(text, text, uuid, uuid)
  to service_role;

comment on function public.claim_legacy_game_character(text, text, uuid, uuid) is
  'Gateway service_role only. Call only after trusted C legacy-password verification; never pass password, proof, JWT, or ticket.';

create or replace function public.begin_game_character_session(
  p_actor_user_id uuid,
  p_character_id uuid,
  p_session_id uuid,
  p_gateway_instance_id text,
  p_expires_at timestamptz
)
returns table (
  character_id uuid,
  session_id uuid,
  expires_at timestamptz,
  owner_user_id uuid,
  lifecycle public.character_lifecycle,
  legacy_name_key text
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_character public.game_characters%rowtype;
  v_lease private.game_character_sessions%rowtype;
begin
  if p_actor_user_id is null or p_character_id is null or p_session_id is null
     or nullif(btrim(p_gateway_instance_id), '') is null
     or char_length(p_gateway_instance_id) > 128
     or p_gateway_instance_id ~ '[[:cntrl:]]'
     or p_expires_at is null
     or p_expires_at <= now()
     or p_expires_at > now() + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'session lease arguments are invalid';
  end if;

  -- The character lock serializes concurrent begin requests before a lease
  -- row is inserted or replaced.
  select * into v_character
    from public.game_characters
    where id = p_character_id
    for update;
  if not found or v_character.lifecycle <> 'active'
     or v_character.owner_user_id <> p_actor_user_id then
    raise exception using errcode = 'P0001', message = 'character is not an active character owned by actor';
  end if;

  select * into v_lease
    from private.game_character_sessions
    where character_id = p_character_id
    for update;

  if found then
    if v_lease.session_id = p_session_id then
      if v_lease.actor_user_id <> p_actor_user_id
         or v_lease.gateway_instance_id <> p_gateway_instance_id then
        raise exception using errcode = 'P0001', message = 'session lease owner does not match retry';
      end if;
      -- Retrying the same Gateway request is idempotent: it neither creates a
      -- second lease nor extends a lease beyond the original issue decision.
      return query select
        v_lease.character_id,
        v_lease.session_id,
        v_lease.expires_at,
        v_character.owner_user_id,
        v_character.lifecycle,
        v_character.legacy_name_key;
      return;
    end if;
    if v_lease.expires_at > now() then
      raise exception using errcode = 'P0001', message = 'character already has an active session lease';
    end if;
    update private.game_character_sessions
      set session_id = p_session_id,
          actor_user_id = p_actor_user_id,
          gateway_instance_id = p_gateway_instance_id,
          expires_at = p_expires_at,
          created_at = now()
      where character_id = p_character_id;
  else
    insert into private.game_character_sessions (
      character_id, session_id, actor_user_id, gateway_instance_id, expires_at
    ) values (
      p_character_id, p_session_id, p_actor_user_id, p_gateway_instance_id, p_expires_at
    );
  end if;

  return query
    select
      lease.character_id,
      lease.session_id,
      lease.expires_at,
      v_character.owner_user_id,
      v_character.lifecycle,
      v_character.legacy_name_key
    from private.game_character_sessions lease
    where lease.character_id = p_character_id;
end;
$$;

create or replace function public.renew_game_character_session(
  p_session_id uuid,
  p_gateway_instance_id text,
  p_expires_at timestamptz
)
returns table (
  character_id uuid,
  session_id uuid,
  expires_at timestamptz,
  owner_user_id uuid,
  lifecycle public.character_lifecycle,
  legacy_name_key text
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_character_id uuid;
  v_character public.game_characters%rowtype;
  v_lease private.game_character_sessions%rowtype;
begin
  if p_session_id is null
     or nullif(btrim(p_gateway_instance_id), '') is null
     or char_length(p_gateway_instance_id) > 128
     or p_gateway_instance_id ~ '[[:cntrl:]]'
     or p_expires_at is null
     or p_expires_at <= now()
     or p_expires_at > now() + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'session lease renewal arguments are invalid';
  end if;

  -- Resolve without a lock, then take locks in the same character -> lease
  -- order as begin_game_character_session to avoid a renewal/takeover deadlock.
  select lease.character_id into v_character_id
    from private.game_character_sessions lease
    where lease.session_id = p_session_id;
  if not found then
    raise exception using errcode = 'P0001', message = 'session lease cannot be renewed';
  end if;

  select * into v_character
    from public.game_characters
    where id = v_character_id
    for update;

  select * into v_lease
    from private.game_character_sessions lease
    where lease.session_id = p_session_id
      and lease.character_id = v_character_id
    for update;

  if not found
     or v_character.lifecycle <> 'active'
     or v_character.owner_user_id is null
     or v_lease.actor_user_id <> v_character.owner_user_id
     or v_lease.gateway_instance_id <> p_gateway_instance_id
     or v_lease.expires_at <= now() then
    raise exception using errcode = 'P0001', message = 'session lease cannot be renewed';
  end if;

  update private.game_character_sessions lease
    set expires_at = p_expires_at
    where lease.session_id = p_session_id;

  return query select
    v_lease.character_id,
    v_lease.session_id,
    p_expires_at,
    v_character.owner_user_id,
    v_character.lifecycle,
    v_character.legacy_name_key;
end;
$$;

drop function if exists public.end_game_character_session(uuid);

create or replace function public.end_game_character_session(
  p_session_id uuid,
  p_gateway_instance_id text
)
returns boolean
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
begin
  if p_session_id is null
     or nullif(btrim(p_gateway_instance_id), '') is null
     or char_length(p_gateway_instance_id) > 128
     or p_gateway_instance_id ~ '[[:cntrl:]]' then
    raise exception using errcode = '22023', message = 'session release arguments are invalid';
  end if;
  delete from private.game_character_sessions
    where session_id = p_session_id
      and gateway_instance_id = p_gateway_instance_id;
  return found;
end;
$$;

revoke all on function public.begin_game_character_session(uuid, uuid, uuid, text, timestamptz)
  from public, anon, authenticated;
revoke all on function public.renew_game_character_session(uuid, text, timestamptz)
  from public, anon, authenticated;
revoke all on function public.end_game_character_session(uuid, text)
  from public, anon, authenticated;
grant execute on function public.begin_game_character_session(uuid, uuid, uuid, text, timestamptz)
  to service_role;
grant execute on function public.renew_game_character_session(uuid, text, timestamptz)
  to service_role;
grant execute on function public.end_game_character_session(uuid, text)
  to service_role;

comment on function public.begin_game_character_session(uuid, uuid, uuid, text, timestamptz) is
  'Gateway service_role only. Atomically creates one active owner-checked lease and returns its canonical legacy name; raw browser token or admission ticket is never stored.';

comment on function public.renew_game_character_session(uuid, text, timestamptz) is
  'Gateway service_role only. Renews only the same live session, gateway instance, active owner, and canonical character mapping.';

comment on function public.end_game_character_session(uuid, text) is
  'Gateway service_role only. Deletes only the exact session id owned by the same Gateway instance.';

comment on table public.game_characters is
  'Ownership index only; legacy player file remains the game-state authority.';

comment on table private.game_character_snapshots is
  'Optional read model; not a browser API or gameplay source of truth.';
