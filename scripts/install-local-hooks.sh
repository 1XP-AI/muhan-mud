#!/usr/bin/env bash
set -Eeuo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
existing="$(git -C "$root" config --get core.hooksPath || true)"
if [[ -n "$existing" && "$existing" != .githooks ]]; then
  echo 'An existing hooksPath is configured; integrate it explicitly instead of overwriting it.' >&2
  exit 1
fi
if [[ -z "$existing" && -f "$(git -C "$root" rev-parse --git-path hooks/pre-push)" ]]; then
  echo 'An existing pre-push hook exists; preserve and integrate it first.' >&2
  exit 1
fi
chmod +x "$root/.githooks/pre-push"
git -C "$root" config --local core.hooksPath .githooks
echo 'Installed repository-local pre-push checks. No global Git settings changed.'
