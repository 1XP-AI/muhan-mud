#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_SHADOW_LOCK_EXPIRY_ALLOW_DISPOSABLE:-}" == 1 ]] || { echo "m3 shadow lock-expiry contract skipped (set M3_SHADOW_LOCK_EXPIRY_ALLOW_DISPOSABLE=1)"; exit 0; }
database_url="${DATABASE_URL:-}"
case "$database_url" in postgres://*@127.0.0.1:*/*|postgresql://*@127.0.0.1:*/*) ;; *) echo "requires loopback query-free DATABASE_URL" >&2; exit 2;; esac
[[ "$database_url" != *"?"* ]] || { echo "requires query-free DATABASE_URL" >&2; exit 2; }
args=("$database_url" --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1)
world=m3-lock-expiry
character=96000000-0000-0000-0000-000000000001
writer=97000000-0000-0000-0000-000000000001
command=98000000-0000-0000-0000-000000000001
post=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
race_world=m3-route-share
race_character=96000000-0000-0000-0000-000000000002
race_writer=97000000-0000-0000-0000-000000000002
race_command=98000000-0000-0000-0000-000000000002
race_post=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
cleanup() {
  psql "${args[@]}" --command="delete from private.game_character_shadow_receipts where character_id='$character'::uuid; delete from private.game_character_legacy_heads where character_id='$character'::uuid; delete from private.game_character_writer_epoch_fences where world_id='$world'; delete from private.game_character_writer_epochs where world_id='$world'; delete from public.game_characters where id='$character'::uuid;" >/dev/null 2>&1 || true
  psql "${args[@]}" --command="delete from private.game_character_shadow_receipts where character_id='$race_character'::uuid; delete from private.game_character_legacy_heads where character_id='$race_character'::uuid; delete from private.game_character_writer_epoch_fences where world_id='$race_world'; delete from private.game_character_writer_epochs where world_id='$race_world'; delete from public.game_characters where id='$race_character'::uuid;" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup
psql "${args[@]}" --command="insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values('$character'::uuid,'$world','M3lock','M3lock',substr(encode(public.digest(convert_to('M3lock','UTF8'),'sha1'),'hex'),1,2),'imported_unclaimed',1); select private.acquire_game_world_writer_epoch('$world','$writer'::uuid,clock_timestamp()+interval '2 seconds');" >/dev/null
request="$(psql "${args[@]}" --command="select private.game_character_shadow_request_sha256('$world','$character'::uuid,'M3lock',substr(encode(public.digest(convert_to('M3lock','UTF8'),'sha1'),'hex'),1,2),'$command'::uuid,'$writer'::uuid,1::bigint,1::bigint,'absent',null,'$post',1::smallint);")"
app=m3-shadow-lock-expiry-holder
( PGAPPNAME="$app" psql "${args[@]}" --command="begin; select 1 from public.game_characters where id='$character'::uuid for update; select pg_sleep(4); rollback;" ) >/dev/null 2>&1 & holder=$!
observed=0
for _ in $(seq 1 100); do
  if [[ "$(psql "${args[@]}" --command="select exists(select 1 from pg_locks l join pg_stat_activity a on a.pid=l.pid where l.locktype='relation' and l.relation='public.game_characters'::regclass and l.granted and a.application_name='$app' and a.wait_event='PgSleep');" 2>/dev/null || true)" == t ]]; then observed=1; break; fi
  sleep .05
done
[[ $observed == 1 ]] || { wait "$holder" || true; echo "could not observe route row holder" >&2; exit 1; }
set +e
psql "${args[@]}" --command="do \$\$ begin perform private.record_legacy_published_receipt('$world','M3lock','$character'::uuid,'$command'::uuid,'$writer'::uuid,'$request',1::bigint,1::bigint,'absent',null,'$post',1::smallint); raise exception using errcode='P0002'; exception when sqlstate 'P0001' then null; end \$\$;" >/dev/null 2>&1
status=$?
set -e
wait "$holder"
[[ $status == 0 ]] || { echo "record did not reject expired epoch after route lock wait" >&2; exit 1; }
[[ "$(psql "${args[@]}" --command="select (select count(*) from private.game_character_shadow_receipts where character_id='$character'::uuid)::text || '|' || (select count(*) from private.game_character_legacy_heads where character_id='$character'::uuid)::text;")" == '0|0' ]] || { echo "stale-clock record mutated receipt/head" >&2; exit 1; }

race_shard="$(psql "${args[@]}" --command="select substr(encode(public.digest(convert_to('M3share','UTF8'),'sha1'),'hex'),1,2);")"
psql "${args[@]}" --command="insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values('$race_character'::uuid,'$race_world','M3share','M3share','$race_shard','imported_unclaimed',1); insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values('$race_character'::uuid,'absent',null,1,0,null); select private.acquire_game_world_writer_epoch('$race_world','$race_writer'::uuid,clock_timestamp()+interval '3 minutes');" >/dev/null
race_request="$(psql "${args[@]}" --command="select private.game_character_shadow_request_sha256('$race_world','$race_character'::uuid,'M3share','$race_shard','$race_command'::uuid,'$race_writer'::uuid,1::bigint,1::bigint,'absent',null,'$race_post',1::smallint);")"
race_app=m3-shadow-route-share-holder
( PGAPPNAME="$race_app" psql "${args[@]}" --command="begin; update public.game_characters set lifecycle='suspended' where id='$race_character'::uuid; select pg_sleep(4); commit;" ) >/dev/null 2>&1 & race_holder=$!
observed=0
for _ in $(seq 1 100); do
  if [[ "$(psql "${args[@]}" --command="select exists(select 1 from pg_locks l join pg_stat_activity a on a.pid=l.pid where l.locktype='relation' and l.relation='public.game_characters'::regclass and l.granted and a.application_name='$race_app' and a.wait_event='PgSleep');" 2>/dev/null || true)" == t ]]; then observed=1; break; fi
  sleep .05
done
[[ $observed == 1 ]] || { wait "$race_holder" || true; echo "could not observe lifecycle update holder" >&2; exit 1; }
started=$SECONDS
set +e
psql "${args[@]}" --command="do \$\$ begin perform private.record_legacy_published_receipt('$race_world','M3share','$race_character'::uuid,'$race_command'::uuid,'$race_writer'::uuid,'$race_request',1::bigint,1::bigint,'absent',null,'$race_post',1::smallint); raise exception using errcode='P0002'; exception when sqlstate 'P0001' then null; end \$\$;" >/dev/null 2>&1
status=$?
set -e
wait "$race_holder"
elapsed=$((SECONDS - started))
[[ $status == 0 && $elapsed -ge 3 ]] || { echo "record did not wait for committed lifecycle update and reject it" >&2; exit 1; }
[[ "$(psql "${args[@]}" --command="select (select lifecycle::text from public.game_characters where id='$race_character'::uuid) || '|' || (select count(*) from private.game_character_shadow_receipts where character_id='$race_character'::uuid)::text || '|' || (select head_state || ':' || revision::text from private.game_character_legacy_heads where character_id='$race_character'::uuid);")" == 'suspended|0|absent:0' ]] || { echo "committed lifecycle race mutated receipt/head or was not rejected" >&2; exit 1; }
echo "m3 shadow receipt contracts passed (PG17 fresh-clock expiry and committed lifecycle-update race; no mutation)"
