#include "character_save_journal_v2_protocol.h"
#include "character_save_journal_v2.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

static const char WORLD[] = "m3-protocol";
static const char INSTANCE_A[] = "11111111-1111-4111-8111-111111111111";
static const char INSTANCE_B[] = "22222222-2222-4222-8222-222222222222";
static const char CHARACTER[] = "33333333-3333-4333-8333-333333333333";
static const char COMMAND_A[] = "10000000-0000-0000-0000-000000000001";
static const char COMMAND_B[] = "20000000-0000-0000-0000-000000000002";
static const unsigned char NAME[] = "M3alpha";

/* Receipt pointers belong to the callback.  Keep the first complete receipt
 * in owned arrays so a retry proves all 12 semantic fields are unchanged. */
typedef struct receipt_snapshot {
    int             valid, expected_sha256_present;
    char            world_id[CHARACTER_SAVE_JOURNAL_V2_WORLD_ID_MAX + 1];
    unsigned char   legacy_name_key[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX];
    size_t          legacy_name_key_length;
    char            character_id[CHARACTER_SAVE_JOURNAL_V2_UUID_LEN + 1];
    char            command_id[CHARACTER_SAVE_JOURNAL_V2_UUID_LEN + 1];
    char            writer_instance_id[CHARACTER_SAVE_JOURNAL_V2_UUID_LEN + 1];
    char            request_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    unsigned long long writer_epoch, writer_revision;
    char            expected_state[9];
    char            expected_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    char            post_sha256[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    unsigned int    storage_format;
} receipt_snapshot;

typedef struct mock {
    int             route_calls, serialize_calls, receipt_calls;
    int             fail_route, fail_serializer, swap_root_after_serialize,
                    route_existing, lose_writer_after_serialize;
    int             head_advances, receipt_exact;
    character_save_journal_v2_receipt_result receipt_result;
    const unsigned char *payload;
    size_t          payload_length;
    char            receipt_command[37];
    char            route_expected_sha256[65];
    char            held_root[PATH_MAX];
    char            request_root[PATH_MAX];
    char            receipt_prepared_root[PATH_MAX];
    char            receipt_expected_command[37];
    receipt_snapshot first_receipt;
    character_save_journal_v2_route_head_state v3_head_state;
    uint64_t        v3_head_revision;
    char            v3_head_sha256[65];
    int             v3_change_on_second;
}               mock;

typedef struct lifecycle {
    const char     *root;
    int             attest_calls, seal_calls, install_calls, old_unlocked, b_observed,
                    same_successor, fail_attest;
}               lifecycle;

typedef struct restart_receipt {
    int             fd;
    char            prepared_root[PATH_MAX];
    char            expected_command[37];
}               restart_receipt;

static int replace_writer_epoch(const char *root, const char *instance,
                                unsigned int epoch_number);

static int expect(condition, message)
    int             condition;
    const char     *message;
{
    if (condition)
        return 0;
    fprintf(stderr, "protocol: %s\n", message);
    return 1;
}
static int join(out, out_size, root, relative)
    char           *out;
    size_t          out_size;
    const char     *root, *relative;
{
    int             n = snprintf(out, out_size, "%s/%s", root, relative);
    return n < 0 || (size_t) n >= out_size ? -1 : 0;
}
static int write_all(fd, bytes, length)
    int             fd;
    const void     *bytes;
    size_t          length;
{
    const unsigned char *cursor = bytes;
    ssize_t         written;
    while (length) {
        written = write(fd, cursor, length);
        if (written < 0 && errno == EINTR)
            continue;
        if (written <= 0)
            return -1;
        cursor += written;
        length -= (size_t) written;
    } return 0;
}
static int leaf(root, relative, bytes, length)
    const char     *root, *relative;
    const void     *bytes;
    size_t          length;
{
    char            path[PATH_MAX];
    int             fd, result = 0;
    if (join(path, sizeof(path), root, relative))
        return -1;
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_NOFOLLOW, 0600);
    if (fd < 0)
        return -1;
    if (fchmod(fd, 0600) || write_all(fd, bytes, length) || fsync(fd))
        result = -1;
    if (close(fd))
        result = -1;
    return result;
}
static int make_directory(root, relative)
    const char     *root, *relative;
{
    char            path[PATH_MAX];
    return join(path, sizeof(path), root, relative) || mkdir(path, 0700) ? -1 : 0;
}
static int remove_tree(path)
    const char     *path;
{
    DIR            *directory;
    struct dirent  *entry;
    struct stat     st;
    char            child[PATH_MAX];
    if (lstat(path, &st))
        return errno == ENOENT ? 0 : -1;
    if (!S_ISDIR(st.st_mode))
        return unlink(path);
    if (!(directory = opendir(path)))
        return -1;
    while ((entry = readdir(directory))) {
        if (!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, ".."))
            continue;
        if (snprintf(child, sizeof(child), "%s/%s", path, entry->d_name) < 0 || remove_tree(child)) {
            closedir(directory);
            return -1;
        }
    } return closedir(directory) || rmdir(path) ? -1 : 0;
}
static int make_root(root, label)
    char            root[PATH_MAX];
const char     *label;
{
    char            tmp[PATH_MAX];
    int             n;
    if (!realpath("/tmp", tmp))
        return -1;
    n = snprintf(root, PATH_MAX, "%s/muhan-v2-protocol-%s-XXXXXX", tmp, label);
    return n < 0 || n >= PATH_MAX || !mkdtemp(root) ? -1 : 0;
}
static int seed(root)
    const char     *root;
{
    char            instance[256], epoch[256];
    int             n;
    n = snprintf(instance, sizeof(instance), "version=2\nkind=writer-instance\nwriter_instance_id=%s\n", INSTANCE_A);
    if (n < 0 || (size_t) n >= sizeof(instance))
        return -1;
    n = snprintf(epoch, sizeof(epoch), "version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=7\n", WORLD, INSTANCE_A);
    if (n < 0 || (size_t) n >= sizeof(epoch))
        return -1;
    return make_directory(root, "player") || make_directory(root, "player/66") || make_directory(root, "character-save-stage") || make_directory(root, "character-save-journal") || leaf(root, "character-save-journal/writer-instance.v2", instance, strlen(instance)) || leaf(root, "character-save-journal/writer-epoch.v2", epoch, strlen(epoch)) ? -1 : 0;
}
static int setup(root, label)
    char            root[PATH_MAX];
