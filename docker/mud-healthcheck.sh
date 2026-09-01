#!/bin/sh
set -eu

# Checking /proc for a LISTEN socket proves readiness without creating a TCP
# connection.  Connecting would create a legacy ident-helper child each time.
case "${MUD_PORT:-4000}" in
  ''|*[!0-9]*) exit 1 ;;
esac

port_hex="$(printf '%04X' "${MUD_PORT}")"

for proc_net in /proc/net/tcp /proc/net/tcp6; do
  [ -r "${proc_net}" ] || continue
  if awk -v local_port=":${port_hex}" '
    NR > 1 && index($2, local_port) && $4 == "0A" { ready = 1 }
    END { exit ready ? 0 : 1 }
  ' "${proc_net}"; then
    exit 0
  fi
done

exit 1
