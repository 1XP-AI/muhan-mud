\set ON_ERROR_STOP on

-- Disposable PG17 contract.  Run through migration 140 first for RED, then
-- apply 150 and rerun for GREEN.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'player snapshot v1 artifact contract failed: %', p_message;
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
    message = format('expected SQLSTATE %s', p_sqlstate);
end;
$$;

select pg_temp.assert_true(
  to_regprocedure('private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)') is not null
  and to_regprocedure('private.list_player_snapshot_v1_artifact_reconciliation(text,integer)') is not null,
  'exact artifact RPC signatures must exist'
);

select pg_temp.assert_true(
  (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass)
  and (select count(*) = 1 from pg_constraint
       where conrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
         and contype = 'p')
  and (select count(*) = 1 from pg_constraint
       where conrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
         and contype = 'u'),
  'artifact table is private/RLS with both identity and revision uniqueness'
);

select pg_temp.assert_true(
  (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p where p.oid = 'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)'::regprocedure)
  and (select p.prosecdef and p.proconfig = array['search_path=pg_catalog, private']::text[]
     from pg_proc p where p.oid = 'private.list_player_snapshot_v1_artifact_reconciliation(text,integer)'::regprocedure),
  'RPCs are SECURITY DEFINER with a pinned search path'
);

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format
) values (
  'a9500000-0000-0000-0000-000000000001', 'pva-contract', 'Pvahero', 'Pvahero',
  substr(encode(public.digest(convert_to('Pvahero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed', 1
);

insert into private.game_character_legacy_heads(character_id, head_state, storage_format, revision)
values ('a9500000-0000-0000-0000-000000000001', 'absent', 1, 0);

select * from private.acquire_game_world_writer_epoch(
  'pva-contract', 'b9500000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);

select private.game_character_shadow_request_sha256(
  'pva-contract', 'a9500000-0000-0000-0000-000000000001'::uuid, 'Pvahero',
  substr(encode(public.digest(convert_to('Pvahero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9500000-0000-0000-0000-000000000001'::uuid,
  'b9500000-0000-0000-0000-000000000001'::uuid, 1::bigint, 1::bigint, 'absent', null::text,
  repeat('a', 64), 1::smallint
) as request_sha256 \gset pva_first_

select private.record_legacy_published_receipt(
  'pva-contract', 'Pvahero', 'a9500000-0000-0000-0000-000000000001'::uuid,
  'c9500000-0000-0000-0000-000000000001'::uuid,
  'b9500000-0000-0000-0000-000000000001'::uuid, :'pva_first_request_sha256',
  1::bigint, 1::bigint, 'absent', null::text, repeat('a', 64), 1::smallint
);

-- pva_payload is the canonical bytes emitted by the C PlayerSnapshotV1
-- encoder for the checked-in Pvahero fixture.  C and Rust fixture tests
-- independently reread it byte-for-byte; this harness passes it as SQL.
\if :{?pva_payload}
\else
  \quit
\endif
\if :{?pva_inventory_payload}
\else
  \quit
\endif
\if :{?pva_tree_inventory_payload}
\else
  \quit
\endif

\set pva_empty_payload 'decode(''4d55484344544f000001000700000000e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'', ''hex'')'
\set pva_empty_snapshot_sha256 ff6b9dc3f41b0c4170f2aa485e2e57c4ce2763198ab273556cf9e8b98c3d23c0

create or replace function pg_temp.cdto_reseal(p_wire bytea, p_body bytea)
returns bytea language sql immutable as $$
  select substring(p_wire from 1 for 12)
      || decode(lpad(to_hex(octet_length(p_body)), 8, '0'), 'hex')
      || p_body
      || public.digest(p_body, 'sha256')
$$;

-- A CDTO record has no field-count word.  These three mutations remain
-- digest-valid kind-7 envelopes, but respectively contain 39 fields, a
-- changed type tag, and a changed fixed-field length.  Before this boundary
-- fix the old envelope-only validator accepted each one.
create or replace function pg_temp.pva_missing_field_40(p_wire bytea)
returns bytea language sql immutable as $$
  select pg_temp.cdto_reseal($1, substring($1 from 17 for 1664))
$$;
create or replace function pg_temp.pva_field_1_wrong_type(p_wire bytea)
returns bytea language sql immutable as $$
  select pg_temp.cdto_reseal(
    $1, set_byte(substring($1 from 17 for octet_length($1) - 48), 2, 10))
$$;
create or replace function pg_temp.pva_field_1_wrong_length(p_wire bytea)
returns bytea language sql immutable as $$
  select pg_temp.cdto_reseal($1,
    substring($1 from 17 for 3) || decode('0000004f', 'hex')
      || substring($1 from 24 for 79) || substring($1 from 104))
$$;
create or replace function pg_temp.pva_inventory_bad_shots(p_wire bytea)
returns bytea language sql immutable as $$
  select pg_temp.cdto_reseal($1,
    substring($1 from 17 for 1671)
    || pg_temp.cdto_reseal(
      substring($1 from 1688),
      set_byte(
        substring(substring($1 from 1688) from 17
          for octet_length(substring($1 from 1688)) - 48),
        345, 1)))
$$;
create or replace function pg_temp.pva_tree_reseal_graph(p_wire bytea, p_graph_body bytea)
returns bytea language sql immutable as $$
  select pg_temp.cdto_reseal($1,
    substring($1 from 17 for 1671)
    || pg_temp.cdto_reseal(substring($1 from 1688), $2))
$$;
create or replace function pg_temp.pva_tree_bad_parent(p_wire bytea)
returns bytea language sql immutable as $$
  select pg_temp.pva_tree_reseal_graph($1,
    set_byte(
      substring(substring($1 from 1688) from 17
        for octet_length(substring($1 from 1688)) - 48),
      1093, 4))
$$;
create or replace function pg_temp.pva_tree_bad_sibling(p_wire bytea)
returns bytea language sql immutable as $$
  select pg_temp.pva_tree_reseal_graph($1,
    set_byte(
      substring(substring($1 from 1688) from 17
        for octet_length(substring($1 from 1688)) - 48),
      1097, 2))
$$;

select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(:pva_empty_payload),
  'the digest-valid empty kind-7 envelope is rejected before immutable insertion'
);
select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(pg_temp.pva_missing_field_40(:pva_payload)),
  'a digest-valid kind-7 envelope with 39 fields is rejected'
);
select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(pg_temp.pva_field_1_wrong_type(:pva_payload)),
  'a digest-valid kind-7 envelope with an altered field type is rejected'
);
select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(pg_temp.pva_field_1_wrong_length(:pva_payload)),
  'a digest-valid kind-7 envelope with an altered fixed field length is rejected'
);
select pg_temp.assert_true(
  private.player_snapshot_v1_payload_valid(:pva_payload),
  'the real C PlayerSnapshotV1 fixture is accepted'
);
select pg_temp.assert_true(
  private.player_snapshot_v1_payload_valid(:pva_inventory_payload),
  'the real C fixture with one inventory node is accepted'
);
select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(pg_temp.pva_inventory_bad_shots(:pva_inventory_payload)),
  'a resealed inventory graph with shots_current above shots_max is rejected'
);
select pg_temp.assert_true(
  private.player_snapshot_v1_payload_valid(:pva_tree_inventory_payload),
  'the real C fixture with roots, siblings, and a grandchild is accepted'
);
select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(pg_temp.pva_tree_bad_parent(:pva_tree_inventory_payload)),
  'a resealed tree graph with an invalid preorder parent is rejected'
);
select pg_temp.assert_true(
  not private.player_snapshot_v1_payload_valid(pg_temp.pva_tree_bad_sibling(:pva_tree_inventory_payload)),
  'a resealed tree graph with an invalid sibling position is rejected'
);

select octet_length(:pva_payload) as snapshot_octets,
  encode(public.digest(:pva_payload, 'sha256'), 'hex') as snapshot_sha256
\gset pva_

select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'pva_first_request_sha256', repeat('b', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));

set local session authorization mud_writer_login;
set local role mud_writer;
select pg_temp.assert_true(private.m3_assert_writer_session(), 'writer identity is the actual login plus SET ROLE');

select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,48,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001',
  :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1',
  :'pva_empty_snapshot_sha256', :'pva_empty_payload'));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(null::uuid,%L::uuid,%L,%L,9,%L,%L,%s,%s)',
  'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,0,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,%s - 1,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'wrong-format', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1', repeat('c', 64), :'pva_snapshot_octets', :'pva_payload'));
