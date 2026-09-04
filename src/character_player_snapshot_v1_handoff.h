#ifndef CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_H
#define CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_H

/*
 * A small, durable handoff separates PREPARED observation from PlayerSnapshot
 * capture.  The observer durably copies an immutable, command-specific
 * private source beside its small token; an independently scheduled consumer
 * decodes that retained source later.  The save-stage leaf is never linked:
 * its legacy one-link verification remains authoritative.  The M3 journal
 * and legacy file remain the sole save authority throughout.
 */
#include "character_player_snapshot_v1_capture.h"
#include "character_player_snapshot_v1_receipt_pair.h"

#include <stdint.h>

#define CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY \
    "character-player-snapshot-v1-handoff"
#define CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING 128U

typedef enum character_player_snapshot_v1_handoff_result {
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK = 0,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_FULL = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CONTEXT = 4,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_PREPARED = 5,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT = 6,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CAPTURE = 7,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR = 8,
    /* The consumer durably reclaimed an unrecoverable local reservation
     * without ever creating an artifact.  This is diagnostic, not capture. */
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DROPPED = 9
} character_player_snapshot_v1_handoff_result;

typedef struct character_player_snapshot_v1_handoff_report {
    uint64_t enqueued;
    uint64_t exact_retries;
    uint64_t full;
    uint64_t consumed;
    uint64_t dropped;
    /* Every identity kept out of consumer discovery by durable poison
     * evidence, including unsafe leaves that could not be reclaimed. */
    uint64_t quarantined_poison;
    /* Backward-compatible aggregate of poison identities handled by cleanup
     * or quarantine; quarantined_poison distinguishes the new marker path. */
    uint64_t reclaimed_poison;
    uint64_t failed;
    character_player_snapshot_v1_handoff_result last_result;
    int last_capture_result;
    char last_command_id[37];
} character_player_snapshot_v1_handoff_report;

/* Receipt pairing is a separately linked relay capability.  Ordinary
 * handoff users neither select nor retain it. */
typedef character_player_snapshot_v1_receipt_pair_result
    (*character_player_snapshot_v1_handoff_receipt_pair)(
    const character_save_journal_v2_writer_context *writer,
    int artifact_directory_fd,
    const character_player_snapshot_v1_artifact_metadata *artifact_key);

typedef struct character_player_snapshot_v1_handoff {
    /* The caller owns the capture object and schedules drain independently
     * from the M3 protocol/recovery PREPARED observer. */
    character_player_snapshot_v1_capture *capture;
    character_player_snapshot_v1_handoff_receipt_pair receipt_pair;
    character_player_snapshot_v1_handoff_report report;
} character_player_snapshot_v1_handoff;

void character_player_snapshot_v1_handoff_init(
    character_player_snapshot_v1_handoff *handoff,
    character_player_snapshot_v1_capture *capture);

/* Enables the optional DB_ACKED-to-manifest relay only for this handoff.
 * Passing a null capability leaves the default capture-to-cleanup consumer
 * unchanged. */
void character_player_snapshot_v1_handoff_enable_receipt_pair(
    character_player_snapshot_v1_handoff *handoff,
    character_player_snapshot_v1_handoff_receipt_pair receipt_pair);

/* Matches character_save_journal_v2_prepared_stage_observer.  A nonzero
 * result is diagnostic only: callers must not let a full or unavailable
 * shadow handoff gate legacy publish/ACK. */
int character_player_snapshot_v1_handoff_observe(
    void *opaque,
    const character_save_journal_v2_writer_context *writer,
    const char *command_uuid);

/* Explicitly separate consumer.  It processes at most limit durable tokens,
 * removes a token only after capture has stored an artifact or verified its
 * exact retry, and leaves failures for a later consumer/recovery pass. */
int character_player_snapshot_v1_handoff_drain(
    character_player_snapshot_v1_handoff *handoff,
    const character_save_journal_v2_writer_context *writer,
    unsigned int limit);

#ifdef CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TESTING
enum character_player_snapshot_v1_handoff_test_fault {
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_WRITE = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_FILE_FSYNC = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_LINK = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_UNLINK = 4,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_DIRECTORY_FSYNC = 5,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_DIRECTORY_FSYNC = 6,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_QUEUE_CREATE_FSYNC = 7,
    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TEST_FAULT_SOURCE_RENAME = 8
};
void character_player_snapshot_v1_handoff_test_fail_next(int fault);
void character_player_snapshot_v1_handoff_test_reset_faults(void);
#endif

#endif
