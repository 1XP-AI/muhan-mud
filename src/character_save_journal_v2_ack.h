#ifndef CHARACTER_SAVE_JOURNAL_V2_ACK_H
#define CHARACTER_SAVE_JOURNAL_V2_ACK_H

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

/* Acknowledge only an already durable LEGACY_PUBLISHED journal.  The command
 * identity is read from descriptor-relative immutable evidence; `command_id`
 * merely selects its derived leaves.  UNAVAILABLE and TIMEOUT preserve all
 * evidence and return DEFERRED. */
character_save_journal_v2_ack_result character_save_journal_v2_ack(
    const character_save_journal_v2_writer_context *writer,
    const char *command_id, character_save_journal_v2_receipt_callback callback,
    void *callback_opaque);

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
