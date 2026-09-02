/* Synthetic-only ObjectGraphV1 codec.  It turns a detached native object
 * forest into explicit portable values, never into a native struct image. */
#include "object_graph_v1.h"
#include "cdto_v1.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

typedef struct ogv1_build {
    const object **objects;
    int32_t *parents;
    uint32_t *siblings;
    uint32_t count;
} ogv1_build;

/* Compile only on the ABI subset the explicit scalar codec supports.  The
 * legacy struct itself is never serialized, but its logical char/short/long
 * fields must have these representable widths. */
typedef char ogv1_assert_char_bits[(CHAR_BIT == 8) ? 1 : -1];
typedef char ogv1_assert_signed_char[(CHAR_MIN < 0) ? 1 : -1];
typedef char ogv1_assert_short_width[(sizeof(short) == 2) ? 1 : -1];
typedef char ogv1_assert_uint32_width[(sizeof(uint32_t) == 4) ? 1 : -1];
typedef char ogv1_assert_int32_width[(sizeof(int32_t) == 4) ? 1 : -1];
typedef char ogv1_assert_uint64_width[(sizeof(uint64_t) == 8) ? 1 : -1];
typedef char ogv1_assert_int64_width[(sizeof(int64_t) == 8) ? 1 : -1];
typedef char ogv1_assert_long_range[(sizeof(long) <= sizeof(int64_t) &&
                                     LONG_MAX <= INT64_MAX && LONG_MIN >= INT64_MIN) ? 1 : -1];

#ifdef OBJECT_GRAPH_V1_TESTING
static long ogv1_fail_after = -1;

void object_graph_v1_test_fail_after(allocation_index)
long allocation_index;
{
    ogv1_fail_after = allocation_index;
}

void object_graph_v1_test_reset_allocator(void)
{
    ogv1_fail_after = -1;
}

static int ogv1_allocation_fails(void)
{
    if(ogv1_fail_after < 0) return 0;
    if(ogv1_fail_after == 0) return 1;
    --ogv1_fail_after;
    return 0;
}
#else
static int ogv1_allocation_fails(void)
{
    return 0;
}
#endif

static void *ogv1_malloc(size)
size_t size;
{
    if(ogv1_allocation_fails()) return 0;
    return malloc(size);
}

static void *ogv1_calloc(count, size)
size_t count, size;
{
    if(ogv1_allocation_fails()) return 0;
    return calloc(count, size);
}

static void ogv1_put16(out, value)
unsigned char *out;
short value;
{
    unsigned short bits = (unsigned short)value;
    out[0] = (unsigned char)(bits >> 8); out[1] = (unsigned char)bits;
}

static short ogv1_get16(value)
const unsigned char *value;
{
    return (short)(((unsigned short)value[0] << 8) | value[1]);
}

static void ogv1_put32(out, value)
unsigned char *out;
uint32_t value;
{
    out[0] = (unsigned char)(value >> 24); out[1] = (unsigned char)(value >> 16);
    out[2] = (unsigned char)(value >> 8); out[3] = (unsigned char)value;
}

static uint32_t ogv1_get32(value)
const unsigned char *value;
{
    return ((uint32_t)value[0] << 24) | ((uint32_t)value[1] << 16) |
           ((uint32_t)value[2] << 8) | value[3];
}

static void ogv1_put64(out, value)
unsigned char *out;
long value;
{
    uint64_t bits = (uint64_t)(int64_t)value;
    unsigned int i;
    for(i = 0; i < 8; ++i) out[i] = (unsigned char)(bits >> (56 - 8 * i));
}

static int ogv1_get64(value, output)
const unsigned char *value;
long *output;
{
    uint64_t bits = 0;
    int64_t parsed;
    unsigned int i;
    for(i = 0; i < 8; ++i) bits = (bits << 8) | value[i];
    parsed = (int64_t)bits;
    if(parsed < (int64_t)LONG_MIN || parsed > (int64_t)LONG_MAX) return 0;
    *output = (long)parsed;
    return 1;
}

/* Fixed C arrays are logical strings in ObjectGraphV1.  Requiring every byte
 * after the first NUL to be zero prevents uninitialized tail/padding data from
 * becoming part of a canonical digest. */
