import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { EventEmitter } from 'node:events'
import { PassThrough } from 'node:stream'
import { test } from 'node:test'
import {
  InvalidPlayerSnapshotV1NormalizedProjectionError,
  type PlayerSnapshotV1NormalizedProjectionProcessFactory,
  projectPlayerSnapshotV1Normalized,
} from '../src/player-snapshot-v1-normalized-projection.js'

const payload = Buffer.from('one immutable PlayerSnapshotV1 artifact')
const snapshotSha256 = createHash('sha256').update(payload).digest('hex')

function projection(value: Record<string, unknown> = {}): string {
  return `${JSON.stringify({
    format: 'player-snapshot-v1-normalized-projection', version: 1, algorithm: 'sha-256',
    canonical_digest: 'a'.repeat(64),
    player: {
      level: 42, hp_max: 100, hp_current: 99, mp_max: 50, mp_current: 49,
      experience: 9_223_372_036_854_775_807n.toString(), gold: '-9223372036854775808',
      daily: Array.from({ length: 10 }, () => ({ max: 255, current: 255, last_used: '9223372036854775807' })),
      timers: Array.from({ length: 45 }, () => ({ interval: '-9223372036854775808', last_used: 0, misc: -32768 })),
      items: [{
        parent_index: null, child_index: 0, value: '9223372036854775807', weight: -32768,
        type_code: -128, adjustment: 127, shots_max: 2, shots_current: 2, ndice: -32768,
        sdice: 32767, pdice: 0, armor: -128, wear_flag: 127, magic_power: 0,
        magic_realm: -1, special: 32767,
      }],
      ...value,
    },
  }).replace(/"(-?\d{16,})"/g, '$1')}\n`
}

function fakeProcess(
  stdout: Uint8Array | string,
  stderr: Uint8Array | string = '',
  code: number | null = 0,
  signal: NodeJS.Signals | null = null,
): {
  factory: PlayerSnapshotV1NormalizedProjectionProcessFactory
  stdin: PassThrough
  child: EventEmitter & { killCalls: NodeJS.Signals[] }
  calls: Array<{ file: string, args: string[], options: Parameters<PlayerSnapshotV1NormalizedProjectionProcessFactory>[2] }>
} {
  const stdin = new PassThrough()
  const calls: Array<{ file: string, args: string[], options: Parameters<PlayerSnapshotV1NormalizedProjectionProcessFactory>[2] }> = []
  const child = Object.assign(new EventEmitter(), {
    stdin, stdout: PassThrough.from([stdout]), stderr: PassThrough.from([stderr]), killCalls: [] as NodeJS.Signals[],
    kill(value: NodeJS.Signals) { this.killCalls.push(value); return true },
  })
  const factory: PlayerSnapshotV1NormalizedProjectionProcessFactory = (file, args, options) => {
    calls.push({ file, args, options })
    setImmediate(() => child.emit('close', code, signal))
    return child as never
  }
  return { factory, stdin, child, calls }
}

function pendingProcess() {
  const child = Object.assign(new EventEmitter(), {
    stdin: new PassThrough(), stdout: new PassThrough(), stderr: new PassThrough(), killCalls: [] as NodeJS.Signals[],
    kill(signal: NodeJS.Signals) { this.killCalls.push(signal); return true },
  })
  return { child, factory: (() => child as never) as PlayerSnapshotV1NormalizedProjectionProcessFactory }
}

async function rejects(action: () => Promise<unknown>): Promise<void> {
  await assert.rejects(action, (error: unknown) => error instanceof InvalidPlayerSnapshotV1NormalizedProjectionError
    && error.message === 'invalid player snapshot normalized projection')
}

