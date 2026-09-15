\set ON_ERROR_STOP on

-- Run only against a disposable self-hosted Supabase database after all
-- migrations:
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f supabase/tests/character_onboarding_contract.sql
-- The transaction rolls every fixture row back.

begin;

create or replace function pg_temp.assert_true(condition boolean, message text)
returns void
language plpgsql
as $$
begin
  if condition is not true then
    raise exception 'character onboarding contract failed: %', message;
  end if;
end;
$$;

create or replace function pg_temp.expect_onboarding_rejection(
  p_actor uuid,
  p_correlation uuid,
  p_mode text,
  p_expires_at timestamptz
)
returns void
language plpgsql
as $$
begin
  perform public.begin_game_character_onboarding(
    p_actor, p_correlation, p_mode, p_expires_at
  );
  raise exception using errcode = 'P0002', message = 'onboarding request unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then
    return;
end;
$$;

create or replace function pg_temp.expect_provisioning_rejection(
  p_actor uuid,
  p_correlation uuid,
  p_world_id text,
  p_legacy_name text
)
returns void
language plpgsql
as $$
begin
  perform public.begin_game_character_provisioning(
    p_actor, p_correlation, p_world_id, p_legacy_name
  );
  raise exception using errcode = 'P0002', message = 'provisioning request unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' or check_violation then
    return;
end;
$$;

create or replace function pg_temp.expect_finalize_rejection(
  p_actor uuid,
  p_correlation uuid,
  p_saved_file_sha256 text,
  p_storage_format smallint
)
returns void
language plpgsql
as $$
begin
  perform public.finalize_game_character_provisioning(
    p_actor, p_correlation, p_saved_file_sha256, p_storage_format
  );
  raise exception using errcode = 'P0002', message = 'finalize request unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then
    return;
end;
$$;

create or replace function pg_temp.expect_claim_onboarding_rejection(
  p_world_id text,
  p_legacy_name_key text,
  p_imported_file_sha256 text,
  p_actor uuid,
  p_correlation uuid
)
returns void
language plpgsql
as $$
begin
  perform public.claim_legacy_game_character_onboarding(
    p_world_id, p_legacy_name_key, p_imported_file_sha256,
    p_actor, p_correlation
  );
  raise exception using errcode = 'P0002', message = 'onboarding claim unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then
    return;
end;
$$;

create or replace function pg_temp.expect_claim_challenge_rejection(
  p_world_id text,
  p_legacy_name_key text,
  p_imported_file_sha256 text,
  p_actor uuid,
  p_correlation uuid
)
returns void
language plpgsql
as $$
begin
  perform public.challenge_legacy_game_character_onboarding(
    p_world_id, p_legacy_name_key, p_imported_file_sha256,
    p_actor, p_correlation
  );
  raise exception using errcode = 'P0002', message = 'claim challenge unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then
    return;
end;
$$;

create or replace function pg_temp.expect_cancel_rejection(
  p_actor uuid,
  p_correlation uuid
)
returns void
language plpgsql
as $$
begin
  perform public.cancel_unreserved_game_character_onboarding(
    p_actor, p_correlation
  );
  raise exception using errcode = 'P0002', message = 'onboarding cancellation unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then
    return;
end;
$$;

create or replace function pg_temp.start_and_cancel_claim(
  p_actor uuid,
  p_correlation uuid
)
returns void
language plpgsql
as $$
begin
  perform public.begin_game_character_onboarding(
    p_actor, p_correlation, 'claim', now() + interval '10 minutes'
  );
  perform public.cancel_unreserved_game_character_onboarding(
    p_actor, p_correlation
  );
