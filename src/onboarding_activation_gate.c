#include "onboarding_activation_gate.h"

static character_save_journal_v2_process_owner *activation_gate_owner;

void onboarding_activation_gate_bind_owner(owner)
character_save_journal_v2_process_owner *owner;
{
    activation_gate_owner=owner;
}

void onboarding_activation_gate_unbind_owner(owner)
character_save_journal_v2_process_owner *owner;
{
    if(activation_gate_owner==owner) activation_gate_owner=0;
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

    if(!capability) return ONBOARDING_ACTIVATION_GATE_REJECTED;
    if(!capability->armed) return ONBOARDING_ACTIVATION_GATE_BYPASS;
    if(!activation_gate_owner) return ONBOARDING_ACTIVATION_GATE_REJECTED;
    result=onboarding_activation_save_runtime_helper_attempt(
        activation_gate_owner,capability,command_id,actor_user_id,
        correlation_id,character_id,mode,canonical_name,legacy_name,player);
    if(result==ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED)
        return ONBOARDING_ACTIVATION_GATE_CONSUMED;
    if(result==ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED)
        return ONBOARDING_ACTIVATION_GATE_RETAINED;
    return ONBOARDING_ACTIVATION_GATE_REJECTED;
}
