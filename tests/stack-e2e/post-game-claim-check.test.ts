import assert from 'node:assert/strict'
import test from 'node:test'
import { assertPostGameClaim, type PostGameLayout } from './post-game-claim-check.js'

function fixture(longSize = 8, littleEndian = true) {
  const layout: PostGameLayout = {
    recordSize: 64 + 45 * (3 * longSize + 8), longSize, littleEndian,
    fd: { offset: 24, length: 4 }, parentRoom: { offset: 32, length: 8 },
    timers: { offset: 64, stride: 3 * longSize + 8, count: 45, ltimeOffset: 0, intervalOffset: longSize, miscOffset: 2 * longSize },
    saveIndex: 22, hoursIndex: 28, healIndex: 8, saveInterval: 600,
  }
  const before = Buffer.alloc(layout.recordSize + 23, 0x37)
  const after = Buffer.from(before)
  const offset = (index: number, field: 'ltimeOffset' | 'intervalOffset' | 'miscOffset') => layout.timers.offset + index * layout.timers.stride + layout.timers[field]
  const set = (buffer: Buffer, index: number, field: 'ltimeOffset' | 'intervalOffset' | 'miscOffset', value: number) => {
    const pos = offset(index, field)
    if (longSize === 8) littleEndian ? buffer.writeBigInt64LE(BigInt(value), pos) : buffer.writeBigInt64BE(BigInt(value), pos)
    else littleEndian ? buffer.writeInt32LE(value, pos) : buffer.writeInt32BE(value, pos)
  }
  for (let i = 0; i < 45; i++) {
    set(before, i, 'ltimeOffset', 890)
    set(before, i, 'intervalOffset', 10)
    set(after, i, 'ltimeOffset', 990)
    set(after, i, 'intervalOffset', 10)
  }
  set(before, 28, 'ltimeOffset', 900)
  set(after, 28, 'ltimeOffset', 1000)
  set(after, 22, 'ltimeOffset', 1000)
  set(after, 22, 'intervalOffset', 600)
  return { before, after, layout, set, offset, window: { startedAt: 1000, endedAt: 1010 } }
}

for (const width of [4, 8]) for (const endian of [true, false]) {
  test(`accepts native ${width}-byte ${endian ? 'little' : 'big'} endian timers and unchanged inventory`, () => {
    const f = fixture(width, endian)
    f.after[24] ^= 1; f.after[32] ^= 1
    assertPostGameClaim(f.before, f.after, f.layout, f.window)
  })
}

test('accepts two admissions, HOURS accounting and quick healing', () => {
  const f = fixture()
  // Admission at 1000, update at 1003, reselection at 1005, update at 1008.
  for (let i = 0; i < 45; i++) f.set(f.after, i, 'ltimeOffset', 992)
  f.set(f.after, 28, 'ltimeOffset', 1008)
  f.set(f.after, 28, 'intervalOffset', 16)
  f.set(f.after, 22, 'ltimeOffset', 1005)
  f.set(f.after, 8, 'ltimeOffset', 1008)
  f.set(f.after, 8, 'intervalOffset', 1)
  assertPostGameClaim(f.before, f.after, f.layout, f.window)
})

const corruptions: Record<string, (f: ReturnType<typeof fixture>) => void> = {
  password: f => { f.after[8] ^= 1 },
  otherBytes: f => { f.after[45] ^= 1 },
  tailInventory: f => { f.after[f.after.length - 1] ^= 1 },
  padding: f => { f.after[f.offset(0, 'miscOffset') + 8] ^= 1 },
  timerMisc: f => f.set(f.after, 0, 'miscOffset', 0),
  ordinaryInterval: f => f.set(f.after, 0, 'intervalOffset', 11),
  hugeTimestamp: f => f.set(f.after, 0, 'ltimeOffset', 999999),
  backwardsTimestamp: f => f.set(f.after, 0, 'ltimeOffset', 889),
  excessiveRebase: f => f.set(f.after, 0, 'ltimeOffset', 1001),
  hoursInterval: f => f.set(f.after, 28, 'intervalOffset', 21),
  backwardsHours: f => f.set(f.after, 28, 'intervalOffset', 9),
  staleHours: f => f.set(f.after, 28, 'ltimeOffset', 999),
  staleSave: f => f.set(f.after, 22, 'ltimeOffset', 999),
  saveInterval: f => f.set(f.after, 22, 'intervalOffset', 601),
  healInterval: f => f.set(f.after, 8, 'intervalOffset', 2),
  staleChangedHeal: f => f.set(f.after, 8, 'intervalOffset', 5),
}
for (const [name, corrupt] of Object.entries(corruptions)) test(`rejects ${name}`, () => {
  const f = fixture(); corrupt(f)
  assert.throws(() => assertPostGameClaim(f.before, f.after, f.layout, f.window))
})

test('rejects invalid windows, lengths and layout before masking bytes', () => {
  const f = fixture()
  for (const window of [{ startedAt: 1011, endedAt: 1010 }, { startedAt: NaN, endedAt: 1010 }, { startedAt: 1000, endedAt: Infinity }, { startedAt: 1000.5, endedAt: 1010 }])
    assert.throws(() => assertPostGameClaim(f.before, f.after, f.layout, window))
  assert.throws(() => assertPostGameClaim(f.before, f.after.subarray(1), f.layout, f.window))
  for (const layout of [
    { ...f.layout, recordSize: f.before.length + 1 },
    { ...f.layout, longSize: 3 },
    { ...f.layout, fd: { offset: -1, length: 4 } },
    { ...f.layout, fd: { offset: 64, length: 4 } },
    { ...f.layout, timers: { ...f.layout.timers, count: 44 } },
    { ...f.layout, timers: { ...f.layout.timers, intervalOffset: 0 } },
    { ...f.layout, timers: { ...f.layout.timers, miscOffset: 99 } },
  ]) assert.throws(() => assertPostGameClaim(f.before, f.after, layout, f.window))
})
