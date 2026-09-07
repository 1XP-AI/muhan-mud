#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
oracle="${MUHAN_UNIT_DIR:-/tmp/muhan-unit}/alias_title_snapshot_manifest_v1_oracle"
mkdir -p "${MUHAN_UNIT_DIR:-/tmp/muhan-unit}"
cd "$root/src"
${CC:-cc} -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections \
  ../tests/harness/alias_title_snapshot_manifest_v1_oracle.c \
  alias_title_snapshot_manifest_v1.c alias_title_snapshot_v1.c cdto_v1.c \
  -Wl,-dead_strip -o "$oracle"
make alias-title-snapshot-manifest-v1-test
cd "$root/rust"
ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_C_ORACLE="$oracle" \
  cargo test -p muhan-core-dto --test alias_title_snapshot_manifest_v1_differential
