import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmod, mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import test from 'node:test'
import {
  OnboardingReconciler,
  NodeReconcilerFilesystem,
  PostgresOnboardingSnapshotEligibilityFulfillmentRpc,
  PostgresPendingOnboardingSnapshotEligibilitySource,
  type Clock,
  type PendingEligibilityPgClient,
  type PendingEligibilityPgPool,
  type ReconcilerFilesystem,
  type RunSummary,
  fulfillPendingOnboardingSnapshotEligibilityOnce,
  runPolling,
} from '../src/reconciler.js'
import { main } from '../src/cli.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const correlation = '123e4567-e89b-12d3-a456-426614174001'
const character = '123e4567-e89b-12d3-a456-426614174002'
const fileBody = Buffer.from('legacy player bytes\n', 'utf8')
const fileHash = createHash('sha256').update(fileBody).digest('hex')

function receipt(state: 'pending' | 'saved' | 'committed', overrides: Partial<Record<string, string>> = {}): string {
  const values: Record<string, string> = {
    version: '1',
    state,
    actor_uuid: actor,
    correlation_uuid: correlation,
    character_uuid: character,
    canonical_name_hex: Buffer.from('Alice', 'utf8').toString('hex'),
    storage_format: 'player-v1',
    saved_file_sha256: state === 'pending' ? '' : fileHash,
    ...overrides,
  }
  return ['version', 'state', 'actor_uuid', 'correlation_uuid', 'character_uuid', 'canonical_name_hex', 'storage_format', 'saved_file_sha256']
    .map((name) => `${name}=${values[name] ?? ''}`).join('\n') + '\n'
}

function rpcResponse(overrides: Record<string, unknown> = {}): Response {
  return Response.json([{
    character_id: character,
    actor_user_id: actor,
    lifecycle: 'handoff_pending',
    status: 'finalized',
    saved_file_sha256: fileHash,
    storage_format: 1,
    ...overrides,
  }])
}

function evidenceFinalizerResponse(mode: 'provision' | 'claim', lifecycle: 'handoff_pending' | 'active', overrides: Record<string, unknown> = {}): Response {
  return Response.json([{
    character_id: character,
    actor_user_id: actor,
    mode,
    lifecycle,
    world_id: 'muhan',
    canonical_legacy_name: 'Alice',
    legacy_shard: createHash('sha1').update('Alice').digest('hex').slice(0, 2),
    player_file_sha256: fileHash,
    evidence_version: 1,
    storage_format: 'player-v1',
    recorded_at: '2026-09-05T00:00:00.000Z',
    ...overrides,
  }])
}

function activationResponse(overrides: Record<string, unknown> = {}): Response {
  return Response.json([{
    character_id: character,
    actor_user_id: actor,
    correlation_id: correlation,
    lifecycle: 'active',
    onboarding_status: 'finalized',
    ...overrides,
  }])
}

interface Fixture {
  home: string
  receiptPath: string
  playerPath: string
  cleanup(): Promise<void>
}

async function fixture(contents: string = receipt('saved')): Promise<Fixture> {
  const home = await mkdtemp(join(process.cwd(), '.muhan-reconciler-'))
  const receiptDirectory = join(home, 'onboarding-receipts')
  const shard = createHash('sha1').update('Alice').digest('hex').slice(0, 2)
  const playerDirectory = join(home, 'player', shard)
  await mkdir(receiptDirectory, { recursive: true })
  await mkdir(playerDirectory, { recursive: true })
  const receiptPath = join(receiptDirectory, `${correlation}.receipt`)
  const playerPath = join(playerDirectory, 'Alice')
  await writeFile(receiptPath, contents)
  await writeFile(playerPath, fileBody)
  await chmod(home, 0o700)
  await chmod(receiptDirectory, 0o700)
  await chmod(join(home, 'player'), 0o700)
  await chmod(playerDirectory, 0o700)
  await chmod(receiptPath, 0o600)
  await chmod(playerPath, 0o600)
  return { home, receiptPath, playerPath, cleanup: () => rm(home, { recursive: true, force: true }) }
}

function reconciler(home: string, fetchImpl: typeof fetch, options: Partial<ConstructorParameters<typeof OnboardingReconciler>[0]> = {}) {
  return new OnboardingReconciler({
    muhanHome: home,
    postgrestUrl: 'http://postgrest.internal:3000',
    serviceRoleKey: 'test-service-key',
    fetchImpl,
    rpcAttempts: 2,
    ...options,
  })
}

function statuses(summary: RunSummary): string[] { return summary.observations.map(({ outcome }) => outcome) }

