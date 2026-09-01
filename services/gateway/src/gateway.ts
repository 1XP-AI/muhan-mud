import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import { Socket, createConnection } from 'node:net'
import { URL } from 'node:url'
import { WebSocketServer, WebSocket, type RawData } from 'ws'
import type { GatewayConfig } from './config.js'
import { AuthenticationError, SupabaseAuthenticator, type AuthenticatedIdentity } from './auth.js'
import { TelnetParser } from './telnet.js'

const PROTOCOL = 'muhan.v1'
const CLOSE_POLICY = 1008
const CLOSE_TRY_AGAIN = 1013
const CLOSE_INTERNAL = 1011
const CLOSE_RESTART = 1012
const CLOSE_TOKEN_EXPIRED = 4001

interface Authenticator {
  verify(accessToken: string): Promise<AuthenticatedIdentity>
}

interface GatewayDependencies {
  authenticator?: Authenticator
  connectTcp?: (host: string, port: number) => Socket
  logger?: Pick<Console, 'info' | 'warn' | 'error'>
}

export interface RunningGateway {
  server: Server
  address(): string
  close(): Promise<void>
}

type SessionState = 'awaiting-auth' | 'connecting' | 'ready' | 'closed'

class ByteRateLimiter {
  private available: number
  private previous = Date.now()

  constructor(private readonly bytesPerSecond: number) {
    this.available = bytesPerSecond
  }

  take(bytes: number): boolean {
    const now = Date.now()
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

function parseAuthMessage(data: Buffer): string {
  let frame: unknown
  try {
    frame = JSON.parse(data.toString('utf8'))
  } catch {
    throw new AuthenticationError('first frame must be valid auth JSON')
  }
  if (!frame || typeof frame !== 'object' || Array.isArray(frame)) throw new AuthenticationError('first frame must be an auth object')
  const value = frame as Record<string, unknown>
  if (value.type !== 'auth' || typeof value.accessToken !== 'string' || value.accessToken.length === 0 || value.accessToken.length > 12_000) {
    throw new AuthenticationError('first frame must contain a non-empty auth accessToken')
  }
  return value.accessToken
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
  const authenticator = dependencies.authenticator ?? (config.authDisabled
    ? { verify: async (): Promise<AuthenticatedIdentity> => ({ sub: 'test-user', expiresAtMs: Date.now() + 3_600_000, claims: {} }) }
    : new SupabaseAuthenticator(config))
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
    const session = new GatewaySession(ws, request, config, authenticator, connectTcp, logger, () => {
      sessions.delete(session)
      activeConnections -= 1
    })
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
  private readonly parser = new TelnetParser()
  private readonly inputLimiter: ByteRateLimiter
  private closed = false
  private failed = false

  constructor(
    private readonly ws: WebSocket,
    private readonly request: IncomingMessage,
    private readonly config: GatewayConfig,
    private readonly authenticator: Authenticator,
    private readonly connectTcp: (host: string, port: number) => Socket,
    private readonly logger: Pick<Console, 'info' | 'warn' | 'error'>,
    private readonly onClosed: () => void
  ) {
    this.inputLimiter = new ByteRateLimiter(config.inputBytesPerSecond)
    this.authTimer = setTimeout(() => this.fail(CLOSE_POLICY, 'authentication timed out'), config.authTimeoutMs)
    this.authTimer.unref()
    ws.on('message', (data, isBinary) => void this.onMessage(rawToBuffer(data), isBinary))
    ws.on('error', (error) => this.logger.warn(`websocket error: ${error.message}`))
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
      let token: string
      try {
        token = parseAuthMessage(data)
      } catch (error) {
        return this.fail(CLOSE_POLICY, error instanceof Error ? error.message : 'invalid auth frame')
      }
      this.state = 'connecting'
      try {
        const identity = await this.authenticator.verify(token)
        if (this.closed) return
        this.scheduleExpiry(identity.expiresAtMs)
        this.connectMud()
      } catch (error) {
        return this.fail(CLOSE_POLICY, error instanceof Error ? error.message : 'authentication failed')
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

  private connectMud(): void {
    const mud = this.connectTcp(this.config.mudHost, this.config.mudPort)
    this.mud = mud
    const timer = setTimeout(() => {
      if (this.state === 'connecting') {
        mud.destroy()
        this.fail(CLOSE_INTERNAL, 'MUD connection timed out')
      }
    }, this.config.tcpConnectTimeoutMs)
    timer.unref()
    mud.once('connect', () => {
      clearTimeout(timer)
      if (this.closed) return mud.destroy()
      this.state = 'ready'
      this.sendText({ type: 'ready' })
    })
    mud.on('data', (data: Buffer) => this.onMudData(data))
    mud.on('drain', () => this.resumeWebSocket())
    mud.on('error', (error) => {
      clearTimeout(timer)
      if (!this.closed) this.fail(CLOSE_INTERNAL, `MUD connection error: ${error.message}`)
    })
    mud.on('end', () => {
      if (!this.closed) this.fail(CLOSE_INTERNAL, 'MUD connection closed')
    })
  }

  private onMudData(data: Buffer): void {
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
      const remaining = expiresAtMs - Date.now()
      if (remaining <= 0) return this.fail(CLOSE_TOKEN_EXPIRED, 'token expired')
      this.expiryTimer = setTimeout(closeWhenDue, Math.min(remaining, 2_147_483_647))
      this.expiryTimer.unref()
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
    clearTimeout(this.authTimer)
    if (this.expiryTimer) clearTimeout(this.expiryTimer)
    this.mud?.destroy()
    this.onClosed()
  }
}

export function startGateway(config: GatewayConfig, dependencies?: GatewayDependencies): RunningGateway {
  const gateway = createGateway(config, dependencies)
  gateway.server.listen(config.port, config.host)
  return gateway
}
