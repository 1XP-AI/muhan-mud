\set ON_ERROR_STOP on

\ir bootstrap_contract.sql
\ir ../migrations/20260902000000_game_identity.sql
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20261008000000_imported_unclaimed_batch_member_identity.sql
\ir ../migrations/20261008000000_imported_unclaimed_batch_member_identity.sql

begin;

create or replace function pg_temp.expect_state(p_sqlstate text, p_sql text)
returns void language plpgsql as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then return; end if;
    raise;
  end;
  raise exception 'batch member identity contract failed: expected %, statement succeeded', p_sqlstate;
end;
$$;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'batch member identity contract failed: %', p_message;
  end if;
end;
$$;

select pg_temp.assert_true(
  (select array_agg(a.attname order by a.attnum)
     from pg_attribute a
    where a.attrelid = 'private.game_imported_unclaimed_batch_member_identities'::regclass
      and a.attnum > 0 and not a.attisdropped)
  = array['world_id', 'stream_id', 'batch_sequence', 'canonical_legacy_name', 'legacy_shard', 'imported_file_sha256', 'storage_format']::text[],
  'private tuple must bind the exact canonical name, shard, SHA-256, and storage format'
);

select pg_temp.assert_true(
  (select relrowsecurity from pg_class where oid = 'private.game_imported_unclaimed_batch_member_identities'::regclass)
  and not has_table_privilege('anon', 'private.game_imported_unclaimed_batch_member_identities', 'select,insert,update,delete')
  and not has_table_privilege('authenticated', 'private.game_imported_unclaimed_batch_member_identities', 'select,insert,update,delete')
  and not has_table_privilege('service_role', 'private.game_imported_unclaimed_batch_member_identities', 'select,insert,update,delete'),
  'private tuple relation must deny direct browser/service CRUD'
);

insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count
) values (
  'identity-contract-world', 'main', 0, 'identity-contract-0', 'identity-manifest-0',
  repeat('a', 64), 1, '1.2.3', 1, 'identity-start-0', 'identity-end-0', 1
);

insert into private.game_imported_unclaimed_batch_member_identities (
  world_id, stream_id, batch_sequence, canonical_legacy_name, legacy_shard,
  imported_file_sha256, storage_format
) values (
  'identity-contract-world', 'main', 0, 'Alice',
  substr(encode(digest(convert_to('Alice', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  repeat('b', 64), 1
);

select pg_temp.expect_state('23505', $$
  insert into private.game_imported_unclaimed_batch_member_identities (
    world_id, stream_id, batch_sequence, canonical_legacy_name, legacy_shard,
    imported_file_sha256, storage_format
  ) values ('identity-contract-world', 'main', 0, 'Alice', '35', repeat('b', 64), 1)
$$);
select pg_temp.expect_state('23514', $$
  insert into private.game_imported_unclaimed_batch_member_identities (
    world_id, stream_id, batch_sequence, canonical_legacy_name, legacy_shard,
    imported_file_sha256, storage_format
  ) values ('identity-contract-world', 'main', 0, 'Bob', '00', repeat('b', 64), 1)
$$);
select pg_temp.expect_state('23514', $$
  insert into private.game_imported_unclaimed_batch_member_identities (
    world_id, stream_id, batch_sequence, canonical_legacy_name, legacy_shard,
    imported_file_sha256, storage_format
  ) values ('identity-contract-world', 'main', 0, 'Carol', '6f', 'invalid', 1)
$$);
select pg_temp.expect_state('23514', $$
  insert into private.game_imported_unclaimed_batch_member_identities (
    world_id, stream_id, batch_sequence, canonical_legacy_name, legacy_shard,
    imported_file_sha256, storage_format
  ) values ('identity-contract-world', 'main', 0, 'Dave', '33', repeat('c', 64), 2)
$$);
select pg_temp.expect_state('P0001', $$
  update private.game_imported_unclaimed_batch_member_identities
     set storage_format = 2
   where world_id = 'identity-contract-world' and stream_id = 'main'
     and batch_sequence = 0 and canonical_legacy_name = 'Alice'
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_imported_unclaimed_batch_member_identities
   where world_id = 'identity-contract-world' and stream_id = 'main'
     and batch_sequence = 0 and canonical_legacy_name = 'Alice'
$$);

rollback;
