import { createHash } from 'node:crypto'
import { canonicalNameKey, expectedShard, SHA256_RE, type InventoryRecord, validRecord } from './inventory.js'

const SHA1_RE = /^[0-9a-f]{40}$/
const SHADOW_LOCATOR_FIELDS = ['canonicalName', 'nameSha1', 'shard']

/** The complete metadata locator admitted from the legacy shadow boundary. */
export interface LegacyPlayerShadowLocatorV1 {
  canonicalName: string
  nameSha1: string
  shard: string
}

/** A successful association with an already-admitted inventory record. */
export interface InventoryLegacyPlayerShadowLocatorBindingV1 {
  canonicalName: string
  nameSha1: string
  shard: string
}

/** Structural shape supplied by the already-strict-decoded evidence boundary. */
export interface LegacyIdentityEvidenceV1Shape {
  outcome: 'ok' | 'not_found' | 'corrupt' | 'io_error' | 'invalid_input'
  canonicalization: 'canonical' | 'normalized' | 'invalid'
  canonicalName: string
  legacyShard: string
  playerFileSha256: string
  storageFormat: string
}

const EVIDENCE_FIELDS = ['canonicalName', 'canonicalization', 'legacyShard', 'outcome', 'playerFileSha256', 'storageFormat']

function closedShadowLocator(value: unknown): LegacyPlayerShadowLocatorV1 | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  const prototype = Object.getPrototypeOf(value)
  if (prototype !== Object.prototype && prototype !== null) return undefined
  const fields = Object.getOwnPropertyDescriptors(value)
  const names = Object.getOwnPropertyNames(value).sort()
  if (Object.getOwnPropertySymbols(value).length !== 0
    || names.length !== SHADOW_LOCATOR_FIELDS.length
    || names.some((name, index) => name !== SHADOW_LOCATOR_FIELDS[index])) return undefined
  if (SHADOW_LOCATOR_FIELDS.some((name) => {
    const field = fields[name]!
    return !('value' in field) || !field.enumerable
  })) return undefined

  const canonicalName = fields.canonicalName!.value
  const nameSha1 = fields.nameSha1!.value
  const shard = fields.shard!.value
  if (typeof canonicalName !== 'string' || typeof nameSha1 !== 'string' || typeof shard !== 'string') return undefined
  return { canonicalName, nameSha1, shard }
}

function canonicalAdmittedInventoryIdentity(record: InventoryRecord): boolean {
  return record.name === canonicalNameKey(record.name)
    && record.canonicalNameKey === record.name
    && record.observedShard === record.expectedShard
    && record.expectedShard === expectedShard(record.name)
}

function closedIdentityEvidence(value: unknown): LegacyIdentityEvidenceV1Shape | undefined {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return undefined
  const prototype = Object.getPrototypeOf(value)
  if (prototype !== Object.prototype && prototype !== null) return undefined
  const fields = Object.getOwnPropertyDescriptors(value)
  const names = Object.getOwnPropertyNames(value).sort()
  if (Object.getOwnPropertySymbols(value).length !== 0
    || names.length !== EVIDENCE_FIELDS.length
    || names.some((name, index) => name !== EVIDENCE_FIELDS[index])) return undefined
  if (EVIDENCE_FIELDS.some((name) => {
    const field = fields[name]!
    return !('value' in field) || !field.enumerable
  })) return undefined

  const outcome = fields.outcome!.value
  const canonicalization = fields.canonicalization!.value
  const canonicalName = fields.canonicalName!.value
  const legacyShard = fields.legacyShard!.value
  const playerFileSha256 = fields.playerFileSha256!.value
  const storageFormat = fields.storageFormat!.value
  if (typeof outcome !== 'string' || typeof canonicalization !== 'string'
    || typeof canonicalName !== 'string' || typeof legacyShard !== 'string'
    || typeof playerFileSha256 !== 'string' || typeof storageFormat !== 'string'
    || outcome !== 'ok'
    || (canonicalization !== 'canonical' && canonicalization !== 'normalized')
    || storageFormat !== 'player-v1'
    || !SHA256_RE.test(playerFileSha256)) return undefined
  return { outcome, canonicalization, canonicalName, legacyShard, playerFileSha256, storageFormat }
}

/**
 * Binds already-admitted import metadata to a closed shadow locator only when
 * canonical name, SHA-1, and shard agree exactly. This is a pure adapter.
 */
export function bindLegacyPlayerShadowLocatorV1(
  record: InventoryRecord,
  metadata: unknown,
): InventoryLegacyPlayerShadowLocatorBindingV1 | undefined {
  const locator = closedShadowLocator(metadata)
  if (!locator || !canonicalAdmittedInventoryIdentity(record)
    || locator.canonicalName !== record.name
    || !SHA1_RE.test(locator.nameSha1)
    || locator.nameSha1 !== createHash('sha1').update(record.name, 'utf8').digest('hex')
    || locator.shard !== locator.nameSha1.slice(0, 2)
    || locator.shard !== record.expectedShard) return undefined

  return {
    canonicalName: locator.canonicalName,
    nameSha1: locator.nameSha1,
    shard: locator.shard,
  }
}

/**
 * Binds strict identity evidence to an already-validated inventory record.
 * Evidence is metadata only: this adapter does not decode wire bytes or build records.
 */
export function bindLegacyPlayerShadowEvidenceV1(
  record: InventoryRecord,
  evidence: unknown,
): InventoryLegacyPlayerShadowLocatorBindingV1 | undefined {
  const decoded = closedIdentityEvidence(evidence)
  if (!decoded || !validRecord(record)
    || decoded.canonicalName !== record.canonicalNameKey
    || decoded.playerFileSha256 !== record.sha256) return undefined

  return bindLegacyPlayerShadowLocatorV1(record, {
    canonicalName: decoded.canonicalName,
    nameSha1: createHash('sha1').update(decoded.canonicalName, 'utf8').digest('hex'),
    shard: decoded.legacyShard,
  })
}
