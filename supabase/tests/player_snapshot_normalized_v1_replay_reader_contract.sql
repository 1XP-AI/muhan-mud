\set ON_ERROR_STOP on
-- Run after all migrations in a disposable PostgreSQL database as its owner.
-- This contract does not execute the TypeScript SELECT; the live adapter test
-- must additionally exercise that query with the dedicated login.
begin;

create or replace function pg_temp.assert_normalized_reader(ok boolean, detail text)
returns void language plpgsql as $$
begin
  if ok is not true then raise exception 'normalized reader contract: %', detail; end if;
end;
$$;

select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_roles where rolname = 'mud_normalized_replay_reader_login'
    and rolcanlogin and not rolsuper and not rolinherit and not rolcreatedb
    and not rolcreaterole and not rolreplication and not rolbypassrls
    and 'default_transaction_read_only=on' = any(rolconfig)),
  'dedicated read-only direct login');
select pg_temp.assert_normalized_reader(
  not exists (select 1 from pg_auth_members where member = 'mud_normalized_replay_reader_login'::regrole),
  'no inherited or assumable roles');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['canonical_digest', 'character_id', 'command_id', 'experience', 'gold', 'hp_current', 'hp_max', 'item_count', 'level', 'mp_current', 'mp_max', 'projection_algorithm', 'projection_format', 'projection_version', 'receipt_request_sha256', 'snapshot_octets', 'snapshot_sha256', 'source_octets', 'source_post_sha256', 'writer_epoch', 'writer_instance_id', 'writer_revision']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_player_snapshot_normalized_v1_projections'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_player_snapshot_normalized_v1_projections: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_player_snapshot_normalized_v1_projections'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_normalized_v1_projections'::regclass),
  'game_character_player_snapshot_normalized_v1_projections: RLS select policy');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['character_id', 'command_id', 'current_value', 'last_used', 'max_value', 'slot']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_player_snapshot_normalized_v1_projection_daily'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_player_snapshot_normalized_v1_projection_daily: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_player_snapshot_normalized_v1_projection_daily'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_normalized_v1_projection_daily'::regclass),
  'game_character_player_snapshot_normalized_v1_projection_daily: RLS select policy');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['character_id', 'command_id', 'interval_value', 'last_used', 'misc', 'slot']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_player_snapshot_normalized_v1_projection_timers'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_player_snapshot_normalized_v1_projection_timers: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_player_snapshot_normalized_v1_projection_timers'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_normalized_v1_projection_timers'::regclass),
  'game_character_player_snapshot_normalized_v1_projection_timers: RLS select policy');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['adjustment', 'armor', 'character_id', 'child_index', 'command_id', 'item_index', 'magic_power', 'magic_realm', 'ndice', 'parent_index', 'pdice', 'sdice', 'shots_current', 'shots_max', 'special', 'type_code', 'value', 'wear_flag', 'weight']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_player_snapshot_normalized_v1_projection_items'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_player_snapshot_normalized_v1_projection_items: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_player_snapshot_normalized_v1_projection_items'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_normalized_v1_projection_items'::regclass),
  'game_character_player_snapshot_normalized_v1_projection_items: RLS select policy');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['character_id', 'command_id', 'legacy_name_key', 'receipt_acknowledged_at', 'receipt_request_sha256', 'snapshot_format', 'snapshot_octets', 'snapshot_sha256', 'source_octets', 'source_post_sha256', 'storage_format', 'world_id', 'writer_epoch', 'writer_instance_id', 'writer_revision']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_player_snapshot_v1_artifacts: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_player_snapshot_v1_artifacts'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass),
  'game_character_player_snapshot_v1_artifacts: RLS select policy');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['acknowledged_at', 'character_id', 'command_id', 'legacy_name_key', 'post_sha256', 'request_sha256', 'storage_format', 'world_id', 'writer_epoch', 'writer_instance_id', 'writer_revision']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_shadow_receipts'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_shadow_receipts: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_shadow_receipts'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_shadow_receipts'::regclass),
  'game_character_shadow_receipts: RLS select policy');

select pg_temp.assert_normalized_reader(
  (select array_agg(a.attname::text order by a.attname) =
    array['character_id', 'command_id', 'file_post_sha256', 'legacy_name_key', 'receipt_acknowledged_at', 'receipt_request_sha256', 'snapshot_format', 'snapshot_octets', 'snapshot_sha256', 'storage_format', 'world_id', 'writer_epoch', 'writer_instance_id', 'writer_revision']::text[]
   from pg_attribute a where a.attrelid = 'private.game_character_m4_file_snapshot_manifests'::regclass
     and a.attnum > 0 and not a.attisdropped
     and has_column_privilege('mud_normalized_replay_reader_login', a.attrelid, a.attname, 'select')),
  'game_character_m4_file_snapshot_manifests: exact readable columns');
select pg_temp.assert_normalized_reader(
  exists (select 1 from pg_policy where polrelid = 'private.game_character_m4_file_snapshot_manifests'::regclass
    and polname = 'normalized_replay_reader_select' and polcmd = 'r'
    and polroles = array['mud_normalized_replay_reader_login'::regrole::oid]
    and pg_get_expr(polqual, polrelid) = 'true')
  and (select relrowsecurity from pg_class where oid = 'private.game_character_m4_file_snapshot_manifests'::regclass),
  'game_character_m4_file_snapshot_manifests: RLS select policy');

select pg_temp.assert_normalized_reader(
  not exists (
    select 1 from pg_class c join pg_namespace n on n.oid = c.relnamespace
    cross join unnest(array['INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']) privilege
    where n.nspname = 'private' and c.relkind in ('r','p')
      and has_table_privilege('mud_normalized_replay_reader_login', c.oid, privilege)
  ), 'no private table mutation privileges');
select pg_temp.assert_normalized_reader(
  not has_column_privilege('mud_normalized_replay_reader_login',
    'private.game_character_player_snapshot_v1_artifacts', 'payload', 'select'),
  'raw payload remains unavailable');

rollback;
