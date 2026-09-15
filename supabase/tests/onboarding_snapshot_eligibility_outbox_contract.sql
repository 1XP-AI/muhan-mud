\set ON_ERROR_STOP on

-- RED: run after migration 20260922100000 only; the catalog assertion below
-- fails because no eligibility outbox exists.  GREEN: apply migration 230
-- (twice, to prove replay safety) and rerun this disposable contract.

begin;

create or replace function pg_temp.assert_true(condition boolean, message text)
returns void language plpgsql as $$
begin
  if condition is not true then
    raise exception 'onboarding snapshot eligibility outbox contract failed: %', message;
  end if;
end;
$$;

create or replace function pg_temp.expect_rejection(statement text)
returns void language plpgsql as $$
begin
  execute statement;
  raise exception using errcode = 'P0002', message = 'handoff activation unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then return;
end;
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('86000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'eligibility-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('86000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'eligibility-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

select pg_temp.assert_true(
  to_regclass('private.game_character_onboarding_snapshot_eligibility_outbox') is not null
  and (select relrowsecurity from pg_class where oid = 'private.game_character_onboarding_snapshot_eligibility_outbox'::regclass)
  and not has_table_privilege('service_role', 'private.game_character_onboarding_snapshot_eligibility_outbox', 'select')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_snapshot_eligibility_outbox', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_snapshot_eligibility_outbox', 'update')
  and not has_table_privilege('service_role', 'private.game_character_onboarding_snapshot_eligibility_outbox', 'delete')
  and (select count(*) = 1 from pg_constraint
       where conrelid = 'private.game_character_onboarding_snapshot_eligibility_outbox'::regclass
         and contype = 'p'),
  'the private correlation-keyed outbox is RLS-protected and service_role has no direct CRUD'
);

select pg_temp.assert_true(
  (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p
    where p.oid = 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)'::regprocedure)
  and has_function_privilege('service_role', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and not has_function_privilege('anon', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.activate_game_character_onboarding_handoff(uuid,uuid,uuid,text)', 'execute'),
  'activation is SECURITY DEFINER with a pinned search_path and service_role-only execution'
);

create temp table pg_temp.fixture(character_id uuid not null);

set local role service_role;

insert into pg_temp.fixture(character_id)
select character_id
from public.begin_game_character_onboarding(
  '86000000-0000-0000-0000-000000000001',
  '87000000-0000-0000-0000-000000000001',
  'provision', clock_timestamp() + interval '10 minutes'
) as intent
join lateral public.begin_game_character_provisioning(
  intent.actor_user_id, intent.correlation_id, 'eligibility-world', 'Eligibilityhero'
) as provision on true;

select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and status = 'finalized'
     from public.finalize_game_character_provisioning(
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000001', repeat('a', 64), 1::smallint
     )),
  'completion creates only a pending handoff, not snapshot eligibility'
);

reset role;
select pg_temp.assert_true(
  (select count(*) = 0
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = '87000000-0000-0000-0000-000000000001'::uuid),
  'no eligibility row exists before an exact activation'
);
set local role service_role;

select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '86000000-0000-0000-0000-000000000002',
  '87000000-0000-0000-0000-000000000001',
  (select character_id::text from pg_temp.fixture), 'provision'
));
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '86000000-0000-0000-0000-000000000001',
  '87000000-0000-0000-0000-000000000002',
  (select character_id::text from pg_temp.fixture), 'provision'
));
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '86000000-0000-0000-0000-000000000001',
  '87000000-0000-0000-0000-000000000001',
  '88000000-0000-0000-0000-000000000001', 'provision'
));
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '86000000-0000-0000-0000-000000000001',
  '87000000-0000-0000-0000-000000000001',
  (select character_id::text from pg_temp.fixture), 'claim'
));

reset role;
select pg_temp.assert_true(
  (select count(*) = 0
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = '87000000-0000-0000-0000-000000000001'::uuid),
  'every mismatched activation leaves no eligibility row'
);
set local role service_role;

select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
     from public.activate_game_character_onboarding_handoff(
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000001',
       (select character_id from pg_temp.fixture), 'provision'
     )),
  'the exact activation succeeds'
);

reset role;
select pg_temp.assert_true(
  (select count(*) = 1
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = '87000000-0000-0000-0000-000000000001'::uuid)
  and (select actor_user_id = '86000000-0000-0000-0000-000000000001'::uuid
              and character_id = (select character_id from pg_temp.fixture)
              and mode = 'provision'
              and status = 'pending'
              and enqueued_at is not null
         from private.game_character_onboarding_snapshot_eligibility_outbox
        where correlation_id = '87000000-0000-0000-0000-000000000001'::uuid),
  'exact activation atomically leaves one matching pending eligibility row'
);
set local role service_role;

select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
     from public.activate_game_character_onboarding_handoff(
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000001',
       (select character_id from pg_temp.fixture), 'provision'
     )),
  'the exact activation retry remains successful'
);

