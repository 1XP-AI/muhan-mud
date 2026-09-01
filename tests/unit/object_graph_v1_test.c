#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cdto_v1.h"
#include "object_graph_v1.h"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "object_graph_v1_test: %s\n", message);
    return 1;
}

static void item(value, name, value_number)
object *value;
const char *name;
long value_number;
{
    memset(value, 0, sizeof(*value));
    strncpy(value->name, name, sizeof(value->name) - 1);
    value->value = value_number;
    value->weight = (short)value_number;
    value->shotsmax = 5;
    value->shotscur = 3;
    value->flags[0] = (char)value_number;
}

static void fixture(roots, objects, tags)
otag **roots;
object objects[4];
otag tags[4];
{
    item(&objects[0], "synthetic-bag", 11);
    item(&objects[1], "synthetic-coin", 12);
    item(&objects[2], "synthetic-key", 13);
    item(&objects[3], "synthetic-gem", 14);
    memset(tags, 0, 4 * sizeof(*tags));
    tags[0].obj = &objects[0];
    tags[1].obj = &objects[1]; tags[2].obj = &objects[2];
    tags[3].obj = &objects[3];
    tags[1].next_tag = &tags[2];
    objects[0].first_obj = &tags[1];
    objects[1].parent_obj = &objects[0];
    objects[2].parent_obj = &objects[0];
    objects[1].first_obj = &tags[3];
    objects[3].parent_obj = &objects[1];
    *roots = &tags[0];
}

static int node_wire(wire, wire_length, output)
const unsigned char *wire;
size_t wire_length;
unsigned char **output;
{
    cdto_v1_decoded_record record;
    int status;
    memset(&record, 0, sizeof(record));
    status = cdto_v1_decode(wire, wire_length, &record);
    if(status != CDTO_V1_OK) return status;
    if(record.field_count < 2) { cdto_v1_free_decoded(&record); return CDTO_V1_INVALID_FIELD_LENGTH; }
    *output = (unsigned char *)malloc(record.fields[1].length);
    if(!*output) { cdto_v1_free_decoded(&record); return CDTO_V1_ALLOCATION_FAILED; }
    memcpy(*output, record.fields[1].value, record.fields[1].length);
    cdto_v1_free_decoded(&record);
    return CDTO_V1_OK;
}

static int reencode_with_extra(canonical, canonical_length, output, output_length)
const unsigned char *canonical;
size_t canonical_length;
unsigned char **output;
size_t *output_length;
{
    cdto_v1_decoded_record decoded;
    cdto_v1_field *fields;
    cdto_v1_record record;
    size_t i;
    int status;
    memset(&decoded, 0, sizeof(decoded));
    *output = 0; *output_length = 0;
    status = cdto_v1_decode(canonical, canonical_length, &decoded);
    if(status != CDTO_V1_OK) return status;
    fields = (cdto_v1_field *)calloc(decoded.field_count + 1, sizeof(*fields));
    if(!fields) { cdto_v1_free_decoded(&decoded); return CDTO_V1_ALLOCATION_FAILED; }
    for(i = 0; i < decoded.field_count; ++i) {
        fields[i].id = decoded.fields[i].id; fields[i].type_tag = decoded.fields[i].type_tag;
        fields[i].value = decoded.fields[i].value; fields[i].length = decoded.fields[i].length;
    }
    fields[decoded.field_count].id = (uint16_t)(decoded.field_count + 1);
    fields[decoded.field_count].type_tag = CDTO_V1_TYPE_BYTES;
    fields[decoded.field_count].value = (const uint8_t *)"x";
    fields[decoded.field_count].length = 1;
    record.kind = decoded.kind; record.fields = fields; record.field_count = decoded.field_count + 1;
    status = cdto_v1_encode(&record, output, output_length);
    free(fields); cdto_v1_free_decoded(&decoded);
    return status;
}

static void put32(out, value)
unsigned char *out;
uint32_t value;
{
    out[0] = (unsigned char)(value >> 24); out[1] = (unsigned char)(value >> 16);
    out[2] = (unsigned char)(value >> 8); out[3] = (unsigned char)value;
}

/* Mutate parent/sibling metadata while retaining a valid CDTO digest.  The
 * resulting shapes satisfy count/depth alone but violate depth-first preorder. */
