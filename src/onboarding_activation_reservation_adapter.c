/* Explicit caller-owned reservation to V4 bridge installation only. */
#include "onboarding_activation_reservation_adapter.h"

#include <string.h>

static int oara_same(const onboarding_activation_binding *left,
                     const onboarding_activation_binding *right)
{
    return left && right && !strcmp(left->actor_user_id,right->actor_user_id) &&
        !strcmp(left->correlation_id,right->correlation_id) &&
        !strcmp(left->character_id,right->character_id) &&
        left->mode == right->mode && !strcmp(left->command_id,right->command_id);
}

onboarding_activation_reservation_adapter_result
onboarding_activation_reservation_adapter_install(reservation_directory_fd,
    expected, capability, canonical_name, bridge_out)
int reservation_directory_fd;
const onboarding_activation_binding *expected;
onboarding_activation_save_capability *capability;
const char *canonical_name;
onboarding_activation_save_bridge *bridge_out;
{
    onboarding_snapshot_command_reservation retained;
    onboarding_activation_binding source;
    onboarding_snapshot_command_consumer_result reserve_result, read_result;

    if(bridge_out) memset(bridge_out,0,sizeof(*bridge_out));
    memset(&retained,0,sizeof(retained)); memset(&source,0,sizeof(source));
    if(reservation_directory_fd < 0 || !expected || !capability || !canonical_name ||
       !bridge_out) return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_INVALID;
    reserve_result=onboarding_snapshot_command_consumer_reserve(
        reservation_directory_fd,expected->command_id,expected->actor_user_id,
        expected->character_id,expected->mode,expected->correlation_id);
    if(reserve_result == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE)
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_NO_CANDIDATE;
    if(reserve_result == ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH)
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_TUPLE_MISMATCH;
    if(reserve_result != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED &&
       reserve_result != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY)
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_CONSUMER_FAILED;
    read_result=onboarding_snapshot_command_consumer_read(reservation_directory_fd,
        expected->command_id,&retained);
    if(read_result != ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED)
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_CONSUMER_FAILED;
    if(retained.state != ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED ||
       !oara_same(&retained.activation,expected))
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_TUPLE_MISMATCH;
    if(onboarding_activation_binding_read(retained.activation.command_id,&source) ||
       !oara_same(&source,&retained.activation))
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_SOURCE_BINDING_MISMATCH;
    if(onboarding_activation_save_bridge_begin(bridge_out,capability,
       retained.activation.command_id,retained.activation.actor_user_id,
       retained.activation.correlation_id,retained.activation.character_id,
       retained.activation.mode,canonical_name) != ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY)
        return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_BRIDGE_REJECTED;
    return ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_READY;
}
