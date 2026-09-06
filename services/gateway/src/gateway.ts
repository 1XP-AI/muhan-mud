import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import { createHash, randomUUID } from 'node:crypto'
import { Socket, createConnection } from 'node:net'
import { URL } from 'node:url'
import { WebSocketServer, WebSocket, type RawData } from 'ws'
import type { GatewayConfig } from './config.js'
import { AuthenticationError, SupabaseAuthenticator, type AuthenticatedIdentity } from './auth.js'
import { createAdmissionTicket, admissionTicketLimits } from './admission-ticket.js'
import {
  CharacterAuthorizationError,
  type CharacterAuthorizer,
  isStrictLowerUuid,
  SupabaseCharacterAuthorizer,
  TestOnlyCharacterAuthorizer
} from './character-authorizer.js'
import { TelnetParser } from './telnet.js'
import { createOnboardingTicket, OnboardingControlDemultiplexer, OnboardingProtocolError, parseOnboardingAuthFrame, type OnboardingControl } from './onboarding-protocol.js'
import { OnboardingAuthorizationError, SupabaseOnboardingAuthorizer, TestOnlyOnboardingAuthorizer, type BindSnapshotCommandRequest, type ChallengeOnboardingRequest, type OnboardingAuthorizer } from './onboarding-authorizer.js'
import { GatewayEvidenceFinalizer, SupabaseEvidenceFinalizerTransport, type FinalizeLegacyIdentityEvidenceRequest } from './evidence-finalizer.js'

const PROTOCOL = 'muhan.v1'
const ONBOARDING_PROTOCOL = 'muhan.onboarding.v1'
const CLOSE_POLICY = 1008
const CLOSE_NORMAL = 1000
const CLOSE_TRY_AGAIN = 1013
const CLOSE_INTERNAL = 1011
const CLOSE_RESTART = 1012
const CLOSE_TOKEN_EXPIRED = 4001
const LEASE_TTL_MS = 120_000
const LEASE_RENEW_INTERVAL_MS = 60_000
const LEASE_MIN_REMAINING_MS = 30_000
const CLAIM_ALLOW_TTL_MS = 90_000
const LEASE_RELEASE_RETRY_DELAYS_MS = [50, 200] as const
const ONBOARDING_CANCEL_RETRY_DELAYS_MS = [50, 200] as const

interface Authenticator {
  verify(accessToken: string): Promise<AuthenticatedIdentity>
}

export interface EvidenceFinalizer {
  finalize(request: FinalizeLegacyIdentityEvidenceRequest): Promise<void>
}

export interface GatewayDependencies {
  authenticator?: Authenticator
  characterAuthorizer?: CharacterAuthorizer
  onboardingAuthorizer?: OnboardingAuthorizer
  evidenceFinalizer?: EvidenceFinalizer
  connectTcp?: (host: string, port: number) => Socket
  logger?: Pick<Console, 'info' | 'warn' | 'error'>
  now?: () => number
  randomBytes?: (size: number) => Buffer
  randomUuid?: () => string
  timers?: GatewayTimers
}

export interface GatewayTimers {
  setTimeout(callback: () => void, delayMs: number): NodeJS.Timeout
  clearTimeout(timer: NodeJS.Timeout): void
}

const systemTimers: GatewayTimers = { setTimeout, clearTimeout }

function unrefTimer(timer: NodeJS.Timeout): void {
  timer.unref?.()
}

export interface RunningGateway {
  server: Server
  address(): string
  close(): Promise<void>
}

type SessionState = 'awaiting-auth' | 'connecting' | 'awaiting-admission' | 'ready' | 'closed'

class ByteRateLimiter {
  private available: number
  private previous: number

  constructor(private readonly bytesPerSecond: number, private readonly now: () => number) {
    this.available = bytesPerSecond
    this.previous = now()
  }

  take(bytes: number): boolean {
    const now = this.now()
    this.available = Math.min(this.bytesPerSecond, this.available + ((now - this.previous) * this.bytesPerSecond) / 1_000)
    this.previous = now
    if (bytes > this.available) return false
    this.available -= bytes
    return true
  }
}

function jsonFrame(value: Record<string, unknown>): string {
  return JSON.stringify(value)
}

/** Shared outbound guard so the isolated onboarding path cannot bypass MUD1 backpressure policy. */
export function sendBufferedWebSocketFrame(
  ws: WebSocket,
  data: Buffer | string,
  binary: boolean,
  maxBufferedBytes: number,
  onFailure: (code: number, reason: string) => void,
): void {
  if (ws.readyState !== WebSocket.OPEN) return
  const byteLength = Buffer.isBuffer(data) ? data.length : Buffer.byteLength(data)
  if (ws.bufferedAmount + byteLength > maxBufferedBytes) {
    onFailure(CLOSE_TRY_AGAIN, 'slow consumer')
    return
  }
  ws.send(data, { binary }, (error) => {
    if (error) onFailure(CLOSE_INTERNAL, 'websocket output failed')
  })
}

function closeSocket(ws: WebSocket, code: number, reason: string): void {
  if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) ws.close(code, reason)
}

function rawToBuffer(data: RawData): Buffer {
  if (Buffer.isBuffer(data)) return data
  if (Array.isArray(data)) return Buffer.concat(data)
  return Buffer.from(data)
}

function isSecureRequest(request: IncomingMessage): boolean {
  if ((request.socket as Socket & { encrypted?: boolean }).encrypted) return true
  const forwarded = request.headers['x-forwarded-proto']
  return typeof forwarded === 'string' && forwarded.split(',')[0].trim() === 'https'
}

interface AuthFrame {
  accessToken: string
  characterId: string
}

function parseAuthMessage(data: Buffer): AuthFrame {
  let frame: unknown
  try {
    frame = JSON.parse(data.toString('utf8'))
  } catch {
    throw new AuthenticationError('first frame must be valid auth JSON')
  }
  if (!frame || typeof frame !== 'object' || Array.isArray(frame)) throw new AuthenticationError('first frame must be an auth object')
  const value = frame as Record<string, unknown>
  const keys = Object.keys(value).sort()
  if (keys.length !== 3 || keys[0] !== 'accessToken' || keys[1] !== 'characterId' || keys[2] !== 'type' ||
      value.type !== 'auth' || typeof value.accessToken !== 'string' || value.accessToken.length === 0 ||
      value.accessToken.length > 12_000 || !isStrictLowerUuid(value.characterId)) {
    throw new AuthenticationError('first frame must contain an auth accessToken and lowercase characterId UUID')
  }
  return { accessToken: value.accessToken, characterId: value.characterId }
}

function isPingMessage(data: Buffer): boolean {
  try {
    const frame: unknown = JSON.parse(data.toString('utf8'))
    return !!frame && typeof frame === 'object' && !Array.isArray(frame) && (frame as Record<string, unknown>).type === 'ping'
  } catch {
    return false
  }
}

