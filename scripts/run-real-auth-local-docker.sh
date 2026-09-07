#!/usr/bin/env bash
# Actual GoTrue on a private loopback-only namespace; never uses production data.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable ]] || { echo 'explicit --allow-disposable required' >&2; exit 2; }
root="$(cd "$(dirname "$0")/.." && pwd -P)"
runner_image="${AUTH_SMOKE_RUNNER_IMAGE:-}"
[[ "$runner_image" =~ ^muhan-local-stack-[0-9]+-[0-9]+:local$ ]] || { echo 'an existing local stack runner image is required' >&2; exit 2; }
docker image inspect "$runner_image" >/dev/null
docker image inspect postgres:17-alpine >/dev/null
docker image inspect supabase/gotrue:v2.189.0 >/dev/null
run_id="muhan-real-auth-$(date +%s)-$$"
created=()
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  if [[ "${#created[@]}" -gt 0 ]]; then
    for id in "${created[@]}"; do docker rm -f "$id" >/dev/null; done
  fi
  echo "real-auth-local: owned containers removed; status=$status"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
pg="$(docker create --name "$run_id-pg" --network none --tmpfs /var/lib/postgresql/data:rw,size=256m -e POSTGRES_PASSWORD=local-auth-db-password postgres:17-alpine)"
created+=("$pg")
docker start "$pg" >/dev/null
ready=0
for attempt in $(seq 1 30); do
  if docker exec "$pg" pg_isready -U postgres >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[[ "$ready" == 1 ]] || { echo 'real-auth-local: database readiness failed' >&2; exit 1; }
docker exec -i "$pg" psql -X -U postgres -v ON_ERROR_STOP=1 < "$root/tests/stack-e2e/real-auth-bootstrap.sql" >/dev/null
auth="$(docker create --name "$run_id-auth" --network "container:$pg" \
  -e GOTRUE_API_HOST=0.0.0.0 -e GOTRUE_API_PORT=9999 \
  -e API_EXTERNAL_URL=http://127.0.0.1:9999 -e GOTRUE_SITE_URL=http://127.0.0.1:9999 \
  -e GOTRUE_DB_DRIVER=postgres -e GOTRUE_DB_DATABASE_URL=postgres://supabase_auth_admin:local-auth-db-password@127.0.0.1:5432/postgres \
  -e GOTRUE_JWT_SECRET=local-auth-jwt-secret-disposable-only-32bytes \
  -e GOTRUE_JWT_AUD=authenticated -e GOTRUE_JWT_DEFAULT_GROUP_NAME=authenticated \
  -e GOTRUE_EXTERNAL_EMAIL_ENABLED=true -e GOTRUE_MAILER_AUTOCONFIRM=true \
  -e GOTRUE_DISABLE_SIGNUP=false -e GOTRUE_EXTERNAL_PHONE_ENABLED=false \
  supabase/gotrue:v2.189.0)"
created=("$auth" "${created[@]}")
docker start "$auth" >/dev/null
ready=0
for attempt in $(seq 1 45); do
  if docker exec "$pg" wget -q -O /dev/null http://127.0.0.1:9999/health >/dev/null 2>&1; then ready=1; break; fi
  [[ "$(docker inspect --format '{{.State.Running}}' "$auth")" == true ]] || break
  sleep 1
done
[[ "$ready" == 1 ]] || { echo 'real-auth-local: auth migration/readiness failed' >&2; exit 1; }
probe="$(docker create -i --name "$run_id-probe" --network "container:$pg" --entrypoint node \
  -e AUTH_SMOKE_URL=http://127.0.0.1:9999 \
  -e AUTH_SMOKE_JWT_SECRET=local-auth-jwt-secret-disposable-only-32bytes \
  "$runner_image" --input-type=module)"
created=("$probe" "${created[@]}")
docker start -ai "$probe" < "$root/tests/stack-e2e/real-auth-smoke.mjs"
