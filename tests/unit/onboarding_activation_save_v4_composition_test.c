/* Explicit activation capability -> bridge -> PlayerStore V4 composition. */
#include "onboarding_activation_gate.h"
#include "mstruct.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <fcntl.h>
#include <unistd.h>

typedef enum fake_outcome {
    FAKE_PREPARED,
    FAKE_PUBLISHED,
    FAKE_WRONG_TUPLE,
    FAKE_WRONG_NAME
} fake_outcome;

typedef struct fixture {
    character_save_journal_v2_process_owner owner;
    character_save_journal_v2_rpc_transport transport;
    player_record_serializer_limits limits;
    onboarding_activation_save_capability capability;
    character_save_journal_v2_writer_tuple tuple;
    creature player;
    char buffer[256];
    char actor[37], correlation[37], character[37], command[37], name[32];
    int validate_calls, renew_calls, bootstrap_calls, uuid_calls, serializer_calls;
    int v3_calls, v4_calls, resolver_calls, route_calls, prepared_calls, publish_calls;
    int candidate_exact, original_copy_calls;
    fake_outcome outcome;
} fixture;

/* Keep this focused test on the public active-owner seam without starting a
 * transport: all lower V4 callbacks remain deliberate local fakes. */
#define store owner.player_store
#define writer_context owner.held_writer
#define live_ops owner.live_ops

static fixture *fixtures[2];
static int fixture_count;
static fixture *copy_fixture;

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_dup_held_root_fd(
    const character_save_journal_v2_writer_context *writer, int *out)
{
    int i;
    copy_fixture=0;
    for(i=0;i<fixture_count;i++)
        if(writer==&fixtures[i]->writer_context) copy_fixture=fixtures[i];
    if(!copy_fixture || !out) return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
    *out=open("/dev/null",O_RDONLY);
    return *out>=0 ? CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK:
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
}

int character_save_journal_v2_request_sha256(
    const character_save_journal_v2_wire *wire, char out[65])
{
    if(!wire || !out) return -1;
    memset(out,'a',64); out[64]=0; return 0;
}

int character_save_journal_v2_copy_existing_at(int fd,
    const character_save_journal_v2_wire *wire, unsigned char *buffer,
    size_t capacity, size_t *length)
{
    static const char original[]="unchanged-original-record";
    if(fd<0 || !copy_fixture || !wire || !buffer || !length ||
       capacity<sizeof(original) ||
       wire->expected_state!=CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ||
       strcmp(wire->character_id,copy_fixture->character) ||
       strcmp(wire->command_uuid,copy_fixture->command)) return -1;
    copy_fixture->original_copy_calls++;
    memcpy(buffer,original,sizeof(original)); *length=sizeof(original); return 0;
}

int player_name_is_valid(const unsigned char *name, unsigned long minimum,
                         unsigned long maximum)
{
    (void)minimum;
    return name && name[0] && strlen((const char *)name) <= maximum;
}

static int expect(int ok, const char *message)
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activation_save_v4_composition_test: %s\n", message);
    return 1;
}

static fixture *fixture_for_writer(const character_save_journal_v2_writer_context *writer)
{
    int index;
    for(index = 0; index < fixture_count; index++)
        if(writer == &fixtures[index]->writer_context) return fixtures[index];
    return 0;
}

static fixture *fixture_for_live_ops(const character_save_journal_v2_live_ops *ops)
{
    int index;
    for(index = 0; index < fixture_count; index++)
        if(ops == &fixtures[index]->live_ops) return fixtures[index];
    return 0;
}

static fixture *fixture_for_player(const creature *player)
{
    int index;
    for(index = 0; index < fixture_count; index++)
        if(player == &fixtures[index]->player) return fixtures[index];
    return 0;
}

static void make_tuple(fixture *test, const char *world, const char *instance,
                       unsigned long long epoch)
{
    memset(&test->tuple, 0, sizeof(test->tuple));
    strcpy(test->tuple.world_id, world);
    strcpy(test->tuple.writer_instance_id, instance);
    test->tuple.writer_epoch = epoch;
}

static void make_route(fixture *test,
                       character_save_journal_v2_bound_route_v3 *route)
{
    memset(route, 0, sizeof(*route));
    strcpy(route->character_id, test->character);
    memcpy(route->legacy_name, test->name, strlen(test->name));
    route->legacy_name_length = strlen(test->name);
    strcpy(route->world_id,test->tuple.world_id);
    strcpy(route->legacy_shard,"16");
    route->storage_format=CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    route->head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING;
    route->head_revision=1;
    memset(route->head_sha256,'a',64); route->head_sha256[64]=0;
}

