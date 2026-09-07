#!/usr/bin/env bash
# Test-only clone/replay, never installs a live player or changes a file authority.
set -Eeuo pipefail
[[ "${1:-}" == --allow-disposable && -f /.dockerenv && "$(uname -s)" == Linux ]] || exit 2
root="$(cd "$(dirname "$0")/.." && pwd -P)"
profile="${2:-canonical}"
case "$profile" in canonical) expected_nodes=0 ;; tree_inventory) expected_nodes=5 ;; *) exit 2 ;; esac
restored_database="replay_restore_$profile"
work="$(mktemp -d /tmp/muhan-restored-clone.XXXXXX)"
trap 'rm -rf -- "$work"' EXIT
export PGPASSWORD=contract-only-password PGOPTIONS='-c default_transaction_read_only=on'
query="from private.game_character_player_snapshot_v1_artifacts where character_id='a9500000-0000-0000-0000-000000000001' and command_id='c9500000-0000-0000-0000-000000000001'"
psql -X -h 127.0.0.1 -U postgres -d "$restored_database" -v ON_ERROR_STOP=1 -At \
  -c "select encode(payload,'hex') $query" > "$work/payload.hex"
digest="$(psql -X -h 127.0.0.1 -U postgres -d "$restored_database" -v ON_ERROR_STOP=1 -At -c "select snapshot_sha256 $query")"
[[ "$digest" =~ ^[0-9a-f]{64}$ ]] || exit 1
cc -std=gnu89 -fcommon -ffunction-sections -fdata-sections -I"$root/src" \
  "$root/tests/harness/legacy_player_snapshot_v1_oracle.c" \
  "$root/src/files1.c" "$root/src/player_record_serializer.c" \
  "$root/src/player_snapshot_v1.c" "$root/src/object_graph_v1.c" "$root/src/cdto_v1.c" \
  -Wl,--gc-sections -o "$work/oracle"
"$work/oracle" project-portable "$work/payload.hex" > "$work/c-roundtrip.txt"
node -e 'const fs=require("node:fs"); const [hexFile,cFile,output]=process.argv.slice(1); const h=fs.readFileSync(hexFile,"utf8").trim(); if(!/^(?:[0-9a-f]{2})+$/.test(h)||fs.readFileSync(cFile,"utf8").trim()!=="accept "+h) process.exit(1); fs.writeFileSync(output,Buffer.from(h,"hex"))' "$work/payload.hex" "$work/c-roundtrip.txt" "$work/payload.bin"
CARGO_TARGET_DIR="$work/target" cargo build --locked --offline --release --manifest-path "$root/rust/Cargo.toml" \
  -p muhan-core-dto --bin player_snapshot_v1_replay_verify
"$work/target/release/player_snapshot_v1_replay_verify" --snapshot-sha256 "$digest" < "$work/payload.bin" > "$work/rust-report.txt"
grep -qx "inventory_node_count=$expected_nodes" "$work/rust-report.txt"
echo "GREEN restored $profile passed C clone roundtrip and digest-bound Rust replay ($expected_nodes inventory nodes)"
