import { createHash } from 'node:crypto'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { isAbsolute } from 'node:path'
import type { Readable, Writable } from 'node:stream'

const DEFAULT_TIMEOUT_MS = 5_000
const MAX_OUTPUT_BYTES = 1_024
const REPORT_FORMAT = 'player-snapshot-v1-replay-verification' as const
const REPORT_VERSION = '1' as const
const REPORT_ALGORITHM = 'sha-256' as const
const LOWER_HEX_64 = /^[0-9a-f]{64}$/
const NON_NEGATIVE_INTEGER = /^(?:0|[1-9][0-9]{0,9})$/

export interface PlayerSnapshotV1ReplayVerification {
  format: typeof REPORT_FORMAT
  version: typeof REPORT_VERSION
  algorithm: typeof REPORT_ALGORITHM
  inputDigest: string
  canonicalDigest: string
  canonicalOctets: number
  inventoryNodeCount: number
}

export interface ReplayVerifyProcess {
  stdin: Writable
  stdout: Readable
  stderr: Readable
  kill(signal?: NodeJS.Signals): boolean
  on(event: 'error', listener: (error: Error) => void): this
  on(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): this
  removeListener(event: 'error', listener: (error: Error) => void): this
  removeListener(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): this
}

export type ReplayVerifyProcessFactory = (
  file: string,
  args: string[],
  options: { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] },
) => ReplayVerifyProcess

export interface PlayerSnapshotV1ReplayVerifierOptions {
  runnerPath?: string
  /** A caller may only shorten the five-second safety deadline. */
  timeoutMs?: number
  /** Dependency injection for tests; production uses node:child_process.spawn. */
  processFactory?: ReplayVerifyProcessFactory
}

export class InvalidPlayerSnapshotV1ReplayVerificationError extends Error {
  constructor() { super('invalid player snapshot replay verification') }
}

function invalid(): InvalidPlayerSnapshotV1ReplayVerificationError {
  return new InvalidPlayerSnapshotV1ReplayVerificationError()
}

function defaultProcessFactory(
  file: string,
  args: string[],
  options: { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] },
): ChildProcessWithoutNullStreams {
  return spawn(file, args, options)
}

function decodeUtf8(bytes: Uint8Array): string {
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}

function parseReport(stdout: Uint8Array, payload: Uint8Array): PlayerSnapshotV1ReplayVerification {
  const text = decodeUtf8(stdout)
  if (!text.endsWith('\n') || text.includes('\r')) throw invalid()
  const lines = text.slice(0, -1).split('\n')
  if (lines.length !== 7) throw invalid()
  const expectedPrefixes = [
    'format=', 'version=', 'algorithm=', 'input_digest=', 'canonical_digest=', 'canonical_octets=', 'inventory_node_count=',
  ]
  const values = lines.map((line, index) => {
    const prefix = expectedPrefixes[index]!
    if (!line.startsWith(prefix) || line.indexOf('=', prefix.length) !== -1) throw invalid()
    return line.slice(prefix.length)
  })
  const [format, version, algorithm, inputDigest, canonicalDigest, canonicalOctets, inventoryNodeCount] = values
  if (format !== REPORT_FORMAT || version !== REPORT_VERSION || algorithm !== REPORT_ALGORITHM
    || !LOWER_HEX_64.test(inputDigest!) || !LOWER_HEX_64.test(canonicalDigest!)
    || !NON_NEGATIVE_INTEGER.test(canonicalOctets!) || !NON_NEGATIVE_INTEGER.test(inventoryNodeCount!)) throw invalid()
  const canonicalOctetsNumber = Number(canonicalOctets)
  const inventoryNodeCountNumber = Number(inventoryNodeCount)
  const expectedDigest = createHash('sha256').update(payload).digest('hex')
  if (!Number.isSafeInteger(canonicalOctetsNumber) || !Number.isSafeInteger(inventoryNodeCountNumber)
    || inputDigest !== expectedDigest || canonicalDigest !== expectedDigest || canonicalOctetsNumber !== payload.length) throw invalid()
  return {
    format: REPORT_FORMAT,
    version: REPORT_VERSION,
    algorithm: REPORT_ALGORITHM,
    inputDigest,
    canonicalDigest,
    canonicalOctets: canonicalOctetsNumber,
    inventoryNodeCount: inventoryNodeCountNumber,
  }
}

