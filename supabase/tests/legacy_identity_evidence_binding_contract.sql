\set ON_ERROR_STOP on

-- Run after all migrations against a disposable self-hosted Supabase database:
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f supabase/tests/legacy_identity_evidence_binding_contract.sql
-- The transaction rolls every fixture row back.

begin;

create or replace function pg_temp.assert_true(condition boolean, message text)
returns void language plpgsql as $$
begin
  if condition is not true then
    raise exception 'legacy identity evidence binding contract failed: %', message;
  end if;
end;
$$;

create or replace function pg_temp.expect_evidence_rejection(
  p_actor uuid, p_correlation uuid, p_character uuid, p_outcome text,
  p_canonical_name text, p_sha256 text, p_version smallint, p_storage text,
  p_legacy_shard text default null
)
returns void language plpgsql as $$
begin
  perform public.finalize_game_character_legacy_identity_evidence(
    p_actor, p_correlation, p_character, p_outcome, p_canonical_name,
    p_sha256, p_version, p_storage,
    coalesce(p_legacy_shard, case when p_canonical_name is null then null else substr(
      encode(public.digest(convert_to(p_canonical_name, 'UTF8'), 'sha1'), 'hex'), 1, 2
    ) end)
  );
  raise exception using errcode = 'P0002', message = 'evidence finalization unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then return;
end;
$$;

create or replace function pg_temp.no_evidence_ledger_crud(p_role name)
returns boolean language sql as $$
  select not has_table_privilege(p_role, 'private.game_character_legacy_identity_evidence', 'select')
     and not has_table_privilege(p_role, 'private.game_character_legacy_identity_evidence', 'insert')
     and not has_table_privilege(p_role, 'private.game_character_legacy_identity_evidence', 'update')
     and not has_table_privilege(p_role, 'private.game_character_legacy_identity_evidence', 'delete');
$$;

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('71000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'evidence-binding-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('71000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'evidence-binding-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

select pg_temp.assert_true(
  to_regclass('private.game_character_legacy_identity_evidence') is not null
  and (select relrowsecurity from pg_class where oid = 'private.game_character_legacy_identity_evidence'::regclass),
  'the correlation-keyed legacy identity evidence ledger must be private and RLS protected'
);
select pg_temp.assert_true(
  pg_temp.no_evidence_ledger_crud('anon')
  and pg_temp.no_evidence_ledger_crud('authenticated')
  and pg_temp.no_evidence_ledger_crud('service_role')
  and has_function_privilege('service_role', 'public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text,text)', 'execute')
  and not has_function_privilege('anon', 'public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text,text)', 'execute')
  and to_regprocedure('public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text)') is not null
  and has_function_privilege('service_role', 'public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text)', 'execute')
  and not has_function_privilege('anon', 'public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text)', 'execute')
  and not has_function_privilege('authenticated', 'public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text)', 'execute')
  and pg_get_function_result('public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text)'::regprocedure)
      = 'TABLE(character_id uuid, actor_user_id uuid, mode text, lifecycle public.character_lifecycle, canonical_legacy_name text, player_file_sha256 text, evidence_version smallint, storage_format text, recorded_at timestamp with time zone)'
  and pg_get_function_result('public.finalize_game_character_legacy_identity_evidence(uuid,uuid,uuid,text,text,text,smallint,text,text)'::regprocedure)
      = 'TABLE(character_id uuid, actor_user_id uuid, mode text, lifecycle public.character_lifecycle, world_id text, canonical_legacy_name text, legacy_shard character(2), player_file_sha256 text, evidence_version smallint, storage_format text, recorded_at timestamp with time zone)',
  'only service role may reach the private ledger through the shard-aware RPC while the old signature remains rolling-compatible'
);

set local role service_role;

-- Positive provision: the evidence SHA is saved-file evidence, never import evidence.
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '71000000-0000-0000-0000-000000000001',
    '72000000-0000-0000-0000-000000000001', 'provision', now() + interval '10 minutes'
  )),
  'provision evidence fixture starts an onboarding intent'
);
create temp table pg_temp.evidence_fixture (kind text primary key, character_id uuid not null);
insert into pg_temp.evidence_fixture(kind, character_id)
select 'provision', character_id
from public.begin_game_character_provisioning(
  '71000000-0000-0000-0000-000000000001',
  '72000000-0000-0000-0000-000000000001', 'evidence-binding-world', 'provisionhero'
);