const char     *label;
{
    return make_root(root, label) || seed(root) ? -1 : 0;
}
static int exists(root, relative)
    const char     *root, *relative;
{
    char            path[PATH_MAX];
    struct stat     st;
    return !join(path, sizeof(path), root, relative) && !lstat(path, &st);
}
static int command_exists(root, command, suffix)
    const char     *root, *command, *suffix;
{
    char            relative[128];
    int             n = snprintf(relative, sizeof(relative), "character-save-journal/%s.%s", command, suffix);
    return n >= 0 && (size_t) n < sizeof(relative) && exists(root, relative);
}

static int file_equals(root, relative, expected, length)
    const char     *root, *relative;
    const void     *expected;
    size_t          length;
{
    unsigned char   actual[256];
    char            path[PATH_MAX];
    struct stat     st;
    ssize_t         count;
    int             fd, result = 0;
    if (!expected || length > sizeof(actual) || join(path, sizeof(path), root,
                                                      relative))
        return 0;
    fd = open(path, O_RDONLY | O_NOFOLLOW);
    if (fd < 0)
        return 0;
    if (fstat(fd, &st) || !S_ISREG(st.st_mode) || st.st_size != (off_t) length)
        result = -1;
    else {
        do
            count = read(fd, actual, length);
        while (count < 0 && errno == EINTR);
        if (count != (ssize_t) length || memcmp(actual, expected, length))
            result = -1;
    }
    if (close(fd))
        result = -1;
    memset(actual, 0, sizeof(actual));
    return !result;
}

static int stage_equals(root, command, expected, length)
    const char     *root, *command;
    const void     *expected;
    size_t          length;
{
    char            relative[128];
    int             n;
    n = snprintf(relative, sizeof(relative), "character-save-stage/%s.stage",
                 command);
    return n >= 0 && (size_t) n < sizeof(relative) &&
        file_equals(root, relative, expected, length);
}

static int file_sha256(root, relative, digest)
    const char     *root, *relative;
    char           *digest;
{
    char            path[PATH_MAX];
    int             fd, result;
    if (!digest || join(path, sizeof(path), root, relative))
        return -1;
    fd = open(path, O_RDONLY | O_NOFOLLOW);
    if (fd < 0)
        return -1;
    result = character_save_journal_v2_hash_fd(fd, digest);
    if (close(fd))
        result = -1;
    return result;
}

static int make_decoy_tree(root)
    const char     *root;
{
    return make_directory(root, "player") || make_directory(root, "player/66") ||
        make_directory(root, "character-save-stage") ||
        make_directory(root, "character-save-journal") ? -1 : 0;
}

