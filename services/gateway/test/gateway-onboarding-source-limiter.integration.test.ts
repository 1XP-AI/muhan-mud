import assert from 'node:assert/strict'
import { once } from 'node:events'
import { createConnection, createServer } from 'node:net'
import test from 'node:test'
import WebSocket from 'ws'
import { loadConfig } from '../src/config.js'
import { createGateway } from '../src/gateway.js'

const correlation = '123e4567-e89b-12d3-a456-426614174001'

test('rejects an excess onboarding upgrade before another MUD TCP connection opens', async (t) => {
  let tcpConnections = 0
  const mudSockets = new Set<import('node:net').Socket>()
  const mud = createServer((socket) => {
    tcpConnections += 1
    mudSockets.add(socket)
    socket.once('close', () => mudSockets.delete(socket))
  })
  mud.listen(0, '127.0.0.1'); await once(mud, 'listening')
  const mudAddress = mud.address(); assert.ok(mudAddress && typeof mudAddress !== 'string')
  const config = loadConfig({
    NODE_ENV: 'test', AUTH_DISABLED: 'true', MUD_ONBOARDING_ENABLED: 'true',
    MUD_ONBOARDING_SOURCE_ATTEMPT_LIMIT: '1', MUD_ONBOARDING_SOURCE_ATTEMPT_WINDOW_MS: '1000', MUD_ONBOARDING_SOURCE_ATTEMPT_MAX_KEYS: '10',
    HOST: '127.0.0.1', PORT: '0', MUD_HOST: '127.0.0.1', MUD_PORT: String(mudAddress.port),
    ALLOWED_ORIGINS: 'http://localhost:3000', AUTH_TIMEOUT_MS: '500', TCP_CONNECT_TIMEOUT_MS: '500', MUD_ADMISSION_TIMEOUT_MS: '500'
  })
  const gateway = createGateway(config)
  gateway.server.listen(0, '127.0.0.1'); await once(gateway.server, 'listening')
  t.after(async () => {
    for (const socket of mudSockets) socket.destroy()
    await gateway.close()
    await new Promise<void>((resolve) => mud.close(() => resolve()))
  })
  const url = `${gateway.address().replace('http:', 'ws:')}/onboarding`

  const first = new WebSocket(url, 'muhan.onboarding.v1', { origin: 'http://localhost:3000' })
  t.after(() => first.terminate())
  await once(first, 'open')
  first.send(JSON.stringify({ type: 'onboarding-auth', accessToken: 'browser-token', mode: 'provision', correlationId: correlation }))
  await eventually(() => assert.equal(tcpConnections, 1))
  const firstClosed = once(first, 'close')
  first.terminate()
  await firstClosed

  const gatewayAddress = gateway.server.address(); assert.ok(gatewayAddress && typeof gatewayAddress !== 'string')
  const second = createConnection(gatewayAddress.port, gatewayAddress.address)
  t.after(() => second.destroy())
  await once(second, 'connect')
  const response = once(second, 'data')
  second.write([
    'GET /onboarding HTTP/1.1', `Host: ${gatewayAddress.address}:${gatewayAddress.port}`,
    'Upgrade: websocket', 'Connection: Upgrade', 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==',
    'Sec-WebSocket-Version: 13', 'Sec-WebSocket-Protocol: muhan.onboarding.v1', 'Origin: http://localhost:3000', '', ''
  ].join('\r\n'))
  const [data] = await withTimeout(response, 'the excess onboarding upgrade did not receive a response') as [Buffer]
  assert.match(data.toString('ascii'), /^HTTP\/1\.1 429/)
  await new Promise((resolve) => setTimeout(resolve, 20))
  assert.equal(tcpConnections, 1)
})

async function eventually(fn: () => void, timeoutMs = 1_000): Promise<void> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    try { fn(); return } catch (error) {
      if (Date.now() >= deadline) throw error
      await new Promise((resolve) => setTimeout(resolve, 5))
    }
  }
}

async function withTimeout<T>(promise: Promise<T>, message: string, timeoutMs = 1_000): Promise<T> {
  let timer: NodeJS.Timeout | undefined
  try {
    return await Promise.race([
      promise,
      new Promise<T>((_resolve, reject) => { timer = setTimeout(() => reject(new Error(message)), timeoutMs) })
    ])
  } finally {
    if (timer) clearTimeout(timer)
  }
}
