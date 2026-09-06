#include "onboarding_activation_reservation_owner.h"

static onboarding_activation_reservation_owner_result
onboarding_activation_reservation_owner_adapter_result(
    onboarding_activation_reservation_adapter_result result)
{
    if(result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_READY)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY;
    if(result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_NO_CANDIDATE)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_NO_CANDIDATE;
    if(result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_TUPLE_MISMATCH)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_TUPLE_MISMATCH;
    if(result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_SOURCE_BINDING_MISMATCH)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_SOURCE_BINDING_MISMATCH;
    if(result == ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_BRIDGE_REJECTED)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_BRIDGE_REJECTED;
    return ONBOARDING_ACTIVATION_RESERVATION_OWNER_ADAPTER_FAILED;
}

onboarding_activation_reservation_owner_result
onboarding_activation_reservation_owner_attempt(owner,
    reservation_directory_fd, expected, capability, canonical_name, bridge_out)
character_save_journal_v2_process_owner *owner;
int reservation_directory_fd;
const onboarding_activation_binding *expected;
onboarding_activation_save_capability *capability;
const char *canonical_name;
onboarding_activation_save_bridge *bridge_out;
{
    onboarding_activation_reservation_adapter_result adapter_result;

    if(!owner || reservation_directory_fd < 0 || !expected || !capability ||
       !canonical_name || !bridge_out)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT;
    if(owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY ||
       !owner->writer_held || !owner->player_store_installed)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_NOT_READY;
    if(owner->operation_active || owner->player_store.state !=
       CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE)
        return ONBOARDING_ACTIVATION_RESERVATION_OWNER_BUSY;

    owner->operation_active = 1;
    adapter_result=onboarding_activation_reservation_adapter_install(
        reservation_directory_fd,expected,capability,canonical_name,bridge_out);
    owner->operation_active = 0;
    return onboarding_activation_reservation_owner_adapter_result(adapter_result);
}
