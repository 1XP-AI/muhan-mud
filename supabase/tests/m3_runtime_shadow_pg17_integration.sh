#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_RUNTIME_SHADOW_PG17_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 runtime shadow PG17 integration skipped (set M3_RUNTIME_SHADOW_PG17_ALLOW_DISPOSABLE=1)"
  exit 0
}

# runtime_native deliberately exposes the shadow adapter only on Linux.  Do
# not turn a non-Linux build into a false green: use a Linux CI/runner with an
# already-installed PostgreSQL client toolchain instead.
[[ "$(uname -s)" == Linux ]] || {
  echo "m3 runtime shadow PG17 integration requires Linux native runtime support" >&2
  exit 2
}
command -v docker >/dev/null || {
  echo "m3 runtime shadow PG17 integration requires docker" >&2
  exit 2
}
command -v "${PG_CONFIG:-pg_config}" >/dev/null || {
  echo "m3 runtime shadow PG17 integration requires pg_config" >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
harness="${1:-}"
container="m3-runtime-shadow-${RANDOM}-${RANDOM}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/m3-runtime-shadow.XXXXXX")"
home="$tmp/muhan-home"
conninfo_file="$tmp/writer.conninfo"
password="m3-runtime-shadow-disposable-password"
database_host="${M3_RUNTIME_SHADOW_PG17_DATABASE_HOST:-127.0.0.1}"

[[ "$database_host" =~ ^[A-Za-z0-9.-]+$ ]] || {
  echo "m3 runtime shadow PG17 integration database host is invalid" >&2
  exit 2
}

