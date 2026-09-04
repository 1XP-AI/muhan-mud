#include "player_snapshot_v1.h"

#include "cdto_v1.h"
#include "object_graph_v1.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

typedef char ps_assert_char_bit[(CHAR_BIT == 8) ? 1 : -1];
typedef char ps_assert_short_size[(sizeof(short) == 2U) ? 1 : -1];
typedef char ps_assert_long_size[(sizeof(long) <= sizeof(int64_t)) ? 1 : -1];
typedef char ps_assert_player_list_limit[
    (PLAYER_SNAPSHOT_V1_MAX_LIST_ITEMS ==
     OBJECT_GRAPH_V1_PLAYER_MAX_LIST_ITEMS) ? 1 : -1];

int
player_snapshot_v1_native_abi_supported(size_t char_bits, size_t short_bits,
    size_t long_bits, int long_covers_i64, int player_wire_value)
{
    return char_bits == 8U && short_bits == 16U && long_bits == 64U &&
        long_covers_i64 && player_wire_value == 0;
}

#define PS_FIELDS PLAYER_SNAPSHOT_V1_FIELD_COUNT
#define PS_LIST_LIMIT PLAYER_SNAPSHOT_V1_MAX_LIST_ITEMS

static const uint8_t ps_types[PS_FIELDS] = {
    CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
    CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
    CDTO_V1_TYPE_U8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8,
    CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I16,
    CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8,
    CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I16,
    CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16,
    CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_I64,
    CDTO_V1_TYPE_I64, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16,
    CDTO_V1_TYPE_I16, CDTO_V1_TYPE_I16, CDTO_V1_TYPE_BYTES,
    CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
    CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_I8, CDTO_V1_TYPE_BYTES,
    CDTO_V1_TYPE_I16, CDTO_V1_TYPE_BYTES, CDTO_V1_TYPE_BYTES,
    CDTO_V1_TYPE_BYTES
};
static const uint32_t ps_lengths[PS_FIELDS] = {
    80,80,80,20,20,20,1,1,1,1,1,2,1,1,1,1,1,2,2,2,2,1,1,8,8,2,2,2,2,40,32,16,8,16,1,20,2,100,810,0
};

static void ps_u16(uint8_t *p, uint16_t n)
{
    p[0] = (uint8_t)(n >> 8);
    p[1] = (uint8_t)n;
}

static int16_t ps_i16(const uint8_t *p)
{
    uint16_t bits = ((uint16_t)p[0] << 8) | p[1];
    if ((bits & 0x8000U) != 0U)
        return (int16_t)(-1 - (int16_t)(0xffffU - bits));
    return (int16_t)bits;
}

static void ps_i64_put(uint8_t *p, int64_t n)
{
    uint64_t bits = (uint64_t)n;
    int i;
    for (i = 7; i >= 0; --i) {
        p[i] = (uint8_t)bits;
        bits >>= 8;
    }
}

static int64_t ps_i64_get(const uint8_t *p)
{
    uint64_t bits = 0U;
    size_t i;
    for (i = 0U; i < 8U; ++i)
        bits = (bits << 8) | p[i];
    if ((bits & (((uint64_t)1U) << 63)) != 0U)
        return -1 - (int64_t)(UINT64_MAX - bits);
    return (int64_t)bits;
}

#ifdef PLAYER_SNAPSHOT_V1_TESTING
static long ps_fail_after = -1L;
static long ps_allocations;
void player_snapshot_v1_test_fail_after(long allocation_index)
{ ps_fail_after = allocation_index; ps_allocations = 0L; }
void player_snapshot_v1_test_reset_allocator(void)
{ ps_fail_after = -1L; ps_allocations = 0L; }
#endif

static void *ps_calloc(size_t count, size_t size)
{
#ifdef PLAYER_SNAPSHOT_V1_TESTING
    if (ps_fail_after >= 0L && ps_allocations++ >= ps_fail_after)
        return NULL;
#endif
    return calloc(count, size);
}

