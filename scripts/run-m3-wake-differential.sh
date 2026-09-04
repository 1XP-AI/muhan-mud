#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-m3-wake-v1.XXXXXX")"
oracle="$temp_dir/m3_wake_v1_oracle"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

cc -std=gnu89 -fcommon -Wall -Wextra -Werror -I"$repo_root/src" \
  -O1 -fno-omit-frame-pointer -fsanitize=address,undefined \
  "$repo_root/tests/harness/m3_wake_v1_oracle.c" "$repo_root/src/m3_wake_v1.c" \
  -o "$oracle"

make -C "$repo_root/src" m3-wake-v1-sanitizer-test
M3_WAKE_V1_C_ORACLE="$oracle" \
  cargo test --manifest-path "$repo_root/rust/Cargo.toml" \
  -p muhan-m3-wake-protocol