test('pending snapshot-eligibility fulfillment passes only each exact pre-bound character and command tuple to the existing RPC', async () => {
  const calls: Array<readonly [string, string]> = []
  const result = await fulfillPendingOnboardingSnapshotEligibilityOnce({
    listPendingOnboardingSnapshotEligibility: async (limit) => {
      assert.equal(limit, 2)
      return [
        { correlationId: correlation, actorUserId: actor, characterId: character, mode: 'claim', commandId: '123e4567-e89b-12d3-a456-426614174003' },
        { correlationId: '123e4567-e89b-12d3-a456-426614174004', actorUserId: actor, characterId: '123e4567-e89b-12d3-a456-426614174005', mode: 'provision', commandId: '123e4567-e89b-12d3-a456-426614174006' },
      ]
    },
  }, {
    fulfillGameCharacterOnboardingSnapshotEligibility: async (characterId, commandId) => {
      calls.push([characterId, commandId])
      return commandId.endsWith('003') ? 'FULFILLED' : 'EXACT_RETRY'
    },
  }, { limit: 2, attempts: 1 })
  assert.deepEqual(calls, [
    [character, '123e4567-e89b-12d3-a456-426614174003'],
    ['123e4567-e89b-12d3-a456-426614174005', '123e4567-e89b-12d3-a456-426614174006'],
  ])
  assert.deepEqual(result, { listed: 2, fulfilled: 1, exactRetry: 1, notEligible: 0, rejected: 0, retryExhausted: 0 })
})

test('pending snapshot-eligibility fulfillment preserves exact retry idempotency', async () => {
  const result = await fulfillPendingOnboardingSnapshotEligibilityOnce({
    listPendingOnboardingSnapshotEligibility: async () => [
      { correlationId: correlation, actorUserId: actor, characterId: character, mode: 'claim', commandId: '123e4567-e89b-12d3-a456-426614174003' },
    ],
  }, {
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => 'EXACT_RETRY',
  }, { limit: 1, attempts: 1 })
  assert.deepEqual(result, { listed: 1, fulfilled: 0, exactRetry: 1, notEligible: 0, rejected: 0, retryExhausted: 0 })
})

test('pending snapshot-eligibility fulfillment fails closed before invoking fulfillment for a malformed or duplicate pre-bound tuple', async () => {
  const calls: string[] = []
  const result = await fulfillPendingOnboardingSnapshotEligibilityOnce({
    listPendingOnboardingSnapshotEligibility: async () => [
      { correlationId: correlation, actorUserId: actor, characterId: character, mode: 'claim', commandId: '123e4567-e89b-12d3-a456-426614174003' },
      { correlationId: correlation, actorUserId: actor, characterId: character, mode: 'claim', commandId: '123e4567-e89b-12d3-a456-426614174004' },
      { correlationId: '123e4567-e89b-12d3-a456-426614174005', actorUserId: actor, characterId: character, mode: 'unexpected', commandId: '123e4567-e89b-12d3-a456-426614174006' },
    ],
  }, {
    fulfillGameCharacterOnboardingSnapshotEligibility: async () => { calls.push('called'); return 'FULFILLED' },
  }, { limit: 3, attempts: 1 })
  assert.deepEqual(calls, [])
  assert.deepEqual(result, { listed: 3, fulfilled: 0, exactRetry: 0, notEligible: 0, rejected: 3, retryExhausted: 0 })
})

test('pending snapshot-eligibility fulfillment retries only bounded transient failures with the unchanged exact tuple', async () => {
  const calls: Array<readonly [string, string]> = []
  const delays: number[] = []
  const clock: Clock = { now: () => 0, sleep: async (milliseconds) => { delays.push(milliseconds) } }
  const result = await fulfillPendingOnboardingSnapshotEligibilityOnce({
    listPendingOnboardingSnapshotEligibility: async () => [
      { correlationId: correlation, actorUserId: actor, characterId: character, mode: 'claim', commandId: '123e4567-e89b-12d3-a456-426614174003' },
    ],
  }, {
    fulfillGameCharacterOnboardingSnapshotEligibility: async (characterId, commandId) => {
      calls.push([characterId, commandId])
      if (calls.length < 3) throw new Error('temporary database transport failure')
      return 'FULFILLED'
    },
  }, { limit: 1, attempts: 3, retryDelayMs: 7, clock })
  assert.deepEqual(calls, [
    [character, '123e4567-e89b-12d3-a456-426614174003'],
    [character, '123e4567-e89b-12d3-a456-426614174003'],
    [character, '123e4567-e89b-12d3-a456-426614174003'],
  ])
  assert.deepEqual(delays, [7, 7])
  assert.deepEqual(result, { listed: 1, fulfilled: 1, exactRetry: 0, notEligible: 0, rejected: 0, retryExhausted: 0 })
})

