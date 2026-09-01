-- 2026-09-09: M3 legacy-authoritative shadow receipts.  This is additive:
-- no player bytes, ownership, lifecycle, or legacy file is written here.

do $$
declare
  v_writer oid;
begin
  if not exists (select 1 from pg_roles where rolname = 'mud_writer') then
    create role mud_writer nologin noinherit;
  end if;
  select oid into v_writer from pg_roles where rolname = 'mud_writer';
  if exists (select 1 from pg_auth_members where member = v_writer or roleid = v_writer) then
    raise exception using errcode = 'P0001', message = 'mud_writer role membership is unsafe';
  end if;
  alter role mud_writer nologin noinherit nosuperuser nocreatedb nocreaterole
    noreplication nobypassrls password null;
end
$$;

create table if not exists private.game_character_writer_epochs (
  world_id text primary key,
  writer_epoch bigint not null check (writer_epoch between 1 and 9223372036854775807),
  writer_instance_id uuid not null,
  issued_at timestamptz not null,
  expires_at timestamptz not null,
  sealed_at timestamptz,
  constraint game_character_writer_epochs_expiry_after_issue check (expires_at > issued_at)
);

create table if not exists private.game_character_writer_epoch_fences (
  world_id text not null,
  writer_epoch bigint not null check (writer_epoch between 1 and 9223372036854775807),
  writer_instance_id uuid not null,
  fenced_at timestamptz not null,
  successor_epoch bigint not null check (successor_epoch between 1 and 9223372036854775807),
  primary key (world_id, writer_epoch),
  constraint game_character_writer_epoch_fences_successor_check check (successor_epoch > writer_epoch)
);

create table if not exists private.game_character_legacy_heads (
  character_id uuid primary key references public.game_characters(id) on delete restrict,
  head_state text not null check (head_state in ('uninitialized', 'absent', 'existing')),
  head_sha256 text,
  storage_format smallint,
  revision bigint not null default 0 check (revision >= 0),
  writer_epoch bigint check (writer_epoch between 1 and 9223372036854775807),
  updated_at timestamptz not null default clock_timestamp(),
  constraint game_character_legacy_heads_shape_check check (
    (head_state = 'existing' and head_sha256 is not null and head_sha256 ~ '^[0-9a-f]{64}$'
      and storage_format is not null and storage_format > 0)
    or (head_state = 'absent' and head_sha256 is null
      and storage_format is not null and storage_format > 0)
    or (head_state = 'uninitialized' and head_sha256 is null and storage_format is null
      and revision = 0 and writer_epoch is null)
  )
);

create table if not exists private.game_character_shadow_receipts (
  character_id uuid not null references public.game_characters(id) on delete restrict,
  command_id uuid not null,
  world_id text not null,
  legacy_name_key text not null,
  request_sha256 text not null check (request_sha256 ~ '^[0-9a-f]{64}$'),
  writer_instance_id uuid not null,
  writer_epoch bigint not null check (writer_epoch between 1 and 9223372036854775807),
  writer_revision bigint not null check (writer_revision between 1 and 9223372036854775807),
  expected_state text not null check (expected_state in ('existing', 'absent')),
  expected_sha256 text,
  post_sha256 text not null check (post_sha256 ~ '^[0-9a-f]{64}$'),
  storage_format smallint not null check (storage_format > 0),
  acknowledged_at timestamptz not null default clock_timestamp(),
  primary key (character_id, command_id),
  constraint game_character_shadow_receipts_expected_hash_check check (
    (expected_state = 'existing' and expected_sha256 is not null and expected_sha256 ~ '^[0-9a-f]{64}$')
    or (expected_state = 'absent' and expected_sha256 is null)
  )
);

create index if not exists game_character_shadow_receipts_world_command_idx
  on private.game_character_shadow_receipts(world_id, command_id);

alter table private.game_character_writer_epochs enable row level security;
alter table private.game_character_writer_epoch_fences enable row level security;
alter table private.game_character_legacy_heads enable row level security;
alter table private.game_character_shadow_receipts enable row level security;

create or replace function private.m3_shadow_valid_world(p_world_id text)
returns boolean
language plpgsql
immutable
set search_path = pg_catalog
as $$
begin
  if p_world_id is null then
    raise exception using errcode = '22023', message = 'writer world id is null';
  end if;
  return p_world_id ~ '^[a-z][a-z0-9_-]{0,63}$';
end;
$$;

