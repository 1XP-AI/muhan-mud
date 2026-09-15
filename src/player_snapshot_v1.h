#ifndef MUHAN_PLAYER_SNAPSHOT_V1_H
#define MUHAN_PLAYER_SNAPSHOT_V1_H
#include <stddef.h>
#include <stdint.h>
#ifndef MUHAN_MSTRUCT_TYPES_INCLUDED
#define MUHAN_MSTRUCT_TYPES_INCLUDED
#include "mstruct.h"
#endif

#define PLAYER_SNAPSHOT_V1_FIELD_COUNT 40U
#define PLAYER_SNAPSHOT_V1_MAX_LIST_ITEMS 4096U

/* Returns nonzero only for the native ABI accepted by the durable handoff
 * contract.  The codec itself remains independently portable. */
int player_snapshot_v1_native_abi_supported(size_t char_bits,
    size_t short_bits, size_t long_bits, int long_covers_i64,
    int player_wire_value);

/* Encodes the pointer-free persisted projection of a normalized player-file
 * load.  Passwords, descriptors, ready slots, and runtime links never enter
 * the wire.  The caller owns a successful wire via cdto_v1_free_wire(). */
int player_snapshot_v1_encode_loaded(const creature *, uint8_t **, size_t *);
/* Publishes no partial clone on error.  On a native ABI whose long is narrower
 * than i64, values outside that native range fail closed.  A successful clone
 * owns its complete inventory graph and must be released with
 * player_snapshot_v1_free_clone(). */
int player_snapshot_v1_decode_clone(const uint8_t *, size_t, creature **);
void player_snapshot_v1_free_clone(creature *);
int player_snapshot_v1_equal_persisted(const creature *, const creature *);

#ifdef PLAYER_SNAPSHOT_V1_TESTING
void player_snapshot_v1_test_fail_after(long allocation_index);
void player_snapshot_v1_test_reset_allocator(void);
#endif
#endif
