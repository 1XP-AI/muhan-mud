\set ON_ERROR_STOP on

-- Run after migrations through 20260922100000 against a disposable Supabase
-- database.  Fixtures roll back; this is also a replay-safe catalog contract.

-- The migration pair must be safe when a CI/job replay reaches this contract.
\ir ../migrations/20260922000000_onboarding_handoff_lifecycle.sql
\ir ../migrations/20260922100000_onboarding_handoff_gate.sql

begin;

create or replace function pg_temp.assert_true(condition boolean, message text)
returns void language plpgsql as $$
begin
  if condition is not true then
    raise exception 'onboarding handoff gate contract failed: %', message;
  end if;
end;
$$;

create or replace function pg_temp.expect_rejection(statement text)
returns void language plpgsql as $$
begin
  execute statement;
  raise exception using errcode = 'P0002', message = 'handoff operation unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then return;
end;
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('83000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'handoff-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('83000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'handoff-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

select pg_temp.assert_true(
  to_regclass('private.game_character_onboarding_handoffs') is not null
  and (select relrowsecurity from pg_class where oid = 'private.game_character_onboarding_handoffs'::regclass)
  and not has_table_privilege('service_role', 'private.game_character_onboarding_handoffs', 'select')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_handoffs', 'insert')
  and has_function_privilege('service_role', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and not has_function_privilege('anon', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and pg_get_function_result('public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)'::regprocedure)
      = 'TABLE(character_id uuid, actor_user_id uuid, correlation_id uuid, lifecycle public.character_lifecycle, onboarding_status text)',
  'handoff ledger is private and activation has the narrow service-only five-field contract'
);

create temp table pg_temp.fixture(kind text primary key, character_id uuid not null);

set local role service_role;

-- Provision completion accepts C save evidence but does not issue admission.
insert into pg_temp.fixture(kind, character_id)
select 'provision', character_id
from public.begin_game_character_onboarding(
  '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000001',
  'provision', clock_timestamp() + interval '10 minutes'
) as intent
join lateral public.begin_game_character_provisioning(
  intent.actor_user_id, intent.correlation_id, 'handoff-world', 'provisiongate'
) as provision on true;

select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and status = 'finalized'
   from public.finalize_game_character_provisioning(
     '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000001', repeat('a', 64), 1
   )),
  'provision completion ends in handoff_pending'
);
select pg_temp.expect_rejection(format(
  'select * from public.begin_game_character_session(%L::uuid, %L::uuid, %L::uuid, %L, clock_timestamp()+interval ''1 minute'')',
  '83000000-0000-0000-0000-000000000001',
  (select character_id::text from pg_temp.fixture where kind = 'provision'),
  '85000000-0000-0000-0000-000000000001', 'handoff-contract'
));
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '83000000-0000-0000-0000-000000000002', '84000000-0000-0000-0000-000000000001',
  (select character_id::text from pg_temp.fixture where kind = 'provision'), 'provision'
));
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000001',
  (select character_id::text from pg_temp.fixture where kind = 'provision'), 'claim'
));
select pg_temp.assert_true(
  (select to_jsonb(activation) = jsonb_build_object(
    'character_id', (select character_id from pg_temp.fixture where kind = 'provision'),
    'actor_user_id', '83000000-0000-0000-0000-000000000001'::uuid,
    'correlation_id', '84000000-0000-0000-0000-000000000001'::uuid,
    'lifecycle', 'active', 'onboarding_status', 'finalized'
  ) from public.activate_game_character_onboarding_handoff(
    '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000001',
    (select character_id from pg_temp.fixture where kind = 'provision'), 'provision'
  ) as activation),
  'the exact provision callback activates and returns exactly the five-field contract'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000001',
     (select character_id from pg_temp.fixture where kind = 'provision'), 'provision'
   )),
  'an exact activated provision callback is idempotent'
);
select pg_temp.assert_true(
  (select count(*) = 1 from public.begin_game_character_session(
    '83000000-0000-0000-0000-000000000001',
    (select character_id from pg_temp.fixture where kind = 'provision'),
    '85000000-0000-0000-0000-000000000001', 'handoff-contract', clock_timestamp() + interval '1 minute'
  )),
  'exact activation permits one normal session lease'
);
select pg_temp.expect_rejection(format(
  'select * from public.begin_game_character_session(%L::uuid, %L::uuid, %L::uuid, %L, clock_timestamp()+interval ''1 minute'')',
  '83000000-0000-0000-0000-000000000001',
  (select character_id::text from pg_temp.fixture where kind = 'provision'),
  '85000000-0000-0000-0000-000000000002', 'other-gateway'
));

