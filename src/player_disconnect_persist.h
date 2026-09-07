#ifndef MUHAN_PLAYER_DISCONNECT_PERSIST_H
#define MUHAN_PLAYER_DISCONNECT_PERSIST_H
struct creature;
/* Called after socket/io cleanup, while the caller still owns the player.
 * Zero means freed or transferred to recovery, and clears *owned. Nonzero
 * retains ownership for the caller's fatal fail-closed path. No DB cutover. */
int player_disconnect_persist(struct creature **owned);
#endif
