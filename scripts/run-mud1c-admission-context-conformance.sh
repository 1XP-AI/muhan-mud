#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/muhan-mud1c-admission-context.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
warn_flags=
if [ "$(uname -s)" = Darwin ]; then warn_flags=-Wno-deprecated-non-prototype; fi

# This test-only binary links only the detached codec with an explicit opt-in.
cc -std=gnu89 -fcommon -Wall -Wextra -Werror $warn_flags \
  -DMUD1C_ADMISSION_CONTEXT_ENABLED=1 -I"$root/src" \
  "$root/tests/harness/mud1c_admission_context_conformance_oracle.c" \
  "$root/src/mud1c_admission_context.c" -o "$tmp/oracle"

MUD1C_ADMISSION_CONTEXT_C_ORACLE="$tmp/oracle" \
  pnpm --dir "$root" --filter @muhan/gateway exec tsx --test \
  test/mud1c-admission-context-conformance.test.ts