end;
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('51000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'onboarding-contract-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'onboarding-contract-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000003', 'authenticated', 'authenticated',
   'onboarding-contract-rate-limit@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000004', 'authenticated', 'authenticated',
   'onboarding-contract-target-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000005', 'authenticated', 'authenticated',
   'onboarding-contract-target-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000006', 'authenticated', 'authenticated',
   'onboarding-contract-target-c@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000007', 'authenticated', 'authenticated',
   'onboarding-contract-target-d@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

-- Private state and all onboarding write paths are unavailable to a browser.
select pg_temp.assert_true(
  exists (select 1 from pg_class where oid = 'private.game_character_onboarding_intents'::regclass)
  and exists (select 1 from pg_class where oid = 'private.game_character_provisioning_requests'::regclass)
  and exists (select 1 from pg_class where oid = 'private.game_character_claim_attempts'::regclass),
  'private onboarding intent, provisioning request, and claim challenge tables must exist'
);
select pg_temp.assert_true(
  (select relrowsecurity from pg_class where oid = 'private.game_character_onboarding_intents'::regclass)
  and (select relrowsecurity from pg_class where oid = 'private.game_character_provisioning_requests'::regclass)
  and (select relrowsecurity from pg_class where oid = 'private.game_character_claim_attempts'::regclass),
  'private onboarding and claim challenge tables must have RLS enabled'
);
select pg_temp.assert_true(
  not has_table_privilege('anon', 'private.game_character_onboarding_intents', 'select')
  and not has_table_privilege('anon', 'private.game_character_onboarding_intents', 'insert')
  and not has_table_privilege('anon', 'private.game_character_onboarding_intents', 'update')
  and not has_table_privilege('anon', 'private.game_character_onboarding_intents', 'delete')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'delete')
  and not has_table_privilege('anon', 'private.game_character_provisioning_requests', 'select')
  and not has_table_privilege('anon', 'private.game_character_provisioning_requests', 'insert')
  and not has_table_privilege('anon', 'private.game_character_provisioning_requests', 'update')
  and not has_table_privilege('anon', 'private.game_character_provisioning_requests', 'delete')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'delete')
  and not has_table_privilege('anon', 'private.game_character_claim_attempts', 'select')
  and not has_table_privilege('anon', 'private.game_character_claim_attempts', 'insert')
  and not has_table_privilege('anon', 'private.game_character_claim_attempts', 'update')
  and not has_table_privilege('anon', 'private.game_character_claim_attempts', 'delete')
  and not has_table_privilege('authenticated', 'private.game_character_claim_attempts', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_claim_attempts', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_claim_attempts', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_claim_attempts', 'delete'),
  'browser role must not directly CRUD private onboarding state'
);
select pg_temp.assert_true(
  not has_table_privilege('service_role', 'private.game_character_onboarding_intents', 'select')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_intents', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_intents', 'update')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_intents', 'delete')
  and not has_table_privilege('service_role', 'private.game_character_provisioning_requests', 'select')
  and not has_table_privilege('service_role', 'private.game_character_provisioning_requests', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_provisioning_requests', 'update')
  and not has_table_privilege('service_role', 'private.game_character_provisioning_requests', 'delete')
  and not has_table_privilege('service_role', 'private.game_character_claim_attempts', 'select')
  and not has_table_privilege('service_role', 'private.game_character_claim_attempts', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_claim_attempts', 'update')
  and not has_table_privilege('service_role', 'private.game_character_claim_attempts', 'delete'),
  'service role must use RPCs rather than direct private onboarding or claim-ledger CRUD'
);
select pg_temp.assert_true(
  not has_function_privilege('anon', 'public.begin_game_character_onboarding(uuid,uuid,text,timestamptz)', 'execute')
  and not has_function_privilege('anon', 'public.begin_game_character_provisioning(uuid,uuid,text,text)', 'execute')
  and not has_function_privilege('anon', 'public.finalize_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and not has_function_privilege('anon', 'public.reconcile_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and not has_function_privilege('anon', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and not has_function_privilege('anon', 'public.challenge_legacy_game_character_onboarding(text,text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('anon', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('anon', 'public.claim_legacy_game_character_onboarding(text,text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('anon', 'public.cancel_unreserved_game_character_onboarding(uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated', 'public.begin_game_character_onboarding(uuid,uuid,text,timestamptz)', 'execute')
  and not has_function_privilege('authenticated', 'public.begin_game_character_provisioning(uuid,uuid,text,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.finalize_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and not has_function_privilege('authenticated', 'public.reconcile_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and not has_function_privilege('authenticated', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.challenge_legacy_game_character_onboarding(text,text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated', 'public.claim_legacy_game_character_onboarding(text,text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated', 'public.cancel_unreserved_game_character_onboarding(uuid,uuid)', 'execute')
  and has_function_privilege('service_role', 'public.begin_game_character_onboarding(uuid,uuid,text,timestamptz)', 'execute')
  and has_function_privilege('service_role', 'public.begin_game_character_provisioning(uuid,uuid,text,text)', 'execute')
  and has_function_privilege('service_role', 'public.finalize_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and has_function_privilege('service_role', 'public.reconcile_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and has_function_privilege('service_role', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and has_function_privilege('service_role', 'public.challenge_legacy_game_character_onboarding(text,text,text,uuid,uuid)', 'execute')
  and has_function_privilege('service_role', 'public.claim_legacy_game_character_onboarding(text,text,text,uuid,uuid)', 'execute')
  and to_regprocedure('public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)') is not null
  and not has_function_privilege('service_role', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and has_function_privilege('service_role', 'public.cancel_unreserved_game_character_onboarding(uuid,uuid)', 'execute'),
  'only service role may execute onboarding RPCs'
);
select pg_temp.assert_true(
  not has_function_privilege('service_role', 'public.claim_legacy_game_character(text,text,uuid,uuid)', 'execute'),
  'service role must not bypass the fingerprint-bound onboarding claim RPC'
);
select pg_temp.assert_true(
  to_regprocedure('public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)') is not null
  and not has_function_privilege('anon', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('service_role', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and pg_get_function_result('public.challenge_legacy_game_character_onboarding(text,text,text,uuid,uuid)'::regprocedure)
      = 'TABLE(character_id uuid, legacy_name_key text, imported_file_sha256 text, challenge_status text, allow_expires_at timestamp with time zone)',
  'the unledgered overload must remain rolling-schema compatible but unexecutable, and challenge response must remain exact'
);
select pg_temp.assert_true(
  not exists (
    select 1
    from information_schema.columns
    where table_schema in ('public', 'private')
      and (
        lower(column_name) ~ '(password|passwd|terminal|input|secret|token|ticket|jwt|proof)'
        or (table_name = 'game_character_claim_attempts' and lower(column_name) ~ 'name')
      )
  ),
  'onboarding state must not store game passwords, terminal input, or application secrets'
);

set local role service_role;

select pg_temp.assert_true(
  (select status = 'started'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000002',
     '52000000-0000-0000-0000-000000000003',
     'provision', now() + interval '10 minutes'
   )),
  'a newly begun onboarding intent must remain started before name reservation'
);
select pg_temp.expect_finalize_rejection(
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000003',
  repeat('d', 64), 1::smallint
);
select pg_temp.expect_cancel_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000003'
);
select pg_temp.assert_true(
  (select status = 'cancelled' and completed_at is not null
   from public.cancel_unreserved_game_character_onboarding(
     '51000000-0000-0000-0000-000000000002',
     '52000000-0000-0000-0000-000000000003'
   )),
  'an actor may cancel its exact started intent before a name is reserved'
);
select pg_temp.assert_true(
  (select count(*) = 1
   from public.cancel_unreserved_game_character_onboarding(
     '51000000-0000-0000-0000-000000000002',
     '52000000-0000-0000-0000-000000000003'
   )),
  'same unreserved cancellation must be idempotent'
);

-- Cancelling a failed password flow must not let one authenticated actor mint
-- unbounded fresh correlations. Exact-correlation retries remain idempotent,
-- and the fixed window expires without deleting audit rows.
select pg_temp.start_and_cancel_claim(
  '51000000-0000-0000-0000-000000000003',
  '53000000-0000-0000-0000-000000000001'
);
select pg_temp.start_and_cancel_claim(
  '51000000-0000-0000-0000-000000000003',
  '53000000-0000-0000-0000-000000000002'
);
select pg_temp.start_and_cancel_claim(
  '51000000-0000-0000-0000-000000000003',
  '53000000-0000-0000-0000-000000000003'
);
select pg_temp.start_and_cancel_claim(
  '51000000-0000-0000-0000-000000000003',
  '53000000-0000-0000-0000-000000000004'
);
select pg_temp.start_and_cancel_claim(
  '51000000-0000-0000-0000-000000000003',
  '53000000-0000-0000-0000-000000000005'
);
select pg_temp.expect_onboarding_rejection(
  '51000000-0000-0000-0000-000000000003',
  '53000000-0000-0000-0000-000000000006',
  'claim', now() + interval '10 minutes'
);
select pg_temp.assert_true(
  (select status = 'cancelled'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000003',
     '53000000-0000-0000-0000-000000000005',
     'claim', now() + interval '10 minutes'
   )),
  'rate limiting must not break exact-correlation idempotency'
);

reset role;
update private.game_character_onboarding_intents
  set created_at = now() - interval '16 minutes'
  where actor_user_id = '51000000-0000-0000-0000-000000000003';
set local role service_role;

select pg_temp.assert_true(
  (select status = 'started'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000003',
     '53000000-0000-0000-0000-000000000006',
     'claim', now() + interval '10 minutes'
   )),
  'a claim actor may start again after the fixed rate window expires'
);
select pg_temp.assert_true(
  (select status = 'cancelled'
   from public.cancel_unreserved_game_character_onboarding(
     '51000000-0000-0000-0000-000000000003',
     '53000000-0000-0000-0000-000000000006'
   )),
  'the post-window claim fixture must close cleanly'
);

select pg_temp.assert_true(
  (select actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and mode = 'provision'
          and status = 'started'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     'provision', now() + interval '10 minutes'
   )),
  'begin onboarding must bind actor, correlation, mode, and started lifecycle'
);
select pg_temp.assert_true(
  (select expires_at = now() + interval '10 minutes'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     'provision', now() + interval '11 minutes'
   )),
  'same actor, correlation, and mode retry must reuse rather than extend the original expiry'
);
select pg_temp.expect_onboarding_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000005',
  'claim', now() + interval '10 minutes'
);
select pg_temp.expect_onboarding_rejection(
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000001',
  'provision', now() + interval '10 minutes'
);
select pg_temp.expect_onboarding_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  'claim', now() + interval '10 minutes'
);

select pg_temp.expect_provisioning_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  'onboarding-contract-world', '뽀뽀'
);

select pg_temp.assert_true(
  (select legacy_name_key = 'Newhero'
          and lifecycle = 'provisioning'
          and actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and status = 'reserved'
   from public.begin_game_character_provisioning(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     'onboarding-contract-world', 'nEwHERO'
   )),
  'begin provisioning must canonically reserve one provisioning character'
);
select pg_temp.expect_cancel_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001'
);
select pg_temp.assert_true(
  (select count(*) from public.begin_game_character_provisioning(
    '51000000-0000-0000-0000-000000000001',
    '52000000-0000-0000-0000-000000000001',
    'onboarding-contract-world', 'nEwHERO'
  )) = 1,
  'same provisioning correlation and payload must be idempotent'
);
select pg_temp.expect_provisioning_rejection(
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000001',
  'onboarding-contract-world', 'nEwHERO'
);
select pg_temp.expect_provisioning_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  'onboarding-contract-world', 'Otherhero'
);

reset role;
select pg_temp.assert_true(
  (select count(*) from public.game_characters
   where world_id = 'onboarding-contract-world'
     and legacy_name_key = 'Newhero'
     and lifecycle = 'provisioning') = 1,
  'canonical provisioning name must have one unique reservation'
);

set local role service_role;

select pg_temp.expect_finalize_rejection(
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000001',
  repeat('a', 64), 1::smallint
);
select pg_temp.expect_finalize_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  'not-a-sha256', 1::smallint
);

select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending'
          and status = 'finalized'
          and saved_file_sha256 = repeat('a', 64)
          and storage_format = 1
   from public.finalize_game_character_provisioning(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     repeat('a', 64), 1::smallint
   )),
  'finalize after C save evidence must leave the reservation pending exact handoff activation'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending'
          and status = 'finalized'
   from public.finalize_game_character_provisioning(
    '51000000-0000-0000-0000-000000000001',
    '52000000-0000-0000-0000-000000000001',
    repeat('a', 64), 1::smallint
  )),
  'same finalize correlation and save payload must retain the pending handoff'
);
select pg_temp.expect_finalize_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  repeat('b', 64), 1::smallint
);
select pg_temp.assert_true(
  (select character_id = (select id from public.game_characters
                            where world_id = 'onboarding-contract-world'
                              and legacy_name_key = 'Newhero')
          and actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and correlation_id = '52000000-0000-0000-0000-000000000001'::uuid
          and lifecycle = 'active'
          and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     (select id from public.game_characters
       where world_id = 'onboarding-contract-world' and legacy_name_key = 'Newhero'),
     'provision'
   )),
  'only the exact provision handoff activation may transition the finalized reservation to active'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     (select id from public.game_characters
       where world_id = 'onboarding-contract-world' and legacy_name_key = 'Newhero'),
     'provision'
   )),
  'an exact activated provision handoff retry must be idempotent'
);

