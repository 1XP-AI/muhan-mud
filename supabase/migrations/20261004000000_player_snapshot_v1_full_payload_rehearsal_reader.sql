-- 2026-10-04: explicit, read-only PlayerSnapshotV1 full-payload rehearsal.
--
-- This login is deliberately separate from the metadata-only replay reader.
-- It may SELECT one immutable artifact plus its acknowledged receipt tuple;
-- it is never a writer capability and no application path enables it by
-- default.

do $$
declare
  v_reader oid;
  v_membership record;
begin
  if not exists (select 1 from pg_roles where rolname = 'mud_replay_reader_login') then
    raise exception using errcode = 'P0001', message = 'metadata replay reader role is required';
  end if;
  if not exists (select 1 from pg_roles where rolname = 'mud_full_payload_rehearsal_reader_login') then
    create role mud_full_payload_rehearsal_reader_login login noinherit nosuperuser nocreatedb nocreaterole
      noreplication nobypassrls password null;
  end if;

  select oid into v_reader from pg_roles where rolname = 'mud_full_payload_rehearsal_reader_login';
  for v_membership in
    select parent.rolname
      from pg_auth_members membership
      join pg_roles parent on parent.oid = membership.roleid
     where membership.member = v_reader
  loop
    execute format('revoke %I from mud_full_payload_rehearsal_reader_login', v_membership.rolname);
  end loop;

  alter role mud_full_payload_rehearsal_reader_login login noinherit nosuperuser nocreatedb nocreaterole
    noreplication nobypassrls;
end
$$;

alter role mud_full_payload_rehearsal_reader_login reset all;
alter role mud_full_payload_rehearsal_reader_login set default_transaction_read_only to 'on';
alter role mud_full_payload_rehearsal_reader_login set statement_timeout to '5s';
alter role mud_full_payload_rehearsal_reader_login set lock_timeout to '1s';
alter role mud_full_payload_rehearsal_reader_login set idle_in_transaction_session_timeout to '5s';
alter role mud_full_payload_rehearsal_reader_login set search_path to pg_catalog;

do $$
begin
  execute format('grant connect on database %I to mud_full_payload_rehearsal_reader_login', current_database());
end
$$;

grant usage on schema private to mud_full_payload_rehearsal_reader_login;
revoke all privileges on all tables in schema private from mud_full_payload_rehearsal_reader_login;
revoke all privileges on all functions in schema private from mud_full_payload_rehearsal_reader_login;

-- The first relation is the immutable CDTO artifact.  The second is the
-- receipt tuple the rehearsal must compare before decoding the payload.
grant select (
  character_id, command_id, world_id, legacy_name_key, receipt_request_sha256,
  writer_instance_id, writer_epoch, writer_revision, source_post_sha256,
  source_octets, storage_format, receipt_acknowledged_at, snapshot_format,
  snapshot_sha256, snapshot_octets, payload
) on table private.game_character_player_snapshot_v1_artifacts
  to mud_full_payload_rehearsal_reader_login;
grant select (
  character_id, command_id, world_id, legacy_name_key, request_sha256,
  writer_instance_id, writer_epoch, writer_revision, post_sha256,
  storage_format, acknowledged_at
) on table private.game_character_shadow_receipts
  to mud_full_payload_rehearsal_reader_login;

drop policy if exists mud_full_payload_rehearsal_artifact_select
  on private.game_character_player_snapshot_v1_artifacts;
create policy mud_full_payload_rehearsal_artifact_select
  on private.game_character_player_snapshot_v1_artifacts
  for select to mud_full_payload_rehearsal_reader_login
  using (true);

drop policy if exists mud_full_payload_rehearsal_receipt_select
  on private.game_character_shadow_receipts;
create policy mud_full_payload_rehearsal_receipt_select
  on private.game_character_shadow_receipts
  for select to mud_full_payload_rehearsal_reader_login
  using (true);

comment on role mud_full_payload_rehearsal_reader_login is
  'M4 direct LOGIN for explicitly invoked, read-only PlayerSnapshotV1 full-payload rehearsal; never a writer capability or game authority.';
