#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
exec python3 "$repo_root/tests/harness/run_admission_scenario.py" --repo-root "$repo_root" "$@"