-- A save/finalize crash window has no DB finalize receipt.  Reconcile accepts
-- the same C save fingerprint and performs the same monotonic transition.
select pg_temp.assert_true(
  (select actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000002',
     'provision', now() + interval '10 minutes'
   )),
  'second onboarding intent starts for crash-window reconciliation'
);
select pg_temp.assert_true(
  (select status = 'reserved'
   from public.begin_game_character_provisioning(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000002',
     'onboarding-contract-world', 'crashhero'
   )),
  'second provisioning reservation starts before C save/finalize crash'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending'
          and status = 'finalized'
          and saved_file_sha256 = repeat('c', 64)
   from public.reconcile_game_character_provisioning(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000002',
     repeat('c', 64), 1::smallint
   )),
  'reconcile must finalize a reserved request into a pending handoff after a matching C save fingerprint'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending'
          and status = 'finalized'
   from public.reconcile_game_character_provisioning(
    '51000000-0000-0000-0000-000000000001',
    '52000000-0000-0000-0000-000000000002',
    repeat('c', 64), 1::smallint
  )),
  'same reconcile correlation and save payload must retain the pending handoff'
);
select pg_temp.assert_true(
  (select character_id = (select id from public.game_characters
                            where world_id = 'onboarding-contract-world'
                              and legacy_name_key = 'Crashhero')
          and actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and correlation_id = '52000000-0000-0000-0000-000000000002'::uuid
          and lifecycle = 'active'
          and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000002',
     (select id from public.game_characters
       where world_id = 'onboarding-contract-world' and legacy_name_key = 'Crashhero'),
     'provision'
   )),
  'only the exact reconciled provision handoff activation may transition the reservation to active'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000002',
     (select id from public.game_characters
       where world_id = 'onboarding-contract-world' and legacy_name_key = 'Crashhero'),
     'provision'
   )),
  'an exact activated reconciled provision handoff retry must be idempotent'
);

