import assert from 'node:assert/strict'
import { EventEmitter, once } from 'node:events'
import { createConnection, createServer, type Socket } from 'node:net'
import test from 'node:test'
import WebSocket, { type RawData } from 'ws'
import type { AuthorizedCharacter, BeginCharacterSessionRequest, CharacterAuthorizer, RenewCharacterSessionRequest, RenewedCharacterSession } from '../src/character-authorizer.js'
import { loadConfig } from '../src/config.js'
import { createGateway, sendBufferedWebSocketFrame, type EvidenceFinalizer, type GatewayDependencies, type GatewayTimers } from '../src/gateway.js'
import { OnboardingAuthorizationError, OnboardingClaimRejectedError, type ChallengeOnboardingRequest, type FinalizeOnboardingRequest, type OnboardingAuthorizer } from '../src/onboarding-authorizer.js'
import { OnboardingControlDemultiplexer } from '../src/onboarding-protocol.js'

const actor = '123e4567-e89b-12d3-a456-426614174000'
const correlation = '123e4567-e89b-12d3-a456-426614174001'
const character = '123e4567-e89b-12d3-a456-426614174002'

class RecordingOnboardingAuthorizer implements OnboardingAuthorizer, CharacterAuthorizer {
  readonly calls: string[] = []
  readonly claimNames: string[] = []
  readonly claimFingerprints: string[] = []
  readonly challenges: ChallengeOnboardingRequest[] = []
  readonly finalizations: FinalizeOnboardingRequest[] = []
  readonly reconciliations: FinalizeOnboardingRequest[] = []
  readonly leaseBegins: BeginCharacterSessionRequest[] = []
  readonly leaseRenewals: RenewCharacterSessionRequest[] = []
  readonly leaseReleases: string[] = []
  readonly cancellations: Array<{ actorUserId: string, correlationId: string }> = []
  private readonly leasedCharacters = new Map<string, string>()
  private readonly reservedNames = new Map<string, string>()
  constructor(
    private readonly finalizeFails = false,
    private readonly reconcileFails = false,
    private readonly leaseNameOverride?: string,
    private claimFailures = 0,
    private readonly claimFailureError?: Error,
  ) {}
  async begin(): Promise<void> { this.calls.push('begin') }
  async reserve(request: { legacyName: string }): Promise<{ characterId: string, legacyNameKey: string }> {
    this.calls.push('reserve')
    this.reservedNames.set(character, request.legacyName)
    return { characterId: character, legacyNameKey: request.legacyName }
  }
  async finalize(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> {
    this.calls.push('finalize'); this.finalizations.push(request)
    if (this.finalizeFails) throw new Error('uncertain finalize failure')
    return { characterId: character }
  }
  async reconcile(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> {
    this.calls.push('reconcile'); this.reconciliations.push(request)
    if (this.reconcileFails) throw new Error('reconcile failure')
    return { characterId: character }
  }
  async claim(request: { legacyNameKey: string, fileSha256: string }): Promise<{ characterId: string }> {
    this.calls.push('claim'); this.claimNames.push(request.legacyNameKey); this.claimFingerprints.push(request.fileSha256)
    if (this.claimFailures > 0) { this.claimFailures -= 1; throw this.claimFailureError ?? new OnboardingAuthorizationError(true) }
    return { characterId: character }
  }
  async activateHandoff(request: { characterId: string }): Promise<{ characterId: string }> {
    this.calls.push('activate')
    return { characterId: request.characterId }
  }
  async bindSnapshotCommand(): Promise<void> { this.calls.push('bind') }
  async challenge(request: ChallengeOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string, fileSha256: string, allowExpiresAtMs: number }> {
    this.calls.push('challenge'); this.challenges.push(request)
    return { characterId: character, legacyNameKey: request.legacyNameKey, fileSha256: request.fileSha256, allowExpiresAtMs: Date.now() + 90_000 }
  }
  async cancelUnreserved(request: { actorUserId: string, correlationId: string }): Promise<void> {
    this.calls.push('cancel'); this.cancellations.push(request)
  }
  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    this.leaseBegins.push(request)
    if (this.leasedCharacters.has(request.characterId)) throw new Error('character already has an active lease')
    this.leasedCharacters.set(request.characterId, request.sessionId)
    return { legacyNameKey: this.leaseNameOverride ?? this.reservedNames.get(request.characterId) ?? 'Contracthero' }
  }
  async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    this.leaseRenewals.push(request)
    return {
      sessionId: request.sessionId,
      actorUserId: actor,
      characterId: character,
      legacyNameKey: this.leaseNameOverride ?? this.reservedNames.get(character) ?? 'Contracthero',
      lifecycle: 'active',
      expiresAtMs: request.expiresAt.getTime(),
    }
  }
  async endSession(sessionId: string): Promise<void> {
    this.leaseReleases.push(sessionId)
    for (const [characterId, activeSessionId] of this.leasedCharacters) {
      if (activeSessionId === sessionId) this.leasedCharacters.delete(characterId)
    }
  }
}

class CommitCallbackMudSocket extends EventEmitter {
  destroyed = false
  readonly writes: Buffer[] = []
  private commitCallback?: (error?: Error | null) => void

  constructor(
    private readonly savedLeadingGame = Buffer.alloc(0),
    private readonly savedTrailingGame = Buffer.alloc(0),
    private readonly preAdmissionGame = Buffer.alloc(0),
  ) { super() }

  connect(): void { queueMicrotask(() => this.emit('connect')) }

  write(data: Uint8Array | string, callback?: (error?: Error | null) => void): boolean {
    const frame = Buffer.from(data)
    this.writes.push(frame)
    const text = frame.toString()
    if (text.startsWith('MUD1O|P|')) {
      callback?.()
      queueMicrotask(() => this.emit('data', this.preAdmissionGame.length ? this.preAdmissionGame : Buffer.from('MUD1O OK\n이름? ')))
    } else if (text === 'Hero\n') {
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from('MUD1O RESERVE|4865726f\n')))
    } else if (text === `MUD1O RESERVED|${character}\n`) {
      callback?.()
      setImmediate(() => this.emit('data', Buffer.from('성별? ')))
    } else if (text === 'm\n') {
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.concat([
        this.savedLeadingGame, Buffer.from(`MUD1O SAVED|${character}|${'f'.repeat(64)}|player-v1\n`), this.savedTrailingGame,
      ])))
    } else if (/^MUD1O ACTIVATED\|[0-9a-f-]+\n$/.test(text)) {
      callback?.(); setImmediate(() => this.emit('data', Buffer.from(text.replace('ACTIVATED', 'ACTIVE'))))
    } else if (text === 'MUD1O COMMIT\n') {
      this.commitCallback = callback
    } else {
      callback?.()
    }
    return true
  }

  releaseCommit(): void {
    const callback = this.commitCallback
    this.commitCallback = undefined
    callback?.()
  }

  end(): this { return this }
  destroy(): this { this.destroyed = true; return this }
}

