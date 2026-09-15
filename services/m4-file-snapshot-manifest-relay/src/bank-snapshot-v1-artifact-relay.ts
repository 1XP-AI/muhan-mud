import { parseManifest } from './manifest.js'
import { classifyDatabaseError, type BankSnapshotV1TopologyShadowStore } from './store.js'
import { BANK_SNAPSHOT_V1_SUFFIX, MAX_BANK_SNAPSHOT_V1_OCTETS, commandFromBankSnapshotV1Filename, parseBankSnapshotV1Artifact } from './bank-snapshot-v1-artifact.js'
import { isManifestFilename, MAX_MANIFEST_BYTES } from './manifest.js'
import { scanImmutableOutboxFilesWithPolicies } from './relay.js'

export interface BankSnapshotV1ArtifactFilesystem { scan(path: string): Promise<ReadonlyArray<{ name: string, bytes?: Uint8Array, receiptManifestBytes?: Uint8Array, error?: 'invalid' | 'io' }>> }
export interface BankSnapshotV1ArtifactRelaySummary { visited:number, valid:number, delivered:number, recorded:number, exactRetry:number, invalid:number, conflict:number, retryable:number, unknown:number, ioError:number }
/** Descriptor-rooted read-only pairing; no legacy file is opened or changed. */
export class NodeBankSnapshotV1ArtifactFilesystem implements BankSnapshotV1ArtifactFilesystem {
  constructor(private readonly platform: NodeJS.Platform = process.platform) {}
  async scan(path: string): Promise<ReadonlyArray<{ name:string, bytes?:Uint8Array, receiptManifestBytes?:Uint8Array, error?:'invalid'|'io' }>> {
    const isBankSnapshotV1Filename = (name: Uint8Array) => Buffer.from(name).toString('utf8').endsWith(BANK_SNAPSHOT_V1_SUFFIX)
    const scanned = await scanImmutableOutboxFilesWithPolicies(path, [
      { isCandidateFilename: isManifestFilename, maximumBytes: MAX_MANIFEST_BYTES },
      { isCandidateFilename: isBankSnapshotV1Filename, maximumBytes: MAX_BANK_SNAPSHOT_V1_OCTETS + 1025 },
    ], this.platform)
    const receipts = scanned.filter((file) => isManifestFilename(Buffer.from(file.name)))
    const banks = scanned.filter((file) => isBankSnapshotV1Filename(Buffer.from(file.name)))
    const byName = new Map(receipts.map((r) => [r.name, r]))
    return banks.map((b) => { const id = commandFromBankSnapshotV1Filename(b.name); const r = id ? byName.get(`${id}.manifest`) : undefined; return b.error ? b : (!r || r.error || !r.bytes ? { name:b.name, bytes:b.bytes, error:r?.error ?? 'invalid' } : { name:b.name, bytes:b.bytes, receiptManifestBytes:r.bytes }) })
  }
}
export async function relayBankSnapshotV1ArtifactsOnce(path: string, store: BankSnapshotV1TopologyShadowStore, fs: BankSnapshotV1ArtifactFilesystem): Promise<BankSnapshotV1ArtifactRelaySummary> {
  const r = { visited:0, valid:0, delivered:0, recorded:0, exactRetry:0, invalid:0, conflict:0, retryable:0, unknown:0, ioError:0 }
  let files; try { files = await fs.scan(path) } catch { r.ioError++; return r }; r.visited = files.length
  for (const f of [...files].sort((a,b) => Buffer.compare(Buffer.from(a.name), Buffer.from(b.name)))) {
    if (f.error === 'invalid') { r.invalid++; continue }; if (f.error === 'io' || !f.bytes || !f.receiptManifestBytes) { r.ioError++; continue }
    try { const a = parseBankSnapshotV1Artifact(f.name, f.bytes, parseManifest(f.receiptManifestBytes)); r.valid++; const o = await store.recordBankSnapshotV1TopologyShadow(a); if (o === 'RECORDED') { r.recorded++; r.delivered++ } else if (o === 'EXACT_RETRY') { r.exactRetry++; r.delivered++ } else r.unknown++ } catch (e) { r[classifyDatabaseError(e)]++ }
  }; return r
}
