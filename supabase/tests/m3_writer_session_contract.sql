\set ON_ERROR_STOP on

-- Contract for 20260911000000_m3_writer_session.sql.  Run after bootstrap and
-- migrations through 110 on disposable PostgreSQL 17.  The companion shell
-- integration records a real password-authenticated RED/GREEN login.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'm3 writer session contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_state(p_sqlstate text, p_sql text)
returns void language plpgsql as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then
      return;
    end if;
    raise;
  end;
  raise exception using errcode = 'P0002',
    message = format('m3 writer session contract failed: expected %s, statement succeeded', p_sqlstate);
end;
$$;

-- RED boundary: bootstrap through 100 has mud_writer only.  The actual-login
-- integration executes this absence check before applying 110.
select pg_temp.assert_true(
  to_regrole('mud_writer_login') is not null
  and to_regprocedure('private.m3_assert_writer_session()') is not null,
  'writer login role and assertion function must exist after 110'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_roles r
     where r.rolname = 'mud_writer'
       and not r.rolcanlogin
       and not r.rolinherit
       and not r.rolsuper
       and not r.rolcreaterole
       and not r.rolcreatedb
       and not r.rolreplication
       and not r.rolbypassrls
       and (select a.rolpassword is null from pg_authid a where a.oid = r.oid)
  ),
  'mud_writer must remain a passwordless NOLOGIN nonprivileged role'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_roles r
     where r.rolname = 'mud_writer_login'
       and r.rolcanlogin
       and not r.rolinherit
       and not r.rolsuper
       and not r.rolcreaterole
       and not r.rolcreatedb
       and not r.rolreplication
       and not r.rolbypassrls
  ),
  'mud_writer_login must be a LOGIN NOINHERIT nonprivileged role'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_auth_members m
      join pg_roles granted on granted.oid = m.roleid
      join pg_roles member on member.oid = m.member
     where granted.rolname = 'mud_writer'
       and member.rolname = 'mud_writer_login'
       and not m.admin_option
       and not m.inherit_option
       and m.set_option
  )
  and (select count(*) from pg_auth_members m
         join pg_roles granted on granted.oid = m.roleid
         where granted.rolname = 'mud_writer') = 1
  and not exists (
    select 1
      from pg_auth_members m
      join pg_roles granted on granted.oid = m.roleid
      join pg_roles member on member.oid = m.member
     where (granted.rolname in ('mud_writer', 'mud_writer_login')
            or member.rolname in ('mud_writer', 'mud_writer_login'))
       and not (granted.rolname = 'mud_writer' and member.rolname = 'mud_writer_login')
  ),
  'the one M3 membership must be login to writer with SET only and no related edges'
);

select pg_temp.assert_true(
  (select rolconfig is null from pg_roles where rolname = 'mud_writer')
  and (select rolconfig @> array[
      'role=mud_writer',
      'statement_timeout=5s',
      'lock_timeout=1s',
      'idle_in_transaction_session_timeout=5s',
      'search_path=pg_catalog'
    ]::text[] from pg_roles where rolname = 'mud_writer_login')
  and (select cardinality(rolconfig) = 5 from pg_roles where rolname = 'mud_writer_login'),
  'writer-login role defaults must be the exact role, timeout, and search-path allowlist'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
     where p.oid = 'private.m3_assert_writer_session()'::regprocedure
       and n.nspname = 'private'
       and p.prorettype = 'boolean'::regtype
       and not p.prosecdef
       and p.proconfig = array['search_path=pg_catalog']::text[]
  ),
  'assertion must be a SECURITY INVOKER boolean with fixed pg_catalog search path'
);

select pg_temp.assert_true(
  has_function_privilege('mud_writer', 'private.m3_assert_writer_session()'::regprocedure, 'execute')
  and not has_function_privilege('mud_writer_login', 'private.m3_assert_writer_session()'::regprocedure, 'execute')
  and not has_function_privilege('anon', 'private.m3_assert_writer_session()'::regprocedure, 'execute')
  and not has_function_privilege('authenticated', 'private.m3_assert_writer_session()'::regprocedure, 'execute')
  and not has_function_privilege('service_role', 'private.m3_assert_writer_session()'::regprocedure, 'execute')
  and has_schema_privilege('mud_writer', 'private', 'usage')
  and not has_schema_privilege('mud_writer_login', 'private', 'usage'),
  'only the writer capability may use and execute the private assertion'
);

select pg_temp.assert_true(
  not pg_has_role('anon', 'mud_writer', 'set')
  and not pg_has_role('authenticated', 'mud_writer', 'set')
  and not pg_has_role('service_role', 'mud_writer', 'set'),
  'browser and service roles must not SET ROLE to mud_writer'
);

-- Same current role but a different session user is rejected by the function.
set session authorization mud_writer_login;
set role mud_writer;
select pg_temp.assert_true(
  current_user = 'mud_writer'
  and session_user = 'mud_writer_login'
  and private.m3_assert_writer_session(),
  'exact login session must assert true after SET ROLE'
);
reset role;
select pg_temp.expect_state('42501', 'select private.m3_assert_writer_session()');
reset session authorization;

set role mud_writer;
select pg_temp.expect_state('P0001', 'select private.m3_assert_writer_session()');
reset role;

set role service_role;
select pg_temp.expect_state('42501', 'select private.m3_assert_writer_session()');
reset role;

rollback;
