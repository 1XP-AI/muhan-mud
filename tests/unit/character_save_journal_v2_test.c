#include "character_save_journal_v2.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "character_save_journal_v2_test: %s\n", message);
    return 1;
}

static int write_all(fd, bytes, length)
int fd;
const void *bytes;
size_t length;
{
    const char *cursor = (const char *)bytes;
    ssize_t n;
    while(length) {
        n = write(fd, cursor, length);
        if(n < 0 && errno == EINTR) continue;
        if(n <= 0) return -1;
        cursor += n;
        length -= (size_t)n;
    }
    return 0;
}

static int join(out, out_size, root, suffix)
char *out;
size_t out_size;
const char *root;
const char *suffix;
{
    int n = snprintf(out, out_size, "%s/%s", root, suffix);
    return n < 0 || (size_t)n >= out_size ? -1 : 0;
}

static int make_dir(root, suffix)
const char *root;
const char *suffix;
{
    char path[512];
    return join(path, sizeof(path), root, suffix) || mkdir(path, 0700) != 0 ? -1 : 0;
}

static void fixture(wire, command)
character_save_journal_v2_wire *wire;
const char *command;
{
    memset(wire, 0, sizeof(*wire));
    wire->state = CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(wire->writer_instance_id, "11111111-1111-4111-8111-111111111111");
    strcpy(wire->character_id, "22222222-2222-4222-8222-222222222222");
    strcpy(wire->world_id, "muhan");
    strcpy(wire->legacy_name_key_hex, "5465727261");
    strcpy(wire->legacy_shard, "16");
    strcpy(wire->command_uuid, command);
    wire->writer_epoch = 7;
    wire->writer_revision = 9;
    wire->expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    wire->storage_format = 1;
}

static void sql_golden_fixture(wire)
character_save_journal_v2_wire *wire;
{
    memset(wire, 0, sizeof(*wire));
    wire->state = CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(wire->writer_instance_id, "94000000-0000-0000-0000-000000000001");
    strcpy(wire->character_id, "92000000-0000-0000-0000-000000000001");
    strcpy(wire->world_id, "m3-contract");
    strcpy(wire->legacy_name_key_hex, "4d336865726f");
    strcpy(wire->legacy_shard, "11");
    strcpy(wire->command_uuid, "93000000-0000-0000-0000-000000000001");
    wire->writer_epoch = 1;
    wire->writer_revision = 1;
    wire->expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    memset(wire->post_sha256, 'a', CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN);
    wire->post_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN] = 0;
    wire->storage_format = 1;
}

