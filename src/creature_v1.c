/* Clone-only CreatureV1 projection.  It is intentionally not linked into
 * legacy files1.c persistence or the running MUD. */
#include "creature_v1.h"
#include "cdto_v1.h"

#include <limits.h>
#include <string.h>

#define CV1_DAILY_BYTES 100U

static void cv1_i16(value, out)
short value;
unsigned char out[2];
{
    unsigned short bits = (unsigned short)value;
    out[0] = (unsigned char)(bits >> 8); out[1] = (unsigned char)bits;
}

static short cv1_get_i16(value)
const unsigned char *value;
{
    unsigned short bits = ((unsigned short)value[0] << 8) | value[1];
    if(bits & 0x8000U) return (short)(-1 - (short)(0xffffU - bits));
    return (short)bits;
}

static void cv1_i64(value, out)
long value;
unsigned char out[8];
{
    uint64_t bits = (uint64_t)(int64_t)value;
    unsigned int i;
    for(i = 0; i < 8; ++i) out[i] = (unsigned char)(bits >> (56 - 8 * i));
}

static int cv1_get_i64(value, out)
const unsigned char *value;
long *out;
{
    uint64_t bits = 0;
    int64_t parsed;
    unsigned int i;
    for(i = 0; i < 8; ++i) bits = (bits << 8) | value[i];
    if(bits & (((uint64_t)1) << 63)) parsed = -1 - (int64_t)(UINT64_MAX - bits);
    else parsed = (int64_t)bits;
    if(parsed < (int64_t)LONG_MIN || parsed > (int64_t)LONG_MAX) return -1;
    *out = (long)parsed;
    return 0;
}

/* Fixed legacy string arrays are canonical only when every byte after their
 * first NUL is zero.  Full-width no-NUL byte strings are also allowed. */
static int cv1_fixed_string(value, length)
const char *value;
size_t length;
{
    size_t i;
    int terminated = 0;
    for(i = 0; i < length; ++i) {
        if(terminated && value[i]) return 0;
        if(!value[i]) terminated = 1;
    }
    return 1;
}

static int cv1_zero(value, length)
const char *value;
size_t length;
{
    size_t i;
    for(i = 0; i < length; ++i) if(value[i]) return 0;
    return 1;
}

static int cv1_supported(input)
const creature *input;
{
    unsigned int i;
    if(!input || !cv1_zero(input->talk, sizeof(input->talk))) return 0;
    if(input->first_obj || input->first_fol || input->first_enm || input->first_tlk ||
       input->following || input->parent_rom) return 0;
    for(i = 0; i < MAXWEAR; ++i) if(input->ready[i]) return 0;
    return 1;
}

static int cv1_canonical(input)
const creature *input;
{
    unsigned int i;
    if(!cv1_fixed_string(input->name, sizeof(input->name)) ||
       !cv1_fixed_string(input->description, sizeof(input->description))) return 0;
    for(i = 0; i < 3; ++i) if(!cv1_fixed_string(input->key[i], sizeof(input->key[i]))) return 0;
    if(input->hpcur > input->hpmax || input->mpcur > input->mpmax) return 0;
    for(i = 0; i < 10; ++i)
        if((unsigned char)input->daily[i].cur > (unsigned char)input->daily[i].max) return 0;
    return 1;
}

static void cv1_pack_i64(values, count, out)
const long *values;
unsigned int count;
unsigned char *out;
{
    unsigned int i;
    for(i = 0; i < count; ++i) cv1_i64(values[i], out + i * 8U);
}

static int cv1_unpack_i64(value, count, out)
const unsigned char *value;
unsigned int count;
long *out;
{
    unsigned int i;
    for(i = 0; i < count; ++i) if(cv1_get_i64(value + i * 8U, &out[i]) < 0) return -1;
    return 0;
}

static int cv1_daily_equal(left, right)
const creature *left;
const creature *right;
{
    unsigned int i;
    for(i = 0; i < 10; ++i)
        if(left->daily[i].max != right->daily[i].max || left->daily[i].cur != right->daily[i].cur ||
           left->daily[i].ltime != right->daily[i].ltime) return 0;
    return 1;
}

