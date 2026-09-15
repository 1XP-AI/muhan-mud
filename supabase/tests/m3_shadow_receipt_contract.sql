\set ON_ERROR_STOP on

-- Disposable 090 contract. Apply 020..080 first, then this file after the
-- 090 migration. It deliberately contains no player bytes or credentials.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'm3 shadow contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_state(p_sqlstate text, p_sql text)
returns void language plpgsql as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then return; end if;
    raise;
  end;
  raise exception using errcode = 'P0002',
    message = format('m3 shadow contract failed: expected %s, statement succeeded', p_sqlstate);
end;
$$;

-- Prove the helper cannot mistake its own success sentinel for a target error.
do $$
begin
  begin
    perform pg_temp.expect_state('P0002', 'select 1');
    raise exception using errcode = 'P0003',
      message = 'm3 shadow contract failed: expect_state accepted a successful statement';
  exception when sqlstate 'P0002' then
    null;
  end;
end;
$$;

do $$
declare
  v_world constant text := 'm3-contract';
  v_character constant uuid := '92000000-0000-0000-0000-000000000001';
  v_other_character constant uuid := '92000000-0000-0000-0000-000000000002';
  v_a constant uuid := '94000000-0000-0000-0000-000000000001';
  v_b constant uuid := '94000000-0000-0000-0000-000000000002';
  v_command_1 constant uuid := '93000000-0000-0000-0000-000000000001';
  v_command_2 constant uuid := '93000000-0000-0000-0000-000000000002';
  v_command_3 constant uuid := '93000000-0000-0000-0000-000000000003';
  v_post_1 constant text := 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
  v_post_2 constant text := 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
  v_post_3 constant text := 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc';
  v_imported_hash constant text := 'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee';
  v_request_1 text;
  v_request_2 text;
  v_request_3 text;
  v_conflict_request text;
  v_other_shard text;
  v_epoch bigint;
  v_snapshot text;
  v_receipt_head_snapshot text;