static int ps_from_long(long input, int64_t *output)
{
#if LONG_MAX > INT64_MAX
    if (input > (long)INT64_MAX || input < (long)INT64_MIN)
        return CDTO_V1_INVALID_ARGUMENT;
#endif
    *output = (int64_t)input;
    return CDTO_V1_OK;
}

static int ps_to_long(int64_t input, long *output)
{
#if LONG_MAX < INT64_MAX
    if (input > (int64_t)LONG_MAX || input < (int64_t)LONG_MIN)
        return CDTO_V1_INVALID_FIELD_LENGTH;
#endif
    *output = (long)input;
    return CDTO_V1_OK;
}

static int ps_fixed_encode(uint8_t *out, const char *in, size_t length)
{
    size_t i;
    for (i = 0U; i < length; ++i) {
        out[i] = (uint8_t)in[i];
        if (in[i] == '\0') {
            ++i;
            while (i < length)
                out[i++] = 0U;
            return CDTO_V1_OK;
        }
    }
    return CDTO_V1_INVALID_ARGUMENT;
}

static int ps_fixed_decode(char *out, const uint8_t *in, size_t length)
{
    size_t i;
    for (i = 0U; i < length && in[i] != 0U; ++i)
        ;
    if (i == length)
        return CDTO_V1_INVALID_FIELD_LENGTH;
    for (; i < length; ++i) {
        if (in[i] != 0U)
            return CDTO_V1_INVALID_FIELD_LENGTH;
    }
    memcpy(out, in, length);
    return CDTO_V1_OK;
}

static void ps_field(cdto_v1_field *f, uint16_t id, uint8_t type,
    const uint8_t *value, uint32_t length)
{
    f->id = id;
    f->type_tag = type;
    f->value = value;
    f->length = length;
}

static int ps_inventory_strings_terminated(const object *value)
{
    return memchr(value->name, 0, sizeof(value->name)) != NULL &&
        memchr(value->description, 0, sizeof(value->description)) != NULL &&
        memchr(value->key[0], 0, sizeof(value->key[0])) != NULL &&
        memchr(value->key[1], 0, sizeof(value->key[1])) != NULL &&
        memchr(value->key[2], 0, sizeof(value->key[2])) != NULL &&
        memchr(value->use_output, 0, sizeof(value->use_output)) != NULL;
}

static int ps_validate_inventory_profile(const otag *roots)
{
    const otag *tag;
    size_t count = 0U;
    int status;

    for (tag = roots; tag != NULL; tag = tag->next_tag) {
        if (++count > PS_LIST_LIMIT)
            return CDTO_V1_SIZE_LIMIT_EXCEEDED;
        if (tag->obj == NULL || !ps_inventory_strings_terminated(tag->obj))
            return CDTO_V1_INVALID_FIELD_LENGTH;
        status = ps_validate_inventory_profile(tag->obj->first_obj);
        if (status != CDTO_V1_OK)
            return status;
    }
    return CDTO_V1_OK;
}

static int ps_validate(const cdto_v1_decoded_record *record)
{
    size_t i;
    if (record->kind != CDTO_V1_KIND_PLAYER_SNAPSHOT || record->field_count != PS_FIELDS)
        return CDTO_V1_INVALID_FIELD_LENGTH;
    for (i = 0U; i < PS_FIELDS; ++i) {
        if (record->fields[i].id != i + 1U || record->fields[i].type_tag != ps_types[i])
            return CDTO_V1_INVALID_FIELD_LENGTH;
        if (i == PS_FIELDS - 1U) {
            if (record->fields[i].length == 0U)
                return CDTO_V1_INVALID_FIELD_LENGTH;
        } else if (record->fields[i].length != ps_lengths[i]) {
            return CDTO_V1_INVALID_FIELD_LENGTH;
        }
    }
    return CDTO_V1_OK;
}

