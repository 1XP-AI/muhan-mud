#!/usr/bin/env bash
set -euo pipefail

# Offline conformance only: C artifact-store fixture production, the detached
# Rust verifier, and the Node M4 parser/relay boundary.  It never starts the
# MUD, a database, network service, container, or feature-enabled runtime.
repo_root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-player-snapshot-artifact-conformance.XXXXXX")"
producer="$work_dir/character_player_snapshot_v1_artifact_fixture_producer"
rust_target_dir="$work_dir/rust-target"
flags=(-std=gnu89 -fcommon -I"$repo_root/src")

cleanup() {
  rm -rf -- "$work_dir"
}
trap cleanup EXIT

if [[ "$(uname -s)" == Darwin ]]; then
  flags+=(-Wno-deprecated-non-prototype)
fi

# Exercise the C artifact-store unit boundary before using its test-only
# fixture producer.  This does not invoke the live PlayerStore runtime.
make -C "$repo_root/src" character-player-snapshot-v1-artifact-test

"${CC:-cc}" "${flags[@]}" \
  "$repo_root/tests/harness/character_player_snapshot_v1_artifact_fixture_producer.c" \
  "$repo_root/src/character_player_snapshot_v1_artifact.c" \
  "$repo_root/src/player_snapshot_v1.c" "$repo_root/src/object_graph_v1.c" \
  "$repo_root/src/cdto_v1.c" -o "$producer"

PLAYER_SNAPSHOT_V1_ARTIFACT_C_PRODUCER="$producer" \
  cargo test --locked --manifest-path "$repo_root/rust/Cargo.toml" --target-dir "$rust_target_dir" \
  -p muhan-core-dto --features player-snapshot-v1-artifact-shadow-observer \
  --test player_snapshot_v1_artifact_shadow_verifier
cargo build --locked --manifest-path "$repo_root/rust/Cargo.toml" --target-dir "$rust_target_dir" \
  -p muhan-core-dto --features player-snapshot-v1-artifact-shadow-observer \
  --bin player_snapshot_v1_artifact_shadow_verify

(
  cd "$repo_root/services/m4-file-snapshot-manifest-relay"
  npm exec -- tsc -p tsconfig.json --noEmit
  PLAYER_SNAPSHOT_V1_ARTIFACT_C_PRODUCER="$producer" \
    PLAYER_SNAPSHOT_V1_ARTIFACT_RUST_VERIFIER="$rust_target_dir/debug/player_snapshot_v1_artifact_shadow_verify" \
    npm exec -- tsx --test test/player-snapshot-v1-artifact-conformance.test.ts
)
