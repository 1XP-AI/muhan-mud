#include "character_save_journal_v2_ack.h"

#include "character_save_journal_v2.h"

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "v2 ack requires O_NOFOLLOW"
#endif
#ifndef O_DIRECTORY
#error "v2 ack requires O_DIRECTORY"
#endif

#define V2_ACK_RUNTIME_UID 10001
#define V2_ACK_TEXT_MAX 1400

typedef struct v2_ack_tree {
    int root_fd, player_fd, shard_fd, journal_fd, stage_fd;
} v2_ack_tree;

/* This private writer seam also proves a stable held generation across the
 * potentially blocking receipt callback.  It is intentionally not public:
 * callers cannot manufacture or compare an authority generation. */
extern character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held_for_route(
    const character_save_journal_v2_writer_context *,
    character_save_journal_v2_writer_tuple *, uint64_t *);

static uid_t v2_ack_trusted_uid = V2_ACK_RUNTIME_UID;
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
static int v2_ack_fail_after_temp;
static int v2_ack_read_close, v2_ack_live_close, v2_ack_marker_close;
static int v2_ack_post_link, v2_ack_first_parent_fsync;
static int v2_ack_temp_unlink, v2_ack_final_parent_fsync;
static int v2_ack_marker_file_fsync;
#endif

static size_t ack_bounded(text, limit)
const char *text;
size_t limit;
{
    size_t i;
    if(!text) return limit + 1;
    for(i = 0; i <= limit; i++) if(!text[i]) return i;
    return limit + 1;
}

static int ack_uuid(text)
const char *text;
{
    size_t i;
    if(ack_bounded(text, CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) !=
       CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) return 0;
    for(i = 0; i < CHARACTER_SAVE_JOURNAL_V2_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(text[i] != '-') return 0;
        } else if(!((text[i] >= '0' && text[i] <= '9') ||
                    (text[i] >= 'a' && text[i] <= 'f'))) return 0;
    }
    return 1;
}

static int ack_dir_ok(fd)
int fd;
{
    struct stat st;
    return fd >= 0 && fstat(fd, &st) == 0 && S_ISDIR(st.st_mode) &&
           st.st_uid == v2_ack_trusted_uid && (st.st_mode & 07777) == 0700;
}

static int ack_file_ok(fd)
int fd;
{
    struct stat st;
    return fd >= 0 && fstat(fd, &st) == 0 && S_ISREG(st.st_mode) &&
           st.st_uid == v2_ack_trusted_uid && (st.st_mode & 07777) == 0600 &&
           st.st_nlink == 1;
}

static int ack_file_links_ok(fd, links)
int fd;
nlink_t links;
{
    struct stat st;
    return fd >= 0 && fstat(fd, &st) == 0 && S_ISREG(st.st_mode) &&
           st.st_uid == v2_ack_trusted_uid && (st.st_mode & 07777) == 0600 &&
           st.st_nlink == links;
}

/* Close consumes descriptor ownership regardless of its error result.  POSIX
 * leaves the fd state unspecified on close failure; retrying close risks
 * closing an unrelated later descriptor. */
static int ack_close_consume(fd, kind)
int *fd;
int kind;
{
    int owned, result;
    (void)kind;
    if(!fd || *fd < 0) return -1;
    owned = *fd;
    *fd = -1;
    result = close(owned);
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
    if((kind == 1 && v2_ack_read_close) ||
       (kind == 2 && v2_ack_live_close) ||
       (kind == 3 && v2_ack_marker_close)) {
        if(kind == 1) v2_ack_read_close = 0;
        else if(kind == 2) v2_ack_live_close = 0;
        else v2_ack_marker_close = 0;
        errno = EIO;
        return -1;
    }
#endif
    return result;
}

