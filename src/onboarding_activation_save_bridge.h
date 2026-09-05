#ifndef ONBOARDING_ACTIVATION_SAVE_BRIDGE_H
#define ONBOARDING_ACTIVATION_SAVE_BRIDGE_H

/* A caller-owned, one-attempt bridge between a descriptor-local ACTIVATED
 * capability and the generic V4 candidate resolver.  It has no registry,
 * pathname, recovery, or live PlayerStore ownership. */
#include "character_save_journal_v2_protocol.h"
#include "onboarding_activation_save_capability.h"

typedef struct onboarding_activation_save_bridge {
    onboarding_activation_save_capability *capability;
    onboarding_activation_save_capability_record selected;
    unsigned char active;
} onboarding_activation_save_bridge;

typedef enum onboarding_activation_save_bridge_result {
    ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY = 0,
    ONBOARDING_ACTIVATION_SAVE_BRIDGE_RETAINED = 1,
    ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED = 2,
    ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED = 3,
    ONBOARDING_ACTIVATION_SAVE_BRIDGE_INVALID = -1
} onboarding_activation_save_bridge_result;

/* Select only an already captured command from this descriptor session.  All
 * identity fields and the canonical name are exact comparisons; this function
 * performs no character/correlation/name search or durable rehydration. */
onboarding_activation_save_bridge_result
onboarding_activation_save_bridge_begin(
    onboarding_activation_save_bridge *bridge,
    onboarding_activation_save_capability *capability,
    const char *command_id, const char *actor_user_id,
    const char *correlation_id, const char *character_id,
    onboarding_activation_binding_mode mode, const char *canonical_name);

/* Pass this callback only to the selected V4 save attempt.  It emits the
 * captured command as the V4 candidate and rejects route/name substitution. */
int onboarding_activation_save_bridge_resolve(
    void *opaque, const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route_v3 *route,
    const unsigned char *canonical_legacy_name,
    size_t canonical_legacy_name_length,
    character_save_journal_v2_protocol_candidate_v4 *candidate_out);

/* End an attempt.  PREPARED and every earlier outcome retain the capability
 * for an explicit retry; PUBLISHED is the sole point that consumes it. */
onboarding_activation_save_bridge_result
onboarding_activation_save_bridge_finish(
    onboarding_activation_save_bridge *bridge,
    const character_save_journal_v2_protocol_report *report);

#endif