create or replace function private.game_character_shadow_request_sha256(
  p_world_id text,
  p_character_id uuid,
  p_legacy_name_key text,
  p_legacy_shard text,
  p_command_id uuid,
  p_writer_instance_id uuid,
  p_writer_epoch bigint,
  p_writer_revision bigint,
  p_expected_state text,
  p_expected_sha256 text,
  p_post_sha256 text,
  p_storage_format smallint
)
returns text
language plpgsql
immutable
set search_path = pg_catalog, private
as $$
declare
  v_envelope text;
begin
  if p_world_id is null or p_character_id is null or p_legacy_name_key is null
     or p_legacy_shard is null or p_command_id is null or p_writer_instance_id is null
     or p_writer_epoch is null or p_writer_revision is null or p_expected_state is null
     or p_post_sha256 is null or p_storage_format is null
     or not private.m3_shadow_valid_world(p_world_id)
     or octet_length(p_legacy_name_key) not between 1 and 14
     or p_legacy_shard !~ '^[0-9a-f]{2}$'
     or p_writer_epoch not between 1 and 9223372036854775807
     or p_writer_revision not between 1 and 9223372036854775807
     or p_storage_format <= 0
     or p_post_sha256 !~ '^[0-9a-f]{64}$'
     or p_expected_state not in ('existing', 'absent')
     or (p_expected_state = 'existing' and (p_expected_sha256 is null or p_expected_sha256 !~ '^[0-9a-f]{64}$'))
     or (p_expected_state = 'absent' and p_expected_sha256 is not null) then
    raise exception using errcode = '22023', message = 'shadow request digest arguments are invalid';
  end if;

  v_envelope :=
    'm3-shadow-receipt-v2' || E'\n' ||
    'world_id=' || p_world_id || E'\n' ||
    'character_id=' || p_character_id::text || E'\n' ||
    'legacy_name_key_hex=' || encode(convert_to(p_legacy_name_key, 'UTF8'), 'hex') || E'\n' ||
    'legacy_shard=' || p_legacy_shard || E'\n' ||
    'command_uuid=' || p_command_id::text || E'\n' ||
    'writer_instance_id=' || p_writer_instance_id::text || E'\n' ||
    'writer_epoch=' || p_writer_epoch::text || E'\n' ||
    'writer_revision=' || p_writer_revision::text || E'\n' ||
    'expected_state=' || p_expected_state || E'\n' ||
    'expected_sha256=' || coalesce(p_expected_sha256, '-') || E'\n' ||
    'post_sha256=' || p_post_sha256 || E'\n' ||
    'storage_format=' || p_storage_format::text || E'\n' ||
    'staged_leaf=' || p_command_id::text || '.stage' || E'\n';
  return encode(public.digest(convert_to(v_envelope, 'UTF8'), 'sha256'), 'hex');
end;
$$;

create or replace function private.resolve_game_character_writer_route(
  p_world_id text,
  p_legacy_name_key text
)
returns table (
  character_id uuid,
  legacy_name_key text,
  legacy_shard char(2),
  storage_format smallint,
  lifecycle public.character_lifecycle
)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
begin
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_legacy_name_key is null then
    raise exception using errcode = '22023', message = 'writer route arguments are invalid';
  end if;
  return query
    select c.id, c.legacy_name_key, c.legacy_shard, c.storage_format, c.lifecycle
      from public.game_characters c
     where c.world_id = p_world_id
       and c.legacy_name_key = p_legacy_name_key
       and c.lifecycle in ('imported_unclaimed', 'provisioning', 'active');
  if not found then
    raise exception using errcode = 'P0001', message = 'writer route is not eligible';
  end if;
end;
$$;

create or replace function private.acquire_game_world_writer_epoch(
  p_world_id text,
  p_writer_instance_id uuid,
  p_lease_expires_at timestamptz
)
returns table (writer_epoch bigint, expires_at timestamptz)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_epoch private.game_character_writer_epochs%rowtype;
  v_now timestamptz;
