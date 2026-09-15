\set ON_ERROR_STOP on

-- Run after the immutable imported-unclaimed member relation and S5b.  The
-- fixture is rolled back so it can also validate replayed migration catalogs.
\ir ../migrations/20260930000000_imported_unclaimed_claim_provenance_gate.sql
\ir ../migrations/20260930000000_imported_unclaimed_claim_provenance_gate.sql

begin;

create or replace function pg_temp.assert_true(condition boolean, message text)
returns void language plpgsql as $$
begin
  if condition is not true then
    raise exception 'imported-unclaimed claim provenance gate contract failed: %', message;
  end if;
end;
$$;

create or replace function pg_temp.expect_unavailable(statement text, expected_message text default null)
returns void language plpgsql as $$
declare
  unavailable_raised boolean := false;
  unavailable_message text;
begin
  begin
    execute statement;
  exception when sqlstate 'P0001' then
    unavailable_raised := true;
    unavailable_message := SQLERRM;
  end;

  if not unavailable_raised then
    raise exception using
      errcode = 'P0004',
      message = 'provenance-gated operation unexpectedly succeeded';
  end if;

  if expected_message is not null and unavailable_message <> expected_message then
    raise exception using
      errcode = 'P0004',
      message = format('expected generic unavailable %s, got %s', expected_message, unavailable_message);
  end if;
end;
$$;

-- Guard the helper itself: a successfully executed negative statement must
-- surface a non-P0001 assertion failure rather than being accepted as unavailable.
do $$
declare
  assertion_raised boolean := false;
  assertion_message text;
begin
  begin
    perform pg_temp.expect_unavailable('select 1');
  exception when sqlstate 'P0004' then
    assertion_raised := true;
    assertion_message := SQLERRM;
  end;
  perform pg_temp.assert_true(
    assertion_raised and assertion_message = 'provenance-gated operation unexpectedly succeeded',
    'expect_unavailable rejects a successfully executed negative statement'
  );
