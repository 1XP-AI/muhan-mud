\set ON_ERROR_STOP on

-- Run only against a disposable self-hosted Supabase database after migrations:
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f supabase/tests/game_identity_contract.sql
-- The transaction rolls every fixture row back.

begin;

create or replace function pg_temp.assert_true(condition boolean, message text)
returns void
language plpgsql
as $$
begin
  if condition is not true then
    raise exception 'game identity contract failed: %', message;
  end if;
end;
$$;

create or replace function pg_temp.expect_claim_rejection()
returns void
language plpgsql
as $$
begin
  perform public.claim_legacy_game_character(
    'contract-world', 'Contracthero',
    '10000000-0000-0000-0000-000000000002',
    '30000000-0000-0000-0000-000000000002'
  );
  raise exception using errcode = 'P0002', message = 'second actor unexpectedly claimed character';
exception
  when sqlstate 'P0001' then
    return;
end;
$$;

create or replace function pg_temp.expect_sensitive_audit_rejection()
returns void
language plpgsql
as $$
begin
  insert into public.game_identity_events (
    character_id, actor_user_id, event_type, correlation_id, details
  ) values (
    '20000000-0000-0000-0000-000000000001',
    '10000000-0000-0000-0000-000000000001',
    'contract_sensitive_payload_check',
    '30000000-0000-0000-0000-000000000003',
    jsonb_build_object('nested', jsonb_build_object('accessToken', 'must-not-store'))
  );
  raise exception using errcode = 'P0002', message = 'sensitive audit payload unexpectedly stored';
exception
  when check_violation then
    return;
end;
$$;

