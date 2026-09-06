import { parseManifest } from './manifest.js'
import { parseBankSnapshotV1Artifact, type BankSnapshotV1Artifact } from './bank-snapshot-v1-artifact.js'
import { classifyDatabaseError, type BankSnapshotV1RootValueShadowInput, type BankSnapshotV1RootValueShadowStore } from './store.js'
import type { BankSnapshotV1ArtifactFilesystem, BankSnapshotV1ArtifactRelaySummary } from './bank-snapshot-v1-artifact-relay.js'

function rootValueInput(artifact: BankSnapshotV1Artifact): BankSnapshotV1RootValueShadowInput {
  return {
    characterId: artifact.characterId, commandId: artifact.commandId,
    receiptRequestSha256: artifact.receiptRequestSha256, sourcePostSha256: artifact.sourcePostSha256,
    sourceOctets: artifact.sourceOctets, bankSha256: artifact.bankSha256,
    bankOctets: artifact.bankOctets, rootValue: artifact.rootValue,
  }
}

/**
 * Default-off projection: parsing settles canonical signed BankSnapshotV1
 * artifact and matching M4 manifest evidence before its one narrow writer is
 * called. It has no dependency on the normal manifest relay or topology flow.
 */
export async function relayBankSnapshotV1RootValueShadowsOnce(
  path: string, store: BankSnapshotV1RootValueShadowStore, fs: BankSnapshotV1ArtifactFilesystem,
): Promise<BankSnapshotV1ArtifactRelaySummary> {
  const r = { visited: 0, valid: 0, delivered: 0, recorded: 0, exactRetry: 0, invalid: 0, conflict: 0, retryable: 0, unknown: 0, ioError: 0 }
  let files
  try { files = await fs.scan(path) } catch { r.ioError++; return r }
  r.visited = files.length
  for (const file of [...files].sort((a, b) => Buffer.compare(Buffer.from(a.name), Buffer.from(b.name)))) {
    if (file.error === 'invalid') { r.invalid++; continue }
    if (file.error === 'io' || !file.bytes || !file.receiptManifestBytes) { r.ioError++; continue }
    let input: BankSnapshotV1RootValueShadowInput
    try {
      input = rootValueInput(parseBankSnapshotV1Artifact(file.name, file.bytes, parseManifest(file.receiptManifestBytes)))
    } catch { r.invalid++; continue }
    r.valid++
    try {
      const outcome = await store.recordBankSnapshotV1RootValueShadow(input)
      if (outcome === 'RECORDED') { r.recorded++; r.delivered++ }
      else if (outcome === 'EXACT_RETRY') { r.exactRetry++; r.delivered++ }
      else r.unknown++
    } catch (error) { r[classifyDatabaseError(error)]++ }
  }
  return r
}