end;
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('93000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'provenance-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('93000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'provenance-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count
) values (
  'provenance-gate-world', 'main', 0, 'provenance-gate-batch', 'provenance-gate-manifest',
  repeat('a', 64), 1, '1.0.0', 1, 'provenance-gate-start', 'provenance-gate-end', 32
);

create or replace function pg_temp.make_imported_character(
  p_id uuid, p_name text, p_sha text, p_member boolean
) returns void language plpgsql as $$
begin
  insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
    storage_format, imported_file_sha256, owner_user_id
  ) values (
    p_id, 'provenance-gate-world', p_name, p_name,
    substr(encode(public.digest(convert_to(p_name, 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'imported_unclaimed', 1, p_sha, null
  );
  if p_member then
    insert into private.game_imported_unclaimed_batch_members(
      world_id, stream_id, batch_sequence, character_id
    ) values ('provenance-gate-world', 'main', 0, p_id);
  end if;
end;
$$;

-- Missing immutable provenance fails before challenge-attempt mutation.
select pg_temp.make_imported_character('94000000-0000-0000-0000-000000000001', 'NoMemChal', repeat('b', 64), false);
set local role service_role;
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000001',
    'claim', clock_timestamp() + interval '10 minutes'
  )),
  'missing-member challenge fixture starts normally'
);
select pg_temp.expect_unavailable($$
  select * from public.challenge_legacy_game_character_onboarding(
    'provenance-gate-world', 'NoMemChal', repeat('b', 64),
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000001'
  )
$$, 'claim challenge unavailable');
reset role;
select pg_temp.assert_true(
  (select status = 'started' from private.game_character_onboarding_intents where correlation_id = '95000000-0000-0000-0000-000000000001')
  and not exists (select 1 from private.game_character_claim_attempts where correlation_id = '95000000-0000-0000-0000-000000000001'),
  'missing-member challenge leaves intent and attempt state unchanged'
);

-- A forged/pre-existing allowance cannot bypass the member check in claim;
-- no claim request, ownership, handoff, or completion mutation is permitted.
select pg_temp.make_imported_character('94000000-0000-0000-0000-000000000002', 'NoMemClaim', repeat('c', 64), false);
set local role service_role;
select * from public.begin_game_character_onboarding(
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000002',
  'claim', clock_timestamp() + interval '10 minutes'
);
reset role;
insert into private.game_character_claim_attempts(
  correlation_id, actor_user_id, character_id, imported_file_sha256, allowed_at, allow_expires_at
) values (
  '95000000-0000-0000-0000-000000000002', '93000000-0000-0000-0000-000000000001',
  '94000000-0000-0000-0000-000000000002', repeat('c', 64), now(), now() + interval '90 seconds'
);
set local role service_role;
select pg_temp.expect_unavailable($$
  select * from public.claim_legacy_game_character_onboarding(
    'provenance-gate-world', 'NoMemClaim', repeat('c', 64),
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000002'
  )
$$, 'onboarding claim unavailable');
reset role;
select pg_temp.assert_true(
  (select lifecycle = 'imported_unclaimed' and owner_user_id is null and claimed_at is null
     from public.game_characters where id = '94000000-0000-0000-0000-000000000002')
  and (select status = 'started' and completed_at is null from private.game_character_onboarding_intents where correlation_id = '95000000-0000-0000-0000-000000000002')
  and (select claimed_at is null from private.game_character_claim_attempts where correlation_id = '95000000-0000-0000-0000-000000000002')
  and not exists (select 1 from private.game_character_claim_requests where correlation_id = '95000000-0000-0000-0000-000000000002')
  and not exists (select 1 from private.game_character_onboarding_handoffs where correlation_id = '95000000-0000-0000-0000-000000000002'),
  'missing-member claim leaves every ownership and handoff state unchanged'
);

-- Positive immutable membership preserves the established challenge, exact
-- retry, concurrent/ownership contender conflict, pending handoff, and activation behavior.
select pg_temp.make_imported_character('94000000-0000-0000-0000-000000000003', 'Memberdirect', repeat('d', 64), true);
set local role service_role;
select * from public.begin_game_character_onboarding(
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000003', 'claim', clock_timestamp() + interval '10 minutes'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed' from public.challenge_legacy_game_character_onboarding(
    'provenance-gate-world', 'Memberdirect', repeat('d', 64),
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000003'
  )), 'member-positive challenge succeeds'
);
select * from public.begin_game_character_onboarding(
  '93000000-0000-0000-0000-000000000002', '95000000-0000-0000-0000-000000000004', 'claim', clock_timestamp() + interval '10 minutes'
);
select pg_temp.assert_true(
  (select challenge_status = 'allowed' from public.challenge_legacy_game_character_onboarding(
    'provenance-gate-world', 'Memberdirect', repeat('d', 64),
    '93000000-0000-0000-0000-000000000002', '95000000-0000-0000-0000-000000000004'
  )), 'competing actor can hold its pre-ownership challenge'
);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'provenance-gate-world', 'Memberdirect', repeat('d', 64),
     '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000003'
   )), 'member-positive claim remains handoff_pending'
);
select pg_temp.expect_unavailable($$
  select * from public.claim_legacy_game_character_onboarding(
    'provenance-gate-world', 'Memberdirect', repeat('d', 64),
    '93000000-0000-0000-0000-000000000002', '95000000-0000-0000-0000-000000000004'
  )
$$);
reset role;
update private.game_character_claim_attempts
   set allowed_at = now() - interval '2 minutes',
       allow_expires_at = now() - interval '30 seconds'
 where correlation_id = '95000000-0000-0000-0000-000000000003';
set local role service_role;
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' and onboarding_status = 'finalized'
   from public.claim_legacy_game_character_onboarding(
     'provenance-gate-world', 'Memberdirect', repeat('d', 64),
     '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000003'
   )), 'exact completed retry remains valid after challenge expiry'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' from public.activate_game_character_onboarding_handoff(
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000003',
    '94000000-0000-0000-0000-000000000003', 'claim'
  )), 'exact handoff callback is still the activation transition'
);

