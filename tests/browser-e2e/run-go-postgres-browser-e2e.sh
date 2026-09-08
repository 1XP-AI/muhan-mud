#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"
: "${MUHAN_BROWSER_DATABASE_URL:?MUHAN_BROWSER_DATABASE_URL is required}"
: "${MUHAN_BROWSER_PORT:?MUHAN_BROWSER_PORT is required}"
: "${MUHAN_GO_PORT:?MUHAN_GO_PORT is required}"

world_id="${MUHAN_BROWSER_WORLD_ID:-browser-e2e}"
character_name="${MUHAN_BROWSER_CHARACTER_NAME:-BrowserAlice}"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/muhan-browser-e2e.XXXXXX")"
templates="$scratch/templates"
mkdir -p "$templates"
go_pid=""
web_pid=""

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  if [[ -n "$web_pid" ]] && kill -0 "$web_pid" 2>/dev/null; then
    kill "$web_pid" 2>/dev/null || true
    wait "$web_pid" 2>/dev/null || true
  fi
  if [[ -n "$go_pid" ]] && kill -0 "$go_pid" 2>/dev/null; then
    kill -TERM "$go_pid" 2>/dev/null || true
    wait "$go_pid" 2>/dev/null || true
  fi
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT INT TERM

(cd "$repo_root/server" && go build -race -o "$scratch/muhan" ./cmd/muhan)
(cd "$repo_root/server" && go build -race -o "$scratch/seed" ./cmd/muhan-browser-e2e)

"$scratch/seed" \
  -database "$MUHAN_BROWSER_DATABASE_URL" \
  -world "$world_id" \
  -name "$character_name"

DATABASE_URL="$MUHAN_BROWSER_DATABASE_URL" \
ALLOWED_ORIGINS="http://127.0.0.1:${MUHAN_BROWSER_PORT}" \
LISTEN_ADDR="127.0.0.1:${MUHAN_GO_PORT}" \
"$scratch/muhan" \
  -world "$world_id" \
  -templates "$templates" \
  -game-hour 12 \
  -player-tick 1h \
  >"$scratch/go-server.log" 2>&1 &
go_pid=$!

for _ in {1..120}; do
  if curl --silent --fail --max-time 1 "http://127.0.0.1:${MUHAN_GO_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$go_pid" 2>/dev/null; then
    sed -n '1,200p' "$scratch/go-server.log" >&2 || true
    exit 1
  fi
  sleep 0.25
done
if ! curl --silent --fail --max-time 1 "http://127.0.0.1:${MUHAN_GO_PORT}/healthz" >/dev/null 2>&1; then
  sed -n '1,200p' "$scratch/go-server.log" >&2 || true
  exit 1
fi

MUD_GO_GATEWAY_URL="ws://127.0.0.1:${MUHAN_GO_PORT}/ws" \
pnpm --filter @muhan/web dev --hostname 127.0.0.1 --port "$MUHAN_BROWSER_PORT" &
web_pid=$!
wait "$web_pid"