begin
  v_now := clock_timestamp();
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_writer_instance_id is null
     or p_lease_expires_at is null
     or p_lease_expires_at <= v_now
     or p_lease_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'writer epoch arguments are invalid';
  end if;
  perform pg_advisory_xact_lock(hashtextextended(p_world_id, 900090));
  select * into v_epoch from private.game_character_writer_epochs
   where world_id = p_world_id for update;
  v_now := clock_timestamp();
  if p_lease_expires_at <= v_now or p_lease_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'writer epoch arguments are invalid';
  end if;
  if not found then
    insert into private.game_character_writer_epochs(
      world_id, writer_epoch, writer_instance_id, issued_at, expires_at
    ) values (p_world_id, 1, p_writer_instance_id, v_now, p_lease_expires_at);
    return query select 1::bigint, p_lease_expires_at;
    return;
  end if;
  if v_epoch.writer_instance_id = p_writer_instance_id and v_epoch.sealed_at is null then
    update private.game_character_writer_epochs
       set expires_at = p_lease_expires_at
     where world_id = p_world_id;
    return query select v_epoch.writer_epoch, p_lease_expires_at;
    return;
  end if;
  if v_epoch.writer_instance_id = p_writer_instance_id
     or v_epoch.sealed_at is null or v_epoch.expires_at > v_now
     or v_epoch.writer_epoch = 9223372036854775807 then
    raise exception using errcode = 'P0001', message = 'writer epoch is held or predecessor is not drain-sealed';
  end if;
  insert into private.game_character_writer_epoch_fences(
    world_id, writer_epoch, writer_instance_id, fenced_at, successor_epoch
  ) values (p_world_id, v_epoch.writer_epoch, v_epoch.writer_instance_id, v_now, v_epoch.writer_epoch + 1);
  update private.game_character_writer_epochs
     set writer_epoch = v_epoch.writer_epoch + 1,
         writer_instance_id = p_writer_instance_id,
         issued_at = v_now,
         expires_at = p_lease_expires_at,
         sealed_at = null
   where world_id = p_world_id;
  return query select v_epoch.writer_epoch + 1, p_lease_expires_at;
end;
$$;

create or replace function private.renew_game_world_writer_epoch(
  p_world_id text,
  p_writer_instance_id uuid,
  p_writer_epoch bigint,
  p_lease_expires_at timestamptz
)
returns table (writer_epoch bigint, expires_at timestamptz)
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_epoch private.game_character_writer_epochs%rowtype;
  v_now timestamptz;
begin
  v_now := clock_timestamp();
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_writer_instance_id is null
     or p_writer_epoch is null
     or p_writer_epoch not between 1 and 9223372036854775807
     or p_lease_expires_at is null
     or p_lease_expires_at <= v_now
     or p_lease_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'writer renewal arguments are invalid';
  end if;
  perform pg_advisory_xact_lock(hashtextextended(p_world_id, 900090));
  select * into v_epoch from private.game_character_writer_epochs
   where world_id = p_world_id for update;
  v_now := clock_timestamp();
  if p_lease_expires_at <= v_now or p_lease_expires_at > v_now + interval '5 minutes' then
    raise exception using errcode = '22023', message = 'writer renewal arguments are invalid';
  end if;
  if not found or v_epoch.writer_instance_id <> p_writer_instance_id
     or v_epoch.writer_epoch <> p_writer_epoch or v_epoch.sealed_at is not null then
    raise exception using errcode = 'P0001', message = 'writer epoch cannot be renewed';
  end if;
  update private.game_character_writer_epochs set expires_at = p_lease_expires_at
   where world_id = p_world_id;
  return query select p_writer_epoch, p_lease_expires_at;
end;
$$;

create or replace function private.seal_game_world_writer_epoch(
  p_world_id text,
  p_writer_instance_id uuid,
  p_writer_epoch bigint
)
returns void
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_epoch private.game_character_writer_epochs%rowtype;
begin
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_writer_instance_id is null
     or p_writer_epoch is null
     or p_writer_epoch not between 1 and 9223372036854775807 then
    raise exception using errcode = '22023', message = 'writer seal arguments are invalid';
  end if;
  perform pg_advisory_xact_lock(hashtextextended(p_world_id, 900090));
  select * into v_epoch from private.game_character_writer_epochs
   where world_id = p_world_id for update;
  if not found or v_epoch.writer_instance_id <> p_writer_instance_id
     or v_epoch.writer_epoch <> p_writer_epoch or v_epoch.sealed_at is not null then
    raise exception using errcode = 'P0001', message = 'writer epoch cannot be sealed';
  end if;
  update private.game_character_writer_epochs set sealed_at = clock_timestamp()
   where world_id = p_world_id;
end;
$$;

