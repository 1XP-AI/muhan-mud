/* Dynamic command1 activation lifecycle harness.  It links the command
 * lifecycle seam with small socket/world fakes; the M3 gate remains a real
 * object and only its already-owned runtime helper is faked. */
#include "mstruct.h"
#include "mextern.h"
#include "onboarding_activation_gate.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern void onboarding_activation_command_test_deliver_activated(int fd,
    const char *command_id);
#ifdef USE_M3_RUNTIME
extern void onboarding_activation_command_test_idle_retry(void);
#endif

#ifdef USE_M3_RUNTIME
typedef enum fake_save_outcome {
    FAKE_SAVE_PUBLISHED,
    FAKE_SAVE_PREPARED,
    FAKE_SAVE_REJECTED
} fake_save_outcome;
#endif

typedef struct fixture {
    creature player;
    iobuf io;
    extra ext;
    int active_count;
    int activation_count;
    int disconnect_count;
    int save_count;
    int binding_count;
} fixture;

static fixture *active_fixture;
#ifdef USE_M3_RUNTIME
static fake_save_outcome save_outcome;
#endif
static const char *expected_command;

static int expect(int ok, const char *message)
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activation_command_lifecycle_test: %s\n",
        message);
    return 1;
}

int scwrite(fd, bytes, length)
int fd;
const void *bytes;
unsigned int length;
{
    (void)fd;
    if(active_fixture && length >= 12 &&
       !memcmp(bytes, "MUD1O ACTIVE", 12)) active_fixture->active_count++;
    return (int)length;
}

int activate_staged_ply(player)
creature *player;
{
    if(!active_fixture || player != &active_fixture->player) return -1;
    active_fixture->activation_count++;
    return 0;
}

void disconnect(fd)
int fd;
{
    (void)fd;
    if(active_fixture) active_fixture->disconnect_count++;
}

void print(fd, fmt)
int fd;
unsigned char *fmt;
{ (void)fd; (void)fmt; }

void onboarding_session_zeroize_claim_memory(password, password_size, input,
    input_size)
void *password;
unsigned long password_size;
void *input;
unsigned long input_size;
{ (void)password; (void)password_size; (void)input; (void)input_size; }

int player_name_is_valid(name, min_codepoints, max_codepoints)
const unsigned char *name;
unsigned long min_codepoints;
unsigned long max_codepoints;
{
    return name && !strcmp((const char *)name, "alpha") &&
        min_codepoints == PLAYER_NAME_MIN_CODEPOINTS &&
        max_codepoints == PLAYER_NAME_MAX_CODEPOINTS;
}

int onboarding_activation_binding_write(actor, correlation, character, mode,
    command_id)
const char *actor;
const char *correlation;
const char *character;
onboarding_activation_binding_mode mode;
const char *command_id;
{
    if(!active_fixture || !actor || !correlation || !character || !command_id ||
       (mode != ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        mode != ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM)) return -1;
    active_fixture->binding_count++;
    return 0;
}

#ifdef USE_M3_RUNTIME
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
    if(!active_fixture || !owner || directory_fd != 7 || !expected ||
       !capability || !canonical_name || !bridge_out || !expected_command ||
       strcmp(expected->command_id, expected_command))
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_ADAPTER_FAILED;
    bridge_out->active=1;
    bridge_out->capability=capability;
    return ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY;
}

onboarding_activation_save_runtime_helper_result
onboarding_activation_save_runtime_helper_attempt_bridge(owner, bridge,
    legacy_name, player)
character_save_journal_v2_process_owner *owner;
onboarding_activation_save_bridge *bridge;
char *legacy_name;
struct creature *player;
{
    (void)owner; (void)legacy_name; (void)player;
    if(!active_fixture || !bridge || !bridge->active || !expected_command)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
    active_fixture->save_count++;
    if(save_outcome == FAKE_SAVE_PREPARED)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED;
    if(save_outcome == FAKE_SAVE_REJECTED)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
    onboarding_activation_save_capability_clear(bridge->capability);
    return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED;
}
#endif

static void setup(fixture *test, onboarding_activation_binding_mode mode)
{
    memset(test, 0, sizeof(*test));
    memset(Ply, 0, sizeof(Ply));
    active_fixture=test;
    strcpy(test->player.name, "alpha");
    test->player.fd=-1;
    strcpy(test->ext.onboarding_actor_id,
        "11111111-1111-4111-8111-111111111111");
    strcpy(test->ext.onboarding_correlation_id,
        "22222222-2222-4222-8222-222222222222");
    strcpy(test->ext.onboarding_character_id,
        "33333333-3333-4333-8333-333333333333");
    test->ext.onboarding_mode=mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM ?
        ONBOARDING_ADMISSION_MODE_CLAIM:ONBOARDING_ADMISSION_MODE_PROVISION;
    test->ext.onboarding_state=mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM ?
        ONBOARDING_STATE_CLAIM_AWAIT_ACTIVATED:
        ONBOARDING_STATE_PROVISION_AWAIT_ACTIVATED;
    test->ext.onboarding_world_staged=
        mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    test->io.fn=mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM ?
        onboarding_claim:onboarding_provision;
    test->io.fnparam=mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM ? 6:7;
    Ply[0].ply=&test->player;
    Ply[0].io=&test->io;
    Ply[0].extr=&test->ext;
    expected_command="44444444-4444-4444-8444-444444444444";
}

