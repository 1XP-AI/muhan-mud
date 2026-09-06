import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import test from 'node:test'
import fixture from '../fixtures/admission_identity_conformance_v1.json' with { type: 'json' }
import { loadConfig } from '../../services/gateway/src/config.js'
import {
  EvidenceFinalizationError,
  GatewayEvidenceFinalizer,
  SupabaseEvidenceFinalizerTransport,
} from '../../services/gateway/src/evidence-finalizer.js'

const run = promisify(execFile)
const disposable = process.env.ADMISSION_IDENTITY_PG17_ALLOW_DISPOSABLE === '1'
const rejectedCorrelation = '123e4567-e89b-12d3-a456-426614174003'
const rejectedCharacter = '123e4567-e89b-12d3-a456-426614174004'
const rejectedWorld = 'muhan-rejected-evidence'

async function sql(query: string): Promise<string> {
  const container = process.env.STACK_E2E_PG_CONTAINER
  assert.ok(container, 'admission identity PG17 integration requires the runner-owned PostgreSQL container')
  const result = await run('docker', [
    'exec', container, 'psql', '-U', 'postgres', '-d', 'stack_e2e',
    '-At', '-v', 'ON_ERROR_STOP=1', '-c', query,
  ], { maxBuffer: 1024 * 1024 })
  return result.stdout.trim()
}

function evidence(sha256 = fixture.playerFileSha256) {
  return {
    outcome: 'ok' as const,
    canonicalization: 'canonical' as const,
    canonicalName: fixture.canonicalName,
    legacyShard: fixture.legacyShard,
    playerFileSha256: sha256,
    storageFormat: fixture.storageFormat,
  }
}

function finalizer(): GatewayEvidenceFinalizer {
  const restUrl = process.env.STACK_E2E_REST_URL
  const serviceRoleKey = process.env.STACK_E2E_SERVICE_ROLE_JWT
  assert.ok(restUrl && /^http:\/\/127\.0\.0\.1:[0-9]+$/.test(restUrl), 'admission identity PG17 integration requires runner-owned loopback PostgREST')
  assert.ok(serviceRoleKey, 'admission identity PG17 integration requires runner-owned service role credentials')
  const config = loadConfig({
    NODE_ENV: 'test',
    MUD_ONBOARDING_ENABLED: 'true',
    MUD_ENABLE_ONBOARDING_EVIDENCE: '1',
    SUPABASE_URL: 'http://127.0.0.1:9999',
    SUPABASE_INTERNAL_REST_URL: restUrl,
    SUPABASE_SERVICE_ROLE_KEY: serviceRoleKey,
    MUD_ADMISSION_SECRET: '0123456789abcdef0123456789abcdef',
    GATEWAY_INSTANCE_ID: 'admission-identity-pg17-contract',
    ALLOWED_ORIGINS: 'http://localhost:3000',
  })
  return new GatewayEvidenceFinalizer(new SupabaseEvidenceFinalizerTransport(config))
}

