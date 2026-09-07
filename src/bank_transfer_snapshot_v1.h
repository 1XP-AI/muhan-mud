#ifndef MUHAN_BANK_TRANSFER_SNAPSHOT_V1_H
#define MUHAN_BANK_TRANSFER_SNAPSHOT_V1_H
#include "player_snapshot_v1.h"
#include "bank_snapshot_v1.h"

/* Caller must exclusively own normalized player and detached bank graphs for
 * this entire synchronous call. This API does NOT lock files or freeze a live
 * game loop. No callbacks, IO, persisted writes or runtime mutation occur.
 * Output is the Rust planner frame: two big-endian u32 lengths, then complete
 * player and bank CDTO payloads. Each payload is capped at 4 MiB, digest included.
 * Outputs reset on failure; release successful output with free(). */
int bank_transfer_snapshot_v1_encode(const creature *,const otag *,unsigned char **,size_t *);
#ifdef BANK_TRANSFER_SNAPSHOT_V1_TESTING
void bank_transfer_snapshot_v1_test_fail_allocation(int);
#endif
#endif
