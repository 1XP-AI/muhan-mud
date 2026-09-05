#ifndef ONBOARDING_ACTIVATION_SAVE_CAPABILITY_H
#define ONBOARDING_ACTIVATION_SAVE_CAPABILITY_H

#include "onboarding_activation_binding.h"
#include "player_path.h"

/* This is an in-memory, descriptor-owned proof that one already accepted
 * ACTIVATED command may later be presented to one explicitly selected save
 * call.  It is neither a resolver nor authority for an ordinary save. */
typedef struct onboarding_activation_save_capability_record {
    char actor_user_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char correlation_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char character_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    onboarding_activation_binding_mode mode;
    char command_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char canonical_name[PLAYER_NAME_MAX_BYTES + 1];
} onboarding_activation_save_capability_record;

typedef struct onboarding_activation_save_capability {
    unsigned char armed;
    onboarding_activation_save_capability_record record;
} onboarding_activation_save_capability;

typedef enum onboarding_activation_save_capability_status {
    ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK = 0,
    ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED = 1,
    ONBOARDING_ACTIVATION_SAVE_CAPABILITY_INVALID = -1,
    ONBOARDING_ACTIVATION_SAVE_CAPABILITY_UNAVAILABLE = -2
} onboarding_activation_save_capability_status;

void onboarding_activation_save_capability_clear(
    onboarding_activation_save_capability *capability);

/* Capture is allowed only for literal MUD_M3_MODE=shadow plus
 * MUD_M3_PLAYER_SNAPSHOT_V1=handoff, after the Gateway control state machine
 * has accepted ACTIVATED.  Every rejected input clears the target. */
onboarding_activation_save_capability_status
onboarding_activation_save_capability_capture(
    onboarding_activation_save_capability *capability, const char *actor_user_id,
    const char *correlation_id, const char *character_id,
    onboarding_activation_binding_mode mode, const char *command_id,
    const char *canonical_name);

/* A future integration must name the exact captured command and consumes this
 * session object in the same call.  No ordinary save can discover or reuse it. */
onboarding_activation_save_capability_status
onboarding_activation_save_capability_consume_for_explicit_save(
    onboarding_activation_save_capability *capability, const char *command_id,
    onboarding_activation_save_capability_record *record);

/* Inspect the exact session proof without consuming it.  An explicit-save
 * bridge uses this before V4 has reached PUBLISHED, then calls the consuming
 * operation above only after that durable publication edge. */
onboarding_activation_save_capability_status
onboarding_activation_save_capability_peek_for_explicit_save(
    const onboarding_activation_save_capability *capability,
    const char *command_id, onboarding_activation_save_capability_record *record);

/* The bridge calls this only after V4 PUBLISHED.  Unlike the legacy
 * command-only helper, all captured identity fields and the canonical name
 * must still match at the consumption edge. */
onboarding_activation_save_capability_status
onboarding_activation_save_capability_consume_published_explicit_save(
    onboarding_activation_save_capability *capability, const char *command_id,
    const char *actor_user_id, const char *correlation_id,
    const char *character_id, onboarding_activation_binding_mode mode,
    const char *canonical_name,
    onboarding_activation_save_capability_record *record);

#endif
