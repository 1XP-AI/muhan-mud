#ifndef CHARACTER_SNAPSHOT_SHADOW_OUTBOX_H
#define CHARACTER_SNAPSHOT_SHADOW_OUTBOX_H

#include <stdint.h>

#define CHARACTER_SNAPSHOT_SHADOW_OUTBOX_UUID_LENGTH 36
#define CHARACTER_SNAPSHOT_SHADOW_OUTBOX_HASH_LENGTH 64
#define CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEXT_MAX 64
#define CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT "legacy-file-manifest-v1"
#define CHARACTER_SNAPSHOT_SHADOW_OUTBOX_MAX_SNAPSHOT_OCTETS 67108864ULL

typedef enum character_snapshot_shadow_outbox_result {
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK = 0,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXISTS = 1,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY = 2,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CONFLICT = 3,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_NOT_FOUND = 4,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_LIMIT = -4,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID = -1,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT = -2,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR = -3
} character_snapshot_shadow_outbox_result;

typedef struct character_snapshot_shadow_outbox_manifest {
    char world_id[65];
    char character_id[37];
    char command_id[37];
    char canonical_name_hex[29];
    char request_sha256[65];
    char post_sha256[65];
    char writer_instance_id[37];
    char snapshot_format[sizeof(CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT)];
    uint64_t writer_epoch;
    uint64_t writer_revision;
    uint64_t snapshot_octets;
    int16_t storage_format;
} character_snapshot_shadow_outbox_manifest;

typedef struct character_snapshot_shadow_outbox_report {
    uint64_t visited;
    uint64_t valid;
    uint64_t corrupt;
    uint64_t frozen;
} character_snapshot_shadow_outbox_report;

typedef int (*character_snapshot_shadow_outbox_visitor)(
    const character_snapshot_shadow_outbox_manifest *, void *);

/* The caller supplies a directory descriptor from its trusted deployment
 * boundary.  The outbox duplicates and validates it on every call; it never
 * reopens a caller-controlled pathname. */
int character_snapshot_shadow_outbox_write(
    int outbox_directory_fd,
    const character_snapshot_shadow_outbox_manifest *manifest);
int character_snapshot_shadow_outbox_retry(
    int outbox_directory_fd,
    const character_snapshot_shadow_outbox_manifest *manifest,
    character_snapshot_shadow_outbox_manifest *existing);
int character_snapshot_shadow_outbox_scan(
    int outbox_directory_fd,
    character_snapshot_shadow_outbox_visitor visitor,
    void *opaque,
    character_snapshot_shadow_outbox_report *report);

#ifdef CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING
enum character_snapshot_shadow_outbox_test_fault {
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_WRITE = 1,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_FILE_FSYNC = 2,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_CLOSE = 3,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_DIR_FSYNC = 4,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_ALLOC = 5,
    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_DIR_CLOSE = 6
};
void character_snapshot_shadow_outbox_test_fail_next(int fault);
void character_snapshot_shadow_outbox_test_reset_faults(void);
#endif

#endif
