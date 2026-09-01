import assert from 'node:assert/strict'
import { once } from 'node:events'
import { createServer, type Server, type Socket } from 'node:net'
import test from 'node:test'
import WebSocket, { type RawData } from 'ws'
import { type AuthenticatedIdentity } from '../src/auth.js'
import { type AuthorizedCharacter, type BeginCharacterSessionRequest, type CharacterAuthorizer, type RenewCharacterSessionRequest, type RenewedCharacterSession } from '../src/character-authorizer.js'
import { loadConfig } from '../src/config.js'
import { createGateway, type GatewayDependencies, type RunningGateway } from '../src/gateway.js'

interface ReceivedMessage { data: RawData; isBinary: boolean }

const actor = '123e4567-e89b-12d3-a456-426614174000'
const character = '123e4567-e89b-12d3-a456-426614174001'
const session = '123e4567-e89b-12d3-a456-426614174002'
const secret = '0123456789abcdef0123456789abcdef'

class RecordingAuthorizer implements CharacterAuthorizer {
  readonly begins: BeginCharacterSessionRequest[] = []
  readonly renewals: RenewCharacterSessionRequest[] = []
  readonly releases: Array<{ sessionId: string, gatewayInstanceId: string }> = []
  constructor(private readonly result: AuthorizedCharacter = { legacyNameKey: 'Contracthero' }, private readonly rejected = false, private readonly endFails = false) {}

  async beginSession(request: BeginCharacterSessionRequest): Promise<AuthorizedCharacter> {
    this.begins.push(request)
    if (this.rejected) throw new Error('untrusted test rejection')
    return this.result
  }

  async endSession(sessionId: string, gatewayInstanceId: string): Promise<void> {
    this.releases.push({ sessionId, gatewayInstanceId })
    if (this.endFails) throw new Error('untrusted test release failure')
  }

  async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    this.renewals.push(request)
    return {
      sessionId: request.sessionId,
      actorUserId: actor,
      characterId: character,
      legacyNameKey: this.result.legacyNameKey,
      lifecycle: 'active',
      expiresAtMs: request.expiresAt.getTime()
    }
  }
}

class RejectingRenewalAuthorizer extends RecordingAuthorizer {
  override async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    this.renewals.push(request)
    throw new Error('renewal rejected')
  }
}

class MismatchedRenewalAuthorizer extends RecordingAuthorizer {
  override async renewSession(request: RenewCharacterSessionRequest): Promise<RenewedCharacterSession> {
    this.renewals.push(request)
    return {
      sessionId: '123e4567-e89b-12d3-a456-426614174099',
      actorUserId: actor,
      characterId: character,
      legacyNameKey: 'Contracthero',
      lifecycle: 'active',
      expiresAtMs: request.expiresAt.getTime()
    }
  }
}

class FakeTimers {
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
      const next = [...this.timers.entries()].find(([, entry]) => entry.due <= this.nowMs)
      if (!next) return
      this.timers.delete(next[0]); next[1].callback()
    }
  }

  get pending(): number { return this.timers.size }
}

function identity(expiresAtMs = Date.now() + 60_000): { verify(): Promise<AuthenticatedIdentity> } {
  return { verify: async () => ({ sub: actor, expiresAtMs, claims: {} }) }
}

function config(mudPort: number): ReturnType<typeof loadConfig> {
  return loadConfig({
    NODE_ENV: 'test', AUTH_DISABLED: 'true', ALLOWED_ORIGINS: 'http://localhost:3000',
    MUD_HOST: '127.0.0.1', MUD_PORT: String(mudPort), PORT: '0', HOST: '127.0.0.1',
    AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '120', SHUTDOWN_GRACE_MS: '100',
    MUD_ADMISSION_SECRET: secret, GATEWAY_INSTANCE_ID: 'gateway-contract'
  })
}

