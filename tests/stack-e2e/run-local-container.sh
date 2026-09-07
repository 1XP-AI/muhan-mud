#!/usr/bin/env bash
set -Eeuo pipefail
[[ "${STACK_E2E_LOCAL_DISPOSABLE:-}" == 1 ]] || exit 2
[[ "$(id -u)" == 10001 ]] || { echo 'local-stack: production-equivalent UID 10001 required' >&2; exit 2; }
[[ ! -S /var/run/docker.sock ]] || exit 2
export STACK_E2E_ROOT=/repo STACK_E2E_FIXTURE=/tmp/stack-fixture
export STACK_E2E_PG_PASSWORD=stack-e2e-postgres-password
export PGPASSWORD="$STACK_E2E_PG_PASSWORD" PGHOST=127.0.0.1 PGPORT=5432 PGUSER=postgres PGDATABASE=stack_e2e
for attempt in $(seq 1 60); do
  if pg_isready >/dev/null 2>&1; then break; fi
  sleep 1
done
psql -X -v ON_ERROR_STOP=1 -f /repo/supabase/tests/bootstrap_contract.sql >/dev/null
psql -X -v ON_ERROR_STOP=1 -f /repo/supabase/tests/bootstrap_auth_uid_contract.sql >/dev/null
for migration in /repo/supabase/migrations/*.sql; do
  [[ "${migration##*/}" == 20260901000000_profiles_and_lobby_presence.sql ]] && continue
  psql -X -v ON_ERROR_STOP=1 -f "$migration" >/dev/null
done
psql -X -v ON_ERROR_STOP=1 -c "alter role mud_writer_login password 'stack-e2e-m3-writer-password'; alter role mud_normalized_replay_reader_login password 'stack-e2e-reader-password';" >/dev/null
export STACK_E2E_REST_URL=http://127.0.0.1:3000
for attempt in $(seq 1 60); do
  if curl --max-time 2 -fsS "$STACK_E2E_REST_URL/" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl --max-time 2 -fsS "$STACK_E2E_REST_URL/" >/dev/null
export STACK_E2E_M3_ENABLED=1 ADMISSION_IDENTITY_PG17_ALLOW_DISPOSABLE=1
export STACK_E2E_BINARY=/usr/local/bin/muhan-stack-frp
export STACK_E2E_NORMALIZED_PROJECTOR=/repo/rust/target/release/player_snapshot_v1_normalized_project
export STACK_E2E_NORMALIZED_READER_URL='postgresql://mud_normalized_replay_reader_login:stack-e2e-reader-password@127.0.0.1:5432/stack_e2e?sslmode=disable'
export STACK_E2E_M3_WRITER_PASSWORD=stack-e2e-m3-writer-password
export STACK_E2E_M3_DATABASE_URL='postgresql://mud_writer_login:stack-e2e-m3-writer-password@127.0.0.1:5432/stack_e2e?sslmode=disable'
export STACK_E2E_M3_CONNINFO_FILE=/tmp/m3-writer.conninfo
umask 077
printf '%s' "$STACK_E2E_M3_DATABASE_URL" > "$STACK_E2E_M3_CONNINFO_FILE"
export STACK_E2E_DATABASE_URL='postgres://postgres:stack-e2e-postgres-password@127.0.0.1:5432/stack_e2e'
export STACK_E2E_JWT_SECRET=stack-e2e-jwt-secret-not-for-production
export STACK_E2E_SERVICE_ROLE_JWT="$(node -e 'const c=require("node:crypto");const b=x=>Buffer.from(JSON.stringify(x)).toString("base64url");const h=b({alg:"HS256",typ:"JWT"})+"."+b({role:"service_role",aud:"authenticated",exp:4102444800});process.stdout.write(h+"."+c.createHmac("sha256",process.env.STACK_E2E_JWT_SECRET).update(h).digest("base64url"))')"
export STACK_E2E_ARTIFACT=/tmp/stack-result.json
# PostgREST starts alongside PostgreSQL, before migrations. Refresh its cache
# after the complete schema exists; readiness requires the final RPC surface.
psql -X -v ON_ERROR_STOP=1 -c "NOTIFY pgrst, 'reload schema';" >/dev/null
schema_ready=0
for attempt in $(seq 1 60); do
  if curl --max-time 2 -fsS -H "Authorization: Bearer $STACK_E2E_SERVICE_ROLE_JWT" "$STACK_E2E_REST_URL/" | node -e 'let s="";process.stdin.on("data",x=>s+=x);process.stdin.on("end",()=>{try{process.exit(JSON.parse(s).paths["/rpc/begin_game_character_onboarding"]?0:1)}catch{process.exit(1)}})'; then schema_ready=1; break; fi
  sleep 1
done
[[ "$schema_ready" == 1 ]] || { echo 'local-stack: PostgREST schema readiness failed' >&2; exit 1; }
exec pnpm --dir /repo/services/gateway exec tsx --test /repo/tests/stack-e2e/local-onboarding-evidence.test.ts /repo/tests/stack-e2e/post-game-claim-check.test.ts /repo/tests/stack-e2e/admission-identity-pg17.integration.test.ts /repo/tests/stack-e2e/stack-e2e.test.ts
