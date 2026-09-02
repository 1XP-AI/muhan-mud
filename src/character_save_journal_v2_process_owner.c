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
}

character_save_journal_v2_process_owner_startup_result
character_save_journal_v2_process_owner_start(
    character_save_journal_v2_process_owner *owner)
{
    const character_save_journal_v2_process_owner_configuration *configuration;
    char deadline[CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX + 1];
    char candidate[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1];
    player_store_ops store_ops;

    if(!owner) return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_INVALID_ARGUMENT;
    if(owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY ||
       owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING) {
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
    owner->shutdown_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    process_owner_clear_held(owner);
    memset(&owner->recovery_report, 0, sizeof(owner->recovery_report));
    owner->recovery_result = CHARACTER_SAVE_JOURNAL_V2_RECOVERY_INVALID_ARGUMENT;
    memset(deadline, 0, sizeof(deadline));
    if(configuration->acquire_deadline(configuration->acquire_deadline_opaque,
        deadline) || !process_owner_text(deadline, sizeof(deadline))) {
        owner->startup_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_DEADLINE;
        owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
        return owner->startup_result;
    }
    memset(candidate, 0, sizeof(candidate));
    if(configuration->candidate_uuid(configuration->candidate_uuid_opaque, candidate) ||
       !process_owner_uuid(candidate)) {
        owner->startup_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANDIDATE_UUID;
        owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
        return owner->startup_result;
    }

    character_save_journal_v2_live_ops_init(&owner->live_ops,
        configuration->transport, deadline);
    if(character_save_journal_v2_writer_bootstrap(configuration->root,
        configuration->world_id, candidate,
        character_save_journal_v2_live_ops_writer_epoch_acquire,
        &owner->live_ops, &owner->held_writer)) {
        process_owner_clear_held(owner);
        owner->startup_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_BOOTSTRAP;
        owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
        return owner->startup_result;
    }
    /* live_ops borrows this stack deadline only for bootstrap acquire.  All
     * later renewals receive a fresh caller-supplied deadline, so retain no
     * pointer to the expired stack storage once bootstrap returns. */
    owner->live_ops.acquire_lease_expires_at = 0;
    owner->writer_held = 1;
    owner->recovery_result = character_save_journal_v2_recovery_run(
        &owner->held_writer,
        character_save_journal_v2_live_ops_receipt_callback,
        &owner->live_ops, &owner->recovery_report);
    if(owner->recovery_result != CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK) {
        owner->startup_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_RECOVERY;
        process_owner_unwind(owner);
        return owner->startup_result;
    }

    character_save_journal_v2_player_store_init(&owner->player_store,
        &owner->held_writer, &owner->live_ops, configuration->buffer,
        configuration->buffer_capacity, &configuration->serializer_limits,
        configuration->acquire_deadline, configuration->acquire_deadline_opaque,
        configuration->candidate_uuid, configuration->candidate_uuid_opaque,
        configuration->file_load, configuration->file_load_opaque);
    store_ops = character_save_journal_v2_player_store_build(&owner->player_store);
    if(player_store_set(&store_ops)) {
        owner->startup_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_PLAYER_STORE;
        process_owner_unwind(owner);
        return owner->startup_result;
    }
    owner->player_store_installed = 1;
    owner->startup_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK;
    owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY;
    return owner->startup_result;
}

character_save_journal_v2_process_owner_shutdown_result
character_save_journal_v2_process_owner_shutdown(
    character_save_journal_v2_process_owner *owner)
{
    if(!owner) return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    if(!owner->player_store_installed && !owner->writer_held &&
       (owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW ||
        owner->state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED))
        return owner->shutdown_result;
    if(owner->player_store_installed) {
        player_store_reset();
        owner->player_store_installed = 0;
    }
    owner->shutdown_result = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
    if(owner->writer_held &&
       character_save_journal_v2_writer_close(&owner->held_writer))
        owner->shutdown_result =
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED;
    if(owner->writer_held) process_owner_clear_held(owner);
    if(owner->state != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW)
        owner->state = CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
    return owner->shutdown_result;
}
