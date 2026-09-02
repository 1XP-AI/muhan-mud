#ifndef CHARACTER_SAVE_JOURNAL_V2_H
#define CHARACTER_SAVE_JOURNAL_V2_H

#include <stddef.h>
#include <stdint.h>

#define CHARACTER_SAVE_JOURNAL_V2_UUID_LEN 36
#define CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN 64
#define CHARACTER_SAVE_JOURNAL_V2_WORLD_ID_MAX 64
#define CHARACTER_SAVE_JOURNAL_V2_NAME_MAX 14
#define CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX (CHARACTER_SAVE_JOURNAL_V2_NAME_MAX * 2)
#define CHARACTER_SAVE_JOURNAL_V2_STAGE_LEAF_MAX 43
#define CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES (UINT64_C(64) * 1024 * 1024)

typedef enum character_save_journal_v2_state {
    CHARACTER_SAVE_JOURNAL_V2_INVALID = 0,
    CHARACTER_SAVE_JOURNAL_V2_PREPARED = 1
} character_save_journal_v2_state;

typedef enum character_save_journal_v2_expected_state {
    CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING = 1,
    CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT = 2
} character_save_journal_v2_expected_state;

typedef struct character_save_journal_v2_wire {
    character_save_journal_v2_state state;
    char writer_instance_id[CHARACTER_SAVE_JOURNAL_V2_UUID_LEN + 1];
    char character_id[CHARACTER_SAVE_JOURNAL_V2_UUID_LEN + 1];
    char request_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    char world_id[CHARACTER_SAVE_JOURNAL_V2_WORLD_ID_MAX + 1];
    char legacy_name_key_hex[CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX + 1];
    char legacy_shard[3];
    char command_uuid[CHARACTER_SAVE_JOURNAL_V2_UUID_LEN + 1];
    uint64_t writer_epoch;
    uint64_t writer_revision;
    character_save_journal_v2_expected_state expected_state;
    char expected_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    char post_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    uint16_t storage_format;
} character_save_journal_v2_wire;

/* Test-only v2 staging API.  It has no player-store, save_ply, bank, or DB
 * dependency.  `root` is an absolute disposable MUHAN_HOME. */
int character_save_journal_v2_stage_leaf(const char *command_uuid, char *out,
                                         size_t out_size);
int character_save_journal_v2_request_sha256(const character_save_journal_v2_wire *wire,
                                              char out[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1]);
int character_save_journal_v2_prepare(const char *root,
                                      const character_save_journal_v2_wire *wire,
                                      const void *stage_bytes, size_t stage_length);
/* Descriptor-capability variants used while a writer lease is held.  `root_fd`
 * is borrowed: these calls never close it or re-open a root pathname.  Stage
 * first, observe the live precondition next, and only then commit PREPARED. */
int character_save_journal_v2_stage_at(int root_fd,
                                       const character_save_journal_v2_wire *wire,
                                       const void *stage_bytes,
                                       size_t stage_length);
int character_save_journal_v2_live_precondition_at(
    int root_fd, const character_save_journal_v2_wire *wire);
int character_save_journal_v2_commit_prepared_at(
    int root_fd, const character_save_journal_v2_wire *wire);
int character_save_journal_v2_prepare_at(int root_fd,
                                         const character_save_journal_v2_wire *wire,
                                         const void *stage_bytes,
                                         size_t stage_length);
int character_save_journal_v2_read_prepared(const char *root, const char *command_uuid,
                                            character_save_journal_v2_wire *out);
int character_save_journal_v2_read_prepared_at(int root_fd,
                                               const char *command_uuid,
                                               character_save_journal_v2_wire *out);
int character_save_journal_v2_hash_fd(int fd,
                                      char out[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1]);
/* Recovery-only hash for the exact two-name absent-link promotion state. */
int character_save_journal_v2_hash_fd_two_links(int fd,
                                                char out[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1]);

#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
#include <sys/types.h>
void character_save_journal_v2_set_trusted_uid_for_test(uid_t uid);
void character_save_journal_v2_fail_fsync_for_test(int stage_file, int stage_dir,
                                                    int journal_file, int journal_dir);
void character_save_journal_v2_fsync_counts_for_test(unsigned int *stage_file,
                                                      unsigned int *stage_dir,
                                                      unsigned int *journal_file,
                                                      unsigned int *journal_dir);
void character_save_journal_v2_pause_hash_after_fstat_for_test(int ready_fd,
                                                                int release_fd);
void character_save_journal_v2_pause_live_precondition_after_open_for_test(
    int ready_fd, int release_fd);
void character_save_journal_v2_pause_component_after_lstat_for_test(
    const char *component, int ready_fd, int release_fd);
void character_save_journal_v2_write_faults_for_test(int eintr_once,
                                                      int short_once,
                                                      int zero_once,
                                                      int eio_once);
void character_save_journal_v2_fail_close_once_for_test(int close_kind);
#endif

/* Kept entirely out of production objects.  The crash runner selects one of
 * these through its test-only environment and terminates immediately after a
 * successful durability operation. */
#if defined(CHARACTER_SAVE_JOURNAL_V2_TESTING) || \
    defined(CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING) || \
    defined(CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING) || \
    defined(CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING)
typedef enum character_save_journal_v2_crash_cutpoint {
    CHARACTER_SAVE_JOURNAL_V2_CRASH_NONE = 0,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_FILE_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_DIR_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_FILE_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_JOURNAL_DIR_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_RENAMEAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_LIVE_PARENT_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_STAGE_PARENT_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LINKAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LIVE_PARENT_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_PARENT_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_RECEIPT_RETURNED,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_FILE_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_MARKER_LINKAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FIRST_JOURNAL_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_TEMP_UNLINKAT,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_ACKED_FINAL_JOURNAL_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_LIVE_PARENT_FSYNC,
    CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_STAGE_PARENT_FSYNC
} character_save_journal_v2_crash_cutpoint;
#endif

#endif
