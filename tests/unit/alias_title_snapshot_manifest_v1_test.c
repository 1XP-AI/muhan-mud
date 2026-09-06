#include "alias_title_snapshot_manifest_v1.h"
#include "alias_title_snapshot_v1.h"
#include "cdto_v1.h"

#include <assert.h>
#include <stdio.h>
#include <string.h>

static int hex_byte(char value)
{
    if(value >= '0' && value <= '9') return value - '0';
    return value >= 'a' && value <= 'f' ? value - 'a' + 10 : -1;
}

static void test_shared_c_rust_fixture(void)
{
    const char *paths[] = {
        "tests/fixtures/alias_title_snapshot_manifest_v1_canonical.hex",
        "../tests/fixtures/alias_title_snapshot_manifest_v1_canonical.hex"
    };
    alias_title_snapshot_manifest_v1 value;
    char hex[4096];
    uint8_t wire[2048], *again;
    FILE *file = NULL;
    size_t length, index, again_length;
    int high, low;

    for(index = 0U; index < sizeof(paths) / sizeof(paths[0]); ++index) {
        file = fopen(paths[index], "rb");
        if(file) break;
    }
    assert(file != NULL);
    length = fread(hex, 1U, sizeof(hex), file);
    assert(fclose(file) == 0 && length > 1U && hex[length - 1U] == '\n');
    --length;
    assert(!(length & 1U) && length / 2U <= sizeof(wire));
    for(index = 0U; index < length; index += 2U) {
        high = hex_byte(hex[index]); low = hex_byte(hex[index + 1U]);
        assert(high >= 0 && low >= 0);
        wire[index / 2U] = (uint8_t)((high << 4) | low);
    }
    assert(alias_title_snapshot_manifest_v1_decode(wire, length / 2U, &value) == CDTO_V1_OK);
    again = NULL; again_length = 0U;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &again, &again_length) == CDTO_V1_OK);
    assert(again_length == length / 2U && !memcmp(again, wire, again_length));
    cdto_v1_free_wire(again);
}

static void make_snapshot(alias_title_snapshot_manifest_v1 *value)
{
    alias_title_snapshot_v1 snapshot;
    uint8_t *wire;
    size_t length;

    memset(&snapshot, 0, sizeof(snapshot));
    snapshot.alias_count = 2U;
    snapshot.aliases[0].alias_length = 1U;
    memcpy(snapshot.aliases[0].alias, "n", 1U);
    snapshot.aliases[0].process_length = 5U;
    memcpy(snapshot.aliases[0].process, "north", 5U);
    snapshot.aliases[1].alias_length = 1U;
    memcpy(snapshot.aliases[1].alias, "a", 1U);
    snapshot.aliases[1].process_length = 13U;
    memcpy(snapshot.aliases[1].process, "attack target", 13U);
    snapshot.title_present = 1U;
    snapshot.title_length = 5U;
    memcpy(snapshot.title, "Ruler", 5U);
    wire = NULL; length = 0U;
    assert(alias_title_snapshot_v1_encode(&snapshot, &wire, &length) == CDTO_V1_OK);
    assert(length <= sizeof(value->snapshot_wire));
    memcpy(value->snapshot_wire, wire, length);
    value->snapshot_wire_length = length;
    value->snapshot_octets = length;
    memcpy(value->snapshot_digest, wire + length - CDTO_V1_DIGEST_LENGTH,
        CDTO_V1_DIGEST_LENGTH);
    cdto_v1_free_wire(wire);
}

static void make_manifest(alias_title_snapshot_manifest_v1 *value)
{
    memset(value, 0, sizeof(*value));
    strcpy(value->world_id, "muhan");
    strcpy(value->canonical_legacy_name_key, "616c696365");
    strcpy(value->character_id, "11111111-1111-1111-1111-111111111111");
    strcpy(value->writer_instance_id, "22222222-2222-2222-2222-222222222222");
    value->writer_epoch = 7U;
    value->writer_revision = 19U;
    strcpy(value->command_id, "33333333-3333-3333-3333-333333333333");
    strcpy(value->correlation_id, "44444444-4444-4444-4444-444444444444");
    strcpy(value->event_id, "55555555-5555-5555-5555-555555555555");
    make_snapshot(value);
}

static void test_exact_round_trip(void)
{
    alias_title_snapshot_manifest_v1 source, decoded;
    uint8_t *wire, *again;
    size_t length, again_length;

    make_manifest(&source);
    wire = NULL; length = 0U; again = NULL; again_length = 0U;
    assert(alias_title_snapshot_manifest_v1_encode(&source, &wire, &length) == CDTO_V1_OK);
    assert(alias_title_snapshot_manifest_v1_decode(wire, length, &decoded) == CDTO_V1_OK);
    assert(!memcmp(&decoded, &source, sizeof(source)));
    assert(alias_title_snapshot_manifest_v1_encode(&decoded, &again, &again_length) == CDTO_V1_OK);
    assert(length == again_length && !memcmp(wire, again, length));
    cdto_v1_free_wire(again);
    cdto_v1_free_wire(wire);
}