static int ogv1_fixed(value, length)
const char *value;
size_t length;
{
    size_t i;
    int terminated = 0;
    for(i = 0; i < length; ++i) {
        if(terminated && value[i] != 0) return 0;
        if(value[i] == 0) terminated = 1;
    }
    return 1;
}

static int ogv1_has_nul(value, length)
const char *value;
size_t length;
{
    return memchr(value, 0, length) != 0;
}

static void ogv1_copy_fixed_canonical(destination, source, length)
unsigned char *destination;
const char *source;
size_t length;
{
    size_t index;
    int terminated;

    terminated = 0;
    for(index = 0; index < length; ++index) {
        if(terminated) destination[index] = 0;
        else {
            destination[index] = (unsigned char)source[index];
            if(!source[index]) terminated = 1;
        }
    }
}

static int ogv1_object_ok(value, parent)
const object *value;
const object *parent;
{
    return value && value->parent_obj == parent && !value->parent_rom && !value->parent_crt &&
           value->shotscur <= value->shotsmax &&
           ogv1_fixed(value->name, sizeof(value->name)) &&
           ogv1_fixed(value->description, sizeof(value->description)) &&
           ogv1_fixed(value->key[0], sizeof(value->key[0])) &&
           ogv1_fixed(value->key[1], sizeof(value->key[1])) &&
           ogv1_fixed(value->key[2], sizeof(value->key[2])) &&
           ogv1_fixed(value->use_output, sizeof(value->use_output));
}

static int ogv1_seen(build, value)
const ogv1_build *build;
const object *value;
{
    uint32_t i;
    for(i = 0; i < build->count; ++i) if(build->objects[i] == value) return 1;
    return 0;
}

static int ogv1_collect_list(build, tag, parent, parent_index, depth)
ogv1_build *build;
const otag *tag;
const object *parent;
int32_t parent_index;
uint32_t depth;
{
    uint32_t sibling = 0, index;
    int status;
    if(!tag) return CDTO_V1_OK;
    if(depth > OBJECT_GRAPH_V1_MAX_DEPTH) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
    while(tag) {
        if(!tag->obj || ogv1_seen(build, tag->obj)) return OBJECT_GRAPH_V1_INVALID_GRAPH;
        if(!ogv1_object_ok(tag->obj, parent)) return OBJECT_GRAPH_V1_INVALID_GRAPH;
        if(build->count == OBJECT_GRAPH_V1_MAX_NODES) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
        index = build->count++;
        build->objects[index] = tag->obj;
        build->parents[index] = parent_index;
        build->siblings[index] = sibling++;
        status = ogv1_collect_list(build, tag->obj->first_obj, tag->obj, (int32_t)index, depth + 1);
        if(status != CDTO_V1_OK) return status;
        tag = tag->next_tag;
    }
    return CDTO_V1_OK;
}

static int ogv1_player_object_ok(value, parent, owner)
const object *value;
const object *parent;
const creature *owner;
{
    if(!value || value->parent_obj != parent || value->parent_rom ||
       value->shotscur > value->shotsmax) return 0;
    if(parent) {
        if(value->parent_crt) return 0;
    } else if(value->parent_crt != owner) return 0;
    return ogv1_has_nul(value->name, sizeof(value->name)) &&
           ogv1_has_nul(value->description, sizeof(value->description)) &&
           ogv1_has_nul(value->key[0], sizeof(value->key[0])) &&
           ogv1_has_nul(value->key[1], sizeof(value->key[1])) &&
           ogv1_has_nul(value->key[2], sizeof(value->key[2])) &&
           ogv1_has_nul(value->use_output, sizeof(value->use_output));
}

static int ogv1_collect_player_list(build, tag, parent, parent_index, depth, owner)
ogv1_build *build;
const otag *tag;
const object *parent;
int32_t parent_index;
uint32_t depth;
const creature *owner;
{
    uint32_t sibling, index;
    int status;

    if(!tag) return CDTO_V1_OK;
    if(depth > OBJECT_GRAPH_V1_MAX_DEPTH) return CDTO_V1_SIZE_LIMIT_EXCEEDED;
    sibling = 0;
    while(tag) {
        if(!tag->obj || ogv1_seen(build, tag->obj) ||
           !ogv1_player_object_ok(tag->obj, parent, owner))
            return OBJECT_GRAPH_V1_INVALID_GRAPH;
        if(sibling == OBJECT_GRAPH_V1_PLAYER_MAX_LIST_ITEMS)
            return CDTO_V1_SIZE_LIMIT_EXCEEDED;
        if(build->count == OBJECT_GRAPH_V1_MAX_NODES)
            return CDTO_V1_SIZE_LIMIT_EXCEEDED;
        index = build->count++;
        build->objects[index] = tag->obj;
        build->parents[index] = parent_index;
        build->siblings[index] = sibling++;
        status = ogv1_collect_player_list(build, tag->obj->first_obj, tag->obj,
            (int32_t)index, depth + 1, owner);
        if(status != CDTO_V1_OK) return status;
        tag = tag->next_tag;
    }
    return CDTO_V1_OK;
}

