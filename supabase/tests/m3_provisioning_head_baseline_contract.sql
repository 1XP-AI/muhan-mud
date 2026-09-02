\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract for the provisioning-to-M3 baseline.
-- It intentionally fails through migration 120, then passes after migration
-- 130 and is safe to execute repeatedly in a rolled-back transaction.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'm3 provisioning head baseline contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_state(p_sqlstate text, p_sql text)
returns void language plpgsql as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then
      return;
    end if;
    raise;
  end;
  raise exception using errcode = 'P0002',
    message = format('m3 provisioning head baseline contract failed: expected %s, statement succeeded', p_sqlstate);
end;
$$;

do $$
declare
  v_actor_a constant uuid := 'd9300000-0000-0000-0000-000000000001';
  v_actor_b constant uuid := 'd9300000-0000-0000-0000-000000000002';
  v_actor_c constant uuid := 'd9300000-0000-0000-0000-000000000003';
  v_finalize_correlation constant uuid := 'e9300000-0000-0000-0000-000000000001';
  v_reconcile_correlation constant uuid := 'e9300000-0000-0000-0000-000000000002';
  v_conflict_correlation constant uuid := 'e9300000-0000-0000-0000-000000000003';
  v_advanced_correlation constant uuid := 'e9300000-0000-0000-0000-000000000004';
  v_finalize_id uuid;
  v_reconcile_id uuid;
  v_conflict_id uuid;
  v_advanced_id uuid;
  v_head_before text;
  v_request_before text;
  v_character_before text;
  v_intent_before text;
  v_route record;
