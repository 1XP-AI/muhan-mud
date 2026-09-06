import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { EventEmitter, once } from 'node:events'
import test from 'node:test'
import WebSocket, { type RawData } from 'ws'
import type { AuthorizedCharacter, BeginCharacterSessionRequest, CharacterAuthorizer, RenewCharacterSessionRequest, RenewedCharacterSession } from '../src/character-authorizer.js'
import { loadConfig } from '../src/config.js'
import { createGateway, type EvidenceFinalizer } from '../src/gateway.js'
import { OnboardingAuthorizationError, type ChallengeOnboardingRequest, type ChallengeOnboardingResult, type ClaimOnboardingRequest, type FinalizeOnboardingRequest, type OnboardingAuthorizer, type ReserveOnboardingRequest } from '../src/onboarding-authorizer.js'
import { formatOnboardingEvidenceControl } from '../src/onboarding-protocol.js'
import type { FinalizeLegacyIdentityEvidenceRequest } from '../src/evidence-finalizer.js'
import type { LegacyIdentityEvidenceV1 } from '../src/evidence-codec/legacy-identity-evidence-v1.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const correlation = '123e4567-e89b-12d3-a456-426614174001'
const character = '123e4567-e89b-12d3-a456-426614174002'

class HeldCommitMudSocket extends EventEmitter {
  destroyed = false
  readonly writes: Buffer[] = []
  private commitCallback?: (error?: Error | null) => void

  connect(): void { queueMicrotask(() => this.emit('connect')) }
  write(data: Uint8Array | string, callback?: (error?: Error | null) => void): boolean {
    const frame = Buffer.from(data)
    const text = frame.toString()
    this.writes.push(frame)
    if (text.startsWith('MUD1O|P|')) {
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from('MUD1O OK\n이름? ')))
    } else if (text === 'Hero\n') {
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from('MUD1O RESERVE|4865726f\n')))
    } else if (text === `MUD1O RESERVED|${character}\n`) {
      callback?.()
      setImmediate(() => this.emit('data', Buffer.from('성별? ')))
    } else if (text === 'm\n') {
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from(`MUD1O SAVED|${character}|${'f'.repeat(64)}|player-v1\n`)))
    } else if (/^MUD1O ACTIVATED\|[0-9a-f-]+\n$/.test(text)) {
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from(text.replace('ACTIVATED', 'ACTIVE'))))
    } else if (text === 'MUD1O COMMIT\n') {
      this.commitCallback = callback
    } else callback?.()
    return true
  }
  releaseCommit(): void { this.commitCallback?.(); this.commitCallback = undefined }
  end(): this { return this }
  destroy(): this { this.destroyed = true; return this }
}

class AdmissionMudSocket extends EventEmitter {
  destroyed = false
  readonly tickets: Buffer[]
  constructor(tickets: Buffer[]) { super(); this.tickets = tickets }
  connect(): void { queueMicrotask(() => this.emit('connect')) }
  write(data: Uint8Array | string, callback?: (error?: Error | null) => void): boolean {
    this.tickets.push(Buffer.from(data))
    callback?.()
    queueMicrotask(() => this.emit('data', Buffer.from('MUD1 OK\n')))
    return true
  }
  end(): this { return this }
  destroy(): this { this.destroyed = true; return this }
}

class HeldEvidenceCompletionMudSocket extends EventEmitter {
  destroyed = false
  readonly writes: Buffer[] = []
  private stage = 0
  private completionCallback?: (error?: Error | null) => void

