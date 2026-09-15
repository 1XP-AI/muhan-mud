#ifndef CHARACTER_SAVE_JOURNAL_H
#define CHARACTER_SAVE_JOURNAL_H

#include "player_path.h"
#include <stdint.h>

#define CHARACTER_SAVE_JOURNAL_VERSION 1
#define CHARACTER_SAVE_JOURNAL_UUID_LEN 36
#define CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN 64
#define CHARACTER_SAVE_JOURNAL_STORAGE_FORMAT_MAX 32
#define CHARACTER_SAVE_JOURNAL_WORLD_ID_MAX 64
#define CHARACTER_SAVE_JOURNAL_LEGACY_SHARD_LEN 2
#define CHARACTER_SAVE_JOURNAL_NAME_HEX_MAX (PLAYER_NAME_MAX_BYTES * 2)

/* Results distinguish a known rejection from a rename-after-parent-fsync
 * ambiguity.  The latter must be reconciled from the on-disk record and must
 * never be blindly retried as a new command. */
#define CHARACTER_SAVE_JOURNAL_OK 0
#define CHARACTER_SAVE_JOURNAL_REJECTED (-1)
#define CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN 1
#define CHARACTER_SAVE_JOURNAL_RECONCILE_REQUIRED 2

typedef enum character_save_journal_state {
    CHARACTER_SAVE_JOURNAL_INVALID = 0,
    CHARACTER_SAVE_JOURNAL_PREPARED,
    CHARACTER_SAVE_JOURNAL_LEGACY_PUBLISHED,
    CHARACTER_SAVE_JOURNAL_DB_ACKED
} character_save_journal_state;

typedef enum character_save_journal_precondition {
    CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING = 1,
    CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT = 2
} character_save_journal_precondition;

typedef struct character_save_journal {
    character_save_journal_state state;
    char command_uuid[CHARACTER_SAVE_JOURNAL_UUID_LEN + 1];
    char canonical_name_hex[CHARACTER_SAVE_JOURNAL_NAME_HEX_MAX + 1];
    character_save_journal_precondition precondition;
    char expected_pre_hash[CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN + 1];
    char post_hash[CHARACTER_SAVE_JOURNAL_HASH_HEX_LEN + 1];
    char storage_format[CHARACTER_SAVE_JOURNAL_STORAGE_FORMAT_MAX + 1];
    char world_id[CHARACTER_SAVE_JOURNAL_WORLD_ID_MAX + 1];
    /* Lowercase SHA-1(name) first-byte hint, derived by player_path. */
    char legacy_shard[CHARACTER_SAVE_JOURNAL_LEGACY_SHARD_LEN + 1];
    uint64_t writer_epoch;
    uint64_t writer_revision;
} character_save_journal;

/* A prepared journal contains only canonical metadata and no player payload.
 * `expected_absent` is an explicit new-file precondition; updates must pass a
 * 64-lowerhex expected_pre_hash.  post_hash is always the intended new bytes'
 * digest, including while state=prepared. */
int character_save_journal_prepare(const char *command_uuid,
                                   const char *canonical_name,
                                   const char *expected_pre_hash,
                                   character_save_journal_precondition precondition,
                                   const char *post_hash,
                                   const char *storage_format,
                                   const char *world_id,
                                   const char *legacy_shard,
                                   uint64_t writer_epoch,
                                   uint64_t writer_revision);
int character_save_journal_mark_legacy_published(
    const char *command_uuid, const char *canonical_name,
    const char *expected_pre_hash, character_save_journal_precondition precondition,
    const char *post_hash,
    const char *storage_format, const char *world_id, const char *legacy_shard,
    uint64_t writer_epoch,
    uint64_t writer_revision);
int character_save_journal_mark_db_acked(
    const char *command_uuid, const char *canonical_name,
    const char *expected_pre_hash, character_save_journal_precondition precondition,
    const char *post_hash, const char *storage_format,
    const char *world_id, const char *legacy_shard, uint64_t writer_epoch,
    uint64_t writer_revision);

int character_save_journal_read(const char *command_uuid,
                                character_save_journal *record);
int character_save_journal_path(const char *command_uuid, char *out,
                                unsigned long out_size);

#ifdef CHARACTER_SAVE_JOURNAL_TESTING
/* Fault injection is compiled only into the hermetic unit binary. */
void character_save_journal_fail_parent_fsync_for_test(int enabled);
void character_save_journal_io_faults_for_test(int write_eintr_once,
                                               int write_short_once,
                                               int read_eintr_once,
                                               int read_short_once);
void character_save_journal_fail_temp_write_for_test(int enabled);
void character_save_journal_fail_temp_fsync_for_test(int enabled);
void character_save_journal_fail_temp_close_once_for_test(int enabled);
void character_save_journal_fail_temp_unlink_once_for_test(int enabled);
#endif

#endif