create or replace function private.record_legacy_published_receipt(
  p_world_id text,
  p_legacy_name_key text,
  p_character_id uuid,
  p_command_id uuid,
  p_writer_instance_id uuid,
  p_request_sha256 text,
  p_writer_epoch bigint,
  p_writer_revision bigint,
  p_expected_state text,
  p_expected_sha256 text,
  p_post_sha256 text,
  p_storage_format smallint
)
returns void
language plpgsql
security definer
set search_path = pg_catalog, private
as $$
declare
  v_route public.game_characters%rowtype;
  v_epoch private.game_character_writer_epochs%rowtype;
  v_head private.game_character_legacy_heads%rowtype;
  v_receipt private.game_character_shadow_receipts%rowtype;
  v_request_sha256 text;
  v_now timestamptz;
begin
  if p_world_id is null or not private.m3_shadow_valid_world(p_world_id)
     or p_legacy_name_key is null
     or p_character_id is null or p_command_id is null or p_writer_instance_id is null
     or p_writer_epoch is null or p_writer_revision is null or p_expected_state is null
     or p_post_sha256 is null or p_request_sha256 is null or p_storage_format is null
     or p_writer_epoch not between 1 and 9223372036854775807
     or p_writer_revision not between 1 and 9223372036854775807
     or p_expected_state not in ('existing', 'absent')
     or (p_expected_state = 'existing' and (p_expected_sha256 is null or p_expected_sha256 !~ '^[0-9a-f]{64}$'))
     or (p_expected_state = 'absent' and p_expected_sha256 is not null)
     or p_post_sha256 !~ '^[0-9a-f]{64}$'
     or p_request_sha256 !~ '^[0-9a-f]{64}$'
     or p_storage_format <= 0 then
    raise exception using errcode = '22023', message = 'shadow receipt arguments are invalid';
  end if;
  perform pg_advisory_xact_lock(hashtextextended(p_world_id, 900090));
  select * into v_route from public.game_characters
   where world_id = p_world_id and legacy_name_key = p_legacy_name_key for share;
  if not found or v_route.id <> p_character_id
     or v_route.lifecycle not in ('imported_unclaimed', 'provisioning', 'active')
     or v_route.storage_format <> p_storage_format then
    raise exception using errcode = 'P0001', message = 'shadow receipt route is not eligible';
  end if;
  select * into v_epoch from private.game_character_writer_epochs
   where world_id = p_world_id for update;
  select * into v_head from private.game_character_legacy_heads
   where character_id = p_character_id for update;
  v_now := clock_timestamp();
  if v_epoch.world_id is null or v_epoch.writer_instance_id <> p_writer_instance_id
     or v_epoch.writer_epoch <> p_writer_epoch or v_epoch.sealed_at is not null
     or v_epoch.expires_at <= v_now then
    raise exception using errcode = 'P0001', message = 'shadow receipt writer epoch is stale or fenced';
  end if;
  v_request_sha256 := private.game_character_shadow_request_sha256(
    p_world_id, p_character_id, v_route.legacy_name_key, v_route.legacy_shard,
    p_command_id, p_writer_instance_id, p_writer_epoch, p_writer_revision,
    p_expected_state, p_expected_sha256, p_post_sha256, p_storage_format
  );
  if v_request_sha256 is null or v_request_sha256 <> p_request_sha256 then
    raise exception using errcode = '22023', message = 'shadow receipt digest is not canonical';
  end if;
  select * into v_receipt from private.game_character_shadow_receipts
   where character_id = p_character_id and command_id = p_command_id for key share;
  if found then
    if v_receipt.world_id = p_world_id
       and v_receipt.legacy_name_key = p_legacy_name_key
       and v_receipt.writer_instance_id = p_writer_instance_id
       and v_receipt.request_sha256 = p_request_sha256
       and v_receipt.writer_epoch = p_writer_epoch
       and v_receipt.writer_revision = p_writer_revision
       and v_receipt.expected_state = p_expected_state
       and v_receipt.expected_sha256 is not distinct from p_expected_sha256
       and v_receipt.post_sha256 = p_post_sha256
       and v_receipt.storage_format = p_storage_format then
      if v_head.character_id is null
         or v_head.head_state <> 'existing'
         or v_head.storage_format is distinct from v_receipt.storage_format
         or v_head.writer_epoch is distinct from v_receipt.writer_epoch
         or v_head.revision < v_receipt.writer_revision
         or (v_head.revision = v_receipt.writer_revision
             and v_head.head_sha256 is distinct from v_receipt.post_sha256) then
        raise exception using errcode = 'P0001', message = 'shadow receipt exact retry head is inconsistent';
      end if;
      return;
    end if;
    raise exception using errcode = 'P0001', message = 'shadow receipt command id has different payload';
  end if;
  if v_head.character_id is null then
    if p_expected_state = 'existing' and v_route.imported_file_sha256 is not null
       and v_route.imported_file_sha256 = p_expected_sha256 then
      insert into private.game_character_legacy_heads(
        character_id, head_state, head_sha256, storage_format, revision, writer_epoch, updated_at
      ) values (p_character_id, 'existing', p_expected_sha256, p_storage_format, 0, null, v_now);
    else
      raise exception using errcode = 'P0001', message = 'shadow receipt head is uninitialized';
    end if;
    select * into v_head from private.game_character_legacy_heads
     where character_id = p_character_id for update;
  end if;
  if p_writer_revision <> v_head.revision + 1
     or v_head.storage_format is distinct from p_storage_format
     or (p_expected_state = 'existing' and (v_head.head_state <> 'existing' or v_head.head_sha256 <> p_expected_sha256))
     or (p_expected_state = 'absent' and v_head.head_state <> 'absent') then
    raise exception using errcode = 'P0001', message = 'shadow receipt head compare-and-swap failed';
  end if;
  insert into private.game_character_shadow_receipts(
    character_id, command_id, world_id, legacy_name_key, request_sha256,
    writer_instance_id, writer_epoch, writer_revision, expected_state,
    expected_sha256, post_sha256, storage_format, acknowledged_at
  ) values (
    p_character_id, p_command_id, p_world_id, p_legacy_name_key, p_request_sha256,
    p_writer_instance_id, p_writer_epoch, p_writer_revision, p_expected_state,
    p_expected_sha256, p_post_sha256, p_storage_format, v_now
  );
  update private.game_character_legacy_heads
     set head_state = 'existing', head_sha256 = p_post_sha256,
         storage_format = p_storage_format, revision = p_writer_revision,
         writer_epoch = p_writer_epoch, updated_at = v_now
   where character_id = p_character_id;
