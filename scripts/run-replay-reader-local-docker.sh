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
pg="$(docker create --label muhan.replay-disposable=true --network none --tmpfs /var/lib/postgresql/data:rw,size=384m \
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
    test -x /usr/bin/flock
    cc -std=gnu89 -fcommon -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/player_authority_store_test.c src/player_authority_store.c src/player_store.c \
      src/player_path.c src/utf8_text.c src/resource_path.c -o /tmp/player-authority-store-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/player-authority-store-test
    # Compile the real login consumer after transient admission struct changes.
    cc -std=gnu89 -fcommon -Isrc -c src/command1.c -o /tmp/session-command1.o
    cc -std=gnu89 -fcommon -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/trusted_admission_test.c src/trusted_admission.c src/player_path.c src/utf8_text.c src/resource_path.c -o /tmp/trusted-admission-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/trusted-admission-test
    /repo/services/m4-file-snapshot-manifest-relay/node_modules/.bin/tsc -p services/m4-file-snapshot-manifest-relay/tsconfig.json
    node services/m4-file-snapshot-manifest-relay/test/money-character-fence.mjs
    node services/m4-file-snapshot-manifest-relay/test/player-pending-request.mjs
    node services/m4-file-snapshot-manifest-relay/test/player-money-fence.mjs
    node services/m4-file-snapshot-manifest-relay/test/player-pending-release.mjs
    CARGO_NET_OFFLINE=true bash scripts/run-player-snapshot-v1-normalized-projection-bridge.sh
    CARGO_NET_OFFLINE=true bash scripts/run-player-snapshot-v1-artifact-conformance.sh
    make -C src bank-snapshot-v1-test bank-snapshot-v1-artifact-test
    make -C src bank-transfer-snapshot-v1-test bank-transfer-snapshot-v1-sanitizer-test
    cargo test --locked --offline --manifest-path rust/Cargo.toml -p muhan-core-dto --lib --bin bank_money_transfer_plan
    cc -std=gnu89 -Wall -Wextra -Werror -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/bank_money_coordinate_native_test.c src/bank_money_coordinate_native.c -o /tmp/bank-money-coordinate-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/bank-money-coordinate-test
    cc -std=gnu89 -Wall -Wextra -Werror -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/bank_money_live_native_test.c src/bank_money_live_native.c -o /tmp/bank-money-live-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/bank-money-live-test
    cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/bank_money_live_snapshot_test.c src/bank_money_live_snapshot.c src/files1.c src/player_record_serializer.c \
      src/player_snapshot_v1.c src/object_graph_v1.c src/cdto_v1.c -Wl,--gc-sections -o /tmp/bank-money-live-snapshot-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/bank-money-live-snapshot-test tests/fixtures/player_snapshot_v1_tree_inventory.hex
    cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/bank_money_result_native_test.c src/bank_money_result_native.c src/bank_money_live_snapshot.c \
      src/files1.c src/player_record_serializer.c src/player_snapshot_v1.c src/object_graph_v1.c src/cdto_v1.c \
      src/bank_snapshot_v1.c src/bank_transfer_snapshot_v1.c -Wl,--gc-sections -o /tmp/bank-money-result-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/bank-money-result-test
    cc -std=gnu89 -Wall -Wextra -Werror -Isrc -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/bank_money_command_native_test.c src/bank_money_command_native.c -o /tmp/bank-money-command-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/bank-money-command-test
    cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -Isrc \
      -fsanitize=address,undefined -fno-omit-frame-pointer \
      -Dfopen=child_reaper_test_fopen -Dunlink=child_reaper_test_unlink \
      tests/unit/child_reaper_test.c src/io.c -Wl,--gc-sections -o /tmp/child-reaper-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/child-reaper-test
    # Characterization only: explicitly exposes the legacy cross-file failure gap.
    cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -Isrc \
      tests/unit/bank_transfer_legacy_characterization.c src/bank.c \
      -Wl,--gc-sections -o /tmp/bank-transfer-characterization
    /tmp/bank-transfer-characterization
    cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -Isrc \
      -DMUHAN_BANK_MONEY_ROUTING -fsanitize=address,undefined -fno-omit-frame-pointer \
      tests/unit/bank_transfer_legacy_characterization.c src/bank.c src/bank_money_route.c \
      -Wl,--gc-sections -o /tmp/bank-money-route-test
    ASAN_OPTIONS=detect_leaks=1:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 /tmp/bank-money-route-test
    MUHAN_BANK_COMMAND_ORACLE=/tmp/bank-transfer-characterization CARGO_TARGET_DIR=/work/rust/target \
      cargo test --locked --offline --manifest-path rust/Cargo.toml -p muhan-core-dto \
      --test bank_transfer_v1 -- --ignored
    CARGO_TARGET_DIR=/work/rust/target cargo build --locked --offline --release --manifest-path rust/Cargo.toml -p muhan-core-dto --bin player_snapshot_v1_replay_verify
    PLAYER_SNAPSHOT_V1_REPLAY_READER_ALLOW_DISPOSABLE=1 PLAYER_SNAPSHOT_V1_REPLAY_READER_CONTAINERLESS=1 bash supabase/tests/player_snapshot_v1_replay_reader_pg17_integration.sh
    make -C src onboarding-activation-command-lifecycle-test onboarding-activation-command-lifecycle-sanitizer-test
    # This scenario injects EACCES by changing its own fixture permissions.
    # Root would bypass that failure, so build/run a private copy as uid 1000.
    mkdir /tmp/onboarding-work
    cp -r /workspace/. /tmp/onboarding-work/
    chown -hR 1000:1000 /tmp/onboarding-work
    setpriv --reuid=1000 --regid=1000 --clear-groups --no-new-privs python3 /tmp/onboarding-work/tests/harness/run_onboarding_scenario.py --repo-root /tmp/onboarding-work --output /tmp/onboarding-close.json
  ')"
created=("$runner" "${created[@]}")
docker start -ai "$runner"
for profile in canonical tree_inventory; do
bash "$root/scripts/verify-replay-db-backup-local.sh" --allow-disposable "$pg" "$profile"
restored="$(docker create --read-only --user 0:0 --network "container:$pg" \
  --tmpfs /tmp:rw,exec,size=256m \
  --mount "type=bind,src=$scratch,dst=/workspace,readonly" \
  --entrypoint bash "$runner_image" /workspace/scripts/verify-restored-snapshot-linux.sh --allow-disposable "$profile")"
created=("$restored" "${created[@]}")
docker start -ai "$restored"
done
