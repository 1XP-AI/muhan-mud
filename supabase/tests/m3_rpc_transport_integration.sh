#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_RPC_TRANSPORT_ALLOW_DISPOSABLE:-}" == 1 ]] || { echo "m3 RPC transport integration skipped (set M3_RPC_TRANSPORT_ALLOW_DISPOSABLE=1)"; exit 0; }
harness="${1:-}"; database_url="${DATABASE_URL:-}"; writer_url="${M3_RPC_TRANSPORT_DATABASE_URL:-}"
[[ -x "$harness" && -n "$database_url" && -n "$writer_url" ]] || { echo "m3 RPC transport integration requires harness, DATABASE_URL, and writer URL" >&2; exit 2; }
for url in "$database_url" "$writer_url"; do
  [[ "$url" != *[[:space:]]* && "$url" != *\?* && "$url" != *\#* && "$url" =~ ^postgres(ql)?://[^:/\?\#@[:space:]]+:[^/\?\#@[:space:]]*@127\.0\.0\.1:[0-9]+/[^/\?\#[:space:]]+$ ]] || { echo "m3 RPC transport integration requires query-free numeric-port 127.0.0.1 URLs" >&2; exit 2; }
done

world=m3-rpc-contract
bad_cast_world=m3-rpc-bad-cast
character=92000000-0000-0000-0000-000000000001
writer=94000000-0000-0000-0000-000000000001
command=93000000-0000-0000-0000-000000000001
run_super() { env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable PGCONNECT_TIMEOUT=5 psql "$database_url" --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 "$@"; }
cleanup() { run_super --command="delete from private.game_character_shadow_receipts where character_id='$character'::uuid or world_id='$bad_cast_world'; delete from private.game_character_legacy_heads where character_id='$character'::uuid; delete from private.game_character_writer_epoch_fences where world_id in ('$world','$bad_cast_world'); delete from private.game_character_writer_epochs where world_id in ('$world','$bad_cast_world'); delete from public.game_characters where id='$character'::uuid;" >/dev/null 2>&1 || true; }
trap cleanup EXIT

ready=0
for _ in $(seq 1 60); do
  if run_super --command='select 1' >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[[ "$ready" == 1 ]] || { echo "m3 RPC transport integration: PostgreSQL was not ready after 60 attempts" >&2; exit 1; }
cleanup
run_super --command="insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format,imported_file_sha256) values('$character'::uuid,'$world','M3hero','M3hero','11','imported_unclaimed',1,repeat('e',64)); insert into private.game_character_legacy_heads(character_id,head_state,storage_format,revision) values('$character'::uuid,'absent',1,0);" >/dev/null
if ! M3_RPC_TRANSPORT_DATABASE_URL="$writer_url" \
  M3_RPC_TRANSPORT_SUPER_DATABASE_URL="$database_url" "$harness"; then
  echo "m3 RPC transport integration harness failed" >&2
  exit 1
fi
[[ "$(run_super --command="select count(*) = 0 from private.game_character_writer_epochs where world_id='$bad_cast_world';")" == t ]] || { echo "bad timestamptz cast persisted a writer epoch" >&2; exit 1; }
[[ "$(run_super --command="select count(*) = 0 from private.game_character_writer_epoch_fences where world_id='$bad_cast_world';")" == t ]] || { echo "bad timestamptz cast persisted a writer epoch fence" >&2; exit 1; }
[[ "$(run_super --command="select count(*) = 0 from private.game_character_shadow_receipts where world_id='$bad_cast_world';")" == t ]] || { echo "bad timestamptz cast persisted a receipt" >&2; exit 1; }
[[ "$(run_super --command="select h.revision=1 and h.head_sha256=repeat('a',64) and count(r.command_id)=1 from private.game_character_legacy_heads h left join private.game_character_shadow_receipts r on r.character_id=h.character_id where h.character_id='$character'::uuid group by h.revision,h.head_sha256;")" == t ]] || { echo "exact receipt retry or no-mutation controls failed" >&2; exit 1; }
[[ "$(run_super --command="select writer_epoch=1 and sealed_at is not null from private.game_character_writer_epochs where world_id='$world';")" == t ]] || { echo "writer lease was not sealed exactly once" >&2; exit 1; }
echo "m3 RPC transport PG17: BEGIN rejection, SET ROLE NONE drift, terminated backend no-retry, bad-cast no-mutation, route/acquire/renew/receipt retry/seal passed"
