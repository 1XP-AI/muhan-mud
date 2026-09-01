import assert from 'node:assert/strict'
import { once } from 'node:events'
import { createServer, type Server } from 'node:net'
import test from 'node:test'
import WebSocket, { type RawData } from 'ws'
import { loadConfig } from '../src/config.js'
import { createGateway, type RunningGateway } from '../src/gateway.js'

interface ReceivedMessage { data: RawData; isBinary: boolean }

async function waitForMessage(messages: ReceivedMessage[], predicate: (message: ReceivedMessage) => boolean): Promise<ReceivedMessage> {
  let found: ReceivedMessage | undefined
  await eventually(() => {
    found = messages.find(predicate)
    assert.ok(found, 'timed out waiting for websocket message')
  })
  return found!
}

test('auth-first WebSocket relay preserves TCP bytes and maps Telnet echo controls', async (t) => {
  const receivedFromGateway: Buffer[] = []
  let mudConnections = 0
  const mud = createServer((socket) => {
    mudConnections += 1
    socket.on('data', (data) => receivedFromGateway.push(Buffer.from(data)))
    socket.write(Buffer.from([0xec, 0x95])) // split UTF-8 for "안"
    socket.write(Buffer.from([0x88, 0xff, 0xfb, 0x01, 0x0d, 0x0a]))
  })
  mud.listen(0, '127.0.0.1')
  await once(mud, 'listening')
  const mudAddress = mud.address()
  assert.ok(mudAddress && typeof mudAddress !== 'string')

  const config = loadConfig({
    NODE_ENV: 'test',
    AUTH_DISABLED: 'true',
    ALLOWED_ORIGINS: 'http://localhost:3000',
    MUD_HOST: '127.0.0.1',
    MUD_PORT: String(mudAddress.port),
    PORT: '0',
    HOST: '127.0.0.1',
    AUTH_TIMEOUT_MS: '500',
    SHUTDOWN_GRACE_MS: '100'
  })
  const gateway = createGateway(config, { logger: { info() {}, warn() {}, error() {} } })
  gateway.server.listen(0, '127.0.0.1')
  await once(gateway.server, 'listening')
  t.after(async () => {
    await closeGateway(gateway)
    await closeServer(mud)
  })

  const ws = new WebSocket(`${gateway.address().replace('http:', 'ws:')}/ws`, 'muhan.v1', { origin: 'http://localhost:3000' })
  await once(ws, 'open')
  const messages: ReceivedMessage[] = []
  ws.on('message', (data, isBinary) => messages.push({ data, isBinary }))
  assert.equal(mudConnections, 0, 'TCP MUD must not be reached before the auth frame')
  ws.send(JSON.stringify({ type: 'auth', accessToken: 'test-token' }))
  const ready = await waitForMessage(messages, ({ data, isBinary }) => !isBinary && Buffer.from(data).toString() === '{"type":"ready"}')
  assert.equal(ready.isBinary, false)
  const echo = await waitForMessage(messages, ({ data, isBinary }) => !isBinary && Buffer.from(data).toString() === '{"type":"echo","enabled":false}')
  assert.equal(echo.isBinary, false)
  await eventually(() => assert.equal(Buffer.concat(messages.filter(({ isBinary }) => isBinary).map(({ data }) => Buffer.from(data))).toString('utf8'), '안\r\n'))
  ws.send(Buffer.from('look\n'))
  await eventually(() => {
    const input = Buffer.concat(receivedFromGateway)
    assert.deepEqual(input.subarray(0, 3), Buffer.from([0xff, 0xfd, 0x01]), 'gateway accepts WILL ECHO with DO ECHO')
    assert.equal(input.subarray(3).toString('utf8'), 'look\n')
  })
  ws.close()
})

async function eventually(assertion: () => void): Promise<void> {
  let lastError: unknown
  for (let attempt = 0; attempt < 20; attempt += 1) {
    try {
      assertion()
      return
    } catch (error) {
      lastError = error
      await new Promise((resolve) => setTimeout(resolve, 20))
    }
  }
  throw lastError
}

async function closeGateway(gateway: RunningGateway): Promise<void> {
  await gateway.close()
}

async function closeServer(server: Server): Promise<void> {
  await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
}
