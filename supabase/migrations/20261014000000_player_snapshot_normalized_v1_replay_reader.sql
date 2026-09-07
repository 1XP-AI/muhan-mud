-- Numeric normalized shadow comparison gets its own direct login.
-- Do not widen mud_replay_reader_login's existing closed metadata contract.
-- No gameplay authority, loader, credentials, or runtime consumer is enabled.
do $$
declare membership record;
begin
  if not exists (select 1 from pg_roles where rolname = 'mud_normalized_replay_reader_login') then
    create role mud_normalized_replay_reader_login login noinherit nosuperuser
      nocreatedb nocreaterole noreplication nobypassrls password null;
  end if;
  for membership in
    select parent.rolname from pg_auth_members m
    join pg_roles parent on parent.oid = m.roleid
    where m.member = 'mud_normalized_replay_reader_login'::regrole
  loop
    execute format('revoke %I from mud_normalized_replay_reader_login', membership.rolname);
  end loop;
  -- Preserve any operator-provisioned password on replay.
  alter role mud_normalized_replay_reader_login login noinherit nosuperuser
    nocreatedb nocreaterole noreplication nobypassrls;
  execute format('grant connect on database %I to mud_normalized_replay_reader_login', current_database());
end;
$$;

alter role mud_normalized_replay_reader_login reset all;
alter role mud_normalized_replay_reader_login set default_transaction_read_only to 'on';
alter role mud_normalized_replay_reader_login set statement_timeout to '5s';
alter role mud_normalized_replay_reader_login set lock_timeout to '1s';
alter role mud_normalized_replay_reader_login set idle_in_transaction_session_timeout to '5s';
alter role mud_normalized_replay_reader_login set search_path to pg_catalog;

grant usage on schema private to mud_normalized_replay_reader_login;
revoke all privileges on all tables in schema private from mud_normalized_replay_reader_login;
revoke all privileges on all functions in schema private from mud_normalized_replay_reader_login;

grant select (
  character_id, command_id, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, snapshot_sha256, snapshot_octets, projection_format, projection_version, projection_algorithm, canonical_digest, level, hp_max, hp_current, mp_max, mp_current, experience, gold, item_count
) on table private.game_character_player_snapshot_normalized_v1_projections to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projections;
create policy normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projections
  for select to mud_normalized_replay_reader_login using (true);

grant select (
  character_id, command_id, slot, max_value, current_value, last_used
) on table private.game_character_player_snapshot_normalized_v1_projection_daily to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projection_daily;
create policy normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projection_daily
  for select to mud_normalized_replay_reader_login using (true);

grant select (
  character_id, command_id, slot, interval_value, last_used, misc
) on table private.game_character_player_snapshot_normalized_v1_projection_timers to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projection_timers;
create policy normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projection_timers
  for select to mud_normalized_replay_reader_login using (true);

grant select (
  character_id, command_id, item_index, parent_index, child_index, value, weight, type_code, adjustment, shots_max, shots_current, ndice, sdice, pdice, armor, wear_flag, magic_power, magic_realm, special
) on table private.game_character_player_snapshot_normalized_v1_projection_items to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projection_items;
create policy normalized_replay_reader_select on private.game_character_player_snapshot_normalized_v1_projection_items
  for select to mud_normalized_replay_reader_login using (true);

grant select (
  character_id, command_id, world_id, legacy_name_key, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, snapshot_sha256, snapshot_octets, snapshot_format, storage_format, receipt_acknowledged_at
) on table private.game_character_player_snapshot_v1_artifacts to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_player_snapshot_v1_artifacts;
create policy normalized_replay_reader_select on private.game_character_player_snapshot_v1_artifacts
  for select to mud_normalized_replay_reader_login using (true);

grant select (
  character_id, command_id, world_id, legacy_name_key, request_sha256, post_sha256, writer_instance_id, writer_epoch, writer_revision, storage_format, acknowledged_at
) on table private.game_character_shadow_receipts to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_shadow_receipts;
create policy normalized_replay_reader_select on private.game_character_shadow_receipts
  for select to mud_normalized_replay_reader_login using (true);

grant select (
  character_id, command_id, world_id, legacy_name_key, receipt_request_sha256, file_post_sha256, writer_instance_id, writer_epoch, writer_revision, storage_format, receipt_acknowledged_at, snapshot_sha256, snapshot_octets, snapshot_format
) on table private.game_character_m4_file_snapshot_manifests to mud_normalized_replay_reader_login;
drop policy if exists normalized_replay_reader_select on private.game_character_m4_file_snapshot_manifests;
create policy normalized_replay_reader_select on private.game_character_m4_file_snapshot_manifests
  for select to mud_normalized_replay_reader_login using (true);

comment on role mud_normalized_replay_reader_login is
  'Direct read-only login for numeric normalized shadow evidence, excluding raw artifact payload; never gameplay authority.';