static int hash_bytes(root, bytes, length, out)
const char *root;
const char *bytes;
size_t length;
char out[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
{
    char path[512];
    int fd, result;
    if(join(path, sizeof(path), root, "hash-input") != 0) return -1;
    fd = open(path, O_CREAT | O_TRUNC | O_RDWR, 0600);
    if(fd < 0 || write_all(fd, bytes, length) != 0 || lseek(fd, 0, SEEK_SET) < 0) {
        if(fd >= 0) close(fd);
        return -1;
    }
    result = character_save_journal_v2_hash_fd(fd, out);
    close(fd);
    unlink(path);
    return result;
}

static int write_raw_journal_bytes(root, command, bytes, length)
const char *root;
const char *command;
const void *bytes;
size_t length;
{
    char suffix[96], path[512];
    int fd, result;
    if(snprintf(suffix, sizeof(suffix), "character-save-journal/%s.prepared",
                command) < 0 ||
       join(path, sizeof(path), root, suffix) != 0) return -1;
    fd = open(path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    if(fd < 0) return -1;
    result = write_all(fd, bytes, length);
    if(result == 0 && fsync(fd) != 0) result = -1;
    if(close(fd) != 0) result = -1;
    return result;
}

static int write_raw_journal(root, command, text)
const char *root;
const char *command;
const char *text;
{
    return write_raw_journal_bytes(root, command, text, strlen(text));
}

static int artifacts_absent(root, command)
const char *root;
const char *command;
{
    char suffix[96], path[512];
    struct stat st;
    int stage_absent, journal_absent;

    if(snprintf(suffix, sizeof(suffix), "character-save-stage/%s.stage",
                command) < 0 ||
       join(path, sizeof(path), root, suffix) != 0) return 0;
    errno = 0;
    stage_absent = lstat(path, &st) < 0 && errno == ENOENT;
    if(snprintf(suffix, sizeof(suffix), "character-save-journal/%s.prepared",
                command) < 0 ||
       join(path, sizeof(path), root, suffix) != 0) return 0;
    errno = 0;
    journal_absent = lstat(path, &st) < 0 && errno == ENOENT;
    return stage_absent && journal_absent;
}

static int artifact_exists(root, directory, command, suffix)
const char *root;
const char *directory;
const char *command;
const char *suffix;
{
    char relative[128], path[512];
    struct stat st;
    if(snprintf(relative, sizeof(relative), "%s/%s%s", directory, command,
                suffix) < 0 ||
       join(path, sizeof(path), root, relative) != 0) return 0;
    return lstat(path, &st) == 0 && S_ISREG(st.st_mode) &&
           (st.st_mode & 07777) == 0600 && st.st_nlink == 1;
}

static int remove_flat_fixture_directory(root, suffix)
const char *root;
const char *suffix;
{
    char path[512];
    struct dirent *entry;
    struct stat st;
    DIR *directory;
    int fd, result;

    if(join(path, sizeof(path), root, suffix) != 0) return -1;
    fd = open(path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW);
    if(fd < 0) return -1;
    directory = fdopendir(fd);
    if(!directory) {
        close(fd);
        return -1;
    }
    result = 0;
    while((entry = readdir(directory)) != 0) {
        if(!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, "..")) continue;
        if(fstatat(fd, entry->d_name, &st, AT_SYMLINK_NOFOLLOW) != 0 ||
           S_ISDIR(st.st_mode) || unlinkat(fd, entry->d_name, 0) != 0) {
            result = -1;
            break;
        }
    }
    if(closedir(directory) != 0) result = -1;
    if(result == 0 && rmdir(path) != 0) result = -1;
    return result;
}

static int teardown_fixture(root)
const char *root;
{
    char path[512];
    if(remove_flat_fixture_directory(root, "character-save-stage") != 0 ||
       remove_flat_fixture_directory(root, "character-save-journal") != 0 ||
       join(path, sizeof(path), root, "player/16") != 0 || rmdir(path) != 0 ||
       join(path, sizeof(path), root, "player") != 0 || rmdir(path) != 0 ||
       rmdir(root) != 0) return -1;
    return 0;
}

static int test_sql_golden_and_name_validation(void)
{
    static const char golden[] =
        "eb83eb12f5875dad83f5f91e128c4874b2673b4089deff4967c58898d9c69171";
    character_save_journal_v2_wire wire, bad;
    char request[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    int failed = 0;

    sql_golden_fixture(&wire);
    failed += expect(character_save_journal_v2_request_sha256(&wire, request) == 0 &&
                     strcmp(request, golden) == 0,
                     "C request digest must match the fixed PostgreSQL LF/UTF-8 golden");

    bad = wire;
    strcpy(bad.legacy_name_key_hex, "c0af");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "overlong UTF-8 legacy name must reject");
    bad = wire;
    strcpy(bad.legacy_name_key_hex, "eda080");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "UTF-8 surrogate legacy name must reject");
    bad = wire;
    strcpy(bad.legacy_name_key_hex, "6d336865726f");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "noncanonical lowercase first ASCII letter must reject");
    bad = wire;
    strcpy(bad.legacy_name_key_hex, "4d334865726f");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "noncanonical interior ASCII uppercase must reject");
    bad = wire;
    strcpy(bad.legacy_name_key_hex, "20");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "all-whitespace route name must reject");
    bad = wire;
    strcpy(bad.legacy_shard, "12");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "stored shard must match SHA-1 of exact canonical name bytes");
    bad = wire;
    strcpy(bad.legacy_name_key_hex,
           "c3a9c3a9c3a9c3a9c3a9c3a9c3a9");
    strcpy(bad.legacy_shard, "81");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) == 0,
                     "seven UTF-8 e-acute codepoints must accept at exactly 14 bytes");
    bad = wire;
    strcpy(bad.legacy_name_key_hex, "4162636465666768696a6b6c");
    strcpy(bad.legacy_shard, "eb");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) == 0,
                     "canonical 12-codepoint ASCII name must accept");
    bad = wire;
    strcpy(bad.legacy_name_key_hex, "4162636465666768696a6b6c6d");
    strcpy(bad.legacy_shard, "24");
    failed += expect(character_save_journal_v2_request_sha256(&bad, request) < 0,
                     "13-codepoint name must reject even below the 14-byte cap");
    return failed;
}