  constructor(
    private readonly mode: 'provision' | 'claim',
    private readonly completionEvidence: LegacyIdentityEvidenceV1,
    private readonly trace?: string[],
  ) { super() }
  connect(): void { queueMicrotask(() => this.emit('connect')) }
  write(data: Uint8Array | string, callback?: (error?: Error | null) => void): boolean {
    const frame = Buffer.from(data)
    const text = frame.toString()
    this.writes.push(frame)
    if (text.startsWith(this.mode === 'provision' ? 'MUD1O|P|' : 'MUD1O|C|')) {
      this.stage = 1; callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from(this.mode === 'provision' ? 'MUD1O OK\n이름? ' : 'MUD1O OK\n기존 이름? ')))
    } else if (this.mode === 'provision' && this.stage === 1 && text === 'Hero\n') {
      this.stage = 2; callback?.(); queueMicrotask(() => this.emit('data', Buffer.from('MUD1O RESERVE|4865726f\n')))
    } else if (this.mode === 'provision' && this.stage === 2 && text === `MUD1O RESERVED|${character}\n`) {
      this.stage = 3; callback?.(); queueMicrotask(() => this.emit('data', Buffer.from('성별? ')))
    } else if (this.mode === 'provision' && this.stage === 3 && text === 'm\n') {
      this.stage = 4; callback?.(); queueMicrotask(() => this.emit('data', formatOnboardingEvidenceControl(this.completionEvidence)))
    } else if (this.mode === 'claim' && this.stage === 1 && text === 'Alice\n') {
      this.stage = 2; callback?.(); queueMicrotask(() => this.emit('data', Buffer.from(`MUD1O CHALLENGE|416c696365|${'b'.repeat(64)}\n`)))
    } else if (this.mode === 'claim' && this.stage === 2 && text === 'MUD1O ALLOW\n') {
      this.stage = 3; callback?.(); queueMicrotask(() => this.emit('data', Buffer.from('비밀번호? ')))
    } else if (this.mode === 'claim' && this.stage === 3 && text === 'old-secret\n') {
      this.stage = 4; callback?.(); queueMicrotask(() => this.emit('data', formatOnboardingEvidenceControl(this.completionEvidence)))
    } else if (/^MUD1O ACTIVATED\|[0-9a-f-]+\n$/.test(text)) {
      callback?.(); queueMicrotask(() => this.emit('data', Buffer.from(text.replace('ACTIVATED', 'ACTIVE'))))
    } else if (this.stage === 4 && text === (this.mode === 'provision' ? 'MUD1O COMMIT\n' : `MUD1O CLAIMED|${character}\n`)) {
      this.completionCallback = callback
    } else callback?.()
    return true
  }
  releaseCompletion(): void {
    this.trace?.push('completion-callback')
    this.completionCallback?.()
    this.completionCallback = undefined
  }
  end(): this { return this }
  destroy(): this { this.destroyed = true; return this }
}

class RecordingEvidenceFinalizer implements EvidenceFinalizer {
  readonly calls: FinalizeLegacyIdentityEvidenceRequest[] = []
  constructor(private readonly trace: string[], private readonly fails = false) {}
  async finalize(request: FinalizeLegacyIdentityEvidenceRequest): Promise<void> {
    this.calls.push(request)
    this.trace.push('evidence')
    if (this.fails) throw new Error('evidence receipt refused')
  }
}

class PendingHandoffAuthorizer implements OnboardingAuthorizer, CharacterAuthorizer {
  readonly calls: string[] = []
  readonly admissionTickets: Buffer[] = []
  beginSessionCalls = 0
  readyLeases = 0
  leaseReleases = 0
  private active = false
  private leaseSessionId?: string
  constructor(
    private readonly activationFails = false,
    private readonly mode: 'provision' | 'claim' = 'provision',
    private readonly trace?: string[],
    private activationIndeterminateFailures = 0,
    private bindIndeterminateFailures = 0,
  ) {}

