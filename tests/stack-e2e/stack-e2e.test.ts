import assert from 'node:assert/strict'
import { createHash, createHmac } from 'node:crypto'
import { execFile, spawn, type ChildProcess } from 'node:child_process'
import { chmod, cp, mkdir, readFile, readdir, rename, stat, writeFile } from 'node:fs/promises'
import { once } from 'node:events'
import test from 'node:test'
import { createRequire } from 'node:module'
import { join, resolve } from 'node:path'
import { promisify } from 'node:util'
import { loadConfig } from '../../services/gateway/src/config.js'
import { createGateway, type RunningGateway } from '../../services/gateway/src/gateway.js'
import { SupabaseCharacterAuthorizer } from '../../services/gateway/src/character-authorizer.js'
import { SupabaseOnboardingAuthorizer, type OnboardingAuthorizer } from '../../services/gateway/src/onboarding-authorizer.js'
import { createBatchIdentity } from '../../services/character-inventory-importer/src/batch-identity.js'
import { importBatch, importRecords, type InventoryRecord } from '../../services/character-inventory-importer/src/inventory.js'
import { PostgresImportStore } from '../../services/character-inventory-importer/src/postgres-store.js'
import { NodeReconcilerFilesystem, OnboardingReconciler } from '../../services/onboarding-reconciler/src/reconciler.js'
import {
  runWebStackAcceptance,
  startWebStackServer,
  stopWebStackServer,
  waitForWebStackServer,
  type WebStackServer,
} from './web-stack-ui.js'
import { cleanupFailure, runCleanupSteps } from './lifecycle.js'
import { assertOnboardingNormalizedSnapshot } from './normalized-snapshot-check.js'
import { parseManifest } from '../../services/m4-file-snapshot-manifest-relay/src/manifest.js'
import { sql } from './sql-transport.js'
import { parsePlayerSnapshotV1ReceiptBoundArtifactEvidence } from '../../services/m4-file-snapshot-manifest-relay/src/player-snapshot-v1-artifact.js'

const run = promisify(execFile)
const actor = '11111111-1111-4111-8111-111111111111'
const webProvisionActor = '88888888-8888-4888-8888-888888888888'
const webClaimActor = '99999999-9999-4999-8999-999999999999'
const cancelledCorrelation = '22222222-2222-4222-8222-222222222222'
const correlation = '33333333-3333-4333-8333-333333333333'
const badCorrelation = '55555555-5555-4555-8555-555555555555'
const denialCorrelation = '77777777-7777-4777-8777-777777777777'
const importedClaimCorrelation = '66666666-6666-4666-8666-666666666666'
const wrongPasswordCorrelation = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'
const expiredClaimCorrelation = 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'
const missingMemberCorrelation = 'cccccccc-cccc-4ccc-8ccc-cccccccccccc'
// C create_ply accepts passwords up to 14 bytes.
const password = 'stack-e2e-pass'
const accessToken = 'stack-e2e-browser-token'
const admissionSecret = 'stack-e2e-admission-secret-0123456789'
const requestedName = 'StackHero'
// C's lowercize(name, 1) folds every ASCII letter and capitalizes only the
// first byte. The DB key and the on-disk player file must use this exact form.
const canonicalName = 'Stackhero'
const badRequestedName = 'StackBad'
const badCanonicalName = 'Stackbad'
const importedClaimName = 'Importhero'
const missingMemberName = 'Orphanhero'
const webClaimName = 'Webclaim'
const webProvisionName = 'Webhero'
const origin = 'http://localhost:3000'
const root = resolve(process.env.STACK_E2E_ROOT ?? process.cwd())
const require = createRequire(join(root, 'package.json'))
const WebSocket: any = require(resolve(root, 'services/gateway/node_modules/ws/index.js'))
const fixture = process.env.STACK_E2E_FIXTURE ?? resolve(process.env.TMPDIR ?? '/tmp', `muhan-stack-e2e-fixture-${process.pid}`)
let mudDiagnostics = ''
let browserDiagnostics = ''

type Evidence = { schema: 1, status: 'passed' | 'failed', events: Array<Record<string, string>>, error?: string }
const evidence: Evidence = { schema: 1, status: 'failed', events: [] }

function browserJwt(subject: string): string {
  const secret = process.env.STACK_E2E_JWT_SECRET
  assert.ok(secret, 'STACK_E2E_JWT_SECRET is required')
  const encode = (value: Record<string, unknown>) => Buffer.from(JSON.stringify(value)).toString('base64url')
  const unsigned = `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode({ aud: 'authenticated', exp: 4_102_444_800, role: 'authenticated', sub: subject })}`
  return `${unsigned}.${createHmac('sha256', secret).update(unsigned).digest('base64url')}`
}

function redact(value: string): string {
  return [process.env.STACK_E2E_SERVICE_ROLE_JWT, process.env.STACK_E2E_PG_PASSWORD, process.env.STACK_E2E_M3_WRITER_PASSWORD, process.env.STACK_E2E_M3_DATABASE_URL, process.env.STACK_E2E_NORMALIZED_READER_URL, 'stack-e2e-reader-password', admissionSecret, password, accessToken]
    .filter((secret): secret is string => Boolean(secret))
    .reduce((output, secret) => output.split(secret).join('<REDACTED>'), value)
}

