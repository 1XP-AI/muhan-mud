#!/usr/bin/env bash
set -euo pipefail

# This is deliberately a CI-service adapter check, not a disposable database
# harness. The workflow's isolated legacy-identity evidence stage has already
# applied the ordered migrations through 20261005000000 to this database.
[[ "${CI:-}" == true && "${BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ALLOW_CI_E2E:-}" == 1 ]] || {
  echo "BankSnapshotV1 topology adapter PG17 E2E skipped (CI=true and BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_ALLOW_CI_E2E=1 required)"
  exit 0
}
for required in psql node; do
  command -v "$required" >/dev/null || {
    echo "BankSnapshotV1 topology adapter PG17 E2E requires $required" >&2
    exit 2
  }
done

: "${BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_DATABASE_URL:?missing writer database URL}"
: "${BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_SUPER_DATABASE_URL:?missing superuser database URL}"

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
run_super() {
  psql "$BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_SUPER_DATABASE_URL" \
    --no-psqlrc --quiet --set=ON_ERROR_STOP=1 "$@"
}

run_super <<'SQL'
insert into public.game_characters (id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle, storage_format)
values ('a9520000-0000-0000-0000-000000000001', 'bank-relay-e2e', 'E2ehero', 'E2ehero',
  substr(encode(public.digest(convert_to('E2ehero', 'UTF8'), 'sha1'), 'hex'), 1, 2), 'imported_unclaimed', 1);
insert into private.game_character_legacy_heads (character_id, head_state, storage_format, revision)
values ('a9520000-0000-0000-0000-000000000001', 'absent', 1, 0);
select * from private.acquire_game_world_writer_epoch(
  'bank-relay-e2e', 'b9520000-0000-0000-0000-000000000001'::uuid, clock_timestamp() + interval '3 minutes');
select private.game_character_shadow_request_sha256(
  'bank-relay-e2e', 'a9520000-0000-0000-0000-000000000001'::uuid, 'E2ehero',
  substr(encode(public.digest(convert_to('E2ehero', 'UTF8'), 'sha1'), 'hex'), 1, 2),
  'c9520000-0000-0000-0000-000000000001'::uuid, 'b9520000-0000-0000-0000-000000000001'::uuid,
  1::bigint, 1::bigint, 'absent', null, repeat('a', 64), 1::smallint) as request_sha256 \gset bank_e2e_
select private.record_legacy_published_receipt(
  'bank-relay-e2e', 'E2ehero', 'a9520000-0000-0000-0000-000000000001'::uuid,
  'c9520000-0000-0000-0000-000000000001'::uuid, 'b9520000-0000-0000-0000-000000000001'::uuid,
  :'bank_e2e_request_sha256', 1::bigint, 1::bigint, 'absent', null, repeat('a', 64), 1::smallint);
insert into private.game_character_m4_file_snapshot_manifests (
  character_id, command_id, world_id, legacy_name_key, receipt_request_sha256,
  writer_instance_id, writer_epoch, writer_revision, file_post_sha256, storage_format,
  receipt_acknowledged_at, snapshot_format, snapshot_sha256, snapshot_octets
)
select character_id, command_id, world_id, legacy_name_key, request_sha256,
  writer_instance_id, writer_epoch, writer_revision, post_sha256, storage_format,
  acknowledged_at, 'legacy-file-manifest-v1', post_sha256, 9
from private.game_character_shadow_receipts
where command_id = 'c9520000-0000-0000-0000-000000000001'::uuid;
SQL

request_sha256="$(run_super --tuples-only --no-align --command="select request_sha256 from private.game_character_shadow_receipts where command_id = 'c9520000-0000-0000-0000-000000000001'::uuid")"
[[ "$request_sha256" =~ ^[0-9a-f]{64}$ ]] || {
  echo "BankSnapshotV1 topology adapter PG17 E2E setup failed" >&2
  exit 2
}

cd "$repo_root/services/m4-file-snapshot-manifest-relay"
BANK_SNAPSHOT_V1_TOPOLOGY_SHADOW_E2E_REQUEST_SHA256="$request_sha256" \
  node node_modules/tsx/dist/cli.mjs test/bank-snapshot-v1-topology-shadow-pg17-e2e.ts
