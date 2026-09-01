#ifndef CHARACTER_SAVE_JOURNAL_V2_WRITER_H
#define CHARACTER_SAVE_JOURNAL_V2_WRITER_H

#include <stdint.h>
#include <sys/types.h>

#define CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX 64
#define CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN 36

typedef struct character_save_journal_v2_writer_context {
    int root_fd;
    int journal_fd;
    int lock_fd;
    char world_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX + 1];
    char writer_instance_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN + 1];
    uint64_t writer_epoch;
} character_save_journal_v2_writer_context;

/* Test-only local durability primitive.  It never contacts a DB or publishes
 * a player file.  `world_id` must match both immutable persisted tuple files. */
int character_save_journal_v2_writer_open(
    const char *root, const char *world_id,
    character_save_journal_v2_writer_context *out);
int character_save_journal_v2_writer_close(
    character_save_journal_v2_writer_context *context);

#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
void character_save_journal_v2_writer_set_trusted_uid_for_test(uid_t uid);
void character_save_journal_v2_writer_fail_fsync_for_test(int lock_file,
                                                           int journal_dir);
void character_save_journal_v2_writer_fail_close_once_for_test(int close_kind);
void character_save_journal_v2_writer_pause_after_create_for_test(int ready_fd,
                                                                    int release_fd);
void character_save_journal_v2_writer_reset_fsync_counts_for_test(void);
void character_save_journal_v2_writer_fsync_counts_for_test(unsigned int *lock_file,
                                                              unsigned int *journal_dir);
#endif

#endif