test('pending snapshot-eligibility adapters call only the bounded direct-login list and existing exact writer fulfillment RPC', async () => {
  const queries: Array<{ sql: string, values?: readonly unknown[] }> = []
  const client: PendingEligibilityPgClient = {
    query: async <Row>(sql: string, values?: readonly unknown[]) => {
      queries.push({ sql, values })
      if (sql.startsWith('select correlation_id')) return { rows: [{
        correlation_id: correlation, actor_user_id: actor, character_id: character,
        mode: 'claim', command_id: '123e4567-e89b-12d3-a456-426614174003',
      }] as Row[] }
      if (sql.startsWith('select outcome')) return { rows: [{ outcome: 'FULFILLED' }] as Row[] }
      return { rows: [] }
    },
    release: () => undefined,
  }
  const pool: PendingEligibilityPgPool = { connect: async () => client, end: async () => undefined }
  const source = new PostgresPendingOnboardingSnapshotEligibilitySource('postgresql://onboarding_snapshot_eligibility_login@localhost/postgres', pool)
  const fulfillment = new PostgresOnboardingSnapshotEligibilityFulfillmentRpc('postgresql://mud_writer_login@localhost/postgres', pool)
  assert.deepEqual(await source.listPendingOnboardingSnapshotEligibility(4), [{
    correlationId: correlation, actorUserId: actor, characterId: character,
    mode: 'claim', commandId: '123e4567-e89b-12d3-a456-426614174003',
  }])
  assert.equal(await fulfillment.fulfillGameCharacterOnboardingSnapshotEligibility(character, '123e4567-e89b-12d3-a456-426614174003'), 'FULFILLED')
  assert.deepEqual(queries, [
    { sql: 'select correlation_id, actor_user_id, character_id, mode, command_id from private.list_pending_game_character_onboarding_snapshot_eligibility($1::integer)', values: [4] },
    { sql: 'set role mud_writer', values: undefined },
    { sql: 'select outcome from private.fulfill_game_character_onboarding_snapshot_eligibility($1::uuid, $2::uuid)', values: [character, '123e4567-e89b-12d3-a456-426614174003'] },
  ])
})

test('pending snapshot-eligibility list adapter rejects service, writer, and option-bearing database URLs', () => {
  const rejected = [
    'postgresql://service_role@localhost/postgres',
    'postgresql://mud_writer_login@localhost/postgres',
    'postgresql://onboarding_snapshot_eligibility_login@localhost/postgres?options=-c%20role%3Dservice_role',
  ]
  for (const url of rejected) {
    assert.throws(() => new PostgresPendingOnboardingSnapshotEligibilitySource(url), /reconciler configuration rejected/)
  }
})

test('pending is observed without a database mutation', async (t) => {
  const data = await fixture(receipt('pending'))
  t.after(data.cleanup)
  let calls = 0
  const result = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.deepEqual(statuses(result), ['pending'])
  assert.equal(calls, 0)
})

test('saved reconciliation calls the exact service RPC once per exact receipt/player identity', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  const calls: Array<{ url: URL, init?: RequestInit }> = []
  const fetchImpl: typeof fetch = async (url, init) => {
    calls.push({ url: new URL(url), init })
    const request = JSON.parse(String(init?.body)) as { p_actor_user_id: string }
    return rpcResponse({ actor_user_id: request.p_actor_user_id })
  }
  const service = reconciler(data.home, fetchImpl)
  assert.deepEqual(statuses(await service.runOnce()), ['reconciled'])
  assert.deepEqual(statuses(await service.runOnce()), ['reconciled'])
  assert.equal(calls.length, 1)
  // A changed player hash is checked before the cache. Its rejected scan also
  // evicts the prior success so restoring the file is reconciled once again.
  await writeFile(data.playerPath, 'changed player bytes\n')
  await chmod(data.playerPath, 0o600)
  assert.deepEqual(statuses(await service.runOnce()), ['rejected'])
  assert.equal(calls.length, 1)
  await writeFile(data.playerPath, fileBody)
  await chmod(data.playerPath, 0o600)
  assert.deepEqual(statuses(await service.runOnce()), ['reconciled'])
  assert.equal(calls.length, 2)
  // A same-inode content change is a new receipt and must not be hidden by the
  // in-process success cache.
  const otherActor = '123e4567-e89b-12d3-a456-426614174003'
  await writeFile(data.receiptPath, receipt('saved', { actor_uuid: otherActor }))
  assert.deepEqual(statuses(await service.runOnce()), ['reconciled'])
  assert.equal(calls.length, 3)
  // A replacement with byte-identical receipt content is also a new receipt
  // inode and must revalidate through the idempotent RPC.
  await rm(data.receiptPath)
  await writeFile(data.receiptPath, receipt('saved', { actor_uuid: otherActor }))
  await chmod(data.receiptPath, 0o600)
  assert.deepEqual(statuses(await service.runOnce()), ['reconciled'])
  assert.equal(calls.length, 4)
  assert.deepEqual(JSON.parse(String(calls[0]?.init?.body)), {
    p_actor_user_id: actor,
    p_correlation_id: correlation,
    p_saved_file_sha256: fileHash,
    p_storage_format: 1,
  })
  assert.equal(calls[0]?.url.pathname, '/rpc/reconcile_game_character_provisioning')
  assert.equal((calls[0]?.init?.headers as Record<string, string>).authorization, 'Bearer test-service-key')
})

