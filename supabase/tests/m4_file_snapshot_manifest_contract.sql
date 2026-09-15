\set ON_ERROR_STOP on

-- Disposable PostgreSQL 17 contract for the manifest-only M4 boundary.
-- The harness first runs this through migration 130 and requires RED, then
-- applies migration 140 and requires every assertion below to pass.
begin;

create or replace function pg_temp.assert_true(
  p_condition boolean,
  p_message text
)
returns void
language plpgsql
as $$
begin
  if p_condition is not true then
    raise exception 'M4 file snapshot manifest contract failed: %', p_message;
  end if;
end;
$$;

create or replace function pg_temp.expect_state(
  p_sqlstate text,
  p_sql text
)
returns void
language plpgsql
as $$
begin
  begin
    execute p_sql;
  exception when others then
    if sqlstate = p_sqlstate then
      return;
    end if;
    raise;
  end;
  raise exception using
    errcode = 'P0002',
    message = format(
      'M4 file snapshot manifest contract failed: expected SQLSTATE %s',
      p_sqlstate
    );
end;
$$;

create or replace function pg_temp.authority_snapshot(
  p_world_id text,
  p_character_id uuid
)
returns text
language sql
stable
set search_path = pg_catalog, public, private
as $$
  select jsonb_build_object(
    'character', (
      select to_jsonb(c)
        from public.game_characters c
       where c.id = p_character_id
    ),
    'head', (
      select to_jsonb(h)
        from private.game_character_legacy_heads h
       where h.character_id = p_character_id
    ),
    'receipts', (
      select coalesce(jsonb_agg(to_jsonb(r) order by r.command_id), '[]'::jsonb)
        from private.game_character_shadow_receipts r
       where r.character_id = p_character_id
    ),
    'writer_epoch', (
      select to_jsonb(e)
        from private.game_character_writer_epochs e
       where e.world_id = p_world_id
    ),
    'writer_fences', (
      select coalesce(jsonb_agg(to_jsonb(f) order by f.writer_epoch), '[]'::jsonb)
        from private.game_character_writer_epoch_fences f
       where f.world_id = p_world_id
    ),
    'legacy_snapshot_read_model', (
      select coalesce(jsonb_agg(to_jsonb(s) order by s.revision), '[]'::jsonb)
        from private.game_character_snapshots s
       where s.character_id = p_character_id
    )
  )::text;
$$;

create or replace function pg_temp.manifest_snapshot(p_character_id uuid)
returns text
language sql
stable
set search_path = pg_catalog, private
as $$
  select coalesce(
    jsonb_agg(to_jsonb(m) order by m.writer_revision, m.command_id),
    '[]'::jsonb
  )::text
    from private.game_character_m4_file_snapshot_manifests m
   where m.character_id = p_character_id;
$$;

select pg_temp.assert_true(
  to_regprocedure(
    'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)'
  ) is not null
  and to_regprocedure(
    'private.list_m4_file_snapshot_manifest_reconciliation(text,integer)'
  ) is not null,
  'both exact M4 RPC signatures must exist'
);

select pg_temp.assert_true(
  (
    select p.prosecdef
       and p.proconfig = array['search_path=pg_catalog, private']::text[]
      from pg_proc p
     where p.oid =
       'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)'::regprocedure
  )
  and (
    select p.prosecdef
       and p.proconfig = array['search_path=pg_catalog, private']::text[]
      from pg_proc p
     where p.oid =
       'private.list_m4_file_snapshot_manifest_reconciliation(text,integer)'::regprocedure
  ),
  'both definer RPCs must pin their search path'
);

select pg_temp.assert_true(
  (
    select c.relrowsecurity
      from pg_class c
     where c.oid =
       'private.game_character_m4_file_snapshot_manifests'::regclass
  )
  and not exists (
    select 1
      from pg_attribute a
     where a.attrelid =
       'private.game_character_m4_file_snapshot_manifests'::regclass
       and a.atttypid = 'bytea'::regtype
       and not a.attisdropped
  ),
  'the evidence table must use RLS and contain no bytea/player payload column'
);