static void test_required_and_cross_checked_values(void)
{
    alias_title_snapshot_manifest_v1 value;
    cdto_v1_field fields[4];
    cdto_v1_record record;
    uint8_t schema[2] = { 0U, 1U }, absent[1] = { 0U }, title[1] = { 'x' };
    uint8_t *wire;
    size_t length;

    make_manifest(&value);
    wire = (uint8_t *)1; length = 99U;
    value.world_id[0] = 0;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    assert(wire == NULL && !length);
    make_manifest(&value);
    strcpy(value.canonical_legacy_name_key, "616C696365");
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    make_manifest(&value);
    value.character_id[0] = 'A';
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    make_manifest(&value);
    value.writer_epoch = 0U;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    make_manifest(&value);
    value.snapshot_octets--;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    make_manifest(&value);
    value.snapshot_digest[0] ^= 1U;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    make_manifest(&value);
    value.snapshot_wire[value.snapshot_wire_length - 1U] ^= 1U;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
    make_manifest(&value);
    fields[0].id = 1U; fields[0].type_tag = CDTO_V1_TYPE_U16;
    fields[0].value = schema; fields[0].length = sizeof(schema);
    fields[1].id = 2U; fields[1].type_tag = CDTO_V1_TYPE_BYTES;
    fields[1].value = title; fields[1].length = 0U;
    fields[2].id = 3U; fields[2].type_tag = CDTO_V1_TYPE_BOOL;
    fields[2].value = absent; fields[2].length = sizeof(absent);
    fields[3].id = 4U; fields[3].type_tag = CDTO_V1_TYPE_BYTES;
    fields[3].value = title; fields[3].length = sizeof(title);
    record.kind = CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT;
    record.fields = fields; record.field_count = 4U;
    wire = NULL; length = 0U;
    assert(cdto_v1_encode(&record, &wire, &length) == CDTO_V1_OK);
    memcpy(value.snapshot_wire, wire, length);
    value.snapshot_wire_length = length; value.snapshot_octets = length;
    memcpy(value.snapshot_digest, wire + length - CDTO_V1_DIGEST_LENGTH,
        CDTO_V1_DIGEST_LENGTH);
    cdto_v1_free_wire(wire);
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_INVALID_ARGUMENT);
}

static void test_outer_digest_missing_and_ordering_errors(void)
{
    alias_title_snapshot_manifest_v1 value, decoded;
    cdto_v1_field fields[2];
    cdto_v1_record record;
    uint8_t schema[2] = { 0U, 1U };
    uint8_t *wire;
    size_t length;

    make_manifest(&value);
    wire = NULL; length = 0U;
    assert(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) == CDTO_V1_OK);
    wire[length - 1U] ^= 1U;
    assert(alias_title_snapshot_manifest_v1_decode(wire, length, &decoded) == CDTO_V1_DIGEST_MISMATCH);
    cdto_v1_free_wire(wire);

    fields[0].id = 1U; fields[0].type_tag = CDTO_V1_TYPE_U16;
    fields[0].value = schema; fields[0].length = sizeof(schema);
    record.kind = CDTO_V1_KIND_ALIAS_TITLE_SNAPSHOT_MANIFEST;
    record.fields = fields; record.field_count = 1U;
    wire = NULL; length = 0U;
    assert(cdto_v1_encode(&record, &wire, &length) == CDTO_V1_OK);
    assert(alias_title_snapshot_manifest_v1_decode(wire, length, &decoded) == CDTO_V1_INVALID_FIELD_LENGTH);
    cdto_v1_free_wire(wire);

    fields[0].id = 2U; fields[0].type_tag = CDTO_V1_TYPE_TEXT;
    fields[0].value = (const uint8_t *)"muhan"; fields[0].length = 5U;
    fields[1].id = 1U; fields[1].type_tag = CDTO_V1_TYPE_U16;
    fields[1].value = schema; fields[1].length = sizeof(schema);
    record.fields = fields; record.field_count = 2U;
    wire = NULL; length = 0U;
    assert(cdto_v1_encode(&record, &wire, &length) == CDTO_V1_OUT_OF_ORDER_FIELD);
    assert(wire == NULL && !length);
}

int main(void)
{
    test_shared_c_rust_fixture();
    test_exact_round_trip();
    test_required_and_cross_checked_values();
    test_outer_digest_missing_and_ordering_errors();
    puts("alias_title_snapshot_manifest_v1_test: ok");
    return 0;
}
