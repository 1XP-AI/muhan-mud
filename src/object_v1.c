/* Clone-only ObjectV1 projection.  It is intentionally independent from the
 * legacy raw struct persistence routines in files1.c. */
#include "object_v1.h"
#include "cdto_v1.h"

#include <limits.h>
#include <string.h>

static void ov1_i16(value, out)
short value;
unsigned char out[2];
{
    unsigned short bits = (unsigned short)value;
    out[0] = (unsigned char)(bits >> 8);
    out[1] = (unsigned char)bits;
}

static short ov1_get_i16(value)
const unsigned char *value;
{
    unsigned short bits = ((unsigned short)value[0] << 8) | value[1];
    return (short)bits;
}

static void ov1_i64(value, out)
long value;
unsigned char out[8];
{
    uint64_t bits = (uint64_t)(int64_t)value;
    unsigned int i;
    for(i = 0; i < 8; ++i) out[i] = (unsigned char)(bits >> (56 - 8 * i));
}

static int ov1_get_i64(value, out)
const unsigned char *value;
long *out;
{
    uint64_t bits = 0;
    int64_t parsed;
    unsigned int i;
    for(i = 0; i < 8; ++i) bits = (bits << 8) | value[i];
    parsed = (int64_t)bits;
    if(parsed < (int64_t)LONG_MIN || parsed > (int64_t)LONG_MAX) return -1;
    *out = (long)parsed;
    return 0;
}

static int ov1_detached(input)
const object *input;
{
    return input && !input->first_obj && !input->parent_obj &&
           !input->parent_rom && !input->parent_crt;
}

