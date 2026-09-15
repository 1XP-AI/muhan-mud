-- 2026-09-20: metadata-only replay-reader access to immutable level projections.
--
-- The dedicated login already has a read-only session contract.  This
-- migration grants only the comparator's closed evidence columns and recreates
-- its RLS SELECT policy so migration replay remains idempotent.

revoke all privileges on table private.game_character_player_snapshot_v1_level_projections
  from mud_replay_reader_login;
grant select (
  command_id,
  character_id,
  receipt_request_sha256,
  source_post_sha256,
  snapshot_sha256,
  snapshot_octets,
  raw_level_u8
) on table private.game_character_player_snapshot_v1_level_projections to mud_replay_reader_login;

drop policy if exists mud_replay_reader_player_snapshot_v1_level_projection_metadata_select
  on private.game_character_player_snapshot_v1_level_projections;
create policy mud_replay_reader_player_snapshot_v1_level_projection_metadata_select
  on private.game_character_player_snapshot_v1_level_projections
  for select to mud_replay_reader_login
  using (true);
