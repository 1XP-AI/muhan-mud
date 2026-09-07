#!/usr/bin/env bash
set -euo pipefail

[[ "${PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "M5e replay reader PG17 integration skipped (set PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE=1)"
  exit 0
}
containerless="${PLAYER_SNAPSHOT_V1_REPLAY_READER_CONTAINERLESS:-0}"
[[ "$containerless" == 0 || "$containerless" == 1 ]] || exit 2
if [[ "$containerless" == 1 ]]; then
  [[ -f /.dockerenv ]] || { echo 'containerless replay requires an isolated test container' >&2; exit 2; }
  command -v psql >/dev/null || exit 2
else
  command -v docker >/dev/null || { echo "M5e replay reader integration requires docker" >&2; exit 2; }
fi
command -v node >/dev/null || { echo "M5e replay reader integration requires a built relay dist" >&2; exit 2; }
[[ "$(node -p 'process.platform')" == linux ]] || {
  echo 'M5e replay reader integration requires Linux Node for descriptor-bound outbox reads; run in an isolated Linux environment' >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)"
container="m5e-replay-reader-${RANDOM}-${RANDOM}"
container_id=""
reader_password="m5e-reader-${RANDOM}-${RANDOM}"
full_payload_reader_password="m5e-full-payload-reader-${RANDOM}-${RANDOM}"
comparator_cli="$repo_root/services/m4-file-snapshot-manifest-relay/dist/player-snapshot-v2-journal-level-shadow-comparator-cli.js"
full_payload_reader_module="$repo_root/services/m4-file-snapshot-manifest-relay/dist/store.js"
full_payload_rehearsal_cli="$repo_root/services/m4-file-snapshot-manifest-relay/dist/player-snapshot-v1-full-payload-rehearsal-cli.js"
full_payload_rehearsal_verifier="$repo_root/rust/target/release/player_snapshot_v1_replay_verify"
fixture_path="$repo_root/tests/fixtures/player_snapshot_v1_canonical.hex"
fixture_hex="$(tr -d '\r\n' < "$fixture_path")"
[[ "$fixture_hex" =~ ^[0-9a-f]+$ && "${#fixture_hex}" -eq 3556 ]] || {
  echo "PlayerSnapshotV1 C fixture is not canonical lowercase hex" >&2; exit 2;
}
[[ -f "$comparator_cli" ]] || {
  echo "M5e replay reader integration requires the compiled v2 comparator dist" >&2; exit 2;
}
[[ -f "$full_payload_reader_module" ]] || {
  echo "M5e replay reader integration requires the compiled full payload reader dist" >&2; exit 2;
}
[[ -f "$full_payload_rehearsal_cli" ]] || {
  echo "M5e replay reader integration requires the compiled full payload rehearsal CLI" >&2; exit 2;
}
[[ -x "$full_payload_rehearsal_verifier" ]] || {
  echo "M5e replay reader integration requires the compiled full payload rehearsal verifier" >&2; exit 2;
}

umask 077
journal_root="$(mktemp -d "${TMPDIR:-/tmp}/m5e-v2-level-shadow.XXXXXX")"
chmod 0700 "$journal_root"
full_payload_rehearsal_outbox="$(mktemp -d "${TMPDIR:-/tmp}/m5e-full-payload-rehearsal.XXXXXX")"
chmod 0700 "$full_payload_rehearsal_outbox"

cleanup() {
  rm -rf -- "$journal_root"
  rm -rf -- "$full_payload_rehearsal_outbox"
  if [[ "$container_id" =~ ^[0-9a-f]{64}$ ]]; then docker rm --force "$container_id" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

if [[ "$containerless" == 0 ]]; then
container_id="$(docker run --detach --rm --name "$container" \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=192m \
  --publish 127.0.0.1::5432 \
  --volume "$repo_root:/workspace:ro" postgres:17-alpine)"
container="$container_id"

postgres_port="$(docker port "$container" 5432/tcp | sed -n '1{s/.*://p;}')"
else
  # Outer harness owns a fresh PG17 container and shares only its network namespace.
  # Never accepts an arbitrary database URL or mounts a Docker control socket.
  postgres_port=5432
fi
[[ "$postgres_port" =~ ^[1-9][0-9]*$ ]] || { echo "M5e replay reader integration did not receive a temporary loopback port" >&2; exit 2; }

run_psql() {
  local password="$1" options="$2"
  shift 2
  if [[ "$containerless" == 1 ]]; then
    PGPASSWORD="$password" PGOPTIONS="$options" psql "$@"
  else
    docker exec --interactive --env "PGPASSWORD=$password" --env "PGOPTIONS=$options" "$container" psql "$@"
  fi
}

run_super() {
  run_psql contract-only-password '' --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}
run_reader() {
  run_psql "$reader_password" '-c default_transaction_read_only=on' \
    --host=127.0.0.1 --username=mud_replay_reader_login --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}
run_full_payload_reader() {
  run_psql "$full_payload_reader_password" '-c default_transaction_read_only=on' \
    --host=127.0.0.1 --username=mud_full_payload_rehearsal_reader_login --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}

assert_comparator_result() {
  local output_path="$1"
  local expected_top_level="$2"
  local expected_records="$3"
  local expected_raw_levels="$4"
  COMPARATOR_OUTPUT_PATH="$output_path" \
  COMPARATOR_EXPECTED_TOP_LEVEL="$expected_top_level" \
  COMPARATOR_EXPECTED_RECORDS="$expected_records" \
  COMPARATOR_EXPECTED_RAW_LEVELS="$expected_raw_levels" \
  COMPARATOR_JOURNAL_ROOT="$journal_root" \
  COMPARATOR_READER_PASSWORD="$reader_password" \
  node <<'NODE'
const fs = require('node:fs')
const output = fs.readFileSync(process.env.COMPARATOR_OUTPUT_PATH, 'utf8')
if (!output.endsWith('\n') || output.split('\n').filter(Boolean).length !== 1) process.exit(1)
const result = JSON.parse(output)
const expectedRecords = process.env.COMPARATOR_EXPECTED_RECORDS.split(',')
const expectedRawLevels = process.env.COMPARATOR_EXPECTED_RAW_LEVELS === ''
  ? [] : process.env.COMPARATOR_EXPECTED_RAW_LEVELS.split(',').map(Number)
if (result.format !== 'player-snapshot-v2-journal-level-shadow-comparison'
  || result.version !== '1'
  || result.classification !== process.env.COMPARATOR_EXPECTED_TOP_LEVEL
  || !Array.isArray(result.records)
  || result.records.map((record) => record.classification).join(',') !== expectedRecords.join(',')) process.exit(1)
const inputKeys = ['characterId', 'commandId', 'rawLevelU8', 'receiptRequestSha256', 'snapshotOctets', 'snapshotSha256', 'sourcePostSha256'].join(',')
for (const record of result.records) {
  const recordKeys = Object.keys(record).sort().join(',')
  if (record.input === undefined) {
    if (recordKeys !== 'classification,index') process.exit(1)
  } else if (recordKeys !== 'classification,index,input'
    || Object.keys(record.input).sort().join(',') !== inputKeys) process.exit(1)
}
const rendered = JSON.stringify(result)
const rawLevels = result.records.flatMap((record) => record.input === undefined ? [] : [record.input.rawLevelU8])
if (rawLevels.join(',') !== expectedRawLevels.join(',')) process.exit(1)
for (const forbidden of [
  process.env.COMPARATOR_JOURNAL_ROOT,
  process.env.COMPARATOR_READER_PASSWORD,
  'legacy-payload-sentinel',
  'raw database error',
  'permission denied',
  'payload',
]) if (forbidden && rendered.includes(forbidden)) process.exit(1)
NODE
}

run_comparator_case() {
  local name="$1"
  local expected_exit="$2"
  local expected_top_level="$3"
  local expected_records="$4"
  local expected_raw_levels="$5"
  local output_path="$journal_root/$name.stdout"
  local error_path="$journal_root/$name.stderr"
  local case_path="$journal_root/$name"
  local exit_code
  set +e
  DATABASE_URL= \
  M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_JOURNAL_PATH="$case_path" \
  M4_PLAYER_SNAPSHOT_V2_JOURNAL_LEVEL_SHADOW_COMPARATOR_DATABASE_URL="$reader_database_url" \
    node "$comparator_cli" --once >"$output_path" 2>"$error_path"
  exit_code=$?
  set -e
  [[ "$exit_code" -eq "$expected_exit" ]] || { echo "v2 comparator $name exit contract failed" >&2; exit 1; }
  assert_comparator_result "$output_path" "$expected_top_level" "$expected_records" "$expected_raw_levels" || {
    echo "v2 comparator $name metadata-only output contract failed" >&2; exit 1;
  }
  if [[ -s "$error_path" ]]; then
    local protected_detail
    for protected_detail in "$reader_password" "$journal_root" legacy-payload-sentinel 'raw database error' 'permission denied'; do
      if rg -q --fixed-strings -- "$protected_detail" "$error_path"; then
        echo "v2 comparator $name exposed protected detail on stderr" >&2; exit 1
      fi
    done
  fi
}

ready=0
for _ in $(seq 1 60); do
  if run_super --command='select 1' >/dev/null 2>&1; then ready=$((ready + 1)); else ready=0; fi
  [[ "$ready" -ge 2 ]] && break
  sleep 1
done
[[ "$ready" -ge 2 ]] || { echo 'disposable PG17 readiness failed' >&2; exit 2; }

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
reader_database_url="postgresql://mud_replay_reader_login:${reader_password}@127.0.0.1:${postgres_port}/postgres?options=-c%20default_transaction_read_only%3Don"

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
if [[ "$(run_super --tuples-only --no-align --command="select exists (select 1 from pg_roles where rolname = 'mud_full_payload_rehearsal_reader_login')")" == true ]]; then
  echo "RED unexpectedly found full payload rehearsal reader before migration 040" >&2; exit 1
fi

# Supply a valid local artifact and a URL for the intended dedicated login
# before 040 exists.  This executes the exact default-off, read-only path;
# PostgreSQL must reject the unavailable login, so it cannot report MATCH.
FULL_PAYLOAD_REHEARSAL_OUTBOX="$full_payload_rehearsal_outbox" \
FULL_PAYLOAD_FIXTURE_HEX="$fixture_hex" \
node <<'NODE'
const { createHash } = require('node:crypto')
const { chmodSync, writeFileSync } = require('node:fs')
const { join } = require('node:path')
const outbox = process.env.FULL_PAYLOAD_REHEARSAL_OUTBOX
const payload = Buffer.from(process.env.FULL_PAYLOAD_FIXTURE_HEX, 'hex')
const characterId = 'a9500000-0000-0000-0000-000000000001'
const commandId = 'c9500000-0000-0000-0000-000000000001'
const requestSha256 = 'a'.repeat(64)
const postSha256 = 'b'.repeat(64)
const writerInstanceId = 'b9500000-0000-0000-0000-000000000001'
const canonicalNameHex = '4d3565726561646572'
const manifest = [
  'version=1', 'world_id=m5e-reader', `character_id=${characterId}`, `command_id=${commandId}`,
  `canonical_name_hex=${canonicalNameHex}`, `request_sha256=${requestSha256}`, `post_sha256=${postSha256}`,
  `writer_instance_id=${writerInstanceId}`, 'snapshot_format=legacy-file-manifest-v1', 'writer_epoch=1',
  'writer_revision=1', 'storage_format=1', 'snapshot_octets=9', '',
].join('\n')
const artifact = Buffer.concat([Buffer.from([
  'version=1', 'world_id=m5e-reader', `character_id=${characterId}`, `command_id=${commandId}`,
  `canonical_name_hex=${canonicalNameHex}`, `request_sha256=${requestSha256}`, `source_post_sha256=${postSha256}`,
  `writer_instance_id=${writerInstanceId}`, 'writer_epoch=1', 'writer_revision=1', 'storage_format=1',
  'snapshot_format=player-snapshot-v1', 'source_octets=9',
  `snapshot_sha256=${createHash('sha256').update(payload).digest('hex')}`, `snapshot_octets=${payload.length}`, '', '',
].join('\n'), 'ascii'), payload])
writeFileSync(join(outbox, `${commandId}.manifest`), manifest, { mode: 0o600, flag: 'wx' })
writeFileSync(join(outbox, `${commandId}.player-snapshot-v1`), artifact, { mode: 0o600, flag: 'wx' })
chmodSync(join(outbox, `${commandId}.manifest`), 0o600)
chmodSync(join(outbox, `${commandId}.player-snapshot-v1`), 0o600)
NODE
pre_migration_rehearsal_stdout="$journal_root/pre-migration-full-payload-rehearsal.stdout"
pre_migration_rehearsal_stderr="$journal_root/pre-migration-full-payload-rehearsal.stderr"
set +e
M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_OUTBOX_PATH="$full_payload_rehearsal_outbox" \
M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_DATABASE_URL="postgresql://mud_full_payload_rehearsal_reader_login:${full_payload_reader_password}@127.0.0.1:${postgres_port}/postgres?options=-c%20default_transaction_read_only%3Don" \
M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_VERIFIER_PATH="$full_payload_rehearsal_verifier" \
  node "$full_payload_rehearsal_cli" --once >"$pre_migration_rehearsal_stdout" 2>"$pre_migration_rehearsal_stderr"
pre_migration_rehearsal_exit=$?
set -e
[[ "$pre_migration_rehearsal_exit" -eq 1 ]] || { echo "RED full payload rehearsal unexpectedly succeeded before migration 040" >&2; exit 1; }
[[ ! -s "$pre_migration_rehearsal_stderr" ]] || { echo "RED pre-migration full payload rehearsal emitted stderr" >&2; exit 1; }
PRE_MIGRATION_REHEARSAL_OUTPUT="$pre_migration_rehearsal_stdout" \
node <<'NODE'
const assert = require('node:assert/strict')
const fs = require('node:fs')
const output = fs.readFileSync(process.env.PRE_MIGRATION_REHEARSAL_OUTPUT, 'utf8')
assert.equal(output.endsWith('\n'), true)
assert.equal(output.split('\n').filter(Boolean).length, 1)
assert.deepEqual(JSON.parse(output), {
  format: 'player-snapshot-v1-full-payload-rehearsal', version: '1', classification: 'DB_READ_ERROR',
  commandId: 'c9500000-0000-0000-0000-000000000001',
  characterId: 'a9500000-0000-0000-0000-000000000001',
})
NODE
echo "RED PostgreSQL 17: full payload rehearsal reports DB_READ_ERROR before migration 040"
run_super --file=/workspace/supabase/migrations/20261004000000_player_snapshot_v1_full_payload_rehearsal_reader.sql
run_super --file=/workspace/supabase/migrations/20261004000000_player_snapshot_v1_full_payload_rehearsal_reader.sql
run_super --file=/workspace/supabase/tests/player_snapshot_v1_full_payload_rehearsal_reader_contract.sql
run_super --command="alter role mud_full_payload_rehearsal_reader_login password '$full_payload_reader_password'"
full_payload_reader_database_url="postgresql://mud_full_payload_rehearsal_reader_login:${full_payload_reader_password}@127.0.0.1:${postgres_port}/postgres?options=-c%20default_transaction_read_only%3Don"
run_super --file=/workspace/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql
run_super --file=/workspace/supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql
run_super --file=/workspace/supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql
run_super --file=/workspace/supabase/tests/player_snapshot_v1_level_projection_replay_reader_contract.sql

# Seed one valid immutable artifact as the disposable database owner.  The
# reader never needs the payload, and the before/after fingerprint proves its
# blocked mutation attempts leave this evidence untouched.
run_super --command="set session_replication_role = replica; insert into private.game_character_player_snapshot_v1_artifacts (character_id, command_id, world_id, legacy_name_key, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, storage_format, receipt_acknowledged_at, snapshot_format, snapshot_sha256, snapshot_octets, payload) values ('a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', 'm5e-reader', 'M5ereader', repeat('a', 64), 'b9500000-0000-0000-0000-000000000001', 1, 1, repeat('b', 64), 9, 1, timestamptz '2026-10-04 00:00:00+00', 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')); set session_replication_role = origin;"
run_super --command="set session_replication_role = replica; insert into private.game_character_shadow_receipts (character_id, command_id, world_id, legacy_name_key, request_sha256, writer_instance_id, writer_epoch, writer_revision, expected_state, expected_sha256, post_sha256, storage_format, acknowledged_at) values ('a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', 'm5e-reader', 'M5ereader', repeat('a', 64), 'b9500000-0000-0000-0000-000000000001', 1, 1, 'absent', null, repeat('b', 64), 1, timestamptz '2026-10-04 00:00:00+00'); set session_replication_role = origin;"
run_super --command="set session_replication_role = replica; insert into private.game_character_player_snapshot_v1_level_projections (character_id, command_id, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, snapshot_sha256, snapshot_octets, raw_level_u8) values ('a9500000-0000-0000-0000-000000000001', 'c9500000-0000-0000-0000-000000000001', repeat('a', 64), 'b9500000-0000-0000-0000-000000000001', 1, 1, repeat('b', 64), 9, encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), 42); set session_replication_role = origin;"
run_super --command="set session_replication_role = replica; insert into private.game_character_player_snapshot_v1_artifacts (character_id, command_id, world_id, legacy_name_key, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, storage_format, receipt_acknowledged_at, snapshot_format, snapshot_sha256, snapshot_octets, payload) values ('a9500000-0000-0000-0000-000000000010', 'c9500000-0000-0000-0000-000000000010', 'm5e-reader', 'M5ereader0', repeat('a', 64), 'b9500000-0000-0000-0000-000000000010', 1, 10, repeat('b', 64), 9, 1, clock_timestamp(), 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')), ('a9500000-0000-0000-0000-000000000011', 'c9500000-0000-0000-0000-000000000011', 'm5e-reader', 'M5ereader255', repeat('a', 64), 'b9500000-0000-0000-0000-000000000011', 1, 11, repeat('b', 64), 9, 1, clock_timestamp(), 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')), ('a9500000-0000-0000-0000-000000000012', 'c9500000-0000-0000-0000-000000000012', 'm5e-reader', 'M5ereaderMissing', repeat('a', 64), 'b9500000-0000-0000-0000-000000000012', 1, 12, repeat('b', 64), 9, 1, clock_timestamp(), 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')), ('a9500000-0000-0000-0000-000000000013', 'c9500000-0000-0000-0000-000000000013', 'm5e-reader', 'M5ereaderDuplicateA', repeat('a', 64), 'b9500000-0000-0000-0000-000000000013', 1, 13, repeat('b', 64), 9, 1, clock_timestamp(), 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')), ('a9500000-0000-0000-0000-000000000014', 'c9500000-0000-0000-0000-000000000013', 'm5e-reader', 'M5ereaderDuplicateB', repeat('a', 64), 'b9500000-0000-0000-0000-000000000014', 1, 14, repeat('b', 64), 9, 1, clock_timestamp(), 'player-snapshot-v1', encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), decode('$fixture_hex', 'hex')); set session_replication_role = origin;"
run_super --command="set session_replication_role = replica; insert into private.game_character_player_snapshot_v1_level_projections (character_id, command_id, receipt_request_sha256, writer_instance_id, writer_epoch, writer_revision, source_post_sha256, source_octets, snapshot_sha256, snapshot_octets, raw_level_u8) values ('a9500000-0000-0000-0000-000000000010', 'c9500000-0000-0000-0000-000000000010', repeat('a', 64), 'b9500000-0000-0000-0000-000000000010', 1, 10, repeat('b', 64), 9, encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), 0), ('a9500000-0000-0000-0000-000000000011', 'c9500000-0000-0000-0000-000000000011', repeat('a', 64), 'b9500000-0000-0000-0000-000000000011', 1, 11, repeat('b', 64), 9, encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), 255), ('a9500000-0000-0000-0000-000000000013', 'c9500000-0000-0000-0000-000000000013', repeat('a', 64), 'b9500000-0000-0000-0000-000000000013', 1, 13, repeat('b', 64), 9, encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), 42), ('a9500000-0000-0000-0000-000000000014', 'c9500000-0000-0000-0000-000000000013', repeat('a', 64), 'b9500000-0000-0000-0000-000000000014', 1, 14, repeat('b', 64), 9, encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex'), octet_length(decode('$fixture_hex', 'hex')), 42); set session_replication_role = origin;"
before_fingerprint="$(run_super --tuples-only --no-align --command="select count(*)::text || ':' || coalesce(string_agg(character_id::text || command_id::text || snapshot_sha256 || md5(payload), ',' order by character_id, command_id), '') from private.game_character_player_snapshot_v1_artifacts")"
before_projection_fingerprint="$(run_super --tuples-only --no-align --command="select count(*)::text || ':' || coalesce(string_agg(character_id::text || command_id::text || receipt_request_sha256 || source_post_sha256 || snapshot_sha256 || snapshot_octets::text || raw_level_u8::text, ',' order by character_id, command_id), '') from private.game_character_player_snapshot_v1_level_projections")"

# Run the explicit, default-off CLI only after migration 040 has been replayed,
# its SQL contract has passed, and both immutable sides of the joined evidence
# have been seeded.  The canonical CDTO fixture above is reused unchanged;
# this creates no runtime authority or production wiring.
full_payload_rehearsal_stdout="$journal_root/full-payload-rehearsal.stdout"
full_payload_rehearsal_stderr="$journal_root/full-payload-rehearsal.stderr"
M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_OUTBOX_PATH="$full_payload_rehearsal_outbox" \
M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_DATABASE_URL="$full_payload_reader_database_url" \
M4_PLAYER_SNAPSHOT_V1_FULL_PAYLOAD_REHEARSAL_VERIFIER_PATH="$full_payload_rehearsal_verifier" \
  node "$full_payload_rehearsal_cli" --once >"$full_payload_rehearsal_stdout" 2>"$full_payload_rehearsal_stderr" || {
    node -e 'const fs = require("node:fs"); let c; try { c = JSON.parse(fs.readFileSync(process.argv[1], "utf8")).classification } catch {} console.error("full payload rehearsal failed: " + (/^[A-Z_]{1,64}$/.test(c ?? "") ? c : "UNREADABLE_DIAGNOSTIC"))' "$full_payload_rehearsal_stdout"
    exit 1
  }
[[ ! -s "$full_payload_rehearsal_stderr" ]] || { echo "full payload rehearsal emitted stderr" >&2; exit 1; }
FULL_PAYLOAD_REHEARSAL_OUTPUT="$full_payload_rehearsal_stdout" \
node <<'NODE'
const assert = require('node:assert/strict')
const fs = require('node:fs')
const output = fs.readFileSync(process.env.FULL_PAYLOAD_REHEARSAL_OUTPUT, 'utf8')
assert.equal(output.endsWith('\n'), true)
assert.equal(output.split('\n').filter(Boolean).length, 1)
assert.deepEqual(JSON.parse(output), {
  format: 'player-snapshot-v1-full-payload-rehearsal', version: '1', classification: 'MATCH',
  commandId: 'c9500000-0000-0000-0000-000000000001',
  characterId: 'a9500000-0000-0000-0000-000000000001',
})
NODE

reader_contract="$(run_reader --tuples-only --no-align --command="select (current_user = 'mud_replay_reader_login' and session_user = 'mud_replay_reader_login' and current_user = session_user and current_setting('default_transaction_read_only') = 'on' and current_setting('transaction_read_only') = 'on' and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'insert') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'update') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'delete') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'truncate') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'references') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'trigger'))::text")"
[[ "$reader_contract" == true ]] || { echo "reader login/session/read-only or mutation privilege contract failed" >&2; exit 1; }

