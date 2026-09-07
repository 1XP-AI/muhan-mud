#!/usr/bin/env bash
# Reuse an existing test image; no builds, published ports, or Docker socket mounts.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable ]] || exit 2
runner_image="${REPLAY_RUNNER_IMAGE:-}"
[[ "$runner_image" =~ ^muhan-local-stack-[0-9]+-[0-9]+:local$ ]] || exit 2
root="$(cd "$(dirname "$0")/.." && pwd -P)"
docker image inspect "$runner_image" postgres:17-alpine >/dev/null
scratch="$(mktemp -d "${TMPDIR:-/tmp}/muhan-linux-replay.XXXXXX")"
git -C "$root" archive HEAD | tar -x -C "$scratch"
printf 'Frozen source: %s\nEvidence source: %s\n' "$(git -C "$root" rev-parse HEAD)" "$scratch"
created=()
cleanup() {
  local status=$?
  trap - EXIT
  for id in "${created[@]}"; do docker rm -f "$id" >/dev/null || true; done
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
pg="$(docker create --network none --tmpfs /var/lib/postgresql/data:rw,size=192m \
  -e POSTGRES_PASSWORD=contract-only-password postgres:17-alpine)"
created+=("$pg")
docker start "$pg" >/dev/null
runner="$(docker create --read-only --user 0:0 --network "container:$pg" \
  --tmpfs /work:rw,exec,size=1536m --tmpfs /tmp:rw,exec,size=256m \
  --mount "type=bind,src=$scratch,dst=/workspace,readonly" \
  --entrypoint bash "$runner_image" -c '
    set -euo pipefail
    cp -a /workspace/. /work/
    # Refuse stale dependency images rather than claiming current-source coverage.
    cmp /repo/pnpm-lock.yaml /work/pnpm-lock.yaml
    # Use image-installed dependencies, but compile the frozen current sources.
    ln -s /repo/node_modules /work/node_modules
    ln -s /repo/services/m4-file-snapshot-manifest-relay/node_modules /work/services/m4-file-snapshot-manifest-relay/node_modules
    cd /work
    /repo/services/m4-file-snapshot-manifest-relay/node_modules/.bin/tsc -p services/m4-file-snapshot-manifest-relay/tsconfig.json
    CARGO_NET_OFFLINE=true bash scripts/run-player-snapshot-v1-normalized-projection-bridge.sh
    CARGO_NET_OFFLINE=true bash scripts/run-player-snapshot-v1-artifact-conformance.sh
    CARGO_TARGET_DIR=/work/rust/target cargo build --locked --offline --release --manifest-path rust/Cargo.toml -p muhan-core-dto --bin player_snapshot_v1_replay_verify
    PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE=1 PLAYER_SNAPSHOT_V1_REPLAY_READER_CONTAINERLESS=1 bash supabase/tests/player_snapshot_v1_replay_reader_pg17_integration.sh
  ')"
created=("$runner" "${created[@]}")
docker start -ai "$runner"
