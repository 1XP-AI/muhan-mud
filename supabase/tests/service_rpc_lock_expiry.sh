#!/usr/bin/env bash
set -euo pipefail

[[ "${SERVICE_RPC_LOCK_EXPIRY_ALLOW_DISPOSABLE:-}" == 1 ]] || { echo "service RPC lock-expiry contract skipped (set SERVICE_RPC_LOCK_EXPIRY_ALLOW_DISPOSABLE=1)"; exit 0; }
database_url="${DATABASE_URL:-}"
case "$database_url" in postgres://*@127.0.0.1:*/*|postgresql://*@127.0.0.1:*/*) ;; *) echo "requires loopback query-free DATABASE_URL" >&2; exit 2;; esac
[[ "$database_url" != *"?"* ]] || { echo "requires query-free DATABASE_URL" >&2; exit 2; }
args=("$database_url" --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1)
tmp="$(mktemp -d "${TMPDIR:-/tmp}/service-rpc-lock-expiry.XXXXXX")"
actor=81000000-0000-0000-0000-000000000001
character=82000000-0000-0000-0000-000000000001
session=83000000-0000-0000-0000-000000000001
correlation=84000000-0000-0000-0000-000000000001
cleanup_fixtures() {
  psql "${args[@]}" --command="delete from private.game_character_sessions where session_id='$session'::uuid; delete from private.game_character_onboarding_intents where correlation_id='$correlation'::uuid; delete from public.game_characters where id='$character'::uuid; delete from auth.users where id='$actor'::uuid;" >/dev/null 2>&1 || true
}
cleanup() {
  cleanup_fixtures
  rm -rf "$tmp"
}
trap cleanup EXIT
cleanup_fixtures
psql "${args[@]}" --command="
 insert into auth.users(id,aud,role,email,encrypted_password,email_confirmed_at,raw_app_meta_data,raw_user_meta_data,created_at,updated_at) values('$actor'::uuid,'authenticated','authenticated','rpc-lock-expiry@example.invalid','contract-only',now(),'{}','{}',now(),now());
 insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,owner_user_id,claimed_at) values('$character'::uuid,'rpc-lock-expiry','Rpclock','Rpclock','7c','active','$actor'::uuid,now());" >/dev/null
snapshot() { psql "${args[@]}" --command="select md5(concat_ws('|',(select lifecycle::text||':'||owner_user_id::text||':'||claimed_at::text from public.game_characters where id='$character'::uuid),(select coalesce(session_id::text,'')||':'||coalesce(expires_at::text,'')||':'||gateway_instance_id from private.game_character_sessions where character_id='$character'::uuid),(select count(*)::text from private.game_character_onboarding_intents where correlation_id='$correlation'::uuid)));"; }
wait_row_holder() { local app="$1"; for _ in $(seq 1 100); do [[ "$(psql "${args[@]}" --command="select exists(select 1 from pg_locks l join pg_stat_activity a on a.pid=l.pid where l.locktype='relation' and l.relation='public.game_characters'::regclass and l.granted and a.application_name='$app' and a.wait_event='PgSleep');" 2>/dev/null || true)" == t ]] && return 0; sleep .05; done; return 1; }
wait_advisory_holder() { local app="$1"; for _ in $(seq 1 100); do [[ "$(psql "${args[@]}" --command="select exists(select 1 from pg_locks l join pg_stat_activity a on a.pid=l.pid where l.locktype='advisory' and l.granted and a.application_name='$app' and a.wait_event='PgSleep');" 2>/dev/null || true)" == t ]] && return 0; sleep .05; done; return 1; }
before="$(snapshot)"
app=session-expiry-holder
( PGAPPNAME="$app" psql "${args[@]}" --command="begin; select 1 from public.game_characters where id='$character'::uuid for update; select pg_sleep(4); rollback;" ) >/dev/null 2>&1 & holder=$!
wait_row_holder "$app" || { echo "could not observe session holder" >&2; exit 1; }
set +e
psql "${args[@]}" --command="begin; set local role service_role; do \$\$ begin perform public.begin_game_character_session('$actor'::uuid,'$character'::uuid,'$session'::uuid,'test-gateway',clock_timestamp()+interval '2 seconds'); raise exception using errcode='P0002'; exception when sqlstate '22023' then null; end \$\$; commit;" >"$tmp/session.log" 2>&1
status=$?
set -e; wait "$holder"
[[ $status == 0 && "$(snapshot)" == "$before" ]] || { echo "session stale-clock rejection mutated fixture" >&2; exit 1; }
psql "${args[@]}" --command="insert into private.game_character_sessions(character_id,session_id,actor_user_id,gateway_instance_id,expires_at) values('$character'::uuid,'$session'::uuid,'$actor'::uuid,'test-gateway',clock_timestamp()+interval '1 minute');" >/dev/null
before="$(snapshot)"
app=renew-expiry-holder
( PGAPPNAME="$app" psql "${args[@]}" --command="begin; select 1 from public.game_characters where id='$character'::uuid for update; select pg_sleep(4); rollback;" ) >/dev/null 2>&1 & holder=$!
wait_row_holder "$app" || { echo "could not observe renew holder" >&2; exit 1; }
set +e
psql "${args[@]}" --command="begin; set local role service_role; do \$\$ begin perform public.renew_game_character_session('$session'::uuid,'test-gateway',clock_timestamp()+interval '2 seconds'); raise exception using errcode='P0002'; exception when sqlstate '22023' then null; end \$\$; commit;" >"$tmp/renew.log" 2>&1
status=$?
set -e; wait "$holder"
[[ $status == 0 && "$(snapshot)" == "$before" ]] || { echo "renew stale-clock rejection mutated lease" >&2; exit 1; }
psql "${args[@]}" --command="delete from private.game_character_sessions where session_id='$session'::uuid;" >/dev/null
before="$(snapshot)"
app=onboarding-expiry-holder
( PGAPPNAME="$app" psql "${args[@]}" --command="begin; select pg_advisory_xact_lock(hashtextextended('$actor',717126642)); select pg_sleep(4); rollback;" ) >/dev/null 2>&1 & holder=$!
wait_advisory_holder "$app" || { echo "could not observe onboarding holder" >&2; exit 1; }
set +e
psql "${args[@]}" --command="begin; set local role service_role; do \$\$ begin perform public.begin_game_character_onboarding('$actor'::uuid,'$correlation'::uuid,'claim',clock_timestamp()+interval '2 seconds'); raise exception using errcode='P0002'; exception when sqlstate '22023' then null; end \$\$; commit;" >"$tmp/onboarding.log" 2>&1
status=$?
set -e; wait "$holder"
[[ $status == 0 && "$(snapshot)" == "$before" ]] || { echo "onboarding stale-clock rejection mutated fixture" >&2; exit 1; }
echo "service RPC lock-expiry contract passed (PG17 lock wait; 22023 expiry policy; fixture unchanged)"
