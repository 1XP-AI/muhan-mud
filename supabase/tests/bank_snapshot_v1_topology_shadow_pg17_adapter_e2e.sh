#!/usr/bin/env bash
set -euo pipefail

[[ "${BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "BankSnapshotV1 topology adapter PG17 E2E skipped (set BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ALLOW_DISPOSABLE=1)"
  exit 0
}
command -v docker >/dev/null || { echo "BankSnapshotV1 topology adapter PG17 E2E requires docker" >&2; exit 2; }

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="bank-topology-adapter-e2e-${RANDOM}-${RANDOM}"
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
  20260914000000_m4_file_snapshot_manifest.sql 20260915000000_player_snapshot_v1_artifacts.sql \
  20260916000000_player_snapshot_v1_receipt_octets_binding.sql 20260917000000_player_snapshot_v1_replay_reader.sql \
  20260918000000_m3_absent_head_seed.sql 20260919000000_player_snapshot_v1_level_projection.sql \
  20260920000000_player_snapshot_v1_level_projection_replay_reader.sql 20260921000000_legacy_identity_evidence_binding.sql \
  20260922000000_onboarding_handoff_lifecycle.sql 20260922100000_onboarding_handoff_gate.sql \
  20260923000000_onboarding_snapshot_eligibility_outbox.sql 20260924000000_onboarding_snapshot_fulfillment.sql \
  20260925000000_onboarding_snapshot_fulfillment_corrective.sql 20260926000000_onboarding_snapshot_fulfillment_terminal_semantics.sql \
  20260927000000_onboarding_snapshot_command_binding.sql 20260928000000_imported_unclaimed_batch_ledger.sql \
  20260929000000_imported_unclaimed_batch_character_provenance.sql 20260930000000_imported_unclaimed_claim_provenance_gate.sql \
  20261001000000_imported_unclaimed_batch_member_legacy_locator.sql 20261002000000_legacy_locator_resolver.sql \
  20261003000000_player_snapshot_v1_inventory_graph_shadow.sql 20261004000000_player_snapshot_v1_full_payload_rehearsal_reader.sql \
  20261005000000_bank_snapshot_v1_topology_shadow.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

run_super <<'SQL'
alter role mud_writer_login password 'contract-only-writer-password';
insert into public.game_characters (id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format)
values ('a9520000-0000-0000-0000-000000000001', 'bank-relay-e2e', 'E2ehero', 'E2ehero',
  substr(encode(public.digest(convert_to('E2ehero', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1);
insert into private.game_character_legacy_heads (character_id, head_state, storage_format, revision)
values ('a9520000-0000-0000-0000-000000000001', 'absent', 1, 0);
select * from private.acquire_game_world_writer_epoch(
  'bank-relay-e2e', 'b9520000-0000-0000-0000-000000000001'::uuid, clock_timestamp() + interval '3 minutes');
select private.game_character_shadow_request_sha256(
  'bank-relay-e2e', 'a9520000-0000-0000-0000-000000000001'::uuid, 'E2ehero',
  substr(encode(public.digest(convert_to('E2ehero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9520000-0000-0000-0000-000000000001'::uuid, 'b9520000-0000-0000-0000-000000000001'::uuid,
  1::bigint, 1::bigint, 'absent', null, repeat('a', 64), 1::smallint) as request_sha256 \gset bank_e2e_
select private.record_legacy_published_receipt(
  'bank-relay-e2e', 'E2ehero', 'a9520000-0000-0000-0000-000000000001'::uuid,
  'c9520000-0000-0000-0000-000000000001'::uuid, 'b9520000-0000-0000-0000-000000000001'::uuid,
  :'bank_e2e_request_sha256', 1::bigint, 1::bigint, 'absent', null, repeat('a', 64), 1::smallint);
insert into private.game_character_m4_file_snapshot_manifests (
  character_id, command_id, world_id, legacy_name_key, receipt_request_sha256,
  writer_instance_id, writer_epoch, writer_revision, file_post_sha256, storage_format,
  receipt_acknowledged_at, snapshot_format, snapshot_sha256, snapshot_octets
)
select character_id, command_id, world_id, legacy_name_key, request_sha256,
  writer_instance_id, writer_epoch, writer_revision, post_sha256, storage_format,
  acknowledged_at, 'legacy-file-manifest-v1', post_sha256, 9
from private.game_character_shadow_receipts
where command_id = 'c9520000-0000-0000-0000-000000000001'::uuid;
SQL

request_sha256="$(run_super --tuples-only --no-align --command="select request_sha256 from private.game_character_shadow_receipts where command_id = 'c9520000-0000-0000-0000-000000000001'::uuid")"
[[ "$request_sha256" =~ ^[0-9a-f]{64}$ ]] || { echo "BankSnapshotV1 topology adapter PG17 E2E setup failed" >&2; exit 2; }

docker run --rm --network "container:$container" \
  --volume "$repo_root/services/m4-file-snapshot-manifest-relay:/source:ro" \
  --env BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_DATABASE_URL="postgresql://mud_writer_login:contract-only-writer-password@127.0.0.1:5432/postgres" \
  --env BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_SUPER_DATABASE_URL="postgresql://postgres:contract-only-password@127.0.0.1:5432/postgres" \
  --env BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_REQUEST_SHA256="$request_sha256" \
  node:22.15.1-alpine3.21 sh -euc '
    mkdir -p /workspace
    cp -a /source/package.json /source/package-lock.json /source/tsconfig.json /source/src /source/test /workspace/
    cd /workspace
    npm ci --ignore-scripts --no-audit --no-fund
    node node_modules/tsx/dist/cli.mjs test/bank-snapshot-v1-topology-shadow-pg17-e2e.ts
  '
