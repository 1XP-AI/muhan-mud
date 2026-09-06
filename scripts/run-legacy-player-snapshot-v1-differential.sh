#!/usr/bin/env bash
set -euo pipefail

# Test-only S1 differential: ABI-bound legacy bytes are decoded by C, then
# compared through named canonical, portable PlayerSnapshotV1 CDTO fixtures.
# Fixtures never contain credentials, native pointers, or runtime descriptors.
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-s1-player-snapshot.XXXXXX")"
oracle="$work_dir/legacy_player_snapshot_v1_oracle"
passed=0
flags=(-std=gnu89 -fcommon -I"$repo_root/src" -ffunction-sections -fdata-sections)
link_flags=()
expected_abi='legacy-player-snapshot-v1/raw-v1;endian=little;char=8;short=16;int=32;long=64;ptr=64;creature=1952;object=376;creature.level=318;creature.type=319;creature.hpmax=332;creature.hpcur=334;creature.mpmax=336;creature.mpcur=338;creature.gold=352;creature.first_obj=1920;object.value=304;object.shotsmax=316;object.shotscur=318;object.first_obj=344'

cleanup() {
  if [[ "$passed" == 1 ]]; then rm -rf "$work_dir"; else
    printf 'S1 legacy player snapshot failure artifacts retained at %s\n' "$work_dir" >&2
  fi
}
trap cleanup EXIT

if [[ "$(uname -s)" == Darwin ]]; then
  flags+=(-Wno-error=implicit-function-declaration -Wno-error=return-mismatch -Wno-deprecated-non-prototype)
  link_flags+=(-Wl,-dead_strip)
else
  link_flags+=(-Wl,--gc-sections)
fi
if [[ "${LEGACY_PLAYER_SNAPSHOT_V1_SANITIZE:-0}" == 1 ]]; then
  flags+=(-O1 -fno-omit-frame-pointer -fsanitize=address,undefined)
  link_flags+=(-fsanitize=address,undefined)
fi

"${CC:-cc}" "${flags[@]}" \
  "$repo_root/tests/harness/legacy_player_snapshot_v1_oracle.c" \
  "$repo_root/src/files1.c" "$repo_root/src/player_record_serializer.c" \
  "$repo_root/src/player_snapshot_v1.c" "$repo_root/src/object_graph_v1.c" \
  "$repo_root/src/cdto_v1.c" "${link_flags[@]}" -o "$oracle"

# CDTO fixtures stay independently portable: their Rust schema validation
# happens before this test-only raw native-layout admission gate.
cargo test --manifest-path "$repo_root/rust/Cargo.toml" -p muhan-core-dto \
  --test legacy_player_snapshot_v1_differential

actual_abi="$("$oracle" abi-fingerprint)"
if [[ "$actual_abi" != "$expected_abi" ]]; then
  printf 'S1 legacy player snapshot unsupported raw ABI: %s\n' "$actual_abi" >&2
  exit 1
fi
"$oracle" abi-check "$expected_abi"
unsupported_abi="${expected_abi%?}x"
if "$oracle" abi-check "$unsupported_abi"; then
  echo 'S1 legacy player snapshot ABI mismatch was accepted' >&2
  exit 1
fi

verify_profile() {
  local profile="$1"
  local fixture="$2"

  if [[ "${LEGACY_PLAYER_SNAPSHOT_V1_SANITIZE:-0}" == 1 ]]; then
    ASAN_OPTIONS="${DECODER_ASAN_OPTIONS:-detect_leaks=1:halt_on_error=1}" \
      UBSAN_OPTIONS=halt_on_error=1 "$oracle" verify "$profile" "$fixture"
  else
    "$oracle" verify "$profile" "$fixture"
  fi
}

verify_profile rich "$repo_root/tests/fixtures/player_snapshot_v1_legacy_decoder_canonical.hex"
verify_profile minimal "$repo_root/tests/fixtures/player_snapshot_v1_legacy_decoder_minimal.hex"
verify_profile persisted-graph "$repo_root/tests/fixtures/player_snapshot_v1_legacy_decoder_persisted_graph.hex"
passed=1
