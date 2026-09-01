#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"

if [[ "${AI_SCENARIO_DOCKER:-0}" == "1" ]]; then
  image="${AI_SCENARIO_IMAGE:-gcc:14}"
  exec docker run --rm --platform linux/amd64 \
    -e KEEP_TEST_FIXTURE \
    -v "$repo_root:/work" \
    -w /work \
    "$image" bash -lc \
    'python3 tests/harness/run_shutdown_scenario.py --repo-root /work --allow-legacy-root-link "$@"' \
    bash "$@"
fi

exec python3 "$repo_root/tests/harness/run_shutdown_scenario.py" \
  --repo-root "$repo_root" \
  "$@"