static int ack_component(parent, name)
int parent;
const char *name;
{
    struct stat before, after;
    int fd;
    if(fstatat(parent, name, &before, AT_SYMLINK_NOFOLLOW) ||
       !S_ISDIR(before.st_mode) || before.st_uid != v2_ack_trusted_uid ||
       (before.st_mode & 07777) != 0700) return -1;
    fd = openat(parent, name, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if(!ack_dir_ok(fd) || fstat(fd, &after) || before.st_dev != after.st_dev ||
       before.st_ino != after.st_ino) {
        if(fd >= 0) close(fd);
        return -1;
    }
    return fd;
}

static void ack_tree_close(tree)
v2_ack_tree *tree;
{
    if(!tree) return;
    if(tree->stage_fd >= 0) close(tree->stage_fd);
    if(tree->journal_fd >= 0) close(tree->journal_fd);
    if(tree->shard_fd >= 0) close(tree->shard_fd);
    if(tree->player_fd >= 0) close(tree->player_fd);
    if(tree->root_fd >= 0) close(tree->root_fd);
    tree->root_fd = tree->player_fd = tree->shard_fd = tree->journal_fd = tree->stage_fd = -1;
}

static int ack_tree_open(root_fd, shard, full, tree)
int root_fd;
const char *shard;
int full;
v2_ack_tree *tree;
{
    tree->root_fd = root_fd;
    tree->player_fd = tree->shard_fd = tree->journal_fd = tree->stage_fd = -1;
    if(!ack_dir_ok(root_fd) || (full && (!shard || strlen(shard) != 2))) goto bad;
    tree->journal_fd = ack_component(root_fd, "character-save-journal");
    if(tree->journal_fd < 0) goto bad;
    if(full) {
        tree->player_fd = ack_component(root_fd, "player");
        if(tree->player_fd < 0) goto bad;
        tree->shard_fd = ack_component(tree->player_fd, shard);
        if(tree->shard_fd < 0) goto bad;
        tree->stage_fd = ack_component(root_fd, "character-save-stage");
        if(tree->stage_fd < 0) goto bad;
    }
    return 0;
bad:
    ack_tree_close(tree);
    return -1;
}

static int ack_read_fd(fd, out, out_size)
int fd;
char *out;
size_t out_size;
{
    size_t total = 0;
    ssize_t n;
    char extra;
    if(fd < 0 || !out || out_size < 2) return -1;
    while(total < out_size - 1) {
        n = read(fd, out + total, out_size - 1 - total);
        if(n < 0 && errno == EINTR) continue;
        if(n <= 0) break;
        total += (size_t)n;
    }
    if(total == out_size - 1) {
        do n = read(fd, &extra, 1); while(n < 0 && errno == EINTR);
        if(n) return -1;
    }
    if(n < 0 || memchr(out, 0, total)) return -1;
    out[total] = 0;
    return 0;
}

static int ack_read_text(parent, leaf, out, out_size)
int parent;
const char *leaf;
char *out;
size_t out_size;
{
    int fd, result = -1;
    fd = openat(parent, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(!ack_file_ok(fd) || ack_read_fd(fd, out, out_size) ||
       ack_close_consume(&fd, 1)) goto done;
    result = 0;
done:
    if(fd >= 0) close(fd);
    if(result && out) memset(out, 0, out_size);
    return result;
}

static int ack_read_text_links(parent, leaf, out, out_size, links)
int parent;
const char *leaf;
char *out;
size_t out_size;
nlink_t links;
{
    int fd, result = -1;
    fd = openat(parent, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(!ack_file_links_ok(fd, links) || ack_read_fd(fd, out, out_size) ||
       ack_close_consume(&fd, 1)) goto done;
    result = 0;
done:
    if(fd >= 0) close(fd);
    if(result && out) memset(out, 0, out_size);
    return result;
}

static int ack_copy(destination, destination_size, source)
char *destination;
size_t destination_size;
const char *source;
{
    size_t n = ack_bounded(source, destination_size - 1);
    if(n >= destination_size) return -1;
    memcpy(destination, source, n + 1);
    return 0;
}

static int ack_u64(text, out)
const char *text;
uint64_t *out;
{
    uint64_t value = 0, digit;
    size_t i, n = ack_bounded(text, 19);
    if(!n || n > 19 || (n > 1 && text[0] == '0')) return -1;
    for(i = 0; i < n; i++) {
        if(text[i] < '0' || text[i] > '9') return -1;
        digit = (uint64_t)(text[i] - '0');
        if(value > ((uint64_t)INT64_MAX - digit) / 10) return -1;
        value = value * 10 + digit;
    }
    if(!value) return -1;
    *out = value;
    return 0;
}

static int ack_parse_prepared(text, wire)
char *text;
character_save_journal_v2_wire *wire;
{
    static const char *keys[] = {"version=", "state=", "writer_instance_id=",
        "character_id=", "request_sha256=", "world_id=", "legacy_name_key_hex=",
        "legacy_shard=", "command_uuid=", "writer_epoch=", "writer_revision=",
        "expected_state=", "expected_sha256=", "post_sha256=", "storage_format=",
        "staged_leaf="};
    char *values[16], *line = text, *next, request[65], leaf[44];
    character_save_journal_v2_wire next_wire;
    uint64_t storage;
    int i;
    if(!text || !wire) return -1;
    memset(&next_wire, 0, sizeof(next_wire));
    for(i = 0; i < 16; i++) {
        next = strchr(line, '\n');
        if(!next || strncmp(line, keys[i], strlen(keys[i]))) goto bad;
        *next = 0;
        values[i] = line + strlen(keys[i]);
        line = next + 1;
    }
    if(*line || strcmp(values[0], "2") || strcmp(values[1], "PREPARED")) goto bad;
    if(ack_copy(next_wire.writer_instance_id, sizeof(next_wire.writer_instance_id), values[2]) ||
       ack_copy(next_wire.character_id, sizeof(next_wire.character_id), values[3]) ||
       ack_copy(next_wire.request_sha256, sizeof(next_wire.request_sha256), values[4]) ||
       ack_copy(next_wire.world_id, sizeof(next_wire.world_id), values[5]) ||
       ack_copy(next_wire.legacy_name_key_hex, sizeof(next_wire.legacy_name_key_hex), values[6]) ||
       ack_copy(next_wire.legacy_shard, sizeof(next_wire.legacy_shard), values[7]) ||
       ack_copy(next_wire.command_uuid, sizeof(next_wire.command_uuid), values[8]) ||
       ack_u64(values[9], &next_wire.writer_epoch) || ack_u64(values[10], &next_wire.writer_revision)) goto bad;
    if(!strcmp(values[11], "existing")) {
        next_wire.expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING;
        if(ack_copy(next_wire.expected_sha256, sizeof(next_wire.expected_sha256), values[12])) goto bad;
    } else if(!strcmp(values[11], "absent") && !strcmp(values[12], "-")) {
        next_wire.expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    } else goto bad;
    if(ack_copy(next_wire.post_sha256, sizeof(next_wire.post_sha256), values[13]) ||
       ack_u64(values[14], &storage) || storage > 32767 || !storage ||
       character_save_journal_v2_stage_leaf(next_wire.command_uuid, leaf, sizeof(leaf)) ||
       strcmp(leaf, values[15])) goto bad;
    next_wire.storage_format = (uint16_t)storage;
    next_wire.state = CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    if(character_save_journal_v2_request_sha256(&next_wire, request) ||
       strcmp(request, next_wire.request_sha256)) goto bad;
    *wire = next_wire;
    memset(&next_wire, 0, sizeof(next_wire));
    return 0;
bad:
    memset(&next_wire, 0, sizeof(next_wire));
    memset(wire, 0, sizeof(*wire));
    return -1;
}

static int ack_format(wire, state, out, out_size)
const character_save_journal_v2_wire *wire;
const char *state;
char *out;
size_t out_size;
{
    char leaf[44];
    int n;
    if(!wire || !state || character_save_journal_v2_stage_leaf(wire->command_uuid, leaf, sizeof(leaf))) return -1;
    n = snprintf(out, out_size, "version=2\nstate=%s\nwriter_instance_id=%s\ncharacter_id=%s\n"
        "request_sha256=%s\nworld_id=%s\nlegacy_name_key_hex=%s\nlegacy_shard=%s\n"
        "command_uuid=%s\nwriter_epoch=%" PRIu64 "\nwriter_revision=%" PRIu64
        "\nexpected_state=%s\nexpected_sha256=%s\npost_sha256=%s\nstorage_format=%u\n"
        "staged_leaf=%s\n", state, wire->writer_instance_id, wire->character_id,
        wire->request_sha256, wire->world_id, wire->legacy_name_key_hex, wire->legacy_shard,
        wire->command_uuid, wire->writer_epoch, wire->writer_revision,
        wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ? "existing" : "absent",
        wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ? wire->expected_sha256 : "-",
        wire->post_sha256, (unsigned int)wire->storage_format, leaf);
    return n < 0 || (size_t)n >= out_size ? -1 : 0;
}

static int ack_exact_marker(tree, leaf, wire, state)
v2_ack_tree *tree;
const char *leaf;
const character_save_journal_v2_wire *wire;
const char *state;
{
    char actual[V2_ACK_TEXT_MAX], expected[V2_ACK_TEXT_MAX];
    int result = -1;
    if(ack_format(wire, state, expected, sizeof(expected)) ||
       ack_read_text(tree->journal_fd, leaf, actual, sizeof(actual)) ||
       strcmp(actual, expected)) goto done;
    result = 0;
done:
    memset(actual, 0, sizeof(actual));
    memset(expected, 0, sizeof(expected));
    return result;
}

static int ack_exact_marker_links(tree, leaf, wire, state, links)
v2_ack_tree *tree;
const char *leaf;
const character_save_journal_v2_wire *wire;
const char *state;
nlink_t links;
{
    char actual[V2_ACK_TEXT_MAX], expected[V2_ACK_TEXT_MAX];
    int result = -1;
    if(ack_format(wire, state, expected, sizeof(expected)) ||
       ack_read_text_links(tree->journal_fd, leaf, actual, sizeof(actual), links) ||
       strcmp(actual, expected)) goto done;
    result = 0;
done:
    memset(actual, 0, sizeof(actual));
    memset(expected, 0, sizeof(expected));
    return result;
}

static int ack_marker_file_sync(fd)
int fd;
{
    int result;
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
    if(v2_ack_marker_file_fsync) {
        v2_ack_marker_file_fsync = 0;
        errno = EIO;
        return -1;
    }
#endif
    do result = fsync(fd); while(result < 0 && errno == EINTR);
    return result;
}

/* Re-read and fsync exact temp evidence through one stable descriptor before
 * a link or pair repair can make it durable under a directory name. */
static int ack_exact_marker_sync_links(tree, leaf, wire, state, links)
v2_ack_tree *tree;
const char *leaf;
const character_save_journal_v2_wire *wire;
const char *state;
nlink_t links;
{
    char actual[V2_ACK_TEXT_MAX], expected[V2_ACK_TEXT_MAX];
    int fd = -1, result = -1;
    if(ack_format(wire, state, expected, sizeof(expected))) goto done;
    fd = openat(tree->journal_fd, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(!ack_file_links_ok(fd, links) || ack_read_fd(fd, actual, sizeof(actual)) ||
       strcmp(actual, expected) || ack_marker_file_sync(fd) ||
       ack_close_consume(&fd, 3)) goto done;
    result = 0;
done:
    if(fd >= 0) close(fd);
    memset(actual, 0, sizeof(actual));
    memset(expected, 0, sizeof(expected));
    return result;
}

static int ack_decode_name(wire, out, length)
const character_save_journal_v2_wire *wire;
unsigned char out[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
size_t *length;
{
    size_t i, n;
    unsigned char high, low;
    if(!wire || !out || !length) return -1;
    n = strlen(wire->legacy_name_key_hex);
    if(!n || (n & 1) || n > CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX) return -1;
    for(i = 0; i < n; i += 2) {
        if(!((wire->legacy_name_key_hex[i] >= '0' && wire->legacy_name_key_hex[i] <= '9') ||
             (wire->legacy_name_key_hex[i] >= 'a' && wire->legacy_name_key_hex[i] <= 'f')) ||
           !((wire->legacy_name_key_hex[i+1] >= '0' && wire->legacy_name_key_hex[i+1] <= '9') ||
             (wire->legacy_name_key_hex[i+1] >= 'a' && wire->legacy_name_key_hex[i+1] <= 'f'))) return -1;
        high = (unsigned char)(wire->legacy_name_key_hex[i] <= '9' ? wire->legacy_name_key_hex[i] - '0' : wire->legacy_name_key_hex[i] - 'a' + 10);
        low = (unsigned char)(wire->legacy_name_key_hex[i+1] <= '9' ? wire->legacy_name_key_hex[i+1] - '0' : wire->legacy_name_key_hex[i+1] - 'a' + 10);
        out[i/2] = (unsigned char)((high << 4) | low);
    }
    out[n/2] = 0;
    *length = n/2;
    return 0;
}

static int ack_live_post(tree, wire, name)
v2_ack_tree *tree;
const character_save_journal_v2_wire *wire;
const unsigned char *name;
{
    int fd, result = -1;
    char digest[65];
    fd = openat(tree->shard_fd, (const char *)name, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(!ack_file_ok(fd) || character_save_journal_v2_hash_fd(fd, digest) ||
       strcmp(digest, wire->post_sha256) || ack_close_consume(&fd, 2)) goto done;
    result = 0;
done:
    if(fd >= 0) close(fd);
    memset(digest, 0, sizeof(digest));
    return result;
}

static int ack_tuple_matches(tuple, wire)
const character_save_journal_v2_writer_tuple *tuple;
const character_save_journal_v2_wire *wire;
{
    return tuple && wire && !strcmp(tuple->world_id, wire->world_id) &&
           !strcmp(tuple->writer_instance_id, wire->writer_instance_id) &&
           tuple->writer_epoch == wire->writer_epoch;
}

static character_save_journal_v2_ack_result ack_context_result(status)
character_save_journal_v2_writer_context_status status;
{
    if(status == CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE) return CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_STALE;
    if(status == CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK) return CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_LOCK;
    return CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_INVALID;
}

static int ack_write_all(fd, bytes, length)
int fd;
const char *bytes;
size_t length;
{
    ssize_t n;
    while(length) {
        n = write(fd, bytes, length);
        if(n < 0 && errno == EINTR) continue;
        if(n <= 0) return -1;
        bytes += n;
        length -= (size_t)n;
    }
    return 0;
}

static int ack_parent_sync(fd, kind)
int fd;
int kind;
{
    int result;
    (void)kind;
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
    if((kind == 1 && v2_ack_first_parent_fsync) ||
       (kind == 2 && v2_ack_final_parent_fsync)) {
        if(kind == 1) v2_ack_first_parent_fsync = 0;
        else v2_ack_final_parent_fsync = 0;
        errno = EIO;
        return -1;
    }
#endif
    do result = fsync(fd); while(result < 0 && errno == EINTR);
    return result;
}

static int ack_temp_unlink(parent, leaf)
int parent;
const char *leaf;
{
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
    if(v2_ack_temp_unlink) {
        v2_ack_temp_unlink = 0;
        errno = EIO;
        return -1;
    }
#endif
    return unlinkat(parent, leaf, 0);
}

/* Recover only the exact transient linkat pair.  It is deliberately stricter
 * than a generic temp retry: any third link, different inode, or malformed
 * marker remains incident evidence and prevents a local repair; every valid
 * retained marker still requires an exact DB retry before it is accepted. */
static int ack_reconcile_marker_pair(tree, target, temporary, wire, repair)
v2_ack_tree *tree;
const char *target;
const char *temporary;
const character_save_journal_v2_wire *wire;
int repair;
{
    struct stat target_st, temporary_st;
    if(fstatat(tree->journal_fd, temporary, &temporary_st, AT_SYMLINK_NOFOLLOW)) {
        return errno == ENOENT ? 0 : -1;
    }
    if(fstatat(tree->journal_fd, target, &target_st, AT_SYMLINK_NOFOLLOW)) {
        if(errno != ENOENT) return -1;
        if(ack_exact_marker_links(tree, temporary, wire, "DB_ACKED", 1)) return -1;
        if(repair && ack_exact_marker_sync_links(tree, temporary, wire,
                                                  "DB_ACKED", 1)) return -1;
        return 0;
    }
    if(!S_ISREG(target_st.st_mode) || !S_ISREG(temporary_st.st_mode) ||
       target_st.st_uid != v2_ack_trusted_uid || temporary_st.st_uid != v2_ack_trusted_uid ||
       (target_st.st_mode & 07777) != 0600 || (temporary_st.st_mode & 07777) != 0600 ||
       target_st.st_nlink != 2 || temporary_st.st_nlink != 2 ||
       target_st.st_dev != temporary_st.st_dev || target_st.st_ino != temporary_st.st_ino ||
       ack_exact_marker_links(tree, target, wire, "DB_ACKED", 2) ||
       ack_exact_marker_links(tree, temporary, wire, "DB_ACKED", 2)) return -1;
    if(!repair) return 1;
    if(ack_exact_marker_sync_links(tree, temporary, wire, "DB_ACKED", 2) ||
       ack_parent_sync(tree->journal_fd, 1) ||
       ack_temp_unlink(tree->journal_fd, temporary) ||
       ack_parent_sync(tree->journal_fd, 2)) return -1;
    return 1;
}

static int ack_mark(tree, wire)
v2_ack_tree *tree;
const character_save_journal_v2_wire *wire;
{
    char target[64], temporary[64], text[V2_ACK_TEXT_MAX];
    struct stat st;
    int fd = -1, n, result = -1, pair;
    n = snprintf(target, sizeof(target), "%s.acked", wire->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(target)) goto done;
    n = snprintf(temporary, sizeof(temporary), "%s.acked.tmp", wire->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(temporary) || ack_format(wire, "DB_ACKED", text, sizeof(text))) goto done;
    pair = ack_reconcile_marker_pair(tree, target, temporary, wire, 1);
    if(pair < 0) goto done;
    if(pair > 0) { result = 0; goto done; }
    if(fstatat(tree->journal_fd, target, &st, AT_SYMLINK_NOFOLLOW) == 0) {
        if(!ack_exact_marker(tree, target, wire, "DB_ACKED")) result = 0;
        goto done;
    }
    if(errno != ENOENT) goto done;
    if(fstatat(tree->journal_fd, temporary, &st, AT_SYMLINK_NOFOLLOW) == 0) {
        if(ack_exact_marker(tree, temporary, wire, "DB_ACKED")) goto done;
    } else if(errno == ENOENT) {
        fd = openat(tree->journal_fd, temporary, O_WRONLY | O_CREAT | O_EXCL |
                    O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC, 0600);
        if(!ack_file_ok(fd) || ack_write_all(fd, text, strlen(text)) || ack_marker_file_sync(fd) ||
           ack_close_consume(&fd, 3)) goto done;
    } else goto done;
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
    if(v2_ack_fail_after_temp) {
        v2_ack_fail_after_temp = 0;
        goto done;
    }
#endif
    if(linkat(tree->journal_fd, temporary, tree->journal_fd, target, 0)) goto done;
#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
    if(v2_ack_post_link) { v2_ack_post_link = 0; goto done; }
#endif
    if(ack_parent_sync(tree->journal_fd, 1) ||
       ack_temp_unlink(tree->journal_fd, temporary) ||
       ack_parent_sync(tree->journal_fd, 2)) goto done;
    result = 0;
done:
    if(fd >= 0) close(fd);
    memset(text, 0, sizeof(text));
    return result;
}

character_save_journal_v2_ack_result character_save_journal_v2_ack(
    writer, command_id, callback, callback_opaque)
const character_save_journal_v2_writer_context *writer;
const char *command_id;
character_save_journal_v2_receipt_callback callback;
void *callback_opaque;
{
    v2_ack_tree tree;
    character_save_journal_v2_wire wire;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_receipt receipt;
    character_save_journal_v2_writer_context_status status;
    character_save_journal_v2_receipt_result callback_result;
    char prepared[64], published[64], acked[64], stage_leaf[44];
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    size_t name_length;
    int root_fd = -1, n, pair, local_repair_incomplete = 0;
    struct stat stage_st, marker_st;
    uint64_t held_generation, after_generation;
    character_save_journal_v2_ack_result result = CHARACTER_SAVE_JOURNAL_V2_ACK_IO;
    memset(&tree, 0, sizeof(tree));
    tree.root_fd = tree.player_fd = tree.shard_fd = tree.journal_fd = tree.stage_fd = -1;
    memset(&wire, 0, sizeof(wire));
    memset(&tuple, 0, sizeof(tuple));
    memset(&receipt, 0, sizeof(receipt));
    memset(name, 0, sizeof(name));
    if(!writer || !callback || !ack_uuid(command_id)) return CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_ARGUMENT;
    n = snprintf(prepared, sizeof(prepared), "%s.prepared", command_id);
    if(n < 0 || (size_t)n >= sizeof(prepared)) return CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_ARGUMENT;
    status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) return ack_context_result(status);
    if(ack_tree_open(root_fd, 0, 0, &tree)) { root_fd = -1; result = CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL; goto done; }
    {
        char text[V2_ACK_TEXT_MAX];
        if(ack_read_text(tree.journal_fd, prepared, text, sizeof(text)) || ack_parse_prepared(text, &wire) || strcmp(wire.command_uuid, command_id)) {
            memset(text, 0, sizeof(text)); result = CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL; goto done;
        }
        memset(text, 0, sizeof(text));
    }
    ack_tree_close(&tree);
    root_fd = -1;
    status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) return ack_context_result(status);
    if(ack_tree_open(root_fd, wire.legacy_shard, 1, &tree)) { root_fd = -1; result = CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL; goto done; }
    root_fd = -1;
    n = snprintf(published, sizeof(published), "%s.published", command_id);
    if(n < 0 || (size_t)n >= sizeof(published) || ack_exact_marker(&tree, published, &wire, "LEGACY_PUBLISHED") ||
       character_save_journal_v2_stage_leaf(wire.command_uuid, stage_leaf, sizeof(stage_leaf)) ||
       fstatat(tree.stage_fd, stage_leaf, &stage_st, AT_SYMLINK_NOFOLLOW) == 0 || errno != ENOENT ||
       ack_decode_name(&wire, name, &name_length) || ack_live_post(&tree, &wire, name)) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_LIVE; goto done; }
    (void)name_length;
    status = character_save_journal_v2_writer_validate_held_for_route(writer, &tuple, &held_generation);
    if(status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) { result = ack_context_result(status); goto done; }
    if(!ack_tuple_matches(&tuple, &wire)) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_CONTEXT_STALE; goto done; }
    n = snprintf(acked, sizeof(acked), "%s.acked", command_id);
    if(n < 0 || (size_t)n >= sizeof(acked)) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL; goto done; }
    {
        char temporary[64];
        n = snprintf(temporary, sizeof(temporary), "%s.acked.tmp", command_id);
        if(n < 0 || (size_t)n >= sizeof(temporary)) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_JOURNAL; goto done; }
        pair = ack_reconcile_marker_pair(&tree, acked, temporary, &wire, 0);
        /* A bad local ACK marker is incident evidence, not an authority
         * failure.  All prepared/published/live/writer checks above still
         * fail closed, but an exact idempotent DB receipt must be replayed
         * before we report that the local repair remains incomplete. */
        if(pair < 0) local_repair_incomplete = 1;
        if(pair == 0) {
            if(fstatat(tree.journal_fd, acked, &marker_st,
                       AT_SYMLINK_NOFOLLOW) == 0) {
                if(ack_exact_marker(&tree, acked, &wire, "DB_ACKED"))
                    local_repair_incomplete = 1;
            } else if(errno != ENOENT) {
                local_repair_incomplete = 1;
            }
        }
    }
    receipt.world_id = wire.world_id;
    receipt.legacy_name_key = name;
    receipt.legacy_name_key_length = name_length;
    receipt.character_id = wire.character_id;
    receipt.command_id = wire.command_uuid;
    receipt.writer_instance_id = wire.writer_instance_id;
    receipt.request_sha256 = wire.request_sha256;
    receipt.writer_epoch = (unsigned long long)wire.writer_epoch;
    receipt.writer_revision = (unsigned long long)wire.writer_revision;
    receipt.expected_state = wire.expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ? "existing" : "absent";
    receipt.expected_sha256 = wire.expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ? wire.expected_sha256 : 0;
    receipt.post_sha256 = wire.post_sha256;
    receipt.storage_format = wire.storage_format;
    callback_result = callback(callback_opaque, &receipt);
    if(callback_result == CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED; goto done; }
    if(callback_result == CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE; goto done; }
    if(callback_result == CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE ||
       callback_result != CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED) { result = CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE; goto done; }
    status = character_save_journal_v2_writer_validate_held_for_route(writer, &tuple, &after_generation);
    if(status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE;
        goto done;
    }
    if(after_generation != held_generation || !ack_tuple_matches(&tuple, &wire) ||
       ack_live_post(&tree, &wire, name)) {
        result = CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE;
        goto done;
    }
    if(local_repair_incomplete) {
        result = CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE;
        goto done;
    }
    result = ack_mark(&tree, &wire) ?
        CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE :
        CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED;
done:
    if(root_fd >= 0) close(root_fd);
    ack_tree_close(&tree);
    memset(&wire, 0, sizeof(wire));
    memset(&tuple, 0, sizeof(tuple));
    memset(&receipt, 0, sizeof(receipt));
    memset(name, 0, sizeof(name));
    return result;
}

