#!/usr/bin/env bash
set -Eeuo pipefail

# Hermetic first-vertical-slice runner. Every Docker object is uniquely named,
# has no host volume, and is removed by the trap below. Do not point this at a
# developer Supabase project: the SQL is intentionally a disposable contract.
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
run_id="stack-e2e-$(date +%s)-$$"
network_name="${run_id}-net"
postgres_name="${run_id}-postgres"
postgrest_name="${run_id}-postgrest"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-stack-e2e.XXXXXX")"
# NodeReconcilerFilesystem refuses symlinked path components. macOS exposes
# /var as a symlink, so canonicalize the disposable root before handing it to
# the actual reconciler process; Linux leaves this unchanged.
work_dir="$(cd "$work_dir" && pwd -P)"
pg_password="stack-e2e-postgres-password"
jwt_secret="stack-e2e-jwt-secret-not-for-production"
m3_writer_password="stack-e2e-m3-writer-password"
network_created=0
postgres_created=0
postgrest_created=0

docker_diagnostics() {
  echo "stack-e2e: Docker root and read-only space diagnostics:" >&2
  docker info --format '  root={{.DockerRootDir}} server={{.ServerVersion}}' 2>&1 || true
  docker system df 2>&1 || true
}

cleanup() {
  local status="$?"
  trap - EXIT INT TERM
  set +e
  if [[ "$postgrest_created" -eq 1 ]]; then docker rm -f "$postgrest_name" >/dev/null 2>&1; fi
  if [[ "$postgres_created" -eq 1 ]]; then docker rm -f "$postgres_name" >/dev/null 2>&1; fi
  if [[ "$network_created" -eq 1 ]]; then docker network rm "$network_name" >/dev/null 2>&1; fi
  rm -rf "$work_dir"
  exit "$status"
}
trap cleanup EXIT INT TERM
trap 'exit 130' INT
trap 'exit 143' TERM

# This runner creates disposable Docker/PostgreSQL/MUD processes and is an
# explicit CI gate only.  Local development must use its hermetic unit/static
# contracts; do not start this stack outside CI.
if [[ "${CI:-}" != "true" ]]; then
  echo "stack-e2e: CI-only disposable gate (set CI=true in the CI job)" >&2
  exit 2
fi

for command in docker node pnpm make curl; do
  command -v "$command" >/dev/null || {
    echo "stack-e2e: required command not found: $command" >&2
    exit 2
  }
done

# A collision is treated as a setup failure; cleanup must never remove an
# object that this invocation did not create.
if docker inspect "$postgres_name" >/dev/null 2>&1 || docker inspect "$postgrest_name" >/dev/null 2>&1 || docker network inspect "$network_name" >/dev/null 2>&1; then
  echo "stack-e2e: generated Docker name already exists: $run_id" >&2
  exit 2
fi

if ! docker network create --internal "$network_name" >/dev/null; then
  echo "stack-e2e: BLOCKED (Docker network setup failed; no Docker resources were removed)" >&2
  docker_diagnostics
  exit 2
fi
network_created=1
if ! docker run -d --name "$postgres_name" --network "$network_name" \
  -p 127.0.0.1::5432 \
  --mount type=tmpfs,destination=/var/lib/postgresql/data,tmpfs-size=512m \
  -e POSTGRES_PASSWORD="$pg_password" -e POSTGRES_DB=stack_e2e \
  postgres:17-alpine >/dev/null; then
  echo "stack-e2e: BLOCKED (PostgreSQL Docker container could not start; no prune was attempted)" >&2
  docker_diagnostics
  exit 2
fi
postgres_created=1

postgres_ready=0
for _ in $(seq 1 60); do
  if docker exec "$postgres_name" pg_isready -U postgres -d stack_e2e >/dev/null 2>&1; then
    postgres_ready=1
    break
  fi
  if [[ "$(docker inspect -f '{{.State.Running}}' "$postgres_name" 2>/dev/null || true)" != "true" ]]; then
    break
  fi
  sleep 1
done
if [[ "$postgres_ready" -ne 1 ]]; then
  echo "stack-e2e: BLOCKED (PostgreSQL did not become ready; no prune was attempted)" >&2
  docker logs --tail 80 "$postgres_name" >&2 || true
  docker_diagnostics
  exit 2
fi

