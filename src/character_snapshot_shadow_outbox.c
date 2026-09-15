/* Immutable local metadata outbox.  This module intentionally knows nothing
 * about database delivery or gameplay authority. */
#include "character_snapshot_shadow_outbox.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "O_NOFOLLOW is required"
#endif

#define CSO_MAX_FILE 2048U
#define CSO_MAX_NAMES 256U
#define CSO_NAME_LENGTH 46U

typedef struct cso_candidate {
    char name[CSO_NAME_LENGTH];
    character_snapshot_shadow_outbox_manifest manifest;
    int valid;
} cso_candidate;

#ifdef CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TESTING
static int cso_test_fault;

void character_snapshot_shadow_outbox_test_fail_next(fault)
int fault;
{
    cso_test_fault = fault;
}

void character_snapshot_shadow_outbox_test_reset_faults(void)
{
    cso_test_fault = 0;
}

static int cso_fault(fault)
int fault;
{
    if(cso_test_fault != fault) return 0;
    cso_test_fault = 0;
    errno = EIO;
    return 1;
}
#else
static int cso_fault(fault)
int fault;
{
    (void)fault;
    return 0;
}
#endif

static unsigned long cso_bounded(value, maximum)
const char *value;
unsigned long maximum;
{
    unsigned long index;

    if(!value) return maximum + 1U;
    for(index = 0; index <= maximum; ++index)
        if(!value[index]) return index;
    return maximum + 1U;
}

static int cso_lower_hex(value)
char value;
{
    return (value >= '0' && value <= '9') ||
           (value >= 'a' && value <= 'f');
}

static int cso_uuid(value)
const char *value;
{
    unsigned long index;

    if(cso_bounded(value, CHARACTER_SNAPSHOT_SHADOW_OUTBOX_UUID_LENGTH) !=
       CHARACTER_SNAPSHOT_SHADOW_OUTBOX_UUID_LENGTH) return 0;
    for(index = 0; index < CHARACTER_SNAPSHOT_SHADOW_OUTBOX_UUID_LENGTH; ++index) {
        if(index == 8U || index == 13U || index == 18U || index == 23U) {
            if(value[index] != '-') return 0;
        } else if(!cso_lower_hex(value[index])) return 0;
    }
    return 1;
}

static int cso_hash(value)
const char *value;
{
    unsigned long index;

    if(cso_bounded(value, CHARACTER_SNAPSHOT_SHADOW_OUTBOX_HASH_LENGTH) !=
       CHARACTER_SNAPSHOT_SHADOW_OUTBOX_HASH_LENGTH) return 0;
    for(index = 0; index < CHARACTER_SNAPSHOT_SHADOW_OUTBOX_HASH_LENGTH; ++index)
        if(!cso_lower_hex(value[index])) return 0;
    return 1;
}

static int cso_world(value)
const char *value;
{
    unsigned long length, index;

    length = cso_bounded(value, CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEXT_MAX);
    if(!length || length > CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEXT_MAX ||
       value[0] < 'a' || value[0] > 'z') return 0;
    for(index = 1; index < length; ++index) {
        if(!((value[index] >= 'a' && value[index] <= 'z') ||
             (value[index] >= '0' && value[index] <= '9') ||
             value[index] == '_' || value[index] == '-')) return 0;
    }
    return 1;
}

static int cso_name_hex(value)
const char *value;
{
    unsigned long length, index;

    length = cso_bounded(value, 28U);
    if(length < 2U || length > 28U || (length & 1U)) return 0;
    for(index = 0; index < length; ++index)
        if(!cso_lower_hex(value[index])) return 0;
    return 1;
}