insert into public.game_characters (
  id,
  world_id,
  legacy_name,
  legacy_name_key,
  legacy_shard,
  lifecycle,
  storage_format
) values (
  'a9400000-0000-0000-0000-000000000001',
  'm4-manifest',
  'M4hero',
  'M4hero',
  substr(encode(public.digest(convert_to('M4hero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed',
  1
);

insert into private.game_character_snapshots (
  character_id,
  revision,
  storage_format,
  blob_ref,
  sha256,
  saved_at
) values (
  'a9400000-0000-0000-0000-000000000001',
  41,
  1,
  'pre-m4-sentinel',
  repeat('f', 64),
  '2026-09-01 00:00:00+00'
);

insert into private.game_character_legacy_heads (
  character_id,
  head_state,
  storage_format,
  revision
) values (
  'a9400000-0000-0000-0000-000000000001',
  'absent',
  1,
  0
);

select * from private.acquire_game_world_writer_epoch(
  'm4-manifest',
  'b9400000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);

select private.game_character_shadow_request_sha256(
  'm4-manifest',
  'a9400000-0000-0000-0000-000000000001'::uuid,
  'M4hero',
  substr(encode(public.digest(convert_to('M4hero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9400000-0000-0000-0000-000000000001'::uuid,
  'b9400000-0000-0000-0000-000000000001'::uuid,
  1::bigint,
  1::bigint,
  'absent',
  null,
  repeat('a', 64),
  1::smallint
) as request_sha256
\gset m4_first_

select * from private.record_legacy_published_receipt(
  'm4-manifest',
  'M4hero',
  'a9400000-0000-0000-0000-000000000001'::uuid,
  'c9400000-0000-0000-0000-000000000001'::uuid,
  'b9400000-0000-0000-0000-000000000001'::uuid,
  :'m4_first_request_sha256',
  1::bigint,
  1::bigint,
  'absent',
  null,
  repeat('a', 64),
  1::smallint
);

select pg_temp.assert_true(
  (
    select count(*) = 1
       and min(manifest_state) = 'MISSING'
       and min(snapshot_sha256) is null
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'm4-manifest', 10
      )
  ),
  'an acknowledged receipt without a manifest must report MISSING'
);

select pg_temp.authority_snapshot(
  'm4-manifest',
  'a9400000-0000-0000-0000-000000000001'
) as state
\gset m4_before_record_

select pg_temp.expect_state(
  'P0001',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);

set local session authorization mud_writer_login;

select pg_temp.expect_state(
  '42501',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);

set local role mud_writer;

select pg_temp.assert_true(
  current_user = 'mud_writer'
  and session_user = 'mud_writer_login'
  and private.m3_assert_writer_session(),
  'the valid M4 caller must satisfy the existing M3 writer session contract'
);

select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(null::uuid,%L::uuid,%L,%L,%L,64)',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,null::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,null,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,null,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,null,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1'
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,null)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,0)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,67108865)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    upper(:'m4_first_request_sha256'),
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'player-cdto-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  '22023',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('A', 64)
  )
);
select pg_temp.expect_state(
  'P0001',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000099',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);
select pg_temp.expect_state(
  'P0001',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,64)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    repeat('b', 64),
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);

select pg_temp.assert_true(
  (
    select outcome = 'RECORDED'
      from private.record_m4_file_snapshot_manifest_for_receipt(
        'a9400000-0000-0000-0000-000000000001'::uuid,
        'c9400000-0000-0000-0000-000000000001'::uuid,
        :'m4_first_request_sha256',
        'legacy-file-manifest-v1',
        repeat('a', 64),
        67108864
      )
  ),
  'the first exact receipt manifest must return RECORDED at the 64 MiB bound'
);

reset role;
reset session authorization;

select pg_temp.assert_true(
  :'m4_before_record_state' = pg_temp.authority_snapshot(
    'm4-manifest',
    'a9400000-0000-0000-0000-000000000001'
  ),
  'recording a manifest must not mutate receipt, head, writer, identity, or the old snapshot read model'
);

select pg_temp.manifest_snapshot(
  'a9400000-0000-0000-0000-000000000001'
) as state
\gset m4_after_record_

select recorded_at::text as recorded_at
  from private.game_character_m4_file_snapshot_manifests
 where character_id = 'a9400000-0000-0000-0000-000000000001'
   and command_id = 'c9400000-0000-0000-0000-000000000001'
\gset m4_first_manifest_

set local session authorization mud_writer_login;
set local role mud_writer;

select pg_temp.assert_true(
  (
    select outcome = 'EXACT_RETRY'
      from private.record_m4_file_snapshot_manifest_for_receipt(
        'a9400000-0000-0000-0000-000000000001'::uuid,
        'c9400000-0000-0000-0000-000000000001'::uuid,
        :'m4_first_request_sha256',
        'legacy-file-manifest-v1',
        repeat('a', 64),
        67108864
      )
  ),
  'an exact retry must be read-only success'
);

select pg_temp.expect_state(
  'P0001',
  format(
    'select * from private.record_m4_file_snapshot_manifest_for_receipt(%L::uuid,%L::uuid,%L,%L,%L,1)',
    'a9400000-0000-0000-0000-000000000001',
    'c9400000-0000-0000-0000-000000000001',
    :'m4_first_request_sha256',
    'legacy-file-manifest-v1',
    repeat('a', 64)
  )
);

reset role;
reset session authorization;

select pg_temp.assert_true(
  :'m4_after_record_state' = pg_temp.manifest_snapshot(
    'a9400000-0000-0000-0000-000000000001'
  )
  and :'m4_first_manifest_recorded_at' = (
    select recorded_at::text
      from private.game_character_m4_file_snapshot_manifests
     where character_id = 'a9400000-0000-0000-0000-000000000001'
       and command_id = 'c9400000-0000-0000-0000-000000000001'
  ),
  'exact and conflicting retries must preserve the row and recorded_at byte-for-byte'
);

select pg_temp.expect_state(
  'P0001',
  $$
    update private.game_character_m4_file_snapshot_manifests
       set snapshot_octets = 1
     where character_id = 'a9400000-0000-0000-0000-000000000001'::uuid
  $$
);
select pg_temp.expect_state(
  'P0001',
  $$
    delete from private.game_character_m4_file_snapshot_manifests
     where character_id = 'a9400000-0000-0000-0000-000000000001'::uuid
  $$
);

select pg_temp.assert_true(
  :'m4_after_record_state' = pg_temp.manifest_snapshot(
    'a9400000-0000-0000-0000-000000000001'
  ),
  'immutable UPDATE and DELETE rejection must leave the manifest unchanged'
);

select private.game_character_shadow_request_sha256(
  'm4-manifest',
  'a9400000-0000-0000-0000-000000000001'::uuid,
  'M4hero',
  substr(encode(public.digest(convert_to('M4hero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9400000-0000-0000-0000-000000000002'::uuid,
  'b9400000-0000-0000-0000-000000000001'::uuid,
  1::bigint,
  2::bigint,
  'existing',
  repeat('a', 64),
  repeat('b', 64),
  1::smallint
) as request_sha256
\gset m4_second_

select * from private.record_legacy_published_receipt(
  'm4-manifest',
  'M4hero',
  'a9400000-0000-0000-0000-000000000001'::uuid,
  'c9400000-0000-0000-0000-000000000002'::uuid,
  'b9400000-0000-0000-0000-000000000001'::uuid,
  :'m4_second_request_sha256',
  1::bigint,
  2::bigint,
  'existing',
  repeat('a', 64),
  repeat('b', 64),
  1::smallint
);

select private.seal_game_world_writer_epoch(
  'm4-manifest',
  'b9400000-0000-0000-0000-000000000001'::uuid,
  1::bigint
);
update private.game_character_writer_epochs
   set issued_at = clock_timestamp() - interval '2 seconds',
       expires_at = clock_timestamp() - interval '1 second'
 where world_id = 'm4-manifest';
select * from private.acquire_game_world_writer_epoch(
  'm4-manifest',
  'b9400000-0000-0000-0000-000000000002'::uuid,
  clock_timestamp() + interval '3 minutes'
);

select pg_temp.authority_snapshot(
  'm4-manifest',
  'a9400000-0000-0000-0000-000000000001'
) as state
\gset m4_after_successor_

set local session authorization mud_writer_login;
set local role mud_writer;

select pg_temp.assert_true(
  (
    select outcome = 'EXACT_RETRY'
      from private.record_m4_file_snapshot_manifest_for_receipt(
        'a9400000-0000-0000-0000-000000000001'::uuid,
        'c9400000-0000-0000-0000-000000000001'::uuid,
        :'m4_first_request_sha256',
        'legacy-file-manifest-v1',
        repeat('a', 64),
        67108864
      )
  ),
  'receipt-anchored backlog retry must survive head advance, seal, expiry, and successor installation'
);

reset role;
reset session authorization;

select pg_temp.assert_true(
  :'m4_after_successor_state' = pg_temp.authority_snapshot(
    'm4-manifest',
    'a9400000-0000-0000-0000-000000000001'
  ),
  'a historical manifest retry must not change current writer or gameplay authority'
);

select pg_temp.assert_true(
  (
    select array_agg(manifest_state order by writer_revision)
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'm4-manifest', 10
      )
  ) = array['EXACT', 'MISSING']::text[],
  'reconciliation must distinguish the exact first revision from the missing second revision'
);

select to_jsonb(array_agg(r order by writer_revision, command_id))::text as state
  from private.list_m4_file_snapshot_manifest_reconciliation(
    'm4-manifest', 10
  ) r
\gset m4_reconcile_first_

select pg_temp.assert_true(
  :'m4_reconcile_first_state' = (
    select to_jsonb(array_agg(r order by writer_revision, command_id))::text
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'm4-manifest', 10
      ) r
  ),
  'two read-only reconciliation runs must return deterministic metadata'
);

select pg_temp.expect_state(
  '22023',
  'select * from private.list_m4_file_snapshot_manifest_reconciliation(null, 1)'
);
select pg_temp.expect_state(
  '22023',
  $$
    select *
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'M4-invalid', 1
      )
  $$
);
select pg_temp.expect_state(
  '22023',
  $$
    select *
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'm4-manifest', null
      )
  $$
);
select pg_temp.expect_state(
  '22023',
  $$
    select *
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'm4-manifest', 0
      )
  $$
);

select pg_temp.assert_true(
  has_function_privilege(
    'mud_writer',
    'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)',
    'execute'
  )
  and not has_function_privilege(
    'mud_writer',
    'private.list_m4_file_snapshot_manifest_reconciliation(text,integer)',
    'execute'
  )
  and not has_function_privilege(
    'mud_writer_login',
    'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)',
    'execute'
  )
  and not has_function_privilege(
    'anon',
    'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)',
    'execute'
  )
  and not has_function_privilege(
    'authenticated',
    'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)',
    'execute'
  )
  and not has_function_privilege(
    'service_role',
    'private.record_m4_file_snapshot_manifest_for_receipt(uuid,uuid,text,text,text,bigint)',
    'execute'
  )
  and not has_function_privilege(
    'anon',
    'private.list_m4_file_snapshot_manifest_reconciliation(text,integer)',
    'execute'
  )
  and not has_function_privilege(
    'authenticated',
    'private.list_m4_file_snapshot_manifest_reconciliation(text,integer)',
    'execute'
  )
  and not has_function_privilege(
    'service_role',
    'private.list_m4_file_snapshot_manifest_reconciliation(text,integer)',
    'execute'
  ),
  'only mud_writer may execute the record RPC and reconciliation must remain owner/admin-only'
);

select pg_temp.assert_true(
  not has_table_privilege(
    'mud_writer',
    'private.game_character_m4_file_snapshot_manifests',
    'select,insert,update,delete'
  )
  and not has_table_privilege(
    'mud_writer_login',
    'private.game_character_m4_file_snapshot_manifests',
    'select,insert,update,delete'
  )
  and not has_table_privilege(
    'anon',
    'private.game_character_m4_file_snapshot_manifests',
    'select,insert,update,delete'
  )
  and not has_table_privilege(
    'authenticated',
    'private.game_character_m4_file_snapshot_manifests',
    'select,insert,update,delete'
  )
  and not has_table_privilege(
    'service_role',
    'private.game_character_m4_file_snapshot_manifests',
    'select,insert,update,delete'
  ),
  'no runtime/browser/service role may access the evidence table directly'
);

select pg_temp.assert_true(
  (
    select blob_ref = 'pre-m4-sentinel'
       and sha256 = repeat('f', 64)
       and revision = 41
      from private.game_character_snapshots
     where character_id = 'a9400000-0000-0000-0000-000000000001'
  ),
  'M4 must not overload or mutate the pre-existing optional snapshot read model'
);

-- Simulate privileged storage corruption only inside this rolled-back
-- disposable contract. The read-only report must expose, never repair, it.
update private.game_character_shadow_receipts
   set acknowledged_at = acknowledged_at + interval '1 microsecond'
 where character_id = 'a9400000-0000-0000-0000-000000000001'
   and command_id = 'c9400000-0000-0000-0000-000000000001';

select pg_temp.assert_true(
  (
    select count(*) = 1
      from private.list_m4_file_snapshot_manifest_reconciliation(
        'm4-manifest', 10
      )
     where command_id = 'c9400000-0000-0000-0000-000000000001'
       and manifest_state = 'INCONSISTENT'
  ),
  'receipt/manifest drift must report INCONSISTENT without repair'
);

rollback;
