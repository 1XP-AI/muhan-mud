#!/bin/sh
set -eu

case "${MUD_PORT:-4000}" in
  ''|*[!0-9]*)
    echo "MUD_PORT must be a decimal TCP port" >&2
    exit 64
    ;;
esac

if [ "${MUD_PORT}" -lt 1 ] 2>/dev/null || [ "${MUD_PORT}" -gt 65535 ] 2>/dev/null; then
  echo "MUD_PORT must be between 1 and 65535" >&2
  exit 64
fi

if [ ! -x /home/muhan/bin/frp ]; then
  echo "missing seeded game runtime at /home/muhan/bin/frp" >&2
  exit 78
fi

# -r enables SO_REUSEADDR.  The Makefile's DEBUG flag keeps this exec'd MUD
# process in the foreground instead of taking the legacy fork/daemon path.
exec /home/muhan/bin/frp -r "${MUD_PORT}"
