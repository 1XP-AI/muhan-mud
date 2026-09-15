#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "legacy_identity_evidence.h"
#include "legacy_identity_evidence_wire.h"
#include "mstruct.h"
#include "player_path.h"
#include "player_store.h"

static int decoder_result;
static char decoder_mutation_path[512];

extern void file_player_store_test_fail_next_hash_read(void);

void lowercize(char *value, int flag)
{
    unsigned long i;

    for(i = 0; value && value[i]; i++)
        if(value[i] >= 'A' && value[i] <= 'Z') value[i] += 'a' - 'A';
    if(value && (flag & 1) && value[0] >= 'a' && value[0] <= 'z')
        value[0] -= 'a' - 'A';
}

void zero(void *memory, int size)
{
    memset(memory, 0, (size_t)size);
}

int write_crt(int fd, creature *player, char perm_only)
{
    (void)fd;
    (void)player;
    (void)perm_only;
    return -1;
}

int utf8_validate(const unsigned char *value, unsigned long length)
{
    (void)value;
    (void)length;
    return 1;
}

unsigned long utf8_codepoint_len(const unsigned char *value)
{
    return value ? (unsigned long)strlen((const char *)value) : 0;
}

int read_crt_player(int fd, creature *player)
{
    int mutation_fd;
    unsigned char byte = 'y';

    (void)fd;
    (void)player;
    if(decoder_mutation_path[0]) {
        mutation_fd = open(decoder_mutation_path, O_WRONLY | O_APPEND);
        if(mutation_fd < 0 || write(mutation_fd, &byte, 1) != 1) {
            if(mutation_fd >= 0) close(mutation_fd);
            return -1;
        }
        if(close(mutation_fd) < 0)
            return -1;
    }
    return decoder_result;
}

void free_crt(creature *player)
{
    free(player);
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "legacy_identity_evidence_test: %s\n", message);
    return 1;
}

static int make_regular_file(const char *path)
{
    creature player;
    int inventory_count;
    int fd;

    /* Synthetic Player V1 fixture: one native creature record followed by
     * the legacy root-inventory count.  The decoder hook below stands in for
     * the already-tested native decoder while this test exercises the real
     * file-open/hash/evidence path over deterministic legacy-shaped bytes. */
    memset(&player, 0, sizeof(player));
    strcpy(player.name, "Alice");
    inventory_count = 0;
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0) return -1;
    if(write(fd, &player, sizeof(player)) != (ssize_t)sizeof(player) ||
       write(fd, &inventory_count, sizeof(inventory_count)) !=
           (ssize_t)sizeof(inventory_count) || close(fd) < 0)
        return -1;
    return 0;
}

