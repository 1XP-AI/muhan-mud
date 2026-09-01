import process from 'node:process'
import WebSocket, { type RawData } from 'ws'

const gatewayUrl = process.argv[2]
if (!gatewayUrl) throw new Error('gateway WebSocket URL is required')

const characterId = '00000000-0000-4000-8000-000000000002'
const healthMarker = Buffer.from('체력', 'utf8')
const output: Buffer[] = []
let ready = false
let healthSeen = false
let normalCloseSeen = false
let finished = false

const ws = new WebSocket(gatewayUrl, 'muhan.v1', { origin: 'http://localhost:3000' })
const timeout = setTimeout(() => finish(new Error('real C relay timed out')), 8_000)

function finish(error?: Error): void {
  if (finished) return
  finished = true
  clearTimeout(timeout)
  if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) ws.close()
  if (error) {
    process.stderr.write('real C gateway scenario failed\n')
    process.exitCode = 1
  } else {
    process.stdout.write('{"status":"passed","scenario":"gateway-real-c"}\n')
  }
}

ws.on('open', () => {
  ws.send(JSON.stringify({
    type: 'auth',
    accessToken: 'test-only-access-token',
    characterId
  }))
})

ws.on('message', (data: RawData, isBinary: boolean) => {
  const bytes = Buffer.isBuffer(data) ? data : Buffer.from(data as ArrayBuffer)
  if (!isBinary) {
    let frame: unknown
    try { frame = JSON.parse(bytes.toString('utf8')) } catch { return finish(new Error('invalid control frame')) }
    if (!frame || typeof frame !== 'object') return finish(new Error('invalid control frame'))
    const type = (frame as { type?: unknown }).type
    if (type === 'error') return finish(new Error('gateway rejected real C relay'))
    if (type === 'closed') {
      if (healthSeen) normalCloseSeen = true
      else return finish(new Error('gateway closed before real C health response'))
      return
    }
    if (type === 'ready' && !ready) {
      ready = true
      ws.send(Buffer.from('건강\n', 'utf8'))
    }
    return
  }
  if (!ready) return finish(new Error('binary data arrived before admission ACK'))
  output.push(bytes)
  if (!healthSeen && Buffer.concat(output).includes(healthMarker)) {
    healthSeen = true
    ws.send(Buffer.from('끝\n', 'utf8'))
  }
})

ws.on('error', () => finish(new Error('websocket transport failed')))
ws.on('close', (code) => {
  if (finished) return
  if (normalCloseSeen && code === 1000) finish()
  else finish(new Error('websocket closed before real C normal close'))
})
