#ifndef MUHAN_PLAYER_AUTHORITY_STORE_H
#define MUHAN_PLAYER_AUTHORITY_STORE_H
#include "player_store.h"
/* Name is a lookup key, not proof of identity. Resolver must consult the held
 * world's authoritative mapping, including offline/recovery players. Only an
 * explicit 0 selects legacy; 1 selects DB; missing/unknown/error rejects.
 * DB provider must bind UUID/revision and pending recovery independently of
 * socket lifetime. This adapter neither supplies that provider nor installs
 * itself. Keep the caller-owned store alive while its facade is bound. */
typedef struct player_authority_store {
    int (*select)(void *,const char *);
    void *selection_context;
    player_store_ops legacy,database;
    int busy;
} player_authority_store;
player_store_ops player_authority_store_build(player_authority_store *);
#endif