async function eventually(check: () => void | Promise<void>, timeoutMs = 15_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  let last: unknown
  while (Date.now() < deadline) {
    try { await check(); return } catch (error) { last = error }
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw last instanceof Error ? last : new Error('condition timed out')
}

async function raceWithTimeout<T>(operation: Promise<T>, timeoutMs: number): Promise<T | undefined> {
  let timer: NodeJS.Timeout | undefined
  try {
    return await Promise.race([
      operation,
      new Promise<undefined>((resolve) => { timer = setTimeout(() => resolve(undefined), timeoutMs) }),
    ])
  } finally {
    if (timer) clearTimeout(timer)
  }
}

async function choosePort(): Promise<number> {
  const net = await import('node:net')
  const server = net.createServer()
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const address = server.address()
  assert.ok(address && typeof address !== 'string')
  const port = address.port
  await new Promise<void>((resolveClose) => server.close(() => resolveClose()))
  return port
}

async function prepareFixture(): Promise<void> {
  await mkdir(fixture, { recursive: true })
  // The native writer deliberately opens an existing private journal root;
  // it must not manufacture a deployment root during ownership acquisition.
  await mkdir(join(fixture, 'character-save-journal'), { mode: 0o700 })
  await mkdir(join(fixture, 'character-save-stage'), { mode: 0o700 })
  assert.equal((await stat(join(fixture, 'character-save-journal'))).mode & 0o777, 0o700)
  for (const directory of ['rooms', 'objmon', 'help', 'post']) {
    await cp(join(root, directory), join(fixture, directory), { recursive: true })
  }
  await cp(join(root, 'resources_utf8', 'player'), join(fixture, 'player'), { recursive: true })
  // Git archives preserve resource-directory mode 0755. The live publisher
  // requires the private writable player root before the first shadow save.
  await chmod(fixture, 0o700)
  await chmod(join(fixture, 'player'), 0o700)
  assert.equal((await stat(join(fixture, 'player'))).mode & 0o777, 0o700)
  for (const directory of ['alias', 'bank', 'simul', 'suic', 'fal', 'invite', 'vote', 'marriage', 'family']) {
    await mkdir(join(fixture, 'player', directory), { recursive: true })
  }
  for (const directory of ['log', 'bin']) await mkdir(join(fixture, directory), { recursive: true })
  await mkdir(join(fixture, 'log', 'auth'), { recursive: true })
}

function startMud(binary: string, port: number, trustedAdmission = true, m3Enabled = process.env.STACK_E2E_M3_ENABLED === '1'): ChildProcess {
  const {
    MUD_REQUIRE_TRUSTED_ADMISSION: _ignoredTrustedAdmission,
    MUD_ADMISSION_SECRET: _ignoredAdmissionSecret,
    MUD_M3_MODE: _ignoredM3Mode,
    MUD_M3_WORLD_ID: _ignoredM3World,
    MUD_M3_CONNINFO_FILE: _ignoredM3Conninfo,
    MUD_M3_PLAYER_SNAPSHOT_V1: _ignoredM3Snapshot,
    ...environment
  } = process.env
  const m3Environment = m3Enabled ? {
    MUD_M3_MODE: 'shadow',
    MUD_M3_WORLD_ID: 'muhan',
    MUD_M3_CONNINFO_FILE: process.env.STACK_E2E_M3_CONNINFO_FILE!,
    MUD_M3_PLAYER_SNAPSHOT_V1: 'handoff',
  } : { MUD_M3_MODE: 'off' }
  if (m3Enabled) assert.ok(process.env.STACK_E2E_M3_CONNINFO_FILE, 'runner must provide an M3 writer conninfo file')
  const child = spawn(binary, ['-r', String(port)], {
    cwd: fixture,
    env: {
      ...environment,
      MUHAN_HOME: fixture,
      ...m3Environment,
      MUD_ENABLE_ONBOARDING: trustedAdmission ? '1' : '0',
      ...(trustedAdmission ? { MUD_REQUIRE_TRUSTED_ADMISSION: '1', MUD_ADMISSION_SECRET: admissionSecret } : {}),
      LC_ALL: 'C.UTF-8',
    },
    stdio: ['ignore', 'pipe', 'pipe']
  })
  const capture = (data: Buffer): void => {
    mudDiagnostics = `${mudDiagnostics}${data.toString('utf8')}`.slice(-8_000)
  }
  child.stdout?.on('data', capture)
  child.stderr?.on('data', capture)
  return child
}

type RelaySummary = Record<string, number>

async function runM3ArtifactRelay(entrypoint: 'player-snapshot-v1-manifest-first-cli.ts' | 'player-snapshot-v1-artifact-cli.ts'): Promise<RelaySummary> {
  const databaseUrl = process.env.STACK_E2E_M3_DATABASE_URL
  assert.ok(databaseUrl, 'runner must provide the M3 writer database URL')
  const projectorPath = process.env.STACK_E2E_NORMALIZED_PROJECTOR
  assert.ok(projectorPath, 'runner must build the normalized Rust projector')
  const { stdout } = await run(join(root, 'services/m4-file-snapshot-manifest-relay/node_modules/.bin/tsx'), [
    join(root, `services/m4-file-snapshot-manifest-relay/src/${entrypoint}`), '--once',
  ], {
    cwd: root,
    env: {
      ...process.env,
      DATABASE_URL: databaseUrl,
      M4_FILE_SNAPSHOT_OUTBOX_DIR: join(fixture, 'character-player-snapshot-v1-outbox'),
      M4_PLAYER_SNAPSHOT_V1_ARTIFACT_FULFILLMENT_ENABLED: 'true',
      M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_PERSISTENCE_ENABLED:
        entrypoint === 'player-snapshot-v1-manifest-first-cli.ts' ? 'true' : 'false',
      M4_PLAYER_SNAPSHOT_NORMALIZED_V1_PROJECTION_RUNNER: projectorPath,
    },
  })
  return JSON.parse(stdout.trim()) as RelaySummary
}

async function relayM3Artifacts(): Promise<{ artifact: RelaySummary, manifest: RelaySummary }> {
  const manifest = await runM3ArtifactRelay('player-snapshot-v1-manifest-first-cli.ts')
  const artifact = await runM3ArtifactRelay('player-snapshot-v1-artifact-cli.ts')
  return { artifact, manifest }
}

function assertNoPlaintextSecrets(label: string, value: string | Buffer, secrets: readonly string[]): void {
  const bytes = typeof value === 'string' ? Buffer.from(value, 'utf8') : value
  for (const secret of secrets) {
    assert.equal(bytes.includes(Buffer.from(secret, 'utf8')), false, `${label} must not contain plaintext secret material`)
  }
}

async function writerSql(query: string): Promise<string> {
  const output = await sql(`set session authorization mud_writer_login; set role mud_writer; ${query}`)
  return output.split('\n').at(-1)!
}

async function assertM3OnboardingEvidence({
  actorUserId,
  correlationId,
  expectedMode,
  secrets,
}: {
  actorUserId: string
  correlationId: string
  expectedMode: 'claim' | 'provision'
  secrets: readonly string[]
}): Promise<{ characterId: string, commandId: string }> {
  const binding = await sql(`select b.character_id || '|' || b.command_id || '|' || b.mode || '|' || o.status || '|' || count(a.command_id)::text || '|' || count(m.command_id)::text || '|' || count(r.command_id)::text || '|' || count(f.correlation_id)::text || '|' || (r.acknowledged_at > b.bound_at)::text || '|' || (f.artifact_command_id = b.command_id)::text from private.game_character_onboarding_snapshot_command_bindings b join private.game_character_onboarding_snapshot_eligibility_outbox o using (correlation_id) left join private.game_character_player_snapshot_v1_artifacts a on a.character_id = b.character_id and a.command_id = b.command_id left join private.game_character_m4_file_snapshot_manifests m on m.character_id = b.character_id and m.command_id = b.command_id left join private.game_character_shadow_receipts r on r.character_id = b.character_id and r.command_id = b.command_id left join private.game_character_onboarding_snapshot_fulfillments f using (correlation_id) where b.correlation_id = '${correlationId}' and b.actor_user_id = '${actorUserId}' group by b.character_id, b.command_id, b.mode, o.status, r.acknowledged_at, b.bound_at, f.artifact_command_id`)
  const [characterId, commandId, mode, outboxStatus, artifacts, manifests, receipts, fulfillments, acknowledgedAfterBinding, fulfillmentBoundCommand] = binding.split('|')
  assert.match(characterId!, /^[0-9a-f-]{36}$/)
  assert.match(commandId!, /^[0-9a-f-]{36}$/)
  assert.equal(mode, expectedMode)
  assert.equal(outboxStatus, 'fulfilled')
  assert.equal(artifacts, '1', 'one immutable PlayerSnapshotV1 artifact must match the bound command')
  assert.equal(manifests, '1', 'one immutable receipt manifest must match the bound command')
  assert.equal(receipts, '1', 'one receipt must match the bound command')
  assert.equal(fulfillments, '1', 'one immutable fulfillment must match the correlation')
  assert.equal(acknowledgedAfterBinding, 'true', 'the receipt must be acknowledged only after the command binding')
  assert.equal(fulfillmentBoundCommand, 'true', 'the fulfillment must retain the immutable bound command')

  const outbox = join(fixture, 'character-player-snapshot-v1-outbox')
  const [artifact, manifest, receipt] = await Promise.all([
    readFile(join(outbox, `${commandId}.player-snapshot-v1`)),
    readFile(join(outbox, `${commandId}.manifest`), 'utf8'),
    readFile(join(fixture, 'onboarding-receipts', `${correlationId}.receipt`), 'utf8'),
  ])
  assertNoPlaintextSecrets('PlayerSnapshotV1 artifact', artifact, secrets)
  assertNoPlaintextSecrets('M3 receipt manifest', manifest, secrets)
  assertNoPlaintextSecrets('onboarding receipt', receipt, secrets)
  const readerUrl = process.env.STACK_E2E_NORMALIZED_READER_URL
  const projectorPath = process.env.STACK_E2E_NORMALIZED_PROJECTOR
  assert.ok(readerUrl, 'runner must provide a dedicated normalized reader login')
  assert.ok(projectorPath, 'runner must build the normalized Rust projector')
  const evidence = parsePlayerSnapshotV1ReceiptBoundArtifactEvidence(
    `${commandId}.player-snapshot-v1`, artifact, parseManifest(Buffer.from(manifest)),
  )
  await assertOnboardingNormalizedSnapshot(evidence, readerUrl, projectorPath)
  return { characterId: characterId!, commandId: commandId! }
}

async function stopMud(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null) {
    assert.equal(child.signalCode, null)
    assert.equal(child.exitCode, 0)
    return
  }
  const exited = once(child, 'exit') as Promise<[number | null, NodeJS.Signals | null]>
  child.kill('SIGTERM')
  let timeout: NodeJS.Timeout | undefined
  try {
    const [code, signal] = await Promise.race([
      exited,
      new Promise<never>((_, reject) => { timeout = setTimeout(() => reject(new Error('MUD did not exit after SIGTERM')), 10_000) }),
    ])
    assert.equal(signal, null)
    assert.equal(code, 0)
  } finally {
    if (timeout) clearTimeout(timeout)
  }
}

async function stopMudDuringFailure(child: ChildProcess): Promise<void> {
  try {
    await stopMud(child)
    return
  } catch {
    // Preserve the original test failure, but do not leave a C server behind
    // if graceful shutdown itself is the thing under test that failed.
    if (child.exitCode === null) {
      const exited = once(child, 'exit')
      child.kill('SIGKILL')
      await exited
    }
  }
}

async function crashMud(child: ChildProcess): Promise<void> {
  if (child.exitCode !== null) return
  const exited = once(child, 'exit') as Promise<[number | null, NodeJS.Signals | null]>
  child.kill('SIGKILL')
  const [code, signal] = await exited
  assert.equal(code, null)
  assert.equal(signal, 'SIGKILL')
}

async function waitForMud(port: number, child: ChildProcess): Promise<void> {
  const net = await import('node:net')
  await eventually(async () => {
    if (child.exitCode !== null) throw new Error(`MUD exited before listening (${child.exitCode}): ${redact(mudDiagnostics)}`)
    await new Promise<void>((resolveConnect, reject) => {
      const socket = net.createConnection({ host: '127.0.0.1', port })
      socket.once('connect', () => { socket.destroy(); resolveConnect() })
      socket.once('error', (error) => { socket.destroy(); reject(error) })
    })
  }, 20_000)
}

function legacyPlayerPath(name: string): string {
  return join(fixture, 'player', createHash('sha1').update(name).digest('hex').slice(0, 2), name)
}

async function createDisposableLegacyPlayer(port: number, name: string): Promise<void> {
  const net = await import('node:net')
  const socket = net.createConnection({ host: '127.0.0.1', port })
  let transcript = ''
  socket.on('data', (data: Buffer) => { transcript = `${transcript}${data.toString('utf8')}`.slice(-16_000) })
  await once(socket, 'connect')
  const expect = async (pattern: RegExp): Promise<void> => {
    await eventually(() => assert.match(transcript, pattern))
    transcript = ''
  }
  try {
    await expect(/엔터/)
    socket.write('\n')
    await expect(/당신의 이름은 무엇입니까/)
    socket.write(`${name}\n`)
    await expect(/하시겠습니까/)
    socket.write('예\n')
    await expect(/엔터/)
    socket.write('\n')
    await expect(/남자입니까/)
    socket.write('남\n')
    await expect(/직업을 고르세요/)
    socket.write('4\n')
    await expect(/능력:/)
    socket.write('12 10 12 10 10\n')
    await expect(/익숙한 무기를/)
    socket.write('1\n')
    await expect(/성향을 고르십시요/)
    socket.write('선\n')
    await expect(/종족을 고르십시요/)
    socket.write('7\n')
    await expect(/새 암호를/)
    socket.write(`${password}\n`)
    await eventually(async () => { await stat(legacyPlayerPath(name)) })
  } finally {
    socket.destroy()
    if (!socket.destroyed) await raceWithTimeout(once(socket, 'close'), 5_000)
  }
}

