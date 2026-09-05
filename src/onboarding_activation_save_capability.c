/* Explicit-only, session-local post-ACTIVATED save capability. */
#include "onboarding_activation_save_capability.h"

#include <stdlib.h>
#include <string.h>

static unsigned long oasc_bounded(value, limit)
const char *value;
unsigned long limit;
{
    unsigned long length;
    if(!value) return limit + 1U;
    for(length = 0; length <= limit; length++)
        if(!value[length]) return length;
    return limit + 1U;
}

static int oasc_hex(value)
char value;
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }

static int oasc_uuid(value)
const char *value;
{
    unsigned long i;
    if(oasc_bounded(value, ONBOARDING_ADMISSION_UUID_LEN) !=
       ONBOARDING_ADMISSION_UUID_LEN) return 0;
    for(i = 0; i < ONBOARDING_ADMISSION_UUID_LEN; i++)
        if(i == 8U || i == 13U || i == 18U || i == 23U) {
            if(value[i] != '-') return 0;
        }
        else if(!oasc_hex(value[i])) return 0;
    return 1;
}

static int oasc_mode(mode)
onboarding_activation_binding_mode mode;
{
    return mode == ONBOARDING_ACTIVATION_BINDING_MODE_PROVISION ||
        mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM;
}

static int oasc_enabled(void)
{
    const char *mode, *handoff;
    mode = getenv("MUD_M3_MODE");
    handoff = getenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return mode && handoff && !strcmp(mode, "shadow") &&
        !strcmp(handoff, "handoff");
}

static int oasc_copy(destination, capacity, source)
char *destination;
unsigned long capacity;
const char *source;
{
    unsigned long length;
    if(!destination || !capacity || !source) return -1;
    length = oasc_bounded(source, capacity - 1U);
    if(length >= capacity) return -1;
    memcpy(destination, source, length);
    destination[length] = 0;
    return 0;
}

void onboarding_activation_save_capability_clear(capability)
onboarding_activation_save_capability *capability;
{
    if(capability) memset(capability, 0, sizeof(*capability));
}

onboarding_activation_save_capability_status
onboarding_activation_save_capability_capture(capability, actor_user_id,
                                               correlation_id, character_id,
                                               mode, command_id, canonical_name)
onboarding_activation_save_capability *capability;
const char *actor_user_id;
const char *correlation_id;
const char *character_id;
onboarding_activation_binding_mode mode;
const char *command_id;
const char *canonical_name;
{
    onboarding_activation_save_capability_status result;

    if(!capability) return ONBOARDING_ACTIVATION_SAVE_CAPABILITY_INVALID;
    onboarding_activation_save_capability_clear(capability);
    if(!oasc_enabled()) return ONBOARDING_ACTIVATION_SAVE_CAPABILITY_DISABLED;
    result = ONBOARDING_ACTIVATION_SAVE_CAPABILITY_INVALID;
    if(!oasc_uuid(actor_user_id) || !oasc_uuid(correlation_id) ||
       !oasc_uuid(character_id) || !oasc_mode(mode) || !oasc_uuid(command_id) ||
       !canonical_name ||
       !player_name_is_valid((const unsigned char *)canonical_name,
                             PLAYER_NAME_MIN_CODEPOINTS,
                             PLAYER_NAME_MAX_CODEPOINTS))
        return result;
    if(oasc_copy(capability->record.actor_user_id,
                 sizeof(capability->record.actor_user_id), actor_user_id) ||
       oasc_copy(capability->record.correlation_id,
                 sizeof(capability->record.correlation_id), correlation_id) ||
       oasc_copy(capability->record.character_id,
                 sizeof(capability->record.character_id), character_id) ||
       oasc_copy(capability->record.command_id,
                 sizeof(capability->record.command_id), command_id) ||
       oasc_copy(capability->record.canonical_name,
                 sizeof(capability->record.canonical_name), canonical_name)) {
        onboarding_activation_save_capability_clear(capability);
        return result;
    }
    capability->record.mode = mode;
    capability->armed = 1;
    return ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK;
}

onboarding_activation_save_capability_status
onboarding_activation_save_capability_consume_for_explicit_save(capability,
                                                                  command_id, record)
onboarding_activation_save_capability *capability;
const char *command_id;
onboarding_activation_save_capability_record *record;
{
    if(record) memset(record, 0, sizeof(*record));
    if(!capability || !record || !oasc_enabled() || !capability->armed ||
       !oasc_uuid(command_id) || strcmp(capability->record.command_id, command_id)) {
        onboarding_activation_save_capability_clear(capability);
        return ONBOARDING_ACTIVATION_SAVE_CAPABILITY_UNAVAILABLE;
    }
    *record = capability->record;
    onboarding_activation_save_capability_clear(capability);
    return ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK;
}
