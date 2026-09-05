#ifndef MUHAN_BANK_SNAPSHOT_V1_ARTIFACT_H
#define MUHAN_BANK_SNAPSHOT_V1_ARTIFACT_H

#include <stddef.h>
#include <stdint.h>

#define BANK_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH 36
#define BANK_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH 64
#define BANK_SNAPSHOT_V1_ARTIFACT_TEXT_MAX 64
#define BANK_SNAPSHOT_V1_ARTIFACT_FORMAT "bank-snapshot-v1"
#define BANK_SNAPSHOT_V1_ARTIFACT_MAX_BANK_OCTETS (4U * 1024U * 1024U)
#define BANK_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE 55

typedef enum bank_snapshot_v1_artifact_result {
    BANK_SNAPSHOT_V1_ARTIFACT_OK = 0,
    BANK_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY = 1,
    BANK_SNAPSHOT_V1_ARTIFACT_CONFLICT = 2,
    BANK_SNAPSHOT_V1_ARTIFACT_NOT_FOUND = 3,
    BANK_SNAPSHOT_V1_ARTIFACT_INVALID = -1,
    BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT = -2,
    BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR = -3,
    BANK_SNAPSHOT_V1_ARTIFACT_LIMIT = -4
} bank_snapshot_v1_artifact_result;

typedef struct bank_snapshot_v1_artifact_metadata {
    char artifact_format[sizeof(BANK_SNAPSHOT_V1_ARTIFACT_FORMAT)];
    char world_id[65];
    char character_id[37];
    char command_id[37];
    char canonical_name_hex[29];
    char request_sha256[65];
    char source_post_sha256[65];
    char bank_sha256[65];
    uint64_t writer_epoch;
    uint64_t writer_revision;
    uint64_t bank_octets;
} bank_snapshot_v1_artifact_metadata;

/* The supplied directory descriptor is duplicated and revalidated; all file
 * access remains descriptor-relative.  Store fills the canonical bank hash
 * and byte count only after canonical BankSnapshotV1 validation. */
int bank_snapshot_v1_artifact_store(
    int directory_fd, bank_snapshot_v1_artifact_metadata *metadata,
    const uint8_t *bank, size_t bank_length);
int bank_snapshot_v1_artifact_load(
    int directory_fd, const bank_snapshot_v1_artifact_metadata *key,
    bank_snapshot_v1_artifact_metadata *metadata,
    uint8_t **bank, size_t *bank_length);
int bank_snapshot_v1_artifact_filename(
    const bank_snapshot_v1_artifact_metadata *metadata,
    char *output, size_t output_size);
void bank_snapshot_v1_artifact_free(uint8_t *bank);

#ifdef BANK_SNAPSHOT_V1_ARTIFACT_TESTING
enum bank_snapshot_v1_artifact_test_fault {
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_WRITE = 1,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_FILE_FSYNC = 2,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_CLOSE = 3,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_FSYNC = 4,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_ALLOC = 5,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_CLOSE = 6,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_FILE_FSYNC_EINTR = 7,
    BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_FSYNC_EINTR = 8
};
void bank_snapshot_v1_artifact_test_fail_next(int fault);
void bank_snapshot_v1_artifact_test_reset_faults(void);
void bank_snapshot_v1_artifact_test_fsync_counts(unsigned int *file_count,
                                                   unsigned int *directory_count);
#endif

#endif
