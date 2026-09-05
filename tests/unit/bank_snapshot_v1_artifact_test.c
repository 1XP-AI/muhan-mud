/* RED first: immutable BankSnapshotV1 evidence has no runtime bank linkage. */
#include "bank_snapshot_v1_artifact.h"
#include "bank_snapshot_v1.h"
#include "cdto_v1.h"

#include <dirent.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static int expect(int ok, const char *message)
{ if(ok) return 0; fprintf(stderr, "bank_snapshot_v1_artifact_test: %s\n", message); return 1; }

static void metadata(bank_snapshot_v1_artifact_metadata *value, const char *command)
{
    memset(value, 0, sizeof(*value));
    strcpy(value->artifact_format, BANK_SNAPSHOT_V1_ARTIFACT_FORMAT);
    strcpy(value->world_id, "muhan-01");
    strcpy(value->character_id, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    strcpy(value->command_id, command);
    strcpy(value->canonical_name_hex, "4d336865726f");
    strcpy(value->request_sha256, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    strcpy(value->source_post_sha256, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
    value->writer_epoch = 7; value->writer_revision = 2;
}

static int wire(uint8_t **bytes, size_t *length)
{
    object object_value; otag root;
    memset(&object_value, 0, sizeof(object_value)); memset(&root, 0, sizeof(root));
    object_value.shotsmax = object_value.shotscur = 1; object_value.type = MONEY;
    strcpy(object_value.name, "bank-money"); object_value.value = 99;
    root.obj = &object_value;
    return bank_snapshot_v1_encode(&root, bytes, length);
}

static int bytes_at(int directory_fd, const char *name, const void *bytes, size_t length, mode_t mode)
{
    int fd; const char *cursor; ssize_t amount;
    fd = openat(directory_fd, name, O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC, mode);
    if(fd < 0) return -1; cursor = (const char *)bytes;
    while(length) { amount = write(fd, cursor, length); if(amount <= 0) { close(fd); return -1; } cursor += amount; length -= (size_t)amount; }
    return close(fd);
}

static int exists_at(int directory_fd, const char *name)
{ struct stat value; return fstatat(directory_fd, name, &value, AT_SYMLINK_NOFOLLOW) == 0; }

static int copy_at(int directory_fd, const char *source, const char *destination)
{
    char buffer[512]; int input, output; ssize_t amount, written;
    input=openat(directory_fd,source,O_RDONLY|O_NOFOLLOW|O_CLOEXEC);
    output=openat(directory_fd,destination,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
    if(input<0||output<0) { if(input>=0) close(input); if(output>=0) close(output); return -1; }
    while((amount=read(input,buffer,sizeof(buffer)))>0) { written=write(output,buffer,(size_t)amount); if(written!=amount) { close(input); close(output); return -1; } }
    if(amount<0||fsync(output)<0||close(input)<0||close(output)<0) return -1;
    return 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-bank-snapshot-artifact-XXXXXX", filename[BANK_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];
    bank_snapshot_v1_artifact_metadata first, other, loaded;
    uint8_t *bank, *read_bank, *bad;
    size_t bank_length, read_length, bad_length;
    int directory_fd, failed; struct stat statbuf;
    bank = read_bank = bad = 0; bank_length = read_length = bad_length = 0U; failed = 0;
    if(!mkdtemp(root) || chmod(root, 0700)) return 2;
    directory_fd = open(root, O_RDONLY|O_DIRECTORY|O_CLOEXEC);
    if(directory_fd < 0 || wire(&bank, &bank_length) != CDTO_V1_OK) return 2;
    metadata(&first, "22222222-2222-4222-8222-222222222222"); metadata(&other, first.command_id);
    other.writer_revision = 3;

    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &first, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_OK, "canonical BankSnapshotV1 must create immutable evidence");
    failed |= expect(bank_snapshot_v1_artifact_filename(&first, filename, sizeof(filename)) == 0 && fstatat(directory_fd, filename, &statbuf, AT_SYMLINK_NOFOLLOW) == 0 && S_ISREG(statbuf.st_mode) && (statbuf.st_mode & 0777) == 0600 && statbuf.st_nlink == 1, "filename must be command UUID scoped and private regular one-link evidence");
    failed |= expect(bank_snapshot_v1_artifact_load(directory_fd, &first, &loaded, &read_bank, &read_length) == BANK_SNAPSHOT_V1_ARTIFACT_OK && read_length == bank_length && !memcmp(read_bank, bank, bank_length) && !strcmp(loaded.artifact_format, BANK_SNAPSHOT_V1_ARTIFACT_FORMAT) && !strcmp(loaded.bank_sha256, first.bank_sha256), "load must verify canonical bytes, declared format, and immutable bank hash");
    bank_snapshot_v1_artifact_free(read_bank); read_bank = 0;
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &first, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY, "same command intent must report exact retry");
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_CONFLICT, "different metadata under command key must report conflict");

    metadata(&other, "33333333-3333-4333-8333-333333333333");
    bad = (uint8_t *)malloc(bank_length); if(!bad) return 2; memcpy(bad, bank, bank_length); bad[bank_length - 1U] ^= 1U; bad_length = bank_length;
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bad, bad_length) == BANK_SNAPSHOT_V1_ARTIFACT_INVALID && bank_snapshot_v1_artifact_filename(&other, filename, sizeof(filename)) == 0 && !exists_at(directory_fd, filename), "noncanonical bank CDTO must be rejected before creation");
    free(bad); bad = 0;
    metadata(&other, "44444444-4444-4444-8444-444444444444");
    failed |= expect(bank_snapshot_v1_artifact_filename(&other, filename, sizeof(filename)) == 0 && bytes_at(directory_fd, filename, "version=1\n\nnot-a-cdto", sizeof("version=1\n\nnot-a-cdto")-1U, 0600) == 0 && bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT, "malformed existing evidence must be corrupt and never replaced");
    metadata(&other, "55555555-5555-4555-8555-555555555555");
    failed |= expect(chmod(root, 0755) == 0 && bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR, "non-owner-private directory must be rejected"); chmod(root, 0700);
    metadata(&other, "66666666-6666-4666-8666-666666666666");
    failed |= expect(bank_snapshot_v1_artifact_filename(&other, filename, sizeof(filename)) == 0 && symlinkat("missing", directory_fd, filename) == 0 && bank_snapshot_v1_artifact_load(directory_fd, &other, &loaded, &read_bank, &read_length) == BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR, "candidate symlink must be an IO-safe failure"); unlinkat(directory_fd, filename, 0);
    failed |= expect(bank_snapshot_v1_artifact_filename(&first, filename, sizeof(filename)) == 0 && linkat(directory_fd, filename, directory_fd, "hardlink-artifact", 0) == 0 && bank_snapshot_v1_artifact_load(directory_fd, &first, &loaded, &read_bank, &read_length) == BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR, "hardlinked artifact must be rejected"); unlinkat(directory_fd, "hardlink-artifact", 0);