function legacyInventoryRecord(name: string, sha256: string, byteSize: number): InventoryRecord {
  const shard = createHash('sha1').update(name).digest('hex').slice(0, 2)
  return { name, canonicalNameKey: name, relativePath: `player/${shard}/${name}`, observedShard: shard, expectedShard: shard, byteSize, sha256 }
}

class Browser {
  readonly frames: Array<{ data: Buffer, binary: boolean }> = []
  constructor(readonly ws: any) {
    ws.on('message', (data: unknown, binary: boolean) => this.frames.push({ data: Buffer.from(data as Uint8Array), binary }))
  }
  text(): string {
    const text = this.frames.filter((frame) => frame.binary).map((frame) => frame.data.toString('utf8')).join('')
    browserDiagnostics = text.slice(-8_000)
    return text
  }
  json(type: string): boolean {
    return this.frames.some((frame) => {
      if (frame.binary) return false
      try { const value = JSON.parse(frame.data.toString('utf8')) as { type?: string }; return value.type === type } catch { return false }
    })
  }
  send(data: string): void { this.ws.send(Buffer.from(data, 'utf8')) }
}

async function openOnboarding(address: string, correlationId: string, mode: 'provision' | 'claim' = 'provision'): Promise<Browser> {
  const ws = new WebSocket(`${address.replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin })
  await once(ws, 'open')
  const browser = new Browser(ws)
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken, mode, correlationId }))
  await eventually(() => assert.ok(browser.frames.some((frame) => !frame.binary && frame.data.toString() === JSON.stringify({ type: 'onboarding-ready', mode })),
    `onboarding-ready missing; error-frame=${browser.json('error')}; socket-state=${ws.readyState}`))
  await eventually(() => assert.match(browser.text(), /당신의 이름은 무엇입니까/))
  return browser
}

async function runReconciler(restUrl: string): Promise<void> {
  const child = spawn(join(root, 'services/onboarding-reconciler/node_modules/.bin/tsx'), [join(root, 'services/onboarding-reconciler/src/cli.ts'), '--once'], {
    cwd: root,
    env: {
      ...process.env,
      MUHAN_HOME: fixture,
      SUPABASE_INTERNAL_REST_URL: restUrl,
      SUPABASE_SERVICE_ROLE_KEY: process.env.STACK_E2E_SERVICE_ROLE_JWT,
      ONBOARDING_RECONCILER_RPC_ATTEMPTS: '2',
      ONBOARDING_RECONCILER_RETRY_DELAY_MS: '10',
      // This acceptance lane verifies saved-receipt handoff recovery, not
      // evidence-only reconciliation, which intentionally stays pending.
      ONBOARDING_RECONCILER_RECOVER_SAVED_HANDOFFS: 'true',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  let output = ''
  const capture = (data: Buffer): void => { output = `${output}${data.toString('utf8')}`.slice(-2_000) }
  child.stdout?.on('data', capture)
  child.stderr?.on('data', capture)
  const [code, signal] = await once(child, 'exit') as [number | null, NodeJS.Signals | null]
  assert.equal(signal, null)
  if (code !== 0) {
    const diagnostic = await new OnboardingReconciler({ muhanHome: fixture, postgrestUrl: restUrl, serviceRoleKey: process.env.STACK_E2E_SERVICE_ROLE_JWT!, rpcAttempts: 1, recoverSavedReceiptHandoffs: true }).runOnce()
    throw new Error(`onboarding reconciler exited unsuccessfully: ${output} ${JSON.stringify(diagnostic)}`)
  }
}

async function hardenPlayerDirectories(...names: string[]): Promise<void> {
  await chmod(fixture, 0o700)
  await chmod(join(fixture, 'player'), 0o700)
  await chmod(join(fixture, 'onboarding-receipts'), 0o700)
  for (const name of names) {
    const shard = createHash('sha1').update(name).digest('hex').slice(0, 2)
    assert.equal((await stat(join(fixture, 'player', shard))).mode & 0o777, 0o700, `C shard directory must be private: ${name}`)
    assert.equal((await stat(join(fixture, 'player', shard, name))).mode & 0o777, 0o600, `C player file must be private: ${name}`)
  }
  for (const path of [fixture, join(fixture, 'onboarding-receipts'), join(fixture, 'player')]) {
    assert.equal((await stat(path)).mode & 0o777, 0o700, `reconciler root must be private: ${path}`)
  }
  const reconcilerFs = new NodeReconcilerFilesystem()
  try { await reconcilerFs.assertSafeDirectory(fixture) } catch { throw new Error('stack fixture is not a safe reconciler home') }
  try { await reconcilerFs.assertSafeDirectory(join(fixture, 'onboarding-receipts')) } catch { throw new Error('stack receipt directory is not safe') }
}

async function runSingleRecoveryReceipt(restUrl: string): Promise<void> {
  const directory = join(fixture, 'onboarding-receipts')
  const entries = ['33333333-3333-4333-8333-333333333333.receipt']
  const hidden = entries.map((entry) => ({ source: join(directory, entry), target: join(directory, `${entry}.during-recovery`) }))
  for (const entry of hidden) await rename(entry.source, entry.target)
  try { await runReconciler(restUrl) } finally {
    for (const entry of hidden) await rename(entry.target, entry.source)
  }
}

async function openGame(address: string, characterId: string): Promise<Browser> {
  const ws = new WebSocket(`${address.replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin })
  await once(ws, 'open')
  const browser = new Browser(ws)
  ws.send(JSON.stringify({ type: 'auth', accessToken, characterId }))
  await eventually(() => assert.ok(browser.json('ready')))
  return browser
}