test('opt-in saved-receipt recovery finalizes evidence before activation and forwards the returned mode across handoff retry states', async (t) => {
  for (const mode of ['provision', 'claim'] as const) {
    const data = await fixture()
    t.after(data.cleanup)
    const calls: Array<{ path: string, body: Record<string, unknown> }> = []
    let finalizerAttempt = 0
    let activationAttempt = 0
    const result = await reconciler(data.home, async (url, init) => {
      const path = new URL(url).pathname
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>
      calls.push({ path, body })
      assert.notEqual(path, '/rpc/reconcile_game_character_provisioning', 'feature-on recovery must not reconcile before evidence')
      if (path === '/rpc/finalize_game_character_legacy_identity_evidence') {
        finalizerAttempt++
        assert.deepEqual(body, {
          p_actor_user_id: actor,
          p_correlation_id: correlation,
          p_character_id: character,
          p_outcome: 'ok',
          p_canonical_legacy_name: 'Alice',
          p_player_file_sha256: fileHash,
          p_evidence_version: 1,
          p_storage_format: 'player-v1',
          p_legacy_shard: createHash('sha1').update('Alice').digest('hex').slice(0, 2),
        })
        return evidenceFinalizerResponse(mode, finalizerAttempt === 1 ? 'handoff_pending' : 'active')
      }
      if (path === '/rpc/activate_game_character_onboarding_handoff') {
        activationAttempt++
        assert.deepEqual(body, {
          p_actor_user_id: actor,
          p_correlation_id: correlation,
          p_character_id: character,
          p_mode: mode,
        })
        // Simulate an ambiguous first activation attempt: a complete retry
        // must preserve the same tuple and re-enter through evidence first.
        return activationAttempt === 1 ? new Response('', { status: 503 }) : activationResponse()
      }
      throw new Error(`unexpected RPC ${path}`)
    }, { recoverSavedReceiptHandoffs: true }).runOnce()
    assert.deepEqual(statuses(result), ['reconciled'])
    assert.equal(finalizerAttempt, 2)
    assert.equal(activationAttempt, 2)
    assert.deepEqual(calls.map(({ path }) => path), [
      '/rpc/finalize_game_character_legacy_identity_evidence',
      '/rpc/activate_game_character_onboarding_handoff',
      '/rpc/finalize_game_character_legacy_identity_evidence',
      '/rpc/activate_game_character_onboarding_handoff',
    ])
  }
})

test('opt-in recovery fails closed on evidence or activation binding mismatches, while committed and feature-off receipts do not use the handoff path', async (t) => {
  const mismatch = await fixture()
  t.after(mismatch.cleanup)
  let activationCalls = 0
  const mismatched = await reconciler(mismatch.home, async (url) => {
    const path = new URL(url).pathname
    if (path === '/rpc/finalize_game_character_legacy_identity_evidence') return evidenceFinalizerResponse('claim', 'handoff_pending', { actor_user_id: '123e4567-e89b-12d3-a456-426614174099' })
    if (path === '/rpc/activate_game_character_onboarding_handoff') activationCalls++
    throw new Error('unexpected RPC after mismatch')
  }, { recoverSavedReceiptHandoffs: true }).runOnce()
  assert.deepEqual(mismatched.observations, [{ outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: 1 }])
  assert.equal(activationCalls, 0)

  const activationMismatch = await fixture()
  t.after(activationMismatch.cleanup)
  const activationMismatched = await reconciler(activationMismatch.home, async (url) => {
    const path = new URL(url).pathname
    if (path === '/rpc/finalize_game_character_legacy_identity_evidence') return evidenceFinalizerResponse('provision', 'handoff_pending')
    if (path === '/rpc/activate_game_character_onboarding_handoff') return activationResponse({ correlation_id: '123e4567-e89b-12d3-a456-426614174099' })
    throw new Error(`unexpected RPC ${path}`)
  }, { recoverSavedReceiptHandoffs: true }).runOnce()
  assert.deepEqual(activationMismatched.observations, [{ outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: 1 }])

  const committed = await fixture(receipt('committed'))
  t.after(committed.cleanup)
  let committedCalls = 0
  assert.deepEqual(statuses(await reconciler(committed.home, async () => { committedCalls++; return rpcResponse() }, { recoverSavedReceiptHandoffs: true }).runOnce()), ['committed'])
  assert.equal(committedCalls, 0)

  const off = await fixture()
  t.after(off.cleanup)
  const offPaths: string[] = []
  let offActivationCalls = 0
  assert.deepEqual(statuses(await reconciler(off.home, async (url) => {
    const path = new URL(url).pathname
    offPaths.push(path)
    if (path === '/rpc/activate_game_character_onboarding_handoff') offActivationCalls++
    return rpcResponse()
  }).runOnce()), ['reconciled'])
  assert.deepEqual(offPaths, ['/rpc/reconcile_game_character_provisioning'])
  assert.equal(offActivationCalls, 0)
})