static character_save_journal_v2_route_lookup_result route(opaque, world, name, name_length, reply)
void           *opaque;
const char     *world;
const unsigned char *name;
size_t          name_length;
character_save_journal_v2_route_reply *reply;
{
    mock           *state = opaque;
    state->route_calls++;
    if (state->fail_route || strcmp(world, WORLD) || name_length != sizeof(NAME) - 1 || memcmp(name, NAME, sizeof(NAME) - 1))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    memset(reply, 0, sizeof(*reply));
    reply->status = CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count = 1;
    strcpy(reply->world_id, WORLD);
    strcpy(reply->character_id, CHARACTER);
    memcpy(reply->legacy_name, NAME, sizeof(NAME) - 1);
    reply->legacy_name_length = sizeof(NAME) - 1;
    strcpy(reply->legacy_shard, "66");
    reply->storage_format = CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    reply->lifecycle = CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    if (state->route_existing) {
        reply->has_imported_file_sha256 = 1;
        if (state->route_expected_sha256[0])
            strcpy(reply->imported_file_sha256, state->route_expected_sha256);
        else {
            memset(reply->imported_file_sha256, '0', 64);
            reply->imported_file_sha256[64] = 0;
        }
    } return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

static character_save_journal_v2_route_lookup_result route_v3(opaque, world, name,
                                                               name_length, reply)
void *opaque;
const char *world;
const unsigned char *name;
size_t name_length;
character_save_journal_v2_route_reply_v3 *reply;
{
    mock *state=opaque;
    state->route_calls++;
    if(state->fail_route||strcmp(world,WORLD)||name_length!=sizeof(NAME)-1||
       memcmp(name,NAME,sizeof(NAME)-1))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    if(state->v3_change_on_second&&state->route_calls==2) {
        state->v3_head_revision++;
        state->v3_change_on_second=0;
    }
    memset(reply,0,sizeof(*reply));
    reply->status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count=1;
    strcpy(reply->world_id,WORLD);
    strcpy(reply->character_id,CHARACTER);
    memcpy(reply->legacy_name,NAME,sizeof(NAME)-1);
    reply->legacy_name_length=sizeof(NAME)-1;
    strcpy(reply->legacy_shard,"66");
    reply->storage_format=1;
    reply->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    reply->head_state=state->v3_head_state;
    reply->head_revision=state->v3_head_revision;
    if(reply->head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING)
        strcpy(reply->head_sha256,state->v3_head_sha256);
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}
static int serialize(opaque, writer, bound, command, bytes_out, length_out)
    void           *opaque;
    const           character_save_journal_v2_writer_tuple *writer;
    const           character_save_journal_v2_bound_route *bound;
    const char     *command;
    const unsigned char **bytes_out;
    size_t         *length_out;
{
    mock           *state = opaque;
    state->serialize_calls++;
    if (state->fail_serializer || strcmp(writer->world_id, WORLD) || strcmp(bound->character_id, CHARACTER) || !command || !bytes_out || !length_out)
        return -1;
    if (state->swap_root_after_serialize) {
        state->swap_root_after_serialize = 0;
        if (snprintf(state->held_root, sizeof(state->held_root), "%s.held", state->request_root) < 0 || rename(state->request_root, state->held_root) != 0 || mkdir(state->request_root, 0700) != 0 || make_decoy_tree(state->request_root) != 0)
            return -1;
        if (snprintf(state->receipt_prepared_root,
                     sizeof(state->receipt_prepared_root), "%s",
                     state->held_root) < 0)
            return -1;
    }
    if (state->lose_writer_after_serialize &&
        replace_writer_epoch(state->request_root, INSTANCE_B, 8))
        return -1;
    *bytes_out = state->payload;
    *length_out = state->payload_length;
    return 0;
}

static int serialize_v3(opaque, writer, bound, command, bytes_out, length_out)
void *opaque;
const character_save_journal_v2_writer_tuple *writer;
const character_save_journal_v2_bound_route_v3 *bound;
const char *command;
const unsigned char **bytes_out;
size_t *length_out;
{
    mock *state=opaque;
    state->serialize_calls++;
    if(state->fail_serializer||strcmp(writer->world_id,WORLD)||
       strcmp(bound->character_id,CHARACTER)||!command||!bytes_out||!length_out)
        return -1;
    *bytes_out=state->payload;
    *length_out=state->payload_length;
    return 0;
}

static int receipt_copy(out, out_size, value)
char *out;
size_t out_size;
const char *value;
{
    size_t length;
    if(!out || !out_size || !value) return -1;
    for(length = 0; length < out_size; length++) {
        if(!value[length]) {
            memcpy(out, value, length + 1);
            return 0;
        }
    }
    return -1;
}

static int exact_receipt(prepared_root, expected_command, value)
const char *prepared_root;
const char *expected_command;
const character_save_journal_v2_receipt *value;
{
    character_save_journal_v2_wire wire;
    const char *expected_state;
    if(!value || !prepared_root || !expected_command || !value->world_id ||
       !value->legacy_name_key || !value->character_id || !value->command_id ||
       !value->writer_instance_id || !value->request_sha256 ||
       !value->expected_state || !value->post_sha256 ||
       strcmp(value->command_id, expected_command) ||
       character_save_journal_v2_read_prepared(prepared_root, expected_command,
                                                &wire) != 0)
        return 0;
    expected_state = wire.expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ?
        "existing" : "absent";
    return !strcmp(value->world_id, WORLD) &&
        value->legacy_name_key_length == sizeof(NAME) - 1 &&
        !memcmp(value->legacy_name_key, NAME, sizeof(NAME) - 1) &&
        !strcmp(value->character_id, CHARACTER) &&
        !strcmp(value->command_id, wire.command_uuid) &&
        !strcmp(value->writer_instance_id, wire.writer_instance_id) &&
        !strcmp(value->request_sha256, wire.request_sha256) &&
        value->writer_epoch == wire.writer_epoch &&
        value->writer_revision == wire.writer_revision &&
        !strcmp(value->expected_state, expected_state) &&
        ((wire.expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING &&
          value->expected_sha256 &&
          !strcmp(value->expected_sha256, wire.expected_sha256)) ||
         (wire.expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT &&
          value->expected_sha256 == 0)) &&
        !strcmp(value->post_sha256, wire.post_sha256) &&
        value->storage_format == wire.storage_format;
}

static int receipt_snapshot_matches(snapshot, value)
receipt_snapshot *snapshot;
const character_save_journal_v2_receipt *value;
{
    int expected_sha256_present;
    if(!snapshot || !value || !value->world_id || !value->legacy_name_key ||
       !value->character_id || !value->command_id || !value->writer_instance_id ||
       !value->request_sha256 || !value->expected_state || !value->post_sha256 ||
       value->legacy_name_key_length > sizeof(snapshot->legacy_name_key))
        return 0;
    expected_sha256_present = value->expected_sha256 != 0;
    if(!snapshot->valid) {
        if(receipt_copy(snapshot->world_id, sizeof(snapshot->world_id), value->world_id) ||
           receipt_copy(snapshot->character_id, sizeof(snapshot->character_id), value->character_id) ||
           receipt_copy(snapshot->command_id, sizeof(snapshot->command_id), value->command_id) ||
           receipt_copy(snapshot->writer_instance_id, sizeof(snapshot->writer_instance_id), value->writer_instance_id) ||
           receipt_copy(snapshot->request_sha256, sizeof(snapshot->request_sha256), value->request_sha256) ||
           receipt_copy(snapshot->expected_state, sizeof(snapshot->expected_state), value->expected_state) ||
           receipt_copy(snapshot->post_sha256, sizeof(snapshot->post_sha256), value->post_sha256) ||
           (expected_sha256_present && receipt_copy(snapshot->expected_sha256,
               sizeof(snapshot->expected_sha256), value->expected_sha256)))
            return 0;
        memcpy(snapshot->legacy_name_key, value->legacy_name_key,
               value->legacy_name_key_length);
        snapshot->legacy_name_key_length = value->legacy_name_key_length;
        snapshot->expected_sha256_present = expected_sha256_present;
        snapshot->writer_epoch = value->writer_epoch;
        snapshot->writer_revision = value->writer_revision;
        snapshot->storage_format = value->storage_format;
        snapshot->valid = 1;
        return 1;
    }
    return !strcmp(snapshot->world_id, value->world_id) &&
        snapshot->legacy_name_key_length == value->legacy_name_key_length &&
        !memcmp(snapshot->legacy_name_key, value->legacy_name_key,
                value->legacy_name_key_length) &&
        !strcmp(snapshot->character_id, value->character_id) &&
        !strcmp(snapshot->command_id, value->command_id) &&
        !strcmp(snapshot->writer_instance_id, value->writer_instance_id) &&
        !strcmp(snapshot->request_sha256, value->request_sha256) &&
        snapshot->writer_epoch == value->writer_epoch &&
        snapshot->writer_revision == value->writer_revision &&
        !strcmp(snapshot->expected_state, value->expected_state) &&
        snapshot->expected_sha256_present == expected_sha256_present &&
        (!expected_sha256_present ||
         !strcmp(snapshot->expected_sha256, value->expected_sha256)) &&
        !strcmp(snapshot->post_sha256, value->post_sha256) &&
        snapshot->storage_format == value->storage_format;
}
static character_save_journal_v2_receipt_result receipt(opaque, value)
void           *opaque;
const           character_save_journal_v2_receipt *value;
{
    mock           *state = opaque;
    state->receipt_calls++;
    if (!exact_receipt(state->receipt_prepared_root,
                       state->receipt_expected_command, value) ||
        !receipt_snapshot_matches(&state->first_receipt, value))
        state->receipt_exact = 0;
    else
        state->receipt_exact = 1;
    if (value && value->command_id) {
        if (!state->receipt_command[0]) {
            strcpy(state->receipt_command, value->command_id);
            state->head_advances++;
        } else if (strcmp(state->receipt_command, value->command_id))
            return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    } return state->receipt_result;
}

static character_save_journal_v2_receipt_result restart_receipt_callback(opaque,
                                                                      value)
void           *opaque;
const           character_save_journal_v2_receipt *value;
{
    restart_receipt *state = opaque;
    char            byte = exact_receipt(state ? state->prepared_root : 0,
                                         state ? state->expected_command : 0,
                                         value) ? 'x' : '!';
    if (!state || state->fd < 0 || write_all(state->fd, &byte, 1))
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    return byte == 'x' ? CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED :
        CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
}

static int recover_in_fresh_child(root, expected_command)
    const char     *root;
    const char     *expected_command;
{
    int             pipefd[2], status;
    char            byte = 0;
    pid_t           child;
    ssize_t         count;
    if (pipe(pipefd))
        return -1;
    child = fork();
    if (child == 0) {
        restart_receipt receipt_state;
        character_save_journal_v2_protocol_report report;
        close(pipefd[0]);
        receipt_state.fd = pipefd[1];
        if (snprintf(receipt_state.prepared_root,
                     sizeof(receipt_state.prepared_root), "%s", root) < 0 ||
            snprintf(receipt_state.expected_command,
                     sizeof(receipt_state.expected_command), "%s",
                     expected_command) < 0)
            _exit(4);
        if (character_save_journal_v2_protocol_recover(root, WORLD,
                                                   restart_receipt_callback,
                                                       &receipt_state,
                                                       &report) !=
            CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK)
            _exit(2);
        if (close(pipefd[1]))
            _exit(3);
        _exit(0);
    }
    if (child < 0) {
        close(pipefd[0]);
        close(pipefd[1]);
        return -1;
    }
    close(pipefd[1]);
    do
        count = read(pipefd[0], &byte, 1);
    while (count < 0 && errno == EINTR);
    if (close(pipefd[0]) || waitpid(child, &status, 0) != child)
        return -1;
    return count == 1 && byte == 'x' && WIFEXITED(status) && WEXITSTATUS(status) == 0 ? 0 : -1;
}
static void request_init(request, root, command)
character_save_journal_v2_protocol_request * request;
    const char     *root, *command;
{
    memset(request, 0, sizeof(*request));
    request->root = root;
    request->world_id = WORLD;
    request->canonical_legacy_name = NAME;
    request->canonical_legacy_name_length = sizeof(NAME) - 1;
    request->command_uuid = command;
    request->writer_revision = 1;
}
static int mock_receipt_expect(state, root, command)
mock           *state;
const char     *root, *command;
{
    int             root_length, command_length;
    if (!state || !root || !command)
        return -1;
    root_length = snprintf(state->receipt_prepared_root,
                           sizeof(state->receipt_prepared_root), "%s", root);
    command_length = snprintf(state->receipt_expected_command,
                              sizeof(state->receipt_expected_command), "%s",
                              command);
    return root_length < 0 ||
        (size_t) root_length >= sizeof(state->receipt_prepared_root) ||
        command_length < 0 ||
        (size_t) command_length >= sizeof(state->receipt_expected_command) ? -1 : 0;
}
static void operations_init(operations, state)
character_save_journal_v2_protocol_operations * operations;
    mock           *state;
{
    memset(operations, 0, sizeof(*operations));
    operations->route_lookup = route;
    operations->route_opaque = state;
    operations->serialize = serialize;
    operations->serialize_opaque = state;
    operations->receipt = receipt;
    operations->receipt_opaque = state;
}

static void operations_v3_init(operations, state)
character_save_journal_v2_protocol_operations_v3 *operations;
mock *state;
{
    memset(operations,0,sizeof(*operations));
    operations->route_lookup=route_v3;
    operations->route_opaque=state;
    operations->serialize=serialize_v3;
    operations->serialize_opaque=state;
    operations->receipt=receipt;
    operations->receipt_opaque=state;
}

/*
 * Regression: once the writer has held the original root, a serializer can
 * rename its request pathname and plant a fully valid decoy.  PREPARED and
 * readback must remain in the held tree, never the pathname's replacement.
 */
static int test_held_root_survives_request_path_swap(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;

    if (setup(root, "held-root"))
        return 1;
    memset(&state, 0, sizeof(state));
    strcpy(state.request_root, root);
    state.swap_root_after_serialize = 1;
    state.payload = (const unsigned char *)"held-root-payload";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    if (mock_receipt_expect(&state, root, request.command_uuid))
        return 1;
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK &&
                     command_exists(state.held_root, COMMAND_A, "prepared") &&
                     command_exists(state.held_root, COMMAND_A, "published") &&
                     command_exists(state.held_root, COMMAND_A, "acked") &&
                     state.receipt_exact && state.first_receipt.valid &&
                     exists(state.held_root, "player/66/M3alpha") &&
                     !command_exists(root, COMMAND_A, "prepared") &&
                     !exists(root, "player/66/M3alpha"),
               "prepare/read/publish must use only the original held root");
    if (remove_tree(root) || remove_tree(state.held_root))
        return failed + 1;
    return failed;
}

