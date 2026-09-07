#ifndef CHARACTER_SAVE_JOURNAL_V2_ACK_H
#define CHARACTER_SAVE_JOURNAL_V2_ACK_H

#include "character_save_journal_v2.h"
#include "character_save_journal_v2_writer.h"

#include <stddef.h>

/* Test-only DB-receipt seam.  These are the exact scalar fields accepted by
 * private.record_legacy_published_receipt; no connection, route reply, path,
 * or player bytes cross this boundary. */
typedef struct character_save_journal_v2_receipt {
    const char *world_id;
    const unsigned char *legacy_name_key;
    size_t legacy_name_key_length;
    const char *character_id;
    const char *command_id;
    const char *writer_instance_id;
    const char *request_sha256;
    unsigned long long writer_epoch;
    unsigned long long writer_revision;
    const char *expected_state;
    const char *expected_sha256; /* NULL exactly when expected_state is absent. */
    const char *post_sha256;
    unsigned int storage_format;
} character_save_journal_v2_receipt;

typedef enum character_save_journal_v2_receipt_result {
    CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED = 0,
    /* timeout, offline, or otherwise no known DB result */
    CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED = 1,
    /* SQLSTATE 22023: malformed/canonical receipt is frozen evidence. */
    CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE = 2,
    /* SQLSTATE P0001: fence/route/CAS rejection is frozen evidence. */
    CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE = 3
} character_save_journal_v2_receipt_result;

typedef character_save_journal_v2_receipt_result
    (*character_save_journal_v2_receipt_callback)(
    void *opaque, const character_save_journal_v2_receipt *receipt);

typedef enum character_save_journal_v2_ack_result {
    CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED = 0,
    CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED = 1,
    CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_ARGUMENT = 2,
    CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_INVALID = 3,
    CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_STALE = 4,
    CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_LOCK = 5,
    CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL = 6,
    CHARACTER_SAVE_JOURNAL_V2_ACK_LIVE = 7,
    CHARACTER_SAVE_JOURNAL_V2_ACK_IO = 8,
    CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE = 9,
    CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE = 10,
    /* The DB callback returned ACKED, but post-callback held-writer/live
     * revalidation or local DB_ACKED durability did not complete. Immutable
     * evidence is retained; a later exact callback retry is required. */
    CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE = 11
} character_save_journal_v2_ack_result;

/* A descriptor-rooted read-only proof for detached consumers.  ACKED is
 * returned only for a one-link, canonical DB_ACKED marker and no retained
 * marker temporary; callback outcomes are intentionally not accepted here. */
typedef enum character_save_journal_v2_ack_marker_result {
    CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_ACKED = 0,
    CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_NOT_ACKED = 1,
    CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_LOCAL_INCOMPLETE = 2,
    CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_INVALID_ARGUMENT = 3,
    CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_CONTEXT = 4,
    CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_JOURNAL = 5
} character_save_journal_v2_ack_marker_result;

/* `wire_out` receives the immutable PREPARED tuple only on ACKED.  The
 * caller supplies no pathname: the held writer capability selects the root. */
character_save_journal_v2_ack_marker_result
character_save_journal_v2_ack_marker_verify(
    const character_save_journal_v2_writer_context *writer,
    const char *command_id, character_save_journal_v2_wire *wire_out);

/* Acknowledge only an already durable LEGACY_PUBLISHED journal.  The command
 * identity is read from descriptor-relative immutable evidence; `command_id`
 * merely selects its derived leaves.  UNAVAILABLE and TIMEOUT preserve all
 * evidence and return DEFERRED. */
character_save_journal_v2_ack_result character_save_journal_v2_ack(
    const character_save_journal_v2_writer_context *writer,
    const char *command_id, character_save_journal_v2_receipt_callback callback,
    void *callback_opaque);

/* Recovery-only, read-only historical receipt replay. Command IDs name a
 * contiguous chain from the already ACKed receipt to a published live anchor.
 * Every identity and hash is reread from canonical journal evidence. */
character_save_journal_v2_ack_result character_save_journal_v2_ack_replay_history(
    const character_save_journal_v2_writer_context *writer,
    const char *const *command_ids, size_t command_count,
    character_save_journal_v2_receipt_callback callback, void *callback_opaque);

/* No callback or mutation. ACK_ACKED plus eligible_out=0 means only that the
 * final candidate has no published marker; callers may try a shorter chain.
 * All other missing/malformed evidence fails closed. */
character_save_journal_v2_ack_result character_save_journal_v2_ack_history_probe(
    const character_save_journal_v2_writer_context *writer,
    const char *const *command_ids, size_t command_count, int *eligible_out);

#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
#include <sys/types.h>
void character_save_journal_v2_ack_set_trusted_uid_for_test(uid_t uid);
void character_save_journal_v2_ack_fail_after_temp_for_test(int enabled);
void character_save_journal_v2_ack_fail_marker_fsync_for_test(int enabled);
/* Each non-zero fault is consumed exactly once.  They exercise descriptor
 * ownership and the DB_ACKED link/fsync/unlink durability sequence. */
void character_save_journal_v2_ack_faults_for_test(
    int read_close_once, int live_close_once, int marker_close_once,
    int post_link_once, int first_parent_fsync_once, int temp_unlink_once,
    int final_parent_fsync_once);
#endif

#endif