static int cso_manifest_valid(manifest)
const character_snapshot_shadow_outbox_manifest *manifest;
{
    if(!manifest || !cso_world(manifest->world_id) ||
       !cso_uuid(manifest->character_id) || !cso_uuid(manifest->command_id) ||
       !cso_uuid(manifest->writer_instance_id) ||
       !cso_name_hex(manifest->canonical_name_hex) ||
       !cso_hash(manifest->request_sha256) || !cso_hash(manifest->post_sha256) ||
       cso_bounded(manifest->snapshot_format,
                   sizeof(CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT) - 1U) !=
                   sizeof(CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT) - 1U ||
       strcmp(manifest->snapshot_format,
              CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT) ||
       manifest->writer_epoch == 0 || manifest->writer_revision == 0 ||
       manifest->writer_epoch > INT64_MAX || manifest->writer_revision > INT64_MAX ||
       manifest->storage_format <= 0 ||
       manifest->snapshot_octets == 0 ||
       manifest->snapshot_octets > CHARACTER_SNAPSHOT_SHADOW_OUTBOX_MAX_SNAPSHOT_OCTETS)
        return 0;
    return 1;
}

static int cso_encode(buffer, capacity, manifest)
char *buffer;
unsigned long capacity;
const character_snapshot_shadow_outbox_manifest *manifest;
{
    int length;

    length = snprintf(buffer, capacity,
        "version=1\n"
        "world_id=%s\n"
        "character_id=%s\n"
        "command_id=%s\n"
        "canonical_name_hex=%s\n"
        "request_sha256=%s\n"
        "post_sha256=%s\n"
        "writer_instance_id=%s\n"
        "snapshot_format=%s\n"
        "writer_epoch=%llu\n"
        "writer_revision=%llu\n"
        "storage_format=%d\n"
        "snapshot_octets=%llu\n",
        manifest->world_id, manifest->character_id, manifest->command_id,
        manifest->canonical_name_hex, manifest->request_sha256,
        manifest->post_sha256, manifest->writer_instance_id,
        manifest->snapshot_format,
        (unsigned long long)manifest->writer_epoch,
        (unsigned long long)manifest->writer_revision,
        (int)manifest->storage_format,
        (unsigned long long)manifest->snapshot_octets);
    if(length < 0 || (unsigned long)length >= capacity) return -1;
    return length;
}

static int cso_close(fd, directory)
int fd;
int directory;
{
    int result;

    result = close(fd);
    if(cso_fault(directory ? 6 : 3)) return -1;
    return result;
}

static int cso_closedir(directory)
DIR *directory;
{
    int result;

    result = closedir(directory);
    if(cso_fault(6)) return -1;
    return result;
}

static int cso_fsync(fd, directory)
int fd;
int directory;
{
    if(cso_fault(directory ? 4 : 2)) return -1;
    return fsync(fd);
}

static void *cso_malloc(size)
size_t size;
{
    if(cso_fault(5)) return 0;
    return malloc(size);
}

static int cso_root_duplicate(directory_fd)
int directory_fd;
{
    int duplicate;
    struct stat metadata;

    if(directory_fd < 0) return -1;
    duplicate = fcntl(directory_fd, F_DUPFD_CLOEXEC, 3);
    if(duplicate < 0) return -1;
    if(fstat(duplicate, &metadata) < 0 || !S_ISDIR(metadata.st_mode) ||
       metadata.st_uid != geteuid() || (metadata.st_mode & 0777) != 0700) {
        cso_close(duplicate, 1);
        return -1;
    }
    return duplicate;
}

static int cso_write_all(fd, bytes, length)
int fd;
const char *bytes;
unsigned long length;
{
    ssize_t written;

    while(length) {
        if(cso_fault(1)) return -1;
        written = write(fd, bytes, length);
        if(written < 0 && errno == EINTR) continue;
        if(written <= 0) return -1;
        bytes += written;
        length -= (unsigned long)written;
    }
    return 0;
}

static int cso_file_safe(file_fd, directory_metadata, before)
int file_fd;
const struct stat *directory_metadata;
struct stat *before;
{
    if(fstat(file_fd, before) < 0 || !S_ISREG(before->st_mode) ||
       before->st_nlink != 1 || before->st_uid != directory_metadata->st_uid ||
       (before->st_mode & 0777) != 0600 || before->st_size < 0 ||
       (uint64_t)before->st_size >= CSO_MAX_FILE) return -1;
    return 0;
}