class ClaimCompletionRaceMudSocket extends EventEmitter {
  destroyed = false
  destroyDuringEnd = 0
  readonly writes: Buffer[] = []
  private stage = 0
  private ending = false

  constructor(private readonly sendActive = true) { super() }

  connect(): void { queueMicrotask(() => this.emit('connect')) }

  write(data: Uint8Array | string, callback?: (error?: Error | null) => void): boolean {
    const frame = Buffer.from(data)
    const text = frame.toString()
    this.writes.push(frame)
    if (text.startsWith('MUD1O|C|')) {
      this.stage = 1
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from('MUD1O OK\n기존 이름? ')))
    } else if (this.stage === 1 && text === 'Alice\n') {
      this.stage = 2
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from(`MUD1O CHALLENGE|416c696365|${'b'.repeat(64)}\n`)))
    } else if (this.stage === 2 && text === 'MUD1O ALLOW\n') {
      this.stage = 3
      callback?.()
    } else if (this.stage === 3 && text === 'old-secret\n') {
      this.stage = 4
      callback?.()
      queueMicrotask(() => this.emit('data', Buffer.from(`MUD1O VERIFIED|416c696365|${'b'.repeat(64)}\n`)))
    } else if (/^MUD1O ACTIVATED\|[0-9a-f-]+\n$/.test(text)) {
      callback?.()
      if (this.sendActive) setImmediate(() => this.emit('data', Buffer.from(text.replace('ACTIVATED', 'ACTIVE'))))
    } else if (this.stage === 4 && text === `MUD1O CLAIMED|${character}\n`) {
      this.stage = 5
      callback?.()
    } else {
      callback?.()
    }
    return true
  }

  end(): this {
    this.ending = true
    this.emit('error', new Error('simulated C close error'))
    this.emit('end')
    this.ending = false
    return this
  }

  destroy(): this {
    if (this.ending) this.destroyDuringEnd += 1
    this.destroyed = true
    return this
  }
}

class DeferredFinalizeAuthorizer extends RecordingOnboardingAuthorizer {
  private releaseGate!: () => void
  private readonly gate = new Promise<void>((resolve) => { this.releaseGate = resolve })

  override async finalize(request: FinalizeOnboardingRequest): Promise<{ characterId: string }> {
    this.calls.push('finalize')
    this.finalizations.push(request)
    await this.gate
    return { characterId: character }
  }

  finishFinalize(): void { this.releaseGate() }
}

class DeferredClaimAuthorizer extends RecordingOnboardingAuthorizer {
  private releaseGate!: () => void
  private readonly gate = new Promise<void>((resolve) => { this.releaseGate = resolve })

  override async claim(request: { legacyNameKey: string, fileSha256: string }): Promise<{ characterId: string }> {
    this.calls.push('claim'); this.claimNames.push(request.legacyNameKey); this.claimFingerprints.push(request.fileSha256)
    await this.gate
    return { characterId: character }
  }

  finishClaim(): void { this.releaseGate() }
}

class RejectingChallengeAuthorizer extends RecordingOnboardingAuthorizer {
  override async challenge(request: ChallengeOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string, fileSha256: string, allowExpiresAtMs: number }> {
    this.calls.push('challenge'); this.challenges.push(request)
    throw new Error('challenge refused before allowance')
  }
}

class DeferredChallengeAuthorizer extends RecordingOnboardingAuthorizer {
  private releaseGate!: () => void
  private readonly gate = new Promise<void>((resolve) => { this.releaseGate = resolve })

  override async challenge(request: ChallengeOnboardingRequest): Promise<{ characterId: string, legacyNameKey: string, fileSha256: string, allowExpiresAtMs: number }> {
    this.calls.push('challenge'); this.challenges.push(request)
    await this.gate
    return { characterId: character, legacyNameKey: request.legacyNameKey, fileSha256: request.fileSha256, allowExpiresAtMs: Date.now() + 90_000 }
  }

  finishChallenge(): void { this.releaseGate() }
}

class SequencedClaimAuthorizer extends RecordingOnboardingAuthorizer {
  constructor(private readonly claimErrors: Error[]) { super() }

  override async claim(request: { legacyNameKey: string, fileSha256: string }): Promise<{ characterId: string }> {
    this.calls.push('claim'); this.claimNames.push(request.legacyNameKey); this.claimFingerprints.push(request.fileSha256)
    const error = this.claimErrors.shift()
    if (error) throw error
    return { characterId: character }
  }
}

class DeferredBeginAuthorizer extends RecordingOnboardingAuthorizer {
  private releaseGate!: () => void
  private readonly gate = new Promise<void>((resolve) => { this.releaseGate = resolve })

  override async begin(): Promise<void> {
    this.calls.push('begin')
    await this.gate
  }

  finishBegin(): void { this.releaseGate() }
}

class DeferredBeginAndCancelAuthorizer extends DeferredBeginAuthorizer {
  private releaseCancelGate!: () => void
  private readonly cancelGate = new Promise<void>((resolve) => { this.releaseCancelGate = resolve })
  cancelFinished = false

  override async cancelUnreserved(request: { actorUserId: string, correlationId: string }): Promise<void> {
    this.cancellations.push(request)
    await this.cancelGate
    this.cancelFinished = true
  }

  finishCancel(): void { this.releaseCancelGate() }
}

class DeferredReserveAuthorizer extends RecordingOnboardingAuthorizer {
  private releaseGate!: () => void
  private readonly gate = new Promise<void>((resolve) => { this.releaseGate = resolve })

  override async reserve(request: { legacyName: string }): Promise<{ characterId: string, legacyNameKey: string }> {
    this.calls.push('reserve')
    await this.gate
    return { characterId: character, legacyNameKey: request.legacyName }
  }