static void ogv1_node(out, index, parent, sibling, value)
unsigned char out[OBJECT_GRAPH_V1_NODE_LENGTH];
uint32_t index;
int32_t parent;
uint32_t sibling;
const object *value;
{
    unsigned char *cursor = out;
    ogv1_put32(cursor, index); cursor += 4;
    ogv1_put32(cursor, (uint32_t)parent); cursor += 4;
    ogv1_put32(cursor, sibling); cursor += 4;
    memcpy(cursor, value->name, sizeof(value->name)); cursor += sizeof(value->name);
    memcpy(cursor, value->description, sizeof(value->description)); cursor += sizeof(value->description);
    memcpy(cursor, value->key, sizeof(value->key)); cursor += sizeof(value->key);
    memcpy(cursor, value->use_output, sizeof(value->use_output)); cursor += sizeof(value->use_output);
    ogv1_put64(cursor, value->value); cursor += 8;
    ogv1_put16(cursor, value->weight); cursor += 2;
    *cursor++ = (unsigned char)value->type; *cursor++ = (unsigned char)value->adjustment;
    ogv1_put16(cursor, value->shotsmax); cursor += 2; ogv1_put16(cursor, value->shotscur); cursor += 2;
    ogv1_put16(cursor, value->ndice); cursor += 2; ogv1_put16(cursor, value->sdice); cursor += 2;
    ogv1_put16(cursor, value->pdice); cursor += 2;
    *cursor++ = (unsigned char)value->armor; *cursor++ = (unsigned char)value->wearflag;
    *cursor++ = (unsigned char)value->magicpower; *cursor++ = (unsigned char)value->magicrealm;
    ogv1_put16(cursor, value->special); cursor += 2;
    memcpy(cursor, value->flags, sizeof(value->flags)); cursor += sizeof(value->flags);
    *cursor = (unsigned char)value->questnum;
}

static void ogv1_player_node(out, index, parent, sibling, value)
unsigned char out[OBJECT_GRAPH_V1_NODE_LENGTH];
uint32_t index;
int32_t parent;
uint32_t sibling;
const object *value;
{
    unsigned char *cursor;

    cursor = out;
    ogv1_put32(cursor, index); cursor += 4;
    ogv1_put32(cursor, (uint32_t)parent); cursor += 4;
    ogv1_put32(cursor, sibling); cursor += 4;
    ogv1_copy_fixed_canonical(cursor, value->name, sizeof(value->name)); cursor += sizeof(value->name);
    ogv1_copy_fixed_canonical(cursor, value->description, sizeof(value->description)); cursor += sizeof(value->description);
    ogv1_copy_fixed_canonical(cursor, value->key[0], sizeof(value->key[0])); cursor += sizeof(value->key[0]);
    ogv1_copy_fixed_canonical(cursor, value->key[1], sizeof(value->key[1])); cursor += sizeof(value->key[1]);
    ogv1_copy_fixed_canonical(cursor, value->key[2], sizeof(value->key[2])); cursor += sizeof(value->key[2]);
    ogv1_copy_fixed_canonical(cursor, value->use_output, sizeof(value->use_output)); cursor += sizeof(value->use_output);
    ogv1_put64(cursor, value->value); cursor += 8;
    ogv1_put16(cursor, value->weight); cursor += 2;
    *cursor++ = (unsigned char)value->type; *cursor++ = (unsigned char)value->adjustment;
    ogv1_put16(cursor, value->shotsmax); cursor += 2; ogv1_put16(cursor, value->shotscur); cursor += 2;
    ogv1_put16(cursor, value->ndice); cursor += 2; ogv1_put16(cursor, value->sdice); cursor += 2;
    ogv1_put16(cursor, value->pdice); cursor += 2;
    *cursor++ = (unsigned char)value->armor; *cursor++ = (unsigned char)value->wearflag;
    *cursor++ = (unsigned char)value->magicpower; *cursor++ = (unsigned char)value->magicrealm;
    ogv1_put16(cursor, value->special); cursor += 2;
    memcpy(cursor, value->flags, sizeof(value->flags)); cursor += sizeof(value->flags);
    *cursor = (unsigned char)value->questnum;
}

