/*
 * TDD record:
 * RED: this standalone seam test was written against the public owner
 * contract before the owner module existed.
 * GREEN: cc -std=gnu89 -Wall -Wextra -Werror -I src this_file owner.c
 * Sanitizer: add -O1 -fno-omit-frame-pointer -fsanitize=address,undefined.
 */
#include "character_save_journal_v2_process_owner.h"

#include <stdio.h>
#include <string.h>

typedef struct fixture {
    character_save_journal_v2_process_owner owner;
    character_save_journal_v2_process_owner_configuration configuration;
    character_save_journal_v2_rpc_transport transport;
    char buffer[128];
    int deadline_fail, uuid_fail, bootstrap_fail, recovery_fail, set_fail;
    int close_fail, finish_calls, deadline_calls, uuid_calls, live_init_calls;
    int bootstrap_calls, recovery_calls, store_init_calls, build_calls, set_calls;
    int reset_calls, close_calls, global_store_installed;
    char trace[32];
    unsigned int trace_length;
    char bootstrap_candidate[37];
    char bootstrap_deadline[64];
    const char *bootstrap_root, *bootstrap_world;
    character_save_journal_v2_writer_context writer;
} fixture;

static fixture *current;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "character_save_journal_v2_process_owner_test: %s\n", message);
    return 1;
}

static void mark(char event)
{
    if(current && current->trace_length + 1 < sizeof(current->trace)) {
        current->trace[current->trace_length++] = event;
        current->trace[current->trace_length] = 0;
    }
}

static int fake_deadline(void *opaque, char output[64])
{
    fixture *test = (fixture *)opaque;
    test->deadline_calls++;
    mark('D');
    if(test->deadline_fail) return -1;
    strcpy(output, "2026-09-03T00:02:00Z");
    return 0;
}

static int fake_uuid(void *opaque, char output[37])
{
    fixture *test = (fixture *)opaque;
    test->uuid_calls++;
    mark('U');
    if(test->uuid_fail) return -1;
    strcpy(output, "20000000-0000-4000-8000-000000000002");
    return 0;
}

static int fake_load(void *opaque, char *name, struct creature **player)
{
    (void)opaque;
    (void)name;
    if(player) *player = 0;
    return PLAYER_STORE_NOT_FOUND;
}

static void fake_finish(void *connection)
{
    fixture *test = (fixture *)connection;
    test->finish_calls++;
}

static const character_save_journal_v2_rpc_transport_operations fake_operations = {
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, fake_finish
};

character_save_journal_v2_rpc_transport_state
character_save_journal_v2_rpc_transport_get_state(
    const character_save_journal_v2_rpc_transport *transport)
{
    return transport ? transport->state :
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED;
}

void character_save_journal_v2_live_ops_init(
    character_save_journal_v2_live_ops *ops,
    character_save_journal_v2_rpc_transport *transport,
    const char *deadline)
{
    current->live_init_calls++;
    mark('L');
    ops->transport = transport;
    ops->acquire_lease_expires_at = deadline;
}

int character_save_journal_v2_live_ops_writer_epoch_acquire(void *opaque,
    const character_save_journal_v2_writer_tuple *request,
    character_save_journal_v2_writer_tuple *granted)
{
    (void)opaque;
    (void)request;
    (void)granted;
    return -1;
}

int character_save_journal_v2_writer_bootstrap(const char *root,
    const char *world, const char *candidate,
    character_save_journal_v2_writer_epoch_acquire acquire, void *opaque,
    character_save_journal_v2_writer_context *writer)
{
    current->bootstrap_calls++;
    mark('B');
    current->bootstrap_root = root;
    current->bootstrap_world = world;
    strcpy(current->bootstrap_candidate, candidate);
    strcpy(current->bootstrap_deadline,
        ((character_save_journal_v2_live_ops *)opaque)->acquire_lease_expires_at);
    if(acquire != character_save_journal_v2_live_ops_writer_epoch_acquire ||
       opaque != &current->owner.live_ops || writer != &current->owner.held_writer)
        return -1;
    if(current->bootstrap_fail) return -1;
    memset(writer, 0x5a, sizeof(*writer));
    current->writer = *writer;
    return 0;
}

int character_save_journal_v2_writer_close(
    character_save_journal_v2_writer_context *writer)
{
    current->close_calls++;
    mark('C');
    if(writer != &current->owner.held_writer) return -1;
    return current->close_fail ? -1 : 0;
}

