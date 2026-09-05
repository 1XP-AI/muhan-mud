#ifndef ONBOARDING_ACTIVATION_BINDING_H
#define ONBOARDING_ACTIVATION_BINDING_H

#include "onboarding_admission.h"

/* A command-keyed, durable non-secret acknowledgement binding.  This is
 * intentionally independent from M3 command consumption: the command UUID
 * is accepted from the already authenticated Gateway control lane only. */
typedef enum onboarding_activation_binding_mode {
    ONBOARDING_ACTIVATION_BINDING_MODE_INVALID = 0,
    ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION,
    ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM
} onboarding_activation_binding_mode;

typedef struct onboarding_activation_binding {
    char actor_user_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char correlation_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    char character_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
    onboarding_activation_binding_mode mode;
    char command_id[ONBOARDING_ADMISSION_UUID_LEN + 1];
} onboarding_activation_binding;

/* Command UUIDs are the only filename component.  The private record contains
 * exactly actor_user_id, correlation_id, character_id, mode, and command_id. */
int onboarding_activation_binding_path(const char *command_id, char *out,
                                       unsigned long out_size);
int onboarding_activation_binding_write(const char *actor_user_id,
                                        const char *correlation_id,
                                        const char *character_id,
                                        onboarding_activation_binding_mode mode,
                                        const char *command_id);
int onboarding_activation_binding_read(const char *command_id,
                                       onboarding_activation_binding *binding);

#endif
