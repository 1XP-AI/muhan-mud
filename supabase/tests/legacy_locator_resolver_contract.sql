\set ON_ERROR_STOP on

-- This executable contract is self-contained for its disposable PostgreSQL
-- runner and replays the resolver migration to prove upgrade/job safety.
\ir bootstrap_contract.sql
\ir ../migrations/20260902000000_game_identity.sql
\ir ../migrations/20260928000000_imported_unclaimed_batch_ledger.sql
\ir ../migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql
\ir ../migrations/20261001000000_imported_unclaimed_batch_member_legacy_locator.sql
\ir ../migrations/20261002000000_legacy_locator_resolver.sql
\ir ../migrations/20261002000000_legacy_locator_resolver.sql

begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'legacy locator resolver contract failed: %', p_message;
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
  raise exception 'legacy locator resolver contract failed: expected %, statement succeeded', p_sqlstate;
end;
$$;

create or replace function pg_temp.resolver_state()
returns jsonb language sql stable as $$
  select jsonb_build_object(
    'characters', coalesce((
      select jsonb_agg(to_jsonb(character) order by character.id)
        from public.game_characters as character
    ), '[]'::jsonb),
    'members', coalesce((
      select jsonb_agg(to_jsonb(member) order by member.character_id)
        from private.game_imported_unclaimed_batch_members as member
    ), '[]'::jsonb),
    'locators', coalesce((
      select jsonb_agg(to_jsonb(locator) order by locator.character_id)
        from private.game_imported_unclaimed_batch_member_legacy_locators as locator
    ), '[]'::jsonb)
  );
$$;

select pg_temp.assert_true(
  to_regprocedure('public.resolve_game_imported_legacy_locator(text,text,text,character)') is not null
  and pg_get_function_result(
    'public.resolve_game_imported_legacy_locator(text,text,text,character)'::regprocedure
  ) = 'TABLE(character_id uuid)'
  and has_function_privilege(
    'service_role', 'public.resolve_game_imported_legacy_locator(text,text,text,character)', 'execute'
  )
  and not has_function_privilege(
    'anon', 'public.resolve_game_imported_legacy_locator(text,text,text,character)', 'execute'
  )
  and not has_function_privilege(
    'authenticated', 'public.resolve_game_imported_legacy_locator(text,text,text,character)', 'execute'
  ),
  'resolver must expose only one character UUID to service_role'
);

insert into private.game_imported_unclaimed_batches (
  world_id, stream_id, batch_sequence, identity_key, source_manifest_id,
  source_sha256, source_byte_size, parser_version, abi, start_marker,
  end_marker, record_count
) values (
  'resolver-contract-world', 'main', 0, 'resolver-identity-0', 'resolver-manifest-0',
  repeat('a', 64), 1, '1.2.3', 1, 'resolver-start-0', 'resolver-end-0', 1
);
insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
  storage_format, imported_file_sha256, owner_user_id
) values (
  '00000000-0000-4000-8000-0000000000c1', 'resolver-contract-world', 'Resolver', 'Resolver',
  substr(encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1, repeat('b', 64), null
);
insert into private.game_imported_unclaimed_batch_members (
  world_id, stream_id, batch_sequence, character_id
) values ('resolver-contract-world', 'main', 0, '00000000-0000-4000-8000-0000000000c1');
insert into private.game_imported_unclaimed_batch_member_legacy_locators (
  character_id, canonical_legacy_name, legacy_name_sha1, legacy_shard
) values (
  '00000000-0000-4000-8000-0000000000c1', 'Resolver',
  encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'),
  substr(encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'), 1, 2)
);

create temp table pg_temp.state_before as select pg_temp.resolver_state() as value;

set local role service_role;
select pg_temp.assert_true(
  (select jsonb_agg(to_jsonb(resolution))
     from public.resolve_game_imported_legacy_locator(
       'resolver-contract-world', 'Resolver',
       encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'),
       substr(encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'), 1, 2)
     ) as resolution)
  = jsonb_build_array(jsonb_build_object(
      'character_id', '00000000-0000-4000-8000-0000000000c1'::uuid
    )),
  'exact immutable locator tuple returns one UUID and no additional output'
);

select pg_temp.expect_state('22023', $$
  select * from public.resolve_game_imported_legacy_locator(
    'resolver-contract-world', 'resolver',
    encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'),
    substr(encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'), 1, 2)
  )
$$);
select pg_temp.expect_state('22023', $$
  select * from public.resolve_game_imported_legacy_locator(
    'resolver-contract-world', 'Resolver', repeat('0', 40), '00'
  )
$$);
select pg_temp.expect_state('P0001', $$
  select * from public.resolve_game_imported_legacy_locator(
    'other-contract-world', 'Resolver',
    encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'),
    substr(encode(digest(convert_to('Resolver', 'UTF8'), 'sha1'), 'hex'), 1, 2)
  )
$$);
select pg_temp.expect_state('P0001', $$
  select * from public.resolve_game_imported_legacy_locator(
    'resolver-contract-world', 'Missing',
    encode(digest(convert_to('Missing', 'UTF8'), 'sha1'), 'hex'),
    substr(encode(digest(convert_to('Missing', 'UTF8'), 'sha1'), 'hex'), 1, 2)
  )
$$);
reset role;

select pg_temp.assert_true(
  (select value from pg_temp.state_before) = pg_temp.resolver_state(),
  'resolver positive, malformed, and mismatch calls leave character, member, and locator state unchanged'
);

rollback;