static int completed(const fixture *test, onboarding_activation_binding_mode mode)
{
    return test->ext.onboarding_mode == 0 &&
        !test->ext.onboarding_activation_pending &&
        !test->ext.onboarding_activation_command_id[0] &&
        test->active_count == 1 &&
        (mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION ?
         test->activation_count == 1 && !test->disconnect_count &&
         !test->ext.onboarding_world_staged:
         !test->activation_count && test->disconnect_count == 1);
}

static void deliver_activated(command_id)
const char *command_id;
{ onboarding_activation_command_test_deliver_activated(0, command_id); }

#ifdef USE_M3_RUNTIME
static int test_runtime_mode(onboarding_activation_binding_mode mode)
{
    fixture test;
    int failed=0;
    character_save_journal_v2_process_owner owner;

    failed+=expect(setenv("MUD_M3_MODE", "shadow", 1) == 0 &&
        setenv("MUD_M3_PLAYER_SNAPSHOT_V1", "handoff", 1) == 0,
        "runtime activation fixture must enable its explicit feature flags");
    memset(&owner, 0, sizeof(owner));
    onboarding_activation_gate_bind_owner(&owner,7);

    setup(&test, mode);
    save_outcome=FAKE_SAVE_PUBLISHED;
    deliver_activated(expected_command);
    failed+=expect(test.save_count == 1 && test.binding_count == 1 &&
        completed(&test, mode),
        "PUBLISHED must emit one ACTIVE and complete the command lifecycle");
    onboarding_activation_command_test_idle_retry();
    failed+=expect(test.active_count == 1 && test.save_count == 1,
        "a completed activation must not replay ACTIVE from idle");

    setup(&test, mode);
    save_outcome=FAKE_SAVE_PREPARED;
    deliver_activated(expected_command);
    failed+=expect(test.save_count == 1 && test.binding_count == 1 &&
        !test.active_count && test.ext.onboarding_activation_pending &&
        test.ext.onboarding_mode != 0,
        "PREPARED must suppress early ACTIVE and retain the command for idle");
    save_outcome=FAKE_SAVE_PUBLISHED;
    onboarding_activation_command_test_idle_retry();
    failed+=expect(test.save_count == 2 && completed(&test, mode),
        "the serialized idle retry must publish and complete exactly once");
    onboarding_activation_command_test_idle_retry();
    failed+=expect(test.active_count == 1 && test.save_count == 2,
        "the post-PUBLISHED idle boundary must not complete twice");

    setup(&test, mode);
    save_outcome=FAKE_SAVE_PUBLISHED;
    deliver_activated("55555555-5555-4555-8555-555555555555");
    failed+=expect(!test.save_count && !test.active_count &&
        test.ext.onboarding_mode != 0 && !test.ext.onboarding_activation_pending,
        "a command mismatch must emit neither ACTIVE nor completion");

    setup(&test, mode);
    save_outcome=FAKE_SAVE_REJECTED;
    deliver_activated(expected_command);
    failed+=expect(
        test.save_count == 1 && !test.active_count && test.ext.onboarding_mode != 0 &&
        !test.ext.onboarding_activation_pending,
        "a gate error must emit neither ACTIVE nor completion");
    onboarding_activation_gate_unbind_owner(&owner);
    return failed;
}

static int test_runtime_feature_off(onboarding_activation_binding_mode mode)
{
    fixture test;

    unsetenv("MUD_M3_MODE");
    unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    setup(&test, mode);
    save_outcome=FAKE_SAVE_REJECTED;
    deliver_activated(expected_command);
    return expect(!test.save_count && test.binding_count == 1 &&
        completed(&test, mode),
        "feature-off must retain the legacy completion path");
}
#else
static int test_legacy_mode(onboarding_activation_binding_mode mode)
{
    fixture test;
    setup(&test, mode);
    deliver_activated(expected_command);
    return expect(!test.save_count && test.binding_count == 1 &&
        completed(&test, mode),
        "USE_M3_RUNTIME off must retain the legacy completion path");
}
#endif

int main(void)
{
    int failed=0;
#ifdef USE_M3_RUNTIME
    failed+=test_runtime_mode(ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION);
    failed+=test_runtime_mode(ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM);
    failed+=test_runtime_feature_off(ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION);
    failed+=test_runtime_feature_off(ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM);
#else
    failed+=test_legacy_mode(ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION);
    failed+=test_legacy_mode(ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM);
#endif
    if(failed) return 1;
    puts("onboarding_activation_command_lifecycle_test: ok");
    return 0;
}