static int test_parser_rejects_overlong_expected_hash(root)
const char *root;
{
    static const char command[] = "77777777-7777-4777-8777-777777777777";
    static const char wrong_leaf_command[] = "77777777-7777-4777-8777-777777777778";
    static const char trailing_command[] = "77777777-7777-4777-8777-777777777779";
    character_save_journal_v2_wire readback, zero;
    char oversized[769], text[1350];
    int n, failed = 0;

    memset(&zero, 0, sizeof(zero));
    memset(oversized, 'c', sizeof(oversized) - 1);
    oversized[sizeof(oversized) - 1] = 0;
    n = snprintf(text, sizeof(text),
        "version=2\n"
        "state=PREPARED\n"
        "writer_instance_id=11111111-1111-4111-8111-111111111111\n"
        "character_id=22222222-2222-4222-8222-222222222222\n"
        "request_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
        "world_id=muhan\n"
        "legacy_name_key_hex=5465727261\n"
        "legacy_shard=16\n"
        "command_uuid=%s\n"
        "writer_epoch=7\n"
        "writer_revision=9\n"
        "expected_state=existing\n"
        "expected_sha256=%s\n"
        "post_sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"
        "storage_format=1\n"
        "staged_leaf=%s.stage\n", command, oversized, command);
    failed += expect(n > 0 && (size_t)n < sizeof(text) &&
                     write_raw_journal(root, command, text) == 0,
                     "overlong expected hash fixture must be written exactly once");
    memset(&readback, 0xa5, sizeof(readback));
    failed += expect(character_save_journal_v2_read_prepared(root, command,
                                                              &readback) < 0 &&
                     !memcmp(&readback, &zero, sizeof(readback)),
                     "overlong expected hash must reject without overflow or partial output");

    n = snprintf(text, sizeof(text),
        "version=2\nstate=PREPARED\n"
        "writer_instance_id=11111111-1111-4111-8111-111111111111\n"
        "character_id=22222222-2222-4222-8222-222222222222\n"
        "request_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
        "world_id=muhan\nlegacy_name_key_hex=5465727261\nlegacy_shard=16\n"
        "command_uuid=%s\nwriter_epoch=7\nwriter_revision=9\n"
        "expected_state=absent\nexpected_sha256=-\n"
        "post_sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"
        "storage_format=1\nstaged_leaf=caller-controlled.stage\n",
        wrong_leaf_command);
    failed += expect(n > 0 && (size_t)n < sizeof(text) &&
                     write_raw_journal(root, wrong_leaf_command, text) == 0 &&
                     character_save_journal_v2_read_prepared(root,
                         wrong_leaf_command, &readback) < 0,
                     "caller-controlled persisted stage leaf must reject");

    n = snprintf(text, sizeof(text),
        "version=2\nstate=PREPARED\n"
        "writer_instance_id=11111111-1111-4111-8111-111111111111\n"
        "character_id=22222222-2222-4222-8222-222222222222\n"
        "request_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
        "world_id=muhan\nlegacy_name_key_hex=5465727261\nlegacy_shard=16\n"
        "command_uuid=%s\nwriter_epoch=7\nwriter_revision=9\n"
        "expected_state=absent\nexpected_sha256=-\n"
        "post_sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"
        "storage_format=1\nstaged_leaf=%s.stage\ntrailing-byte",
        trailing_command, trailing_command);
    failed += expect(n > 0 && (size_t)n < sizeof(text) &&
                     write_raw_journal(root, trailing_command, text) == 0 &&
                     character_save_journal_v2_read_prepared(root,
                         trailing_command, &readback) < 0,
                     "missing final LF or trailing bytes must reject");
    memset(text, 0, sizeof(text));
    memset(oversized, 0, sizeof(oversized));
    memset(&readback, 0, sizeof(readback));
    return failed;
}

