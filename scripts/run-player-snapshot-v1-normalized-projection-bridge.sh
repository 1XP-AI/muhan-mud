#!/usr/bin/env bash
set -euo pipefail

# Test-only bridge: check the approved portable fixture with the deterministic
# native oracle, exercise Rust's real CLI contract, then let the M4 test drive
# the production Node parser and relay against in-memory side-effect capture.
repo_root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
fixture="$repo_root/tests/fixtures/player_snapshot_v1_tree_inventory.hex"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-normalized-projection-bridge.XXXXXX")"
oracle="$work_dir/legacy_player_snapshot_v1_oracle"
rust_target_dir="$work_dir/rust-target"
flags=(-std=gnu89 -fcommon -I"$repo_root/src" -ffunction-sections -fdata-sections)
link_flags=()

cleanup() {
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

if [[ "$(uname -s)" == Darwin ]]; then
  flags+=(-Wno-error=implicit-function-declaration -Wno-error=return-mismatch -Wno-deprecated-non-prototype)
  link_flags+=(-Wl,-dead_strip)
else
  link_flags+=(-Wl,--gc-sections)
fi

"${CC:-cc}" "${flags[@]}" \
  "$repo_root/tests/harness/legacy_player_snapshot_v1_oracle.c" \
  "$repo_root/src/files1.c" "$repo_root/src/player_record_serializer.c" \
  "$repo_root/src/player_snapshot_v1.c" "$repo_root/src/object_graph_v1.c" \
  "$repo_root/src/cdto_v1.c" "${link_flags[@]}" -o "$oracle"

expected_wire="$(tr -d '\r\n' < "$fixture")"
[[ "$("$oracle" project-portable "$fixture")" == "accept $expected_wire" ]] || {
  printf '%s\n' 'PlayerSnapshotV1 normalized-projection bridge: C oracle rejected or changed the checked-in tree fixture' >&2
  exit 1
}

cargo test --locked --manifest-path "$repo_root/rust/Cargo.toml" --target-dir "$rust_target_dir" -p muhan-core-dto \
  --test player_snapshot_normalized_v1_cli normalized_projection_cli_is_versioned_machine_readable_and_byte_stable -- --exact
cargo build --locked --manifest-path "$repo_root/rust/Cargo.toml" --target-dir "$rust_target_dir" -p muhan-core-dto --bin player_snapshot_v1_normalized_project

(
  cd "$repo_root/services/m4-file-snapshot-manifest-relay"
  M4_PLAYER_SNAPSHOT_V1_NORMALIZED_PROJECT_RUNNER="$rust_target_dir/debug/player_snapshot_v1_normalized_project" \
    npm exec -- tsx --test test/*.test.ts
)
