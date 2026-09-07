#ifndef MUHAN_PLAYER_SESSION_STORE_NATIVE_H
#define MUHAN_PLAYER_SESSION_STORE_NATIVE_H
#include "player_store.h"
#include "player_snapshot_save_native.h"
typedef struct player_session_store {
    void *connection;
    const char *node,*script,*root;
    char fields[8][129];
    char owner[37];
    unsigned char *pending;
    size_t pending_length;
    uint64_t committed_revision;
    int configured,loaded,busy,timeout_ms,status;
} player_session_store;
/* One caller-owned character/load/command lifetime. Zero initialize. Identity
 * and loaded baseline are copied; trusted helper paths and PGconn are borrowed.
 * No fd/Ply/pointer identity used: savegame copies/disconnected saves retain
 * this original baseline. No reload or new command while active. Not installed
 * globally or a multi-character router. Caller must retain durable ownership
 * and reconcile/release/adopt before constructing the next command lifetime. */
int player_session_store_init(player_session_store *,void *,const char *,const char *,
    const char *,const char *,const char *,const char *,const char *,const char *,int);
player_store_ops player_session_store_build(player_session_store *);
/* Revalidate exact pending bytes against live memory/current DB and durable
 * release evidence before advancing to a fresh command. Does not release a
 * reservation or mutate gameplay. Subsequent writes still require CAS/fence. */
int player_session_store_adopt(player_session_store *,const struct creature *,const char *);
/* Frees memory only; NEVER releases/deletes a durable pending reservation. */
void player_session_store_dispose(player_session_store *);
#endif
