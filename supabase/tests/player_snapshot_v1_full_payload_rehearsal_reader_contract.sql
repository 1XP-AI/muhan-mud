\set ON_ERROR_STOP on

-- Catalog-only contract.  This test does not connect as the reader or apply a
-- database migration; disposable integration remains an operator action.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'full payload rehearsal reader contract failed: %', p_message;
  end if;
end;
$$;

\ir ../migrations/20261004000000_player_snapshot_v1_full_payload_rehearsal_reader.sql

create temp table pg_temp.full_payload_rehearsal_replay_baseline as
select
  (select rolpassword from pg_authid where rolname = 'mud_full_payload_rehearsal_reader_login') as password_hash,
  c.oid as relation_oid,
  c.relowner as relation_owner,
  n.oid as schema_oid,
  n.nspowner as schema_owner
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where c.oid in (
  'private.game_character_player_snapshot_v1_artifacts'::regclass,
  'private.game_character_shadow_receipts'::regclass
);

create role full_payload_rehearsal_contract_capability nologin noinherit;
grant full_payload_rehearsal_contract_capability to mud_full_payload_rehearsal_reader_login;
create role "full payload rehearsal ""quoted"" capability" nologin noinherit;
grant "full payload rehearsal ""quoted"" capability" to mud_full_payload_rehearsal_reader_login;
create role full_payload_rehearsal_contract_unrelated_parent nologin noinherit;
create role full_payload_rehearsal_contract_unrelated_member nologin noinherit;
grant full_payload_rehearsal_contract_unrelated_parent to full_payload_rehearsal_contract_unrelated_member;

select pg_temp.assert_true(
  pg_has_role('mud_full_payload_rehearsal_reader_login', 'full_payload_rehearsal_contract_capability', 'member')
  and pg_has_role('mud_full_payload_rehearsal_reader_login', 'full_payload_rehearsal_contract_capability', 'set')
  and pg_has_role('mud_full_payload_rehearsal_reader_login', 'full payload rehearsal "quoted" capability', 'member')
  and pg_has_role('mud_full_payload_rehearsal_reader_login', 'full payload rehearsal "quoted" capability', 'set')
  and pg_has_role(
    'full_payload_rehearsal_contract_unrelated_member',
    'full_payload_rehearsal_contract_unrelated_parent',
    'set'
  ),
  'reader and unrelated memberships must exist before replay normalizes only the reader'
);

\ir ../migrations/20261004000000_player_snapshot_v1_full_payload_rehearsal_reader.sql
\ir ../migrations/20261004000000_player_snapshot_v1_full_payload_rehearsal_reader.sql

select pg_temp.assert_true(
  exists (
    select 1 from pg_roles r where r.rolname = 'mud_full_payload_rehearsal_reader_login'
      and r.rolcanlogin and not r.rolinherit and not r.rolsuper and not r.rolcreaterole
      and not r.rolcreatedb and not r.rolreplication and not r.rolbypassrls
  ) and not exists (
    select 1 from pg_auth_members m
     where m.member = 'mud_full_payload_rehearsal_reader_login'::regrole
  ),
  'reader must be a normalized nonprivileged LOGIN without role membership'
);

select pg_temp.assert_true(
  not pg_has_role('mud_full_payload_rehearsal_reader_login', 'full_payload_rehearsal_contract_capability', 'set')
  and not pg_has_role('mud_full_payload_rehearsal_reader_login', 'full payload rehearsal "quoted" capability', 'set')
  and pg_has_role(
    'full_payload_rehearsal_contract_unrelated_member',
    'full_payload_rehearsal_contract_unrelated_parent',
    'member'
  )
  and pg_has_role(
    'full_payload_rehearsal_contract_unrelated_member',
    'full_payload_rehearsal_contract_unrelated_parent',
    'set'
  )
  and (select password_hash from pg_temp.full_payload_rehearsal_replay_baseline) is not distinct from
    (select rolpassword from pg_authid where rolname = 'mud_full_payload_rehearsal_reader_login'),
  'replay must remove only reader memberships and preserve its password hash'
);

select pg_temp.assert_true(
  not exists (
    select 1
      from pg_temp.full_payload_rehearsal_replay_baseline baseline
      join pg_class c on c.oid = baseline.relation_oid
      join pg_namespace n on n.oid = c.relnamespace
     where c.relowner <> baseline.relation_owner
        or n.oid <> baseline.schema_oid
        or n.nspowner <> baseline.schema_owner
  ),
  'replay must not change either joined relation or private schema ownership'
);