static void register_fixture(fixture *test)
{
    if(fixture_count < 2) fixtures[fixture_count++] = test;
}

/* The real reservation owner is covered separately.  This V4 composition
 * fixture supplies its already-authorized bridge so the gate remains the
 * sole path from the explicit capability into PlayerStore dispatch. */
onboarding_activation_reservation_owner_result
onboarding_activation_reservation_owner_attempt(owner, directory_fd, expected,
    capability, canonical_name, bridge_out)
character_save_journal_v2_process_owner *owner;
int directory_fd;
const onboarding_activation_binding *expected;
onboarding_activation_save_capability *capability;
const char *canonical_name;
onboarding_activation_save_bridge *bridge_out;
{
    if(!owner || directory_fd != 7 || !expected || !capability ||
       !canonical_name || !bridge_out)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT;
    return onboarding_activation_save_bridge_begin(bridge_out, capability,
        expected->command_id, expected->actor_user_id,
        expected->correlation_id, expected->character_id, expected->mode,
        canonical_name) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY ?
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY:
        ONBOARDING_ACTIVATION_RESERVATION_OWNER_BRIDGE_REJECTED;
}

static void setup(fixture *test, const char *actor, const char *correlation,
                  const char *character, const char *command, const char *name,
                  const char *world, const char *instance, unsigned long long epoch)
{
    memset(test, 0, sizeof(*test));
    strcpy(test->actor, actor);
    strcpy(test->correlation, correlation);
    strcpy(test->character, character);
    strcpy(test->command, command);
    strcpy(test->name, name);
    strcpy(test->player.name, name);
    make_tuple(test, world, instance, epoch);
    test->transport.state = CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;
    test->live_ops.transport = &test->transport;
    test->limits.max_depth = 64;
    test->limits.max_objects = 8192;
    test->owner.state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY;
    test->owner.writer_held = 1;
    test->owner.player_store_installed = 1;
    character_save_journal_v2_player_store_init(&test->store, &test->writer_context,
        &test->live_ops, test->buffer, sizeof(test->buffer), &test->limits,
        0, 0, 0, 0, 0, 0);
	/* The production host binds its active native owner once; rebinding here
	 * makes each deterministic fixture the sole active owner. */
    onboarding_activation_gate_bind_owner(&test->owner,7);
    register_fixture(test);
}

static int absent_bootstrap(void *opaque,
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_live_ops *ops, const unsigned char *name, size_t length)
{
    fixture *test = (fixture *)opaque;
    if(!test || writer != &test->writer_context || ops != &test->live_ops ||
       length != strlen(test->name) || memcmp(name, test->name, length)) return -1;
    test->bootstrap_calls++;
    return 0;
}

static onboarding_activation_save_runtime_helper_result attempt_mode(fixture *test,
    const char *command, const char *actor, const char *correlation,
    const char *character, onboarding_activation_binding_mode mode,
    const char *name)
{
    onboarding_activation_gate_result result=onboarding_activation_gate_attempt(
        &test->capability, command, actor, correlation, character,
        mode, name, test->name, &test->player);
    return result==ONBOARDING_ACTIVATION_GATE_CONSUMED ?
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED :
        result==ONBOARDING_ACTIVATION_GATE_RETAINED ?
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED :
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
}

static onboarding_activation_save_runtime_helper_result attempt(fixture *test,
    const char *command, const char *actor, const char *correlation,
    const char *character, const char *name)
{
    return attempt_mode(test, command, actor, correlation, character,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, name);
}

static int arm(fixture *test)
{
    return onboarding_activation_save_capability_capture(&test->capability,
        test->actor, test->correlation, test->character,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, test->command, test->name) ==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK;
}

static int arm_mode(fixture *test, onboarding_activation_binding_mode mode)
{
    return onboarding_activation_save_capability_capture(&test->capability,
        test->actor, test->correlation, test->character, mode, test->command,
        test->name) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK;
}

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_writer_tuple *tuple)
{
    fixture *test = fixture_for_writer(writer);
    if(!test || !tuple) return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
    test->validate_calls++;
    *tuple = test->tuple;
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK;
}