  async begin(): Promise<void> { this.calls.push('begin') }
  async cancelUnreserved(): Promise<void> { this.calls.push('cancel') }
  async reserve(request: ReserveOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string }> {
    this.calls.push('reserve')
    return { characterId: character, legacyNameKey: request.legacyName }
  }
  async finalize(_request: FinalizeOnboardingRequest): Promise<{ characterId: string }> {
    this.calls.push('finalize')
    return { characterId: character }
  }
  async reconcile(_request: FinalizeOnboardingRequest): Promise<{ characterId: string }> { throw new Error('reconcile should not run') }
  async challenge(request: ChallengeOnboardingRequest): Promise<ChallengeOnboardingResult> {
    this.calls.push('challenge')
    return { characterId: character, legacyNameKey: request.legacyNameKey, fileSha256: request.fileSha256, allowExpiresAtMs: Date.now() + 60_000 }
  }
  async claim(_request: ClaimOnboardingRequest): Promise<{ characterId: string }> { throw new Error('claim is outside this fixture') }
  async activateHandoff(request: { actorUserId: string, correlationId: string, characterId: string, mode: 'provision' | 'claim' }): Promise<{ characterId: string }> {
    this.calls.push('activate')
    this.trace?.push('activate')
    assert.deepEqual(request, { actorUserId: actor, correlationId: correlation, characterId: character, mode: this.mode })
    if (this.activationFails) throw new Error('activation refused')
    if (this.activationIndeterminateFailures > 0) {
      this.activationIndeterminateFailures -= 1
      throw new OnboardingAuthorizationError(true)
    }
    this.active = true
    return { characterId: character }
  }
  async bindSnapshotCommand(): Promise<void> {
    this.trace?.push('bind')
    if (this.bindIndeterminateFailures > 0) {
      this.bindIndeterminateFailures -= 1
      throw new OnboardingAuthorizationError(true)
    }
  }
  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    this.beginSessionCalls += 1
    if (!this.active || request.actorUserId !== actor || request.characterId !== character || this.leaseSessionId) {
      throw new Error('owner-active roster admission was refused')
    }
    this.readyLeases += 1
    this.leaseSessionId = request.sessionId
    return { legacyNameKey: 'Hero' }
  }
  async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    return { sessionId: request.sessionId, actorUserId: actor, characterId: character, legacyNameKey: 'Hero', lifecycle: 'active', expiresAtMs: request.expiresAt.getTime() }
  }
  async endSession(sessionId: string): Promise<void> {
    if (this.leaseSessionId === sessionId) { this.leaseReleases += 1; this.leaseSessionId = undefined }
  }
}

function config(evidenceEnabled = false) {
  return loadConfig({
    NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0',
    ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500',
    ...(evidenceEnabled ? { MUD_ENABLE_ONBOARDING_EVIDENCE: '1' } : {}),
  })
}

function messages(ws: WebSocket): Array<{ data: RawData, binary: boolean }> {
  const received: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => received.push({ data, binary }))
  return received
}
function hasText(received: Array<{ data: RawData, binary: boolean }>, value: string): boolean {
  return received.some(({ data, binary }) => !binary && Buffer.from(data).toString() === value)
}
const privateClaimControlFrame = /^MUD1O (?:CHALLENGE\|(?:[0-9a-f]{2}){1,14}\|[0-9a-f]{64}|ALLOW)\n$/

function hasPrivateClaimControl(received: Array<{ data: RawData, binary: boolean }>): boolean {
  return received.some(({ data }) => privateClaimControlFrame.test(Buffer.from(data).toString('ascii')))
}
async function eventually(check: () => void, timeoutMs = 1_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    try { check(); return } catch (error) {
      if (Date.now() >= deadline) throw error
      await new Promise((resolve) => setTimeout(resolve, 5))
    }
  }
}

function cEvidence(canonicalName: string, playerFileSha256: string): LegacyIdentityEvidenceV1 {
  return {
    outcome: 'ok', canonicalization: 'canonical', canonicalName,
    legacyShard: createHash('sha1').update(canonicalName, 'utf8').digest('hex').slice(0, 2),
    playerFileSha256, storageFormat: 'player-v1',
  }
}

test('claim privacy detector recognizes only exact private controls in text and binary frames', () => {
  for (const data of ['ALLOWANCE earned', 'CHALLENGE accepted', 'MUD1O ALLOW\nextra', 'MUD1O CHALLENGE|416c696365|' + 'b'.repeat(64) + '\nextra']) {
    for (const binary of [false, true]) {
      assert.equal(hasPrivateClaimControl([{ data: binary ? Buffer.from(data) : data, binary }]), false, `${binary ? 'binary' : 'text'} incidental game data must not be detected: ${data}`)
    }
  }

  for (const control of ['MUD1O CHALLENGE|416c696365|' + 'b'.repeat(64) + '\n', 'MUD1O ALLOW\n']) {
    for (const binary of [false, true]) {
      assert.equal(hasPrivateClaimControl([{ data: binary ? Buffer.from(control) : control, binary }]), true, `${binary ? 'binary' : 'text'} ${control.trim()} must be detected`)
    }
  }
})

