#include "character_save_journal_v2_recovery.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "v2 recovery requires O_NOFOLLOW"
#endif
#ifndef O_DIRECTORY
#error "v2 recovery requires O_DIRECTORY"
#endif

#define V2_RECOVERY_UUID_LEN 36
#define V2_RECOVERY_PREPARED_SUFFIX ".prepared"
#define V2_RECOVERY_PREPARED_SUFFIX_LEN 9
#define V2_RECOVERY_PREPARED_LEN 45

typedef struct v2_recovery_entry {
    character_save_journal_v2_wire wire;
} v2_recovery_entry;

#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
static unsigned int v2_recovery_entry_cap = CHARACTER_SAVE_JOURNAL_V2_RECOVERY_MAX_ENTRIES;
static int v2_recovery_fail_allocation;
static int v2_recovery_fail_closedir;
static int v2_recovery_fail_root_close;
static int v2_recovery_scan_fd_cloexec;
#endif

static int rec_uuid(text)
const char *text;
{
    size_t i;
    if(!text) return 0;
    for(i = 0; i < V2_RECOVERY_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(text[i] != '-') return 0;
        } else if(!((text[i] >= '0' && text[i] <= '9') ||
                    (text[i] >= 'a' && text[i] <= 'f'))) return 0;
    }
    return text[V2_RECOVERY_UUID_LEN] == 0;
}

static int rec_context_result(status)
character_save_journal_v2_writer_context_status status;
{
    if(status == CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE)
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_STALE;
    if(status == CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK)
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_LOCK;
    return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_INVALID;
}

static int rec_directory_ok(fd, uid)
int fd;
uid_t uid;
{
    struct stat st;
    return fd >= 0 && fstat(fd, &st) == 0 && S_ISDIR(st.st_mode) &&
        st.st_uid == uid && (st.st_mode & 07777) == 0700;
}

/* Opening after lstat makes a directory-entry swap fail closed rather than
 * turning the snapshot into an authority grant for a replacement leaf. */
static int rec_preflight_file(journal_fd, leaf, uid)
int journal_fd;
const char *leaf;
uid_t uid;
{
    struct stat before, after;
    int fd, close_result;
    if(fstatat(journal_fd, leaf, &before, AT_SYMLINK_NOFOLLOW) != 0 ||
       !S_ISREG(before.st_mode) || before.st_uid != uid ||
       (before.st_mode & 07777) != 0600 || before.st_nlink != 1) return -1;
    fd = openat(journal_fd, leaf,
                O_RDONLY | O_NONBLOCK | O_NOFOLLOW | O_CLOEXEC);
    if(fd < 0) return -1;
    close_result = fstat(fd, &after);
    if(close(fd) != 0) close_result = -1;
    if(close_result != 0 || !S_ISREG(after.st_mode) || after.st_uid != uid ||
       (after.st_mode & 07777) != 0600 || after.st_nlink != 1 ||
       before.st_dev != after.st_dev || before.st_ino != after.st_ino) return -1;
    return 0;
}

