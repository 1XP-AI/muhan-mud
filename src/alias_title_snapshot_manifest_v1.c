/* Detached AliasTitleSnapshotManifestV1 CDTO boundary.  It never reads or
 * writes a local outbox, invokes legacy alias code, or links runtime/M3 code. */
#include "alias_title_snapshot_manifest_v1.h"

#include "cdto_v1.h"

#include <limits.h>
#include <string.h>

#define ATSM_FIELDS 13U

static uint64_t atsm_u64_get(const uint8_t *in)
{
    uint64_t value = 0U;
    size_t index;
    for(index = 0U; index < 8U; ++index) value = (value << 8) | in[index];
    return value;
}

static void atsm_u64_put(uint8_t *out, uint64_t value)
{
    size_t index;
    for(index = 8U; index != 0U; --index) {
        out[index - 1U] = (uint8_t)value;
        value >>= 8;
    }
}

static size_t atsm_bounded(const char *value, size_t maximum)
{
    size_t index;
    if(!value) return maximum + 1U;
    for(index = 0U; index <= maximum; ++index)
        if(!value[index]) return index;
    return maximum + 1U;
}

static int atsm_hex(char value)
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }

static int atsm_world(const char *value)
{
    size_t index, length = atsm_bounded(value,
        ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX);
    if(!length || length > ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX ||
       value[0] < 'a' || value[0] > 'z') return 0;
    for(index = 1U; index < length; ++index)
        if(!((value[index] >= 'a' && value[index] <= 'z') ||
             (value[index] >= '0' && value[index] <= '9') ||
             value[index] == '_' || value[index] == '-')) return 0;
    return 1;
}

static int atsm_name_key(const char *value)
{
    size_t index, length = atsm_bounded(value,
        ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX);
    if(length < 2U || length > ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX ||
       (length & 1U)) return 0;
    for(index = 0U; index < length; ++index) if(!atsm_hex(value[index])) return 0;
    return 1;
}

static int atsm_uuid(const char *value)
{
    size_t index;
    if(atsm_bounded(value, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) !=
       ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) return 0;
    for(index = 0U; index < ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH; ++index)
        if(index == 8U || index == 13U || index == 18U || index == 23U) {
            if(value[index] != '-') return 0;
        } else if(!atsm_hex(value[index])) return 0;
    return 1;
}

static int atsm_snapshot_canonical(const alias_title_snapshot_manifest_v1 *value)
{
    alias_title_snapshot_v1 snapshot;
    uint8_t *again = NULL;
    size_t again_length = 0U;
    int status;

    if(!value || !value->snapshot_wire_length ||
       value->snapshot_wire_length > ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SNAPSHOT_WIRE_MAX ||
       value->snapshot_octets != value->snapshot_wire_length ||
       value->snapshot_wire_length < CDTO_V1_DIGEST_LENGTH ||
       memcmp(value->snapshot_digest,
           value->snapshot_wire + value->snapshot_wire_length - CDTO_V1_DIGEST_LENGTH,
           CDTO_V1_DIGEST_LENGTH)) return 0;
    memset(&snapshot, 0, sizeof(snapshot));
    status = alias_title_snapshot_v1_decode(value->snapshot_wire,
        value->snapshot_wire_length, &snapshot);
    if(status != CDTO_V1_OK) return 0;
    status = alias_title_snapshot_v1_encode(&snapshot, &again, &again_length);
    if(status != CDTO_V1_OK) return 0;
    status = again_length == value->snapshot_wire_length &&
        !memcmp(again, value->snapshot_wire, again_length);
    cdto_v1_free_wire(again);
    return status;
}

static int atsm_valid(const alias_title_snapshot_manifest_v1 *value)
{
    return value && atsm_world(value->world_id) &&
        atsm_name_key(value->canonical_legacy_name_key) &&
        atsm_uuid(value->character_id) && atsm_uuid(value->writer_instance_id) &&
        atsm_uuid(value->command_id) && atsm_uuid(value->correlation_id) &&
        atsm_uuid(value->event_id) && value->writer_epoch && value->writer_revision &&
        value->writer_epoch <= INT64_MAX && value->writer_revision <= INT64_MAX &&
        atsm_snapshot_canonical(value);
}

