/* Detached AliasTitleSnapshotV1 CDTO boundary.  No live alias persistence
 * code calls this file; the legacy parser authority remains in the test oracle. */
#include "alias_title_snapshot_v1.h"

#include "cdto_v1.h"

#include <string.h>

#define ATS_FIELDS 4U
#define ATS_ALIAS_LIST_MAX (ALIAS_TITLE_SNAPSHOT_V1_MAX_ALIASES * \
    (1U + ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES + 2U + \
     ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES))

static void ats_u16_put(uint8_t *out, uint16_t value)
{
    out[0] = (uint8_t)(value >> 8);
    out[1] = (uint8_t)value;
}

static uint16_t ats_u16_get(const uint8_t *in)
{ return (uint16_t)(((uint16_t)in[0] << 8) | in[1]); }

static int ats_valid(const alias_title_snapshot_v1 *value)
{
    size_t i,j;
    if (!value || value->alias_count > ALIAS_TITLE_SNAPSHOT_V1_MAX_ALIASES ||
        value->title_present > 1U ||
        value->title_length > ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES)
        return 0;
    if (!value->title_present && value->title_length != 0U)
        return 0;
    for (i = 0U; i < value->alias_count; ++i) {
        if (value->aliases[i].alias_length == 0U ||
            value->aliases[i].alias_length > ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES ||
            value->aliases[i].process_length > ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES)
            return 0;
        for (j = 0U; j < i; ++j) {
            if (value->aliases[j].alias_length == value->aliases[i].alias_length &&
                !memcmp(value->aliases[j].alias, value->aliases[i].alias,
                    value->aliases[i].alias_length)) return 0;
        }
    }
    return 1;
}

int alias_title_snapshot_v1_encode(const alias_title_snapshot_v1 *value,
    uint8_t **wire, size_t *wire_length)
{
    cdto_v1_field fields[ATS_FIELDS];
    cdto_v1_record record;
    uint8_t schema[2], present[1], list[ATS_ALIAS_LIST_MAX];
    size_t at = 0U, i;
    int status;

    if (!wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    *wire = NULL;
    *wire_length = 0U;
    if (!ats_valid(value)) return CDTO_V1_INVALID_ARGUMENT;
    for (i = 0U; i < value->alias_count; ++i) {
        const alias_title_snapshot_v1_alias *entry = &value->aliases[i];
        list[at++] = entry->alias_length;
        memcpy(list + at, entry->alias, entry->alias_length);
        at += entry->alias_length;
        ats_u16_put(list + at, entry->process_length);
        at += 2U;
        memcpy(list + at, entry->process, entry->process_length);
        at += entry->process_length;
    }
    ats_u16_put(schema, ALIAS_TITLE_SNAPSHOT_V1_SCHEMA);
    present[0] = value->title_present;
    fields[0].id = 1U; fields[0].type_tag = CDTO_V1_TYPE_U16;
    fields[0].value = schema; fields[0].length = sizeof(schema);
    fields[1].id = 2U; fields[1].type_tag = CDTO_V1_TYPE_BYTES;
    fields[1].value = list; fields[1].length = (uint32_t)at;
    fields[2].id = 3U; fields[2].type_tag = CDTO_V1_TYPE_BOOL;
    fields[2].value = present; fields[2].length = sizeof(present);
    fields[3].id = 4U; fields[3].type_tag = CDTO_V1_TYPE_BYTES;
    fields[3].value = value->title; fields[3].length = value->title_length;
    record.kind = CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT;
    record.fields = fields;
    record.field_count = ATS_FIELDS;
    status = cdto_v1_encode(&record, wire, wire_length);
    return status;
}

int alias_title_snapshot_v1_decode(const uint8_t *wire, size_t wire_length,
    alias_title_snapshot_v1 *value)
{
    cdto_v1_decoded_record record;
    const uint8_t *list;
    size_t at = 0U, index = 0U;
    int status = CDTO_V1_INVALID_FIELD_LENGTH;

    if (!value) return CDTO_V1_INVALID_ARGUMENT;
    memset(value, 0, sizeof(*value));
    status = cdto_v1_decode(wire, wire_length, &record);
    if (status != CDTO_V1_OK) return status;
    if (record.kind != CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT ||
        record.field_count != ATS_FIELDS ||
        record.fields[0].id != 1U || record.fields[0].type_tag != CDTO_V1_TYPE_U16 ||
        record.fields[0].length != 2U ||
        ats_u16_get(record.fields[0].value) != ALIAS_TITLE_SNAPSHOT_V1_SCHEMA ||
        record.fields[1].id != 2U || record.fields[1].type_tag != CDTO_V1_TYPE_BYTES ||
        record.fields[2].id != 3U || record.fields[2].type_tag != CDTO_V1_TYPE_BOOL ||
        record.fields[2].length != 1U || record.fields[2].value[0] > 1U ||
        record.fields[3].id != 4U || record.fields[3].type_tag != CDTO_V1_TYPE_BYTES ||
        record.fields[3].length > ALIAS_TITLE_SNAPSHOT_V1_TITLE_MAX_BYTES ||
        (!record.fields[2].value[0] && record.fields[3].length != 0U))
        goto done;
    value->title_present = record.fields[2].value[0];
    value->title_length = (uint8_t)record.fields[3].length;
    memcpy(value->title, record.fields[3].value, value->title_length);
    list = record.fields[1].value;
    while (at < record.fields[1].length) {
        alias_title_snapshot_v1_alias *entry;
        uint8_t alias_length;
        uint16_t process_length;
        if (index == ALIAS_TITLE_SNAPSHOT_V1_MAX_ALIASES ||
            record.fields[1].length - at < 3U)
            goto done;
        alias_length = list[at++];
        if (alias_length == 0U ||
            alias_length > ALIAS_TITLE_SNAPSHOT_V1_ALIAS_MAX_BYTES ||
            record.fields[1].length - at < (size_t)alias_length + 2U)
            goto done;
        entry = &value->aliases[index];
        entry->alias_length = alias_length;
        memcpy(entry->alias, list + at, alias_length);
        at += alias_length;
        process_length = ats_u16_get(list + at);
        at += 2U;
        if (process_length > ALIAS_TITLE_SNAPSHOT_V1_PROCESS_MAX_BYTES ||
            record.fields[1].length - at < process_length)
            goto done;
        entry->process_length = process_length;
        memcpy(entry->process, list + at, process_length);
        at += process_length;
        ++index;
    }
    value->alias_count = (uint16_t)index;
    status = ats_valid(value) ? CDTO_V1_OK : CDTO_V1_INVALID_FIELD_LENGTH;
done:
    cdto_v1_free_decoded(&record);
    if (status != CDTO_V1_OK) memset(value, 0, sizeof(*value));
    return status;
}