int creature_v1_encode_flat(input, wire, wire_length)
const creature *input;
uint8_t **wire;
size_t *wire_length;
{
    cdto_v1_field fields[CREATURE_V1_FIELD_COUNT];
    unsigned char alignment[2], hpmax[2], hpcur[2], mpmax[2], mpcur[2], experience[8], gold[8];
    unsigned char ndice[2], sdice[2], pdice[2], special[2], proficiency[40], realm[32], carry[20], rom_num[2], daily[CV1_DAILY_BYTES];
    cdto_v1_record record;
    unsigned int i;

    if(wire) *wire = 0;
    if(wire_length) *wire_length = 0;
    if(!wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    if(!cv1_supported(input)) return CREATURE_V1_UNSUPPORTED_STATE;
    if(!cv1_canonical(input)) return CDTO_V1_INVALID_ARGUMENT;
    for(i = 0; i < CREATURE_V1_FIELD_COUNT; ++i) {
        fields[i].id = (uint16_t)(i + 1); fields[i].type_tag = CDTO_V1_TYPE_BYTES;
        fields[i].value = 0; fields[i].length = 0;
    }
    fields[0].value = (const uint8_t *)input->name; fields[0].length = sizeof(input->name);
    fields[1].value = (const uint8_t *)input->description; fields[1].length = sizeof(input->description);
    fields[2].value = (const uint8_t *)input->key[0]; fields[2].length = sizeof(input->key[0]);
    fields[3].value = (const uint8_t *)input->key[1]; fields[3].length = sizeof(input->key[1]);
    fields[4].value = (const uint8_t *)input->key[2]; fields[4].length = sizeof(input->key[2]);
    fields[5].type_tag = CDTO_V1_TYPE_U8; fields[5].value = &input->level; fields[5].length = 1;
    fields[6].type_tag = CDTO_V1_TYPE_I8; fields[6].value = (const uint8_t *)&input->type; fields[6].length = 1;
    fields[7].type_tag = CDTO_V1_TYPE_I8; fields[7].value = (const uint8_t *)&input->class; fields[7].length = 1;
    fields[8].type_tag = CDTO_V1_TYPE_I8; fields[8].value = (const uint8_t *)&input->race; fields[8].length = 1;
    fields[9].type_tag = CDTO_V1_TYPE_I8; fields[9].value = (const uint8_t *)&input->numwander; fields[9].length = 1;
    cv1_i16(input->alignment, alignment); fields[10].type_tag = CDTO_V1_TYPE_I16; fields[10].value = alignment; fields[10].length = 2;
    fields[11].type_tag = CDTO_V1_TYPE_I8; fields[11].value = (const uint8_t *)&input->strength; fields[11].length = 1;
    fields[12].type_tag = CDTO_V1_TYPE_I8; fields[12].value = (const uint8_t *)&input->dexterity; fields[12].length = 1;
    fields[13].type_tag = CDTO_V1_TYPE_I8; fields[13].value = (const uint8_t *)&input->constitution; fields[13].length = 1;
    fields[14].type_tag = CDTO_V1_TYPE_I8; fields[14].value = (const uint8_t *)&input->intelligence; fields[14].length = 1;
    fields[15].type_tag = CDTO_V1_TYPE_I8; fields[15].value = (const uint8_t *)&input->piety; fields[15].length = 1;
    cv1_i16(input->hpmax, hpmax); fields[16].type_tag = CDTO_V1_TYPE_I16; fields[16].value = hpmax; fields[16].length = 2;
    cv1_i16(input->hpcur, hpcur); fields[17].type_tag = CDTO_V1_TYPE_I16; fields[17].value = hpcur; fields[17].length = 2;
    cv1_i16(input->mpmax, mpmax); fields[18].type_tag = CDTO_V1_TYPE_I16; fields[18].value = mpmax; fields[18].length = 2;
    cv1_i16(input->mpcur, mpcur); fields[19].type_tag = CDTO_V1_TYPE_I16; fields[19].value = mpcur; fields[19].length = 2;
    fields[20].type_tag = CDTO_V1_TYPE_I8; fields[20].value = (const uint8_t *)&input->armor; fields[20].length = 1;
    fields[21].type_tag = CDTO_V1_TYPE_I8; fields[21].value = (const uint8_t *)&input->thaco; fields[21].length = 1;
    cv1_i64(input->experience, experience); fields[22].type_tag = CDTO_V1_TYPE_I64; fields[22].value = experience; fields[22].length = 8;
    cv1_i64(input->gold, gold); fields[23].type_tag = CDTO_V1_TYPE_I64; fields[23].value = gold; fields[23].length = 8;
    cv1_i16(input->ndice, ndice); fields[24].type_tag = CDTO_V1_TYPE_I16; fields[24].value = ndice; fields[24].length = 2;
    cv1_i16(input->sdice, sdice); fields[25].type_tag = CDTO_V1_TYPE_I16; fields[25].value = sdice; fields[25].length = 2;
    cv1_i16(input->pdice, pdice); fields[26].type_tag = CDTO_V1_TYPE_I16; fields[26].value = pdice; fields[26].length = 2;
    cv1_i16(input->special, special); fields[27].type_tag = CDTO_V1_TYPE_I16; fields[27].value = special; fields[27].length = 2;
    cv1_pack_i64(input->proficiency, 5, proficiency); fields[28].value = proficiency; fields[28].length = sizeof(proficiency);
    cv1_pack_i64(input->realm, 4, realm); fields[29].value = realm; fields[29].length = sizeof(realm);
    fields[30].value = (const uint8_t *)input->spells; fields[30].length = sizeof(input->spells);
    fields[31].value = (const uint8_t *)input->flags; fields[31].length = sizeof(input->flags);
    fields[32].value = (const uint8_t *)input->quests; fields[32].length = sizeof(input->quests);
    fields[33].type_tag = CDTO_V1_TYPE_I8; fields[33].value = (const uint8_t *)&input->questnum; fields[33].length = 1;
    for(i = 0; i < 10; ++i) cv1_i16(input->carry[i], carry + i * 2U);
    fields[34].value = carry; fields[34].length = sizeof(carry);
    cv1_i16(input->rom_num, rom_num); fields[35].type_tag = CDTO_V1_TYPE_I16; fields[35].value = rom_num; fields[35].length = 2;
    for(i = 0; i < 10; ++i) { daily[i * 10U] = (unsigned char)input->daily[i].max; daily[i * 10U + 1] = (unsigned char)input->daily[i].cur; cv1_i64(input->daily[i].ltime, daily + i * 10U + 2); }
    fields[36].value = daily; fields[36].length = sizeof(daily);
    record.kind = CDTO_V1_KIND_CREATURE; record.fields = fields; record.field_count = CREATURE_V1_FIELD_COUNT;
    return cdto_v1_encode(&record, wire, wire_length);
}

int creature_v1_decode_flat(wire, wire_length, output)
const uint8_t *wire;
size_t wire_length;
creature *output;
{
    static const unsigned char types[CREATURE_V1_FIELD_COUNT] = {
        CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
        CDTO_V1_TYPE_U8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I16,
        CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8,
        CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8,
        CDTO_V1_TYPE_I64, CDTO_V1_TYPE_I64, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16,
        CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
        CDTO_V1_TYPE_I8, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_BYTES };
    static const unsigned short lengths[CREATURE_V1_FIELD_COUNT] = {
        80,80,20,20,20,1,1,1,1,1,2,1,1,1,1,1,2,2,2,2,1,1,8,8,2,2,2,2,40,32,16,8,16,1,20,2,CV1_DAILY_BYTES };
    cdto_v1_decoded_record record;
    creature parsed;
    unsigned int i;
    int status;
    memset(&record, 0, sizeof(record));
    if(!wire || !output) return CDTO_V1_INVALID_ARGUMENT;
    status = cdto_v1_decode(wire, wire_length, &record);
    if(status != CDTO_V1_OK) return status;
    if(record.kind != CDTO_V1_KIND_CREATURE || record.field_count != CREATURE_V1_FIELD_COUNT) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_ARGUMENT; }
    for(i = 0; i < CREATURE_V1_FIELD_COUNT; ++i)
        if(record.fields[i].id != i + 1 || record.fields[i].type_tag != types[i] || record.fields[i].length != lengths[i]) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH; }
    memset(&parsed, 0, sizeof(parsed)); parsed.fd = -1;
    memcpy(parsed.name, record.fields[0].value, sizeof(parsed.name)); memcpy(parsed.description, record.fields[1].value, sizeof(parsed.description));
    memcpy(parsed.key[0], record.fields[2].value, sizeof(parsed.key[0])); memcpy(parsed.key[1], record.fields[3].value, sizeof(parsed.key[1])); memcpy(parsed.key[2], record.fields[4].value, sizeof(parsed.key[2]));
    parsed.level = record.fields[5].value[0]; *((unsigned char *)&parsed.type) = record.fields[6].value[0]; *((unsigned char *)&parsed.class) = record.fields[7].value[0]; *((unsigned char *)&parsed.race) = record.fields[8].value[0]; *((unsigned char *)&parsed.numwander) = record.fields[9].value[0]; parsed.alignment = cv1_get_i16(record.fields[10].value);
    *((unsigned char *)&parsed.strength) = record.fields[11].value[0]; *((unsigned char *)&parsed.dexterity) = record.fields[12].value[0]; *((unsigned char *)&parsed.constitution) = record.fields[13].value[0]; *((unsigned char *)&parsed.intelligence) = record.fields[14].value[0]; *((unsigned char *)&parsed.piety) = record.fields[15].value[0];
    parsed.hpmax = cv1_get_i16(record.fields[16].value); parsed.hpcur = cv1_get_i16(record.fields[17].value); parsed.mpmax = cv1_get_i16(record.fields[18].value); parsed.mpcur = cv1_get_i16(record.fields[19].value); *((unsigned char *)&parsed.armor) = record.fields[20].value[0]; *((unsigned char *)&parsed.thaco) = record.fields[21].value[0];
    if(cv1_get_i64(record.fields[22].value, &parsed.experience) < 0 || cv1_get_i64(record.fields[23].value, &parsed.gold) < 0 || cv1_unpack_i64(record.fields[28].value, 5, parsed.proficiency) < 0 || cv1_unpack_i64(record.fields[29].value, 4, parsed.realm) < 0) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH; }
    parsed.ndice = cv1_get_i16(record.fields[24].value); parsed.sdice = cv1_get_i16(record.fields[25].value); parsed.pdice = cv1_get_i16(record.fields[26].value); parsed.special = cv1_get_i16(record.fields[27].value);
    memcpy(parsed.spells, record.fields[30].value, sizeof(parsed.spells)); memcpy(parsed.flags, record.fields[31].value, sizeof(parsed.flags)); memcpy(parsed.quests, record.fields[32].value, sizeof(parsed.quests)); *((unsigned char *)&parsed.questnum) = record.fields[33].value[0];
    for(i = 0; i < 10; ++i) parsed.carry[i] = cv1_get_i16(record.fields[34].value + i * 2U);
    parsed.rom_num = cv1_get_i16(record.fields[35].value);
    for(i = 0; i < 10; ++i) { *((unsigned char *)&parsed.daily[i].max) = record.fields[36].value[i * 10U]; *((unsigned char *)&parsed.daily[i].cur) = record.fields[36].value[i * 10U + 1]; if(cv1_get_i64(record.fields[36].value + i * 10U + 2, &parsed.daily[i].ltime) < 0) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH; } }
    if(!cv1_canonical(&parsed)) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH; }
    cdto_v1_free_decoded(&record);
    *output = parsed;
    return CDTO_V1_OK;
}

