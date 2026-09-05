/* Session-local, explicit-only post-ACTIVATED save capability contract. */
#include "onboarding_activation_save_capability.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"
#define OTHER_COMMAND "55555555-5555-4555-8555-555555555555"

static int expect(ok, message)
int ok;
const char *message;
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activation_save_capability_test: %s\n", message);
    return 1;
}

/* The production dependency owns UTF-8 legacy-name validation; this focused
 * seam test supplies its established success result without a file-path link. */
int player_name_is_valid(name, min_cp, max_cp)
const unsigned char *name;
unsigned long min_cp;
unsigned long max_cp;
{
    (void)min_cp;
    (void)max_cp;
    return name && name[0] && strlen((const char *)name) <= PLAYER_NAME_MAX_BYTES;
}

static int empty(capability)
const onboarding_activation_save_capability *capability;
{
    onboarding_activation_save_capability zero;
    memset(&zero, 0, sizeof(zero));
    return !memcmp(capability, &zero, sizeof(zero));
}

static int capture(capability, mode)
onboarding_activation_save_capability *capability;
onboarding_activation_binding_mode mode;
{
    return onboarding_activation_save_capability_capture(capability, ACTOR,
        CORRELATION, CHARACTER, mode, COMMAND, "Alice");
}

int main(void)
{
    onboarding_activation_save_capability capability;
    onboarding_activation_save_capability_record record;
    int failed;

    failed = 0;
    unsetenv("MUD_M3_MODE");
    unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    memset(&capability, 0x5a, sizeof(capability));
    failed += expect(capture(&capability,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION) ==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED && empty(&capability),
        "feature-off ACTIVATED must capture nothing");
    setenv("MUD_M3_MODE", "shadow", 1);
    failed += expect(capture(&capability,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION) ==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED && empty(&capability),
        "shadow alone must not enable the session capability");
    setenv("MUD_M3_PLAYER_SNAPSHOT_V1", "handoff", 1);
    failed += expect(capture(&capability,
        ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION) ==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK,
        "accepted provision ACTIVATED must capture the exact tuple");
    memset(&record, 0, sizeof(record));
    failed += expect(onboarding_activation_save_capability_consume_for_explicit_save(
        &capability, COMMAND, &record) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        !strcmp(record.actor_user_id, ACTOR) &&
        !strcmp(record.correlation_id, CORRELATION) &&
        !strcmp(record.character_id, CHARACTER) &&
        record.mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        !strcmp(record.command_id, COMMAND) && !strcmp(record.canonical_name, "Alice") &&
        empty(&capability),
        "the one-shot future save call must receive only the exact captured tuple");
    failed += expect(onboarding_activation_save_capability_consume_for_explicit_save(
        &capability, COMMAND, &record) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_UNAVAILABLE,
        "a consumed capability must not arm a later arbitrary save");

    memset(&capability, 0, sizeof(capability));
    failed += expect(capture(&capability,
        ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
        onboarding_activation_save_capability_consume_for_explicit_save(
            &capability, OTHER_COMMAND, &record) ==
            ONBOARDING_ACTIVATION_SAVE_CAPABILITY_UNAVAILABLE && empty(&capability),
        "a mismatched future command must fail closed and consume nothing reusable");
    failed += expect(capture(&capability,
        ONBOARDING_ACTIVATION_BINDING_MODE_INVALID) ==
        ONBOARDING_ACTIVATION_SAVE_CAPABILITY_INVALID && empty(&capability),
        "malformed activation state must not create a capability");
    failed += expect(onboarding_activation_save_capability_capture(&capability, "bad",
        CORRELATION, CHARACTER, ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM,
        COMMAND, "Alice") == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_INVALID &&
        empty(&capability), "malformed activation identifiers must fail closed");

    failed += expect(capture(&capability,
        ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK,
        "claim success may create a new session-local capability");
    onboarding_activation_save_capability_clear(&capability);
    failed += expect(onboarding_activation_save_capability_consume_for_explicit_save(
        &capability, COMMAND, &record) == ONBOARDING_ACTIVATION_SAVE_CAPABILITY_UNAVAILABLE &&
        empty(&capability),
        "disconnect/claim cleanup must leave no capability to revive");

    unsetenv("MUD_M3_MODE");
    unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    if(failed) return 1;
    puts("onboarding_activation_save_capability_test: ok");
    return 0;
}
