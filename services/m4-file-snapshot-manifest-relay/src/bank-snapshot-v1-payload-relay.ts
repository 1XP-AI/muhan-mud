import { parseManifest } from './manifest.js'
import { parseBankSnapshotV1Artifact, type BankSnapshotV1Artifact } from './bank-snapshot-v1-artifact.js'
import { classifyDatabaseError, type StoreOutcome } from './store.js'
import type { BankSnapshotV1ArtifactFilesystem, BankSnapshotV1ArtifactRelaySummary } from './bank-snapshot-v1-artifact-relay.js'

export interface BankSnapshotV1PayloadArtifact extends BankSnapshotV1Artifact { payload: Uint8Array }
export interface BankSnapshotV1PayloadStore {
  recordBankSnapshotV1Payload(artifact: BankSnapshotV1PayloadArtifact): Promise<StoreOutcome>
  close?(): Promise<void>
}

/** Explicit payload lane; the existing metadata-only relay is unchanged. */
export async function relayBankSnapshotV1PayloadsOnce(
  path: string, store: BankSnapshotV1PayloadStore, fs: BankSnapshotV1ArtifactFilesystem,
): Promise<BankSnapshotV1ArtifactRelaySummary> {
  const result = { visited:0, valid:0, delivered:0, recorded:0, exactRetry:0, invalid:0, conflict:0, retryable:0, unknown:0, ioError:0 }
  let files
  try { files = await fs.scan(path) } catch { result.ioError++; return result }
  result.visited = files.length
  for (const file of [...files].sort((a,b) => Buffer.compare(Buffer.from(a.name),Buffer.from(b.name)))) {
    if (file.error === 'invalid') { result.invalid++; continue }
    if (file.error === 'io' || !file.bytes || !file.receiptManifestBytes) { result.ioError++; continue }
    let parsed: BankSnapshotV1PayloadArtifact
    try {
      // Own the buffer before validation; never give the DB a mutable scanner view.
      const bytes = Buffer.from(file.bytes)
      const artifact = parseBankSnapshotV1Artifact(file.name,bytes,parseManifest(file.receiptManifestBytes))
      const payload = Buffer.from(bytes.subarray(bytes.indexOf('\n\n')+2))
      parsed = {...artifact,payload}
    } catch { result.invalid++; continue }
    result.valid++
    try {
      const outcome = await store.recordBankSnapshotV1Payload(parsed)
      if (outcome === 'RECORDED') { result.recorded++; result.delivered++ }
      else if (outcome === 'EXACT_RETRY') { result.exactRetry++; result.delivered++ }
      else result.unknown++
    } catch (error) { result[classifyDatabaseError(error)]++ }
  }
  return result
}
