#include "character_save_journal_v2_publish.h"

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
#include <signal.h>
#endif

#ifndef O_NOFOLLOW
#error "v2 publish requires O_NOFOLLOW"
#endif

#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
static void pub_crash_after(event)
character_save_journal_v2_crash_cutpoint event;
{
    const char *text = getenv("M3_V2_CRASH_CUTPOINT");
    char *end;
    unsigned long selected;
    if(!text || !*text) return;
    selected = strtoul(text, &end, 10);
    if(*end || selected != (unsigned long)event) return;
    (void)kill(getpid(), SIGKILL);
    _exit(127);
}
#else
#define pub_crash_after(event) ((void)0)
#endif
#ifndef O_DIRECTORY
#error "v2 publish requires O_DIRECTORY"
#endif

#define V2_PUBLISH_RUNTIME_UID 10001
#define V2_PUBLISH_TEXT_MAX 1400

typedef struct v2_publish_tree {
    int root_fd, player_fd, shard_fd, journal_fd, stage_fd;
} v2_publish_tree;

static uid_t v2_publish_trusted_uid = V2_PUBLISH_RUNTIME_UID;
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
static int v2_publish_write_eintr, v2_publish_write_short;
static int v2_publish_write_zero, v2_publish_write_eio;
static int v2_publish_file_fsync, v2_publish_live_parent_fsync;
static int v2_publish_stage_parent_fsync;
static int v2_publish_live_promotion, v2_publish_journal_promotion;
static int v2_publish_journal_parent_fsync;
static int v2_publish_stage_unlink, v2_publish_journal_temp_unlink;
static int v2_publish_read_close, v2_publish_stage_close;
static int v2_publish_live_close, v2_publish_journal_write_close;
static int v2_publish_crash_after_live_promotion;
static int v2_publish_link_ready_fd = -1, v2_publish_link_release_fd = -1;
static int v2_publish_link_pause_kind;
static unsigned int v2_publish_cleanup_close_failures;
#endif

static size_t pub_bounded(text, limit)
const char *text;
size_t limit;
{
    size_t i;
    if(!text) return limit + 1;
    for(i = 0; i <= limit; i++) if(!text[i]) return i;
    return limit + 1;
}

static int pub_lower_hex(text, length)
const char *text;
size_t length;
{
    size_t i;
    if(!text || pub_bounded(text, length) != length) return 0;
    for(i = 0; i < length; i++)
        if(!((text[i] >= '0' && text[i] <= '9') ||
             (text[i] >= 'a' && text[i] <= 'f'))) return 0;
    return 1;
}

static int pub_uuid(text)
const char *text;
{
    size_t i;
    if(!text || pub_bounded(text, CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) !=
       CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) return 0;
    for(i = 0; i < CHARACTER_SAVE_JOURNAL_V2_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(text[i] != '-') return 0;
        } else if(!((text[i] >= '0' && text[i] <= '9') ||
                    (text[i] >= 'a' && text[i] <= 'f'))) return 0;
    }
    return 1;
}

static int pub_dir_ok(fd)
int fd;
{
    struct stat st;
    return fd >= 0 && fstat(fd, &st) == 0 && S_ISDIR(st.st_mode) &&
           st.st_uid == v2_publish_trusted_uid && (st.st_mode & 07777) == 0700;
}

static int pub_file_ok(fd)
int fd;
{
    struct stat st;
    return fd >= 0 && fstat(fd, &st) == 0 && S_ISREG(st.st_mode) &&
           st.st_uid == v2_publish_trusted_uid && (st.st_mode & 07777) == 0600 &&
           st.st_nlink == 1;
}