int object_v1_encode_flat(input, wire, wire_length)
const object *input;
uint8_t **wire;
size_t *wire_length;
{
    cdto_v1_field fields[OBJECT_V1_FIELD_COUNT];
    unsigned char value[8], weight[2], shotsmax[2], shotscur[2], ndice[2], sdice[2], pdice[2], special[2];
    unsigned int i;
    cdto_v1_record record;

    /* Clear caller-visible outputs before every validation failure so a
       previous successful allocation cannot be mistakenly reused. */
    if(wire) *wire = 0;
    if(wire_length) *wire_length = 0;
    if(!wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    if(!ov1_detached(input)) return OBJECT_V1_UNSUPPORTED_GRAPH;
    /* read_obj() canonicalizes this legacy invalid state; never export
       noncanonical bytes which cannot round-trip byte-for-byte. */
    if(input->shotscur > input->shotsmax) return CDTO_V1_INVALID_ARGUMENT;
    for(i = 0; i < OBJECT_V1_FIELD_COUNT; ++i) {
        fields[i].id = (uint16_t)(i + 1);
        fields[i].type_tag = CDTO_V1_TYPE_BYTES;
        fields[i].value = 0;
        fields[i].length = 0;
    }
    fields[0].value = (const uint8_t *)input->name; fields[0].length = sizeof(input->name);
    fields[1].value = (const uint8_t *)input->description; fields[1].length = sizeof(input->description);
    fields[2].value = (const uint8_t *)input->key[0]; fields[2].length = sizeof(input->key[0]);
    fields[3].value = (const uint8_t *)input->key[1]; fields[3].length = sizeof(input->key[1]);
    fields[4].value = (const uint8_t *)input->key[2]; fields[4].length = sizeof(input->key[2]);
    fields[5].value = (const uint8_t *)input->use_output; fields[5].length = sizeof(input->use_output);
    ov1_i64(input->value, value); fields[6].type_tag = CDTO_V1_TYPE_I64; fields[6].value = value; fields[6].length = 8;
    ov1_i16(input->weight, weight); fields[7].type_tag = CDTO_V1_TYPE_I16; fields[7].value = weight; fields[7].length = 2;
    fields[8].type_tag = CDTO_V1_TYPE_I8; fields[8].value = (const uint8_t *)&input->type; fields[8].length = 1;
    fields[9].type_tag = CDTO_V1_TYPE_I8; fields[9].value = (const uint8_t *)&input->adjustment; fields[9].length = 1;
    ov1_i16(input->shotsmax, shotsmax); fields[10].type_tag = CDTO_V1_TYPE_I16; fields[10].value = shotsmax; fields[10].length = 2;
    ov1_i16(input->shotscur, shotscur); fields[11].type_tag = CDTO_V1_TYPE_I16; fields[11].value = shotscur; fields[11].length = 2;
    ov1_i16(input->ndice, ndice); fields[12].type_tag = CDTO_V1_TYPE_I16; fields[12].value = ndice; fields[12].length = 2;
    ov1_i16(input->sdice, sdice); fields[13].type_tag = CDTO_V1_TYPE_I16; fields[13].value = sdice; fields[13].length = 2;
    ov1_i16(input->pdice, pdice); fields[14].type_tag = CDTO_V1_TYPE_I16; fields[14].value = pdice; fields[14].length = 2;
    fields[15].type_tag = CDTO_V1_TYPE_I8; fields[15].value = (const uint8_t *)&input->armor; fields[15].length = 1;
    fields[16].type_tag = CDTO_V1_TYPE_I8; fields[16].value = (const uint8_t *)&input->wearflag; fields[16].length = 1;
    fields[17].type_tag = CDTO_V1_TYPE_I8; fields[17].value = (const uint8_t *)&input->magicpower; fields[17].length = 1;
    fields[18].type_tag = CDTO_V1_TYPE_I8; fields[18].value = (const uint8_t *)&input->magicrealm; fields[18].length = 1;
    ov1_i16(input->special, special); fields[19].type_tag = CDTO_V1_TYPE_I16; fields[19].value = special; fields[19].length = 2;
    fields[20].value = (const uint8_t *)input->flags; fields[20].length = sizeof(input->flags);
    fields[21].type_tag = CDTO_V1_TYPE_I8; fields[21].value = (const uint8_t *)&input->questnum; fields[21].length = 1;
    record.kind = CDTO_V1_KIND_OBJECT; record.fields = fields; record.field_count = OBJECT_V1_FIELD_COUNT;
    return cdto_v1_encode(&record, wire, wire_length);
}

int object_v1_decode_flat(wire, wire_length, output)
const uint8_t *wire;
size_t wire_length;
object *output;
{
    static const unsigned char types[OBJECT_V1_FIELD_COUNT] = {
        CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
        CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
        CDTO_V1_TYPE_I64, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I8,
        CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16,
        CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16,
        CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8,
        CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_BYTES,
        CDTO_V1_TYPE_I8 };
    static const unsigned char lengths[OBJECT_V1_FIELD_COUNT] = {
        80,80,20,20,20,80,8,2,1,1,2,2,2,2,2,1,1,1,1,2,8,1 };
    cdto_v1_decoded_record record;
    object parsed;
    unsigned int i;
    int status;
    memset(&record, 0, sizeof(record));
    if(!wire || !output) return CDTO_V1_INVALID_ARGUMENT;
    status = cdto_v1_decode(wire, wire_length, &record);
    if(status != CDTO_V1_OK) return status;
    if(record.kind != CDTO_V1_KIND_OBJECT || record.field_count != OBJECT_V1_FIELD_COUNT) {
        cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_ARGUMENT;
    }
    for(i = 0; i < OBJECT_V1_FIELD_COUNT; ++i)
        if(record.fields[i].id != i + 1 || record.fields[i].type_tag != types[i] ||
           record.fields[i].length != lengths[i]) {
            cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH;
        }
    memset(&parsed, 0, sizeof(parsed));
    memcpy(parsed.name, record.fields[0].value, sizeof(parsed.name));
    memcpy(parsed.description, record.fields[1].value, sizeof(parsed.description));
    memcpy(parsed.key[0], record.fields[2].value, sizeof(parsed.key[0]));
    memcpy(parsed.key[1], record.fields[3].value, sizeof(parsed.key[1]));
    memcpy(parsed.key[2], record.fields[4].value, sizeof(parsed.key[2]));
    memcpy(parsed.use_output, record.fields[5].value, sizeof(parsed.use_output));
    if(ov1_get_i64(record.fields[6].value, &parsed.value) < 0) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH; }
    parsed.weight = ov1_get_i16(record.fields[7].value); parsed.type = (char)record.fields[8].value[0];
    parsed.adjustment = (char)record.fields[9].value[0]; parsed.shotsmax = ov1_get_i16(record.fields[10].value);
    parsed.shotscur = ov1_get_i16(record.fields[11].value); parsed.ndice = ov1_get_i16(record.fields[12].value);
    parsed.sdice = ov1_get_i16(record.fields[13].value); parsed.pdice = ov1_get_i16(record.fields[14].value);
    parsed.armor = (char)record.fields[15].value[0]; parsed.wearflag = (char)record.fields[16].value[0];
    parsed.magicpower = (char)record.fields[17].value[0]; parsed.magicrealm = (char)record.fields[18].value[0];
    parsed.special = ov1_get_i16(record.fields[19].value); memcpy(parsed.flags, record.fields[20].value, sizeof(parsed.flags));
    parsed.questnum = (char)record.fields[21].value[0];
    if(parsed.shotscur > parsed.shotsmax) {
        cdto_v1_free_decoded(&record);
        return CDTO_V1_INVALID_FIELD_LENGTH;
    }
    cdto_v1_free_decoded(&record);
    *output = parsed;
    return CDTO_V1_OK;
}

int object_v1_equal_flat(left, right)
const object *left;
const object *right;
{
    if(!ov1_detached(left) || !ov1_detached(right)) return 0;
    return !memcmp(left->name, right->name, sizeof(left->name)) && !memcmp(left->description, right->description, sizeof(left->description)) &&
           !memcmp(left->key, right->key, sizeof(left->key)) && !memcmp(left->use_output, right->use_output, sizeof(left->use_output)) &&
           left->value == right->value && left->weight == right->weight && left->type == right->type && left->adjustment == right->adjustment &&
           left->shotsmax == right->shotsmax && left->shotscur == right->shotscur && left->ndice == right->ndice && left->sdice == right->sdice &&
           left->pdice == right->pdice && left->armor == right->armor && left->wearflag == right->wearflag && left->magicpower == right->magicpower &&
           left->magicrealm == right->magicrealm && left->special == right->special && !memcmp(left->flags, right->flags, sizeof(left->flags)) && left->questnum == right->questnum;
}