cleanup() {
  rm -rf -- "$tmp"
  docker rm --force "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

if [[ -z "$harness" ]]; then
  harness="$tmp/character_save_journal_v2_runtime_shadow_pg17_integration"
  compiler="${CC:-cc}"
  flags=(-std=gnu89 -fcommon -Wall -Wextra -Werror)
  if "$compiler" --version 2>/dev/null | head -1 | grep -qi clang; then
    flags+=(-Wno-deprecated-non-prototype)
  fi
  if [[ "${M3_RUNTIME_SHADOW_PG17_SANITIZE:-1}" != 0 ]]; then
    flags+=(-O1 -fno-omit-frame-pointer -fsanitize=address,undefined)
  fi
  "$compiler" "${flags[@]}" \
    -DCHARACTER_SAVE_JOURNAL_V2_TESTING \
    -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING \
    -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING \
    -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING \
    -I"$repo_root/src" -I"$(${PG_CONFIG:-pg_config} --includedir)" \
    "$repo_root/tests/integration/character_save_journal_v2_runtime_shadow_pg17_integration.c" \
    "$repo_root/src/character_save_journal_v2_runtime.c" \
    "$repo_root/src/character_save_journal_v2_runtime_native.c" \
    "$repo_root/src/character_save_journal_v2_process_owner.c" \
    "$repo_root/src/character_save_journal_v2_live_ops.c" \
    "$repo_root/src/character_save_journal_v2_player_store.c" \
    "$repo_root/src/character_save_journal_v2_rpc_transport.c" \
    "$repo_root/src/character_save_journal_v2_rpc_transport_native.c" \
    "$repo_root/src/character_save_journal_v2_protocol.c" \
    "$repo_root/src/character_save_journal_v2_recovery.c" \
    "$repo_root/src/character_save_journal_v2_ack.c" \
    "$repo_root/src/character_save_journal_v2_publish.c" \
    "$repo_root/src/character_save_journal_v2_route.c" \
    "$repo_root/src/character_save_journal_v2_writer.c" \
    "$repo_root/src/character_save_journal_v2_deadline.c" \
    "$repo_root/src/character_save_journal_v2_deadline_native.c" \
    "$repo_root/src/character_save_journal_v2_uuid.c" \
    "$repo_root/src/character_save_journal_v2_uuid_native.c" \
    "$repo_root/src/character_save_journal_v2.c" \
    "$repo_root/src/player_record_serializer.c" "$repo_root/src/player_store.c" \
    "$repo_root/src/utf8_text.c" -L"$(${PG_CONFIG:-pg_config} --libdir)" -lpq \
    -o "$harness"
fi
[[ -x "$harness" ]] || {
  echo "m3 runtime shadow PG17 integration requires an executable harness" >&2
  exit 2
}

docker run --detach --rm --name "$container" --publish 127.0.0.1::5432 \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=128m \
  --volume "$repo_root:/workspace:ro" postgres:17-alpine >/dev/null

# The entrypoint briefly accepts connections on its setup server.  Two
# consecutive queries prove that PostgreSQL 17's final server is serving.
ready_streak=0
for _ in $(seq 1 60); do
  if docker exec --env PGPASSWORD=contract-only-password "$container" \
      psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --tuples-only --no-align --command='select 1' \
      >/dev/null 2>&1; then
    ready_streak=$((ready_streak + 1))
    [[ "$ready_streak" -ge 2 ]] && break
  else
    ready_streak=0
  fi
  sleep 1
done
[[ "$ready_streak" -ge 2 ]] || {
  docker logs "$container" >&2 || true
  echo "m3 runtime shadow PG17 integration did not reach stable readiness" >&2
  exit 2
}

run_super() {
  docker exec --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}
scalar() {
  run_super --tuples-only --no-align --command="$1"
}

run_super --file=/workspace/supabase/tests/bootstrap_contract.sql
for migration in \
  20260902000000_game_identity.sql \
  20260903000000_character_onboarding.sql \
  20260904000000_onboarding_intent_safety.sql \
  20260905000000_claim_fingerprint_safety.sql \
  20260906000000_claim_actor_rate_limit.sql \
  20260907000000_claim_challenge_safety.sql \
  20260908000000_service_rpc_fresh_clock.sql \
  20260909000000_m3_shadow_receipts.sql \
  20260910000000_m3_shadow_receipt_route_v2.sql \
  20260911000000_m3_writer_session.sql \
  20260912000000_m3_live_save_route.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

red_log="$tmp/migration-130-red.log"
if run_super --file=/workspace/supabase/tests/m3_provisioning_head_baseline_contract.sql \
    >"$red_log" 2>&1; then
  echo "m3 runtime shadow RED unexpectedly passed through migration 120" >&2
  exit 1
fi
if ! grep -Fq \
    "m3 provisioning head baseline contract failed: normal finalize must atomically seed the exact existing revision-zero baseline" \
    "$red_log"; then
  cat "$red_log" >&2
  echo "m3 runtime shadow RED failed for an unexpected reason" >&2
  exit 1
fi
echo "RED PostgreSQL 17: runtime shadow prerequisite baseline is absent through migration 120"

run_super --file=/workspace/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql
run_super --file=/workspace/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql
run_super --command="alter role mud_writer_login password '$password';"
port="$(docker port "$container" 5432/tcp | sed -n '1{s/.*://;p;}')"
[[ "$port" =~ ^[0-9]+$ ]] || {
  echo "could not discover PostgreSQL loopback port" >&2
  exit 1
}
umask 077
printf '%s' "postgresql://mud_writer_login:${password}@${database_host}:${port}/postgres" >"$conninfo_file"
chmod 600 "$conninfo_file"

mkdir -m 700 "$home" "$home/player" "$home/player/d2" \
  "$home/character-save-stage" "$home/character-save-journal"
run_super --command="insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values ('c9600000-0000-4000-8000-000000000001'::uuid,'m3-runtime-shadow','M3shadow','M3shadow','d2','imported_unclaimed',1); insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values ('c9600000-0000-4000-8000-000000000001'::uuid,'absent',null,1,0,null);"

# This is a PostgreSQL fault at the true receipt boundary.  The C harness
# still executes runtime conninfo loading, native PQconnectdb/PQexecParams,
# owner bootstrap, route lookup, serializer, journal prepare and publish.
run_super --command="create function private.m3_runtime_shadow_receipt_fault() returns trigger language plpgsql as \$\$ begin raise exception using errcode='P0001', message='disposable runtime receipt fault'; end \$\$; create trigger m3_runtime_shadow_receipt_fault before insert on private.game_character_shadow_receipts for each row execute function private.m3_runtime_shadow_receipt_fault();"

set +e
env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable \
  ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
  MUHAN_HOME="$home" MUD_M3_MODE=shadow MUD_M3_WORLD_ID=m3-runtime-shadow \
  MUD_M3_CONNINFO_FILE="$conninfo_file" "$harness" fault
fault_status=$?
set -e
[[ "$fault_status" -eq 137 ]] || {
  echo "runtime shadow fault process did not terminate at the required SIGKILL recovery boundary" >&2
  exit 1
}
[[ "$(scalar "select (select count(*)=0 from private.game_character_shadow_receipts where character_id='c9600000-0000-4000-8000-000000000001'::uuid) and (select revision=0 and head_state='absent' from private.game_character_legacy_heads where character_id='c9600000-0000-4000-8000-000000000001'::uuid);")" == t ]] || {
  echo "receipt failure advanced PostgreSQL before durable replay" >&2
  exit 1
}
[[ "$(find "$home/character-save-journal" -maxdepth 1 -type f -name '*.published' | wc -l | tr -d ' ')" == 1 && \
   "$(find "$home/character-save-journal" -maxdepth 1 -type f -name '*.acked' | wc -l | tr -d ' ')" == 0 ]] || {
  echo "fault run did not preserve exactly one published, unacknowledged journal" >&2
  exit 1
}

run_super --command="drop trigger m3_runtime_shadow_receipt_fault on private.game_character_shadow_receipts; drop function private.m3_runtime_shadow_receipt_fault();"
env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable \
  ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
  MUHAN_HOME="$home" MUD_M3_MODE=shadow MUD_M3_WORLD_ID=m3-runtime-shadow \
  MUD_M3_CONNINFO_FILE="$conninfo_file" "$harness" recover

[[ "$(scalar "select (select count(*)=1 from private.game_character_shadow_receipts where character_id='c9600000-0000-4000-8000-000000000001'::uuid) and (select head_state='existing' and revision=1 and writer_epoch=1 from private.game_character_legacy_heads where character_id='c9600000-0000-4000-8000-000000000001'::uuid) and (select writer_revision=1 and writer_epoch=1 from private.game_character_shadow_receipts where character_id='c9600000-0000-4000-8000-000000000001'::uuid);")" == t ]] || {
  echo "native runtime restart did not replay the real receipt/head exactly once" >&2
  exit 1
}
[[ "$(find "$home/character-save-journal" -maxdepth 1 -type f -name '*.published' | wc -l | tr -d ' ')" == 1 && \
   "$(find "$home/character-save-journal" -maxdepth 1 -type f -name '*.acked' | wc -l | tr -d ' ')" == 1 ]] || {
  echo "native runtime recovery did not durably acknowledge its published journal" >&2
  exit 1
}
echo "GREEN PostgreSQL 17: native shadow runtime persisted, crash-recovered, and safely shut down"