static int pub_open_component(parent, name)
int parent;
const char *name;
{
    struct stat before, after;
    int fd;
    if(fstatat(parent, name, &before, AT_SYMLINK_NOFOLLOW) != 0 ||
       !S_ISDIR(before.st_mode) || before.st_uid != v2_publish_trusted_uid ||
       (before.st_mode & 07777) != 0700) return -1;
    fd = openat(parent, name, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    if(!pub_dir_ok(fd) || fstat(fd, &after) != 0 ||
       before.st_dev != after.st_dev || before.st_ino != after.st_ino) {
        if(fd >= 0) close(fd);
        return -1;
    }
    return fd;
}

/* Cleanup owns every descriptor exactly once.  The test build records an
 * unexpected cleanup close failure so ownership transfers stay observable. */
static void pub_cleanup_close(fd)
int fd;
{
    if(fd >= 0 && close(fd) != 0) {
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
        v2_publish_cleanup_close_failures++;
#endif
    }
}

static void pub_tree_close(tree)
v2_publish_tree *tree;
{
    if(!tree) return;
    pub_cleanup_close(tree->stage_fd);
    pub_cleanup_close(tree->journal_fd);
    pub_cleanup_close(tree->shard_fd);
    pub_cleanup_close(tree->player_fd);
    pub_cleanup_close(tree->root_fd);
    memset(tree, 0, sizeof(*tree));
    tree->root_fd = tree->player_fd = tree->shard_fd = tree->journal_fd =
        tree->stage_fd = -1;
}

static int pub_tree_open(root_fd, shard, tree)
int root_fd;
const char *shard;
v2_publish_tree *tree;
{
    memset(tree, 0, sizeof(*tree));
    tree->root_fd = tree->player_fd = tree->shard_fd = tree->journal_fd =
        tree->stage_fd = -1;
    tree->root_fd = root_fd;
    if(!pub_dir_ok(tree->root_fd)) goto bad;
    tree->player_fd = pub_open_component(tree->root_fd, "player");
    if(tree->player_fd < 0) goto bad;
    tree->shard_fd = pub_open_component(tree->player_fd, shard);
    if(tree->shard_fd < 0) goto bad;
    tree->journal_fd = pub_open_component(tree->root_fd, "character-save-journal");
    if(tree->journal_fd < 0) goto bad;
    tree->stage_fd = pub_open_component(tree->root_fd, "character-save-stage");
    if(tree->stage_fd < 0) goto bad;
    return 0;
bad:
    pub_tree_close(tree);
    return -1;
}

static int pub_journal_open(root_fd, tree)
int root_fd;
v2_publish_tree *tree;
{
    memset(tree, 0, sizeof(*tree));
    tree->root_fd = tree->player_fd = tree->shard_fd = tree->journal_fd =
        tree->stage_fd = -1;
    tree->root_fd = root_fd;
    if(!pub_dir_ok(tree->root_fd)) goto bad;
    tree->journal_fd = pub_open_component(tree->root_fd, "character-save-journal");
    if(tree->journal_fd < 0) goto bad;
    return 0;
bad:
    pub_tree_close(tree);
    return -1;
}

static ssize_t pub_write_operation(fd, bytes, length)
int fd;
const void *bytes;
size_t length;
{
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    if(v2_publish_write_eintr) {
        v2_publish_write_eintr = 0;
        errno = EINTR;
        return -1;
    }
    if(v2_publish_write_zero) {
        v2_publish_write_zero = 0;
        return 0;
    }
    if(v2_publish_write_eio) {
        v2_publish_write_eio = 0;
        errno = EIO;
        return -1;
    }
    if(v2_publish_write_short && length > 1) {
        v2_publish_write_short = 0;
        return write(fd, bytes, length / 2);
    }
#endif
    return write(fd, bytes, length);
}

static int pub_write_all(fd, bytes, length)
int fd;
const void *bytes;
size_t length;
{
    const char *cursor = (const char *)bytes;
    ssize_t n;
    while(length) {
        n = pub_write_operation(fd, cursor, length);
        if(n < 0 && errno == EINTR) continue;
        if(n <= 0) return -1;
        cursor += n;
        length -= (size_t)n;
    }
    return 0;
}

static int pub_close(fd, kind)
int fd;
int kind;
{
    int result = close(fd);
    (void)kind;
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    if((kind == 1 && v2_publish_read_close) ||
       (kind == 2 && v2_publish_stage_close) ||
       (kind == 3 && v2_publish_live_close) ||
       (kind == 4 && v2_publish_journal_write_close)) {
        if(kind == 1) v2_publish_read_close = 0;
        else if(kind == 2) v2_publish_stage_close = 0;
        else if(kind == 3) v2_publish_live_close = 0;
        else v2_publish_journal_write_close = 0;
        errno = EIO;
        return -1;
    }
#endif
    return result;
}

static int pub_sync(fd, kind)
int fd;
int kind;
{
    int result;
    (void)kind;
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    if((kind == 1 && v2_publish_file_fsync) ||
       (kind == 2 && v2_publish_live_parent_fsync) ||
       (kind == 3 && v2_publish_journal_parent_fsync) ||
       (kind == 4 && v2_publish_stage_parent_fsync)) {
        if(kind == 1) v2_publish_file_fsync = 0;
        else if(kind == 2) v2_publish_live_parent_fsync = 0;
        else if(kind == 3) v2_publish_journal_parent_fsync = 0;
        else v2_publish_stage_parent_fsync = 0;
        errno = EIO;
        return -1;
    }
#endif
    do result = fsync(fd); while(result < 0 && errno == EINTR);
    return result;
}

static int pub_unlink(parent, leaf, kind)
int parent;
const char *leaf;
int kind;
{
#ifndef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    (void)kind;
#endif
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    if((kind == 1 && v2_publish_stage_unlink) ||
       (kind == 2 && v2_publish_journal_temp_unlink)) {
        if(kind == 1) v2_publish_stage_unlink = 0;
        else v2_publish_journal_temp_unlink = 0;
        errno = EIO;
        return -1;
    }
#endif
    return unlinkat(parent, leaf, 0);
}

static int pub_rename(from_parent, from, to_parent, to)
int from_parent;
const char *from;
int to_parent;
const char *to;
{
    return renameat(from_parent, from, to_parent, to);
}

static int pub_link(from_parent, from, to_parent, to, kind)
int from_parent;
const char *from;
int to_parent;
const char *to;
int kind;
{
#ifndef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    (void)kind;
#endif
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    if((kind == 1 && v2_publish_live_promotion) ||
       (kind == 2 && v2_publish_journal_promotion)) {
        if(kind == 1) v2_publish_live_promotion = 0;
        else v2_publish_journal_promotion = 0;
        errno = EIO;
        return -1;
    }
    if(kind == v2_publish_link_pause_kind && v2_publish_link_ready_fd >= 0) {
        char signal;
        ssize_t count;
        do count = write(v2_publish_link_ready_fd, "x", 1);
        while(count < 0 && errno == EINTR);
        if(count != 1) return -1;
        do count = read(v2_publish_link_release_fd, &signal, 1);
        while(count < 0 && errno == EINTR);
        v2_publish_link_ready_fd = v2_publish_link_release_fd = -1;
        v2_publish_link_pause_kind = 0;
        if(count != 1) return -1;
    }
#endif
    return linkat(from_parent, from, to_parent, to, 0);
}

static int pub_u64(text, out)
const char *text;
uint64_t *out;
{
    uint64_t value = 0, digit;
    size_t i, n = pub_bounded(text, 19);
    if(!out || !n || n > 19 || (n > 1 && text[0] == '0')) return -1;
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

static int pub_copy(destination, destination_size, source)
char *destination;
size_t destination_size;
const char *source;
{
    size_t n;
    if(!destination || !destination_size || !source) return -1;
    n = pub_bounded(source, destination_size - 1);
    if(n >= destination_size) return -1;
    memcpy(destination, source, n + 1);
    return 0;
}

static int pub_parse(text, wire, published)
char *text;
character_save_journal_v2_wire *wire;
int *published;
{
    static const char *keys[] = {
        "version=", "state=", "writer_instance_id=", "character_id=",
        "request_sha256=", "world_id=", "legacy_name_key_hex=",
        "legacy_shard=", "command_uuid=", "writer_epoch=", "writer_revision=",
        "expected_state=", "expected_sha256=", "post_sha256=",
        "storage_format=", "staged_leaf="
    };
    character_save_journal_v2_wire parsed;
    char *line, *next, *values[16], request[65], leaf[44];
    uint64_t format;
    int i, is_published;
    if(!text || !wire || !published) return -1;
    memset(&parsed, 0, sizeof(parsed));
    line = text;
    for(i = 0; i < 16; i++) {
        next = strchr(line, '\n');
        if(!next || strncmp(line, keys[i], strlen(keys[i])) != 0) goto bad;
        *next = 0;
        values[i] = line + strlen(keys[i]);
        line = next + 1;
    }
    if(*line || strcmp(values[0], "2")) goto bad;
    if(!strcmp(values[1], "PREPARED")) is_published = 0;
    else if(!strcmp(values[1], "LEGACY_PUBLISHED")) is_published = 1;
    else goto bad;
    parsed.state = CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    if(pub_copy(parsed.writer_instance_id, sizeof(parsed.writer_instance_id), values[2]) ||
       pub_copy(parsed.character_id, sizeof(parsed.character_id), values[3]) ||
       pub_copy(parsed.request_sha256, sizeof(parsed.request_sha256), values[4]) ||
       pub_copy(parsed.world_id, sizeof(parsed.world_id), values[5]) ||
       pub_copy(parsed.legacy_name_key_hex, sizeof(parsed.legacy_name_key_hex), values[6]) ||
       pub_copy(parsed.legacy_shard, sizeof(parsed.legacy_shard), values[7]) ||
       pub_copy(parsed.command_uuid, sizeof(parsed.command_uuid), values[8]) ||
       pub_u64(values[9], &parsed.writer_epoch) ||
       pub_u64(values[10], &parsed.writer_revision)) goto bad;
    if(!strcmp(values[11], "existing")) {
        parsed.expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING;
        if(!pub_lower_hex(values[12], 64) ||
           pub_copy(parsed.expected_sha256, sizeof(parsed.expected_sha256), values[12]))
            goto bad;
    } else if(!strcmp(values[11], "absent")) {
        parsed.expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
        if(strcmp(values[12], "-")) goto bad;
    } else goto bad;
    if(pub_copy(parsed.post_sha256, sizeof(parsed.post_sha256), values[13]) ||
       pub_u64(values[14], &format) || format > 32767 ||
       (parsed.storage_format = (uint16_t)format) == 0 ||
       character_save_journal_v2_stage_leaf(parsed.command_uuid, leaf, sizeof(leaf)) ||
       strcmp(leaf, values[15]) ||
       character_save_journal_v2_request_sha256(&parsed, request) ||
       strcmp(request, parsed.request_sha256)) goto bad;
    *wire = parsed;
    *published = is_published;
    memset(&parsed, 0, sizeof(parsed));
    return 0;
bad:
    memset(&parsed, 0, sizeof(parsed));
    memset(wire, 0, sizeof(*wire));
    *published = 0;
    return -1;
}

static int pub_format(wire, state, out, out_size)
const character_save_journal_v2_wire *wire;
const char *state;
char *out;
size_t out_size;
{
    char leaf[44];
    int n;
    if(!wire || !state || character_save_journal_v2_stage_leaf(
       wire->command_uuid, leaf, sizeof(leaf))) return -1;
    n = snprintf(out, out_size,
        "version=2\nstate=%s\nwriter_instance_id=%s\ncharacter_id=%s\n"
        "request_sha256=%s\nworld_id=%s\nlegacy_name_key_hex=%s\n"
        "legacy_shard=%s\ncommand_uuid=%s\nwriter_epoch=%" PRIu64
        "\nwriter_revision=%" PRIu64 "\nexpected_state=%s\n"
        "expected_sha256=%s\npost_sha256=%s\nstorage_format=%u\n"
        "staged_leaf=%s\n",
        state, wire->writer_instance_id, wire->character_id, wire->request_sha256,
        wire->world_id, wire->legacy_name_key_hex, wire->legacy_shard,
        wire->command_uuid, wire->writer_epoch, wire->writer_revision,
        wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ?
        "existing" : "absent",
        wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ?
        wire->expected_sha256 : "-", wire->post_sha256,
        (unsigned int)wire->storage_format, leaf);
    return n < 0 || (size_t)n >= out_size ? -1 : 0;
}

static int pub_read_fd_text(fd, out, out_size)
int fd;
char *out;
size_t out_size;
{
    size_t total = 0;
    ssize_t n;
    char extra;
    if(fd < 0 || !out || out_size < 2) return -1;
    out[0] = 0;
    while(total < out_size - 1) {
        n = read(fd, out + total, out_size - 1 - total);
        if(n < 0 && errno == EINTR) continue;
        if(n < 0) return -1;
        if(!n) break;
        total += (size_t)n;
    }
    if(total == out_size - 1) {
        do n = read(fd, &extra, 1); while(n < 0 && errno == EINTR);
        if(n != 0) return -1;
    }
    if(memchr(out, 0, total)) return -1;
    out[total] = 0;
    return 0;
}

static int pub_read_text_links(parent, leaf, out, out_size, links, close_kind)
int parent;
const char *leaf;
char *out;
size_t out_size;
nlink_t links;
int close_kind;
{
    int fd, result = -1;
    struct stat st;
    if(!out || out_size < 2) return -1;
    out[0] = 0;
    fd = openat(parent, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(fd < 0 || fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) ||
       st.st_uid != v2_publish_trusted_uid || (st.st_mode & 07777) != 0600 ||
       st.st_nlink < 1 || st.st_nlink > links || pub_read_fd_text(fd, out, out_size))
        goto done;
    if(pub_close(fd, close_kind) != 0) {
        fd = -1;
        goto done;
    }
    fd = -1;
    result = 0;
done:
    if(fd >= 0) close(fd);
    if(result) memset(out, 0, out_size);
    return result;
}

static int pub_read_text(parent, leaf, out, out_size)
int parent;
const char *leaf;
char *out;
size_t out_size;
{
    return pub_read_text_links(parent, leaf, out, out_size, 1, 1);
}

/* Every reusable marker must be the exact canonical representation of this
 * PREPARED wire.  A partial write or a competing command's evidence is never
 * removed or overwritten. */
static int pub_marker_exact(tree, leaf, wire, links, sync_file)
v2_publish_tree *tree;
const char *leaf;
const character_save_journal_v2_wire *wire;
nlink_t links;
int sync_file;
{
    char text[V2_PUBLISH_TEXT_MAX], expected[V2_PUBLISH_TEXT_MAX];
    character_save_journal_v2_wire parsed;
    int state, fd = -1, result = -1;
    struct stat st;
    if(!tree || !leaf || !wire || pub_format(wire, "LEGACY_PUBLISHED", expected,
                                               sizeof(expected))) goto done;
    if(!sync_file) {
        if(pub_read_text_links(tree->journal_fd, leaf, text, sizeof(text), links, 1) ||
           strcmp(text, expected) || pub_parse(text, &parsed, &state) || !state ||
           memcmp(&parsed, wire, sizeof(parsed))) goto done;
        result = 0;
        goto done;
    }
    fd = openat(tree->journal_fd, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(fd < 0 || fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) ||
       st.st_uid != v2_publish_trusted_uid || (st.st_mode & 07777) != 0600 ||
       st.st_nlink < 1 || st.st_nlink > links) goto done;
    /* Read through the stable descriptor, then force exact temp evidence to
     * durable storage before it can be promoted on a retry. */
    if(pub_read_fd_text(fd, text, sizeof(text)) ||
       strcmp(text, expected) || pub_parse(text, &parsed, &state) || !state ||
       memcmp(&parsed, wire, sizeof(parsed)) || pub_sync(fd, 1)) goto done;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC);
    if(pub_close(fd, 4) != 0) {
        fd = -1;
        goto done;
    }
    fd = -1;
    result = 0;
done:
    if(fd >= 0) close(fd);
    memset(text, 0, sizeof(text));
    memset(expected, 0, sizeof(expected));
    memset(&parsed, 0, sizeof(parsed));
    return result;
}

/* linkat creates a transient two-name state.  It is safe to finish only when
 * both names prove to be the exact marker for this command.  A two-link
 * target is valid only as the exact target-plus-temp transactional pair. */
static int pub_reconcile_marker_temp(tree, target, temporary, wire)
v2_publish_tree *tree;
const char *target;
const char *temporary;
const character_save_journal_v2_wire *wire;
{
    struct stat target_st, temporary_st;
    if(fstatat(tree->journal_fd, temporary, &temporary_st, AT_SYMLINK_NOFOLLOW) != 0) {
        return errno == ENOENT ? 0 : -1;
    }
    if(fstatat(tree->journal_fd, target, &target_st, AT_SYMLINK_NOFOLLOW) != 0 ||
       !S_ISREG(target_st.st_mode) || !S_ISREG(temporary_st.st_mode) ||
       target_st.st_uid != v2_publish_trusted_uid ||
       temporary_st.st_uid != v2_publish_trusted_uid ||
       (target_st.st_mode & 07777) != 0600 ||
       (temporary_st.st_mode & 07777) != 0600 || target_st.st_nlink < 1 ||
       target_st.st_nlink > 2 || temporary_st.st_nlink < 1 ||
       temporary_st.st_nlink > 2) return -1;
    /* A two-link name is only our transient linkat pair when both names
     * prove that they name the same inode.  Never collapse an extra alias. */
    if((target_st.st_nlink == 2 || temporary_st.st_nlink == 2) &&
       (target_st.st_nlink != 2 || temporary_st.st_nlink != 2 ||
        target_st.st_dev != temporary_st.st_dev ||
        target_st.st_ino != temporary_st.st_ino)) return -1;
    if(pub_marker_exact(tree, target, wire, 2, 0) ||
       pub_marker_exact(tree, temporary, wire, 2, 1)) return -1;
    /* Persist target creation before removing the only other name. */
    if(pub_sync(tree->journal_fd, 3)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC);
    if(pub_unlink(tree->journal_fd, temporary, 2)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT);
    if(pub_sync(tree->journal_fd, 3)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC);
    return 0;
}

/* This phase reads only the immutable PREPARED record.  Route-free recovery
 * uses it before it considers any marker reconciliation, so a mismatched
 * held writer tuple cannot trigger a local marker mutation. */
static int pub_read_prepared(tree, command_uuid, wire)
v2_publish_tree *tree;
const char *command_uuid;
character_save_journal_v2_wire *wire;
{
    char leaf[64], text[V2_PUBLISH_TEXT_MAX];
    int n, result, prepared_state;
    if(!tree || !command_uuid || !wire) return -1;
    n = snprintf(leaf, sizeof(leaf), "%s.prepared", command_uuid);
    if(n < 0 || (size_t)n >= sizeof(leaf)) return -1;
    result = pub_read_text(tree->journal_fd, leaf, text, sizeof(text));
    if(!result) result = pub_parse(text, wire, &prepared_state) || prepared_state ||
                          strcmp(wire->command_uuid, command_uuid);
    memset(text, 0, sizeof(text));
    return result ? -1 : 0;
}

static int pub_marker_state(tree, wire, published)
v2_publish_tree *tree;
const character_save_journal_v2_wire *wire;
int *published;
{
    char published_leaf[64], temporary[64];
    struct stat st;
    int n;
    if(!tree || !wire || !published) return -1;
    *published = 0;
    n = snprintf(published_leaf, sizeof(published_leaf), "%s.published", wire->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(published_leaf)) return -1;
    n = snprintf(temporary, sizeof(temporary), "%s.published.tmp", wire->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(temporary)) return -1;
    if(fstatat(tree->journal_fd, published_leaf, &st, AT_SYMLINK_NOFOLLOW) != 0) {
        if(errno != ENOENT) return -1;
        if(fstatat(tree->journal_fd, temporary, &st, AT_SYMLINK_NOFOLLOW) == 0)
            return pub_marker_exact(tree, temporary, wire, 1, 0) ? -1 : 0;
        return errno == ENOENT ? 0 : -1;
    }
    if(fstatat(tree->journal_fd, temporary, &st, AT_SYMLINK_NOFOLLOW) == 0) {
        if(pub_reconcile_marker_temp(tree, published_leaf, temporary, wire)) return -1;
    } else if(errno != ENOENT || pub_marker_exact(tree, published_leaf, wire, 1, 0)) {
        return -1;
    }
    *published = 1;
    return 0;
}

static int pub_read_record(tree, command_uuid, wire, published)
v2_publish_tree *tree;
const char *command_uuid;
character_save_journal_v2_wire *wire;
int *published;
{
    return pub_read_prepared(tree, command_uuid, wire) ||
           pub_marker_state(tree, wire, published) ? -1 : 0;
}

static int pub_hash_leaf(parent, leaf, digest, missing, close_kind)
int parent;
const char *leaf;
char digest[65];
int *missing;
int close_kind;
{
    int fd, result = -1;
    if(missing) *missing = 0;
    fd = openat(parent, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(fd < 0 && errno == ENOENT) {
        if(missing) *missing = 1;
        return 0;
    }
    if(!pub_file_ok(fd) || character_save_journal_v2_hash_fd(fd, digest) != 0)
        goto done;
    if(pub_close(fd, close_kind) != 0) {
        fd = -1;
        goto done;
    }
    fd = -1;
    result = 0;
done:
    if(fd >= 0) close(fd);
    if(result) memset(digest, 0, 65);
    return result;
}

/* The absent-only link promotion has exactly two trusted names until stage
 * removal.  Hash it before any fsync or unlink cleanup, and let the v2 hash
 * helper prove the descriptor kept its inode, size, and exact link count. */
static int pub_hash_two_link_leaf(parent, leaf, digest, close_kind)
int parent;
const char *leaf;
char digest[65];
int close_kind;
{
    int fd, result = -1;
    fd = openat(parent, leaf, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(fd < 0 || character_save_journal_v2_hash_fd_two_links(fd, digest) != 0)
        goto done;
    if(pub_close(fd, close_kind) != 0) {
        fd = -1;
        goto done;
    }
    fd = -1;
    result = 0;
done:
    if(fd >= 0) close(fd);
    if(result) memset(digest, 0, 65);
    return result;
}

/* Recover only the two-name state produced by our absent-only link promotion.
 * The matching protected inode is the identity previously hash-checked before
 * linkat; no unrelated pair of leaves is ever collapsed. */
static int pub_reconcile_absent_link(tree, stage_leaf, live_leaf, wire)
v2_publish_tree *tree;
const char *stage_leaf;
const char *live_leaf;
const character_save_journal_v2_wire *wire;
{
    struct stat stage_st, live_st;
    char digest[65];
    int missing;
    if(fstatat(tree->stage_fd, stage_leaf, &stage_st, AT_SYMLINK_NOFOLLOW) != 0 ||
       fstatat(tree->shard_fd, live_leaf, &live_st, AT_SYMLINK_NOFOLLOW) != 0)
        return 0;
    if(!S_ISREG(stage_st.st_mode) || !S_ISREG(live_st.st_mode) ||
       stage_st.st_uid != v2_publish_trusted_uid || live_st.st_uid != v2_publish_trusted_uid ||
       (stage_st.st_mode & 07777) != 0600 || (live_st.st_mode & 07777) != 0600 ||
       stage_st.st_nlink != 2 || live_st.st_nlink != 2 ||
       stage_st.st_dev != live_st.st_dev || stage_st.st_ino != live_st.st_ino)
        return 0;
    /* Reject corrupt shared evidence before its names can be made less
     * inspectable.  The later strict one-link hash remains defense in depth. */
    if(pub_hash_two_link_leaf(tree->stage_fd, stage_leaf, digest, 2) ||
       strcmp(digest, wire->post_sha256)) {
        memset(digest, 0, sizeof(digest));
        return -1;
    }
    /* The live name must be durable before its stage name can be removed. */
    if(pub_sync(tree->shard_fd, 2)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LIVE_PARENT_FSYNC);
    if(pub_unlink(tree->stage_fd, stage_leaf, 1)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT);
    if(pub_sync(tree->stage_fd, 4)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_PARENT_FSYNC);
    if(pub_hash_leaf(tree->shard_fd, live_leaf, digest, &missing, 3) || missing ||
       strcmp(digest, wire->post_sha256)) {
        memset(digest, 0, sizeof(digest));
        return -1;
    }
    memset(digest, 0, sizeof(digest));
    return 1;
}

static int pub_promote_absent(tree, stage_leaf, live_leaf)
v2_publish_tree *tree;
const char *stage_leaf;
const char *live_leaf;
{
    /* linkat fails EEXIST, so a leaf created after the precondition read is
     * never replaced.  The ordered directory fsyncs make the two-name state
     * recoverable across a crash. */
    if(pub_link(tree->stage_fd, stage_leaf, tree->shard_fd, live_leaf, 1)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LINKAT);
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    /* linkat and unlink are not an atomic move: expose the recoverable pair. */
    if(v2_publish_crash_after_live_promotion) _exit(91);
#endif
    if(pub_sync(tree->shard_fd, 2)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_LIVE_PARENT_FSYNC);
    if(pub_unlink(tree->stage_fd, stage_leaf, 1)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_UNLINKAT);
    if(pub_sync(tree->stage_fd, 4)) return -1;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_ABSENT_STAGE_PARENT_FSYNC);
    return 0;
}

static int pub_decode_name(wire, out, length)
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
        high = (unsigned char)(wire->legacy_name_key_hex[i] <= '9' ?
            wire->legacy_name_key_hex[i] - '0' : wire->legacy_name_key_hex[i] - 'a' + 10);
        low = (unsigned char)(wire->legacy_name_key_hex[i + 1] <= '9' ?
            wire->legacy_name_key_hex[i + 1] - '0' : wire->legacy_name_key_hex[i + 1] - 'a' + 10);
        out[i / 2] = (unsigned char)((high << 4) | low);
    }
    out[n / 2] = 0;
    *length = n / 2;
    return 0;
}