  finishReserve(): void { this.releaseGate() }
}

test('provision relays the original wizard before DB finalize and blocks only at reserve/finalize boundaries', async (t) => {
  const toMud: Buffer[] = []
  let activationCommandId: string | undefined
  let clientSocket: Socket | undefined
  let stage = 0
  const mud = createServer((socket) => {
    clientSocket = socket
    socket.on('data', (data) => {
    const frame = Buffer.from(data); toMud.push(frame)
    if (stage === 0) { stage = 1; socket.write('MUD1O OK\n이름? ') }
    else if (stage === 1 && frame.toString() === 'Hero\n') { stage = 2; socket.write('MUD1O RESERVE|4865726f\n') }
    else if (stage === 2 && frame.toString('ascii') === `MUD1O RESERVED|${character}\n`) { stage = 3; socket.write('성별? ') }
    else if (stage === 3 && frame.toString() === 'm\n') { stage = 4; socket.write(`MUD1O SAVED|${character}|${'a'.repeat(64)}|player-v1\n`) }
    else if (stage === 4 && frame.toString('ascii').includes('MUD1O COMMIT\n')) {
      stage = 5
      const activation = frame.toString('ascii').match(/MUD1O ACTIVATED\|([0-9a-f-]+)\n/)
      if (activation) { activationCommandId = activation[1]; stage = 6 }
    }
    else if (stage === 5 && /MUD1O ACTIVATED\|[0-9a-f-]+\n/.test(frame.toString('ascii'))) {
      activationCommandId = frame.toString('ascii').match(/MUD1O ACTIVATED\|([0-9a-f-]+)\n/)![1]
      stage = 6
    }
    })
  })
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const dependencies: GatewayDependencies = { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) }, randomBytes: () => Buffer.from([...Array(16).keys()]) }
  const gateway = createGateway(config, dependencies)
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"onboarding-ready","mode":"provision"}')))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'reserve']))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.ok(toMud.some((value) => value.toString('ascii').includes('MUD1O COMMIT\n'))))
  await eventually(() => assert.equal(activationCommandId !== undefined, true))
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'activate'])
  assert.equal(authorizer.calls.includes('bind'), false)
  clientSocket!.write(`MUD1O ACTIVE|${activationCommandId}\n`)
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'activate', 'bind']))
  assert.match(toMud[0]!.toString('ascii'), /^MUD1O\|P\|\d+\|000102030405060708090a0b0c0d0e0f\|123e4567-e89b-12d3-a456-426614174000\|123e4567-e89b-12d3-a456-426614174001\|[0-9a-f]{64}\n$/)
  assert.equal(toMud.reduce((count, value) => count + (value.toString('ascii').match(/MUD1O (?:COMMIT|ACTIVATED)\|?/g) ?? []).length, 0), 2)
  assert.equal(authorizer.leaseBegins.length, 0, 'the normal game socket owns the gameplay lease')
  assert.equal(stage, 6)
  ws.close()
})

test('onboarding bounds only a pending pre-ready control line, not an arbitrary game chunk', () => {
  const demultiplexer = new OnboardingControlDemultiplexer()
  const game = Buffer.alloc(4 * 1024, 0x78)
  assert.deepEqual(demultiplexer.push(game).game, [game])
  assert.deepEqual(demultiplexer.push(Buffer.from('MUD1O OK\n')).controls, [{ type: 'OK' }])
})

test('fragmented onboarding admission control is buffered without rejecting the C connection', async (t) => {
  const mud = createServer((socket) => socket.once('data', () => {
    socket.write('MU')
    setTimeout(() => socket.write('D1O OK\n이름? '), 5)
  }))
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: new RecordingOnboardingAuthorizer(), authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"onboarding-ready","mode":"provision"}')))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  assert.equal(ws.readyState, WebSocket.OPEN)
  ws.close()
})

test('uncertain provision finalize reconciles exactly once, provisions the browser, and closes the one-shot C connection', async (t) => {
  const toMud: Buffer[] = []
  let activationCommandId: string | undefined
  let clientSocket: Socket | undefined
  let stage = 0
  let mudClosed = false
  const mud = createServer((socket) => {
    clientSocket = socket
    socket.once('close', () => { mudClosed = true })
    socket.on('data', (data) => {
      const frame = Buffer.from(data); toMud.push(frame)
      if (stage === 0) { stage = 1; socket.write('MUD1O OK\n') }
      else if (stage === 1 && frame.toString() === 'Hero\n') { stage = 2; socket.write('MUD1O RESERVE|4865726f\n') }
      else if (stage === 2 && frame.toString('ascii') === `MUD1O RESERVED|${character}\n`) { stage = 3; socket.write('성별? ') }
      else if (stage === 3 && frame.toString() === 'm\n') { stage = 4; socket.write(`MUD1O SAVED|${character}|${'c'.repeat(64)}|player-v1\n`) }
      else if (stage === 4 && frame.toString('ascii').includes('MUD1O COMMIT\n')) {
        stage = 5
        const activation = frame.toString('ascii').match(/MUD1O ACTIVATED\|([0-9a-f-]+)\n/)
        if (activation) { activationCommandId = activation[1]; stage = 6 }
      }
      else if (stage === 5 && /MUD1O ACTIVATED\|[0-9a-f-]+\n/.test(frame.toString('ascii'))) {
        activationCommandId = frame.toString('ascii').match(/MUD1O ACTIVATED\|([0-9a-f-]+)\n/)![1]
        stage = 6
      }
    })
  })
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const authorizer = new RecordingOnboardingAuthorizer(true)
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  // Arm this before completion can synchronously close the browser socket.
  const uncertainProvisionClose = once(ws, 'close')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"onboarding-ready","mode":"provision"}')))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))

  const expectedFinalize: FinalizeOnboardingRequest = { actorUserId: actor, correlationId: correlation, characterId: character, fileSha256: 'c'.repeat(64), storageFormat: 'player-v1' }
  await eventually(() => assert.deepEqual(authorizer.finalizations, [expectedFinalize]))
  await eventually(() => assert.deepEqual(authorizer.reconciliations, [expectedFinalize]))
  await eventually(() => assert.ok(toMud.some((value) => value.toString('ascii').includes('MUD1O COMMIT\n'))))
  await eventually(() => assert.equal(activationCommandId !== undefined, true))
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'reconcile', 'activate'])
  assert.equal(authorizer.calls.includes('bind'), false)
  clientSocket!.write(`MUD1O ACTIVE|${activationCommandId}\n`)
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'reconcile', 'activate', 'bind']))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`)))
  const [closeCode] = await uncertainProvisionClose as [number]
  assert.equal(closeCode, 1000)
  await eventually(() => assert.equal(mudClosed, true))
  assert.equal(stage, 6)
  assert.equal(toMud.some((value) => value.toString() === 'look\n'), false)
})

test('browser is provisioned only after the C COMMIT write callback succeeds', async (t) => {
  const mud = new CommitCallbackMudSocket()
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n')))
  assert.equal(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString().includes('"type":"provisioned"')), false)
  mud.releaseCommit()
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`)))
  ws.close()
})