function dependencies(authorizer: CharacterAuthorizer, logs: string[] = [], now: () => number = Date.now): GatewayDependencies {
  return {
    authenticator: identity(), characterAuthorizer: authorizer,
    logger: { info() {}, warn(message: unknown) { logs.push(String(message)) }, error() {} },
    now, randomUuid: () => session, randomBytes: () => Buffer.from([...Array(16).keys()])
  }
}

async function openWs(gateway: RunningGateway): Promise<{ ws: WebSocket, messages: ReceivedMessage[] }> {
  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: ReceivedMessage[] = []
  ws.on('message', (data, isBinary) => messages.push({ data, isBinary }))
  return { ws, messages }
}

function authFrame(): string {
  return JSON.stringify({ type: 'auth', accessToken: 'browser-token-not-for-logs', characterId: character })
}

async function startGateway(mud: Server, dependenciesValue: GatewayDependencies): Promise<RunningGateway> {
  const address = mud.address()
  assert.ok(address && typeof address !== 'string')
  const gateway = createGateway(config(address.port), dependenciesValue)
  gateway.server.listen(0, '127.0.0.1')
  await once(gateway.server, 'listening')
  return gateway
}

test('auth-first trusted relay writes ticket, waits for fragmented ACK, and preserves coalesced game bytes', async (t) => {
  const receivedFromGateway: Buffer[] = []
  let mudConnections = 0
  const mud = createServer((socket) => {
    mudConnections += 1
    socket.once('data', (data) => {
      receivedFromGateway.push(Buffer.from(data))
      socket.on('data', (input) => receivedFromGateway.push(Buffer.from(input)))
      socket.write('MUD1 ')
      setTimeout(() => socket.write(Buffer.concat([
        Buffer.from('OK\n'), Buffer.from([0xec, 0x95, 0x88, 0xff, 0xfb, 0x01, 0x0d, 0x0a])
      ])), 5)
    })
  })
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const authorizer = new RecordingAuthorizer()
  const gateway = await startGateway(mud, dependencies(authorizer))
  t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

  const { ws, messages } = await openWs(gateway)
  assert.equal(mudConnections, 0, 'TCP MUD must not be reached before the auth frame')
  ws.send(authFrame())
  await waitForMessage(messages, ({ data, isBinary }) => !isBinary && Buffer.from(data).toString() === '{"type":"ready"}')
  await waitForMessage(messages, ({ data, isBinary }) => !isBinary && Buffer.from(data).toString() === '{"type":"echo","enabled":false}')
  await eventually(() => assert.equal(Buffer.concat(messages.filter(({ isBinary }) => isBinary).map(({ data }) => Buffer.from(data))).toString('utf8'), '안\r\n'))
  const received = Buffer.concat(receivedFromGateway)
  const ticketEnd = received.indexOf(0x0a) + 1
  assert.match(received.subarray(0, ticketEnd).toString('ascii'), /^MUD1\|\d+\|000102030405060708090a0b0c0d0e0f\|123e4567-e89b-12d3-a456-426614174000\|123e4567-e89b-12d3-a456-426614174001\|436f6e74726163746865726f\|[0-9a-f]{64}\n$/)
  ws.send(Buffer.from('look\n'))
  await eventually(() => {
    const input = Buffer.concat(receivedFromGateway)
    const ticketEnd = input.indexOf(0x0a) + 1
    assert.deepEqual(input.subarray(ticketEnd, ticketEnd + 3), Buffer.from([0xff, 0xfd, 0x01]))
    assert.equal(input.subarray(ticketEnd + 3).toString('utf8'), 'look\n')
  })
  ws.close()
})

