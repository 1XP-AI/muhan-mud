#!/usr/bin/env bash

set -euo pipefail

if [[ "${CLAIM_CHALLENGE_LOCK_EXPIRY_ALLOW_DISPOSABLE:-}" != "1" ]]; then
  echo "claim challenge lock-expiry contract skipped (set CLAIM_CHALLENGE_LOCK_EXPIRY_ALLOW_DISPOSABLE=1)"
  exit 0
fi

database_url="${DATABASE_URL:-}"
if [[ -z "$database_url" || "$database_url" == *"?"* ]]; then
  echo "claim challenge lock-expiry contract requires a query-free loopback DATABASE_URL" >&2
  exit 2
fi
case "$database_url" in
  postgres://*@127.0.0.1:*/*|postgresql://*@127.0.0.1:*/*) ;;
  *)
    echo "claim challenge lock-expiry contract requires a PostgreSQL DATABASE_URL on 127.0.0.1" >&2
    exit 2
    ;;
esac

psql_args=("$database_url" --no-psqlrc --quiet --tuples-only --no-align --set=ON_ERROR_STOP=1)
test_tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/claim-challenge-lock-expiry.XXXXXX")"

cleanup_fixtures() {
  psql "${psql_args[@]}" --command="
    delete from public.game_identity_events
      where correlation_id in (
        '73000000-0000-0000-0000-000000000001'::uuid,
        '73000000-0000-0000-0000-000000000002'::uuid
      );
    delete from private.game_character_claim_requests
      where correlation_id in (
        '73000000-0000-0000-0000-000000000001'::uuid,
        '73000000-0000-0000-0000-000000000002'::uuid
      );
    delete from private.game_character_claim_attempts
      where correlation_id in (
        '73000000-0000-0000-0000-000000000001'::uuid,
        '73000000-0000-0000-0000-000000000002'::uuid
      );
    delete from private.game_character_onboarding_intents
      where correlation_id in (
        '73000000-0000-0000-0000-000000000001'::uuid,
        '73000000-0000-0000-0000-000000000002'::uuid
      );
    delete from public.game_characters
      where id in (
        '72000000-0000-0000-0000-000000000001'::uuid,
        '72000000-0000-0000-0000-000000000002'::uuid
      );
    delete from auth.users
      where id in (
        '71000000-0000-0000-0000-000000000001'::uuid,
        '71000000-0000-0000-0000-000000000002'::uuid
      );
  " >/dev/null 2>&1 || true
}

cleanup() {
  cleanup_fixtures
  rm -f "$test_tmp_dir/red.log" "$test_tmp_dir/call.log" >/dev/null 2>&1 || true
  rmdir "$test_tmp_dir" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Remove only exact fixtures left by an interrupted copy of this test.
cleanup_fixtures

psql "${psql_args[@]}" --command="
  insert into auth.users (
    id, aud, role, email, encrypted_password, email_confirmed_at,
    raw_app_meta_data, raw_user_meta_data, created_at, updated_at
  ) values
    ('71000000-0000-0000-0000-000000000001', 'authenticated', 'authenticated',
     'claim-lock-expiry-challenge@example.invalid', 'contract-only', now(), '{}', '{}', now(), now()),
    ('71000000-0000-0000-0000-000000000002', 'authenticated', 'authenticated',
     'claim-lock-expiry-final@example.invalid', 'contract-only', now(), '{}', '{}', now(), now());
  insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
    imported_file_sha256
  ) values
    ('72000000-0000-0000-0000-000000000001', 'contract-lock-expiry-challenge', 'Lockchal', 'Lockchal',
     substr(encode(public.digest(convert_to('Lockchal', 'UTF8'), 'sha1'), 'hex'), 1, 2),
     'imported_unclaimed', repeat('a', 64)),
    ('72000000-0000-0000-0000-000000000002', 'contract-lock-expiry-final', 'Lockfinal', 'Lockfinal',
     substr(encode(public.digest(convert_to('Lockfinal', 'UTF8'), 'sha1'), 'hex'), 1, 2),
     'imported_unclaimed', repeat('b', 64));
  set role service_role;
  select public.begin_game_character_onboarding(
    '71000000-0000-0000-0000-000000000001',
    '73000000-0000-0000-0000-000000000001', 'claim', now() + interval '10 minutes'
  );
  select public.challenge_legacy_game_character_onboarding(
    'contract-lock-expiry-challenge', 'Lockchal', repeat('a', 64),
    '71000000-0000-0000-0000-000000000001',
    '73000000-0000-0000-0000-000000000001'
  );
  select public.begin_game_character_onboarding(
    '71000000-0000-0000-0000-000000000002',
    '73000000-0000-0000-0000-000000000002', 'claim', now() + interval '10 minutes'
  );
  select public.challenge_legacy_game_character_onboarding(
    'contract-lock-expiry-final', 'Lockfinal', repeat('b', 64),
    '71000000-0000-0000-0000-000000000002',
    '73000000-0000-0000-0000-000000000002'
  );
  reset role;
" >/dev/null