test('provision completion silently drops game bytes coalesced with SAVED', async (t) => {
  const trailingGame = Buffer.from('trailing C gameplay fragment')
  const mud = new CommitCallbackMudSocket(Buffer.alloc(0), trailingGame)
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n')))
  mud.releaseCommit()

  const [closeCode] = await once(ws, 'close') as [number]
  assert.equal(closeCode, 1000)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`))
  assert.equal(messages.some(({ data, binary }) => binary && Buffer.from(data).equals(trailingGame)), false)
  assert.equal(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString().includes('"type":"error"')), false)
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'activate', 'bind'])
})

test('provision completion drops a later C game event while SAVED finalization is pending', async (t) => {
  const trailingGame = Buffer.from('later C gameplay fragment')
  const mud = new CommitCallbackMudSocket()
  const authorizer = new DeferredFinalizeAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { authorizer.finishFinalize(); await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.equal(authorizer.finalizations.length, 1))

  mud.emit('data', trailingGame)
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.equal(ws.readyState, WebSocket.OPEN)
  assert.equal(messages.some(({ data, binary }) => binary && Buffer.from(data).equals(trailingGame)), false)
  assert.equal(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString().includes('"type":"error"')), false)
  authorizer.finishFinalize()
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n')))
  mud.releaseCommit()
  const [closeCode] = await once(ws, 'close') as [number]
  assert.equal(closeCode, 1000)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`))
})

test('game bytes ordered before SAVED in a coalesced C event are relayed before provision completion', async (t) => {
  const leadingGame = Buffer.from('pre-SAVED gameplay fragment')
  const mud = new CommitCallbackMudSocket(leadingGame)
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n')))
  assert.ok(messages.some(({ data, binary }) => binary && Buffer.from(data).equals(leadingGame)))
  mud.releaseCommit()
  const [closeCode] = await once(ws, 'close') as [number]
  assert.equal(closeCode, 1000)
  assert.equal(authorizer.finalizations.length, 1)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`))
})

test('unexpected C game bytes before a valid SAVED still fail closed', async (t) => {
  const mud = new CommitCallbackMudSocket(Buffer.alloc(0), Buffer.alloc(0), Buffer.from('unexpected pre-admission gameplay'))
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  const [closeCode] = await once(ws, 'close') as [number]
  assert.equal(closeCode, 1011)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"error","reason":"unexpected MUD game bytes"}'))
  assert.equal(authorizer.finalizations.length, 0)
})

test('provision completion closes onboarding and hands the owned active character to normal /ws lease and ticket admission', async (t) => {
  const timers = new FakeTimers(1_700_000_000_000)
  const mud = new CommitCallbackMudSocket()
  const authorizer = new RecordingOnboardingAuthorizer()
  const sessionIds = ['123e4567-e89b-12d3-a456-426614174003', '123e4567-e89b-12d3-a456-426614174004']
  let tcpConnections = 0
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: timers.nowMs + 3_600_000, claims: {} }) },
    connectTcp: () => {
      tcpConnections += 1
      mud.connect()
      return mud as unknown as Socket
    },
    now: () => timers.nowMs,
    randomUuid: () => sessionIds.shift()!,
    timers,
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const onboardingClosed = once(ws, 'close')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n')))
  mud.releaseCommit()
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`)))
  await onboardingClosed
  assert.equal(authorizer.leaseBegins.length, 0, 'onboarding must not retain the gameplay lease')

  const regular = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(regular, 'open')
  const regularClosed = once(regular, 'close')
  regular.send(JSON.stringify({ type: 'auth', accessToken: 'browser-token-not-for-logs', characterId: character }))
  await eventually(() => assert.equal(authorizer.leaseBegins.length, 1))
  await eventually(() => assert.equal(tcpConnections, 2))
  assert.match(mud.writes.at(-1)!.toString('ascii'), /^MUD1\|/)
  regular.close()
  await regularClosed
  await eventually(() => assert.ok(authorizer.leaseReleases.includes(authorizer.leaseBegins[0]!.sessionId)))
})

test('closing while begin is in flight cancels the exact intent after begin succeeds', async (t) => {
  const authorizer = new DeferredBeginAuthorizer()
  let tcpConnections = 0
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { tcpConnections += 1; throw new Error('must not connect after browser closes') },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.equal(authorizer.calls.includes('begin'), true))
  ws.close()
  await once(ws, 'close')
  authorizer.finishBegin()

  await eventually(() => assert.deepEqual(authorizer.cancellations, [{ actorUserId: actor, correlationId: correlation }]))
  assert.equal(tcpConnections, 0)
})

test('closing after the reserve RPC starts never uses the unreserved cancellation path', async (t) => {
  const mud = new CommitCallbackMudSocket()
  const authorizer = new DeferredReserveAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.equal(authorizer.calls.includes('reserve'), true))
  ws.close()
  await once(ws, 'close')
  authorizer.finishReserve()

  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.deepEqual(authorizer.cancellations, [])
})

test('input queued during finalize is not relayed after the provisioning handoff closes', async (t) => {
  const mud = new CommitCallbackMudSocket()
  const authorizer = new DeferredFinalizeAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
    randomUuid: () => '123e4567-e89b-12d3-a456-426614174004',
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.equal(authorizer.finalizations.length, 1))

  ws.send(Buffer.from('look\n'))
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.equal(mud.writes.some((value) => value.toString() === 'look\n'), false)
  authorizer.finishFinalize()
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n')))
  assert.equal(mud.writes.some((value) => value.toString() === 'look\n'), false)
  mud.releaseCommit()
  await once(ws, 'close')
  assert.equal(mud.writes.some((value) => value.toString() === 'look\n'), false)
})