apply_sql() {
  docker exec -i "$postgres_name" psql -v ON_ERROR_STOP=1 -U postgres -d stack_e2e < "$1" >/dev/null
}
apply_sql "$repo_root/supabase/tests/bootstrap_contract.sql"
apply_sql "$repo_root/supabase/migrations/20260902000000_game_identity.sql"
apply_sql "$repo_root/supabase/migrations/20260903000000_character_onboarding.sql"
apply_sql "$repo_root/supabase/migrations/20260904000000_onboarding_intent_safety.sql"
apply_sql "$repo_root/supabase/migrations/20260905000000_claim_fingerprint_safety.sql"
apply_sql "$repo_root/supabase/migrations/20260906000000_claim_actor_rate_limit.sql"
apply_sql "$repo_root/supabase/migrations/20260907000000_claim_challenge_safety.sql"
apply_sql "$repo_root/supabase/migrations/20260908000000_service_rpc_fresh_clock.sql"
# Apply the complete handoff/provenance lane so the stack test exercises an
# importer-admitted legacy row through claim activation, never a fixture-only
# lifecycle relabel.
apply_sql "$repo_root/supabase/migrations/20260909000000_m3_shadow_receipts.sql"
apply_sql "$repo_root/supabase/migrations/20260910000000_m3_shadow_receipt_route_v2.sql"
apply_sql "$repo_root/supabase/migrations/20260911000000_m3_writer_session.sql"
apply_sql "$repo_root/supabase/migrations/20260912000000_m3_live_save_route.sql"
apply_sql "$repo_root/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql"
apply_sql "$repo_root/supabase/migrations/20260914000000_m4_file_snapshot_manifest.sql"
apply_sql "$repo_root/supabase/migrations/20260915000000_player_snapshot_v1_artifacts.sql"
apply_sql "$repo_root/supabase/migrations/20260916000000_player_snapshot_v1_receipt_octets_binding.sql"
apply_sql "$repo_root/supabase/migrations/20260917000000_player_snapshot_v1_replay_reader.sql"
apply_sql "$repo_root/supabase/migrations/20260918000000_m3_absent_head_seed.sql"
apply_sql "$repo_root/supabase/migrations/20260919000000_player_snapshot_v1_level_projection.sql"
apply_sql "$repo_root/supabase/migrations/20260920000000_player_snapshot_v1_level_projection_replay_reader.sql"
apply_sql "$repo_root/supabase/migrations/20260921000000_legacy_identity_evidence_binding.sql"
apply_sql "$repo_root/supabase/migrations/20260922000000_onboarding_handoff_lifecycle.sql"
apply_sql "$repo_root/supabase/migrations/20260922100000_onboarding_handoff_gate.sql"
apply_sql "$repo_root/supabase/migrations/20260923000000_onboarding_snapshot_eligibility_outbox.sql"
apply_sql "$repo_root/supabase/migrations/20260924000000_onboarding_snapshot_fulfillment.sql"
apply_sql "$repo_root/supabase/migrations/20260925000000_onboarding_snapshot_fulfillment_corrective.sql"
apply_sql "$repo_root/supabase/migrations/20260926000000_onboarding_snapshot_fulfillment_terminal_semantics.sql"
apply_sql "$repo_root/supabase/migrations/20260927000000_onboarding_snapshot_command_binding.sql"
apply_sql "$repo_root/supabase/migrations/20260928000000_imported_unclaimed_batch_ledger.sql"
apply_sql "$repo_root/supabase/migrations/20260929000000_imported_unclaimed_batch_character_provenance.sql"
apply_sql "$repo_root/supabase/migrations/20260930000000_imported_unclaimed_claim_provenance_gate.sql"
apply_sql "$repo_root/supabase/migrations/20261001000000_imported_unclaimed_batch_member_legacy_locator.sql"
apply_sql "$repo_root/supabase/migrations/20261008000000_imported_unclaimed_batch_member_identity.sql"
apply_sql "$repo_root/supabase/migrations/20261009000000_imported_unclaimed_historic_batch_tuple_gate.sql"

postgres_port="$(docker port "$postgres_name" 5432/tcp 2>/dev/null | sed -n '1s/.*://p' || true)"
if [[ -z "$postgres_port" ]]; then
  echo "stack-e2e: BLOCKED (PostgreSQL host port was not published; no prune was attempted)" >&2
  exit 2
fi

if ! docker create --name "$postgrest_name" --network bridge \
  -p 127.0.0.1::3000 \
  -e PGRST_DB_URI="postgres://postgres:${pg_password}@${postgres_name}:5432/stack_e2e" \
  -e PGRST_DB_SCHEMAS=public -e PGRST_DB_ANON_ROLE=anon \
  -e PGRST_JWT_SECRET="$jwt_secret" -e PGRST_SERVER_PORT=3000 \
  postgrest/postgrest:v12.2.8 >/dev/null; then
  echo "stack-e2e: BLOCKED (PostgREST Docker container could not start; no prune was attempted)" >&2
  docker_diagnostics
  exit 2
fi
postgrest_created=1
# PostgREST gets a loopback-only host port via the default bridge, then joins
# the private network for database access. PostgreSQL itself remains internal.
if ! docker network connect "$network_name" "$postgrest_name" || ! docker start "$postgrest_name" >/dev/null; then
  echo "stack-e2e: BLOCKED (PostgREST could not join the private network; no prune was attempted)" >&2
  docker_diagnostics
  exit 2
