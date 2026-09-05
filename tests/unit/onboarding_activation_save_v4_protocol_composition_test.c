/* Activation capability -> bridge resolver -> production protocol V4.
 *
 * The journal root is a local temporary fixture.  Route, serializer, and
 * receipt remain controlled callbacks so this target cannot link a database,
 * runtime service, or the live MUD executable. */
#include "character_save_journal_v2_protocol.h"
#include "onboarding_activation_save_bridge.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char world[] = "m3-v4-composition";
static const char instance[] = "11111111-1111-4111-8111-111111111111";
static const char actor[] = "22222222-2222-4222-8222-222222222222";
static const char correlation[] = "33333333-3333-4333-8333-333333333333";
static const char character[] = "44444444-4444-4444-8444-444444444444";
static const char command[] = "55555555-5555-4555-8555-555555555555";
static const unsigned char name[] = "M3alpha";

typedef struct fixture {
    int route_calls, serialize_calls, receipt_calls;
    int tuple_exact, route_exact, command_exact;
    int change_head_before_publish;
} fixture;

int player_name_is_valid(const unsigned char *value, unsigned long minimum,
                         unsigned long maximum)
{
    size_t length;
    (void)minimum;
    if(!value || !value[0]) return 0;
    length = strlen((const char *)value);
    return length <= maximum;
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "onboarding_activation_save_v4_protocol_composition_test: %s\n",
        message);
    return 1;
}

static int join(char *output, size_t output_size, const char *root,
                const char *relative)
{
    int count = snprintf(output, output_size, "%s/%s", root, relative);
    return count < 0 || (size_t)count >= output_size ? -1 : 0;
}

static int write_all(int fd, const void *bytes, size_t length)
{
    const unsigned char *cursor = bytes;
    ssize_t written;
    while(length) {
        written = write(fd, cursor, length);
        if(written < 0 && errno == EINTR) continue;
        if(written <= 0) return -1;
        cursor += written;
        length -= (size_t)written;
    }
    return 0;
}

static int leaf(const char *root, const char *relative, const void *bytes,
                size_t length)
{
    char path[PATH_MAX];
    int fd, result = 0;
    if(join(path, sizeof(path), root, relative)) return -1;
    fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_NOFOLLOW, 0600);
    if(fd < 0) return -1;
    if(fchmod(fd, 0600) || write_all(fd, bytes, length) || fsync(fd)) result = -1;
    if(close(fd)) result = -1;
    return result;
}

static int directory(const char *root, const char *relative)
{
    char path[PATH_MAX];
    return join(path, sizeof(path), root, relative) || mkdir(path, 0700) ? -1 : 0;
}

static int remove_tree(const char *path)
{
    DIR *directory_handle;
    struct dirent *entry;
    struct stat status;
    char child[PATH_MAX];
    if(lstat(path, &status)) return errno == ENOENT ? 0 : -1;
    if(!S_ISDIR(status.st_mode)) return unlink(path);
    directory_handle = opendir(path);
    if(!directory_handle) return -1;
    while((entry = readdir(directory_handle))) {
        if(!strcmp(entry->d_name, ".") || !strcmp(entry->d_name, "..")) continue;
        if(snprintf(child, sizeof(child), "%s/%s", path, entry->d_name) < 0 ||
           remove_tree(child)) {
            closedir(directory_handle);
            return -1;
        }
    }
    return closedir(directory_handle) || rmdir(path) ? -1 : 0;
}

static int command_exists(const char *root, const char *suffix)
{
    char relative[128], path[PATH_MAX];
    struct stat status;
    int count = snprintf(relative, sizeof(relative),
        "character-save-journal/%s.%s", command, suffix);
    return count >= 0 && (size_t)count < sizeof(relative) &&
        !join(path, sizeof(path), root, relative) && !lstat(path, &status);
}

static int root_removed(const char *path)
{
    struct stat status;
    return lstat(path, &status) && errno == ENOENT;
}

