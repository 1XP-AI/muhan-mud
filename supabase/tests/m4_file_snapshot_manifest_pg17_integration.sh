#!/usr/bin/env bash
set -euo pipefail

[[ "${M4_FILE_SNAPSHOT_MANIFEST_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m4 manifest PG17 integration skipped (set M4_FILE_SNAPSHOT_MANIFEST_ALLOW_DISPOSABLE=1)"
  exit 0
}

command -v docker >/dev/null || {
  echo "m4 manifest PG17 integration requires docker" >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="m4-manifest-${RANDOM}-${RANDOM}"
container_id=""
scratch="$(mktemp -d "${TMPDIR:-/tmp}/muhan-m4-manifest.XXXXXX")"

cleanup() {
  if [[ "$container_id" =~ ^[0-9a-f]{64}$ ]]; then docker rm --force "$container_id" >/dev/null 2>&1 || true; fi
  rm -f -- "$scratch"/* >/dev/null 2>&1 || true
  rmdir "$scratch" >/dev/null 2>&1 || true
}
trap cleanup EXIT

container_id="$(docker run --detach --rm --name "$container" \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=128m \
  --volume "$repo_root:/workspace:ro" \
  postgres:17-alpine)"
container="$container_id"

run_super() {
  docker exec --interactive \
    --env PGPASSWORD=contract-only-password \
    "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}

ready_streak=0
for _ in $(seq 1 60); do
  if run_super --command='select 1' >/dev/null 2>&1; then
    ready_streak=$((ready_streak + 1))
    if [[ "$ready_streak" -ge 2 ]]; then
      break
    fi
  else
    ready_streak=0
  fi
  sleep 1
done
if [[ "$ready_streak" -lt 2 ]]; then
  docker logs "$container" >&2 || true
  echo "m4 manifest PG17 integration did not reach stable readiness" >&2
  exit 2
fi

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
  20260912000000_m3_live_save_route.sql \
  20260913000000_m3_provisioning_head_baseline.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

if run_super \
  --file=/workspace/supabase/tests/m4_file_snapshot_manifest_contract.sql \
  >/dev/null 2>&1; then
  echo "m4 manifest RED unexpectedly passed through migration 130" >&2
  exit 1
fi
echo "RED PostgreSQL 17: M4 manifest RPCs are absent through migration 130"

run_super \
  --file=/workspace/supabase/migrations/20260914000000_m4_file_snapshot_manifest.sql
run_super \
  --file=/workspace/supabase/tests/m4_file_snapshot_manifest_contract.sql

run_super <<'SQL'
alter role mud_writer_login password 'contract-only-writer-password';

insert into public.game_characters (
  id, world_id, legacy_name, legacy_name_key, legacy_shard,
  lifecycle, storage_format
) values (
  'a9410000-0000-0000-0000-000000000001',
  'm4-race',
  'Raceone',
  'Raceone',
  substr(encode(public.digest(convert_to('Raceone', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'imported_unclaimed',
  1
);

insert into private.game_character_legacy_heads (
  character_id, head_state, storage_format, revision
) values (
  'a9410000-0000-0000-0000-000000000001',
  'absent',
  1,
  0
);

select * from private.acquire_game_world_writer_epoch(
  'm4-race',
  'b9410000-0000-0000-0000-000000000001'::uuid,
  clock_timestamp() + interval '3 minutes'
);

select * from private.record_legacy_published_receipt(
  'm4-race',
  'Raceone',
  'a9410000-0000-0000-0000-000000000001'::uuid,
  'c9410000-0000-0000-0000-000000000001'::uuid,
  'b9410000-0000-0000-0000-000000000001'::uuid,
  private.game_character_shadow_request_sha256(
    'm4-race',
    'a9410000-0000-0000-0000-000000000001'::uuid,
    'Raceone',
    substr(encode(public.digest(convert_to('Raceone', 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'c9410000-0000-0000-0000-000000000001'::uuid,
    'b9410000-0000-0000-0000-000000000001'::uuid,
    1::bigint,
    1::bigint,
    'absent',
    null,
    repeat('a', 64),
    1::smallint
  ),
  1::bigint,
  1::bigint,
  'absent',
  null,
  repeat('a', 64),
  1::smallint
);

select * from private.record_legacy_published_receipt(
  'm4-race',
  'Raceone',
  'a9410000-0000-0000-0000-000000000001'::uuid,
  'c9410000-0000-0000-0000-000000000002'::uuid,
  'b9410000-0000-0000-0000-000000000001'::uuid,
  private.game_character_shadow_request_sha256(
    'm4-race',
    'a9410000-0000-0000-0000-000000000001'::uuid,
    'Raceone',
    substr(encode(public.digest(convert_to('Raceone', 'UTF8'), 'sha1'), 'hex'), 1, 2),
    'c9410000-0000-0000-0000-000000000002'::uuid,
    'b9410000-0000-0000-0000-000000000001'::uuid,
    1::bigint,
    2::bigint,
    'existing',
    repeat('a', 64),
    repeat('b', 64),
    1::smallint
  ),
  1::bigint,
  2::bigint,
  'existing',
  repeat('a', 64),
  repeat('b', 64),
  1::smallint
);
SQL

request_one="$(run_super --tuples-only --no-align --command="
  select request_sha256
    from private.game_character_shadow_receipts
   where command_id = 'c9410000-0000-0000-0000-000000000001'::uuid
")"
request_two="$(run_super --tuples-only --no-align --command="
  select request_sha256
    from private.game_character_shadow_receipts
   where command_id = 'c9410000-0000-0000-0000-000000000002'::uuid
")"

writer_call() {
  local application_name="$1"
  local sql="$2"
  docker exec \
    --env PGPASSWORD=contract-only-writer-password \
    --env PGAPPNAME="$application_name" \
    "$container" \
    psql --host=127.0.0.1 --username=mud_writer_login --dbname=postgres \
      --no-psqlrc --quiet --tuples-only --no-align \
      --set=ON_ERROR_STOP=1 --set=VERBOSITY=verbose \
      --command="$sql"
}

writer_identity="$(writer_call m4-writer-identity \
  "select current_user || '|' || session_user || '|' || current_setting('role') || '|' || private.m3_assert_writer_session()::text")"
[[ "$writer_identity" == "mud_writer|mud_writer_login|mud_writer|true" ]] || {
  echo "actual M4 writer login identity is wrong: $writer_identity" >&2
  exit 1
}

wait_for_table_lock() {
  local expected="$1"
  local sql="$2"
  local observed
  for _ in $(seq 1 100); do
    observed="$(run_super --tuples-only --no-align --command="$sql")"
    if [[ "$observed" == "$expected" ]]; then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

run_race() {
  local label="$1"
  local command_id="$2"
  local request_sha256="$3"
  local post_sha256="$4"
  local first_octets="$5"
  local second_octets="$6"
  local expectation="$7"
  local control="$scratch/$label.control"
  local blocker_out="$scratch/$label.blocker.out"
  local blocker_err="$scratch/$label.blocker.err"
  local first_out="$scratch/$label.first.out"
  local first_err="$scratch/$label.first.err"
  local second_out="$scratch/$label.second.out"
  local second_err="$scratch/$label.second.err"
  local blocker_pid first_pid second_pid first_status second_status
  local first_result second_result
  local blocker_application="m4-blocker-$label"
  local first_application="m4-writer-$label-1"
  local second_application="m4-writer-$label-2"
  local first_sql second_sql

  mkfifo "$control"
  (
    {
      printf '%s\n' \
        'begin;' \
        'lock table private.game_character_m4_file_snapshot_manifests in share row exclusive mode;'
      cat "$control"
    } | docker exec --interactive \
      --env PGPASSWORD=contract-only-password \
      --env PGAPPNAME="$blocker_application" \
      "$container" \
      psql --host=127.0.0.1 --username=postgres --dbname=postgres \
        --no-psqlrc --quiet --set=ON_ERROR_STOP=1 \
        >"$blocker_out" 2>"$blocker_err"
  ) &
  blocker_pid=$!

  wait_for_table_lock 1 "
    select count(*)
      from pg_locks l
      join pg_class c on c.oid = l.relation
      join pg_stat_activity a on a.pid = l.pid
     where c.oid = 'private.game_character_m4_file_snapshot_manifests'::regclass
       and l.mode = 'ShareRowExclusiveLock'
       and l.granted
       and a.application_name = '$blocker_application'
  " || {
    echo "race $label blocker did not acquire its table lock" >&2
    return 1
  }

  first_sql="select outcome from private.record_m4_file_snapshot_manifest_for_receipt(
    'a9410000-0000-0000-0000-000000000001'::uuid,
    '$command_id'::uuid,
    '$request_sha256',
    'legacy-file-manifest-v1',
    '$post_sha256',
    $first_octets
  )"
  second_sql="select outcome from private.record_m4_file_snapshot_manifest_for_receipt(
    'a9410000-0000-0000-0000-000000000001'::uuid,
    '$command_id'::uuid,
    '$request_sha256',
    'legacy-file-manifest-v1',
    '$post_sha256',
    $second_octets
  )"

  writer_call "$first_application" "$first_sql" \
    >"$first_out" 2>"$first_err" &
  first_pid=$!
  writer_call "$second_application" "$second_sql" \
    >"$second_out" 2>"$second_err" &
  second_pid=$!

  wait_for_table_lock 2 "
    select count(*)
      from pg_stat_activity
     where application_name in ('$first_application', '$second_application')
       and wait_event_type = 'Lock'
       and state = 'active'
  " || {
    echo "race $label did not place both writers behind the same barrier" >&2
    return 1
  }

  printf '%s\n' 'commit;' >"$control"
  wait "$blocker_pid"

  set +e
  wait "$first_pid"
  first_status=$?
  wait "$second_pid"
  second_status=$?
  set -e

  first_result="$(tr -d '[:space:]' <"$first_out")"
  second_result="$(tr -d '[:space:]' <"$second_out")"

  if grep -q '23505' "$first_err" "$second_err"; then
    echo "race $label leaked a unique violation" >&2
    return 1
  fi

  if [[ "$expectation" == exact ]]; then
    if [[ "$first_status" -ne 0 || "$second_status" -ne 0 ]] ||
       [[ "$first_result|$second_result" != "RECORDED|EXACT_RETRY" &&
          "$first_result|$second_result" != "EXACT_RETRY|RECORDED" ]]; then
      echo "same-payload race failed: $first_result|$second_result|$first_status|$second_status" >&2
      return 1
    fi
  else
    if [[ "$first_status" -eq 0 && "$second_status" -ne 0 ]]; then
      [[ "$first_result" == RECORDED ]] || return 1
      grep -q 'P0001' "$second_err" || return 1
    elif [[ "$second_status" -eq 0 && "$first_status" -ne 0 ]]; then
      [[ "$second_result" == RECORDED ]] || return 1
      grep -q 'P0001' "$first_err" || return 1
    else
      echo "different-payload race did not produce one winner and one rejection" >&2
      return 1
    fi
  fi

  rm -f -- \
    "$control" "$blocker_out" "$blocker_err" \
    "$first_out" "$first_err" "$second_out" "$second_err"
}

run_race \
  exact \
  c9410000-0000-0000-0000-000000000001 \
  "$request_one" \
  "$(printf 'a%.0s' $(seq 1 64))" \
  9 9 exact

exact_state_before="$(run_super --tuples-only --no-align --command="
  select to_jsonb(m)::text
    from private.game_character_m4_file_snapshot_manifests m
   where command_id = 'c9410000-0000-0000-0000-000000000001'::uuid
")"
[[ "$(writer_call m4-writer-exact-retry "
  select outcome
    from private.record_m4_file_snapshot_manifest_for_receipt(
      'a9410000-0000-0000-0000-000000000001'::uuid,
      'c9410000-0000-0000-0000-000000000001'::uuid,
      '$request_one',
      'legacy-file-manifest-v1',
      repeat('a', 64),
      9
    )
")" == EXACT_RETRY ]] || {
  echo "post-race exact retry did not converge" >&2
  exit 1
}
exact_state_after="$(run_super --tuples-only --no-align --command="
  select to_jsonb(m)::text
    from private.game_character_m4_file_snapshot_manifests m
   where command_id = 'c9410000-0000-0000-0000-000000000001'::uuid
")"
[[ "$exact_state_before" == "$exact_state_after" ]] || {
  echo "post-race exact retry changed immutable evidence" >&2
  exit 1
}

run_race \
  conflict \
  c9410000-0000-0000-0000-000000000002 \
  "$request_two" \
  "$(printf 'b%.0s' $(seq 1 64))" \
  10 11 conflict

[[ "$(run_super --tuples-only --no-align --command="
  select count(*) = 2
     and count(*) filter (
       where command_id = 'c9410000-0000-0000-0000-000000000001'::uuid
         and snapshot_octets = 9
     ) = 1
     and count(*) filter (
       where command_id = 'c9410000-0000-0000-0000-000000000002'::uuid
         and snapshot_octets in (10, 11)
     ) = 1
    from private.game_character_m4_file_snapshot_manifests
")" == t ]] || {
  echo "concurrent manifests did not converge to exactly two immutable rows" >&2
  exit 1
}

replay_state_before="$(run_super --tuples-only --no-align --command="
  select jsonb_build_object(
    'manifests', (
      select jsonb_agg(to_jsonb(m) order by m.command_id)
        from private.game_character_m4_file_snapshot_manifests m
    ),
    'writer_password', (
      select rolpassword from pg_authid where rolname = 'mud_writer_login'
    )
  )::text
")"

# The second application happens with real evidence rows and an operator-set
# password present, not only against an empty schema.
run_super \
  --file=/workspace/supabase/migrations/20260914000000_m4_file_snapshot_manifest.sql

replay_state_after="$(run_super --tuples-only --no-align --command="
  select jsonb_build_object(
    'manifests', (
      select jsonb_agg(to_jsonb(m) order by m.command_id)
        from private.game_character_m4_file_snapshot_manifests m
    ),
    'writer_password', (
      select rolpassword from pg_authid where rolname = 'mud_writer_login'
    )
  )::text
")"
[[ "$replay_state_before" == "$replay_state_after" ]] || {
  echo "migration replay changed manifest evidence or writer credentials" >&2
  exit 1
}

[[ "$(run_super --tuples-only --no-align --command="
  select count(*) = 2 and bool_and(manifest_state = 'EXACT')
    from private.list_m4_file_snapshot_manifest_reconciliation('m4-race', 10)
")" == t ]] || {
  echo "final reconciliation did not report both race manifests as exact" >&2
  exit 1
}

echo "GREEN PostgreSQL 17: M4 manifest contract, concurrent retry, conflict, and data-bearing replay passed"
