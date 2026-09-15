\set ON_ERROR_STOP on

-- Contract for 20260917000000_player_snapshot_v1_replay_reader.sql.  This is
-- catalog-only: the companion PG17 integration proves an actual reader login.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'M5e replay reader contract failed: %', p_message;
  end if;
end;
$$;

select pg_temp.assert_true(
  exists (
    select 1 from pg_roles r
     where r.rolname = 'mud_replay_reader_login'
       and r.rolcanlogin
       and not r.rolinherit
       and not r.rolsuper
       and not r.rolcreaterole
       and not r.rolcreatedb
       and not r.rolreplication
       and not r.rolbypassrls
  ),
  'reader must be a normalized nonprivileged LOGIN role'
);

select pg_temp.assert_true(
  not exists (
    select 1
      from pg_auth_members membership
     where membership.member = 'mud_replay_reader_login'::regrole
  ),
  'reader must have no direct membership in any role'
);

create temp table pg_temp.m5e_replay_reader_replay_baseline as
select
  (select rolpassword from pg_authid where rolname = 'mud_replay_reader_login') as password_hash,
  c.oid as artifact_relation_oid,
  c.relowner as artifact_relation_owner,
  n.oid as artifact_schema_oid,
  n.nspowner as artifact_schema_owner
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where c.oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass;

create role m5e_replay_reader_contract_capability nologin noinherit;
grant m5e_replay_reader_contract_capability to mud_replay_reader_login;
create role "m5e replay reader ""quoted"" capability" nologin noinherit;
grant "m5e replay reader ""quoted"" capability" to mud_replay_reader_login;

grant mud_writer to mud_writer_login;
create role m5e_replay_reader_contract_unrelated_parent nologin noinherit;
create role m5e_replay_reader_contract_unrelated_member nologin noinherit;
grant m5e_replay_reader_contract_unrelated_parent to m5e_replay_reader_contract_unrelated_member;

select pg_temp.assert_true(
  pg_has_role('mud_replay_reader_login', 'm5e_replay_reader_contract_capability', 'member')
  and pg_has_role('mud_replay_reader_login', 'm5e_replay_reader_contract_capability', 'set'),
  'third NOLOGIN capability membership must demonstrate the SET ROLE route before replay'
);

select pg_temp.assert_true(
  pg_has_role('mud_replay_reader_login', 'm5e replay reader "quoted" capability', 'member')
  and pg_has_role('mud_replay_reader_login', 'm5e replay reader "quoted" capability', 'set')
  and pg_has_role('mud_writer_login', 'mud_writer', 'member')
  and pg_has_role('mud_writer_login', 'mud_writer', 'set')
  and pg_has_role(
    'm5e_replay_reader_contract_unrelated_member',
    'm5e_replay_reader_contract_unrelated_parent',
    'member'
  )
  and pg_has_role(
    'm5e_replay_reader_contract_unrelated_member',
    'm5e_replay_reader_contract_unrelated_parent',
    'set'
  ),
  'quoted reader membership and non-reader memberships must exist before replay'
);

\ir ../migrations/20260917000000_player_snapshot_v1_replay_reader.sql

select pg_temp.assert_true(
  not exists (
    select 1
      from pg_auth_members membership
     where membership.member = 'mud_replay_reader_login'::regrole
  )
  and not pg_has_role('mud_replay_reader_login', 'm5e_replay_reader_contract_capability', 'set')
  and not pg_has_role('mud_replay_reader_login', 'm5e replay reader "quoted" capability', 'set')
  and pg_has_role('mud_writer_login', 'mud_writer', 'member')
  and pg_has_role('mud_writer_login', 'mud_writer', 'set')
  and pg_has_role(
    'm5e_replay_reader_contract_unrelated_member',
    'm5e_replay_reader_contract_unrelated_parent',
    'member'
  )
  and pg_has_role(
    'm5e_replay_reader_contract_unrelated_member',
    'm5e_replay_reader_contract_unrelated_parent',
    'set'
  ),
  'migration replay must remove only reader memberships and SET ROLE routes'
);

select pg_temp.assert_true(
  (select password_hash from pg_temp.m5e_replay_reader_replay_baseline) is not distinct from
  (select rolpassword from pg_authid where rolname = 'mud_replay_reader_login'),
  'migration replay must preserve the reader password hash'
);

select pg_temp.assert_true(
  exists (
    select 1
      from pg_temp.m5e_replay_reader_replay_baseline baseline
      join pg_class c on c.oid = baseline.artifact_relation_oid
      join pg_namespace n on n.oid = c.relnamespace
     where c.relowner = baseline.artifact_relation_owner
       and n.oid = baseline.artifact_schema_oid
       and n.nspowner = baseline.artifact_schema_owner
  ),
  'migration replay must not change artifact relation or schema ownership'
);

select pg_temp.assert_true(
  (select rolconfig @> array[
      'default_transaction_read_only=on',
      'statement_timeout=5s',
      'lock_timeout=1s',
      'idle_in_transaction_session_timeout=5s',
      'search_path=pg_catalog'
    ]::text[] and cardinality(rolconfig) = 5
   from pg_roles where rolname = 'mud_replay_reader_login'),
  'reader defaults must be exactly read-only plus the bounded M3 timeouts and search path'
);

select pg_temp.assert_true(
  has_database_privilege('mud_replay_reader_login', current_database(), 'connect')
  and has_schema_privilege('mud_replay_reader_login', 'private', 'usage'),
  'reader must connect and use the private schema'
);

select pg_temp.assert_true(
  (select array_agg(column_name::text order by column_name) = array[
      'character_id', 'command_id', 'receipt_request_sha256', 'snapshot_format',
      'snapshot_octets', 'snapshot_sha256', 'source_post_sha256'
    ]::text[]
   from information_schema.column_privileges
   where grantee = 'mud_replay_reader_login'
     and table_schema = 'private'
     and table_name = 'game_character_player_snapshot_v1_artifacts'
     and privilege_type = 'SELECT')
  and (select array_agg(a.attname::text order by a.attname) = array[
      'character_id', 'command_id', 'receipt_request_sha256', 'snapshot_format',
      'snapshot_octets', 'snapshot_sha256', 'source_post_sha256'
    ]::text[]
       from pg_attribute a
      where a.attrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
        and a.attnum > 0
        and not a.attisdropped
        and has_column_privilege('mud_replay_reader_login', a.attrelid, a.attname, 'select'))
  and not has_table_privilege('mud_replay_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'select')
  and has_column_privilege('mud_replay_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'command_id', 'select')
  and not has_column_privilege('mud_replay_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'payload', 'select')
  and not has_column_privilege('mud_replay_reader_login',
      'private.game_character_player_snapshot_v1_artifacts', 'world_id', 'select'),
  'reader must receive exactly the M5d evidence columns, never payload or unrelated metadata'
);

select pg_temp.assert_true(
  exists (
    select 1 from pg_policy p
     where p.polrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
       and p.polname = 'mud_replay_reader_player_snapshot_v1_metadata_select'
       and p.polcmd = 'r'
       and p.polroles = array['mud_replay_reader_login'::regrole::oid]
       and pg_get_expr(p.polqual, p.polrelid) = 'true'
  ),
  'RLS must allow this reader metadata SELECT path only'
);

select pg_temp.assert_true(
  not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'insert')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'update')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'delete')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'truncate')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'references')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'trigger')
  and not has_function_privilege('mud_replay_reader_login',
    'private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)', 'execute'),
  'reader must have no mutation table or artifact RPC privilege'
);

rollback;