#ifdef CHARACTER_SAVE_JOURNAL_V2_ACK_TESTING
void character_save_journal_v2_ack_set_trusted_uid_for_test(uid)
uid_t uid;
{
    v2_ack_trusted_uid = uid;
}
void character_save_journal_v2_ack_fail_after_temp_for_test(enabled)
int enabled;
{
    v2_ack_fail_after_temp = enabled != 0;
}
void character_save_journal_v2_ack_fail_marker_fsync_for_test(enabled)
int enabled;
{
    v2_ack_marker_file_fsync = enabled != 0;
}
void character_save_journal_v2_ack_faults_for_test(read_close_once, live_close_once,
    marker_close_once, post_link_once, first_parent_fsync_once, temp_unlink_once,
    final_parent_fsync_once)
int read_close_once, live_close_once, marker_close_once;
int post_link_once, first_parent_fsync_once, temp_unlink_once, final_parent_fsync_once;
{
    v2_ack_read_close = read_close_once != 0;
    v2_ack_live_close = live_close_once != 0;
    v2_ack_marker_close = marker_close_once != 0;
    v2_ack_post_link = post_link_once != 0;
    v2_ack_first_parent_fsync = first_parent_fsync_once != 0;
    v2_ack_temp_unlink = temp_unlink_once != 0;
    v2_ack_final_parent_fsync = final_parent_fsync_once != 0;
}
#endif