static int test_ordered_success(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;
    if (setup(root, "success"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"serialized-A";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    if (mock_receipt_expect(&state, root, request.command_uuid))
        return 1;
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK && report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED && state.route_calls == 2 && state.serialize_calls == 1 && state.receipt_calls == 1 && state.receipt_exact && state.first_receipt.valid && !strcmp(state.receipt_command, COMMAND_A) && command_exists(root, COMMAND_A, "prepared") && command_exists(root, COMMAND_A, "published") && command_exists(root, COMMAND_A, "acked") && exists(root, "player/66/M3alpha"), "actual prepare/publish/ack path must establish DB_ACKED evidence in order");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_live_precondition_blocks_prepared(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;

    if (setup(root, "precondition-absent"))
        return 1;
    if (leaf(root, "player/66/M3alpha", "unexpected-live", 15))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"precondition";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE &&
                     report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED &&
                     state.serialize_calls == 1 &&
                     stage_equals(root, COMMAND_A, state.payload,
                                  state.payload_length) &&
                     !command_exists(root, COMMAND_A, "prepared") &&
                     !command_exists(root, COMMAND_A, "published") &&
                     !command_exists(root, COMMAND_A, "acked") && !state.receipt_calls,
    "absent precondition mismatch must retain exact durable stage and stop before PREPARED");
    if (remove_tree(root) || setup(root, "precondition-existing"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.route_existing = 1;
    state.payload = (const unsigned char *)"precondition";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE &&
                     report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED &&
                     state.serialize_calls == 1 &&
                     stage_equals(root, COMMAND_A, state.payload,
                                  state.payload_length) &&
                     !command_exists(root, COMMAND_A, "prepared") &&
                     !command_exists(root, COMMAND_A, "published") &&
                     !command_exists(root, COMMAND_A, "acked") && !state.receipt_calls,
                     "existing-missing mismatch must retain exact durable stage and stop before PREPARED");
    if (remove_tree(root) || setup(root, "precondition-existing-hash"))
        return failed + 1;
    if (leaf(root, "player/66/M3alpha", "wrong-live", 10))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.route_existing = 1;
    state.payload = (const unsigned char *)"precondition";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE &&
                     report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED &&
                     state.serialize_calls == 1 &&
                     stage_equals(root, COMMAND_A, state.payload,
                                  state.payload_length) &&
                     !command_exists(root, COMMAND_A, "prepared") &&
                     !command_exists(root, COMMAND_A, "published") &&
                     !command_exists(root, COMMAND_A, "acked") && !state.receipt_calls,
                     "existing-hash mismatch must retain exact durable stage and stop before PREPARED");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_serializer_tuple_loss_blocks_stage(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;

    if (setup(root, "serializer-tuple-loss"))
        return 1;
    memset(&state, 0, sizeof(state));
    strcpy(state.request_root, root);
    state.lose_writer_after_serialize = 1;
    state.payload = (const unsigned char *)"serializer-tuple-loss";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER &&
                     report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED &&
                     state.serialize_calls == 1 &&
                     !exists(root, "character-save-stage/10000000-0000-0000-0000-000000000001.stage") &&
                     !command_exists(root, COMMAND_A, "prepared") &&
                     !command_exists(root, COMMAND_A, "published") &&
                     !command_exists(root, COMMAND_A, "acked") && !state.receipt_calls,
                     "serializer-time writer tuple loss must prohibit all stage and later mutations");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_post_stage_tuple_loss_blocks_prepared(void)
{
    char            root[PATH_MAX], digest[65];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    unsigned int    stage_file_before, stage_dir_before, stage_file_after,
                    stage_dir_after;
    int             ready[2], release[2], status, failed = 0;
    char            signal;
    pid_t           child;

    if (setup(root, "post-stage-tuple-loss") ||
        leaf(root, "player/66/M3alpha", "matching-live", 13) ||
        file_sha256(root, "player/66/M3alpha", digest) || pipe(ready) ||
        pipe(release))
        return 1;
    memset(&state, 0, sizeof(state));
    state.route_existing = 1;
    strcpy(state.route_expected_sha256, digest);
    state.payload = (const unsigned char *)"post-stage-tuple-loss";
    state.payload_length = strlen((const char *)state.payload);
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    child = fork();
    if (child == 0) {
        close(ready[1]);
        close(release[0]);
        if (read(ready[0], &signal, 1) != 1 ||
            replace_writer_epoch(root, INSTANCE_B, 8) ||
            write_all(release[1], "x", 1) || close(ready[0]) ||
            close(release[1]))
            _exit(1);
        _exit(0);
    }
    if (child < 0)
        return 1;
    close(ready[0]);
    close(release[1]);
    character_save_journal_v2_fsync_counts_for_test(&stage_file_before,
                                                     &stage_dir_before, 0, 0);
    character_save_journal_v2_pause_live_precondition_after_open_for_test(
        ready[1], release[0]);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER &&
                     report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED &&
                     stage_equals(root, COMMAND_A, state.payload,
                                  state.payload_length) &&
                     !command_exists(root, COMMAND_A, "prepared") &&
                     !command_exists(root, COMMAND_A, "published") &&
                     !command_exists(root, COMMAND_A, "acked") && !state.receipt_calls,
                     "writer tuple loss after durable stage must stop before PREPARED");
    character_save_journal_v2_pause_live_precondition_after_open_for_test(-1, -1);
    character_save_journal_v2_fsync_counts_for_test(&stage_file_after,
                                                     &stage_dir_after, 0, 0);
    if (close(ready[1]) || close(release[0]) || waitpid(child, &status, 0) != child)
        return failed + 1;
    failed += expect(WIFEXITED(status) && WEXITSTATUS(status) == 0 &&
                     stage_file_after == stage_file_before + 1 &&
                     stage_dir_after == stage_dir_before + 1,
                     "post-stage tuple failure must retain file and directory fsync evidence");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_local_incomplete_retries_exact_receipt(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;

    if (setup(root, "local-incomplete"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"local-incomplete";
    state.payload_length = strlen((const char *)state.payload);
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    if (mock_receipt_expect(&state, root, request.command_uuid))
        return 1;
    character_save_journal_v2_ack_fail_marker_fsync_for_test(1);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK &&
                     report.ack_result ==
                     CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE &&
                     report.reached ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED &&
                     state.receipt_calls == 1 && state.receipt_exact && state.head_advances == 1 &&
                     !command_exists(root, COMMAND_A, "acked"),
         "RPC ACK with incomplete local marker must report published cutpoint and retry evidence");
    state.receipt_calls = 0;
    failed += expect(character_save_journal_v2_protocol_recover(root, WORLD, receipt,
                                                         &state, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK &&
                     state.receipt_calls == 1 && state.receipt_exact && state.head_advances == 1 &&
                     command_exists(root, COMMAND_A, "acked"),
                     "local-incomplete restart must exact-retry receipt without duplicate head advance");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_fresh_process_restart_cutpoints(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;

    if (setup(root, "child-prepared"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"child-prepared";
    state.payload_length = strlen((const char *)state.payload);
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    character_save_journal_v2_publish_faults_for_test(0, 0, 0, 0, 0, 0, 0, 1,
                                                      0, 0, 0, 0, 0);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH &&
                     command_exists(root, COMMAND_A, "prepared") &&
                     !command_exists(root, COMMAND_A, "published") &&
                     recover_in_fresh_child(root, COMMAND_A) == 0 &&
                     command_exists(root, COMMAND_A, "acked"),
                 "fresh process must recover a retained PREPARED cutpoint");
    character_save_journal_v2_publish_faults_for_test(0, 0, 0, 0, 0, 0, 0, 0,
                                                      0, 0, 0, 0, 0);
    if (remove_tree(root) || setup(root, "child-published"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"child-published";
    state.payload_length = strlen((const char *)state.payload);
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED &&
                     command_exists(root, COMMAND_A, "published") &&
                     recover_in_fresh_child(root, COMMAND_A) == 0 &&
                     command_exists(root, COMMAND_A, "acked"),
                "fresh process must recover a retained PUBLISHED cutpoint");
    if (remove_tree(root) || setup(root, "child-local-incomplete"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"child-local-incomplete";
    state.payload_length = strlen((const char *)state.payload);
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    character_save_journal_v2_ack_fail_marker_fsync_for_test(1);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK &&
                     !command_exists(root, COMMAND_A, "acked") &&
                     recover_in_fresh_child(root, COMMAND_A) == 0 &&
                     command_exists(root, COMMAND_A, "acked"),
            "fresh process must exact-retry RPC-local-incomplete evidence");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_cutpoint_failures(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    mock            state;
    int             failed = 0;
    if (setup(root, "route"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.fail_route = 1;
    state.payload = (const unsigned char *)"x";
    state.payload_length = 1;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE && state.route_calls == 1 && !state.serialize_calls && !state.receipt_calls && !command_exists(root, COMMAND_A, "prepared"), "route failure must prohibit serializer and every later transport call");
    if (remove_tree(root) || setup(root, "serializer"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.fail_serializer = 1;
    state.payload = (const unsigned char *)"x";
    state.payload_length = 1;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER && state.route_calls == 1 && state.serialize_calls == 1 && !state.receipt_calls && !command_exists(root, COMMAND_A, "prepared"), "serializer failure must prohibit staging, publish, and receipt");
    if (remove_tree(root) || setup(root, "prepare"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"stage";
    state.payload_length = 5;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    character_save_journal_v2_fail_fsync_for_test(1, 0, 0, 0);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE && report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED && !state.receipt_calls && exists(root, "character-save-stage/10000000-0000-0000-0000-000000000001.stage") && !command_exists(root, COMMAND_A, "prepared"), "stage fsync failure must remain SERIALIZED and prohibit publish/receipt");
    character_save_journal_v2_fail_fsync_for_test(0, 0, 0, 0);
    if (remove_tree(root) || setup(root, "publish"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"publish";
    state.payload_length = 7;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    character_save_journal_v2_publish_faults_for_test(0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH && !state.receipt_calls && command_exists(root, COMMAND_A, "prepared") && exists(root, "character-save-stage/10000000-0000-0000-0000-000000000001.stage") && !command_exists(root, COMMAND_A, "published"), "publish failure must retain PREPARED/stage evidence and prohibit receipt");
    character_save_journal_v2_publish_faults_for_test(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0);
    if (remove_tree(root) || setup(root, "receipt"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"receipt";
    state.payload_length = 7;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations, &report) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED && state.receipt_calls == 1 && command_exists(root, COMMAND_A, "prepared") && command_exists(root, COMMAND_A, "published") && !command_exists(root, COMMAND_A, "acked"), "deferred receipt must preserve published evidence without DB_ACKED");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int replace_writer_epoch(root, instance, epoch_number)
    const char     *root, *instance;
    unsigned int    epoch_number;
{
    char            instance_text[256], epoch_text[256], path[PATH_MAX];
    int             n;
    n = snprintf(instance_text, sizeof(instance_text), "version=2\nkind=writer-instance\nwriter_instance_id=%s\n", instance);
    if (n < 0 || (size_t) n >= sizeof(instance_text))
        return -1;
    n = snprintf(epoch_text, sizeof(epoch_text), "version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=%u\n", WORLD, instance, epoch_number);
    if (n < 0 || (size_t) n >= sizeof(epoch_text) || join(path, sizeof(path), root, "character-save-journal/writer-instance.v2") || unlink(path) || leaf(root, "character-save-journal/writer-instance.v2", instance_text, strlen(instance_text)) || join(path, sizeof(path), root, "character-save-journal/writer-epoch.v2") || unlink(path) || leaf(root, "character-save-journal/writer-epoch.v2", epoch_text, strlen(epoch_text)))
        return -1;
    return 0;
}
static int lifecycle_attest(opaque, drained)
    void           *opaque;
    const           character_save_journal_v2_writer_tuple *drained;
{
    lifecycle      *state = opaque;
    state->attest_calls++;
    return state->fail_attest || !drained || strcmp(drained->world_id, WORLD) ||
        strcmp(drained->writer_instance_id, INSTANCE_A) ||
        drained->writer_epoch != 7 || state->seal_calls || state->install_calls ? -1 : 0;
}

static int lifecycle_seal(opaque, drained)
    void           *opaque;
    const           character_save_journal_v2_writer_tuple *drained;
{
    lifecycle      *state = opaque;
    state->seal_calls++;
    return !drained || state->attest_calls != 1 || state->install_calls ||
        strcmp(drained->writer_instance_id, INSTANCE_A) || drained->writer_epoch != 7 ? -1 : 0;
}

static int lifecycle_install(opaque, drained, expected_next)
    void           *opaque;
    const           character_save_journal_v2_writer_tuple *drained;
    character_save_journal_v2_writer_tuple *expected_next;
{
    lifecycle      *state = opaque;
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_writer_tuple tuple;
    state->install_calls++;
    if (!drained || !expected_next || state->attest_calls != 1 || state->seal_calls != 1 ||
        state->install_calls != 1 || strcmp(drained->writer_instance_id, INSTANCE_A) ||
        drained->writer_epoch != 7)
        return -1;
    memset(&writer, 0, sizeof(writer));
    if (character_save_journal_v2_writer_open(state->root, WORLD, &writer) != 0)
        return -1;
    state->old_unlocked = 1;
    if (character_save_journal_v2_writer_close(&writer) != 0)
        return -1;
    if (state->same_successor) {
        memset(expected_next, 0, sizeof(*expected_next));
        strcpy(expected_next->world_id, WORLD);
        strcpy(expected_next->writer_instance_id, INSTANCE_A);
        expected_next->writer_epoch = 7;
        return 0;
    }
    if (replace_writer_epoch(state->root, INSTANCE_B, 8) != 0)
        return -1;
    if (character_save_journal_v2_writer_open(state->root, WORLD, &writer) != 0 ||
        character_save_journal_v2_writer_validate_held(&writer, &tuple) !=
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
        strcmp(tuple.writer_instance_id, INSTANCE_B) || tuple.writer_epoch != 8 ||
        character_save_journal_v2_writer_close(&writer) != 0)
        return -1;
    memset(expected_next, 0, sizeof(*expected_next));
    strcpy(expected_next->world_id, WORLD);
    strcpy(expected_next->writer_instance_id, INSTANCE_B);
    expected_next->writer_epoch = 8;
    state->b_observed = 1;
    return 0;
}

static int test_restart_and_cutover(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    character_save_journal_v2_protocol_cutover_operations cutover;
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_writer_tuple tuple;
    mock            state;
    lifecycle       life;
    int             failed = 0;

    if (setup(root, "restart"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"retry";
    state.payload_length = 5;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_A);
    if (mock_receipt_expect(&state, root, request.command_uuid))
        return 1;
    if (character_save_journal_v2_protocol_save(&request, &operations, &report) !=
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED)
        return 1;
    state.receipt_calls = 0;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    failed += expect(character_save_journal_v2_protocol_recover(root, WORLD, receipt,
                                                         &state, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK &&
                     state.receipt_calls == 1 && state.receipt_exact &&
       state.head_advances == 1 && command_exists(root, COMMAND_A, "acked"),
         "PUBLISHED restart must exact-retry receipt and persist DB_ACKED");
    state.receipt_calls = 0;
    failed += expect(character_save_journal_v2_protocol_recover(root, WORLD, receipt,
                                                         &state, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK &&
                     state.receipt_calls == 1 && state.receipt_exact &&
                     state.head_advances == 1 &&
      report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DRAINED,
                     "DB_ACKED restart must reissue exact idempotent receipt without head advance");
    if (remove_tree(root) || setup(root, "cutover"))
        return failed + 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"handoff";
    state.payload_length = 7;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_B);
    if (mock_receipt_expect(&state, root, request.command_uuid))
        return failed + 1;
    failed += expect(character_save_journal_v2_protocol_save(&request, &operations,
                                                             &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED &&
                     state.receipt_calls == 1 && state.receipt_exact &&
                     state.first_receipt.valid &&
                     !strcmp(state.first_receipt.command_id, COMMAND_B),
         "COMMAND_B deferred save must exactly match its durable PREPARED receipt");
    memset(&life, 0, sizeof(life));
    life.root = root;
    memset(&cutover, 0, sizeof(cutover));
    cutover.receipt = receipt;
    cutover.receipt_opaque = &state;
    cutover.attest_drained = lifecycle_attest;
    cutover.seal = lifecycle_seal;
    cutover.install_next_writer = lifecycle_install;
    cutover.lifecycle_opaque = &life;
    state.receipt_calls = 0;
    failed += expect(character_save_journal_v2_protocol_drain_and_install(root, WORLD,
                                                       &cutover, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_RECOVERY &&
       state.receipt_calls == 1 && state.receipt_exact && state.first_receipt.valid &&
       !strcmp(state.first_receipt.command_id, COMMAND_B) &&
       !life.attest_calls && !life.seal_calls &&
        !life.install_calls && command_exists(root, COMMAND_B, "published"),
    "COMMAND_B deferred drain retry must exactly match receipt and block cutover");
    state.receipt_calls = 0;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    failed += expect(character_save_journal_v2_protocol_drain_and_install(root, WORLD,
                                                       &cutover, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK &&
                     state.receipt_calls == 1 && state.receipt_exact &&
                     state.first_receipt.valid &&
                     !strcmp(state.first_receipt.command_id, COMMAND_B) &&
                     life.attest_calls == 1 && life.seal_calls == 1 && life.install_calls == 1 &&
                     life.old_unlocked && life.b_observed &&
                     report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NEXT_WRITER,
     "COMMAND_B ACKED drain retry must exactly match receipt before cutover");
    memset(&writer, 0, sizeof(writer));
    memset(&tuple, 0, sizeof(tuple));
    failed += expect(character_save_journal_v2_writer_open(root, WORLD, &writer) == 0 &&
          character_save_journal_v2_writer_validate_held(&writer, &tuple) ==
                     CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK &&
                     !strcmp(tuple.writer_instance_id, INSTANCE_B) && tuple.writer_epoch == 8 &&
                     character_save_journal_v2_writer_close(&writer) == 0,
                     "after B installation no A tuple can be acquired from durable writer state");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_same_a_successor_rejects_install(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    character_save_journal_v2_protocol_cutover_operations cutover;
    mock            state;
    lifecycle       life;
    int             failed = 0;

    if (setup(root, "same-a"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"same-a";
    state.payload_length = 6;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_B);
    if (character_save_journal_v2_protocol_save(&request, &operations, &report) !=
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED)
        return 1;
    memset(&life, 0, sizeof(life));
    life.root = root;
    life.same_successor = 1;
    memset(&cutover, 0, sizeof(cutover));
    cutover.receipt = receipt;
    cutover.receipt_opaque = &state;
    cutover.attest_drained = lifecycle_attest;
    cutover.seal = lifecycle_seal;
    cutover.install_next_writer = lifecycle_install;
    cutover.lifecycle_opaque = &life;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    failed += expect(character_save_journal_v2_protocol_drain_and_install(root, WORLD,
                                                       &cutover, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INSTALL &&
                     life.attest_calls == 1 && life.seal_calls == 1 &&
                     life.install_calls == 1,
                     "same-instance, same-epoch successor must reject after install verification");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static int test_drain_attestation_blocks_seal_and_install(void)
{
    char            root[PATH_MAX];
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    character_save_journal_v2_protocol_cutover_operations cutover;
    mock            state;
    lifecycle       life;
    int             failed = 0;

    if (setup(root, "attestation"))
        return 1;
    memset(&state, 0, sizeof(state));
    state.payload = (const unsigned char *)"attestation";
    state.payload_length = strlen((const char *)state.payload);
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_init(&operations, &state);
    request_init(&request, root, COMMAND_B);
    if (character_save_journal_v2_protocol_save(&request, &operations, &report) !=
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED)
        return 1;
    memset(&life, 0, sizeof(life));
    life.root = root;
    life.fail_attest = 1;
    memset(&cutover, 0, sizeof(cutover));
    cutover.receipt = receipt;
    cutover.receipt_opaque = &state;
    cutover.attest_drained = lifecycle_attest;
    cutover.seal = lifecycle_seal;
    cutover.install_next_writer = lifecycle_install;
    cutover.lifecycle_opaque = &life;
    state.receipt_result = CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    failed += expect(character_save_journal_v2_protocol_drain_and_install(root, WORLD,
                                                       &cutover, &report) ==
                     CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_RECOVERY &&
          life.attest_calls == 1 && !life.seal_calls && !life.install_calls,
       "failed drain attestation must make seal and install zero-mutation");
    if (remove_tree(root))
        return failed + 1;
    return failed;
}

static void held_request_v3_init(request, command)
character_save_journal_v2_protocol_held_request_v3 *request;
const char *command;
{
    memset(request,0,sizeof(*request));
    request->canonical_legacy_name=NAME;
    request->canonical_legacy_name_length=sizeof(NAME)-1;
    request->command_uuid=command;
}

static int test_held_v3_head_authority(void)
{
    char root[PATH_MAX], digest[65];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_protocol_held_request_v3 request;
    character_save_journal_v2_protocol_operations_v3 operations;
    character_save_journal_v2_protocol_report report;
    character_save_journal_v2_wire wire;
    mock state;
    int failed=0;

    if(setup(root,"held-v3-existing")||
       leaf(root,"player/66/M3alpha","revision-seven",14)||
       file_sha256(root,"player/66/M3alpha",digest)||
       character_save_journal_v2_writer_open(root,WORLD,&writer)) return 1;
    memset(&state,0,sizeof(state));
    state.payload=(const unsigned char *)"revision-eight";
    state.payload_length=strlen((const char *)state.payload);
    state.v3_head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING;
    state.v3_head_revision=7;
    strcpy(state.v3_head_sha256,digest);
    operations_v3_init(&operations,&state);
    held_request_v3_init(&request,COMMAND_A);
    if(mock_receipt_expect(&state,root,COMMAND_A)) return 1;
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK&&
        report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED&&
        state.route_calls==2&&state.serialize_calls==1&&state.receipt_exact&&
        character_save_journal_v2_read_prepared(root,COMMAND_A,&wire)==0&&
        wire.expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING&&
        wire.writer_revision==8&&character_save_journal_v2_writer_validate_held(
            &writer,&tuple)==CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
        "held v3 existing head 7 must produce exact ACK at revision 8 and retain writer");
    if(file_sha256(root,"player/66/M3alpha",digest)) return failed+1;
    state.v3_head_revision=8;
    strcpy(state.v3_head_sha256,digest);
    state.route_calls=state.serialize_calls=0;
    held_request_v3_init(&request,COMMAND_A);
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE&&
        report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED&&
        state.route_calls==1&&state.serialize_calls==1&&
        !exists(root,"character-save-stage/10000000-0000-0000-0000-000000000001.stage")&&
        character_save_journal_v2_read_prepared(root,COMMAND_A,&wire)==0&&
        wire.writer_revision==8&&character_save_journal_v2_writer_validate_held(
            &writer,&tuple)==CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
        "reused command UUID must reject before creating a contaminating stage");
    state.v3_head_revision=8;
    strcpy(state.v3_head_sha256,digest);
    state.payload=(const unsigned char *)"revision-nine";
    state.payload_length=strlen((const char *)state.payload);
    state.receipt_command[0]=0;
    memset(&state.first_receipt,0,sizeof(state.first_receipt));
    held_request_v3_init(&request,COMMAND_B);
    if(mock_receipt_expect(&state,root,COMMAND_B)) return failed+1;
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK&&
        character_save_journal_v2_read_prepared(root,COMMAND_B,&wire)==0&&
        wire.writer_revision==9&&state.receipt_exact&&
        character_save_journal_v2_writer_validate_held(&writer,&tuple)==
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
        "a sequential held v3 save must derive revision 9 from returned head 8");
    failed+=expect(character_save_journal_v2_writer_close(&writer)==0,
                   "held v3 writer must remain caller-owned after success");
    if(remove_tree(root)) return failed+1;
    return failed;
}

static int test_held_v3_rejects_and_preserves_evidence(void)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_protocol_held_request_v3 request;
    character_save_journal_v2_protocol_operations_v3 operations;
    character_save_journal_v2_protocol_report report;
    character_save_journal_v2_wire wire;
    mock state;
    int failed=0;

    if(setup(root,"held-v3-rejections")||
       character_save_journal_v2_writer_open(root,WORLD,&writer)) return 1;
    memset(&state,0,sizeof(state));
    state.payload=(const unsigned char *)"must-not-serialize";
    state.payload_length=strlen((const char *)state.payload);
    state.v3_head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED;
    operations_v3_init(&operations,&state);
    held_request_v3_init(&request,COMMAND_A);
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE&&
        !state.serialize_calls&&!exists(root,"character-save-stage/10000000-0000-0000-0000-000000000001.stage")&&
        !command_exists(root,COMMAND_A,"prepared")&&
        character_save_journal_v2_writer_validate_held(&writer,&tuple)==
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
        "uninitialized head must reject before serializer or local mutation");
    state.v3_head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT;
    state.v3_head_revision=(uint64_t)INT64_MAX;
    held_request_v3_init(&request,COMMAND_B);
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE&&
        !state.serialize_calls&&!exists(root,"character-save-stage/20000000-0000-0000-0000-000000000002.stage")&&
        !command_exists(root,COMMAND_B,"prepared"),
        "overflow head must reject before serializer or local mutation");
    state.v3_head_revision=0;
    state.v3_change_on_second=1;
    state.payload=(const unsigned char *)"changed-head";
    state.payload_length=strlen((const char *)state.payload);
    state.receipt_result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    state.route_calls=state.serialize_calls=0;
    held_request_v3_init(&request,COMMAND_A);
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH&&
        command_exists(root,COMMAND_A,"prepared")&&!command_exists(root,COMMAND_A,"published")&&
        !exists(root,"player/66/M3alpha")&&!state.receipt_calls&&
        character_save_journal_v2_writer_validate_held(&writer,&tuple)==
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
        "changed pre-publish head must preserve PREPARED and never publish");
    if(character_save_journal_v2_writer_close(&writer)||remove_tree(root)||
       setup(root,"held-v3-absent")||
       character_save_journal_v2_writer_open(root,WORLD,&writer)) return failed+1;
    memset(&state,0,sizeof(state));
    state.payload=(const unsigned char *)"absent-one";
    state.payload_length=strlen((const char *)state.payload);
    state.v3_head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT;
    state.v3_head_revision=0;
    state.receipt_result=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    operations_v3_init(&operations,&state);
    held_request_v3_init(&request,COMMAND_A);
    if(mock_receipt_expect(&state,root,COMMAND_A)) return failed+1;
    failed+=expect(character_save_journal_v2_protocol_save_held_v3(&writer,
        &request,&operations,&report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED&&
        character_save_journal_v2_read_prepared(root,COMMAND_A,&wire)==0&&
        wire.expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT&&
        wire.writer_revision==1&&command_exists(root,COMMAND_A,"prepared")&&
        command_exists(root,COMMAND_A,"published")&&!command_exists(root,COMMAND_A,"acked")&&
        character_save_journal_v2_writer_validate_held(&writer,&tuple)==
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
        "absent head 0 must produce revision 1 and retain recoverable deferred evidence");
    failed+=expect(character_save_journal_v2_writer_close(&writer)==0,
                   "held v3 writer must remain caller-owned on every outcome");
    if(remove_tree(root)) return failed+1;
    return failed;
}

int main(void)
{
    int             failed;
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    failed = test_held_root_survives_request_path_swap();
    failed += test_ordered_success();
    failed += test_live_precondition_blocks_prepared();
    failed += test_serializer_tuple_loss_blocks_stage();
    failed += test_post_stage_tuple_loss_blocks_prepared();
    failed += test_local_incomplete_retries_exact_receipt();
    failed += test_fresh_process_restart_cutpoints();
    failed += test_cutpoint_failures();
    failed += test_restart_and_cutover();
    failed += test_same_a_successor_rejects_install();
    failed += test_drain_attestation_blocks_seal_and_install();
    failed += test_held_v3_head_authority();
    failed += test_held_v3_rejects_and_preserves_evidence();
    if (failed)
        fprintf(stderr, "protocol failures: %d\n", failed);
    return failed ? 1 : 0;
}