test('shared admission evidence reaches the real Gateway-to-RPC boundary on disposable PostgreSQL 17', {
  skip: disposable ? false : 'set ADMISSION_IDENTITY_PG17_ALLOW_DISPOSABLE=1 inside the disposable stack runner',
  timeout: 30_000,
}, async () => {
  // This preparation is intentionally superuser-only and runner-local. The
  // behavior under test below is the Gateway's production narrow transport,
  // which reaches PostgREST as service_role and cannot use table CRUD.
  await sql(`insert into auth.users (id) values ('${fixture.actorUserId}'::uuid) on conflict (id) do nothing;`)
  await sql(`insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, lifecycle,
    storage_format, imported_file_sha256
  ) values (
    '${fixture.characterId}'::uuid, '${fixture.worldId}', '${fixture.canonicalName}',
    '${fixture.canonicalName}', '${fixture.legacyShard}', 'imported_unclaimed', 1,
    '${fixture.playerFileSha256}'
  );`)
  await sql(`select status from public.begin_game_character_onboarding(
    '${fixture.actorUserId}'::uuid, '${fixture.correlationId}'::uuid, 'claim',
    clock_timestamp() + interval '10 minutes'
  );`)
  await sql(`select character_id from public.challenge_legacy_game_character_onboarding(
    '${fixture.worldId}', '${fixture.canonicalName}', '${fixture.playerFileSha256}',
    '${fixture.actorUserId}'::uuid, '${fixture.correlationId}'::uuid
  );`)

  const gatewayFinalizer = finalizer()
  const request = {
    actorUserId: fixture.actorUserId,
    correlationId: fixture.correlationId,
    characterId: fixture.characterId,
    mode: 'claim' as const,
    worldId: fixture.worldId,
    evidence: evidence(),
  }

  await gatewayFinalizer.finalize(request)
  assert.equal(await sql(`select lifecycle || '|' || owner_user_id || '|' || imported_file_sha256
    from public.game_characters where id = '${fixture.characterId}'::uuid;`),
  `active|${fixture.actorUserId}|${fixture.playerFileSha256}`,
  'accepted canonical evidence must bind the fixture actor and durable file fingerprint')
  assert.equal(await sql(`select canonical_legacy_name || '|' || legacy_shard || '|' || player_file_sha256
    from private.game_character_legacy_identity_evidence
    where correlation_id = '${fixture.correlationId}'::uuid;`),
  `${fixture.canonicalName}|${fixture.legacyShard}|${fixture.playerFileSha256}`,
  'the SQL ledger must contain exactly the shared canonical tuple')

  // The real HTTP transport must accept a finalizer replay after the lifecycle
  // is active, while a same-correlation tuple conflict remains fail closed.
  await gatewayFinalizer.finalize(request)
  await assert.rejects(
    () => gatewayFinalizer.finalize({ ...request, evidence: evidence(fixture.mismatchPlayerFileSha256) }),
    EvidenceFinalizationError,
  )
  assert.equal(await sql(`select lifecycle || '|' || (select player_file_sha256
    from private.game_character_legacy_identity_evidence
    where correlation_id = '${fixture.correlationId}'::uuid)
    from public.game_characters where id = '${fixture.characterId}'::uuid;`),
  `active|${fixture.playerFileSha256}`,
  'a rejected conflicting tuple must not mutate active ownership or immutable evidence')

  // Non-ok outcomes are rejected by the SQL RPC itself, not merely filtered by
  // the Gateway client. The runner invokes it as service_role and confirms no
  // lifecycle or ledger mutation is possible for rejected C evidence.
  await sql(`insert into public.game_characters (
    id, world_id, legacy_name, legacy_name_key, legacy_shard, owner_user_id, lifecycle
  ) values (
    '${rejectedCharacter}'::uuid, '${rejectedWorld}', '${fixture.canonicalName}',
    '${fixture.canonicalName}', '${fixture.legacyShard}', '${fixture.actorUserId}'::uuid,
    'provisioning'
  );
  insert into private.game_character_onboarding_intents (
    correlation_id, actor_user_id, mode, status, expires_at
  ) values (
    '${rejectedCorrelation}'::uuid, '${fixture.actorUserId}'::uuid, 'provision', 'provisioning',
    clock_timestamp() + interval '10 minutes'
  );
  insert into private.game_character_provisioning_requests (
    correlation_id, actor_user_id, character_id, world_id, legacy_name_key
  ) values (
    '${rejectedCorrelation}'::uuid, '${fixture.actorUserId}'::uuid, '${rejectedCharacter}'::uuid,
    '${rejectedWorld}', '${fixture.canonicalName}'
  );`)
  await sql(`begin;
    set local role service_role;
    do $$ begin
      perform public.finalize_game_character_legacy_identity_evidence(
        '${fixture.actorUserId}'::uuid, '${rejectedCorrelation}'::uuid, '${rejectedCharacter}'::uuid,
        'not_found', '${fixture.canonicalName}', '${fixture.playerFileSha256}', 1::smallint,
        '${fixture.storageFormat}', '${fixture.legacyShard}'
      );
      raise exception using errcode = 'P0002', message = 'rejected evidence unexpectedly finalized';
    exception when sqlstate '22023' then null;
    end $$;
    commit;`)
  assert.equal(await sql(`select c.lifecycle || '|' || i.status || '|' || (
    select count(*) from private.game_character_legacy_identity_evidence e
    where e.correlation_id = '${rejectedCorrelation}'::uuid
  ) from public.game_characters c
    join private.game_character_onboarding_intents i on i.correlation_id = '${rejectedCorrelation}'::uuid
    where c.id = '${rejectedCharacter}'::uuid;`),
  'provisioning|provisioning|0',
  'rejected evidence must leave the pending lifecycle and private ledger unchanged')
})
