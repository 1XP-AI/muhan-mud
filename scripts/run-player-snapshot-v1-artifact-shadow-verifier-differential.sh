#!/usr/bin/env bash
set -euo pipefail

# The Rust observer is default-unwired.  This test-only script builds a small
# C producer that uses the real immutable artifact store, then gives its exact
# artifact bytes to the Rust verifier integration test.
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-player-snapshot-artifact-shadow.XXXXXX")"
producer="$work_dir/character_player_snapshot_v1_artifact_fixture_producer"
passed=0
flags=(-std=gnu89 -fcommon -I"$repo_root/src")

cleanup() {
  if [[ "$passed" == 1 ]]; then rm -rf "$work_dir"; else
    printf 'PlayerSnapshotV1 artifact shadow verifier failure artifacts retained at %s\n' "$work_dir" >&2
  fi
}
trap cleanup EXIT

if [[ "$(uname -s)" == Darwin ]]; then
  flags+=(-Wno-deprecated-non-prototype)
fi

"${CC:-cc}" "${flags[@]}" \
  "$repo_root/tests/harness/character_player_snapshot_v1_artifact_fixture_producer.c" \
  "$repo_root/src/character_player_snapshot_v1_artifact.c" \
  "$repo_root/src/player_snapshot_v1.c" "$repo_root/src/object_graph_v1.c" \
  "$repo_root/src/cdto_v1.c" -o "$producer"

PLAYER_SNAPSHOT_V1_ARTIFACT_C_PRODUCER="$producer" \
  cargo test --manifest-path "$repo_root/rust/Cargo.toml" -p muhan-core-dto \
  --features player-snapshot-v1-artifact-shadow-observer \
  --test player_snapshot_v1_artifact_shadow_verifier

passed=1
