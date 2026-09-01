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
  when sqlstate 'P0001' or sqlstate '22023' then
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
  p_actor uuid,
  p_correlation uuid
)
returns void
language plpgsql
as $$
begin
  perform public.claim_legacy_game_character_onboarding(
    p_world_id, p_legacy_name_key, p_actor, p_correlation
  );
  raise exception using errcode = 'P0002', message = 'onboarding claim unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then
    return;
end;
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('51000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'onboarding-contract-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('51000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'onboarding-contract-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

-- Private state and all onboarding write paths are unavailable to a browser.
select pg_temp.assert_true(
  exists (select 1 from pg_class where oid = 'private.game_character_onboarding_intents'::regclass)
  and exists (select 1 from pg_class where oid = 'private.game_character_provisioning_requests'::regclass),
  'private onboarding intent and provisioning request tables must exist'
);
select pg_temp.assert_true(
  (select relrowsecurity from pg_class where oid = 'private.game_character_onboarding_intents'::regclass)
  and (select relrowsecurity from pg_class where oid = 'private.game_character_provisioning_requests'::regclass),
  'private onboarding tables must have RLS enabled'
);
select pg_temp.assert_true(
  not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_onboarding_intents', 'delete')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_provisioning_requests', 'delete'),
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
  and not has_table_privilege('service_role', 'private.game_character_provisioning_requests', 'delete'),
  'service role must use onboarding RPCs rather than direct private CRUD'
);
select pg_temp.assert_true(
  not has_function_privilege('authenticated', 'public.begin_game_character_onboarding(uuid,uuid,text,timestamptz)', 'execute')
  and not has_function_privilege('authenticated', 'public.begin_game_character_provisioning(uuid,uuid,text,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.finalize_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and not has_function_privilege('authenticated', 'public.reconcile_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and not has_function_privilege('authenticated', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute')
  and has_function_privilege('service_role', 'public.begin_game_character_onboarding(uuid,uuid,text,timestamptz)', 'execute')
  and has_function_privilege('service_role', 'public.begin_game_character_provisioning(uuid,uuid,text,text)', 'execute')
  and has_function_privilege('service_role', 'public.finalize_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and has_function_privilege('service_role', 'public.reconcile_game_character_provisioning(uuid,uuid,text,smallint)', 'execute')
  and has_function_privilege('service_role', 'public.claim_legacy_game_character_onboarding(text,text,uuid,uuid)', 'execute'),
  'only service role may execute onboarding RPCs'
);
select pg_temp.assert_true(
  not exists (
    select 1
    from information_schema.columns
    where table_schema in ('public', 'private')
      and table_name in ('game_character_onboarding_intents', 'game_character_provisioning_requests')
      and lower(column_name) ~ '(password|passwd|terminal|input|secret|token|ticket|jwt|proof)'
  ),
  'onboarding state must not store game passwords, terminal input, or application secrets'
);

set local role service_role;

select pg_temp.assert_true(
  (select status = 'started'
   from public.begin_game_character_onboarding(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000003',
     'provision', now() + interval '10 minutes'
   )),
  'a newly begun onboarding intent must remain started before name reservation'
);
select pg_temp.expect_finalize_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000003',
  repeat('d', 64), 1::smallint
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
  '51000000-0000-0000-0000-000000000002',
  '52000000-0000-0000-0000-000000000001',
  'provision', now() + interval '10 minutes'
);
select pg_temp.expect_onboarding_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  'claim', now() + interval '10 minutes'
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
  (select lifecycle = 'active'
          and status = 'finalized'
          and saved_file_sha256 = repeat('a', 64)
          and storage_format = 1
   from public.finalize_game_character_provisioning(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000001',
     repeat('a', 64), 1::smallint
   )),
  'finalize after C save evidence must monotonically activate the reservation'
);
select pg_temp.assert_true(
  (select count(*) from public.finalize_game_character_provisioning(
    '51000000-0000-0000-0000-000000000001',
    '52000000-0000-0000-0000-000000000001',
    repeat('a', 64), 1::smallint
  )) = 1,
  'same finalize correlation and save payload must be idempotent'
);
select pg_temp.expect_finalize_rejection(
  '51000000-0000-0000-0000-000000000001',
  '52000000-0000-0000-0000-000000000001',
  repeat('b', 64), 1::smallint
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
  (select lifecycle = 'active'
          and status = 'finalized'
          and saved_file_sha256 = repeat('c', 64)
   from public.reconcile_game_character_provisioning(
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000002',
     repeat('c', 64), 1::smallint
   )),
  'reconcile must finalize a reserved request after a matching C save fingerprint'
);
select pg_temp.assert_true(
  (select count(*) from public.reconcile_game_character_provisioning(
    '51000000-0000-0000-0000-000000000001',
    '52000000-0000-0000-0000-000000000002',
    repeat('c', 64), 1::smallint
  )) = 1,
  'same reconcile correlation and save payload must be idempotent'
);

reset role;

insert into public.game_characters (
  world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle
) values (
  'onboarding-contract-world', 'Legacyhero', 'Legacyhero',
  substr(encode(public.digest(convert_to('Legacyhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed'
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
select pg_temp.assert_true(
  (select lifecycle = 'active'
          and owner_user_id = '51000000-0000-0000-0000-000000000001'::uuid
          and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero',
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'C-verified claim must atomically claim the character and finalize its onboarding intent'
);
select pg_temp.assert_true(
  (select count(*) = 1
   from public.claim_legacy_game_character_onboarding(
     'onboarding-contract-world', 'Legacyhero',
     '51000000-0000-0000-0000-000000000001',
     '52000000-0000-0000-0000-000000000004'
   )),
  'same finalized onboarding claim must be idempotent'
);
select pg_temp.expect_claim_onboarding_rejection(
  'onboarding-contract-world', 'Legacyhero',
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

rollback;