static int pub_route_matches(route, tuple, wire)
const character_save_journal_v2_bound_route *route;
const character_save_journal_v2_writer_tuple *tuple;
const character_save_journal_v2_wire *wire;
{
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    size_t length, i;
    if(!route || !tuple || !wire || pub_decode_name(wire, name, &length) ||
       strcmp(tuple->world_id, wire->world_id) ||
       strcmp(tuple->writer_instance_id, wire->writer_instance_id) ||
       tuple->writer_epoch != wire->writer_epoch ||
       strcmp(route->world_id, wire->world_id) ||
       strcmp(route->character_id, wire->character_id) ||
       route->legacy_name_length != length || memcmp(route->legacy_name, name, length) ||
       strcmp(route->legacy_shard, wire->legacy_shard) ||
       route->storage_format != wire->storage_format ||
       (route->lifecycle != CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED &&
        route->lifecycle != CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING &&
        route->lifecycle != CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE)) return 0;
    for(i = length; i < sizeof(route->legacy_name); i++)
        if(route->legacy_name[i]) return 0;
    return 1;
}

static int pub_mark_published(tree, wire)
v2_publish_tree *tree;
const character_save_journal_v2_wire *wire;
{
    char target[64], temporary[64], text[V2_PUBLISH_TEXT_MAX];
    struct stat st;
    int fd = -1, n, result = -1, temporary_exists = 0;
    n = snprintf(target, sizeof(target), "%s.published", wire->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(target)) goto done;
    n = snprintf(temporary, sizeof(temporary), "%s.published.tmp", wire->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(temporary) ||
       pub_format(wire, "LEGACY_PUBLISHED", text, sizeof(text))) goto done;
    if(fstatat(tree->journal_fd, temporary, &st, AT_SYMLINK_NOFOLLOW) == 0)
        temporary_exists = 1;
    else if(errno != ENOENT) goto done;
    if(temporary_exists) {
        /* Reuse only exact, fsynced, single-name retry evidence. */
        if(pub_marker_exact(tree, temporary, wire, 1, 1)) goto done;
    } else {
        fd = openat(tree->journal_fd, temporary,
                    O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC, 0600);
        if(!pub_file_ok(fd) || pub_write_all(fd, text, strlen(text)) || pub_sync(fd, 1))
            goto done;
        pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_FILE_FSYNC);
        if(pub_close(fd, 4) != 0) {
            fd = -1;
            goto done;
        }
        fd = -1;
    }
    /* linkat is the portable no-replace promotion primitive.  EEXIST leaves
     * both command evidence and a competing target untouched for inspection. */
    if(pub_link(tree->journal_fd, temporary, tree->journal_fd, target, 2)) goto done;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_MARKER_LINKAT);
    if(pub_sync(tree->journal_fd, 3)) goto done;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FIRST_JOURNAL_FSYNC);
    if(pub_unlink(tree->journal_fd, temporary, 2)) goto done;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_TEMP_UNLINKAT);
    if(pub_sync(tree->journal_fd, 3)) goto done;
    pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PUBLISHED_FINAL_JOURNAL_FSYNC);
    result = 0;
