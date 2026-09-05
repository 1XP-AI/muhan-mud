\set ON_ERROR_STOP on

-- The unmerged migration remains replay-safe and its exact shape supports the
-- PostgresImportTransaction store API.
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql

-- Ledger worlds must remain insertable into public.game_characters: 64
-- characters is admissible while 65 is rejected before any durable write.
do $$
begin
  insert into private.game_imported_unclaimed_batches (
    world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
    source_sha256, source_byte_size, parser_version, abi, start_marker,
    end_marker, record_count
  ) values (
    repeat('w', 64), 'bounds', 0, 'world-boundary-64', 'manifest-bounds', repeat('d', 64),
    0, '1.2.3', 1, 'range-start', 'range-end', 0
  );

  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
      source_sha256, source_byte_size, parser_version, abi, start_marker,
      end_marker, record_count
    ) values (
      repeat('w', 65), 'bounds', 0, 'world-boundary-65', 'manifest-bounds', repeat('e', 64),
      0, '1.2.3', 1, 'range-start', 'range-end', 0
    );
    raise exception 'overlong ledger world unexpectedly succeeded';
  exception when check_violation then null;
  end;
end;
$$;

do $$
declare
  v_rows integer;
begin
  insert into private.game_imported_unclaimed_batches (
    world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
    source_sha256, source_byte_size, parser_version, abi, start_marker,
    end_marker, record_count
  ) values (
    'ledger-world', 'main', 0, 'stable-identity-0', 'manifest-v1', repeat('a', 64),
    4096, '1.2.3', 1, 'range-start', 'range-end', 2
  );

  -- Same world/stream/sequence is occupied by one immutable identity.
  begin
    insert into private.game_imported_unclaimed_batches (
      world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
      source_sha256, source_byte_size, parser_version, abi, start_marker,
      end_marker, record_count
    ) values (
      'ledger-world', 'main', 0, 'changed-identity-0', 'manifest-v2', repeat('b', 64),
      1, '1.2.3', 1, 'range-start-2', 'range-end-2', 1
    );
    raise exception 'changed identity occupied sequence unexpectedly succeeded';
  exception when unique_violation then null;
  end;

  insert into private.game_imported_unclaimed_batch_watermarks (
    world_id, stream_id, watermark_sequence, committed_batch_sequence
  ) values ('ledger-world', 'main', 0, 0);

  insert into private.game_imported_unclaimed_batches (
    world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
    source_sha256, source_byte_size, parser_version, abi, start_marker,
    end_marker, record_count
  ) values (
    'ledger-world', 'main', 1, 'stable-identity-1', 'manifest-v2', repeat('b', 64),
    8192, '1.2.3+build.7', 2, 'range-start-2', 'range-end-2', 3
  );
  update private.game_imported_unclaimed_batch_watermarks
     set watermark_sequence = 1, committed_batch_sequence = 1
   where world_id = 'ledger-world' and stream_id = 'main';

  -- Another stream has an isolated zero-based sequence and watermark.
  insert into private.game_imported_unclaimed_batches (
    world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
    source_sha256, source_byte_size, parser_version, abi, start_marker,
    end_marker, record_count
  ) values (
    'ledger-world', 'recovery', 0, 'recovery-identity-0', 'manifest-v1', repeat('c', 64),
    0, '1.2.3-rc.1', 1, 'range-start', 'range-end', 0
  );
  insert into private.game_imported_unclaimed_batch_watermarks (
    world_id, stream_id, watermark_sequence, committed_batch_sequence
  ) values ('ledger-world', 'recovery', 0, 0);

  select count(*) into v_rows from private.game_imported_unclaimed_batch_watermarks
   where world_id = 'ledger-world';
  if v_rows <> 2 then raise exception 'streams were not independently watermarked'; end if;
end;
$$;

-- Batch evidence is append-only and watermark writes cannot regress or reuse
-- an equal sequence to retarget durable state.
do $$
begin
  begin
    update private.game_imported_unclaimed_batches set record_count = 99
     where world_id = 'ledger-world' and stream_id = 'main' and batch_sequence = 0;
    raise exception 'immutable batch update unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;
  begin
    update private.game_imported_unclaimed_batch_watermarks
       set watermark_sequence = 0, committed_batch_sequence = 0
     where world_id = 'ledger-world' and stream_id = 'main';
    raise exception 'watermark regression unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;
  begin
    update private.game_imported_unclaimed_batch_watermarks
       set observed_at = clock_timestamp()
     where world_id = 'ledger-world' and stream_id = 'main';
    raise exception 'equal watermark retarget/update unexpectedly succeeded';
  exception when sqlstate 'P0001' then null;
  end;
end;
$$;