int creature_v1_equal_safe(left, right)
const creature *left;
const creature *right;
{
    if(!cv1_supported(left) || !cv1_supported(right) || !cv1_canonical(left) || !cv1_canonical(right)) return 0;
    return !memcmp(left->name, right->name, sizeof(left->name)) && !memcmp(left->description, right->description, sizeof(left->description)) && !memcmp(left->key, right->key, sizeof(left->key)) &&
           left->level == right->level && left->type == right->type && left->class == right->class && left->race == right->race && left->numwander == right->numwander && left->alignment == right->alignment && left->strength == right->strength && left->dexterity == right->dexterity && left->constitution == right->constitution && left->intelligence == right->intelligence && left->piety == right->piety && left->hpmax == right->hpmax && left->hpcur == right->hpcur && left->mpmax == right->mpmax && left->mpcur == right->mpcur && left->armor == right->armor && left->thaco == right->thaco && left->experience == right->experience && left->gold == right->gold && left->ndice == right->ndice && left->sdice == right->sdice && left->pdice == right->pdice && left->special == right->special && !memcmp(left->proficiency, right->proficiency, sizeof(left->proficiency)) && !memcmp(left->realm, right->realm, sizeof(left->realm)) && !memcmp(left->spells, right->spells, sizeof(left->spells)) && !memcmp(left->flags, right->flags, sizeof(left->flags)) && !memcmp(left->quests, right->quests, sizeof(left->quests)) && left->questnum == right->questnum && !memcmp(left->carry, right->carry, sizeof(left->carry)) && left->rom_num == right->rom_num && cv1_daily_equal(left, right);
}
