#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "bank_snapshot_v1.h"
#include "cdto_v1.h"
#include "mtype.h"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "bank_snapshot_v1_test: %s\n", message);
    return 1;
}

static void item(value, name, type, amount)
object *value;
const char *name;
char type;
long amount;
{
    memset(value, 0, sizeof(*value));
    strncpy(value->name, name, sizeof(value->name) - 1);
    value->type = type;
    value->value = amount;
    value->shotsmax = 1;
    value->shotscur = 1;
}

static void one_root(roots, objects, tags)
otag **roots;
object objects[2];
otag tags[2];
{
    item(&objects[0], "bank-chest", 4, 99L);
    item(&objects[1], "bank-money", MONEY, LONG_MAX);
    memset(tags, 0, 2 * sizeof(*tags));
    tags[0].obj = &objects[0];
    tags[1].obj = &objects[1];
    objects[0].first_obj = &tags[1];
    objects[1].parent_obj = &objects[0];
    *roots = &tags[0];
}

static int wrap_graph(graph, graph_length, fields, field_count, wire, wire_length)
const unsigned char *graph;
size_t graph_length;
const cdto_v1_field *fields;
size_t field_count;
unsigned char **wire;
size_t *wire_length;
{
    cdto_v1_record record;
    record.kind = CDTO_V1_KIND_BANK_SNAPSHOT;
    record.fields = fields;
    record.field_count = field_count;
    (void)graph;
    (void)graph_length;
    return cdto_v1_encode(&record, wire, wire_length);
}