function isFinalizableEvidence(event: Extract<OnboardingControl, { type: 'EVIDENCE' }>, trusted: TrustedOnboardingCompletion, expectedSha256?: string): boolean {
  const evidence = event.evidence
  return event.version === 1 && evidence.outcome === 'ok' &&
    (evidence.canonicalization === 'canonical' || evidence.canonicalization === 'normalized') &&
    evidence.storageFormat === 'player-v1' && evidence.canonicalName === trusted.legacyNameKey &&
    /^[0-9a-f]{64}$/.test(evidence.playerFileSha256) &&
    (expectedSha256 === undefined || evidence.playerFileSha256 === expectedSha256) &&
    evidence.legacyShard === createHash('sha1').update(evidence.canonicalName, 'utf8').digest('hex').slice(0, 2)
}

export function createGateway(config: GatewayConfig, dependencies: GatewayDependencies = {}): RunningGateway {
  const logger = dependencies.logger ?? console
  const now = dependencies.now ?? Date.now
  const authenticator = dependencies.authenticator ?? (config.authDisabled
    ? { verify: async (): Promise<AuthenticatedIdentity> => ({ sub: TestOnlyCharacterAuthorizer.actorUserId, expiresAtMs: now() + 3_600_000, claims: {} }) }
    : new SupabaseAuthenticator(config))
  const characterAuthorizer = dependencies.characterAuthorizer ?? (config.authDisabled
    ? new TestOnlyCharacterAuthorizer()
    : new SupabaseCharacterAuthorizer(config))
  const onboardingAuthorizer = dependencies.onboardingAuthorizer ?? (config.authDisabled
    ? new TestOnlyOnboardingAuthorizer()
    : new SupabaseOnboardingAuthorizer(config))
  // Do not construct a service transport while the feature is off. Enabled
  // production uses the narrow RPC-only transport; isolated tests inject it.
  const evidenceFinalizer = dependencies.evidenceFinalizer ?? (config.mudOnboardingEvidenceEnabled
    ? new GatewayEvidenceFinalizer(new SupabaseEvidenceFinalizerTransport(config))
    : undefined)
  const connectTcp = dependencies.connectTcp ?? ((host, port) => createConnection({ host, port }))
  let accepting = true
  let activeConnections = 0
  const sessions = new Set<GatewaySession | OnboardingSession>()
  const allSessions = new Set<GatewaySession | OnboardingSession>()
  const sockets = new Set<Socket>()
  let closePromise: Promise<void> | undefined

  const server = createServer((request, response) => {
    if (request.url === '/healthz' && request.method === 'GET') {
      response.writeHead(accepting ? 200 : 503, { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' })
      response.end(JSON.stringify({ status: accepting ? 'ok' : 'draining', activeConnections }))
      return
    }
    response.writeHead(404, { 'content-type': 'application/json; charset=utf-8' })
    response.end('{"error":"not found"}')
  })
  // `server.close()` does not account for sockets upgraded to WebSocket.
  // Keep the transport handles themselves so shutdown can destroy any socket
  // whose peer does not complete the WebSocket close handshake.
  server.on('connection', (socket) => {
    sockets.add(socket)
    socket.once('close', () => sockets.delete(socket))
  })

  const webSocketServer = new WebSocketServer({
    noServer: true,
    maxPayload: config.maxFrameBytes,
    clientTracking: false,
    handleProtocols(protocols) {
      return protocols.has(PROTOCOL) ? PROTOCOL : false
    }
  })
  const onboardingWebSocketServer = new WebSocketServer({
    noServer: true, maxPayload: config.maxFrameBytes, clientTracking: false,
    handleProtocols(protocols) { return protocols.has(ONBOARDING_PROTOCOL) ? ONBOARDING_PROTOCOL : false }
  })

  server.on('upgrade', (request, socket, head) => {
    let pathname: string
    try {
      pathname = new URL(request.url ?? '/', 'http://gateway.invalid').pathname
    } catch {
      socket.destroy()
      return
    }
    const origin = request.headers.origin
    const offeredProtocols = request.headers['sec-websocket-protocol']
    const protocols = typeof offeredProtocols === 'string' ? offeredProtocols.split(',').map((protocol) => protocol.trim()) : []
    const legacy = pathname === '/ws' && protocols.includes(PROTOCOL)
    const onboarding = config.mudOnboardingEnabled && pathname === '/onboarding' && protocols.includes(ONBOARDING_PROTOCOL)
    if (!accepting || (!legacy && !onboarding) || typeof origin !== 'string' || !config.allowedOrigins.has(origin) || (config.requireSecureTransport && !isSecureRequest(request))) {
      socket.write('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n')
      socket.destroy()
      return
    }
    if (activeConnections >= config.maxConnections) {
      socket.write('HTTP/1.1 503 Service Unavailable\r\nConnection: close\r\n\r\n')
      socket.destroy()
      return
    }
    const target = onboarding ? onboardingWebSocketServer : webSocketServer
    target.handleUpgrade(request, socket, head, (ws) => target.emit('connection', ws, request))
  })

  webSocketServer.on('connection', (ws, request) => {
    activeConnections += 1
    const session = new GatewaySession(
      ws, request, config, authenticator, characterAuthorizer, connectTcp, logger,
      now, dependencies.randomBytes, dependencies.randomUuid ?? randomUUID, dependencies.timers ?? systemTimers, () => {
      sessions.delete(session)
      activeConnections -= 1
      }
    )
    sessions.add(session)
    allSessions.add(session)
  })
  onboardingWebSocketServer.on('connection', (ws, request) => {
    activeConnections += 1
    const session = new OnboardingSession(ws, request, config, authenticator, onboardingAuthorizer, evidenceFinalizer, connectTcp, logger,
      now, dependencies.randomBytes, dependencies.randomUuid ?? randomUUID, dependencies.timers ?? systemTimers, () => { sessions.delete(session); activeConnections -= 1 })
    sessions.add(session)
    allSessions.add(session)
  })

  return {
    server,
    address: () => {
      const address = server.address()
      if (!address || typeof address === 'string') throw new Error('gateway is not listening')
      return `http://${address.address}:${address.port}`
    },
    close: async () => {
      if (closePromise) return closePromise
      closePromise = (async () => {
        accepting = false
        for (const session of sessions) session.drain()
        let serverClosedResolved = false
        const serverClosed = new Promise<void>((resolve) => server.close(() => { serverClosedResolved = true; resolve() }))
        server.closeIdleConnections?.()
        const deadline = Date.now() + config.shutdownGraceMs
        let graceTimer: NodeJS.Timeout | undefined
        const graceElapsed = new Promise<void>((resolve) => {
          graceTimer = setTimeout(resolve, config.shutdownGraceMs)
          unrefTimer(graceTimer)
        })
        await Promise.race([serverClosed, graceElapsed])
        if (graceTimer) clearTimeout(graceTimer)
        for (const session of sessions) session.forceClose()
        // Upgraded sockets are outside Node's HTTP close accounting. Destroy
        // the exact handles we accepted, including a client that ignored the
        // close frame, and close any remaining HTTP keep-alive connections.
        for (const socket of sockets) socket.destroy()
        server.closeAllConnections?.()
        if (!serverClosedResolved) {
          const remainingMs = Math.max(0, deadline - Date.now())
          if (remainingMs > 0) {
            let timer: NodeJS.Timeout | undefined
            try {
              await Promise.race([serverClosed, new Promise<void>((resolve) => {
                timer = setTimeout(resolve, remainingMs)
                unrefTimer(timer)
              })])
            } finally {
              if (timer) clearTimeout(timer)
            }
          }
        }
        const pending = Promise.all([...allSessions].map((session) => session.waitForPending()))
        const remainingMs = Math.max(0, deadline - Date.now())
        if (remainingMs > 0) {
          let timer: NodeJS.Timeout | undefined
          try {
            await Promise.race([pending, new Promise<void>((resolve) => {
              timer = setTimeout(resolve, remainingMs)
              unrefTimer(timer)
            })])
          } finally {
            if (timer) clearTimeout(timer)
          }
        }
      })()
      return closePromise
    }
  }
}

class GatewaySession {
  private state: SessionState = 'awaiting-auth'
  private mud?: Socket
  private authTimer: NodeJS.Timeout
  private expiryTimer?: NodeJS.Timeout
  private admissionTimer?: NodeJS.Timeout
  private renewalTimer?: NodeJS.Timeout
  private readonly parser = new TelnetParser()
  private readonly inputLimiter: ByteRateLimiter
  private admissionBuffer = Buffer.alloc(0)
  private sessionId?: string
  private tokenExpiresAtMs = 0
  private actorUserId?: string
  private characterId?: string
  private legacyNameKey?: string
  private leaseMayExist = false
  private closed = false
  private failed = false
  private normalClosing = false
  private readonly pendingOperations = new Set<Promise<unknown>>()

  constructor(
    private readonly ws: WebSocket,
    private readonly request: IncomingMessage,
    private readonly config: GatewayConfig,
    private readonly authenticator: Authenticator,
    private readonly characterAuthorizer: CharacterAuthorizer,
    private readonly connectTcp: (host: string, port: number) => Socket,
    private readonly logger: Pick<Console, 'info' | 'warn' | 'error'>,
    private readonly now: () => number,
    private readonly randomBytes: ((size: number) => Buffer) | undefined,
    private readonly randomUuid: () => string,
    private readonly timers: GatewayTimers,
    private readonly onClosed: () => void
  ) {
    this.inputLimiter = new ByteRateLimiter(config.inputBytesPerSecond, now)
    this.authTimer = this.timers.setTimeout(() => this.fail(CLOSE_POLICY, 'authentication timed out'), config.authTimeoutMs)
    unrefTimer(this.authTimer)
    ws.on('message', (data, isBinary) => { void this.track(this.onMessage(rawToBuffer(data), isBinary)).catch(() => undefined) })
    ws.on('error', () => this.logger.warn('websocket transport error'))
    ws.on('close', () => this.finish())
  }

  drain(): void {
    this.sendText({ type: 'closed', reason: 'gateway shutting down' })
    closeSocket(this.ws, CLOSE_RESTART, 'gateway shutting down')
    this.mud?.end()
  }

  forceClose(): void {
    this.mud?.destroy()
    this.ws.terminate()
    this.finish()
  }

  async waitForPending(): Promise<void> {
    while (this.pendingOperations.size > 0) await Promise.allSettled([...this.pendingOperations])
  }

  private track<T>(operation: Promise<T>): Promise<T> {
    let tracked!: Promise<T>
    tracked = operation.then((value) => { this.pendingOperations.delete(tracked); return value }, (error) => { this.pendingOperations.delete(tracked); throw error })
    this.pendingOperations.add(tracked)
    return tracked
  }

  private async onMessage(data: Buffer, isBinary: boolean): Promise<void> {
    if (this.closed) return
    if (data.length > this.config.maxFrameBytes) return this.fail(CLOSE_POLICY, 'frame too large')
    if (this.state === 'awaiting-auth') {
      if (isBinary) return this.fail(CLOSE_POLICY, 'first frame must be text auth')
      let authFrame: AuthFrame
      try {
        authFrame = parseAuthMessage(data)
      } catch (error) {
        return this.fail(CLOSE_POLICY, error instanceof Error ? error.message : 'invalid auth frame')
      }
      this.state = 'connecting'
      try {
        const identity = await this.authenticator.verify(authFrame.accessToken)
        if (this.closed) return
        if (!isStrictLowerUuid(identity.sub)) throw new AuthenticationError('token subject must be a lowercase UUID')
        const currentMs = this.now()
        const leaseExpiryMs = Math.min(identity.expiresAtMs, currentMs + LEASE_TTL_MS)
        if (leaseExpiryMs <= currentMs) throw new AuthenticationError('token has expired')
        const sessionId = this.randomUuid()
        if (!isStrictLowerUuid(sessionId)) throw new CharacterAuthorizationError()
        this.sessionId = sessionId
        this.tokenExpiresAtMs = identity.expiresAtMs
        this.actorUserId = identity.sub
        this.characterId = authFrame.characterId
        this.leaseMayExist = true
        const character = await this.characterAuthorizer.beginSession({
          actorUserId: identity.sub,
          characterId: authFrame.characterId,
          sessionId,
          gatewayInstanceId: this.config.gatewayInstanceId!,
          expiresAt: new Date(leaseExpiryMs)
        })
        if (this.closed) {
          this.releaseLease()
          return
        }
        this.legacyNameKey = character.legacyNameKey
        const ticket = createAdmissionTicket({
          actorUserId: identity.sub,
          characterId: authFrame.characterId,
          legacyNameKey: this.legacyNameKey,
          jwtExpiresAtMs: identity.expiresAtMs,
          nowMs: this.now()
        }, this.config.mudAdmissionSecret!, { randomBytes: this.randomBytes })
        this.timers.clearTimeout(this.authTimer)
        this.scheduleExpiry(identity.expiresAtMs)
        this.connectMud(ticket)
      } catch {
        return this.fail(CLOSE_POLICY, 'authentication or character authorization failed')
      }
      return
    }
    if (this.state !== 'ready') return this.fail(CLOSE_POLICY, 'session is not ready')
    if (!isBinary) {
      if (isPingMessage(data)) this.sendText({ type: 'pong' })
      else this.fail(CLOSE_POLICY, 'only binary input and ping controls are allowed')
      return
    }
    if (!this.inputLimiter.take(data.length)) return this.fail(CLOSE_TRY_AGAIN, 'input rate exceeded')
    if (!this.mud || this.mud.destroyed) return this.fail(CLOSE_INTERNAL, 'MUD connection unavailable')
    if (!this.mud.write(data)) this.pauseWebSocketUntilTcpDrain()
  }

  private connectMud(ticket: Buffer): void {
    const mud = this.connectTcp(this.config.mudHost, this.config.mudPort)
    this.mud = mud
    const timer = this.timers.setTimeout(() => {
      if (this.state === 'connecting') {
        mud.destroy()
        this.fail(CLOSE_INTERNAL, 'MUD connection timed out')
      }
    }, this.config.tcpConnectTimeoutMs)
    unrefTimer(timer)
    mud.once('connect', () => {
      this.timers.clearTimeout(timer)
      if (this.closed) return mud.destroy()
      this.state = 'awaiting-admission'
      this.admissionTimer = this.timers.setTimeout(() => {
        if (this.state === 'awaiting-admission') this.fail(CLOSE_INTERNAL, 'MUD admission timed out')
      }, this.config.mudAdmissionTimeoutMs)
      unrefTimer(this.admissionTimer)
      mud.write(ticket, (error) => {
        if (error && !this.closed) this.fail(CLOSE_INTERNAL, 'MUD admission write failed')
      })
    })
    mud.on('data', (data: Buffer) => this.onMudData(data))
    mud.on('drain', () => this.resumeWebSocket())
    mud.on('error', () => {
      this.timers.clearTimeout(timer)
      if (!this.closed && !this.normalClosing) this.fail(CLOSE_INTERNAL, 'MUD connection failed')
    })
    mud.on('end', () => {
      if (this.closed || this.normalClosing) return
      if (this.state === 'ready') {
        this.normalClosing = true
        this.sendText({ type: 'closed', reason: 'MUD connection closed' })
        closeSocket(this.ws, CLOSE_NORMAL, 'MUD connection closed')
        return
      }
      this.fail(CLOSE_INTERNAL, 'MUD admission failed')
    })
  }

  private onMudData(data: Buffer): void {
    if (this.state === 'awaiting-admission') {
      this.consumeAdmissionPreface(data)
      return
    }
    if (this.state !== 'ready') return
    this.relayMudData(data)
  }

  private consumeAdmissionPreface(data: Buffer): void {
    const newline = data.indexOf(0x0a)
    if (newline < 0) {
      if (this.admissionBuffer.length + data.length > admissionTicketLimits.maxTicketLineBytes) {
        this.fail(CLOSE_INTERNAL, 'invalid MUD admission preface')
        return
      }
      this.admissionBuffer = Buffer.concat([this.admissionBuffer, data])
      return
    }

    const prefaceLength = this.admissionBuffer.length + newline + 1
    if (prefaceLength > admissionTicketLimits.maxTicketLineBytes) {
      this.fail(CLOSE_INTERNAL, 'invalid MUD admission preface')
      return
    }
    const preface = Buffer.concat([this.admissionBuffer, data.subarray(0, newline + 1)])
    this.admissionBuffer = Buffer.alloc(0)
    if (!preface.equals(Buffer.from('MUD1 OK\n', 'ascii'))) {
      this.fail(CLOSE_INTERNAL, 'MUD admission failed')
      return
    }
    if (this.admissionTimer) this.timers.clearTimeout(this.admissionTimer)
    this.state = 'ready'
    this.sendText({ type: 'ready' })
    this.scheduleLeaseRenewal()
    const gameBytes = data.subarray(newline + 1)
    if (gameBytes.length > 0) this.relayMudData(gameBytes)
  }

  private relayMudData(data: Buffer): void {
    for (const event of this.parser.feed(data)) {
      if (event.type === 'data') this.sendBinary(event.data)
      else if (event.type === 'echo') this.sendText({ type: 'echo', enabled: event.enabled })
      else if (this.mud && !this.mud.destroyed) this.mud.write(event.data)
    }
  }

  private sendBinary(data: Buffer): void {
    sendBufferedWebSocketFrame(this.ws, data, true, this.config.maxBufferedBytes, (code, reason) => this.fail(code, reason))
  }

  private sendText(value: Record<string, unknown>): void {
    sendBufferedWebSocketFrame(this.ws, jsonFrame(value), false, this.config.maxBufferedBytes, (code, reason) => this.fail(code, reason))
  }

  private scheduleExpiry(expiresAtMs: number): void {
    const closeWhenDue = () => {
      const remaining = expiresAtMs - this.now()
      if (remaining <= 0) return this.fail(CLOSE_TOKEN_EXPIRED, 'token expired')
      this.expiryTimer = this.timers.setTimeout(closeWhenDue, Math.min(remaining, 2_147_483_647))
      unrefTimer(this.expiryTimer)
    }
    closeWhenDue()
  }

  private pauseWebSocketUntilTcpDrain(): void {
    const stream = (this.ws as unknown as { _socket?: Socket })._socket
    stream?.pause()
  }

  private resumeWebSocket(): void {
    const stream = (this.ws as unknown as { _socket?: Socket })._socket
    stream?.resume()
  }

  private fail(code: number, reason: string): void {
    if (this.closed || this.failed) return
    this.failed = true
    this.sendText({ type: 'error', reason })
    this.sendText({ type: 'closed', reason })
    closeSocket(this.ws, code, reason)
    this.mud?.destroy()
  }

  private finish(): void {
    if (this.closed) return
    this.closed = true
    this.state = 'closed'
    this.timers.clearTimeout(this.authTimer)
    if (this.expiryTimer) this.timers.clearTimeout(this.expiryTimer)
    if (this.admissionTimer) this.timers.clearTimeout(this.admissionTimer)
    if (this.renewalTimer) this.timers.clearTimeout(this.renewalTimer)
    this.mud?.destroy()
    this.releaseLease()
    this.onClosed()
  }

  private releaseLease(attempt = 0): void {
    if (!this.leaseMayExist || !this.sessionId) return
    void this.track(this.releaseLeaseAttempt(attempt)).catch(() => undefined)
  }

  private async releaseLeaseAttempt(attempt: number): Promise<void> {
    try {
      await this.characterAuthorizer.endSession(this.sessionId!, this.config.gatewayInstanceId!)
    } catch {
      const delay = LEASE_RELEASE_RETRY_DELAYS_MS[attempt]
      if (delay === undefined) {
        this.logger.warn('character session lease release failed after bounded retries')
        return
      }
      await new Promise<void>((resolve) => {
        const timer = this.timers.setTimeout(resolve, delay)
        unrefTimer(timer)
      })
      return this.releaseLeaseAttempt(attempt + 1)
    }
  }

  private scheduleLeaseRenewal(): void {
    const renew = async () => {
      if (this.closed || this.state !== 'ready' || !this.sessionId || !this.actorUserId || !this.characterId || !this.legacyNameKey) return
      const expiresAtMs = Math.min(this.tokenExpiresAtMs, this.now() + LEASE_TTL_MS)
      if (expiresAtMs - this.now() < LEASE_MIN_REMAINING_MS) {
        this.fail(CLOSE_TOKEN_EXPIRED, 'token expires too soon to renew session')
        return
      }
      try {
        const renewed = await this.characterAuthorizer.renewSession({
          sessionId: this.sessionId,
          gatewayInstanceId: this.config.gatewayInstanceId!,
          expiresAt: new Date(expiresAtMs)
        })
        if (this.closed) {
          this.releaseLease()
          return
        }
        if (renewed.sessionId !== this.sessionId || renewed.actorUserId !== this.actorUserId ||
          renewed.characterId !== this.characterId || renewed.legacyNameKey !== this.legacyNameKey ||
          renewed.lifecycle !== 'active' || renewed.expiresAtMs < expiresAtMs - 1_000 ||
          renewed.expiresAtMs > expiresAtMs + 1_000 || renewed.expiresAtMs <= this.now()) {
          throw new CharacterAuthorizationError()
        }
      } catch {
        this.fail(CLOSE_INTERNAL, 'character session renewal failed')
        return
      }
      this.renewalTimer = this.timers.setTimeout(() => { void this.track(renew()).catch(() => undefined) }, LEASE_RENEW_INTERVAL_MS)
      unrefTimer(this.renewalTimer)
    }
    this.renewalTimer = this.timers.setTimeout(() => { void this.track(renew()).catch(() => undefined) }, LEASE_RENEW_INTERVAL_MS)
    unrefTimer(this.renewalTimer)
  }
}

type OnboardingSessionState = 'awaiting-auth' | 'connecting' | 'awaiting-admission' | 'awaiting-control' | 'ready' | 'closed'
type TrustedOnboardingCompletion = {
  actorUserId: string
  correlationId: string
  characterId: string
  legacyNameKey: string
}

/** Isolated MUD1O state machine; it never shares the MUD1 session path. */
class OnboardingSession {
  private state: OnboardingSessionState = 'awaiting-auth'
  private mud?: Socket
  private readonly controls: OnboardingControlDemultiplexer
  private readonly telnet = new TelnetParser()
  private readonly inputLimiter: ByteRateLimiter
  private readonly pendingMessages: Array<{ data: Buffer, binary: boolean }> = []
  private pendingMessageBytes = 0
  private drainingMessages = false
  private authTimer: NodeJS.Timeout
  private connectTimer?: NodeJS.Timeout
  private admissionTimer?: NodeJS.Timeout
  private expiryTimer?: NodeJS.Timeout
  private actorUserId?: string
  private correlationId?: string
  private mode?: 'provision' | 'claim'
  private characterId?: string
  // Written only after the authorizer accepts RESERVE. The C EVIDENCE record
  // has no actor/correlation/character fields, so it can never supply these.
  private provisionReservation?: TrustedOnboardingCompletion
  private unreservedIntentMayExist = false
  private unreservedCancellationStarted = false
  private controlPhase: 'admission' | 'provision-reserve' | 'provision-saved' | 'provision-evidence' | 'claim-challenge' | 'claim-allow' | 'activation' | 'completing' | 'done' = 'admission'
  private activationCommandId?: string
  private claimChallenge?: { characterId: string; legacyNameKey: string; fileSha256: string; allowExpiresAtMs: number }
  private controlQueue: Promise<void> = Promise.resolve()
  private paused = false
  // Once C has provided the success evidence that starts finalization, this
  // one-shot connection carries controls only. It must never become a game
  // relay while the browser is being handed back to normal /ws admission.
  private completionControlOnly = false
  private closed = false
  private failed = false
  private normalClosing = false
  private readonly pendingOperations = new Set<Promise<unknown>>()

  constructor(
    private readonly ws: WebSocket, private readonly request: IncomingMessage, private readonly config: GatewayConfig,
    private readonly authenticator: Authenticator, private readonly authorizer: OnboardingAuthorizer,
    private readonly evidenceFinalizer: EvidenceFinalizer | undefined,
    private readonly connectTcp: (host: string, port: number) => Socket,
    private readonly logger: Pick<Console, 'info' | 'warn' | 'error'>, private readonly now: () => number,
    private readonly randomBytes: ((size: number) => Buffer) | undefined, private readonly randomUuid: () => string,
    private readonly timers: GatewayTimers,
    private readonly onClosed: () => void,
  ) {
    this.controls = new OnboardingControlDemultiplexer({ evidenceEnabled: config.mudOnboardingEvidenceEnabled })
    this.inputLimiter = new ByteRateLimiter(config.inputBytesPerSecond, now)
    this.authTimer = timers.setTimeout(() => this.fail(CLOSE_POLICY, 'authentication timed out'), config.authTimeoutMs); unrefTimer(this.authTimer)
    ws.on('message', (data, binary) => this.enqueueMessage(rawToBuffer(data), binary))
    ws.on('error', () => this.logger.warn('websocket transport error'))
    ws.on('close', () => this.finish())
  }
  drain(): void { this.sendText({ type: 'closed', reason: 'gateway shutting down' }); closeSocket(this.ws, CLOSE_RESTART, 'gateway shutting down'); this.mud?.end() }
  forceClose(): void { this.mud?.destroy(); this.ws.terminate(); this.finish() }
  async waitForPending(): Promise<void> {
    while (this.pendingOperations.size > 0) await Promise.allSettled([...this.pendingOperations])
  }
  private track<T>(operation: Promise<T>): Promise<T> {
    let tracked!: Promise<T>
    tracked = operation.then((value) => { this.pendingOperations.delete(tracked); return value }, (error) => { this.pendingOperations.delete(tracked); throw error })
    this.pendingOperations.add(tracked)
    return tracked
  }
  private enqueueMessage(data: Buffer, binary: boolean): void {
    if (this.closed) return
    if (data.length > this.config.maxFrameBytes) return this.fail(CLOSE_POLICY, 'frame too large')
    if (this.pendingMessageBytes + data.length > this.config.maxBufferedBytes) return this.fail(CLOSE_TRY_AGAIN, 'input buffer exceeded')
    this.pendingMessages.push({ data: Buffer.from(data), binary })
    this.pendingMessageBytes += data.length
    void this.track(this.drainMessages()).catch(() => undefined)
  }
  private async drainMessages(): Promise<void> {
    if (this.drainingMessages || this.paused || this.closed) return
    this.drainingMessages = true
    try {
      while (!this.closed && !this.paused && this.pendingMessages.length > 0) {
        const message = this.pendingMessages.shift()!
        this.pendingMessageBytes -= message.data.length
        await this.handleMessage(message.data, message.binary)
      }
    } finally {
      this.drainingMessages = false
      if (!this.closed && !this.paused && this.pendingMessages.length > 0) void this.track(this.drainMessages()).catch(() => undefined)
    }
  }
  private async handleMessage(data: Buffer, binary: boolean): Promise<void> {
    if (this.closed) return
    if (this.state === 'awaiting-auth') {
      if (binary) return this.fail(CLOSE_POLICY, 'first frame must be text auth')
      try {
        const frame = parseOnboardingAuthFrame(data.toString('utf8'))
        this.state = 'connecting'
        const identity = await this.authenticator.verify(frame.accessToken)
        if (this.closed || !isStrictLowerUuid(identity.sub) || identity.expiresAtMs <= this.now()) throw new AuthenticationError('invalid onboarding identity')
        const expiresAt = new Date(Math.min(identity.expiresAtMs, this.now() + 15 * 60_000))
        this.actorUserId = identity.sub; this.correlationId = frame.correlationId; this.mode = frame.mode
        await this.authorizer.begin({ actorUserId: identity.sub, correlationId: frame.correlationId, mode: frame.mode, expiresAt })
        this.unreservedIntentMayExist = true
        if (this.closed || this.ws.readyState !== WebSocket.OPEN) { this.cancelUnreservedIntent(); return }
        if (identity.expiresAtMs <= this.now()) throw new AuthenticationError('invalid onboarding identity')
        const ticket = createOnboardingTicket({ mode: frame.mode, userId: identity.sub, correlationId: frame.correlationId }, this.config.mudAdmissionSecret!, { nowMs: this.now(), randomBytes: this.randomBytes })
        this.timers.clearTimeout(this.authTimer); this.scheduleExpiry(identity.expiresAtMs); this.connectMud(ticket)
      } catch { this.fail(CLOSE_POLICY, 'onboarding authentication failed') }
      return
    }
    if (this.state !== 'awaiting-control' && this.state !== 'ready') return this.fail(CLOSE_POLICY, 'session is not ready')
    if (!binary) { if (isPingMessage(data)) this.sendText({ type: 'pong' }); else this.fail(CLOSE_POLICY, 'only binary input and ping controls are allowed'); return }
    if (!this.inputLimiter.take(data.length)) return this.fail(CLOSE_TRY_AGAIN, 'input rate exceeded')
    if (!this.mud || this.mud.destroyed) return this.fail(CLOSE_INTERNAL, 'MUD connection unavailable')
    if (!this.mud.write(data)) this.pauseInput()
  }
  private connectMud(ticket: Buffer): void {
    const mud = this.connectTcp(this.config.mudHost, this.config.mudPort); this.mud = mud
    this.connectTimer = this.timers.setTimeout(() => { if (this.state === 'connecting') { mud.destroy(); this.fail(CLOSE_INTERNAL, 'MUD connection timed out') } }, this.config.tcpConnectTimeoutMs); unrefTimer(this.connectTimer)
    mud.once('connect', () => { this.timers.clearTimeout(this.connectTimer!); if (this.closed) return mud.destroy(); this.state = 'awaiting-admission'; this.admissionTimer = this.timers.setTimeout(() => this.fail(CLOSE_INTERNAL, 'MUD admission timed out'), this.config.mudAdmissionTimeoutMs); unrefTimer(this.admissionTimer); mud.write(ticket, (error) => { if (error) this.fail(CLOSE_INTERNAL, 'MUD admission write failed') }) })
    mud.on('data', (data: Buffer) => this.onMudData(data))
    mud.on('drain', () => this.resumeInput())
    mud.on('error', () => { if (!this.closed && !this.normalClosing) this.fail(CLOSE_INTERNAL, 'MUD connection failed') })
    mud.on('end', () => {
      if (this.closed || this.normalClosing) return
      if (this.state === 'ready') {
        this.normalClosing = true
        this.sendText({ type: 'closed', reason: 'MUD connection closed' })
        closeSocket(this.ws, CLOSE_NORMAL, 'MUD connection closed')
        return
      }
      this.fail(CLOSE_INTERNAL, 'MUD connection closed')
    })
  }
  private onMudData(data: Buffer): void {
    if (this.closed || this.state === 'closed') return
    if (this.state === 'ready') {
      this.relayMudData(data)
      return
    }
    let result: ReturnType<OnboardingControlDemultiplexer['push']>
    try { result = this.controls.push(data) } catch { this.fail(CLOSE_INTERNAL, 'invalid MUD onboarding control'); return }
    // A reserved control prefix may be split across TCP packets. The
    // demultiplexer retains that prefix and deliberately returns no bytes.
    if (result.ordered.length === 0) return
    // Preserve C byte ordering here: game bytes preceding a completion control
    // keep their ordinary behavior, while bytes following it are discarded.
    // Queue game-only events too, so a later TCP event cannot overtake a SAVED
    // or VERIFIED control whose finalizer is still pending.
    this.controlQueue = this.track(this.controlQueue.then(async () => {
      for (const item of result.ordered) {
        if (item.type === 'control') await this.onControl(item.control)
        else this.handleOnboardingGameBytes(item.data)
      }
    }).catch(() => this.fail(CLOSE_INTERNAL, 'invalid MUD onboarding control')))
  }
  private handleOnboardingGameBytes(game: Buffer): void {
    if (this.completionControlOnly) return
    if (!this.canRelayGameBytes()) { this.fail(CLOSE_INTERNAL, 'unexpected MUD game bytes'); return }
    this.relayMudData(game)
  }
  private canRelayGameBytes(): boolean {
    return !this.paused && (this.state === 'awaiting-control' || this.state === 'ready')
  }
  private relayMudData(data: Buffer): void {
    for (const event of this.telnet.feed(data)) {
      if (event.type === 'data') this.sendBinary(event.data)
      else if (event.type === 'echo') this.sendText({ type: 'echo', enabled: event.enabled })
      else if (this.mud && !this.mud.destroyed) this.mud.write(event.data)
    }
  }
  private async onControl(event: OnboardingControl): Promise<void> {
    if (this.closed) return
    if (this.state === 'awaiting-admission') {
      if (this.controlPhase !== 'admission' || event.type !== 'OK') return this.fail(CLOSE_INTERNAL, 'MUD admission failed')
      this.timers.clearTimeout(this.admissionTimer!); this.state = 'awaiting-control'; this.controlPhase = this.mode === 'provision' ? 'provision-reserve' : 'claim-challenge'; this.sendText({ type: 'onboarding-ready', mode: this.mode! }); return
    }
    if (this.state !== 'awaiting-control') return this.fail(CLOSE_INTERNAL, 'invalid MUD onboarding control')
    try {
      if (this.controlPhase === 'activation') {
        if (event.type !== 'ACTIVE' || event.commandId !== this.activationCommandId) throw new OnboardingProtocolError()
        const request: BindSnapshotCommandRequest = {
          actorUserId: this.actorUserId!, correlationId: this.correlationId!, characterId: this.characterId!,
          mode: this.mode!, commandId: this.activationCommandId!,
        }
        // The binding RPC is immutable and exact-correlation idempotent. A
        // lost response must not strand an already-active handoff without its
        // browser completion acknowledgement.
        await this.retryIndeterminate(() => this.authorizer.bindSnapshotCommand(request))
        if (this.closed) return
        this.controlPhase = 'done'; this.state = 'closed'; this.normalClosing = true
        this.sendText({ type: this.mode === 'provision' ? 'provisioned' : 'claimed', characterId: this.characterId! })
        closeSocket(this.ws, CLOSE_NORMAL, 'onboarding complete')
        this.mud?.end()
        return
      }
      if (this.mode === 'provision' && this.controlPhase === 'provision-reserve' && event.type === 'RESERVE') {
        this.controlPhase = this.config.mudOnboardingEvidenceEnabled ? 'provision-evidence' : 'provision-saved'
        this.pauseInput(); const legacyName = Buffer.from(event.nameHex, 'hex').toString('utf8')
        if (Buffer.from(legacyName, 'utf8').toString('hex') !== event.nameHex) throw new OnboardingProtocolError()
        // A reserve RPC may commit even if its transport response is lost. From
        // this point on, only provisioning reconciliation may change DB state.
        this.unreservedIntentMayExist = false
        const result = await this.authorizer.reserve({ actorUserId: this.actorUserId!, correlationId: this.correlationId!, worldId: 'muhan', legacyName })
        if (!isStrictLowerUuid(result.characterId) || result.legacyNameKey !== legacyName) throw new OnboardingProtocolError()
        this.characterId = result.characterId
        this.provisionReservation = {
          actorUserId: this.actorUserId!, correlationId: this.correlationId!,
          characterId: result.characterId, legacyNameKey: result.legacyNameKey
        }
        await this.writeControl(`MUD1O RESERVED|${result.characterId}\n`); this.resumeInput(); return
      }
      if (this.config.mudOnboardingEvidenceEnabled && event.type === 'SAVED') throw new OnboardingProtocolError()
      if (this.mode === 'provision' && this.controlPhase === 'provision-saved' && event.type === 'SAVED') {
        if (event.characterId !== this.characterId) throw new OnboardingProtocolError()
        this.completionControlOnly = true
        this.pauseInput()
        const finalizeRequest = { actorUserId: this.actorUserId!, correlationId: this.correlationId!, characterId: event.characterId, fileSha256: event.fileSha256, storageFormat: event.storageFormat }
        let result: { characterId: string }
        try {
          result = await this.authorizer.finalize(finalizeRequest)
        } catch {
          // A failed finalize can be an indeterminate transport outcome; reconcile once using the exact same intent.
          result = await this.authorizer.reconcile(finalizeRequest)
        }
        if (this.closed) return
        if (result.characterId !== event.characterId) throw new OnboardingProtocolError()
        // Do not tell the browser that the character is live until the exact
        // COMMIT control has been accepted by the C socket write path.
        await this.writeControl('MUD1O COMMIT\n')
        if (this.closed) return
        await this.startActivation(result.characterId)
        return
      }
      if (this.mode === 'provision' && this.controlPhase === 'provision-evidence' && event.type === 'EVIDENCE') {
        const reservation = this.provisionReservation
        if (!reservation || reservation.characterId !== this.characterId) throw new OnboardingProtocolError()
        await this.completeFromEvidence(event, 'provision', reservation, 'MUD1O COMMIT\n', 'provisioned')
        return
      }
      if (this.mode === 'claim' && this.controlPhase === 'claim-challenge' && event.type === 'CHALLENGE') {
        this.pauseInput()
        const legacyNameKey = Buffer.from(event.nameHex, 'hex').toString('utf8')
        if (Buffer.from(legacyNameKey, 'utf8').toString('hex') !== event.nameHex) throw new OnboardingProtocolError()
        // The challenge is the only point where C-provided SHA evidence crosses
        // the service boundary. Do not cancel the intent after this ledger write.
        this.unreservedIntentMayExist = false
        const request: ChallengeOnboardingRequest = { actorUserId: this.actorUserId!, correlationId: this.correlationId!, worldId: 'muhan', legacyNameKey, fileSha256: event.fileSha256 }
        const result = await this.authorizer.challenge(request)
        if (this.closed) return
        const now = this.now()
        if (result.legacyNameKey !== legacyNameKey || result.fileSha256 !== event.fileSha256 || !isStrictLowerUuid(result.characterId) || !Number.isFinite(result.allowExpiresAtMs) || result.allowExpiresAtMs <= now || result.allowExpiresAtMs > now + CLAIM_ALLOW_TTL_MS) throw new OnboardingProtocolError()
        this.claimChallenge = result
        await this.writeControl('MUD1O ALLOW\n')
        this.controlPhase = 'claim-allow'
        this.resumeInput()
        return
      }
      if (!this.config.mudOnboardingEvidenceEnabled && this.mode === 'claim' && this.controlPhase === 'claim-allow' && event.type === 'VERIFIED') {
        const challenge = this.claimChallenge
        if (!challenge || challenge.allowExpiresAtMs <= this.now()) throw new OnboardingProtocolError()
        const legacyNameKey = Buffer.from(event.nameHex, 'hex').toString('utf8')
        if (Buffer.from(legacyNameKey, 'utf8').toString('hex') !== event.nameHex || legacyNameKey !== challenge.legacyNameKey || event.fileSha256 !== challenge.fileSha256) throw new OnboardingProtocolError()
        this.controlPhase = 'done'
        this.completionControlOnly = true
        this.pauseInput()
        // A claim RPC is an irreversible ownership boundary when its transport
        // outcome is indeterminate. Retry the exact request once so a committed
        // transaction can be recovered through the migration's idempotency key.
        this.unreservedIntentMayExist = false
        const claimRequest = { actorUserId: this.actorUserId!, correlationId: this.correlationId!, worldId: 'muhan', legacyNameKey, fileSha256: event.fileSha256 }
        let result: { characterId: string }
        try {
          result = await this.authorizer.claim(claimRequest)
        } catch (error) {
          if (!(error instanceof OnboardingAuthorizationError && error.indeterminate)) throw error
          if (this.closed) return
          result = await this.authorizer.claim(claimRequest)
        }
        if (this.closed) return
        if (result.characterId !== challenge.characterId) throw new OnboardingProtocolError()
        await this.writeControl(`MUD1O CLAIMED|${result.characterId}\n`)
        if (this.closed) return
        await this.startActivation(result.characterId)
        return
      }
      if (this.config.mudOnboardingEvidenceEnabled && event.type === 'VERIFIED') throw new OnboardingProtocolError()
      if (this.mode === 'claim' && this.controlPhase === 'claim-allow' && event.type === 'EVIDENCE') {
        const challenge = this.claimChallenge
        if (!challenge || challenge.allowExpiresAtMs <= this.now()) throw new OnboardingProtocolError()
        await this.completeFromEvidence(event, 'claim', {
          actorUserId: this.actorUserId!, correlationId: this.correlationId!,
          characterId: challenge.characterId, legacyNameKey: challenge.legacyNameKey
        }, `MUD1O CLAIMED|${challenge.characterId}\n`, 'claimed', challenge.fileSha256)
        return
      }
      if (event.type === 'ERR') return this.fail(CLOSE_POLICY, 'onboarding failed')
      throw new OnboardingProtocolError()
    } catch { this.fail(CLOSE_POLICY, 'onboarding failed') }
  }
  private async completeFromEvidence(
    event: Extract<OnboardingControl, { type: 'EVIDENCE' }>,
    mode: 'provision' | 'claim',
    trusted: TrustedOnboardingCompletion,
    completionControl: string,
    browserCompletion: 'provisioned' | 'claimed',
    expectedSha256?: string,
  ): Promise<void> {
    // A coalesced/later C game event cannot escape while completing the
    // one-shot handoff. The only evidence sent to the RPC is decoded metadata.
    this.completionControlOnly = true
    this.controlPhase = 'completing'
    this.pauseInput()
    if (!this.evidenceFinalizer || !isStrictLowerUuid(trusted.actorUserId) || !isStrictLowerUuid(trusted.correlationId) ||
        !isStrictLowerUuid(trusted.characterId) || trusted.actorUserId !== this.actorUserId ||
        trusted.correlationId !== this.correlationId || !isFinalizableEvidence(event, trusted, expectedSha256)) {
      throw new OnboardingProtocolError()
    }
    // C must first accept the exact completion control. Its write callback is
    // the durable C-side boundary before the DB may bind the evidence receipt.
    await this.writeControl(completionControl)
    if (this.closed) return
    await this.evidenceFinalizer.finalize({
      actorUserId: trusted.actorUserId, correlationId: trusted.correlationId, characterId: trusted.characterId,
      mode, worldId: 'muhan', evidence: event.evidence
    })
    if (this.closed) return
    await this.startActivation(trusted.characterId)
  }
  private async startActivation(characterId: string): Promise<void> {
    const request = { actorUserId: this.actorUserId!, correlationId: this.correlationId!, characterId, mode: this.mode! }
    // Activation is also an exact-correlation idempotent RPC. If its response
    // is lost after the transaction commits, retrying the same tuple recovers
    // the active row before issuing the C activation command.
    const activated = await this.retryIndeterminate(() => this.authorizer.activateHandoff(request))
    if (this.closed || activated.characterId !== characterId) throw new OnboardingProtocolError()
    const commandId = this.randomUuid()
    if (!isStrictLowerUuid(commandId)) throw new OnboardingProtocolError()
    this.activationCommandId = commandId
    this.characterId = characterId
    this.controlPhase = 'activation'
    await this.writeControl(`MUD1O ACTIVATED|${commandId}\n`)
  }
  private async retryIndeterminate<T>(operation: () => Promise<T>): Promise<T> {
    try {
      return await operation()
    } catch (error) {
      if (!(error instanceof OnboardingAuthorizationError && error.indeterminate) || this.closed) throw error
      return operation()
    }
  }
  private writeControl(line: string): Promise<void> {
    if (!this.mud || this.mud.destroyed) {
      this.fail(CLOSE_INTERNAL, 'MUD connection unavailable')
      return Promise.reject(new Error('MUD connection unavailable'))
    }
    return new Promise<void>((resolve, reject) => {
      this.mud!.write(Buffer.from(line, 'ascii'), (error) => {
        if (!error) { resolve(); return }
        this.fail(CLOSE_INTERNAL, 'MUD control write failed')
        reject(error)
      })
    })
  }
  private pauseInput(): void { this.paused = true }
  private resumeInput(): void { if (this.closed) return; this.paused = false; void this.track(this.drainMessages()).catch(() => undefined) }
  private sendBinary(data: Buffer): void { sendBufferedWebSocketFrame(this.ws, data, true, this.config.maxBufferedBytes, (code, reason) => this.fail(code, reason)) }
  private sendText(value: Record<string, unknown>): void { sendBufferedWebSocketFrame(this.ws, jsonFrame(value), false, this.config.maxBufferedBytes, (code, reason) => this.fail(code, reason)) }
  private scheduleExpiry(expiresAtMs: number): void {
    const closeWhenDue = () => {
      const remaining = expiresAtMs - this.now()
      if (remaining <= 0) return this.fail(CLOSE_TOKEN_EXPIRED, 'token expired')
      this.expiryTimer = this.timers.setTimeout(closeWhenDue, Math.min(remaining, 2_147_483_647))
      unrefTimer(this.expiryTimer)
    }
    closeWhenDue()
  }
  private fail(code: number, reason: string): void { if (this.closed || this.failed) return; this.failed = true; this.sendText({ type: 'error', reason }); closeSocket(this.ws, code, reason); this.mud?.destroy() }
  private finish(): void { if (this.closed) return; this.closed = true; this.state = 'closed'; this.pendingMessages.length = 0; this.pendingMessageBytes = 0; this.timers.clearTimeout(this.authTimer); if (this.connectTimer) this.timers.clearTimeout(this.connectTimer); if (this.admissionTimer) this.timers.clearTimeout(this.admissionTimer); if (this.expiryTimer) this.timers.clearTimeout(this.expiryTimer); this.mud?.destroy(); this.cancelUnreservedIntent(); this.onClosed() }
  private cancelUnreservedIntent(attempt = 0): void {
    if (!this.unreservedIntentMayExist || this.unreservedCancellationStarted || !this.actorUserId || !this.correlationId) return
    this.unreservedCancellationStarted = true
    void this.track(this.cancelUnreservedAttempt(attempt)).catch(() => undefined)
  }

  private async cancelUnreservedAttempt(attempt: number): Promise<void> {
    try {
      await this.authorizer.cancelUnreserved({ actorUserId: this.actorUserId!, correlationId: this.correlationId! })
      this.unreservedIntentMayExist = false
    } catch {
      const delay = ONBOARDING_CANCEL_RETRY_DELAYS_MS[attempt]
      if (delay === undefined) { this.logger.warn('unreserved onboarding cancellation failed after bounded retries'); return }
      await new Promise<void>((resolve) => {
        const retryTimer = this.timers.setTimeout(resolve, delay)
        unrefTimer(retryTimer)
      })
      this.unreservedCancellationStarted = false
      return this.cancelUnreservedAttempt(attempt + 1)
    }
  }
}

export function startGateway(config: GatewayConfig, dependencies?: GatewayDependencies): RunningGateway {
  const gateway = createGateway(config, dependencies)
  gateway.server.listen(config.port, config.host)
  return gateway
}