test('normal MUD TCP end after admission sends closed without an error and releases the lease', async (t) => {
  const mud = createServer((socket) => {
    socket.once('data', () => {
      socket.write('MUD1 OK\n')
      setTimeout(() => socket.end(), 10)
    })
  })
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const authorizer = new RecordingAuthorizer()
  const gateway = await startGateway(mud, dependencies(authorizer))
  t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

  const { ws, messages } = await openWs(gateway)
  ws.send(authFrame())
  await waitForMessage(messages, ({ data, isBinary }) => !isBinary && Buffer.from(data).toString() === '{"type":"ready"}')
  const [code] = await once(ws, 'close') as [number]

  assert.equal(code, 1000)
  assert.ok(messages.some(({ data, isBinary }) => !isBinary && Buffer.from(data).toString() === '{"type":"closed","reason":"MUD connection closed"}'))
  assert.ok(!messages.some(({ data, isBinary }) => !isBinary && Buffer.from(data).toString().includes('"type":"error"')))
  await eventually(() => assert.deepEqual(authorizer.releases, [{ sessionId: session, gatewayInstanceId: 'gateway-contract' }]))
})

test('non-owner authorization rejection never opens MUD TCP and releases the exact attempted session', async (t) => {
  let mudConnections = 0
  const mud = createServer(() => { mudConnections += 1 })
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const authorizer = new RecordingAuthorizer({ legacyNameKey: 'Contracthero' }, true)
  const gateway = await startGateway(mud, dependencies(authorizer))
  t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

  const { ws, messages } = await openWs(gateway)
  ws.send(authFrame())
  await once(ws, 'close')
  assert.equal(mudConnections, 0)
  assert.ok(messages.some(({ data }) => Buffer.from(data).toString().includes('authentication or character authorization failed')))
  await eventually(() => assert.deepEqual(authorizer.releases, [{ sessionId: session, gatewayInstanceId: 'gateway-contract' }]))
})

test('auth frame rejects unknown fields before authentication or MUD TCP', async (t) => {
  let mudConnections = 0
  const mud = createServer(() => { mudConnections += 1 })
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const authorizer = new RecordingAuthorizer()
  const gateway = await startGateway(mud, dependencies(authorizer))
  t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

  const { ws } = await openWs(gateway)
  ws.send(JSON.stringify({
    type: 'auth', accessToken: 'browser-token-not-for-logs', characterId: character, legacyName: 'Injected'
  }))
  await once(ws, 'close')
  assert.equal(mudConnections, 0)
  assert.deepEqual(authorizer.begins, [])
})

for (const scenario of [
  { name: 'ERR', response: (socket: Socket) => socket.write('MUD1 ERR\n') },
  { name: 'end', response: (socket: Socket) => socket.end() },
  { name: 'oversized preface', response: (socket: Socket) => socket.write(Buffer.alloc(257, 0x41)) },
  { name: 'timeout', response: (_socket: Socket) => {} }
]) {
  test(`fails closed and releases lease when MUD admission ${scenario.name}`, async (t) => {
    const mud = createServer((socket) => socket.once('data', () => scenario.response(socket)))
    mud.listen(0, '127.0.0.1')
    await once(mud, 'listening')
    const authorizer = new RecordingAuthorizer()
    const gateway = await startGateway(mud, dependencies(authorizer))
    t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

    const { ws, messages } = await openWs(gateway)
    ws.send(authFrame())
    await once(ws, 'close')
    assert.ok(!messages.some(({ data }) => Buffer.from(data).toString() === '{"type":"ready"}'))
    await eventually(() => assert.deepEqual(authorizer.releases, [{ sessionId: session, gatewayInstanceId: 'gateway-contract' }]))
  })
}

test('renews an active lease on the injected clock and stops renewal timers on cleanup', async (t) => {
  const timers = new FakeTimers(1_700_000_000_000)
  const mud = createServer((socket) => socket.once('data', () => socket.write('MUD1 OK\n')))
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const authorizer = new RecordingAuthorizer()
  const gateway = await startGateway(mud, {
    ...dependencies(authorizer, [], () => timers.nowMs),
    authenticator: identity(timers.nowMs + 3_600_000),
    timers
  })
  t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

  const { ws, messages } = await openWs(gateway)
  ws.send(authFrame())
  await waitForMessage(messages, ({ data }) => Buffer.from(data).toString() === '{"type":"ready"}')
  assert.equal(authorizer.begins[0]!.expiresAt.getTime(), timers.nowMs + 120_000)
  timers.advance(60_000)
  await eventually(() => assert.equal(authorizer.renewals.length, 1))
  assert.equal(authorizer.renewals[0]!.expiresAt.getTime(), timers.nowMs + 120_000)
  ws.close()
  await once(ws, 'close')
  await eventually(() => assert.deepEqual(authorizer.releases, [{ sessionId: session, gatewayInstanceId: 'gateway-contract' }]))
  assert.equal(timers.pending, 0, 'all session timers must be cleared after cleanup')
  timers.advance(120_000)
  await new Promise((resolve) => setImmediate(resolve))
  assert.equal(authorizer.renewals.length, 1, 'closed session must not renew again')
})