#ifdef BANK_SNAPSHOT_V1_ARTIFACT_TESTING
    metadata(&other, "77777777-7777-4777-8777-777777777777"); bank_snapshot_v1_artifact_test_fail_next(BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_FILE_FSYNC);
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR && bank_snapshot_v1_artifact_filename(&other, filename, sizeof(filename)) == 0 && !exists_at(directory_fd, filename), "file fsync fault must never expose a partial final artifact");
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_OK, "retry after unpublished file fsync failure must publish durable evidence");
    metadata(&other, "88888888-8888-4888-4888-888888888888"); bank_snapshot_v1_artifact_test_fail_next(BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_FSYNC);
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR && bank_snapshot_v1_artifact_filename(&other, filename, sizeof(filename)) == 0 && exists_at(directory_fd, filename), "directory fsync failure must have precise IO outcome");
    bank_snapshot_v1_artifact_test_reset_faults();
    { unsigned int file_syncs, directory_syncs; file_syncs=directory_syncs=0U;
      failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY, "post-directory-fsync exact retry must remain exact");
      bank_snapshot_v1_artifact_test_fsync_counts(&file_syncs,&directory_syncs);
      failed |= expect(file_syncs>=1U && directory_syncs>=1U, "exact retry must re-establish both file and parent-directory durability"); }
    metadata(&other, "99999999-9999-4999-8999-999999999999"); bank_snapshot_v1_artifact_test_fail_next(BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_FILE_FSYNC_EINTR);
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_OK, "file fsync EINTR must be retried");
    metadata(&other, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaab"); bank_snapshot_v1_artifact_test_fail_next(BANK_SNAPSHOT_V1_ARTIFACT_TEST_FAULT_DIR_FSYNC_EINTR);
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_OK, "directory fsync EINTR must be retried");
    bank_snapshot_v1_artifact_test_reset_faults();
#endif
    metadata(&other, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb");
    failed |= expect(bank_snapshot_v1_artifact_store(directory_fd, &other, bank, bank_length) == BANK_SNAPSHOT_V1_ARTIFACT_OK && bank_snapshot_v1_artifact_filename(&other, filename, sizeof(filename)) == 0, "swap source must be a valid independent artifact");
    { char source[BANK_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];
      failed |= expect(bank_snapshot_v1_artifact_filename(&other, source, sizeof(source)) == 0 && bank_snapshot_v1_artifact_filename(&first, filename, sizeof(filename)) == 0 && unlinkat(directory_fd, filename, 0) == 0 && copy_at(directory_fd, source, filename) == 0 && bank_snapshot_v1_artifact_load(directory_fd, &first, &loaded, &read_bank, &read_length) == BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT, "load must bind parsed command_id to the lookup filename and reject swaps"); }
    bank_snapshot_v1_free_wire(bank); close(directory_fd);
    { DIR *dir = opendir(root); struct dirent *entry; if(dir) { while((entry=readdir(dir))) if(strcmp(entry->d_name,".")&&strcmp(entry->d_name,"..")) unlinkat(dirfd(dir),entry->d_name,0); closedir(dir); } }
    rmdir(root); if(failed) return 1; puts("bank_snapshot_v1_artifact_test: ok"); return 0;
}