test('normal admission remains locked until the held COMMIT callback activates the pending handoff', async (t) => {
  const onboardingMud = new HeldCommitMudSocket()
  const authorizer = new PendingHandoffAuthorizer()
  let connections = 0
  const gateway = createGateway(config(), {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => {
      connections += 1
      const socket = connections === 1 ? onboardingMud : new AdmissionMudSocket(authorizer.admissionTickets)
      socket.connect()
      return socket as unknown as import('node:net').Socket
    },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })
  const base = gateway.address().replace('http:', 'ws:')

  const onboarding = new WebSocket(`${base}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(onboarding, 'open')
  const onboardingClosed = once(onboarding, 'close')
  const onboardingMessages = messages(onboarding)
  onboarding.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(hasText(onboardingMessages, '{"type":"onboarding-ready","mode":"provision"}')))
  onboarding.send(Buffer.from('Hero\n'))
  await eventually(() => assert.ok(onboardingMud.writes.some((frame) => frame.toString('ascii') === `MUD1O RESERVED|${character}\n`)))
  onboarding.send(Buffer.from('m\n'))
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize']))
  await eventually(() => assert.ok(onboardingMud.writes.some((frame) => frame.toString('ascii') === 'MUD1O COMMIT\n')))
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize'])

  const before = new WebSocket(`${base}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(before, 'open')
  const beforeClosed = once(before, 'close')
  before.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token', characterId: character }))
  await eventually(() => assert.equal(authorizer.beginSessionCalls, 1))
  assert.equal(authorizer.admissionTickets.length, 0)
  assert.equal(authorizer.readyLeases, 0)
  assert.equal(authorizer.leaseReleases, 0)
  await beforeClosed

  onboardingMud.releaseCommit()
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'activate']))
  await eventually(() => assert.ok(hasText(onboardingMessages, `{"type":"provisioned","characterId":"${character}"}`)))
  await onboardingClosed

  const after = new WebSocket(`${base}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(after, 'open')
  const afterClosed = once(after, 'close')
  const afterMessages = messages(after)
  after.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token', characterId: character }))
  await eventually(() => assert.equal(authorizer.admissionTickets.length, 1))
  await eventually(() => assert.equal(afterMessages.filter(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"ready"}').length, 1))
  assert.equal(authorizer.readyLeases, 1)
  after.close()
  await afterClosed
  await eventually(() => assert.equal(authorizer.leaseReleases, 1))
})

test('activation failure after COMMIT never emits browser completion', async (t) => {
  const onboardingMud = new HeldCommitMudSocket()
  const authorizer = new PendingHandoffAuthorizer(true)
  const gateway = createGateway(config(), {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { onboardingMud.connect(); return onboardingMud as unknown as import('node:net').Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const closed = once(ws, 'close')
  const received = messages(ws)
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(hasText(received, '{"type":"onboarding-ready","mode":"provision"}')))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.ok(onboardingMud.writes.some((frame) => frame.toString('ascii') === `MUD1O RESERVED|${character}\n`)))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize']))
  await eventually(() => assert.ok(onboardingMud.writes.some((frame) => frame.toString('ascii') === 'MUD1O COMMIT\n')))
  onboardingMud.releaseCommit()
  await closed
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'activate'])
  assert.equal(received.some(({ data, binary }) => !binary && Buffer.from(data).toString().includes('"type":"provisioned"')), false)
})

test('indeterminate activation and binding responses retry the exact handoff before browser completion', async (t) => {
  const trace: string[] = []
  const mud = new HeldEvidenceCompletionMudSocket('provision', cEvidence('Hero', 'a'.repeat(64)), trace)
  const authorizer = new PendingHandoffAuthorizer(false, 'provision', trace, 1, 1)
  const finalizer = new RecordingEvidenceFinalizer(trace)
  let connections = 0
  const gateway = createGateway(config(true), {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    evidenceFinalizer: finalizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => {
      connections += 1
      const socket = connections === 1 ? mud : new AdmissionMudSocket(authorizer.admissionTickets)
      socket.connect()
      return socket as unknown as import('node:net').Socket
    },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const closed = once(ws, 'close')
  const received = messages(ws)
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(hasText(received, '{"type":"onboarding-ready","mode":"provision"}')))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === `MUD1O RESERVED|${character}\n`)))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === 'MUD1O COMMIT\n')))

  assert.equal(authorizer.beginSessionCalls, 0)
  mud.releaseCompletion()
  await closed
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'activate', 'activate'])
  assert.deepEqual(trace, ['completion-callback', 'evidence', 'activate', 'activate', 'bind', 'bind'])
  assert.ok(hasText(received, `{"type":"provisioned","characterId":"${character}"}`))

  const normal = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(normal, 'open')
  const normalClosed = once(normal, 'close')
  normal.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token', characterId: character }))
  await eventually(() => assert.equal(authorizer.beginSessionCalls, 1))
  normal.close()
  await normalClosed
})

for (const scenario of [
  { mode: 'provision' as const, legacyName: 'Hero', sha256: 'a'.repeat(64), input: 'm\n', completion: 'MUD1O COMMIT\n', browser: 'provisioned', before: ['begin', 'reserve'] },
  { mode: 'claim' as const, legacyName: 'Alice', sha256: 'b'.repeat(64), input: 'old-secret\n', completion: `MUD1O CLAIMED|${character}\n`, browser: 'claimed', before: ['begin', 'challenge'] },
]) {
  test(`evidence ${scenario.mode} finalizes only after the held C completion callback, then activates handoff`, async (t) => {
    const trace: string[] = []
    const mud = new HeldEvidenceCompletionMudSocket(scenario.mode, cEvidence(scenario.legacyName, scenario.sha256))
    const authorizer = new PendingHandoffAuthorizer(false, scenario.mode, trace)
    const finalizer = new RecordingEvidenceFinalizer(trace)
    const gateway = createGateway(config(true), {
      onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, evidenceFinalizer: finalizer,
      authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
      connectTcp: () => { mud.connect(); return mud as unknown as import('node:net').Socket },
    })
    gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
    t.after(async () => { await gateway.close() })
    const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
    await once(ws, 'open')
    const closed = once(ws, 'close')
    const received = messages(ws)
    ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: scenario.mode, correlationId: correlation }))
    await eventually(() => assert.ok(hasText(received, `{"type":"onboarding-ready","mode":"${scenario.mode}"}`)))
    ws.send(Buffer.from(scenario.mode === 'provision' ? 'Hero\n' : 'Alice\n'))
    await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === (scenario.mode === 'provision' ? `MUD1O RESERVED|${character}\n` : 'MUD1O ALLOW\n'))))
    ws.send(Buffer.from(scenario.input))
    await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === scenario.completion)))
    assert.deepEqual(authorizer.calls, scenario.before)
    assert.deepEqual(finalizer.calls, [], 'the C completion callback is the evidence finalization boundary')
    assert.deepEqual(trace, [])

    mud.releaseCompletion()
    await closed
    assert.deepEqual(trace, ['evidence', 'activate', 'bind'])
    assert.deepEqual(authorizer.calls, [...scenario.before, 'activate'])
    assert.deepEqual(finalizer.calls, [{
      actorUserId: actor, correlationId: correlation, characterId: character, mode: scenario.mode,
      worldId: 'muhan', evidence: cEvidence(scenario.legacyName, scenario.sha256),
    }])
    assert.ok(hasText(received, `{"type":"${scenario.browser}","characterId":"${character}"}`))
  })
}

test('mismatched C evidence is rejected without opening the normal admission gate', async (t) => {
  const mud = new HeldEvidenceCompletionMudSocket('provision', cEvidence('WrongHero', 'a'.repeat(64)))
  const authorizer = new PendingHandoffAuthorizer(false, 'provision')
  const finalizer = new RecordingEvidenceFinalizer([])
  let connections = 0
  const gateway = createGateway(config(true), {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    evidenceFinalizer: finalizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => {
      connections += 1
      const socket = connections === 1 ? mud : new AdmissionMudSocket(authorizer.admissionTickets)
      socket.connect()
      return socket as unknown as import('node:net').Socket
    },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })
  const base = gateway.address().replace('http:', 'ws:')

  const onboarding = new WebSocket(`${base}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(onboarding, 'open')
  const received = messages(onboarding)
  const closed = once(onboarding, 'close') as Promise<[number]>
  onboarding.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(hasText(received, '{"type":"onboarding-ready","mode":"provision"}')))
  onboarding.send(Buffer.from('Hero\n'))
  await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === `MUD1O RESERVED|${character}\n`)))
  onboarding.send(Buffer.from('m\n'))

  const [code] = await closed
  assert.equal(code, 1008)
  assert.deepEqual(finalizer.calls, [])
  assert.deepEqual(authorizer.calls, ['begin', 'reserve'])
  assert.equal(authorizer.beginSessionCalls, 0)
  assert.deepEqual(authorizer.admissionTickets, [])
  assert.equal(received.some(({ data, binary }) => !binary && Buffer.from(data).toString().includes('"type":"provisioned"')), false)
})

