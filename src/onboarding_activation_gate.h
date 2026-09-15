#ifndef ONBOARDING_ACTIVATION_GATE_H
#define ONBOARDING_ACTIVATION_GATE_H

/* Host-owned, one-owner dispatcher for the explicit M3 activation save.  It
 * owns neither transport nor writer resources; main binds the already-live
 * native process owner and command1 supplies descriptor-local state. */
#include "onboarding_activation_reservation_owner.h"
#include "onboarding_activation_save_runtime_helper.h"

typedef enum onboarding_activation_gate_result {
    ONBOARDING_ACTIVATION_GATE_BYPASS = 0,
    ONBOARDING_ACTIVATION_GATE_CONSUMED = 1,
    ONBOARDING_ACTIVATION_GATE_RETAINED = 2,
    ONBOARDING_ACTIVATION_GATE_REJECTED = 3
} onboarding_activation_gate_result;

void onboarding_activation_gate_bind_owner(
    character_save_journal_v2_process_owner *owner,
    int reservation_directory_fd);
void onboarding_activation_gate_unbind_owner(
    character_save_journal_v2_process_owner *owner);

/* A disarmed capability is the feature-off/probe legacy path.  An armed
 * capability must be dispatched through the currently bound READY owner. */
onboarding_activation_gate_result onboarding_activation_gate_attempt(
    onboarding_activation_save_capability *capability, const char *command_id,
    const char *actor_user_id, const char *correlation_id,
    const char *character_id, onboarding_activation_binding_mode mode,
    const char *canonical_name, char *legacy_name, struct creature *player);

#endif