reset role;
select pg_temp.assert_true(
  (select count(*) = 1 and bool_and(status = 'pending')
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = '87000000-0000-0000-0000-000000000001'::uuid),
  'the exact activation retry neither duplicates nor fulfills eligibility'
);

-- RED: a successful claim activation must enqueue the same single pending
-- eligibility row as provision activation.  GREEN: the row is correlation
-- keyed and carries the exact claim actor, character, and mode.
reset role;
insert into public.game_characters(
  world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, imported_file_sha256
) values (
  'eligibility-world', 'Eligclaim', 'Eligclaim',
  substr(encode(public.digest(convert_to('Eligclaim', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', repeat('b', 64)
);
set local role service_role;

select pg_temp.assert_true(
  (select status = 'started'
     from public.begin_game_character_onboarding(
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000002',
       'claim', clock_timestamp() + interval '10 minutes'
     )),
  'claim eligibility fixture starts an onboarding intent'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
     from public.challenge_legacy_game_character_onboarding(
       'eligibility-world', 'Eligclaim', repeat('b', 64),
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000002'
     )),
  'claim eligibility fixture receives an allowed fingerprint challenge'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
     from public.claim_legacy_game_character_onboarding(
       'eligibility-world', 'Eligclaim', repeat('b', 64),
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000002'
     )),
  'claim eligibility fixture completes with a pending handoff'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
     from public.activate_game_character_onboarding_handoff(
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000002',
       (select id from public.game_characters
         where world_id = 'eligibility-world'
           and legacy_name_key = 'Eligclaim'),
       'claim'
     )),
  'the exact claim activation succeeds'
);

reset role;
select pg_temp.assert_true(
  (select count(*) = 1
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = '87000000-0000-0000-0000-000000000002'::uuid)
  and (select actor_user_id = '86000000-0000-0000-0000-000000000001'::uuid
              and character_id = (select id from public.game_characters
                                    where world_id = 'eligibility-world'
                                      and legacy_name_key = 'Eligclaim')
              and mode = 'claim'
              and status = 'pending'
              and enqueued_at is not null
         from private.game_character_onboarding_snapshot_eligibility_outbox
        where correlation_id = '87000000-0000-0000-0000-000000000002'::uuid),
  'exact claim activation atomically leaves one matching pending eligibility row'
);

-- RED: reach the activation guard after the eligibility insert using a
-- deterministic inconsistent handoff fixture.  GREEN: the activation
-- exception rolls back that insert, leaving no eligibility row behind.
reset role;
insert into public.game_characters(
  world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, imported_file_sha256
) values (
  'eligibility-world', 'Eligrollback', 'Eligrollback',
  substr(encode(public.digest(convert_to('Eligrollback', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', repeat('c', 64)
);
set local role service_role;

select pg_temp.assert_true(
  (select status = 'started'
     from public.begin_game_character_onboarding(
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000003',
       'claim', clock_timestamp() + interval '10 minutes'
     )),
  'rollback fixture starts an onboarding intent'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed'
     from public.challenge_legacy_game_character_onboarding(
       'eligibility-world', 'Eligrollback', repeat('c', 64),
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000003'
     )),
  'rollback fixture receives an allowed fingerprint challenge'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
     from public.claim_legacy_game_character_onboarding(
       'eligibility-world', 'Eligrollback', repeat('c', 64),
       '86000000-0000-0000-0000-000000000001',
       '87000000-0000-0000-0000-000000000003'
     )),
  'rollback fixture completes with a pending handoff'
);

reset role;
update private.game_character_onboarding_handoffs
   set status = 'activated', activated_at = clock_timestamp()
 where correlation_id = '87000000-0000-0000-0000-000000000003'::uuid;
select pg_temp.assert_true(
  (select status = 'activated' and activated_at is not null
     from private.game_character_onboarding_handoffs
    where correlation_id = '87000000-0000-0000-0000-000000000003'::uuid)
  and (select lifecycle = 'handoff_pending'
     from public.game_characters
    where world_id = 'eligibility-world'
      and legacy_name_key = 'Eligrollback'),
  'rollback fixture is activated in the ledger while its character remains pending'
);

set local role service_role;
select pg_temp.expect_rejection(format(
  'select * from public.activate_game_character_onboarding_handoff(%L::uuid, %L::uuid, %L::uuid, %L)',
  '86000000-0000-0000-0000-000000000001',
  '87000000-0000-0000-0000-000000000003',
  (select id::text from public.game_characters
    where world_id = 'eligibility-world'
      and legacy_name_key = 'Eligrollback'),
  'claim'
));

reset role;
select pg_temp.assert_true(
  (select count(*) = 0
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = '87000000-0000-0000-0000-000000000003'::uuid)
  and (select status = 'activated' and activated_at is not null
     from private.game_character_onboarding_handoffs
    where correlation_id = '87000000-0000-0000-0000-000000000003'::uuid)
  and (select lifecycle = 'handoff_pending'
     from public.game_characters
    where world_id = 'eligibility-world'
      and legacy_name_key = 'Eligrollback'),
  'post-insert activation failure rolls back with no eligibility row'
);

rollback;