test('legacy saved-receipt reconciliation accepts the exact finalized handoff-pending lifecycle tuple', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  const result = await reconciler(data.home, async () => {
    calls++
    return rpcResponse()
  }).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'reconciled', attempts: 1 }])
  assert.equal(calls, 1)
})

test('legacy saved-receipt reconciliation rejects a finalized active lifecycle tuple', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  const result = await reconciler(data.home, async () => {
    calls++
    return rpcResponse({ lifecycle: 'active' })
  }).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: 1 }])
  assert.equal(calls, 1)
})

test('the injected reconciliation response matcher permits only the exact post-save handoff-pending contract', async () => {
  const fs: ReconcilerFilesystem = {
    assertSafeDirectory: async () => {},
    listReceiptEntries: async () => [{ name: `${correlation}.receipt`, kind: 'file' }],
    readSmallRegularFile: async () => {
      const bytes = Buffer.from(receipt('saved'))
      return { bytes, sha256: createHash('sha256').update(bytes).digest('hex'), byteLength: bytes.length, device: 1, inode: 1, modifiedAtMs: 1, changedAtMs: 1 }
    },
    sha256RegularFile: async () => ({ sha256: fileHash, byteLength: fileBody.length, device: 2, inode: 2, modifiedAtMs: 1, changedAtMs: 1 }),
  }
  const responseCases: Array<{ label: string, response: Response, expected: RunSummary['observations'] }> = [
    { label: 'the exact post-save contract', response: rpcResponse({ lifecycle: 'handoff_pending' }), expected: [{ outcome: 'reconciled', attempts: 1 }] },
    { label: 'an active playable response', response: rpcResponse({ lifecycle: 'active' }), expected: [{ outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: 1 }] },
    { label: 'a provisioning response', response: rpcResponse({ lifecycle: 'handoff_pending', status: 'provisioning' }), expected: [{ outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: 1 }] },
  ]
  for (const { label, response, expected } of responseCases) {
    const result = await reconciler('/isolated-home', async () => response, { fs, rpcAttempts: 1 }).runOnce()
    assert.deepEqual(result.observations, expected, label)
  }

  const indeterminate = await reconciler('/isolated-home', async () => { throw new DOMException('request timed out', 'AbortError') }, { fs, rpcAttempts: 1 }).runOnce()
  assert.deepEqual(indeterminate.observations, [{ outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: 1 }])
})

test('committed receipts are fully validated but never invoke PostgREST', async (t) => {
  const data = await fixture(receipt('committed'))
  t.after(data.cleanup)
  let calls = 0
  const result = await reconciler(data.home, async () => {
    calls++
    return rpcResponse()
  }).runOnce()
  assert.deepEqual(statuses(result), ['committed'])
  assert.equal(calls, 0)
})

test('a player hash mismatch makes no RPC and never rewrites the receipt', async (t) => {
  const data = await fixture(receipt('saved', { saved_file_sha256: 'a'.repeat(64) }))
  t.after(data.cleanup)
  const before = await readFile(data.receiptPath)
  let calls = 0
  const result = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.deepEqual(statuses(result), ['rejected'])
  assert.equal(result.observations[0]?.reason, 'player_hash_mismatch')
  assert.equal(calls, 0)
  assert.deepEqual(await readFile(data.receiptPath), before)
})