character_save_journal_v2_receipt_result
character_save_journal_v2_live_ops_receipt_callback(void *opaque,
    const character_save_journal_v2_receipt *receipt)
{
    (void)opaque;
    (void)receipt;
    return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
}

character_save_journal_v2_recovery_result
character_save_journal_v2_recovery_run(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_receipt_callback receipt, void *opaque,
    character_save_journal_v2_recovery_report *report)
{
    current->recovery_calls++;
    mark('R');
    if(writer != &current->owner.held_writer ||
       receipt != character_save_journal_v2_live_ops_receipt_callback ||
       opaque != &current->owner.live_ops || !report)
        return CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INVALID_ARGUMENT;
    report->discovered = 2;
    report->visited = 2;
    return current->recovery_fail ? CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE :
        CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK;
}

void character_save_journal_v2_player_store_init(
    character_save_journal_v2_player_store *store,
    character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_live_ops *ops, char *buffer,
    unsigned long capacity, const player_record_serializer_limits *limits,
    character_save_journal_v2_player_store_lease_deadline deadline,
    void *deadline_opaque, character_save_journal_v2_player_store_command_uuid uuid,
    void *uuid_opaque, character_save_journal_v2_player_store_file_load load,
    void *load_opaque)
{
    current->store_init_calls++;
    mark('P');
    if(store == &current->owner.player_store && writer == &current->owner.held_writer &&
       ops == &current->owner.live_ops && buffer == current->buffer &&
       capacity == sizeof(current->buffer) && limits == &current->configuration.serializer_limits &&
       deadline == fake_deadline && deadline_opaque == current && uuid == fake_uuid &&
       uuid_opaque == current && load == fake_load && load_opaque == current)
        store->held_writer = writer;
}

static int fake_save(void *opaque, char *name, struct creature *player)
{
    (void)opaque;
    (void)name;
    (void)player;
    return PLAYER_STORE_OK;
}

player_store_ops character_save_journal_v2_player_store_build(
    character_save_journal_v2_player_store *store)
{
    player_store_ops result;
    current->build_calls++;
    mark('G');
    result.save = fake_save;
    result.load = fake_load;
    result.opaque = store;
    return result;
}

int player_store_set(const player_store_ops *ops)
{
    current->set_calls++;
    mark('S');
    if(!ops || ops->opaque != &current->owner.player_store) return -1;
    if(current->set_fail) return -1;
    current->global_store_installed = 1;
    return 0;
}

void player_store_reset(void)
{
    current->reset_calls++;
    mark('X');
    current->global_store_installed = 0;
}

static void setup(fixture *test)
{
    memset(test, 0, sizeof(*test));
    current = test;
    test->transport.state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;
    test->transport.operations = &fake_operations;
    test->transport.connection = test;
    test->configuration.root = "/caller/root";
    test->configuration.world_id = "m3-world";
    test->configuration.transport = &test->transport;
    test->configuration.buffer = test->buffer;
    test->configuration.buffer_capacity = sizeof(test->buffer);
    test->configuration.serializer_limits.max_depth = 32;
    test->configuration.serializer_limits.max_objects = 1024;
    test->configuration.acquire_deadline = fake_deadline;
    test->configuration.acquire_deadline_opaque = test;
    test->configuration.candidate_uuid = fake_uuid;
    test->configuration.candidate_uuid_opaque = test;
    test->configuration.file_load = fake_load;
    test->configuration.file_load_opaque = test;
    character_save_journal_v2_process_owner_init(&test->owner,
        &test->configuration);
}

static int no_later_calls(const fixture *test)
{
    return !test->live_init_calls && !test->bootstrap_calls &&
        !test->recovery_calls && !test->store_init_calls && !test->build_calls &&
        !test->set_calls && !test->reset_calls && !test->close_calls;
}

static int test_validate_and_early_cutpoints(void)
{
    fixture test;
    int failed = 0;
    setup(&test);
    test.configuration.buffer = 0;
    character_save_journal_v2_process_owner_init(&test.owner, &test.configuration);
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_INVALID_ARGUMENT &&
        no_later_calls(&test) && !test.deadline_calls && !test.uuid_calls &&
        !test.global_store_installed, "invalid arguments mutate neither callbacks nor PlayerStore");
    setup(&test);
    test.transport.state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_TRANSPORT_NOT_READY &&
        no_later_calls(&test) && !test.deadline_calls && !test.uuid_calls &&
        !test.finish_calls, "non-READY transport is rejected without taking connection ownership");
    setup(&test);
    test.deadline_fail = 1;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_DEADLINE &&
        !strcmp(test.trace, "D") && !test.uuid_calls && no_later_calls(&test) &&
        test.owner.state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED,
        "deadline cutpoint precedes UUID and leaves no installed store");
    setup(&test);
    test.uuid_fail = 1;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANDIDATE_UUID &&
        !strcmp(test.trace, "DU") && no_later_calls(&test) &&
        !test.global_store_installed, "candidate UUID cutpoint follows deadline and precedes live ops");
    return failed;
}

