-- 2026-10-11: correct the pending-eligibility list identity.  service_role is
-- deliberately NOLOGIN in bootstrap, so this worker needs its own direct,
-- list-only login and must never inherit or SET a service/writer capability.

do $$
declare
  v_login oid;
begin
  if not exists (select 1 from pg_roles where rolname = 'onboarding_snapshot_eligibility_login') then
    create role onboarding_snapshot_eligibility_login login noinherit nosuperuser nocreatedb nocreaterole
      noreplication nobypassrls password null;
  end if;

  select oid into v_login
    from pg_roles
   where rolname = 'onboarding_snapshot_eligibility_login';

  -- Any membership makes the list login capable of acquiring, granting, or
  -- exposing another role.  Do not silently repair an unsafe deployment.
  if exists (
    select 1 from pg_auth_members m
     where m.roleid = v_login or m.member = v_login
  ) then
    raise exception using errcode = 'P0001',
      message = 'pending onboarding snapshot eligibility login membership is unsafe';
  end if;

  -- Do not mention PASSWORD here: replays preserve the operator-provisioned
  -- credential of the direct login while enforcing every safe role attribute.
  alter role onboarding_snapshot_eligibility_login login noinherit nosuperuser nocreatedb nocreaterole
    noreplication nobypassrls connection limit 1;
end
$$;

alter role onboarding_snapshot_eligibility_login reset all;
alter role onboarding_snapshot_eligibility_login set statement_timeout to '5s';
alter role onboarding_snapshot_eligibility_login set lock_timeout to '1s';
alter role onboarding_snapshot_eligibility_login set idle_in_transaction_session_timeout to '5s';
alter role onboarding_snapshot_eligibility_login set search_path to pg_catalog;

-- The login receives schema lookup and one explicitly bounded function only;
-- it receives no table, sequence, writer, or service capability.
revoke all on schema private from onboarding_snapshot_eligibility_login;
grant usage on schema private to onboarding_snapshot_eligibility_login;
revoke all on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer)
  from public, anon, authenticated, service_role, mud_writer, mud_writer_login,
       onboarding_snapshot_eligibility_login;
grant execute on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer)
  to onboarding_snapshot_eligibility_login;

comment on role onboarding_snapshot_eligibility_login is
  'Direct NOINHERIT list-only login for pending onboarding snapshot eligibility. It has no role memberships and only private-schema usage plus the bounded list RPC.';

comment on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer) is
  'onboarding_snapshot_eligibility_login only. Lists at most the requested pending eligibility rows with an exact immutable actor/correlation/character/mode/command binding in enqueued-at/correlation order; it never selects artifact candidates or mutates gameplay state.';
