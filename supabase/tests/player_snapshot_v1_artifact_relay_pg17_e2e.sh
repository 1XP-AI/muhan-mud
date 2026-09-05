#!/usr/bin/env bash
set -euo pipefail

[[ "${PLAYER_SNAPSHOT_V1_ARTIFACT_RELAY_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "PlayerSnapshotV1 relay PG17 E2E skipped (set PLAYER_SNAPSHOT_V1_ARTIFACT_RELAY_ALLOW_DISPOSABLE=1)"
  exit 0
}
command -v docker >/dev/null || { echo "PlayerSnapshotV1 relay PG17 E2E requires docker" >&2; exit 2; }

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="pva-relay-e2e-${RANDOM}-${RANDOM}"
cleanup() { docker rm --force "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run --detach --rm --name "$container" --publish 127.0.0.1::5432 \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=192m \
  --volume "$repo_root:/workspace:ro" postgres:17-alpine >/dev/null

run_super() {
  docker exec --interactive --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
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
  20260914000000_m4_file_snapshot_manifest.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done
if run_super --command="select private.record_player_snapshot_v1_artifact_for_receipt(null,null,null,null,null,null,null,null,null)" >/dev/null 2>&1; then
  echo "RED unexpectedly passed before PlayerSnapshotV1 artifact migration" >&2; exit 1
fi
echo "RED PostgreSQL 17: PlayerSnapshotV1 artifact relay RPC is absent through migration 140"
run_super --file=/workspace/supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql
run_super --file=/workspace/supabase/migrations/20260916000000_player_snapshot_v1_receipt_octets_binding.sql
# Forward dependencies are replayed deliberately: the disposable E2E depends
# on their idempotent role/RPC setup before it reaches migration 190.
run_super --file=/workspace/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql
run_super --file=/workspace/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql
run_super --file=/workspace/supabase/migrations/20260918000000_m3_absent_head_seed.sql
run_super --file=/workspace/supabase/migrations/20260918000000_m3_absent_head_seed.sql
run_super --file=/workspace/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql
run_super --file=/workspace/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql

run_super <<'SQL'
alter role mud_writer_login password 'contract-only-writer-password';
insert into public.game_characters (id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format)
values ('a9510000-0000-0000-0000-000000000001', 'pva-relay-e2e', 'E2ehero', 'E2ehero',
  substr(encode(public.digest(convert_to('E2ehero', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1);
insert into private.game_character_snapshots (character_id, revision, storage_format, blob_ref, sha256, saved_at)
values ('a9510000-0000-0000-0000-000000000001', 41, 1, 'relay-e2e-legacy-byte-sentinel', repeat('f', 64), '2026-09-01 00:00:00+00');
insert into private.game_character_legacy_heads (character_id, head_state, storage_format, revision)
values ('a9510000-0000-0000-0000-000000000001', 'absent', 1, 0);
select * from private.acquire_game_world_writer_epoch(
  'pva-relay-e2e', 'b9510000-0000-0000-0000-000000000001'::uuid, clock_timestamp() + interval '3 minutes');
select private.game_character_shadow_request_sha256(
  'pva-relay-e2e', 'a9510000-0000-0000-0000-000000000001'::uuid, 'E2ehero',
  substr(encode(public.digest(convert_to('E2ehero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9510000-0000-0000-0000-000000000001'::uuid, 'b9510000-0000-0000-0000-000000000001'::uuid,
  1::bigint, 1::bigint, 'absent', null, repeat('a', 64), 1::smallint) as request_sha256 \gset relay_e2e_
select private.record_legacy_published_receipt(
  'pva-relay-e2e', 'E2ehero', 'a9510000-0000-0000-0000-000000000001'::uuid,
  'c9510000-0000-0000-0000-000000000001'::uuid, 'b9510000-0000-0000-0000-000000000001'::uuid,
  :'relay_e2e_request_sha256', 1::bigint, 1::bigint, 'absent', null, repeat('a', 64), 1::smallint);
insert into private.game_character_m4_file_snapshot_manifests (
  character_id, command_id, world_id, legacy_name_key, receipt_request_sha256,
  writer_instance_id, writer_epoch, writer_revision, file_post_sha256, storage_format,
  receipt_acknowledged_at, snapshot_format, snapshot_sha256, snapshot_octets
)
select character_id, command_id, world_id, legacy_name_key, request_sha256,
  writer_instance_id, writer_epoch, writer_revision, post_sha256, storage_format,
  acknowledged_at, 'legacy-file-manifest-v1', post_sha256, 9
from private.game_character_shadow_receipts
where command_id = 'c9510000-0000-0000-0000-000000000001'::uuid;
SQL

request_sha256="$(run_super --tuples-only --no-align --command="select request_sha256 from private.game_character_shadow_receipts where command_id = 'c9510000-0000-0000-0000-000000000001'::uuid")"
[[ "$request_sha256" =~ ^[0-9a-f]{64}$ ]] || { echo "PlayerSnapshotV1 relay PG17 E2E setup failed" >&2; exit 2; }

# The descriptor-rooted adapter deliberately fails closed on macOS.  Run the
# actual Node path in a short-lived Linux container sharing only this test's
# disposable PostgreSQL network namespace and a container-local working directory.
docker run --rm --network "container:$container" \
  --volume "$repo_root/services/m4-file-snapshot-manifest-relay:/source:ro" \
  --env PLAYER_SNAPSHOT_V1_RELAY_E2E_DATABASE_URL="postgresql://mud_writer_login:contract-only-writer-password@127.0.0.1:5432/postgres" \
  --env PLAYER_SNAPSHOT_V1_RELAY_E2E_SUPER_DATABASE_URL="postgresql://postgres:contract-only-password@127.0.0.1:5432/postgres" \
  --env PLAYER_SNAPSHOT_V1_RELAY_E2E_REQUEST_SHA256="$request_sha256" \
  node:22.15.1-alpine3.21 sh -euc '
    mkdir -p /workspace
    cp -a /source/package.json /source/package-lock.json /source/tsconfig.json /source/src /source/test /workspace/
    cd /workspace
    npm ci --ignore-scripts --no-audit --no-fund
    node node_modules/tsx/dist/cli.mjs test/player-snapshot-v1-artifact-pg17-e2e.ts
  '
