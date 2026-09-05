\set ON_ERROR_STOP on

-- This executable contract is self-contained for its disposable PostgreSQL
-- runner: bootstrap provides the Supabase roles/auth stub, game identity
-- provides the immutable three-field character scope and canonicalizer, and
-- the following migrations provide the batch/member parents.
\ir bootstrap_contract.sql
\ir ../migrations/20260902000000_game_identity.sql
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql
\ir ../migrations/20261001000000_imported_unclaimed_batch_member_legacy_locator.sql
\ir ../migrations/20261001000000_imported_unclaimed_batch_member_legacy_locator.sql

begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'batch member legacy locator contract failed: %', p_message;
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
  raise exception 'batch member legacy locator contract failed: expected %, statement succeeded', p_sqlstate;
end;
$$;

select pg_temp.assert_true(
  (select array_agg(a.attname order by a.attnum)
     from pg_attribute a
    where a.attrelid = 'private.game_imported_unclaimed_batch_member_legacy_locators'::regclass
      and a.attnum > 0 and not a.attisdropped)
  = array['character_id', 'canonical_legacy_name', 'legacy_name_sha1', 'legacy_shard']::text[],
  'locator must retain only member identity plus canonical name, SHA-1 digest, and shard'
);

select pg_temp.assert_true(
  exists (
    select 1 from pg_constraint c
     where c.conrelid = 'private.game_imported_unclaimed_batch_member_legacy_locators'::regclass
       and c.conname = 'game_imported_unclaimed_batch_member_legacy_locators_member_fkey'
       and c.contype = 'f'
       and c.confrelid = 'private.game_imported_unclaimed_batch_members'::regclass
       and c.confdeltype = 'r'
  )
  and exists (
    select 1 from pg_constraint c
     where c.conrelid = 'private.game_imported_unclaimed_batch_member_legacy_locators'::regclass
       and c.contype = 'p'
  )
  and (select relrowsecurity from pg_class where oid = 'private.game_imported_unclaimed_batch_member_legacy_locators'::regclass)
  and not has_table_privilege('anon', 'private.game_imported_unclaimed_batch_member_legacy_locators', 'select,insert,update,delete')
  and not has_table_privilege('authenticated', 'private.game_imported_unclaimed_batch_member_legacy_locators', 'select,insert,update,delete')
  and not has_table_privilege('service_role', 'private.game_imported_unclaimed_batch_member_legacy_locators', 'select,insert,update,delete'),
  'locator must be one immutable private row for one immutable member'
);

insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count
) values (
  'locator-contract-world', 'main', 0, 'locator-identity-0', 'locator-manifest-0',
  repeat('a', 64), 1, '1.2.3', 1, 'locator-start-0', 'locator-end-0', 1
);
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id
) values (
  '00000000-0000-4000-8000-0000000000b1', 'locator-contract-world', 'Locator', 'Locator',
  substr(encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('b', 64), null
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('locator-contract-world', 'main', 0, '00000000-0000-4000-8000-0000000000b1');
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id
) values (
  '00000000-0000-4000-8000-0000000000b2', 'locator-contract-world', 'Digest', 'Digest',
  substr(encode(digest(convert_to('Digest', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('c', 64), null
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('locator-contract-world', 'main', 0, '00000000-0000-4000-8000-0000000000b2');

insert into private.game_imported_unclaimed_batch_member_legacy_locators (
  character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
) values (
  '00000000-0000-4000-8000-0000000000b1', 'Locator',
  encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'),
  substr(encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'), 1, 2)
);

-- The store's conflict path permits an exact retry without a second write.
insert into private.game_imported_unclaimed_batch_member_legacy_locators (
  character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
) values (
  '00000000-0000-4000-8000-0000000000b1', 'Locator',
  encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'),
  substr(encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'), 1, 2)
) on conflict (character_id) do nothing;
select pg_temp.assert_true(
  (select count(*) = 1 from private.game_imported_unclaimed_batch_member_legacy_locators
    where character_id = '00000000-0000-4000-8000-0000000000b1'),
  'exact locator retry must retain one unchanged row'
);

select pg_temp.expect_state('P0001', $$
  insert into private.game_imported_unclaimed_batch_member_legacy_locators (
    character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
  ) values (
    '00000000-0000-4000-8000-0000000000b1', 'Other',
    encode(digest(convert_to('Other', 'UTF8'), 'sha1'), 'hex'),
    substr(encode(digest(convert_to('Other', 'UTF8'), 'sha1'), 'hex'), 1, 2)
  )
$$);
select pg_temp.expect_state('23514', $$
  insert into private.game_imported_unclaimed_batch_member_legacy_locators (
    character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
  ) values (
    '00000000-0000-4000-8000-0000000000b2', 'Digest', repeat('0', 40),
    substr(encode(digest(convert_to('Digest', 'UTF8'), 'sha1'), 'hex'), 1, 2)
  )
$$);
select pg_temp.assert_true(
  E'Bad\\name' ~ E'[[:cntrl:]/\\\\:]'
  and 'Locator' !~ E'[[:cntrl:]/\\\\:]',
  'locator C-safe regex must match the game-identity rule and reject backslash'
);
select pg_temp.expect_state('P0001', $$
  update private.game_imported_unclaimed_batch_member_legacy_locators
     set legacy_shard = '00'
   where character_id = '00000000-0000-4000-8000-0000000000b1'
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_imported_unclaimed_batch_member_legacy_locators
   where character_id = '00000000-0000-4000-8000-0000000000b1'
$$);
-- PostgreSQL runs this parent-validation BEFORE INSERT trigger before the
-- foreign-key constraint, so a missing member has the trigger's P0001 result.
select pg_temp.expect_state('P0001', $$
  insert into private.game_imported_unclaimed_batch_member_legacy_locators (
    character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
  ) values (
    '00000000-0000-4000-8000-0000000000b3', 'Locator',
    encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'),
    substr(encode(digest(convert_to('Locator', 'UTF8'), 'sha1'), 'hex'), 1, 2)
  )
$$);

rollback;