static int reparent_node(canonical, canonical_length, node_index, parent, sibling, output, output_length)
const unsigned char *canonical;
size_t canonical_length;
uint32_t node_index, parent, sibling;
unsigned char **output;
size_t *output_length;
{
    cdto_v1_decoded_record decoded;
    cdto_v1_field *fields;
    cdto_v1_record record;
    size_t i;
    int status;
    memset(&decoded, 0, sizeof(decoded)); *output = 0; *output_length = 0;
    status = cdto_v1_decode(canonical, canonical_length, &decoded);
    if(status != CDTO_V1_OK) return status;
    if(node_index + 1 >= decoded.field_count || decoded.fields[node_index + 1].length < 12) {
        cdto_v1_free_decoded(&decoded); return CDTO_V1_INVALID_FIELD_LENGTH;
    }
    fields = (cdto_v1_field *)calloc(decoded.field_count, sizeof(*fields));
    if(!fields) { cdto_v1_free_decoded(&decoded); return CDTO_V1_ALLOCATION_FAILED; }
    for(i = 0; i < decoded.field_count; ++i) {
        fields[i].id = decoded.fields[i].id; fields[i].type_tag = decoded.fields[i].type_tag;
        fields[i].value = decoded.fields[i].value; fields[i].length = decoded.fields[i].length;
    }
    put32(decoded.fields[node_index + 1].value + 4, parent);
    put32(decoded.fields[node_index + 1].value + 8, sibling);
    record.kind = decoded.kind; record.fields = fields; record.field_count = decoded.field_count;
    status = cdto_v1_encode(&record, output, output_length);
    free(fields); cdto_v1_free_decoded(&decoded);
    return status;
}

static int extend_depth_wire(canonical, canonical_length, output, output_length)
const unsigned char *canonical;
size_t canonical_length;
unsigned char **output;
size_t *output_length;
{
    cdto_v1_decoded_record decoded;
    cdto_v1_field *fields;
    cdto_v1_record record;
    unsigned char *node;
    size_t i;
    int status;
    memset(&decoded, 0, sizeof(decoded)); *output = 0; *output_length = 0; node = 0;
    status = cdto_v1_decode(canonical, canonical_length, &decoded);
    if(status != CDTO_V1_OK) return status;
    if(decoded.field_count != OBJECT_GRAPH_V1_MAX_DEPTH + 1 || decoded.fields[0].length != 4 ||
       decoded.fields[decoded.field_count - 1].length != OBJECT_GRAPH_V1_NODE_LENGTH) {
        cdto_v1_free_decoded(&decoded); return CDTO_V1_INVALID_FIELD_LENGTH;
    }
    fields = (cdto_v1_field *)calloc(decoded.field_count + 1, sizeof(*fields));
    node = (unsigned char *)malloc(OBJECT_GRAPH_V1_NODE_LENGTH);
    if(!fields || !node) { free(node); free(fields); cdto_v1_free_decoded(&decoded); return CDTO_V1_ALLOCATION_FAILED; }
    for(i = 0; i < decoded.field_count; ++i) {
        fields[i].id = decoded.fields[i].id; fields[i].type_tag = decoded.fields[i].type_tag;
        fields[i].value = decoded.fields[i].value; fields[i].length = decoded.fields[i].length;
    }
    put32(decoded.fields[0].value, OBJECT_GRAPH_V1_MAX_DEPTH + 1);
    memcpy(node, decoded.fields[decoded.field_count - 1].value, OBJECT_GRAPH_V1_NODE_LENGTH);
    put32(node, OBJECT_GRAPH_V1_MAX_DEPTH); put32(node + 4, OBJECT_GRAPH_V1_MAX_DEPTH - 1); put32(node + 8, 0);
    fields[decoded.field_count].id = (uint16_t)(decoded.field_count + 1);
    fields[decoded.field_count].type_tag = CDTO_V1_TYPE_BYTES;
    fields[decoded.field_count].value = node; fields[decoded.field_count].length = OBJECT_GRAPH_V1_NODE_LENGTH;
    record.kind = decoded.kind; record.fields = fields; record.field_count = decoded.field_count + 1;
    status = cdto_v1_encode(&record, output, output_length);
    free(node); free(fields); cdto_v1_free_decoded(&decoded);
    return status;
}

