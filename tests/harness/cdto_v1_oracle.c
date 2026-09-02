/* Test-only command-line oracle for CDTO v1 C/Rust differential tests.
 *
 * It is not linked into the MUD and accepts only synthetic hexadecimal
 * values.  Its terse output is deliberate: failure artifacts never echo a
 * player file or an application secret.
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cdto_v1.h"
#include "creature_v1.h"
#include "object_v1.h"
#include "object_graph_v1.h"
#include "player_snapshot_v1.h"

static void object_fixture(value)
object *value;
{
    memset(value, 0, sizeof(*value));
    memcpy(value->name, "bronze-key", 11); memcpy(value->description, "A weathered bronze key.", 24);
    memcpy(value->key[0], "key", 4); memcpy(value->key[1], "bronze", 7); memcpy(value->key[2], "quest", 6);
    memcpy(value->use_output, "The key turns.\n", 16);
    value->value = 0x102030405L; value->weight = -7; value->type = 4; value->adjustment = -2;
    value->shotsmax = 9; value->shotscur = 7; value->ndice = 1; value->sdice = 8; value->pdice = -3;
    value->armor = -1; value->wearflag = 3; value->magicpower = 6; value->magicrealm = 2; value->special = 77;
    value->flags[0] = 0x55; value->flags[7] = (char)0xaa; value->questnum = 12;
}

static void object_graph_fixture(roots, objects, tags)
otag **roots;
object objects[4];
otag tags[4];
{
    object_fixture(&objects[0]); object_fixture(&objects[1]);
    object_fixture(&objects[2]); object_fixture(&objects[3]);
    strcpy(objects[0].name, "synthetic-bag"); objects[0].value = 11; objects[0].weight = 11;
    strcpy(objects[1].name, "synthetic-coin"); objects[1].value = 12; objects[1].weight = 12;
    strcpy(objects[2].name, "synthetic-key"); objects[2].value = 13; objects[2].weight = 13;
    strcpy(objects[3].name, "synthetic-gem"); objects[3].value = 14; objects[3].weight = 14;
    memset(tags, 0, 4 * sizeof(*tags));
    tags[0].obj = &objects[0]; tags[1].obj = &objects[1];
    tags[2].obj = &objects[2]; tags[3].obj = &objects[3];
    tags[1].next_tag = &tags[2]; objects[0].first_obj = &tags[1];
    objects[1].parent_obj = &objects[0]; objects[2].parent_obj = &objects[0];
    objects[1].first_obj = &tags[3]; objects[3].parent_obj = &objects[1];
    *roots = &tags[0];
}

static void object_graph_two_root_fixture(roots, objects, tags)
otag **roots;
object objects[3];
otag tags[3];
{
    object_fixture(&objects[0]); object_fixture(&objects[1]); object_fixture(&objects[2]);
    strcpy(objects[0].name, "synthetic-root-a"); objects[0].value = 21; objects[0].weight = 21;
    strcpy(objects[1].name, "synthetic-child-a"); objects[1].value = 22; objects[1].weight = 22;
    strcpy(objects[2].name, "synthetic-root-b"); objects[2].value = 23; objects[2].weight = 23;
    memset(tags, 0, 3 * sizeof(*tags));
    tags[0].obj = &objects[0]; tags[1].obj = &objects[1]; tags[2].obj = &objects[2];
    tags[0].next_tag = &tags[2]; objects[0].first_obj = &tags[1]; objects[1].parent_obj = &objects[0];
    *roots = &tags[0];
}

static void creature_fixture(value)
creature *value;
{
    int i;
    memset(value, 0, sizeof(*value));
    memcpy(value->name, "synthetic-ranger", 17);
    memcpy(value->description, "Synthetic clone fixture.", 25);
    memcpy(value->key[0], "ranger", 7); memcpy(value->key[1], "synthetic", 10); memcpy(value->key[2], "test", 5);
    memset(value->password, 0xa5, sizeof(value->password));
    value->fd = 57; value->level = 255; value->type = -2; value->class = 3; value->race = 4; value->numwander = -1;
    value->alignment = -123; value->strength = 18; value->dexterity = 17; value->constitution = 16; value->intelligence = 15; value->piety = 14;
    value->hpmax = 123; value->hpcur = 99; value->mpmax = 77; value->mpcur = 66; value->armor = -4; value->thaco = 12;
    value->experience = 0x102030405L; value->gold = -0x1020304L; value->ndice = 2; value->sdice = 7; value->pdice = -1; value->special = 44; value->questnum = 9; value->rom_num = 31;
    for(i = 0; i < 5; ++i) value->proficiency[i] = (long)(i * 101 - 200);
    for(i = 0; i < 4; ++i) value->realm[i] = (long)(i * 89 - 110);
    for(i = 0; i < 16; ++i) { value->spells[i] = (char)(i - 8); value->quests[i] = (char)(0x30 + i); }
    for(i = 0; i < 8; ++i) value->flags[i] = (char)(0xa0 + i);
    for(i = 0; i < 10; ++i) { value->carry[i] = (short)(i - 5); value->daily[i].max = (char)(i + 3); value->daily[i].cur = (char)(i + 1); value->daily[i].ltime = (long)(1000 + i); }
}

static void player_snapshot_fixture(value, objects, tags)
creature *value;
object objects[5];
otag tags[5];
{
    int i;

    creature_fixture(value);
    value->type = PLAYER;
    memcpy(value->talk, "Synthetic player talk.", 23);
    memset(value->password, 0, sizeof(value->password));
    memcpy(value->password, "PW-SENTINEL", 11);
    value->fd = 57;
    value->following = (creature *)1;
    value->first_fol = (ctag *)1;
    value->first_enm = (etag *)1;
    value->first_tlk = (ttag *)1;
    value->parent_rom = (room *)1;
    value->daily[0].max = 1;
    value->daily[0].cur = 2;
    value->daily[0].ltime = -17L;
    for(i = 0; i < 45; ++i) {
        value->lasttime[i].interval = (long)(i * 37 - 700);
        value->lasttime[i].ltime = (long)(900 - i * 41);
        value->lasttime[i].misc = (short)(i - 22);
    }
    value->name[40] = 'x';

    for(i = 0; i < 5; ++i) object_fixture(&objects[i]);
    strcpy(objects[0].name, "snapshot-root-a");
    strcpy(objects[1].name, "snapshot-child-a");
    strcpy(objects[2].name, "snapshot-child-b");
    strcpy(objects[3].name, "snapshot-grandchild");
    strcpy(objects[4].name, "snapshot-root-b");
    objects[0].name[50] = 'y';
    memset(tags, 0, 5 * sizeof(*tags));
    tags[0].obj = &objects[0];
    tags[1].obj = &objects[1];
    tags[2].obj = &objects[2];
    tags[3].obj = &objects[3];
    tags[4].obj = &objects[4];
    tags[0].next_tag = &tags[4];
    objects[0].first_obj = &tags[1];
    tags[1].next_tag = &tags[2];
    objects[1].parent_obj = &objects[0];
    objects[2].parent_obj = &objects[0];
    objects[1].first_obj = &tags[3];
    objects[3].parent_obj = &objects[1];
    objects[0].parent_crt = value;
    objects[4].parent_crt = value;
    value->first_obj = &tags[0];
}

static int nibble(value)
char value;
{
    if (value >= '0' && value <= '9') return value - '0';
    if (value >= 'a' && value <= 'f') return value - 'a' + 10;
    if (value >= 'A' && value <= 'F') return value - 'A' + 10;
    return -1;
}

static int parse_hex(input, output, length)
const char *input;
unsigned char **output;
size_t *length;
{
    size_t i, input_length;
    int high, low;
    unsigned char *bytes;

    *output = 0;
    *length = 0;
    input_length = strlen(input);
    if (input_length & 1) return 0;
    bytes = (unsigned char *)malloc(input_length / 2 ? input_length / 2 : 1);
    if (!bytes) return 0;
    for (i = 0; i < input_length; i += 2) {
        high = nibble(input[i]);
        low = nibble(input[i + 1]);
        if (high < 0 || low < 0) { free(bytes); return 0; }
        bytes[i / 2] = (unsigned char)((high << 4) | low);
    }
    *output = bytes;
    *length = input_length / 2;
    return 1;
}

static void print_hex(value, length)
const unsigned char *value;
size_t length;
{
    static const char digits[] = "0123456789abcdef";
    size_t i;
    for (i = 0; i < length; ++i) {
        putchar(digits[value[i] >> 4]);
        putchar(digits[value[i] & 15]);
    }
}

static int parse_field(input, field, owned)
const char *input;
cdto_v1_field *field;
unsigned char **owned;
{
    char *copy, *first, *second, *end;
    unsigned long id, type;
    size_t length;

    *owned = 0;
    copy = (char *)malloc(strlen(input) + 1);
    if (!copy) return 0;
    strcpy(copy, input);
    first = strchr(copy, ':');
    if (!first) { free(copy); return 0; }
    *first++ = 0;
    second = strchr(first, ':');
    if (!second) { free(copy); return 0; }
    *second++ = 0;
    id = strtoul(copy, &end, 10);
    if (*copy == 0 || *end || id > 65535U) { free(copy); return 0; }
    type = strtoul(first, &end, 10);
    if (*first == 0 || *end || type > 255U) { free(copy); return 0; }
    if (!parse_hex(second, owned, &length) || length > 0xffffffffU) {
        free(copy);
        return 0;
    }
    field->id = (uint16_t)id;
    field->type_tag = (uint8_t)type;
    field->value = *owned;
    field->length = (uint32_t)length;
    free(copy);
    return 1;
}

static int write_wire(path, wire, wire_length)
const char *path;
const unsigned char *wire;
size_t wire_length;
{
    FILE *file = fopen(path, "wb");
    int ok;

    if (!file) return 0;
    ok = fwrite(wire, 1, wire_length, file) == wire_length;
    if (fclose(file) != 0) ok = 0;
    if (!ok) {
        remove(path);
        return 0;
    }
    return 1;
}

static int read_wire(path, wire, wire_length)
const char *path;
unsigned char **wire;
size_t *wire_length;
{
    FILE *file;
    unsigned char *bytes;
    long end;
    size_t length;

    *wire = 0;
    *wire_length = 0;
    file = fopen(path, "rb");
    if (!file) return 0;
    if (fseek(file, 0L, SEEK_END) != 0 || (end = ftell(file)) < 0L ||
        (unsigned long)end > (size_t)-1 || fseek(file, 0L, SEEK_SET) != 0) {
        fclose(file);
        return 0;
    }
    length = (size_t)end;
    bytes = (unsigned char *)malloc(length ? length : 1U);
    if (!bytes) {
        fclose(file);
        return 0;
    }
    if (length && fread(bytes, 1, length, file) != length) {
        fclose(file);
        free(bytes);
        return 0;
    }
    if (fclose(file) != 0) {
        free(bytes);
        return 0;
    }
    *wire = bytes;
    *wire_length = length;
    return 1;
}

static int boundary(which, path)
const char *which;
const char *path;
{
    cdto_v1_field *fields;
    cdto_v1_record record;
    unsigned char *value, *wire;
    size_t count, value_length, wire_length, i;
    int status;

    fields = 0;
    value = 0;
    wire = 0;
    if (!strcmp(which, "max-fields")) {
        count = CDTO_V1_MAX_FIELDS;
        value_length = 0;
    } else if (!strcmp(which, "one-mib")) {
        count = 1;
        value_length = CDTO_V1_SESSION_PAYLOAD_LIMIT - CDTO_V1_FIELD_HEADER_LENGTH;
    } else if (!strcmp(which, "one-mib-plus")) {
        count = 1;
        value_length = CDTO_V1_SESSION_PAYLOAD_LIMIT;
    } else if (!strcmp(which, "count-overflow")) {
        count = CDTO_V1_MAX_FIELDS + 1U;
        value_length = 0;
    } else {
        return 0;
    }
    fields = (cdto_v1_field *)calloc(count, sizeof(*fields));
    if (!fields) return 0;
    if (value_length) {
        value = (unsigned char *)malloc(value_length);
        if (!value) { free(fields); return 0; }
        for (i = 0; i < value_length; ++i) value[i] = (unsigned char)(i * 37U + 11U);
    }
    for (i = 0; i < count; ++i) {
        fields[i].id = count == 1 ? 1 : (uint16_t)i;
        fields[i].type_tag = CDTO_V1_TYPE_BYTES;
        fields[i].value = value;
        fields[i].length = (uint32_t)value_length;
    }
    if (!strcmp(which, "count-overflow")) fields[count - 1].id = 65535U;
    record.kind = CDTO_V1_KIND_SESSION;
    record.fields = fields;
    record.field_count = count;
    status = cdto_v1_encode(&record, &wire, &wire_length);
    if (status == CDTO_V1_OK && path) {
        if (!write_wire(path, wire, wire_length)) status = CDTO_V1_ALLOCATION_FAILED;
    }
    if (status == CDTO_V1_OK) printf("ok %lu\n", (unsigned long)wire_length);
    else printf("err %d\n", status);
    cdto_v1_free_wire(wire);
    free(value);
    free(fields);
    return 1;
}

int main(argc, argv)
int argc;
char **argv;
{
    cdto_v1_record record;
    cdto_v1_decoded_record decoded;
    cdto_v1_field *fields;
    unsigned char **owned, *wire;
    size_t wire_length, i;
    int status;
    unsigned long kind;
    char *end;

    if (argc < 2) return 2;
    if (!strcmp(argv[1], "decode") && argc == 3) {
        if (!parse_hex(argv[2], &wire, &wire_length)) return 2;
        memset(&decoded, 0, sizeof(decoded));
        status = cdto_v1_decode(wire, wire_length, &decoded);
        printf("%d\n", status);
        cdto_v1_free_decoded(&decoded);
        free(wire);
        return 0;
    }
    if (!strcmp(argv[1], "abi") && argc == 2) {
        status = cdto_v1_encode_abi_fingerprint(&wire, &wire_length);
        if (status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "object-fixture") && argc == 2) {
        object value;
        object_fixture(&value);
        status = object_v1_encode_flat(&value, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "object-roundtrip") && argc == 3) {
        object value;
        if(!parse_hex(argv[2], &wire, &wire_length)) return 2;
        status = object_v1_decode_flat(wire, wire_length, &value);
        free(wire); wire = 0;
        if(status == CDTO_V1_OK) status = object_v1_encode_flat(&value, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "object-graph-fixture") && argc == 2) {
        object objects[4]; otag tags[4], *roots;
        object_graph_fixture(&roots, objects, tags);
        status = object_graph_v1_encode(roots, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "object-graph-two-root-fixture") && argc == 2) {
        object objects[3]; otag tags[3], *roots;
        object_graph_two_root_fixture(&roots, objects, tags);
        status = object_graph_v1_encode(roots, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "object-graph-roundtrip") && argc == 3) {
        otag *roots;
        if(!parse_hex(argv[2], &wire, &wire_length)) return 2;
        status = object_graph_v1_decode(wire, wire_length, &roots);
        free(wire); wire = 0;
        if(status == CDTO_V1_OK) status = object_graph_v1_encode(roots, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        object_graph_v1_free(roots); cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "object-graph-decode") && argc == 3) {
        otag *roots;
        if(!parse_hex(argv[2], &wire, &wire_length)) return 2;
        status = object_graph_v1_decode(wire, wire_length, &roots);
        printf("%d\n", status);
        object_graph_v1_free(roots); free(wire);
        return 0;
    }
    if (!strcmp(argv[1], "creature-fixture") && argc == 2) {
        creature value;
        creature_fixture(&value);
        status = creature_v1_encode_flat(&value, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "creature-roundtrip") && argc == 3) {
        creature value;
        if(!parse_hex(argv[2], &wire, &wire_length)) return 2;
        status = creature_v1_decode_flat(wire, wire_length, &value);
        free(wire); wire = 0;
        if(status == CDTO_V1_OK) status = creature_v1_encode_flat(&value, &wire, &wire_length);
        if(status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
        else printf("err %d\n", status);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "player-snapshot-fixture") && argc == 2) {
        creature value;
        object objects[5];
        otag tags[5];

        player_snapshot_fixture(&value, objects, tags);
        status = player_snapshot_v1_encode_loaded(&value, &wire, &wire_length);
        if (status == CDTO_V1_OK &&
            (value.name[40] != 'x' || objects[0].name[50] != 'y'))
            status = CDTO_V1_INVALID_ARGUMENT;
        if (status == CDTO_V1_OK) {
            print_hex(wire, wire_length);
            putchar('\n');
        } else {
            printf("err %d\n", status);
        }
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "player-snapshot-roundtrip") && argc == 3) {
        creature *value;

        value = 0;
        if (!parse_hex(argv[2], &wire, &wire_length)) return 2;
        status = player_snapshot_v1_decode_clone(wire, wire_length, &value);
        free(wire);
        wire = 0;
        if (status == CDTO_V1_OK)
            status = player_snapshot_v1_encode_loaded(value, &wire, &wire_length);
        if (status == CDTO_V1_OK) {
            print_hex(wire, wire_length);
            putchar('\n');
        } else {
            printf("err %d\n", status);
        }
        player_snapshot_v1_free_clone(value);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "player-snapshot-decode") && argc == 3) {
        creature *value;

        value = 0;
        if (!parse_hex(argv[2], &wire, &wire_length)) return 2;
        status = player_snapshot_v1_decode_clone(wire, wire_length, &value);
        printf("%d\n", status);
        player_snapshot_v1_free_clone(value);
        free(wire);
        return 0;
    }
    if (!strcmp(argv[1], "player-snapshot-roundtrip-file") && argc == 4) {
        creature *value;

        value = 0;
        if (!read_wire(argv[2], &wire, &wire_length)) return 2;
        status = player_snapshot_v1_decode_clone(wire, wire_length, &value);
        free(wire);
        wire = 0;
        if (status == CDTO_V1_OK)
            status = player_snapshot_v1_encode_loaded(value, &wire, &wire_length);
        if (status == CDTO_V1_OK && !write_wire(argv[3], wire, wire_length))
            status = CDTO_V1_ALLOCATION_FAILED;
        if (status == CDTO_V1_OK) printf("ok %lu\n", (unsigned long)wire_length);
        else printf("err %d\n", status);
        player_snapshot_v1_free_clone(value);
        cdto_v1_free_wire(wire);
        return 0;
    }
    if (!strcmp(argv[1], "player-snapshot-decode-file") && argc == 3) {
        creature *value;

        value = 0;
        if (!read_wire(argv[2], &wire, &wire_length)) return 2;
        status = player_snapshot_v1_decode_clone(wire, wire_length, &value);
        printf("%d\n", status);
        player_snapshot_v1_free_clone(value);
        free(wire);
        return 0;
    }
    if (!strcmp(argv[1], "boundary") && (argc == 3 || argc == 4))
        return boundary(argv[2], argc == 4 ? argv[3] : 0) ? 0 : 2;
    if (strcmp(argv[1], "encode") || argc < 3) return 2;
    kind = strtoul(argv[2], &end, 10);
    if (*argv[2] == 0 || *end || kind > 65535U) return 2;
    fields = (cdto_v1_field *)calloc((size_t)(argc - 3), sizeof(*fields));
    owned = (unsigned char **)calloc((size_t)(argc - 3), sizeof(*owned));
    if (!fields || !owned) { free(fields); free(owned); return 2; }
    for (i = 0; i < (size_t)(argc - 3); ++i) {
        if (!parse_field(argv[i + 3], &fields[i], &owned[i])) {
            while (i) free(owned[--i]);
            free(owned); free(fields); return 2;
        }
    }
    record.kind = (uint16_t)kind;
    record.fields = fields;
    record.field_count = (size_t)(argc - 3);
    status = cdto_v1_encode(&record, &wire, &wire_length);
    if (status == CDTO_V1_OK) { print_hex(wire, wire_length); putchar('\n'); }
    else printf("err %d\n", status);
    cdto_v1_free_wire(wire);
    for (i = 0; i < (size_t)(argc - 3); ++i) free(owned[i]);
    free(owned); free(fields);
    return 0;
}