static int test_parser_rejects_embedded_nul(root)
const char *root;
{
    static const char command[] = "78787878-7878-4787-8787-787878787878";
    character_save_journal_v2_wire wire, readback, zero;
    char request[65], text[1200];
    int n, failed = 0;

    fixture(&wire, command);
    memset(wire.post_sha256, 'b', CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN);
    wire.post_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN] = 0;
    if(character_save_journal_v2_request_sha256(&wire, request) != 0) return 1;
    strcpy(wire.request_sha256, request);
    n = snprintf(text, sizeof(text),
        "version=2\nstate=PREPARED\nwriter_instance_id=%s\n"
        "character_id=%s\nrequest_sha256=%s\nworld_id=%s\n"
        "legacy_name_key_hex=%s\nlegacy_shard=%s\ncommand_uuid=%s\n"
        "writer_epoch=7\nwriter_revision=9\nexpected_state=absent\n"
        "expected_sha256=-\npost_sha256=%s\nstorage_format=1\n"
        "staged_leaf=%s.stage\nXignored-trailing-bytes",
        wire.writer_instance_id, wire.character_id, wire.request_sha256,
        wire.world_id, wire.legacy_name_key_hex, wire.legacy_shard,
        wire.command_uuid, wire.post_sha256, wire.command_uuid);
    if(n <= 0 || (size_t)n >= sizeof(text)) return 1;
    text[n - (int)strlen("Xignored-trailing-bytes")] = 0;
    failed += expect(write_raw_journal_bytes(root, command, text, (size_t)n) == 0,
                     "embedded NUL wire fixture must preserve bytes after NUL");
    memset(&readback, 0xa5, sizeof(readback));
    memset(&zero, 0, sizeof(zero));
    failed += expect(character_save_journal_v2_read_prepared(root, command,
                                                              &readback) < 0 &&
                     !memcmp(&readback, &zero, sizeof(readback)),
                     "embedded NUL plus ignored trailing bytes must reject wholly");
    memset(text, 0, sizeof(text));
    return failed;
}

static int test_wire_stage_and_fsync(root)
char *root;
{
    static const char command[] = "33333333-3333-4333-8333-333333333333";
    static const char payload[] = "synthetic-stage-only\n";
    character_save_journal_v2_wire wire, readback, bad;
    char request[65], path[512];
    struct stat st;
    unsigned int stage_file, stage_dir, journal_file, journal_dir;
    int failed = 0;
    fixture(&wire, command);
    failed += expect(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) == 0,
                     "synthetic stage digest must be available");
    failed += expect(character_save_journal_v2_request_sha256(&wire, request) == 0,
                     "canonical request digest must build");
    strcpy(wire.request_sha256, request);
    failed += expect(character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) == 0,
                     "canonical v2 PREPARED must stage and fsync");
    failed += expect(character_save_journal_v2_read_prepared(root, command, &readback) == 0 &&
                     !memcmp(&wire, &readback, sizeof(wire)),
                     "v2 wire must preserve all canonical fields exactly");
    failed += expect(character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     character_save_journal_v2_read_prepared(root, command,
                                                              &readback) == 0 &&
                     !memcmp(&wire, &readback, sizeof(wire)),
                     "exact PREPARED retry must not overwrite immutable evidence");
    failed += expect(join(path, sizeof(path), root,
                          "character-save-stage/33333333-3333-4333-8333-333333333333.stage") == 0 &&
                     lstat(path, &st) == 0 && S_ISREG(st.st_mode) &&
                     (st.st_mode & 0777) == 0600 && st.st_nlink == 1,
                     "derived stage leaf must be one private regular file");
    character_save_journal_v2_fsync_counts_for_test(&stage_file, &stage_dir,
                                                     &journal_file, &journal_dir);
    failed += expect(stage_file && stage_dir && journal_file && journal_dir,
                     "PREPARED must retain stage and journal fsync evidence");
    failed += expect(character_save_journal_v2_stage_leaf("not-a-uuid", request,
                                                          sizeof(request)) < 0,
                     "caller-controlled stage suffix has no API and invalid UUID rejects");
    bad = wire;
    strcpy(bad.command_uuid, "33333333-3333-4333-8333-333333333334");
    bad.character_id[35] = '3';
    failed += expect(character_save_journal_v2_prepare(root, &bad, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, bad.command_uuid),
                     "syntactically valid mismatched identity/digest must reject before mutation");
    return failed;
}