done:
    if(fd >= 0) close(fd);
    memset(text, 0, sizeof(text));
    return result;
}

static character_save_journal_v2_publish_result pub_context_result(status)
character_save_journal_v2_writer_context_status status;
{
    if(status == CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE)
        return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_STALE;
    if(status == CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK)
        return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_LOCK;
    return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_CONTEXT_INVALID;
}

/* Both initial publish and route-free recovery arrive here only after their
 * distinct authority checks have completed.  From this point forward every
 * file name and byte is derived from the immutable PREPARED journal, never
 * from a route response or recovery caller input. */
static character_save_journal_v2_publish_result pub_local_publish(tree, wire,
    name, stage_leaf, published)
v2_publish_tree *tree;
const character_save_journal_v2_wire *wire;
const unsigned char *name;
const char *stage_leaf;
int published;
{
    char live_hash[65], stage_hash[65];
    int stage_missing, live_missing;
    character_save_journal_v2_publish_result result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
    if(!tree || !wire || !name || !stage_leaf) goto done;
    if(published) {
        if(pub_hash_leaf(tree->stage_fd, stage_leaf, stage_hash, &stage_missing, 2) ||
           !stage_missing || pub_hash_leaf(tree->shard_fd, (const char *)name,
                                           live_hash, &live_missing, 3) ||
           live_missing || strcmp(live_hash, wire->post_sha256))
            result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
        else result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK;
        goto done;
    }
    if(wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT) {
        int recovered_link = pub_reconcile_absent_link(tree, stage_leaf,
                                                        (const char *)name, wire);
        if(recovered_link < 0) {
            result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
            goto done;
        }
        if(recovered_link > 0) {
            result = pub_mark_published(tree, wire) ?
                CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO : CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK;
            goto done;
        }
    }
    if(pub_hash_leaf(tree->stage_fd, stage_leaf, stage_hash, &stage_missing, 2)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_STAGE;
        goto done;
    }
    if(stage_missing) {
        if(pub_hash_leaf(tree->shard_fd, (const char *)name, live_hash, &live_missing, 3) ||
           live_missing || strcmp(live_hash, wire->post_sha256)) {
            result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
            goto done;
        }
        if(pub_sync(tree->shard_fd, 2)) {
            result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
            goto done;
        }
        pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_LIVE_PARENT_FSYNC);
        if(pub_sync(tree->stage_fd, 4)) {
            result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
            goto done;
        }
        pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_RECOVERY_STAGE_PARENT_FSYNC);
        result = pub_mark_published(tree, wire) ?
            CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE : CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK;
        goto done;
    }
    if(strcmp(stage_hash, wire->post_sha256)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_STAGE;
        goto done;
    }
    if(pub_hash_leaf(tree->shard_fd, (const char *)name, live_hash, &live_missing, 3)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
        goto done;
    }
    if((wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT && !live_missing) ||
       (wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING &&
        (live_missing || strcmp(live_hash, wire->expected_sha256)))) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
        goto done;
    }
    if((wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT &&
        pub_promote_absent(tree, stage_leaf, (const char *)name)) ||
       (wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING &&
        pub_rename(tree->stage_fd, stage_leaf, tree->shard_fd, (const char *)name))) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
        goto done;
    }
    if(wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING)
        pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_RENAMEAT);
