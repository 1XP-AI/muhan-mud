#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "onboarding_receipt.h"

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define DIGEST "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_receipt_test: %s\n", message);
    return 1;
}

static int read_text(path, out, out_sz)
const char *path;
char *out;
unsigned long out_sz;
{
    int fd, n;

    if(!out || out_sz < 2) return -1;
    fd = open(path, O_RDONLY);
    if(fd < 0) return -1;
    n = read(fd, out, out_sz - 1);
    if(close(fd) < 0 || n < 0) return -1;
    out[n] = 0;
    return 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-onboarding-receipt.XXXXXX";
    char path[1024], contents[1024];
    struct stat st;
    onboarding_receipt receipt;
    int failed = 0;
    static const char pending_expected[] =
        "version=1\n"
        "state=pending\n"
        "actor_uuid=11111111-1111-4111-8111-111111111111\n"
        "correlation_uuid=22222222-2222-4222-8222-222222222222\n"
        "character_uuid=33333333-3333-4333-8333-333333333333\n"
        "canonical_name_hex=416c696365\n"
        "storage_format=player-v1\n"
        "saved_file_sha256=\n";
    static const char saved_expected[] =
        "version=1\n"
        "state=saved\n"
        "actor_uuid=11111111-1111-4111-8111-111111111111\n"
        "correlation_uuid=22222222-2222-4222-8222-222222222222\n"
        "character_uuid=33333333-3333-4333-8333-333333333333\n"
        "canonical_name_hex=416c696365\n"
        "storage_format=player-v1\n"
        "saved_file_sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n";
    static const char committed_expected[] =
        "version=1\n"
        "state=committed\n"
        "actor_uuid=11111111-1111-4111-8111-111111111111\n"
        "correlation_uuid=22222222-2222-4222-8222-222222222222\n"
        "character_uuid=33333333-3333-4333-8333-333333333333\n"
        "canonical_name_hex=416c696365\n"
        "storage_format=player-v1\n"
        "saved_file_sha256=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n";

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    if(setenv("MUHAN_HOME", root, 1) < 0) {
        perror("setenv");
        return 1;
    }

    failed += expect(onboarding_receipt_path(CORRELATION, path, sizeof(path)) == 0,
                     "canonical correlation must produce a receipt path");
    failed += expect(strstr(path, "/onboarding-receipts/") != 0 &&
                     strstr(path, CORRELATION) != 0,
                     "receipt filename must derive only from the correlation UUID");
    failed += expect(onboarding_receipt_write_pending(ACTOR, CORRELATION, CHARACTER,
                                                      "Alice", "player-v1") == 0,
                     "pending provenance must be durable before player save");
    failed += expect(stat(path, &st) == 0 && S_ISREG(st.st_mode) &&
                     (st.st_mode & 0777) == 0600,
                     "pending receipt must be a private 0600 file");
    {
        char dir[1024];
        char *slash;
        strcpy(dir, path);
        slash = strrchr(dir, '/');
        if(slash) *slash = 0;
        failed += expect(stat(dir, &st) == 0 && S_ISDIR(st.st_mode) &&
                         (st.st_mode & 0777) == 0700,
                         "receipt directory must be private");
    }
    failed += expect(read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, pending_expected),
                     "pending receipt must have the exact allowlisted content");
    failed += expect(onboarding_receipt_write_pending(ACTOR, CORRELATION, CHARACTER,
                                                      "Alice", "player-v1") == 0 &&
                     read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, pending_expected),
                     "an exact pending retry must idempotently retain its provenance");
    failed += expect(onboarding_receipt_write_pending(ACTOR, CORRELATION,
                                                      "44444444-4444-4444-8444-444444444444",
                                                      "Alice", "player-v1") < 0 &&
                     read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, pending_expected),
                     "a correlation retry with different metadata must fail closed");
    failed += expect(onboarding_receipt_write_pending("not-an-actor", CORRELATION,
                                                      CHARACTER, "Alice", "player-v1") < 0 &&
                     read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, pending_expected),
                     "invalid retry metadata must not inspect or replace the receipt");
    failed += expect(strstr(contents, "ticket") == 0 && strstr(contents, "password") == 0 &&
                     strstr(contents, "0123456789abcdef0123456789abcdef") == 0,
                     "pending receipt must not contain admission or password material");
    failed += expect(onboarding_receipt_mark_saved(ACTOR, CORRELATION, CHARACTER,
                                                   "Alice", "player-v1", DIGEST) == 0,
                     "save plus digest must atomically replace pending receipt");
    failed += expect(read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, saved_expected),
                     "saved receipt must have the exact allowlisted content");
    failed += expect(onboarding_receipt_read(CORRELATION, &receipt) == 0 &&
                     receipt.state == ONBOARDING_RECEIPT_SAVED &&
                     !strcmp(receipt.file_sha256, DIGEST),
                     "saved receipt must be readable for exact reconciliation");
    failed += expect(onboarding_receipt_mark_committed(ACTOR, CORRELATION, CHARACTER,
                                                       "Alice", "player-v1", DIGEST) == 0,
                     "COMMIT must retain a durable committed receipt");
    failed += expect(read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, committed_expected),
                     "committed receipt must retain the exact saved provenance");
    failed += expect(onboarding_receipt_mark_saved(ACTOR, CORRELATION, CHARACTER,
                                                   "Alice", "player-v1", DIGEST) < 0 &&
                     read_text(path, contents, sizeof(contents)) == 0 &&
                     !strcmp(contents, committed_expected),
                     "terminal committed receipt must never be rolled back or overwritten");
    failed += expect(onboarding_receipt_path("../../not-a-uuid", path, sizeof(path)) < 0,
                     "correlation filename construction must reject traversal");
    failed += expect(onboarding_receipt_write_pending(ACTOR, "../../not-a-uuid", CHARACTER,
                                                      "Alice", "player-v1") < 0,
                     "invalid correlation must not create a receipt");

    if(onboarding_receipt_path(CORRELATION, path, sizeof(path)) == 0)
        unlink(path);
    {
        char dir[1024];
        char *slash;
        strcpy(dir, path);
        slash = strrchr(dir, '/');
        if(slash) {
            *slash = 0;
            rmdir(dir);
        }
    }
    rmdir(root);
    if(failed) return 1;
    puts("onboarding_receipt_test: ok");
    return 0;
}