create or replace function pg_temp.expect_invalid_legacy_name_rejection()
returns void
language plpgsql
as $$
begin
  insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle
  ) values (
    '20000000-0000-0000-0000-000000000099',
    'contract-world-invalid-name',
    'bad/name',
    'bad/name',
    substr(encode(digest(convert_to('bad/name', 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'imported_unclaimed'
  );
  raise exception using errcode = 'P0002', message = 'invalid legacy name unexpectedly stored';
exception
  when check_violation then
    return;
end;
$$;

create or replace function pg_temp.expect_noncanonical_legacy_name_rejection()
returns void
language plpgsql
as $$
begin
  insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle
  ) values (
    '20000000-0000-0000-0000-000000000098',
    'contract-world-invalid-case',
    'hERO',
    'hERO',
    substr(encode(digest(convert_to('hERO', 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'imported_unclaimed'
  );
  raise exception using errcode = 'P0002', message = 'noncanonical legacy name unexpectedly stored';
exception
  when check_violation then
    return;
end;
$$;

create or replace function pg_temp.expect_reserved_legacy_name_rejection(
  p_name text,
  p_id uuid,
  p_world text
)
returns void
language plpgsql
as $$
begin
  insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle
  ) values (
    p_id,
    p_world,
    p_name,
    p_name,
    substr(encode(digest(convert_to(p_name, 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'imported_unclaimed'
  );
  raise exception using errcode = 'P0002', message = 'reserved legacy name unexpectedly stored';
exception
  when check_violation then
    return;
end;
$$;

create or replace function pg_temp.expect_session_rejection(
  p_actor uuid,
  p_session uuid,
  p_gateway text
)
returns void
language plpgsql
as $$
begin
  perform public.begin_game_character_session(
    p_actor,
    '20000000-0000-0000-0000-000000000001',
    p_session,
    p_gateway,
    now() + interval '1 minute'
  );
  raise exception using errcode = 'P0002', message = 'invalid session lease unexpectedly created';
exception
  when sqlstate 'P0001' then
    return;
end;
$$;

create or replace function pg_temp.expect_session_renewal_rejection(
  p_session uuid,
  p_gateway text
)
returns void
language plpgsql
as $$
begin
  perform public.renew_game_character_session(
    p_session,
    p_gateway,
    now() + interval '2 minutes'
  );
  raise exception using errcode = 'P0002', message = 'invalid session lease renewal unexpectedly succeeded';
exception
  when sqlstate 'P0001' then
    return;
end;
$$;

-- Standard GoTrue columns used by supported self-hosted Supabase releases.
-- `on conflict` also makes this safe if a fixture id survives a manually
-- interrupted earlier run.
insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values
  ('10000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
   'identity-contract-a@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now()),
  ('10000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
   'identity-contract-b@example.invalid', '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000', now(), '{}'::jsonb, '{}'::jsonb, now(), now())
on conflict (id) do nothing;

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle
) values (
  '20000000-0000-0000-0000-000000000001',
  'contract-world',
  'Contracthero',
  'Contracthero',
  substr(encode(digest(convert_to('Contracthero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed'
);

select pg_temp.assert_true(
  not has_table_privilege('anon', 'public.game_identity_events', 'select')
  and not has_table_privilege('anon', 'public.game_identity_events', 'insert')
  and not has_table_privilege('anon', 'public.game_identity_events', 'update')
  and not has_table_privilege('anon', 'public.game_identity_events', 'delete')
  and not has_table_privilege('authenticated', 'public.game_identity_events', 'select')
  and not has_table_privilege('authenticated', 'public.game_identity_events', 'insert')
  and not has_table_privilege('authenticated', 'public.game_identity_events', 'update')
  and not has_table_privilege('authenticated', 'public.game_identity_events', 'delete'),
  'browser role must not access audit events'
);
select pg_temp.assert_true(
  not has_table_privilege('anon', 'private.game_character_sessions', 'select')
  and not has_table_privilege('anon', 'private.game_character_sessions', 'insert')
  and not has_table_privilege('anon', 'private.game_character_sessions', 'update')
  and not has_table_privilege('anon', 'private.game_character_sessions', 'delete')
  and not has_table_privilege('authenticated', 'private.game_character_sessions', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_sessions', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_sessions', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_sessions', 'delete'),
  'browser role must not access session leases'
);
select pg_temp.assert_true(
  not has_table_privilege('anon', 'private.game_character_snapshots', 'select')
  and not has_table_privilege('anon', 'private.game_character_snapshots', 'insert')
  and not has_table_privilege('anon', 'private.game_character_snapshots', 'update')
  and not has_table_privilege('anon', 'private.game_character_snapshots', 'delete')
  and not has_table_privilege('authenticated', 'private.game_character_snapshots', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_snapshots', 'insert')
  and not has_table_privilege('authenticated', 'private.game_character_snapshots', 'update')
  and not has_table_privilege('authenticated', 'private.game_character_snapshots', 'delete'),
  'browser role must not access snapshots'
);
select pg_temp.assert_true(
  not has_table_privilege('anon', 'public.game_characters', 'select')
  and not has_table_privilege('anon', 'public.game_characters', 'insert')
  and not has_table_privilege('anon', 'public.game_characters', 'update')
  and not has_table_privilege('anon', 'public.game_characters', 'delete')
  and not has_table_privilege('authenticated', 'public.game_characters', 'insert')
  and not has_table_privilege('authenticated', 'public.game_characters', 'update')
  and not has_table_privilege('authenticated', 'public.game_characters', 'delete'),
  'browser role must not mutate character ownership or lifecycle'
);
select pg_temp.assert_true(
  not has_function_privilege('anon', 'public.claim_legacy_game_character(text,text,uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated', 'public.claim_legacy_game_character(text,text,uuid,uuid)', 'execute'),
  'browser roles must not call claim RPC'
);
select pg_temp.assert_true(
  not has_function_privilege('service_role', 'public.claim_legacy_game_character(text,text,uuid,uuid)', 'execute'),
  'service role must use the fingerprint-bound onboarding claim wrapper'
);
select pg_temp.assert_true(
  not has_function_privilege('authenticated', 'public.begin_game_character_session(uuid,uuid,uuid,text,timestamptz)', 'execute')
  and not has_function_privilege('authenticated', 'public.renew_game_character_session(uuid,text,timestamptz)', 'execute')
  and not has_function_privilege('authenticated', 'public.end_game_character_session(uuid,text)', 'execute'),
  'browser role must not call session lease RPCs'
);
select pg_temp.assert_true(
  has_function_privilege('service_role', 'public.begin_game_character_session(uuid,uuid,uuid,text,timestamptz)', 'execute')
  and has_function_privilege('service_role', 'public.renew_game_character_session(uuid,text,timestamptz)', 'execute')
  and has_function_privilege('service_role', 'public.end_game_character_session(uuid,text)', 'execute'),
  'service role must be able to call session lease RPCs'
);
select pg_temp.assert_true(
  not has_table_privilege('service_role', 'public.game_characters', 'select')
  and not has_table_privilege('service_role', 'public.game_characters', 'insert')
  and not has_table_privilege('service_role', 'public.game_characters', 'update')
  and not has_table_privilege('service_role', 'public.game_characters', 'delete')
  and not has_table_privilege('service_role', 'public.game_identity_events', 'select')
  and not has_table_privilege('service_role', 'public.game_identity_events', 'insert')
  and not has_table_privilege('service_role', 'public.game_identity_events', 'update')
  and not has_table_privilege('service_role', 'public.game_identity_events', 'delete')
  and not has_table_privilege('service_role', 'private.game_character_claim_requests', 'select')
  and not has_table_privilege('service_role', 'private.game_character_claim_requests', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_claim_requests', 'update')
  and not has_table_privilege('service_role', 'private.game_character_claim_requests', 'delete')
  and not has_table_privilege('service_role', 'private.game_character_sessions', 'select')
  and not has_table_privilege('service_role', 'private.game_character_sessions', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_sessions', 'update')
  and not has_table_privilege('service_role', 'private.game_character_sessions', 'delete')
  and not has_table_privilege('service_role', 'private.game_character_snapshots', 'select')
  and not has_table_privilege('service_role', 'private.game_character_snapshots', 'insert')
  and not has_table_privilege('service_role', 'private.game_character_snapshots', 'update')
  and not has_table_privilege('service_role', 'private.game_character_snapshots', 'delete')
  and not has_schema_privilege('service_role', 'private', 'usage'),
  'service role must use RPCs rather than direct mutation/private schema access'
);
select pg_temp.assert_true(
  not exists (
    select 1
    from pg_class sequence_relation
    join pg_namespace sequence_schema on sequence_schema.oid = sequence_relation.relnamespace
    where sequence_relation.relkind = 'S'
      and sequence_schema.nspname = 'public'
      and sequence_relation.relname = 'game_identity_events_id_seq'
      and (
        has_sequence_privilege('service_role', sequence_relation.oid, 'usage')
        or has_sequence_privilege('service_role', sequence_relation.oid, 'select')
        or has_sequence_privilege('service_role', sequence_relation.oid, 'update')
      )
  ),
  'service role must not hold direct privileges on the game identity sequence'
);
select pg_temp.expect_invalid_legacy_name_rejection();
do $$
begin
  insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle
  ) values (
    '20000000-0000-0000-0000-000000000095',
    'contract-world-invalid-backslash',
    E'Bad\\name', E'Bad\\name',
    substr(encode(digest(convert_to(E'Bad\\name', 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'imported_unclaimed'
  );
  raise exception 'backslash legacy name unexpectedly stored';
exception when check_violation then
  null;
end;
$$;
select pg_temp.expect_noncanonical_legacy_name_rejection();
select pg_temp.expect_reserved_legacy_name_rejection(
  '.', '20000000-0000-0000-0000-000000000097', 'contract-world-reserved-dot'
);
select pg_temp.expect_reserved_legacy_name_rejection(
  '..', '20000000-0000-0000-0000-000000000096', 'contract-world-reserved-dotdot'
);
select pg_temp.assert_true(
  private.game_identity_canonical_legacy_name('hERO') = 'Hero'
  and private.game_identity_canonical_legacy_name('타BOT') = '타bot',
  'database canonicalizer must match C lowercize(name, 1)'
);

set local role authenticated;
select set_config('request.jwt.claim.sub', '10000000-0000-0000-0000-000000000001', true);
select pg_temp.assert_true(
  (select count(*) from public.game_characters where world_id = 'contract-world') = 0,
  'unclaimed character must not be visible to browser'
);
reset role;

-- The unexposed primitive remains covered as the migration owner; runtime
-- service_role access is tested above and must remain revoked.
select pg_temp.assert_true(
  (select lifecycle = 'active' and owner_user_id = '10000000-0000-0000-0000-000000000001'::uuid
   from public.claim_legacy_game_character(
     'contract-world', 'Contracthero',
     '10000000-0000-0000-0000-000000000001',
     '30000000-0000-0000-0000-000000000001'
   )),
  'first verified claim must activate exactly the requested character'
);
select pg_temp.assert_true(
  (select count(*) from public.claim_legacy_game_character(
    'contract-world', 'Contracthero',
    '10000000-0000-0000-0000-000000000001',
    '30000000-0000-0000-0000-000000000001'
  )) = 1,
  'same correlation must be idempotent'
);
select pg_temp.expect_claim_rejection();
reset role;

select pg_temp.assert_true(
  (select count(*) from public.game_identity_events
   where correlation_id = '30000000-0000-0000-0000-000000000001') = 1,
  'idempotent retry must not duplicate claim audit'
);
select pg_temp.expect_sensitive_audit_rejection();

set local role authenticated;
select set_config('request.jwt.claim.sub', '10000000-0000-0000-0000-000000000001', true);
select pg_temp.assert_true(
  (select count(*) from public.game_characters
   where world_id = 'contract-world' and legacy_name_key = 'Contracthero') = 1,
  'owner may select active character'
);
reset role;

set local role authenticated;
select set_config('request.jwt.claim.sub', '10000000-0000-0000-0000-000000000002', true);
select pg_temp.assert_true(
  (select count(*) from public.game_characters
   where world_id = 'contract-world' and legacy_name_key = 'Contracthero') = 0,
  'different browser user must not select another owner active character'
);
reset role;

set local role service_role;
select pg_temp.assert_true(
  (select session_id = '40000000-0000-0000-0000-000000000001'::uuid
          and owner_user_id = '10000000-0000-0000-0000-000000000001'::uuid
          and lifecycle = 'active'
          and legacy_name_key = 'Contracthero'
   from public.begin_game_character_session(
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000001',
     '40000000-0000-0000-0000-000000000001',
     'gateway-contract',
     now() + interval '1 minute'
   )),
  'owner may acquire active character session lease'
);
select pg_temp.assert_true(
  (select count(*) from public.begin_game_character_session(
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    '40000000-0000-0000-0000-000000000001',
    'gateway-contract',
    now() + interval '1 minute'
  )) = 1,
  'same session id must be idempotent'
);
select pg_temp.assert_true(
  (select session_id = '40000000-0000-0000-0000-000000000001'::uuid
          and character_id = '20000000-0000-0000-0000-000000000001'::uuid
          and owner_user_id = '10000000-0000-0000-0000-000000000001'::uuid
          and lifecycle = 'active'
          and legacy_name_key = 'Contracthero'
          and expires_at = now() + interval '2 minutes'
   from public.renew_game_character_session(
     '40000000-0000-0000-0000-000000000001',
     'gateway-contract',
     now() + interval '2 minutes'
   )),
  'same live session and gateway may renew its bounded lease'
);
select pg_temp.expect_session_renewal_rejection(
  '40000000-0000-0000-0000-000000000001',
  'different-gateway-contract'
);
select pg_temp.expect_session_rejection(
  '10000000-0000-0000-0000-000000000002',
  '40000000-0000-0000-0000-000000000002',
  'gateway-contract'
);
select pg_temp.expect_session_rejection(
  '10000000-0000-0000-0000-000000000001',
  '40000000-0000-0000-0000-000000000002',
  'gateway-contract'
);
select pg_temp.expect_session_rejection(
  '10000000-0000-0000-0000-000000000001',
  '40000000-0000-0000-0000-000000000001',
  'different-gateway-contract'
);
select pg_temp.assert_true(
  not public.end_game_character_session(
    '40000000-0000-0000-0000-000000000001', 'different-gateway-contract'
  ),
  'different gateway must not release a live lease'
);
select pg_temp.assert_true(
  public.end_game_character_session(
    '40000000-0000-0000-0000-000000000001', 'gateway-contract'
  ),
  'matching session id must release its lease'
);
select pg_temp.assert_true(
  not public.end_game_character_session(
    '40000000-0000-0000-0000-000000000001', 'gateway-contract'
  ),
  'ending an already released session must be idempotent false'
);
reset role;

insert into private.game_character_sessions (
  character_id, session_id, actor_user_id, gateway_instance_id, expires_at, created_at
) values (
  '20000000-0000-0000-0000-000000000001',
  '40000000-0000-0000-0000-000000000003',
  '10000000-0000-0000-0000-000000000001',
  'expired-gateway-contract',
  now() - interval '1 minute',
  now() - interval '2 minutes'
);

set local role service_role;
select pg_temp.expect_session_renewal_rejection(
  '40000000-0000-0000-0000-000000000003',
  'expired-gateway-contract'
);
select pg_temp.assert_true(
  (select session_id = '40000000-0000-0000-0000-000000000004'::uuid
   from public.begin_game_character_session(
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000001',
     '40000000-0000-0000-0000-000000000004',
     'gateway-contract',
     now() + interval '1 minute'
   )),
  'only an expired lease may be taken over'
);
select pg_temp.assert_true(
  not public.end_game_character_session(
    '40000000-0000-0000-0000-000000000003', 'expired-gateway-contract'
  ),
  'stale session id must not release a replacement lease'
);
select pg_temp.assert_true(
  (select count(*) from public.begin_game_character_session(
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    '40000000-0000-0000-0000-000000000004',
    'gateway-contract',
    now() + interval '1 minute'
  )) = 1,
  'replacement lease must survive a stale end request'
);
select pg_temp.assert_true(
  public.end_game_character_session(
    '40000000-0000-0000-0000-000000000004', 'gateway-contract'
  ),
  'matching replacement session id must release its lease'
);
reset role;

rollback;
