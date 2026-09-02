#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 receipt transport integration skipped (set M3_RECEIPT_TRANSPORT_ALLOW_DISPOSABLE=1)"
  exit 0
}

database_url="${M3_RECEIPT_TRANSPORT_DATABASE_URL:-}"
harness="${1:-}"
[[ -n "$harness" && -x "$harness" ]] || { echo "m3 receipt transport integration harness is unavailable" >&2; exit 2; }
[[ "$database_url" != *[[:space:]]* && "$database_url" != *\?* && "$database_url" != *\#* ]] || {
  echo "m3 receipt transport integration requires a query-free loopback URL" >&2
  exit 2
}
if [[ ! "$database_url" =~ ^postgres(ql)?://[^:/\?\#@[:space:]]+:[^/\?\#@[:space:]]*@127\.0\.0\.1:[0-9]+/[^/\?\#[:space:]]+$ ]]; then
  echo "m3 receipt transport integration requires a numeric-port 127.0.0.1 postgres URI" >&2
  exit 2
fi

world=m3-contract
character=92000000-0000-0000-0000-000000000001
writer_a=94000000-0000-0000-0000-000000000001
function_signature='private.record_legacy_published_receipt(text,text,uuid,uuid,uuid,text,bigint,bigint,text,text,text,smallint)'
identity_snapshot=''
initial_state=''
committed_state=''

fail() { echo "m3 receipt transport integration failed: $1" >&2; exit 1; }
run_super() {
  env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable PGCONNECT_TIMEOUT=5 \
    PGAPPNAME=m3-receipt-transport-fixture \
    PGOPTIONS='-c statement_timeout=5000 -c lock_timeout=1000' \
    psql "$database_url" --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 "$@" 2>/dev/null
}
run_adapter() {
  env -i PATH="$PATH" LANG=C M3_RECEIPT_TRANSPORT_DATABASE_URL="$database_url" \
    PGPASSFILE=/dev/null PGSSLMODE=disable PGCONNECT_TIMEOUT=5 \
    PGAPPNAME=m3-receipt-transport-adapter \
    PGOPTIONS='-c role=mud_writer -c statement_timeout=5000 -c lock_timeout=1000' \
    "$harness" "$1" >/dev/null 2>&1
}
state_snapshot() {
  run_super --command="select jsonb_build_object('head',(select to_jsonb(h) from private.game_character_legacy_heads h where h.character_id='$character'::uuid),'receipts',coalesce((select jsonb_agg(to_jsonb(r) order by r.command_id) from private.game_character_shadow_receipts r where r.character_id='$character'::uuid),'[]'::jsonb),'writer_epoch',(select to_jsonb(w) from private.game_character_writer_epochs w where w.world_id='$world'),'writer_fences',coalesce((select jsonb_agg(to_jsonb(f) order by f.writer_epoch) from private.game_character_writer_epoch_fences f where f.world_id='$world'),'[]'::jsonb))::text;"
}
identity_state() {
  run_super --command="select id::text || '|' || world_id || '|' || coalesce(owner_user_id::text,'-') || '|' || lifecycle::text || '|' || legacy_name || '|' || legacy_name_key || '|' || legacy_shard || '|' || storage_format::text || '|' || coalesce(imported_file_sha256,'-') from public.game_characters where id='$character'::uuid;"
}
restore_grant() {
  run_super --command="grant execute on function $function_signature to mud_writer;" >/dev/null
}
cleanup_fixture() {
  local failed=0
  restore_grant || failed=1
  run_super --command="delete from private.game_character_shadow_receipts where character_id='$character'::uuid; delete from private.game_character_legacy_heads where character_id='$character'::uuid; delete from private.game_character_writer_epoch_fences where world_id='$world'; delete from private.game_character_writer_epochs where world_id='$world'; delete from public.game_characters where id='$character'::uuid;" >/dev/null || failed=1
  return "$failed"
}
on_exit() {
  local exit_code=$?
  trap - EXIT
  if ! cleanup_fixture; then
    echo "m3 receipt transport integration cleanup failed" >&2
    if [[ "$exit_code" -eq 0 ]]; then
      exit_code=1
    fi
  fi
  exit "$exit_code"
}
trap on_exit EXIT
cleanup_fixture || fail "could not reset disposable fixture"

run_super --command="insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format,imported_file_sha256) values('$character'::uuid,'$world','M3hero','M3hero','11','imported_unclaimed',1,repeat('e',64)); insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values('$character'::uuid,'absent',null,1,0,null); select private.acquire_game_world_writer_epoch('$world','$writer_a'::uuid,clock_timestamp()+interval '3 minutes');" >/dev/null || fail "could not create disposable fixture"

[[ "$(run_super --command="select (not rolcanlogin) and rolpassword is null and not exists(select 1 from pg_auth_members m join pg_roles r on r.oid=m.member or r.oid=m.roleid where r.rolname='mud_writer') from pg_roles where rolname='mud_writer';")" == t ]] || fail "mud_writer role is not isolated"
[[ "$(run_super --command="select not exists(select 1 from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname='private' and c.relkind in ('r','p') and has_table_privilege('mud_writer',c.oid,'select,insert,update,delete,truncate,references,trigger'));")" == t ]] || fail "mud_writer has private table privileges"
identity_snapshot="$(identity_state)" || fail "could not capture identity snapshot"
[[ "$(run_super --command="select head_state='absent' and head_sha256 is null and storage_format=1 and revision=0 and writer_epoch is null from private.game_character_legacy_heads where character_id='$character'::uuid;")" == t ]] || fail "fixture head is not explicit absent revision zero"
[[ "$(run_super --command="select writer_epoch=1 and writer_instance_id='$writer_a'::uuid and sealed_at is null and expires_at>clock_timestamp() from private.game_character_writer_epochs where world_id='$world';")" == t ]] || fail "fixture writer epoch is not active and unsealed"
initial_state="$(state_snapshot)" || fail "could not capture initial receipt/head snapshot"

run_adapter probe || fail "native connection did not prove role/session or private-table denial"

run_super --command="revoke execute on function $function_signature from mud_writer;" >/dev/null || fail "could not revoke receipt function"
run_adapter denied || fail "revoked receipt function did not map to deferred"
[[ "$(state_snapshot)" == "$initial_state" ]] || fail "revoked negative control mutated receipt or head"
[[ "$(identity_state)" == "$identity_snapshot" ]] || fail "revoked negative control mutated identity"

restore_grant || fail "could not restore receipt function grant"
run_adapter acked || fail "valid receipt was not acknowledged"
[[ "$(run_super --command="select h.head_state='existing' and h.head_sha256=repeat('a',64) and h.storage_format=1 and h.revision=1 and h.writer_epoch=1 and (select count(*)=1 from private.game_character_shadow_receipts r where r.character_id=h.character_id) from private.game_character_legacy_heads h where h.character_id='$character'::uuid;")" == t ]] || fail "acknowledged receipt/head state is wrong"
[[ "$(run_super --command="select request_sha256='eb83eb12f5875dad83f5f91e128c4874b2673b4089deff4967c58898d9c69171' and expected_state='absent' and expected_sha256 is null and post_sha256=repeat('a',64) and writer_epoch=1 and writer_revision=1 from private.game_character_shadow_receipts where character_id='$character'::uuid and command_id='93000000-0000-0000-0000-000000000001'::uuid;")" == t ]] || fail "golden receipt row is wrong"
committed_state="$(state_snapshot)" || fail "could not snapshot acknowledged receipt/head state"
run_adapter acked || fail "exact retry was not acknowledged"
[[ "$(state_snapshot)" == "$committed_state" ]] || fail "exact retry changed receipt/head rows or timestamps"

run_adapter invalid || fail "wrong canonical digest did not map to invalid freeze"
[[ "$(state_snapshot)" == "$committed_state" ]] || fail "invalid freeze mutated receipt or head"
run_adapter rejected || fail "stale writer did not map to rejected freeze"
[[ "$(state_snapshot)" == "$committed_state" ]] || fail "rejected freeze mutated receipt or head"
[[ "$(identity_state)" == "$identity_snapshot" ]] || fail "receipt transport mutated identity/lifecycle/ownership/storage snapshot"

echo "m3 receipt transport integration passed (native libpq callback, role isolation, retry, and freeze mappings)"
