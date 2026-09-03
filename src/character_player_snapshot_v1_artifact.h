#ifndef CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_H
#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_H

#include <stddef.h>
#include <stdint.h>

#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH 36
#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH 64
#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEXT_MAX 64
#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT "player-snapshot-v1"
#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS 4194352ULL
#define CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE 56

typedef enum character_player_snapshot_v1_artifact_result {
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK = 0,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CONFLICT = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID = -1,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT = -2,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR = -3,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_LIMIT = -4
} character_player_snapshot_v1_artifact_result;

typedef struct character_player_snapshot_v1_artifact_metadata {
    char world_id[65];
    char character_id[37];
    char command_id[37];
    char canonical_name_hex[29];
    char request_sha256[65];
    char source_post_sha256[65];
    char writer_instance_id[37];
    char snapshot_sha256[65];
    char snapshot_format[sizeof(CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT)];
    uint64_t writer_epoch;
    uint64_t writer_revision;
    uint64_t source_octets;
    uint64_t snapshot_octets;
    int16_t storage_format;
} character_player_snapshot_v1_artifact_metadata;

/* The directory descriptor is a deployment trust boundary: it is duplicated
 * and revalidated, and no caller supplied pathname is reopened.  Store fills
 * snapshot_sha256 and snapshot_octets after canonical CDTO validation. */
int character_player_snapshot_v1_artifact_store(
    int directory_fd, character_player_snapshot_v1_artifact_metadata *metadata,
    const uint8_t *snapshot, size_t snapshot_length);
int character_player_snapshot_v1_artifact_load(
    int directory_fd, const character_player_snapshot_v1_artifact_metadata *key,
    character_player_snapshot_v1_artifact_metadata *metadata,
    uint8_t **snapshot, size_t *snapshot_length);
int character_player_snapshot_v1_artifact_filename(
    const character_player_snapshot_v1_artifact_metadata *metadata,
    char *output, size_t output_size);
void character_player_snapshot_v1_artifact_free(uint8_t *snapshot);

#ifdef CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING
enum character_player_snapshot_v1_artifact_test_fault {
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_WRITE = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_FILE_FSYNC = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_CLOSE = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_FSYNC = 4,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_ALLOC = 5,
    CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_CLOSE = 6
};
void character_player_snapshot_v1_artifact_test_fail_next(int fault);
void character_player_snapshot_v1_artifact_test_reset_faults(void);
#endif

#endif
