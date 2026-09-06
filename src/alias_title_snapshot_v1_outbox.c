/* Default-off local durability boundary with no external runtime dependency. */
#include "alias_title_snapshot_v1_outbox.h"

#include "alias_title_snapshot_v1.h"
#include "cdto_v1.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "O_NOFOLLOW is required"
#endif

#define ATSO_PREFIX "ats-"
#define ATSO_SUFFIX ".event"
#define ATSO_NAME_LENGTH 47U
#define ATSO_MAX_FILE (CDTO_V1_ALIAS_TITLE_SNAPSHOT_PAYLOAD_LIMIT + 512U)
#define ATSO_HEADER_MAX 192U

typedef struct atso_candidate { char name[ATSO_NAME_LENGTH]; } atso_candidate;

#ifdef ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TESTING
static int atso_test_fault;
void alias_title_snapshot_v1_outbox_test_fail_next(int fault)
{ atso_test_fault = fault; }
void alias_title_snapshot_v1_outbox_test_reset_faults(void)
{ atso_test_fault = 0; }
static int atso_fault(int fault)
{ if(atso_test_fault != fault) return 0; atso_test_fault = 0; errno = EIO; return 1; }
#else
static int atso_fault(int fault)
{ (void)fault; return 0; }
#endif

static int atso_close(int fd)
{ int result = close(fd); if(atso_fault(3)) return -1; return result; }
static int atso_fsync(int fd, int directory)
{ if(atso_fault(directory ? 4 : 2)) return -1; return fsync(fd); }

static unsigned long atso_bounded(const char *value, unsigned long maximum)
{ unsigned long i; if(!value) return maximum + 1U; for(i = 0; i <= maximum; ++i) if(!value[i]) return i; return maximum + 1U; }
static int atso_hex(char value)
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }
static int atso_uuid(const char *value)
{ unsigned long i; if(atso_bounded(value, 36U) != 36U) return 0; for(i = 0; i < 36U; ++i) { if(i == 8U || i == 13U || i == 18U || i == 23U) { if(value[i] != '-') return 0; } else if(!atso_hex(value[i])) return 0; } return 1; }

static int atso_root(int fd)
{
    int copy; struct stat st;
    if(fd < 0) return -1;
    copy = fcntl(fd, F_DUPFD_CLOEXEC, 3);
    if(copy < 0) return -1;
    if(fstat(copy, &st) || !S_ISDIR(st.st_mode) || st.st_uid != geteuid() ||
       (st.st_mode & 0777) != 0700) { atso_close(copy); return -1; }
    return copy;
}

static int atso_write_all(int fd, const uint8_t *value, size_t length)
{
    ssize_t written;
    while(length) { if(atso_fault(1)) return -1; written = write(fd, value, length); if(written < 0 && errno == EINTR) continue; if(written <= 0) return -1; value += written; length -= (size_t)written; }
    return 0;
}
static int atso_file_safe(int fd, const struct stat *root)
{
    struct stat st;
    if(fstat(fd, &st) || !S_ISREG(st.st_mode) || st.st_nlink != 1 ||
       st.st_uid != root->st_uid || (st.st_mode & 0777) != 0600 ||
       st.st_size < 0 || (uint64_t)st.st_size > ATSO_MAX_FILE) return -1;
    return 0;
}
static int atso_same_file(const struct stat *left, const struct stat *right)
{
    return left->st_dev == right->st_dev && left->st_ino == right->st_ino &&
        left->st_uid == right->st_uid && left->st_nlink == right->st_nlink &&
        left->st_mode == right->st_mode;
}

static void atso_hex_encode(char *output, const uint8_t *input, size_t length)
{ static const char digits[] = "0123456789abcdef"; size_t i; for(i = 0; i < length; ++i) { output[i * 2U] = digits[input[i] >> 4]; output[i * 2U + 1U] = digits[input[i] & 15U]; } output[length * 2U] = 0; }
/* A decode followed by byte-for-byte re-encode rejects alternate layouts,
 * malformed envelopes, invalid digest, and any non-kind-9 input. */
static int atso_wire_valid(const uint8_t *wire, size_t length, char digest[65])
{
    alias_title_snapshot_v1 snapshot; uint8_t *again; size_t again_length; int status;
    if(!wire || length < CDTO_V1_PREFIX_LENGTH + CDTO_V1_DIGEST_LENGTH ||
       length > CDTO_V1_ALIAS_TITLE_SNAPSHOT_PAYLOAD_LIMIT + CDTO_V1_PREFIX_LENGTH + CDTO_V1_DIGEST_LENGTH) return -1;
    status = alias_title_snapshot_v1_decode(wire, length, &snapshot);
    if(status != CDTO_V1_OK) return -1;
    again = 0; again_length = 0;
    status = alias_title_snapshot_v1_encode(&snapshot, &again, &again_length);
    if(status != CDTO_V1_OK || again_length != length || memcmp(again, wire, length)) { cdto_v1_free_wire(again); return -1; }
    cdto_v1_free_wire(again);
    atso_hex_encode(digest, wire + length - CDTO_V1_DIGEST_LENGTH, CDTO_V1_DIGEST_LENGTH);
    return 0;
}

