#!/usr/bin/env bash
set -euo pipefail

[[ "${PLAYER_SNAPSHOT_V1_ARTIFACT_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "player snapshot v1 artifact PG17 integration skipped (set PLAYER_SNAPSHOT_V1_ARTIFACT_ALLOW_DISPOSABLE=1)"
  exit 0
}
command -v docker >/dev/null || { echo "player snapshot v1 artifact integration requires docker" >&2; exit 2; }

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="player-snapshot-v1-${RANDOM}-${RANDOM}"
container_id=""
fixture_path="$repo_root/tests/fixtures/player_snapshot_v1_canonical.hex"
fixture_hex="$(tr -d '\r\n' < "$fixture_path")"
[[ "$fixture_hex" =~ ^[0-9a-f]+$ && "${#fixture_hex}" -eq 3556 ]] || {
  echo "PlayerSnapshotV1 C fixture is not canonical lowercase hex" >&2; exit 2;
}
inventory_fixture_path="$repo_root/tests/fixtures/player_snapshot_v1_one_inventory_item.hex"
inventory_fixture_hex="$(tr -d '\r\n' < "$inventory_fixture_path")"
[[ "$inventory_fixture_hex" =~ ^[0-9a-f]+$ && "${#inventory_fixture_hex}" -eq 4268 ]] || {
  echo "PlayerSnapshotV1 C inventory fixture is not canonical lowercase hex" >&2; exit 2;
}
tree_fixture_path="$repo_root/tests/fixtures/player_snapshot_v1_tree_inventory.hex"
tree_fixture_hex="$(tr -d '\r\n' < "$tree_fixture_path")"
[[ "$tree_fixture_hex" =~ ^[0-9a-f]+$ && "${#tree_fixture_hex}" -eq 7116 ]] || {
  echo "PlayerSnapshotV1 C tree fixture is not canonical lowercase hex" >&2; exit 2;
}
fixture_sql="decode('$fixture_hex', 'hex')"
inventory_fixture_sql="decode('$inventory_fixture_hex', 'hex')"
tree_fixture_sql="decode('$tree_fixture_hex', 'hex')"
cleanup() { if [[ "$container_id" =~ ^[0-9a-f]{64}$ ]]; then docker rm --force "$container_id" >/dev/null 2>&1 || true; fi; }
trap cleanup EXIT
container_id="$(docker run --detach --rm --name "$container" \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=192m \
  --volume "$repo_root:/workspace:ro" postgres:17-alpine)"
container="$container_id"
run_super() {
  docker exec --interactive --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}
ready=0
for _ in $(seq 1 60); do
  if run_super --command='select 1' >/dev/null 2>&1; then ready=$((ready+1)); else ready=0; fi
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
if run_super --set="pva_payload=$fixture_sql" --set="pva_inventory_payload=$inventory_fixture_sql" \
  --set="pva_tree_inventory_payload=$tree_fixture_sql" \
  --file=/workspace/supabase/tests/player_snapshot_v1_artifact_contract.sql >/dev/null 2>&1; then
  echo "RED unexpectedly passed through migration 140" >&2; exit 1
fi
echo "RED PostgreSQL 17: PlayerSnapshotV1 artifact RPCs are absent through migration 140"
run_super --file=/workspace/supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql
if run_super --set="pva_payload=$fixture_sql" --set="pva_inventory_payload=$inventory_fixture_sql" \
  --set="pva_tree_inventory_payload=$tree_fixture_sql" \
  --file=/workspace/supabase/tests/player_snapshot_v1_artifact_contract.sql >/dev/null 2>&1; then
  echo "RED unexpectedly accepted mismatched PlayerSnapshotV1 source octets through migration 150" >&2; exit 1
fi
echo "RED PostgreSQL 17: PlayerSnapshotV1 source octets are not receipt-bound through migration 150"
run_super --file=/workspace/supabase/migrations/20260916000000_player_snapshot_v1_receipt_octets_binding.sql
for missing_payload_variable in pva_payload pva_inventory_payload pva_tree_inventory_payload; do
  case "$missing_payload_variable" in
    pva_payload)
      contract_payload_args=(--set="pva_inventory_payload=$inventory_fixture_sql" --set="pva_tree_inventory_payload=$tree_fixture_sql")
      ;;
    pva_inventory_payload)
      contract_payload_args=(--set="pva_payload=$fixture_sql" --set="pva_tree_inventory_payload=$tree_fixture_sql")
      ;;
    pva_tree_inventory_payload)
      contract_payload_args=(--set="pva_payload=$fixture_sql" --set="pva_inventory_payload=$inventory_fixture_sql")
      ;;
  esac
  if run_super "${contract_payload_args[@]}" --file=/workspace/supabase/tests/player_snapshot_v1_artifact_contract.sql >/dev/null 2>&1; then
    echo "RED unexpectedly accepted missing $missing_payload_variable through migration 160" >&2; exit 1
  fi
done
echo "RED PostgreSQL 17: PlayerSnapshotV1 artifact contract rejects each missing payload variable"
run_super --set="pva_payload=$fixture_sql" --set="pva_inventory_payload=$inventory_fixture_sql" \
  --set="pva_tree_inventory_payload=$tree_fixture_sql" \
  --file=/workspace/supabase/tests/player_snapshot_v1_artifact_contract.sql
run_super --file=/workspace/supabase/migrations/20260916000000_player_snapshot_v1_receipt_octets_binding.sql
echo "GREEN PostgreSQL 17: PlayerSnapshotV1 artifact receipt-octets contract and idempotent replay passed"

# The level projection must remain unavailable until its own forward-only
# migration. It consumes only the receipt/source-octet-bound artifact above.
run_super --file=/workspace/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql
if run_super --set="pvl_payload=$fixture_sql" \
  --file=/workspace/supabase/tests/player_snapshot_v1_level_projection_contract.sql >/dev/null 2>&1; then
  echo "RED unexpectedly passed before PlayerSnapshotV1 level projection migration" >&2; exit 1
fi
echo "RED PostgreSQL 17: PlayerSnapshotV1 raw-U8 level projection is absent through migration 170"
run_super --file=/workspace/supabase/migrations/20260918000000_m3_absent_head_seed.sql
run_super --file=/workspace/supabase/migrations/20260918000000_m3_absent_head_seed.sql
run_super --file=/workspace/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql
run_super --file=/workspace/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql
run_super --set="pvl_payload=$fixture_sql" \
  --file=/workspace/supabase/tests/player_snapshot_v1_level_projection_contract.sql
echo "GREEN PostgreSQL 17: PlayerSnapshotV1 receipt-bound raw-U8 level projection passed"
