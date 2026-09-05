#!/usr/bin/env bash
set -euo pipefail

[[ "${PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "M5e replay reader PG17 integration skipped (set PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE=1)"
  exit 0
}
command -v docker >/dev/null || { echo "M5e replay reader integration requires docker" >&2; exit 2; }

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="m5e-replay-reader-${RANDOM}-${RANDOM}"
reader_password="m5e-reader-${RANDOM}-${RANDOM}"
fixture_path="$repo_root/tests/fixtures/player_snapshot_v1_canonical.hex"
fixture_hex="$(tr -d '\r\n' < "$fixture_path")"
[[ "$fixture_hex" =~ ^[0-9a-f]+$ && "${#fixture_hex}" -eq 3556 ]] || {
  echo "PlayerSnapshotV1 C fixture is not canonical lowercase hex" >&2; exit 2;
}

cleanup() { docker rm --force "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run --detach --rm --name "$container" \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=192m \
  --volume "$repo_root:/workspace:ro" postgres:17-alpine >/dev/null

run_super() {
  docker exec --interactive --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}
run_reader() {
  docker exec --interactive --env PGPASSWORD="$reader_password" \
    --env PGOPTIONS='-c default_transaction_read_only=on' "$container" \
    psql --host=127.0.0.1 --username=mud_replay_reader_login --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}

ready=0
for _ in $(seq 1 60); do
  if run_super --command='select 1' >/dev/null 2>&1; then ready=$((ready + 1)); else ready=0; fi
  [[ "$ready" -ge 2 ]] && break
  sleep 1
done
[[ "$ready" -ge 2 ]] || { docker logs "$container" >&2; exit 2; }

run_super --file=/workspace/supabase/tests/bootstrap_contract.sql
for migration in \
  20260902000000_game_identity.sql 20260903000000_character_onboarding.sql \
  20260904000000_onboarding_intent_safety.sql 20260905000000_claim_fingerprint_safety.sql \
  20260906000000_claim_actor_rate_limit.sql 20260907000000_claim_challenge_safety.sql \
  20260908000000_service_rpc_fresh_clock.sql 20260909000000_m3_shadow_receipts.sql \
  20260910000000_m3_shadow_receipt_route_v2.sql 20260911000000_m3_writer_session.sql \
  20260912000000_m3_live_save_route.sql 20260913000000_m3_provisioning_head_baseline.sql \
  20260914000000_m4_file_snapshot_manifest.sql 20260915000000_player_snapshot_v1_artifacts.sql \
  20260916000000_player_snapshot_v1_receipt_octets_binding.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

run_super --command='grant mud_writer to mud_writer_login;'
writer_membership_before="$(run_super --tuples-only --no-align --command="select coalesce(string_agg(roleid::text || ':' || member::text || ':' || admin_option::text || ':' || inherit_option::text || ':' || set_option::text, ',' order by roleid, member), '') from pg_auth_members where roleid = 'mud_writer'::regrole and member = 'mud_writer_login'::regrole")"
[[ -n "$writer_membership_before" ]] || { echo "writer membership setup failed" >&2; exit 1; }
unrelated_parent="m5e_unrelated_parent_${RANDOM}_${RANDOM}"
unrelated_member="m5e_unrelated_member_${RANDOM}_${RANDOM}"
run_super --command="create role \"$unrelated_parent\" nologin noinherit; create role \"$unrelated_member\" nologin noinherit; grant \"$unrelated_parent\" to \"$unrelated_member\";"
unrelated_membership_before="$(run_super --tuples-only --no-align --command="select coalesce(string_agg(roleid::text || ':' || member::text || ':' || admin_option::text || ':' || inherit_option::text || ':' || set_option::text, ',' order by roleid, member), '') from pg_auth_members where roleid = '$unrelated_parent'::regrole and member = '$unrelated_member'::regrole")"
[[ -n "$unrelated_membership_before" ]] || { echo "unrelated membership setup failed" >&2; exit 1; }

artifact_ownership_before_first_m5e="$(run_super --tuples-only --no-align --command="select c.oid::text || ':' || c.relowner::text || ':' || n.oid::text || ':' || n.nspowner::text from pg_class c join pg_namespace n on n.oid = c.relnamespace where c.oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass")"
run_super --file=/workspace/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql
artifact_ownership_after_first_m5e="$(run_super --tuples-only --no-align --command="select c.oid::text || ':' || c.relowner::text || ':' || n.oid::text || ':' || n.nspowner::text from pg_class c join pg_namespace n on n.oid = c.relnamespace where c.oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass")"
[[ "$artifact_ownership_before_first_m5e" == "$artifact_ownership_after_first_m5e" ]] || { echo "initial reader migration changed artifact ownership" >&2; exit 1; }
writer_membership_after_first_m5e="$(run_super --tuples-only --no-align --command="select coalesce(string_agg(roleid::text || ':' || member::text || ':' || admin_option::text || ':' || inherit_option::text || ':' || set_option::text, ',' order by roleid, member), '') from pg_auth_members where roleid = 'mud_writer'::regrole and member = 'mud_writer_login'::regrole")"
[[ "$writer_membership_before" == "$writer_membership_after_first_m5e" ]] || { echo "initial reader migration changed writer membership" >&2; exit 1; }
unrelated_membership_after_first_m5e="$(run_super --tuples-only --no-align --command="select coalesce(string_agg(roleid::text || ':' || member::text || ':' || admin_option::text || ':' || inherit_option::text || ':' || set_option::text, ',' order by roleid, member), '') from pg_auth_members where roleid = '$unrelated_parent'::regrole and member = '$unrelated_member'::regrole")"
[[ "$unrelated_membership_before" == "$unrelated_membership_after_first_m5e" ]] || { echo "initial reader migration changed unrelated membership" >&2; exit 1; }
run_super --command="alter role mud_replay_reader_login password '$reader_password'"
password_before="$(run_super --tuples-only --no-align --command="select rolpassword from pg_authid where rolname = 'mud_replay_reader_login'")"
[[ -n "$password_before" ]] || { echo "reader credential setup failed" >&2; exit 1; }

third_capability="m5e_replay_reader_capability_${RANDOM}_${RANDOM}"
run_super --command="create role \"$third_capability\" nologin noinherit; grant \"$third_capability\" to mud_replay_reader_login;"
third_membership_before="$(run_super --tuples-only --no-align --command="select (pg_has_role('mud_replay_reader_login', '$third_capability', 'member') and pg_has_role('mud_replay_reader_login', '$third_capability', 'set'))::text")"
[[ "$third_membership_before" == true ]] || { echo "third NOLOGIN capability did not create a reader SET ROLE route" >&2; exit 1; }
run_super --command='create role "m5e quoted ""capability"" role" nologin noinherit; grant "m5e quoted ""capability"" role" to mud_replay_reader_login;'
quoted_membership_before="$(run_super --tuples-only --no-align --command="select (pg_has_role('mud_replay_reader_login', 'm5e quoted \"capability\" role', 'member') and pg_has_role('mud_replay_reader_login', 'm5e quoted \"capability\" role', 'set'))::text")"
[[ "$quoted_membership_before" == true ]] || { echo "quoted membership setup failed" >&2; exit 1; }
run_super --file=/workspace/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql
run_super --file=/workspace/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql
password_after="$(run_super --tuples-only --no-align --command="select rolpassword from pg_authid where rolname = 'mud_replay_reader_login'")"
[[ "$password_before" == "$password_after" ]] || { echo "reader credential changed on migration replay" >&2; exit 1; }
reader_membership_after="$(run_super --tuples-only --no-align --command="select count(*) from pg_auth_members where member = 'mud_replay_reader_login'::regrole")"
[[ "$reader_membership_after" == 0 ]] || { echo "reader retained role membership after migration replay" >&2; exit 1; }
quoted_membership_after="$(run_super --tuples-only --no-align --command="select (not pg_has_role('mud_replay_reader_login', 'm5e quoted \"capability\" role', 'member') and not pg_has_role('mud_replay_reader_login', 'm5e quoted \"capability\" role', 'set'))::text")"
[[ "$quoted_membership_after" == true ]] || { echo "quoted reader membership survived migration replay" >&2; exit 1; }
writer_membership_after="$(run_super --tuples-only --no-align --command="select coalesce(string_agg(roleid::text || ':' || member::text || ':' || admin_option::text || ':' || inherit_option::text || ':' || set_option::text, ',' order by roleid, member), '') from pg_auth_members where roleid = 'mud_writer'::regrole and member = 'mud_writer_login'::regrole")"
[[ "$writer_membership_after" == "$writer_membership_before" ]] || { echo "writer membership changed on reader migration replay" >&2; exit 1; }
unrelated_membership_after="$(run_super --tuples-only --no-align --command="select coalesce(string_agg(roleid::text || ':' || member::text || ':' || admin_option::text || ':' || inherit_option::text || ':' || set_option::text, ',' order by roleid, member), '') from pg_auth_members where roleid = '$unrelated_parent'::regrole and member = '$unrelated_member'::regrole")"
[[ "$unrelated_membership_after" == "$unrelated_membership_before" ]] || { echo "unrelated membership changed on reader migration replay" >&2; exit 1; }
artifact_ownership_after_replay="$(run_super --tuples-only --no-align --command="select c.oid::text || ':' || c.relowner::text || ':' || n.oid::text || ':' || n.nspowner::text from pg_class c join pg_namespace n on n.oid = c.relnamespace where c.oid = 'private.game_character_player_snapshot_v1_artifacts'::regclass")"
[[ "$artifact_ownership_before_first_m5e" == "$artifact_ownership_after_replay" ]] || { echo "reader migration replay changed artifact ownership" >&2; exit 1; }

run_super --file=/workspace/supabase/tests/player_snapshot_v1_replay_reader_contract.sql
run_super --file=/workspace/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql
run_super --file=/workspace/supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql
run_super --file=/workspace/supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql
run_super --file=/workspace/supabase/tests/player_snapshot_v1_level_projection_replay_reader_contract.sql

# Seed one valid immutable artifact as the disposable database owner.  The
# reader never needs the payload, and the before/after fingerprint proves its
# blocked mutation attempts leave this evidence untouched.
run_super --command="set session_replication_role = replica; insert into private.game_character_player_snapshot_v1_artifacts (character_id, command_id, world_id, legacy_name_key, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, storage_format, receipt_acknowledged_at, snapshot_format, snapshot_sha256, snapshot_octets, payload) values ('a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', 'm5e-reader', 'M5ereader', repeat('a', 64), 'b9500000-0000-0000-0000-000000000001', 1, 1, repeat('b', 64), 9, 1, clock_timestamp(), 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')); set session_replication_role = origin;"
run_super --command="set session_replication_role = replica; insert into private.game_character_player_snapshot_v1_level_projections (character_id, command_id, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, snapshot_sha256, snapshot_octets, raw_level_u8) values ('a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', repeat('a', 64), 'b9500000-0000-0000-0000-000000000001', 1, 1, repeat('b', 64), 9, encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), 42); set session_replication_role = origin;"
before_fingerprint="$(run_super --tuples-only --no-align --command="select count(*)::text || ':' || coalesce(string_agg(character_id::text || command_id::text || snapshot_sha256 || md5(payload), ',' order by character_id, command_id), '') from private.game_character_player_snapshot_v1_artifacts")"

reader_contract="$(run_reader --tuples-only --no-align --command="select (current_user = 'mud_replay_reader_login' and session_user = 'mud_replay_reader_login' and current_user = session_user and current_setting('default_transaction_read_only') = 'on' and current_setting('transaction_read_only') = 'on' and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'insert') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'update') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'delete') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'truncate') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'references') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'trigger'))::text")"
[[ "$reader_contract" == true ]] || { echo "reader login/session/read-only or mutation privilege contract failed" >&2; exit 1; }