static int atso_name(const char *uuid, char name[ATSO_NAME_LENGTH])
{ return snprintf(name, ATSO_NAME_LENGTH, ATSO_PREFIX "%s" ATSO_SUFFIX, uuid) == 46 ? 0 : -1; }
static int atso_header(const alias_title_snapshot_v1_outbox_event *event,
    char *header, size_t *header_length)
{
    int n = snprintf(header, ATSO_HEADER_MAX,
        "alias_title_event_outbox=1\nevent_uuid=%s\nkind=9\nwire_digest_sha256=%s\nwire_octets=%llu\n\n",
        event->event_uuid, event->wire_digest_hex, (unsigned long long)event->wire_octets);
    if(n < 0 || (size_t)n >= ATSO_HEADER_MAX) return -1; *header_length = (size_t)n; return 0;
}

static int atso_random_uuid(char value[37])
{
    uint8_t bytes[16]; int fd; size_t used; ssize_t amount; static const char hex[] = "0123456789abcdef"; unsigned int i, at;
    fd = open("/dev/urandom", O_RDONLY | O_CLOEXEC | O_NOFOLLOW); if(fd < 0) return -1;
    used = 0; while(used < sizeof(bytes)) { amount = read(fd, bytes + used, sizeof(bytes) - used); if(amount < 0 && errno == EINTR) continue; if(amount <= 0) { atso_close(fd); return -1; } used += (size_t)amount; }
    if(atso_close(fd)) return -1; bytes[6] = (uint8_t)((bytes[6] & 15U) | 64U); bytes[8] = (uint8_t)((bytes[8] & 63U) | 128U);
    at = 0; for(i = 0; i < 16U; ++i) { if(at == 8U || at == 13U || at == 18U || at == 23U) value[at++] = '-'; value[at++] = hex[bytes[i] >> 4]; value[at++] = hex[bytes[i] & 15U]; } value[36] = 0; return 0;
}