static int setup(char root[PATH_MAX], int fail_after_mkdtemp)
{
    char temporary[PATH_MAX], writer_instance[256], writer_epoch[256];
    int count;
    if(!realpath("/tmp", temporary)) return -1;
    count = snprintf(root, PATH_MAX, "%s/muhan-v4-composition-XXXXXX", temporary);
    if(count < 0 || count >= PATH_MAX) return -1;
    if(!mkdtemp(root)) return -1;
    if(fail_after_mkdtemp) goto cleanup;
    count = snprintf(writer_instance, sizeof(writer_instance),
        "version=2\nkind=writer-instance\nwriter_instance_id=%s\n", instance);
    if(count < 0 || (size_t)count >= sizeof(writer_instance)) goto cleanup;
    count = snprintf(writer_epoch, sizeof(writer_epoch),
        "version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=7\n",
        world, instance);
    if(count < 0 || (size_t)count >= sizeof(writer_epoch)) goto cleanup;
    if(!(directory(root, "player") || directory(root, "player/66") ||
        directory(root, "character-save-stage") ||
        directory(root, "character-save-journal") ||
        leaf(root, "character-save-journal/writer-instance.v2", writer_instance,
             strlen(writer_instance)) ||
        leaf(root, "character-save-journal/writer-epoch.v2", writer_epoch,
             strlen(writer_epoch)))) return 0;
cleanup:
    remove_tree(root);
    return -1;
}

static character_save_journal_v2_route_lookup_result route_lookup(void *opaque,
    const char *persisted_world_id, const unsigned char *canonical_name,
    size_t canonical_name_length, character_save_journal_v2_route_reply_v3 *reply)
{
    fixture *test = opaque;
    if(!test || !reply || strcmp(persisted_world_id, world) ||
       canonical_name_length != sizeof(name) - 1 ||
       memcmp(canonical_name, name, sizeof(name) - 1))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    test->route_calls++;
    memset(reply, 0, sizeof(*reply));
    reply->status = CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count = 1;
    strcpy(reply->world_id, world);
    strcpy(reply->character_id, character);
    memcpy(reply->legacy_name, name, sizeof(name) - 1);
    reply->legacy_name_length = sizeof(name) - 1;
    strcpy(reply->legacy_shard, "66");
    reply->storage_format = CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    reply->lifecycle = CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    reply->head_state = CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT;
    if(test->change_head_before_publish && test->route_calls == 3)
        reply->head_revision = 1;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

static int serialize(void *opaque, const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route_v3 *route, const char *command_uuid,
    const unsigned char **bytes_out, size_t *length_out)
{
    static const unsigned char bytes[] = "activation-v4-record";
    fixture *test = opaque;
    if(!test || !writer || !route || !command_uuid || !bytes_out || !length_out)
        return -1;
    test->serialize_calls++;
    test->tuple_exact = !strcmp(writer->world_id, world) &&
        !strcmp(writer->writer_instance_id, instance) && writer->writer_epoch == 7;
    test->route_exact = !strcmp(route->character_id, character) &&
        route->legacy_name_length == sizeof(name) - 1 &&
        !memcmp(route->legacy_name, name, sizeof(name) - 1);
    test->command_exact = !strcmp(command_uuid, command);
    *bytes_out = bytes;
    *length_out = sizeof(bytes) - 1;
    return 0;
}

static character_save_journal_v2_receipt_result receipt(void *opaque,
    const character_save_journal_v2_receipt *value)
{
    fixture *test = opaque;
    if(!test || !value || strcmp(value->command_id, command) ||
       strcmp(value->character_id, character))
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    test->receipt_calls++;
    return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
}

static int generated_uuid(void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1])
{
    (void)opaque;
    (void)output;
    return -1; /* A bridge candidate must make native UUID generation inert. */
}

static int activate(onboarding_activation_save_capability *capability,
                    onboarding_activation_save_bridge *bridge)
{
    return onboarding_activation_save_capability_capture(capability, actor,
        correlation, character, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        command, (const char *)name) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        onboarding_activation_save_bridge_begin(bridge, capability, command, actor,
        correlation, character, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        (const char *)name) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY;
}

static int test_setup_cleanup(void)
{
    char root[PATH_MAX];
    memset(root, 0, sizeof(root));
    return expect(setup(root, 1) && root[0] && root_removed(root),
        "setup removes an mkdtemp journal when a later setup step fails");
}

