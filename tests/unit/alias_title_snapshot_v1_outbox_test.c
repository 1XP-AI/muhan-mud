/* GREEN: cc -std=gnu89 -fcommon -Wall -Wextra -Werror \
 * -Wno-deprecated-non-prototype -I src \
 * -DALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TESTING this_file \
 * src/alias_title_snapshot_v1_outbox.c src/alias_title_snapshot_v1.c src/cdto_v1.c */
#include "alias_title_snapshot_v1.h"
#include "alias_title_snapshot_v1_outbox.h"
#include "cdto_v1.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static int failed;
static unsigned int visits;

static int expect(int ok, const char *message)
{
    if(ok) return 0;
    fprintf(stderr, "alias_title_snapshot_v1_outbox_test: %s\n", message);
    return 1;
}

static int seen(const alias_title_snapshot_v1_outbox_event *event,
    const uint8_t *wire, size_t wire_length, void *opaque)
{
    (void)event;
    (void)wire;
    (void)wire_length;
    (void)opaque;
    ++visits;
    return 0;
}

static int make_wire(uint8_t **wire, size_t *wire_length, int changed)
{
    alias_title_snapshot_v1 snapshot;

    memset(&snapshot, 0, sizeof(snapshot));
    snapshot.alias_count = 1U;
    snapshot.aliases[0].alias_length = 1U;
    snapshot.aliases[0].alias[0] = 'a';
    snapshot.aliases[0].process_length = 1U;
    snapshot.aliases[0].process[0] = changed ? 'q' : 'p';
    return alias_title_snapshot_v1_encode(&snapshot, wire, wire_length);
}

static int write_noncanonical_entry(int directory_fd,
    const alias_title_snapshot_v1_outbox_event *event,
    const uint8_t *wire, size_t wire_length)
{
    static const char uuid[] = "00000000-0000-4000-8000-000000000000";
    char name[48], header[256];
    int fd, length;

    if(snprintf(name, sizeof(name), "ats-%s.event", uuid) != 46) return -1;
    length = snprintf(header, sizeof(header),
        "alias_title_event_outbox=1\nevent_uuid=%s\nkind=9\n"
        "wire_digest_sha256=%s\nwire_octets=0\n\n",
        uuid, event->wire_digest_hex);
    if(length < 0 || (size_t)length >= sizeof(header)) return -1;
    fd = openat(directory_fd, name, O_WRONLY | O_CREAT | O_EXCL, 0600);
    if(fd < 0) return -1;
    if(write(fd, header, (size_t)length) != length ||
       write(fd, wire, wire_length) != (ssize_t)wire_length || close(fd)) return -1;
    return 0;
}

static int event_stat(int directory_fd,
    const alias_title_snapshot_v1_outbox_event *event, struct stat *st)
{
    char name[48];

    if(snprintf(name, sizeof(name), "ats-%s.event", event->event_uuid) != 46)
        return -1;
    return fstatat(directory_fd, name, st, AT_SYMLINK_NOFOLLOW);
}

