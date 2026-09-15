#include <stdio.h>
#include <string.h>
#include <stdlib.h>

#include "cdto_v1.h"
#include "object_v1.h"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "object_v1_test: %s\n", message);
    return 1;
}

static void fixture(value)
object *value;
{
    memset(value, 0, sizeof(*value));
    memcpy(value->name, "bronze-key", 11);
    memcpy(value->description, "A weathered bronze key.", 24);
    memcpy(value->key[0], "key", 4); memcpy(value->key[1], "bronze", 7); memcpy(value->key[2], "quest", 6);
    memcpy(value->use_output, "The key turns.\n", 16);
    value->value = 0x102030405L; value->weight = -7; value->type = 4; value->adjustment = -2;
    value->shotsmax = 9; value->shotscur = 7; value->ndice = 1; value->sdice = 8; value->pdice = -3;
    value->armor = -1; value->wearflag = 3; value->magicpower = 6; value->magicrealm = 2; value->special = 77;
    value->flags[0] = 0x55; value->flags[7] = (char)0xaa; value->questnum = 12;
}

static void hex(out, input, length)
char *out;
const unsigned char *input;
size_t length;
{
    static const char digits[] = "0123456789abcdef";
    size_t i;
    for(i = 0; i < length; ++i) { out[2*i] = digits[input[i] >> 4]; out[2*i+1] = digits[input[i] & 15]; }
    out[2*length] = 0;
}

/* Build a valid-digest Object envelope whose shots_current (field 12) exceeds
 * shots_max (field 11).  Schema decoders must reject it as noncanonical. */
static int noncanonical_shots_wire(canonical, canonical_length, wire, wire_length)
const unsigned char *canonical;
size_t canonical_length;
unsigned char **wire;
size_t *wire_length;
{
    cdto_v1_decoded_record decoded;
    cdto_v1_field fields[OBJECT_V1_FIELD_COUNT];
    cdto_v1_record record;
    unsigned int i;
    int status;

    memset(&decoded, 0, sizeof(decoded));
    *wire = 0;
    *wire_length = 0;
    status = cdto_v1_decode(canonical, canonical_length, &decoded);
    if(status != CDTO_V1_OK) return status;
    for(i = 0; i < OBJECT_V1_FIELD_COUNT; ++i) {
        fields[i].id = decoded.fields[i].id;
        fields[i].type_tag = decoded.fields[i].type_tag;
        fields[i].value = decoded.fields[i].value;
        fields[i].length = decoded.fields[i].length;
    }
    decoded.fields[11].value[0] = 0;
    decoded.fields[11].value[1] = 10;
    record.kind = decoded.kind;
    record.fields = fields;
    record.field_count = OBJECT_V1_FIELD_COUNT;
    status = cdto_v1_encode(&record, wire, wire_length);
    cdto_v1_free_decoded(&decoded);
    return status;
}

int main(void)
{
    object input, output, nested;
    unsigned char *wire, *noncanonical, *stale_wire, *invalid_wire;
    size_t wire_length, noncanonical_length, stale_length, invalid_length;
    char *wire_hex;
    int failed = 0;

    fixture(&input); wire = 0; wire_length = 0; noncanonical = 0; noncanonical_length = 0;
    failed += expect(object_v1_encode_flat(&input, &wire, &wire_length) == CDTO_V1_OK,
                     "detached object must encode");
    failed += expect(wire && wire_length > 32 && wire[10] == 0 && wire[11] == CDTO_V1_KIND_OBJECT,
                     "ObjectV1 must use the CDTO object kind");
    failed += expect(wire_length == 539 && !memcmp(wire + wire_length - 32,
                     "\x40\x1f\x4f\xbe\x70\x10\x88\x76"
                     "\x75\x8d\x69\x7a\x3a\x96\xea\xf5"
                     "\xe3\xbd\x6c\xbe\x10\xc6\x8b\xc5"
                     "\x14\xf3\xfc\x8b\x75\x10\x88\x52", 32),
                     "ObjectV1 fixture envelope and digest must remain golden");
    memset(&output, 0xa5, sizeof(output));
    failed += expect(object_v1_decode_flat(wire, wire_length, &output) == CDTO_V1_OK &&
                     object_v1_equal_flat(&input, &output),
                     "C ObjectV1 encode/decode must retain flat semantics");
    failed += expect(!output.first_obj && !output.parent_obj && !output.parent_rom && !output.parent_crt,
                     "ObjectV1 decoder must not materialize pointers");
    stale_wire = wire; stale_length = wire_length;
    nested = input; nested.first_obj = (otag *)1;
    failed += expect(object_v1_encode_flat(&nested, &stale_wire, &stale_length) == OBJECT_V1_UNSUPPORTED_GRAPH &&
                     !stale_wire && stale_length == 0,
                     "failed encode must clear a stale successful output buffer");
    nested = input; nested.parent_crt = (creature *)1;
    invalid_wire = (unsigned char *)1; invalid_length = 1;
    failed += expect(object_v1_encode_flat(&nested, &invalid_wire, &invalid_length) == OBJECT_V1_UNSUPPORTED_GRAPH &&
                     !invalid_wire && invalid_length == 0,
                     "attached object must fail closed");
    nested = input; nested.shotscur = (short)(nested.shotsmax + 1);
    invalid_wire = (unsigned char *)1; invalid_length = 1;
    failed += expect(object_v1_encode_flat(&nested, &invalid_wire, &invalid_length) == CDTO_V1_INVALID_ARGUMENT &&
                     !invalid_wire && invalid_length == 0,
                     "noncanonical legacy shots must not produce non-roundtrippable bytes");
    failed += expect(noncanonical_shots_wire(wire, wire_length, &noncanonical, &noncanonical_length) == CDTO_V1_OK,
                     "test must produce a valid-digest noncanonical ObjectV1 envelope");
    failed += expect(object_v1_decode_flat(noncanonical, noncanonical_length, &output) == CDTO_V1_INVALID_FIELD_LENGTH,
                     "noncanonical shots envelope must be rejected, not clamped");
    cdto_v1_free_wire(noncanonical);
    failed += expect(object_v1_decode_flat(wire, wire_length - 1, &output) == CDTO_V1_TRUNCATED,
                     "truncated ObjectV1 must fail closed");
    wire[wire_length - 1] ^= 1;
    failed += expect(object_v1_decode_flat(wire, wire_length, &output) == CDTO_V1_DIGEST_MISMATCH,
                     "digest mismatch must fail closed");
    wire[wire_length - 1] ^= 1;
    wire_hex = (char *)malloc(wire_length * 2 + 1);
    failed += expect(wire_hex != 0, "golden allocation");
    if(wire_hex) { hex(wire_hex, wire, wire_length); printf("object_v1_test golden=%s\n", wire_hex); free(wire_hex); }
    cdto_v1_free_wire(wire);
    return failed ? 1 : 0;
}