static int test_descriptor_rejections(root)
char *root;
{
    static const char command[] = "44444444-4444-4444-8444-444444444444";
    static const char payload[] = "descriptor-fixture";
    static const char *mode_components[] = {
        "", "player", "player/16", "character-save-journal",
        "character-save-stage"
    };
    character_save_journal_v2_wire wire;
    char request[65], path[512], alias[512], source[512], linked[512];
    struct stat st;
    uid_t other_uid;
    size_t i;
    int fd, ready[2], release[2], status, prepare_result, failed = 0;
    pid_t child;

    fixture(&wire, command);
    if(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) != 0 ||
       character_save_journal_v2_request_sha256(&wire, request) != 0) return 1;
    strcpy(wire.request_sha256, request);

    for(i = 0; i < sizeof(mode_components) / sizeof(mode_components[0]); i++) {
        if(mode_components[i][0]) {
            if(join(path, sizeof(path), root, mode_components[i]) != 0) return failed + 1;
        }
        else strcpy(path, root);
        if(chmod(path, 0750) != 0) return failed + 1;
        failed += expect(character_save_journal_v2_prepare(root, &wire, payload,
                                                            sizeof(payload) - 1) < 0 &&
                         artifacts_absent(root, command),
                         "every wrong root/player/shard/journal/stage mode must reject before mutation");
        if(chmod(path, 0700) != 0) return failed + 1;
    }

    other_uid = getuid() == (uid_t)0 ? (uid_t)1 : (uid_t)0;
    character_save_journal_v2_set_trusted_uid_for_test(other_uid);
    failed += expect(character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, command),
                     "wrong trusted uid must reject before mutation");
    character_save_journal_v2_set_trusted_uid_for_test(getuid());

    if(snprintf(alias, sizeof(alias), "%s-symlink", root) < 0) return failed + 1;
    failed += expect(symlink(root, alias) == 0 &&
                     character_save_journal_v2_prepare(alias, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, command),
                     "symlink trust root must reject before mutation");
    unlink(alias);

    if(join(path, sizeof(path), root, "character-save-stage") != 0 ||
       rmdir(path) != 0) return failed + 1;
    failed += expect(symlink("outside", path) == 0 &&
                     character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, command),
                     "stage symlink ancestor must reject before mutation");
    unlink(path);
    if(mkdir(path, 0700) != 0) return failed + 1;

    if(join(path, sizeof(path), root, "character-save-journal") != 0 ||
       rmdir(path) != 0) return failed + 1;
    failed += expect(symlink("outside", path) == 0 &&
                     character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, command),
                     "journal symlink ancestor must reject before mutation");
    unlink(path);
    if(mkdir(path, 0700) != 0) return failed + 1;

    if(join(path, sizeof(path), root, "player/16") != 0 ||
       rmdir(path) != 0) return failed + 1;
    failed += expect(symlink("outside", path) == 0 &&
                     character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, command),
                     "shard symlink ancestor must reject before mutation");
    unlink(path);
    if(mkdir(path, 0700) != 0) return failed + 1;

    if(join(path, sizeof(path), root, "player/16") != 0 ||
       rmdir(path) != 0 || join(path, sizeof(path), root, "player") != 0 ||
       rmdir(path) != 0) return failed + 1;
    failed += expect(symlink("outside", path) == 0 &&
                     character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     artifacts_absent(root, command),
                     "player symlink ancestor must reject before mutation");
    unlink(path);
    if(mkdir(path, 0700) != 0 ||
       join(path, sizeof(path), root, "player/16") != 0 ||
       mkdir(path, 0700) != 0) return failed + 1;

    if(join(path, sizeof(path), root, "character-save-stage") != 0 ||
       join(source, sizeof(source), root, "character-save-stage-before-swap") != 0 ||
       pipe(ready) != 0 || pipe(release) != 0) return failed + 1;
    child = fork();
    if(child == 0) {
        char signal;
        int code = 0;
        close(ready[1]);
        close(release[0]);
        if(read(ready[0], &signal, 1) != 1) code = 2;
        else if(rename(path, source) != 0) code = 3;
        else if(mkdir(path, 0700) != 0) code = 4;
        if(write(release[1], "x", 1) != 1 && code == 0) code = 5;
        _exit(code);
    }
    if(child < 0) return failed + 1;
    close(ready[0]);
    close(release[1]);
    character_save_journal_v2_pause_component_after_lstat_for_test(
        "character-save-stage", ready[1], release[0]);
    prepare_result = character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1);
    character_save_journal_v2_pause_component_after_lstat_for_test(0, -1, -1);
    close(ready[1]);
    close(release[0]);
    status = 0;
    if(waitpid(child, &status, 0) != child) return failed + 1;
    failed += expect(prepare_result < 0 && WIFEXITED(status) &&
                     WEXITSTATUS(status) == 0 && artifacts_absent(root, command),
                     "component replacement between lstat and open must fail by inode check");
    if(rmdir(path) != 0 || rename(source, path) != 0) return failed + 1;

    if(join(path, sizeof(path), root,
            "character-save-stage/44444444-4444-4444-8444-444444444444.stage") != 0)
        return failed + 1;
    failed += expect(symlink("../../outside", path) == 0 &&
                     character_save_journal_v2_prepare(root, &wire, payload,
                                                        sizeof(payload) - 1) < 0 &&
                     lstat(path, &st) == 0 && S_ISLNK(st.st_mode),
                     "derived stage symlink leaf must reject without replacement");
    unlink(path);

    if(join(path, sizeof(path), root,
            "character-save-stage/44444444-4444-4444-8444-444444444444.stage") == 0 &&
       mkfifo(path, 0600) == 0) {
        fd = open(path, O_RDONLY | O_NONBLOCK | O_NOFOLLOW);
        failed += expect(fd >= 0 && character_save_journal_v2_hash_fd(fd, request) < 0,
                         "FIFO must reject without blocking");
        if(fd >= 0) close(fd);
        unlink(path);
    }

    if(join(source, sizeof(source), root, "hardlink-source") != 0 ||
       join(linked, sizeof(linked), root, "hardlink-alias") != 0) return failed + 1;
    fd = open(source, O_CREAT | O_EXCL | O_RDWR | O_NOFOLLOW, 0600);
    if(fd < 0 || write_all(fd, payload, sizeof(payload) - 1) != 0 ||
       link(source, linked) != 0 || lseek(fd, 0, SEEK_SET) < 0) {
        if(fd >= 0) close(fd);
        return failed + 1;
    }
    failed += expect(character_save_journal_v2_hash_fd(fd, request) < 0,
                     "hard-linked regular leaf must reject");
    close(fd);
    unlink(linked);
    unlink(source);

    fd = open("/dev/null", O_RDONLY | O_NONBLOCK);
    failed += expect(fd >= 0 && character_save_journal_v2_hash_fd(fd, request) < 0,
                     "device leaf must reject");
    if(fd >= 0) close(fd);
    return failed;
}

