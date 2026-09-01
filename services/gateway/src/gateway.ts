import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import { randomUUID } from 'node:crypto'
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

const PROTOCOL = 'muhan.v1'
const CLOSE_POLICY = 1008
const CLOSE_TRY_AGAIN = 1013
const CLOSE_INTERNAL = 1011
const CLOSE_RESTART = 1012
const CLOSE_TOKEN_EXPIRED = 4001
const LEASE_TTL_MS = 120_000
const LEASE_RENEW_INTERVAL_MS = 60_000
const LEASE_MIN_REMAINING_MS = 30_000
const LEASE_RELEASE_RETRY_DELAYS_MS = [50, 200] as const

interface Authenticator {
  verify(accessToken: string): Promise<AuthenticatedIdentity>
}

export interface GatewayDependencies {
  authenticator?: Authenticator
  characterAuthorizer?: CharacterAuthorizer
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

export function createGateway(config: GatewayConfig, dependencies: GatewayDependencies = {}): RunningGateway {
  const logger = dependencies.logger ?? console
  const now = dependencies.now ?? Date.now
  const authenticator = dependencies.authenticator ?? (config.authDisabled
    ? { verify: async (): Promise<AuthenticatedIdentity> => ({ sub: TestOnlyCharacterAuthorizer.actorUserId, expiresAtMs: now() + 3_600_000, claims: {} }) }
    : new SupabaseAuthenticator(config))
  const characterAuthorizer = dependencies.characterAuthorizer ?? (config.authDisabled
    ? new TestOnlyCharacterAuthorizer()
    : new SupabaseCharacterAuthorizer(config))
  const connectTcp = dependencies.connectTcp ?? ((host, port) => createConnection({ host, port }))
  let accepting = true
  let activeConnections = 0
  const sessions = new Set<GatewaySession>()

  const server = createServer((request, response) => {
    if (request.url === '/healthz' && request.method === 'GET') {
      response.writeHead(accepting ? 200 : 503, { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' })
      response.end(JSON.stringify({ status: accepting ? 'ok' : 'draining', activeConnections }))
      return
    }
    response.writeHead(404, { 'content-type': 'application/json; charset=utf-8' })
    response.end('{"error":"not found"}')
  })

  const webSocketServer = new WebSocketServer({
    noServer: true,
    maxPayload: config.maxFrameBytes,
    clientTracking: false,
    handleProtocols(protocols) {
      return protocols.has(PROTOCOL) ? PROTOCOL : false
    }
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
    const hasProtocol = typeof offeredProtocols === 'string' && offeredProtocols.split(',').some((protocol) => protocol.trim() === PROTOCOL)
    if (!accepting || pathname !== '/ws' || typeof origin !== 'string' || !config.allowedOrigins.has(origin) || !hasProtocol || (config.requireSecureTransport && !isSecureRequest(request))) {
      socket.write('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n')
      socket.destroy()
      return
    }
    if (activeConnections >= config.maxConnections) {
      socket.write('HTTP/1.1 503 Service Unavailable\r\nConnection: close\r\n\r\n')
      socket.destroy()
      return
    }
    webSocketServer.handleUpgrade(request, socket, head, (ws) => webSocketServer.emit('connection', ws, request))
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
  })

  return {
    server,
    address: () => {
      const address = server.address()
      if (!address || typeof address === 'string') throw new Error('gateway is not listening')
      return `http://${address.address}:${address.port}`
    },
    close: async () => {
      if (!accepting) return
      accepting = false
      for (const session of sessions) session.drain()
      const serverClosed = new Promise<void>((resolve) => server.close(() => resolve()))
      await Promise.race([
        serverClosed,
        new Promise<void>((resolve) => setTimeout(resolve, config.shutdownGraceMs))
      ])
      for (const session of sessions) session.forceClose()
      await serverClosed
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
    ws.on('message', (data, isBinary) => void this.onMessage(rawToBuffer(data), isBinary))
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
    this.finish()
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
      if (!this.closed) this.fail(CLOSE_INTERNAL, 'MUD connection failed')
    })
    mud.on('end', () => {
      if (!this.closed) this.fail(CLOSE_INTERNAL, this.state === 'awaiting-admission' ? 'MUD admission failed' : 'MUD connection closed')
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
    if (this.ws.readyState !== WebSocket.OPEN) return
    if (this.ws.bufferedAmount + data.length > this.config.maxBufferedBytes) {
      this.fail(CLOSE_TRY_AGAIN, 'slow consumer')
      return
    }
    this.ws.send(data, { binary: true }, (error) => {
      if (error && !this.closed) this.fail(CLOSE_INTERNAL, 'websocket output failed')
    })
  }

  private sendText(value: Record<string, unknown>): void {
    if (this.ws.readyState !== WebSocket.OPEN) return
    const data = jsonFrame(value)
    if (this.ws.bufferedAmount + Buffer.byteLength(data) > this.config.maxBufferedBytes) return this.fail(CLOSE_TRY_AGAIN, 'slow consumer')
    this.ws.send(data, { binary: false }, (error) => {
      if (error && !this.closed) this.fail(CLOSE_INTERNAL, 'websocket output failed')
    })
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
    void this.characterAuthorizer.endSession(this.sessionId, this.config.gatewayInstanceId!).catch(() => {
      const delay = LEASE_RELEASE_RETRY_DELAYS_MS[attempt]
      if (delay === undefined) {
        this.logger.warn('character session lease release failed after bounded retries')
        return
      }
      this.timers.setTimeout(() => this.releaseLease(attempt + 1), delay)
    })
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
      this.renewalTimer = this.timers.setTimeout(() => { void renew() }, LEASE_RENEW_INTERVAL_MS)
      unrefTimer(this.renewalTimer)
    }
    this.renewalTimer = this.timers.setTimeout(() => { void renew() }, LEASE_RENEW_INTERVAL_MS)
    unrefTimer(this.renewalTimer)
  }
}

export function startGateway(config: GatewayConfig, dependencies?: GatewayDependencies): RunningGateway {
  const gateway = createGateway(config, dependencies)
  gateway.server.listen(config.port, config.host)
  return gateway
}