-- Existing SHA/name mismatch and expiry outcomes remain generic and do not
-- create an attempt or complete an expired member-positive claim.
select pg_temp.make_imported_character('94000000-0000-0000-0000-000000000004', 'Mismatch', repeat('e', 64), true);
select * from public.begin_game_character_onboarding(
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000005', 'claim', clock_timestamp() + interval '10 minutes'
);
select pg_temp.expect_unavailable($$
  select * from public.challenge_legacy_game_character_onboarding(
    'provenance-gate-world', 'Mismatch', repeat('f', 64),
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000005'
  )
$$);
select pg_temp.expect_unavailable($$
  select * from public.challenge_legacy_game_character_onboarding(
    'provenance-gate-world', 'Missingname', repeat('e', 64),
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000005'
  )
$$);
select pg_temp.assert_true(
  not exists (select 1 from private.game_character_claim_attempts where correlation_id = '95000000-0000-0000-0000-000000000005'),
  'SHA/name mismatch creates no challenge attempt'
);
select pg_temp.make_imported_character('94000000-0000-0000-0000-000000000005', 'Expired', repeat('f', 64), true);
select * from public.begin_game_character_onboarding(
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000006', 'claim', clock_timestamp() + interval '10 minutes'
);
select * from public.challenge_legacy_game_character_onboarding(
  'provenance-gate-world', 'Expired', repeat('f', 64),
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000006'
);
reset role;
update private.game_character_claim_attempts
   set allowed_at = now() - interval '2 minutes',
       allow_expires_at = now() - interval '30 seconds'
 where correlation_id = '95000000-0000-0000-0000-000000000006';
set local role service_role;
select pg_temp.expect_unavailable($$
  select * from public.claim_legacy_game_character_onboarding(
    'provenance-gate-world', 'Expired', repeat('f', 64),
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000006'
  )
$$);
reset role;
select pg_temp.assert_true(
  (select lifecycle = 'imported_unclaimed' and owner_user_id is null from public.game_characters where id = '94000000-0000-0000-0000-000000000005')
  and not exists (select 1 from private.game_character_claim_requests where correlation_id = '95000000-0000-0000-0000-000000000006'),
  'expired member-positive claim does not mutate ownership or request state'
);

-- The evidence finalizer delegates into the same member-gated claim RPC.
select pg_temp.make_imported_character('94000000-0000-0000-0000-000000000006', 'Evidence', repeat('1', 64), true);
set local role service_role;
select * from public.begin_game_character_onboarding(
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000007', 'claim', clock_timestamp() + interval '10 minutes'
);
select * from public.challenge_legacy_game_character_onboarding(
  'provenance-gate-world', 'Evidence', repeat('1', 64),
  '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000007'
);
select pg_temp.assert_true(
  (select mode = 'claim' and lifecycle = 'handoff_pending'
   from public.finalize_game_character_legacy_identity_evidence(
     '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000007',
     '94000000-0000-0000-0000-000000000006', 'ok', 'Evidence', repeat('1', 64),
     1::smallint, 'player-v1', substr(encode(public.digest(convert_to('Evidence', 'UTF8'), 'sha1'), 'hex'), 1, 2)
   )), 'evidence finalizer uses the member-positive claim route'
);
select pg_temp.assert_true(
  (select lifecycle = 'active' from public.activate_game_character_onboarding_handoff(
    '93000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000007',
    '94000000-0000-0000-0000-000000000006', 'claim'
  )), 'evidence claim also requires the independent handoff activation'
);

-- No provenance backfill: a preexisting active legacy row without a member
-- remains usable by normal session lease behavior.
reset role;
insert into public.game_characters(
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, owner_user_id, claimed_at
) values (
  '94000000-0000-0000-0000-000000000007', 'provenance-gate-world', 'Legacylease', 'Legacylease',
  substr(encode(public.digest(convert_to('Legacylease', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'active', '93000000-0000-0000-0000-000000000001', clock_timestamp()
);
set local role service_role;
select pg_temp.assert_true(
  (select count(*) = 1 from public.begin_game_character_session(
    '93000000-0000-0000-0000-000000000001', '94000000-0000-0000-0000-000000000007',
    '96000000-0000-0000-0000-000000000001', 'legacy-no-member', clock_timestamp() + interval '1 minute'
  )), 'legacy active no-member row retains normal session lease behavior'
);

rollback;
