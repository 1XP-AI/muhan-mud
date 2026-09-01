import assert from 'node:assert/strict'
import test from 'node:test'
import { TelnetParser } from '../src/telnet.js'

test('preserves split UTF-8 bytes while consuming fragmented Telnet echo negotiation', () => {
  const parser = new TelnetParser()
  assert.deepEqual(parser.feed(Buffer.from([0xec, 0x95])), [{ type: 'data', data: Buffer.from([0xec, 0x95]) }])
  const events = parser.feed(Buffer.from([0x88, 0xff, 0xfb, 0x01, 0xeb]))
  assert.deepEqual(events, [
    { type: 'data', data: Buffer.from([0x88]) },
    { type: 'echo', enabled: false },
    { type: 'reply', data: Buffer.from([0xff, 0xfd, 0x01]) },
    { type: 'data', data: Buffer.from([0xeb]) }
  ])
})

test('consumes fragmented WONT ECHO and rejects unknown options', () => {
  const parser = new TelnetParser()
  assert.deepEqual(parser.feed(Buffer.from([0xff, 0xfc])), [])
  assert.deepEqual(parser.feed(Buffer.from([0x01, 0xff, 0xfb, 0x18])), [
    { type: 'echo', enabled: true },
    { type: 'reply', data: Buffer.from([0xff, 0xfe, 0x01]) },
    { type: 'reply', data: Buffer.from([0xff, 0xfe, 0x18]) }
  ])
})

test('drops Telnet subnegotiation without corrupting surrounding bytes', () => {
  const parser = new TelnetParser()
  const events = parser.feed(Buffer.from([0x41, 0xff, 0xfa, 0x18, 0x01, 0xff, 0xf0, 0x42]))
  assert.deepEqual(events, [
    { type: 'data', data: Buffer.from('A') },
    { type: 'data', data: Buffer.from('B') }
  ])
})