for (const Authorizer of [RejectingRenewalAuthorizer, MismatchedRenewalAuthorizer]) {
  test(`fails closed when lease renewal is ${Authorizer === RejectingRenewalAuthorizer ? 'rejected' : 'mismatched'}`, async (t) => {
    const timers = new FakeTimers(1_700_000_000_000)
    const mud = createServer((socket) => socket.once('data', () => socket.write('MUD1 OK\n')))
    mud.listen(0, '127.0.0.1')
    await once(mud, 'listening')
    const authorizer = new Authorizer()
    const gateway = await startGateway(mud, {
      ...dependencies(authorizer, [], () => timers.nowMs),
      authenticator: identity(timers.nowMs + 3_600_000),
      timers
    })
    t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

    const { ws, messages } = await openWs(gateway)
    ws.send(authFrame())
    await waitForMessage(messages, ({ data }) => Buffer.from(data).toString() === '{"type":"ready"}')
    timers.advance(60_000)
    await once(ws, 'close')
    assert.ok(messages.some(({ data }) => Buffer.from(data).toString().includes('character session renewal failed')))
    await eventually(() => assert.deepEqual(authorizer.releases, [{ sessionId: session, gatewayInstanceId: 'gateway-contract' }]))
    assert.equal(timers.pending, 0)
  })
}

test('redacts release failures and refuses admission when ticket expiry cannot survive the current second', async (t) => {
  let mudConnections = 0
  const mud = createServer(() => { mudConnections += 1 })
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const logs: string[] = []
  const nowMs = 1_700_000_000_900
  const authorizer = new RecordingAuthorizer({ legacyNameKey: 'Contracthero' }, false, true)
  const gateway = await startGateway(mud, {
    ...dependencies(authorizer, logs, () => nowMs),
    authenticator: identity(1_700_000_000_950)
  })
  t.after(async () => { await closeGateway(gateway); await closeServer(mud) })

  const { ws } = await openWs(gateway)
  ws.send(authFrame())
  await once(ws, 'close')
  assert.equal(mudConnections, 0)
  await eventually(() => assert.equal(authorizer.releases.length, 3))
  assert.ok(authorizer.releases.every((release) =>
    release.sessionId === session && release.gatewayInstanceId === 'gateway-contract'))
  await eventually(() => assert.deepEqual(logs, ['character session lease release failed after bounded retries']))
  assert.ok(logs.every((line) => !line.includes(secret) && !line.includes('browser-token-not-for-logs') && !line.includes('MUD1|')))
})

async function waitForMessage(messages: ReceivedMessage[], predicate: (message: ReceivedMessage) => boolean): Promise<ReceivedMessage> {
  let found: ReceivedMessage | undefined
  await eventually(() => { found = messages.find(predicate); assert.ok(found, 'timed out waiting for websocket message') })
  return found!
}

async function eventually(assertion: () => void): Promise<void> {
  let lastError: unknown
  for (let attempt = 0; attempt < 50; attempt += 1) {
    try { assertion(); return } catch (error) { lastError = error; await new Promise((resolve) => setTimeout(resolve, 10)) }
  }
  throw lastError
}

async function closeGateway(gateway: RunningGateway): Promise<void> { await gateway.close() }

async function closeServer(server: Server): Promise<void> {
  server.closeAllConnections?.()
  await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
}
