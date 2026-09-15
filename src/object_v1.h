#ifndef MUHAN_OBJECT_V1_H
#define MUHAN_OBJECT_V1_H

#include <stddef.h>
#include <stdint.h>

#ifndef MUHAN_MSTRUCT_TYPES_INCLUDED
#define MUHAN_MSTRUCT_TYPES_INCLUDED
#include "mstruct.h"
#endif

/* Clone-only, detached-object projection.  It deliberately excludes every
 * pointer and rejects contained or attached objects rather than serializing a
 * graph.  Its fixed legacy char arrays are preserved as raw 80/20-byte data,
 * including NUL padding; it is not wired into write_obj/read_obj or any player
 * save path. */
#define OBJECT_V1_FIELD_COUNT 22U
#define OBJECT_V1_UNSUPPORTED_GRAPH (-100)

int object_v1_encode_flat(const object *input, uint8_t **wire, size_t *wire_length);
int object_v1_decode_flat(const uint8_t *wire, size_t wire_length, object *output);
int object_v1_equal_flat(const object *left, const object *right);

#endif