static int atso_read(int root, const char *name,
    alias_title_snapshot_v1_outbox_event *event, uint8_t **wire,
    size_t *wire_length)
{
    int fd; struct stat root_st, st; uint8_t *bytes; size_t used, header_length;
    ssize_t amount; char digest[65]; unsigned long long octets;
    if(fstat(root, &root_st)) return -1;
    fd = openat(root, name, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(fd < 0) return errno == ENOENT ? -2 : -1;
    if(atso_file_safe(fd, &root_st)) { atso_close(fd); return -1; }
    if(fstat(fd, &st)) { atso_close(fd); return -1; }
    bytes = (uint8_t *)malloc((size_t)st.st_size + 1U); if(!bytes) { atso_close(fd); return -1; }
    used = 0; while(used < (size_t)st.st_size) { amount = read(fd, bytes + used, (size_t)st.st_size - used); if(amount < 0 && errno == EINTR) continue; if(amount <= 0) { free(bytes); atso_close(fd); return -1; } used += (size_t)amount; }
    if(atso_close(fd)) { free(bytes); return -1; }
    header_length = 0; while(header_length + 1U < used && !(bytes[header_length] == '\n' && bytes[header_length + 1U] == '\n')) ++header_length;
    if(header_length + 2U >= used) { free(bytes); return 1; }
    bytes[header_length + 1U] = 0;
    memset(event, 0, sizeof(*event));
    if(sscanf((char *)bytes, "alias_title_event_outbox=1\nevent_uuid=%36[0-9a-f-]\nkind=9\nwire_digest_sha256=%64[0-9a-f]\nwire_octets=%llu\n",
        event->event_uuid, event->wire_digest_hex, &octets) != 3) {
        free(bytes); return 1;
    }
    event->wire_octets = (uint64_t)octets;
    bytes[header_length + 1U] = '\n';
    if(!atso_uuid(event->event_uuid) || strlen(event->wire_digest_hex) != 64U) {
        free(bytes); return 1;
    }
    /* Regenerate the header outside the file buffer to enforce exact canonical metadata. */
    { char canonical[ATSO_HEADER_MAX]; size_t canonical_length; if(atso_header(event, canonical, &canonical_length) || canonical_length != header_length + 2U || memcmp(bytes, canonical, canonical_length)) { free(bytes); return 1; } }
    if(event->wire_octets == 0 || event->wire_octets > ATSO_MAX_FILE || event->wire_octets != (uint64_t)(used - header_length - 2U)) { free(bytes); return 1; }
    if(atso_wire_valid(bytes + header_length + 2U, used - header_length - 2U, digest) || strcmp(digest, event->wire_digest_hex)) { free(bytes); return 1; }
    *wire_length = used - header_length - 2U; *wire = bytes + header_length + 2U;
    memmove(bytes, *wire, *wire_length); *wire = bytes; return 0;
}

int alias_title_snapshot_v1_outbox_create(int directory_fd,
    const uint8_t *wire, size_t wire_length,
    alias_title_snapshot_v1_outbox_event *created)
{
    int root, fd, result; struct stat root_st, before, sealed, published;
    char name[ATSO_NAME_LENGTH], header[ATSO_HEADER_MAX]; size_t header_length;
    if(!created || atso_wire_valid(wire, wire_length, created->wire_digest_hex)) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_INVALID;
    memset(created->event_uuid, 0, sizeof(created->event_uuid)); created->wire_octets = wire_length;
    if(atso_random_uuid(created->event_uuid) || atso_name(created->event_uuid, name) || atso_header(created, header, &header_length)) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR;
    root = atso_root(directory_fd); if(root < 0) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR;
    fd = openat(root, name, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC, 0600);
    if(fd < 0) { result = errno == EEXIST ? ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_EXISTS : ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; atso_close(root); return result; }
    result = fstat(root, &root_st) || atso_file_safe(fd, &root_st) ||
        fstat(fd, &before) || atso_write_all(fd, (const uint8_t *)header,
        header_length) || atso_write_all(fd, wire, wire_length) ||
        atso_fsync(fd, 0);
    if(!result && (atso_file_safe(fd, &root_st) || fstat(fd, &sealed) ||
        !atso_same_file(&before, &sealed) ||
        sealed.st_size != (off_t)(header_length + wire_length) ||
        fstatat(root, name, &published, AT_SYMLINK_NOFOLLOW) ||
        !atso_same_file(&sealed, &published))) result = 1;
    if(atso_close(fd)) result = 1;
    if(!result && atso_fsync(root, 1)) result = 1;
    if(atso_close(root)) result = 1;
    return result ? ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR : ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_OK;
}

int alias_title_snapshot_v1_outbox_retry(int directory_fd,
    const char *event_uuid, const uint8_t *wire, size_t wire_length,
    alias_title_snapshot_v1_outbox_event *existing)
{
    int root, read_result, close_result; char name[ATSO_NAME_LENGTH], digest[65]; uint8_t *stored; size_t stored_length;
    if(existing) memset(existing, 0, sizeof(*existing));
    if(!existing || !atso_uuid(event_uuid) || atso_name(event_uuid, name) || atso_wire_valid(wire, wire_length, digest)) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_INVALID;
    root = atso_root(directory_fd); if(root < 0) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR;
    stored = 0; stored_length = 0; read_result = atso_read(root, name, existing, &stored, &stored_length); close_result = atso_close(root);
    if(close_result) { free(stored); memset(existing, 0, sizeof(*existing)); return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; }
    if(read_result == -2) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_NOT_FOUND;
    if(read_result < 0) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR;
    if(read_result) { memset(existing, 0, sizeof(*existing)); return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT; }
    read_result = stored_length == wire_length && !memcmp(stored, wire, wire_length); free(stored);
    return read_result ? ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_EXACT_RETRY : ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CONFLICT;
}

static int atso_compare(const void *left, const void *right)
{ return strcmp(((const atso_candidate *)left)->name, ((const atso_candidate *)right)->name); }
int alias_title_snapshot_v1_outbox_scan(int directory_fd,
    alias_title_snapshot_v1_outbox_visitor visitor, void *opaque,
    alias_title_snapshot_v1_outbox_report *report)
{
    int root, copy, read_result, result; DIR *dir; struct dirent *entry;
    atso_candidate candidates[ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_MAX_EVENTS];
    unsigned int count, i; size_t length; uint8_t *wire;
    alias_title_snapshot_v1_outbox_event event; char expected_name[ATSO_NAME_LENGTH];
    if(!visitor || !report) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_INVALID; memset(report, 0, sizeof(*report));
    root = atso_root(directory_fd); if(root < 0) return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR;
    copy = fcntl(root, F_DUPFD_CLOEXEC, 3); if(copy < 0) { atso_close(root); return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; }
    dir = fdopendir(copy); if(!dir) { atso_close(copy); atso_close(root); return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; }
    count = 0; errno = 0; while((entry = readdir(dir)) != 0) { if(strlen(entry->d_name) != 46U || strncmp(entry->d_name, ATSO_PREFIX, 4U) || strcmp(entry->d_name + 40U, ATSO_SUFFIX)) continue; report->visited++; if(count == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_MAX_EVENTS) { closedir(dir); atso_close(root); return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_LIMIT; } strcpy(candidates[count++].name, entry->d_name); } if(errno || closedir(dir)) { atso_close(root); return ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; }
    qsort(candidates, count, sizeof(candidates[0]), atso_compare); result = ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_OK;
    for(i = 0; i < count; ++i) { wire = 0; length = 0; read_result = atso_read(root, candidates[i].name, &event, &wire, &length); if(read_result == 1 || read_result == -2) { report->corrupt++; report->frozen++; free(wire); continue; } if(read_result < 0) { free(wire); result = ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; break; } if(atso_name(event.event_uuid, expected_name) || strcmp(candidates[i].name, expected_name)) { report->corrupt++; report->frozen++; free(wire); continue; } report->valid++; if(visitor(&event, wire, length, opaque)) { free(wire); result = ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR; break; } free(wire); }
    if(atso_close(root)) result = ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR;
    return result == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_OK && report->corrupt ? ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT : result;
}
