#include "onboarding_activation_gate.h"

#include <string.h>

static character_save_journal_v2_process_owner *activation_gate_owner;
static int activation_gate_reservation_directory_fd=-1;

static int activation_gate_copy_uuid(destination, source)
char *destination;
const char *source;
{
    const char *ending;
    unsigned long length;

    if(!destination || !source) return -1;
    ending=(const char *)memchr(source,0,ONBOARDING_ADMISSION_UUID_LEN+1U);
    if(!ending) return -1;
    length=(unsigned long)(ending-source);
    if(length!=ONBOARDING_ADMISSION_UUID_LEN) return -1;
    memcpy(destination,source,length+1U);
    return 0;
}

void onboarding_activation_gate_bind_owner(owner, reservation_directory_fd)
character_save_journal_v2_process_owner *owner;
int reservation_directory_fd;
{
    activation_gate_owner=owner;
    activation_gate_reservation_directory_fd=reservation_directory_fd;
}

void onboarding_activation_gate_unbind_owner(owner)
character_save_journal_v2_process_owner *owner;
{
    if(activation_gate_owner==owner) {
        activation_gate_owner=0;
        activation_gate_reservation_directory_fd=-1;
    }
}

onboarding_activation_gate_result
onboarding_activation_gate_attempt(capability, command_id, actor_user_id,
    correlation_id, character_id, mode, canonical_name, legacy_name, player)
onboarding_activation_save_capability *capability;
const char *command_id;
const char *actor_user_id;
const char *correlation_id;
const char *character_id;
onboarding_activation_binding_mode mode;
const char *canonical_name;
char *legacy_name;
struct creature *player;
{
    onboarding_activation_save_runtime_helper_result result;
    onboarding_activation_reservation_owner_result reservation_result;
    onboarding_activation_binding expected;
    onboarding_activation_save_bridge bridge;

    if(!capability) return ONBOARDING_ACTIVATION_GATE_REJECTED;
    if(!capability->armed) return ONBOARDING_ACTIVATION_GATE_BYPASS;
    if(!activation_gate_owner || activation_gate_reservation_directory_fd<0 ||
       !canonical_name ||
       activation_gate_copy_uuid(expected.actor_user_id,actor_user_id) ||
       activation_gate_copy_uuid(expected.correlation_id,correlation_id) ||
       activation_gate_copy_uuid(expected.character_id,character_id) ||
       activation_gate_copy_uuid(expected.command_id,command_id) ||
       (mode!=ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION &&
        mode!=ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM))
        return ONBOARDING_ACTIVATION_GATE_REJECTED;
    expected.mode=mode;
    memset(&bridge,0,sizeof(bridge));
    reservation_result=onboarding_activation_reservation_owner_attempt(
        activation_gate_owner,activation_gate_reservation_directory_fd,&expected,
        capability,canonical_name,&bridge);
    if(reservation_result!=ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY)
        return ONBOARDING_ACTIVATION_GATE_REJECTED;
    result=onboarding_activation_save_runtime_helper_attempt_bridge(
        activation_gate_owner,&bridge,legacy_name,player);
    if(result==ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED)
        return ONBOARDING_ACTIVATION_GATE_CONSUMED;
    if(result==ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED)
        return ONBOARDING_ACTIVATION_GATE_RETAINED;
    return ONBOARDING_ACTIVATION_GATE_REJECTED;
}