#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
    /* Existing-state replacement retains its post-rename, pre-destination
     * fsync crash boundary. */
    if(wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING &&
       v2_publish_crash_after_live_promotion) _exit(91);
#endif
    if(pub_sync(tree->shard_fd, 2)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
        goto done;
    }
    if(wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING)
        pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_LIVE_PARENT_FSYNC);
    if(pub_sync(tree->stage_fd, 4)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
        goto done;
    }
    if(wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING)
        pub_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_EXISTING_STAGE_PARENT_FSYNC);
    if(pub_hash_leaf(tree->shard_fd, (const char *)name, live_hash, &live_missing, 3) ||
       live_missing || strcmp(live_hash, wire->post_sha256)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE;
        goto done;
    }
    result = pub_mark_published(tree, wire) ?
        CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO : CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK;
done:
    memset(live_hash, 0, sizeof(live_hash));
    memset(stage_hash, 0, sizeof(stage_hash));
    return result;
}

character_save_journal_v2_publish_result
character_save_journal_v2_publish_recover(writer, command_uuid)
const character_save_journal_v2_writer_context *writer;
const char *command_uuid;
{
    v2_publish_tree tree;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire, reread_wire;
    character_save_journal_v2_writer_context_status context_status;
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    char stage_leaf[44];
    size_t name_length;
    int published, root_fd = -1;
    character_save_journal_v2_publish_result result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
    memset(&tree, 0, sizeof(tree));
    tree.root_fd = tree.player_fd = tree.shard_fd = tree.journal_fd = tree.stage_fd = -1;
    memset(&tuple, 0, sizeof(tuple));
    memset(&wire, 0, sizeof(wire));
    memset(&reread_wire, 0, sizeof(reread_wire));
    memset(name, 0, sizeof(name));
    if(!writer || !pub_uuid(command_uuid))
        return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_INVALID_ARGUMENT;
    context_status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK)
        return pub_context_result(context_status);
    /* Read immutable journal identity through the held root before selecting
     * its journal-owned shard.  Recovery receives no route or name input. */
    if(pub_journal_open(root_fd, &tree)) {
        root_fd = -1;
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    /* pub_journal_open now owns this duplicate on every later exit. */
    root_fd = -1;
    if(pub_read_prepared(&tree, command_uuid, &wire)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    context_status = character_save_journal_v2_writer_validate_held(writer, &tuple);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = pub_context_result(context_status);
        goto done;
    }
    if(strcmp(tuple.world_id, wire.world_id) ||
       strcmp(tuple.writer_instance_id, wire.writer_instance_id) ||
       tuple.writer_epoch != wire.writer_epoch) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY;
        goto done;
    }
    pub_tree_close(&tree);
    context_status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = pub_context_result(context_status);
        goto done;
    }
    if(pub_tree_open(root_fd, wire.legacy_shard, &tree)) {
        root_fd = -1;
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    /* pub_tree_open now owns this duplicate on every later exit. */
    root_fd = -1;
    if(pub_read_prepared(&tree, command_uuid, &reread_wire)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    context_status = character_save_journal_v2_writer_validate_held(writer, &tuple);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = pub_context_result(context_status);
        goto done;
    }
    if(memcmp(&wire, &reread_wire, sizeof(wire)) ||
       strcmp(tuple.world_id, wire.world_id) ||
       strcmp(tuple.writer_instance_id, wire.writer_instance_id) ||
       tuple.writer_epoch != wire.writer_epoch ||
       pub_decode_name(&wire, name, &name_length) ||
       character_save_journal_v2_stage_leaf(wire.command_uuid, stage_leaf,
                                            sizeof(stage_leaf))) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY;
        goto done;
    }
    if(pub_marker_state(&tree, &wire, &published)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    result = pub_local_publish(&tree, &wire, name, stage_leaf, published);
done:
    pub_cleanup_close(root_fd);
    memset(&tuple, 0, sizeof(tuple));
    memset(&wire, 0, sizeof(wire));
    memset(&reread_wire, 0, sizeof(reread_wire));
    memset(name, 0, sizeof(name));
    pub_tree_close(&tree);
    return result;
}