int alias_title_snapshot_manifest_v1_encode(value, wire, wire_length)
const alias_title_snapshot_manifest_v1 *value;
uint8_t **wire;
size_t *wire_length;
{
    cdto_v1_field fields[ATSM_FIELDS];
    cdto_v1_record record;
    uint8_t schema[2] = { 0U, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SCHEMA };
    uint8_t epoch[8], revision[8], octets[8];
    size_t world_length, key_length;

    if(!wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    *wire = NULL; *wire_length = 0U;
    if(!atsm_valid(value)) return CDTO_V1_INVALID_ARGUMENT;
    world_length = strlen(value->world_id);
    key_length = strlen(value->canonical_legacy_name_key);
    atsm_u64_put(epoch, value->writer_epoch);
    atsm_u64_put(revision, value->writer_revision);
    atsm_u64_put(octets, value->snapshot_octets);
#define ATSM_FIELD(index, field_id, tag, bytes, byte_length) \
    fields[index].id = field_id; fields[index].type_tag = tag; \
    fields[index].value = (const uint8_t *)(bytes); fields[index].length = (uint32_t)(byte_length)
    ATSM_FIELD(0, 1U, CDTO_V1_TYPE_U16, schema, sizeof(schema));
    ATSM_FIELD(1, 2U, CDTO_V1_TYPE_TEXT, value->world_id, world_length);
    ATSM_FIELD(2, 3U, CDTO_V1_TYPE_TEXT, value->canonical_legacy_name_key, key_length);
    ATSM_FIELD(3, 4U, CDTO_V1_TYPE_TEXT, value->character_id, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH);
    ATSM_FIELD(4, 5U, CDTO_V1_TYPE_TEXT, value->writer_instance_id, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH);
    ATSM_FIELD(5, 6U, CDTO_V1_TYPE_U64, epoch, sizeof(epoch));
    ATSM_FIELD(6, 7U, CDTO_V1_TYPE_U64, revision, sizeof(revision));
    ATSM_FIELD(7, 8U, CDTO_V1_TYPE_TEXT, value->command_id, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH);
    ATSM_FIELD(8, 9U, CDTO_V1_TYPE_TEXT, value->correlation_id, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH);
    ATSM_FIELD(9, 10U, CDTO_V1_TYPE_TEXT, value->event_id, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH);
    ATSM_FIELD(10, 11U, CDTO_V1_TYPE_BYTES, value->snapshot_wire, value->snapshot_wire_length);
    ATSM_FIELD(11, 12U, CDTO_V1_TYPE_BYTES, value->snapshot_digest, CDTO_V1_DIGEST_LENGTH);
    ATSM_FIELD(12, 13U, CDTO_V1_TYPE_U64, octets, sizeof(octets));
#undef ATSM_FIELD
    record.kind = CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT_MANIFEST;
    record.fields = fields; record.field_count = ATSM_FIELDS;
    return cdto_v1_encode(&record, wire, wire_length);
}

static int atsm_field(const cdto_v1_decoded_record *record, size_t index,
    uint16_t id, uint8_t tag, uint32_t length)
{
    return record->fields[index].id == id && record->fields[index].type_tag == tag &&
        (length == UINT32_MAX || record->fields[index].length == length);
}

static int atsm_no_nul(const uint8_t *value, uint32_t length)
{
    uint32_t index;
    for(index = 0U; index < length; ++index) if(!value[index]) return 0;
    return 1;
}

int alias_title_snapshot_manifest_v1_decode(wire, wire_length, value)
const uint8_t *wire;
size_t wire_length;
alias_title_snapshot_manifest_v1 *value;
{
    cdto_v1_decoded_record record;
    alias_title_snapshot_manifest_v1 candidate;
    int status;

    if(!value) return CDTO_V1_INVALID_ARGUMENT;
    memset(value, 0, sizeof(*value)); memset(&record, 0, sizeof(record));
    status = cdto_v1_decode(wire, wire_length, &record);
    if(status != CDTO_V1_OK) return status;
    memset(&candidate, 0, sizeof(candidate));
    if(record.kind != CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT_MANIFEST ||
       record.field_count != ATSM_FIELDS ||
       !atsm_field(&record, 0U, 1U, CDTO_V1_TYPE_U16, 2U) ||
       record.fields[0].value[0] || record.fields[0].value[1] != ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SCHEMA ||
       !atsm_field(&record, 1U, 2U, CDTO_V1_TYPE_TEXT, UINT32_MAX) ||
       !atsm_field(&record, 2U, 3U, CDTO_V1_TYPE_TEXT, UINT32_MAX) ||
       !atsm_field(&record, 3U, 4U, CDTO_V1_TYPE_TEXT, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) ||
       !atsm_field(&record, 4U, 5U, CDTO_V1_TYPE_TEXT, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) ||
       !atsm_field(&record, 5U, 6U, CDTO_V1_TYPE_U64, 8U) ||
       !atsm_field(&record, 6U, 7U, CDTO_V1_TYPE_U64, 8U) ||
       !atsm_field(&record, 7U, 8U, CDTO_V1_TYPE_TEXT, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) ||
       !atsm_field(&record, 8U, 9U, CDTO_V1_TYPE_TEXT, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) ||
       !atsm_field(&record, 9U, 10U, CDTO_V1_TYPE_TEXT, ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_UUID_LENGTH) ||
       !atsm_field(&record, 10U, 11U, CDTO_V1_TYPE_BYTES, UINT32_MAX) ||
       !atsm_field(&record, 11U, 12U, CDTO_V1_TYPE_BYTES, CDTO_V1_DIGEST_LENGTH) ||
       !atsm_field(&record, 12U, 13U, CDTO_V1_TYPE_U64, 8U) ||
       record.fields[1].length > ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_WORLD_ID_MAX ||
       record.fields[2].length > ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_LEGACY_NAME_KEY_MAX ||
       !atsm_no_nul(record.fields[1].value, record.fields[1].length) ||
       !atsm_no_nul(record.fields[2].value, record.fields[2].length) ||
       !atsm_no_nul(record.fields[3].value, record.fields[3].length) ||
       !atsm_no_nul(record.fields[4].value, record.fields[4].length) ||
       !atsm_no_nul(record.fields[7].value, record.fields[7].length) ||
       !atsm_no_nul(record.fields[8].value, record.fields[8].length) ||
       !atsm_no_nul(record.fields[9].value, record.fields[9].length) ||
       record.fields[10].length > ALIAS_TITLE_SNAPSHOT_MANIFEST_V1_SNAPSHOT_WIRE_MAX)
        status = CDTO_V1_INVALID_FIELD_LENGTH;
    else {
        memcpy(candidate.world_id, record.fields[1].value, record.fields[1].length);
        memcpy(candidate.canonical_legacy_name_key, record.fields[2].value, record.fields[2].length);
        memcpy(candidate.character_id, record.fields[3].value, record.fields[3].length);
        memcpy(candidate.writer_instance_id, record.fields[4].value, record.fields[4].length);
        candidate.writer_epoch = atsm_u64_get(record.fields[5].value);
        candidate.writer_revision = atsm_u64_get(record.fields[6].value);
        memcpy(candidate.command_id, record.fields[7].value, record.fields[7].length);
        memcpy(candidate.correlation_id, record.fields[8].value, record.fields[8].length);
        memcpy(candidate.event_id, record.fields[9].value, record.fields[9].length);
        candidate.snapshot_wire_length = record.fields[10].length;
        memcpy(candidate.snapshot_wire, record.fields[10].value, candidate.snapshot_wire_length);
        memcpy(candidate.snapshot_digest, record.fields[11].value, CDTO_V1_DIGEST_LENGTH);
        candidate.snapshot_octets = atsm_u64_get(record.fields[12].value);
        status = atsm_valid(&candidate) ? CDTO_V1_OK : CDTO_V1_INVALID_FIELD_LENGTH;
    }
    cdto_v1_free_decoded(&record);
    if(status == CDTO_V1_OK) *value = candidate;
    return status;
}
