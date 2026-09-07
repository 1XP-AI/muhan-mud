import { parsePlayerSnapshotV1NormalizedProjection } from './player-snapshot-v1-normalized-projection.js'
import {
  isImmutableNormalizedProjectionRecord,
  type ImmutablePlayerSnapshotV1NormalizedProjectionRecord,
  type ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader,
} from './player-snapshot-v1-normalized-projection-shadow-comparator.js'

/** Caller owns the connection and must supply an exclusively held read-only session.
 * This adapter does not create a login, grant access, or install a runtime consumer. */
export interface NormalizedProjectionQueryClient {
  query(sql: string, values?: readonly string[]): Promise<{ rows: Record<string, unknown>[] }>
}

const SQL = `select a.world_id as "worldId", p.character_id::text as "characterId", p.command_id::text as "commandId",
  p.receipt_request_sha256 as "receiptRequestSha256", p.writer_instance_id::text as "writerInstanceId",
  p.writer_epoch::text as "writerEpoch", p.writer_revision::text as "writerRevision",
  p.source_post_sha256 as "sourcePostSha256", p.source_octets::text as "sourceOctets",
  p.snapshot_sha256 as "snapshotSha256", p.snapshot_octets::text as "snapshotOctets",
  jsonb_build_object('format', p.projection_format, 'version', p.projection_version,
    'algorithm', p.projection_algorithm, 'canonical_digest', p.canonical_digest,
    'player', jsonb_build_object('level', p.level, 'hp_max', p.hp_max, 'hp_current', p.hp_current,
      'mp_max', p.mp_max, 'mp_current', p.mp_current, 'experience', p.experience, 'gold', p.gold,
      'daily', (select jsonb_agg(jsonb_build_object('max', d.max_value, 'current', d.current_value,
        'last_used', d.last_used) order by d.slot)
        from private.game_character_player_snapshot_normalized_v1_projection_daily d
        where d.character_id = p.character_id and d.command_id = p.command_id),
      'timers', (select jsonb_agg(jsonb_build_object('interval', t.interval_value, 'last_used', t.last_used,
        'misc', t.misc) order by t.slot)
        from private.game_character_player_snapshot_normalized_v1_projection_timers t
        where t.character_id = p.character_id and t.command_id = p.command_id),
      'items', coalesce((select jsonb_agg(jsonb_build_object('parent_index', i.parent_index,
        'child_index', i.child_index, 'value', i.value, 'weight', i.weight, 'type_code', i.type_code,
        'adjustment', i.adjustment, 'shots_max', i.shots_max, 'shots_current', i.shots_current,
        'ndice', i.ndice, 'sdice', i.sdice, 'pdice', i.pdice, 'armor', i.armor, 'wear_flag', i.wear_flag,
        'magic_power', i.magic_power, 'magic_realm', i.magic_realm, 'special', i.special) order by i.item_index)
        from private.game_character_player_snapshot_normalized_v1_projection_items i
        where i.character_id = p.character_id and i.command_id = p.command_id), '[]'::jsonb)
    ))::text as "projectionText"
from private.game_character_player_snapshot_normalized_v1_projections p
join private.game_character_player_snapshot_v1_artifacts a
  on a.character_id = p.character_id and a.command_id = p.command_id
  and a.receipt_request_sha256 = p.receipt_request_sha256 and a.writer_instance_id = p.writer_instance_id
  and a.writer_epoch = p.writer_epoch and a.writer_revision = p.writer_revision
  and a.source_post_sha256 = p.source_post_sha256 and a.source_octets = p.source_octets
  and a.snapshot_sha256 = p.snapshot_sha256 and a.snapshot_octets = p.snapshot_octets
  and a.snapshot_format = 'player-snapshot-v1'
join private.game_character_shadow_receipts r
  on r.character_id = a.character_id and r.command_id = a.command_id
  and r.world_id = a.world_id and r.legacy_name_key = a.legacy_name_key
  and r.request_sha256 = a.receipt_request_sha256 and r.post_sha256 = a.source_post_sha256
  and r.writer_instance_id = a.writer_instance_id and r.writer_epoch = a.writer_epoch
  and r.writer_revision = a.writer_revision and r.storage_format = a.storage_format
  and r.acknowledged_at = a.receipt_acknowledged_at
join private.game_character_m4_file_snapshot_manifests m
  on m.character_id = a.character_id and m.command_id = a.command_id
  and m.world_id = a.world_id and m.legacy_name_key = a.legacy_name_key
  and m.receipt_request_sha256 = a.receipt_request_sha256 and m.file_post_sha256 = a.source_post_sha256
  and m.writer_instance_id = a.writer_instance_id and m.writer_epoch = a.writer_epoch
  and m.writer_revision = a.writer_revision and m.storage_format = a.storage_format
  and m.receipt_acknowledged_at = a.receipt_acknowledged_at
  and m.snapshot_sha256 = a.source_post_sha256 and m.snapshot_octets = a.source_octets
  and m.snapshot_format = 'legacy-file-manifest-v1'
where a.world_id = $1::text and p.character_id = $2::uuid and p.command_id = $3::uuid
  and p.item_count = (select count(*) from private.game_character_player_snapshot_normalized_v1_projection_items n
    where n.character_id = p.character_id and n.command_id = p.command_id)
  and p.item_count - 1 = coalesce((select max(n.item_index) from private.game_character_player_snapshot_normalized_v1_projection_items n
    where n.character_id = p.character_id and n.command_id = p.command_id), -1)`

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const invalid = () => new Error('invalid normalized projection read')

export class PostgresNormalizedProjectionReader implements ImmutablePlayerSnapshotV1NormalizedProjectionRecordReader {
  constructor(private readonly client: NormalizedProjectionQueryClient) {}

  async findByIdentity(identity: Readonly<{ worldId: string, characterId: string, commandId: string }>): Promise<readonly ImmutablePlayerSnapshotV1NormalizedProjectionRecord[]> {
    if (!identity || typeof identity.worldId !== 'string' || !/^[a-z][a-z0-9_-]{0,63}$/.test(identity.worldId)
      || typeof identity.characterId !== 'string' || !UUID.test(identity.characterId)
      || typeof identity.commandId !== 'string' || !UUID.test(identity.commandId)) throw invalid()
    const { worldId, characterId, commandId } = identity
    const session = await this.client.query('show transaction_read_only')
    if (session.rows.length !== 1 || session.rows[0]?.transaction_read_only !== 'on') throw invalid()
    const result = await this.client.query(SQL, [worldId, characterId, commandId])
    return result.rows.map((row) => {
      const { projectionText, snapshotOctets, ...metadata } = row
      if (typeof projectionText !== 'string' || Buffer.byteLength(projectionText) > 4_194_351
        || typeof snapshotOctets !== 'string' || !/^[1-9][0-9]{1,6}$/.test(snapshotOctets)
        || row.worldId !== worldId || row.characterId !== characterId || row.commandId !== commandId) throw invalid()
      const record = { ...metadata, snapshotOctets: Number(snapshotOctets),
        projection: parsePlayerSnapshotV1NormalizedProjection(Buffer.from(projectionText + '\n')) }
      if (!isImmutableNormalizedProjectionRecord(record)) throw invalid()
      return record
    })
  }
}
