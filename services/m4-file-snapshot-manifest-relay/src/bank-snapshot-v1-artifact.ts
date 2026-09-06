import { createHash } from 'node:crypto'
import type { Manifest } from './manifest.js'

/** Detached evidence only: this module is not imported by the default relay. */
export const BANK_SNAPSHOT_V1_FORMAT = 'bank-snapshot-v1' as const
export const BANK_SNAPSHOT_V1_SUFFIX = '.bank-snapshot-v1'
export const MAX_BANK_SNAPSHOT_V1_OCTETS = 4_194_304
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HASH = /^[0-9a-f]{64}$/
const WORLD = /^[a-z][a-z0-9_-]{0,63}$/
const NAME_HEX = /^(?:[0-9a-f]{2}){1,14}$/

export interface BankTopologyNode { nodeIndex: number, parentNodeIndex: number | null, siblingOrdinal: number }
export interface BankSnapshotV1Artifact {
  characterId: string, commandId: string, receiptRequestSha256: string
  sourcePostSha256: string, sourceOctets: string, bankSha256: string
  bankOctets: number, nodes: readonly BankTopologyNode[]
}
export class InvalidBankSnapshotV1ArtifactError extends Error { constructor() { super('invalid BankSnapshotV1 artifact') } }

export function commandFromBankSnapshotV1Filename(name: string): string | undefined {
  const id = name.endsWith(BANK_SNAPSHOT_V1_SUFFIX) ? name.slice(0, -BANK_SNAPSHOT_V1_SUFFIX.length) : ''
  return UUID.test(id) ? id : undefined
}
function u16(b: Uint8Array, p: number) { return b[p]! * 256 + b[p + 1]! }
function u32(b: Uint8Array, p: number) { return b[p]! * 0x1000000 + b[p + 1]! * 0x10000 + b[p + 2]! * 0x100 + b[p + 3]! }
function positive(s: string) { return /^[1-9][0-9]{0,18}$/.test(s) && BigInt(s) <= 9223372036854775807n }

function topology(payload: Uint8Array): readonly BankTopologyNode[] {
  if (payload.length < 48 || payload.length > MAX_BANK_SNAPSHOT_V1_OCTETS || !Buffer.from(payload.subarray(0, 8)).equals(Buffer.from('MUHCDTO\0'))
    || payload[8] !== 0 || payload[9] !== 1 || payload[10] !== 0 || payload[11] !== 8 || u32(payload, 12) + 48 !== payload.length
    || !createHash('sha256').update(payload.subarray(16, payload.length - 32)).digest().equals(payload.subarray(-32))) throw new InvalidBankSnapshotV1ArtifactError()
  const body = payload.subarray(16, payload.length - 32)
  if (body.length < 7 || u16(body, 0) !== 1 || body[2] !== 9 || u32(body, 3) !== body.length - 7) throw new InvalidBankSnapshotV1ArtifactError()
  const graph = body.subarray(7)
  if (graph.length < 48 || !Buffer.from(graph.subarray(0, 8)).equals(Buffer.from('MUHCDTO\0')) || graph[8] !== 0 || graph[9] !== 1 || graph[10] !== 0 || graph[11] !== 6 || u32(graph, 12) + 48 !== graph.length || !createHash('sha256').update(graph.subarray(16, -32)).digest().equals(graph.subarray(-32))) throw new InvalidBankSnapshotV1ArtifactError()
  const g = graph.subarray(16, -32), count = u32(g, 7)
  if (g.length !== 11 + count * 356 || u16(g, 0) !== 1 || g[2] !== 3 || u32(g, 3) !== 4 || count > 8192) throw new InvalidBankSnapshotV1ArtifactError()
  const nodes: BankTopologyNode[] = [], children = new Array<number>(count).fill(0), ancestors: number[] = []
  for (let i = 0, p = 11; i < count; i++, p += 356) {
    if (u16(g, p) !== i + 2 || g[p + 2] !== 9 || u32(g, p + 3) !== 349 || u32(g, p + 7) !== i) throw new InvalidBankSnapshotV1ArtifactError()
    const parentRaw = u32(g, p + 11), sibling = u32(g, p + 15), parent = parentRaw === 0xffffffff ? null : parentRaw
    if (sibling > 8191 || (parent !== null && (parent >= i || !ancestors.includes(parent))) || sibling !== (parent === null ? nodes.filter((n) => n.parentNodeIndex === null).length : children[parent]!)) throw new InvalidBankSnapshotV1ArtifactError()
    if (parent === null) ancestors.length = 0; else { ancestors.length = ancestors.indexOf(parent) + 1; children[parent] = children[parent]! + 1 }
    ancestors.push(i); nodes.push({ nodeIndex: i, parentNodeIndex: parent, siblingOrdinal: sibling })
  }
  return nodes
}

export function parseBankSnapshotV1Artifact(filename: string, bytes: Uint8Array, receipt: Manifest): BankSnapshotV1Artifact {
  const commandId = commandFromBankSnapshotV1Filename(filename)
  const at = Buffer.from(bytes).indexOf('\n\n')
  if (!commandId || at < 0 || at + 2 >= bytes.length || at + 2 > 1024) throw new InvalidBankSnapshotV1ArtifactError()
  const lines = Buffer.from(bytes.subarray(0, at + 2)).toString('ascii').split('\n')
  const names = ['version','artifact_format','world_id','character_id','command_id','canonical_name_hex','request_sha256','source_post_sha256','writer_epoch','writer_revision','bank_sha256','bank_octets']
  if (lines.length !== 14 || lines[12] !== '' || lines[13] !== '') throw new InvalidBankSnapshotV1ArtifactError()
  const v = names.map((n, i) => lines[i]?.startsWith(`${n}=`) ? lines[i]!.slice(n.length + 1) : undefined)
  if (v.some((x) => !x || x!.includes('=')) || v[0] !== '1' || v[1] !== BANK_SNAPSHOT_V1_FORMAT || !WORLD.test(v[2]!) || !UUID.test(v[3]!) || !UUID.test(v[4]!) || !NAME_HEX.test(v[5]!) || !HASH.test(v[6]!) || !HASH.test(v[7]!) || !HASH.test(v[10]!) || !positive(v[8]!) || !positive(v[9]!) || !positive(v[11]!)) throw new InvalidBankSnapshotV1ArtifactError()
  const canonical = names.map((n, i) => `${n}=${v[i]}`).join('\n') + '\n\n'
  const bank = bytes.subarray(at + 2)
  if (!Buffer.from(canonical, 'ascii').equals(bytes.subarray(0, at + 2)) || commandId !== receipt.commandId || v[2] !== receipt.worldId || v[3] !== receipt.characterId || v[4] !== receipt.commandId || v[5] !== receipt.canonicalNameHex || v[6] !== receipt.requestSha256 || v[7] !== receipt.postSha256 || v[8] !== receipt.writerEpoch || v[9] !== receipt.writerRevision || BigInt(v[11]!) !== BigInt(bank.length) || createHash('sha256').update(bank).digest('hex') !== v[10]) throw new InvalidBankSnapshotV1ArtifactError()
  return { characterId: receipt.characterId, commandId, receiptRequestSha256: receipt.requestSha256, sourcePostSha256: receipt.postSha256, sourceOctets: receipt.snapshotOctets, bankSha256: v[10]!, bankOctets: bank.length, nodes: topology(bank) }
}