metadata_count="$(run_reader --tuples-only --no-align --command="select count(*) from (select command_id, character_id, receipt_request_sha256, source_post_sha256, snapshot_format, snapshot_sha256, snapshot_octets from private.game_character_player_snapshot_v1_artifacts where command_id = 'c9500000-0000-0000-0000-000000000001'::uuid order by character_id) evidence")"
[[ "$metadata_count" == 1 ]] || { echo "reader metadata SELECT did not return seeded evidence" >&2; exit 1; }

level_projection_metadata_count="$(run_reader --tuples-only --no-align --command="select count(*) from (select command_id, character_id, receipt_request_sha256, source_post_sha256, snapshot_sha256, snapshot_octets, raw_level_u8 from private.game_character_player_snapshot_v1_level_projections where command_id = 'c9500000-0000-0000-0000-000000000001'::uuid order by character_id) evidence")"
[[ "$level_projection_metadata_count" == 1 ]] || { echo "reader level projection metadata SELECT did not return seeded evidence" >&2; exit 1; }

if run_reader --command="select payload from private.game_character_player_snapshot_v1_artifacts" >/dev/null 2>&1; then
  echo "reader unexpectedly selected a restricted column" >&2; exit 1
fi
if run_reader --command="insert into private.game_character_player_snapshot_v1_artifacts default values" >/dev/null 2>&1; then
  echo "reader unexpectedly mutated artifacts" >&2; exit 1