static int ogv1_read_node(input, value)
const unsigned char input[OBJECT_GRAPH_V1_NODE_LENGTH];
object *value;
{
    const unsigned char *cursor = input + 12;
    memset(value, 0, sizeof(*value));
    memcpy(value->name, cursor, sizeof(value->name)); cursor += sizeof(value->name);
    memcpy(value->description, cursor, sizeof(value->description)); cursor += sizeof(value->description);
    memcpy(value->key, cursor, sizeof(value->key)); cursor += sizeof(value->key);
    memcpy(value->use_output, cursor, sizeof(value->use_output)); cursor += sizeof(value->use_output);
    if(!ogv1_get64(cursor, &value->value)) return 0;
    cursor += 8; value->weight = ogv1_get16(cursor); cursor += 2;
    value->type = (char)*cursor++; value->adjustment = (char)*cursor++;
    value->shotsmax = ogv1_get16(cursor); cursor += 2; value->shotscur = ogv1_get16(cursor); cursor += 2;
    value->ndice = ogv1_get16(cursor); cursor += 2; value->sdice = ogv1_get16(cursor); cursor += 2;
    value->pdice = ogv1_get16(cursor); cursor += 2;
    value->armor = (char)*cursor++; value->wearflag = (char)*cursor++;
    value->magicpower = (char)*cursor++; value->magicrealm = (char)*cursor++;
    value->special = ogv1_get16(cursor); cursor += 2;
    memcpy(value->flags, cursor, sizeof(value->flags)); cursor += sizeof(value->flags);
    value->questnum = (char)*cursor;
    return value->shotscur <= value->shotsmax && ogv1_fixed(value->name, sizeof(value->name)) &&
           ogv1_fixed(value->description, sizeof(value->description)) &&
           ogv1_fixed(value->key[0], sizeof(value->key[0])) && ogv1_fixed(value->key[1], sizeof(value->key[1])) &&
           ogv1_fixed(value->key[2], sizeof(value->key[2])) && ogv1_fixed(value->use_output, sizeof(value->use_output));
}

