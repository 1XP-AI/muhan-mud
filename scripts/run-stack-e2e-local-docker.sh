#!/usr/bin/env bash
# Explicitly authorized local lane: no published ports, host mounts or socket.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable ]] || { echo 'usage: bash scripts/run-stack-e2e-local-docker.sh --allow-disposable' >&2; exit 2; }
root="$(cd "$(dirname "$0")/.." && pwd -P)"
scratch="$(mktemp -d /tmp/muhan-local-stack.XXXXXX)"
id="muhan-local-stack-$(date +%s)-$$"
network="$id-net"
pg="$id-pg"
rest="$id-rest"
runner="$id-runner"
created=()
network_created=0
cleanup() {
  local status=$?
  trap - EXIT INT TERM
  set +e
  for name in "${created[@]}"; do docker rm -f "$name" >/dev/null; done
  if [[ "$network_created" == 1 ]]; then docker network rm "$network" >/dev/null; fi
  echo "local-stack: source/build evidence retained at $scratch; status=$status"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
git -C "$root" archive HEAD | tar -x -C "$scratch"
# Only committed sources are tested; no working-tree files are copied.
git -C "$root" rev-parse HEAD
docker build -t "$id:local" -f "$scratch/tests/stack-e2e/Dockerfile.local" "$scratch"
docker network create --internal "$network" >/dev/null
network_created=1
docker create --name "$pg" --network "$network" --tmpfs /var/lib/postgresql/data:rw,size=512m -e POSTGRES_PASSWORD=stack-e2e-postgres-password -e POSTGRES_DB=stack_e2e postgres:17-alpine >/dev/null
created+=("$pg")
docker start "$pg" >/dev/null
docker create --name "$rest" --network "container:$pg" -e PGRST_DB_URI=postgres://postgres:stack-e2e-postgres-password@127.0.0.1:5432/stack_e2e -e PGRST_DB_SCHEMAS=public -e PGRST_DB_ANON_ROLE=anon -e PGRST_JWT_SECRET=stack-e2e-jwt-secret-not-for-production postgrest/postgrest:v12.2.8 >/dev/null
created=("$rest" "${created[@]}")
docker start "$rest" >/dev/null
docker create --name "$runner" --network "container:$pg" --shm-size=1g -e STACK_E2E_LOCAL_DISPOSABLE=1 "$id:local" >/dev/null
created=("$runner" "${created[@]}")
set +e
docker start -a "$runner"
status=$?
set -e
docker cp "$runner:/tmp/stack-result.json" "$scratch/result.json" 2>/dev/null || true
exit "$status"
