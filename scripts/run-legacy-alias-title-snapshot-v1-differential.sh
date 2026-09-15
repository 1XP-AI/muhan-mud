#!/usr/bin/env bash
set -euo pipefail

# Default-off characterization only: legacy sidecar fixtures are parsed by C,
# then Rust validates the exact portable CDTO representation and digest.
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-alias-title-snapshot.XXXXXX")"
oracle="$work_dir/legacy_alias_title_snapshot_v1_oracle"
passed=0
flags=(-std=gnu89 -fcommon -I"$repo_root/src" -ffunction-sections -fdata-sections)
link_flags=()

cleanup() {
  if [[ "$passed" == 1 ]]; then rm -rf "$work_dir"; else
    printf 'AliasTitleSnapshotV1 failure artifacts retained at %s\n' "$work_dir" >&2
  fi
}
trap cleanup EXIT

if [[ "$(uname -s)" == Darwin ]]; then
  flags+=(-Wno-deprecated-non-prototype)
  link_flags+=(-Wl,-dead_strip)
else
  link_flags+=(-Wl,--gc-sections)
fi

"${CC:-cc}" "${flags[@]}" \
  "$repo_root/tests/harness/legacy_alias_title_snapshot_v1_oracle.c" \
  "$repo_root/src/alias_title_snapshot_v1.c" "$repo_root/src/cdto_v1.c" \
  "${link_flags[@]}" -o "$oracle"

LEGACY_ALIAS_TITLE_SNAPSHOT_V1_C_ORACLE="$oracle" \
  cargo test --locked --manifest-path "$repo_root/rust/Cargo.toml" -p muhan-core-dto \
    --test legacy_alias_title_snapshot_v1_differential
passed=1
