import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { LegacyIdentityEvidenceV1Shape } from '../src/legacy-player-shadow-binding.js'
import type { InventoryRecord } from '../src/inventory.js'

const IMPORTER_BINDING_FIXTURE_PATH = fileURLToPath(new URL(
  '../../../tests/fixtures/legacy_identity_evidence_importer_binding_v1.fixture',
  import.meta.url,
))
const IMPORTER_BINDING_FIXTURE_FIELDS = [
  'canonical_name',
  'canonicalization',
  'contract_version',
  'inventory_byte_size',
  'inventory_canonical_name_key',
  'inventory_expected_shard',
  'inventory_name',
  'inventory_observed_shard',
  'inventory_relative_path',
  'inventory_sha256',
  'legacy_shard',
  'outcome',
  'player_file_sha256',
  'storage_format',
  'wire_hex',
]

/** Reads the reviewed cross-language fixture; production never parses this test fixture. */
export function importerBindingFixture(): { evidence: LegacyIdentityEvidenceV1Shape, record: InventoryRecord } {
  const fields = new Map<string, string>()
  for (const line of readFileSync(IMPORTER_BINDING_FIXTURE_PATH, 'utf8').trimEnd().split('\n')) {
    const separator = line.indexOf('=')
    assert.ok(separator > 0, 'fixture lines are key=value')
    const key = line.slice(0, separator)
    const value = line.slice(separator + 1)
    assert.ok(value.length > 0 && !fields.has(key), 'fixture fields are nonempty and unique')
    fields.set(key, value)
  }
  assert.deepEqual([...fields.keys()].sort(), IMPORTER_BINDING_FIXTURE_FIELDS)
  assert.equal(fields.get('contract_version'), '1')

  return {
    evidence: {
      outcome: fields.get('outcome')! as LegacyIdentityEvidenceV1Shape['outcome'],
      canonicalization: fields.get('canonicalization')! as LegacyIdentityEvidenceV1Shape['canonicalization'],
      canonicalName: fields.get('canonical_name')!,
      legacyShard: fields.get('legacy_shard')!,
      playerFileSha256: fields.get('player_file_sha256')!,
      storageFormat: fields.get('storage_format')!,
    },
    record: {
      name: fields.get('inventory_name')!,
      canonicalNameKey: fields.get('inventory_canonical_name_key')!,
      relativePath: fields.get('inventory_relative_path')!,
      observedShard: fields.get('inventory_observed_shard')!,
      expectedShard: fields.get('inventory_expected_shard')!,
      byteSize: Number(fields.get('inventory_byte_size')),
      sha256: fields.get('inventory_sha256')!,
    },
  }
}
