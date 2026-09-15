/* One explicit ACTIVATED capability save through an already active owner. */
#include "onboarding_activation_save_runtime_helper.h"

#include "mstruct.h"

#include <string.h>

static unsigned long oasrh_bounded(const char *value, unsigned long limit)
{
    unsigned long length;

    if(!value) return limit + 1U;
    for(length = 0; length <= limit; length++)
        if(!value[length]) return length;
    return limit + 1U;
}

static int oasrh_equal(const char *left, const char *right, unsigned long limit)
{
    unsigned long left_length, right_length;

    left_length = oasrh_bounded(left, limit);
    right_length = oasrh_bounded(right, limit);
    return left_length <= limit && right_length == left_length &&
        !memcmp(left, right, left_length);
}

static int oasrh_owner_ready(
    const character_save_journal_v2_process_owner *owner)
{
    const character_save_journal_v2_player_store *store;

    if(!owner || owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY ||
       !owner->writer_held || !owner->player_store_installed) return 0;
    store = &owner->player_store;
    return store->state == CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE &&
        store->held_writer == &owner->held_writer &&
        store->live_ops == &owner->live_ops &&
        !store->resolve_candidate && !store->resolve_candidate_opaque;
}

onboarding_activation_save_runtime_helper_result
onboarding_activation_save_runtime_helper_attempt_bridge(owner, bridge,
    legacy_name, player)
character_save_journal_v2_process_owner *owner;
onboarding_activation_save_bridge *bridge;
char *legacy_name;
struct creature *player;
{
    character_save_journal_v2_player_store *store;
    onboarding_activation_save_bridge_result finished;

    if(!owner || !bridge) return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_INVALID;
    if(!oasrh_owner_ready(owner) || !bridge->active || !legacy_name || !player ||
       !oasrh_equal(bridge->selected.canonical_name, legacy_name,
                    PLAYER_NAME_MAX_BYTES) ||
       !oasrh_equal(bridge->selected.canonical_name, player->name,
                    PLAYER_NAME_MAX_BYTES))
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;

    store = &owner->player_store;
    if(character_save_journal_v2_player_store_set_candidate_resolver(
       store, onboarding_activation_save_bridge_resolve, bridge)) {
        (void)onboarding_activation_save_bridge_finish(bridge,
            &store->last_report);
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
    }
    /* Claim owns an existing canonical record. Its authentication-only
     * creature has deliberately erased credentials and is never a save source. */
    if(bridge->selected.mode == ONBOARDING_ACTIVATION_BINDING_MODE_CLAIM)
        (void)character_save_journal_v2_player_store_save_existing(store,
            legacy_name, player);
    else
        (void)character_save_journal_v2_player_store_save(store, legacy_name, player);
    /* save is synchronous and always returns its store to IDLE; use the public
     * setter rather than leaving a bridge pointer in the caller-owned store. */
    if(character_save_journal_v2_player_store_set_candidate_resolver(store, 0, 0)) {
        (void)onboarding_activation_save_bridge_finish(bridge,
            &store->last_report);
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
    }
    finished = onboarding_activation_save_bridge_finish(bridge,
        &store->last_report);
    if(finished == ONBOARDING_ACTIVATION_SAVE_BRIDGE_CONSUMED)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_CONSUMED;
    if(finished == ONBOARDING_ACTIVATION_SAVE_BRIDGE_RETAINED &&
       store->last_report.reached >=
       CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_RETAINED;
    return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
}

onboarding_activation_save_runtime_helper_result
onboarding_activation_save_runtime_helper_attempt(owner, capability, command_id,
    actor_user_id, correlation_id, character_id, mode, canonical_name,
    legacy_name, player)
character_save_journal_v2_process_owner *owner;
onboarding_activation_save_capability *capability;
const char *command_id;
const char *actor_user_id;
const char *correlation_id;
const char *character_id;
onboarding_activation_binding_mode mode;
const char *canonical_name;
char *legacy_name;
struct creature *player;
{
    onboarding_activation_save_bridge bridge;

    if(!owner || !capability) return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_INVALID;
    memset(&bridge,0,sizeof(bridge));
    if(onboarding_activation_save_bridge_begin(&bridge,capability,command_id,
       actor_user_id,correlation_id,character_id,mode,canonical_name) !=
       ONBOARDING_ACTIVATION_SAVE_BRIDGE_READY)
        return ONBOARDING_ACTIVATION_SAVE_RUNTIME_HELPER_REJECTED;
    return onboarding_activation_save_runtime_helper_attempt_bridge(owner,&bridge,
        legacy_name,player);
}
