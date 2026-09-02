-- 2026-09-11: M3 writer actual-login boundary.
--
-- mud_writer remains a NOLOGIN capability role.  The only login principal is
-- mud_writer_login, which starts each session as mud_writer without inheriting
-- that capability.  An operator may set mud_writer_login's password hash after
-- this migration; replay deliberately never changes that hash.

do $$
declare
  v_writer oid;
  v_login oid;
begin
  if not exists (select 1 from pg_roles where rolname = 'mud_writer') then
    raise exception using errcode = 'P0001', message = 'mud_writer capability role is required';
  end if;

  if not exists (select 1 from pg_roles where rolname = 'mud_writer_login') then
    create role mud_writer_login login noinherit nosuperuser nocreatedb nocreaterole
      noreplication nobypassrls password null;
  end if;

  select oid into v_writer from pg_roles where rolname = 'mud_writer';
  select oid into v_login from pg_roles where rolname = 'mud_writer_login';

  -- There may be precisely one related membership: login may SET ROLE to the
  -- writer capability.  Any additional edge could create an alternate login,
  -- inherited privilege, or escalation route, so migration replay fails closed.
  if exists (
    select 1
      from pg_auth_members m
     where (m.roleid in (v_writer, v_login) or m.member in (v_writer, v_login))
       and not (m.roleid = v_writer and m.member = v_login)
  ) then
    raise exception using errcode = 'P0001', message = 'M3 writer role membership is unsafe';
  end if;

  -- Correct a legacy version of the one allowed edge to the exact PG17
  -- options.  REVOKE/GRANT also makes replay deterministic.
  if exists (
    select 1 from pg_auth_members
     where roleid = v_writer and member = v_login
  ) then
    revoke mud_writer from mud_writer_login;
  end if;
  grant mud_writer to mud_writer_login
    with admin false, inherit false, set true;

  -- Keep the capability role unusable as a direct connection and strip every
  -- privilege-bearing attribute.  Its password is intentionally null.
  alter role mud_writer nologin noinherit nosuperuser nocreatedb nocreaterole
    noreplication nobypassrls password null;

  -- Do not mention PASSWORD here.  In particular, a replay must preserve an
  -- operator-provisioned SCRAM/MD5 hash for the actual login role.
  alter role mud_writer_login login noinherit nosuperuser nocreatedb nocreaterole
    noreplication nobypassrls;
end
$$;

-- This dedicated session has no ambient settings.  It always begins as the
-- capability role, with bounded statements/locks/idle transactions and no
-- user-writable schemas in its path.
alter role mud_writer reset all;
alter role mud_writer_login reset all;
alter role mud_writer_login set role to mud_writer;
alter role mud_writer_login set statement_timeout to '5s';
alter role mud_writer_login set lock_timeout to '1s';
alter role mud_writer_login set idle_in_transaction_session_timeout to '5s';
alter role mud_writer_login set search_path to pg_catalog;

create or replace function private.m3_assert_writer_session()
returns boolean
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  if current_user <> 'mud_writer' or session_user <> 'mud_writer_login' then
    raise exception using errcode = 'P0001', message = 'M3 writer session identity is unsafe';
  end if;
  return true;
end;
$$;

revoke all on function private.m3_assert_writer_session()
  from public, anon, authenticated, service_role, mud_writer_login, mud_writer;
grant execute on function private.m3_assert_writer_session() to mud_writer;

comment on function private.m3_assert_writer_session() is
  'mud_writer only, SECURITY INVOKER. Returns true only when the session originated as mud_writer_login and its current role is mud_writer.';
