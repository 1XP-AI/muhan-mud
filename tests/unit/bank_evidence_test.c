#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "bank_evidence.h"
#include "bank_store.h"
#include "mstruct.h"

static char bank_path[512];
static char replacement_path[512];
static int store_calls;

/* The active facade remains linkable for the no-dispatch assertion, but this
 * test never grants it a usable legacy decoder/writer. */
int file_bank_store_save(char *name, object *value)
{ (void)name; (void)value; return BANK_STORE_ERROR; }
int file_bank_store_load(char *name, object **value)
{ (void)name; if(value)*value=0; return BANK_STORE_ERROR; }

static int unused_save(void *opaque, char *name, object *value)
{ (void)opaque; (void)name; (void)value; ++store_calls; return BANK_STORE_ERROR; }
static int unused_load(void *opaque, char *name, object **value)
{ (void)opaque; (void)name; (void)value; ++store_calls; return BANK_STORE_ERROR; }

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "bank_evidence_test: %s\n", message);
    return 1;
}

static int cleared(const bank_evidence *value)
{
    return !value->sha256[0] && !value->octet_count && !value->item_count &&
        !value->max_depth;
}

static int expect_failure(bank_evidence_result actual,
    bank_evidence_result expected, const bank_evidence *value, const char *message)
{
    return expect(actual == expected && value->version == BANK_EVIDENCE_VERSION &&
        value->result == expected && cleared(value), message);
}

static void object_init(object *value, const char *name)
{
    memset(value, 0, sizeof(*value));
    strcpy(value->name, name);
    value->shotsmax = value->shotscur = 1;
}

static int write_bytes(const void *bytes, size_t length, int flags, mode_t mode)
{
    int fd = open(bank_path, O_WRONLY | O_CREAT | O_TRUNC | flags, mode);
    if(fd < 0) return -1;
    if(length && write(fd, bytes, length) != (ssize_t)length) { close(fd); return -1; }
    return close(fd);
}

static int write_node(int fd, object *value, int children)
{
    return write(fd, value, sizeof(*value)) == (ssize_t)sizeof(*value) &&
        write(fd, &children, sizeof(children)) == (ssize_t)sizeof(children) ? 0 : -1;
}

static int write_valid_bank_at(path, root_name, trailing)
const char *path;
const char *root_name;
int trailing;
{
    object root, child;
    int fd;
    unsigned char tail = 0xaa;
    object_init(&root, root_name);
    object_init(&child, "ruby");
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0) return -1;
    if(write_node(fd, &root, 1) || write_node(fd, &child, 0) ||
       (trailing && write(fd, &tail, 1) != 1) || close(fd) < 0) return -1;
    return 0;
}

static int write_valid_bank(int trailing)
{ return write_valid_bank_at(bank_path, "bank-root", trailing); }

static int write_deep_bank(void)
{
    object value;
    int fd, i, count;
    fd = open(bank_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0) return -1;
    for(i = 0; i < 65; ++i) {
        object_init(&value, "nested");
        count = i == 64 ? 0 : 1;
        if(write_node(fd, &value, count)) { close(fd); return -1; }
    }
    return close(fd);
}

static void mutate_after_read(void *opaque)
{
    int fd;
    unsigned char byte = 1;
    (void)opaque;
    fd = open(bank_path, O_WRONLY | O_APPEND);
    if(fd >= 0) { (void)write(fd, &byte, 1); close(fd); }
}

static void mutate_same_size_after_read(void *opaque)
{
    int fd;
    unsigned char byte = 1;
    (void)opaque;
    fd = open(bank_path, O_WRONLY);
    if(fd >= 0) { (void)pwrite(fd, &byte, 1, 0); close(fd); }
}

static void replace_same_size_after_read(void *opaque)
{
    (void)opaque;
    (void)rename(replacement_path, bank_path);
}

