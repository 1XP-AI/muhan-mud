#include "character_snapshot_shadow_outbox.h"

#include <dirent.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "character_snapshot_shadow_outbox_test: %s\n", message);
    return 1;
}

static void fixture(character_snapshot_shadow_outbox_manifest *manifest,
    const char *command, const char *name, const char *request,
    const char *post, unsigned long revision)
{
    memset(manifest, 0, sizeof(*manifest));
    strcpy(manifest->world_id, "muhan-01");
    strcpy(manifest->character_id, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    strcpy(manifest->command_id, command);
    strcpy(manifest->canonical_name_hex, name);
    strcpy(manifest->request_sha256, request);
    strcpy(manifest->post_sha256, post);
    strcpy(manifest->writer_instance_id,
        "11111111-1111-4111-8111-111111111111");
    strcpy(manifest->snapshot_format, CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT);
    manifest->storage_format = 1;
    manifest->writer_epoch = 7;
    manifest->writer_revision = revision;
    manifest->snapshot_octets = 128;
}

static int write_bytes_at(int directory_fd, const char *name,
    const void *bytes, size_t length, mode_t mode)
{
    int fd;
    const char *cursor;
    ssize_t amount;

    fd = openat(directory_fd, name, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC,
        mode);
    if(fd < 0) return -1;
    cursor = (const char *)bytes;
    while(length) {
        amount = write(fd, cursor, length);
        if(amount <= 0) { close(fd); return -1; }
        cursor += amount;
        length -= (size_t)amount;
    }
    return close(fd);
}

static int read_file_at(int directory_fd, const char *name, char *bytes,
    size_t capacity)
{
    int fd;
    ssize_t amount;
    size_t used = 0;

    fd = openat(directory_fd, name, O_RDONLY | O_CLOEXEC);
    if(fd < 0) return -1;
    while(used + 1U < capacity) {
        amount = read(fd, bytes + used, capacity - used - 1U);
        if(amount < 0) { close(fd); return -1; }
        if(!amount) break;
        used += (size_t)amount;
    }
    bytes[used] = 0;
    if(close(fd)) return -1;
    return (int)used;
}

static int fd_count(void)
{
    DIR *directory;
    struct dirent *entry;
    int count = 0;

    directory = opendir("/dev/fd");
    if(!directory) return -1;
    while((entry = readdir(directory)) != 0)
        if(strcmp(entry->d_name, ".") && strcmp(entry->d_name, "..")) ++count;
    closedir(directory);
    return count;
}

static int seen[300];
static int seen_count;

static int visit(const character_snapshot_shadow_outbox_manifest *manifest,
    void *opaque)
{
    (void)opaque;
    seen[seen_count++] = (int)manifest->writer_revision;
    return 0;
}

static int all_zero(const void *value, size_t length)
{
    const unsigned char *cursor = (const unsigned char *)value;
    while(length--) if(*cursor++) return 0;
    return 1;
}

static int canonical_text(const character_snapshot_shadow_outbox_manifest *manifest,
    char *output, size_t capacity)
{
    return snprintf(output, capacity,
        "version=1\nworld_id=%s\ncharacter_id=%s\ncommand_id=%s\n"
        "canonical_name_hex=%s\nrequest_sha256=%s\npost_sha256=%s\n"
        "writer_instance_id=%s\nsnapshot_format=%s\nwriter_epoch=%llu\n"
        "writer_revision=%llu\nstorage_format=%d\nsnapshot_octets=%llu\n",
        manifest->world_id, manifest->character_id, manifest->command_id,
        manifest->canonical_name_hex, manifest->request_sha256,
        manifest->post_sha256, manifest->writer_instance_id,
        manifest->snapshot_format, (unsigned long long)manifest->writer_epoch,
        (unsigned long long)manifest->writer_revision,
        (int)manifest->storage_format,
        (unsigned long long)manifest->snapshot_octets);
}

int main(void)
{
    char root[] = "/tmp/muhan-snapshot-outbox-XXXXXX";
    character_snapshot_shadow_outbox_manifest first, second, third, loaded, padded;
    character_snapshot_shadow_outbox_manifest malformed;
    character_snapshot_shadow_outbox_report report;
    int directory_fd, failed = 0, before_fds, after_fds;
    char body[2048];

    if(!mkdtemp(root) || chmod(root, 0700)) return 2;
    directory_fd = open(root, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if(directory_fd < 0) return 2;
    fixture(&first, "22222222-2222-4222-8222-222222222222", "4d3341",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 2);
    fixture(&second, "22222222-2222-4222-8222-222222222222", "4d3342",
        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", 1);
    fixture(&third, "33333333-3333-4333-8333-333333333333", "4d3343",
        "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
        "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 1);

    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK, "first manifest must be created");
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXISTS, "overwrite must be rejected");
    failed |= expect(character_snapshot_shadow_outbox_retry(directory_fd, &first, &loaded) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY && loaded.writer_revision == 2,
        "logical exact retry must ignore struct padding");
    memset(&padded, 0xa5, sizeof(padded));
    fixture(&padded, "22222222-2222-4222-8222-222222222222", "4d3341",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 2);
    failed |= expect(character_snapshot_shadow_outbox_retry(directory_fd, &padded, &loaded) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY,
        "uninitialized struct padding cannot change an exact retry");
    memset(&loaded, 0xa5, sizeof(loaded));
    failed |= expect(character_snapshot_shadow_outbox_retry(directory_fd, &second, &loaded) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CONFLICT && loaded.writer_revision == 2,
        "same command with other logical fields is conflict");

    first.snapshot_octets = 0;
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "zero snapshot octets rejected");
    third.snapshot_octets = CHARACTER_SNAPSHOT_SHADOW_OUTBOX_MAX_SNAPSHOT_OCTETS;
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &third) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK, "maximum snapshot octets accepted");
    third.snapshot_octets = CHARACTER_SNAPSHOT_SHADOW_OUTBOX_MAX_SNAPSHOT_OCTETS + 1U;
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &third) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "maximum plus one rejected");
    third.snapshot_octets = CHARACTER_SNAPSHOT_SHADOW_OUTBOX_MAX_SNAPSHOT_OCTETS;
    first.snapshot_octets = 128;

    first.world_id[0] = 'A';
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "world must begin lowercase");
    strcpy(first.world_id, "muhan!");
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "world regex must reject punctuation");
    strcpy(first.world_id, "muhan-01");
    first.command_id[0] = 'A';
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "uppercase UUID rejected");
    strcpy(first.command_id, "22222222-2222-4222-8222-222222222222");
    first.post_sha256[0] = 'A';
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "uppercase SHA rejected");
    strcpy(first.post_sha256,
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
    memset(first.world_id, 'a', sizeof(first.world_id));
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &first) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_INVALID, "unterminated text rejected");
    strcpy(first.world_id, "muhan-01");

    memset(&report, 0, sizeof(report));
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK && report.visited == 2 && report.valid == 2 &&
        seen_count == 2 && seen[0] == 2 && seen[1] == 1,
        "valid manifests must visit lexical filename order");

    fixture(&malformed, "40000000-0000-4000-8000-000000000000", "4d3345",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4);
    canonical_text(&malformed, body, sizeof(body));
    body[0] = 'x';
    failed |= expect(write_bytes_at(directory_fd,
        "40000000-0000-4000-8000-000000000000.manifest",
        body, strlen(body), 0600) == 0,
        "malformed manifest creation");
    seen_count = 0;
    {
        int scan_status = character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report);
        failed |= expect(scan_status == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT && report.corrupt == 1 &&
        report.frozen == 1 && seen_count == 2, "malformed evidence freezes but preserves valid visits");
    }

    fixture(&malformed, "41000000-0000-4000-8000-000000000000", "4d3345",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4);
    canonical_text(&malformed, body, sizeof(body));
    {
        char *where = strstr(body, "writer_epoch=7\n");
        if(where) {
            memmove(where + 14, where + 13, strlen(where + 13) + 1U);
            where[13] = '0';
        }
    }
    failed |= expect(write_bytes_at(directory_fd,
        "41000000-0000-4000-8000-000000000000.manifest",
        body, strlen(body), 0600) == 0,
        "leading-zero fixture creation");
    failed |= expect(write_bytes_at(directory_fd,
        "42000000-0000-4000-8000-000000000000.manifest",
        "world_id=muhan-01\nversion=1\n",
        strlen("world_id=muhan-01\nversion=1\n"), 0600) == 0,
        "reordered fixture creation");
    fixture(&malformed, "43000000-0000-4000-8000-000000000000", "4d3345",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4);
    canonical_text(&malformed, body, sizeof(body));
    failed |= expect(write_bytes_at(directory_fd,
        "43000000-0000-4000-8000-000000000000.manifest",
        body, strlen(body) - 1U, 0600) == 0, "final-newline fixture creation");
    {
        size_t body_length;

        fixture(&malformed, "44000000-0000-4000-8000-000000000000", "4d3345",
            "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4);
        canonical_text(&malformed, body, sizeof(body));
        body_length = strlen(body);
        body[5] = 0;
        failed |= expect(write_bytes_at(directory_fd,
            "44000000-0000-4000-8000-000000000000.manifest",
            body, body_length, 0600) == 0, "NUL fixture creation");
    }
    fixture(&malformed, "45000000-0000-4000-8000-000000000000", "4d3345",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4);
    canonical_text(&malformed, body, sizeof(body));
    strcat(body, "unknown_field=x\n");
    failed |= expect(write_bytes_at(directory_fd,
        "45000000-0000-4000-8000-000000000000.manifest",
        body, strlen(body), 0600) == 0,
        "unknown field fixture creation");
    fixture(&malformed, "46000000-0000-4000-8000-000000000000", "4d3345",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 4);
    canonical_text(&malformed, body, sizeof(body));
    strcat(body, "snapshot_octets=128\n");
    failed |= expect(write_bytes_at(directory_fd,
        "46000000-0000-4000-8000-000000000000.manifest",
        body, strlen(body), 0600) == 0,
        "duplicate field fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT && report.corrupt == 7 &&
        seen_count == 2, "canonical parser rejects malformed forms");

    canonical_text(&third, body, sizeof(body));
    failed |= expect(write_bytes_at(directory_fd,
        "47000000-0000-4000-8000-000000000000.manifest", body, strlen(body), 0600) == 0,
        "filename/body mismatch fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT && report.frozen >= 1 && seen_count == 2,
        "filename/body mismatch freezes without blocking valid manifests");
    unlinkat(directory_fd, "47000000-0000-4000-8000-000000000000.manifest", 0);

    before_fds = fd_count();
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_WRITE);
    fixture(&second, "55555555-5555-4555-8555-555555555555", "4d3344",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 9);
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &second) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR &&
        read_file_at(directory_fd, "55555555-5555-4555-8555-555555555555.manifest", body,
            sizeof(body)) >= 0, "partial capture evidence remains after write fault");
    after_fds = fd_count();
    failed |= expect(before_fds < 0 || after_fds == before_fds,
        "write fault must not leak descriptors");
    unlinkat(directory_fd, "55555555-5555-4555-8555-555555555555.manifest", 0);
    fixture(&second, "88888888-8888-4888-8888-888888888888", "4d3344",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 9);
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_FILE_FSYNC);
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &second) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR &&
        read_file_at(directory_fd, "88888888-8888-4888-8888-888888888888.manifest", body,
            sizeof(body)) >= 0, "file fsync fault keeps immutable evidence");
    unlinkat(directory_fd, "88888888-8888-4888-8888-888888888888.manifest", 0);
    fixture(&second, "99999999-9999-4999-8999-999999999999", "4d3344",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 9);
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_CLOSE);
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &second) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR &&
        read_file_at(directory_fd, "99999999-9999-4999-8999-999999999999.manifest", body,
            sizeof(body)) >= 0, "close fault keeps immutable evidence");
    unlinkat(directory_fd, "99999999-9999-4999-8999-999999999999.manifest", 0);
    fixture(&second, "abababab-abab-4bab-8bab-abababababab", "4d3344",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 9);
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_DIR_FSYNC);
    failed |= expect(character_snapshot_shadow_outbox_write(directory_fd, &second) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR &&
        read_file_at(directory_fd, "abababab-abab-4bab-8bab-abababababab.manifest", body,
            sizeof(body)) >= 0, "directory fsync fault keeps immutable evidence");
    unlinkat(directory_fd, "abababab-abab-4bab-8bab-abababababab.manifest", 0);

    memset(&loaded, 0xa5, sizeof(loaded));
    failed |= expect(character_snapshot_shadow_outbox_retry(-1, &first, &loaded) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && all_zero(&loaded, sizeof(loaded)),
        "retry output clears on every failure");
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_ALLOC);
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "allocation failure must not call visitor");
    character_snapshot_shadow_outbox_test_fail_next(
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_TEST_FAULT_DIR_CLOSE);
    before_fds = fd_count();
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "directory close failure must not call visitor");
    after_fds = fd_count();
    failed |= expect(before_fds < 0 || after_fds == before_fds,
        "directory close failure must still release descriptors");
    character_snapshot_shadow_outbox_test_reset_faults();

    failed |= expect(chmod(root, 0755) == 0, "unsafe outbox mode fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "outbox directory must remain private to its owner");
    failed |= expect(chmod(root, 0700) == 0, "outbox mode fixture restoration");

    failed |= expect(symlinkat("no-target", directory_fd,
        "66666666-6666-4666-8666-666666666666.manifest") == 0,
        "symlink fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "symlink candidate is unsafe and invokes no callback");
    unlinkat(directory_fd, "66666666-6666-4666-8666-666666666666.manifest", 0);
    failed |= expect(mkfifoat(directory_fd,
        "77777777-7777-4777-8777-777777777777.manifest", 0600) == 0,
        "FIFO fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "FIFO candidate is nonblocking but unsafe");
    unlinkat(directory_fd, "77777777-7777-4777-8777-777777777777.manifest", 0);
    failed |= expect(fchmodat(directory_fd, "22222222-2222-4222-8222-222222222222.manifest", 0644, 0) == 0,
        "mode fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "wrong file mode is unsafe and invokes no callback");
    failed |= expect(fchmodat(directory_fd,
        "22222222-2222-4222-8222-222222222222.manifest", 0600, 0) == 0,
        "mode fixture restoration");
    failed |= expect(linkat(directory_fd, "22222222-2222-4222-8222-222222222222.manifest",
        directory_fd, "hard.manifest", 0) == 0, "hardlink fixture creation");
    seen_count = 0;
    failed |= expect(character_snapshot_shadow_outbox_scan(directory_fd, visit, 0, &report) ==
        CHARACTER_SNAPSHOT_SHADOW_OUTBOX_IO_ERROR && seen_count == 0,
        "hardlinked evidence is unsafe and invokes no callback");
    unlinkat(directory_fd, "hard.manifest", 0);

    {
        char limit_root[] = "/tmp/muhan-snapshot-outbox-limit-XXXXXX";
        char name[64];
        unsigned int index, created;
        int limit_fd;
        DIR *directory;
        struct dirent *entry;

        limit_fd = -1;
        created = 0;
        if(mkdtemp(limit_root) && chmod(limit_root, 0700) == 0)
            limit_fd = open(limit_root, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        failed |= expect(limit_fd >= 0, "candidate limit directory creation");
        if(limit_fd >= 0) {
            for(index = 0; index < 257U; ++index) {
                snprintf(name, sizeof(name),
                    "%08x-0000-4000-8000-%012x.manifest", index, index);
                if(write_bytes_at(limit_fd, name, "x", 1U, 0600)) break;
                ++created;
            }
            failed |= expect(created == 257U, "candidate limit fixture creation");
            before_fds = fd_count();
            seen_count = 0;
            failed |= expect(created != 257U ||
                character_snapshot_shadow_outbox_scan(limit_fd, visit, 0, &report) ==
                    CHARACTER_SNAPSHOT_SHADOW_OUTBOX_LIMIT,
                "candidate limit must fail before callbacks");
            after_fds = fd_count();
            failed |= expect(seen_count == 0 &&
                (before_fds < 0 || after_fds == before_fds),
                "candidate limit must not call back or leak descriptors");
            close(limit_fd);
        }
        directory = opendir(limit_root);
        if(directory) {
            while((entry = readdir(directory)) != 0)
                if(strcmp(entry->d_name, ".") && strcmp(entry->d_name, ".."))
                    unlinkat(dirfd(directory), entry->d_name, 0);
            closedir(directory);
        }
        rmdir(limit_root);
    }

    close(directory_fd);
    {
        DIR *directory = opendir(root);
        struct dirent *entry;
        if(directory) {
            while((entry = readdir(directory)) != 0)
                if(strcmp(entry->d_name, ".") && strcmp(entry->d_name, ".."))
                    unlinkat(dirfd(directory), entry->d_name, 0);
            closedir(directory);
        }
    }
    rmdir(root);
    if(failed) return 1;
    puts("character_snapshot_shadow_outbox_test: ok");
    return 0;
}