fi
if run_reader --command="select writer_revision from private.game_character_player_snapshot_v1_level_projections" >/dev/null 2>&1; then
  echo "reader unexpectedly selected restricted level projection metadata" >&2; exit 1
fi
if run_reader --command="insert into private.game_character_player_snapshot_v1_level_projections default values" >/dev/null 2>&1; then
  echo "reader unexpectedly mutated level projections" >&2; exit 1
fi
if run_reader --command="set role mud_writer" >/dev/null 2>&1; then
  echo "reader unexpectedly SET ROLE mud_writer" >&2; exit 1
fi
if run_reader --command="set role \"$third_capability\"" >/dev/null 2>&1; then
  echo "reader unexpectedly SET ROLE third capability after migration replay" >&2; exit 1
fi
if run_reader --command='set role "m5e quoted ""capability"" role"' >/dev/null 2>&1; then
  echo "reader unexpectedly SET ROLE quoted capability after migration replay" >&2; exit 1
fi
if run_reader --command="select * from private.record_player_snapshot_v1_artifact_for_receipt(null, null, null, null, null, null, null, null, null)" >/dev/null 2>&1; then
  echo "reader unexpectedly executed artifact mutation RPC" >&2; exit 1
fi
if run_reader --command="select * from private.record_player_snapshot_v1_level_projection_for_receipt(null, null, null, null, null)" >/dev/null 2>&1; then
  echo "reader unexpectedly executed level projection mutation RPC" >&2; exit 1
fi

after_fingerprint="$(run_super --tuples-only --no-align --command="select count(*)::text || ':' || coalesce(string_agg(character_id::text || command_id::text || snapshot_sha256 || md5(payload), ',' order by character_id, command_id), '') from private.game_character_player_snapshot_v1_artifacts")"
[[ "$before_fingerprint" == "$after_fingerprint" ]] || { echo "reader activity changed artifact data" >&2; exit 1; }

echo "GREEN PostgreSQL 17 M5e replay reader login is metadata-only, read-only, and non-writer"