static int cso_same_file(left, right)
const struct stat *left;
const struct stat *right;
{
    return left->st_dev == right->st_dev && left->st_ino == right->st_ino &&
           left->st_size == right->st_size && left->st_uid == right->st_uid &&
           left->st_nlink == right->st_nlink && left->st_mode == right->st_mode;
}

/* -2 means the directory entry disappeared; -1 means unsafe/I/O failure. */
static int cso_read_manifest(directory_fd, name, buffer, capacity, length)
int directory_fd;
const char *name;
char *buffer;
unsigned long capacity;
unsigned long *length;
{
    int file_fd, result;
    ssize_t amount;
    struct stat directory_metadata, before, after;
    unsigned long used;

    *length = 0;
    if(fstat(directory_fd, &directory_metadata) < 0) return -1;
    file_fd = openat(directory_fd, name,
        O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(file_fd < 0) return errno == ENOENT ? -2 : -1;
    result = cso_file_safe(file_fd, &directory_metadata, &before);
    used = 0;
    while(!result && used < (unsigned long)before.st_size) {
        amount = read(file_fd, buffer + used,
            (size_t)((unsigned long)before.st_size - used));
        if(amount < 0 && errno == EINTR) continue;
        if(amount <= 0) { result = -1; break; }
        used += (unsigned long)amount;
    }
    if(!result) {
        do {
            amount = read(file_fd, buffer + used, 1);
        } while(amount < 0 && errno == EINTR);
        if(amount != 0 || fstat(file_fd, &after) < 0 ||
           !cso_same_file(&before, &after)) result = -1;
    }
    if(cso_close(file_fd, 0) < 0) result = -1;
    if(result) return -1;
    if(used >= capacity) return -1;
    buffer[used] = 0;
    *length = used;
    return 0;
}

static int cso_parse_number(value, length, output)
const char *value;
unsigned long length;
uint64_t *output;
{
    uint64_t parsed;
    unsigned long index;

    if(!length || length > 19U || value[0] == '0') return -1;
    parsed = 0;
    for(index = 0; index < length; ++index) {
        if(value[index] < '0' || value[index] > '9') return -1;
        if(parsed > ((uint64_t)INT64_MAX - (uint64_t)(value[index] - '0')) / 10U)
            return -1;
        parsed = parsed * 10U + (uint64_t)(value[index] - '0');
    }
    if(!parsed) return -1;
    *output = parsed;
    return 0;
}

static int cso_parse_text(destination, capacity, value, length)
char *destination;
unsigned long capacity;
const char *value;
unsigned long length;
{
    if(length >= capacity) return -1;
    memcpy(destination, value, length);
    destination[length] = 0;
    return 0;
}

static int cso_line(cursor, remaining, expected, value, value_length)
const char **cursor;
unsigned long *remaining;
const char *expected;
const char **value;
unsigned long *value_length;
{
    const char *newline;
    unsigned long prefix, line_length;

    prefix = (unsigned long)strlen(expected);
    if(*remaining < prefix + 2U || memcmp(*cursor, expected, prefix) ||
       (*cursor)[prefix] != '=') return -1;
    newline = (const char *)memchr(*cursor + prefix + 1U, '\n',
        *remaining - prefix - 1U);
    if(!newline) return -1;
    line_length = (unsigned long)(newline - (*cursor + prefix + 1U));
    *value = *cursor + prefix + 1U;
    *value_length = line_length;
    line_length += prefix + 2U;
    *cursor += line_length;
    *remaining -= line_length;
    return 0;
}

static int cso_parse(bytes, length, manifest)
const char *bytes;
unsigned long length;
character_snapshot_shadow_outbox_manifest *manifest;
{
    static const char *names[] = {
        "version", "world_id", "character_id", "command_id",
        "canonical_name_hex", "request_sha256", "post_sha256",
        "writer_instance_id", "snapshot_format", "writer_epoch",
        "writer_revision", "storage_format", "snapshot_octets"
    };
    const char *cursor, *value;
    unsigned long remaining, value_length, index;
    uint64_t number;
    char canonical[CSO_MAX_FILE];
    int canonical_length;

    if(!bytes || !manifest || !length || length >= CSO_MAX_FILE ||
       bytes[length - 1U] != '\n' || memchr(bytes, 0, length)) return -1;
    memset(manifest, 0, sizeof(*manifest));
    cursor = bytes;
    remaining = length;
    for(index = 0; index < 13U; ++index) {
        if(cso_line(&cursor, &remaining, names[index], &value, &value_length))
            return -1;
        if(index == 0U) {
            if(value_length != 1U || value[0] != '1') return -1;
        } else if(index == 1U) {
            if(cso_parse_text(manifest->world_id, sizeof(manifest->world_id), value, value_length)) return -1;
        } else if(index == 2U) {
            if(cso_parse_text(manifest->character_id, sizeof(manifest->character_id), value, value_length)) return -1;
        } else if(index == 3U) {
            if(cso_parse_text(manifest->command_id, sizeof(manifest->command_id), value, value_length)) return -1;
        } else if(index == 4U) {
            if(cso_parse_text(manifest->canonical_name_hex, sizeof(manifest->canonical_name_hex), value, value_length)) return -1;
        } else if(index == 5U) {
            if(cso_parse_text(manifest->request_sha256, sizeof(manifest->request_sha256), value, value_length)) return -1;
        } else if(index == 6U) {
            if(cso_parse_text(manifest->post_sha256, sizeof(manifest->post_sha256), value, value_length)) return -1;
        } else if(index == 7U) {
            if(cso_parse_text(manifest->writer_instance_id, sizeof(manifest->writer_instance_id), value, value_length)) return -1;
        } else if(index == 8U) {
            if(cso_parse_text(manifest->snapshot_format, sizeof(manifest->snapshot_format), value, value_length)) return -1;
        } else if(index == 9U) {
            if(cso_parse_number(value, value_length, &number)) return -1;
            manifest->writer_epoch = number;
        } else if(index == 10U) {
            if(cso_parse_number(value, value_length, &number)) return -1;
            manifest->writer_revision = number;
        } else if(index == 11U) {
            if(cso_parse_number(value, value_length, &number) || number > INT16_MAX) return -1;
            manifest->storage_format = (int16_t)number;
        } else {
            if(cso_parse_number(value, value_length, &number)) return -1;
            manifest->snapshot_octets = number;
        }
    }
    if(remaining || !cso_manifest_valid(manifest)) return -1;
    canonical_length = cso_encode(canonical, sizeof(canonical), manifest);
    if(canonical_length < 0 || (unsigned long)canonical_length != length ||
       memcmp(canonical, bytes, length)) return -1;
    return 0;
}

static int cso_equal(left, right)
const character_snapshot_shadow_outbox_manifest *left;
const character_snapshot_shadow_outbox_manifest *right;
{
    return !strcmp(left->world_id, right->world_id) &&
           !strcmp(left->character_id, right->character_id) &&
           !strcmp(left->command_id, right->command_id) &&
           !strcmp(left->canonical_name_hex, right->canonical_name_hex) &&
           !strcmp(left->request_sha256, right->request_sha256) &&
           !strcmp(left->post_sha256, right->post_sha256) &&
           !strcmp(left->writer_instance_id, right->writer_instance_id) &&
           !strcmp(left->snapshot_format, right->snapshot_format) &&
           left->writer_epoch == right->writer_epoch &&
           left->writer_revision == right->writer_revision &&
           left->snapshot_octets == right->snapshot_octets &&
           left->storage_format == right->storage_format;
}

static int cso_name(manifest, name)
const character_snapshot_shadow_outbox_manifest *manifest;
char name[CSO_NAME_LENGTH];
{
    int length;

    length = snprintf(name, CSO_NAME_LENGTH, "%s.manifest", manifest->command_id);
    return length == 45 ? 0 : -1;
}

int character_snapshot_shadow_outbox_write(directory_fd, manifest)
int directory_fd;
const character_snapshot_shadow_outbox_manifest *manifest;
{
    int root_fd, file_fd, result, close_result;
    char bytes[CSO_MAX_FILE], name[CSO_NAME_LENGTH];
    struct stat directory_metadata, created_metadata, sealed_metadata;
    struct stat published_metadata;

    if(!cso_manifest_valid(manifest) || cso_encode(bytes, sizeof(bytes), manifest) < 0 ||
       cso_name(manifest, name)) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID;
    root_fd = cso_root_duplicate(directory_fd);
    if(root_fd < 0) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    file_fd = openat(root_fd, name,
        O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC, 0600);
    if(file_fd < 0) {
        result = errno == EEXIST ? CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXISTS :
                                  CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
        if(cso_close(root_fd, 1) < 0) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
        return result;
    }
    result = 0;
    if(fstat(root_fd, &directory_metadata) < 0 ||
       cso_file_safe(file_fd, &directory_metadata, &created_metadata) < 0 ||
       cso_write_all(file_fd, bytes, (unsigned long)strlen(bytes)) < 0 ||
       cso_fsync(file_fd, 0) < 0) result = -1;
    if(!result && (cso_file_safe(file_fd, &directory_metadata,
                                 &sealed_metadata) < 0 ||
       created_metadata.st_dev != sealed_metadata.st_dev ||
       created_metadata.st_ino != sealed_metadata.st_ino ||
       sealed_metadata.st_size != (off_t)strlen(bytes) ||
       fstatat(root_fd, name, &published_metadata, AT_SYMLINK_NOFOLLOW) < 0 ||
       !cso_same_file(&sealed_metadata, &published_metadata))) result = -1;
    close_result = cso_close(file_fd, 0);
    if(close_result < 0) result = -1;
    if(!result && cso_fsync(root_fd, 1) < 0) result = -1;
    if(cso_close(root_fd, 1) < 0) result = -1;
    return result ? CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR :
                    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK;
}

int character_snapshot_shadow_outbox_retry(directory_fd, manifest, existing)
int directory_fd;
const character_snapshot_shadow_outbox_manifest *manifest;
character_snapshot_shadow_outbox_manifest *existing;
{
    int root_fd, read_result, close_result;
    char bytes[CSO_MAX_FILE], name[CSO_NAME_LENGTH];
    unsigned long length;

    if(existing) memset(existing, 0, sizeof(*existing));
    if(!existing || !cso_manifest_valid(manifest) || cso_name(manifest, name))
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID;
    root_fd = cso_root_duplicate(directory_fd);
    if(root_fd < 0) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    read_result = cso_read_manifest(root_fd, name, bytes, sizeof(bytes), &length);
    close_result = cso_close(root_fd, 1);
    if(close_result < 0) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    if(read_result == -2) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_NOT_FOUND;
    if(read_result || cso_parse(bytes, length, existing)) {
        memset(existing, 0, sizeof(*existing));
        return read_result ? CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR :
                             CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT;
    }
    return cso_equal(manifest, existing) ? CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY :
                                            CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CONFLICT;
}

static int cso_compare_names(left, right)
const void *left;
const void *right;
{
    const cso_candidate *left_candidate;
    const cso_candidate *right_candidate;

    left_candidate = (const cso_candidate *)left;
    right_candidate = (const cso_candidate *)right;
    return strcmp(left_candidate->name, right_candidate->name);
}

int character_snapshot_shadow_outbox_scan(directory_fd, visitor, opaque, report)
int directory_fd;
character_snapshot_shadow_outbox_visitor visitor;
void *opaque;
character_snapshot_shadow_outbox_report *report;
{
    int root_fd, directory_copy, close_result, read_result, result;
    DIR *directory;
    struct dirent *entry;
    cso_candidate *candidates;
    char bytes[CSO_MAX_FILE], command[37];
    unsigned long count, index, length, name_length;

    if(!visitor || !report) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID;
    memset(report, 0, sizeof(*report));
    root_fd = cso_root_duplicate(directory_fd);
    if(root_fd < 0) return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    candidates = (cso_candidate *)cso_malloc(CSO_MAX_NAMES * sizeof(*candidates));
    if(!candidates) {
        cso_close(root_fd, 1);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    directory_copy = fcntl(root_fd, F_DUPFD_CLOEXEC, 3);
    if(directory_copy < 0) {
        free(candidates);
        cso_close(root_fd, 1);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    if(lseek(directory_copy, 0, SEEK_SET) < 0) {
        cso_close(directory_copy, 1);
        free(candidates);
        cso_close(root_fd, 1);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    directory = fdopendir(directory_copy);
    if(!directory) {
        cso_close(directory_copy, 1);
        free(candidates);
        cso_close(root_fd, 1);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    count = 0;
    errno = 0;
    while((entry = readdir(directory)) != 0) {
        name_length = cso_bounded(entry->d_name, CSO_NAME_LENGTH - 1U);
        if(name_length < 10U || name_length >= CSO_NAME_LENGTH ||
           strcmp(entry->d_name + name_length - 9U, ".manifest")) continue;
        report->visited++;
        if(count == CSO_MAX_NAMES) {
            close_result = cso_closedir(directory);
            result = close_result < 0 ?
                CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR :
                CHARACTER_SNAPSHOT_SHADOW_OUTBOX_LIMIT;
            lseek(root_fd, 0, SEEK_SET);
            free(candidates);
            cso_close(root_fd, 1);
            return result;
        }
        memset(&candidates[count], 0, sizeof(candidates[count]));
        memcpy(candidates[count].name, entry->d_name, name_length + 1U);
        ++count;
        errno = 0;
    }
    read_result = errno;
    close_result = cso_closedir(directory);
    if(read_result != 0 || close_result < 0) {
        lseek(root_fd, 0, SEEK_SET);
        free(candidates);
        cso_close(root_fd, 1);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    if(lseek(root_fd, 0, SEEK_SET) < 0) {
        free(candidates);
        cso_close(root_fd, 1);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    qsort(candidates, count, sizeof(*candidates), cso_compare_names);
    for(index = 0; index < count; ++index) {
        name_length = (unsigned long)strlen(candidates[index].name);
        if(name_length != 45U) {
            report->corrupt++;
            report->frozen++;
            continue;
        }
        memcpy(command, candidates[index].name, 36U);
        command[36] = 0;
        read_result = cso_read_manifest(root_fd, candidates[index].name,
            bytes, sizeof(bytes), &length);
        if(read_result == -2 || (!read_result &&
           cso_parse(bytes, length, &candidates[index].manifest))) {
            report->corrupt++;
            report->frozen++;
            continue;
        }
        if(read_result) {
            free(candidates);
            cso_close(root_fd, 1);
            return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
        }
        if(strcmp(command, candidates[index].manifest.command_id)) {
            report->corrupt++;
            report->frozen++;
            continue;
        }
        candidates[index].valid = 1;
        report->valid++;
    }
    close_result = cso_close(root_fd, 1);
    if(close_result < 0) {
        free(candidates);
        return CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
    }
    result = CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK;
    for(index = 0; index < count; ++index) {
        if(candidates[index].valid && visitor(&candidates[index].manifest, opaque)) {
            result = CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR;
            break;
        }
    }
    free(candidates);
    if(result != CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK) return result;
    return report->corrupt ? CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT :
                             CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK;
}
