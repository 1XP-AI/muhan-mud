#!/usr/bin/env bash
set -euo pipefail

migration='supabase/migrations/20261005000000_bank_snapshot_v1_topology_shadow.sql'
contract='supabase/tests/bank_snapshot_v1_topology_shadow_contract.sql'

fail() { printf 'BankSnapshotV1 topology shadow static contract failed: %s\n' "$1" >&2; exit 1; }
require_source() { grep -Fq -- "$1" "$migration" || fail "missing migration contract: $1"; }
require_contract() { grep -Fq -- "$1" "$contract" || fail "missing SQL coverage: $1"; }

[[ -f "$migration" && -f "$contract" ]] || fail 'missing migration or executable SQL contract'
require_source 'p_nodes is null'
require_source 'jsonb_array_length(p_nodes) not between 1 and 8192'
require_source "not (n ?& array['nodeIndex','parentNodeIndex','siblingOrdinal'])"
require_source "(n - 'nodeIndex' - 'parentNodeIndex' - 'siblingOrdinal') <> '{}'::jsonb"
require_source 'depths integer[]:=array_fill(0,array[8192])'
require_source 'if depth>64 then raise exception'
require_source 'if roots<>1 then raise exception'
require_source "message='BankSnapshotV1 topology must have exactly one root'"
require_contract "record_bank_nodes(null::jsonb)"
require_contract "record_bank_nodes(''[]''::jsonb)"
require_contract '"parentNodeIndex":null,"siblingOrdinal":0'
require_contract '"nodeIndex":0,"siblingOrdinal":0'
require_contract '"nodeIndex":0,"parentNodeIndex":null} ]'
require_contract 'parentNodeIndex":null,"siblingOrdinal":1'
require_contract 'parentNodeIndex":0,"siblingOrdinal":0'
require_contract 'nodeIndex":"0"'
require_contract 'bsv_depth_nodes'
require_contract "='RECORDED'"
require_contract 'explicit null root records'
require_contract "='EXACT_RETRY'"