select pg_temp.expect_state('22023', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,48,decode(''00'',''hex''))',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1', :'pva_snapshot_sha256'));

select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', repeat('b', 64), repeat('a', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,9,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('b', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));

select pg_temp.assert_true(
  (select outcome = 'RECORDED' from private.record_player_snapshot_v1_artifact_for_receipt(
    'a9500000-0000-0000-0000-000000000001'::uuid, 'c9500000-0000-0000-0000-000000000001'::uuid,
    :'pva_first_request_sha256', repeat('a', 64), 9, 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :pva_payload)),
  'first valid write is RECORDED'
);
select pg_temp.assert_true(
  (select outcome = 'EXACT_RETRY' from private.record_player_snapshot_v1_artifact_for_receipt(
    'a9500000-0000-0000-0000-000000000001'::uuid, 'c9500000-0000-0000-0000-000000000001'::uuid,
    :'pva_first_request_sha256', repeat('a', 64), 9, 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :pva_payload)),
  'exact retry is EXACT_RETRY'
);
select pg_temp.expect_state('P0001', format(
  'select * from private.record_player_snapshot_v1_artifact_for_receipt(%L::uuid,%L::uuid,%L,%L,10,%L,%L,%s,%s)',
  'a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', :'pva_first_request_sha256', repeat('a', 64), 'player-snapshot-v1', :'pva_snapshot_sha256', :'pva_snapshot_octets', :'pva_payload'));

