\set ON_ERROR_STOP on

-- This contract owns a disposable database supplied by CI.  Replaying the
-- migration here makes idempotence an executable property rather than a
-- source-text convention.
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql

do $$
declare
  v_rows integer;
  v_committed uuid := '11111111-1111-1111-1111-111111111111';
  v_second uuid := '22222222-2222-2222-2222-222222222222';
  v_isolated uuid := '33333333-3333-3333-3333-333333333333';
  v_mismatch uuid := '44444444-4444-4444-4444-444444444444';
begin
  insert into private.game_imported_unclaimed_batches (
    world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
    parser_version, abi, start_marker, end_marker, batch_sequence
  ) values (
    'ledger-world', v_committed, 'manifest-v1', repeat('a', 64), 9007199254740991,
    '1.2.3-rc.1+build.7', 9007199254740991, 'range-start', 'range-end', 11
  );

  -- An exact retry resolves the same complete identity without adding a row.
  insert into private.game_imported_unclaimed_batches (
    world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
    parser_version, abi, start_marker, end_marker, batch_sequence
  ) values (
    'ledger-world', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', 'manifest-v1', repeat('a', 64), 9007199254740991,
    '1.2.3-rc.1+build.7', 9007199254740991, 'range-start', 'range-end', 12
  ) on conflict on constraint game_imported_unclaimed_batches_durable_identity_key do nothing;

  select count(*) into v_rows from private.game_imported_unclaimed_batches
   where world_id = 'ledger-world';
  if v_rows <> 1 then
    raise exception 'exact durable identity retry inserted % rows, expected 1', v_rows;
  end if;

  -- A different valid source range is a different identity, even with the
  -- same manifest, bytes, parser, ABI, and digest.
  insert into private.game_imported_unclaimed_batches (
    world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
    parser_version, abi, start_marker, end_marker, batch_sequence
  ) values (
    'ledger-world', v_second, 'manifest-v1', repeat('a', 64), 9007199254740991,
    '1.2.3-rc.1+build.7', 9007199254740991, 'range-start-2', 'range-end-2', 11
  );

  -- The same range can exist in another canonical world without crossing a
  -- world's ledger or watermark boundary.
  insert into private.game_imported_unclaimed_batches (
    world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
    parser_version, abi, start_marker, end_marker, batch_sequence
  ) values (
    'ledger-world-two', v_isolated, 'manifest-v1', repeat('a', 64), 0,
    '1.2.3', 1, 'range-start', 'range-end', 0
  );

  insert into private.game_imported_unclaimed_batches (
    world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
    parser_version, abi, start_marker, end_marker, batch_sequence
  ) values (
    'ledger-world', v_mismatch, 'manifest-v1', repeat('a', 64), 1,
    '1.2.3', 1, 'range-start-3', 'range-end-3', 13
  );

  insert into private.game_imported_unclaimed_batch_watermarks (
    world_id, watermark_sequence, committed_batch_id
  ) values ('ledger-world', 11, v_committed);

  select count(*) into v_rows from private.game_imported_unclaimed_batch_watermarks w
   join private.game_imported_unclaimed_batches b
     on (b.world_id, b.batch_id) = (w.world_id, w.committed_batch_id)
  where w.world_id = 'ledger-world' and w.watermark_sequence = b.batch_sequence;
  if v_rows <> 1 then
    raise exception 'watermark is not auditable against its committed batch';
  end if;
end;
$$;

-- Immutable evidence cannot be rewritten or deleted.
do $$
begin
  begin
    update private.game_imported_unclaimed_batches
       set batch_sequence = 12
     where world_id = 'ledger-world';
    raise exception 'immutable batch update unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;

  begin
    delete from private.game_imported_unclaimed_batches
     where world_id = 'ledger-world';
    raise exception 'immutable batch delete unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;
end;
$$;

-- Per-world watermarks may advance but never regress, and must continue to
-- point at a batch whose committed sequence is the stated watermark.
do $$
begin
  begin
    update private.game_imported_unclaimed_batch_watermarks
       set watermark_sequence = 10
     where world_id = 'ledger-world';
    raise exception 'watermark regression unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;

  begin
    update private.game_imported_unclaimed_batch_watermarks
       set committed_batch_id = '44444444-4444-4444-4444-444444444444'
     where world_id = 'ledger-world';
    raise exception 'watermark commit mismatch unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;
end;
$$;

-- The SQL boundary deliberately mirrors the TypeScript validator's lowercase
-- identifiers, safe number bounds, strict SemVer, and non-empty range.
do $$
begin
  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker, batch_sequence
    ) values ('Ledger-World', '99999999-9999-9999-9999-999999999999', 'manifest-v1', repeat('b', 64), 1,
      '1.2.3', 1, 'range-start', 'range-end', 1);
    raise exception 'noncanonical identifier unexpectedly succeeded';
  exception when check_violation then null;
  end;

  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker, batch_sequence
    ) values ('ledger-world-three', '55555555-5555-5555-5555-555555555555', 'manifest-v1', repeat('b', 64), 9007199254740992,
      '1.2.3', 1, 'range-start', 'range-end', 1);
    raise exception 'unsafe source byte size unexpectedly succeeded';
  exception when check_violation then null;
  end;

  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker, batch_sequence
    ) values ('ledger-world-four', '66666666-6666-6666-6666-666666666666', 'manifest-v1', repeat('b', 64), 1,
      '01.2.3', 1, 'range-start', 'range-end', 1);
    raise exception 'non-SemVer parser version unexpectedly succeeded';
  exception when check_violation then null;
  end;

  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker, batch_sequence
    ) values ('ledger-world-five', '77777777-7777-7777-7777-777777777777', 'manifest-v1', repeat('b', 64), 1,
      '1.2.3', 0, 'range-start', 'range-end', 1);
    raise exception 'nonpositive ABI unexpectedly succeeded';
  exception when check_violation then null;
  end;

  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker, batch_sequence
    ) values ('ledger-world-five-b', 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', 'manifest-v1', repeat('b', 64), 1,
      '1.2.3', 9007199254740992, 'range-start', 'range-end', 1);
    raise exception 'unsafe ABI unexpectedly succeeded';
  exception when check_violation then null;
  end;

  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, batch_id, source_manifest_id, source_sha256, source_byte_size,
      parser_version, abi, start_marker, end_marker, batch_sequence
    ) values ('ledger-world-six', '88888888-8888-8888-8888-888888888888', 'manifest-v1', repeat('b', 64), 1,
      '1.2.3', 1, 'same-range', 'same-range', 1);
    raise exception 'equal range markers unexpectedly succeeded';
  exception when check_violation then null;
  end;
end;
$$;