test('symlinks, traversal names, malformed fields, and oversize receipts fail closed', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  await rm(data.receiptPath)
  await symlink(data.playerPath, data.receiptPath)
  let calls = 0
  const first = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.deepEqual(statuses(first), ['rejected'])
  assert.equal(first.observations[0]?.reason, 'unsafe_receipt_file')
  assert.equal(calls, 0)

  await rm(data.receiptPath)
  await writeFile(data.receiptPath, receipt('saved'))
  await chmod(data.receiptPath, 0o600)
  await rm(data.playerPath)
  await symlink('/etc/hosts', data.playerPath)
  const playerSymlink = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.equal(playerSymlink.observations[0]?.reason, 'unsafe_player_path')
  assert.equal(calls, 0)
  await rm(data.playerPath)
  await writeFile(data.playerPath, fileBody)
  await chmod(data.playerPath, 0o600)

  await writeFile(data.receiptPath, receipt('saved', { canonical_name_hex: Buffer.from('../escape').toString('hex') }))
  const traversal = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.equal(traversal.observations[0]?.reason, 'invalid_canonical_name')

  await writeFile(data.receiptPath, `${receipt('saved')}unexpected=x\n`)
  const malformed = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.equal(malformed.observations[0]?.reason, 'malformed_receipt')

  await writeFile(data.receiptPath, receipt('saved').replace('version=1\nstate=saved\n', 'state=saved\nversion=1\n'))
  const reordered = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.equal(reordered.observations[0]?.reason, 'malformed_receipt')

  await writeFile(data.receiptPath, 'x'.repeat(641))
  const oversized = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.equal(oversized.observations[0]?.reason, 'unsafe_receipt_file')
  assert.equal(calls, 0)
  assert.throws(() => reconciler(data.home, async () => rpcResponse(), { maxReceiptBytes: 641 }), /configuration rejected/)
})

test('bad lowercase UUID, invalid hex, and invalid UTF-8 are rejected before RPC', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  await writeFile(data.receiptPath, receipt('saved', { actor_uuid: actor.toUpperCase() }))
  assert.equal((await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()).observations[0]?.reason, 'malformed_receipt')
  await writeFile(data.receiptPath, receipt('saved', { canonical_name_hex: 'fg' }))
  assert.equal((await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()).observations[0]?.reason, 'malformed_receipt')
  await writeFile(data.receiptPath, receipt('saved', { canonical_name_hex: 'c328' }))
  assert.equal((await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()).observations[0]?.reason, 'invalid_canonical_name')
  await writeFile(data.receiptPath, Buffer.from([0xff, 0xfe]))
  assert.equal((await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()).observations[0]?.reason, 'malformed_receipt')
  assert.equal(calls, 0)
})

test('receipt v1 requires its exact eight fields and fingerprint semantics before file or RPC use', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  const otherCorrelation = '123e4567-e89b-12d3-a456-426614174099'
  const cases: Array<{ contents: string, reason: string }> = [
    { contents: receipt('saved').replace('storage_format=player-v1', 'unknown_field=player-v1'), reason: 'malformed_receipt' },
    { contents: receipt('saved', { saved_file_sha256: fileHash.toUpperCase() }), reason: 'malformed_receipt' },
    { contents: receipt('pending', { saved_file_sha256: fileHash }), reason: 'malformed_receipt' },
    { contents: receipt('saved', { correlation_uuid: otherCorrelation }), reason: 'receipt_filename_mismatch' },
    { contents: receipt('saved', { storage_format: 'player-v2' }), reason: 'unsupported_storage_format' },
  ]
  let calls = 0
  for (const { contents, reason } of cases) {
    await writeFile(data.receiptPath, contents)
    const result = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
    assert.equal(result.observations[0]?.reason, reason)
  }
  assert.equal(calls, 0)
})

test('receipt v1 accepts only the C canonical UTF-8 name spelling before any player lookup or RPC', async () => {
  const fs = (contents: string): ReconcilerFilesystem => ({
    assertSafeDirectory: async () => {},
    listReceiptEntries: async () => [{ name: `${correlation}.receipt`, kind: 'file' }],
    readSmallRegularFile: async () => {
      const bytes = Buffer.from(contents, 'utf8')
      return { bytes, sha256: createHash('sha256').update(bytes).digest('hex'), byteLength: bytes.length, device: 1, inode: 1, modifiedAtMs: 1, changedAtMs: 1 }
    },
    sha256RegularFile: async () => ({ sha256: fileHash, byteLength: fileBody.length, device: 2, inode: 2, modifiedAtMs: 1, changedAtMs: 1 }),
  })
  for (const name of ['alice', 'ALIce']) {
    let calls = 0
    const result = await reconciler('/isolated-home', async () => {
      calls++
      return rpcResponse()
    }, { fs: fs(receipt('saved', { canonical_name_hex: Buffer.from(name, 'utf8').toString('hex') })) }).runOnce()
    assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'invalid_canonical_name' }], name)
    assert.equal(calls, 0, name)
  }

  let calls = 0
  const validUtf8 = await reconciler('/isolated-home', async () => {
    calls++
    return rpcResponse()
  }, { fs: fs(receipt('saved', { canonical_name_hex: Buffer.from('가', 'utf8').toString('hex') })) }).runOnce()
  assert.deepEqual(statuses(validUtf8), ['reconciled'])
  assert.equal(calls, 1)
})

