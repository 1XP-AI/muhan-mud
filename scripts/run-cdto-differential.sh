#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
seed="${CDTO_V1_DIFF_SEED:-4d55484344544f31}"
if [[ ! "$seed" =~ ^[0-9A-Fa-f]{16}$ ]]; then
  printf 'CDTO_V1_DIFF_SEED must be exactly 16 hexadecimal digits\n' >&2
  exit 2
fi
seed="$(printf '%s' "$seed" | tr 'A-F' 'a-f')"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/muhan-cdto-diff.XXXXXX")"
artifact_dir="${CDTO_V1_DIFF_ARTIFACT_DIR:-$work_dir/artifacts}"
oracle="$work_dir/cdto_v1_oracle"
passed=0

cleanup() {
  if [[ "$passed" == 1 ]]; then
    rm -rf "$work_dir"
  else
    printf 'CDTO differential failure artifacts retained at %s\n' "$artifact_dir" >&2
  fi
}
trap cleanup EXIT

mkdir -p "$artifact_dir"
"${CC:-gcc}" -std=gnu89 -fcommon -I"$repo_root/src" -O1 -fno-omit-frame-pointer \
  -fsanitize=address,undefined \
  "$repo_root/tests/harness/cdto_v1_oracle.c" "$repo_root/src/cdto_v1.c" "$repo_root/src/object_v1.c" "$repo_root/src/object_graph_v1.c" "$repo_root/src/creature_v1.c" \
  -o "$oracle"

CDTO_V1_C_ORACLE="$oracle" \
CDTO_V1_DIFF_ARTIFACT_DIR="$artifact_dir" \
CDTO_V1_DIFF_SEED="$seed" \
cargo test --manifest-path "$repo_root/rust/Cargo.toml" -p muhan-core-dto --test differential

make -C "$repo_root/src" cdto-v1-sanitizer-test CC="${CC:-gcc}"
make -C "$repo_root/src" object-v1-sanitizer-test CC="${CC:-gcc}"
make -C "$repo_root/src" object-graph-v1-sanitizer-test CC="${CC:-gcc}"
make -C "$repo_root/src" creature-v1-sanitizer-test CC="${CC:-gcc}"
passed=1
