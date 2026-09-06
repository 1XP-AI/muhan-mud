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
  and has_function_privilege('service_role',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('anon',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('authenticated',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute')
  and not has_function_privilege('mud_writer',
        'private.list_pending_game_character_onboarding_snapshot_eligibility(integer)', 'execute'),
  'the pending list is a private security-definer service-only RPC'
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

set local role service_role;
select pg_temp.expect_rejection(
  'select * from private.list_pending_game_character_onboarding_snapshot_eligibility(0)'
);
select pg_temp.expect_rejection(
  'select * from private.list_pending_game_character_onboarding_snapshot_eligibility(1001)'
);
reset role;

rollback;
