/* BankSnapshotV1 is intentionally an artifact-only boundary.  It composes
 * the established ObjectGraphV1 codec and has no dependency on bank runtime
 * code, persistence, network, or transaction behavior. */
#include "bank_snapshot_v1.h"
#include "cdto_v1.h"

#include <string.h>

static int bsv1_one_detached_root(root)
const otag *root;
{
    return root && root->obj && !root->next_tag && !root->obj->parent_obj &&
           !root->obj->parent_rom && !root->obj->parent_crt;
}

int bank_snapshot_v1_encode(root, wire, wire_length)
const otag *root;
uint8_t **wire;
size_t *wire_length;
{
    cdto_v1_field field;
    cdto_v1_record record;
    uint8_t *graph;
    size_t graph_length;
    int status;

    if(wire) *wire = 0;
    if(wire_length) *wire_length = 0;
    if(!wire || !wire_length) return CDTO_V1_INVALID_ARGUMENT;
    if(!bsv1_one_detached_root(root)) return BANK_SNAPSHOT_V1_INVALID_ROOTS;
    graph = 0; graph_length = 0;
    status = object_graph_v1_encode(root, &graph, &graph_length);
    if(status != CDTO_V1_OK) return status;
    field.id = 1; field.type_tag = CDTO_V1_TYPE_BYTES;
    field.value = graph; field.length = (uint32_t)graph_length;
    record.kind = CDTO_V1_KIND_BANK_SNAPSHOT;
    record.fields = &field;
    record.field_count = 1;
    status = cdto_v1_encode(&record, wire, wire_length);
    cdto_v1_free_wire(graph);
    return status;
}

int bank_snapshot_v1_decode(wire, wire_length, root)
const uint8_t *wire;
size_t wire_length;
otag **root;
{
    cdto_v1_decoded_record record;
    uint8_t *canonical;
    size_t canonical_length;
    int status;

    if(root) *root = 0;
    if(!wire || !root) return CDTO_V1_INVALID_ARGUMENT;
    memset(&record, 0, sizeof(record));
    canonical = 0; canonical_length = 0;
    status = cdto_v1_decode(wire, wire_length, &record);
    if(status != CDTO_V1_OK) goto done;
    if(record.kind != CDTO_V1_KIND_BANK_SNAPSHOT || record.field_count != 1 ||
       record.fields[0].id != 1 || record.fields[0].type_tag != CDTO_V1_TYPE_BYTES ||
       !record.fields[0].length) {
        status = CDTO_V1_INVALID_FIELD_LENGTH;
        goto done;
    }
    status = object_graph_v1_decode(record.fields[0].value, record.fields[0].length, root);
    if(status != CDTO_V1_OK) goto done;
    if(!bsv1_one_detached_root(*root)) {
        status = BANK_SNAPSHOT_V1_INVALID_ROOTS;
        goto done;
    }
    status = object_graph_v1_encode(*root, &canonical, &canonical_length);
    if(status != CDTO_V1_OK) goto done;
    if(canonical_length != record.fields[0].length ||
       memcmp(canonical, record.fields[0].value, canonical_length))
        status = CDTO_V1_INVALID_FIELD_LENGTH;
done:
    cdto_v1_free_wire(canonical);
    cdto_v1_free_decoded(&record);
    if(status != CDTO_V1_OK) {
        object_graph_v1_free(*root);
        *root = 0;
    }
    return status;
}

void bank_snapshot_v1_free(root)
otag *root;
{
    object_graph_v1_free(root);
}

void bank_snapshot_v1_free_wire(wire)
uint8_t *wire;
{
    cdto_v1_free_wire(wire);
}