reset role;

insert into public.game_characters (
  world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  imported_file_sha256
) values (
  'onboarding-contract-world', 'Legacyhero', 'Legacyhero',
  substr(encode(public.digest(convert_to('Legacyhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', repeat('e', 64)
);

set local role service_role;

select pg_temp.assert_true(
  (select status = 'started'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004',
     'claim', now() + interval '10 minutes'
   )),
  'claim starts from the same actor-bound onboarding intent boundary'
);
select pg_temp.expect_claim_onboarding_rejection(
  'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000004'
);
select pg_temp.expect_claim_challenge_rejection(
  'onboarding-contract-world', 'Legacyhero', repeat('f', 64),
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000004'
);
select pg_temp.assert_true(
  (select character_id is not null
          and legacy_name_key = 'Legacyhero'
          and imported_file_sha256 = repeat('e', 64)
          and challenge_status = 'allowed'
          and allow_expires_at > now()
   from public.challenge_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'an exact C-verified fingerprint must receive the fixed response shape and an allowed challenge'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
   from public.challenge_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'an exact unexpired challenge retry must be idempotent'
);
select pg_temp.expect_claim_challenge_rejection(
  'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000004'
);
select pg_temp.expect_claim_challenge_rejection(
  'onboarding-contract-world', 'Legacyhero', repeat('d', 64),
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000004'
);
reset role;
select pg_temp.assert_true(
  (select allow_expires_at = allowed_at + interval '90 seconds'
   from private.game_character_claim_attempts
   where correlation_id = '52000000-0000-0000-0000-000000000004'),
  'challenge allowance must have an exact 90-second TTL'
);
set local role service_role;
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending'
          and owner_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and onboarding_status = 'finalized'
          and imported_file_sha256 = repeat('e', 64)
   from public.claim_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'C-verified claim must atomically claim the character and finalize into a pending handoff'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'same finalized onboarding claim must retain the pending handoff'
);
select pg_temp.expect_claim_onboarding_rejection(
  'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000004'
);

reset role;

select pg_temp.assert_true(
  (select status = 'finalized' and completed_at is not null
   from private.game_character_onboarding_intents
   where correlation_id = '52000000-0000-0000-0000-000000000004'),
  'successful claim must leave no started onboarding intent behind'
);
select pg_temp.assert_true(
  (select count(*) = 1
   from private.game_character_claim_requests request
   join public.game_characters character on character.id = request.character_id
   where request.correlation_id = '52000000-0000-0000-0000-000000000004'
     and request.actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
     and request.completed_at is not null
     and character.world_id = 'onboarding-contract-world'
     and character.legacy_name_key = 'Legacyhero'),
  'successful ledger-bound claim must preserve and complete the private claim-request audit row'
);

update private.game_character_claim_attempts
  set allowed_at = now() - interval '2 minutes',
      allow_expires_at = now() - interval '30 seconds'
  where correlation_id = '52000000-0000-0000-0000-000000000004';
set local role service_role;
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'an already committed exact final retry remains pending and idempotent after challenge expiry'
);
select pg_temp.assert_true(
  (select character_id = (select id from public.game_characters
                            where world_id = 'onboarding-contract-world'
                              and legacy_name_key = 'Legacyhero')
          and actor_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and correlation_id = '52000000-0000-0000-0000-000000000004'::uuid
          and lifecycle = 'active'
          and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004',
     (select id from public.game_characters
       where world_id = 'onboarding-contract-world' and legacy_name_key = 'Legacyhero'),
     'claim'
   )),
  'only the exact claim handoff activation may transition the finalized claim to active'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004',
     (select id from public.game_characters
       where world_id = 'onboarding-contract-world' and legacy_name_key = 'Legacyhero'),
     'claim'
   )),
  'an exact activated claim handoff retry must be idempotent'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero', repeat('e', 64),
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'the finalized claim retry must report active only after the exact handoff activation'
);
reset role;