int player_snapshot_v1_encode_loaded(const creature *in, uint8_t **wire,
    size_t *wire_length)
{
    cdto_v1_field f[PS_FIELDS];
    cdto_v1_record record;
    uint8_t fixed[3][80], key[3][20], one[12][1], two[9][2];
    uint8_t exp[8], gold[8], proficiency[40], realm[32], carry[20], rom_num[2];
    uint8_t daily[100], lasttime[810];
    uint8_t *graph = NULL;
    size_t graph_length = 0U, i;
    int64_t value;
    const char *string_value;
    int status;

    if (wire == NULL || wire_length == NULL)
        return CDTO_V1_INVALID_ARGUMENT;
    *wire = NULL;
    *wire_length = 0U;
    if (in == NULL || in->type != PLAYER || in->hpcur > in->hpmax || in->mpcur > in->mpmax)
        return CDTO_V1_INVALID_ARGUMENT;
    for (i = 0U; i < MAXWEAR; ++i) {
        if (in->ready[i] != NULL)
            return CDTO_V1_INVALID_ARGUMENT;
    }
    for (i = 0U; i < 3U; ++i) {
        string_value = i == 0U ? in->name :
            (i == 1U ? in->description : in->talk);
        status = ps_fixed_encode(fixed[i], string_value, 80U);
        if (status != CDTO_V1_OK) return status;
        status = ps_fixed_encode(key[i], in->key[i], 20U);
        if (status != CDTO_V1_OK) return status;
    }
    status = object_graph_v1_encode_player_inventory(in->first_obj, in, &graph, &graph_length);
    if (status != CDTO_V1_OK) return status;
    if (graph_length > UINT32_MAX) {
        status = CDTO_V1_SIZE_LIMIT_EXCEEDED;
        goto done;
    }

    one[0][0] = in->level;
    one[1][0] = (uint8_t)in->type;
    one[2][0] = (uint8_t)in->class;
    one[3][0] = (uint8_t)in->race;
    one[4][0] = (uint8_t)in->numwander;
    ps_u16(two[0], (uint16_t)(int16_t)in->alignment);
    one[5][0] = (uint8_t)in->strength;
    one[6][0] = (uint8_t)in->dexterity;
    one[7][0] = (uint8_t)in->constitution;
    one[8][0] = (uint8_t)in->intelligence;
    one[9][0] = (uint8_t)in->piety;
    ps_u16(two[1], (uint16_t)(int16_t)in->hpmax);
    ps_u16(two[2], (uint16_t)(int16_t)in->hpcur);
    ps_u16(two[3], (uint16_t)(int16_t)in->mpmax);
    ps_u16(two[4], (uint16_t)(int16_t)in->mpcur);
    one[10][0] = (uint8_t)in->armor;
    one[11][0] = (uint8_t)in->thaco;
    status = ps_from_long(in->experience, &value);
    if (status != CDTO_V1_OK) goto done;
    ps_i64_put(exp, value);
    status = ps_from_long(in->gold, &value);
    if (status != CDTO_V1_OK) goto done;
    ps_i64_put(gold, value);
    ps_u16(two[5], (uint16_t)(int16_t)in->ndice);
    ps_u16(two[6], (uint16_t)(int16_t)in->sdice);
    ps_u16(two[7], (uint16_t)(int16_t)in->pdice);
    ps_u16(two[8], (uint16_t)(int16_t)in->special);
    for (i = 0U; i < 5U; ++i) {
        status = ps_from_long(in->proficiency[i], &value);
        if (status != CDTO_V1_OK) goto done;
        ps_i64_put(proficiency + i * 8U, value);
    }
    for (i = 0U; i < 4U; ++i) {
        status = ps_from_long(in->realm[i], &value);
        if (status != CDTO_V1_OK) goto done;
        ps_i64_put(realm + i * 8U, value);
    }
    for (i = 0U; i < 10U; ++i) {
        ps_u16(carry + i * 2U, (uint16_t)(int16_t)in->carry[i]);
        daily[i * 10U] = (uint8_t)in->daily[i].max;
        daily[i * 10U + 1U] = (uint8_t)in->daily[i].cur;
        status = ps_from_long(in->daily[i].ltime, &value);
        if (status != CDTO_V1_OK) goto done;
        ps_i64_put(daily + i * 10U + 2U, value);
    }
    ps_u16(rom_num, (uint16_t)(int16_t)in->rom_num);
    for (i = 0U; i < 45U; ++i) {
        status = ps_from_long(in->lasttime[i].interval, &value);
        if (status != CDTO_V1_OK) goto done;
        ps_i64_put(lasttime + i * 18U, value);
        status = ps_from_long(in->lasttime[i].ltime, &value);
        if (status != CDTO_V1_OK) goto done;
        ps_i64_put(lasttime + i * 18U + 8U, value);
        ps_u16(lasttime + i * 18U + 16U, (uint16_t)(int16_t)in->lasttime[i].misc);
    }
    ps_field(&f[0], 1, CDTO_V1_TYPE_BYTES, fixed[0], 80);
    ps_field(&f[1], 2, CDTO_V1_TYPE_BYTES, fixed[1], 80);
    ps_field(&f[2], 3, CDTO_V1_TYPE_BYTES, fixed[2], 80);
    ps_field(&f[3], 4, CDTO_V1_TYPE_BYTES, key[0], 20);
    ps_field(&f[4], 5, CDTO_V1_TYPE_BYTES, key[1], 20);
    ps_field(&f[5], 6, CDTO_V1_TYPE_BYTES, key[2], 20);
    ps_field(&f[6], 7, CDTO_V1_TYPE_U8, one[0], 1);
    ps_field(&f[7], 8, CDTO_V1_TYPE_I8, one[1], 1);
    ps_field(&f[8], 9, CDTO_V1_TYPE_I8, one[2], 1);
    ps_field(&f[9], 10, CDTO_V1_TYPE_I8, one[3], 1);
    ps_field(&f[10], 11, CDTO_V1_TYPE_I8, one[4], 1);
    ps_field(&f[11], 12, CDTO_V1_TYPE_I16, two[0], 2);
    ps_field(&f[12], 13, CDTO_V1_TYPE_I8, one[5], 1);
    ps_field(&f[13], 14, CDTO_V1_TYPE_I8, one[6], 1);
    ps_field(&f[14], 15, CDTO_V1_TYPE_I8, one[7], 1);
    ps_field(&f[15], 16, CDTO_V1_TYPE_I8, one[8], 1);
    ps_field(&f[16], 17, CDTO_V1_TYPE_I8, one[9], 1);
    ps_field(&f[17], 18, CDTO_V1_TYPE_I16, two[1], 2);
    ps_field(&f[18], 19, CDTO_V1_TYPE_I16, two[2], 2);
    ps_field(&f[19], 20, CDTO_V1_TYPE_I16, two[3], 2);
    ps_field(&f[20], 21, CDTO_V1_TYPE_I16, two[4], 2);
    ps_field(&f[21], 22, CDTO_V1_TYPE_I8, one[10], 1);
    ps_field(&f[22], 23, CDTO_V1_TYPE_I8, one[11], 1);
    ps_field(&f[23], 24, CDTO_V1_TYPE_I64, exp, 8);
    ps_field(&f[24], 25, CDTO_V1_TYPE_I64, gold, 8);
    ps_field(&f[25], 26, CDTO_V1_TYPE_I16, two[5], 2);
    ps_field(&f[26], 27, CDTO_V1_TYPE_I16, two[6], 2);
    ps_field(&f[27], 28, CDTO_V1_TYPE_I16, two[7], 2);
    ps_field(&f[28], 29, CDTO_V1_TYPE_I16, two[8], 2);
    ps_field(&f[29], 30, CDTO_V1_TYPE_BYTES, proficiency, 40);
    ps_field(&f[30], 31, CDTO_V1_TYPE_BYTES, realm, 32);
    ps_field(&f[31], 32, CDTO_V1_TYPE_BYTES,
        (const uint8_t *)in->spells, 16);
    ps_field(&f[32], 33, CDTO_V1_TYPE_BYTES,
        (const uint8_t *)in->flags, 8);
    ps_field(&f[33], 34, CDTO_V1_TYPE_BYTES,
        (const uint8_t *)in->quests, 16);
    ps_field(&f[34], 35, CDTO_V1_TYPE_I8,
        (const uint8_t *)&in->questnum, 1);
    ps_field(&f[35], 36, CDTO_V1_TYPE_BYTES, carry, 20);
    ps_field(&f[36], 37, CDTO_V1_TYPE_I16, rom_num, 2);
    ps_field(&f[37], 38, CDTO_V1_TYPE_BYTES, daily, 100);
    ps_field(&f[38], 39, CDTO_V1_TYPE_BYTES, lasttime, 810);
    ps_field(&f[39], 40, CDTO_V1_TYPE_BYTES, graph,
        (uint32_t)graph_length);
    record.kind = CDTO_V1_KIND_PLAYER_SNAPSHOT;
    record.fields = f;
    record.field_count = PS_FIELDS;
    status = cdto_v1_encode(&record, wire, wire_length);
done:
    cdto_v1_free_wire(graph);
    return status;
}

