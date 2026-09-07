#ifndef MUHAN_PLAYER_SESSION_REGISTRY_H
#define MUHAN_PLAYER_SESSION_REGISTRY_H
#include "player_session_store_native.h"
#define PLAYER_SESSION_REGISTRY_CAPACITY 64
typedef struct player_session_registry {
    char world[65];
    struct {player_session_store *context;char name[15];} slots[PLAYER_SESSION_REGISTRY_CAPACITY];
    int busy;
} player_session_registry;
/* Zero initialize. Single world, exact name, caller-owned contexts. Unknown,
 * duplicate, capacity-exhausted and inconsistent bindings fail closed; never
 * infer a FileStore fallback. Contexts must outlive the registry binding. */
int player_session_registry_add(player_session_registry *,player_session_store *);
/* Caller requests removal only after gameplay is drained. Pending/busy contexts
 * cannot be removed. Does not free a context or release durable ownership. */
int player_session_registry_remove(player_session_registry *,player_session_store *);
player_store_ops player_session_registry_build(player_session_registry *);
#endif