-- Three allowed challenge rows for one imported character are a target-wide
-- fixed-window budget, not a per-actor budget. A cancelled intent leaves its
-- allowed row intact so it cannot be used to erase the count.
insert into public.game_characters (
  world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  imported_file_sha256
) values (
  'onboarding-contract-world', 'Caphero', 'Caphero',
  substr(encode(public.digest(convert_to('Caphero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', repeat('c', 64)
);

set local role service_role;
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000004',
     '54000000-0000-0000-0000-000000000001', 'claim', now() + interval '10 minutes'
   ) cross join lateral public.challenge_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Caphero', repeat('c', 64),
     '51000000-0000-0000-0000-000000000004', '54000000-0000-0000-0000-000000000001'
   )),
  'first target attempt is allowed'
);
select pg_temp.assert_true(
  (select status = 'cancelled' from public.cancel_unreserved_game_character_onboarding(
    '51000000-0000-0000-0000-000000000004', '54000000-0000-0000-0000-000000000001'
  )),
  'cancelled challenge intent remains auditable'
);
select pg_temp.expect_claim_challenge_rejection(
  'onboarding-contract-world', 'Caphero', repeat('c', 64),
  '51000000-0000-0000-0000-000000000004', '54000000-0000-0000-0000-000000000001'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000005',
     '54000000-0000-0000-0000-000000000002', 'claim', now() + interval '10 minutes'
   ) cross join lateral public.challenge_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Caphero', repeat('c', 64),
     '51000000-0000-0000-0000-000000000005', '54000000-0000-0000-0000-000000000002'
   )),
  'second actor target attempt is allowed'
);
select pg_temp.assert_true(
  (select status = 'cancelled' from public.cancel_unreserved_game_character_onboarding(
    '51000000-0000-0000-0000-000000000005', '54000000-0000-0000-0000-000000000002'
  )),
  'second challenge intent may be cancelled without removing its allowance'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000006',
     '54000000-0000-0000-0000-000000000003', 'claim', now() + interval '10 minutes'
   ) cross join lateral public.challenge_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Caphero', repeat('c', 64),
     '51000000-0000-0000-0000-000000000006', '54000000-0000-0000-0000-000000000003'
   )),
  'third target attempt is allowed'
);
select pg_temp.assert_true(
  (select status = 'cancelled' from public.cancel_unreserved_game_character_onboarding(
    '51000000-0000-0000-0000-000000000006', '54000000-0000-0000-0000-000000000003'
  )),
  'third challenge cancellation still preserves the target count'
);
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '51000000-0000-0000-0000-000000000007',
    '54000000-0000-0000-0000-000000000004', 'claim', now() + interval '10 minutes'
  )),
  'fourth actor may create an intent before target admission is evaluated'
);
select pg_temp.expect_claim_challenge_rejection(
  'onboarding-contract-world', 'Caphero', repeat('c', 64),
  '51000000-0000-0000-0000-000000000007',
  '54000000-0000-0000-0000-000000000004'
);