int main(void)
{
    char root[] = "/tmp/muhan-legacy-identity-evidence.XXXXXX";
    char player_dir[512], player_file[512], shard_dir[512], expected_shard[3];
    legacy_identity_evidence first, second, invalid, missing, corrupt, io_error;
    legacy_identity_evidence read_error, changed_after_hash;
    legacy_identity_evidence wire_decoded;
    unsigned char *wire = 0;
    size_t wire_length = 0;
    int failed = 0;

    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) < 0) {
        perror("fixture root");
        return 1;
    }
    snprintf(player_dir, sizeof(player_dir), "%s/player", root);
    if(mkdir(player_dir, 0700) < 0 ||
       player_path_from_name("Alice", player_file, sizeof(player_file)) != 0 ||
       player_path_shard_from_name("Alice", expected_shard) != 0) {
        perror("fixture paths");
        return 1;
    }
    strcpy(shard_dir, player_file);
    *strrchr(shard_dir, '/') = 0;
    if(mkdir(shard_dir, 0700) < 0 || make_regular_file(player_file) < 0) {
        perror("fixture player file");
        return 1;
    }

    decoder_result = 0;
    failed += expect(legacy_identity_evidence_inspect("aLiCe", &first) ==
                     LEGACY_IDENTITY_EVIDENCE_OK,
                     "valid legacy file must produce evidence");
    failed += expect(first.version == LEGACY_IDENTITY_EVIDENCE_VERSION &&
                     first.canonicalization == LEGACY_IDENTITY_EVIDENCE_NORMALIZED &&
                     strcmp(first.canonical_name, "Alice") == 0 &&
                     strcmp(first.legacy_shard, expected_shard) == 0 &&
                     strcmp(first.player_file_sha256,
                            "3aa88f70383981ecc2d8888f7493dced5f18e97f62e9bb2b10e5c8e31d5c7417") == 0 &&
                     strcmp(first.storage_format, "player-v1") == 0,
                     "evidence must contain only canonical identity metadata");
    failed += expect(legacy_identity_evidence_wire_encode(&first, &wire,
                   &wire_length) == LEGACY_IDENTITY_EVIDENCE_WIRE_OK &&
                   legacy_identity_evidence_wire_decode(wire, wire_length,
                   &wire_decoded) == LEGACY_IDENTITY_EVIDENCE_WIRE_OK &&
                   !memcmp(&first, &wire_decoded, sizeof(first)),
                   "producer evidence must preserve its tuple through MUDLIE wire");
    legacy_identity_evidence_wire_free(wire);
    wire = 0;
    failed += expect(legacy_identity_evidence_inspect("aLiCe", &second) ==
                     LEGACY_IDENTITY_EVIDENCE_OK && !memcmp(&first, &second, sizeof(first)),
                     "unchanged input and player file must inspect deterministically");

    failed += expect(legacy_identity_evidence_inspect("bad/name", &invalid) ==
                     LEGACY_IDENTITY_EVIDENCE_INVALID_INPUT &&
                     invalid.canonicalization == LEGACY_IDENTITY_EVIDENCE_INVALID &&
                     !invalid.canonical_name[0] && !invalid.legacy_shard[0] &&
                     !invalid.player_file_sha256[0],
                     "malformed names must have an explicit no-file invalid outcome");
    failed += expect(legacy_identity_evidence_inspect("Missing", &missing) ==
                     LEGACY_IDENTITY_EVIDENCE_NOT_FOUND &&
                     missing.canonicalization == LEGACY_IDENTITY_EVIDENCE_CANONICAL &&
                     !missing.player_file_sha256[0],
                     "missing player files must remain distinct from corrupt records");

    file_player_store_test_fail_next_hash_read();
    failed += expect(legacy_identity_evidence_inspect("Alice", &read_error) ==
                     LEGACY_IDENTITY_EVIDENCE_IO_ERROR &&
                     !read_error.player_file_sha256[0],
                     "hash read faults must fail closed without a digest");

    strcpy(decoder_mutation_path, player_file);
    failed += expect(legacy_identity_evidence_inspect("Alice", &changed_after_hash) ==
                     LEGACY_IDENTITY_EVIDENCE_IO_ERROR &&
                     !changed_after_hash.player_file_sha256[0],
                     "mutation after hashing during decode must not return evidence");
    memset(decoder_mutation_path, 0, sizeof(decoder_mutation_path));

    decoder_result = -1;
    failed += expect(legacy_identity_evidence_inspect("Alice", &corrupt) ==
                     LEGACY_IDENTITY_EVIDENCE_CORRUPT && !corrupt.player_file_sha256[0],
                     "decoder rejection must not emit a digest for corrupt input");
    decoder_result = 0;
    if(unlink(player_file) < 0 || symlink("elsewhere", player_file) < 0) {
        perror("symlink fixture");
        return 1;
    }
    failed += expect(legacy_identity_evidence_inspect("Alice", &io_error) ==
                     LEGACY_IDENTITY_EVIDENCE_IO_ERROR && !io_error.player_file_sha256[0],
                     "unsafe player-file access must be reported as an IO error");

    unlink(player_file);
    rmdir(shard_dir);
    rmdir(player_dir);
    rmdir(root);
    if(failed) return 1;
    puts("legacy_identity_evidence_test: ok");
    return 0;
}
