-- 2026-09-17: M5e PlayerSnapshotV1 replay differential read boundary.
--
-- This adds one direct login for metadata-only replay comparison.  It is not
-- a capability role and it deliberately has no relationship to the M3 writer
-- login/capability pair, so current_user and session_user remain identical.

do $$
declare
  v_reader oid;
  v_membership record;
begin
  if not exists (select 1 from pg_roles where rolname = 'mud_writer')
     or not exists (select 1 from pg_roles where rolname = 'mud_writer_login') then
    raise exception using errcode = 'P0001', message = 'M3 writer roles are required';
  end if;

  if not exists (select 1 from pg_roles where rolname = 'mud_replay_reader_login') then
    create role mud_replay_reader_login login noinherit nosuperuser nocreatedb nocreaterole
      noreplication nobypassrls password null;
  end if;

  select oid into v_reader from pg_roles where rolname = 'mud_replay_reader_login';

  -- NOINHERIT does not prevent SET ROLE.  Normalize every direct membership
  -- granted to this login, including an unexpected future capability role,
  -- without touching memberships between any other roles.
  for v_membership in
    select parent.rolname
      from pg_auth_members membership
      join pg_roles parent on parent.oid = membership.roleid
     where membership.member = v_reader
  loop
    execute format('revoke %I from mud_replay_reader_login', v_membership.rolname);
  end loop;

  -- Do not mention PASSWORD here: an operator-provisioned login secret must
  -- survive migration replays.
  alter role mud_replay_reader_login login noinherit nosuperuser nocreatedb nocreaterole
    noreplication nobypassrls;
end
$$;

-- Match the bounded M3 role setup convention, while making every replay
-- session read-only even when the client omits its URL startup option.
alter role mud_replay_reader_login reset all;
alter role mud_replay_reader_login set default_transaction_read_only to 'on';
alter role mud_replay_reader_login set statement_timeout to '5s';
alter role mud_replay_reader_login set lock_timeout to '1s';
alter role mud_replay_reader_login set idle_in_transaction_session_timeout to '5s';
alter role mud_replay_reader_login set search_path to pg_catalog;

do $$
begin
  execute format('grant connect on database %I to mud_replay_reader_login', current_database());
end
$$;

grant usage on schema private to mud_replay_reader_login;

-- Start with no ambient private privileges for this login.  The following
-- column grant is the entire M5d differential read surface; in particular it
-- excludes payload and all non-evidence artifact metadata.
revoke all privileges on all tables in schema private from mud_replay_reader_login;
revoke all privileges on all functions in schema private from mud_replay_reader_login;
grant select (
  command_id,
  character_id,
  receipt_request_sha256,
  source_post_sha256,
  snapshot_format,
  snapshot_sha256,
  snapshot_octets
) on table private.game_character_player_snapshot_v1_artifacts to mud_replay_reader_login;

-- The artifact relation already uses RLS.  Recreate only this role's
-- metadata read policy so migration replay has the same boundary every time.
drop policy if exists mud_replay_reader_player_snapshot_v1_metadata_select
  on private.game_character_player_snapshot_v1_artifacts;
create policy mud_replay_reader_player_snapshot_v1_metadata_select
  on private.game_character_player_snapshot_v1_artifacts
  for select to mud_replay_reader_login
  using (true);

revoke all on function private.record_player_snapshot_v1_artifact_for_receipt(uuid,uuid,text,text,bigint,text,text,bigint,bytea)
  from mud_replay_reader_login;

comment on role mud_replay_reader_login is
  'M5e direct LOGIN for metadata-only PlayerSnapshotV1 replay differential; never a writer capability.';