reset role;
reset session authorization;

select pg_temp.assert_true(
  (select source_post_sha256 = repeat('a', 64)
      and source_octets = 9 and snapshot_sha256 = :'pva_snapshot_sha256'
      and snapshot_octets = :'pva_snapshot_octets' and snapshot_format = 'player-snapshot-v1'
      and payload = :pva_payload
    from private.game_character_player_snapshot_v1_artifacts
   where character_id = 'a9500000-0000-0000-0000-000000000001'::uuid
     and command_id = 'c9500000-0000-0000-0000-000000000001'::uuid),
  'receipt tuple and exact payload evidence are stored'
);

select pg_temp.expect_state('P0001', $$
  update private.game_character_player_snapshot_v1_artifacts set source_octets = 10;
$$);
select pg_temp.expect_state('P0001', $$
  delete from private.game_character_player_snapshot_v1_artifacts;
$$);

select pg_temp.assert_true(
  (select array_agg(artifact_state order by writer_revision)
     from private.list_player_snapshot_v1_artifact_reconciliation('pva-contract', 10)) = array['EXACT']::text[],
  'reconciliation reports EXACT'
);
select pg_temp.expect_state('22023', $$
  select * from private.list_player_snapshot_v1_artifact_reconciliation(null, 1)
$$);
select pg_temp.expect_state('22023', $$
  select * from private.list_player_snapshot_v1_artifact_reconciliation('PVA-invalid', 1)
$$);

select pg_temp.assert_true(
  has_function_privilege('mud_writer', 'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute')
  and not has_function_privilege('mud_writer_login', 'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute')
  and not has_function_privilege('anon', 'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute')
  and not has_function_privilege('authenticated', 'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute')
  and not has_function_privilege('service_role', 'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute')
  and not has_function_privilege('mud_writer', 'private.list_player_snapshot_v1_artifact_reconciliation(text,integer)', 'execute')
  and not has_table_privilege('mud_writer', 'private.game_character_player_snapshot_v1_artifacts', 'select,insert,update,delete')
  and not has_table_privilege('service_role', 'private.game_character_player_snapshot_v1_artifacts', 'select,insert,update,delete'),
  'least privilege denies direct table/browser/service role paths'
);

rollback;