test('claim binding reaches the active owner roster once, then only the exact browser actor and character can receive a normal MUD1 handoff', async (t) => {
  const trace: string[] = []
  const mud = new HeldEvidenceCompletionMudSocket('claim', cEvidence('Alice', 'b'.repeat(64)), trace)
  const authorizer = new PendingHandoffAuthorizer(false, 'claim', trace)
  const finalizer = new RecordingEvidenceFinalizer(trace)
  let connections = 0
  const gateway = createGateway(config(true), {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    evidenceFinalizer: finalizer,
    authenticator: {
      verify: async (token) => ({
        sub: token === 'other-browser-token' ? '123e4567-e89b-12d3-a456-426614174099' : actor,
        expiresAtMs: token === 'expired-browser-token' ? Date.now() - 1 : Date.now() + 60_000,
        claims: {},
      }),
    },
    connectTcp: () => {
      connections += 1
      const socket = connections === 1 ? mud : new AdmissionMudSocket(authorizer.admissionTickets)
      socket.connect()
      return socket as unknown as import('node:net').Socket
    },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })
  const base = gateway.address().replace('http:', 'ws:')

  const onboarding = new WebSocket(`${base}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(onboarding, 'open')
  const onboardingClosed = once(onboarding, 'close')
  const onboardingMessages = messages(onboarding)
  onboarding.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.ok(hasText(onboardingMessages, '{"type":"onboarding-ready","mode":"claim"}')))
  onboarding.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === 'MUD1O ALLOW\n')))
  await eventually(() => assert.ok(onboardingMessages.some(({ data, binary }) => binary && Buffer.from(data).toString('utf8') === '비밀번호? ')), 'ordinary binary game data still reaches the browser')
  assert.equal(hasPrivateClaimControl(onboardingMessages), false, 'claim controls remain C-private in text and binary frames')
  assert.deepEqual(authorizer.calls, ['begin', 'challenge'], 'browser auth alone cannot fabricate or claim a character')
  assert.deepEqual(finalizer.calls, [])

  onboarding.send(Buffer.from('old-secret\n'))
  await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === `MUD1O CLAIMED|${character}\n`)))
  assert.deepEqual(authorizer.calls, ['begin', 'challenge'])
  assert.deepEqual(trace, [], 'CLAIMED write acceptance gates evidence finalization and activation')

  const before = new WebSocket(`${base}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(before, 'open')
  const beforeClosed = once(before, 'close')
  before.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token', characterId: character }))
  await beforeClosed
  assert.equal(authorizer.beginSessionCalls, 1)
  assert.equal(authorizer.readyLeases, 0)
  assert.deepEqual(authorizer.admissionTickets, [], 'pending handoff cannot mint a normal MUD admission ticket')

  mud.releaseCompletion()
  await onboardingClosed
  assert.deepEqual(trace, ['completion-callback', 'evidence', 'activate', 'bind'])
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'activate'])
  assert.deepEqual(finalizer.calls, [{
    actorUserId: actor, correlationId: correlation, characterId: character, mode: 'claim',
    worldId: 'muhan', evidence: cEvidence('Alice', 'b'.repeat(64)),
  }])
  assert.ok(hasText(onboardingMessages, `{"type":"claimed","characterId":"${character}"}`))

  for (const rejected of [
    { accessToken: 'other-browser-token', characterId: character, label: 'wrong browser actor' },
    { accessToken: 'browser-token', characterId: '123e4567-e89b-12d3-a456-426614174099', label: 'wrong character' },
    { accessToken: 'expired-browser-token', characterId: character, label: 'expired browser identity' },
  ]) {
    const denied = new WebSocket(`${base}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
    await once(denied, 'open')
    const deniedClosed = once(denied, 'close')
    denied.send(JSON.stringify({ type: 'auth', accessToken: rejected.accessToken, characterId: rejected.characterId }))
    await deniedClosed
    assert.deepEqual(authorizer.admissionTickets, [], `${rejected.label} must not mint a MUD1 ticket`)
  }

  const after = new WebSocket(`${base}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(after, 'open')
  const afterClosed = once(after, 'close')
  const afterMessages = messages(after)
  after.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token', characterId: character }))
  await eventually(() => assert.equal(authorizer.admissionTickets.length, 1))
  await eventually(() => assert.ok(hasText(afterMessages, '{"type":"ready"}')))
  assert.equal(authorizer.readyLeases, 1)

  const replay = new WebSocket(`${base}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(replay, 'open')
  const replayClosed = once(replay, 'close')
  replay.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token', characterId: character }))
  await replayClosed
  assert.equal(authorizer.admissionTickets.length, 1, 'a replayed active character lease must not mint a second MUD1 ticket')

  after.close()
  await afterClosed
  await eventually(() => assert.equal(authorizer.leaseReleases, 1))
})

for (const scenario of [
  { label: 'evidence finalization', finalizerFails: true, activationFails: false, expectedCalls: ['begin', 'reserve'] },
  { label: 'handoff activation', finalizerFails: false, activationFails: true, expectedCalls: ['begin', 'reserve', 'activate'] },
]) {
  test(`${scenario.label} failure after the C callback never emits evidence provision completion`, async (t) => {
    const trace: string[] = []
    const mud = new HeldEvidenceCompletionMudSocket('provision', cEvidence('Hero', 'c'.repeat(64)))
    const authorizer = new PendingHandoffAuthorizer(scenario.activationFails, 'provision', trace)
    const finalizer = new RecordingEvidenceFinalizer(trace, scenario.finalizerFails)
    const gateway = createGateway(config(true), {
      onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, evidenceFinalizer: finalizer,
      authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
      connectTcp: () => { mud.connect(); return mud as unknown as import('node:net').Socket },
    })
    gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
    t.after(async () => { await gateway.close() })
    const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
    await once(ws, 'open')
    const closed = once(ws, 'close')
    const received = messages(ws)
    ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'provision', correlationId: correlation }))
    await eventually(() => assert.ok(hasText(received, '{"type":"onboarding-ready","mode":"provision"}')))
    ws.send(Buffer.from('Hero\n'))
    await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === `MUD1O RESERVED|${character}\n`)))
    ws.send(Buffer.from('m\n'))
    await eventually(() => assert.ok(mud.writes.some((frame) => frame.toString('ascii') === 'MUD1O COMMIT\n')))
    mud.releaseCompletion()
    await closed
    assert.deepEqual(authorizer.calls, scenario.expectedCalls)
    assert.deepEqual(trace, scenario.finalizerFails ? ['evidence'] : scenario.activationFails ? ['evidence', 'activate'] : ['evidence', 'activate', 'bind'])
    assert.equal(hasText(received, `{"type":"provisioned","characterId":"${character}"}`), false)
    assert.equal(authorizer.beginSessionCalls, 0, 'a rejected handoff must not reach normal character admission')
    assert.deepEqual(authorizer.admissionTickets, [], 'a rejected handoff must not mint a normal MUD admission ticket')
  })
}
