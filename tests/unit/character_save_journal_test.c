#include "character_save_journal.h"

#include <errno.h>
#include <fcntl.h>
#include <dirent.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "character_save_journal_test: %s\n", message);
    return 1;
}

static int read_file(path, out, out_size)
const char *path;
char *out;
unsigned long out_size;
{
    int fd, n;
    if(!path || !out || out_size < 2) return -1;
    fd = open(path, O_RDONLY);
    if(fd < 0) return -1;
    n = read(fd, out, out_size - 1);
    if(n < 0 || read(fd, out + (n < 0 ? 0 : n), 1) != 0 || close(fd) < 0)
        return -1;
    out[n] = 0;
    return n;
}

static int write_file(path, text)
const char *path;
const char *text;
{
    int fd, n;
    fd = open(path, O_WRONLY | O_TRUNC);
    if(fd < 0) return -1;
    n = write(fd, text, strlen(text));
    if(n != (int)strlen(text) || close(fd) < 0) return -1;
    return 0;
}

static int test_lifecycle(root)
char *root;
{
    static const char command[] = "11111111-1111-4111-8111-111111111111";
    static const char other_command[] = "22222222-2222-4222-8222-222222222222";
    static const char conflict_command[] = "44444444-4444-4444-8444-444444444444";
    static const char io_command[] = "55555555-5555-4555-8555-555555555555";
    static const char shard_command[] = "66666666-6666-4666-8666-666666666666";
    static const char fail_command[] = "77777777-7777-4777-8777-777777777777";
    static const char fsync_fail_command[] = "88888888-8888-4888-8888-888888888888";
    static const char close_fail_command[] = "99999999-9999-4999-8999-999999999999";
    static const char unlink_fail_command[] = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
    static const char pre[] = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    static const char post[] = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789";
    character_save_journal record;
    char path[1024], before[896], after[896], too_long[PLAYER_NAME_MAX_BYTES + 2];
    char max_name_shard[3], max_shard[3];
    struct stat st;
    int failed = 0, fd;

    if(setenv("MUHAN_HOME", root, 1) != 0) return 1;
    if(player_path_shard_from_name("Terra", max_name_shard) != 0) return 1;
    if(player_path_shard_from_name("ééééééé", max_shard) != 0) return 1;
    failed += expect(character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) == 0,
                     "prepared journal must accept signed fencing upper bound");
    failed += expect(character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, 0, 1) < 0 &&
                     character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard,
                         UINT64_MAX, 1) < 0,
                     "zero and unsigned-overflow fencing values must reject");
    failed += expect(character_save_journal_read(command, &record) == 0 &&
                     record.state == CHARACTER_SAVE_JOURNAL_PREPARED &&
                     !strcmp(record.canonical_name_hex, "5465727261") &&
                     !strcmp(record.expected_pre_hash, pre) &&
                     !strcmp(record.post_hash, post) &&
                     record.writer_epoch == (uint64_t)INT64_MAX,
                     "prepared record must preserve canonical bytes and both hashes");
    failed += expect(character_save_journal_path(command, path, sizeof(path)) == 0 &&
                     stat(path, &st) == 0 && (st.st_mode & 0777) == 0600,
                     "journal file must be private");
    if(read_file(path, before, sizeof(before)) < 0) failed++;
    failed += expect(strstr(before, "expected_precondition=existing\n") != 0 &&
                     strstr(before, "post_hash=") != 0,
                     "wire record must include explicit precondition and intended post hash");
    failed += expect(character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) == 0 &&
                     read_file(path, after, sizeof(after)) >= 0 &&
                     !strcmp(before, after),
                     "same command and payload must be byte-idempotent");
    failed += expect(character_save_journal_mark_db_acked(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0,
                     "state skip to db_acked must reject");
    failed += expect(character_save_journal_mark_legacy_published(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) == 0,
                     "prepared to legacy_published must succeed");
    failed += expect(character_save_journal_mark_legacy_published(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) == 0,
                     "same legacy transition retry must be idempotent");
    failed += expect(character_save_journal_mark_db_acked(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) == 0 &&
                     character_save_journal_read(command, &record) == 0 &&
                     record.state == CHARACTER_SAVE_JOURNAL_DB_ACKED,
                     "legacy_published to db_acked must succeed");
    failed += expect(character_save_journal_prepare(
                         command, "Other", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0,
                     "same command with different name must reject");
    failed += expect(character_save_journal_prepare(
                         command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0,
                     "existing-file update must reject an absent/unknown pre-hash");
    failed += expect(character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "other-world", max_name_shard, INT64_MAX, INT64_MAX) < 0,
                     "same command must reject a different world routing identity");
    failed += expect(character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "Muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0 &&
                     character_save_journal_prepare(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", "0", INT64_MAX, INT64_MAX) < 0,
                     "world id and legacy shard must use strict routing syntax");
    failed += expect(character_save_journal_prepare(
                         command, "tERRA", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0 &&
                     character_save_journal_prepare(
                         command, "TErra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0,
                     "noncanonical ASCII casing must reject");
    failed += expect(character_save_journal_mark_db_acked(
                         command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING,
                         "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
                         "player-v1", "muhan", max_name_shard, INT64_MAX, INT64_MAX) < 0,
                     "same command with different post hash must reject");
    failed += expect(character_save_journal_prepare(
                         conflict_command, "ééééééé", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_shard, 1, 1) == 0,
                     "maximum 14-byte canonical name must be accepted");
    failed += expect(character_save_journal_prepare(
                         conflict_command, "Terra", pre,
                         CHARACTER_SAVE_JOURNAL_EXPECT_EXISTING, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) < 0,
                     "existing update with the same command but different payload must reject");
    failed += expect(character_save_journal_mark_legacy_published(
                         conflict_command, "ééééééé", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT,
                         "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
                         "player-v1", "muhan", max_shard, 1, 1) < 0,
                     "transition must keep the prepared post hash immutable");
    if(character_save_journal_path(conflict_command, path, sizeof(path)) == 0)
        unlink(path);
    character_save_journal_io_faults_for_test(1, 1, 0, 0);
    failed += expect(character_save_journal_prepare(
                         io_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) == 0,
                     "short writes and EINTR writes must complete safely");
    character_save_journal_io_faults_for_test(0, 0, 1, 1);
    failed += expect(character_save_journal_read(io_command, &record) == 0,
                     "short reads and EINTR reads must complete safely");
    character_save_journal_io_faults_for_test(0, 0, 0, 0);
    if(character_save_journal_path(io_command, path, sizeof(path)) == 0)
        unlink(path);
    failed += expect(character_save_journal_prepare(
                         shard_command, "Terra1140", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", "af", 1, 1) == 0,
                     "SHA-1 first-byte shard af must be accepted");
    failed += expect(character_save_journal_prepare(
                         shard_command, "Terra1140", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", "gg", 1, 1) < 0,
                     "non-hex shard must reject");
    if(character_save_journal_path(shard_command, path, sizeof(path)) == 0)
        unlink(path);
    failed += expect(character_save_journal_prepare(
                         fail_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 0, 1) < 0,
                     "zero fencing epoch must reject for a new command");
    character_save_journal_fail_temp_write_for_test(1);
    failed += expect(character_save_journal_prepare(
                         fail_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) < 0,
                     "injected temp write failure must reject");
    character_save_journal_fail_temp_write_for_test(0);
    {
        char journal_dir[1024];
        char *slash;
        DIR *directory;
        struct dirent *entry;
        int found_temp = 0;
        if(character_save_journal_path(fail_command, journal_dir,
                                       sizeof(journal_dir)) == 0 &&
           (slash = strrchr(journal_dir, '/')) != 0) {
            *slash = 0;
            directory = opendir(journal_dir);
            if(directory) {
                while((entry = readdir(directory)) != 0)
                    if(!strncmp(entry->d_name, ".save-journal.tmp.", 18))
                        found_temp = 1;
                closedir(directory);
            }
            failed += expect(!found_temp,
                             "failed temp publication must leave no staging file");
        }
    }
    character_save_journal_fail_temp_fsync_for_test(1);
    failed += expect(character_save_journal_prepare(
                         fsync_fail_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) < 0,
                     "injected temp fsync failure must reject");
    character_save_journal_fail_temp_fsync_for_test(0);
    if(character_save_journal_path(fsync_fail_command, path, sizeof(path)) == 0)
        unlink(path);
    character_save_journal_fail_temp_close_once_for_test(1);
    failed += expect(character_save_journal_prepare(
                         close_fail_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) < 0,
                     "ambiguous temp close failure must reject");
    character_save_journal_fail_temp_close_once_for_test(0);
    failed += expect(character_save_journal_path(close_fail_command, path,
                                                 sizeof(path)) == 0 &&
                     access(path, F_OK) < 0 && errno == ENOENT &&
                     character_save_journal_prepare(
                         close_fail_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) == 0,
                     "close failure must relinquish the fd once and clean staging");
    if(character_save_journal_path(close_fail_command, path, sizeof(path)) == 0)
        unlink(path);
    character_save_journal_fail_temp_unlink_once_for_test(1);
    failed += expect(character_save_journal_prepare(
                         unlink_fail_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, 1, 1) ==
                         CHARACTER_SAVE_JOURNAL_RECONCILE_REQUIRED,
                     "unlink failure after canonical publication must require reconcile");
    character_save_journal_fail_temp_unlink_once_for_test(0);
    {
        char journal_dir[1024], temp_path[1200];
        char *slash;
        DIR *directory;
        struct dirent *entry;
        struct stat canonical_st, temp_st;
        int found_temp = 0;
        temp_path[0] = 0;
        if(character_save_journal_path(unlink_fail_command, path,
                                       sizeof(path)) == 0 &&
           stat(path, &canonical_st) == 0) {
            strcpy(journal_dir, path);
            slash = strrchr(journal_dir, '/');
            if(slash) {
                *slash = 0;
                directory = opendir(journal_dir);
                if(directory) {
                    while((entry = readdir(directory)) != 0) {
                        if(!strncmp(entry->d_name, ".save-journal.tmp.", 18)) {
                            found_temp++;
                            snprintf(temp_path, sizeof(temp_path), "%s/%s",
                                     journal_dir, entry->d_name);
                        }
                    }
                    closedir(directory);
                }
            }
            failed += expect(found_temp == 1 && temp_path[0] &&
                             stat(temp_path, &temp_st) == 0 &&
                             canonical_st.st_dev == temp_st.st_dev &&
                             canonical_st.st_ino == temp_st.st_ino,
                             "reconcile-required must retain the linked staging artifact");
            if(temp_path[0]) unlink(temp_path);
            unlink(path);
        }
        else failed++;
    }
    if(character_save_journal_path(command, path, sizeof(path)) != 0) failed++;
    failed += expect(character_save_journal_mark_legacy_published(
                         other_command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, UINT64_C(7), UINT64_C(1)) < 0,
                     "db ack without a prepared record must reject");
    memset(too_long, 'N', sizeof(too_long) - 1);
    too_long[sizeof(too_long) - 1] = 0;
    failed += expect(character_save_journal_prepare(
                         other_command, too_long, 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", max_name_shard, UINT64_C(7), UINT64_C(1)) < 0,
                     "overlong canonical name must reject at the API boundary");

    /* A malformed/unknown wire field is never interpreted as a new payload. */
    if(write_file(path,
        "version=1\nstate=db_acked\ncommand_uuid=11111111-1111-4111-8111-111111111111\n"
        "canonical_name_hex=5465727261\nexpected_precondition=existing\n"
        "expected_pre_hash=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"
        "post_hash=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789\n"
        "storage_format=player-v1\nworld_id=muhan\nlegacy_shard=16\n"
        "writer_epoch=18446744073709551616\nwriter_revision=42\n"
        "unknown=reject\n") != 0) failed++;
    failed += expect(character_save_journal_read(command, &record) < 0,
                     "overflow and unknown wire fields must reject");
    fd = open(path, O_WRONLY | O_TRUNC);
    if(fd >= 0) {
        close(fd);
        unlink(path);
    }
    if(mkdir(path, 0700) == 0) {
        failed += expect(character_save_journal_read(command, &record) < 0,
                         "nonregular journal path must reject");
        rmdir(path);
    }
    if(mkfifo(path, 0600) == 0) {
        failed += expect(character_save_journal_read(command, &record) < 0,
                         "FIFO journal path must reject without blocking");
        unlink(path);
    }
    if(symlink("outside-journal-target", path) == 0) {
        failed += expect(character_save_journal_read(command, &record) < 0,
                         "journal symlink must reject");
        unlink(path);
    }
    {
        char journal_dir[1024];
        char *slash;
        if(character_save_journal_path(command, journal_dir, sizeof(journal_dir)) == 0 &&
           (slash = strrchr(journal_dir, '/')) != 0) {
            *slash = 0;
            if(chmod(journal_dir, 0750) == 0) {
                failed += expect(character_save_journal_prepare(
                                     other_command, "Terra", 0,
                                     CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                                     "player-v1", "muhan", max_name_shard, 1, 1) < 0,
                                 "journal directory with non-private mode must reject");
                chmod(journal_dir, 0700);
            }
        }
    }
    {
        char journal_dir[1024];
        char *slash;
        if(character_save_journal_path(command, journal_dir, sizeof(journal_dir)) == 0 &&
           (slash = strrchr(journal_dir, '/')) != 0) {
            *slash = 0;
            character_save_journal_path(command, path, sizeof(path)); unlink(path);
            character_save_journal_path(conflict_command, path, sizeof(path)); unlink(path);
            character_save_journal_path(io_command, path, sizeof(path)); unlink(path);
            rmdir(journal_dir);
        }
    }
    unsetenv("MUHAN_HOME");
    return failed;
}

static int test_rename_uncertain(root)
char *root;
{
    static const char command[] = "33333333-3333-4333-8333-333333333333";
    static const char post[] = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789";
    character_save_journal record;
    int failed = 0;
    if(setenv("MUHAN_HOME", root, 1) != 0) return 1;
    failed += expect(character_save_journal_prepare(
                         command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", "16", UINT64_C(8), UINT64_C(2)) == 0,
                     "explicit absent precondition must prepare");
#ifdef CHARACTER_SAVE_JOURNAL_TESTING
    character_save_journal_fail_parent_fsync_for_test(1);
    failed += expect(character_save_journal_mark_legacy_published(
                         command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", "16", UINT64_C(8), UINT64_C(2)) ==
                         CHARACTER_SAVE_JOURNAL_RENAME_DURABILITY_UNCERTAIN,
                     "parent fsync failure after rename must be explicit uncertain result");
    character_save_journal_fail_parent_fsync_for_test(0);
    failed += expect(character_save_journal_read(command, &record) == 0 &&
                     record.state == CHARACTER_SAVE_JOURNAL_LEGACY_PUBLISHED &&
                     character_save_journal_mark_legacy_published(
                         command, "Terra", 0,
                         CHARACTER_SAVE_JOURNAL_EXPECT_ABSENT, post,
                         "player-v1", "muhan", "16", UINT64_C(8), UINT64_C(2)) == 0,
                     "uncertain result must be reconciled from the durable record");
#endif
    {
        char journal_dir[1024], path[1024];
        char *slash;
        if(character_save_journal_path(command, path, sizeof(path)) == 0) {
            unlink(path);
            strcpy(journal_dir, path);
            slash = strrchr(journal_dir, '/');
            if(slash) { *slash = 0; rmdir(journal_dir); }
        }
    }
    unsetenv("MUHAN_HOME");
    return failed;
}

int main(void)
{
    char first[] = "/tmp/character-save-journal-test-XXXXXX";
    char second[] = "/tmp/character-save-journal-test-XXXXXX";
    int failed;
    if(!mkdtemp(first) || !mkdtemp(second)) return 1;
    failed = test_lifecycle(first) + test_rename_uncertain(second);
    rmdir(first);
    rmdir(second);
    if(failed) return 1;
    puts("character_save_journal_test: ok");
    return 0;
}
