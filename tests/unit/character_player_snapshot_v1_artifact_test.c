#include "character_player_snapshot_v1_artifact.h"
#include "cdto_v1.h"
#include "player_snapshot_v1.h"

#include <dirent.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "character_player_snapshot_v1_artifact_test: %s\n", message);
    return 1;
}

static void fixture(character_player_snapshot_v1_artifact_metadata *value,
    const char *command)
{
    memset(value, 0, sizeof(*value));
    strcpy(value->world_id, "muhan-01");
    strcpy(value->character_id, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    strcpy(value->command_id, command);
    strcpy(value->canonical_name_hex, "4d336865726f");
    strcpy(value->request_sha256,
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    strcpy(value->source_post_sha256,
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
    strcpy(value->writer_instance_id, "11111111-1111-4111-8111-111111111111");
    strcpy(value->snapshot_format, CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT);
    value->writer_epoch = 7;
    value->writer_revision = 2;
    value->storage_format = 1;
    value->source_octets = 9;
}

static int wire(uint8_t **bytes, size_t *length)
{
    creature player;

    memset(&player, 0, sizeof(player));
    player.type = PLAYER;
    player.fd = -1;
    strcpy(player.name, "M3hero");
    memset(player.password, 0x5a, sizeof(player.password));
    return player_snapshot_v1_encode_loaded(&player, bytes, length);
}

static int schema_invalid_wire(uint8_t **bytes, size_t *length)
{
    cdto_v1_record record;

    memset(&record, 0, sizeof(record));
    record.kind = CDTO_V1_KIND_PLAYER_SNAPSHOT;
    return cdto_v1_encode(&record, bytes, length);
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

static int read_bytes_at(int directory_fd, const char *name, char *bytes,
    size_t capacity)
{
    int fd;
    ssize_t amount;
    size_t used;

    if(!capacity) return -1;
    fd = openat(directory_fd, name, O_RDONLY | O_CLOEXEC);
    if(fd < 0) return -1;
    used = 0U;
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

static int exists_at(int directory_fd, const char *name)
{
    struct stat metadata;
    return fstatat(directory_fd, name, &metadata, AT_SYMLINK_NOFOLLOW) == 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-player-snapshot-artifact-XXXXXX";
    character_player_snapshot_v1_artifact_metadata first, other, loaded;
    uint8_t *snapshot, *read_snapshot;
    uint8_t *invalid_snapshot;
    size_t snapshot_length, read_length;
    size_t invalid_snapshot_length;
    int directory_fd, failed;
    int body_length;
    char filename[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];
    char body[8192];
    char length_line[64];
    struct stat metadata;

    snapshot = 0;
    invalid_snapshot = 0;
    read_snapshot = 0;
    snapshot_length = read_length = invalid_snapshot_length = 0U;
    body_length = -1;
    failed = 0;
    if(!mkdtemp(root) || chmod(root, 0700)) return 2;
    directory_fd = open(root, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if(directory_fd < 0 || wire(&snapshot, &snapshot_length) != CDTO_V1_OK) return 2;
    fixture(&first, "22222222-2222-4222-8222-222222222222");
    fixture(&other, "22222222-2222-4222-8222-222222222222");
    other.writer_revision = 3;

    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd,
        &first, snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK,
        "canonical player snapshot must create exactly once");
    failed |= expect(character_player_snapshot_v1_artifact_filename(&first, filename,
        sizeof(filename)) == 0 && fstatat(directory_fd, filename, &metadata,
        AT_SYMLINK_NOFOLLOW) == 0 && S_ISREG(metadata.st_mode) &&
        (metadata.st_mode & 0777) == 0600 && metadata.st_nlink == 1,
        "artifact must be private regular single-link evidence");
    failed |= expect(character_player_snapshot_v1_artifact_load(directory_fd, &first,
        &loaded, &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK &&
        read_length == snapshot_length && !memcmp(read_snapshot, snapshot, snapshot_length) &&
        !strcmp(loaded.snapshot_sha256, first.snapshot_sha256),
        "load must verify and return exact canonical bytes");
    character_player_snapshot_v1_artifact_free(read_snapshot);
    read_snapshot = 0;
    failed |= expect(character_player_snapshot_v1_artifact_load_metadata_for_command(
        directory_fd, first.command_id, &loaded) ==
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK &&
        !strcmp(loaded.command_id, first.command_id) &&
        !strcmp(loaded.snapshot_sha256, first.snapshot_sha256),
        "exact command lookup must validate one immutable artifact without discovery");
    memset(&loaded, 0, sizeof(loaded));
    failed |= expect(character_player_snapshot_v1_artifact_load_metadata_for_command(
        directory_fd, "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", &loaded) ==
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID && !loaded.command_id[0],
        "metadata lookup must reject a malformed selector before any artifact read");
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd,
        &first, snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY,
        "same key and exact artifact must be an exact retry");
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd,
        &other, snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CONFLICT,
        "different valid intent under same key must preserve conflict evidence");

    fixture(&other, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee");
    failed |= expect(schema_invalid_wire(&invalid_snapshot,
        &invalid_snapshot_length) == CDTO_V1_OK &&
        character_player_snapshot_v1_artifact_store(directory_fd, &other,
        invalid_snapshot, invalid_snapshot_length) ==
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID &&
        character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && !exists_at(directory_fd, filename),
        "canonical kind-7 CDTO without the PlayerSnapshotV1 schema must be rejected");
    cdto_v1_free_wire(invalid_snapshot);
    invalid_snapshot = 0;

    snapshot[0] ^= 1U;
    fixture(&other, "33333333-3333-4333-8333-333333333333");
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd,
        &other, snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID,
        "noncanonical CDTO must be rejected before creation");
    failed |= expect(character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && !exists_at(directory_fd, filename),
        "invalid input must not leave an artifact");
    snapshot[0] ^= 1U;

    fixture(&other, "44444444-4444-4444-8444-444444444444");
    failed |= expect(character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && write_bytes_at(directory_fd, filename,
        "version=1\n\nnot-a-cdto", sizeof("version=1\n\nnot-a-cdto") - 1U, 0600) == 0 &&
        character_player_snapshot_v1_artifact_store(directory_fd, &other, snapshot,
        snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT,
        "malformed existing evidence must be reported, never replaced");

    fixture(&other, "55555555-5555-4555-8555-555555555555");
    failed |= expect(symlinkat("missing", directory_fd, "unsafe-link") == 0 &&
        character_player_snapshot_v1_artifact_load(directory_fd, &other, &loaded,
        &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND,
        "unrelated symlink cannot influence lookup");
    failed |= expect(chmod(root, 0755) == 0 &&
        character_player_snapshot_v1_artifact_store(directory_fd, &other, snapshot,
        snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR,
        "directory must remain owner-private");
    chmod(root, 0700);

    fixture(&other, "66666666-6666-4666-8666-666666666666");
    failed |= expect(character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && symlinkat("missing", directory_fd, filename) == 0 &&
        character_player_snapshot_v1_artifact_load(directory_fd, &other, &loaded,
        &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR,
        "candidate symlink must be unsafe");
    unlinkat(directory_fd, filename, 0);

    fixture(&other, "77777777-7777-4777-8777-777777777777");
    failed |= expect(character_player_snapshot_v1_artifact_filename(&first, filename,
        sizeof(filename)) == 0 && linkat(directory_fd, filename, directory_fd,
        "hardlink-artifact", 0) == 0 &&
        character_player_snapshot_v1_artifact_load(directory_fd, &first, &loaded,
        &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR,
        "hardlinked artifact must be unsafe");
    unlinkat(directory_fd, "hardlink-artifact", 0);

    failed |= expect(character_player_snapshot_v1_artifact_filename(&first, filename,
        sizeof(filename)) == 0 && fchmodat(directory_fd, filename, 0644, 0) == 0 &&
        character_player_snapshot_v1_artifact_load(directory_fd, &first, &loaded,
        &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR,
        "non-private artifact mode must be unsafe");
    fchmodat(directory_fd, filename, 0600, 0);
    {
        int fd;
        unsigned char changed;
        fd = openat(directory_fd, filename, O_RDWR | O_CLOEXEC);
        changed = 0;
        if(fd >= 0 && lseek(fd, -1, SEEK_END) >= 0 && read(fd, &changed, 1U) == 1 &&
           lseek(fd, -1, SEEK_END) >= 0) { changed ^= 1U; write(fd, &changed, 1U); }
        if(fd >= 0) close(fd);
    }
    failed |= expect(character_player_snapshot_v1_artifact_load(directory_fd, &first,
        &loaded, &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT,
        "payload digest or canonical-length mismatch must freeze evidence");

    fixture(&other, "dddddddd-dddd-4ddd-8ddd-dddddddddddd");
    snprintf(length_line, sizeof(length_line), "snapshot_octets=%llu\n",
        (unsigned long long)snapshot_length);
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd, &other,
        snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK &&
        character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && (body_length = read_bytes_at(directory_fd, filename,
        body, sizeof(body))) > 0 && strstr(body, length_line) != 0,
        "length mismatch fixture must contain canonical artifact header");
    {
        char *where,*newline;
        where = strstr(body, length_line);
        newline = where ? strchr(where, '\n') : 0;
        if(newline && newline > where && newline[-1] >= '0' && newline[-1] <= '9')
            newline[-1] = newline[-1] == '9' ? '8' : (char)(newline[-1] + 1);
    }
    failed |= expect(body_length > 0 && write_bytes_at(directory_fd, filename, body,
        (size_t)body_length, 0600) == 0 &&
        character_player_snapshot_v1_artifact_load(directory_fd, &other, &loaded,
        &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT,
        "declared snapshot length mismatch must freeze evidence");

#ifdef CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING
    fixture(&other, "88888888-8888-4888-8888-888888888888");
    character_player_snapshot_v1_artifact_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_WRITE);
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd, &other,
        snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR &&
        character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && exists_at(directory_fd, filename),
        "write fault must preserve immutable partial evidence");
    fixture(&other, "99999999-9999-4999-8999-999999999999");
    character_player_snapshot_v1_artifact_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_FILE_FSYNC);
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd, &other,
        snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR &&
        character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && exists_at(directory_fd, filename),
        "file fsync fault must preserve immutable evidence");
    fixture(&other, "abababab-abab-4bab-8bab-abababababab");
    character_player_snapshot_v1_artifact_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_CLOSE);
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd, &other,
        snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR &&
        character_player_snapshot_v1_artifact_filename(&other, filename,
        sizeof(filename)) == 0 && exists_at(directory_fd, filename),
        "close fault must preserve immutable evidence");
    fixture(&other, "cdcdcdcd-cdcd-4dcd-8dcd-cdcdcdcdcdcd");
    failed |= expect(character_player_snapshot_v1_artifact_store(directory_fd, &other,
        snapshot, snapshot_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK,
        "allocation fault fixture must first create canonical evidence");
    character_player_snapshot_v1_artifact_test_fail_next(
        CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_ALLOC);
    failed |= expect(character_player_snapshot_v1_artifact_load(directory_fd, &other,
        &loaded, &read_snapshot, &read_length) == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR,
        "allocation fault must not publish a partial load");
    character_player_snapshot_v1_artifact_test_reset_faults();
#endif

    cdto_v1_free_wire(snapshot);
    close(directory_fd);
    {
        DIR *directory;
        struct dirent *entry;

        directory = opendir(root);
        if(directory) {
            while((entry = readdir(directory)) != 0)
                if(strcmp(entry->d_name, ".") && strcmp(entry->d_name, ".."))
                    unlinkat(dirfd(directory), entry->d_name, 0);
            closedir(directory);
        }
    }
    rmdir(root);
    if(failed) return 1;
    puts("character_player_snapshot_v1_artifact_test: ok");
    return 0;
}