static int test_growth_and_faults(root)
char *root;
{
    static const char command[] = "55555555-5555-4555-8555-555555555555";
    static const char payload[] = "growth-fixture";
    character_save_journal_v2_wire wire;
    char request[65], path[512];
    int fd, ready[2], release[2], status, failed = 0;
    pid_t child;
    fixture(&wire, command);
    if(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) != 0 ||
       character_save_journal_v2_request_sha256(&wire, request) != 0) return 1;
    strcpy(wire.request_sha256, request);
    if(join(path, sizeof(path), root, "growth-file") != 0 ||
       (fd = open(path, O_CREAT | O_TRUNC | O_RDONLY, 0600)) < 0) return 1;
    close(fd);
    fd = open(path, O_RDWR | O_NOFOLLOW);
    if(fd < 0 || write_all(fd, payload, sizeof(payload) - 1) != 0 || lseek(fd, 0, SEEK_SET) < 0 ||
       pipe(ready) != 0 || pipe(release) != 0) { if(fd >= 0) close(fd); return 1; }
    child = fork();
    if(child == 0) {
        char signal;
        int grow_fd;
        close(ready[1]); close(release[0]);
        if(read(ready[0], &signal, 1) != 1) _exit(2);
        grow_fd = open(path, O_WRONLY | O_APPEND | O_NOFOLLOW);
        if(grow_fd < 0 || ftruncate(grow_fd,
              (off_t)CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES + 1) != 0) _exit(3);
        close(grow_fd);
        if(write(release[1], "x", 1) != 1) _exit(4);
        _exit(0);
    }
    close(ready[0]); close(release[1]);
    character_save_journal_v2_pause_hash_after_fstat_for_test(ready[1], release[0]);
    failed += expect(character_save_journal_v2_hash_fd(fd, request) < 0,
                     "open fd growth beyond cumulative 64MiB cap must reject");
    character_save_journal_v2_pause_hash_after_fstat_for_test(-1, -1);
    close(ready[1]); close(release[0]); close(fd); waitpid(child, &status, 0); unlink(path);
    failed += expect(WIFEXITED(status) && WEXITSTATUS(status) == 0,
                     "growth child must extend only after parent fstat");
    return failed;
}

