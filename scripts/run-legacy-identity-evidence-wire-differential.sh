#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
oracle=/tmp/muhan-unit/legacy_identity_evidence_wire_oracle
mkdir -p /tmp/muhan-unit
cd "$root/src"
${CC:-cc} -std=gnu89 -fcommon -I. -ffunction-sections -fdata-sections \
  ../tests/harness/legacy_identity_evidence_wire_oracle.c \
  legacy_identity_evidence_wire.c -Wl,-dead_strip -o "$oracle"
make legacy-identity-evidence-wire-test
cd "$root/rust"
LEGACY_IDENTITY_EVIDENCE_WIRE_C_ORACLE="$oracle" \
  cargo test -p muhan-core-dto --test legacy_identity_evidence_wire_differential
