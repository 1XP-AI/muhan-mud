import { createHash } from 'node:crypto'
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { isAbsolute } from 'node:path'
import type { Readable, Writable } from 'node:stream'

const DEFAULT_TIMEOUT_MS = 5_000
/** Enough for the largest closed 8,192-node projection, never unbounded runner output. */
const MAX_OUTPUT_BYTES = 4_194_352
const FORMAT = 'player-snapshot-v1-normalized-projection' as const
const VERSION = 1 as const
const ALGORITHM = 'sha-256' as const
const LOWER_HEX_64 = /^[0-9a-f]{64}$/
const INTEGER = /^(?:0|-?[1-9][0-9]*)$/
const MAX_I8 = 127n
const MIN_I8 = -128n
const MAX_I16 = 32_767n
const MIN_I16 = -32_768n
const MAX_I64 = 9_223_372_036_854_775_807n
const MIN_I64 = -9_223_372_036_854_775_808n
const MAX_U8 = 255n
const MAX_U32 = 4_294_967_295n
const MAX_ITEMS = 8_192
const MAX_DEPTH = 64
const MAX_LIST_ITEMS = 4_096

export interface PlayerSnapshotV1NormalizedDaily {
  max: number
  current: number
  lastUsed: bigint
}

export interface PlayerSnapshotV1NormalizedTimer {
  interval: bigint
  lastUsed: bigint
  misc: number
}

/** Numeric-only object graph node: no strings, keys, raw bytes, or flags cross this boundary. */
export interface PlayerSnapshotV1NormalizedItem {
  parentIndex: number | null
  childIndex: number
  value: bigint
  weight: number
  typeCode: number
  adjustment: number
  shotsMax: number
  shotsCurrent: number
  ndice: number
  sdice: number
  pdice: number
  armor: number
  wearFlag: number
  magicPower: number
  magicRealm: number
  special: number
}

export interface PlayerSnapshotV1NormalizedProjection {
  format: typeof FORMAT
  version: typeof VERSION
  algorithm: typeof ALGORITHM
  canonicalDigest: string
  player: {
    level: number
    hpMax: number
    hpCurrent: number
    mpMax: number
    mpCurrent: number
    experience: bigint
    gold: bigint
    daily: readonly PlayerSnapshotV1NormalizedDaily[]
    timers: readonly PlayerSnapshotV1NormalizedTimer[]
    items: readonly PlayerSnapshotV1NormalizedItem[]
  }
}

export interface PlayerSnapshotV1NormalizedProjectionProcess {
  stdin: Writable
  stdout: Readable
  stderr: Readable
  kill(signal?: NodeJS.Signals): boolean
  on(event: 'error', listener: (error: Error) => void): this
  on(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): this
  removeListener(event: 'error', listener: (error: Error) => void): this
  removeListener(event: 'close', listener: (code: number | null, signal: NodeJS.Signals | null) => void): this
}

export type PlayerSnapshotV1NormalizedProjectionProcessFactory = (
  file: string,
  args: string[],
  options: { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] },
) => PlayerSnapshotV1NormalizedProjectionProcess

export interface PlayerSnapshotV1NormalizedProjectionOptions {
  /** Pinned deployment configuration, never inferred from PATH or the artifact. */
  runnerPath?: string
  /** Digest from the already parsed immutable artifact header. */
  snapshotSha256?: string
  /** A caller may only reduce the fixed safety deadline. */
  timeoutMs?: number
  /** Test-only dependency injection; production uses child_process.spawn. */
  processFactory?: PlayerSnapshotV1NormalizedProjectionProcessFactory
}

export class InvalidPlayerSnapshotV1NormalizedProjectionError extends Error {
  constructor() { super('invalid player snapshot normalized projection') }
}

