\set ON_ERROR_STOP on

\ir bootstrap_contract.sql
\ir ../migrations/20260902000000_game_identity.sql
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql
\ir ../migrations/20261008000000_imported_unclaimed_batch_member_identity.sql

begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'historic batch tuple gate contract failed: %', p_message;
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
  raise exception 'historic batch tuple gate contract failed: expected %, statement succeeded', p_sqlstate;
end;
$$;

-- This row models an old ledger batch whose complete membership remains
-- untouched. It is the sole accepted historic backfill shape.
insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count, recorded_at
) values (
  'historic-tuple-world', 'main', 0, 'historic-tuple-0', 'historic-manifest-0',
  repeat('a', 64), 1, '1.2.3', 1, 'historic-start-0', 'historic-end-0', 1,
  clock_timestamp() - interval '1 day'
);
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id, claimed_at
) values (
  '00000000-0000-4000-8000-0000000000c1', 'historic-tuple-world', 'Historic', 'Historic',
  substr(encode(digest(convert_to('Historic', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('b', 64), null, null
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('historic-tuple-world', 'main', 0, '00000000-0000-4000-8000-0000000000c1');

-- A complete membership with changed lifecycle facts cannot prove the
-- original tuple, so it deliberately receives no backfill.
insert into auth.users(id) values ('00000000-0000-4000-8000-0000000000d2');
insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count, recorded_at
) values (
  'historic-tuple-world', 'main', 1, 'historic-tuple-1', 'historic-manifest-1',
  repeat('c', 64), 1, '1.2.3', 1, 'historic-start-1', 'historic-end-1', 1,
  clock_timestamp() - interval '1 day'
);
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id, claimed_at
) values (
  '00000000-0000-4000-8000-0000000000c2', 'historic-tuple-world', 'Changed', 'Changed',
  substr(encode(digest(convert_to('Changed', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'handoff_pending', 1, repeat('d', 64), '00000000-0000-4000-8000-0000000000d2', clock_timestamp()
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('historic-tuple-world', 'main', 1, '00000000-0000-4000-8000-0000000000c2');

-- A count/member mismatch also remains unverifiable even while its one known
-- character still looks imported-unclaimed.
insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count, recorded_at
) values (
  'historic-tuple-world', 'main', 2, 'historic-tuple-2', 'historic-manifest-2',
  repeat('e', 64), 1, '1.2.3', 1, 'historic-start-2', 'historic-end-2', 2,
  clock_timestamp() - interval '1 day'
);
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id, claimed_at
) values (
  '00000000-0000-4000-8000-0000000000c3', 'historic-tuple-world', 'Partial', 'Partial',
  substr(encode(digest(convert_to('Partial', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('f', 64), null, null
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('historic-tuple-world', 'main', 2, '00000000-0000-4000-8000-0000000000c3');

\ir ../migrations/20261009000000_imported_unclaimed_historic_batch_tuple_gate.sql
\ir ../migrations/20261009000000_imported_unclaimed_historic_batch_tuple_gate.sql

select pg_temp.assert_true(
  (select count(*) = 1
     from private.game_imported_unclaimed_batch_member_identities
    where world_id = 'historic-tuple-world' and stream_id = 'main'
      and batch_sequence = 0 and canonical_legacy_name = 'Historic'
      and legacy_shard = substr(encode(digest(convert_to('Historic', 'UTF8'), 'sha1'), 'hex'), 1, 2)
      and imported_file_sha256 = repeat('b', 64) and storage_format = 1),
  'only a complete unchanged historic member set may persist its exact tuple'
);
select pg_temp.assert_true(
  not exists (
    select 1 from private.game_imported_unclaimed_batch_member_identities
     where world_id = 'historic-tuple-world' and stream_id = 'main'
       and batch_sequence in (1, 2)
  ),
  'changed or count-incomplete historic batches must remain without reconstructable tuples'
);

-- The valid backfill has persisted first, so the lifecycle gate permits the
-- normal transition. Missing tuple evidence rejects before a verdict-mutating
-- lifecycle write can be committed.
update public.game_characters set lifecycle = 'handoff_pending'
 where id = '00000000-0000-4000-8000-0000000000c1';
select pg_temp.expect_state('P0001', $$
  update public.game_characters set lifecycle = 'active'
   where id = '00000000-0000-4000-8000-0000000000c2'
$$);
select pg_temp.expect_state('P0001', $$
  update public.game_characters set lifecycle = 'handoff_pending'
   where id = '00000000-0000-4000-8000-0000000000c3'
$$);
select pg_temp.assert_true(
  (select lifecycle = 'handoff_pending' from public.game_characters where id = '00000000-0000-4000-8000-0000000000c1')
  and (select lifecycle = 'handoff_pending' from public.game_characters where id = '00000000-0000-4000-8000-0000000000c2')
  and (select lifecycle = 'imported_unclaimed' from public.game_characters where id = '00000000-0000-4000-8000-0000000000c3'),
  'historic lifecycle updates must fail closed without exact complete tuple evidence'
);

rollback;