async function assertNormalAdmissionDenied(address: string, characterId: string): Promise<void> {
  const ws = new WebSocket(`${address.replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin })
  await once(ws, 'open')
  const closed = once(ws, 'close') as Promise<[number]>
  ws.send(JSON.stringify({ type: 'auth', accessToken, characterId }))
  const [code] = await closed
  assert.equal(code, 1008)
}

async function closeAndWait(ws: any): Promise<void> {
  if (ws.readyState === WebSocket.OPEN) ws.close()
  if (ws.readyState !== WebSocket.CLOSED) {
    await raceWithTimeout(once(ws, 'close'), 5_000)
    if (ws.readyState !== WebSocket.CLOSED) ws.terminate()
  }
}

async function terminateAndWait(ws: any): Promise<void> {
  if (ws.readyState !== WebSocket.CLOSED) {
    const closed = once(ws, 'close')
    ws.terminate()
    await raceWithTimeout(closed, 5_000)
  }
}

async function waitForRejectedWebSocket(ws: any): Promise<void> {
  await new Promise<void>((resolve) => {
    const finish = (): void => {
      ws.off('error', onError)
      ws.off('unexpected-response', onUnexpected)
      resolve()
    }
    const onError = (): void => finish()
    const onUnexpected = (_request: unknown, response: import('node:http').IncomingMessage): void => {
      response.resume()
      finish()
    }
    ws.once('error', onError)
    ws.once('unexpected-response', onUnexpected)
  })
  if (ws.readyState !== WebSocket.CLOSED) {
    // ws can remain in CONNECTING after an HTTP rejection while its transport
    // has already been destroyed by the server.
    ws.on('error', () => undefined)
    try { ws.terminate() } catch { /* the rejected transport is already closed */ }
  }
}

async function closeGatewayBounded(gateway: RunningGateway): Promise<void> {
  // Let the Gateway own its configured drain grace period. Returning before
  // that lifecycle settles leaves its server handle alive in this worker.
  await gateway.close()
  // These are test-owned, in-process connections; make the final cleanup
  // idempotent without affecting any external resource.
  gateway.server.closeAllConnections?.()
  gateway.server.closeIdleConnections?.()
}

async function main(): Promise<void> {
  let gateway: RunningGateway | undefined
  let mud: ChildProcess | undefined
  let web: WebStackServer | undefined
  let scenarioFailed = false
  try {
    assert.ok(process.env.STACK_E2E_BINARY, 'STACK_E2E_BINARY is required')
    assert.ok(process.env.STACK_E2E_SERVICE_ROLE_JWT, 'STACK_E2E_SERVICE_ROLE_JWT is required')
    assert.equal(process.env.STACK_E2E_M3_ENABLED, '1', 'this acceptance lane requires the M3-enabled disposable binary')
    assert.ok(process.env.STACK_E2E_M3_CONNINFO_FILE, 'runner must provide a protected M3 writer conninfo file')
    assert.ok(process.env.STACK_E2E_M3_DATABASE_URL, 'runner must provide the M3 writer database URL')
    assert.ok(process.env.STACK_E2E_NORMALIZED_READER_URL, 'runner must provide a dedicated normalized reader')
    assert.ok(process.env.STACK_E2E_NORMALIZED_PROJECTOR, 'runner must build the normalized projector')
    const webProvisionJwt = browserJwt(webProvisionActor)
    const webClaimJwt = browserJwt(webClaimActor)
    const authenticatedSubjects = new Map([
      [accessToken, actor],
      [webProvisionJwt, webProvisionActor],
      [webClaimJwt, webClaimActor],
    ])
    await prepareFixture()
    await sql(`insert into auth.users (id, aud, role, email_confirmed_at) values
      ('${actor}', 'authenticated', 'authenticated', now()),
      ('${webProvisionActor}', 'authenticated', 'authenticated', now()),
      ('${webClaimActor}', 'authenticated', 'authenticated', now())
      on conflict (id) do nothing`)

    // Build three C-owned, synthetic legacy files before the trusted-admission
    // stack starts.  They are never copied from the source tree or relabelled
    // in SQL: the importer below is the only path that admits their DB rows.
    const legacyMudPort = await choosePort()
    mud = startMud(process.env.STACK_E2E_BINARY, legacyMudPort, false, false)
    await waitForMud(legacyMudPort, mud)
    await createDisposableLegacyPlayer(legacyMudPort, importedClaimName)
    await createDisposableLegacyPlayer(legacyMudPort, missingMemberName)
    await createDisposableLegacyPlayer(legacyMudPort, webClaimName)
    await stopMud(mud)
    mud = undefined

    const importedClaimPlayer = legacyPlayerPath(importedClaimName)
    const missingMemberPlayer = legacyPlayerPath(missingMemberName)
    const webClaimPlayer = legacyPlayerPath(webClaimName)
    const importedClaimBytes = await readFile(importedClaimPlayer)
    const missingMemberBytes = await readFile(missingMemberPlayer)
    const webClaimBytes = await readFile(webClaimPlayer)
    const importedClaimDigest = createHash('sha256').update(importedClaimBytes).digest('hex')
    const missingMemberDigest = createHash('sha256').update(missingMemberBytes).digest('hex')
    const webClaimDigest = createHash('sha256').update(webClaimBytes).digest('hex')
    const databaseUrl = process.env.STACK_E2E_DATABASE_URL
    assert.ok(databaseUrl, 'runner must provide a disposable PostgreSQL URL for the real importer')
    const importer = new PostgresImportStore(databaseUrl)
    try {
      const sourceManifest = Buffer.from(`${importedClaimDigest}\n${webClaimDigest}\n`, 'utf8')
      const batch = createBatchIdentity({
        worldId: 'muhan', sourceManifestId: 'stack-e2e-legacy-fixture-v1',
        sourceSha256: createHash('sha256').update(sourceManifest).digest('hex'),
        sourceByteSize: sourceManifest.byteLength, parserVersion: '1.0.0', abi: 1,
        startMarker: 'imported-claim', endMarker: 'web-claim',
      })
      const batchRecords = [
        legacyInventoryRecord(importedClaimName, importedClaimDigest, importedClaimBytes.byteLength),
        legacyInventoryRecord(webClaimName, webClaimDigest, webClaimBytes.byteLength),
      ]
      const admitted = await importBatch(importer, batchRecords, { identity: batch, streamId: 'stack-e2e', sequence: 0, apply: true })
      assert.deepEqual(admitted, {
        wouldInsert: 0, inserted: 2, idempotent: 0,
        quarantined: { invalid_metadata: 0, duplicate_input_identity: 0, owned_row: 0, lifecycle_conflict: 0, hash_conflict: 0, identity_conflict: 0 },
        streamId: 'stack-e2e', sequence: 0, ledger: 'committed',
      })
      const retry = await importBatch(importer, batchRecords, { identity: batch, streamId: 'stack-e2e', sequence: 0, apply: true })
      assert.equal(retry.ledger, 'idempotent')
      assert.equal(retry.idempotent, 2)
      const unprovenanced = await importRecords(importer, [legacyInventoryRecord(missingMemberName, missingMemberDigest, missingMemberBytes.byteLength)], { worldId: 'muhan', apply: true })
      assert.equal(unprovenanced.inserted, 1)
    } finally {
      await importer.close()
    }
    const importedClaimCharacterId = await sql(`select id from public.game_characters where world_id = 'muhan' and legacy_name_key = '${importedClaimName}'`)
    const missingMemberCharacterId = await sql(`select id from public.game_characters where world_id = 'muhan' and legacy_name_key = '${missingMemberName}'`)
    const webClaimCharacterId = await sql(`select id from public.game_characters where world_id = 'muhan' and legacy_name_key = '${webClaimName}'`)
    assert.match(importedClaimCharacterId, /^[0-9a-f-]{36}$/)
    assert.match(webClaimCharacterId, /^[0-9a-f-]{36}$/)
    assert.equal(await sql(`select count(*) from private.game_imported_unclaimed_batch_members where character_id in ('${importedClaimCharacterId}', '${webClaimCharacterId}')`), '2')
    assert.equal(await sql(`select canonical_legacy_name || '|' || legacy_name_sha1 || '|' || legacy_shard from private.game_imported_unclaimed_batch_member_legacy_locators where character_id = '${importedClaimCharacterId}'`), `${importedClaimName}|${createHash('sha1').update(importedClaimName).digest('hex')}|${createHash('sha1').update(importedClaimName).digest('hex').slice(0, 2)}`)
    assert.equal(await sql(`select count(*) from private.game_imported_unclaimed_batch_members where character_id = '${missingMemberCharacterId}'`), '0')
    assert.equal(createHash('sha256').update(await readFile(importedClaimPlayer)).digest('hex'), importedClaimDigest)
    evidence.events.push({ case: 'disposable-legacy-import', result: 'real-importBatch-member-locator-exact-retry' })

    // RED/GREEN guard: /onboarding is not routable with the feature disabled,
    // therefore the C connector must remain untouched.
    let cConnects = 0
    const offConfig = loadConfig({ NODE_ENV: 'test', MUD_ONBOARDING_ENABLED: 'false', SUPABASE_URL: 'http://127.0.0.1:9999', SUPABASE_INTERNAL_REST_URL: 'http://127.0.0.1:9999', SUPABASE_SERVICE_ROLE_KEY: process.env.STACK_E2E_SERVICE_ROLE_JWT, MUD_ADMISSION_SECRET: admissionSecret, GATEWAY_INSTANCE_ID: `off-${process.pid}`, HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: '1', ALLOWED_ORIGINS: origin })
    const offGateway = createGateway(offConfig, { connectTcp: () => { cConnects += 1; throw new Error('flag-off must not connect') } })
    offGateway.server.listen(0, '127.0.0.1')
    await once(offGateway.server, 'listening')
    const off = new WebSocket(`${offGateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin })
    await waitForRejectedWebSocket(off)
    assert.equal(cConnects, 0)
    await offGateway.close()
    evidence.events.push({ case: 'flag-off', result: 'no-c-connect' })

    const mudPort = await choosePort()
    mud = startMud(process.env.STACK_E2E_BINARY, mudPort)
    await waitForMud(mudPort, mud)
    const config = loadConfig({ NODE_ENV: 'test', MUD_ONBOARDING_ENABLED: 'true', SUPABASE_URL: 'http://127.0.0.1:9999', SUPABASE_INTERNAL_REST_URL: 'http://127.0.0.1:0', SUPABASE_SERVICE_ROLE_KEY: process.env.STACK_E2E_SERVICE_ROLE_JWT, MUD_ADMISSION_SECRET: admissionSecret, GATEWAY_INSTANCE_ID: `stack-${process.pid}`, HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(mudPort), ALLOWED_ORIGINS: origin, AUTH_TIMEOUT_MS: '2000', TCP_CONNECT_TIMEOUT_MS: '3000', MUD_ADMISSION_TIMEOUT_MS: '3000' })
    // The REST origin is injected after the PostgREST host port is published
    // by the runner; this process-level override is replaced below.
    const restUrl = process.env.STACK_E2E_REST_URL
    assert.ok(restUrl && !restUrl.endsWith(':0'), 'runner must provide a PostgREST URL')
    config.supabaseInternalRestUrl = restUrl
    const authenticator = {
      verify: async (token: string) => {
        const sub = authenticatedSubjects.get(token)
        if (!sub) throw new Error('unknown deterministic stack browser token')
        return { sub, expiresAtMs: Date.now() + 120_000, claims: {} }
      },
    }
    const completionRpcErrors: unknown[] = []
    const diagnosticFetch: typeof fetch = async (input, init) => {
      const response = await fetch(input, init)
      const path = new URL(String(input)).pathname
      if (!response.ok && path.startsWith('/rpc/')) {
        const error = await response.clone().json().catch(() => ({})) as Record<string, unknown>
        completionRpcErrors.push({ path, status: response.status, code: error.code, message: error.message })
      }
      return response
    }
    const actualOnboardingAuthorizer = new SupabaseOnboardingAuthorizer(config, diagnosticFetch)
    let finalizeObservedSaved = false
    let provisioningDigest: string | undefined
    let completionPhase = 'not-started'
    const onboardingAuthorizer: OnboardingAuthorizer = {
      begin: (request) => actualOnboardingAuthorizer.begin(request),
      cancelUnreserved: (request) => actualOnboardingAuthorizer.cancelUnreserved(request),
      reserve: (request) => actualOnboardingAuthorizer.reserve(request),
      challenge: (request) => actualOnboardingAuthorizer.challenge(request),
      finalize: async (request) => {
        completionPhase = 'finalize-receipt-check'
        const savedReceipt = await readFile(join(fixture, 'onboarding-receipts', `${request.correlationId}.receipt`), 'utf8')
        assert.match(savedReceipt, /state=saved\n/)
        provisioningDigest = createHash('sha256').update(await readFile(legacyPlayerPath(canonicalName))).digest('hex')
        assert.equal(request.fileSha256, provisioningDigest)
        assert.match(savedReceipt, new RegExp(`saved_file_sha256=${provisioningDigest}\\n`))
        finalizeObservedSaved = true
        completionPhase = 'finalize-rpc'
        const result = await actualOnboardingAuthorizer.finalize(request)
        completionPhase = 'finalize-ok'
        return result
      },
      reconcile: async (request) => {
        completionPhase = 'reconcile-rpc'
        const result = await actualOnboardingAuthorizer.reconcile(request)
        completionPhase = 'reconcile-ok'
        return result
      },
      claim: (request) => actualOnboardingAuthorizer.claim(request),
      activateHandoff: async (request) => {
        completionPhase = 'activate-rpc'
        const result = await actualOnboardingAuthorizer.activateHandoff(request)
        completionPhase = 'activate-ok'
        return result
      },
      bindSnapshotCommand: async (request) => {
        completionPhase = 'bind-rpc'
        await actualOnboardingAuthorizer.bindSnapshotCommand(request)
        completionPhase = 'bind-ok'
      },
    }
    gateway = createGateway(config, { authenticator, characterAuthorizer: new SupabaseCharacterAuthorizer(config), onboardingAuthorizer })
    gateway.server.listen(0, '127.0.0.1')
    await once(gateway.server, 'listening')

    const cancelled = await openOnboarding(gateway.address(), cancelledCorrelation)
    await closeAndWait(cancelled.ws)
    await eventually(async () => assert.equal(await sql(`select status from private.game_character_onboarding_intents where correlation_id = '${cancelledCorrelation}'`), 'cancelled'))
    evidence.events.push({ case: 'early-cancel-retry', result: 'cancelled-then-new-correlation-allowed' })

    const browser = await openOnboarding(gateway.address(), correlation)
    browser.send(`${requestedName}\n`)
    await eventually(() => assert.match(browser.text(), /하시겠습니까/))
    browser.send('예\n')
    await eventually(() => assert.match(browser.text(), /\[엔터\]를 누르십시요/))
    browser.send('\n')
    await eventually(() => assert.match(browser.text(), /당신은 남자입니까/))
    await eventually(async () => assert.equal(await sql(`select i.status || '|' || p.status || '|' || c.lifecycle || '|' || c.legacy_name || '|' || c.legacy_name_key from private.game_character_onboarding_intents i join private.game_character_provisioning_requests p using (correlation_id) join public.game_characters c on c.id = p.character_id where i.correlation_id = '${correlation}'`), 'provisioning|reserved|provisioning|Stackhero|Stackhero'))
    browser.send('남\n')
    await eventually(() => assert.match(browser.text(), /직업을 고르세요/))
    browser.send('4\n')
    await eventually(() => assert.match(browser.text(), /능력:/))
    browser.send('12 10 12 10 10\n')
    await eventually(() => assert.match(browser.text(), /익숙한 무기를/))
    browser.send('1\n')
    await eventually(() => assert.match(browser.text(), /성향을 고르십시요/))
    browser.send('선\n')
    await eventually(() => assert.match(browser.text(), /종족/))
    browser.send('7\n')
    await eventually(() => assert.match(browser.text(), /새 암호를/))
    browser.send(`${password}\n`)
    try {
      await eventually(async () => assert.match(await readFile(join(fixture, 'onboarding-receipts', `${correlation}.receipt`), 'utf8'), /state=(saved|committed)\n/))
    } catch {
      const controls = browser.frames.filter(frame => !frame.binary).map(frame => frame.data.toString('utf8'))
      const journal = await readdir(join(fixture, 'character-save-journal'))
      const stage = await readdir(join(fixture, 'character-save-stage'))
      const shards = await readdir(join(fixture, 'player'))
      throw new Error(redact(`first save incomplete; C=${mudDiagnostics}; controls=${JSON.stringify(controls)}; journal=${JSON.stringify(journal)}; stage=${JSON.stringify(stage)}; playerDirectories=${JSON.stringify(shards)}; terminal=${browser.text()}`))
    }
    try {
      await eventually(() => assert.ok(browser.json('provisioned')))
    } catch {
      const dbState = await sql(`select i.status || '|' || p.status || '|' || c.lifecycle || '|' || coalesce(h.status, 'none') from private.game_character_onboarding_intents i join private.game_character_provisioning_requests p using (correlation_id) join public.game_characters c on c.id = p.character_id left join private.game_character_onboarding_handoffs h on h.correlation_id = i.correlation_id where i.correlation_id = '${correlation}'`)
      const headState = await sql(`select json_build_object('state', h.head_state, 'revision', h.revision, 'hasWriterEpoch', h.writer_epoch is not null, 'storageFormat', h.storage_format)::text from private.game_character_legacy_heads h join private.game_character_provisioning_requests p on p.character_id = h.character_id where p.correlation_id = '${correlation}'`)
      const journal = await readdir(join(fixture, 'character-save-journal'))
      throw new Error(redact(`provision completion missing; phase=${completionPhase}; rpc=${JSON.stringify(completionRpcErrors)}; db=${dbState}; head=${headState}; journal=${JSON.stringify(journal)}; C=${mudDiagnostics}; controls=${JSON.stringify(browser.frames.filter(frame => !frame.binary).map(frame => frame.data.toString('utf8')))}`))
    }
    assert.equal(finalizeObservedSaved, true)

    const player = join(fixture, 'player', createHash('sha1').update(canonicalName).digest('hex').slice(0, 2), canonicalName)
    const digest = createHash('sha256').update(await readFile(player)).digest('hex')
    const characterId = await sql(`select character_id from private.game_character_provisioning_requests where correlation_id = '${correlation}'`)
    const state = await sql(`select i.status || '|' || p.status || '|' || c.lifecycle || '|' || p.saved_file_sha256 || '|' || c.owner_user_id || '|' || c.legacy_name || '|' || c.legacy_name_key from private.game_character_onboarding_intents i join private.game_character_provisioning_requests p using (correlation_id) join public.game_characters c on c.id = p.character_id where i.correlation_id = '${correlation}'`)
    // ACTIVATED performs another explicit save. The immutable onboarding
    // receipt proves the first generation, not the current mutable file.
    assert.ok(provisioningDigest)
    assert.equal(state, `finalized|finalized|active|${provisioningDigest}|${actor}|Stackhero|Stackhero`)
    const currentHead = await sql(`select head_state || '|' || head_sha256 || '|' || revision from private.game_character_legacy_heads where character_id = '${characterId}'`)
    const [headState, headDigest, headRevision] = currentHead.split('|')
    assert.equal(headState, 'existing')
    assert.equal(headDigest, digest)
    assert.ok(Number(headRevision) >= 2, 'activation must acknowledge a later save generation')
    await assert.rejects(
      () => new SupabaseOnboardingAuthorizer(config).finalize({ actorUserId: actor, correlationId: correlation, characterId, fileSha256: 'f'.repeat(64), storageFormat: 'player-v1' }),
      /onboarding authorization was refused/
    )
    assert.equal(await sql(`select status || '|' || lifecycle from private.game_character_provisioning_requests p join public.game_characters c on c.id = p.character_id where p.correlation_id = '${correlation}'`), 'finalized|active')
    const receipt = await readFile(join(fixture, 'onboarding-receipts', `${correlation}.receipt`), 'utf8')
    assert.match(receipt, /state=committed\n/)
    assert.match(receipt, new RegExp(`saved_file_sha256=${provisioningDigest}\\n`))
    assert.match(receipt, new RegExp(`canonical_name_hex=${Buffer.from(canonicalName, 'utf8').toString('hex')}\\n`))
    assert.doesNotMatch(receipt, new RegExp(`${password}|${admissionSecret}|${accessToken}`))
    // Onboarding closes normally after ACTIVE/binding. Gameplay acquires a
    // separate /ws session, as the web client does after provisioning.
    await closeAndWait(browser.ws)
    const provisionedGame = await openGame(gateway.address(), characterId)
    provisionedGame.send('건강\n')
    await eventually(() => assert.match(provisionedGame.text(), /체력/))

    // A second browser cannot acquire the same DB-backed lease while gameplay
    // is still connected; closing the first socket must release it.
    const duplicate = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin })
    await once(duplicate, 'open')
    duplicate.send(JSON.stringify({ type: 'auth', accessToken, characterId: (await sql(`select character_id from private.game_character_provisioning_requests where correlation_id = '${correlation}'`)) }))
    const [duplicateCloseCode] = await once(duplicate, 'close') as [number]
    assert.equal(duplicateCloseCode, 1008)
    await closeAndWait(provisionedGame.ws)
    await eventually(async () => assert.equal(await sql(`select count(*) from private.game_character_sessions where character_id = '${characterId}'`), '0'))
    evidence.events.push({ case: 'wrong-db-hash', result: 'actual-RPC-rejected-without-state-change' })
    evidence.events.push({ case: 'real-stack-provision', result: 'intent-reservation-finalized-file-sha-receipt-game-command' })
    evidence.events.push({ case: 'lease', result: 'duplicate-rejected-release-on-close' })

    // Fault injection is above the transport only: both finalize attempts get
    // a transient RPC failure. The Gateway must fail before writing MUD1O
    // COMMIT, leaving the reservation provisioning rather than publishing a
    // partially verified player.
    await gateway.close()
    gateway = undefined
    await stopMud(mud)
    mud = undefined
    evidence.events.push({ case: 'mud-process-exit', result: 'SIGTERM-exit-0' })
    const badMudPort = await choosePort()
    mud = startMud(process.env.STACK_E2E_BINARY, badMudPort)
    await waitForMud(badMudPort, mud)
    const badConfig = loadConfig({ NODE_ENV: 'test', MUD_ONBOARDING_ENABLED: 'true', SUPABASE_URL: 'http://127.0.0.1:9999', SUPABASE_INTERNAL_REST_URL: restUrl, SUPABASE_SERVICE_ROLE_KEY: process.env.STACK_E2E_SERVICE_ROLE_JWT, MUD_ADMISSION_SECRET: admissionSecret, GATEWAY_INSTANCE_ID: `bad-${process.pid}`, HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(badMudPort), ALLOWED_ORIGINS: origin, AUTH_TIMEOUT_MS: '2000', TCP_CONNECT_TIMEOUT_MS: '3000', MUD_ADMISSION_TIMEOUT_MS: '3000' })
    let badFinalizeRpcCalls = 0
    const badFetch: typeof fetch = async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      // The Gateway may reconcile an indeterminate finalize response. Fail
      // both RPC attempts before PostgREST sees them so this is a genuine
      // finalize outage, not a successful reconciliation that permits COMMIT.
      if (url.endsWith('/rpc/finalize_game_character_provisioning') || url.endsWith('/rpc/reconcile_game_character_provisioning')) {
        badFinalizeRpcCalls += 1
        return new Response('{"error":"injected finalize outage"}', { status: 503, headers: { 'content-type': 'application/json' } })
      }
      return fetch(input, init)
    }
    gateway = createGateway(badConfig, {
      authenticator,
      characterAuthorizer: new SupabaseCharacterAuthorizer(badConfig),
      onboardingAuthorizer: new SupabaseOnboardingAuthorizer(badConfig, badFetch),
    })
    gateway.server.listen(0, '127.0.0.1')
    await once(gateway.server, 'listening')
    const bad = await openOnboarding(gateway.address(), badCorrelation)
    bad.send(`${badRequestedName}\n`)
    await eventually(() => assert.match(bad.text(), /하시겠습니까/))
    bad.send('예\n')
    await eventually(() => assert.match(bad.text(), /\[엔터\]를 누르십시요/))
    bad.send('\n')
    await eventually(() => assert.match(bad.text(), /당신은 남자입니까/))
    bad.send('남\n')
    await eventually(() => assert.match(bad.text(), /직업을 고르세요/))
    bad.send('4\n')
    await eventually(() => assert.match(bad.text(), /능력:/))
    bad.send('12 10 12 10 10\n')
    await eventually(() => assert.match(bad.text(), /익숙한 무기를/))
    bad.send('1\n')
    await eventually(() => assert.match(bad.text(), /성향을 고르십시요/))
    bad.send('선\n')
    await eventually(() => assert.match(bad.text(), /종족/))
    bad.send('7\n')
    await eventually(() => assert.match(bad.text(), /새 암호를/))
    bad.send(`${password}\n`)
    await eventually(async () => assert.equal(await sql(`select i.status || '|' || p.status || '|' || c.lifecycle || '|' || c.legacy_name || '|' || c.legacy_name_key from private.game_character_onboarding_intents i join private.game_character_provisioning_requests p using (correlation_id) join public.game_characters c on c.id = p.character_id where i.correlation_id = '${badCorrelation}'`), 'provisioning|reserved|provisioning|Stackbad|Stackbad'))
    const badPlayer = join(fixture, 'player', createHash('sha1').update(badCanonicalName).digest('hex').slice(0, 2), badCanonicalName)
    let badDigest = ''
    let badReceipt = ''
    await eventually(async () => {
      badDigest = createHash('sha256').update(await readFile(badPlayer)).digest('hex')
      badReceipt = await readFile(join(fixture, 'onboarding-receipts', `${badCorrelation}.receipt`), 'utf8')
      assert.match(badReceipt, /state=saved\n/)
    })
    assert.equal(bad.json('provisioned'), false)
    const badState = await sql(`select i.status || '|' || p.status || '|' || c.lifecycle || '|' || coalesce(p.saved_file_sha256, '<null>') || '|' || c.owner_user_id || '|' || c.legacy_name || '|' || c.legacy_name_key from private.game_character_onboarding_intents i join private.game_character_provisioning_requests p using (correlation_id) join public.game_characters c on c.id = p.character_id where i.correlation_id = '${badCorrelation}'`)
    assert.equal(badState, `provisioning|reserved|provisioning|<null>|${actor}|Stackbad|Stackbad`)
    const badCharacterId = await sql(`select character_id from private.game_character_provisioning_requests where correlation_id = '${badCorrelation}'`)
    assert.doesNotMatch(badReceipt, /state=committed\n/)
    assert.match(badReceipt, new RegExp(`canonical_name_hex=${Buffer.from(badCanonicalName, 'utf8').toString('hex')}\\n`))
    assert.match(badReceipt, new RegExp(`saved_file_sha256=${badDigest}\\n`))
    assert.equal(badFinalizeRpcCalls, 2)
    // Preserve the exact saved-file bytes for the crash-window evidence. A
    // graceful C shutdown may flush the still-staged onboarding creature and
    // legitimately change the file after the receipt hash was recorded.
    await crashMud(mud)
    mud = undefined
    evidence.events.push({ case: 'mud-process-crash', result: 'SIGKILL-preserved-saved-receipt-bytes' })
    await terminateAndWait(bad.ws)
    // The fault-injection Gateway is no longer used after the crash window;
    // close it before replacing the handle with the recovery Gateway.
    await gateway.close()
    gateway = undefined
    evidence.events.push({ case: 'finalize-failure', result: 'rpc-outage-saved-no-COMMIT-no-state-publish' })

    // RED/GREEN recovery gate: a saved C receipt is durable evidence after the
    // Gateway outage. A fresh reconciler process must promote exactly that
    // reservation before a regular /ws admission can be issued.
    // The reconciler intentionally accepts only private roots. The C writer
    // must already have emitted 0700 shard directories and 0600 player files;
    // only the copied disposable roots are hardened to mirror the Helm volume
    // bootstrap.
    await hardenPlayerDirectories(canonicalName, badCanonicalName)
    process.stderr.write('stack-e2e: recovery-start\n')
    await runSingleRecoveryReceipt(restUrl)
    process.stderr.write('stack-e2e: recovery-rpc-green\n')
    const recoveredState = await sql(`select i.status || '|' || p.status || '|' || c.lifecycle || '|' || p.saved_file_sha256 || '|' || c.owner_user_id from private.game_character_onboarding_intents i join private.game_character_provisioning_requests p using (correlation_id) join public.game_characters c on c.id = p.character_id where i.correlation_id = '${badCorrelation}'`)
    assert.equal(recoveredState, `finalized|finalized|active|${badDigest}|${actor}`)
    evidence.events.push({ case: 'saved-receipt-recovery', result: 'fresh-reconciler-promoted-active' })

    const recoveredMudPort = await choosePort()
    mud = startMud(process.env.STACK_E2E_BINARY, recoveredMudPort)
    await waitForMud(recoveredMudPort, mud)
    const recoveredConfig = loadConfig({ NODE_ENV: 'test', MUD_ONBOARDING_ENABLED: 'true', SUPABASE_URL: 'http://127.0.0.1:9999', SUPABASE_INTERNAL_REST_URL: restUrl, SUPABASE_SERVICE_ROLE_KEY: process.env.STACK_E2E_SERVICE_ROLE_JWT, MUD_ADMISSION_SECRET: admissionSecret, GATEWAY_INSTANCE_ID: `recover-${process.pid}`, HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(recoveredMudPort), ALLOWED_ORIGINS: origin, AUTH_TIMEOUT_MS: '2000', TCP_CONNECT_TIMEOUT_MS: '3000', MUD_ADMISSION_TIMEOUT_MS: '3000' })
    gateway = createGateway(recoveredConfig, { authenticator, characterAuthorizer: new SupabaseCharacterAuthorizer(recoveredConfig), onboardingAuthorizer: new SupabaseOnboardingAuthorizer(recoveredConfig) })
    gateway.server.listen(0, '127.0.0.1')
    await once(gateway.server, 'listening')
    process.stderr.write('stack-e2e: recovery-gateway-listening\n')
    const recoveredGame = await openGame(gateway.address(), badCharacterId)
    process.stderr.write('stack-e2e: recovery-ws-ready\n')
    recoveredGame.send('건강\n')
    await eventually(() => assert.match(recoveredGame.text(), /체력/))
    await closeAndWait(recoveredGame.ws)
    await eventually(async () => assert.equal(await sql(`select count(*) from private.game_character_sessions where character_id = '${badCharacterId}'`), '0'))
    await gateway.close()
    gateway = undefined
    await stopMud(mud)
    mud = undefined
    process.stderr.write('stack-e2e: recovery-game-green\n')
    evidence.events.push({ case: 'recovered-mud1-admission', result: 'regular-ws-and-game-command' })

    // Claim gate: use the C-created files that the real importer admitted
    // above.  No lifecycle or ownership value is relabelled in this fixture.
    // The positive target carries the immutable batch/member/locator proof;
    // the orphan was deliberately imported through the legacy non-ledger path
    // to prove that a C-visible file alone cannot be claimed.
    const claimMudPort = await choosePort()
    mud = startMud(process.env.STACK_E2E_BINARY, claimMudPort)
    await waitForMud(claimMudPort, mud)
    const claimConfig = loadConfig({ NODE_ENV: 'test', MUD_ONBOARDING_ENABLED: 'true', SUPABASE_URL: 'http://127.0.0.1:9999', SUPABASE_INTERNAL_REST_URL: restUrl, SUPABASE_SERVICE_ROLE_KEY: process.env.STACK_E2E_SERVICE_ROLE_JWT, MUD_ADMISSION_SECRET: admissionSecret, GATEWAY_INSTANCE_ID: `claim-${process.pid}`, HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(claimMudPort), ALLOWED_ORIGINS: origin, AUTH_TIMEOUT_MS: '2000', TCP_CONNECT_TIMEOUT_MS: '3000', MUD_ADMISSION_TIMEOUT_MS: '3000' })
    const claimAuthorizer = new SupabaseOnboardingAuthorizer(claimConfig)
    let claimCompletionPhase = 'before-claim'
    const expiringClaimAuthorizer: OnboardingAuthorizer = {
      begin: (request) => claimAuthorizer.begin(request),
      cancelUnreserved: (request) => claimAuthorizer.cancelUnreserved(request),
      reserve: (request) => claimAuthorizer.reserve(request),
      challenge: async (request) => {
        const challenge = await claimAuthorizer.challenge(request)
        if (request.correlationId === expiredClaimCorrelation) {
          // Preserve the table's exact 90-second invariant while making this
          // disposable challenge stale before C can submit the verified
          // password. This mutates no character lifecycle or ownership data.
          await sql(`update private.game_character_claim_attempts set allowed_at = now() - interval '91 seconds', allow_expires_at = now() - interval '1 second' where correlation_id = '${expiredClaimCorrelation}'`)
        }
        return challenge
      },
      finalize: (request) => claimAuthorizer.finalize(request),
      reconcile: (request) => claimAuthorizer.reconcile(request),
      claim: async (request) => {
        claimCompletionPhase = 'claim-rpc-start'
        const result = await claimAuthorizer.claim(request)
        claimCompletionPhase = 'claim-rpc-ok'
        return result
      },
      activateHandoff: async (request) => {
        claimCompletionPhase = 'activation-rpc-start'
        const result = await claimAuthorizer.activateHandoff(request)
        claimCompletionPhase = 'activation-rpc-ok'
        return result
      },
      bindSnapshotCommand: async (request) => {
        claimCompletionPhase = 'binding-rpc-start'
        await claimAuthorizer.bindSnapshotCommand(request)
        claimCompletionPhase = 'binding-rpc-ok'
      },
    }
    gateway = createGateway(claimConfig, { authenticator, characterAuthorizer: new SupabaseCharacterAuthorizer(claimConfig), onboardingAuthorizer: expiringClaimAuthorizer })
    gateway.server.listen(0, '127.0.0.1')
    await once(gateway.server, 'listening')
    process.stderr.write('stack-e2e: claim-gateway-listening\n')

    const missingMember = await openOnboarding(gateway.address(), missingMemberCorrelation, 'claim')
    missingMember.send(`${missingMemberName}\n`)
    await eventually(() => assert.ok(missingMember.json('error')))
    assert.equal(missingMember.text().includes('암호를 넣어 주십시요'), false)
    assert.equal(await sql(`select lifecycle || '|' || coalesce(owner_user_id::text, '<null>') from public.game_characters where id = '${missingMemberCharacterId}'`), 'imported_unclaimed|<null>')
    await assertNormalAdmissionDenied(gateway.address(), missingMemberCharacterId)
    await closeAndWait(missingMember.ws)
    await eventually(async () => assert.equal(await sql(`select status from private.game_character_onboarding_intents where correlation_id = '${missingMemberCorrelation}'`), 'cancelled'))

    const wrongPassword = await openOnboarding(gateway.address(), wrongPasswordCorrelation, 'claim')
    wrongPassword.send(`${importedClaimName}\n`)
    await eventually(() => assert.match(wrongPassword.text(), /암호를 넣어 주십시요/))
    wrongPassword.send('wrong-password\n')
    await eventually(() => assert.ok(wrongPassword.json('error')))
    assert.equal(await sql(`select lifecycle || '|' || coalesce(owner_user_id::text, '<null>') from public.game_characters where id = '${importedClaimCharacterId}'`), 'imported_unclaimed|<null>')
    assert.equal(createHash('sha256').update(await readFile(importedClaimPlayer)).digest('hex'), importedClaimDigest)
    await assertNormalAdmissionDenied(gateway.address(), importedClaimCharacterId)
    await closeAndWait(wrongPassword.ws)
    await eventually(async () => assert.equal(await sql(`select status from private.game_character_onboarding_intents where correlation_id = '${wrongPasswordCorrelation}'`), 'cancelled'))
    assert.equal(await sql(`select count(*) from private.game_character_claim_attempts where correlation_id = '${wrongPasswordCorrelation}' and claimed_at is null`), '1')

    const expiredClaim = await openOnboarding(gateway.address(), expiredClaimCorrelation, 'claim')
    expiredClaim.send(`${importedClaimName}\n`)
    await eventually(() => assert.match(expiredClaim.text(), /암호를 넣어 주십시요/))
    expiredClaim.send(`${password}\n`)
    await eventually(() => assert.ok(expiredClaim.json('error')))
    assert.equal(await sql(`select lifecycle || '|' || coalesce(owner_user_id::text, '<null>') from public.game_characters where id = '${importedClaimCharacterId}'`), 'imported_unclaimed|<null>')
    assert.equal(createHash('sha256').update(await readFile(importedClaimPlayer)).digest('hex'), importedClaimDigest)
    await assertNormalAdmissionDenied(gateway.address(), importedClaimCharacterId)
    await closeAndWait(expiredClaim.ws)
    await eventually(async () => assert.equal(await sql(`select status from private.game_character_onboarding_intents where correlation_id = '${expiredClaimCorrelation}'`), 'cancelled'))
    assert.equal(await sql(`select count(*) from private.game_character_claim_attempts where correlation_id = '${expiredClaimCorrelation}' and claimed_at is null`), '1')

    const claim = await openOnboarding(gateway.address(), importedClaimCorrelation, 'claim')
    claim.send(`${importedClaimName}\n`)
    await eventually(() => assert.match(claim.text(), /암호를 넣어 주십시요/))
    claim.send(`${password}\n`)
    try {
      await eventually(() => assert.ok(claim.json('claimed')))
    } catch (error) {
      const state = await sql(`select i.status || '|' || c.lifecycle from private.game_character_onboarding_intents i join public.game_characters c on c.id = '${importedClaimCharacterId}' where i.correlation_id = '${importedClaimCorrelation}'`)
      process.stderr.write(`stack-e2e: positive-claim phase=${claimCompletionPhase} state=${state} error-frame=${claim.json('error')} socket=${claim.ws.readyState}\n`)
      const headCount = await sql(`select count(*) from private.game_character_legacy_heads where character_id = '${importedClaimCharacterId}'`)
      const saveFailures = mudDiagnostics.match(/M3 player save failed: step=[a-z-]+ cutpoint=[0-9]+/g) ?? []
      process.stderr.write(`stack-e2e: positive-claim heads=${headCount} save-failures=${JSON.stringify(saveFailures)}\n`)
      throw error
    }
    process.stderr.write('stack-e2e: claim-rpc-green\n')
    await closeAndWait(claim.ws)
    const claimState = await sql(`select i.status || '|' || c.lifecycle || '|' || c.owner_user_id || '|' || c.legacy_name_key from private.game_character_onboarding_intents i join public.game_characters c on c.id = '${importedClaimCharacterId}' where i.correlation_id = '${importedClaimCorrelation}'`)
    assert.equal(claimState, `finalized|active|${actor}|${importedClaimName}`)
    assert.equal(createHash('sha256').update(await readFile(importedClaimPlayer)).digest('hex'), importedClaimDigest)
    evidence.events.push({ case: 'legacy-claim', result: 'batch-provenanced-C-password-verified-rpc-handoff-active' })
    evidence.events.push({ case: 'legacy-claim-denials', result: 'missing-member-wrong-password-expired-no-owner-no-normal-admission' })

    // A subsequent claim against the now-active target must be denied at the
    // challenge RPC. C must not receive ALLOW or expose its password prompt,
    // and the existing owner/lifecycle must remain unchanged.
    const denied = await openOnboarding(gateway.address(), denialCorrelation, 'claim')
    denied.send(`${importedClaimName}\n`)
    await eventually(() => assert.ok(denied.json('error')))
    assert.equal(denied.text().includes('암호를 넣어 주십시요'), false)
    const deniedState = await sql(`select lifecycle || '|' || c.owner_user_id from public.game_characters c where c.id = '${importedClaimCharacterId}'`)
    assert.equal(deniedState, `active|${actor}`)
    await closeAndWait(denied.ws)
    evidence.events.push({ case: 'legacy-claim-denial', result: 'challenge-denied-owner-unchanged' })

    // The claimed row must immediately use the unchanged normal MUD1 lane.
    process.stderr.write('stack-e2e: claim-before-mud1\n')
    const claimedGame = await openGame(gateway.address(), importedClaimCharacterId)
    process.stderr.write('stack-e2e: claim-mud1-ready\n')
    claimedGame.send('건강\n')
    await eventually(() => assert.match(claimedGame.text(), /체력/))
    await closeAndWait(claimedGame.ws)
    await eventually(async () => assert.equal(await sql(`select count(*) from private.game_character_sessions where character_id = '${importedClaimCharacterId}'`), '0'))
    process.stderr.write('stack-e2e: claim-game-green\n')
    evidence.events.push({ case: 'claimed-mud1-admission', result: 'regular-ws-and-game-command' })

    // Browser acceptance stays inside this runner's disposable resources. The
    // UI receives a deterministic Auth response only because PostgREST is not
    // an Auth server; every roster read, onboarding frame, C prompt, lifecycle
    // transition, and subsequent ordinary MUD admission is otherwise live.
    const webPort = await choosePort()
    web = startWebStackServer({
      gatewayUrl: `${gateway.address().replace('http:', 'ws:')}/ws`,
      port: webPort,
      root,
      supabaseUrl: restUrl,
      supabasePublishableKey: webProvisionJwt,
    })
    await waitForWebStackServer(web)
    await runWebStackAcceptance({
      server: web,
      provision: {
        accessToken: webProvisionJwt,
        alignment: '선',
        characterClass: '4',
        characterName: webProvisionName,
        email: 'web-provision@example.test',
        gamePassword: password,
        gender: '남',
        race: '7',
        stats: '12 10 12 10 10',
        userId: webProvisionActor,
        weapon: '1',
      },
      claim: {
        accessToken: webClaimJwt,
        characterName: webClaimName,
        email: 'web-claim@example.test',
        gamePassword: password,
        userId: webClaimActor,
      },
    })
    const webProvisionState = await sql(`select lifecycle || '|' || owner_user_id || '|' || legacy_name from public.game_characters where owner_user_id = '${webProvisionActor}'`)
    assert.equal(webProvisionState, `active|${webProvisionActor}|${webProvisionName}`)
    const webClaimState = await sql(`select lifecycle || '|' || owner_user_id || '|' || legacy_name from public.game_characters where id = '${webClaimCharacterId}'`)
    assert.equal(webClaimState, `active|${webClaimActor}|${webClaimName}`)
    assert.equal(await sql(`select count(*) from private.game_imported_unclaimed_batch_members where character_id = '${webClaimCharacterId}'`), '1')
    assert.equal(await sql(`select canonical_legacy_name || '|' || legacy_name_sha1 || '|' || legacy_shard from private.game_imported_unclaimed_batch_member_legacy_locators where character_id = '${webClaimCharacterId}'`), `${webClaimName}|${createHash('sha1').update(webClaimName).digest('hex')}|${createHash('sha1').update(webClaimName).digest('hex').slice(0, 2)}`)
    assert.equal(createHash('sha256').update(await readFile(webClaimPlayer)).digest('hex'), webClaimDigest)
    await eventually(async () => assert.equal(await sql(`select count(*) from private.game_character_sessions where character_id in (select id from public.game_characters where owner_user_id in ('${webProvisionActor}', '${webClaimActor}'))`), '0'))

    // The browser has now exercised distinct real provision and claim flows.
    // Resolve their server-issued correlations from the immutable intent rows,
    // rather than inventing a client-side identity for either flow.
    const webProvisionCorrelation = await sql(`select correlation_id from private.game_character_onboarding_intents where actor_user_id = '${webProvisionActor}' and mode = 'provision' order by created_at desc limit 1`)
    const webClaimCorrelation = await sql(`select correlation_id from private.game_character_onboarding_intents where actor_user_id = '${webClaimActor}' and mode = 'claim' order by created_at desc limit 1`)
    assert.match(webProvisionCorrelation, /^[0-9a-f-]{36}$/)
    assert.match(webClaimCorrelation, /^[0-9a-f-]{36}$/)

    // M4 records the immutable manifest/artifact pair before the dedicated
    // fulfillment pass consumes it. Retrying both passes is intentional: the
    // exact same command evidence must remain an idempotent replay.
    const secretValues = [password, accessToken, admissionSecret, webProvisionJwt, webClaimJwt]
    await eventually(async () => {
      await relayM3Artifacts()
      await assertM3OnboardingEvidence({ actorUserId: webProvisionActor, correlationId: webProvisionCorrelation, expectedMode: 'provision', secrets: secretValues })
      await assertM3OnboardingEvidence({ actorUserId: webClaimActor, correlationId: webClaimCorrelation, expectedMode: 'claim', secrets: secretValues })
    }, 45_000)
    const replay = await relayM3Artifacts()
    assert.ok((replay.manifest.exactRetry ?? 0) >= 2, 'manifest relay must exact-retry the browser provision and claim evidence')
    assert.ok((replay.manifest.normalizedProjectionExactRetry ?? 0) >= 2, 'normalized persistence must exact-retry both real C onboarding snapshots')
    assert.ok((replay.artifact.exactRetry ?? 0) >= 2, 'artifact relay must exact-retry the browser provision and claim evidence')
    assert.ok((replay.artifact.fulfillmentExactRetry ?? 0) >= 2, 'fulfillment relay must exact-retry the browser provision and claim evidence')

    const provisionEvidence = await assertM3OnboardingEvidence({ actorUserId: webProvisionActor, correlationId: webProvisionCorrelation, expectedMode: 'provision', secrets: secretValues })
    const claimEvidence = await assertM3OnboardingEvidence({ actorUserId: webClaimActor, correlationId: webClaimCorrelation, expectedMode: 'claim', secrets: secretValues })
    const missingCommand = '00000000-0000-4000-8000-000000000000'
    assert.equal(await writerSql(`select outcome from private.fulfill_game_character_onboarding_snapshot_eligibility('${provisionEvidence.characterId}', '${missingCommand}')`), 'NOT_ELIGIBLE', 'missing command evidence must not fulfill the outbox')
    assert.equal(await writerSql(`select outcome from private.fulfill_game_character_onboarding_snapshot_eligibility('${provisionEvidence.characterId}', '${claimEvidence.commandId}')`), 'NOT_ELIGIBLE', 'wrong command evidence must not fulfill another correlation')
    await assert.rejects(
      () => sql(`select outcome from public.register_game_character_onboarding_snapshot_command_binding('${webProvisionActor}', '${webProvisionCorrelation}', '${provisionEvidence.characterId}', 'provision', '${missingCommand}')`),
      'a duplicate correlation cannot substitute a second immutable command binding',
    )
    assert.equal(await sql(`select count(*) from private.game_character_onboarding_snapshot_command_bindings where correlation_id in ('${webProvisionCorrelation}', '${webClaimCorrelation}')`), '2', 'each browser correlation must retain exactly one command binding')
    assert.equal(await writerSql(`select outcome from private.fulfill_game_character_onboarding_snapshot_eligibility('${claimEvidence.characterId}', '${provisionEvidence.commandId}')`), 'NOT_ELIGIBLE', 'artifact substitution across the provision/claim boundary must fail closed')
    assertNoPlaintextSecrets('redacted stack evidence', JSON.stringify(evidence), secretValues)
    evidence.events.push({ case: 'web-ui-provision-and-claim-m3', result: 'real-next-xterm-provision-and-legacy-claim-bound-before-receipt-one-artifact-manifest-fulfilled-exact-retry-secret-free' })
    evidence.status = 'passed'
  } catch (error) {
    scenarioFailed = true
    throw error
  } finally {
    // A web cleanup failure must not skip Gateway/MUD teardown or suppress
    // evidence. If the scenario already failed, retain that original failure;
    // otherwise surface the recorded teardown failure after all cleanup runs.
    const cleanupFailures = await runCleanupSteps([
      {
        name: 'web',
        run: async () => {
          process.stderr.write(`stack-e2e: finally-web-${web ? 'start' : 'none'}\n`)
          if (web) await stopWebStackServer(web)
          process.stderr.write('stack-e2e: finally-web-done\n')
        },
      },
      {
        name: 'gateway',
        run: async () => {
          process.stderr.write(`stack-e2e: finally-gateway-${gateway ? 'start' : 'none'}\n`)
          if (gateway) await closeGatewayBounded(gateway)
          process.stderr.write('stack-e2e: finally-gateway-done\n')
        },
      },
      {
        name: 'mud',
        run: async () => {
          process.stderr.write(`stack-e2e: finally-mud-${mud ? 'start' : 'none'}\n`)
          if (mud) await stopMudDuringFailure(mud)
          process.stderr.write('stack-e2e: finally-mud-done\n')
        },
      },
    ])
    if (cleanupFailures.length > 0) evidence.status = 'failed'
    evidence.error = evidence.status === 'passed'
      ? undefined
      : 'stack-e2e failed; inspect redacted runner output'
    const artifact = process.env.STACK_E2E_ARTIFACT
    if (artifact) {
      cleanupFailures.push(...await runCleanupSteps([{
        name: 'evidence',
        run: () => writeFile(artifact, redact(JSON.stringify(evidence, null, 2)), 'utf8'),
      }]))
    }
    const teardownFailure = cleanupFailure(cleanupFailures)
    if (!scenarioFailed && teardownFailure) throw teardownFailure
  }
}

// Keep the runner-owned Promise in node:test's lifecycle. A bare main().catch
// leaves failures and cleanup outside the test worker's awaited graph, which
// can report a pending Promise after the event loop becomes idle.
test('real stack E2E completes with deterministic cleanup', { timeout: 240_000 }, async () => {
  await main()
})