static int test_hash_cap_boundaries(root)
const char *root;
{
    char path[512], digest[65];
    int fd, failed = 0;

    if(join(path, sizeof(path), root, "hash-cap-boundary") != 0) return 1;
    fd = open(path, O_CREAT | O_EXCL | O_RDWR | O_NOFOLLOW, 0600);
    if(fd < 0 || ftruncate(fd, (off_t)CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES) != 0)
        return 1;
    failed += expect(character_save_journal_v2_hash_fd(fd, digest) == 0,
                     "shared stage/live hash reader must accept exactly 64 MiB");
    if(ftruncate(fd, (off_t)CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES + 1) != 0 ||
       lseek(fd, 0, SEEK_SET) < 0) failed++;
    else
        failed += expect(character_save_journal_v2_hash_fd(fd, digest) < 0,
                         "shared stage/live hash reader must reject initial 64 MiB plus one");
    close(fd);
    unlink(path);
    return failed;
}

static int test_fsync_failure_evidence(root)
const char *root;
{
    static const char *commands[] = {
        "66666666-6666-4666-8666-666666666661",
        "66666666-6666-4666-8666-666666666662",
        "66666666-6666-4666-8666-666666666663",
        "66666666-6666-4666-8666-666666666664"
    };
    static const char payload[] = "fsync-failure-fixture";
    character_save_journal_v2_wire wire;
    char request[65];
    int failed = 0, result, kind;

    for(kind = 1; kind <= 4; kind++) {
        fixture(&wire, commands[kind - 1]);
        if(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) != 0 ||
           character_save_journal_v2_request_sha256(&wire, request) != 0)
            return failed + 1;
        strcpy(wire.request_sha256, request);
        character_save_journal_v2_fail_fsync_for_test(kind == 1, kind == 2,
                                                       kind == 3, kind == 4);
        result = character_save_journal_v2_prepare(root, &wire, payload,
                                                    sizeof(payload) - 1);
        character_save_journal_v2_fail_fsync_for_test(0, 0, 0, 0);
        failed += expect(result < 0 &&
                         artifact_exists(root, "character-save-stage",
                                         commands[kind - 1], ".stage") &&
                         (kind < 3 || artifact_exists(root,
                                         "character-save-journal",
                                         commands[kind - 1], ".prepared")),
                         "every file/directory fsync fault must fail closed and retain evidence");
        if(kind < 3)
            failed += expect(!artifact_exists(root, "character-save-journal",
                                              commands[kind - 1], ".prepared"),
                             "stage-side fsync failure must not create PREPARED");
    }
    return failed;
}

