import {
  readPlayerSnapshotV1ReplayLevelDifferentialInputs,
  type PlayerSnapshotV1ReplayDifferentialJournalFileReader,
  type PlayerSnapshotV1ReplayLevelDifferentialInput,
} from './player-snapshot-v1-replay-differential.js'
import {
  comparePlayerSnapshotV2JournalLevel,
  type ImmutablePlayerSnapshotLevelProjectionReader,
  type PlayerSnapshotV2JournalLevelComparison,
} from './player-snapshot-v1-level-comparator.js'

export type PlayerSnapshotV2JournalLevelShadowComparisonRecordClassification =
  | 'JOURNAL_INVALID'
  | 'JOURNAL_BOUND_EXCEEDED'
  | PlayerSnapshotV2JournalLevelComparison

export interface PlayerSnapshotV2JournalLevelShadowComparisonRecord {
  index: number
  classification: PlayerSnapshotV2JournalLevelShadowComparisonRecordClassification
  input?: PlayerSnapshotV1ReplayLevelDifferentialInput
}

export interface PlayerSnapshotV2JournalLevelShadowComparisonResult {
  format: 'player-snapshot-v2-journal-level-shadow-comparison'
  version: '1'
  classification: 'MATCH' | 'INCONSISTENT'
  records: readonly PlayerSnapshotV2JournalLevelShadowComparisonRecord[]
}

function closedInput(input: PlayerSnapshotV1ReplayLevelDifferentialInput): PlayerSnapshotV1ReplayLevelDifferentialInput {
  return {
    commandId: input.commandId,
    characterId: input.characterId,
    receiptRequestSha256: input.receiptRequestSha256,
    sourcePostSha256: input.sourcePostSha256,
    snapshotSha256: input.snapshotSha256,
    snapshotOctets: input.snapshotOctets,
    rawLevelU8: input.rawLevelU8,
  }
}

/**
 * Explicit shadow-only composition of the v2 journal input boundary and pure
 * level comparator.  It returns only fixed metadata: no journal paths,
 * payloads, legacy files, or reader errors can escape this boundary.
 */
export async function comparePlayerSnapshotV2JournalLevelShadowJournal(
  journalDirectory: string,
  reader: ImmutablePlayerSnapshotLevelProjectionReader,
  journalFiles?: PlayerSnapshotV1ReplayDifferentialJournalFileReader,
): Promise<PlayerSnapshotV2JournalLevelShadowComparisonResult> {
  const journal = await readPlayerSnapshotV1ReplayLevelDifferentialInputs(journalDirectory, journalFiles)
  const records: PlayerSnapshotV2JournalLevelShadowComparisonRecord[] = []
  for (const record of journal.records) {
    if (record.classification !== 'INPUT') {
      records.push({ index: record.index, classification: record.classification })
      continue
    }
    const input = closedInput(record.input!)
    records.push({
      index: record.index,
      classification: await comparePlayerSnapshotV2JournalLevel(input, reader),
      input,
    })
  }
  return {
    format: 'player-snapshot-v2-journal-level-shadow-comparison',
    version: '1',
    // An empty directory provides no evidence to compare, so it cannot prove
    // a match.  Keep the stable empty record list, but fail the one-shot
    // command closed rather than treating a vacuous match as success.
    classification: records.length > 0 && records.every((record) => record.classification === 'MATCH')
      ? 'MATCH'
      : 'INCONSISTENT',
    records,
  }
}
