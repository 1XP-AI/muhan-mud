import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { EventEmitter } from 'node:events'
import { PassThrough } from 'node:stream'
import { test } from 'node:test'
import {
  InvalidPlayerSnapshotV1ReplayVerificationError,
  type ReplayVerifyProcessFactory,
  verifyPlayerSnapshotV1Replay,
} from '../src/player-snapshot-v1-replay-verifier.js'

const payload = Buffer.from('trusted payload')

function report(input: Uint8Array = payload): string {
  const digest = createHash('sha256').update(input).digest('hex')
  return [
    'format=player-snapshot-v1-replay-verification',
    'version=1',
    'algorithm=sha-256',
    `input_digest=${digest}`,
    `canonical_digest=${digest}`,
    `canonical_octets=${input.length}`,
    'inventory_node_count=0',
    '',
  ].join('\n')
}

function fakeProcess(
  stdout: Uint8Array | string,
  stderr: Uint8Array | string = '',
  code: number | null = 0,
  signal: NodeJS.Signals | null = null,
): {
  factory: ReplayVerifyProcessFactory
  stdin: PassThrough
  child: EventEmitter & { killCalls: NodeJS.Signals[] }
  calls: Array<{ file: string, args: string[], options: Parameters<ReplayVerifyProcessFactory>[2] }>
} {
  const stdin = new PassThrough()
  const calls: Array<{ file: string, args: string[], options: Parameters<ReplayVerifyProcessFactory>[2] }> = []
  const child = Object.assign(new EventEmitter(), {
    stdin,
    stdout: PassThrough.from([stdout]),
    stderr: PassThrough.from([stderr]),
    killCalls: [] as NodeJS.Signals[],
    kill(value: NodeJS.Signals) { this.killCalls.push(value); return true },
  })
  const factory: ReplayVerifyProcessFactory = (file, args, options) => {
    calls.push({ file, args, options })
    setImmediate(() => child.emit('close', code, signal))
    return child as never
  }
  return { factory, stdin, child, calls }
}

async function rejectsPublicly(action: () => Promise<unknown>): Promise<void> {
  await assert.rejects(action, (error: unknown) => error instanceof InvalidPlayerSnapshotV1ReplayVerificationError
    && error.message === 'invalid player snapshot replay verification')
}

function pendingProcess(kill: (signal: NodeJS.Signals) => boolean = () => true) {
  const stdin = new PassThrough()
  const child = Object.assign(new EventEmitter(), {
    stdin,
    stdout: new PassThrough(),
    stderr: new PassThrough(),
    killCalls: [] as NodeJS.Signals[],
    kill(signal: NodeJS.Signals) { this.killCalls.push(signal); return kill(signal) },
  })
  return {
    child,
    factory: (() => child as never) as ReplayVerifyProcessFactory,
  }
}

test('replay verifier fails closed with one public error when the runner is not configured', async () => {
  await assert.rejects(
    () => verifyPlayerSnapshotV1Replay(payload),
    (error: unknown) => error instanceof InvalidPlayerSnapshotV1ReplayVerificationError
      && error.message === 'invalid player snapshot replay verification',
  )
})

test('replay verifier accepts only an absolute runner path', async () => {
  await rejectsPublicly(() => verifyPlayerSnapshotV1Replay(payload, { runnerPath: 'player_snapshot_v1_replay_verify' }))
})

test('replay verifier passes only payload bytes to an absolute shell-free runner and returns metadata', async () => {
  const fake = fakeProcess(report())
  const result = await verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify',
    processFactory: fake.factory,
  })
  const received: Buffer[] = []
  fake.stdin.on('data', (chunk: Buffer) => received.push(chunk))
  await new Promise<void>((resolve) => fake.stdin.on('end', resolve))
  assert.deepEqual(Buffer.concat(received), payload)
  assert.deepEqual(fake.calls, [{
    file: '/opt/muhan/player_snapshot_v1_replay_verify',
    args: [],
    options: { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] },
  }])
  assert.deepEqual(result, {
    format: 'player-snapshot-v1-replay-verification',
    version: '1',
    algorithm: 'sha-256',
    inputDigest: createHash('sha256').update(payload).digest('hex'),
    canonicalDigest: createHash('sha256').update(payload).digest('hex'),
    canonicalOctets: payload.length,
    inventoryNodeCount: 0,
  })
})

test('replay verifier maps execution and report failures to the same public error', async () => {
  const malformed = report().replace('algorithm=sha-256', 'algorithm=SHA-256')
  const cases: Array<{ stdout: Uint8Array | string, stderr?: Uint8Array | string, code?: number | null, signal?: NodeJS.Signals | null }> = [
    { stdout: malformed },
    { stdout: report().replace('version=1', 'version=2') },
    { stdout: report().replace(/input_digest=[0-9a-f]{64}/, `input_digest=${'f'.repeat(64)}`) },
    { stdout: report().replace(`canonical_octets=${payload.length}`, `canonical_octets=${payload.length + 1}`) },
    { stdout: report().replace('version=1\nalgorithm=sha-256', 'algorithm=sha-256\nversion=1') },
    { stdout: Buffer.from([0xc3, 0x28]) },
    { stdout: report(), stderr: 'runner diagnostic' },
    { stdout: report(), code: 1 },
    { stdout: report(), signal: 'SIGTERM' },
    { stdout: 'x'.repeat(2_048) },
    { stdout: report(), stderr: 'x'.repeat(2_048) },
    { stdout: 'x'.repeat(700), stderr: 'x'.repeat(700) },
  ]
  for (const value of cases) {
    const fake = fakeProcess(value.stdout, value.stderr, value.code, value.signal)
    await rejectsPublicly(() => verifyPlayerSnapshotV1Replay(payload, {
      runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: fake.factory,
    }))
  }
})