int main(void)
{
    char root[] = "/tmp/muhan-bank-evidence.XXXXXX";
    char player[512], bank_dir[512], link_path[512], hard_path[512], long_name[32];
    char player_real[512], bank_real[512], outside[] = "/tmp/muhan-bank-evidence-escape.XXXXXX";
    char outside_player[512], outside_bank[512], outside_file[512];
    char invalid_utf8[] = "\303\050", control_name[] = "bad\001name";
    object value;
    bank_evidence first, second;
    bank_store_ops unused = { unused_save, unused_load, 0 };
    int fd, count, failed = 0;

    if(!mkdtemp(root) || setenv("MUHAN_HOME", root, 1) < 0) return 2;
    snprintf(player, sizeof(player), "%s/player", root);
    snprintf(bank_dir, sizeof(bank_dir), "%s/bank", player);
    snprintf(bank_path, sizeof(bank_path), "%s/Alice", bank_dir);
    snprintf(link_path, sizeof(link_path), "%s/Link", bank_dir);
    snprintf(hard_path, sizeof(hard_path), "%s/Hard", bank_dir);
    snprintf(player_real, sizeof(player_real), "%s/player.real", root);
    snprintf(bank_real, sizeof(bank_real), "%s/bank.real", player);
    snprintf(replacement_path, sizeof(replacement_path), "%s/.Alice.replacement", bank_dir);
    if(mkdir(player, 0700) || mkdir(bank_dir, 0700)) return 2;

    if(write_valid_bank(0)) return 2;
    bank_store_set(&unused);
    failed += expect(bank_evidence_inspect("Alice", &first) == BANK_EVIDENCE_OK &&
        first.version == BANK_EVIDENCE_VERSION && first.result == BANK_EVIDENCE_OK &&
        first.octet_count == (uint64_t)(2 * (sizeof(object) + sizeof(int))) &&
        first.item_count == 1 && first.max_depth == 1 && strlen(first.sha256) == 64,
        "canonical legacy bank must yield bounded metadata only");
    failed += expect(!strcmp(first.sha256,
        "fa8859873bcdef6493f79e862c5dfcb9863c82a1917c5bbaae985f9dd814d780"),
        "canonical legacy fixture must retain its exact SHA-256 evidence");
    failed += expect(!store_calls, "inspection must not dispatch through the active bank store");
    failed += expect(bank_evidence_inspect("Alice", &second) == BANK_EVIDENCE_OK &&
        !memcmp(&first, &second, sizeof(first)),
        "unchanged canonical input must yield deterministic evidence");
    bank_store_reset();

    failed += expect_failure(bank_evidence_inspect("Missing", &second),
        BANK_EVIDENCE_NOT_FOUND, &second, "missing records must be distinct and clear metadata");
    failed += expect_failure(bank_evidence_inspect("bad/name", &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "slashed names must not reach the file store");
    failed += expect_failure(bank_evidence_inspect("", &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "empty names must not reach the file store");
    failed += expect_failure(bank_evidence_inspect("bad\\name", &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "backslash names must not reach the file store");
    failed += expect_failure(bank_evidence_inspect("bad:name", &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "colon names rejected by player admission must not reach the file store");
    failed += expect_failure(bank_evidence_inspect(control_name, &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "control-byte names must not reach the file store");
    failed += expect_failure(bank_evidence_inspect(invalid_utf8, &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "invalid UTF-8 names must not reach the file store");
    memset(long_name, 'x', sizeof(long_name) - 1); long_name[sizeof(long_name) - 1] = 0;
    failed += expect_failure(bank_evidence_inspect(long_name, &second),
        BANK_EVIDENCE_INVALID_INPUT, &second, "overlong names must not reach the file store");

    object_init(&value, "bank-root");
    if(write_bytes(&value, sizeof(value), 0, 0600)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_CORRUPT,
        &second, "truncated native nodes must fail closed");
    count = -1;
    if(write_valid_bank(0) || (fd = open(bank_path, O_WRONLY | O_APPEND)) < 0 ||
       write(fd, &count, sizeof(count)) != (ssize_t)sizeof(count) || close(fd)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_CORRUPT,
        &second, "trailing bytes must fail closed");
    fd = open(bank_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write_node(fd, &value, -1) || close(fd)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_CORRUPT,
        &second, "negative child counts must fail closed");
    count = 4097;
    fd = open(bank_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write_node(fd, &value, count) || close(fd)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_LIMIT,
        &second, "excessive child counts must remain bounded");
    if(write_deep_bank()) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_LIMIT,
        &second, "excessive nesting must remain bounded");
    object_init(&value, "a"); value.name[1] = 0; value.name[2] = 'x';
    fd = open(bank_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write_node(fd, &value, 0) || close(fd)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_CORRUPT,
        &second, "fixed-string tails must be canonical before hashing");

    if(write_valid_bank(0) || symlink("Alice", link_path)) return 2;
    failed += expect_failure(bank_evidence_inspect("Link", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "symlinks must not be treated as evidence sources");
    unlink(link_path);
    if(link(bank_path, hard_path)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "hard-linked sources must not be treated as evidence");
    unlink(hard_path);
    if(chmod(bank_path, 0644)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "unsafe source modes must be rejected");
    if(chmod(bank_path, 0600) || unlink(bank_path) || mkfifo(bank_path, 0600)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "non-regular sources must be rejected");
    unlink(bank_path);

    if(write_valid_bank(0) || chmod(player, 0755)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "PLAYERPATH must remain a private directory");
    if(chmod(player, 0700) || chmod(bank_dir, 0750)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "the fixed bank directory must remain private");
    if(chmod(bank_dir, 0700)) return 2;
    file_bank_store_test_set_expected_uid(geteuid() ? (uid_t)0 : (uid_t)1);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "all fixed directories and leaves must retain the expected owner");
    file_bank_store_test_reset_expected_uid();
    if((fd = open(bank_path, O_WRONLY)) < 0 ||
       ftruncate(fd, (off_t)BANK_EVIDENCE_MAX_OCTETS + 1) || close(fd)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "oversized records must be rejected before allocation");

    if(!mkdtemp(outside)) return 2;
    snprintf(outside_player, sizeof(outside_player), "%s/player", outside);
    snprintf(outside_bank, sizeof(outside_bank), "%s/bank", outside_player);
    snprintf(outside_file, sizeof(outside_file), "%s/Alice", outside_bank);
    if(mkdir(outside_player, 0700) || mkdir(outside_bank, 0700) ||
       write_valid_bank_at(outside_file, "escaped", 0)) return 2;
    if(rename(player, player_real) || symlink(outside_player, player)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "PLAYERPATH symlink escapes must fail closed");
    if(unlink(player) || rename(player_real, player)) return 2;
    if(rename(bank_dir, bank_real) || symlink(outside_bank, bank_dir)) return 2;
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "bank-directory symlink escapes must fail closed");
    if(unlink(bank_dir) || rename(bank_real, bank_dir)) return 2;

    if(write_valid_bank(0)) return 2;
    bank_evidence_test_set_after_read(mutate_after_read, 0);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "mutation after open must not yield evidence");
    bank_evidence_test_reset();
    if(write_valid_bank(0)) return 2;
    bank_evidence_test_set_after_read(mutate_same_size_after_read, 0);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "same-size in-place mutations must not yield evidence");
    bank_evidence_test_reset();
    if(write_valid_bank(0) || write_valid_bank_at(replacement_path, "replacement", 0)) return 2;
    bank_evidence_test_set_after_read(replace_same_size_after_read, 0);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "same-size replacement after open must not yield evidence");
    bank_evidence_test_reset();
    if(write_valid_bank(0)) return 2;
    bank_evidence_test_fail_next(BANK_EVIDENCE_TEST_FAULT_ALLOC);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "allocation failures must clear evidence");
    bank_evidence_test_fail_next(BANK_EVIDENCE_TEST_FAULT_READ);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "read failures must clear evidence");
    bank_evidence_test_fail_next(BANK_EVIDENCE_TEST_FAULT_CLOSE);
    failed += expect_failure(bank_evidence_inspect("Alice", &second), BANK_EVIDENCE_IO_ERROR,
        &second, "close failures must clear evidence");
    bank_evidence_test_reset();

    unlink(bank_path); rmdir(bank_dir); rmdir(player); rmdir(root);
    unlink(outside_file); rmdir(outside_bank); rmdir(outside_player); rmdir(outside);
    return failed ? 1 : 0;
}
