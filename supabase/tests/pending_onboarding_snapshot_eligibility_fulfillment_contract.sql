\set ON_ERROR_STOP on

begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'pending onboarding snapshot eligibility contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_rejection(p_sql text)
returns void language plpgsql as $$
begin
  execute p_sql;
  raise exception using errcode = 'P0002', message = 'pending eligibility list unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then return;
end;
$$;

select pg_temp.assert_true(
  to_regprocedure('private.list_pending_game_character_onboarding_snapshot_eligibility(integer)') is not null
  and (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
         from pg_proc p
        where p.oid = 'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)'::regprocedure)
  and has_function_privilege('onboarding_snapshot_eligibility_login',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and has_schema_privilege('onboarding_snapshot_eligibility_login', 'private', 'usage')
  and not has_function_privilege('service_role',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('anon',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('authenticated',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('mud_writer',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('mud_writer_login',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute'),
  'the pending list is a private security-definer direct-login-only RPC'
);

select pg_temp.assert_true(
  exists (
    select 1 from pg_roles r
     where r.rolname = 'onboarding_snapshot_eligibility_login'
       and r.rolcanlogin
       and not r.rolinherit
       and not r.rolsuper
       and not r.rolcreaterole
       and not r.rolcreatedb
       and not r.rolreplication
       and not r.rolbypassrls
       and r.rolconnlimit = 1
  )
  and not exists (
    select 1 from pg_auth_members m
      join pg_roles login on login.oid = m.member
      join pg_roles granted on granted.oid = m.roleid
     where login.rolname = 'onboarding_snapshot_eligibility_login'
        or granted.rolname = 'onboarding_snapshot_eligibility_login'
  )
  and (select rolconfig @> array[
      'default_transaction_read_only=on',
      'statement_timeout=5s',
      'lock_timeout=1s',
      'idle_in_transaction_session_timeout=5s',
      'search_path=pg_catalog'
    ]::text[] from pg_roles where rolname = 'onboarding_snapshot_eligibility_login')
  and (select cardinality(rolconfig) = 5 from pg_roles where rolname = 'onboarding_snapshot_eligibility_login')
  and has_database_privilege('onboarding_snapshot_eligibility_login', current_database(), 'connect')
  and not pg_has_role('onboarding_snapshot_eligibility_login', 'mud_writer', 'member')
  and not pg_has_role('onboarding_snapshot_eligibility_login', 'service_role', 'member')
  and not has_table_privilege('onboarding_snapshot_eligibility_login',
        'private.game_character_onboarding_snapshot_eligibility_outbox', 'select')
  and not has_table_privilege('onboarding_snapshot_eligibility_login',
        'private.game_character_onboarding_snapshot_command_bindings', 'select')
  and not exists (
    select 1
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
     where n.nspname = 'private'
       and c.relkind in ('r', 'p', 'v', 'm', 'f')
       and (
         has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'select')
         or has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'insert')
         or has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'update')
         or has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'delete')
         or has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'truncate')
         or has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'references')
         or has_table_privilege('onboarding_snapshot_eligibility_login', c.oid, 'trigger')
       )
  )
  and not exists (
    select 1
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
     where n.nspname = 'private'
       and c.relkind = 'S'
       and (
         has_sequence_privilege('onboarding_snapshot_eligibility_login', c.oid, 'usage')
         or has_sequence_privilege('onboarding_snapshot_eligibility_login', c.oid, 'select')
         or has_sequence_privilege('onboarding_snapshot_eligibility_login', c.oid, 'update')
       )
  ),
  'the list login has database CONNECT, no memberships, read-only bounded session settings, and no direct private relation or sequence access'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_proc p
      cross join lateral aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) privilege
     where p.oid = 'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)'::regprocedure
       and privilege.grantee = 'onboarding_snapshot_eligibility_login'::regrole
       and privilege.privilege_type = 'EXECUTE'
  )
  and not exists (
    select 1
      from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
     where n.nspname = 'private'
       and p.oid <> 'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)'::regprocedure
       and has_function_privilege('onboarding_snapshot_eligibility_login', p.oid, 'EXECUTE')
  )
  and not exists (
    select 1
      from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
      cross join lateral aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) privilege
     where n.nspname = 'private'
       and privilege.grantee = 0
       and privilege.privilege_type = 'EXECUTE'
  ),
  'the list login has effective execute only on its bounded RPC and no private function has PUBLIC execute'
);

-- This probe is transaction-local because the contract rolls back. It is
-- created by the same migration owner as the CI replay, proving the default
-- privilege hardening covers future private functions from that owner.
create function private.pending_onboarding_snapshot_eligibility_default_execute_probe()
returns boolean
language sql
as $$ select true $$;

select pg_temp.assert_true(
  not has_function_privilege(
    'onboarding_snapshot_eligibility_login',
    'private.pending_onboarding_snapshot_eligibility_default_execute_probe()'::regprocedure,
    'EXECUTE'
  )
  and not exists (
    select 1
      from pg_proc p
      cross join lateral aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) privilege
     where p.oid = 'private.pending_onboarding_snapshot_eligibility_default_execute_probe()'::regprocedure
       and privilege.grantee = 0
       and privilege.privilege_type = 'EXECUTE'
  ),
  'future private functions created by the migration owner do not acquire PUBLIC execute'
);

select pg_temp.assert_true(
  not exists (
    select 1
      from pg_proc p
      join pg_namespace n on n.oid = p.pronamespace
     where n.nspname = 'private'
       and p.oid <> 'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)'::regprocedure
       and has_function_privilege('onboarding_snapshot_eligibility_login', p.oid, 'EXECUTE')
  ),
  'the list login has no effective private-function execute beyond the bounded list RPC'
);

select pg_temp.assert_true(
  regexp_replace(pg_get_functiondef(
    'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)'::regprocedure
  ), '[[:space:]]+', ' ', 'g') like
    '%select o.correlation_id, o.actor_user_id, o.character_id, o.mode, b.command_id from private.game_character_onboarding_snapshot_eligibility_outbox o join private.game_character_onboarding_snapshot_command_bindings b on b.correlation_id = o.correlation_id and b.actor_user_id = o.actor_user_id and b.character_id = o.character_id and b.mode = o.mode where o.status = ''pending'' and o.fulfilled_at is null order by o.enqueued_at, o.correlation_id limit p_limit%'
  and pg_get_function_result(
    'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)'::regprocedure
  ) = 'TABLE(correlation_id uuid, actor_user_id uuid, character_id uuid, mode text, command_id uuid)',
  'the list exposes only the exact immutable tuple in deterministic pending order'
);

set local role onboarding_snapshot_eligibility_login;
select pg_temp.expect_rejection(
  'select * from private.list_pending_game_character_onboarding_snapshot_eligibility(0)'
);
select pg_temp.expect_rejection(
  'select * from private.list_pending_game_character_onboarding_snapshot_eligibility(1001)'
);
reset role;

rollback;
