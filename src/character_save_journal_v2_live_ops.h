#ifndef CHARACTER_SAVE_JOURNAL_V2_LIVE_OPS_H
#define CHARACTER_SAVE_JOURNAL_V2_LIVE_OPS_H

/* Thin live-MUD composition layer.  The callbacks below only translate
 * rpc_transport's already-validated scalar answers to the writer, route,
 * and receipt contracts. */
#include "character_save_journal_v2_rpc_transport.h"
#include "character_save_journal_v2_route.h"

typedef struct character_save_journal_v2_live_ops {
    character_save_journal_v2_rpc_transport *transport;
    /* Caller-owned, explicit timestamp used by bootstrap acquire only. */
    const char *acquire_lease_expires_at;
} character_save_journal_v2_live_ops;

/* `transport` must remain READY for every call.  No connection ownership or
 * clock authority transfers to this adapter. */
void character_save_journal_v2_live_ops_init(
    character_save_journal_v2_live_ops *ops,
    character_save_journal_v2_rpc_transport *transport,
    const char *acquire_lease_expires_at);

/* Implements character_save_journal_v2_writer_epoch_acquire.  On every
 * failure `granted` remains byte-for-byte unchanged. */
int character_save_journal_v2_live_ops_writer_epoch_acquire(
    void *opaque, const character_save_journal_v2_writer_tuple *request,
    character_save_journal_v2_writer_tuple *granted);

/* Explicit renewal leaves expiry policy with the caller.  A successful
 * renewal must echo the exact held tuple; `expires_at` is validated by the
 * transport but deliberately not retained here. */
character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_live_ops_writer_epoch_renew(
    void *opaque, const character_save_journal_v2_writer_tuple *held,
    const char *lease_expires_at);

/* Implements character_save_journal_v2_route_lookup_v3.  It accepts canonical
 * bytes, materializes a bounded NUL-terminated RPC key, and makes one lookup. */
character_save_journal_v2_route_lookup_result
character_save_journal_v2_live_ops_route_lookup_v3(
    void *opaque, const char *persisted_world_id,
    const unsigned char *canonical_legacy_name,
    size_t canonical_legacy_name_length,
    character_save_journal_v2_route_reply_v3 *reply);

/* Implements character_save_journal_v2_receipt_callback. */
character_save_journal_v2_receipt_result
character_save_journal_v2_live_ops_receipt_callback(
    void *opaque, const character_save_journal_v2_receipt *receipt);

#endif
