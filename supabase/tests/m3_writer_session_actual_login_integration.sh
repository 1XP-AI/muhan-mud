#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_WRITER_SESSION_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 writer session actual-login integration skipped set M3_WRITER_SESSION_ALLOW_DISPOSABLE=1"
  exit 0
}

command -v docker >/dev/null || {
  echo "m3 writer session actual-login integration requires docker" >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="m3-writer-session-${RANDOM}-${RANDOM}"
password="m3-contract-login-password"
runtime_harness="${1:-}"
conninfo_file=""

[[ -n "$runtime_harness" && -x "$runtime_harness" ]] || {
  echo "m3 writer session actual-login integration requires an executable runtime harness" >&2
  exit 2
}

cleanup() {
  if [[ -n "$conninfo_file" ]]; then
    rm -f -- "$conninfo_file"
  fi
  docker rm --force "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --detach --rm --name "$container" \
  --publish 127.0.0.1::5432 \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=128m \
  --volume "$repo_root:/workspace:ro" \
  postgres:17-alpine >/dev/null

for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready --username=postgres --dbname=postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$container" pg_isready --username=postgres --dbname=postgres >/dev/null

run_super() {
  docker exec --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
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
  20260910000000_m3_shadow_receipt_route_v2.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

# RED command on a real PostgreSQL 17 server: before 110, neither the actual
# login role nor the session assertion exists.
[[ "$(run_super --tuples-only --no-align --command="select (to_regrole('mud_writer_login') is null and to_regprocedure('private.m3_assert_writer_session()') is null)::text;")" == true ]] || {
  echo "RED failed expected no writer login role or assertion before 110" >&2
  exit 1
}
echo "RED PostgreSQL 17 pre migration writer login is absent"

run_super --file=/workspace/supabase/migrations/20260911000000_m3_writer_session.sql
run_super --command="alter role mud_writer_login password '$password';"
password_before="$(run_super --tuples-only --no-align --command="select rolpassword from pg_authid where rolname='mud_writer_login';")"
[[ -n "$password_before" ]] || {
  echo "operator password hash was not installed" >&2
  exit 1
}

# Unexpected membership is fail-closed before migration can change either role.
run_super --command="create role m3_writer_session_intruder nologin; grant m3_writer_session_intruder to mud_writer_login;"
if run_super --file=/workspace/supabase/migrations/20260911000000_m3_writer_session.sql >/dev/null 2>&1; then
  echo "unexpected related membership was accepted" >&2
  exit 1
fi
run_super --command="revoke m3_writer_session_intruder from mud_writer_login; drop role m3_writer_session_intruder;"
echo "GREEN PostgreSQL 17 unexpected membership fails closed"

# GREEN replay command: the new migration must retain the operator password
# hash while correcting all privilege-bearing login-role attributes.
run_super --command="alter role mud_writer_login superuser inherit createdb createrole replication bypassrls;"
run_super --file=/workspace/supabase/migrations/20260911000000_m3_writer_session.sql
password_after="$(run_super --tuples-only --no-align --command="select rolpassword from pg_authid where rolname='mud_writer_login';")"
[[ "$password_before" == "$password_after" ]] || {
  echo "writer login password hash changed on migration replay" >&2
  exit 1
}

run_super --file=/workspace/supabase/tests/m3_writer_session_contract.sql

host_port="$(docker port "$container" 5432/tcp | sed -n '1{s/.*://;p;}')"
[[ "$host_port" =~ ^[0-9]+$ ]] || {
  echo "could not discover disposable PostgreSQL 17 loopback port" >&2
  exit 1
}
conninfo_file="$(mktemp "${TMPDIR:-/tmp}/m3-writer-session-conninfo.XXXXXX")"
chmod 600 "$conninfo_file"
printf 'host=127.0.0.1 port=%s dbname=postgres user=mud_writer_login password=%s connect_timeout=5\n' \
  "$host_port" "$password" >"$conninfo_file"

green="$(docker exec --env PGPASSWORD="$password" "$container" \
  psql --host=127.0.0.1 --username=mud_writer_login --dbname=postgres \
    --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1 \
    --command="select (current_user='mud_writer' and session_user='mud_writer_login' and current_setting('statement_timeout')='5s' and current_setting('lock_timeout')='1s' and current_setting('idle_in_transaction_session_timeout')='5s' and current_setting('search_path')='pg_catalog' and private.m3_assert_writer_session())::text;")"
[[ "$green" == true ]] || {
  echo "GREEN failed actual password login did not establish the writer session" >&2
  exit 1
}
echo "GREEN PostgreSQL 17 actual password login establishes mud_writer session"

env -i PATH="$PATH" LANG=C MUD_M3_MODE=probe MUD_M3_WORLD_ID=m3-runtime-contract \
  MUD_M3_CONNINFO_FILE="$conninfo_file" "$runtime_harness"
echo "GREEN PostgreSQL 17 native M3 runtime probe accepted mud_writer_login"
