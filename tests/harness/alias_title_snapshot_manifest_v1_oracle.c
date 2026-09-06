/* Narrow C oracle for the closed AliasTitleSnapshotManifestV1 fixture.  It
 * owns no legacy I/O and is used only by the Rust differential test. */
#include "alias_title_snapshot_manifest_v1.h"
#include "alias_title_snapshot_v1.h"
#include "cdto_v1.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int nibble(char value)
{
    if(value >= '0' && value <= '9') return value - '0';
    if(value >= 'a' && value <= 'f') return value - 'a' + 10;
    return -1;
}

static int parse_hex(const char *input, uint8_t **output, size_t *length)
{
    size_t index, input_length;
    int high, low;
    if(!input || !output || !length) return -1;
    *output = NULL; *length = 0U;
    input_length = strlen(input);
    if((input_length & 1U) || input_length / 2U >
       CDTO_V1_ALIAS_TITLE_SNAPSHOT_MANIFEST_PAYLOAD_LIMIT +
       CDTO_V1_PREFIX_LENGTH + CDTO_V1_DIGEST_LENGTH) return -1;
    *output = (uint8_t *)malloc(input_length / 2U ? input_length / 2U : 1U);
    if(!*output) return -1;
    for(index = 0U; index < input_length; index += 2U) {
        high = nibble(input[index]); low = nibble(input[index + 1U]);
        if(high < 0 || low < 0) { free(*output); *output = NULL; return -1; }
        (*output)[index / 2U] = (uint8_t)((high << 4) | low);
    }
    *length = input_length / 2U;
    return 0;
}

static void print_hex(const uint8_t *value, size_t length)
{
    size_t index;
    for(index = 0U; index < length; ++index) printf("%02x", value[index]);
    putchar('\n');
}

static int fixture(alias_title_snapshot_manifest_v1 *value)
{
    alias_title_snapshot_v1 snapshot;
    uint8_t *wire;
    size_t length;
    memset(value, 0, sizeof(*value)); memset(&snapshot, 0, sizeof(snapshot));
    strcpy(value->world_id, "muhan");
    strcpy(value->canonical_legacy_name_key, "616c696365");
    strcpy(value->character_id, "11111111-1111-1111-1111-111111111111");
    strcpy(value->writer_instance_id, "22222222-2222-2222-2222-222222222222");
    value->writer_epoch = 7U; value->writer_revision = 19U;
    strcpy(value->command_id, "33333333-3333-3333-3333-333333333333");
    strcpy(value->correlation_id, "44444444-4444-4444-4444-444444444444");
    strcpy(value->event_id, "55555555-5555-5555-5555-555555555555");
    snapshot.alias_count = 2U;
    snapshot.aliases[0].alias_length = 1U; memcpy(snapshot.aliases[0].alias, "n", 1U);
    snapshot.aliases[0].process_length = 5U; memcpy(snapshot.aliases[0].process, "north", 5U);
    snapshot.aliases[1].alias_length = 1U; memcpy(snapshot.aliases[1].alias, "a", 1U);
    snapshot.aliases[1].process_length = 13U; memcpy(snapshot.aliases[1].process, "attack target", 13U);
    snapshot.title_present = 1U; snapshot.title_length = 5U; memcpy(snapshot.title, "Ruler", 5U);
    wire = NULL; length = 0U;
    if(alias_title_snapshot_v1_encode(&snapshot, &wire, &length) != CDTO_V1_OK ||
       length > sizeof(value->snapshot_wire)) return -1;
    memcpy(value->snapshot_wire, wire, length); value->snapshot_wire_length = length;
    value->snapshot_octets = length;
    memcpy(value->snapshot_digest, wire + length - CDTO_V1_DIGEST_LENGTH,
        CDTO_V1_DIGEST_LENGTH);
    cdto_v1_free_wire(wire);
    return 0;
}

int main(int argc, char **argv)
{
    alias_title_snapshot_manifest_v1 value;
    uint8_t *wire;
    size_t length;
    int status;

    if(argc == 2 && !strcmp(argv[1], "fixture")) {
        if(fixture(&value)) return 2;
        wire = NULL; length = 0U;
        if(alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) != CDTO_V1_OK)
            return 2;
        print_hex(wire, length); cdto_v1_free_wire(wire); return 0;
    }
    if(argc == 3 && (!strcmp(argv[1], "decode") || !strcmp(argv[1], "roundtrip"))) {
        if(parse_hex(argv[2], &wire, &length)) return 2;
        status = alias_title_snapshot_manifest_v1_decode(wire, length, &value);
        free(wire);
        if(!strcmp(argv[1], "decode")) { puts(status == CDTO_V1_OK ? "0" : "1"); return 0; }
        if(status != CDTO_V1_OK ||
           alias_title_snapshot_manifest_v1_encode(&value, &wire, &length) != CDTO_V1_OK)
            return 1;
        print_hex(wire, length); cdto_v1_free_wire(wire); return 0;
    }
    fputs("usage: alias_title_snapshot_manifest_v1_oracle fixture | decode HEX | roundtrip HEX\n", stderr);
    return 2;
}
