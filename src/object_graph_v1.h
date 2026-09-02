#ifndef MUHAN_OBJECT_GRAPH_V1_H
#define MUHAN_OBJECT_GRAPH_V1_H

#include <stddef.h>
#include <stdint.h>

#ifndef MUHAN_MSTRUCT_TYPES_INCLUDED
#define MUHAN_MSTRUCT_TYPES_INCLUDED
#include "mstruct.h"
#endif

/* Synthetic-only, detached object forest projection.  A graph contains a
 * preorder sequence of fixed logical object values; every node explicitly
 * carries its own index, parent preorder index, and index within that parent.
 * The codec never calls game allocators, save/load paths, or global lists. */
#define OBJECT_GRAPH_V1_MAX_DEPTH 64U
#define OBJECT_GRAPH_V1_MAX_NODES 8192U
/* A legacy player file stores every root/child list with a signed count that
 * is accepted only through 4096.  The player adapter preserves that stricter
 * per-list profile while the detached generic graph remains more general. */
#define OBJECT_GRAPH_V1_PLAYER_MAX_LIST_ITEMS 4096U
#define OBJECT_GRAPH_V1_NODE_LENGTH 349U
#define OBJECT_GRAPH_V1_INVALID_GRAPH (-200)

int object_graph_v1_encode(const otag *roots, uint8_t **wire, size_t *wire_length);
/* Player-file projection.  Top-level objects must be attached to owner;
 * nested objects remain object-attached.  The source graph is never changed. */
int object_graph_v1_encode_player_inventory(const otag *roots,
    const creature *owner, uint8_t **wire, size_t *wire_length);
int object_graph_v1_decode(const uint8_t *wire, size_t wire_length, otag **roots);
/* Accepts only a successful object_graph_v1_decode() output.  Passing an
 * arbitrary/source legacy list is unsupported and may double-free its owner. */
void object_graph_v1_free(otag *roots);

/* Test-only deterministic allocation fault injection.  These hooks do not
 * exist in production builds and are not part of the production API. */
#ifdef OBJECT_GRAPH_V1_TESTING
void object_graph_v1_test_fail_after(long allocation_index);
void object_graph_v1_test_reset_allocator(void);
#endif

#endif
