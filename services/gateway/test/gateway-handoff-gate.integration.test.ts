import assert from 'node:assert/strict'
import { EventEmitter, once } from 'node:events'
import test from 'node:test'
import WebSocket, { type RawData } from 'ws'
import type { AuthorizedCharacter, BeginCharacterSessionRequest, CharacterAuthorizer, RenewCharacterSessionRequest, RenewedCharacterSession } from '../src/character-authorizer.js'
import { loadConfig } from '../src/config.js'
import { createGateway } from '../src/gateway.js'
import type { ChallengeOnboardingRequest, ChallengeOnboardingResult, ClaimOnboardingRequest, FinalizeOnboardingRequest, OnboardingAuthorizer, ReserveOnboardingRequest } from '../src/onboarding-authorizer.js'

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

class PendingHandoffAuthorizer implements OnboardingAuthorizer, CharacterAuthorizer {
  readonly calls: string[] = []
  readonly admissionTickets: Buffer[] = []
  beginSessionCalls = 0
  readyLeases = 0
  leaseReleases = 0
  private active = false
  private leaseSessionId?: string
  constructor(private readonly activationFails = false) {}

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
  async challenge(_request: ChallengeOnboardingRequest): Promise<ChallengeOnboardingResult> { throw new Error('claim is outside this fixture') }
  async claim(_request: ClaimOnboardingRequest): Promise<{ characterId: string }> { throw new Error('claim is outside this fixture') }
  async activateHandoff(request: { actorUserId: string, correlationId: string, characterId: string, mode: 'provision' | 'claim' }): Promise<{ characterId: string }> {
    this.calls.push('activate')
    assert.deepEqual(request, { actorUserId: actor, correlationId: correlation, characterId: character, mode: 'provision' })
    if (this.activationFails) throw new Error('activation refused')
    this.active = true
    return { characterId: character }
  }
  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    this.beginSessionCalls += 1
    if (!this.active) throw new Error('handoff is still pending')
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

function config() {
  return loadConfig({
    NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0',
    ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500',
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
async function eventually(check: () => void, timeoutMs = 1_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    try { check(); return } catch (error) {
      if (Date.now() >= deadline) throw error
      await new Promise((resolve) => setTimeout(resolve, 5))
    }
  }
}

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
