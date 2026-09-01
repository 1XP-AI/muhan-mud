#ifndef MUHAN_CREATURE_V1_H
#define MUHAN_CREATURE_V1_H

#include <stddef.h>
#include <stdint.h>

#ifndef MUHAN_MSTRUCT_TYPES_INCLUDED
#define MUHAN_MSTRUCT_TYPES_INCLUDED
#include "mstruct.h"
#endif

/* Test-only, pointer-free CreatureV1 clone projection.  It has exactly the
 * safe 37-field schema and is never linked into a MUD save/load path. */
#define CREATURE_V1_FIELD_COUNT 37U
#define CREATURE_V1_UNSUPPORTED_STATE (-101)

int creature_v1_encode_flat(const creature *input, uint8_t **wire, size_t *wire_length);
int creature_v1_decode_flat(const uint8_t *wire, size_t wire_length, creature *output);
int creature_v1_equal_safe(const creature *left, const creature *right);

#endif