full_payload_reader_contract="$(run_full_payload_reader --tuples-only --no-align --command="select (current_user = 'mud_full_payload_rehearsal_reader_login' and session_user = 'mud_full_payload_rehearsal_reader_login' and current_user = session_user and current_setting('default_transaction_read_only') = 'on' and current_setting('transaction_read_only') = 'on' and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'insert') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'update') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'delete') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'truncate') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'references') and not has_table_privilege(current_user, 'private.game_character_player_snapshot_v1_artifacts', 'trigger') and not has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'insert') and not has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'update') and not has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'delete') and not has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'truncate') and not has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'references') and not has_table_privilege(current_user, 'private.game_character_shadow_receipts', 'trigger'))::text")"
[[ "$full_payload_reader_contract" == true ]] || { echo "full payload reader login/session/read-only or mutation privilege contract failed" >&2; exit 1; }

full_payload_evidence="$(run_full_payload_reader --tuples-only --no-align --command="select (count(*) = 1 and bool_and(a.character_id = 'a9500000-0000-0000-0000-000000000001'::uuid and a.command_id = 'c9500000-0000-0000-0000-000000000001'::uuid and a.world_id = 'm5e-reader' and a.legacy_name_key = 'M5ereader' and a.receipt_request_sha256 = repeat('a', 64) and a.writer_instance_id = 'b9500000-0000-0000-0000-000000000001'::uuid and a.writer_epoch = 1 and a.writer_revision = 1 and a.source_post_sha256 = repeat('b', 64) and a.source_octets = 9 and a.storage_format = 1 and a.receipt_acknowledged_at = timestamptz '2026-10-04 00:00:00+00' and a.snapshot_format = 'player-snapshot-v1' and a.snapshot_sha256 = encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex') and a.snapshot_octets = octet_length(decode('$fixture_hex', 'hex')) and encode(a.payload, 'hex') = '$fixture_hex' and r.character_id = a.character_id and r.command_id = a.command_id and r.world_id = a.world_id and r.legacy_name_key = a.legacy_name_key and r.request_sha256 = a.receipt_request_sha256 and r.writer_instance_id = a.writer_instance_id and r.writer_epoch = a.writer_epoch and r.writer_revision = a.writer_revision and r.post_sha256 = a.source_post_sha256 and r.storage_format = a.storage_format and r.acknowledged_at = a.receipt_acknowledged_at))::text from private.game_character_player_snapshot_v1_artifacts a join private.game_character_shadow_receipts r on r.character_id = a.character_id and r.command_id = a.command_id where a.command_id = 'c9500000-0000-0000-0000-000000000001'::uuid")"
[[ "$full_payload_evidence" == true ]] || { echo "full payload reader actual-login joined evidence check failed" >&2; exit 1; }
FULL_PAYLOAD_READER_MODULE="$full_payload_reader_module" \
FULL_PAYLOAD_READER_DATABASE_URL="$full_payload_reader_database_url" \
FULL_PAYLOAD_FIXTURE_HEX="$fixture_hex" \
node --input-type=module <<'NODE'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { pathToFileURL } from 'node:url'
const { PostgresPlayerSnapshotV1FullPayloadRehearsalReader } = await import(pathToFileURL(process.env.FULL_PAYLOAD_READER_MODULE).href)
const fixture = Buffer.from(process.env.FULL_PAYLOAD_FIXTURE_HEX, 'hex')
const reader = new PostgresPlayerSnapshotV1FullPayloadRehearsalReader(process.env.FULL_PAYLOAD_READER_DATABASE_URL)
try {
  const rows = await reader.findByCommandId('c9500000-0000-0000-0000-000000000001')
  assert.equal(rows.length, 1)
  const [row] = rows
  assert.equal(row.characterId, 'a9500000-0000-0000-0000-000000000001')
  assert.equal(row.commandId, 'c9500000-0000-0000-0000-000000000001')
  assert.equal(row.snapshotSha256, createHash('sha256').update(fixture).digest('hex'))
  assert.deepEqual(row.payload, fixture)
  assert.deepEqual(row.receipt, {
    characterId: row.characterId, commandId: row.commandId, worldId: row.worldId,
    legacyNameKey: row.legacyNameKey, requestSha256: row.receiptRequestSha256,
    writerInstanceId: row.writerInstanceId, writerEpoch: row.writerEpoch,
    writerRevision: row.writerRevision, postSha256: row.sourcePostSha256,
    storageFormat: row.storageFormat, acknowledgedAt: row.receiptAcknowledgedAt,
  })
} finally {
  await reader.close()
}
NODE
if run_full_payload_reader --command="select expected_state from private.game_character_shadow_receipts" >/dev/null 2>&1; then
  echo "full payload reader unexpectedly selected restricted receipt column" >&2; exit 1
fi
if run_full_payload_reader --command="insert into private.game_character_shadow_receipts default values" >/dev/null 2>&1; then
  echo "full payload reader unexpectedly mutated receipts" >&2; exit 1
fi

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

snapshot_sha256="$(run_super --tuples-only --no-align --command="select encode(public.digest(decode('$fixture_hex', 'hex'), 'sha256'), 'hex')")"
snapshot_octets="$(( ${#fixture_hex} / 2 ))"
JOURNAL_ROOT="$journal_root" \
SNAPSHOT_SHA256="$snapshot_sha256" \
SNAPSHOT_OCTETS="$snapshot_octets" \
READER_DATABASE_URL="$reader_database_url" \
node <<'NODE'
const fs = require('node:fs')
const path = require('node:path')
const root = process.env.JOURNAL_ROOT
const digest = process.env.SNAPSHOT_SHA256
const octets = Number(process.env.SNAPSHOT_OCTETS)
const request = 'a'.repeat(64)
const source = 'b'.repeat(64)
function entry(commandId, characterId, rawLevelU8) {
  return JSON.stringify({
    format: 'player-snapshot-v1-replay-shadow-journal', version: '2', commandId, characterId,
    receiptRequestSha256: request, sourcePostSha256: source, rawLevelU8,
    verification: {
      format: 'player-snapshot-v1-replay-verification', version: '2', algorithm: 'sha-256',
      inputDigest: digest, canonicalDigest: digest, canonicalOctets: octets, inventoryNodeCount: 0,
    },
  })
}
function fixture(name, files) {
  const directory = path.join(root, name)
  fs.mkdirSync(directory, { mode: 0o700 })
  fs.chmodSync(directory, 0o700)
  for (const [file, body] of Object.entries(files)) {
    const target = path.join(directory, file)
    fs.writeFileSync(target, body, { mode: 0o600 })
    fs.chmodSync(target, 0o600)
  }
}
fixture('match', {
  // The comparator contract is bytewise lexical filename order: 1, 10, 2.
  // Keep raw levels deliberately non-numeric so this catches numeric sorting.
  '1.json': entry('c9500000-0000-0000-0000-000000000001', 'a9500000-0000-0000-0000-000000000001', 42),
  '10.json': entry('c9500000-0000-0000-0000-000000000011', 'a9500000-0000-0000-0000-000000000011', 255),
  '2.json': entry('c9500000-0000-0000-0000-000000000010', 'a9500000-0000-0000-0000-000000000010', 0),
})
fixture('mismatch', {
  'entry.json': entry('c9500000-0000-0000-0000-000000000001', 'a9500000-0000-0000-0000-000000000001', 41),
})
fixture('missing', {
  'entry.json': entry('c9500000-0000-0000-0000-000000000012', 'a9500000-0000-0000-0000-000000000012', 42),
})
fixture('duplicate', {
  'entry.json': entry('c9500000-0000-0000-0000-000000000013', 'a9500000-0000-0000-0000-000000000013', 42),
})
fixture('read-error', {
  'entry.json': entry('c9500000-0000-0000-0000-000000000001', 'a9500000-0000-0000-0000-000000000001', 42),
})
fixture('sanitized', {
  'entry.json': JSON.stringify({
    payload: 'legacy-payload-sentinel', rawError: 'raw database error', databaseCredential: process.env.READER_DATABASE_URL,
  }),
})
NODE

run_comparator_case match 0 MATCH MATCH,MATCH,MATCH 42,255,0
run_comparator_case mismatch 1 INCONSISTENT MISMATCH_LEVEL 41
run_comparator_case missing 1 INCONSISTENT MISSING_PROJECTION 42
run_comparator_case duplicate 1 INCONSISTENT UNEXPECTED_DUPLICATE 42
run_comparator_case sanitized 1 INCONSISTENT JOURNAL_INVALID ''
run_super --command='revoke select (command_id, character_id, receipt_request_sha256, source_post_sha256, snapshot_sha256, snapshot_octets, raw_level_u8) on table private.game_character_player_snapshot_v1_level_projections from mud_replay_reader_login;'
run_comparator_case read-error 1 INCONSISTENT PROJECTION_READ_ERROR 42
run_super --file=/workspace/supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql

after_fingerprint="$(run_super --tuples-only --no-align --command="select count(*)::text || ':' || coalesce(string_agg(character_id::text || command_id::text || snapshot_sha256 || md5(payload), ',' order by character_id, command_id), '') from private.game_character_player_snapshot_v1_artifacts")"
[[ "$before_fingerprint" == "$after_fingerprint" ]] || { echo "reader activity changed artifact data" >&2; exit 1; }
after_projection_fingerprint="$(run_super --tuples-only --no-align --command="select count(*)::text || ':' || coalesce(string_agg(character_id::text || command_id::text || receipt_request_sha256 || source_post_sha256 || snapshot_sha256 || snapshot_octets::text || raw_level_u8::text, ',' order by character_id, command_id), '') from private.game_character_player_snapshot_v1_level_projections")"
[[ "$before_projection_fingerprint" == "$after_projection_fingerprint" ]] || { echo "reader activity changed level projection data" >&2; exit 1; }

echo "GREEN PostgreSQL 17 M5e replay reader login and v2 comparator are metadata-only, read-only, and non-writer"

# Separate bank shadow contracts keep their own rolled-back fixtures; never
# confuse topology/value evidence with a complete bank restoration payload.
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261005000000_bank_snapshot_v1_topology_shadow.sql
  run_super --file=/workspace/supabase/migrations/20261007000000_bank_snapshot_v1_root_value_shadow.sql
done
run_super --file=/workspace/supabase/tests/bank_snapshot_v1_topology_shadow_contract.sql
run_super --file=/workspace/supabase/tests/bank_snapshot_v1_root_value_shadow_contract.sql
echo 'GREEN bank topology and root-value SQL shadow contracts'
if run_super --file=/workspace/supabase/tests/bank_snapshot_v1_payload_contract.sql; then
  echo 'bank payload contract unexpectedly passed before its migration' >&2
  exit 1
fi
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261016000000_bank_snapshot_v1_payload.sql
done
run_super --file=/workspace/supabase/tests/bank_snapshot_v1_payload_contract.sql
echo 'GREEN complete bank payload persistence contract'
BANK_PAYLOAD_LOCAL_DISPOSABLE=1 BANK_PAYLOAD_LOCAL_PORT="$postgres_port" \
  node "$repo_root/services/m4-file-snapshot-manifest-relay/test/bank-payload-local-pg.mjs"
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261017000000_paired_snapshot_transaction_kernel.sql
done
run_super --set="fixture_hex=$(tr -d '\r\n' < "$repo_root/tests/fixtures/player_snapshot_v1_canonical.hex")" \
  --set="next_fixture_hex=$(tr -d '\r\n' < "$repo_root/tests/fixtures/player_snapshot_v1_tree_inventory.hex")" \
  --file=/workspace/supabase/tests/paired_snapshot_transaction_contract.sql
echo 'GREEN paired snapshot transaction rollback and retry contract'
BANK_PAYLOAD_LOCAL_DISPOSABLE=1 BANK_PAYLOAD_LOCAL_PORT="$postgres_port" \
  node "$repo_root/services/m4-file-snapshot-manifest-relay/test/paired-snapshot-concurrency-local-pg.mjs"
CARGO_TARGET_DIR="$repo_root/rust/target" cargo test --locked --offline --manifest-path "$repo_root/rust/Cargo.toml" -p muhan-core-dto --bin bank_money_transfer_plan
CARGO_TARGET_DIR="$repo_root/rust/target" cargo build --locked --offline --release --manifest-path "$repo_root/rust/Cargo.toml" -p muhan-core-dto --bin bank_money_transfer_plan
make -C "$repo_root/src" bank-transfer-snapshot-v1-test
BANK_TRANSFER_C_ENCODER="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_transfer_snapshot_v1_test" \
  BANK_TRANSFER_PLANNER="$repo_root/rust/target/release/bank_money_transfer_plan" \
  node "$repo_root/services/m4-file-snapshot-manifest-relay/test/c-bank-transfer-rust.mjs"
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261018000000_money_transfer_semantics.sql
done
BANK_PAYLOAD_LOCAL_DISPOSABLE=1 BANK_PAYLOAD_LOCAL_PORT="$postgres_port" \
  BANK_TRANSFER_PLANNER="$repo_root/rust/target/release/bank_money_transfer_plan" \
  node "$repo_root/services/m4-file-snapshot-manifest-relay/test/bank-transfer-rust-pg.mjs"
# Reuse the fully constrained seed under a separate identity namespace: the
# historical reader-negative fixtures intentionally already occupy 9500000.
sed -e 's/9500000/9220000/g' -e 's/backup-contract/paired-baseline/g' "$repo_root/supabase/tests/replay_backup_valid_seed.sql" |
  run_super --set="fixture_hex=$(tr -d '\r\n' < "$repo_root/tests/fixtures/player_snapshot_v1_canonical.hex")" --file=-
if run_super --file=/workspace/supabase/tests/paired_snapshot_baseline_contract.sql; then
  echo 'RED unexpectedly passed before paired baseline migration' >&2
  exit 1
fi
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261021000000_paired_snapshot_baseline.sql
done
run_super --file=/workspace/supabase/tests/paired_snapshot_baseline_contract.sql
echo 'GREEN receipt-bound paired baseline rejects stale heads and never resets advanced state'
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261019000000_money_transfer_authority.sql
done
run_super --file=/workspace/supabase/tests/money_transfer_authority_contract.sql
echo 'GREEN money transfer owner session and writer authority contract'
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261020000000_qualified_money_transfer.sql
done
if run_super --command="select 'private.read_qualified_money_transfer_state(uuid,text,uuid,uuid,text,uuid,bigint)'::regprocedure"; then
  echo 'RED unexpectedly found qualified read before migration' >&2
  exit 1
fi
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261022000000_qualified_money_transfer_read.sql
done
for pass in 1 2; do
  run_super --file=/workspace/supabase/migrations/20261023000000_money_transfer_reconciliation.sql
done
cc -std=gnu89 -Wall -Wextra -Werror -I"$repo_root/src" -I"$(pg_config --includedir)" \
  -O1 -fsanitize=address,undefined -fno-omit-frame-pointer \
  "$repo_root/src/bank_money_read_native.c" "$repo_root/tests/unit/bank_money_read_native_pg.c" \
  -L"$(pg_config --libdir)" -lpq -o "${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_read_native_pg"
bank_codec_objects=()
for bank_codec in files1 player_record_serializer player_snapshot_v1 object_graph_v1 cdto_v1 bank_snapshot_v1 bank_money_result_native bank_money_command_native bank_money_route bank; do
  bank_codec_object="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank-native-${bank_codec}.o"
  cc -std=gnu89 -fcommon -DMUHAN_BANK_MONEY_ROUTING -ffunction-sections -fdata-sections -O1 -fsanitize=address,undefined -fno-omit-frame-pointer \
    -I"$repo_root/src" -c "$repo_root/src/${bank_codec}.c" -o "$bank_codec_object"
  bank_codec_objects+=("$bank_codec_object")
done
cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -Wall -Wextra -Werror -O1 -fsanitize=address,undefined -fno-omit-frame-pointer \
  -I"$repo_root/src" -I"$(pg_config --includedir)" \
  "$repo_root/src/bank_money_read_native.c" "$repo_root/src/bank_money_commit_native.c" "$repo_root/src/bank_money_plan_native.c" "$repo_root/src/bank_money_coordinate_native.c" "$repo_root/src/bank_money_live_native.c" "$repo_root/src/bank_money_live_snapshot.c" "$repo_root/tests/unit/bank_money_commit_native_pg.c" "${bank_codec_objects[@]}" \
  "$repo_root/tests/unit/bank_money_command_pg_stubs.c" \
  -Wl,--gc-sections -L"$(pg_config --libdir)" -lpq -o "${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_commit_native_pg"
cc -std=gnu89 -Wall -Wextra -Werror -O1 -fsanitize=address,undefined -fno-omit-frame-pointer -I"$repo_root/src" \
  "$repo_root/src/bank_money_plan_native.c" "$repo_root/tests/unit/bank_money_plan_native_runner.c" \
  -o "${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_plan_native_runner"
cc -std=gnu89 -Wall -Wextra -Werror "$repo_root/tests/unit/bank_money_plan_stalled_child.c" \
  -o "${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_plan_stalled_child"
BANK_PAYLOAD_LOCAL_DISPOSABLE=1 BANK_PAYLOAD_LOCAL_PORT="$postgres_port" BANK_TRANSFER_QUALIFIED=1 \
  BANK_TRANSFER_STALLED_PLANNER="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_plan_stalled_child" \
  BANK_TRANSFER_NATIVE_PLANNER="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_plan_native_runner" \
  BANK_TRANSFER_NATIVE_COMMIT="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_commit_native_pg" \
  BANK_TRANSFER_NATIVE_READER="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/bank_money_read_native_pg" \
  BANK_TRANSFER_PLANNER="$repo_root/rust/target/release/bank_money_transfer_plan" \
  node "$repo_root/services/m4-file-snapshot-manifest-relay/test/bank-transfer-rust-pg.mjs"