test('ready provision reports completion before closing the one-shot onboarding socket', async (t) => {
  let stage = 0
  const mud = createServer((socket) => socket.on('data', (data) => {
    const frame = Buffer.from(data)
    if (stage === 0) { stage = 1; socket.write('MUD1O OK\n') }
    else if (stage === 1 && frame.toString() === 'Hero\n') { stage = 2; socket.write('MUD1O RESERVE|4865726f\n') }
    else if (stage === 2 && frame.toString('ascii') === `MUD1O RESERVED|${character}\n`) { stage = 3 }
    else if (stage === 3 && frame.toString() === 'm\n') { stage = 4; socket.write(`MUD1O SAVED|${character}|${'e'.repeat(64)}|player-v1\n`) }
    else if (stage === 4 && frame.toString('ascii').includes('MUD1O COMMIT\n')) {
      stage = 5
      const activation = frame.toString('ascii').match(/MUD1O ACTIVATED\|([0-9a-f-]+)\n/)
      if (activation) { stage = 6; socket.write(`MUD1O ACTIVE|${activation[1]}\n`) }
    }
    else if (stage === 5 && /^MUD1O ACTIVATED\|[0-9a-f-]+\n$/.test(frame.toString('ascii'))) {
      stage = 6; socket.write(frame.toString('ascii').replace('ACTIVATED', 'ACTIVE'))
    }
  }))
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const authorizer = new RecordingOnboardingAuthorizer()
  const gateway = createGateway(config, { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"onboarding-ready","mode":"provision"}')))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.equal(stage, 3))
  ws.send(Buffer.from('m\n'))
  await once(ws, 'close')
  assert.equal(stage, 6)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"provisioned","characterId":"${character}"}`))
})

test('input queued during finalize fail-closes when the bounded buffer is exceeded', async (t) => {
  const mud = new CommitCallbackMudSocket()
  const authorizer = new DeferredFinalizeAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500', MAX_FRAME_BYTES: '1024', MAX_BUFFERED_BYTES: '1024' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { authorizer.finishFinalize(); await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.match(binaryText(messages), /성별\? /))
  ws.send(Buffer.from('m\n'))
  await eventually(() => assert.equal(authorizer.finalizations.length, 1))

  ws.send(Buffer.alloc(600, 0x61))
  ws.send(Buffer.alloc(600, 0x62))
  const [closeCode] = await once(ws, 'close') as [number]
  assert.equal(closeCode, 1013)
  assert.equal(mud.writes.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n'), false)
})

test('a failed provision reconcile never commits the C transaction', async (t) => {
  const toMud: Buffer[] = []
  let stage = 0
  const mud = createServer((socket) => socket.on('data', (data) => {
    const frame = Buffer.from(data); toMud.push(frame)
    if (stage === 0) { stage = 1; socket.write('MUD1O OK\n') }
    else if (stage === 1 && frame.toString() === 'Hero\n') { stage = 2; socket.write('MUD1O RESERVE|4865726f\n') }
    else if (stage === 2 && frame.toString('ascii') === `MUD1O RESERVED|${character}\n`) { stage = 3 }
    else if (stage === 3 && frame.toString() === 'm\n') { stage = 4; socket.write(`MUD1O SAVED|${character}|${'d'.repeat(64)}|player-v1\n`) }
  }))
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const authorizer = new RecordingOnboardingAuthorizer(true, true)
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await new Promise<void>((resolve) => ws.once('message', () => resolve()))
  ws.send(Buffer.from('Hero\n'))
  await eventually(() => assert.equal(stage, 3))
  ws.send(Buffer.from('m\n'))
  const [code] = await once(ws, 'close') as [number]
  assert.equal(code, 1008)
  assert.deepEqual(authorizer.calls, ['begin', 'reserve', 'finalize', 'reconcile'])
  assert.equal(toMud.some((value) => value.toString('ascii') === 'MUD1O COMMIT\n'), false)
})

test('onboarding expiry timer fail-closes with 4001', async (t) => {
  const timers = new FakeTimers(1_700_000_000_000)
  const mud = createServer((socket) => socket.once('data', () => socket.write('MUD1O OK\n')))
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: new RecordingOnboardingAuthorizer(), authenticator: { verify: async () => ({ sub: actor, expiresAtMs: timers.nowMs + 1_000, claims: {} }) }, now: () => timers.nowMs, timers })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"onboarding-ready","mode":"provision"}')))
  const closed = once(ws, 'close') as Promise<[number]>
  timers.advance(1_000)
  const [code] = await closed
  assert.equal(code, 4001)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"error","reason":"token expired"}'))
})

test('onboarding output uses the legacy buffered-byte and callback-error fail-closed policy', () => {
  const failures: Array<[number, string]> = []
  const overLimit = {
    readyState: WebSocket.OPEN,
    bufferedAmount: 1_024,
    send: () => assert.fail('must not send over the buffered-byte limit'),
  } as unknown as WebSocket
  sendBufferedWebSocketFrame(overLimit, Buffer.from('x'), true, 1_024, (code, reason) => failures.push([code, reason]))

  const callbackFailure = {
    readyState: WebSocket.OPEN,
    bufferedAmount: 0,
    send: (_data: unknown, _options: unknown, callback: (error?: Error) => void) => callback(new Error('simulated send failure')),
  } as unknown as WebSocket
  sendBufferedWebSocketFrame(callbackFailure, 'text', false, 1_024, (code, reason) => failures.push([code, reason]))
  assert.deepEqual(failures, [[1013, 'slow consumer'], [1011, 'websocket output failed']])
})

test('claim relays the legacy password prompt, calls only the name-bound claim RPC, and closes normally', async (t) => {
  const toMud: Buffer[] = []
  let claimMudSocket: Socket | undefined
  let allowReceived = false
  let stage = 0
  const mud = createServer((socket) => { claimMudSocket = socket; socket.on('data', (data) => {
    const frame = Buffer.from(data); toMud.push(frame)
    if (stage === 0) { stage = 1; socket.write('MUD1O OK\n기존 이름? ') }
    else if (stage === 1 && frame.toString() === 'Alice\n') {
      stage = 2
      socket.write(`MUD1O CHALLENGE|416c696365|${'b'.repeat(64)}\n`)
    } else if (stage === 2 && frame.toString('ascii') === 'MUD1O ALLOW\n') {
      stage = 3; allowReceived = true
    } else if (stage === 3 && frame.toString() === 'old-secret\n') {
      stage = 4; socket.write(`MUD1O VERIFIED|416c696365|${'b'.repeat(64)}\n`)
    } else if (/^MUD1O ACTIVATED\|[0-9a-f-]+\n$/.test(frame.toString('ascii'))) {
      setImmediate(() => socket.write(frame.toString('ascii').replace('ACTIVATED', 'ACTIVE'), () => socket.end()))
    } else if (stage === 4 && frame.toString('ascii').includes(`MUD1O CLAIMED|${character}\n`)) {
      stage = 5
      const activation = frame.toString('ascii').match(/MUD1O ACTIVATED\|([0-9a-f-]+)\n/)
      if (activation) socket.write(`MUD1O ACTIVE|${activation[1]}\n`, () => socket.end())
    }
  }) })
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const authorizer = new RecordingOnboardingAuthorizer(false, false, undefined, 1)
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.equal(allowReceived, true))
  assert.equal(binaryText(messages).includes('게임 비밀번호? '), false)
  assert.equal(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"echo","enabled":false}'), false)
  claimMudSocket!.write(Buffer.concat([Buffer.from([0xff, 0xfb, 0x01]), Buffer.from('게임 비밀번호? ')]))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"echo","enabled":false}')))
  await eventually(() => assert.match(binaryText(messages), /게임 비밀번호\? /))
  ws.send(Buffer.from('old-secret\n'))
  const [code] = await once(ws, 'close')
  assert.equal(code, 1000)
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'claim', 'claim', 'activate', 'bind'])
  assert.deepEqual(authorizer.challenges.map(({ legacyNameKey, fileSha256 }) => ({ legacyNameKey, fileSha256 })), [{ legacyNameKey: 'Alice', fileSha256: 'b'.repeat(64) }])
  assert.deepEqual(authorizer.claimNames, ['Alice', 'Alice'])
  assert.deepEqual(authorizer.claimFingerprints, ['b'.repeat(64), 'b'.repeat(64)])
  assert.deepEqual(authorizer.leaseBegins, [])
  assert.deepEqual(authorizer.cancellations, [])
  assert.equal(stage, 5)
})

test('claim challenge rejection cancels the exact unreserved intent before ALLOW', async (t) => {
  const mud = new ClaimCompletionRaceMudSocket()
  const authorizer = new RejectingChallengeAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const received: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => received.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(received), /기존 이름\? /))
  const closed = once(ws, 'close') as Promise<[number]>
  ws.send(Buffer.from('Alice\n'))
  const [code] = await closed

  assert.equal(code, 1008)
  await eventually(() => assert.deepEqual(authorizer.cancellations, [{ actorUserId: actor, correlationId: correlation }]))
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'cancel'])
  assert.equal(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n'), false)
})

test('disconnect after a successful claim challenge cancels before VERIFIED or EVIDENCE', async () => {
  for (const evidenceEnabled of [false, true]) {
    const mud = new ClaimCompletionRaceMudSocket()
    const authorizer = new RecordingOnboardingAuthorizer()
    const config = loadConfig({
      NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0',
      ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500',
      ...(evidenceEnabled ? { MUD_ENABLE_ONBOARDING_EVIDENCE: '1' } : {}),
    })
    const gateway = createGateway(config, {
      onboardingAuthorizer: authorizer,
      characterAuthorizer: authorizer,
      authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
      ...(evidenceEnabled ? { evidenceFinalizer: { finalize: async (_request: Parameters<EvidenceFinalizer['finalize']>[0]): Promise<void> => {} } } : {}),
      connectTcp: () => { mud.connect(); return mud as unknown as Socket },
    })
    gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
    try {
      const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
      await once(ws, 'open')
      const received: Array<{ data: RawData, binary: boolean }> = []
      ws.on('message', (data, binary) => received.push({ data, binary }))
      ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
      await eventually(() => assert.match(binaryText(received), /기존 이름\? /))
      ws.send(Buffer.from('Alice\n'))
      await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n')))
      const closed = once(ws, 'close') as Promise<[number]>
      ws.close(1000)
      const [code] = await closed

      assert.equal(code, 1000)
      await eventually(() => assert.deepEqual(authorizer.cancellations, [{ actorUserId: actor, correlationId: correlation }]))
      assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'cancel'])
      assert.equal(mud.writes.some((value) => value.toString('ascii') === `MUD1O CLAIMED|${character}\n`), false)
    } finally {
      await gateway.close()
    }
  }
})

test('disconnect while claim challenge is pending cancels and never writes ALLOW after resolution', async (t) => {
  const mud = new ClaimCompletionRaceMudSocket()
  const authorizer = new DeferredChallengeAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { authorizer.finishChallenge(); await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const received: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => received.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(received), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'challenge']))
  const closed = once(ws, 'close') as Promise<[number]>
  ws.close(1000)
  const [code] = await closed
  assert.equal(code, 1000)

  await eventually(() => assert.deepEqual(authorizer.cancellations, [{ actorUserId: actor, correlationId: correlation }]))
  authorizer.finishChallenge()
  await gateway.close()
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'cancel'])
  assert.equal(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n'), false)
})

test('claim completion ignores synchronous C error and end events after CLAIMED', async (t) => {
  const mud = new ClaimCompletionRaceMudSocket()
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n')))
  ws.send(Buffer.from('old-secret\n'))
  const [code] = await once(ws, 'close') as [number]

  assert.equal(code, 1000)
  assert.equal(mud.destroyDuringEnd, 0, 'C close events after CLAIMED must not enter fail()')
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'claim', 'activate', 'bind'])
  assert.deepEqual(authorizer.cancellations, [])
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"claimed","characterId":"${character}"}`))
})

