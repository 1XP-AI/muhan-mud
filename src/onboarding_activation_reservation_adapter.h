#ifndef ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_H
#define ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_H

/* Caller-owned, one-attempt adapter from an already-open private reservation
 * descriptor and exact ACTIVATED binding into the existing V4 bridge.  It
 * never duplicates, closes, stores, or otherwise owns the descriptor. */
#include "onboarding_activation_save_bridge.h"
#include "onboarding_snapshot_command_consumer.h"

typedef enum onboarding_activation_reservation_adapter_result {
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_READY = 0,
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_NO_CANDIDATE = 1,
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_TUPLE_MISMATCH = 2,
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_SOURCE_BINDING_MISMATCH = 3,
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_BRIDGE_REJECTED = 4,
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_INVALID = -1,
    ONBOARDING_ACTIVATION_RESERVATION_ADAPTER_CONSUMER_FAILED = -2
} onboarding_activation_reservation_adapter_result;

/* Reserve and reread only the caller-selected command, then prove both its
 * retained reservation and current durable source binding still equal the
 * complete caller tuple before installing bridge_out.  The caller retains the
 * directory FD, capability, and bridge, and drives resolve/finish explicitly. */
onboarding_activation_reservation_adapter_result
onboarding_activation_reservation_adapter_install(
    int reservation_directory_fd,
    const onboarding_activation_binding *expected,
    onboarding_activation_save_capability *capability,
    const char *canonical_name,
    onboarding_activation_save_bridge *bridge_out);

#endif