select pg_temp.assert_true(
  (select rolconfig @> array[
    'default_transaction_read_only=on', 'statement_timeout=5s', 'lock_timeout=1s',
    'idle_in_transaction_session_timeout=5s', 'search_path=pg_catalog'
  ]::text[] and cardinality(rolconfig) = 5
   from pg_roles where rolname = 'mud_full_payload_rehearsal_reader_login'),
  'reader defaults must be exactly read-only plus bounded timeouts and search path'
);

select pg_temp.assert_true(
  has_database_privilege('mud_full_payload_rehearsal_reader_login', current_database(), 'connect')
  and has_schema_privilege('mud_full_payload_rehearsal_reader_login', 'private', 'usage'),
  'reader must connect and use only the private schema path required for rehearsal'
);

select pg_temp.assert_true(
  (select array_agg(column_name::text order by column_name) = array[
    'character_id', 'command_id', 'legacy_name_key', 'payload', 'receipt_acknowledged_at',
    'receipt_request_sha256', 'snapshot_format', 'snapshot_octets', 'snapshot_sha256',
    'source_octets', 'source_post_sha256', 'storage_format', 'world_id', 'writer_epoch',
    'writer_instance_id', 'writer_revision'
  ]::text[]
   from information_schema.column_privileges
   where grantee = 'mud_full_payload_rehearsal_reader_login' and table_schema = 'private'
     and table_name = 'game_character_player_snapshot_v1_artifacts' and privilege_type = 'SELECT')
  and (select array_agg(a.attname::text order by a.attname) = array[
    'character_id', 'command_id', 'legacy_name_key', 'payload', 'receipt_acknowledged_at',
    'receipt_request_sha256', 'snapshot_format', 'snapshot_octets', 'snapshot_sha256',
    'source_octets', 'source_post_sha256', 'storage_format', 'world_id', 'writer_epoch',
    'writer_instance_id', 'writer_revision'
  ]::text[]
   from pg_attribute a
   where a.attrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_full_payload_rehearsal_reader_login', a.attrelid, a.attname, 'select'))
  and has_column_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'payload', 'select')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'select'),
  'reader must receive exactly the immutable artifact tuple and payload columns'
);

select pg_temp.assert_true(
  (select array_agg(column_name::text order by column_name) = array[
    'acknowledged_at', 'character_id', 'command_id', 'legacy_name_key', 'post_sha256',
    'request_sha256', 'storage_format', 'world_id', 'writer_epoch', 'writer_instance_id',
    'writer_revision'
  ]::text[]
   from information_schema.column_privileges
   where grantee = 'mud_full_payload_rehearsal_reader_login' and table_schema = 'private'
     and table_name = 'game_character_shadow_receipts' and privilege_type = 'SELECT')
  and (select array_agg(a.attname::text order by a.attname) = array[
    'acknowledged_at', 'character_id', 'command_id', 'legacy_name_key', 'post_sha256',
    'request_sha256', 'storage_format', 'world_id', 'writer_epoch', 'writer_instance_id',
    'writer_revision'
  ]::text[]
   from pg_attribute a
   where a.attrelid = 'private.game_character_shadow_receipts'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_full_payload_rehearsal_reader_login', a.attrelid, a.attname, 'select'))
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'select'),
  'reader must receive exactly the acknowledged receipt tuple columns'
);

select pg_temp.assert_true(
  (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass)
  and (select relrowsecurity from pg_class where oid = 'private.game_character_shadow_receipts'::regclass)
  and exists (
    select 1 from pg_policy p
     where p.polrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
       and p.polname = 'mud_full_payload_rehearsal_artifact_select'
       and p.polcmd = 'r'
       and p.polroles = array['mud_full_payload_rehearsal_reader_login'::regrole::oid]
       and pg_get_expr(p.polqual, p.polrelid) = 'true'
  )
  and exists (
    select 1 from pg_policy p
     where p.polrelid = 'private.game_character_shadow_receipts'::regclass
       and p.polname = 'mud_full_payload_rehearsal_receipt_select'
       and p.polcmd = 'r'
       and p.polroles = array['mud_full_payload_rehearsal_reader_login'::regrole::oid]
       and pg_get_expr(p.polqual, p.polrelid) = 'true'
  ),
  'RLS must expose the two exact joined SELECT paths to only this reader login'
);

select pg_temp.assert_true(
  not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'insert')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'update')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'delete')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'truncate')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'references')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'trigger')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'insert')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'update')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'delete')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'truncate')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'references')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'trigger')
  and not has_function_privilege('mud_full_payload_rehearsal_reader_login',
      'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute'),
  'reader must have no mutation or artifact RPC privilege'
);

rollback;