reset role;
insert into public.game_characters(world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, imported_file_sha256)
values ('handoff-world', 'Claimgate', 'Claimgate',
  substr(encode(public.digest(convert_to('Claimgate', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', repeat('b', 64));
insert into pg_temp.fixture(kind, character_id)
select 'claim', id from public.game_characters where world_id = 'handoff-world' and legacy_name_key = 'Claimgate';

set local role service_role;
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000002',
    'claim', clock_timestamp() + interval '10 minutes'
  )),
  'claim fixture starts an onboarding intent'
);
select pg_temp.assert_true(
  (select character_id = (select character_id from pg_temp.fixture where kind = 'claim')
   from public.challenge_legacy_game_character_onboarding(
     'handoff-world', 'Claimgate', repeat('b', 64),
     '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000002'
   )),
  'claim still requires the existing fingerprint-bound challenge'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'handoff-world', 'Claimgate', repeat('b', 64),
     '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000002'
   )),
  'claim completion ends in handoff_pending'
);
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000099',
  (select character_id::text from pg_temp.fixture where kind = 'claim'), 'claim'
));
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
   from public.activate_game_character_onboarding_handoff(
     '83000000-0000-0000-0000-000000000001', '84000000-0000-0000-0000-000000000002',
     (select character_id from pg_temp.fixture where kind = 'claim'), 'claim'
   )),
  'the exact claim callback is the only claim activation path'
);

-- Evidence-aware provisioning must remain valid while it is pending.  Its
-- immutable receipt retry stays pending until the independent callback.
insert into pg_temp.fixture(kind, character_id)
select 'evidence', character_id
from public.begin_game_character_onboarding(
  '83000000-0000-0000-0000-000000000002', '84000000-0000-0000-0000-000000000003',
  'provision', clock_timestamp() + interval '10 minutes'
) as intent
join lateral public.begin_game_character_provisioning(
  intent.actor_user_id, intent.correlation_id, 'handoff-world', 'evidencegate'
) as provision on true;
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and mode = 'provision'
   from public.finalize_game_character_legacy_identity_evidence(
     '83000000-0000-0000-0000-000000000002', '84000000-0000-0000-0000-000000000003',
     (select character_id from pg_temp.fixture where kind = 'evidence'), 'ok', 'Evidencegate', repeat('c', 64),
     1::smallint, 'player-v1', substr(encode(public.digest(convert_to('Evidencegate', 'UTF8'), 'sha1'), 'hex'), 1, 2)
   )),
  'evidence finalization records a pending, non-admissible handoff'
);
select pg_temp.assert_true(
  (select count(*) = 1 from public.finalize_game_character_legacy_identity_evidence(
     '83000000-0000-0000-0000-000000000002', '84000000-0000-0000-0000-000000000003',
     (select character_id from pg_temp.fixture where kind = 'evidence'), 'ok', 'Evidencegate', repeat('c', 64),
     1::smallint, 'player-v1', substr(encode(public.digest(convert_to('Evidencegate', 'UTF8'), 'sha1'), 'hex'), 1, 2)
   )),
  'exact evidence retry remains valid while the handoff is pending'
);
select pg_temp.expect_rejection(format(
  'select * from public.begin_game_character_session(%L::uuid, %L::uuid, %L::uuid, %L, clock_timestamp()+interval ''1 minute'')',
  '83000000-0000-0000-0000-000000000002',
  (select character_id::text from pg_temp.fixture where kind = 'evidence'),
  '85000000-0000-0000-0000-000000000003', 'evidence-before-callback'
));

rollback;