for (const outcome of ['accepted', 'rejected', 'missing ACTIVE'] as const) {
test(`claim EOF respects deferred snapshot binding (${outcome})`, async (t) => {
  const bindingFails = outcome === 'rejected'
  const sendsActive = outcome !== 'missing ACTIVE'
  const mud = new ClaimCompletionRaceMudSocket(sendsActive)
  let releaseBinding!: () => void
  const bindingGate = new Promise<void>((resolve) => { releaseBinding = resolve })
  const authorizer = new RecordingOnboardingAuthorizer()
  authorizer.bindSnapshotCommand = async () => {
    authorizer.calls.push('bind')
    await bindingGate
    if (bindingFails) throw new Error('snapshot binding rejected')
  }
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { releaseBinding(); await gateway.close() })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  const closed = once(ws, 'close')
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n')))
  ws.send(Buffer.from('old-secret\n'))
  await eventually(() => assert.ok(sendsActive ? authorizer.calls.includes('bind') :
    mud.writes.some((value) => value.toString('ascii').startsWith('MUD1O ACTIVATED|'))))
  // C closes immediately after ACTIVE. Deliver its EOF while the database
  // binding is definitely pending, then give the EOF handler a full turn.
  mud.emit('end')
  await new Promise<void>((resolve) => setImmediate(resolve))
  releaseBinding()
  const [code] = await closed as [number]
  assert.equal(code, !sendsActive ? 1011 : bindingFails ? 1008 : 1000)
  assert.deepEqual(authorizer.calls, sendsActive ? ['begin', 'challenge', 'claim', 'activate', 'bind'] : ['begin', 'challenge', 'claim', 'activate'])
  assert.equal(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"claimed","characterId":"${character}"}`), outcome === 'accepted')
  assert.equal(messages.some(({ data, binary }) => !binary && JSON.parse(Buffer.from(data).toString()).type === 'error'), outcome !== 'accepted')
})
}

test('claim completion drops later C game bytes while ownership finalization is pending', async (t) => {
  const trailingGame = Buffer.from('later C gameplay fragment')
  const mud = new ClaimCompletionRaceMudSocket()
  const authorizer = new DeferredClaimAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { authorizer.finishClaim(); await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  // Activation can close immediately after the CLAIMED write callback.
  const claimCompletionClose = once(ws, 'close')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n')))
  ws.send(Buffer.from('old-secret\n'))
  await eventually(() => assert.equal(authorizer.calls.filter((call) => call === 'claim').length, 1))

  mud.emit('data', trailingGame)
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.equal(ws.readyState, WebSocket.OPEN)
  assert.equal(messages.some(({ data, binary }) => binary && Buffer.from(data).equals(trailingGame)), false)
  assert.equal(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString().includes('"type":"error"')), false)
  authorizer.finishClaim()
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === `MUD1O CLAIMED|${character}\n`)))
  const [closeCode] = await claimCompletionClose as [number]
  assert.equal(closeCode, 1000)
  assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === `{"type":"claimed","characterId":"${character}"}`))
  assert.deepEqual(authorizer.cancellations, [])
})

test('first confirmed claim rejection cancels once without retrying or sending CLAIMED', async (t) => {
  const mud = new ClaimCompletionRaceMudSocket()
  const authorizer = new SequencedClaimAuthorizer([new OnboardingClaimRejectedError()])
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n')))
  const closed = once(ws, 'close') as Promise<[number]>
  ws.send(Buffer.from('old-secret\n'))
  const [code] = await closed

  assert.equal(code, 1008)
  await eventually(() => assert.deepEqual(authorizer.cancellations, [{ actorUserId: actor, correlationId: correlation }]))
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'claim', 'cancel'])
  assert.equal(mud.writes.some((value) => value.toString('ascii') === `MUD1O CLAIMED|${character}\n`), false)
  assert.equal(mud.writes.some((value) => /^MUD1O ACTIVATED\|/.test(value.toString('ascii'))), false)
})

test('indeterminate claim rejection followed by confirmed rejection preserves no-cancel history', async (t) => {
  const mud = new ClaimCompletionRaceMudSocket()
  const authorizer = new SequencedClaimAuthorizer([
    new OnboardingAuthorizationError(true), new OnboardingClaimRejectedError(),
  ])
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { mud.connect(); return mud as unknown as Socket },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /기존 이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(mud.writes.some((value) => value.toString('ascii') === 'MUD1O ALLOW\n')))
  const closed = once(ws, 'close') as Promise<[number]>
  ws.send(Buffer.from('old-secret\n'))
  const [code] = await closed

  assert.equal(code, 1008)
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'claim', 'claim'])
  assert.deepEqual(authorizer.cancellations, [])
  assert.equal(mud.writes.some((value) => value.toString('ascii') === `MUD1O CLAIMED|${character}\n`), false)
  assert.equal(mud.writes.some((value) => /^MUD1O ACTIVATED\|/.test(value.toString('ascii'))), false)
})

test('claim rejects out-of-order VERIFIED and never exposes private claim controls to the browser', async (t) => {
  const sha = 'b'.repeat(64)
  const mud = createServer((socket) => socket.once('data', () => {
    socket.write(`MUD1O OK\nMUD1O VERIFIED|416c696365|${sha}\n`)
  }))
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const authorizer = new RecordingOnboardingAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'private-browser-token', mode: 'claim', correlationId: correlation }))
  const [code] = await once(ws, 'close') as [number]
  assert.equal(code, 1008)
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin', 'cancel']))
  const browserText = messages.map(({ data, binary }) => binary ? Buffer.from(data).toString('utf8') : Buffer.from(data).toString()).join('')
  assert.equal(browserText.includes('CHALLENGE'), false)
  assert.equal(browserText.includes('ALLOW'), false)
  assert.equal(browserText.includes(sha), false)
  assert.equal(browserText.includes('416c696365'), false)
})

test('claim does not retry a deterministic programmer error from the finalizer', async (t) => {
  const sha = 'b'.repeat(64)
  let stage = 0
  const mud = createServer((socket) => socket.on('data', (data) => {
    const frame = Buffer.from(data).toString()
    if (stage === 0) { stage = 1; socket.write('MUD1O OK\n이름? ') }
    else if (stage === 1 && frame === 'Alice\n') { stage = 2; socket.write(`MUD1O CHALLENGE|416c696365|${sha}\n`) }
    else if (stage === 2 && frame === 'MUD1O ALLOW\n') { stage = 3; socket.write(Buffer.concat([Buffer.from([0xff, 0xfb, 0x01]), Buffer.from('게임 비밀번호? ')])) }
    else if (stage === 3 && frame === 'old-secret\n') { stage = 4; socket.write(`MUD1O VERIFIED|416c696365|${sha}\n`) }
  }))
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const authorizer = new RecordingOnboardingAuthorizer(false, false, undefined, 1, new Error('programmer error'))
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, { onboardingAuthorizer: authorizer, characterAuthorizer: authorizer, authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) } })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: Array<{ data: RawData, binary: boolean }> = []
  ws.on('message', (data, binary) => messages.push({ data, binary }))
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'claim', correlationId: correlation }))
  await eventually(() => assert.match(binaryText(messages), /이름\? /))
  ws.send(Buffer.from('Alice\n'))
  await eventually(() => assert.ok(messages.some(({ data, binary }) => !binary && Buffer.from(data).toString() === '{"type":"echo","enabled":false}')))
  await eventually(() => assert.match(binaryText(messages), /게임 비밀번호\? /))
  ws.send(Buffer.from('old-secret\n'))
  const [code] = await once(ws, 'close') as [number]
  assert.equal(code, 1008)
  assert.deepEqual(authorizer.calls, ['begin', 'challenge', 'claim'])
  assert.equal(stage, 4)
})

test('disabled onboarding rejects the isolated endpoint without opening MUD TCP', async (t) => {
  let tcpConnections = 0
  const mud = createServer(() => { tcpConnections += 1 })
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const address = mud.address(); assert.ok(address && typeof address !== 'string')
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(address.port), ALLOWED_ORIGINS: 'http://localhost:3000' })
  const gateway = createGateway(config)
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close(); await new Promise<void>((resolve) => mud.close(() => resolve())) })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  const [, response] = await once(ws, 'unexpected-response')
  assert.equal((response as import('node:http').IncomingMessage).statusCode, 403)
  assert.equal(tcpConnections, 0)
  ;(response as import('node:http').IncomingMessage).resume()
})

test('gateway close resolves after an upgraded socket ignores the close handshake', async (t) => {
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: '1', ALLOWED_ORIGINS: 'http://localhost:3000', SHUTDOWN_GRACE_MS: '100', AUTH_TIMEOUT_MS: '500' })
  const gateway = createGateway(config)
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { await gateway.close() })
  const address = gateway.server.address(); assert.ok(address && typeof address !== 'string')
  const httpSocket = createConnection(address.port, address.address)
  t.after(() => httpSocket.destroy())
  await once(httpSocket, 'connect')
  let httpResponse = ''
  httpSocket.on('data', (data) => { httpResponse += data.toString('ascii') })
  httpSocket.write(`GET /healthz HTTP/1.1\r\nHost: ${address.address}:${address.port}\r\nConnection: keep-alive\r\n\r\n`)
  await eventually(() => assert.match(httpResponse, /HTTP\/1\.1 200/))
  const socket = createConnection(address.port, address.address)
  t.after(() => socket.destroy())
  await once(socket, 'connect')
  let response = ''
  socket.on('data', (data) => { response += data.toString('ascii') })
  socket.write([
    'GET /ws HTTP/1.1', `Host: ${address.address}:${address.port}`, 'Upgrade: websocket', 'Connection: Upgrade',
    'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==', 'Sec-WebSocket-Version: 13', 'Sec-WebSocket-Protocol: muhan.v1',
    'Origin: http://localhost:3000', '', ''
  ].join('\r\n'))
  await eventually(() => assert.match(response, /HTTP\/1\.1 101/))
  const closing = gateway.close()
  const resolvedDuringGrace = await Promise.race([
    closing.then(() => true),
    new Promise<boolean>((resolve) => setTimeout(() => resolve(false), 250)),
  ])
  socket.destroy()
  assert.equal(httpSocket.destroyed, true)
  await closing
  assert.equal(resolvedDuringGrace, true)
})

test('gateway close waits for an in-flight onboarding cancellation to settle', async (t) => {
  const authorizer = new DeferredBeginAndCancelAuthorizer()
  const config = loadConfig({ NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true', HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: '1', ALLOWED_ORIGINS: 'http://localhost:3000', SHUTDOWN_GRACE_MS: '100', AUTH_TIMEOUT_MS: '500' })
  const gateway = createGateway(config, {
    onboardingAuthorizer: authorizer,
    characterAuthorizer: authorizer,
    authenticator: { verify: async () => ({ sub: actor, expiresAtMs: Date.now() + 60_000, claims: {} }) },
    connectTcp: () => { throw new Error('must not connect after close') },
  })
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => { authorizer.finishBegin(); authorizer.finishCancel(); await gateway.close() })
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/onboarding`, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  ws.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token-not-for-logs', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.deepEqual(authorizer.calls, ['begin']))
  const closing = gateway.close()
  authorizer.finishBegin()
  await eventually(() => assert.equal(authorizer.cancellations.length, 1))
  const releaseTimer = setTimeout(() => authorizer.finishCancel(), 50)
  await closing
  clearTimeout(releaseTimer)
  assert.equal(authorizer.cancelFinished, true)
})

