/* The production activation gate must keep reservation wiring dormant until
 * an explicit, armed ACTIVATED capability reaches its bound native owner. */
#include "onboarding_activation_gate.h"
#include "mstruct.h"

#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

#define ACTOR "11111111-1111-4111-8111-111111111111"
#define CORRELATION "22222222-2222-4222-8222-222222222222"
#define CHARACTER "33333333-3333-4333-8333-333333333333"
#define COMMAND "44444444-4444-4444-8444-444444444444"

static int reservation_calls;
static int helper_calls;
static int expected_fd;
static onboarding_activation_reservation_owner_result reservation_result;
static onboarding_activation_save_runtime_helper_result helper_result;

static int expect(int ok, const char *message)
{
    if(ok) return 0;
    fprintf(stderr, "onboarding_activation_owner_runtime_test: %s\n", message);
    return 1;
}

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
    reservation_calls++;
    if(!owner || directory_fd != expected_fd || !expected || !capability ||
       !canonical_name || strcmp(canonical_name, "Alice") || !bridge_out ||
       strcmp(expected->actor_user_id, ACTOR) ||
       strcmp(expected->correlation_id, CORRELATION) ||
       strcmp(expected->character_id, CHARACTER) ||
       expected->mode != ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION ||
       strcmp(expected->command_id, COMMAND))
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT;
    if(reservation_result == ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY)
        bridge_out->active=1;
    return reservation_result;
}

onboarding_activation_save_runtime_helper_result
onboarding_activation_save_runtime_helper_attempt_bridge(owner, bridge,
    legacy_name, player)
character_save_journal_v2_process_owner *owner;
onboarding_activation_save_bridge *bridge;
char *legacy_name;
struct creature *player;
{
    helper_calls++;
    if(!owner || !bridge || !bridge->active || !legacy_name ||
       strcmp(legacy_name, "Alice") || !player)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
    return helper_result;
}

static void arm(onboarding_activation_save_capability *capability)
{
    memset(capability, 0, sizeof(*capability));
    capability->armed=1;
    strcpy(capability->record.actor_user_id, ACTOR);
    strcpy(capability->record.correlation_id, CORRELATION);
    strcpy(capability->record.character_id, CHARACTER);
    capability->record.mode=ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION;
    strcpy(capability->record.command_id, COMMAND);
    strcpy(capability->record.canonical_name, "Alice");
}

int main(void)
{
    character_save_journal_v2_process_owner owner;
    onboarding_activation_save_capability capability;
    struct creature player;
    int failed=0;

    memset(&owner,0,sizeof(owner)); memset(&player,0,sizeof(player));
    expected_fd=open(".",O_RDONLY);
    if(expected_fd<0) return 1;
    reservation_result=ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY;
    helper_result=ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED;
    onboarding_activation_gate_bind_owner(&owner,expected_fd);

    memset(&capability,0,sizeof(capability)); reservation_calls=helper_calls=0;
    failed+=expect(onboarding_activation_gate_attempt(&capability,COMMAND,ACTOR,
        CORRELATION,CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        "Alice","Alice",&player)==ONBOARDING_ACTIVATION_GATE_BYPASS &&
        !reservation_calls && !helper_calls && fcntl(expected_fd,F_GETFD)>=0,
        "an absent capability bypasses without reserving, bridging, or closing its descriptor");

    arm(&capability); reservation_calls=helper_calls=0;
    failed+=expect(onboarding_activation_gate_attempt(&capability,COMMAND,ACTOR,
        CORRELATION,CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        "Alice","Alice",&player)==ONBOARDING_ACTIVATION_GATE_CONSUMED &&
        reservation_calls==1 && helper_calls==1 && fcntl(expected_fd,F_GETFD)>=0,
        "one explicit armed ACTIVATED capability reaches the owner reservation boundary once");

    arm(&capability); reservation_result=ONBOARDING_ACTIVATION_RESERVATION_OWNER_NO_CANDIDATE;
    reservation_calls=helper_calls=0;
    failed+=expect(onboarding_activation_gate_attempt(&capability,COMMAND,ACTOR,
        CORRELATION,CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        "Alice","Alice",&player)==ONBOARDING_ACTIVATION_GATE_REJECTED &&
        reservation_calls==1 && !helper_calls && fcntl(expected_fd,F_GETFD)>=0,
        "a reservation failure reaches neither bridge dispatch nor legacy completion");

    onboarding_activation_gate_unbind_owner(&owner);
    arm(&capability); reservation_calls=helper_calls=0;
    failed+=expect(onboarding_activation_gate_attempt(&capability,COMMAND,ACTOR,
        CORRELATION,CHARACTER,ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
        "Alice","Alice",&player)==ONBOARDING_ACTIVATION_GATE_REJECTED &&
        !reservation_calls && !helper_calls,
        "an armed activation without the native owner binding cannot reserve or save");
    close(expected_fd);
    if(failed) return 1;
    puts("onboarding_activation_owner_runtime_test: ok");
    return 0;
}