static void two_root_fixture(roots, objects, tags)
otag **roots;
object objects[3];
otag tags[3];
{
    item(&objects[0], "synthetic-root-a", 21); item(&objects[1], "synthetic-child-a", 22);
    item(&objects[2], "synthetic-root-b", 23); memset(tags, 0, 3 * sizeof(*tags));
    tags[0].obj = &objects[0]; tags[1].obj = &objects[1]; tags[2].obj = &objects[2];
    tags[0].next_tag = &tags[2]; objects[0].first_obj = &tags[1]; objects[1].parent_obj = &objects[0];
    *roots = &tags[0];
}

#ifdef OBJECT_GRAPH_V1_TESTING
static int allocator_faults(roots, canonical, canonical_length)
const otag *roots;
const unsigned char *canonical;
size_t canonical_length;
{
    unsigned char *wire;
    size_t wire_length;
    otag *decoded;
    int index, status, failed, saw_success;
    failed = 0; saw_success = 0;
    for(index = 0; index < 64; ++index) {
        wire = (unsigned char *)1; wire_length = 1;
        object_graph_v1_test_fail_after(index);
        status = object_graph_v1_encode(roots, &wire, &wire_length);
        if(status == CDTO_V1_OK) { cdto_v1_free_wire(wire); saw_success = 1; break; }
        failed += expect(status == CDTO_V1_ALLOCATION_FAILED && !wire && wire_length == 0,
                         "every graph-export allocator fault must clear outputs and report allocation failure");
    }
    failed += expect(saw_success, "allocator injection must reach export success after every allocation site");
    object_graph_v1_test_reset_allocator(); saw_success = 0;
    for(index = 0; index < 64; ++index) {
        decoded = (otag *)1;
        object_graph_v1_test_fail_after(index);
        status = object_graph_v1_decode(canonical, canonical_length, &decoded);
        if(status == CDTO_V1_OK) { object_graph_v1_free(decoded); saw_success = 1; break; }
        failed += expect(status == CDTO_V1_ALLOCATION_FAILED && !decoded,
                         "every graph-import allocator fault must clear output and report allocation failure");
    }
    failed += expect(saw_success, "allocator injection must reach import success after every allocation site");
    object_graph_v1_test_reset_allocator();
    return failed;
}
#endif

