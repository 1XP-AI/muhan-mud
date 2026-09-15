#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_LIVE_SAVE_ROUTE_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 live save route PG17 integration skipped (set M3_LIVE_SAVE_ROUTE_ALLOW_DISPOSABLE=1)"
  exit 0
}

command -v docker >/dev/null || {
  echo "m3 live save route PG17 integration requires docker" >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="m3-live-save-route-${RANDOM}-${RANDOM}"
container_id=""

cleanup() {
  if [[ "$container_id" =~ ^[0-9a-f]{64}$ ]]; then docker rm --force "$container_id" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

container_id="$(docker run --detach --rm --name "$container" \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=128m \
  --volume "$repo_root:/workspace:ro" \
  postgres:17-alpine)"
container="$container_id"

# The image entrypoint briefly exposes its initialization server before
# restarting PostgreSQL for normal service.  Require two consecutive real
# queries so the harness cannot mistake that transient window for readiness.
ready_streak=0
for _ in $(seq 1 60); do
  if docker exec --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --tuples-only --no-align --command='select 1' \
      >/dev/null 2>&1; then
    ready_streak=$((ready_streak + 1))
    if [[ "$ready_streak" -ge 2 ]]; then
      break
    fi
  else
    ready_streak=0
  fi
  sleep 1
done
if [[ "$ready_streak" -lt 2 ]]; then
  docker logs "$container" >&2 || true
  echo "m3 live save route PG17 integration did not reach stable readiness" >&2
  exit 2
fi

run_super() {
  docker exec --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}

run_super --file=/workspace/supabase/tests/bootstrap_contract.sql
for migration in \
  20260902000000_game_identity.sql \
  20260903000000_character_onboarding.sql \
  20260904000000_onboarding_intent_safety.sql \
  20260905000000_claim_fingerprint_safety.sql \
  20260906000000_claim_actor_rate_limit.sql \
  20260907000000_claim_challenge_safety.sql \
  20260908000000_service_rpc_fresh_clock.sql \
  20260909000000_m3_shadow_receipts.sql \
  20260910000000_m3_shadow_receipt_route_v2.sql \
  20260911000000_m3_writer_session.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

if run_super --file=/workspace/supabase/tests/m3_live_save_route_contract.sql >/dev/null 2>&1; then
  echo "m3 live save route RED unexpectedly passed before migration 120" >&2
  exit 1
fi
echo "RED PostgreSQL 17: v3 head-aware save route is absent through migration 110"

run_super --file=/workspace/supabase/migrations/20260912000000_m3_live_save_route.sql
run_super --file=/workspace/supabase/migrations/20260912000000_m3_live_save_route.sql
run_super --file=/workspace/supabase/tests/m3_live_save_route_contract.sql
echo "GREEN PostgreSQL 17: v3 route contract, malformed-row rejection, permissions, no-mutation checks, and migration replay passed"
