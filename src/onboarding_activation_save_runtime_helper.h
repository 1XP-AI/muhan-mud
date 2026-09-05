#ifndef ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_H
#define ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_H

/*
 * Caller-owned, dormant composition for exactly one explicit activation save.
 * It deliberately does not bind a PlayerStore, alter process-owner startup,
 * or select any command/lifecycle ordering.  A caller supplies the already
 * active owner and descriptor-local capability at its chosen safe boundary.
 */
#include "character_save_journal_v2_process_owner.h"
#include "onboarding_activation_save_bridge.h"

typedef enum onboarding_activation_save_runtime_helper_result {
    ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED = 0,
    ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED = 1,
    ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED = 2,
    ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_INVALID = -1
} onboarding_activation_save_runtime_helper_result;

/*
 * Begin, resolve, dispatch, clear, and finish exactly one selected V4 save.
 * The resolver is installed only for the synchronous PlayerStore call and is
 * cleared on every post-install exit.  PUBLISHED consumes the capability;
 * PREPARED retains it for a retry; malformed/ineligible requests and bridge
 * resolver substitutions are rejected without consuming it.
 */
onboarding_activation_save_runtime_helper_result
onboarding_activation_save_runtime_helper_attempt(
    character_save_journal_v2_process_owner *owner,
    onboarding_activation_save_capability *capability,
    const char *command_id, const char *actor_user_id,
    const char *correlation_id, const char *character_id,
    onboarding_activation_binding_mode mode, const char *canonical_name,
    char *legacy_name, struct creature *player);

#endif
