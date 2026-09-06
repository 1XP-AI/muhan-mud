#ifndef MUHAN_ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_H
#define MUHAN_ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_H

#include <stddef.h>
#include <stdint.h>

/* Detached, default-off local store for canonical AliasTitleSnapshotV1
 * envelopes.  Callers pass a trusted, owner-only directory descriptor; this
 * API never resolves a pathname supplied by an untrusted source. */
#define ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_UUID_LENGTH 36
#define ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_DIGEST_HEX_LENGTH 64
#define ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_MAX_EVENTS 128U

typedef enum alias_title_snapshot_v1_outbox_result {
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_OK = 0,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_EXISTS = 1,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_EXACT_RETRY = 2,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CONFLICT = 3,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_NOT_FOUND = 4,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_LIMIT = -4,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_INVALID = -1,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT = -2,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR = -3
} alias_title_snapshot_v1_outbox_result;

typedef struct alias_title_snapshot_v1_outbox_event {
    char event_uuid[ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_UUID_LENGTH + 1];
    char wire_digest_hex[ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_DIGEST_HEX_LENGTH + 1];
    uint64_t wire_octets;
} alias_title_snapshot_v1_outbox_event;

typedef struct alias_title_snapshot_v1_outbox_report {
    uint64_t visited;
    uint64_t valid;
    uint64_t corrupt;
    uint64_t frozen;
} alias_title_snapshot_v1_outbox_report;

typedef int (*alias_title_snapshot_v1_outbox_visitor)(
    const alias_title_snapshot_v1_outbox_event *, const uint8_t *, size_t,
    void *);

/* Create validates that wire is exactly the canonical, digest-verified CDTO
 * kind 9 representation.  A successful call generates a fresh local event
 * UUID and durably creates exactly one event file. */
int alias_title_snapshot_v1_outbox_create(int trusted_directory_fd,
    const uint8_t *wire, size_t wire_length,
    alias_title_snapshot_v1_outbox_event *created);

/* Compare an existing event with the exact canonical wire passed by the
 * caller.  It reports NOT_FOUND, CORRUPT, EXACT_RETRY, or CONFLICT explicitly. */
int alias_title_snapshot_v1_outbox_retry(int trusted_directory_fd,
    const char *event_uuid, const uint8_t *wire, size_t wire_length,
    alias_title_snapshot_v1_outbox_event *existing);

/* Recover at most ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_MAX_EVENTS, in filename
 * order.  Bad entries are frozen in place and counted; they are never
 * repaired, deleted, or delivered. */
int alias_title_snapshot_v1_outbox_scan(int trusted_directory_fd,
    alias_title_snapshot_v1_outbox_visitor visitor, void *opaque,
    alias_title_snapshot_v1_outbox_report *report);

#ifdef ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TESTING
enum alias_title_snapshot_v1_outbox_test_fault {
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_WRITE = 1,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_FILE_FSYNC = 2,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_CLOSE = 3,
    ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_DIR_FSYNC = 4
};
void alias_title_snapshot_v1_outbox_test_fail_next(int fault);
void alias_title_snapshot_v1_outbox_test_reset_faults(void);
#endif

#endif