select pg_temp.assert_true(
  (select mode = 'provision' and lifecycle = 'active' and world_id = 'evidence-binding-world'
          and evidence_version = 1 and storage_format = 'player-v1'
          and canonical_legacy_name = 'Provisionhero' and legacy_shard = 'ed'
          and player_file_sha256 = repeat('a', 64)
   from public.finalize_game_character_legacy_identity_evidence(
     '71000000-0000-0000-0000-000000000001',
     '72000000-0000-0000-0000-000000000001',
     (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
     'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1', 'ed'
   )),
  'ok provision evidence finalizes the exact reserved character'
);

reset role;
select pg_temp.assert_true(
  (select evidence.actor_user_id = '71000000-0000-0000-0000-000000000001'::uuid
          and evidence.character_id = fixture.character_id
          and evidence.mode = 'provision' and evidence.world_id = 'evidence-binding-world'
          and evidence.canonical_legacy_name = 'Provisionhero' and evidence.legacy_shard = 'ed'
          and evidence.player_file_sha256 = repeat('a', 64) and evidence.outcome = 'ok'
          and evidence.evidence_version = 1 and evidence.storage_format = 'player-v1'
          and evidence.recorded_at is not null
   from private.game_character_legacy_identity_evidence evidence
   join pg_temp.evidence_fixture fixture on fixture.kind = 'provision'
   where evidence.correlation_id = '72000000-0000-0000-0000-000000000001')
  and (select saved_file_sha256 = repeat('a', 64) and storage_format = 1
       from private.game_character_provisioning_requests
       where correlation_id = '72000000-0000-0000-0000-000000000001')
  and (select imported_file_sha256 is null from public.game_characters
       where id = (select character_id from pg_temp.evidence_fixture where kind = 'provision')),
  'provision ledger is complete and does not reinterpret save evidence as imported-file evidence'
);

set local role service_role;
select pg_temp.assert_true(
  (select count(*) = 1 from public.finalize_game_character_legacy_identity_evidence(
    '71000000-0000-0000-0000-000000000001',
    '72000000-0000-0000-0000-000000000001',
    (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
    'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1', 'ed'
  )),
  'an exact provision evidence retry is idempotent'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('b', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000009',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'pRoViSiOnHeRo', repeat('a', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 2::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v2'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1', '00'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1', 'ED'
);

-- A retry is only successful while the completed private save receipt still
-- proves player-v1 storage for this exact character identity.
reset role;
update private.game_character_provisioning_requests
   set storage_format = 2
 where correlation_id = '72000000-0000-0000-0000-000000000001';
set local role service_role;
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000001',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1'
);
reset role;
select pg_temp.assert_true(
  (select world_id = 'evidence-binding-world' and legacy_shard = 'ed'
          and storage_format = 'player-v1' and player_file_sha256 = repeat('a', 64)
   from private.game_character_legacy_identity_evidence
   where correlation_id = '72000000-0000-0000-0000-000000000001'),
  'a rejected provision retry leaves immutable identity evidence unchanged'
);
update private.game_character_provisioning_requests
   set storage_format = 1
 where correlation_id = '72000000-0000-0000-0000-000000000001';
set local role service_role;
select pg_temp.assert_true(
  (select count(*) = 1 from public.finalize_game_character_legacy_identity_evidence(
    '71000000-0000-0000-0000-000000000001',
    '72000000-0000-0000-0000-000000000001',
    (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
    'ok', 'Provisionhero', repeat('a', 64), 1::smallint, 'player-v1', 'ed'
  )),
  'restored matching provision storage permits a read-only exact retry'
);

-- The finalizer accepts only the successful wire outcome. Every other V1 outcome
-- must fail closed and roll back both the lifecycle and any ledger write.
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '71000000-0000-0000-0000-000000000002',
    '72000000-0000-0000-0000-000000000002', 'provision', now() + interval '10 minutes'
  )),
  'non-ok fixture starts an onboarding intent'
);
insert into pg_temp.evidence_fixture(kind, character_id)
select 'nonok', character_id from public.begin_game_character_provisioning(
  '71000000-0000-0000-0000-000000000002',
  '72000000-0000-0000-0000-000000000002', 'evidence-binding-world', 'nonokhero'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'not_found', 'Nonokhero', repeat('c', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'corrupt', 'Nonokhero', repeat('c', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'io_error', 'Nonokhero', repeat('c', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'invalid_input', 'Nonokhero', repeat('c', 64), 1::smallint, 'player-v1'
);

-- Unfinalized tuple mismatches and malformed evidence cannot partially activate it.
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'provision'),
  'ok', 'Nonokhero', repeat('c', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'ok', 'nOnOkhero', repeat('c', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'ok', 'Nonokhero', repeat('C', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'ok', 'Nonokhero', repeat('c', 64), 2::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'ok', 'Nonokhero', repeat('c', 64), 1::smallint, 'player-v2'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'ok', 'Nonok/hero', repeat('c', 64), 1::smallint, 'player-v1'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000002', '72000000-0000-0000-0000-000000000002',
  (select character_id from pg_temp.evidence_fixture where kind = 'nonok'),
  'ok', E'Nonok\\hero', repeat('c', 64), 1::smallint, 'player-v1'
);

reset role;
select pg_temp.assert_true(
  (select lifecycle = 'provisioning' from public.game_characters
   where id = (select character_id from pg_temp.evidence_fixture where kind = 'nonok'))
  and not exists (select 1 from private.game_character_legacy_identity_evidence
                  where correlation_id = '72000000-0000-0000-0000-000000000002'),
  'failed evidence finalization rolls back lifecycle and ledger atomically'
);

-- Positive claim: the same SHA is imported-file evidence and must match both the
-- imported row and its active challenge; it is never written as a provision request.
insert into public.game_characters (
  world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, imported_file_sha256
) values (
  'evidence-binding-world', 'Claimhero', 'Claimhero',
  substr(encode(public.digest(convert_to('Claimhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', repeat('d', 64)
);

insert into pg_temp.evidence_fixture(kind, character_id)
select 'claim', id from public.game_characters
where world_id = 'evidence-binding-world' and legacy_name_key = 'Claimhero';

set local role service_role;
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '71000000-0000-0000-0000-000000000001',
    '72000000-0000-0000-0000-000000000003', 'claim', now() + interval '10 minutes'
  )),
  'claim evidence fixture starts an onboarding intent'
);
select pg_temp.assert_true(
  (select character_id = (select character_id from pg_temp.evidence_fixture where kind = 'claim')
   from public.challenge_legacy_game_character_onboarding(
     'evidence-binding-world', 'Claimhero', repeat('d', 64),
     '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000003'
   )),
  'claim evidence requires an exact imported-file challenge first'
);
select pg_temp.assert_true(
  (select mode = 'claim' and lifecycle = 'active' and world_id = 'evidence-binding-world'
          and canonical_legacy_name = 'Claimhero' and legacy_shard = '9a'
          and player_file_sha256 = repeat('d', 64)
   from public.finalize_game_character_legacy_identity_evidence(
     '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000003',
     (select character_id from pg_temp.evidence_fixture where kind = 'claim'),
     'ok', 'Claimhero', repeat('d', 64), 1::smallint, 'player-v1', '9a'
   )),
  'ok claim evidence atomically moves ownership only for the imported fingerprint'
);
select pg_temp.assert_true(
  (select count(*) = 1 from public.finalize_game_character_legacy_identity_evidence(
    '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000003',
    (select character_id from pg_temp.evidence_fixture where kind = 'claim'),
    'ok', 'Claimhero', repeat('d', 64), 1::smallint, 'player-v1', '9a'
  )),
  'an exact claim evidence retry is idempotent'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000003',
  (select character_id from pg_temp.evidence_fixture where kind = 'claim'),
  'ok', 'Claimhero', repeat('e', 64), 1::smallint, 'player-v1'
);

-- The rolling-compatible overload derives a shard, but still succeeds only by
-- delegating through the full shard-aware verifier and its identity checks.
select pg_temp.assert_true(
  (select status = 'started' from public.begin_game_character_onboarding(
    '71000000-0000-0000-0000-000000000001',
    '72000000-0000-0000-0000-000000000004', 'provision', now() + interval '10 minutes'
  )),
  'eight-argument wrapper fixture starts an onboarding intent'
);
insert into pg_temp.evidence_fixture(kind, character_id)
select 'wrapper', character_id
from public.begin_game_character_provisioning(
  '71000000-0000-0000-0000-000000000001',
  '72000000-0000-0000-0000-000000000004', 'evidence-binding-world', 'wrapperhero'
);
select pg_temp.assert_true(
  (select count(*) = 1 from public.finalize_game_character_legacy_identity_evidence(
    '71000000-0000-0000-0000-000000000001',
    '72000000-0000-0000-0000-000000000004',
    (select character_id from pg_temp.evidence_fixture where kind = 'wrapper'),
    'ok', 'Wrapperhero', repeat('f', 64), 1::smallint, 'player-v1'
  )),
  'eight-argument wrapper succeeds through the shard-aware verifier'
);
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000004',
  (select character_id from pg_temp.evidence_fixture where kind = 'wrapper'),
  'ok', 'Otherhero', repeat('f', 64), 1::smallint, 'player-v1'
);
reset role;
select pg_temp.assert_true(
  (select mode = 'provision'
          and canonical_legacy_name = 'Wrapperhero'
          and legacy_shard = substr(
            encode(public.digest(convert_to('Wrapperhero', 'UTF8'), 'sha1'), 'hex'), 1, 2
          )
          and player_file_sha256 = repeat('f', 64)
   from private.game_character_legacy_identity_evidence
   where correlation_id = '72000000-0000-0000-0000-000000000004'),
  'wrapper records only the full verifier canonical identity tuple'
);
set local role service_role;

-- The ledger is not a blind idempotency key: an exact retry must reject after
-- the currently locked character has drifted away from its recorded world.
reset role;
update public.game_characters
   set world_id = 'evidence-binding-world-drift'
 where id = (select character_id from pg_temp.evidence_fixture where kind = 'claim');
set local role service_role;
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000003',
  (select character_id from pg_temp.evidence_fixture where kind = 'claim'),
  'ok', 'Claimhero', repeat('d', 64), 1::smallint, 'player-v1'
);
reset role;
select pg_temp.assert_true(
  (select imported_file_sha256 = repeat('d', 64) and lifecycle = 'active'
   from public.game_characters where id = (select character_id from pg_temp.evidence_fixture where kind = 'claim'))
  and (select count(*) = 1 from private.game_character_legacy_identity_evidence
       where correlation_id = '72000000-0000-0000-0000-000000000003'
         and mode = 'claim' and world_id = 'evidence-binding-world'
         and legacy_shard = '9a' and player_file_sha256 = repeat('d', 64)),
  'claim retry identity drift is rejected without changing its immutable ledger row'
);

-- NULL is drift too: ordinary SQL inequality is three-valued and must not let
-- an exact claim retry bypass the imported-file fingerprint check.
update public.game_characters
   set world_id = 'evidence-binding-world', imported_file_sha256 = null
 where id = (select character_id from pg_temp.evidence_fixture where kind = 'claim');
set local role service_role;
select pg_temp.expect_evidence_rejection(
  '71000000-0000-0000-0000-000000000001', '72000000-0000-0000-0000-000000000003',
  (select character_id from pg_temp.evidence_fixture where kind = 'claim'),
  'ok', 'Claimhero', repeat('d', 64), 1::smallint, 'player-v1'
);
reset role;
select pg_temp.assert_true(
  (select imported_file_sha256 is null and lifecycle = 'active'
   from public.game_characters where id = (select character_id from pg_temp.evidence_fixture where kind = 'claim'))
  and (select count(*) = 1 from private.game_character_legacy_identity_evidence
       where correlation_id = '72000000-0000-0000-0000-000000000003'
         and player_file_sha256 = repeat('d', 64)),
  'NULL imported-file drift rejects the retry without changing immutable claim evidence'
);

rollback;
