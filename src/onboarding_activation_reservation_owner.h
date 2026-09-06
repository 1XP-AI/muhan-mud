#ifndef ONBOARDING_ACTIVATION_RESERVATION_OWNER_H
#define ONBOARDING_ACTIVATION_RESERVATION_OWNER_H

/*
 * Explicit, opt-in owner boundary for one ACTIVATED reservation attempt.  It
 * borrows the actual M3 process owner only to prove its safe lifecycle state;
 * the directory descriptor, activation capability, and bridge remain wholly
 * caller-owned and are passed through unchanged to the low-level adapter.
 */
#include "character_save_journal_v2_process_owner.h"
#include "onboarding_activation_reservation_adapter.h"

typedef enum onboarding_activation_reservation_owner_result {
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_READY = 0,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_INVALID_ARGUMENT = 1,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_NOT_READY = 2,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_BUSY = 3,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_NO_CANDIDATE = 4,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_TUPLE_MISMATCH = 5,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_SOURCE_BINDING_MISMATCH = 6,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_BRIDGE_REJECTED = 7,
    ONBOARDING_ACTIVATION_RESERVATION_OWNER_ADAPTER_FAILED = 8
} onboarding_activation_reservation_owner_result;

/*
 * Attempts the caller-owned adapter exactly once only while `owner` is READY,
 * has its writer held, has its V4 PlayerStore installed and IDLE, and has no
 * other operation in flight.  This API accepts no caller-provided lifecycle
 * flags; it raises owner->operation_active only across that adapter call and
 * does not retain, duplicate, close, or otherwise own any caller resource.
 */
onboarding_activation_reservation_owner_result
onboarding_activation_reservation_owner_attempt(
    character_save_journal_v2_process_owner *owner,
    int reservation_directory_fd,
    const onboarding_activation_binding *expected,
    onboarding_activation_save_capability *capability,
    const char *canonical_name,
    onboarding_activation_save_bridge *bridge_out);

#endif