int main(void)
{
    object objects[4], before[4], deep[65], alias, two_root_objects[3];
    otag tags[4], deep_tags[65], alias_tags[2], two_root_tags[3], *roots, *decoded_roots;
    object *many_objects;
    otag *many_tags;
    unsigned char *wire, *roundtrip, *malformed, *node, *trailing;
    size_t wire_length, roundtrip_length, malformed_length, trailing_length;
    cdto_v1_decoded_record record;
    int failed, status, i;

    failed = 0; wire = roundtrip = malformed = node = trailing = 0;
    wire_length = roundtrip_length = malformed_length = trailing_length = 0;
    fixture(&roots, objects, tags);
    memcpy(before, objects, sizeof(objects));
    failed += expect(object_graph_v1_encode(roots, &wire, &wire_length) == CDTO_V1_OK,
                     "nested synthetic forest must encode");
    memset(&record, 0, sizeof(record));
    status = cdto_v1_decode(wire, wire_length, &record);
    failed += expect(status == CDTO_V1_OK && record.kind == CDTO_V1_KIND_OBJECT_GRAPH &&
                     record.field_count == 5 && record.fields[0].id == 1 &&
                     record.fields[0].type_tag == CDTO_V1_TYPE_U32 && record.fields[0].length == 4,
                     "ObjectGraphV1 must have an explicit graph kind and node count");
    cdto_v1_free_decoded(&record);
    failed += expect(!memcmp(before, objects, sizeof(objects)),
                     "export must not mutate the source object graph");
#ifdef OBJECT_GRAPH_V1_TESTING
    failed += allocator_faults(roots, wire, wire_length);
#endif
    failed += expect(node_wire(wire, wire_length, &node) == CDTO_V1_OK &&
                     node[0] == 0 && node[1] == 0 && node[2] == 0 && node[3] == 0 &&
                     node[4] == 0xff && node[5] == 0xff && node[6] == 0xff && node[7] == 0xff &&
                     node[8] == 0 && node[9] == 0 && node[10] == 0 && node[11] == 0,
                     "first preorder node must explicitly identify root parent and sibling index");
    free(node); node = 0;
    decoded_roots = (otag *)1;
    failed += expect(object_graph_v1_decode(wire, wire_length, &decoded_roots) == CDTO_V1_OK &&
                     decoded_roots && decoded_roots->obj && decoded_roots->obj->first_obj &&
                     decoded_roots->obj->first_obj->obj->parent_obj == decoded_roots->obj,
                     "import must allocate a detached tree and reconstruct only object parent links");
    failed += expect(!decoded_roots->obj->parent_rom && !decoded_roots->obj->parent_crt,
                     "import must never materialize room or creature pointers");
    failed += expect(object_graph_v1_encode(decoded_roots, &roundtrip, &roundtrip_length) == CDTO_V1_OK &&
                     roundtrip_length == wire_length && !memcmp(roundtrip, wire, wire_length),
                     "decode then encode must retain canonical bytes");
    object_graph_v1_free(decoded_roots);
    cdto_v1_free_wire(roundtrip); roundtrip = 0; roundtrip_length = 0;

    two_root_fixture(&roots, two_root_objects, two_root_tags);
    failed += expect(object_graph_v1_encode(roots, &roundtrip, &roundtrip_length) == CDTO_V1_OK &&
                     object_graph_v1_decode(roundtrip, roundtrip_length, &decoded_roots) == CDTO_V1_OK &&
                     decoded_roots && decoded_roots->obj->first_obj && decoded_roots->next_tag &&
                     decoded_roots->next_tag->obj && !strcmp(decoded_roots->obj->name, "synthetic-root-a") &&
                     !strcmp(decoded_roots->next_tag->obj->name, "synthetic-root-b"),
                     "two roots and a child must retain source-list order across import");
    object_graph_v1_free(decoded_roots); cdto_v1_free_wire(roundtrip); roundtrip = 0; roundtrip_length = 0;

    failed += expect(reparent_node(wire, wire_length, 2, 0xffffffffU, 1, &malformed, &malformed_length) == CDTO_V1_OK &&
                     object_graph_v1_decode(malformed, malformed_length, &decoded_roots) == CDTO_V1_INVALID_FIELD_LENGTH,
                     "a closed root subtree must not be re-entered by a later child");
    cdto_v1_free_wire(malformed); malformed = 0; malformed_length = 0;
    failed += expect(reparent_node(wire, wire_length, 2, 0, 1, &malformed, &malformed_length) == CDTO_V1_OK &&
                     reparent_node(malformed, malformed_length, 3, 1, 0, &roundtrip, &roundtrip_length) == CDTO_V1_OK &&
                     object_graph_v1_decode(roundtrip, roundtrip_length, &decoded_roots) == CDTO_V1_INVALID_FIELD_LENGTH,
                     "a closed sibling subtree must not be re-entered by a late grandchild");
    cdto_v1_free_wire(malformed); malformed = 0; malformed_length = 0;
    cdto_v1_free_wire(roundtrip); roundtrip = 0; roundtrip_length = 0;

    alias = objects[1]; memset(alias_tags, 0, sizeof(alias_tags));
    alias_tags[0].obj = &objects[0]; alias_tags[1].obj = &alias;
    objects[0].first_obj = &alias_tags[1]; alias.parent_obj = &objects[0];
    alias.first_obj = &alias_tags[1];
    failed += expect(object_graph_v1_encode(&alias_tags[0], &malformed, &malformed_length) == OBJECT_GRAPH_V1_INVALID_GRAPH &&
                     !malformed && malformed_length == 0,
                     "cycle or alias in source pointers must fail without a stale allocation");

    fixture(&roots, objects, tags);
    objects[2].parent_obj = 0;
    failed += expect(object_graph_v1_encode(roots, &malformed, &malformed_length) == OBJECT_GRAPH_V1_INVALID_GRAPH,
                     "source child parent mismatch must fail closed");
    fixture(&roots, objects, tags);
    objects[0].parent_crt = (creature *)1;
    failed += expect(object_graph_v1_encode(roots, &malformed, &malformed_length) == OBJECT_GRAPH_V1_INVALID_GRAPH,
                     "attached roots must not enter the synthetic-only graph codec");
    fixture(&roots, objects, tags);
    objects[0].name[20] = 'x';
    failed += expect(object_graph_v1_encode(roots, &malformed, &malformed_length) == OBJECT_GRAPH_V1_INVALID_GRAPH,
                     "bytes after a fixed-string NUL must not become graph padding");

    memset(deep, 0, sizeof(deep)); memset(deep_tags, 0, sizeof(deep_tags));
    for(i = 0; i < 65; ++i) {
        item(&deep[i], "synthetic-depth", i); deep_tags[i].obj = &deep[i];
        if(i) { deep[i - 1].first_obj = &deep_tags[i]; deep[i].parent_obj = &deep[i - 1]; }
    }
    failed += expect(object_graph_v1_encode(&deep_tags[0], &malformed, &malformed_length) == CDTO_V1_SIZE_LIMIT_EXCEEDED,
                     "depth 65 must exceed the fixed depth 64 limit");
    deep[64].parent_obj = 0; deep[63].first_obj = 0;
    failed += expect(object_graph_v1_encode(&deep_tags[0], &malformed, &malformed_length) == CDTO_V1_OK,
                     "depth 64 must be accepted");
    failed += expect(extend_depth_wire(malformed, malformed_length, &roundtrip, &roundtrip_length) == CDTO_V1_OK &&
                     object_graph_v1_decode(roundtrip, roundtrip_length, &decoded_roots) == CDTO_V1_SIZE_LIMIT_EXCEEDED &&
                     !decoded_roots,
                     "wire depth 65 must use the same size-limit class as source export");
    cdto_v1_free_wire(roundtrip); roundtrip = 0; roundtrip_length = 0;
    cdto_v1_free_wire(malformed); malformed = 0; malformed_length = 0;

    many_objects = (object *)calloc(OBJECT_GRAPH_V1_MAX_NODES + 1, sizeof(*many_objects));
    many_tags = (otag *)calloc(OBJECT_GRAPH_V1_MAX_NODES + 1, sizeof(*many_tags));
    failed += expect(many_objects && many_tags, "node limit fixture allocation");
    if(many_objects && many_tags) {
        for(i = 0; i <= (int)OBJECT_GRAPH_V1_MAX_NODES; ++i) {
            item(&many_objects[i], "synthetic-limit", i); many_tags[i].obj = &many_objects[i];
            if(i) many_tags[i - 1].next_tag = &many_tags[i];
        }
        many_tags[OBJECT_GRAPH_V1_MAX_NODES - 1].next_tag = 0;
        failed += expect(object_graph_v1_encode(&many_tags[0], &malformed, &malformed_length) == CDTO_V1_OK,
                         "node 8192 must remain within the fixed total-node limit");
        cdto_v1_free_wire(malformed); malformed = 0; malformed_length = 0;
        many_tags[OBJECT_GRAPH_V1_MAX_NODES - 1].next_tag = &many_tags[OBJECT_GRAPH_V1_MAX_NODES];
        failed += expect(object_graph_v1_encode(&many_tags[0], &malformed, &malformed_length) == CDTO_V1_SIZE_LIMIT_EXCEEDED &&
                         !malformed && malformed_length == 0,
                         "node 8193 must exceed the fixed total-node limit without a stale allocation");
    }
    free(many_tags); free(many_objects);

    failed += expect(reencode_with_extra(wire, wire_length, &malformed, &malformed_length) == CDTO_V1_OK &&
                     object_graph_v1_decode(malformed, malformed_length, &decoded_roots) == CDTO_V1_INVALID_FIELD_LENGTH,
                     "unknown graph fields must be rejected");
    cdto_v1_free_wire(malformed); malformed = 0;
    trailing_length = wire_length + 1; trailing = (unsigned char *)malloc(trailing_length);
    if(trailing) { memcpy(trailing, wire, wire_length); trailing[wire_length] = 0; }
    failed += expect(trailing && object_graph_v1_decode(trailing, trailing_length, &decoded_roots) == CDTO_V1_TRAILING_BYTES,
                     "trailing graph bytes must be rejected");
    free(trailing);

    cdto_v1_free_wire(roundtrip); cdto_v1_free_wire(wire);
    return failed ? 1 : 0;
}
