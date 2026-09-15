#ifndef MUHAN_BANK_MONEY_LIVE_SNAPSHOT_H
#define MUHAN_BANK_MONEY_LIVE_SNAPSHOT_H
#include <stddef.h>
struct creature;
/* Read-only capture: bounded legacy serialization -> actual player decoder ->
 * canonical PlayerSnapshotV1. Linux anonymous fd, no game-file writes.
 * Caller owns returned bytes with free(). No output on any failure. */
int bank_money_live_snapshot(const struct creature *,unsigned char **,size_t *);
#endif
