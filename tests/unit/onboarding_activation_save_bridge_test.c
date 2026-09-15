/* Session-only explicit ACTIVATED save -> V4 candidate bridge contract. */
#include "onboarding_activation_save_bridge.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"
#define OTHER_COMMAND "55555555-5555-4555-8555-555555555555"

int player_name_is_valid(const unsigned char *name, unsigned long minimum,
                         unsigned long maximum)
{
    (void)minimum;
    return name && name[0] && strlen((const char *)name) <= maximum;
}

static int expect(int ok, const char *message)
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activation_save_bridge_test: %s\n", message);
    return 1;
}

static int armed(const onboarding_activation_save_capability *capability)
{ return capability && capability->armed; }

static int capture(onboarding_activation_save_capability *capability)
{
    return onboarding_activation_save_capability_capture(capability, ACTOR,
        CORRELATION, CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        COMMAND, "Alice");
}

static void writer(character_save_journal_v2_writer_tuple *tuple)
{
    memset(tuple, 0, sizeof(*tuple));
    strcpy(tuple->world_id, "m3-world");
    strcpy(tuple->writer_instance_id, "66666666-6666-4666-8666-666666666666");
    tuple->writer_epoch = 9;
}

static void route(character_save_journal_v2_bound_route_v3 *route_out,
                  const char *name, const char *character_id)
{
    memset(route_out, 0, sizeof(*route_out));
    strcpy(route_out->character_id, character_id);
    memcpy(route_out->legacy_name, name, strlen(name));
    route_out->legacy_name_length = strlen(name);
}

static int begin(onboarding_activation_save_bridge *bridge,
                 onboarding_activation_save_capability *capability)
{
    return onboarding_activation_save_bridge_begin(bridge, capability,
        COMMAND, ACTOR, CORRELATION, CHARACTER,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, "Alice");
}

static int test_success(void)
{
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_writer_tuple held;
    character_save_journal_v2_bound_route_v3 bound;
    character_save_journal_v2_protocol_candidate_v4 candidate;
    character_save_journal_v2_protocol_report report;
    int failed = 0;

    memset(&capability, 0, sizeof(capability));
    memset(&bridge, 0, sizeof(bridge));
    writer(&held); route(&bound, "Alice", CHARACTER);
    failed += expect(capture(&capability) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        begin(&bridge, &capability) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY &&
        onboarding_activation_save_bridge_resolve(&bridge, &held, &bound,
            (const unsigned char *)"Alice", 5, &candidate) == 1 &&
        !strcmp(candidate.command_uuid, COMMAND) &&
        !memcmp(&candidate.writer, &held, sizeof(held)) &&
        !strcmp(candidate.character_id, CHARACTER) &&
        candidate.canonical_legacy_name_length == 5 &&
        !memcmp(candidate.canonical_legacy_name, "Alice", 5),
        "the exact selected session tuple must become the V4 candidate");
    memset(&report, 0, sizeof(report));
    report.reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    failed += expect(onboarding_activation_save_bridge_finish(&bridge, &report) ==
        ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED && !armed(&capability),
        "only a published V4 save consumes the selected capability");
    return failed;
}

static int test_mismatch_rejected(void)
{
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_writer_tuple held;
    character_save_journal_v2_bound_route_v3 bound;
    character_save_journal_v2_protocol_candidate_v4 candidate;
    int failed = 0;

    memset(&capability, 0, sizeof(capability));
    memset(&bridge, 0, sizeof(bridge));
    failed += expect(capture(&capability) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        onboarding_activation_save_bridge_begin(&bridge, &capability, COMMAND,
            "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, "Alice") ==
            ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED && armed(&capability) &&
        onboarding_activation_save_bridge_begin(&bridge, &capability, COMMAND,
            ACTOR, CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM, "Alice") ==
            ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED && armed(&capability) &&
        onboarding_activation_save_bridge_begin(&bridge, &capability, OTHER_COMMAND,
            ACTOR, CORRELATION, CHARACTER,
            ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION, "Alice") ==
            ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED && armed(&capability),
        "actor mode or command substitution must be rejected without consumption");
    writer(&held); route(&bound, "Alice", CHARACTER);
    failed += expect(begin(&bridge, &capability) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY &&
        onboarding_activation_save_bridge_resolve(&bridge, &held, &bound,
            (const unsigned char *)"Alicf", 5, &candidate) < 0 &&
        onboarding_activation_save_bridge_resolve(&bridge, &held, &bound,
            (const unsigned char *)"Alice", 5, &candidate) == 1,
        "V4 route/name substitution must not be accepted by the selected bridge");
    memset(&candidate, 0, sizeof(candidate));
    return failed;
}

static int test_prepublish_retry(void)
{
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_protocol_report report;
    int failed = 0;

    memset(&capability, 0, sizeof(capability));
    memset(&bridge, 0, sizeof(bridge));
    failed += expect(capture(&capability) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        begin(&bridge, &capability) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY,
        "an exact explicit save starts with a retained capability");
    memset(&report, 0, sizeof(report));
    report.reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED;
    failed += expect(onboarding_activation_save_bridge_finish(&bridge, &report) ==
        ONBOARDING_ACTIVATION_SAVE_BRIDGE_RETAINED && armed(&capability) &&
        begin(&bridge, &capability) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY,
        "a pre-publish failure must preserve the exact command for retry");
    report.reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    failed += expect(onboarding_activation_save_bridge_finish(&bridge, &report) ==
        ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED && !armed(&capability),
        "the successful retry consumes only after PUBLISHED");
    return failed;
}

static int test_published_reuse_rejected(void)
{
    onboarding_activation_save_capability capability;
    onboarding_activation_save_bridge bridge;
    character_save_journal_v2_protocol_report report;
    int failed = 0;

    memset(&capability, 0, sizeof(capability));
    memset(&bridge, 0, sizeof(bridge));
    memset(&report, 0, sizeof(report));
    report.reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    failed += expect(capture(&capability) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        begin(&bridge, &capability) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY &&
        onboarding_activation_save_bridge_finish(&bridge, &report) ==
            ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED &&
        begin(&bridge, &capability) == ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED,
        "a published command must never become a second V4 candidate");
    return failed;
}

int main(void)
{
    int failed;
    setenv("MUD_M3_MODE", "shadow", 1);
    setenv("MUD_M3_PLAYER_SNAPSHOT_V1", "handoff", 1);
    failed = test_success() | test_mismatch_rejected() |
        test_prepublish_retry() | test_published_reuse_rejected();
    unsetenv("MUD_M3_MODE");
    unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    if(failed) return 1;
    puts("onboarding_activation_save_bridge_test: ok");
    return 0;
}
