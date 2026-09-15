#!/usr/bin/env bash
# Explicitly authorized local lane: no published ports, host mounts or socket.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable ]] || { echo 'usage: bash scripts/run-stack-e2e-local-docker.sh --allow-disposable' >&2; exit 2; }
lane="${STACK_E2E_LOCAL_LANE:-legacy}"
case "$lane" in
  legacy|go) ;;
  *)
    echo 'local-stack: STACK_E2E_LOCAL_LANE must be legacy or go' >&2
    exit 2
    ;;
esac
real_auth="${STACK_E2E_REAL_AUTH:-0}"
[[ "$real_auth" == 0 || "$real_auth" == 1 ]] || exit 2
[[ "$lane" != go || "$real_auth" == 0 ]] || {
  echo 'local-stack: STACK_E2E_REAL_AUTH is only supported by the legacy lane' >&2
  exit 2
}
root="$(cd "$(dirname "$0")/.." && pwd -P)"
scratch="$(mktemp -d /tmp/muhan-local-stack.XXXXXX)"
id="muhan-local-${lane}-stack-$(date +%s)-$$"
docker_cmd() { docker --context=default "$@"; }
network="$id-net"
pg="$id-pg"
rest="$id-rest"
runner="$id-runner"
dockerfile="Dockerfile.local"
[[ "$lane" == go ]] && dockerfile="Dockerfile.go-local"
created=()
network_created=0
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  # macOS ships Bash 3.2, where an empty array under nounset is unbound.
  if [[ "${#created[@]}" -gt 0 ]]; then
    for name in "${created[@]}"; do docker_cmd rm -f "$name" >/dev/null; done
  fi
  if [[ "$network_created" == 1 ]]; then docker_cmd network rm "$network" >/dev/null; fi
  echo "local-stack: source/build evidence retained at $scratch; status=$status"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# The default is a frozen committed snapshot; no working-tree files are copied.
git -C "$root" archive HEAD | tar -x -C "$scratch"
git -C "$root" rev-parse HEAD
# Explicit local Docker driver, regardless of the user's selected cloud builder.
docker_cmd buildx build --builder default --load -t "$id:local" -f "$scratch/tests/stack-e2e/$dockerfile" "$scratch"
docker_cmd network create --internal "$network" >/dev/null
network_created=1
docker_cmd create --name "$pg" --network "$network" --tmpfs /var/lib/postgresql/data:rw,size=512m -e POSTGRES_PASSWORD=stack-e2e-postgres-password -e POSTGRES_DB=stack_e2e postgres:17-alpine >/dev/null
created+=("$pg")
docker_cmd start "$pg" >/dev/null
auth_env=()
if [[ "$lane" == legacy && "$real_auth" == 1 ]]; then
  ready=0
  for attempt in $(seq 1 30); do
    if docker_cmd exec "$pg" pg_isready -U postgres >/dev/null 2>&1; then ready=1; break; fi
    sleep 1
  done
  [[ "$ready" == 1 ]] || exit 1
  docker_cmd exec -i "$pg" psql -X -U postgres -d stack_e2e -v ON_ERROR_STOP=1 < "$scratch/tests/stack-e2e/real-auth-bootstrap.sql" >/dev/null
  auth="$id-auth"
  docker_cmd create --name "$auth" --network "container:$pg" \
    -e GOTRUE_API_HOST=0.0.0.0 -e GOTRUE_API_PORT=9999 \
    -e API_EXTERNAL_URL=http://127.0.0.1:9999 -e GOTRUE_SITE_URL=http://127.0.0.1:9999 \
    -e GOTRUE_DB_DRIVER=postgres -e GOTRUE_DB_DATABASE_URL=postgres://supabase_auth_admin:local-auth-db-password@127.0.0.1:5432/stack_e2e \
    -e GOTRUE_JWT_SECRET=stack-e2e-jwt-secret-not-for-production \
    -e GOTRUE_JWT_ISSUER=http://127.0.0.1:9999 \
    -e GOTRUE_JWT_AUD=authenticated -e GOTRUE_JWT_DEFAULT_GROUP_NAME=authenticated \
    -e GOTRUE_EXTERNAL_EMAIL_ENABLED=true -e GOTRUE_MAILER_AUTOCONFIRM=true \
    -e GOTRUE_DISABLE_SIGNUP=false -e GOTRUE_EXTERNAL_PHONE_ENABLED=false \
    supabase/gotrue:v2.189.0 >/dev/null
  created=("$auth" "${created[@]}")
  docker_cmd start "$auth" >/dev/null
  ready=0
  for attempt in $(seq 1 45); do
    if docker_cmd exec "$pg" wget -q -O /dev/null http://127.0.0.1:9999/health >/dev/null 2>&1; then ready=1; break; fi
    [[ "$(docker_cmd inspect --format '{{.State.Running}}' "$auth")" == true ]] || break
    sleep 1
  done
  [[ "$ready" == 1 ]] || { echo 'local-stack: real Auth readiness failed' >&2; exit 1; }
  auth_env=(-e STACK_E2E_AUTH_URL=http://127.0.0.1:9999)
fi
if [[ "$lane" == go ]]; then
  ready=0
  for attempt in $(seq 1 60); do
    if docker_cmd exec "$pg" pg_isready -U postgres -d stack_e2e >/dev/null 2>&1; then ready=1; break; fi
    [[ "$(docker_cmd inspect --format '{{.State.Running}}' "$pg" 2>/dev/null || true)" == true ]] || break
    sleep 1
  done
  [[ "$ready" == 1 ]] || { echo 'local-stack: Go lane PostgreSQL readiness failed' >&2; exit 1; }
else
  docker_cmd create --name "$rest" --network "container:$pg" -e PGRST_DB_URI=postgres://postgres:stack-e2e-postgres-password@127.0.0.1:5432/stack_e2e -e PGRST_DB_SCHEMAS=public -e PGRST_DB_ANON_ROLE=anon -e PGRST_JWT_SECRET=stack-e2e-jwt-secret-not-for-production postgrest/postgrest:v12.2.8 >/dev/null
  created=("$rest" "${created[@]}")
  docker_cmd start "$rest" >/dev/null
fi
runner_env=(-e STACK_E2E_LOCAL_DISPOSABLE=1)
if [[ "$lane" == legacy && "${#auth_env[@]}" -gt 0 ]]; then
  runner_env+=("${auth_env[@]}")
fi
if [[ "$lane" == go ]]; then
  docker_cmd create --name "$runner" --network "container:$pg" --shm-size=1g --user 10001:10001 "${runner_env[@]}" "$id:local" >/dev/null
else
  docker_cmd create --name "$runner" --network "container:$pg" --shm-size=1g "${runner_env[@]}" "$id:local" >/dev/null
fi
created=("$runner" "${created[@]}")
set +e
docker_cmd start -a "$runner"
status=$?
set -e
if [[ "$lane" == go ]]; then
  docker_cmd cp "$runner:/tmp/go-terminal-stack-artifacts" "$scratch/go-terminal-stack-artifacts" 2>/dev/null || true
else
  docker_cmd cp "$runner:/tmp/stack-result.json" "$scratch/result.json" 2>/dev/null || true
fi
exit "$status"
