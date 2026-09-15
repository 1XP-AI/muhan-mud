#ifndef CHARACTER_SAVE_JOURNAL_V2_WRITER_H
#define CHARACTER_SAVE_JOURNAL_V2_WRITER_H

#include <stdint.h>
#include <sys/types.h>

#define CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX 64
#define CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN 36
#define CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OPAQUE_BYTES 32

typedef enum character_save_journal_v2_writer_context_status {
    CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID = 1,
    CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE = 2,
    CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK = 3
} character_save_journal_v2_writer_context_status;

/* This is stack-allocatable storage only.  Its bytes deliberately have no
 * credential meaning: writer.c grants authority solely to its registered
 * owner address, owner pid, and private per-open generation. */
typedef struct character_save_journal_v2_writer_context {
    unsigned char opaque[CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OPAQUE_BYTES];
} character_save_journal_v2_writer_context;

/* validate_held copies this only after exact owner/state registry validation. */
typedef struct character_save_journal_v2_writer_tuple {
  char world_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX + 1];
  char writer_instance_id[CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN + 1];
  uint64_t writer_epoch;
} character_save_journal_v2_writer_tuple;

/* The live route injects this DB seam.  `request` contains the secured,
 * lock-held world and writer instance and the already persisted epoch (or
 * zero when only the instance survived a prior crash).  On success `granted`
 * must echo that world and instance exactly and provide a positive epoch. */
typedef int (*character_save_journal_v2_writer_epoch_acquire)(
    void *argument,
    const character_save_journal_v2_writer_tuple *request,
    character_save_journal_v2_writer_tuple *granted);

/* Production bootstrap for live M3 saves.  The candidate is supplied by the
 * caller and must be a canonical lowercase UUID, but is used only when no
 * durable instance exists; restarts always reuse that durable instance.
 * Success returns the same already-held context as writer_open, so the caller
 * must eventually close it. */
int character_save_journal_v2_writer_bootstrap(
    const char *root, const char *world_id, const char *writer_instance_candidate,
    character_save_journal_v2_writer_epoch_acquire acquire, void *acquire_argument,
    character_save_journal_v2_writer_context *out);

/* Test-only local durability primitive.  It never contacts a DB or publishes
 * a player file.  `world_id` must match both immutable persisted tuple files. */
int character_save_journal_v2_writer_open(
    const char *root, const char *world_id,
    character_save_journal_v2_writer_context *out);
int character_save_journal_v2_writer_close(
    character_save_journal_v2_writer_context *context);

/* Read-only held-context revalidation for the route seam.  It first proves
 * the exact registered owner and state, then rechecks descriptor ancestry,
 * lifetime lock descriptor, and the exact persisted tuple. */
character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held(
    const character_save_journal_v2_writer_context *context,
    character_save_journal_v2_writer_tuple *tuple_out);

/* Read-only descriptor handoff for test-only descendants.  On OK it returns
 * a CLOEXEC duplicate of the exact registered MUHAN_HOME descriptor, never a
 * pathname; the caller owns only the duplicate. */
character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_dup_held_root_fd(
    const character_save_journal_v2_writer_context *context, int *root_fd_out);

#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
void character_save_journal_v2_writer_set_trusted_uid_for_test(uid_t uid);
void character_save_journal_v2_writer_fail_fsync_for_test(int lock_file,
                                                           int journal_dir);
void character_save_journal_v2_writer_fail_epoch_create_once_for_test(void);
void character_save_journal_v2_writer_fail_temp_cleanup_fsync_once_for_test(void);
void character_save_journal_v2_writer_crash_after_temp_create_for_test(
    int instance_temp, int epoch_temp);
void character_save_journal_v2_writer_fail_close_once_for_test(int close_kind);
void character_save_journal_v2_writer_pause_after_create_for_test(int ready_fd,
                                                                    int release_fd);
void character_save_journal_v2_writer_reset_fsync_counts_for_test(void);
void character_save_journal_v2_writer_fsync_counts_for_test(unsigned int *lock_file,
                                                              unsigned int *journal_dir);
#endif

#endif