class FakeTimers implements GatewayTimers {
  nowMs: number
  private readonly timers = new Map<object, { due: number, callback: () => void }>()

  constructor(nowMs: number) { this.nowMs = nowMs }
  setTimeout = (callback: () => void, delayMs: number): NodeJS.Timeout => {
    const handle = { unref() {} }
    this.timers.set(handle, { due: this.nowMs + delayMs, callback })
    return handle as unknown as NodeJS.Timeout
  }
  clearTimeout = (timer: NodeJS.Timeout): void => { this.timers.delete(timer as unknown as object) }
  advance(ms: number): void {
    this.nowMs += ms
    for (;;) {
      const due = [...this.timers.entries()].find(([, timer]) => timer.due <= this.nowMs)
      if (!due) return
      this.timers.delete(due[0])
      due[1].callback()
    }
  }
}

function binaryText(messages: Array<{ data: RawData, binary: boolean }>): string {
  return Buffer.concat(messages.filter(({ binary }) => binary).map(({ data }) => Buffer.from(data))).toString()
}

async function eventually(fn: () => void, timeoutMs = 1_000): Promise<void> { const deadline = Date.now() + timeoutMs; for (;;) { try { fn(); return } catch (error) { if (Date.now() >= deadline) throw error; await new Promise((resolve) => setTimeout(resolve, 5)) } } }
