#ifndef MUHAN_BANK_SNAPSHOT_V1_H
#define MUHAN_BANK_SNAPSHOT_V1_H

#include <stddef.h>
#include <stdint.h>

#include "object_graph_v1.h"

/* A deterministic, non-live bank artifact.  It is never part of the legacy
 * bank save/load authority: the public API only moves owned DTO bytes and an
 * owned detached ObjectGraphV1 root. */
#define BANK_SNAPSHOT_V1_INVALID_ROOTS (-201)

/* Encode exactly one detached root through ObjectGraphV1.  On every failure
 * both output values are reset; callers release successful bytes with
 * bank_snapshot_v1_free_wire(). */
int bank_snapshot_v1_encode(const otag *root, uint8_t **wire, size_t *wire_length);

/* Decode a kind-8 wrapper into one newly allocated detached root.  On every
 * failure *root is NULL.  Release it only with bank_snapshot_v1_free(). */
int bank_snapshot_v1_decode(const uint8_t *wire, size_t wire_length, otag **root);
void bank_snapshot_v1_free(otag *root);
void bank_snapshot_v1_free_wire(uint8_t *wire);

#endif
