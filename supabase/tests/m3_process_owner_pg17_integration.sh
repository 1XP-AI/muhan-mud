#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_PROCESS_OWNER_PG17_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 process-owner PG17 integration skipped (set M3_PROCESS_OWNER_PG17_ALLOW_DISPOSABLE=1)"
  exit 0
}

command -v docker >/dev/null || {
  echo "m3 process-owner PG17 integration requires docker" >&2
  exit 2
}
command -v "${PG_CONFIG:-pg_config}" >/dev/null || {
  echo "m3 process-owner PG17 integration requires pg_config" >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
harness="${1:-}"

container="m3-process-owner-${RANDOM}-${RANDOM}"
container_id=""
tmp="$(mktemp -d "${TMPDIR:-/tmp}/m3-process-owner.XXXXXX")"
home="$tmp/muhan-home"
ready="$tmp/stale-ready"
password="m3-process-owner-disposable-password"
writer_url=""
database_host="${M3_PROCESS_OWNER_DATABASE_HOST:-127.0.0.1}"
stale_pid=""

[[ "$database_host" =~ ^[A-Za-z0-9.-]+$ ]] || {
  echo "m3 process-owner PG17 integration database host is invalid" >&2
  exit 2
}

cleanup() {
  if [[ -n "$stale_pid" ]]; then
    kill "$stale_pid" >/dev/null 2>&1 || true
    wait "$stale_pid" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$tmp"
  if [[ "$container_id" =~ ^[0-9a-f]{64}$ ]]; then docker rm --force "$container_id" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

if [[ -z "$harness" ]]; then
  harness="$tmp/character_save_journal_v2_process_owner_pg17_integration"
  compiler="${CC:-cc}"
  flags=(-std=gnu89 -fcommon -Wall -Wextra -Werror)
  if "$compiler" --version 2>/dev/null | head -1 | grep -qi clang; then
    flags+=(-Wno-deprecated-non-prototype)
  fi
  if [[ "${M3_PROCESS_OWNER_PG17_SANITIZE:-1}" != 0 ]]; then
    flags+=(-O1 -fno-omit-frame-pointer -fsanitize=address,undefined)
  fi
  "$compiler" "${flags[@]}" \
    -DCHARACTER_SAVE_JOURNAL_V2_TESTING \
    -DCHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING \
    -DCHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING \
    -DCHARACTER_SAVE_JOURNAL_V2_ACK_TESTING \
    -I"$repo_root/src" -I"$(${PG_CONFIG:-pg_config} --includedir)" \
    "$repo_root/tests/integration/character_save_journal_v2_process_owner_pg17_integration.c" \
    "$repo_root/src/character_save_journal_v2_process_owner.c" \
    "$repo_root/src/character_save_journal_v2_live_ops.c" \
    "$repo_root/src/character_save_journal_v2_player_store.c" \
    "$repo_root/src/character_save_journal_v2_bootstrap.c" \
    "$repo_root/src/character_save_journal_v2_rpc_transport.c" \
    "$repo_root/src/character_save_journal_v2_rpc_transport_native.c" \
    "$repo_root/src/character_save_journal_v2_protocol.c" \
    "$repo_root/src/character_save_journal_v2_recovery.c" \
    "$repo_root/src/character_save_journal_v2_ack.c" \
    "$repo_root/src/character_save_journal_v2_publish.c" \
    "$repo_root/src/character_save_journal_v2_route.c" \
    "$repo_root/src/character_save_journal_v2_writer.c" \
    "$repo_root/src/character_save_journal_v2.c" \
    "$repo_root/src/character_player_snapshot_v1_handoff.c" \
    "$repo_root/src/character_player_snapshot_v1_capture.c" \
    "$repo_root/src/character_player_snapshot_v1_artifact.c" \
    "$repo_root/src/player_snapshot_v1.c" \
    "$repo_root/src/object_graph_v1.c" "$repo_root/src/cdto_v1.c" \
    "$repo_root/src/player_record_serializer.c" "$repo_root/src/player_store.c" \
    "$repo_root/src/utf8_text.c" -L"$(${PG_CONFIG:-pg_config} --libdir)" -lpq \
    -o "$harness"
fi
[[ -x "$harness" ]] || {
  echo "m3 process-owner PG17 integration requires an executable harness" >&2
  exit 2
}

container_id="$(docker run --detach --rm --name "$container" --publish 127.0.0.1::5432 \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=128m \
  --volume "$repo_root:/workspace:ro" postgres:17-alpine)"
container="$container_id"

# PostgreSQL's entrypoint starts a temporary initialization server.  Two
# consecutive SELECTs are required so that server cannot be mistaken for the
# final PostgreSQL 17 process.
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
  echo "m3 process-owner PG17 integration did not reach stable readiness" >&2
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
  echo "m3 process-owner RED unexpectedly passed through migration 120" >&2
  exit 1
fi
if ! grep -Fq \
    "m3 provisioning head baseline contract failed: normal finalize must atomically seed the exact existing revision-zero baseline" \
    "$red_log"; then
  cat "$red_log" >&2
  echo "m3 process-owner RED failed for an unexpected reason" >&2
  exit 1
fi
echo "RED PostgreSQL 17: owner E2E baseline contract is absent through migration 120"

run_super --file=/workspace/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql
run_super --file=/workspace/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql

run_super --command="alter role mud_writer_login password '$password';"
port="$(docker port "$container" 5432/tcp | sed -n '1{s/.*://;p;}')"
[[ "$port" =~ ^[0-9]+$ ]] || { echo "could not discover PostgreSQL loopback port" >&2; exit 1; }
writer_url="postgresql://mud_writer_login:${password}@${database_host}:${port}/postgres"

# The private baseline is deliberately seeded as an absent revision zero head:
# the owner must atomically publish the first real legacy record and receipt.
run_super --command="insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,lifecycle,storage_format) values ('c9500000-0000-4000-8000-000000000001'::uuid,'m3-process-owner','M3owner','M3owner','a5','imported_unclaimed',1); insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values ('c9500000-0000-4000-8000-000000000001'::uuid,'absent',null,1,0,null);"

mkdir -m 700 "$home" "$home/player" "$home/player/a5" \
  "$home/character-save-stage" "$home/character-save-journal"
deadline="$(docker exec --env PGPASSWORD=contract-only-password "$container" \
  psql --host=127.0.0.1 --username=postgres --dbname=postgres --no-psqlrc \
    --tuples-only --no-align --command="select to_char(clock_timestamp()+interval '4 minutes','YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"')")"
[[ "$deadline" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || {
  echo "could not generate canonical injected lease deadline" >&2
  exit 1
}

# This is a database failure injection at the exact receipt write boundary;
# it is not a C transport/receipt stub.  Route lookup, serialization, local
# prepare, atomic publication, and native PQexecParams all run first.
run_super --command="create function private.m3_process_owner_receipt_fault() returns trigger language plpgsql as \$\$ begin if new.command_id='96000000-0000-4000-8000-000000000001'::uuid then raise exception using errcode='P0001', message='disposable receipt fault'; end if; return new; end \$\$; create trigger m3_process_owner_receipt_fault before insert on private.game_character_shadow_receipts for each row execute function private.m3_process_owner_receipt_fault();"

set +e
env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable \
  ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
  MUHAN_HOME="$home" M3_PROCESS_OWNER_DATABASE_URL="$writer_url" \
  M3_PROCESS_OWNER_DEADLINE="$deadline" "$harness" receipt-crash
crash_status=$?
set -e
[[ "$crash_status" -eq 137 ]] || {
  echo "receipt failure process did not terminate at the required SIGKILL cutpoint" >&2
  exit 1
}

[[ "$(scalar "select (select count(*)=0 from private.game_character_shadow_receipts where character_id='c9500000-0000-4000-8000-000000000001'::uuid) and (select revision=0 and head_state='absent' from private.game_character_legacy_heads where character_id='c9500000-0000-4000-8000-000000000001'::uuid);")" == t ]] || {
  echo "receipt failure advanced the database before acknowledgement" >&2
  exit 1
}
[[ -f "$home/character-save-journal/96000000-0000-4000-8000-000000000001.prepared" && \
   -f "$home/character-save-journal/96000000-0000-4000-8000-000000000001.published" && \
   ! -e "$home/character-save-journal/96000000-0000-4000-8000-000000000001.acked" ]] || {
  echo "receipt failure did not preserve exactly published local evidence" >&2
  exit 1
}

run_super --command="drop trigger m3_process_owner_receipt_fault on private.game_character_shadow_receipts; drop function private.m3_process_owner_receipt_fault();"
env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable \
  ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
  MUHAN_HOME="$home" M3_PROCESS_OWNER_DATABASE_URL="$writer_url" \
  M3_PROCESS_OWNER_DEADLINE="$deadline" "$harness" recover-replay

[[ "$(scalar "select (select count(*)=1 from private.game_character_shadow_receipts where character_id='c9500000-0000-4000-8000-000000000001'::uuid) and (select head_state='existing' and revision=1 and writer_epoch=1 from private.game_character_legacy_heads where character_id='c9500000-0000-4000-8000-000000000001'::uuid) and (select writer_revision=1 and writer_epoch=1 from private.game_character_shadow_receipts where character_id='c9500000-0000-4000-8000-000000000001'::uuid and command_id='96000000-0000-4000-8000-000000000001'::uuid);")" == t ]] || {
  echo "recovery did not advance the real receipt/head exactly from revision 0 to 1" >&2
  exit 1
}
[[ -f "$home/character-save-journal/96000000-0000-4000-8000-000000000001.acked" && \
   -f "$home/character-save-journal/96000000-0000-4000-8000-000000000001.published" ]] || {
  echo "recovery did not publish the DB_ACKED local marker atomically" >&2
  exit 1
}

# A real server-side seal between startup and save makes the held writer stale.
# The shell only coordinates timing; the owner discovers the stale lease via
# its native renew RPC before it can mutate the already-acknowledged evidence.
env -i PATH="$PATH" LANG=C PGPASSFILE=/dev/null PGSSLMODE=disable \
  ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1 \
  MUHAN_HOME="$home" M3_PROCESS_OWNER_DATABASE_URL="$writer_url" \
  M3_PROCESS_OWNER_DEADLINE="$deadline" M3_PROCESS_OWNER_STALE_READY="$ready" \
  "$harness" stale &
stale_pid=$!
for _ in $(seq 1 200); do
  [[ -s "$ready" && "$(<"$ready")" == ready ]] && break
  kill -0 "$stale_pid" 2>/dev/null || { wait "$stale_pid" || true; echo "stale owner exited before coordination" >&2; exit 1; }
  sleep .05
done
[[ -s "$ready" && "$(<"$ready")" == ready ]] || { kill "$stale_pid" 2>/dev/null || true; wait "$stale_pid" 2>/dev/null || true; echo "stale owner did not become ready" >&2; exit 1; }
run_super --command="update private.game_character_writer_epochs set sealed_at=clock_timestamp() where world_id='m3-process-owner';"
: >"$ready.go"
wait "$stale_pid"
stale_pid=""

[[ "$(scalar "select (select count(*)=1 from private.game_character_shadow_receipts where character_id='c9500000-0000-4000-8000-000000000001'::uuid) and (select revision=1 from private.game_character_legacy_heads where character_id='c9500000-0000-4000-8000-000000000001'::uuid);")" == t ]] || {
  echo "stale writer advanced receipt or head" >&2
  exit 1
}
echo "GREEN PostgreSQL 17: native owner bootstrap, SIGKILL restart recovery-before-install, legacy serialization/publication, receipt/head 0->1, idempotent replay, stale preservation, DB_ACKED marker, transport ownership, and secret-safe harness passed"