fi

postgrest_port="$(docker port "$postgrest_name" 3000/tcp 2>/dev/null | sed -n '1s/.*://p' || true)"
for _ in $(seq 1 60); do
  if [[ -n "$postgrest_port" ]] && curl --max-time 2 -fsS "http://127.0.0.1:${postgrest_port}/" >/dev/null 2>&1; then break; fi
  sleep 1
  postgrest_port="$(docker port "$postgrest_name" 3000/tcp 2>/dev/null | sed -n '1s/.*://p' || true)"
done
if [[ -z "$postgrest_port" ]] || ! curl --max-time 2 -fsS "http://127.0.0.1:${postgrest_port}/" >/dev/null 2>&1; then
  echo "stack-e2e: BLOCKED (PostgREST health check failed; no prune was attempted)" >&2
  docker logs --tail 80 "$postgrest_name" >&2 || true
  docker_diagnostics
  exit 2
fi

# The stack acceptance runs the real opt-in M3 graph.  This role/password and
# conninfo are created only in this runner's disposable PostgreSQL instance,
# never in a developer database or a checked-in fixture.
docker exec "$postgres_name" psql -U postgres -d stack_e2e -v ON_ERROR_STOP=1 \
  -c "alter role mud_writer_login password '${m3_writer_password}'" >/dev/null
m3_conninfo_file="$work_dir/m3-writer.conninfo"
umask 077
printf '%s' "postgresql://mud_writer_login:${m3_writer_password}@127.0.0.1:${postgres_port}/stack_e2e?sslmode=disable" >"$m3_conninfo_file"
chmod 600 "$m3_conninfo_file"

# The test uses a signed service_role JWT solely for PostgREST's role switch.
# The browser token is a deterministic authenticator fixture inside the test.
service_role_jwt="$(STACK_E2E_JWT_SECRET="$jwt_secret" node -e 'const c=require("node:crypto"); const b=x=>Buffer.from(JSON.stringify(x)).toString("base64url"); const h=b({alg:"HS256",typ:"JWT"})+"."+b({role:"service_role",aud:"authenticated",exp:4102444800}); process.stdout.write(h+"."+c.createHmac("sha256",process.env.STACK_E2E_JWT_SECRET).update(h).digest("base64url"))')"

echo "stack-e2e: RED (preflight)"
build_log="$work_dir/build.log"
if ! make -B -C "$repo_root/src" -j2 CC=gcc USE_M3_RUNTIME=1 PG_CONFIG=pg_config OUTFILE="$work_dir/frp.new" >"$build_log" 2>&1; then
  echo "stack-e2e: RED (preflight build failed)" >&2
  tail -n 80 "$build_log" >&2
  exit 1
fi
echo "stack-e2e: GREEN (preflight)"

test_status=0
STACK_E2E_PG_CONTAINER="$postgres_name" \
STACK_E2E_PG_PASSWORD="$pg_password" \
STACK_E2E_M3_ENABLED=1 \
STACK_E2E_M3_WRITER_PASSWORD="$m3_writer_password" \
STACK_E2E_M3_CONNINFO_FILE="$m3_conninfo_file" \
STACK_E2E_M3_DATABASE_URL="postgresql://mud_writer_login:${m3_writer_password}@127.0.0.1:${postgres_port}/stack_e2e?sslmode=disable" \
STACK_E2E_ROOT="$repo_root" \
STACK_E2E_FIXTURE="$work_dir/fixture" \
STACK_E2E_REST_URL="http://127.0.0.1:${postgrest_port}" \
STACK_E2E_DATABASE_URL="postgres://postgres:${pg_password}@127.0.0.1:${postgres_port}/stack_e2e" \
STACK_E2E_SERVICE_ROLE_JWT="$service_role_jwt" \
STACK_E2E_JWT_SECRET="$jwt_secret" \
STACK_E2E_BINARY="$work_dir/frp.new" \
STACK_E2E_ARTIFACT="$work_dir/result.json" \
ADMISSION_IDENTITY_PG17_ALLOW_DISPOSABLE=1 \
  pnpm --dir "$repo_root/services/gateway" exec tsx --test \
    "$repo_root/tests/stack-e2e/admission-identity-pg17.integration.test.ts" \
    "$repo_root/tests/stack-e2e/stack-e2e.test.ts" || test_status=$?

if [[ -n "${STACK_E2E_OUTPUT:-}" ]] && [[ -f "$work_dir/result.json" ]]; then
  cp "$work_dir/result.json" "$STACK_E2E_OUTPUT"
fi
if [[ "$test_status" -ne 0 ]]; then
  exit "$test_status"
fi

echo "stack-e2e: passed; all credentials and game passwords were redacted from evidence"
