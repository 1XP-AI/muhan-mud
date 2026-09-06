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
  (select rolconfig @> array[
    'default_transaction_read_only=on', 'statement_timeout=5s', 'lock_timeout=1s',
    'idle_in_transaction_session_timeout=5s', 'search_path=pg_catalog'
  ]::text[] and cardinality(rolconfig) = 5
   from pg_roles where rolname = 'mud_full_payload_rehearsal_reader_login'),
  'reader defaults must be exactly read-only plus bounded timeouts and search path'
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
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'select'),
  'reader must receive exactly the acknowledged receipt tuple columns'
);

select pg_temp.assert_true(
  not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'insert')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'update')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'delete')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'insert')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'update')
  and not has_table_privilege('mud_full_payload_rehearsal_reader_login',
      'private.game_character_shadow_receipts', 'delete')
  and not has_function_privilege('mud_full_payload_rehearsal_reader_login',
      'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute'),
  'reader must have no mutation or artifact RPC privilege'
);

rollback;