end;
$$;

revoke all on schema private from public, anon, authenticated, service_role, mud_writer;
grant usage on schema private to mud_writer;
revoke all on table private.game_character_writer_epochs,
  private.game_character_writer_epoch_fences,
  private.game_character_legacy_heads,
  private.game_character_shadow_receipts
  from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.m3_shadow_valid_world(text) from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.game_character_shadow_request_sha256(text,uuid,text,text,uuid,uuid,bigint,bigint,text,text,text,smallint) from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.resolve_game_character_writer_route(text,text) from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.acquire_game_world_writer_epoch(text,uuid,timestamptz) from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.renew_game_world_writer_epoch(text,uuid,bigint,timestamptz) from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.seal_game_world_writer_epoch(text,uuid,bigint) from public, anon, authenticated, service_role, mud_writer;
revoke all on function private.record_legacy_published_receipt(text,text,uuid,uuid,uuid,text,bigint,bigint,text,text,text,smallint) from public, anon, authenticated, service_role, mud_writer;
grant execute on function private.resolve_game_character_writer_route(text,text) to mud_writer;
grant execute on function private.acquire_game_world_writer_epoch(text,uuid,timestamptz) to mud_writer;
grant execute on function private.renew_game_world_writer_epoch(text,uuid,bigint,timestamptz) to mud_writer;
grant execute on function private.seal_game_world_writer_epoch(text,uuid,bigint) to mud_writer;
grant execute on function private.record_legacy_published_receipt(text,text,uuid,uuid,uuid,text,bigint,bigint,text,text,text,smallint) to mud_writer;

comment on table private.game_character_writer_epochs is
  'M3 current world writer fence. Same persisted instance may renew an expired unsealed epoch; a sealed expired predecessor is required for successor installation.';
comment on table private.game_character_shadow_receipts is
  'M3 immutable acknowledgement of an already-published legacy hash. It contains no player bytes, password, JWT, ticket, ownership, or lifecycle mutation.';
comment on function private.record_legacy_published_receipt(text,text,uuid,uuid,uuid,text,bigint,bigint,text,text,text,smallint) is
  'mud_writer only. Revalidates world/name route to character ID, canonical request SHA-256, exact writer fence, and legacy-head CAS; DB is never a pre-publish authorizer.';