int main(void)
{
    char directory[] = "/tmp/muhan-alias-title-outbox-XXXXXX";
    char limit_directory[] = "/tmp/muhan-alias-title-outbox-limit-XXXXXX";
    alias_title_snapshot_v1_outbox_event created, loaded, partial, closed;
    alias_title_snapshot_v1_outbox_report report;
    uint8_t *wire, *changed;
    size_t wire_length, changed_length;
    int directory_fd, limit_fd, scan_status;
    unsigned int i;
    struct stat partial_stat;

    if(!mkdtemp(directory) || chmod(directory, 0700)) return 2;
    directory_fd = open(directory, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if(directory_fd < 0 || make_wire(&wire, &wire_length, 0) ||
       make_wire(&changed, &changed_length, 1)) return 2;

    failed |= expect(alias_title_snapshot_v1_outbox_create(directory_fd, wire,
        wire_length, &created) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_OK,
        "create must atomically persist a new canonical event");
    failed |= expect(alias_title_snapshot_v1_outbox_retry(directory_fd,
        created.event_uuid, wire, wire_length, &loaded) ==
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_EXACT_RETRY &&
        !strcmp(created.wire_digest_hex, loaded.wire_digest_hex),
        "same event UUID and canonical bytes must be an exact retry");
    failed |= expect(alias_title_snapshot_v1_outbox_retry(directory_fd,
        created.event_uuid, changed, changed_length, &loaded) ==
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CONFLICT,
        "same event UUID with different bytes must conflict");
    failed |= expect(alias_title_snapshot_v1_outbox_retry(directory_fd,
        "11111111-1111-4111-8111-111111111111", wire, wire_length,
        &loaded) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_NOT_FOUND,
        "unseen local event UUID must be reported as not found");

    wire[wire_length - 1U] ^= 1U;
    failed |= expect(alias_title_snapshot_v1_outbox_create(directory_fd, wire,
        wire_length, &loaded) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_INVALID,
        "bad CDTO digest must be rejected before a file is created");
    wire[wire_length - 1U] ^= 1U;
    failed |= expect(write_noncanonical_entry(directory_fd, &created, wire,
        wire_length) == 0, "test setup must create a noncanonical entry");
    visits = 0;
    failed |= expect(alias_title_snapshot_v1_outbox_scan(directory_fd, seen, 0,
        &report) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT &&
        report.visited == 2U && report.valid == 1U && report.corrupt == 1U &&
        report.frozen == 1U && visits == 1U,
        "bounded recovery must deliver valid entries and freeze noncanonical ones");

    alias_title_snapshot_v1_outbox_test_fail_next(
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_WRITE);
    failed |= expect(alias_title_snapshot_v1_outbox_create(directory_fd, wire,
        wire_length, &partial) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR &&
        event_stat(directory_fd, &partial, &partial_stat) == 0 &&
        partial_stat.st_size == 0,
        "write fault must report I/O failure and leave its partial event file");
    failed |= expect(alias_title_snapshot_v1_outbox_retry(directory_fd,
        partial.event_uuid, wire, wire_length, &loaded) ==
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT,
        "a partial write event must have an explicit corrupt retry outcome");
    visits = 0;
    scan_status = alias_title_snapshot_v1_outbox_scan(directory_fd, seen, 0,
        &report);
    failed |= expect(scan_status == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT,
        "partial-event recovery must report corruption");
    failed |= expect(report.visited == 3U && report.valid == 1U &&
        report.corrupt == 2U && report.frozen == 2U && visits == 1U,
        "partial-event recovery must freeze and skip only corrupt entries");
    failed |= expect(event_stat(directory_fd, &partial, &partial_stat) == 0 &&
        partial_stat.st_size == 0,
        "partial-event recovery must retain the partial file in place");

    alias_title_snapshot_v1_outbox_test_fail_next(
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_CLOSE);
    failed |= expect(alias_title_snapshot_v1_outbox_create(directory_fd, wire,
        wire_length, &closed) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR,
        "event-file close fault must be reported as I/O failure");
    failed |= expect(alias_title_snapshot_v1_outbox_retry(directory_fd,
        closed.event_uuid, wire, wire_length, &loaded) ==
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_EXACT_RETRY,
        "closed event file must retain its exact canonical retry identity");
    visits = 0;
    scan_status = alias_title_snapshot_v1_outbox_scan(directory_fd, seen, 0,
        &report);
    failed |= expect(scan_status == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_CORRUPT,
        "close-event recovery must still report earlier corruption");
    failed |= expect(report.visited == 4U && report.valid == 2U &&
        report.corrupt == 2U && report.frozen == 2U && visits == 2U,
        "recovery must deliver the complete close-fault event independently");

    alias_title_snapshot_v1_outbox_test_fail_next(
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_FILE_FSYNC);
    failed |= expect(alias_title_snapshot_v1_outbox_create(directory_fd, wire,
        wire_length, &loaded) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR,
        "file fsync failure must be reported as I/O failure");
    alias_title_snapshot_v1_outbox_test_fail_next(
        ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_TEST_FAULT_DIR_FSYNC);
    failed |= expect(alias_title_snapshot_v1_outbox_create(directory_fd, wire,
        wire_length, &loaded) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_IO_ERROR,
        "directory fsync failure must be reported as I/O failure");
    alias_title_snapshot_v1_outbox_test_reset_faults();

    if(!mkdtemp(limit_directory) || chmod(limit_directory, 0700)) return 2;
    limit_fd = open(limit_directory, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if(limit_fd < 0) return 2;
    for(i = 0; i < ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_MAX_EVENTS + 1U; ++i)
        if(alias_title_snapshot_v1_outbox_create(limit_fd, wire, wire_length,
            &loaded) != ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_OK) return 2;
    visits = 0;
    failed |= expect(alias_title_snapshot_v1_outbox_scan(limit_fd, seen, 0,
        &report) == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_LIMIT &&
        report.visited == ALIAS_TITLE_SNAPSHOT_V1_OUTBOX_MAX_EVENTS + 1U &&
        report.valid == 0U && report.corrupt == 0U && report.frozen == 0U &&
        visits == 0U,
        "a deterministic 129th event must stop recovery before delivery");

    cdto_v1_free_wire(wire);
    cdto_v1_free_wire(changed);
    close(limit_fd);
    close(directory_fd);
    if(!failed) puts("alias_title_snapshot_v1_outbox_test: ok");
    return failed ? 1 : 0;
}
