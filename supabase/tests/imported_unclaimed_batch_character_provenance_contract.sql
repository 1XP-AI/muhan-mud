\set ON_ERROR_STOP on

-- This can run on its own in a disposable database and proves both the S4
-- prerequisite and S5a migration replay safely.
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql
\ir ../migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql

begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'batch character provenance contract failed: %', p_message;
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
  raise exception 'batch character provenance contract failed: expected %, statement succeeded', p_sqlstate;
end;
$$;

select pg_temp.assert_true(
  (select array_agg(a.attname order by a.attnum)
     from pg_attribute a
    where a.attrelid = 'private.game_imported_unclaimed_batch_members'::regclass
      and a.attnum > 0 and not a.attisdropped)
  = array['world_id', 'stream_id', 'batch_sequence', 'character_id']::text[],
  'member relation must retain only exact batch and character references'
);

select pg_temp.assert_true(
  exists (
    select 1 from pg_constraint c
     where c.conrelid = 'private.game_imported_unclaimed_batch_members'::regclass
       and c.conname = 'game_imported_unclaimed_batch_members_batch_fkey'
       and c.contype = 'f' and c.confrelid = 'private.game_imported_unclaimed_batches'::regclass
       and c.conkey = array[
         (select attnum from pg_attribute where attrelid = c.conrelid and attname = 'world_id' and not attisdropped),
         (select attnum from pg_attribute where attrelid = c.conrelid and attname = 'stream_id' and not attisdropped),
         (select attnum from pg_attribute where attrelid = c.conrelid and attname = 'batch_sequence' and not attisdropped)
       ]::smallint[]
       and c.confkey = array[
         (select attnum from pg_attribute where attrelid = c.confrelid and attname = 'world_id' and not attisdropped),
         (select attnum from pg_attribute where attrelid = c.confrelid and attname = 'stream_id' and not attisdropped),
         (select attnum from pg_attribute where attrelid = c.confrelid and attname = 'batch_sequence' and not attisdropped)
       ]::smallint[]
       and c.confdeltype = 'r'
  )
  and exists (
    select 1 from pg_constraint c
     where c.conrelid = 'private.game_imported_unclaimed_batch_members'::regclass
       and c.conname = 'game_imported_unclaimed_batch_members_character_fkey'
       and c.contype = 'f' and c.confrelid = 'public.game_characters'::regclass
       and c.conkey = array[
         (select attnum from pg_attribute where attrelid = c.conrelid and attname = 'character_id' and not attisdropped)
       ]::smallint[]
       and c.confkey = array[
         (select attnum from pg_attribute where attrelid = c.confrelid and attname = 'id' and not attisdropped)
       ]::smallint[]
       and c.confdeltype = 'r'
  )
  and exists (
    select 1 from pg_constraint c
     where c.conrelid = 'private.game_imported_unclaimed_batch_members'::regclass
       and c.contype = 'p' and c.conkey = array[
         (select attnum from pg_attribute where attrelid = c.conrelid and attname = 'character_id' and not attisdropped)
       ]::smallint[]
  ),
  'membership must bind both exact parents and permit only one row per character'
);

select pg_temp.assert_true(
  (select relrowsecurity from pg_class where oid = 'private.game_imported_unclaimed_batch_members'::regclass)
  and not has_table_privilege('anon', 'private.game_imported_unclaimed_batch_members', 'select,insert,update,delete')
  and not has_table_privilege('authenticated', 'private.game_imported_unclaimed_batch_members', 'select,insert,update,delete')
  and not has_table_privilege('service_role', 'private.game_imported_unclaimed_batch_members', 'select,insert,update,delete'),
  'members must have RLS and no browser or service direct CRUD'
);

insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count
) values (
  'provenance-contract-world', 'main', 0, 'provenance-identity-0', 'provenance-manifest-0',
  repeat('a', 64), 1, '1.2.3', 1, 'provenance-start-0', 'provenance-end-0', 1
);
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id
) values (
  '00000000-0000-4000-8000-0000000000a1', 'provenance-contract-world', 'Provenance', 'Provenance',
  substr(encode(digest(convert_to('Provenance', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('b', 64), null
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('provenance-contract-world', 'main', 0, '00000000-0000-4000-8000-0000000000a1');

select pg_temp.expect_state('23505', $$
  insert into private.game_imported_unclaimed_batch_members (world_id, stream_id, batch_sequence, character_id)
  values ('provenance-contract-world', 'main', 0, '00000000-0000-4000-8000-0000000000a1')
$$);
select pg_temp.expect_state('P0001', $$
  update private.game_imported_unclaimed_batch_members set stream_id = 'changed'
   where character_id = '00000000-0000-4000-8000-0000000000a1'
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_imported_unclaimed_batch_members
   where character_id = '00000000-0000-4000-8000-0000000000a1'
$$);
select pg_temp.expect_state('23503', $$
  delete from public.game_characters where id = '00000000-0000-4000-8000-0000000000a1'
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_imported_unclaimed_batches
   where world_id = 'provenance-contract-world' and stream_id = 'main' and batch_sequence = 0
$$);

rollback;

do $$
begin
  if exists (
    select 1 from private.game_imported_unclaimed_batch_members
     where character_id = '00000000-0000-4000-8000-0000000000a1'
  ) then
    raise exception 'batch character provenance contract failed: rollback persisted member';
  end if;
end;
$$;
