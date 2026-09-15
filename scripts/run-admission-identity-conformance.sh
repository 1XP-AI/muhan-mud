#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/muhan-admission-identity.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
warn_flags=
if [ "$(uname -s)" = Darwin ]; then warn_flags=-Wno-deprecated-non-prototype; fi

cc -std=gnu89 -fcommon -DFILE_PLAYER_STORE_TESTING $warn_flags -I"$root/src" \
  "$root/tests/harness/admission_identity_conformance_oracle.c" \
  "$root/src/trusted_admission.c" "$root/src/player_path.c" \
  "$root/src/resource_path.c" \
  "$root/src/onboarding_evidence_emission.c" \
  "$root/src/onboarding_evidence_control.c" \
  "$root/src/legacy_identity_evidence.c" \
  "$root/src/legacy_identity_evidence_wire.c" \
  "$root/src/file_player_store.c" -o "$tmp/oracle"

ADMISSION_IDENTITY_CONFORMANCE_C_ORACLE="$tmp/oracle" \
  pnpm --dir "$root" --filter @muhan/gateway exec tsx --test \
  test/admission-identity-conformance.test.ts
