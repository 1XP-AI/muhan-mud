\set ON_ERROR_STOP on

-- Catalog contract for the additive level-projection replay-reader migration.
begin;

create or replace function pg_temp.assert_true(p_condition boolean, p_message text)
returns void language plpgsql as $$
begin
  if p_condition is not true then
    raise exception 'level projection replay reader contract failed: %', p_message;
  end if;
end;
$$;

\ir ../migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql

select pg_temp.assert_true(
  (select array_agg(column_name::text order by column_name) = array[
      'character_id', 'command_id', 'raw_level_u8', 'receipt_request_sha256',
      'snapshot_octets', 'snapshot_sha256', 'source_post_sha256'
    ]::text[]
   from information_schema.column_privileges
   where grantee = 'mud_replay_reader_login'
     and table_schema = 'private'
     and table_name = 'game_character_player_snapshot_v1_level_projections'
     and privilege_type = 'SELECT')
  and (select array_agg(a.attname::text order by a.attname) = array[
      'character_id', 'command_id', 'raw_level_u8', 'receipt_request_sha256',
      'snapshot_octets', 'snapshot_sha256', 'source_post_sha256'
    ]::text[]
       from pg_attribute a
      where a.attrelid = 'private.game_character_player_snapshot_v1_level_projections'::regclass
        and a.attnum > 0
        and not a.attisdropped
        and has_column_privilege('mud_replay_reader_login', a.attrelid, a.attname, 'select'))
  and not has_table_privilege('mud_replay_reader_login',
      'private.game_character_player_snapshot_v1_level_projections', 'select')
  and not has_column_privilege('mud_replay_reader_login',
      'private.game_character_player_snapshot_v1_level_projections', 'writer_revision', 'select'),
  'reader must receive exactly the closed level comparison metadata columns'
);

select pg_temp.assert_true(
  exists (
    select 1 from pg_policy p
     where p.polrelid = 'private.game_character_player_snapshot_v1_level_projections'::regclass
       and p.polname = 'mud_replay_reader_player_snapshot_v1_level_projection_metadata_select'
       and p.polcmd = 'r'
       and p.polroles = array['mud_replay_reader_login'::regrole::oid]
       and pg_get_expr(p.polqual, p.polrelid) = 'true'
  ),
  'RLS must permit only the reader metadata SELECT path'
);

select pg_temp.assert_true(
  not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'insert')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'update')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'delete')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'truncate')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'references')
  and not has_table_privilege('mud_replay_reader_login',
    'private.game_character_player_snapshot_v1_level_projections', 'trigger')
  and not has_function_privilege('mud_replay_reader_login',
    'private.record_player_snapshot_v1_level_projection_for_receipt(uuid,uuid,text,text,bigint)', 'execute'),
  'reader must have no level projection mutation or RPC privilege'
);

rollback;