int object_graph_v1_encode(roots, wire, wire_length)
const otag *roots;
uint8_t **wire;
size_t *wire_length;
{
    ogv1_build build;
    cdto_v1_field *fields;
    cdto_v1_record record;
    unsigned char *values, count[4];
    uint32_t i;
    int status;
    if(wire) *wire = 0;
    if(wire_length) *wire_length = 0;
    if(!wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    memset(&build, 0, sizeof(build)); fields = 0; values = 0;
    build.objects = (const object **)ogv1_calloc(OBJECT_GRAPH_V1_MAX_NODES, sizeof(*build.objects));
    build.parents = (int32_t *)ogv1_calloc(OBJECT_GRAPH_V1_MAX_NODES, sizeof(*build.parents));
    build.siblings = (uint32_t *)ogv1_calloc(OBJECT_GRAPH_V1_MAX_NODES, sizeof(*build.siblings));
    if(!build.objects || !build.parents || !build.siblings) { status = CDTO_V1_ALLOCATION_FAILED; goto done; }
    status = ogv1_collect_list(&build, roots, 0, -1, 1);
    if(status != CDTO_V1_OK) goto done;
    fields = (cdto_v1_field *)ogv1_calloc((size_t)build.count + 1, sizeof(*fields));
    if(!fields) { status = CDTO_V1_ALLOCATION_FAILED; goto done; }
    if(build.count) {
        values = (unsigned char *)ogv1_malloc((size_t)build.count * OBJECT_GRAPH_V1_NODE_LENGTH);
        if(!values) { status = CDTO_V1_ALLOCATION_FAILED; goto done; }
    }
    ogv1_put32(count, build.count);
    fields[0].id = 1; fields[0].type_tag = CDTO_V1_TYPE_U32; fields[0].value = count; fields[0].length = 4;
    for(i = 0; i < build.count; ++i) {
        ogv1_node(values + (size_t)i * OBJECT_GRAPH_V1_NODE_LENGTH, i, build.parents[i], build.siblings[i], build.objects[i]);
        fields[i + 1].id = (uint16_t)(i + 2); fields[i + 1].type_tag = CDTO_V1_TYPE_BYTES;
        fields[i + 1].value = values + (size_t)i * OBJECT_GRAPH_V1_NODE_LENGTH;
        fields[i + 1].length = OBJECT_GRAPH_V1_NODE_LENGTH;
    }
    record.kind = CDTO_V1_KIND_OBJECT_GRAPH; record.fields = fields; record.field_count = (size_t)build.count + 1;
    status = cdto_v1_encode(&record, wire, wire_length);
done:
    free(values); free(fields); free(build.siblings); free(build.parents); free(build.objects);
    return status;
}

int object_graph_v1_encode_player_inventory(roots, owner, wire, wire_length)
const otag *roots;
const creature *owner;
uint8_t **wire;
size_t *wire_length;
{
    ogv1_build build;
    cdto_v1_field *fields;
    cdto_v1_record record;
    unsigned char *values, count[4];
    uint32_t index;
    int status;

    if(wire) *wire = 0;
    if(wire_length) *wire_length = 0;
    if(!owner || !wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    memset(&build, 0, sizeof(build));
    fields = 0;
    values = 0;
    build.objects = (const object **)ogv1_calloc(OBJECT_GRAPH_V1_MAX_NODES,
        sizeof(*build.objects));
    build.parents = (int32_t *)ogv1_calloc(OBJECT_GRAPH_V1_MAX_NODES,
        sizeof(*build.parents));
    build.siblings = (uint32_t *)ogv1_calloc(OBJECT_GRAPH_V1_MAX_NODES,
        sizeof(*build.siblings));
    if(!build.objects || !build.parents || !build.siblings) {
        status = CDTO_V1_ALLOCATION_FAILED;
        goto done;
    }
    status = ogv1_collect_player_list(&build, roots, 0, -1, 1, owner);
    if(status != CDTO_V1_OK) goto done;
    fields = (cdto_v1_field *)ogv1_calloc((size_t)build.count + 1,
        sizeof(*fields));
    if(!fields) {
        status = CDTO_V1_ALLOCATION_FAILED;
        goto done;
    }
    if(build.count) {
        values = (unsigned char *)ogv1_malloc((size_t)build.count *
            OBJECT_GRAPH_V1_NODE_LENGTH);
        if(!values) {
            status = CDTO_V1_ALLOCATION_FAILED;
            goto done;
        }
    }
    ogv1_put32(count, build.count);
    fields[0].id = 1;
    fields[0].type_tag = CDTO_V1_TYPE_U32;
    fields[0].value = count;
    fields[0].length = 4;
    for(index = 0; index < build.count; ++index) {
        ogv1_player_node(values + (size_t)index * OBJECT_GRAPH_V1_NODE_LENGTH,
            index, build.parents[index], build.siblings[index], build.objects[index]);
        fields[index + 1].id = (uint16_t)(index + 2);
        fields[index + 1].type_tag = CDTO_V1_TYPE_BYTES;
        fields[index + 1].value = values + (size_t)index * OBJECT_GRAPH_V1_NODE_LENGTH;
        fields[index + 1].length = OBJECT_GRAPH_V1_NODE_LENGTH;
    }
    record.kind = CDTO_V1_KIND_OBJECT_GRAPH;
    record.fields = fields;
    record.field_count = (size_t)build.count + 1;
    status = cdto_v1_encode(&record, wire, wire_length);
done:
    free(values);
    free(fields);
    free(build.siblings);
    free(build.parents);
    free(build.objects);
    return status;
}

void object_graph_v1_free(roots)
otag *roots;
{
    otag *next;
    while(roots) {
        next = roots->next_tag;
        if(roots->obj) object_graph_v1_free(roots->obj->first_obj);
        free(roots->obj); free(roots); roots = next;
    }
}

int object_graph_v1_decode(wire, wire_length, roots)
const uint8_t *wire;
size_t wire_length;
otag **roots;
{
    cdto_v1_decoded_record record;
    object **objects;
    otag **tags, **tails, *root_tail;
    uint32_t *children, *depths, *ancestors, count, index, parent, sibling, expected, root_count, ancestor_count, position;
    size_t i;
    int status;
    if(roots) *roots = 0;
    if(!wire || !roots) return CDTO_V1_INVALID_ARGUMENT;
    memset(&record, 0, sizeof(record)); objects = 0; tags = tails = 0; root_tail = 0; children = depths = ancestors = 0; count = 0;
    status = cdto_v1_decode(wire, wire_length, &record);
    if(status != CDTO_V1_OK) goto done;
    if(record.kind != CDTO_V1_KIND_OBJECT_GRAPH || record.field_count < 1 || record.fields[0].id != 1 ||
       record.fields[0].type_tag != CDTO_V1_TYPE_U32 || record.fields[0].length != 4) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto done; }
    count = ogv1_get32(record.fields[0].value);
    if(count > OBJECT_GRAPH_V1_MAX_NODES || record.field_count != (size_t)count + 1) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto done; }
    if(!count) { status = CDTO_V1_OK; goto done; }
    objects = (object **)ogv1_calloc(count, sizeof(*objects)); tags = (otag **)ogv1_calloc(count, sizeof(*tags));
    tails = (otag **)ogv1_calloc(count, sizeof(*tails)); children = (uint32_t *)ogv1_calloc(count, sizeof(*children)); depths = (uint32_t *)ogv1_calloc(count, sizeof(*depths));
    ancestors = (uint32_t *)ogv1_calloc(count, sizeof(*ancestors));
    if(!objects || !tags || !tails || !children || !depths || !ancestors) { status = CDTO_V1_ALLOCATION_FAILED; goto done; }
    root_count = ancestor_count = 0;
    for(index = 0; index < count; ++index) {
        cdto_v1_decoded_field *field = &record.fields[index + 1];
        if(field->id != index + 2 || field->type_tag != CDTO_V1_TYPE_BYTES || field->length != OBJECT_GRAPH_V1_NODE_LENGTH ||
           ogv1_get32(field->value) != index) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto done; }
        parent = ogv1_get32(field->value + 4); sibling = ogv1_get32(field->value + 8);
        if(parent == 0xffffffffU) { expected = root_count++; depths[index] = 1; ancestor_count = 0; }
        else {
            if(parent >= index) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto done; }
            for(position = 0; position < ancestor_count && ancestors[position] != parent; ++position) ;
            if(position == ancestor_count) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto done; }
            ancestor_count = position + 1;
            expected = children[parent]++; depths[index] = depths[parent] + 1;
        }
        if(sibling != expected) { status = CDTO_V1_INVALID_FIELD_LENGTH; goto done; }
        if(depths[index] > OBJECT_GRAPH_V1_MAX_DEPTH) { status = CDTO_V1_SIZE_LIMIT_EXCEEDED; goto done; }
        objects[index] = (object *)ogv1_calloc(1, sizeof(*objects[index])); tags[index] = (otag *)ogv1_calloc(1, sizeof(*tags[index]));
        if(!objects[index] || !tags[index] || !ogv1_read_node(field->value, objects[index])) { status = objects[index] && tags[index] ? CDTO_V1_INVALID_FIELD_LENGTH : CDTO_V1_ALLOCATION_FAILED; goto done; }
        tags[index]->obj = objects[index];
        if(parent == 0xffffffffU) {
            if(!*roots) *roots = tags[index]; else root_tail->next_tag = tags[index];
            root_tail = tags[index];
        } else {
            objects[index]->parent_obj = objects[parent];
            if(!objects[parent]->first_obj) objects[parent]->first_obj = tags[index]; else tails[parent]->next_tag = tags[index];
            tails[parent] = tags[index];
        }
        ancestors[ancestor_count++] = index;
    }
    status = CDTO_V1_OK;
done:
    if(status != CDTO_V1_OK) {
        /* No exposed graph survives an error.  Objects/tags are individually
           allocated, so cleanup never needs to trust malformed links. */
        *roots = 0;
        if(tags || objects) for(i = 0; i < (record.field_count > 0 ? (size_t)count : 0); ++i) { free(tags ? tags[i] : 0); free(objects ? objects[i] : 0); }
    }
    free(ancestors); free(depths); free(children); free(tails); free(tags); free(objects);
    cdto_v1_free_decoded(&record);
    return status;
}
