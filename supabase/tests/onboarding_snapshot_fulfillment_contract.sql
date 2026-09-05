\set ON_ERROR_STOP on

-- RED: run after migration 20260926000000 only; the prior writer has no
-- service-only immutable command binding.  GREEN: apply 20260927000000 twice,
-- then rerun this disposable PG17 contract.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'onboarding snapshot fulfillment contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_rejection(p_sql text)
returns void language plpgsql as $$
begin
  execute p_sql;
  raise exception using errcode = 'P0002', message = 'fulfillment unexpectedly succeeded';
exception
  when sqlstate 'P0001' or sqlstate '22023' then return;
end;
$$;

-- The checked-in canonical C PlayerSnapshotV1 fixture is deliberately
-- supplied by the PG17 harness, exactly as the artifact contract does.
\if :{?pva_payload}
\else
  \echo pva_payload is required
  do $$ begin raise exception 'pva_payload is required' using errcode = '22023'; end $$;
\endif

select pg_temp.assert_true(
  to_regclass('private.game_character_onboarding_snapshot_fulfillments') is not null
  and to_regclass('private.game_character_onboarding_snapshot_command_bindings') is not null
  and to_regprocedure('private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)') is not null
  and to_regprocedure('private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid,uuid)') is null
  and to_regprocedure('public.register_game_character_onboarding_snapshot_command_binding(uuid,uuid,uuid,text,uuid)') is not null
  and (select relrowsecurity from pg_class
         where oid = 'private.game_character_onboarding_snapshot_fulfillments'::regclass)
  and (select count(*) = 1 from pg_constraint
        where conrelid = 'private.game_character_onboarding_snapshot_fulfillments'::regclass
          and contype = 'p')
  and (select count(*) = 1 from pg_trigger
        where tgrelid = 'private.game_character_onboarding_snapshot_fulfillments'::regclass
          and tgname = 'game_character_onboarding_snapshot_fulfillments_immutable'
          and not tgisinternal)
  and (select count(*) = 1 from pg_trigger
        where tgrelid = 'private.game_character_onboarding_snapshot_command_bindings'::regclass
          and tgname = 'game_character_onboarding_snapshot_command_bindings_immutable'
          and not tgisinternal)
  and (select count(*) = 1 from pg_constraint
        where conrelid = 'private.game_character_onboarding_snapshot_command_bindings'::regclass
          and contype = 'p')
  and (select count(*) = 1 from pg_constraint
        where conrelid = 'private.game_character_onboarding_snapshot_eligibility_outbox'::regclass
          and conname = 'game_character_onboarding_snapshot_eligibility_outbox_status')
  and (select count(*) = 1 from information_schema.columns
        where table_schema = 'private'
          and table_name = 'game_character_onboarding_snapshot_eligibility_outbox'
          and column_name = 'fulfilled_at'),
  'twice-applied migration leaves immutable command-keyed bindings and fulfillment receipts with one fulfilled outbox shape'
);

