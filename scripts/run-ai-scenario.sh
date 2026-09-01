#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

if [[ -n "${FRP_BIN:-}" && "${AI_SCENARIO_ALLOW_EXTERNAL_BINARY:-0}" != "1" ]]; then
  echo "refusing external FRP_BIN; set AI_SCENARIO_ALLOW_EXTERNAL_BINARY=1 for an explicit opt-in" >&2
  exit 2
fi

if [[ "${AI_SCENARIO_DOCKER:-0}" == "1" ]]; then
  if [[ -n "${FRP_BIN:-}" ]]; then
    echo "FRP_BIN external override is supported only for local runs" >&2
    exit 2
  fi
  image="${AI_SCENARIO_IMAGE:-gcc:14}"
  exec docker run --rm --platform linux/amd64 \
    -e KEEP_TEST_FIXTURE \
    -v "$repo_root:/work" \
    -w /work \
    "$image" bash -lc \
    'python3 tests/harness/run_scenario.py --repo-root /work --allow-legacy-root-link "$@"' \
    bash "$@"
fi

exec python3 "$repo_root/tests/harness/run_scenario.py" \
  --repo-root "$repo_root" \
  "$@"
