#ifndef MUHAN_PLAYER_PAIRED_ROUTE_NATIVE_H
#define MUHAN_PLAYER_PAIRED_ROUTE_NATIVE_H
#include <stdint.h>
typedef struct player_paired_route {
    char character_id[37],owner_user_id[37],player_hash[65],bank_hash[65];
    uint64_t revision;
} player_paired_route;
typedef struct player_paired_route_context {
    void *connection;
    const char *world,*writer,*epoch;
    int timeout_ms;
    player_paired_route last;
} player_paired_route_context;
/* PlayerAuthorityStore selector: 1 = verified paired DB route, -1 = reject.
 * Never infers legacy. Clear last on every call; no cached fallback. Borrowed
 * idle authenticated PGconn must be discarded after any transport failure.
 * Lookup is not a save authorization; provider must revalidate UUID/revision. */
int player_paired_route_select(void *,const char *);
#endif