test('replay verifier maps spawn exceptions to the public error', async () => {
  const spawnFailure: ReplayVerifyProcessFactory = () => { throw new Error('untrusted runner detail') }
  await rejectsPublicly(() => verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: spawnFailure,
  }))

})

test('replay verifier waits for close after a timeout before returning its public error', async () => {
  const fake = pendingProcess()
  let settled = false
  const verification = verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', timeoutMs: 1, processFactory: fake.factory,
  })
  void verification.then(() => { settled = true }, () => { settled = true })

  await new Promise((resolve) => setTimeout(resolve, 10))
  assert.deepEqual(fake.child.killCalls, ['SIGKILL'])
  assert.equal(settled, false)
  fake.child.emit('close', null, 'SIGKILL')
  await rejectsPublicly(() => verification)
})

test('replay verifier terminates and waits for close after each process or stream error', async () => {
  for (const source of ['process', 'stdin', 'stdout', 'stderr'] as const) {
    const fake = pendingProcess()
    let settled = false
    const verification = verifyPlayerSnapshotV1Replay(payload, {
      runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: fake.factory,
    })
    void verification.then(() => { settled = true }, () => { settled = true })

    if (source === 'process') fake.child.emit('error', new Error('spawn failed after start'))
    else fake.child[source].emit('error', new Error(`${source} failed`))
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.deepEqual(fake.child.killCalls, ['SIGKILL'], source)
    assert.equal(settled, false, source)
    fake.child.emit('close', null, 'SIGKILL')
    await rejectsPublicly(() => verification)
  }
})

test('replay verifier preserves its public error when kill returns false or throws', async () => {
  for (const kill of [() => false, () => { throw new Error('kill failed') }]) {
    const fake = pendingProcess(kill)
    let settled = false
    const verification = verifyPlayerSnapshotV1Replay(payload, {
      runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: fake.factory,
    })
    void verification.then(() => { settled = true }, () => { settled = true })
    fake.child.emit('error', new Error('runner failed'))
    assert.deepEqual(fake.child.killCalls, ['SIGKILL'])
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(settled, false)
    fake.child.emit('close', null, 'SIGKILL')
    await rejectsPublicly(() => verification)
  }
})

test('replay verifier settles its public error when close races synchronously with kill', async () => {
  const stdin = new PassThrough()
  const child = Object.assign(new EventEmitter(), {
    stdin,
    stdout: new PassThrough(),
    stderr: new PassThrough(),
    killCalls: [] as NodeJS.Signals[],
    kill(signal: NodeJS.Signals) {
      this.killCalls.push(signal)
      this.emit('close', null, signal)
      return true
    },
  })
  const verification = verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify',
    processFactory: (() => child as never) as ReplayVerifyProcessFactory,
  })
  child.emit('error', new Error('runner failed'))
  await rejectsPublicly(() => verification)
  assert.deepEqual(child.killCalls, ['SIGKILL'])
})

test('replay verifier enforces the 1,024 byte combined output boundary', async () => {
  const atLimit = pendingProcess()
  const atLimitVerification = verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: atLimit.factory,
  })
  atLimit.child.stdout.write('x'.repeat(512))
  atLimit.child.stderr.write('x'.repeat(512))
  assert.deepEqual(atLimit.child.killCalls, [])
  atLimit.child.emit('close', 0, null)
  await rejectsPublicly(() => atLimitVerification)

  const overLimit = pendingProcess()
  const overLimitVerification = verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: overLimit.factory,
  })
  overLimit.child.stdout.write('x'.repeat(512))
  overLimit.child.stderr.write('x'.repeat(513))
  assert.deepEqual(overLimit.child.killCalls, ['SIGKILL'])
  overLimit.child.emit('close', null, 'SIGKILL')
  await rejectsPublicly(() => overLimitVerification)
})

test('replay verifier keeps child and stream error listeners harmless after close', async () => {
  const success = fakeProcess(report())
  await verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: success.factory,
  })
  assert.doesNotThrow(() => {
    success.child.emit('error', new Error('late child error'))
    success.child.stdin.emit('error', new Error('late stdin error'))
    success.child.stdout.emit('error', new Error('late stdout error'))
    success.child.stderr.emit('error', new Error('late stderr error'))
  })

  const failure = pendingProcess()
  const verification = verifyPlayerSnapshotV1Replay(payload, {
    runnerPath: '/opt/muhan/player_snapshot_v1_replay_verify', processFactory: failure.factory,
  })
  failure.child.emit('error', new Error('initial error'))
  failure.child.emit('close', null, 'SIGKILL')
  await rejectsPublicly(() => verification)
  assert.doesNotThrow(() => {
    failure.child.emit('error', new Error('late child error'))
    failure.child.stdin.emit('error', new Error('late stdin error'))
    failure.child.stdout.emit('error', new Error('late stdout error'))
    failure.child.stderr.emit('error', new Error('late stderr error'))
  })
})