character_save_journal_v2_rpc_transport_state
character_save_journal_v2_rpc_transport_get_state(
    const character_save_journal_v2_rpc_transport *transport)
{
    return transport ? transport->state : CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_live_ops_writer_epoch_renew(void *opaque,
    const character_save_journal_v2_writer_tuple *tuple, const char *deadline)
{
    fixture *test = fixture_for_live_ops((character_save_journal_v2_live_ops *)opaque);
    if(!test || !tuple || !deadline || memcmp(tuple, &test->tuple, sizeof(*tuple)))
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    test->renew_calls++;
    return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
}

character_save_journal_v2_route_lookup_result
character_save_journal_v2_live_ops_route_lookup_v3(void *opaque, const char *world,
    const unsigned char *name, size_t length, character_save_journal_v2_route_reply_v3 *reply)
{
    fixture *test = fixture_for_live_ops((character_save_journal_v2_live_ops *)opaque);
    if(!test || !world || strcmp(world, test->tuple.world_id) || !name ||
       length != strlen(test->name) || memcmp(name, test->name, length) || !reply)
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    test->route_calls++;
    memset(reply, 0, sizeof(*reply));
    reply->status = CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count = 1;
    strcpy(reply->world_id, test->tuple.world_id);
    strcpy(reply->character_id, test->character);
    memcpy(reply->legacy_name, test->name, length);
    reply->legacy_name_length = length;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

character_save_journal_v2_receipt_result
character_save_journal_v2_live_ops_receipt_callback(void *opaque,
    const character_save_journal_v2_receipt *receipt)
{
    (void)opaque;
    (void)receipt;
    return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
}

int character_save_journal_v2_bootstrap_absent_head(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_live_ops *ops, const unsigned char *name, size_t length)
{
    (void)writer;
    (void)ops;
    (void)name;
    (void)length;
    return -1;
}

int player_record_serialize_bounded(creature *player, char perm_only, char *buffer,
    unsigned long capacity, unsigned long *written,
    const player_record_serializer_limits *limits)
{
    static const char record[] = "composition-record";
    fixture *test = fixture_for_player(player);
    if(!test || perm_only || !buffer || capacity < sizeof(record) || !written || !limits)
        return PLAYER_RECORD_SERIALIZER_INVALID;
    test->serializer_calls++;
    memcpy(buffer, record, sizeof(record));
    *written = sizeof(record);
    return PLAYER_RECORD_SERIALIZER_OK;
}

static int deadline(void *opaque, char output[64])
{
    fixture *test = (fixture *)opaque;
    if(!test || !output) return -1;
    strcpy(output, "2026-09-06T00:00:00Z");
    return 0;
}

static int generated_uuid(void *opaque, char output[37])
{
    fixture *test = (fixture *)opaque;
    if(!test || !output) return -1;
    test->uuid_calls++;
    strcpy(output, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee");
    return 0;
}

static int delegated_load(void *opaque, char *name, creature **player)
{
    fixture *test = (fixture *)opaque;
    if(!test || !name || !player) return PLAYER_STORE_IO_ERROR;
    *player = &test->player;
    return PLAYER_STORE_NOT_FOUND;
}

static int stale_candidate_resolver(void *opaque,
    const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route_v3 *route,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length,
    character_save_journal_v2_protocol_candidate_v4 *candidate)
{
    (void)opaque;
    (void)writer;
    (void)route;
    (void)canonical_legacy_name;
    (void)canonical_legacy_name_length;
    (void)candidate;
    return 0;
}

character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_save_held_v3(
    const character_save_journal_v2_writer_context *writer,
    const character_save_journal_v2_protocol_held_request_v3 *request,
    const character_save_journal_v2_protocol_operations_v3 *operations,
    character_save_journal_v2_protocol_report *report)
{
    fixture *test = fixture_for_writer(writer);
    const unsigned char *bytes = 0;
    size_t length = 0;
    if(!test || !request || !operations || !report ||
       strcmp(request->command_uuid, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee") ||
       operations->serialize(operations->serialize_opaque, &test->tuple, 0,
       request->command_uuid, &bytes, &length) || !bytes || !length)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    test->v3_calls++;
    report->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK;
}

character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_save_held_v4(
    const character_save_journal_v2_writer_context *writer,
    const character_save_journal_v2_protocol_held_request_v3 *request,
    const character_save_journal_v2_protocol_operations_v4 *operations,
    character_save_journal_v2_protocol_report *report)
{
    fixture *test = fixture_for_writer(writer);
    character_save_journal_v2_writer_tuple candidate_writer;
    character_save_journal_v2_bound_route_v3 route;
    character_save_journal_v2_protocol_candidate_v4 candidate;
    character_save_journal_v2_route_reply_v3 reply;
    const unsigned char *bytes = 0;
    size_t length = 0;
    int found;

    if(!test || !request || !operations || !report ||
       operations->resolve_candidate != onboarding_activation_save_bridge_resolve ||
       !operations->resolve_candidate_opaque) return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    test->v4_calls++;
    memset(report, 0, sizeof(*report));
    candidate_writer = test->tuple;
    if(test->outcome == FAKE_WRONG_TUPLE) candidate_writer.writer_epoch++;
    make_route(test, &route);
    if(test->outcome == FAKE_WRONG_NAME) route.legacy_name[0] = 'Z';
    memset(&candidate, 0, sizeof(candidate));
    found = operations->resolve_candidate(operations->resolve_candidate_opaque,
        &candidate_writer, &route, request->canonical_legacy_name,
        request->canonical_legacy_name_length, &candidate);
    test->resolver_calls++;
    if(test->outcome == FAKE_WRONG_TUPLE || test->outcome == FAKE_WRONG_NAME) {
        report->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;
        return found < 0 ? CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE :
            CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    }
    if(found != 1 || strcmp(candidate.command_uuid, test->command) ||
       memcmp(&candidate.writer, &test->tuple, sizeof(candidate.writer)) ||
       strcmp(candidate.character_id, test->character) ||
       candidate.canonical_legacy_name_length != strlen(test->name) ||
       memcmp(candidate.canonical_legacy_name, test->name, strlen(test->name)))
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    test->candidate_exact = 1;
    if(operations->route_lookup(operations->route_opaque, test->tuple.world_id,
       request->canonical_legacy_name, request->canonical_legacy_name_length,
       &reply) != CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK ||
       operations->serialize(operations->serialize_opaque, &test->tuple, &route,
       candidate.command_uuid, &bytes, &length) || !bytes || !length)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    if(test->original_copy_calls &&
       (length != sizeof("unchanged-original-record") ||
        memcmp(bytes,"unchanged-original-record",length)))
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER;
    test->prepared_calls++;
    if(test->outcome == FAKE_PREPARED) {
        report->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED;
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH;
    }
    test->publish_calls++;
    report->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED;
}

static void initialize_store_callbacks(fixture *test)
{
    test->store.lease_deadline = deadline;
    test->store.lease_deadline_opaque = test;
    test->store.command_uuid = generated_uuid;
    test->store.command_uuid_opaque = test;
    test->store.file_load = delegated_load;
    test->store.file_load_opaque = test;
    (void)character_save_journal_v2_player_store_set_absent_bootstrap(&test->store,
        absent_bootstrap, test);
}

static int test_exact_tuple_and_published_once(void)
{
    fixture test;
    int failed = 0;
    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    failed += expect(arm(&test) && attempt(&test,
        "55555555-5555-4555-8555-555555555555", test.actor, test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        test.capability.armed && !test.store.resolve_candidate,
        "only the exact descriptor command may begin");
    test.outcome = FAKE_PUBLISHED;
    failed += expect(attempt(&test, test.command, test.actor, test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED &&
        test.candidate_exact &&
        test.v4_calls == 1 && !test.v3_calls &&
        !test.capability.armed && !test.store.resolve_candidate &&
        !test.store.resolve_candidate_opaque && test.serializer_calls == 1 &&
        !test.original_copy_calls,
        "a published exact V4 candidate consumes its descriptor capability exactly once");
    return failed;
}

static int test_prepared_retains_same_command_retry(void)
{
    fixture test;
    int failed = 0;
    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    test.outcome = FAKE_PREPARED;
    failed += expect(arm(&test) && attempt(&test, test.command, test.actor,
        test.correlation, test.character, test.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED && test.prepared_calls == 1 &&
        !test.publish_calls && test.capability.armed && !test.store.resolve_candidate,
        "PREPARED retains the exact descriptor capability for its command retry");
    test.outcome = FAKE_PUBLISHED;
    failed += expect(attempt(&test, test.command, test.actor, test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED &&
        test.v4_calls == 2 && test.candidate_exact && !test.capability.armed &&
        !test.store.resolve_candidate,
        "the same command retries and consumes only after PUBLISHED");
    return failed;
}

static int test_claim_published_once(void)
{
    fixture test;
    int failed = 0;
    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    test.outcome = FAKE_PUBLISHED;
    failed += expect(arm_mode(&test, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) &&
        attempt_mode(&test, test.command, test.actor, test.correlation,
        test.character, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, test.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED &&
        !test.capability.armed && test.v4_calls == 1 && !test.v3_calls &&
        !test.store.resolve_candidate,
        "claim uses the same explicit V4 gate and consumes only on PUBLISHED");
    failed += expect(test.original_copy_calls == 1 && !test.serializer_calls &&
        !test.player.password[0] && !test.buffer[0],
        "claim preserves original bytes without serializing erased credentials and wipes scratch");
    return failed;
}

static int test_claim_prepared_retains_until_idle_retry(void)
{
    fixture test;
    int failed = 0;
    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    test.outcome = FAKE_PREPARED;
    failed += expect(arm_mode(&test, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) &&
        attempt_mode(&test, test.command, test.actor, test.correlation,
        test.character, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, test.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED && test.capability.armed &&
        !test.publish_calls && !test.store.resolve_candidate,
        "claim PREPARED retains its exact capability without completing or leaking resolver state");
    test.outcome = FAKE_PUBLISHED;
    failed += expect(attempt_mode(&test, test.command, test.actor,
        test.correlation, test.character, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM,
        test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED &&
        !test.capability.armed && test.v4_calls == 2 && test.publish_calls == 1 &&
        !test.store.resolve_candidate,
        "claim retry publishes before its caller may send ACTIVE or disconnect");
    return failed;
}

static int test_wrong_tuple_fails_closed(void)
{
    fixture test;
    char before[sizeof(test.buffer)];
    int failed = 0;
    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    memset(test.buffer, 'W', sizeof(test.buffer));
    memcpy(before, test.buffer, sizeof(before));
    test.outcome = FAKE_WRONG_TUPLE;
    failed += expect(arm(&test) && attempt(&test, test.command, test.actor,
        test.correlation, test.character, test.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED && test.v4_calls == 1 &&
        test.resolver_calls == 1 && !test.candidate_exact && !test.serializer_calls &&
        !test.route_calls && !test.prepared_calls && !test.publish_calls &&
        !memcmp(test.buffer, before, sizeof(before)) && test.capability.armed &&
        !test.store.resolve_candidate,
        "a mismatched writer tuple fails closed before caller-buffer or durable mutation");
    test.outcome = FAKE_WRONG_NAME;
    failed += expect(attempt(&test, test.command, test.actor, test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        test.v4_calls == 2 &&
        test.resolver_calls == 2 && !test.serializer_calls && !test.route_calls &&
        !test.prepared_calls && !test.publish_calls &&
        !memcmp(test.buffer, before, sizeof(before)) && test.capability.armed &&
        !test.store.resolve_candidate,
        "a substituted route name also fails closed without mutation");
    return failed;
}

static int test_v3_fallback_untouched(void)
{
    fixture test;
    int failed = 0;
    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    failed += expect(character_save_journal_v2_player_store_save(&test.store,
        test.name, &test.player) == PLAYER_STORE_OK && test.v3_calls == 1 &&
        !test.v4_calls && test.uuid_calls == 1 && test.serializer_calls == 1 &&
        !test.capability.armed && !test.store.resolve_candidate,
        "the dormant helper leaves ordinary V3 saves without a resolver");
    return failed;
}

static int test_exact_identity_feature_and_empty_capability_rejections(void)
{
    fixture test;
    onboarding_activation_save_capability empty;
    onboarding_activation_save_capability disabled;
    int failed = 0;

    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    memset(&empty, 0, sizeof(empty));
    failed += expect(onboarding_activation_save_runtime_helper_attempt(&test.owner,
        &empty, test.command, test.actor, test.correlation, test.character,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, test.name, test.name,
        &test.player) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        arm(&test) && attempt(&test, test.command,
        "12111111-1111-4111-8111-111111111111", test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        attempt(&test, test.command, test.actor,
        "23222222-2222-4222-8222-222222222222", test.character,
        test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        attempt(&test, test.command, test.actor, test.correlation,
        "34333333-3333-4333-8333-333333333333", test.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        attempt_mode(&test, test.command, test.actor, test.correlation,
        test.character, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, test.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        attempt(&test, test.command, test.actor, test.correlation,
        test.character, "Other") == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        test.capability.armed && !test.v4_calls && !test.store.resolve_candidate,
        "wrong command, actor, correlation, character, mode, name, or empty capability reject before save");
    unsetenv("MUD_M3_MODE");
    failed += expect(attempt(&test, test.command, test.actor, test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        test.capability.armed && !test.v4_calls && !test.store.resolve_candidate,
        "feature-off rejects without installing a resolver or consuming capability");
	memset(&disabled, 0, sizeof(disabled));
    failed += expect(onboarding_activation_save_capability_capture(&disabled,
        test.actor, test.correlation, test.character,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, test.command, test.name) ==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED && !disabled.armed &&
        onboarding_activation_gate_attempt(&disabled, test.command, test.actor,
        test.correlation, test.character,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, test.name, test.name,
        &test.player) == ONBOARDING_ACTIVATION_GATE_BYPASS &&
        !test.store.resolve_candidate,
        "feature-off capture bypasses the native gate without changing legacy save ownership");
    setenv("MUD_M3_MODE", "shadow", 1);
    return failed;
}

static int test_stale_resolver_rejects_without_dispatch(void)
{
    fixture test;
    int failed = 0;

    fixture_count = 0;
    setup(&test, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    initialize_store_callbacks(&test);
    failed += expect(arm(&test) &&
        !character_save_journal_v2_player_store_set_candidate_resolver(
            &test.store, stale_candidate_resolver, &test) &&
        attempt(&test, test.command, test.actor, test.correlation,
        test.character, test.name) == ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED &&
        test.capability.armed && test.store.resolve_candidate == stale_candidate_resolver &&
        test.store.resolve_candidate_opaque == &test && !test.v3_calls && !test.v4_calls &&
        !test.validate_calls && !test.renew_calls && !test.bootstrap_calls,
        "a stale resolver rejects before PlayerStore save dispatch and retains capability");
    return failed;
}

static int test_independent_fixtures_do_not_cross_contaminate(void)
{
    fixture alpha, beta;
    int failed = 0;
    fixture_count = 0;
    setup(&alpha, "11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333",
        "44444444-4444-4444-8444-444444444444", "Alpha", "m3-a",
        "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 7);
    setup(&beta, "12121212-1212-4212-8212-121212121212",
        "23232323-2323-4232-8232-232323232323",
        "34343434-3434-4434-8434-343434343434",
        "45454545-4545-4454-8454-454545454545", "Beta", "m3-b",
        "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 9);
    initialize_store_callbacks(&alpha);
    initialize_store_callbacks(&beta);
    alpha.outcome = FAKE_PUBLISHED;
    beta.outcome = FAKE_PUBLISHED;
    failed += expect(arm(&alpha) && arm(&beta) &&
        (onboarding_activation_gate_bind_owner(&alpha.owner,7), 1) &&
        attempt(&alpha, alpha.command, alpha.actor, alpha.correlation,
        alpha.character, alpha.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED &&
        (onboarding_activation_gate_bind_owner(&beta.owner,7), 1) && attempt(&beta,
        beta.command, beta.actor, beta.correlation, beta.character, beta.name) ==
        ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED && alpha.candidate_exact && beta.candidate_exact &&
        alpha.v4_calls == 1 && beta.v4_calls == 1 && alpha.route_calls == 1 &&
        beta.route_calls == 1 && !alpha.capability.armed && !beta.capability.armed &&
        !alpha.store.resolve_candidate && !beta.store.resolve_candidate,
        "independent descriptor fixtures keep their candidates, tuples, and consumption isolated");
    return failed;
}

int main(void)
{
    int failed;
    setenv("MUD_M3_MODE", "shadow", 1);
    setenv("MUD_M3_PLAYER_SNAPSHOT_V1", "handoff", 1);
    failed = test_exact_tuple_and_published_once() |
        test_prepared_retains_same_command_retry() | test_claim_published_once() |
        test_claim_prepared_retains_until_idle_retry() |
        test_wrong_tuple_fails_closed() | test_v3_fallback_untouched() |
        test_exact_identity_feature_and_empty_capability_rejections() |
        test_stale_resolver_rejects_without_dispatch() |
        test_independent_fixtures_do_not_cross_contaminate();
    unsetenv("MUD_M3_MODE");
    unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    if(failed) return 1;
    puts("onboarding_activation_save_v4_composition_test: ok");
    return 0;
}
