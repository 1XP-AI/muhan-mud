import { createHash } from 'node:crypto'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { isAbsolute } from 'node:path'
import type { Readable, Writable } from 'node:stream'

const DEFAULT_TIMEOUT_MS = 5_000
const MAX_OUTPUT_BYTES = 1_024
const REPORT_FORMAT = 'player-snapshot-v1-replay-verification' as const
const REPORT_VERSION = '2' as const
const REPORT_ALGORITHM = 'sha-256' as const
const LOWER_HEX_64 = /^[0-9a-f]{64}$/
const NON_NEGATIVE_INTEGER = /^(?:0|[1-9][0-9]{0,9})$/

/** Closed metadata emitted only by the explicitly named v2 runner. */
export interface PlayerSnapshotV2ReplayVerification {
  format: typeof REPORT_FORMAT
  version: typeof REPORT_VERSION
  algorithm: typeof REPORT_ALGORITHM
  inputDigest: string
  canonicalDigest: string
  canonicalOctets: number
  inventoryNodeCount: number
  rawLevelU8: number
}

export interface PlayerSnapshotV2ReplayProcess {
  stdin: Writable
  stdout: Readable
  stderr: Readable
  kill(signal?: NodeJS.Signals): boolean
  on(event: 'error', listener: (error: Error) => void): this
  on(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): this
  removeListener(event: 'error', listener: (error: Error) => void): this
  removeListener(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): this
}

export type PlayerSnapshotV2ReplayProcessFactory = (
  file: string,
  args: string[],
  options: { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] },
) => PlayerSnapshotV2ReplayProcess

export interface PlayerSnapshotV2ReplayVerifierOptions {
  runnerPath?: string
  timeoutMs?: number
  processFactory?: PlayerSnapshotV2ReplayProcessFactory
}

export class InvalidPlayerSnapshotV2ReplayVerificationError extends Error {
  constructor() { super('invalid player snapshot v2 replay verification') }
}

function invalid(): InvalidPlayerSnapshotV2ReplayVerificationError {
  return new InvalidPlayerSnapshotV2ReplayVerificationError()
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

function parseReport(stdout: Uint8Array, payload: Uint8Array): PlayerSnapshotV2ReplayVerification {
  const text = decodeUtf8(stdout)
  if (!text.endsWith('\n') || text.includes('\r')) throw invalid()
  const lines = text.slice(0, -1).split('\n')
  if (lines.length !== 8) throw invalid()
  const prefixes = [
    'format=', 'version=', 'algorithm=', 'input_digest=', 'canonical_digest=', 'canonical_octets=', 'inventory_node_count=', 'raw_level_u8=',
  ]
  const values = lines.map((line, index) => {
    const prefix = prefixes[index]!
    if (!line.startsWith(prefix) || line.indexOf('=', prefix.length) !== -1) throw invalid()
    return line.slice(prefix.length)
  })
  const [format, version, algorithm, inputDigest, canonicalDigest, canonicalOctets, inventoryNodeCount, rawLevelU8] = values
  if (format !== REPORT_FORMAT || version !== REPORT_VERSION || algorithm !== REPORT_ALGORITHM
    || !LOWER_HEX_64.test(inputDigest!) || !LOWER_HEX_64.test(canonicalDigest!)
    || !NON_NEGATIVE_INTEGER.test(canonicalOctets!) || !NON_NEGATIVE_INTEGER.test(inventoryNodeCount!)
    || !NON_NEGATIVE_INTEGER.test(rawLevelU8!)) throw invalid()
  const canonicalOctetsNumber = Number(canonicalOctets)
  const inventoryNodeCountNumber = Number(inventoryNodeCount)
  const rawLevelU8Number = Number(rawLevelU8)
  const expectedDigest = createHash('sha256').update(payload).digest('hex')
  if (!Number.isSafeInteger(canonicalOctetsNumber) || !Number.isSafeInteger(inventoryNodeCountNumber)
    || !Number.isSafeInteger(rawLevelU8Number) || rawLevelU8Number > 255
    || inputDigest !== expectedDigest || canonicalDigest !== expectedDigest || canonicalOctetsNumber !== payload.length) throw invalid()
  return {
    format: REPORT_FORMAT, version: REPORT_VERSION, algorithm: REPORT_ALGORITHM,
    inputDigest, canonicalDigest, canonicalOctets: canonicalOctetsNumber,
    inventoryNodeCount: inventoryNodeCountNumber, rawLevelU8: rawLevelU8Number,
  }
}

/**
 * An explicit, non-default v2 process boundary. Existing relay runtime wiring
 * imports the v1 verifier and therefore cannot select this parser implicitly.
 */
export async function verifyPlayerSnapshotV2Replay(
  payload: Uint8Array,
  options: PlayerSnapshotV2ReplayVerifierOptions = {},
): Promise<PlayerSnapshotV2ReplayVerification> {
  const { runnerPath, processFactory = defaultProcessFactory, timeoutMs = DEFAULT_TIMEOUT_MS } = options
  if (!runnerPath || !isAbsolute(runnerPath) || !Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > DEFAULT_TIMEOUT_MS) throw invalid()
  let child: PlayerSnapshotV2ReplayProcess
  try { child = processFactory(runnerPath, [], { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] }) }
  catch { throw invalid() }
  return new Promise<PlayerSnapshotV2ReplayVerification>((resolve, reject) => {
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
      if (!destroyStreams) return
      try { child.stdin.destroy() } catch { /* deterministic cleanup */ }
      try { child.stdout.destroy() } catch { /* deterministic cleanup */ }
      try { child.stderr.destroy() } catch { /* deterministic cleanup */ }
    }
    const settleError = () => {
      if (settled) return
      settled = true
      cleanupAfterClose(true)
      reject(invalid())
    }
    const terminate = () => { try { child.kill('SIGKILL') } catch { /* one public error */ } }
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
    const onStdoutData = collect(stdout)
    const onStderrData = collect(stderr)
    const onClose = (code: number | null, signal: NodeJS.Signals | null) => {
      closed = true
      if (settled) return
      if (failed || code !== 0 || signal !== null) { fail(); return }
      try {
        if (decodeUtf8(Buffer.concat(stderr)).length !== 0) { fail(); return }
        const result = parseReport(Buffer.concat(stdout), payload)
        settled = true
        cleanupAfterClose()
        resolve(result)
      } catch { fail() }
    }
    const timeout = setTimeout(fail, timeoutMs)
    child.on('error', fail)
    child.stdin.on('error', fail)
    child.stdout.on('error', fail)
    child.stderr.on('error', fail)
    child.stdout.on('data', onStdoutData)
    child.stderr.on('data', onStderrData)
    child.on('close', onClose)
    try { child.stdin.end(Buffer.from(payload)) } catch { fail() }
  })
}
