\set ON_ERROR_STOP on

-- Run with psql after bootstrap and ALL current migrations. Never replay an
-- older function definition here: this contract tests the deployed catalog.
-- Every fixture and helper is rolled back, including immutable import rows.
begin;

create function pg_temp.assert_true(condition boolean, message text)
returns void language plpgsql as $$
begin
  if condition is not true then
    raise exception using errcode = 'P0004', message = message;
  end if;
end;
$$;

create function pg_temp.expect_denied(statement text, expected_message text)
returns void language plpgsql as $$
declare denied boolean := false; actual_message text;
begin
  begin
    execute statement;
  exception when sqlstate 'P0001' then
    denied := true;
    actual_message := SQLERRM;
  end;
  perform pg_temp.assert_true(denied, 'negative operation unexpectedly succeeded');
  perform pg_temp.assert_true(actual_message = expected_message,
    format('expected %s, got %s', expected_message, actual_message));
end;
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('c7100000-0000-4000-8000-000000000001', 'authenticated', 'authenticated',
   'claim-cancel-retry@example.invalid', 'contract-only', now(), '{}', '{}', now(), now()),
  ('c7100000-0000-4000-8000-000000000002', 'authenticated', 'authenticated',
   'claim-cancel-guards@example.invalid', 'contract-only', now(), '{}', '{}', now(), now());

insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count
) values ('claim-cancel-contract', 'main', 0, 'claim-cancel-batch', 'claim-cancel-manifest',
  repeat('a', 64), 1, '1.0.0', 1, 'claim-cancel-start', 'claim-cancel-end', 2);

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id
)
select id::uuid, 'claim-cancel-contract', name, name,
  substr(encode(public.digest(convert_to(name, 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('b', 64), null
from (values
  ('c7200000-0000-4000-8000-000000000001', 'Cancelretry'),
  ('c7200000-0000-4000-8000-000000000002', 'Cancelguard')
) as fixture(id, name);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
)
select 'claim-cancel-contract', 'main', 0, id
from public.game_characters where world_id = 'claim-cancel-contract';

do $$
declare
  actor constant uuid := 'c7100000-0000-4000-8000-000000000001';
  other_actor constant uuid := 'c7100000-0000-4000-8000-000000000002';
  correlation uuid;
  before_attempt jsonb;
  after_attempt jsonb;
  before_intent jsonb;
  original_character jsonb;
  result_status text;
  i integer;
begin
  -- All retry/rate-limit cases use ONE actor and ONE imported character.
  -- The second actor tests ownership/terminal guards only, never a retry.
  select to_jsonb(c) into original_character from public.game_characters c
    where id = 'c7200000-0000-4000-8000-000000000001';
  set local role service_role;
  perform public.begin_game_character_onboarding(actor,
    'c7300000-0000-4000-8000-000000000001', 'claim', clock_timestamp() + interval '10 minutes');
  select status into result_status from public.cancel_unreserved_game_character_onboarding(
    actor, 'c7300000-0000-4000-8000-000000000001');
  perform pg_temp.assert_true(result_status = 'cancelled', 'no-ledger intent cancels');
  reset role;
  perform pg_temp.assert_true(not exists (select 1 from private.game_character_claim_attempts
    where correlation_id = 'c7300000-0000-4000-8000-000000000001'), 'no-ledger cancellation creates no allowance');

  for i in 2..4 loop
    correlation := ('c7300000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid;
    set local role service_role;
    select status into result_status from public.begin_game_character_onboarding(
      actor, correlation, 'claim', clock_timestamp() + interval '10 minutes');
    perform pg_temp.assert_true(result_status = 'started', 'same actor can retry after cancellation');
    perform public.challenge_legacy_game_character_onboarding(
      'claim-cancel-contract', 'Cancelretry', repeat('b', 64), actor, correlation);
    reset role;
    select to_jsonb(a) into before_attempt from private.game_character_claim_attempts a
      where correlation_id = correlation;
    perform pg_temp.assert_true(before_attempt is not null and before_attempt->>'claimed_at' is null,
      'successful challenge creates an unclaimed allowance');
    set local role service_role;
    perform pg_temp.expect_denied(format(
      'select * from public.cancel_unreserved_game_character_onboarding(%L,%L)', other_actor, correlation),
      'onboarding intent does not belong to cancellation actor');
    select status into result_status from public.cancel_unreserved_game_character_onboarding(actor, correlation);
    perform pg_temp.assert_true(result_status = 'cancelled', 'allowed but unclaimed intent cancels');
    reset role;
    select to_jsonb(a) into after_attempt from private.game_character_claim_attempts a
      where correlation_id = correlation;
    perform pg_temp.assert_true(after_attempt = before_attempt, 'cancellation preserves every allowance field');
    select to_jsonb(n) into before_intent from private.game_character_onboarding_intents n
      where correlation_id = correlation;
    set local role service_role;
    perform public.cancel_unreserved_game_character_onboarding(actor, correlation);
    perform pg_temp.expect_denied(format(
      'select * from public.challenge_legacy_game_character_onboarding(%L,%L,%L,%L,%L)',
      'claim-cancel-contract', 'Cancelretry', repeat('b', 64), actor, correlation), 'claim challenge unavailable');
    perform pg_temp.expect_denied(format(
      'select * from public.claim_legacy_game_character_onboarding(%L,%L,%L,%L,%L)',
      'claim-cancel-contract', 'Cancelretry', repeat('b', 64), actor, correlation), 'onboarding claim unavailable');
    reset role;
    perform pg_temp.assert_true((select to_jsonb(n) = before_intent from private.game_character_onboarding_intents n
      where correlation_id = correlation), 'repeat cancellation preserves completion timestamp and all intent fields');
    perform pg_temp.assert_true((select to_jsonb(a) = before_attempt from private.game_character_claim_attempts a
      where correlation_id = correlation), 'rejected exact retries preserve all allowance fields');
    perform pg_temp.assert_true(not exists (select 1 from private.game_character_claim_requests
      where correlation_id = correlation), 'rejected claim leaves no claim request');
  end loop;

  set local role service_role;
  perform public.begin_game_character_onboarding(actor,
    'c7300000-0000-4000-8000-000000000005', 'claim', clock_timestamp() + interval '10 minutes');
  perform pg_temp.expect_denied(format(
    'select * from public.challenge_legacy_game_character_onboarding(%L,%L,%L,%L,%L)',
    'claim-cancel-contract', 'Cancelretry', repeat('b', 64), actor,
    'c7300000-0000-4000-8000-000000000005'), 'claim challenge unavailable');
  perform public.cancel_unreserved_game_character_onboarding(actor, 'c7300000-0000-4000-8000-000000000005');
  perform pg_temp.expect_denied(format(
    'select * from public.begin_game_character_onboarding(%L,%L,%L,%L::timestamptz)', actor,
    'c7300000-0000-4000-8000-000000000006', 'claim', clock_timestamp() + interval '10 minutes'),
    'claim onboarding rate limit exceeded');
  reset role;
  perform pg_temp.assert_true((select count(*) = 3 from private.game_character_claim_attempts
    where actor_user_id = actor), 'cancellation does not reset target challenge rate limit');
  perform pg_temp.assert_true((select count(*) = 5 from private.game_character_onboarding_intents
    where actor_user_id = actor), 'cancellation does not reset actor intent rate limit');
  perform pg_temp.assert_true((select to_jsonb(c) = original_character from public.game_characters c
    where id = 'c7200000-0000-4000-8000-000000000001'), 'failed-password retries do not change character ownership or lifecycle');
  perform pg_temp.assert_true(not exists (select 1 from private.game_character_onboarding_handoffs
    where actor_user_id = actor), 'cancelled allowances create no handoff');

  -- A genuinely completed claim cannot be rolled back by cancellation.
  set local role service_role;
  perform public.begin_game_character_onboarding(other_actor,
    'c7300000-0000-4000-8000-000000000010', 'claim', clock_timestamp() + interval '10 minutes');
  perform public.challenge_legacy_game_character_onboarding(
    'claim-cancel-contract', 'Cancelguard', repeat('b', 64), other_actor, 'c7300000-0000-4000-8000-000000000010');
  perform public.claim_legacy_game_character_onboarding(
    'claim-cancel-contract', 'Cancelguard', repeat('b', 64), other_actor, 'c7300000-0000-4000-8000-000000000010');
  perform pg_temp.expect_denied(format(
    'select * from public.cancel_unreserved_game_character_onboarding(%L,%L)',
    other_actor, 'c7300000-0000-4000-8000-000000000010'),
    'reserved or completed onboarding cannot be cancelled by the unreserved path');
  perform public.begin_game_character_onboarding(other_actor,
    'c7300000-0000-4000-8000-000000000011', 'provision', clock_timestamp() + interval '10 minutes');
  perform public.begin_game_character_provisioning(other_actor,
    'c7300000-0000-4000-8000-000000000011', 'claim-cancel-contract', 'Reserveguard');
  perform pg_temp.expect_denied(format(
    'select * from public.cancel_unreserved_game_character_onboarding(%L,%L)',
    other_actor, 'c7300000-0000-4000-8000-000000000011'),
    'reserved or completed onboarding cannot be cancelled by the unreserved path');
  reset role;
  perform pg_temp.assert_true((select status = 'finalized' from private.game_character_onboarding_intents
    where correlation_id = 'c7300000-0000-4000-8000-000000000010'), 'completed intent remains finalized');
  perform pg_temp.assert_true((select status = 'provisioning' from private.game_character_onboarding_intents
    where correlation_id = 'c7300000-0000-4000-8000-000000000011'), 'reserved intent remains provisioning');
end;
$$;

rollback;
