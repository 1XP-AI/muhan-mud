#!/usr/bin/env bash
set -euo pipefail

[[ "${M3_PROVISIONING_HEAD_BASELINE_ALLOW_DISPOSABLE:-}" == 1 ]] || {
  echo "m3 provisioning head baseline PG17 integration skipped (set M3_PROVISIONING_HEAD_BASELINE_ALLOW_DISPOSABLE=1)"
  exit 0
}

command -v docker >/dev/null || {
  echo "m3 provisioning head baseline PG17 integration requires docker" >&2
  exit 2
}

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
container="m3-provisioning-head-baseline-${RANDOM}-${RANDOM}"

cleanup() {
  docker rm --force "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --detach --rm --name "$container" \
  --env POSTGRES_PASSWORD=contract-only-password \
  --tmpfs /var/lib/postgresql/data:rw,size=128m \
  --volume "$repo_root:/workspace:ro" \
  postgres:17-alpine >/dev/null

# Do not accept the entrypoint's short-lived initialization server as the
# final database.  Two consecutive real queries straddle that restart window
# and make the migration harness deterministic on shared CI runners.
ready_streak=0
for _ in $(seq 1 60); do
  if docker exec --env PGPASSWORD=contract-only-password "$container" \
    psql --host=127.0.0.1 --username=postgres --dbname=postgres \
      --no-psqlrc --tuples-only --no-align --command='select 1' \
      >/dev/null 2>&1; then
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
  echo "m3 provisioning head baseline PG17 integration did not reach stable readiness" >&2
  exit 2
fi

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
  20260910000000_m3_shadow_receipt_route_v2.sql \
  20260911000000_m3_writer_session.sql \
  20260912000000_m3_live_save_route.sql; do
  run_super --file="/workspace/supabase/migrations/$migration"
done

if run_super --file=/workspace/supabase/tests/m3_provisioning_head_baseline_contract.sql >/dev/null 2>&1; then
  echo "m3 provisioning baseline RED unexpectedly passed through migration 120" >&2
  exit 1
fi
echo "RED PostgreSQL 17: provisioning completion does not seed an M3 baseline through migration 120"

run_super --file=/workspace/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql
run_super --file=/workspace/supabase/tests/m3_provisioning_head_baseline_contract.sql

# Seed pre-130 finalized rows after the first application.  The second
# application must backfill only the missing head and preserve the advanced
# one byte-for-byte.
run_super --command="insert into auth.users(id) values ('d9300000-0000-0000-0000-000000000098'); insert into public.game_characters(id,world_id,legacy_name,legacy_name_key,legacy_shard,owner_user_id,lifecycle,storage_format,claimed_at) values ('c9300000-0000-0000-0000-000000000001'::uuid,'m3-provision-backfill','Bhero','Bhero',substr(encode(public.digest(convert_to('Bhero','UTF8'),'sha1'),'hex'),1,2),'d9300000-0000-0000-0000-000000000098'::uuid,'active',1,clock_timestamp()-interval '1 hour'), ('c9300000-0000-0000-0000-000000000002'::uuid,'m3-provision-backfill','Badvance','Badvance',substr(encode(public.digest(convert_to('Badvance','UTF8'),'sha1'),'hex'),1,2),'d9300000-0000-0000-0000-000000000098'::uuid,'active',1,clock_timestamp()-interval '1 hour'); insert into private.game_character_onboarding_intents(correlation_id,actor_user_id,mode,status,expires_at,completed_at) values ('e9300000-0000-0000-0000-000000000098'::uuid,'d9300000-0000-0000-0000-000000000098'::uuid,'provision','finalized',clock_timestamp()-interval '1 hour',clock_timestamp()-interval '1 hour'), ('e9300000-0000-0000-0000-000000000097'::uuid,'d9300000-0000-0000-0000-000000000098'::uuid,'provision','finalized',clock_timestamp()-interval '1 hour',clock_timestamp()-interval '1 hour'); insert into private.game_character_provisioning_requests(correlation_id,actor_user_id,character_id,world_id,legacy_name_key,status,saved_file_sha256,storage_format,finalized_at,finalized_via) values ('e9300000-0000-0000-0000-000000000098'::uuid,'d9300000-0000-0000-0000-000000000098'::uuid,'c9300000-0000-0000-0000-000000000001'::uuid,'m3-provision-backfill','Bhero','finalized',repeat('9',64),1,clock_timestamp()-interval '1 hour','finalize'), ('e9300000-0000-0000-0000-000000000097'::uuid,'d9300000-0000-0000-0000-000000000098'::uuid,'c9300000-0000-0000-0000-000000000002'::uuid,'m3-provision-backfill','Badvance','finalized',repeat('8',64),1,clock_timestamp()-interval '1 hour','reconcile'); insert into private.game_character_legacy_heads(character_id,head_state,head_sha256,storage_format,revision,writer_epoch) values ('c9300000-0000-0000-0000-000000000002'::uuid,'existing',repeat('e',64),1,4,1);" >/dev/null
run_super --file=/workspace/supabase/migrations/20260913000000_m3_provisioning_head_baseline.sql
[[ "$(run_super --tuples-only --no-align --command="select (select head_state='existing' and head_sha256=repeat('9',64) and storage_format=1 and revision=0 and writer_epoch is null from private.game_character_legacy_heads where character_id='c9300000-0000-0000-0000-000000000001'::uuid) and (select head_state='existing' and head_sha256=repeat('e',64) and storage_format=1 and revision=4 and writer_epoch=1 from private.game_character_legacy_heads where character_id='c9300000-0000-0000-0000-000000000002'::uuid);")" == t ]] || {
  echo "migration replay did not backfill only the missing finalized baseline" >&2
  exit 1
}

# Prove a real M3 receipt can advance one provisioned baseline from revision
# 0 to revision 1.
run_super --command="begin; insert into auth.users(id) values ('d9300000-0000-0000-0000-000000000099'); select * from public.begin_game_character_onboarding('d9300000-0000-0000-0000-000000000099'::uuid, 'e9300000-0000-0000-0000-000000000099'::uuid, 'provision', clock_timestamp()+interval '10 minutes'); select * from public.begin_game_character_provisioning('d9300000-0000-0000-0000-000000000099'::uuid, 'e9300000-0000-0000-0000-000000000099'::uuid, 'm3-provision-replay', 'ReplayHero'); select * from public.finalize_game_character_provisioning('d9300000-0000-0000-0000-000000000099'::uuid, 'e9300000-0000-0000-0000-000000000099'::uuid, repeat('a',64), 1::smallint); commit;" >/dev/null
character="$(run_super --tuples-only --no-align --command="select id from public.game_characters where world_id='m3-provision-replay' and legacy_name_key='Replayhero';")"
world='m3-provision-replay'
writer='f9300000-0000-0000-0000-000000000001'
run_super --command="select private.acquire_game_world_writer_epoch('$world','$writer'::uuid,clock_timestamp()+interval '3 minutes'); select private.record_legacy_published_receipt('$world','Replayhero','$character'::uuid,'f9300000-0000-0000-0000-000000000002'::uuid,'$writer'::uuid,private.game_character_shadow_request_sha256('$world','$character'::uuid,'Replayhero',substr(encode(public.digest(convert_to('Replayhero','UTF8'),'sha1'),'hex'),1,2),'f9300000-0000-0000-0000-000000000002'::uuid,'$writer'::uuid,1::bigint,1::bigint,'existing',repeat('a',64),repeat('b',64),1::smallint),1::bigint,1::bigint,'existing',repeat('a',64),repeat('b',64),1::smallint);" >/dev/null
[[ "$(run_super --tuples-only --no-align --command="select head_state='existing' and head_sha256=repeat('b',64) and storage_format=1 and revision=1 and writer_epoch=1 from private.game_character_legacy_heads where character_id='$character'::uuid;")" == t ]] || {
  echo "real receipt did not advance provisioned baseline from revision 0 to revision 1" >&2
  exit 1
}

echo "GREEN PostgreSQL 17: finalize/reconcile baseline, conflict atomicity, replay, v3 revision 0, and receipt revision 1 passed"