begin
  insert into public.game_characters(
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format, imported_file_sha256
  ) values
    (v_character, v_world, 'M3hero', 'M3hero', '11', 'imported_unclaimed', 1, v_imported_hash),
    (v_other_character, v_world, 'M3other', 'M3other',
     substr(encode(public.digest(convert_to('M3other', 'UTF8'), 'sha1'), 'hex'), 1, 2),
     'suspended', 1, null);

  perform pg_temp.expect_state('22023',
    'select private.m3_shadow_valid_world(null::text)');
  perform pg_temp.expect_state('23514', format(
    $query$insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values(%L::uuid,'existing',null,1,0,null)$query$,
    v_other_character
  ));
  perform pg_temp.expect_state('23514', format(
    $query$insert into private.game_character_shadow_receipts(character_id,command_id,world_id,legacy_name_key,request_sha256,writer_instance_id,writer_epoch,writer_revision,expected_state,expected_sha256,post_sha256,storage_format) values(%L::uuid,%L::uuid,%L,%L,%L,%L::uuid,1,1,'existing',null,%L,1)$query$,
    v_other_character, v_command_1, v_world, 'M3other', repeat('a', 64), v_a, v_post_1
  ));

  select writer_epoch into v_epoch
    from private.acquire_game_world_writer_epoch(v_world, v_a, clock_timestamp() + interval '3 minutes');
  perform pg_temp.assert_true(v_epoch = 1, 'first writer epoch must be one');
  update private.game_character_writer_epochs
     set issued_at = clock_timestamp() - interval '2 seconds',
         expires_at = clock_timestamp() - interval '1 second'
   where world_id = v_world;
  perform private.renew_game_world_writer_epoch(v_world, v_a, 1::bigint, clock_timestamp() + interval '3 minutes');
  perform pg_temp.assert_true(
    (select writer_epoch = 1 and writer_instance_id = v_a and sealed_at is null
       from private.game_character_writer_epochs where world_id = v_world),
    'same persisted writer must renew an expired unfenced epoch'
  );

  v_request_1 := private.game_character_shadow_request_sha256(
    v_world, v_character, 'M3hero', '11', v_command_1, v_a, 1::bigint, 1::bigint,
    'absent', null, v_post_1, 1::smallint
  );

  -- A file-authoritative writer must seed an explicit absent head outside this
  -- RPC. DB cannot infer that a legacy file is absent from an identity row.
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,1::bigint,%L,null,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_1, v_a, v_request_1,
    'absent', v_post_1
  ));
  perform pg_temp.assert_true(
    (select count(*) = 0 from private.game_character_legacy_heads where character_id = v_character)
    and (select count(*) = 0 from private.game_character_shadow_receipts where character_id = v_character),
    'missing head plus absent request, even with imported hash, must not bootstrap or mutate'
  );
  -- Test-owner fixture only. Production absent-head seeding remains a later
  -- explicitly authorized bootstrap protocol, not a receipt side effect.
  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision, writer_epoch
  ) values (v_character, 'absent', null, 1, 0, null);

  perform pg_temp.assert_true(
    v_request_1 = 'eb83eb12f5875dad83f5f91e128c4874b2673b4089deff4967c58898d9c69171',
    'canonical request digest must match fixed LF/UTF-8 golden bytes'
  );

  perform private.record_legacy_published_receipt(
    v_world, 'M3hero', v_character, v_command_1, v_a, v_request_1, 1::bigint, 1::bigint,
    'absent', null, v_post_1, 1::smallint
  );
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_1) || '|' ||
           (select updated_at::text from private.game_character_legacy_heads
             where character_id = v_character)
  );
  perform private.record_legacy_published_receipt(
    v_world, 'M3hero', v_character, v_command_1, v_a, v_request_1, 1::bigint, 1::bigint,
    'absent', null, v_post_1, 1::smallint
  );
  perform pg_temp.assert_true(
    (select count(*) = 1 from private.game_character_shadow_receipts
      where character_id = v_character and command_id = v_command_1),
    'same receipt retry must be read-only and unique'
  );
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_1) || '|' ||
             (select updated_at::text from private.game_character_legacy_heads
               where character_id = v_character)
    ),
    'same active writer exact receipt retry must not mutate receipt or head timestamps'
  );
  v_conflict_request := private.game_character_shadow_request_sha256(
    v_world, v_character, 'M3hero', '11', v_command_1, v_a, 1::bigint, 1::bigint,
    'absent', null, v_post_2, 1::smallint
  );
  v_receipt_head_snapshot := (
    select count(*)::text || '|' || coalesce(string_agg(command_id::text, ',' order by command_id::text), '-') || '|' ||
           (select head_state || '|' || head_sha256 || '|' || revision::text
              from private.game_character_legacy_heads where character_id = v_character)
      from private.game_character_shadow_receipts where character_id = v_character
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,1::bigint,%L,null,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_1, v_a, v_conflict_request,
    'absent', v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select count(*)::text || '|' || coalesce(string_agg(command_id::text, ',' order by command_id::text), '-') || '|' ||
             (select head_state || '|' || head_sha256 || '|' || revision::text
                from private.game_character_legacy_heads where character_id = v_character)
        from private.game_character_shadow_receipts where character_id = v_character
    ),
    'same command with different canonical payload must leave receipt and head unchanged'
  );
  perform pg_temp.assert_true(
    (select head_state = 'existing' and head_sha256 = v_post_1 and revision = 1
       from private.game_character_legacy_heads where character_id = v_character),
    'absent CAS must advance one legacy head'
  );

  v_receipt_head_snapshot := (
    select count(*)::text || '|' || coalesce(string_agg(command_id::text, ',' order by command_id::text), '-') || '|' ||
           (select head_state || '|' || head_sha256 || '|' || revision::text
              from private.game_character_legacy_heads where character_id = v_character)
      from private.game_character_shadow_receipts where character_id = v_character
  );
  perform pg_temp.expect_state('22023', format(
    $query$select private.game_character_shadow_request_sha256(%L,%L::uuid,%L,%L,%L::uuid,%L::uuid,1::bigint,2::bigint,%L,null,%L,1::smallint)$query$,
    v_world, v_character, 'M3hero', '11', v_command_2, v_a, 'existing', v_post_2
  ));
  perform pg_temp.expect_state('22023', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,null,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_1,
    'existing', v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select count(*)::text || '|' || coalesce(string_agg(command_id::text, ',' order by command_id::text), '-') || '|' ||
             (select head_state || '|' || head_sha256 || '|' || revision::text
                from private.game_character_legacy_heads where character_id = v_character)
        from private.game_character_shadow_receipts where character_id = v_character
    ),
    'existing NULL prehash must be 22023 and cannot mutate head or receipt rows'
  );

  v_snapshot := (select coalesce(owner_user_id::text, '-') || '|' || lifecycle::text || '|' || legacy_name || '|' || legacy_shard
                   from public.game_characters where id = v_character);
  perform pg_temp.expect_state('22023', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,1::bigint,%L,null,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_1, v_a,
    upper(v_request_1), 'absent', v_post_1
  ));
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a,
    private.game_character_shadow_request_sha256(v_world, v_character, 'M3hero', '11', v_command_2, v_a, 1::bigint, 2::bigint, 'existing', repeat('c',64), v_post_2, 1::smallint),
    'existing', repeat('c',64), v_post_2
  ));
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3other', v_character, v_command_2, v_a,
    v_request_1, 'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_snapshot = (select coalesce(owner_user_id::text, '-') || '|' || lifecycle::text || '|' || legacy_name || '|' || legacy_shard
                    from public.game_characters where id = v_character),
    'receipt calls must not mutate identity fields'
  );

  -- A missing head may bootstrap only from the exact imported existing hash.
  update public.game_characters
     set lifecycle = 'imported_unclaimed', imported_file_sha256 = v_imported_hash
   where id = v_other_character;
  select legacy_shard::text into v_other_shard
    from public.game_characters where id = v_other_character;
  v_request_3 := private.game_character_shadow_request_sha256(
    v_world, v_other_character, 'M3other', v_other_shard, v_command_3, v_a,
    1::bigint, 1::bigint, 'existing', repeat('d', 64), v_post_3, 1::smallint
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,1::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3other', v_other_character, v_command_3, v_a, v_request_3,
    'existing', repeat('d', 64), v_post_3
  ));
  perform pg_temp.assert_true(
    (select count(*) = 0 from private.game_character_legacy_heads
      where character_id = v_other_character)
    and (select count(*) = 0 from private.game_character_shadow_receipts
      where character_id = v_other_character),
    'missing-head existing bootstrap must reject an observed hash different from the imported hash'
  );
  v_request_3 := private.game_character_shadow_request_sha256(
    v_world, v_other_character, 'M3other', v_other_shard, v_command_3, v_a,
    1::bigint, 1::bigint, 'existing', v_imported_hash, v_post_3, 1::smallint
  );
  perform private.record_legacy_published_receipt(
    v_world, 'M3other', v_other_character, v_command_3, v_a, v_request_3,
    1::bigint, 1::bigint, 'existing', v_imported_hash, v_post_3, 1::smallint
  );
  perform pg_temp.assert_true(
    (select head_state = 'existing' and head_sha256 = v_post_3 and storage_format = 1
       and revision = 1 and writer_epoch = 1
       from private.game_character_legacy_heads where character_id = v_other_character)
    and (select count(*) = 1 from private.game_character_shadow_receipts
      where character_id = v_other_character and command_id = v_command_3
        and expected_state = 'existing' and expected_sha256 = v_imported_hash
        and post_sha256 = v_post_3),
    'exact imported existing hash must bootstrap revision zero and atomically advance one receipt'
  );

  v_request_2 := private.game_character_shadow_request_sha256(
    v_world, v_character, 'M3hero', '11', v_command_2, v_a, 1::bigint, 2::bigint,
    'existing', v_post_1, v_post_2, 1::smallint
  );
  perform private.record_legacy_published_receipt(
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2, 1::bigint, 2::bigint,
    'existing', v_post_1, v_post_2, 1::smallint
  );
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|' ||
           (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                   coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
              from private.game_character_legacy_heads where character_id = v_character)
  );
  update private.game_character_writer_epochs
     set issued_at = clock_timestamp() - interval '2 seconds',
         expires_at = clock_timestamp() - interval '1 second'
   where world_id = v_world;
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'expired writer exact retry must be P0001 and leave receipt/head unchanged'
  );
  perform private.renew_game_world_writer_epoch(v_world, v_a, 1::bigint, clock_timestamp() + interval '3 minutes');
  perform private.record_legacy_published_receipt(
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2, 1::bigint, 2::bigint,
    'existing', v_post_1, v_post_2, 1::smallint
  );
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'renewed current writer exact retry must be read-only'
  );

  delete from private.game_character_legacy_heads where character_id = v_character;
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|missing'
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|missing'
    ),
    'missing head exact retry must be P0001 and cannot mutate receipt'
  );
  insert into private.game_character_legacy_heads(
    character_id, head_state, head_sha256, storage_format, revision, writer_epoch
  ) values (v_character, 'existing', v_post_1, 1, 1, 1);
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|' ||
           (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                   coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
              from private.game_character_legacy_heads where character_id = v_character)
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'behind head exact retry must be P0001 and cannot mutate receipt'
  );
  update private.game_character_legacy_heads
     set head_sha256 = v_post_1, revision = 2, writer_epoch = 1
   where character_id = v_character;
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|' ||
           (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                   coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
              from private.game_character_legacy_heads where character_id = v_character)
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'equal revision with a different posthash must reject exact retry'
  );
  update private.game_character_legacy_heads
     set head_sha256 = v_post_2, revision = 2, writer_epoch = 2
   where character_id = v_character;
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|' ||
           (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                   coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
              from private.game_character_legacy_heads where character_id = v_character)
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'different head writer epoch must reject exact retry without mutation'
  );
  update private.game_character_legacy_heads
     set storage_format = 2, writer_epoch = 1
   where character_id = v_character;
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|' ||
           (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                   coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
              from private.game_character_legacy_heads where character_id = v_character)
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'different head storage format must reject exact retry without mutation'
  );
  update private.game_character_legacy_heads
     set head_sha256 = repeat('f', 64), storage_format = 1, revision = 3, writer_epoch = 1
   where character_id = v_character;
  v_receipt_head_snapshot := (
    select (select acknowledged_at::text from private.game_character_shadow_receipts
             where character_id = v_character and command_id = v_command_2) || '|' ||
           (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                   coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
              from private.game_character_legacy_heads where character_id = v_character)
  );
  perform private.record_legacy_published_receipt(
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2, 1::bigint, 2::bigint,
    'existing', v_post_1, v_post_2, 1::smallint
  );
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select (select acknowledged_at::text from private.game_character_shadow_receipts
               where character_id = v_character and command_id = v_command_2) || '|' ||
             (select head_state || '|' || head_sha256 || '|' || storage_format::text || '|' ||
                     coalesce(writer_epoch::text, '-') || '|' || revision::text || '|' || updated_at::text
                from private.game_character_legacy_heads where character_id = v_character)
    ),
    'higher consistent-epoch head permits a read-only exact retry'
  );
  update private.game_character_legacy_heads
     set head_sha256 = v_post_2, revision = 2, writer_epoch = 1
   where character_id = v_character;
  update private.game_character_writer_epochs
     set issued_at = clock_timestamp() - interval '2 seconds',
         expires_at = clock_timestamp() - interval '1 second'
   where world_id = v_world;
  perform pg_temp.expect_state('P0001', format(
    $query$select private.acquire_game_world_writer_epoch(%L,%L::uuid,clock_timestamp()+interval '3 minutes')$query$,
    v_world, v_b
  ));
  perform private.renew_game_world_writer_epoch(v_world, v_a, 1::bigint, clock_timestamp() + interval '3 minutes');
  perform private.seal_game_world_writer_epoch(v_world, v_a, 1::bigint);
  perform pg_temp.expect_state('P0001', format(
    $query$select private.acquire_game_world_writer_epoch(%L,%L::uuid,clock_timestamp()+interval '3 minutes')$query$,
    v_world, v_b
  ));
  update private.game_character_writer_epochs
     set issued_at = clock_timestamp() - interval '2 seconds',
         expires_at = clock_timestamp() - interval '1 second'
   where world_id = v_world;
  perform pg_temp.expect_state('P0001', format(
    $query$select private.acquire_game_world_writer_epoch(%L,%L::uuid,clock_timestamp()+interval '3 minutes')$query$,
    v_world, v_a
  ));
  perform private.acquire_game_world_writer_epoch(v_world, v_b, clock_timestamp() + interval '3 minutes');
  perform pg_temp.assert_true(
    (select writer_epoch = 2 and writer_instance_id = v_b
       from private.game_character_writer_epochs where world_id = v_world),
    'only sealed expired predecessor permits successor installation'
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.renew_game_world_writer_epoch(%L,%L::uuid,1::bigint,clock_timestamp()+interval '3 minutes')$query$,
    v_world, v_a
  ));
  v_receipt_head_snapshot := (
    select count(*)::text || '|' || coalesce(string_agg(command_id::text, ',' order by command_id::text), '-') || '|' ||
           (select head_state || '|' || head_sha256 || '|' || revision::text
              from private.game_character_legacy_heads where character_id = v_character)
      from private.game_character_shadow_receipts where character_id = v_character
  );
  perform pg_temp.expect_state('P0001', format(
    $query$select private.seal_game_world_writer_epoch(%L,%L::uuid,1::bigint)$query$,
    v_world, v_a
  ));
  perform pg_temp.expect_state('P0001', format(
    $query$select private.record_legacy_published_receipt(%L,%L,%L::uuid,%L::uuid,%L::uuid,%L,1::bigint,2::bigint,%L,%L,%L,1::smallint)$query$,
    v_world, 'M3hero', v_character, v_command_2, v_a, v_request_2,
    'existing', v_post_1, v_post_2
  ));
  perform pg_temp.assert_true(
    v_receipt_head_snapshot = (
      select count(*)::text || '|' || coalesce(string_agg(command_id::text, ',' order by command_id::text), '-') || '|' ||
             (select head_state || '|' || head_sha256 || '|' || revision::text
                from private.game_character_legacy_heads where character_id = v_character)
        from private.game_character_shadow_receipts where character_id = v_character
    ),
    'successor must reject old exact receipt retry without head or receipt mutation'
  );
  perform pg_temp.assert_true(
    exists(select 1 from private.game_character_writer_epoch_fences
      where world_id = v_world and writer_epoch = 1 and writer_instance_id = v_a and successor_epoch = 2),
    'successor install must retain permanent predecessor fence evidence'
  );
