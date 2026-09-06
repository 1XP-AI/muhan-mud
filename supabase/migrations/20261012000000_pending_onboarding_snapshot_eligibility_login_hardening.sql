-- 2026-10-12: normalize the direct pending-eligibility list login after its
-- initial deployment.  This is deliberately a read-only list capability: it
-- cannot acquire another role, directly access private relations, or invoke
-- any private function other than the bounded list RPC.

do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'onboarding_snapshot_eligibility_login') then
    raise exception using errcode = 'P0001',
      message = 'pending onboarding snapshot eligibility login is required';
  end if;
end
$$;

-- Preserve an operator-provisioned password while making every replayed
-- session bounded and read-only before it reaches the SECURITY DEFINER list.
alter role onboarding_snapshot_eligibility_login reset all;
alter role onboarding_snapshot_eligibility_login set default_transaction_read_only to 'on';
alter role onboarding_snapshot_eligibility_login set statement_timeout to '5s';
alter role onboarding_snapshot_eligibility_login set lock_timeout to '1s';
alter role onboarding_snapshot_eligibility_login set idle_in_transaction_session_timeout to '5s';
alter role onboarding_snapshot_eligibility_login set search_path to pg_catalog;

do $$
begin
  execute format('grant connect on database %I to onboarding_snapshot_eligibility_login', current_database());
end
$$;

-- Revoke every direct private object privilege before restoring exactly the
-- schema lookup plus list-RPC surface.  This includes sequences, which are
-- not covered by the table revoke and must remain unavailable to this login.
revoke all on schema private from onboarding_snapshot_eligibility_login;
revoke all privileges on all tables in schema private from onboarding_snapshot_eligibility_login;
revoke all privileges on all sequences in schema private from onboarding_snapshot_eligibility_login;
revoke all privileges on all functions in schema private from onboarding_snapshot_eligibility_login;
grant usage on schema private to onboarding_snapshot_eligibility_login;
grant execute on function private.list_pending_game_character_onboarding_snapshot_eligibility(integer)
  to onboarding_snapshot_eligibility_login;

comment on role onboarding_snapshot_eligibility_login is
  'Direct NOINHERIT read-only list login for pending onboarding snapshot eligibility. It has no role memberships, database CONNECT, bounded read-only session defaults, and only private-schema usage plus the bounded list RPC.';