test('normalized projection passes one immutable artifact to the fixed shell-free runner and returns only the safe projection', async () => {
  const fake = fakeProcess(projection())
  const result = await projectPlayerSnapshotV1Normalized(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_normalized_project', snapshotSha256, processFactory: fake.factory,
  })
  const received: Buffer[] = []
  fake.stdin.on('data', (chunk: Buffer) => received.push(chunk))
  await new Promise<void>((resolve) => fake.stdin.on('end', resolve))
  assert.deepEqual(Buffer.concat(received), payload)
  assert.deepEqual(fake.calls, [{
    file: '/opt/muhan/player_snapshot_v1_normalized_project', args: ['--snapshot-sha256', snapshotSha256],
    options: { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] },
  }])
  assert.equal(result.canonicalDigest, 'a'.repeat(64))
  assert.equal(result.player.experience, 9_223_372_036_854_775_807n)
  assert.equal(result.player.gold, -9_223_372_036_854_775_808n)
  assert.deepEqual(Object.keys(result.player).sort(), [
    'daily', 'experience', 'gold', 'hpCurrent', 'hpMax', 'items', 'level', 'mpCurrent', 'mpMax', 'timers',
  ])
  assert.equal(Object.keys(result.player.items[0]!).includes('parent_index'), false)
})

test('normalized projection rejects bad configuration or a payload/digest mismatch before spawning', async () => {
  for (const options of [
    { runnerPath: 'relative-runner', snapshotSha256 },
    { runnerPath: '/opt/runner', snapshotSha256: 'A'.repeat(64) },
    { runnerPath: '/opt/runner', snapshotSha256: 'f'.repeat(64) },
  ]) {
    let calls = 0
    const processFactory: PlayerSnapshotV1NormalizedProjectionProcessFactory = () => { calls++; throw new Error('must not run') }
    await rejects(() => projectPlayerSnapshotV1Normalized(payload, { ...options, processFactory }))
    assert.equal(calls, 0)
  }
})

test('normalized projection rejects non-success, diagnostics, partial/malformed output, unknown keys, leaks, invalid bounds, and invalid topology', async () => {
  const cases: Array<{ stdout: string, stderr?: string, code?: number | null, signal?: NodeJS.Signals | null }> = [
    { stdout: projection(), stderr: 'diagnostic' }, { stdout: projection(), code: 1 }, { stdout: projection(), signal: 'SIGTERM' },
    { stdout: projection().slice(0, -2) }, { stdout: projection().replace('"algorithm":"sha-256"', '"algorithm":"sha-512"') },
    { stdout: projection().replace('"items":[', '"flags":[],"items":[') },
    { stdout: projection().replace('"level":42', '"level":256') },
    { stdout: projection().replace('"shots_current":2', '"shots_current":3') },
    { stdout: projection().replace('"child_index":0', '"child_index":1') },
    { stdout: projection().replace('"a'.repeat(1), '"NO_LEAK_TEXT') },
    { stdout: projection().replace('"daily":[', '"daily":[').replace(/\},\{"max":255/g, '') },
  ]
  for (const value of cases) {
    const fake = fakeProcess(value.stdout, value.stderr, value.code, value.signal)
    await rejects(() => projectPlayerSnapshotV1Normalized(payload, {
      runnerPath: '/opt/muhan/player_snapshot_v1_normalized_project', snapshotSha256, processFactory: fake.factory,
    }))
  }
})

test('normalized projection bounds combined output and timeout, waiting for close before failing', async () => {
  const fake = pendingProcess()
  const result = projectPlayerSnapshotV1Normalized(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_normalized_project', snapshotSha256, timeoutMs: 1, processFactory: fake.factory,
  })
  await new Promise((resolve) => setTimeout(resolve, 10))
  assert.deepEqual(fake.child.killCalls, ['SIGKILL'])
  fake.child.emit('close', null, 'SIGKILL')
  await rejects(() => result)

  const oversized = pendingProcess()
  const overflow = projectPlayerSnapshotV1Normalized(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_normalized_project', snapshotSha256, processFactory: oversized.factory,
  })
  oversized.child.stdout.write('x'.repeat(4_194_353))
  assert.deepEqual(oversized.child.killCalls, ['SIGKILL'])
  oversized.child.emit('close', null, 'SIGKILL')
  await rejects(() => overflow)
})