test('rejects group- or world-accessible receipt and player filesystem objects', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  const fetchImpl: typeof fetch = async () => { calls++; return rpcResponse() }

  await chmod(data.receiptPath, 0o640)
  let result = await reconciler(data.home, fetchImpl).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'unsafe_receipt_file' }])
  assert.equal(calls, 0)

  await chmod(data.receiptPath, 0o600)
  await chmod(join(data.home, 'onboarding-receipts'), 0o750)
  result = await reconciler(data.home, fetchImpl).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'unsafe_receipt_directory' }])
  assert.equal(calls, 0)

  await chmod(join(data.home, 'onboarding-receipts'), 0o700)
  await chmod(data.playerPath, 0o640)
  result = await reconciler(data.home, fetchImpl).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'unsafe_player_path' }])
  assert.equal(calls, 0)

  await chmod(data.playerPath, 0o600)
  await chmod(join(data.home, 'player'), 0o750)
  result = await reconciler(data.home, fetchImpl).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'unsafe_player_path' }])
  assert.equal(calls, 0)

  await chmod(join(data.home, 'player'), 0o700)
  await chmod(data.home, 0o750)
  result = await reconciler(data.home, fetchImpl).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'unsafe_receipt_directory' }])
  assert.equal(calls, 0)
})

test('the Node filesystem boundary rejects a symlink in any parent component', async (t) => {
  const root = await mkdtemp(join(process.cwd(), '.muhan-reconciler-parent-link-'))
  t.after(() => rm(root, { recursive: true, force: true }))
  const safe = join(root, 'safe')
  const outside = join(root, 'outside')
  await mkdir(safe)
  await mkdir(outside)
  await writeFile(join(outside, 'receipt'), 'attacker-controlled')
  await symlink(outside, join(safe, 'redirect'))

  const nodeFs = new NodeReconcilerFilesystem()
  await assert.rejects(nodeFs.readSmallRegularFile(join(safe, 'redirect', 'receipt'), 128))
  await assert.rejects(nodeFs.sha256RegularFile(join(safe, 'redirect', 'receipt'), 128))
})

test('non-regular or size-limited player files fail closed before reconciliation', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  const tooLargeForConfiguredLimit = await reconciler(data.home, async () => { calls++; return rpcResponse() }, { maxPlayerFileBytes: fileBody.length - 1 }).runOnce()
  assert.deepEqual(tooLargeForConfiguredLimit.observations, [{ outcome: 'rejected', reason: 'unsafe_player_path' }])

  await rm(data.playerPath)
  await mkdir(data.playerPath)
  const directoryInsteadOfFile = await reconciler(data.home, async () => { calls++; return rpcResponse() }).runOnce()
  assert.deepEqual(directoryInsteadOfFile.observations, [{ outcome: 'rejected', reason: 'unsafe_player_path' }])
  assert.equal(calls, 0)
})

test('an uncertain PostgREST failure retries within its bound and polling retains aggregate counts only', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  const clock: Clock = { now: () => 0, sleep: async () => { sleeps++ } }
  let sleeps = 0
  const result = await reconciler(data.home, async () => {
    calls++
    if (calls === 1) throw new TypeError('network unavailable')
    return rpcResponse()
  }, { clock }).runOnce()
  assert.deepEqual(statuses(result), ['reconciled'])
  assert.equal(calls, 2)
  assert.equal(sleeps, 1)

  let exhaustedCalls = 0
  let exhaustedSleeps = 0
  const exhausted = await reconciler(data.home, async () => {
    exhaustedCalls++
    return new Response('', { status: 503 })
  }, { clock: { now: () => 0, sleep: async () => { exhaustedSleeps++ } } }).runOnce()
  assert.deepEqual(exhausted.observations, [{ outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: 2 }])
  assert.equal(exhaustedCalls, 2)
  assert.equal(exhaustedSleeps, 1)

  const pollingClock: Clock = { now: () => 0, sleep: async () => { polls++ } }
  let polls = 0
  const pollService = { runOnce: async (): Promise<RunSummary> => ({ observations: [{ outcome: 'pending' }] }) }
  const aggregate = await runPolling(pollService, pollingClock, { runs: 10_000, intervalMs: 1 })
  assert.deepEqual(aggregate, { pending: 10_000, reconciled: 0, committed: 0, rejected: 0, retry_exhausted: 0 })
  assert.equal(Array.isArray(aggregate), false)
  assert.equal(polls, 9_999)
})

test('operational polling rejects configurations outside its finite run and interval bounds', async () => {
  const clock: Clock = { now: () => 0, sleep: async () => {} }
  const service = { runOnce: async (): Promise<RunSummary> => ({ observations: [] }) }
  await assert.rejects(runPolling(service, clock, { runs: 10_001, intervalMs: 0 }), /configuration rejected/)
  await assert.rejects(runPolling(service, clock, { runs: 1, intervalMs: 60_001 }), /configuration rejected/)
  await assert.rejects(main({ ONBOARDING_RECONCILER_POLLS: '10001' }, []), /configuration rejected/)
})

