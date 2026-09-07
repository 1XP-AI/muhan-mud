#ifndef MUHAN_PLAYER_SNAPSHOT_SAVE_NATIVE_H
#define MUHAN_PLAYER_SNAPSHOT_SAVE_NATIVE_H
#include <stddef.h>
#include <stdint.h>
enum player_snapshot_save_status {
    PLAYER_SNAPSHOT_SAVE_NOT_SENT=-3,
    PLAYER_SNAPSHOT_SAVE_INVALID=-2, PLAYER_SNAPSHOT_SAVE_REJECTED=-1,
    PLAYER_SNAPSHOT_SAVE_UNKNOWN=0, PLAYER_SNAPSHOT_SAVE_COMMITTED=1,
    PLAYER_SNAPSHOT_SAVE_RETRY=2
};
/* Explicit immutable request: world, name, writer UUID, epoch, character UUID,
 * command UUID, ORIGINAL loaded revision, ORIGINAL full-player SHA256.
 * Never derives a baseline from a fresh route lookup. Payload is canonical
 * PlayerSnapshotV1. Borrowed idle writer PGconn; no files or live state changed.
 * UNKNOWN requires retaining the exact request and discarding the connection.
 * RETRY reports historical commit only, not permission to adopt it as live head.
 * Internal transport boundary, NOT a PlayerStore provider: the caller must
 * durably prepare/fence the request before use. No production installation. */
int player_snapshot_save_native(void *,const char *const [8],const unsigned char *,size_t,int,uint64_t *);
/* Internal prepared transport. Exact helper echo is mandatory before any SQL.
 * Failure is NOT_SENT, but preparation may already have persisted a request;
 * retain it. This does not supply cross-operation character ownership. */
int player_snapshot_save_prepared_native(void *,const char *,const char *,const char *,
    const char *const [8],const unsigned char *,size_t,int,uint64_t *);
#endif