function invalid(): InvalidPlayerSnapshotV1NormalizedProjectionError {
  return new InvalidPlayerSnapshotV1NormalizedProjectionError()
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

type JsonValue = string | null | JsonNumber | JsonValue[] | { [key: string]: JsonValue }
interface JsonNumber { readonly token: string }

/** A small JSON parser that retains integer tokens, avoiding i64 precision loss in JSON.parse. */
class JsonParser {
  private offset = 0
  constructor(private readonly text: string) {}

  parse(): JsonValue {
    const value = this.value()
    this.whitespace()
    if (this.offset !== this.text.length) throw invalid()
    return value
  }

  private value(): JsonValue {
    this.whitespace()
    const char = this.text[this.offset]
    if (char === '{') return this.object()
    if (char === '[') return this.array()
    if (char === '"') return this.string()
    if (char === 'n' && this.take('null')) return null
    if (char === '-' || (char !== undefined && char >= '0' && char <= '9')) return this.number()
    throw invalid()
  }

  private object(): { [key: string]: JsonValue } {
    this.offset++
    this.whitespace()
    const output: { [key: string]: JsonValue } = Object.create(null) as { [key: string]: JsonValue }
    if (this.text[this.offset] === '}') { this.offset++; return output }
    while (true) {
      this.whitespace()
      if (this.text[this.offset] !== '"') throw invalid()
      const key = this.string()
      if (Object.hasOwn(output, key)) throw invalid()
      this.whitespace()
      if (this.text[this.offset++] !== ':') throw invalid()
      output[key] = this.value()
      this.whitespace()
      const delimiter = this.text[this.offset++]
      if (delimiter === '}') return output
      if (delimiter !== ',') throw invalid()
    }
  }

  private array(): JsonValue[] {
    this.offset++
    this.whitespace()
    const output: JsonValue[] = []
    if (this.text[this.offset] === ']') { this.offset++; return output }
    while (true) {
      output.push(this.value())
      this.whitespace()
      const delimiter = this.text[this.offset++]
      if (delimiter === ']') return output
      if (delimiter !== ',') throw invalid()
    }
  }

  private string(): string {
    if (this.text[this.offset++] !== '"') throw invalid()
    let value = ''
    while (this.offset < this.text.length) {
      const char = this.text[this.offset++]!
      if (char === '"') return value
      if (char < ' ') throw invalid()
      if (char !== '\\') { value += char; continue }
      const escaped = this.text[this.offset++]
      if (escaped === '"' || escaped === '\\' || escaped === '/') { value += escaped; continue }
      if (escaped === 'b') { value += '\b'; continue }
      if (escaped === 'f') { value += '\f'; continue }
      if (escaped === 'n') { value += '\n'; continue }
      if (escaped === 'r') { value += '\r'; continue }
      if (escaped === 't') { value += '\t'; continue }
      if (escaped !== 'u') throw invalid()
      const hex = this.text.slice(this.offset, this.offset + 4)
      if (!/^[0-9a-fA-F]{4}$/.test(hex)) throw invalid()
      this.offset += 4
      value += String.fromCharCode(Number.parseInt(hex, 16))
    }
    throw invalid()
  }

  private number(): JsonNumber {
    const start = this.offset
    if (this.text[this.offset] === '-') this.offset++
    if (this.text[this.offset] === '0') this.offset++
    else {
      if (!this.digit(this.text[this.offset])) throw invalid()
      while (this.digit(this.text[this.offset])) this.offset++
    }
    if (this.text[this.offset] === '.') {
      this.offset++
      if (!this.digit(this.text[this.offset])) throw invalid()
      while (this.digit(this.text[this.offset])) this.offset++
    }
    if (this.text[this.offset] === 'e' || this.text[this.offset] === 'E') {
      this.offset++
      if (this.text[this.offset] === '+' || this.text[this.offset] === '-') this.offset++
      if (!this.digit(this.text[this.offset])) throw invalid()
      while (this.digit(this.text[this.offset])) this.offset++
    }
    return { token: this.text.slice(start, this.offset) }
  }

  private take(value: string): boolean {
    if (!this.text.startsWith(value, this.offset)) return false
    this.offset += value.length
    return true
  }

  private whitespace(): void {
    while (this.text[this.offset] === ' ' || this.text[this.offset] === '\t' || this.text[this.offset] === '\n') this.offset++
  }

  private digit(value: string | undefined): boolean { return value !== undefined && value >= '0' && value <= '9' }
}

function closedObject(value: JsonValue, fields: readonly string[]): { [key: string]: JsonValue } {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw invalid()
  const keys = Object.keys(value).sort()
  if (keys.length !== fields.length || !keys.every((key, index) => key === fields[index])) throw invalid()
  return value as { [key: string]: JsonValue }
}

function isJsonNumber(value: JsonValue): value is JsonNumber {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    && Object.keys(value).length === 1 && typeof (value as { token?: unknown }).token === 'string'
}

function number(value: JsonValue, lower: bigint, upper: bigint): bigint {
  if (!isJsonNumber(value) || !INTEGER.test(value.token)) throw invalid()
  const parsed = BigInt(value.token)
  if (parsed < lower || parsed > upper) throw invalid()
  return parsed
}

function nativeNumber(value: JsonValue, lower: bigint, upper: bigint): number {
  return Number(number(value, lower, upper))
}

function string(value: JsonValue): string {
  if (typeof value !== 'string') throw invalid()
  return value
}

function asArray(value: JsonValue, expectedLength: number): JsonValue[] {
  if (!Array.isArray(value) || value.length !== expectedLength) throw invalid()
  return value
}

function parseDaily(value: JsonValue): PlayerSnapshotV1NormalizedDaily {
  const daily = closedObject(value, ['current', 'last_used', 'max'])
  const max = nativeNumber(daily.max!, 0n, MAX_U8)
  const current = nativeNumber(daily.current!, 0n, MAX_U8)
  if (current > max) throw invalid()
  return { max, current, lastUsed: number(daily.last_used!, MIN_I64, MAX_I64) }
}

function parseTimer(value: JsonValue): PlayerSnapshotV1NormalizedTimer {
  const timer = closedObject(value, ['interval', 'last_used', 'misc'])
  return {
    interval: number(timer.interval!, MIN_I64, MAX_I64), lastUsed: number(timer.last_used!, MIN_I64, MAX_I64),
    misc: nativeNumber(timer.misc!, MIN_I16, MAX_I16),
  }
}

function parseItem(value: JsonValue): PlayerSnapshotV1NormalizedItem {
  const item = closedObject(value, [
    'adjustment', 'armor', 'child_index', 'magic_power', 'magic_realm', 'ndice', 'parent_index', 'pdice',
    'sdice', 'shots_current', 'shots_max', 'special', 'type_code', 'value', 'wear_flag', 'weight',
  ])
  const parentIndex = item.parent_index === null ? null : nativeNumber(item.parent_index!, 0n, MAX_U32)
  const shotsMax = nativeNumber(item.shots_max!, MIN_I16, MAX_I16)
  const shotsCurrent = nativeNumber(item.shots_current!, MIN_I16, MAX_I16)
  if (shotsCurrent > shotsMax) throw invalid()
  return {
    parentIndex, childIndex: nativeNumber(item.child_index!, 0n, MAX_U32), value: number(item.value!, MIN_I64, MAX_I64),
    weight: nativeNumber(item.weight!, MIN_I16, MAX_I16), typeCode: nativeNumber(item.type_code!, MIN_I8, MAX_I8),
    adjustment: nativeNumber(item.adjustment!, MIN_I8, MAX_I8), shotsMax, shotsCurrent,
    ndice: nativeNumber(item.ndice!, MIN_I16, MAX_I16), sdice: nativeNumber(item.sdice!, MIN_I16, MAX_I16),
    pdice: nativeNumber(item.pdice!, MIN_I16, MAX_I16), armor: nativeNumber(item.armor!, MIN_I8, MAX_I8),
    wearFlag: nativeNumber(item.wear_flag!, MIN_I8, MAX_I8), magicPower: nativeNumber(item.magic_power!, MIN_I8, MAX_I8),
    magicRealm: nativeNumber(item.magic_realm!, MIN_I8, MAX_I8), special: nativeNumber(item.special!, MIN_I16, MAX_I16),
  }
}

function validTopology(items: readonly PlayerSnapshotV1NormalizedItem[]): boolean {
  if (items.length > MAX_ITEMS) return false
  const childCounts = new Uint16Array(items.length)
  const depth = new Uint8Array(items.length)
  const ancestors: number[] = []
  let roots = 0
  for (const [index, item] of items.entries()) {
    let expected: number
    if (item.parentIndex === null) {
      ancestors.length = 0
      expected = roots++
      depth[index] = 1
      if (roots > MAX_LIST_ITEMS) return false
    } else {
      const parent = item.parentIndex
      if (parent >= index) return false
      const ancestor = ancestors.lastIndexOf(parent)
      if (ancestor < 0) return false
      ancestors.length = ancestor + 1
      depth[index] = depth[parent]! + 1
      expected = childCounts[parent]!
      childCounts[parent]++
      if (childCounts[parent]! > MAX_LIST_ITEMS) return false
    }
    if (item.childIndex !== expected || depth[index]! > MAX_DEPTH) return false
    ancestors.push(index)
  }
  return true
}

function parseProjection(stdout: Uint8Array): PlayerSnapshotV1NormalizedProjection {
  const text = decodeUtf8(stdout)
  if (!text.endsWith('\n') || text.includes('\r')) throw invalid()
  const root = closedObject(new JsonParser(text.slice(0, -1)).parse(), ['algorithm', 'canonical_digest', 'format', 'player', 'version'])
  if (string(root.format!) !== FORMAT || nativeNumber(root.version!, 1n, 1n) !== VERSION || string(root.algorithm!) !== ALGORITHM) throw invalid()
  const canonicalDigest = string(root.canonical_digest!)
  if (!LOWER_HEX_64.test(canonicalDigest)) throw invalid()
  const player = closedObject(root.player!, ['daily', 'experience', 'gold', 'hp_current', 'hp_max', 'items', 'level', 'mp_current', 'mp_max', 'timers'])
  const hpMax = nativeNumber(player.hp_max!, MIN_I16, MAX_I16)
  const hpCurrent = nativeNumber(player.hp_current!, MIN_I16, MAX_I16)
  const mpMax = nativeNumber(player.mp_max!, MIN_I16, MAX_I16)
  const mpCurrent = nativeNumber(player.mp_current!, MIN_I16, MAX_I16)
  if (hpCurrent > hpMax || mpCurrent > mpMax) throw invalid()
  const daily = asArray(player.daily!, 10).map(parseDaily)
  const timers = asArray(player.timers!, 45).map(parseTimer)
  if (!Array.isArray(player.items!) || player.items!.length > MAX_ITEMS) throw invalid()
  const items = player.items!.map(parseItem)
  if (!validTopology(items)) throw invalid()
  return {
    format: FORMAT, version: VERSION, algorithm: ALGORITHM, canonicalDigest,
    player: {
      level: nativeNumber(player.level!, 0n, MAX_U8), hpMax, hpCurrent, mpMax, mpCurrent,
      experience: number(player.experience!, MIN_I64, MAX_I64), gold: number(player.gold!, MIN_I64, MAX_I64),
      daily, timers, items,
    },
  }
}

/**
 * Explicit, injectable Node boundary for the committed Rust normalized runner.
 * It sends exactly one immutable artifact over stdin and returns only the fully
 * validated numeric allowlist; every failure intentionally has one public error.
 */
export async function projectPlayerSnapshotV1Normalized(
  payload: Uint8Array,
  options: PlayerSnapshotV1NormalizedProjectionOptions = {},
): Promise<PlayerSnapshotV1NormalizedProjection> {
  const { runnerPath, snapshotSha256, processFactory = defaultProcessFactory, timeoutMs = DEFAULT_TIMEOUT_MS } = options
  if (!runnerPath || !isAbsolute(runnerPath) || runnerPath.includes('\0') || !snapshotSha256 || !LOWER_HEX_64.test(snapshotSha256)
    || createHash('sha256').update(payload).digest('hex') !== snapshotSha256
    || !Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > DEFAULT_TIMEOUT_MS) throw invalid()
  let child: PlayerSnapshotV1NormalizedProjectionProcess
  try { child = processFactory(runnerPath, ['--snapshot-sha256', snapshotSha256], { shell: false, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] }) }
  catch { throw invalid() }
  return new Promise<PlayerSnapshotV1NormalizedProjection>((resolve, reject) => {
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
      try { child.stdin.destroy() } catch { /* public failure remains generic */ }
      try { child.stdout.destroy() } catch { /* public failure remains generic */ }
      try { child.stderr.destroy() } catch { /* public failure remains generic */ }
    }
    const settleError = () => {
      if (settled) return
      settled = true
      cleanupAfterClose(true)
      reject(invalid())
    }
    const terminate = () => { try { child.kill('SIGKILL') } catch { /* public failure remains generic */ } }
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
        if (decodeUtf8(Buffer.concat(stderr)).length !== 0) { fail(); return }
        const projection = parseProjection(Buffer.concat(stdout))
        settled = true
        cleanupAfterClose()
        resolve(projection)
      } catch { fail() }
    }
    const timeout = setTimeout(fail, timeoutMs)
    child.on('error', onProcessError)
    child.stdin.on('error', onStdinError)
    child.stdout.on('error', onStdoutError)
    child.stderr.on('error', onStderrError)
    child.stdout.on('data', onStdoutData)
    child.stderr.on('data', onStderrData)
    child.on('close', onClose)
    try { child.stdin.end(Buffer.from(payload)) } catch { fail() }
  })
}