test('a PostgREST response mismatch is rejected and not retried', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let calls = 0
  const result = await reconciler(data.home, async () => {
    calls++
    return rpcResponse({ character_id: '123e4567-e89b-12d3-a456-426614174099' })
  }).runOnce()
  assert.deepEqual(statuses(result), ['rejected'])
  assert.equal(result.observations[0]?.reason, 'rpc_response_mismatch')
  assert.equal(calls, 1)
})

test('PostgREST must return the exact JSON RPC contract and fetch must not follow credential-leaking redirects', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  const responseCases: Array<{ label: string, response: Response }> = [
    {
      label: 'a non-JSON content type',
      response: new Response(JSON.stringify([{
        character_id: character,
        actor_user_id: actor,
        lifecycle: 'active',
        status: 'finalized',
        saved_file_sha256: fileHash,
        storage_format: 1,
      }]), { headers: { 'content-type': 'text/plain' } }),
    },
    { label: 'an unknown response column', response: rpcResponse({ unexpected: true }) },
    {
      label: 'an oversized JSON response',
      response: new Response(`${' '.repeat(16 * 1024)}${JSON.stringify([{
        character_id: character,
        actor_user_id: actor,
        lifecycle: 'active',
        status: 'finalized',
        saved_file_sha256: fileHash,
        storage_format: 1,
      }])}`, { headers: { 'content-type': 'application/json' } }),
    },
  ]
  for (const { label, response } of responseCases) {
    const result = await reconciler(data.home, async () => response).runOnce()
    assert.deepEqual(result.observations, [{ outcome: 'rejected', reason: 'rpc_response_mismatch', attempts: 1 }], label)
  }

  let request: RequestInit | undefined
  const result = await reconciler(data.home, async (_url, init) => {
    request = init
    return rpcResponse()
  }).runOnce()
  assert.deepEqual(statuses(result), ['reconciled'])
  assert.equal(request?.redirect, 'error')
  assert.ok(request?.signal instanceof AbortSignal)
})

test('RPC requests abort at their configured deadline before bounded retries are exhausted', async (t) => {
  const data = await fixture()
  t.after(data.cleanup)
  let aborted = false
  const result = await reconciler(data.home, async (_url, init) => new Promise<Response>((_resolve, reject) => {
    if (!init?.signal) {
      reject(new Error('missing abort signal'))
      return
    }
    init.signal.addEventListener('abort', () => {
      aborted = true
      reject(new DOMException('request timed out', 'AbortError'))
    }, { once: true })
  }), { rpcAttempts: 1, rpcRequestTimeoutMs: 1 }).runOnce()
  assert.deepEqual(result.observations, [{ outcome: 'retry_exhausted', reason: 'rpc_retry_exhausted', attempts: 1 }])
  assert.equal(aborted, true)
})

test('CLI output is aggregate-only and never includes service credentials or receipt data', async (t) => {
  const data = await fixture(receipt('pending'))
  t.after(data.cleanup)
  const serviceRoleKey = 'test-service-role-key-must-not-be-logged'
  let output = ''
  const originalWrite = process.stdout.write
  process.stdout.write = ((chunk: string | Uint8Array) => {
    output += Buffer.from(chunk).toString('utf8')
    return true
  }) as typeof process.stdout.write
  try {
    assert.equal(await main({
      MUHAN_HOME: data.home,
      SUPABASE_INTERNAL_REST_URL: 'http://postgrest.internal:3000',
      SUPABASE_SERVICE_ROLE_KEY: serviceRoleKey,
    }, ['--once']), 0)
  } finally {
    process.stdout.write = originalWrite
  }
  assert.equal(output, '{"pending":1,"reconciled":0,"committed":0,"rejected":0,"retry_exhausted":0}\n')
  assert.equal(output.includes(serviceRoleKey), false)
  assert.equal(output.includes(data.receiptPath), false)
  assert.equal(output.includes(fileHash), false)
})

test('the filesystem boundary can be injected for a hermetic operational test', async () => {
  const fs: ReconcilerFilesystem = {
    assertSafeDirectory: async () => {},
    listReceiptEntries: async () => [{ name: `${correlation}.receipt`, kind: 'file' }],
    readSmallRegularFile: async () => {
      const bytes = Buffer.from(receipt('pending'))
      return { bytes, sha256: createHash('sha256').update(bytes).digest('hex'), byteLength: bytes.length, device: 1, inode: 1, modifiedAtMs: 1, changedAtMs: 1 }
    },
    sha256RegularFile: async () => { throw new Error('pending must not inspect player file') },
  }
  const result = await reconciler('/isolated-home', async () => rpcResponse(), { fs }).runOnce()
  assert.deepEqual(statuses(result), ['pending'])
})
