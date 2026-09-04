#include "character_save_journal_v2_process_owner.h"

#include <string.h>

static int process_owner_text(const char *text, unsigned long capacity)
{
    return text && capacity && memchr(text, 0, capacity) != 0 && text[0];
}

static int process_owner_uuid(const char text[
    CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1])
{
    unsigned int index;
    char value;

    if(!text || text[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH]) return 0;
    if(text[14] != '4' || (text[19] != '8' && text[19] != '9' &&
        text[19] != 'a' && text[19] != 'b')) return 0;
    for(index = 0; index < CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH;
        index++) {
        if(index == 8 || index == 13 || index == 18 || index == 23) {
            if(text[index] != '-') return 0;
            continue;
        }
        value = text[index];
        if(!((value >= '0' && value <= '9') ||
             (value >= 'a' && value <= 'f'))) return 0;
    }
    return 1;
}

static int process_owner_valid(const character_save_journal_v2_process_owner *owner)
{
    const character_save_journal_v2_process_owner_configuration *configuration;

    if(!owner) return 0;
    configuration = &owner->configuration;
    return configuration->root && configuration->root[0] &&
        configuration->world_id && configuration->world_id[0] &&
        strlen(configuration->world_id) <=
            CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX &&
        configuration->transport && configuration->buffer &&
        configuration->buffer_capacity &&
        configuration->buffer_capacity <= CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_BUFFER_MAX &&
        configuration->acquire_deadline && configuration->candidate_uuid &&
        configuration->file_load;
}

static character_save_journal_v2_prepared_stage_observer
process_owner_stage_observer(
    const character_save_journal_v2_process_owner *owner,
    void **observer_opaque)
{
    if(owner->configuration.snapshot_handoff) {
        *observer_opaque=owner->configuration.snapshot_handoff;
        return character_player_snapshot_v1_handoff_observe;
    }
    *observer_opaque=owner->configuration.stage_observer_opaque;
    return owner->configuration.stage_observer;
}

static void process_owner_clear_held(character_save_journal_v2_process_owner *owner)
{
    memset(&owner->held_writer, 0, sizeof(owner->held_writer));
    memset(&owner->player_store, 0, sizeof(owner->player_store));
    owner->writer_held = 0;
    memset(&owner->live_ops, 0, sizeof(owner->live_ops));
}

static void process_owner_unwind(character_save_journal_v2_process_owner *owner)
{
    if(owner->writer_held &&
       character_save_journal_v2_writer_close(&owner->held_writer))
        owner->shutdown_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED;
    process_owner_clear_held(owner);
    owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
}

static void process_owner_unbind_store(
    character_save_journal_v2_process_owner *owner)
{
    if(!owner->player_store_installed) return;
    (void)player_store_unbind(&owner->player_store_binding);
    owner->player_store_installed = 0;
}

static character_save_journal_v2_process_owner_startup_result
process_owner_stop_start(character_save_journal_v2_process_owner *owner,
    character_save_journal_v2_process_owner_startup_result result,
    int unwind)
{
    owner->startup_result = result;
    if(unwind)
        process_owner_unwind(owner);
    else
        owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
    owner->operation_active = 0;
    owner->shutdown_requested = 0;
    return owner->startup_result;
}

static character_save_journal_v2_process_owner_startup_result
process_owner_cancel_start(character_save_journal_v2_process_owner *owner)
{
    process_owner_unbind_store(owner);
    return process_owner_stop_start(owner,
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANCELLED, 1);
}

void character_save_journal_v2_process_owner_init(
    character_save_journal_v2_process_owner *owner,
    const character_save_journal_v2_process_owner_configuration *configuration)
{
    if(!owner) return;
    memset(owner, 0, sizeof(*owner));
    if(configuration) owner->configuration = *configuration;
    owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW;
    owner->startup_result =
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_INVALID_ARGUMENT;
    owner->shutdown_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    owner->recovery_result = CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INVALID_ARGUMENT;
    owner->snapshot_tick_result =
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF;
    owner->snapshot_handoff_result = CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
}