static int rec_snapshot_open(root_fd, journal_fd_out, uid_out)
int root_fd;
int *journal_fd_out;
uid_t *uid_out;
{
    struct stat root_st, before, after;
    int fd;
    if(!journal_fd_out || !uid_out || fstat(root_fd, &root_st) != 0 ||
       !S_ISDIR(root_st.st_mode) || (root_st.st_mode & 07777) != 0700) return -1;
    if(fstatat(root_fd, "character-save-journal", &before,
               AT_SYMLINK_NOFOLLOW) != 0 || !S_ISDIR(before.st_mode) ||
       before.st_uid != root_st.st_uid || (before.st_mode & 07777) != 0700)
        return -1;
    fd = openat(root_fd, "character-save-journal",
                O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if(!rec_directory_ok(fd, before.st_uid) || fstat(fd, &after) != 0 ||
       before.st_dev != after.st_dev || before.st_ino != after.st_ino) {
        if(fd >= 0) close(fd);
        return -1;
    }
    *journal_fd_out = fd;
    *uid_out = before.st_uid;
    return 0;
}

static int rec_entry_compare(left, right)
const void *left;
const void *right;
{
    const v2_recovery_entry *left_entry = left;
    const v2_recovery_entry *right_entry = right;
    int result;
    result = memcmp(left_entry->wire.character_id, right_entry->wire.character_id,
                    V2_RECOVERY_UUID_LEN);
    if(result) return result;
    if(left_entry->wire.writer_revision < right_entry->wire.writer_revision) return -1;
    if(left_entry->wire.writer_revision > right_entry->wire.writer_revision) return 1;
    return memcmp(left_entry->wire.command_uuid, right_entry->wire.command_uuid,
                  V2_RECOVERY_UUID_LEN);
}

static int rec_snapshot_entry(root_fd, command_uuid, tuple, entry)
int root_fd;
const char *command_uuid;
const character_save_journal_v2_writer_tuple *tuple;
v2_recovery_entry *entry;
{
    character_save_journal_v2_wire wire;
    if(!tuple || !entry ||
       character_save_journal_v2_read_prepared_at(root_fd, command_uuid, &wire) != 0 ||
       strcmp(wire.command_uuid, command_uuid) ||
       strcmp(wire.world_id, tuple->world_id) ||
       strcmp(wire.writer_instance_id, tuple->writer_instance_id) ||
       wire.writer_epoch != tuple->writer_epoch || !wire.writer_revision) {
        memset(&wire, 0, sizeof(wire));
        return -1;
    }
    entry->wire = wire;
    memset(&wire, 0, sizeof(wire));
    return 0;
}

static int rec_same_character(left, right)
const v2_recovery_entry *left;
const v2_recovery_entry *right;
{
    return !memcmp(left->wire.character_id, right->wire.character_id,
                   V2_RECOVERY_UUID_LEN);
}

static int rec_same_legacy_path(left, right)
const v2_recovery_entry *left;
const v2_recovery_entry *right;
{
    return !strcmp(left->wire.legacy_shard, right->wire.legacy_shard) &&
        !strcmp(left->wire.legacy_name_key_hex, right->wire.legacy_name_key_hex);
}

/* A retained backlog is authoritative only when every later mutation is the
 * exact next state of the same legacy leaf.  Validate all of it before the
 * first publish or receipt so no prefix can become visible on a bad snapshot. */
static int rec_snapshot_validate(entries, count)
const v2_recovery_entry *entries;
unsigned int count;
{
    unsigned int i, j;
    for(i = 1; i < count; i++) {
        const character_save_journal_v2_wire *previous = &entries[i - 1].wire;
        const character_save_journal_v2_wire *current = &entries[i].wire;
        if(!rec_same_character(&entries[i - 1], &entries[i])) continue;
        if(strcmp(previous->legacy_name_key_hex, current->legacy_name_key_hex) ||
           strcmp(previous->legacy_shard, current->legacy_shard) ||
           previous->storage_format != current->storage_format ||
           strcmp(previous->world_id, current->world_id) ||
           strcmp(previous->writer_instance_id, current->writer_instance_id) ||
           previous->writer_epoch != current->writer_epoch ||
           current->writer_revision <= previous->writer_revision ||
           current->writer_revision != previous->writer_revision + 1 ||
           current->expected_state != CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ||
           strcmp(current->expected_sha256, previous->post_sha256)) return -1;
    }
    for(i = 0; i < count; i++) {
        for(j = i + 1; j < count; j++) {
            if(!rec_same_character(&entries[i], &entries[j]) &&
               rec_same_legacy_path(&entries[i], &entries[j])) return -1;
        }
    }
    return 0;
}

static int rec_report_zero(report)
const character_save_journal_v2_recovery_report *report;
{
    const unsigned char *bytes = (const unsigned char *)report;
    size_t i;
    if(!report) return 0;
    for(i = 0; i < sizeof(*report); i++) if(bytes[i]) return 0;
    return 1;
}

static void rec_publish_count(report, result)
character_save_journal_v2_recovery_report *report;
character_save_journal_v2_publish_result result;
{
    if((unsigned int)result <= CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO)
        report->publish_results[(unsigned int)result]++;
}

static void rec_ack_count(report, result)
character_save_journal_v2_recovery_report *report;
character_save_journal_v2_ack_result result;
{
    if((unsigned int)result <= CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE)
        report->ack_results[(unsigned int)result]++;
}

character_save_journal_v2_recovery_result
character_save_journal_v2_recovery_run(writer, receipt_callback, receipt_opaque,
                                        report_out)
const character_save_journal_v2_writer_context *writer;
character_save_journal_v2_receipt_callback receipt_callback;
void *receipt_opaque;
character_save_journal_v2_recovery_report *report_out;
{
    character_save_journal_v2_recovery_report report;
    character_save_journal_v2_writer_context_status status;
    v2_recovery_entry *entries = 0;
    struct dirent *entry;
    unsigned int count = 0, cap = CHARACTER_SAVE_JOURNAL_V2_RECOVERY_MAX_ENTRIES;
    int root_fd = -1, journal_fd = -1, scan_fd = -1;
    DIR *directory = 0;
    uid_t trusted_uid;
    int scan_error = 0, incomplete = 0, root_close_result, blocked_character = 0;
    character_save_journal_v2_writer_tuple tuple;
    unsigned int i;
    if(!writer || !receipt_callback || !rec_report_zero(report_out))
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INVALID_ARGUMENT;
    memset(&report, 0, sizeof(report));
    status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK)
        return (character_save_journal_v2_recovery_result)rec_context_result(status);
    if(rec_snapshot_open(root_fd, &journal_fd, &trusted_uid) != 0) {
        close(root_fd);
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_JOURNAL;
    }
    memset(&tuple, 0, sizeof(tuple));
    status = character_save_journal_v2_writer_validate_held(writer, &tuple);
    if(status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        close(journal_fd);
        close(root_fd);
        return (character_save_journal_v2_recovery_result)rec_context_result(status);
    }
    scan_fd = fcntl(journal_fd, F_DUPFD_CLOEXEC, 0);
    if(scan_fd >= 0) {
#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
        v2_recovery_scan_fd_cloexec = (fcntl(scan_fd, F_GETFD) & FD_CLOEXEC) != 0;
#endif
    }
    if(scan_fd < 0 || !(directory = fdopendir(scan_fd))) {
        if(scan_fd >= 0) close(scan_fd);
        close(journal_fd);
        close(root_fd);
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_JOURNAL;
    }
    scan_fd = -1;
#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
    cap = v2_recovery_entry_cap;
#endif
    if(cap > CHARACTER_SAVE_JOURNAL_V2_RECOVERY_MAX_ENTRIES)
        cap = CHARACTER_SAVE_JOURNAL_V2_RECOVERY_MAX_ENTRIES;
#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
    if(v2_recovery_fail_allocation) {
        v2_recovery_fail_allocation = 0;
        scan_error = 1;
    } else
#endif
    entries = malloc((size_t)cap * sizeof(*entries));
    if(!entries && cap) scan_error = 1;
    errno = 0;
    while(!scan_error && (entry = readdir(directory)) != 0) {
        size_t length = strlen(entry->d_name);
        if(length < V2_RECOVERY_PREPARED_SUFFIX_LEN ||
           strcmp(entry->d_name + length - V2_RECOVERY_PREPARED_SUFFIX_LEN,
                  V2_RECOVERY_PREPARED_SUFFIX) != 0) continue;
        if(length != V2_RECOVERY_PREPARED_LEN ||
           entry->d_name[V2_RECOVERY_UUID_LEN] != '.' ||
           memcmp(entry->d_name + V2_RECOVERY_UUID_LEN,
                  V2_RECOVERY_PREPARED_SUFFIX,
                  V2_RECOVERY_PREPARED_SUFFIX_LEN) != 0) {
            scan_error = 2;
            break;
        }
        {
            char uuid[V2_RECOVERY_UUID_LEN + 1];
            memcpy(uuid, entry->d_name, V2_RECOVERY_UUID_LEN);
            uuid[V2_RECOVERY_UUID_LEN] = 0;
            if(!rec_uuid(uuid) || rec_preflight_file(journal_fd, entry->d_name,
                                                      trusted_uid) != 0 || count >= cap ||
               rec_snapshot_entry(root_fd, uuid, &tuple, &entries[count]) != 0) {
                scan_error = 2;
                break;
            }
            count++;
        }
    }
    if(!scan_error && errno != 0) scan_error = 2;
#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
    if(v2_recovery_fail_closedir) {
        v2_recovery_fail_closedir = 0;
        (void)closedir(directory);
        directory = 0;
        scan_error = 2;
    } else
#endif
    if(directory && closedir(directory) != 0) scan_error = 2;
    directory = 0;
    if(close(journal_fd) != 0) scan_error = 2;
    journal_fd = -1;
    root_close_result = close(root_fd);
    root_fd = -1;
#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
    if(v2_recovery_fail_root_close) {
        v2_recovery_fail_root_close = 0;
        root_close_result = -1;
    }
#endif
    if(root_close_result != 0) {
        free(entries);
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_JOURNAL;
    }
    if(scan_error) {
        free(entries);
        return scan_error == 1 ? CHARACTER_SAVE_JOURNAL_V2_RECOVERY_NOMEM :
            CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE;
    }
    if(count > 1) qsort(entries, count, sizeof(*entries), rec_entry_compare);
    if(rec_snapshot_validate(entries, count) != 0) {
        free(entries);
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_STRUCTURE;
    }
    report.discovered = count;
    for(i = 0; i < count; i++) {
        character_save_journal_v2_publish_result published;
        character_save_journal_v2_ack_result acknowledged;
        int same_character = i && rec_same_character(&entries[i - 1], &entries[i]);
        if(!same_character) blocked_character = 0;
        if(blocked_character) continue;
        report.visited++;
        report.publish_attempted++;
        published = character_save_journal_v2_publish_recover(writer,
                                                                 entries[i].wire.command_uuid);
        rec_publish_count(&report, published);
        if(published == CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_INVALID ||
           published == CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_STALE ||
           published == CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_LOCK) {
            free(entries); *report_out = report;
            return published == CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_STALE ?
                CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_STALE :
                (published == CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_LOCK ?
                 CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_LOCK :
                 CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_INVALID);
        }
        if(published != CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK) {
            incomplete = 1;
            blocked_character = 1;
            continue;
        }
        report.ack_attempted++;
        acknowledged = character_save_journal_v2_ack(writer, entries[i].wire.command_uuid,
                                                       receipt_callback, receipt_opaque);
        rec_ack_count(&report, acknowledged);
        if(acknowledged == CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_INVALID ||
           acknowledged == CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_STALE ||
           acknowledged == CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_LOCK) {
            free(entries); *report_out = report;
            if(acknowledged == CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_STALE)
                return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_STALE;
            if(acknowledged == CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_LOCK)
                return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_LOCK;
            if(acknowledged == CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_INVALID)
                return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_INVALID;
            return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_CONTEXT_INVALID;
        }
        if(acknowledged != CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED) {
            incomplete = 1;
            blocked_character = 1;
        }
    }
    free(entries);
    *report_out = report;
    return incomplete ? CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE :
        CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK;
}

#ifdef CHARACTER_SAVE_JOURNAL_V2_RECOVERY_TESTING
void character_save_journal_v2_recovery_set_entry_cap_for_test(cap)
unsigned int cap;
{ v2_recovery_entry_cap = cap; }
void character_save_journal_v2_recovery_fail_allocation_for_test(enabled)
int enabled;
{ v2_recovery_fail_allocation = enabled; }
void character_save_journal_v2_recovery_fail_closedir_for_test(enabled)
int enabled;
{ v2_recovery_fail_closedir = enabled; }
void character_save_journal_v2_recovery_fail_root_close_for_test(enabled)
int enabled;
{ v2_recovery_fail_root_close = enabled; }
int character_save_journal_v2_recovery_scan_fd_cloexec_for_test(void)
{ return v2_recovery_scan_fd_cloexec; }
#endif