character_save_journal_v2_publish_result character_save_journal_v2_publish(
    writer, canonical_legacy_name, canonical_legacy_name_length, lookup,
    lookup_opaque, command_uuid)
const character_save_journal_v2_writer_context *writer;
const unsigned char *canonical_legacy_name;
size_t canonical_legacy_name_length;
character_save_journal_v2_route_lookup lookup;
void *lookup_opaque;
const char *command_uuid;
{
    v2_publish_tree tree;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire;
    character_save_journal_v2_bound_route route;
    character_save_journal_v2_writer_context_status context_status;
    char stage_leaf[44];
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    size_t name_length;
    int published, root_fd = -1;
    character_save_journal_v2_publish_result result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
    memset(&tree, 0, sizeof(tree));
    tree.root_fd = tree.player_fd = tree.shard_fd = tree.journal_fd = tree.stage_fd = -1;
    memset(&tuple, 0, sizeof(tuple));
    memset(&wire, 0, sizeof(wire));
    if(!writer || !canonical_legacy_name || !canonical_legacy_name_length || !lookup ||
       !pub_uuid(command_uuid))
        return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_INVALID_ARGUMENT;
    memset(&route, 0, sizeof(route));
    if(character_save_journal_v2_route_bind(writer, canonical_legacy_name,
                                             canonical_legacy_name_length, lookup,
                                             lookup_opaque, &route) !=
       CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK)
        return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY;
    context_status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK)
        return pub_context_result(context_status);
    /* Journal first supplies the shard: no route string ever selects a file
     * descriptor.  Reopen its canonical tree before any file mutation. */
    if(pub_journal_open(root_fd, &tree)) {
        root_fd = -1;
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    /* pub_journal_open now owns this duplicate on every later exit. */
    root_fd = -1;
    if(pub_read_record(&tree, command_uuid, &wire, &published)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    pub_tree_close(&tree);
    context_status = character_save_journal_v2_writer_dup_held_root_fd(writer, &root_fd);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = pub_context_result(context_status);
        goto done;
    }
    if(pub_tree_open(root_fd, wire.legacy_shard, &tree)) {
        root_fd = -1;
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    /* pub_tree_open now owns this duplicate on every later exit. */
    root_fd = -1;
    if(pub_read_record(&tree, command_uuid, &wire, &published)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    context_status = character_save_journal_v2_writer_validate_held(writer, &tuple);
    if(context_status != CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = pub_context_result(context_status);
        goto done;
    }
    if(!pub_route_matches(&route, &tuple, &wire) ||
       pub_decode_name(&wire, name, &name_length) ||
       character_save_journal_v2_stage_leaf(wire.command_uuid, stage_leaf,
                                            sizeof(stage_leaf))) {
        result = CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY;
        goto done;
    }
    result = pub_local_publish(&tree, &wire, name, stage_leaf, published);
done:
    pub_cleanup_close(root_fd);
    memset(&tuple, 0, sizeof(tuple));
    memset(&route, 0, sizeof(route));
    memset(&wire, 0, sizeof(wire));
    memset(name, 0, sizeof(name));
    pub_tree_close(&tree);
    return result;
}

static int pub_route_v3_matches(route, tuple, wire)
const character_save_journal_v2_bound_route_v3 *route;
const character_save_journal_v2_writer_tuple *tuple;
const character_save_journal_v2_wire *wire;
{
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    size_t length, i;
    int expected_existing;
    if(!route||!tuple||!wire||wire->writer_revision==0||
       wire->writer_revision>(uint64_t)INT64_MAX||
       route->head_revision>(uint64_t)INT64_MAX||
       pub_decode_name(wire,name,&length)||
       strcmp(tuple->world_id,wire->world_id)||
       strcmp(tuple->writer_instance_id,wire->writer_instance_id)||
       tuple->writer_epoch!=wire->writer_epoch||
       strcmp(route->world_id,wire->world_id)||
       strcmp(route->character_id,wire->character_id)||
       route->legacy_name_length!=length||memcmp(route->legacy_name,name,length)||
       strcmp(route->legacy_shard,wire->legacy_shard)||
       route->storage_format!=wire->storage_format||
       (route->lifecycle!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED&&
        route->lifecycle!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING&&
        route->lifecycle!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE)||
       route->head_revision+1!=wire->writer_revision) return 0;
    for(i=length;i<sizeof(route->legacy_name);i++) if(route->legacy_name[i]) return 0;
    expected_existing=wire->expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING;
    if(expected_existing)
        return route->head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING&&
            !strcmp(route->head_sha256,wire->expected_sha256);
    return wire->expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT&&
        route->head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT&&
        !route->head_sha256[0];
}

character_save_journal_v2_publish_result character_save_journal_v2_publish_v3(
    writer, canonical_legacy_name, canonical_legacy_name_length, lookup,
    lookup_opaque, command_uuid)
const character_save_journal_v2_writer_context *writer;
const unsigned char *canonical_legacy_name;
size_t canonical_legacy_name_length;
character_save_journal_v2_route_lookup_v3 lookup;
void *lookup_opaque;
const char *command_uuid;
{
    v2_publish_tree tree;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire;
    character_save_journal_v2_bound_route_v3 route;
    character_save_journal_v2_writer_context_status context_status;
    char stage_leaf[44];
    unsigned char name[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    size_t name_length;
    int published, root_fd=-1;
    character_save_journal_v2_publish_result result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;
    memset(&tree,0,sizeof(tree));
    tree.root_fd=tree.player_fd=tree.shard_fd=tree.journal_fd=tree.stage_fd=-1;
    memset(&tuple,0,sizeof(tuple));
    memset(&wire,0,sizeof(wire));
    memset(&route,0,sizeof(route));
    if(!writer||!canonical_legacy_name||!canonical_legacy_name_length||!lookup||
       !pub_uuid(command_uuid))
        return CHARACTER_SAVE_JOURNAL_V2_PUBLISH_INVALID_ARGUMENT;
    context_status=character_save_journal_v2_writer_dup_held_root_fd(writer,&root_fd);
    if(context_status!=CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK)
        return pub_context_result(context_status);
    if(pub_journal_open(root_fd,&tree)) {
        root_fd=-1;
        result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    root_fd=-1;
    if(pub_read_record(&tree,command_uuid,&wire,&published)) {
        result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    pub_tree_close(&tree);
    context_status=character_save_journal_v2_writer_dup_held_root_fd(writer,&root_fd);
    if(context_status!=CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result=pub_context_result(context_status);
        goto done;
    }
    if(pub_tree_open(root_fd,wire.legacy_shard,&tree)) {
        root_fd=-1;
        result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    root_fd=-1;
    if(pub_read_record(&tree,command_uuid,&wire,&published)) {
        result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL;
        goto done;
    }
    /* No local mutation has happened.  Re-resolve here, not at entry, so
     * the callback proves the head still matches this exact PREPARED wire. */
    if(character_save_journal_v2_route_bind_v3(writer,canonical_legacy_name,
                                                canonical_legacy_name_length,
                                                lookup,lookup_opaque,&route)!=
       CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK) {
        result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY;
        goto done;
    }
    context_status=character_save_journal_v2_writer_validate_held(writer,&tuple);
    if(context_status!=CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result=pub_context_result(context_status);
        goto done;
    }
    if(!pub_route_v3_matches(&route,&tuple,&wire)||
       pub_decode_name(&wire,name,&name_length)||
       character_save_journal_v2_stage_leaf(wire.command_uuid,stage_leaf,
                                            sizeof(stage_leaf))) {
        result=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY;
        goto done;
    }
    result=pub_local_publish(&tree,&wire,name,stage_leaf,published);
done:
    pub_cleanup_close(root_fd);
    memset(&tuple,0,sizeof(tuple));
    memset(&route,0,sizeof(route));
    memset(&wire,0,sizeof(wire));
    memset(name,0,sizeof(name));
    pub_tree_close(&tree);
    return result;
}

#ifdef CHARACTER_SAVE_JOURNAL_V2_PUBLISH_TESTING
void character_save_journal_v2_publish_set_trusted_uid_for_test(uid)
uid_t uid;
{
    v2_publish_trusted_uid = uid;
}

void character_save_journal_v2_publish_faults_for_test(eintr_once, short_once,
    zero_once, eio_once, file_fsync_once, live_parent_fsync_once,
    live_promotion_once, journal_promotion_once, journal_parent_fsync_once,
    read_close_once, stage_close_once,
    live_close_once, journal_write_close_once)
int eintr_once, short_once, zero_once, eio_once, file_fsync_once;
int live_parent_fsync_once, live_promotion_once, journal_promotion_once;
int journal_parent_fsync_once;
int read_close_once, stage_close_once, live_close_once, journal_write_close_once;
{
    v2_publish_write_eintr = eintr_once != 0;
    v2_publish_write_short = short_once != 0;
    v2_publish_write_zero = zero_once != 0;
    v2_publish_write_eio = eio_once != 0;
    v2_publish_file_fsync = file_fsync_once != 0;
    v2_publish_live_parent_fsync = live_parent_fsync_once != 0;
    v2_publish_live_promotion = live_promotion_once != 0;
    v2_publish_journal_promotion = journal_promotion_once != 0;
    v2_publish_journal_parent_fsync = journal_parent_fsync_once != 0;
    v2_publish_read_close = read_close_once != 0;
    v2_publish_stage_close = stage_close_once != 0;
    v2_publish_live_close = live_close_once != 0;
    v2_publish_journal_write_close = journal_write_close_once != 0;
    v2_publish_stage_parent_fsync = 0;
    v2_publish_stage_unlink = 0;
    v2_publish_journal_temp_unlink = 0;
}

void character_save_journal_v2_publish_operation_faults_for_test(
    destination_parent_fsync_once, stage_parent_fsync_once,
    stage_unlink_once, journal_temp_unlink_once)
int destination_parent_fsync_once, stage_parent_fsync_once;
int stage_unlink_once, journal_temp_unlink_once;
{
    v2_publish_live_parent_fsync = destination_parent_fsync_once != 0;
    v2_publish_stage_parent_fsync = stage_parent_fsync_once != 0;
    v2_publish_stage_unlink = stage_unlink_once != 0;
    v2_publish_journal_temp_unlink = journal_temp_unlink_once != 0;
}

void character_save_journal_v2_publish_crash_after_live_promotion_for_test(enabled)
int enabled;
{
    v2_publish_crash_after_live_promotion = enabled != 0;
}

void character_save_journal_v2_publish_reset_cleanup_close_failures_for_test(void)
{
    v2_publish_cleanup_close_failures = 0;
}

unsigned int character_save_journal_v2_publish_cleanup_close_failures_for_test(void)
{
    return v2_publish_cleanup_close_failures;
}

void character_save_journal_v2_publish_pause_before_absent_link_for_test(ready_fd,
                                                                           release_fd)
int ready_fd;
int release_fd;
{
    v2_publish_link_ready_fd = ready_fd;
    v2_publish_link_release_fd = release_fd;
    v2_publish_link_pause_kind = ready_fd >= 0 ? 1 : 0;
}

void character_save_journal_v2_publish_pause_before_marker_link_for_test(ready_fd,
                                                                           release_fd)
int ready_fd;
int release_fd;
{
    v2_publish_link_ready_fd = ready_fd;
    v2_publish_link_release_fd = release_fd;
    v2_publish_link_pause_kind = ready_fd >= 0 ? 2 : 0;
}
#endif