void player_snapshot_v1_free_clone(creature *value)
{
    if (value != NULL) {
        object_graph_v1_free(value->first_obj);
        memset(value, 0, sizeof(*value));
        free(value);
    }
}

int
player_snapshot_v1_decode_clone(const uint8_t *wire, size_t wire_length,
    creature **out)
{
    cdto_v1_decoded_record r;
    creature *v = NULL;
    otag *roots = NULL, *tag;
    int64_t number;
    size_t i;
    int status;
    if (out == NULL)
        return CDTO_V1_INVALID_ARGUMENT;
    *out = NULL;
    memset(&r, 0, sizeof(r));
    status = cdto_v1_decode(wire, wire_length, &r);
    if (status != CDTO_V1_OK)
        return status;
    status = ps_validate(&r);
    if (status != CDTO_V1_OK)
        goto record_done;
    v = (creature *)ps_calloc(1U, sizeof(*v));
    if (v == NULL) {
        status = CDTO_V1_ALLOCATION_FAILED;
        goto record_done;
    }
    v->fd = -1;
    status = ps_fixed_decode(v->name, r.fields[0].value, 80U);
    if (status != CDTO_V1_OK) goto bad;
    status = ps_fixed_decode(v->description, r.fields[1].value, 80U);
    if (status != CDTO_V1_OK) goto bad;
    status = ps_fixed_decode(v->talk, r.fields[2].value, 80U);
    if (status != CDTO_V1_OK) goto bad;
    for (i = 0U; i < 3U; ++i) {
        status = ps_fixed_decode(v->key[i], r.fields[3U + i].value, 20U);
        if (status != CDTO_V1_OK) goto bad;
    }
    v->level = r.fields[6].value[0];
    if (r.fields[7].value[0] != (uint8_t)PLAYER) {
        status = CDTO_V1_INVALID_FIELD_LENGTH;
        goto bad;
    }
    v->type = PLAYER;
    *((unsigned char *)&v->class) = r.fields[8].value[0];
    *((unsigned char *)&v->race) = r.fields[9].value[0];
    *((unsigned char *)&v->numwander) = r.fields[10].value[0];
    v->alignment = (short)ps_i16(r.fields[11].value);
    *((unsigned char *)&v->strength) = r.fields[12].value[0];
    *((unsigned char *)&v->dexterity) = r.fields[13].value[0];
    *((unsigned char *)&v->constitution) = r.fields[14].value[0];
    *((unsigned char *)&v->intelligence) = r.fields[15].value[0];
    *((unsigned char *)&v->piety) = r.fields[16].value[0];
    v->hpmax = (short)ps_i16(r.fields[17].value);
    v->hpcur = (short)ps_i16(r.fields[18].value);
    v->mpmax = (short)ps_i16(r.fields[19].value);
    v->mpcur = (short)ps_i16(r.fields[20].value);
    if (v->hpcur > v->hpmax || v->mpcur > v->mpmax) {
        status = CDTO_V1_INVALID_FIELD_LENGTH;
        goto bad;
    }
    *((unsigned char *)&v->armor) = r.fields[21].value[0];
    *((unsigned char *)&v->thaco) = r.fields[22].value[0];
    number = ps_i64_get(r.fields[23].value);
    status = ps_to_long(number, &v->experience);
    if (status != CDTO_V1_OK) goto bad;
    number = ps_i64_get(r.fields[24].value);
    status = ps_to_long(number, &v->gold);
    if (status != CDTO_V1_OK) goto bad;
    v->ndice = (short)ps_i16(r.fields[25].value);
    v->sdice = (short)ps_i16(r.fields[26].value);
    v->pdice = (short)ps_i16(r.fields[27].value);
    v->special = (short)ps_i16(r.fields[28].value);
    for (i = 0U; i < 5U; ++i) {
        number = ps_i64_get(r.fields[29].value + i * 8U);
        status = ps_to_long(number, &v->proficiency[i]);
        if (status != CDTO_V1_OK) goto bad;
    }
    for (i = 0U; i < 4U; ++i) {
        number = ps_i64_get(r.fields[30].value + i * 8U);
        status = ps_to_long(number, &v->realm[i]);
        if (status != CDTO_V1_OK) goto bad;
    }
    memcpy(v->spells, r.fields[31].value, 16U);
    memcpy(v->flags, r.fields[32].value, 8U);
    memcpy(v->quests, r.fields[33].value, 16U);
    *((unsigned char *)&v->questnum) = r.fields[34].value[0];
    for (i = 0U; i < 10U; ++i) {
        v->carry[i] = (short)ps_i16(r.fields[35].value + i * 2U);
        *((unsigned char *)&v->daily[i].max) = r.fields[37].value[i * 10U];
        *((unsigned char *)&v->daily[i].cur) =
            r.fields[37].value[i * 10U + 1U];
        number = ps_i64_get(r.fields[37].value + i * 10U + 2U);
        status = ps_to_long(number, &v->daily[i].ltime);
        if (status != CDTO_V1_OK) goto bad;
    }
    v->rom_num = (short)ps_i16(r.fields[36].value);
    for (i = 0U; i < 45U; ++i) {
        number = ps_i64_get(r.fields[38].value + i * 18U);
        status = ps_to_long(number, &v->lasttime[i].interval);
        if (status != CDTO_V1_OK) goto bad;
        number = ps_i64_get(r.fields[38].value + i * 18U + 8U);
        status = ps_to_long(number, &v->lasttime[i].ltime);
        if (status != CDTO_V1_OK) goto bad;
        v->lasttime[i].misc =
            (short)ps_i16(r.fields[38].value + i * 18U + 16U);
    }
    status = object_graph_v1_decode(r.fields[39].value,
        r.fields[39].length, &roots);
    if (status != CDTO_V1_OK) goto bad;
    status = ps_validate_inventory_profile(roots);
    if (status != CDTO_V1_OK) goto roots_bad;
    v->first_obj = roots;
    for (tag = roots; tag != NULL; tag = tag->next_tag)
        tag->obj->parent_crt = v;
    cdto_v1_free_decoded(&r);
    *out = v;
    return CDTO_V1_OK;
roots_bad:
    object_graph_v1_free(roots);
bad:
    player_snapshot_v1_free_clone(v);
record_done:
    cdto_v1_free_decoded(&r);
    return status;
}

int player_snapshot_v1_equal_persisted(const creature *left, const creature *right)
{
    uint8_t *a = NULL, *b = NULL;
    size_t al = 0U, bl = 0U;
    int equal;

    if (player_snapshot_v1_encode_loaded(left, &a, &al) != CDTO_V1_OK ||
        player_snapshot_v1_encode_loaded(right, &b, &bl) != CDTO_V1_OK)
        equal = 0;
    else
        equal = al == bl && memcmp(a, b, al) == 0;
    cdto_v1_free_wire(a);
    cdto_v1_free_wire(b);
    return equal;
}
