#include "character_save_journal_v2_protocol.h"

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static const char world[] = "m3-named-volume";
static const char instance[] = "22222222-0000-4000-8000-000000000092";
static const char character[] = "33333333-0000-4000-8000-000000000092";
static const char command[] = "11111111-0000-4000-8000-000000000092";
static const unsigned char name[] = "M3volume";
static const unsigned char payload[] = "m3named-volume-v2-payload\n";
static const char payload_sha256[] =
    "35a0200c0a2907dbe0abc39c439810ba34e4e2b0b3b6f120b96fcbb5ae89767a";

static int join_path(char *out, size_t size, const char *root, const char *leaf)
{
    int n = snprintf(out, size, "%s/%s", root, leaf);
    return n < 0 || (size_t)n >= size ? -1 : 0;
}

static int write_all(int fd, const void *bytes, size_t length)
{
    const unsigned char *cursor = bytes;
    ssize_t count;

    while (length) {
        count = write(fd, cursor, length);
        if (count < 0 && errno == EINTR)
            continue;
        if (count <= 0)
            return -1;
        cursor += count;
        length -= (size_t)count;
    }
    return 0;
}

static int make_directory(const char *root, const char *leaf)
{
    char path[4096];

    if (join_path(path, sizeof(path), root, leaf) || mkdir(path, 0700))
        return -1;
    return chmod(path, 0700);
}

static int put_file(const char *root, const char *leaf, const void *bytes,
                    size_t length)
{
    char path[4096];
    int fd;
    int result = 0;

    if (join_path(path, sizeof(path), root, leaf))
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

static int seed_volume(const char *root)
{
    char writer_instance[256];
    char writer_epoch[256];
    int instance_length;
    int epoch_length;

    if (chmod(root, 0700) ||
        make_directory(root, "player") ||
        make_directory(root, "player/ba") ||
        make_directory(root, "character-save-stage") ||
        make_directory(root, "character-save-journal"))
        return -1;
    instance_length = snprintf(writer_instance, sizeof(writer_instance),
        "version=2\nkind=writer-instance\nwriter_instance_id=%s\n", instance);
    if (instance_length < 0 || (size_t)instance_length >= sizeof(writer_instance))
        return -1;
    epoch_length = snprintf(writer_epoch, sizeof(writer_epoch),
        "version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=1\n",
        world, instance);
    if (epoch_length < 0 || (size_t)epoch_length >= sizeof(writer_epoch))
        return -1;
    return put_file(root, "character-save-journal/writer-instance.v2",
                    writer_instance, (size_t)instance_length) ||
        put_file(root, "character-save-journal/writer-epoch.v2",
                 writer_epoch, (size_t)epoch_length);
}

static character_save_journal_v2_route_lookup_result
route_lookup(void *opaque, const char *persisted_world,
             const unsigned char *canonical_name, size_t name_length,
             character_save_journal_v2_route_reply *reply)
{
    (void)opaque;
    if (!reply || strcmp(persisted_world, world) ||
        name_length != sizeof(name) - 1 ||
        memcmp(canonical_name, name, sizeof(name) - 1))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    memset(reply, 0, sizeof(*reply));
    reply->status = CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count = 1;
    strcpy(reply->world_id, world);
    strcpy(reply->character_id, character);
    memcpy(reply->legacy_name, name, sizeof(name) - 1);
    reply->legacy_name_length = sizeof(name) - 1;
    strcpy(reply->legacy_shard, "ba");
    reply->storage_format = CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    reply->lifecycle = CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

static int serialize_payload(void *opaque,
    const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route *route,
    const char *command_uuid, const unsigned char **bytes_out,
    size_t *length_out)
{
    (void)opaque;
    if (!writer || !route || !bytes_out || !length_out ||
        strcmp(writer->world_id, world) || strcmp(route->character_id, character) ||
        strcmp(command_uuid, command))
        return -1;
    *bytes_out = payload;
    *length_out = sizeof(payload) - 1;
    return 0;
}

static character_save_journal_v2_receipt_result
receipt_ack(void *opaque, const character_save_journal_v2_receipt *receipt)
{
    (void)opaque;
    if (!receipt || strcmp(receipt->world_id, world) ||
        receipt->legacy_name_key_length != sizeof(name) - 1 ||
        memcmp(receipt->legacy_name_key, name, sizeof(name) - 1) ||
        strcmp(receipt->character_id, character) ||
        strcmp(receipt->command_id, command) ||
        strcmp(receipt->writer_instance_id, instance) ||
        receipt->writer_epoch != 1 || receipt->writer_revision != 1 ||
        !receipt->expected_state || strcmp(receipt->expected_state, "absent") ||
        receipt->expected_sha256 || !receipt->post_sha256 ||
        strcmp(receipt->post_sha256, payload_sha256) ||
        receipt->storage_format != CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1)
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
}

static void trust_fixture_uid(void)
{
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
}

static int save_once(const char *root)
{
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;

    trust_fixture_uid();
    memset(&request, 0, sizeof(request));
    memset(&operations, 0, sizeof(operations));
    memset(&report, 0, sizeof(report));
    request.root = root;
    request.world_id = world;
    request.canonical_legacy_name = name;
    request.canonical_legacy_name_length = sizeof(name) - 1;
    request.command_uuid = command;
    request.writer_revision = 1;
    operations.route_lookup = route_lookup;
    operations.serialize = serialize_payload;
    operations.receipt = receipt_ack;
    return character_save_journal_v2_protocol_save(&request, &operations, &report);
}

static int crash_save_child(const char *root)
{
    pid_t child;
    int status;

    child = fork();
    if (child < 0)
        return -1;
    if (!child) {
        if (setenv("M3_V2_CRASH_CUTPOINT", "19", 1))
            _exit(125);
        _exit(save_once(root) == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK ? 124 : 123);
    }
    do status = 0, child = waitpid(child, &status, 0);
    while (child < 0 && errno == EINTR);
    if (child < 0 || !WIFSIGNALED(status) || WTERMSIG(status) != SIGKILL)
        return -1;
    return 0;
}

static int recover_once(const char *root)
{
    character_save_journal_v2_protocol_report report;

    trust_fixture_uid();
    unsetenv("M3_V2_CRASH_CUTPOINT");
    memset(&report, 0, sizeof(report));
    return character_save_journal_v2_protocol_recover(root, world, receipt_ack,
                                                       0, &report) ==
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK ? 0 : -1;
}

int main(int argc, char **argv)
{
    const char *root = getenv("MUHAN_HOME");

    if (argc != 2 || !root || !root[0])
        return 2;
    if (!strcmp(argv[1], "save-crash")) {
        if (seed_volume(root) || crash_save_child(root))
            return 1;
        puts("SAVE_CRASH_READY");
        fflush(stdout);
        for (;;)
            pause();
    }
    if (!strcmp(argv[1], "recover")) {
        if (recover_once(root))
            return 1;
        puts("RECOVERY_OK");
        return 0;
    }
    return 2;
}