begin
  insert into auth.users(id) values (v_actor_a), (v_actor_b), (v_actor_c);

  select character_id into v_finalize_id from public.begin_game_character_onboarding(
    v_actor_a, v_finalize_correlation, 'provision', clock_timestamp() + interval '10 minutes'
  ) as i join lateral public.begin_game_character_provisioning(
    v_actor_a, v_finalize_correlation, 'm3-provision-baseline', 'BaseFinal'
  ) as p on true;

  perform pg_temp.assert_true(
    (select count(*) = 1 from public.finalize_game_character_provisioning(
      v_actor_a, v_finalize_correlation, repeat('a', 64), 1::smallint
    )), 'normal finalize must return one row'
  );
  perform pg_temp.assert_true(
    (select h.head_state = 'existing' and h.head_sha256 = repeat('a', 64)
            and h.storage_format = 1 and h.revision = 0 and h.writer_epoch is null
       from private.game_character_legacy_heads h where h.character_id = v_finalize_id),
    'normal finalize must atomically seed the exact existing revision-zero baseline'
  );
  perform pg_temp.assert_true(
    (select imported_file_sha256 is null from public.game_characters where id = v_finalize_id),
    'provisioned C-save evidence must not be overloaded into imported_file_sha256'
  );
  v_head_before := (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_finalize_id);
  perform pg_temp.assert_true(
    (select count(*) = 1 from public.finalize_game_character_provisioning(
      v_actor_a, v_finalize_correlation, repeat('a', 64), 1::smallint
    )), 'same normal completion must remain idempotent'
  );
  perform pg_temp.assert_true(
    v_head_before = (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_finalize_id),
    'idempotent normal completion must not change the baseline'
  );

  select character_id into v_reconcile_id from public.begin_game_character_onboarding(
    v_actor_a, v_reconcile_correlation, 'provision', clock_timestamp() + interval '10 minutes'
  ) as i join lateral public.begin_game_character_provisioning(
    v_actor_a, v_reconcile_correlation, 'm3-provision-baseline', 'BaseRecon'
  ) as p on true;
  update private.game_character_onboarding_intents
     set expires_at = clock_timestamp() - interval '1 second'
   where correlation_id = v_reconcile_correlation;
  perform pg_temp.assert_true(
    (select count(*) = 1 from public.reconcile_game_character_provisioning(
      v_actor_a, v_reconcile_correlation, repeat('b', 64), 1::smallint
    )), 'reconcile must return one row after the crash-window expiry'
  );
  perform pg_temp.assert_true(
    (select h.head_state = 'existing' and h.head_sha256 = repeat('b', 64)
            and h.storage_format = 1 and h.revision = 0 and h.writer_epoch is null
       from private.game_character_legacy_heads h where h.character_id = v_reconcile_id),
    'reconcile must seed the same revision-zero baseline'
  );

  select character_id into v_conflict_id from public.begin_game_character_onboarding(
    v_actor_b, v_conflict_correlation, 'provision', clock_timestamp() + interval '10 minutes'
  ) as i join lateral public.begin_game_character_provisioning(
    v_actor_b, v_conflict_correlation, 'm3-provision-baseline', 'BaseConflict'
  ) as p on true;
  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision, writer_epoch
  ) values (v_conflict_id, 'existing', repeat('c', 64), 1, 0, null);
  v_head_before := (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_conflict_id);
  v_request_before := (select to_jsonb(r)::text from private.game_character_provisioning_requests r where r.character_id = v_conflict_id);
  v_character_before := (select to_jsonb(c)::text from public.game_characters c where c.id = v_conflict_id);
  v_intent_before := (select to_jsonb(i)::text from private.game_character_onboarding_intents i where i.correlation_id = v_conflict_correlation);
  perform pg_temp.expect_state('P0001', format(
    'select * from public.finalize_game_character_provisioning(%L::uuid, %L::uuid, %L, 1::smallint)',
    v_actor_b, v_conflict_correlation, repeat('d', 64)
  ));
  perform pg_temp.assert_true(
    v_head_before = (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_conflict_id)
    and v_request_before = (select to_jsonb(r)::text from private.game_character_provisioning_requests r where r.character_id = v_conflict_id)
    and v_character_before = (select to_jsonb(c)::text from public.game_characters c where c.id = v_conflict_id)
    and v_intent_before = (select to_jsonb(i)::text from private.game_character_onboarding_intents i where i.correlation_id = v_conflict_correlation),
    'conflicting revision-zero head must reject without partial head, request, character, or intent mutation'
  );

  select character_id into v_advanced_id from public.begin_game_character_onboarding(
    v_actor_c, v_advanced_correlation, 'provision', clock_timestamp() + interval '10 minutes'
  ) as i join lateral public.begin_game_character_provisioning(
    v_actor_c, v_advanced_correlation, 'm3-provision-baseline', 'BaseAdvance'
  ) as p on true;
  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision, writer_epoch
  ) values (v_advanced_id, 'existing', repeat('e', 64), 1, 4, 1);
  v_head_before := (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_advanced_id);
  perform pg_temp.assert_true(
    (select count(*) = 1 from public.finalize_game_character_provisioning(
      v_actor_c, v_advanced_correlation, repeat('f', 64), 1::smallint
    )), 'a valid already-advanced head must not block completing the reservation'
  );
  perform pg_temp.assert_true(
    v_head_before = (select to_jsonb(h)::text from private.game_character_legacy_heads h where h.character_id = v_advanced_id),
    'a valid already-advanced head must never be overwritten'
  );

  select * into v_route from private.resolve_game_character_writer_route_v3('m3-provision-baseline', 'Basefinal');
  perform pg_temp.assert_true(
    v_route.head_state = 'existing' and v_route.head_sha256 = repeat('a', 64)
    and v_route.head_revision = 0,
    'v3 must return the stored provisioned baseline at revision zero'
  );
end;
$$;

select pg_temp.assert_true(
  not has_table_privilege('anon', 'private.game_character_legacy_heads', 'select')
  and not has_table_privilege('authenticated', 'private.game_character_legacy_heads', 'select')
  and not has_table_privilege('service_role', 'private.game_character_legacy_heads', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_legacy_heads', 'select'),
  'browser roles and mud_writer must gain no direct legacy-head access'
);

rollback;
