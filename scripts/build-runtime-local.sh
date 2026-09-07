#!/usr/bin/env bash
# Freeze reviewed Git content and override the private-fetch stage locally.
set -Eeuo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
revision="${1:-}"
recipe="${2:-}"
mode="${3:-}"
[[ "$revision" =~ ^[0-9a-f]{40}$ && -f "$recipe" ]] || {
  echo 'usage: build-runtime-local.sh <full commit SHA> <deployment Dockerfile> [--dry-run]' >&2
  exit 2
}
[[ -z "$mode" || "$mode" == --dry-run ]] || exit 2
[[ "$(git -C "$root" rev-parse --verify "$revision^{commit}")" == "$revision" ]] || exit 2
# Require an explicit Unix socket, not a selected cloud/remote Docker context.
endpoint="${DOCKER_HOST:-}"
[[ "$endpoint" == unix:///* ]] || { echo 'explicit local Unix DOCKER_HOST required' >&2; exit 2; }
tag="muhan-runtime-local-${revision:0:12}-$(date +%s)-$$:local"
if [[ "$mode" == --dry-run ]]; then
  printf 'source=%s\nrecipe=%s\nendpoint=%s\nbuilder=default\nplatform=linux/amd64\ntarget=runtime\noutput=load\ntag=%s\n' "$revision" "$recipe" "$endpoint" "$tag"
  exit 0
fi
scratch="$(mktemp -d "${TMPDIR:-/tmp}/muhan-runtime-build.XXXXXX")"
# Retain this task-owned evidence and source on failure; never prune shared caches.
printf 'Build evidence: %s\n' "$scratch"
export BUILDX_CONFIG="$scratch/buildx"
mkdir -p "$BUILDX_CONFIG"
docker --host "$endpoint" buildx inspect default > "$scratch/builder.txt"
driver="$(awk '$1 == "Driver:" {print $2}' "$scratch/builder.txt")"
[[ "$driver" == docker ]] || { echo 'default builder must use the local docker driver' >&2; exit 2; }
mkdir -p "$scratch/source/src" "$scratch/context"
git -C "$root" archive "$revision" | tar -x -C "$scratch/source/src"
cp "$recipe" "$scratch/Dockerfile"
printf '%s\n' "$revision" > "$scratch/source-revision.txt"
shasum -a 256 "$scratch/Dockerfile" > "$scratch/dockerfile-sha256.txt"
docker --host "$endpoint" buildx build --builder default \
  --platform linux/amd64 --target runtime --load \
  --build-context "source=$scratch/source" \
  --build-arg "SOURCE_REVISION=$revision" \
  --file "$scratch/Dockerfile" --tag "$tag" "$scratch/context" \
  2>&1 | tee "$scratch/build.log"
docker --host "$endpoint" image inspect "$tag" > "$scratch/image.json"
actual="$(docker --host "$endpoint" image inspect --format '{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.revision"}}' "$tag")"
[[ "$actual" == "amd64 $revision" ]] || { echo 'runtime image identity mismatch' >&2; exit 1; }
printf 'Built local runtime: %s\nEvidence: %s\n' "$tag" "$scratch"