static int test_bootstrap_recovery_and_install_cutpoints(void)
{
    fixture test;
    int failed = 0;
    setup(&test);
    test.bootstrap_fail = 1;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_BOOTSTRAP &&
        !strcmp(test.trace, "DULB") && !test.recovery_calls && !test.set_calls &&
        !test.close_calls && !test.global_store_installed,
        "bootstrap failure does not invent a held writer or alter PlayerStore");
    setup(&test);
    test.recovery_fail = 1;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_RECOVERY &&
        !strcmp(test.trace, "DULBRC") && test.owner.recovery_result ==
        CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INCOMPLETE &&
        test.owner.recovery_report.discovered == 2 && !test.set_calls &&
        !test.global_store_installed && !test.owner.writer_held,
        "recovery report survives failed startup and closes writer before store installation");
    setup(&test);
    test.set_fail = 1;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_PLAYER_STORE &&
        !strcmp(test.trace, "DULBRPGSC") && test.set_calls == 1 && !test.reset_calls &&
        !test.global_store_installed && !test.owner.player_store_installed &&
        !test.owner.writer_held,
        "failed installation is the last cutpoint and never resets a prior global store");
    return failed;
}

static int test_success_repeated_start_and_shutdown(void)
{
    fixture test;
    int failed = 0;
    setup(&test);
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK &&
        !strcmp(test.trace, "DULBRPGS") && test.owner.state ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY && test.owner.player_store_installed &&
        test.owner.live_ops.acquire_lease_expires_at == 0 &&
        test.global_store_installed && !strcmp(test.bootstrap_candidate,
        "20000000-0000-4000-8000-000000000002") && !strcmp(test.bootstrap_deadline,
        "2026-09-03T00:02:00Z") && test.bootstrap_root == test.configuration.root &&
        test.bootstrap_world == test.configuration.world_id && !test.finish_calls,
        "successful startup uses caller data in exact observable order without transport ownership");
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_ALREADY_STARTED &&
        !strcmp(test.trace, "DULBRPGS") && test.set_calls == 1,
        "repeated start cannot reinstall PlayerStore");
    failed += expect(character_save_journal_v2_process_owner_shutdown(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK &&
        !strcmp(test.trace, "DULBRPGSXC") && !test.global_store_installed &&
        !test.owner.player_store_installed && !test.owner.writer_held &&
        test.owner.state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED &&
        !test.finish_calls, "shutdown resets PlayerStore before close and leaves transport borrowed");
    failed += expect(character_save_journal_v2_process_owner_shutdown(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK && test.reset_calls == 1 &&
        test.close_calls == 1, "repeated shutdown is idempotent");
    return failed;
}

static int test_close_failure_and_restart(void)
{
    fixture test;
    int failed = 0;
    setup(&test);
    if(character_save_journal_v2_process_owner_start(&test.owner)) return 1;
    test.close_fail = 1;
    failed += expect(character_save_journal_v2_process_owner_shutdown(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED &&
        !strcmp(test.trace, "DULBRPGSXC") && !test.global_store_installed &&
        !test.owner.writer_held && test.owner.shutdown_result ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED,
        "writer close failure remains diagnostic after PlayerStore is safely reset");
    failed += expect(character_save_journal_v2_process_owner_shutdown(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED && test.close_calls == 1,
        "a failed close is not retried by an idempotent shutdown");
    test.close_fail = 0;
    failed += expect(character_save_journal_v2_process_owner_start(&test.owner) ==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK && test.set_calls == 2 &&
        test.bootstrap_calls == 2, "a stopped caller-owned owner can be explicitly started again");
    return failed;
}

int main(void)
{
    return test_validate_and_early_cutpoints() |
        test_bootstrap_recovery_and_install_cutpoints() |
        test_success_repeated_start_and_shutdown() |
        test_close_failure_and_restart();
}
