#ifndef ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_H
#define ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_H

#include "onboarding_activation_binding.h"

/* This remains a local, feature-off seam.  An activation binding is public
 * non-secret metadata to this module; it is the PENDING candidate.  A caller
 * supplies the already-open 0700 private reservation directory, so no
 * caller-controlled pathname is reopened while reserving a command. */
typedef enum onboarding_snapshot_command_reservation_state {
    ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_INVALID = 0,
    ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_PENDING,
    ONBOARDING_SNAPSHOT_COMMAND_RESERVATION_RESERVED
} onboarding_snapshot_command_reservation_state;

typedef struct onboarding_snapshot_command_reservation {
    onboarding_snapshot_command_reservation_state state;
    onboarding_activation_binding activation;
} onboarding_snapshot_command_reservation;

typedef enum onboarding_snapshot_command_consumer_result {
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_RESERVED = 0,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_EXACT_RETRY = 1,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_NO_CANDIDATE = 2,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_TUPLE_MISMATCH = 3,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CONFLICT = 4,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_INVALID = -1,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_CORRUPT = -2,
    ONBOARDING_SNAPSHOT_COMMAND_CONSUMER_IO_ERROR = -3
} onboarding_snapshot_command_consumer_result;

/* Consume exactly the pending activation metadata named by command_id.  No
 * lookup by character, correlation, or save candidate exists: every caller
 * must present the command id plus the exact expected tuple.  A complete
 * RESERVED record is hard-linked into the supplied directory and retained;
 * neither this call nor the reader deletes the activation source binding. */
onboarding_snapshot_command_consumer_result
onboarding_snapshot_command_consumer_reserve(
    int reservation_directory_fd,
    const char *command_id,
    const char *expected_character_id,
    onboarding_activation_binding_mode expected_mode,
    const char *expected_correlation_id);

/* Read only the retained local reservation from the supplied private
 * descriptor.  This has no fallback search and cannot choose an id. */
onboarding_snapshot_command_consumer_result
onboarding_snapshot_command_consumer_read(
    int reservation_directory_fd,
    const char *command_id,
    onboarding_snapshot_command_reservation *reservation);

#endif