reset role;
update private.game_character_claim_attempts
  set allowed_at = now() - interval '16 minutes',
      allow_expires_at = now() - interval '16 minutes' + interval '90 seconds'
  where correlation_id in (
    '54000000-0000-0000-0000-000000000001',
    '54000000-0000-0000-0000-000000000002',
    '54000000-0000-0000-0000-000000000003'
  );
set local role service_role;
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
   from public.challenge_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Caphero', repeat('c', 64),
     '51000000-0000-0000-0000-000000000007', '54000000-0000-0000-0000-000000000004'
   )),
  'the target budget opens after its 15-minute fixed window expires'
);
reset role;
update private.game_character_claim_attempts
  set allowed_at = now() - interval '91 seconds',
      allow_expires_at = now() - interval '1 second'
  where correlation_id = '54000000-0000-0000-0000-000000000004';
set local role service_role;
select pg_temp.expect_claim_challenge_rejection(
  'onboarding-contract-world', 'Caphero', repeat('c', 64),
  '51000000-0000-0000-0000-000000000007', '54000000-0000-0000-0000-000000000004'
);
select pg_temp.expect_claim_onboarding_rejection(
  'onboarding-contract-world', 'Caphero', repeat('c', 64),
  '51000000-0000-0000-0000-000000000007', '54000000-0000-0000-0000-000000000004'
);
reset role;
select pg_temp.assert_true(
  not exists (
    select 1
    from private.game_character_claim_requests
    where correlation_id = '54000000-0000-0000-0000-000000000004'
  ),
  'an expired claim must roll back its newly created audit request atomically'
);

rollback;