character_save_journal_v2_process_owner_startup_result
character_save_journal_v2_process_owner_start(
    character_save_journal_v2_process_owner *owner)
{
    const character_save_journal_v2_process_owner_configuration *configuration;
    char deadline[CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX + 1];
    char candidate[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1];
    player_store_ops store_ops;
    character_save_journal_v2_prepared_stage_observer stage_observer;
    void *stage_observer_opaque;
    int callback_result;

    if(!owner) return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_INVALID_ARGUMENT;
    if(owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY ||
       owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING ||
       owner->operation_active) {
        owner->startup_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_ALREADY_STARTED;
        return owner->startup_result;
    }
    if(!process_owner_valid(owner)) {
        owner->startup_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_INVALID_ARGUMENT;
        return owner->startup_result;
    }
    configuration = &owner->configuration;
    if(character_save_journal_v2_rpc_transport_get_state(
        configuration->transport) != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY) {
        owner->startup_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_TRANSPORT_NOT_READY;
        return owner->startup_result;
    }

    owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING;
    owner->operation_active = 1;
    owner->shutdown_requested = 0;
    owner->shutdown_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    process_owner_clear_held(owner);
    memset(&owner->player_store_binding, 0,
        sizeof(owner->player_store_binding));
    memset(&owner->recovery_report, 0, sizeof(owner->recovery_report));
    owner->recovery_result = CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INVALID_ARGUMENT;
    owner->snapshot_tick_result = owner->configuration.snapshot_handoff ?
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_NOT_READY :
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF;
    owner->snapshot_handoff_result = CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
    memset(deadline, 0, sizeof(deadline));
    callback_result = configuration->acquire_deadline(
        configuration->acquire_deadline_opaque, deadline);
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    if(callback_result || !process_owner_text(deadline, sizeof(deadline)))
        return process_owner_stop_start(owner,
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_DEADLINE, 0);
    memset(candidate, 0, sizeof(candidate));
    callback_result = configuration->candidate_uuid(
        configuration->candidate_uuid_opaque, candidate);
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    if(callback_result || !process_owner_uuid(candidate))
        return process_owner_stop_start(owner,
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANDIDATE_UUID, 0);

    character_save_journal_v2_live_ops_init(&owner->live_ops,
        configuration->transport, deadline);
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    callback_result = character_save_journal_v2_writer_bootstrap(configuration->root,
        configuration->world_id, candidate,
        character_save_journal_v2_live_ops_writer_epoch_acquire,
        &owner->live_ops, &owner->held_writer);
    /* live_ops borrows this stack deadline only for bootstrap acquire.  All
     * later renewals receive a fresh caller-supplied deadline, so retain no
     * pointer to the expired stack storage once bootstrap returns. */
    owner->live_ops.acquire_lease_expires_at = 0;
    if(!callback_result) owner->writer_held = 1;
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    if(callback_result) {
        process_owner_clear_held(owner);
        return process_owner_stop_start(owner,
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_BOOTSTRAP, 0);
    }
    stage_observer = process_owner_stage_observer(owner,
        &stage_observer_opaque);
    owner->recovery_result = character_save_journal_v2_recovery_run_with_stage_observer(
        &owner->held_writer,
        character_save_journal_v2_live_ops_receipt_callback, &owner->live_ops,
        stage_observer, stage_observer_opaque, &owner->recovery_report);
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    if(owner->recovery_result != CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK) {
        return process_owner_stop_start(owner,
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_RECOVERY, 1);
    }

    character_save_journal_v2_player_store_init(&owner->player_store,
        &owner->held_writer, &owner->live_ops, configuration->buffer,
        configuration->buffer_capacity, &configuration->serializer_limits,
        configuration->acquire_deadline, configuration->acquire_deadline_opaque,
        configuration->candidate_uuid, configuration->candidate_uuid_opaque,
        configuration->file_load, configuration->file_load_opaque);
    if(character_save_journal_v2_player_store_set_stage_observer(
       &owner->player_store, stage_observer, stage_observer_opaque))
        return process_owner_stop_start(owner,
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_PLAYER_STORE, 1);
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    store_ops = character_save_journal_v2_player_store_build(&owner->player_store);
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    callback_result = player_store_bind(&store_ops,
        &owner->player_store_binding);
    if(!callback_result) owner->player_store_installed = 1;
    if(owner->shutdown_requested) return process_owner_cancel_start(owner);
    if(callback_result)
        return process_owner_stop_start(owner,
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_PLAYER_STORE, 1);
    owner->startup_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK;
    owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY;
    owner->operation_active = 0;
    owner->shutdown_requested = 0;
    return owner->startup_result;
}

character_save_journal_v2_process_owner_shutdown_result
character_save_journal_v2_process_owner_shutdown(
    character_save_journal_v2_process_owner *owner)
{
    if(!owner) return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    if(owner->operation_active) {
        if(owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING)
            owner->shutdown_requested = 1;
        return owner->shutdown_result;
    }
    if(!owner->player_store_installed && !owner->writer_held &&
       (owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW ||
        owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED))
        return owner->shutdown_result;
    owner->operation_active = 1;
    process_owner_unbind_store(owner);
    owner->shutdown_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    if(owner->writer_held &&
       character_save_journal_v2_writer_close(&owner->held_writer))
        owner->shutdown_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED;
    if(owner->writer_held) process_owner_clear_held(owner);
    if(owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW)
        owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
    owner->operation_active = 0;
    owner->shutdown_requested = 0;
    return owner->shutdown_result;
}

character_save_journal_v2_process_owner_snapshot_tick_result
character_save_journal_v2_process_owner_snapshot_tick(
    character_save_journal_v2_process_owner *owner, unsigned int limit)
{
    if(!owner)
        return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_INVALID_ARGUMENT;
    if(!limit) {
        owner->snapshot_tick_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_INVALID_ARGUMENT;
        return owner->snapshot_tick_result;
    }
    if(!owner->configuration.snapshot_handoff) {
        owner->snapshot_tick_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF;
        return owner->snapshot_tick_result;
    }
    if(owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY ||
       !owner->writer_held) {
        owner->snapshot_tick_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_NOT_READY;
        return owner->snapshot_tick_result;
    }
    if(owner->operation_active || owner->player_store.state !=
       CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE) {
        owner->snapshot_tick_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_BUSY;
        return owner->snapshot_tick_result;
    }
    /* The host is responsible for choosing this single-owner idle boundary;
     * this guard also prevents recursive tick calls from entering the same
     * consumer.  No publish/ACK or PlayerStore dispatch is made here. */
    owner->operation_active = 1;
    owner->snapshot_handoff_result = character_player_snapshot_v1_handoff_drain(
        owner->configuration.snapshot_handoff, &owner->held_writer, limit);
    owner->operation_active = 0;
    owner->snapshot_tick_result = owner->snapshot_handoff_result ==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK ?
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OK :
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_HANDOFF_FAILED;
    return owner->snapshot_tick_result;
}