static int test_write_and_close_durability(root)
const char *root;
{
    static const char *commands[] = {
        "68686868-6868-4686-8686-686868686861",
        "68686868-6868-4686-8686-686868686862",
        "68686868-6868-4686-8686-686868686863",
        "68686868-6868-4686-8686-686868686864",
        "68686868-6868-4686-8686-686868686865",
        "68686868-6868-4686-8686-686868686866"
    };
    static const char payload[] = "write-and-close-fixture";
    character_save_journal_v2_wire wire, readback, zero;
    char request[65];
    int failed = 0, result, kind;

    fixture(&wire, commands[0]);
    if(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) != 0 ||
       character_save_journal_v2_request_sha256(&wire, request) != 0)
        return 1;
    strcpy(wire.request_sha256, request);
    character_save_journal_v2_write_faults_for_test(1, 1, 0, 0);
    result = character_save_journal_v2_prepare(root, &wire, payload,
                                                sizeof(payload) - 1);
    character_save_journal_v2_write_faults_for_test(0, 0, 0, 0);
    failed += expect(result == 0 &&
                     character_save_journal_v2_read_prepared(root, commands[0],
                                                              &readback) == 0 &&
                     !memcmp(&wire, &readback, sizeof(wire)),
                     "interrupted and short writes must retry to an exact durable record");

    for(kind = 1; kind <= 2; kind++) {
        fixture(&wire, commands[kind]);
        if(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) != 0 ||
           character_save_journal_v2_request_sha256(&wire, request) != 0)
            return failed + 1;
        strcpy(wire.request_sha256, request);
        character_save_journal_v2_write_faults_for_test(0, 0,
                                                         kind == 1,
                                                         kind == 2);
        result = character_save_journal_v2_prepare(root, &wire, payload,
                                                    sizeof(payload) - 1);
        character_save_journal_v2_write_faults_for_test(0, 0, 0, 0);
        failed += expect(result < 0 &&
                         artifact_exists(root, "character-save-stage",
                                         commands[kind], ".stage") &&
                         !artifact_exists(root, "character-save-journal",
                                          commands[kind], ".prepared"),
                         "zero-length or I/O write result must stop before PREPARED and retain stage evidence");
    }

    for(kind = 1; kind <= 3; kind++) {
        fixture(&wire, commands[kind + 2]);
        if(hash_bytes(root, payload, sizeof(payload) - 1, wire.post_sha256) != 0 ||
           character_save_journal_v2_request_sha256(&wire, request) != 0)
            return failed + 1;
        strcpy(wire.request_sha256, request);
        character_save_journal_v2_fail_close_once_for_test(kind);
        result = character_save_journal_v2_prepare(root, &wire, payload,
                                                    sizeof(payload) - 1);
        character_save_journal_v2_fail_close_once_for_test(0);
        failed += expect(result < 0 &&
                         artifact_exists(root, "character-save-stage",
                                         commands[kind + 2], ".stage") &&
                         ((kind == 3) == artifact_exists(
                             root, "character-save-journal",
                             commands[kind + 2], ".prepared")),
                         "uncertain close result must report failure without reusing the consumed descriptor");
    }

    memset(&readback, 0xa5, sizeof(readback));
    memset(&zero, 0, sizeof(zero));
    character_save_journal_v2_fail_close_once_for_test(4);
    result = character_save_journal_v2_read_prepared(root, commands[0], &readback);
    character_save_journal_v2_fail_close_once_for_test(0);
    failed += expect(result < 0 && !memcmp(&readback, &zero, sizeof(readback)) &&
                     character_save_journal_v2_read_prepared(root, commands[0],
                                                              &readback) == 0,
                     "journal read close uncertainty must zero output and remain readable on retry");
    return failed;
}

int main(void)
{
    char temporary_base[512], root[512];
    int failed, formatted;
    if(!realpath("/tmp", temporary_base)) return 1;
    formatted = snprintf(root, sizeof(root),
                         "%s/character-save-journal-v2-test-XXXXXX",
                         temporary_base);
    if(formatted < 0 || (size_t)formatted >= sizeof(root) || !mkdtemp(root) ||
       make_dir(root, "player") || make_dir(root, "player/16") ||
       make_dir(root, "character-save-journal") || make_dir(root, "character-save-stage")) return 1;
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    failed = test_sql_golden_and_name_validation() +
             test_descriptor_rejections(root) + test_wire_stage_and_fsync(root) +
             test_growth_and_faults(root) +
             test_hash_cap_boundaries(root) +
             test_fsync_failure_evidence(root) +
             test_write_and_close_durability(root) +
             test_parser_rejects_overlong_expected_hash(root) +
             test_parser_rejects_embedded_nul(root);
    failed += expect(teardown_fixture(root) == 0,
                     "disposable fixture teardown must remove only its exact tree");
    puts(failed ? "character_save_journal_v2_test: failed" : "character_save_journal_v2_test: ok");
    return failed ? 1 : 0;
}
