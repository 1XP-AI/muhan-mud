import assert from 'node:assert/strict'

export interface PostGameLayout {
  recordSize: number
  longSize: number
  littleEndian: boolean
  fd: { offset: number; length: number }
  parentRoom: { offset: number; length: number }
  timers: { offset: number; stride: number; count: number; ltimeOffset: number; intervalOffset: number; miscOffset: number }
  saveIndex: 22
  hoursIndex: 28
  healIndex: 8
  saveInterval: 600
}

export function assertPostGameClaim(before: Buffer, after: Buffer, layout: PostGameLayout,
  window: { startedAt: number; endedAt: number }): void {
  const natural = (value: number) => Number.isSafeInteger(value) && value >= 0
  assert(natural(window.startedAt) && natural(window.endedAt) && window.startedAt <= window.endedAt, 'invalid post-game observation window')
  assert.equal(before.length, after.length, 'player file size changed')
  assert(natural(layout.recordSize) && layout.recordSize > 0 && layout.recordSize <= before.length, 'invalid creature record size')
  assert(layout.longSize === 4 || layout.longSize === 8, 'unsupported native long size')
  assert.equal(typeof layout.littleEndian, 'boolean')
  assert.equal(layout.saveIndex, 22); assert.equal(layout.hoursIndex, 28); assert.equal(layout.healIndex, 8)
  assert.equal(layout.saveInterval, 600)
  const timers = layout.timers
  assert.equal(timers.count, 45, 'C admission rebases exactly 45 timers')
  assert(natural(timers.offset) && natural(timers.stride) && timers.stride > 0 &&
    timers.offset + timers.stride * timers.count <= layout.recordSize, 'timer array outside creature record')
  const fieldOffsets = [timers.ltimeOffset, timers.intervalOffset, timers.miscOffset]
  for (const offset of fieldOffsets) assert(natural(offset) && offset + layout.longSize <= timers.stride, 'timer member outside timer stride')
  for (let i = 0; i < fieldOffsets.length; i++) for (let j = i + 1; j < fieldOffsets.length; j++)
    assert(Math.abs(fieldOffsets[i] - fieldOffsets[j]) >= layout.longSize, 'overlapping timer members')
  const regions = [layout.fd, layout.parentRoom, { offset: timers.offset, length: timers.stride * timers.count }]
  for (const region of regions) assert(natural(region.offset) && natural(region.length) && region.length > 0 &&
    region.offset + region.length <= layout.recordSize, 'native field outside creature record')
  for (let i = 0; i < regions.length; i++) for (let j = i + 1; j < regions.length; j++)
    assert(regions[i].offset + regions[i].length <= regions[j].offset || regions[j].offset + regions[j].length <= regions[i].offset, 'overlapping native fields')

  const position = (index: number, offset: number) => timers.offset + index * timers.stride + offset
  const read = (buffer: Buffer, index: number, offset: number): bigint => {
    const at = position(index, offset)
    if (layout.longSize === 8) return layout.littleEndian ? buffer.readBigInt64LE(at) : buffer.readBigInt64BE(at)
    return BigInt(layout.littleEndian ? buffer.readInt32LE(at) : buffer.readInt32BE(at))
  }
  const start = BigInt(window.startedAt), end = BigInt(window.endedAt)
  const oldHours = read(before, layout.hoursIndex, timers.ltimeOffset)
  assert(oldHours >= 0n && oldHours <= start, 'before HOURS timestamp must precede observation window')
  const maxRebase = end - oldHours
  const inWindow = (value: bigint) => value >= start && value <= end
  const allowed = new Uint8Array(layout.recordSize)
  const allow = (offset: number, length: number) => allowed.fill(1, offset, offset + length)
  allow(layout.fd.offset, layout.fd.length)
  allow(layout.parentRoom.offset, layout.parentRoom.length)
  for (let i = 0; i < timers.count; i++) {
    const oldTime = read(before, i, timers.ltimeOffset), newTime = read(after, i, timers.ltimeOffset)
    const oldInterval = read(before, i, timers.intervalOffset), newInterval = read(after, i, timers.intervalOffset)
    assert(newTime >= oldTime && newTime <= end, `timer ${i} timestamp moved backwards or beyond observation window`)
    // C admission adds t-oldHours and clamps at t. Repeated admissions cannot
    // accumulate more than end-oldHours; save/heal can additionally reset to t.
    const retainedRebase = newTime - oldTime <= maxRebase
    if (i === layout.saveIndex) {
      assert(inWindow(newTime), 'PSAVE timestamp outside admission window')
      assert.equal(newInterval, BigInt(layout.saveInterval), 'PSAVE interval changed')
    } else if (i === layout.hoursIndex) {
      assert(inWindow(newTime), 'HOURS timestamp outside update/admission window')
      assert(newInterval >= oldInterval && newInterval - oldInterval <= end - start, 'HOURS interval outside observed elapsed duration')
      assert(retainedRebase, 'HOURS timestamp exceeds maximum rebase')
    } else if (i === layout.healIndex) {
      assert(newInterval === oldInterval || newInterval === 5n || newInterval === 1n, 'HEALS interval is neither retained nor normal/quick healing')
      assert((newInterval === oldInterval && retainedRebase) ||
        ((newInterval === 5n || newInterval === 1n) && inWindow(newTime)), 'HEALS timestamp is neither retained/rebased nor an observed heal')
    } else {
      assert(retainedRebase, `timer ${i} timestamp exceeds maximum rebase`)
      assert.equal(newInterval, oldInterval, `timer ${i} interval changed`)
    }
    allow(position(i, timers.ltimeOffset), layout.longSize)
    if (i === layout.saveIndex || i === layout.hoursIndex || i === layout.healIndex)
      allow(position(i, timers.intervalOffset), layout.longSize)
  }
  // Compare the whole file, including password, structure padding, timer misc,
  // and serialized inventory following the native creature record.
  for (let offset = 0; offset < before.length; offset++) if (!allowed[offset])
    assert(after[offset] === before[offset], `post-game claim changed protected byte ${offset}`)
}