/**
 * Runs the version-pinned Rust verifier directly. The only data written to
 * its stdin is the native CDTO payload; all process and report failures share
 * one intentionally detail-free public error.
 */
export async function verifyPlayerSnapshotV1Replay(
  payload: Uint8Array,
  options: PlayerSnapshotV1ReplayVerifierOptions = {},
): Promise<PlayerSnapshotV1ReplayVerification> {
  const { runnerPath, processFactory = defaultProcessFactory, timeoutMs = DEFAULT_TIMEOUT_MS } = options
  if (!runnerPath || !isAbsolute(runnerPath) || !Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > DEFAULT_TIMEOUT_MS) throw invalid()
  let child: ReplayVerifyProcess
  try {
    child = processFactory(runnerPath, [], { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] })
  } catch {
    throw invalid()
  }
  return new Promise<PlayerSnapshotV1ReplayVerification>((resolve, reject) => {
    let settled = false
    let failed = false
    let closed = false
    let outputSize = 0
    const stdout: Buffer[] = []
    const stderr: Buffer[] = []
    const cleanupAfterClose = (destroyStreams = false) => {
      clearTimeout(timeout)
      child.removeListener('close', onClose)
      child.stdout.removeListener('data', onStdoutData)
      child.stderr.removeListener('data', onStderrData)
      // Error events can still be delivered after close, so their listeners
      // deliberately remain attached for the lifetime of the child streams.
      if (!destroyStreams) return
      try { child.stdin.destroy() } catch { /* cleanup must remain deterministic */ }
      try { child.stdout.destroy() } catch { /* cleanup must remain deterministic */ }
      try { child.stderr.destroy() } catch { /* cleanup must remain deterministic */ }
    }
    const settleError = () => {
      if (settled) return
      settled = true
      cleanupAfterClose(true)
      reject(invalid())
    }
    const terminate = () => {
      try { child.kill('SIGKILL') } catch { /* the generic error remains the contract */ }
    }
    const fail = () => {
      if (settled || failed) return
      failed = true
      if (closed) { settleError(); return }
      terminate()
    }
    const collect = (target: Buffer[]) => (chunk: Uint8Array) => {
      if (failed || settled) return
      const bytes = Buffer.from(chunk)
      outputSize += bytes.length
      if (outputSize > MAX_OUTPUT_BYTES) { fail(); return }
      target.push(bytes)
    }
    const onProcessError = () => fail()
    const onStdinError = () => fail()
    const onStdoutError = () => fail()
    const onStderrError = () => fail()
    const onStdoutData = collect(stdout)
    const onStderrData = collect(stderr)
    const onClose = (code: number | null, signal: NodeJS.Signals | null) => {
      closed = true
      if (settled) return
      if (failed) { settleError(); return }
      if (code !== 0 || signal !== null) { fail(); return }
      try {
        const stderrText = decodeUtf8(Buffer.concat(stderr))
        if (stderrText.length !== 0) { fail(); return }
        const result = parseReport(Buffer.concat(stdout), payload)
        settled = true
        cleanupAfterClose()
        resolve(result)
      } catch { fail() }
    }
    const timeout = setTimeout(fail, timeoutMs)
    child.on('error', onProcessError)
    child.stdout.on('error', onStdoutError)
    child.stderr.on('error', onStderrError)
    child.stdin.on('error', onStdinError)
    child.stdout.on('data', onStdoutData)
    child.stderr.on('data', onStderrData)
    child.on('close', onClose)
    try { child.stdin.end(Buffer.from(payload)) } catch { fail() }
  })
}