snapshot() {
  local correlation="$1"
  local character="$2"
  psql "${psql_args[@]}" --command="
    select md5(concat_ws('|',
      coalesce((select lifecycle::text || ':' || coalesce(owner_user_id::text, '') || ':' || coalesce(claimed_at::text, '') || ':' || imported_file_sha256 || ':' || updated_at::text
                 from public.game_characters where id = '$character'::uuid), '<missing>'),
      coalesce((select actor_user_id::text || ':' || mode || ':' || status || ':' || expires_at::text || ':' || coalesce(completed_at::text, '')
                 from private.game_character_onboarding_intents where correlation_id = '$correlation'::uuid), '<missing>'),
      coalesce((select actor_user_id::text || ':' || character_id::text || ':' || imported_file_sha256 || ':' || allowed_at::text || ':' || allow_expires_at::text || ':' || coalesce(claimed_at::text, '')
                 from private.game_character_claim_attempts where correlation_id = '$correlation'::uuid), '<missing>'),
      (select count(*)::text from private.game_character_claim_requests where correlation_id = '$correlation'::uuid),
      (select count(*)::text from public.game_identity_events where correlation_id = '$correlation'::uuid)
    ));
  "
}

run_expiry_probe() {
  local world="$1"
  local name="$2"
  local correlation="$3"
  local actor="$4"
  local fingerprint="$5"
  local rpc="$6"
  local character="$7"
  local app="claim-lock-expiry-holder-${correlation##*-}"
  local advisory="select pg_advisory_xact_lock(hashtextextended(jsonb_build_array('$world', '$name')::text, 870254061));"
  local before after
  local holder_pid red_probe_pid
  local red_log="$test_tmp_dir/red.log"
  local call_log="$test_tmp_dir/call.log"
  local call_exit

  # Re-seed each attempt immediately before its snapshot. The two probes run
  # sequentially, so ageing both rows once would make the second RED proof
  # nondeterministic. The table check constraint enforces the exact 90-second
  # window for this 88-second-old seed.
  psql "${psql_args[@]}" --command="
    with challenge_time as (
      select clock_timestamp() - interval '88 seconds' as allowed_at
    )
    update private.game_character_claim_attempts as attempt
       set allowed_at = challenge_time.allowed_at,
           allow_expires_at = challenge_time.allowed_at + interval '90 seconds'
      from challenge_time
     where attempt.correlation_id = '$correlation'::uuid;
  " >/dev/null
  before="$(snapshot "$correlation" "$character")"

  (
    PGAPPNAME="$app" psql "${psql_args[@]}" --command="begin; $advisory select pg_sleep(4); rollback;"
  ) >/dev/null 2>&1 &
  holder_pid=$!

  for _ in $(seq 1 100); do
    marker="$(psql "${psql_args[@]}" --command="
      select exists (
        select 1
        from pg_locks lock
        join pg_stat_activity activity on activity.pid = lock.pid
        where lock.locktype = 'advisory'
          and lock.granted
          and activity.application_name = '$app'
      );
    " 2>/dev/null || true)"
    if [[ "$marker" == "t" ]]; then break; fi
    sleep 0.05
  done
  if [[ "${marker:-}" != "t" ]]; then
    echo "claim challenge lock-expiry contract could not confirm DB locker marker" >&2
    kill "$holder_pid" >/dev/null 2>&1 || true
    wait "$holder_pid" >/dev/null 2>&1 || true
    return 1
  fi

  # RED evidence: the transaction-start clock remains before the exact expiry
  # while wall-clock time is already after it.  The old now() implementation
  # would accept this predicate after the lock wait.
  (
    psql "${psql_args[@]}" --command="begin; $advisory select (now() < (select allow_expires_at from private.game_character_claim_attempts where correlation_id = '$correlation'::uuid) and clock_timestamp() > (select allow_expires_at from private.game_character_claim_attempts where correlation_id = '$correlation'::uuid)); rollback;"
  ) >"$red_log" 2>&1 &
  red_probe_pid=$!

  set +e
  psql "${psql_args[@]}" --command="
    begin;
    set local role service_role;
    do \$contract\$
    begin
      perform public.${rpc}(
        '$world', '$name', repeat('$fingerprint', 64),
        '$actor'::uuid, '$correlation'::uuid
      );
      raise exception using errcode = 'P0002', message = 'expired authorization unexpectedly succeeded';
    exception
      when sqlstate 'P0001' then null;
    end
    \$contract\$;
    commit;
  " >"$call_log" 2>&1
  call_exit=$?
  set -e

  wait "$red_probe_pid"
  wait "$holder_pid"
  if [[ "$call_exit" -ne 0 ]] || ! grep -qx 't' "$red_log"; then
    echo "claim challenge lock-expiry contract did not prove RED baseline and P0001 rejection" >&2
    return 1
  fi

  after="$(snapshot "$correlation" "$character")"
  if [[ "$before" != "$after" ]]; then
    echo "claim challenge lock-expiry contract observed fixture mutation" >&2
    return 1
  fi
}

run_expiry_probe \
  "contract-lock-expiry-challenge" "Lockchal" \
  "73000000-0000-0000-0000-000000000001" \
  "71000000-0000-0000-0000-000000000001" "a" \
  "challenge_legacy_game_character_onboarding" \
  "72000000-0000-0000-0000-000000000001"
run_expiry_probe \
  "contract-lock-expiry-final" "Lockfinal" \
  "73000000-0000-0000-0000-000000000002" \
  "71000000-0000-0000-0000-000000000002" "b" \
  "claim_legacy_game_character_onboarding" \
  "72000000-0000-0000-0000-000000000002"

echo "claim challenge lock-expiry contract passed (RED evidence: now() would accept; GREEN: clock_timestamp() rejects with P0001)"
