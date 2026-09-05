/* Explicit, descriptor-local ACTIVATED -> V4 candidate bridge. */
#include "onboarding_activation_save_bridge.h"

#include <string.h>

static unsigned long oasb_bounded(const char *value, unsigned long limit)
{
    unsigned long length;
    if(!value) return limit + 1U;
    for(length = 0; length <= limit; length++)
        if(!value[length]) return length;
    return limit + 1U;
}

static int oasb_equal(const char *left, const char *right, unsigned long limit)
{
    unsigned long left_length, right_length;
    left_length = oasb_bounded(left, limit);
    right_length = oasb_bounded(right, limit);
    return left_length <= limit && right_length == left_length &&
        !memcmp(left, right, left_length);
}

static void oasb_clear(onboarding_activation_save_bridge *bridge)
{
    if(bridge) memset(bridge, 0, sizeof(*bridge));
}

static int oasb_selected_matches(
    const onboarding_activation_save_capability_record *selected,
    const char *command_id, const char *actor_user_id,
    const char *correlation_id, const char *character_id,
    onboarding_activation_binding_mode mode, const char *canonical_name)
{
    return selected && oasb_equal(selected->command_id, command_id,
        ONBOARDING_ADMISSION_UUID_LEN) && oasb_equal(selected->actor_user_id,
        actor_user_id, ONBOARDING_ADMISSION_UUID_LEN) &&
        oasb_equal(selected->correlation_id, correlation_id,
        ONBOARDING_ADMISSION_UUID_LEN) && oasb_equal(selected->character_id,
        character_id, ONBOARDING_ADMISSION_UUID_LEN) && selected->mode == mode &&
        oasb_equal(selected->canonical_name, canonical_name,
        PLAYER_NAME_MAX_BYTES);
}

onboarding_activation_save_bridge_result
onboarding_activation_save_bridge_begin(bridge, capability, command_id,
    actor_user_id, correlation_id, character_id, mode, canonical_name)
onboarding_activation_save_bridge *bridge;
onboarding_activation_save_capability *capability;
const char *command_id;
const char *actor_user_id;
const char *correlation_id;
const char *character_id;
onboarding_activation_binding_mode mode;
const char *canonical_name;
{
    onboarding_activation_save_capability_record selected;

    if(!bridge || !capability) return ONBOARDING_ACTIVATION_SAVE_BRIDGE_INVALID;
    oasb_clear(bridge);
    memset(&selected, 0, sizeof(selected));
    if(onboarding_activation_save_capability_peek_for_explicit_save(capability,
       command_id, &selected) != ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK)
        return ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED;
    if(!oasb_selected_matches(&selected, command_id, actor_user_id,
       correlation_id, character_id, mode, canonical_name)) {
        memset(&selected, 0, sizeof(selected));
        return ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED;
    }
    bridge->capability = capability;
    bridge->selected = selected;
    bridge->active = 1;
    memset(&selected, 0, sizeof(selected));
    return ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY;
}

int onboarding_activation_save_bridge_resolve(opaque, writer, route,
    canonical_legacy_name, canonical_legacy_name_length, candidate_out)
void *opaque;
const character_save_journal_v2_writer_tuple *writer;
const character_save_journal_v2_bound_route_v3 *route;
const unsigned char *canonical_legacy_name;
size_t canonical_legacy_name_length;
character_save_journal_v2_protocol_candidate_v4 *candidate_out;
{
    onboarding_activation_save_bridge *bridge =
        (onboarding_activation_save_bridge *)opaque;
    unsigned long selected_length;

    if(candidate_out) memset(candidate_out, 0, sizeof(*candidate_out));
    if(!bridge || !bridge->active || !bridge->capability || !writer || !route ||
       !canonical_legacy_name || !candidate_out) return -1;
    selected_length = oasb_bounded(bridge->selected.canonical_name,
        PLAYER_NAME_MAX_BYTES);
    if(!selected_length || selected_length > PLAYER_NAME_MAX_BYTES ||
       canonical_legacy_name_length != selected_length ||
       route->legacy_name_length != selected_length ||
       memcmp(canonical_legacy_name, bridge->selected.canonical_name,
              selected_length) || memcmp(route->legacy_name,
              bridge->selected.canonical_name, selected_length) ||
       !oasb_equal(route->character_id, bridge->selected.character_id,
                   ONBOARDING_ADMISSION_UUID_LEN)) return -1;
    memcpy(candidate_out->command_uuid, bridge->selected.command_id,
           sizeof(candidate_out->command_uuid));
    candidate_out->writer = *writer;
    memcpy(candidate_out->character_id, bridge->selected.character_id,
           sizeof(candidate_out->character_id));
    memcpy(candidate_out->canonical_legacy_name,
           bridge->selected.canonical_name, selected_length);
    candidate_out->canonical_legacy_name_length = selected_length;
    return 1;
}

onboarding_activation_save_bridge_result
onboarding_activation_save_bridge_finish(bridge, report)
onboarding_activation_save_bridge *bridge;
const character_save_journal_v2_protocol_report *report;
{
    onboarding_activation_save_capability_record consumed;
    onboarding_activation_save_bridge_result result;

    if(!bridge || !bridge->active || !bridge->capability || !report)
        return ONBOARDING_ACTIVATION_SAVE_BRIDGE_INVALID;
    result = ONBOARDING_ACTIVATION_SAVE_BRIDGE_RETAINED;
    if(report->reached >= CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED) {
        memset(&consumed, 0, sizeof(consumed));
        if(onboarding_activation_save_capability_consume_published_explicit_save(
           bridge->capability, bridge->selected.command_id,
           bridge->selected.actor_user_id, bridge->selected.correlation_id,
           bridge->selected.character_id, bridge->selected.mode,
           bridge->selected.canonical_name, &consumed) ==
           ONBOARDING_ACTIVATION_SAVE_CAPABILITY_OK &&
           oasb_selected_matches(&consumed, bridge->selected.command_id,
           bridge->selected.actor_user_id, bridge->selected.correlation_id,
           bridge->selected.character_id, bridge->selected.mode,
           bridge->selected.canonical_name))
            result = ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED;
        else
            result = ONBOARDING_ACTIVATION_SAVE_BRIDGE_REJECTED;
        memset(&consumed, 0, sizeof(consumed));
    }
    oasb_clear(bridge);
    return result;
}