select pg_temp.assert_true(
  (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p
    where p.oid = 'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure)
  and has_function_privilege('mud_writer',
        'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)', 'execute')
  and not has_function_privilege('mud_writer_login',
        'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)', 'execute')
  and not has_function_privilege('service_role',
        'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)', 'execute')
  and not has_function_privilege('anon',
        'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)', 'execute')
  and not has_function_privilege('authenticated',
        'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)', 'execute')
  and not has_table_privilege('mud_writer',
        'private.game_character_onboarding_snapshot_fulfillments', 'select')
  and not has_table_privilege('service_role',
        'private.game_character_onboarding_snapshot_fulfillments', 'insert')
  and (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
         from pg_proc p
        where p.oid = 'public.register_game_character_onboarding_snapshot_command_binding(uuid,uuid,uuid,text,uuid)'::regprocedure)
  and has_function_privilege('service_role',
        'public.register_game_character_onboarding_snapshot_command_binding(uuid,uuid,uuid,text,uuid)', 'execute')
  and not has_function_privilege('anon',
        'public.register_game_character_onboarding_snapshot_command_binding(uuid,uuid,uuid,text,uuid)', 'execute')
  and not has_function_privilege('authenticated',
        'public.register_game_character_onboarding_snapshot_command_binding(uuid,uuid,uuid,text,uuid)', 'execute')
  and not has_table_privilege('service_role',
        'private.game_character_onboarding_snapshot_command_bindings', 'insert')
  and not has_table_privilege('mud_writer',
        'private.game_character_onboarding_snapshot_command_bindings', 'select'),
  'only service_role may register a private immutable command binding and only mud_writer may fulfill it'
);

select pg_temp.assert_true(
  position(
    'select * into v_intent from private.game_character_onboarding_intents where correlation_id = v_correlation_id for share;'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  )
  < position(
    'select * into v_handoff from private.game_character_onboarding_handoffs where correlation_id = v_correlation_id for share;'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  )
  and position(
    'select * into v_handoff from private.game_character_onboarding_handoffs where correlation_id = v_correlation_id for share;'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  )
  < position(
    'select * into v_character from public.game_characters where id = p_character_id for share;'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  )
  and position(
    'select * into v_character from public.game_characters where id = p_character_id for share;'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  )
  < position(
    'select * into v_outbox from private.game_character_onboarding_snapshot_eligibility_outbox where correlation_id = v_correlation_id for update;'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  ),
  'fulfillment acquires overlapping intent, handoff, character, and outbox row locks in activation order'
);

select pg_temp.assert_true(
  position(
    'from private.game_character_onboarding_snapshot_command_bindings where command_id = p_artifact_command_id and character_id = p_character_id'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  ) > 0
  and position(
    'v_receipt.acknowledged_at <= v_binding.bound_at'
    in regexp_replace(pg_get_functiondef(
      'private.fulfill_game_character_onboarding_snapshot_eligibility(uuid,uuid)'::regprocedure
    ), '[[:space:]]+', ' ', 'g')
  ) > 0,
  'fulfillment resolves only the supplied command-keyed binding and requires a receipt acknowledged strictly after it'
);

insert into auth.users (
  id, aud, role, email, encrypted_password, email_confirmed_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at
) values (
  'a2400000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
  'snapshot-fulfillment@example.invalid',
  '$2a$10$contractfixtureonlynotarealhash00000000000000000000000000000',
  now(), '{}'::jsonb, '{}'::jsonb, now(), now()
) on conflict (id) do nothing;

insert into public.game_characters(
  id, world_id, legacy_name, legacy_name_key, legacy_shard,
  owner_user_id, lifecycle, storage_format, claimed_at
) values (
  'b2400000-0000-0000-0000-000000000001', 'snapshot-fulfillment',
  'Fulfillhero', 'Fulfillhero',
  substr(encode(public.digest(convert_to('Fulfillhero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'a2400000-0000-0000-0000-000000000001', 'handoff_pending', 1, clock_timestamp()
);

insert into private.game_character_onboarding_intents(
  correlation_id, actor_user_id, mode, status, expires_at, completed_at
) values (
  'c2400000-0000-0000-0000-000000000001',
  'a2400000-0000-0000-0000-000000000001', 'claim', 'finalized',
  clock_timestamp() + interval '10 minutes', clock_timestamp()
);
insert into private.game_character_onboarding_handoffs(
  correlation_id, actor_user_id, character_id, mode
) values (
  'c2400000-0000-0000-0000-000000000001',
  'a2400000-0000-0000-0000-000000000001',
  'b2400000-0000-0000-0000-000000000001', 'claim'
);

create or replace function pg_temp.add_artifact(
  p_command_id uuid, p_revision bigint, p_acknowledged_at timestamptz,
  p_artifact_world_id text default 'snapshot-fulfillment',
  p_artifact_name_key text default 'Fulfillhero',
  p_artifact_storage_format smallint default 1
)
returns void language plpgsql as $$
begin
  insert into private.game_character_shadow_receipts(
    character_id, command_id, world_id, legacy_name_key, request_sha256,
    writer_instance_id, writer_epoch, writer_revision, expected_state,
    expected_sha256, post_sha256, storage_format, acknowledged_at
  ) values (
    'b2400000-0000-0000-0000-000000000001', p_command_id,
    'snapshot-fulfillment', 'Fulfillhero', repeat('a', 64),
    'd2400000-0000-0000-0000-000000000001', 1, p_revision, 'existing',
    repeat('b', 64), repeat('c', 64), 1, p_acknowledged_at
  );
  insert into private.game_character_m4_file_snapshot_manifests(
    character_id, command_id, world_id, legacy_name_key,
    receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision,
    file_post_sha256, storage_format, receipt_acknowledged_at,
    snapshot_format, snapshot_sha256, snapshot_octets
  ) values (
    'b2400000-0000-0000-0000-000000000001', p_command_id,
    'snapshot-fulfillment', 'Fulfillhero', repeat('a', 64),
    'd2400000-0000-0000-0000-000000000001', 1, p_revision,
    repeat('c', 64), 1, p_acknowledged_at,
    'legacy-file-manifest-v1', repeat('c', 64), 1
  );
  insert into private.game_character_player_snapshot_v1_artifacts(
    character_id, command_id, world_id, legacy_name_key,
    receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision,
    source_post_sha256, source_octets, storage_format, receipt_acknowledged_at,
    snapshot_format, snapshot_sha256, snapshot_octets, payload
  ) values (
    'b2400000-0000-0000-0000-000000000001', p_command_id,
    p_artifact_world_id, p_artifact_name_key, repeat('a', 64),
    'd2400000-0000-0000-0000-000000000001', 1, p_revision,
    repeat('c', 64), 1, p_artifact_storage_format, p_acknowledged_at,
    'player-snapshot-v1', encode(public.digest(:pva_payload, 'sha256'), 'hex'),
    octet_length(:pva_payload), :pva_payload
  );
end;
$$;

-- A complete immutable artifact/receipt pair before activation has no active
-- exact relation, so it is a normal terminal delivery state.
select pg_temp.add_artifact(
  'e2400000-0000-0000-0000-000000000001', 1,
  clock_timestamp()
);
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(
  (select outcome = 'NOT_ELIGIBLE'
     from private.fulfill_game_character_onboarding_snapshot_eligibility(
       'b2400000-0000-0000-0000-000000000001',
       'e2400000-0000-0000-0000-000000000001')),
  'no active exact handoff is a NOT_ELIGIBLE terminal outcome'
);
reset role;
reset session authorization;
select pg_temp.assert_true(
  (select count(*) = 0 from private.game_character_onboarding_snapshot_eligibility_outbox)
  and (select count(*) = 0 from private.game_character_onboarding_snapshot_fulfillments),
  'preactivation NOT_ELIGIBLE leaves no outbox or partial fulfillment'
);

-- RED-first claim-mode handoff: activation creates the eligibility, exact
-- fulfillment closes it, and the same exact activation replay remains valid.
set local role service_role;
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
     from public.activate_game_character_onboarding_handoff(
       'a2400000-0000-0000-0000-000000000001',
       'c2400000-0000-0000-0000-000000000001',
       'b2400000-0000-0000-0000-000000000001', 'claim')),
  'claim activation creates one exact pending eligibility handoff'
);
reset role;

-- Artifact evidence cannot nominate itself: only a previously bound command
-- is eligible, even when its receipt otherwise has matching facts.
select pg_temp.add_artifact(
  'e2400000-0000-0000-0000-000000000002', 2,
  (select enqueued_at + interval '1 microsecond'
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = 'c2400000-0000-0000-0000-000000000001')
);
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(
  (select outcome = 'NOT_ELIGIBLE'
     from private.fulfill_game_character_onboarding_snapshot_eligibility(
       'b2400000-0000-0000-0000-000000000001',
       'e2400000-0000-0000-0000-000000000002')),
  'an unbound artifact command is a NOT_ELIGIBLE terminal outcome'
);
reset role;
reset session authorization;
select pg_temp.assert_true(
  (select status = 'pending' and fulfilled_at is null
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = 'c2400000-0000-0000-0000-000000000001')
  and (select count(*) = 0 from private.game_character_onboarding_snapshot_fulfillments),
  'unbound command rejection rolls back with no receipt or outbox transition'
);

-- Gateway registers the exact command after activation and before writer
-- acknowledgement.  Exact replay is allowed; every tuple substitution fails
-- closed and cannot replace the command key.
set local role service_role;
select pg_temp.assert_true(
  (select outcome = 'BOUND'
     from public.register_game_character_onboarding_snapshot_command_binding(
       'a2400000-0000-0000-0000-000000000001',
       'c2400000-0000-0000-0000-000000000001',
       'b2400000-0000-0000-0000-000000000001', 'claim',
       'e2400000-0000-0000-0000-000000000003')),
  'the service binds one exact activated onboarding tuple to one command id'
);
select pg_temp.assert_true(
  (select outcome = 'EXACT_RETRY'
     from public.register_game_character_onboarding_snapshot_command_binding(
       'a2400000-0000-0000-0000-000000000001',
       'c2400000-0000-0000-0000-000000000001',
       'b2400000-0000-0000-0000-000000000001', 'claim',
       'e2400000-0000-0000-0000-000000000003')),
  'the exact command binding registration replay is idempotent'
);
select pg_temp.expect_rejection(
  'select * from public.register_game_character_onboarding_snapshot_command_binding(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000001'', ''claim'', ''e2400000-0000-0000-0000-000000000004'')'
);
select pg_temp.expect_rejection(
  'select * from public.register_game_character_onboarding_snapshot_command_binding(''a2400000-0000-0000-0000-000000000099'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000001'', ''claim'', ''e2400000-0000-0000-0000-000000000003'')'
);
select pg_temp.expect_rejection(
  'select * from public.register_game_character_onboarding_snapshot_command_binding(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000099'', ''b2400000-0000-0000-0000-000000000001'', ''claim'', ''e2400000-0000-0000-0000-000000000003'')'
);
select pg_temp.expect_rejection(
  'select * from public.register_game_character_onboarding_snapshot_command_binding(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000099'', ''claim'', ''e2400000-0000-0000-0000-000000000003'')'
);
select pg_temp.expect_rejection(
  'select * from public.register_game_character_onboarding_snapshot_command_binding(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000001'', ''provision'', ''e2400000-0000-0000-0000-000000000003'')'
);
reset role;
select pg_temp.expect_rejection(
  'delete from private.game_character_onboarding_snapshot_command_bindings where command_id = ''e2400000-0000-0000-0000-000000000003'''
);

-- Multiple post-binding artifacts now exist for the character, but only the
-- registered command can fulfill.  The receipt for that command is created
-- after bound_at by construction.
select pg_temp.add_artifact(
  'e2400000-0000-0000-0000-000000000003', 3,
  clock_timestamp()
);
select pg_temp.add_artifact(
  'e2400000-0000-0000-0000-000000000004', 4,
  clock_timestamp(),
  'wrong-world'
);
set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(
  (select outcome = 'NOT_ELIGIBLE'
     from private.fulfill_game_character_onboarding_snapshot_eligibility(
       'b2400000-0000-0000-0000-000000000001',
       'e2400000-0000-0000-0000-000000000004')),
  'an unbound command cannot fulfill even when its artifact is supplied'
);
select pg_temp.assert_true(
  (select outcome = 'FULFILLED'
     from private.fulfill_game_character_onboarding_snapshot_eligibility(
       'b2400000-0000-0000-0000-000000000001',
       'e2400000-0000-0000-0000-000000000003')),
  'the writer fulfills only the exact service-bound command after its receipt acknowledgement'
);
select pg_temp.assert_true(
  (select outcome = 'EXACT_RETRY'
     from private.fulfill_game_character_onboarding_snapshot_eligibility(
       'b2400000-0000-0000-0000-000000000001',
       'e2400000-0000-0000-0000-000000000003')),
  'duplicate delivery is an exact idempotent retry'
);
select pg_temp.add_artifact(
  'e2400000-0000-0000-0000-000000000005', 5,
  (select enqueued_at + interval '3 microseconds'
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = 'c2400000-0000-0000-0000-000000000001')
);
select pg_temp.assert_true(
  (select outcome = 'NOT_ELIGIBLE'
     from private.fulfill_game_character_onboarding_snapshot_eligibility(
       'b2400000-0000-0000-0000-000000000001',
       'e2400000-0000-0000-0000-000000000005')),
  'a later unbound artifact cannot substitute for the immutable command binding'
);
reset role;
reset session authorization;

set local role service_role;
select pg_temp.assert_true(
  (select lifecycle = 'active' and onboarding_status = 'finalized'
     from public.activate_game_character_onboarding_handoff(
       'a2400000-0000-0000-0000-000000000001',
       'c2400000-0000-0000-0000-000000000001',
       'b2400000-0000-0000-0000-000000000001', 'claim')),
  'a valid exact activation callback replay remains idempotent after its outbox is fulfilled'
);
reset role;

select pg_temp.assert_true(
  (select status = 'fulfilled' and fulfilled_at is not null
     from private.game_character_onboarding_snapshot_eligibility_outbox
    where correlation_id = 'c2400000-0000-0000-0000-000000000001')
  and (select count(*) = 1
         from private.game_character_onboarding_snapshot_fulfillments
        where correlation_id = 'c2400000-0000-0000-0000-000000000001'
          and actor_user_id = 'a2400000-0000-0000-0000-000000000001'
          and character_id = 'b2400000-0000-0000-0000-000000000001'
          and mode = 'claim'
          and world_id = 'snapshot-fulfillment'
          and legacy_name_key = 'Fulfillhero'
          and storage_format = 1
          and artifact_command_id = 'e2400000-0000-0000-0000-000000000003'),
  'exact fulfillment records the one command-bound immutable receipt and closes only that outbox row'
);

select pg_temp.assert_true(
  (select command_id = 'e2400000-0000-0000-0000-000000000003'::uuid
              and correlation_id = 'c2400000-0000-0000-0000-000000000001'::uuid
              and actor_user_id = 'a2400000-0000-0000-0000-000000000001'::uuid
              and character_id = 'b2400000-0000-0000-0000-000000000001'::uuid
              and mode = 'claim'
              and bound_at < (select receipt_acknowledged_at
                                from private.game_character_onboarding_snapshot_fulfillments
                               where correlation_id = 'c2400000-0000-0000-0000-000000000001')
         from private.game_character_onboarding_snapshot_command_bindings
        where command_id = 'e2400000-0000-0000-0000-000000000003'),
  'the immutable command-keyed binding proves the exact tuple and predates the acknowledged receipt'
);

-- After fulfillment, every substituted activation tuple must fail and leave
-- the immutable receipt-bound terminal state untouched.
set local role service_role;
select pg_temp.expect_rejection(
  'select * from public.activate_game_character_onboarding_handoff(''a2400000-0000-0000-0000-000000000099'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000001'', ''claim'')'
);
select pg_temp.expect_rejection(
  'select * from public.activate_game_character_onboarding_handoff(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000099'', ''b2400000-0000-0000-0000-000000000001'', ''claim'')'
);
select pg_temp.expect_rejection(
  'select * from public.activate_game_character_onboarding_handoff(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000099'', ''claim'')'
);
select pg_temp.expect_rejection(
  'select * from public.activate_game_character_onboarding_handoff(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000001'', ''provision'')'
);
reset role;
select pg_temp.assert_true(
  (select count(*) = 1
     from private.game_character_onboarding_snapshot_fulfillments
    where correlation_id = 'c2400000-0000-0000-0000-000000000001'
      and artifact_command_id = 'e2400000-0000-0000-0000-000000000003'),
  'tuple substitution cannot overwrite the first immutable claim fulfillment'
);

-- A fulfilled outbox without its correlation-keyed immutable receipt is
-- corruption, not a normal replay state, so activation must fail closed.
set local session_replication_role = replica;
delete from private.game_character_onboarding_snapshot_fulfillments
 where correlation_id = 'c2400000-0000-0000-0000-000000000001';
set local session_replication_role = origin;
set local role service_role;
select pg_temp.expect_rejection(
  'select * from public.activate_game_character_onboarding_handoff(''a2400000-0000-0000-0000-000000000001'', ''c2400000-0000-0000-0000-000000000001'', ''b2400000-0000-0000-0000-000000000001'', ''claim'')'
);
reset role;

rollback;
