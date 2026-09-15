#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 receipt transport restart integration skipped (set M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE=1)"
  exit 0
}

database_url="${M3_RECEIPT_TRANSPORT_DATABASE_URL:-}"
harness="${1:-}"
[[ -n "$harness" && -x "$harness" ]] || { echo "restart integration harness is unavailable" >&2; exit 2; }
[[ "$database_url" != *[[:space:]]* && "$database_url" != *\?* && "$database_url" != *\#* ]] || {
  echo "restart integration requires a query-free loopback URL" >&2; exit 2
}
if [[ ! "$database_url" =~ ^postgres(ql)?://[^:/\?\#@[:space:]]+:[^/\?\#@[:space:]]*@127\.0\.0\.1:[0-9]+/[^/\?\#[:space:]]+$ ]]; then
  echo "restart integration requires a numeric-port 127.0.0.1 postgres URI" >&2
  exit 2
fi

world=m3-restart
character=92500000-0000-0000-0000-000000000001
writer_a=94500000-0000-0000-0000-000000000001
writer_b=94500000-0000-0000-0000-000000000002
command=93500000-0000-0000-0000-000000000001
post="$(printf 'a%.0s' {1..64})"
protocol_post=4997288ce1f8562892e980808345ba92f0c67779868362543ef2cc8258539b6b
offline_port=1
tmp="$(mktemp -d)"
request=''
protocol_root="$tmp/protocol-root"
waiting_pid=''
wait_timeout_seconds=15

fail() { echo "m3 receipt transport restart integration failed: $1" >&2; exit 1; }
run_super() {
  env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable PGCONNECT_TIMEOUT=1 \
    PGAPPNAME=m3-receipt-restart-fixture \
    PGOPTIONS='-c statement_timeout=1000 -c lock_timeout=500' \
    psql "$database_url" --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 "$@" 2>/dev/null
}
run_child() {
  local mode="$1" target_url="${2:-$database_url}"
  env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable PGCONNECT_TIMEOUT=5 \
    PGAPPNAME=m3-receipt-restart-child \
    PGOPTIONS='-c role=mud_writer -c statement_timeout=5000 -c lock_timeout=1000' \
    M3_RECEIPT_TRANSPORT_DATABASE_URL="$target_url" \
    M3_RPC_RESTART_REQUEST_SHA256="$request" \
    M3_RPC_RESTART_ROOT="$protocol_root" \
    "$harness" "$mode"
}
snapshot() {
  run_super --command="select jsonb_build_object('route',(select to_jsonb(r) from private.resolve_game_character_writer_route_v2('$world','M3restart') r),'identity',(select to_jsonb(c) from public.game_characters c where c.id='$character'::uuid),'head',(select to_jsonb(h) from private.game_character_legacy_heads h where h.character_id='$character'::uuid),'receipts',coalesce((select jsonb_agg(to_jsonb(r) order by r.command_id) from private.game_character_shadow_receipts r where r.character_id='$character'::uuid),'[]'::jsonb),'epoch',(select to_jsonb(e) from private.game_character_writer_epochs e where e.world_id='$world'),'fences',coalesce((select jsonb_agg(to_jsonb(f) order by f.writer_epoch) from private.game_character_writer_epoch_fences f where f.world_id='$world'),'[]'::jsonb))::text;"
}
row_count() {
  run_super --command="select count(*) from private.game_character_shadow_receipts where character_id='$character'::uuid;"
}
reset_fixture() {
  run_super --command="delete from private.game_character_shadow_receipts where character_id='$character'::uuid; delete from private.game_character_legacy_heads where character_id='$character'::uuid; delete from private.game_character_writer_epoch_fences where world_id='$world'; delete from private.game_character_writer_epochs where world_id='$world'; delete from public.game_characters where id='$character'::uuid; insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values('$character'::uuid,'$world','M3restart','M3restart',substr(encode(public.digest(convert_to('M3restart','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1); insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values('$character'::uuid,'absent',null,1,0,null);" >/dev/null
  request="$(run_super --command="select private.game_character_shadow_request_sha256('$world','$character'::uuid,'M3restart',substr(encode(public.digest(convert_to('M3restart','UTF8'),'sha1'),'hex'),1,2),'$command'::uuid,'$writer_a'::uuid,1::bigint,1::bigint,'absent',null,'$post',1::smallint);")"
  [[ "$request" =~ ^[0-9a-f]{64}$ ]] || fail "could not derive canonical request digest"
}
reset_protocol_fixture() {
  reset_fixture
  request="$(run_super --command="select private.game_character_shadow_request_sha256('$world','$character'::uuid,'M3restart',substr(encode(public.digest(convert_to('M3restart','UTF8'),'sha1'),'hex'),1,2),'$command'::uuid,'$writer_a'::uuid,1::bigint,1::bigint,'absent',null,'$protocol_post',1::smallint);")"
  [[ "$request" =~ ^[0-9a-f]{64}$ ]] || fail "could not derive protocol request digest"
}
make_protocol_root() {
  mkdir -m 700 "$protocol_root" "$protocol_root/player" "$protocol_root/player/19" \
    "$protocol_root/character-save-stage" "$protocol_root/character-save-journal"
  printf 'version=2\nkind=writer-instance\nwriter_instance_id=%s\n' "$writer_a" >"$protocol_root/character-save-journal/writer-instance.v2"
  printf 'version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=1\n' "$world" "$writer_a" >"$protocol_root/character-save-journal/writer-epoch.v2"
  chmod 600 "$protocol_root/character-save-journal/writer-instance.v2" "$protocol_root/character-save-journal/writer-epoch.v2"
}
file_topology() {
  if [[ "$(uname)" == Darwin ]]; then
    stat -f '%Lp:%l' "$1"
  else
    stat -c '%a:%h' "$1"
  fi
}
local_snapshot() {
  local leaf
  while IFS= read -r leaf; do
    printf '%s %s %s ' "${leaf#"$protocol_root"/}" "$(file_topology "$leaf")" "$(wc -c <"$leaf" | tr -d ' ')"
    shasum -a 256 "$leaf" | awk '{print $1}'
  done < <(find "$protocol_root" -type f -print | LC_ALL=C sort)
}
assert_protocol_local() {
  local marker="$1" state="$2"
  [[ "$(<"$protocol_root/player/19/M3restart")" == m3-restart-protocol-payload ]] || fail "protocol live bytes are not exact"
  [[ "$(shasum -a 256 "$protocol_root/player/19/M3restart" | awk '{print $1}')" == "$protocol_post" ]] || fail "protocol live hash is not exact"
  [[ -f "$protocol_root/character-save-journal/$command.prepared" ]] || fail "protocol prepared evidence is absent"
  [[ -f "$protocol_root/character-save-journal/$command.$marker" ]] || fail "protocol $marker marker is absent"
  [[ ! -e "$protocol_root/character-save-stage/$command.stage" && ! -e "$protocol_root/character-save-journal/$command.published.tmp" && ! -e "$protocol_root/character-save-journal/$command.acked.tmp" ]] || fail "protocol temporary topology remains"
  grep -Fx "request_sha256=$request" "$protocol_root/character-save-journal/$command.prepared" >/dev/null || fail "protocol prepared receipt differs from supplied digest"
  grep -Fx "post_sha256=$protocol_post" "$protocol_root/character-save-journal/$command.prepared" >/dev/null || fail "protocol prepared post digest differs"
  grep -Fx "state=$state" "$protocol_root/character-save-journal/$command.$marker" >/dev/null || fail "protocol marker state is not $state"
  grep -Fx "request_sha256=$request" "$protocol_root/character-save-journal/$command.$marker" >/dev/null || fail "protocol marker receipt differs from supplied digest"
  if [[ "$marker" == published ]]; then
    [[ ! -e "$protocol_root/character-save-journal/$command.acked" ]] || fail "protocol DB_ACKED marker exists before recovery"
  else
    [[ -f "$protocol_root/character-save-journal/$command.published" ]] || fail "protocol LEGACY_PUBLISHED evidence was lost during recovery"
  fi
}
wait_ready() {
  local ready="$1" deadline=$((SECONDS + wait_timeout_seconds))
  while (( SECONDS < deadline )); do
    [[ -n "$waiting_pid" ]] && kill -0 "$waiting_pid" 2>/dev/null || return 1
    [[ -s "$ready" && "$(<"$ready")" == ready ]] && return 0
    sleep .05
  done
  return 1
}
wait_committed() {
  local deadline=$((SECONDS + wait_timeout_seconds))
  while (( SECONDS < deadline )); do
    [[ -n "$waiting_pid" ]] && kill -0 "$waiting_pid" 2>/dev/null || return 1
    [[ "$(row_count)" == 1 ]] && return 0
    sleep .05
  done
  return 1
}
run_waiting_child() {
  local mode="$1" ready="$2"
  : >"$ready"
  env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable PGCONNECT_TIMEOUT=2 \
    PGAPPNAME=m3-receipt-restart-child \
    PGOPTIONS='-c role=mud_writer -c statement_timeout=5000 -c lock_timeout=1000' \
    M3_RECEIPT_TRANSPORT_DATABASE_URL="$database_url" \
    M3_RPC_RESTART_REQUEST_SHA256="$request" M3_RPC_RESTART_ROOT="$protocol_root" M3_RPC_RESTART_READY_FD=3 \
    "$harness" "$mode" 3>"$ready" &
  waiting_pid=$!
}
expect_sigkill() {
  local pid="$1" status
  kill -KILL "$pid" || return 1
  set +e
  wait "$pid" 2>/dev/null
  status=$?
  set -e
  waiting_pid=''
  [[ "$status" == 137 ]]
}
assert_epoch() {
  local epoch="$1" writer="$2" sealed="$3" fences="$4"
  [[ "$(epoch_matches "$epoch" "$writer" "$sealed" "$fences")" == t ]] || fail "epoch oracle is not expected $epoch/$writer/$sealed/$fences"
}
epoch_matches() {
  local epoch="$1" writer="$2" sealed="$3" fences="$4"
  run_super --command="select exists(select 1 from private.game_character_writer_epochs where world_id='$world' and writer_epoch=$epoch and writer_instance_id='$writer'::uuid and (sealed_at is not null)=$sealed) and (select count(*)=$fences from private.game_character_writer_epoch_fences where world_id='$world') and (select count(*)=0 from private.game_character_shadow_receipts where character_id='$character'::uuid) and exists(select 1 from private.game_character_legacy_heads where character_id='$character'::uuid and head_state='absent' and revision=0);"
}
wait_epoch() {
  local epoch="$1" writer="$2" sealed="$3" fences="$4"
  local deadline=$((SECONDS + wait_timeout_seconds))
  while (( SECONDS < deadline )); do
    [[ -n "$waiting_pid" ]] && kill -0 "$waiting_pid" 2>/dev/null || return 1
    [[ "$(epoch_matches "$epoch" "$writer" "$sealed" "$fences")" == t ]] && return 0
    sleep .05
  done
  return 1
}
renew_matches() {
  [[ "$(epoch_matches 1 "$writer_a" false 0)" == t ]] &&
    [[ "$(run_super --command="select expires_at > clock_timestamp()+interval '2 minutes' from private.game_character_writer_epochs where world_id='$world';")" == t ]]
}
wait_renewed_epoch() {
  local deadline=$((SECONDS + wait_timeout_seconds))
  while (( SECONDS < deadline )); do
    [[ -n "$waiting_pid" ]] && kill -0 "$waiting_pid" 2>/dev/null || return 1
    renew_matches && return 0
    sleep .05
  done
  return 1
}
assert_successor_fence() {
  [[ "$(run_super --command="select count(*)=1 and bool_and(writer_epoch=1 and writer_instance_id='$writer_a'::uuid and successor_epoch=2) from private.game_character_writer_epoch_fences where world_id='$world';")" == t ]] || fail "successor fence does not preserve A epoch 1 to B epoch 2"
}
cleanup() {
  if [[ -n "$waiting_pid" ]] && kill -0 "$waiting_pid" 2>/dev/null; then
    kill -KILL "$waiting_pid" 2>/dev/null || true
    wait "$waiting_pid" 2>/dev/null || true
  fi
  run_super --command="delete from private.game_character_shadow_receipts where character_id='$character'::uuid; delete from private.game_character_legacy_heads where character_id='$character'::uuid; delete from private.game_character_writer_epoch_fences where world_id='$world'; delete from private.game_character_writer_epochs where world_id='$world'; delete from public.game_characters where id='$character'::uuid;" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

[[ "$database_url" == *"@127.0.0.1:1/"* ]] && offline_port=65535
offline_url="$(sed -E "s#@127\\.0\\.0\\.1:[0-9]+/#@127.0.0.1:${offline_port}/#" <<<"$database_url")"

# A connection loss before the server receives a receipt leaves the SQL oracle unchanged.
reset_fixture
run_child acquire-a || fail "fresh A acquire failed"
before="$(snapshot)"
run_child record-deferred "$offline_url" || fail "pre-commit connection loss did not defer"
[[ "$(snapshot)" == "$before" && "$(row_count)" == 0 ]] || fail "pre-commit loss mutated oracle or receipt count"
run_child record-acked || fail "fresh exact retry after pre-commit loss did not ACK"
[[ "$(row_count)" == 1 ]] || fail "pre-commit retry did not create exactly one receipt"
[[ "$(run_super --command="select head_state='existing' and head_sha256='$post' and revision=1 and writer_epoch=1 from private.game_character_legacy_heads where character_id='$character'::uuid;")" == t ]] || fail "pre-commit retry did not advance exactly one head"

# A fresh connected process killed before PQsendQueryParams has the same no-commit oracle.
reset_fixture
run_child acquire-a || fail "fresh A acquire for process-loss lane failed"
before="$(snapshot)"
run_waiting_child record-before-send-wait "$tmp/before.ready"
pid="$waiting_pid"
wait_ready "$tmp/before.ready" || { kill -KILL "$pid" 2>/dev/null || true; fail "pre-send child did not signal readiness"; }
expect_sigkill "$pid" || fail "pre-send child was not SIGKILLed"
[[ "$(snapshot)" == "$before" && "$(row_count)" == 0 ]] || fail "pre-send SIGKILL mutated oracle"
run_child record-acked || fail "fresh retry after pre-send SIGKILL did not ACK"
[[ "$(row_count)" == 1 ]] || fail "pre-send recovery did not create one receipt"

# External SQL observes the receipt commit before the waiting process can consume its result.
reset_fixture
run_child acquire-a || fail "fresh A acquire for outcome-unknown lane failed"
run_waiting_child record-after-send-wait "$tmp/after.ready"
pid="$waiting_pid"
wait_ready "$tmp/after.ready" || { kill -KILL "$pid" 2>/dev/null || true; fail "post-send child did not signal readiness"; }
wait_committed || { kill -KILL "$pid" 2>/dev/null || true; fail "SQL oracle did not observe committed receipt"; }
committed="$(snapshot)"
expect_sigkill "$pid" || fail "outcome-unknown child was not SIGKILLed"
run_child record-acked || fail "fresh exact retry after outcome-unknown did not ACK"
[[ "$(snapshot)" == "$committed" && "$(row_count)" == 1 ]] || fail "outcome-unknown exact retry changed committed receipt/head"

# A fresh protocol process reaches a durable local LEGACY_PUBLISHED marker before
# its exact dynamic receipt is committed, then recovery writes DB_ACKED via native libpq.
reset_protocol_fixture
make_protocol_root
run_child acquire-a || fail "fresh A acquire for protocol composition lane failed"
run_waiting_child protocol-after-send-wait "$tmp/protocol.ready"
pid="$waiting_pid"
wait_ready "$tmp/protocol.ready" || { kill -KILL "$pid" 2>/dev/null || true; fail "protocol child did not signal async receipt readiness"; }
wait_committed || { kill -KILL "$pid" 2>/dev/null || true; fail "SQL oracle did not observe committed protocol receipt"; }
protocol_committed="$(snapshot)"
protocol_local_before="$(local_snapshot)"
assert_protocol_local published LEGACY_PUBLISHED
expect_sigkill "$pid" || fail "protocol outcome-unknown child was not SIGKILLed"
[[ "$(snapshot)" == "$protocol_committed" && "$(local_snapshot)" == "$protocol_local_before" ]] || fail "SIGKILL changed committed protocol SQL or local topology"
run_child protocol-recover || fail "fresh native-libpq protocol recovery failed"
[[ "$(snapshot)" == "$protocol_committed" && "$(row_count)" == 1 ]] || fail "protocol exact replay changed committed SQL snapshot"
protocol_local_after="$(local_snapshot)"
assert_protocol_local acked DB_ACKED
[[ "$protocol_local_before" != "$protocol_local_after" ]] || fail "protocol recovery did not durably transition local marker topology"

# Fresh RPC processes are killed after the server commits and before they can consume results.
reset_fixture
run_waiting_child acquire-a-after-send-wait "$tmp/acquire.ready"
pid="$waiting_pid"
wait_ready "$tmp/acquire.ready" || { kill -KILL "$pid" 2>/dev/null || true; fail "acquire child did not signal readiness"; }
wait_epoch 1 "$writer_a" false 0 || { kill -KILL "$pid" 2>/dev/null || true; fail "SQL oracle did not observe committed A acquire"; }
expect_sigkill "$pid" || fail "committed acquire child was not SIGKILLed"
run_child acquire-a || fail "fresh exact retry after committed A acquire failed"
assert_epoch 1 "$writer_a" false 0

run_super --command="update private.game_character_writer_epochs set issued_at=clock_timestamp()-interval '2 seconds', expires_at=clock_timestamp()-interval '1 second' where world_id='$world';" >/dev/null
run_waiting_child renew-a-after-send-wait "$tmp/renew.ready"
pid="$waiting_pid"
wait_ready "$tmp/renew.ready" || { kill -KILL "$pid" 2>/dev/null || true; fail "renew child did not signal readiness"; }
wait_renewed_epoch || { kill -KILL "$pid" 2>/dev/null || true; fail "SQL oracle did not observe committed A renew"; }
expect_sigkill "$pid" || fail "committed renew child was not SIGKILLed"
run_child renew-a || fail "fresh exact retry after committed A renew failed"
assert_epoch 1 "$writer_a" false 0

run_waiting_child seal-a-after-send-wait "$tmp/seal.ready"
pid="$waiting_pid"
wait_ready "$tmp/seal.ready" || { kill -KILL "$pid" 2>/dev/null || true; fail "seal child did not signal readiness"; }
wait_epoch 1 "$writer_a" true 0 || { kill -KILL "$pid" 2>/dev/null || true; fail "SQL oracle did not observe committed A seal"; }
expect_sigkill "$pid" || fail "committed seal child was not SIGKILLed"
run_child seal-a-p0001 || fail "successful A seal was not terminal P0001 on exact retry"
assert_epoch 1 "$writer_a" true 0
run_super --command="update private.game_character_writer_epochs set issued_at=clock_timestamp()-interval '2 seconds', expires_at=clock_timestamp()-interval '1 second' where world_id='$world';" >/dev/null
run_child acquire-b || fail "sealed expired A did not permit B acquire"
assert_epoch 2 "$writer_b" false 1
assert_successor_fence
fenced="$(snapshot)"
run_child renew-a-p0001 || fail "fenced A renew was not P0001"
run_child record-rejected || fail "fenced A record was not P0001/rejected"
[[ "$(snapshot)" == "$fenced" && "$(row_count)" == 0 ]] || fail "fenced A retry mutated route/head/receipt/epoch/fence oracle"

echo "m3 receipt transport restart integration passed (fresh libpq SIGKILL lanes, SQL commit oracles, exact retries, and terminal seal successor fence)"