end;
$$;

select pg_temp.assert_true(
  has_function_privilege('mud_writer', 'private.resolve_game_character_writer_route(text,text)', 'execute')
  and has_function_privilege('mud_writer', 'private.acquire_game_world_writer_epoch(text,uuid,timestamptz)', 'execute')
  and has_function_privilege('mud_writer', 'private.renew_game_world_writer_epoch(text,uuid,bigint,timestamptz)', 'execute')
  and has_function_privilege('mud_writer', 'private.seal_game_world_writer_epoch(text,uuid,bigint)', 'execute')
  and has_function_privilege('mud_writer', 'private.record_legacy_published_receipt(text,text,uuid,uuid,uuid,text,bigint,bigint,text,text,text,smallint)', 'execute')
  and not has_function_privilege('service_role', 'private.record_legacy_published_receipt(text,text,uuid,uuid,uuid,text,bigint,bigint,text,text,text,smallint)', 'execute')
  and not has_function_privilege('anon', 'private.acquire_game_world_writer_epoch(text,uuid,timestamptz)', 'execute')
  and not has_function_privilege('authenticated', 'private.acquire_game_world_writer_epoch(text,uuid,timestamptz)', 'execute')
  and not has_table_privilege('mud_writer', 'private.game_character_writer_epochs', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_writer_epoch_fences', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_legacy_heads', 'select')
  and not has_table_privilege('mud_writer', 'private.game_character_shadow_receipts', 'select')
  and (select not role_flags.rolcanlogin and not role_flags.rolinherit and not role_flags.rolsuper
         and not role_flags.rolcreatedb and not role_flags.rolcreaterole and not role_flags.rolreplication
         and not role_flags.rolbypassrls and role_secret.rolpassword is null
       from pg_roles role_flags
       join pg_authid role_secret on role_secret.oid = role_flags.oid
       where role_flags.rolname = 'mud_writer')
  and not exists (
    select 1 from pg_auth_members membership
    join pg_roles writer_role on writer_role.oid = membership.member or writer_role.oid = membership.roleid
    where writer_role.rolname = 'mud_writer'
  ),
  'mud_writer must be a non-login non-inheriting least-privilege role with no memberships'
);

rollback;