static int run_case(int fail_publish, int fail_close)
{
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_protocol_held_request_v3 request;
    character_save_journal_v2_protocol_operations_v4 operations;
    character_save_journal_v2_protocol_report report;
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    fixture test;
    character_save_journal_v2_protocol_result result;
    int failed = 0;

    memset(root, 0, sizeof(root));
    memset(&writer, 0, sizeof(writer));
    memset(&request, 0, sizeof(request));
    memset(&operations, 0, sizeof(operations));
    memset(&report, 0, sizeof(report));
    memset(&capability, 0, sizeof(capability));
    memset(&bridge, 0, sizeof(bridge));
    memset(&test, 0, sizeof(test));
    if(setup(root, 0)) {
        fprintf(stderr, "onboarding_activation_save_v4_protocol_composition_test: fixture setup failed\n");
        return 1;
    }
    if(character_save_journal_v2_writer_open(root, world, &writer)) {
        fprintf(stderr, "onboarding_activation_save_v4_protocol_composition_test: writer open failed: %s\n",
            strerror(errno));
        remove_tree(root);
        return 1;
    }
    if(!activate(&capability, &bridge)) {
        fprintf(stderr, "onboarding_activation_save_v4_protocol_composition_test: bridge activation failed\n");
        character_save_journal_v2_writer_close(&writer);
        remove_tree(root);
        return 1;
    }
    request.canonical_legacy_name = name;
    request.canonical_legacy_name_length = sizeof(name) - 1;
    operations.route_lookup = route_lookup;
    operations.route_opaque = &test;
    operations.serialize = serialize;
    operations.serialize_opaque = &test;
    operations.receipt = receipt;
    operations.receipt_opaque = &test;
    operations.resolve_candidate = onboarding_activation_save_bridge_resolve;
    operations.resolve_candidate_opaque = &bridge;
    operations.generate_uuid = generated_uuid;
    test.change_head_before_publish = fail_publish;
    result = character_save_journal_v2_protocol_save_held_v4(&writer, &request,
        &operations, &report);
    failed += expect(test.route_calls == 3 && test.serialize_calls == 1 &&
        test.tuple_exact && test.route_exact && test.command_exact,
        "the real V4 path carries the bridge candidate's command through exact route and held tuple callbacks");
    if(fail_publish) {
        failed += expect(result == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH &&
            report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED &&
            command_exists(root, "prepared") && !command_exists(root, "published") &&
            !test.receipt_calls && capability.armed &&
            onboarding_activation_save_bridge_finish(&bridge, &report) ==
            ONBOARDING_ACTIVATION_SAVE_BRIDGE_RETAINED,
            "the production V4 implementation reports PREPARED and retains the bridge capability when its live publish route changes");
    } else {
        failed += expect(result == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED &&
            report.reached == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED &&
            command_exists(root, "prepared") && command_exists(root, "published") &&
            test.receipt_calls == 1 &&
            onboarding_activation_save_bridge_finish(&bridge, &report) ==
            ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED && !capability.armed,
            "the production V4 implementation reports PUBLISHED and consumes only then");
    }
    if(fail_close) character_save_journal_v2_writer_fail_close_once_for_test(1);
    {
        int close_result = character_save_journal_v2_writer_close(&writer);
        int cleanup_result = remove_tree(root);
        failed += expect(close_result == (fail_close ? -1 : 0) &&
            cleanup_result == 0 && root_removed(root),
            "fixture cleanup runs and removes the journal even when writer close fails");
    }
    return failed;
}

int main(void)
{
    int failed;
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    setenv("MUD_M3_MODE", "shadow", 1);
    setenv("MUD_M3_PLAYER_SNAPSHOT_V1", "handoff", 1);
    failed = test_setup_cleanup() | run_case(1, 0) | run_case(0, 0) |
        run_case(0, 1);
    unsetenv("MUD_M3_MODE");
    unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    if(failed) return 1;
    puts("onboarding_activation_save_v4_protocol_composition_test: ok");
    return 0;
}