int main(void)
{
    object objects[2], two[2], attached, deep[65], *many;
    otag tags[2], two_tags[2], deep_tags[65], *many_tags, *roots, *decoded;
    unsigned char *wire, *again, *bad, *oversize, *graph;
    size_t wire_length, again_length, bad_length, oversize_length, graph_length;
    cdto_v1_decoded_record record;
    cdto_v1_field fields[2];
    int failed, status, i, saw_success;

    failed = 0; wire = again = bad = oversize = graph = 0;
    wire_length = again_length = bad_length = oversize_length = graph_length = 0;
    one_root(&roots, objects, tags);

    failed += expect(bank_snapshot_v1_encode(roots, &wire, &wire_length) == CDTO_V1_OK,
                     "one detached root must encode");
    memset(&record, 0, sizeof(record));
    failed += expect(cdto_v1_decode(wire, wire_length, &record) == CDTO_V1_OK &&
                     record.kind == CDTO_V1_KIND_BANK_SNAPSHOT && record.field_count == 1 &&
                     record.fields[0].id == 1 && record.fields[0].type_tag == CDTO_V1_TYPE_BYTES,
                     "kind 8 must contain exactly one ObjectGraphV1 bytes field");
    cdto_v1_free_decoded(&record);
    failed += expect(object_graph_v1_encode(roots, &graph, &graph_length) == CDTO_V1_OK,
                     "fixture graph must be available for wrapper schema tests");

    decoded = 0;
    failed += expect(bank_snapshot_v1_decode(wire, wire_length, &decoded) == CDTO_V1_OK && decoded &&
                     !decoded->next_tag && decoded->obj && decoded->obj->first_obj &&
                     decoded->obj->first_obj->obj->type == MONEY &&
                     decoded->obj->first_obj->obj->value == LONG_MAX,
                     "Money type and value must be retained losslessly through graph nodes");
    failed += expect(bank_snapshot_v1_encode(decoded, &again, &again_length) == CDTO_V1_OK &&
                     again_length == wire_length && !memcmp(again, wire, wire_length),
                     "decode/re-encode must be canonical and byte stable");
    bank_snapshot_v1_free(decoded); decoded = 0;
    cdto_v1_free_wire(again); again = 0;

    item(&two[0], "root-a", 1, 1L); item(&two[1], "root-b", 1, 2L);
    memset(two_tags, 0, sizeof(two_tags)); two_tags[0].obj = &two[0]; two_tags[1].obj = &two[1];
    two_tags[0].next_tag = &two_tags[1];
    failed += expect(bank_snapshot_v1_encode(&two_tags[0], &bad, &bad_length) == BANK_SNAPSHOT_V1_INVALID_ROOTS &&
                     !bad && !bad_length,
                     "multiple roots must be rejected without output");

    item(&attached, "attached", MONEY, 7L); attached.parent_crt = (creature *)1;
    memset(two_tags, 0, sizeof(two_tags)); two_tags[0].obj = &attached;
    failed += expect(bank_snapshot_v1_encode(&two_tags[0], &bad, &bad_length) == BANK_SNAPSHOT_V1_INVALID_ROOTS &&
                     !bad && !bad_length,
                     "attached roots must be rejected without output");
    failed += expect(bank_snapshot_v1_encode(0, &bad, &bad_length) == BANK_SNAPSHOT_V1_INVALID_ROOTS,
                     "empty roots must be rejected");

    memset(fields, 0, sizeof(fields));
    fields[0].id = 1; fields[0].type_tag = CDTO_V1_TYPE_BYTES; fields[0].value = graph; fields[0].length = (uint32_t)graph_length;
    fields[1].id = 2; fields[1].type_tag = CDTO_V1_TYPE_BYTES; fields[1].value = (const unsigned char *)"x"; fields[1].length = 1;
    failed += expect(wrap_graph(wire, wire_length, fields, 2, &bad, &bad_length) == CDTO_V1_OK &&
                     bank_snapshot_v1_decode(bad, bad_length, &decoded) == CDTO_V1_INVALID_FIELD_LENGTH && !decoded,
                     "extra wrapper fields must be rejected and leave no owned graph");
    cdto_v1_free_wire(bad); bad = 0;

    item(&two[0], "root-a", 1, 1L); item(&two[1], "root-b", 1, 2L);
    memset(two_tags, 0, sizeof(two_tags)); two_tags[0].obj = &two[0]; two_tags[1].obj = &two[1];
    two_tags[0].next_tag = &two_tags[1];
    failed += expect(object_graph_v1_encode(&two_tags[0], &again, &again_length) == CDTO_V1_OK,
                     "two roots remain valid only in the general graph codec");
    fields[0].value = again; fields[0].length = (uint32_t)again_length;
    failed += expect(wrap_graph(again, again_length, fields, 1, &bad, &bad_length) == CDTO_V1_OK &&
                     bank_snapshot_v1_decode(bad, bad_length, &decoded) == BANK_SNAPSHOT_V1_INVALID_ROOTS && !decoded,
                     "wrapped ObjectGraphV1 must still contain exactly one root");
    cdto_v1_free_wire(bad); bad = 0; cdto_v1_free_wire(again); again = 0;

    wire[wire_length - 1] ^= 1;
    failed += expect(bank_snapshot_v1_decode(wire, wire_length, &decoded) == CDTO_V1_DIGEST_MISMATCH && !decoded,
                     "outer digest corruption must fail closed");
    wire[wire_length - 1] ^= 1;
    bad = (unsigned char *)malloc(wire_length + 1);
    if(!bad) return 2;
    memcpy(bad, wire, wire_length); bad[wire_length] = 0;
    failed += expect(bank_snapshot_v1_decode(bad, wire_length + 1, &decoded) == CDTO_V1_TRAILING_BYTES && !decoded,
                     "trailing bytes must be rejected");
    free(bad); bad = 0;

    fields[0].length = CDTO_V1_BANK_SNAPSHOT_PAYLOAD_LIMIT;
    oversize = (unsigned char *)calloc(fields[0].length, 1);
    if(!oversize) return 2;
    fields[0].value = oversize;
    failed += expect(wrap_graph(0, 0, fields, 1, &bad, &bad_length) == CDTO_V1_SIZE_LIMIT_EXCEEDED,
                     "kind 8 payload cap must include its field header");
    free(oversize);

    memset(deep, 0, sizeof(deep)); memset(deep_tags, 0, sizeof(deep_tags));
    for(i = 0; i < 65; ++i) {
        deep_tags[i].obj = &deep[i]; deep[i].shotsmax = deep[i].shotscur = 1;
        if(i) { deep[i - 1].first_obj = &deep_tags[i]; deep[i].parent_obj = &deep[i - 1]; }
    }
    failed += expect(bank_snapshot_v1_encode(&deep_tags[0], &bad, &bad_length) == CDTO_V1_SIZE_LIMIT_EXCEEDED &&
                     !bad && !bad_length,
                     "graph depth limits must propagate through the wrapper");

    many = (object *)calloc(OBJECT_GRAPH_V1_MAX_NODES + 1U, sizeof(*many));
    many_tags = (otag *)calloc(OBJECT_GRAPH_V1_MAX_NODES + 1U, sizeof(*many_tags));
    if(!many || !many_tags) { free(many_tags); free(many); return 2; }
    for(i = 0; i <= (int)OBJECT_GRAPH_V1_MAX_NODES; ++i) {
        many[i].shotsmax = many[i].shotscur = 1; many_tags[i].obj = &many[i];
        if(i) { many[i].parent_obj = &many[0]; if(i > 1) many_tags[i - 1].next_tag = &many_tags[i]; }
    }
    many[0].first_obj = &many_tags[1];
    many_tags[OBJECT_GRAPH_V1_MAX_NODES - 1].next_tag = 0;
    failed += expect(bank_snapshot_v1_encode(&many_tags[0], &bad, &bad_length) == CDTO_V1_OK,
                     "maximum node count must remain encodable through the wrapper");
    cdto_v1_free_wire(bad); bad = 0; bad_length = 0;
    many_tags[OBJECT_GRAPH_V1_MAX_NODES - 1].next_tag = &many_tags[OBJECT_GRAPH_V1_MAX_NODES];
    failed += expect(bank_snapshot_v1_encode(&many_tags[0], &bad, &bad_length) == CDTO_V1_SIZE_LIMIT_EXCEEDED &&
                     !bad && !bad_length,
                     "over-limit graph counts must fail before exposing a wrapper");
    free(many_tags); free(many);

#ifdef OBJECT_GRAPH_V1_TESTING
    saw_success = 0;
    for(i = 0; i < 16; ++i) {
        object_graph_v1_test_fail_after((long)i);
        status = bank_snapshot_v1_encode(roots, &bad, &bad_length);
        if(status == CDTO_V1_OK) { saw_success = 1; cdto_v1_free_wire(bad); bad = 0; break; }
        failed += expect(status == CDTO_V1_ALLOCATION_FAILED && !bad && !bad_length,
                         "encode allocation faults must leave no owned wire");
    }
    failed += expect(saw_success, "allocation injection must reach a successful wrapper encode");
    object_graph_v1_test_reset_allocator(); saw_success = 0;
    for(i = 0; i < 20; ++i) {
        object_graph_v1_test_fail_after((long)i);
        status = bank_snapshot_v1_decode(wire, wire_length, &decoded);
        if(status == CDTO_V1_OK) { saw_success = 1; bank_snapshot_v1_free(decoded); decoded = 0; break; }
        failed += expect(status == CDTO_V1_ALLOCATION_FAILED && !decoded,
                         "decode allocation faults must leave no owned graph");
    }
    failed += expect(saw_success, "allocation injection must reach a successful wrapper decode");
    object_graph_v1_test_reset_allocator();
#endif

    objects[0].name[1] = 0; objects[0].name[2] = 'x';
    failed += expect(bank_snapshot_v1_encode(roots, &bad, &bad_length) == OBJECT_GRAPH_V1_INVALID_GRAPH,
                     "fixed-string nonzero tails must not enter a snapshot digest");

    cdto_v1_free_wire(graph);
    cdto_v1_free_wire(wire);
    return failed ? 1 : 0;
}
