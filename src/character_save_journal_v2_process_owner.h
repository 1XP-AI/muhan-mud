#ifndef CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_H
#define CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_H

/*
 * Caller-owned composition root for the opt-in v2 save path.  This module
 * never opens a database connection, allocates memory, or closes the supplied
 * RPC transport: it only borrows a READY transport while it holds a durable
 * writer context.
 */
#include "character_save_journal_v2_player_store.h"
#include "character_save_journal_v2_recovery.h"
#include "character_player_snapshot_v1_handoff.h"

typedef enum character_save_journal_v2_process_owner_state {
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW = 0,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTING = 1,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY = 2,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED = 3
} character_save_journal_v2_process_owner_state;

typedef enum character_save_journal_v2_process_owner_startup_result {
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_INVALID_ARGUMENT = 1,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_TRANSPORT_NOT_READY = 2,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_ALREADY_STARTED = 3,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_DEADLINE = 4,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANDIDATE_UUID = 5,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_BOOTSTRAP = 6,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_RECOVERY = 7,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_PLAYER_STORE = 8,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_CANCELLED = 9
} character_save_journal_v2_process_owner_startup_result;

typedef enum character_save_journal_v2_process_owner_shutdown_result {
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_CLOSE_FAILED = 1
} character_save_journal_v2_process_owner_shutdown_result;

/* The handoff consumer is deliberately driven only by an owner caller at a
 * known-safe lifecycle/tick boundary.  It is never dispatched from recovery,
 * PlayerStore save, publish, or ACK. */
typedef enum character_save_journal_v2_process_owner_snapshot_tick_result {
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OK = 0,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF = 1,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_NOT_READY = 2,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_BUSY = 3,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_INVALID_ARGUMENT = 4,
    CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_HANDOFF_FAILED = 5
} character_save_journal_v2_process_owner_snapshot_tick_result;

typedef struct character_save_journal_v2_process_owner_configuration {
    const char *root;
    const char *world_id;
    character_save_journal_v2_rpc_transport *transport;
    char *buffer;
    unsigned long buffer_capacity;
    player_record_serializer_limits serializer_limits;
    character_save_journal_v2_player_store_lease_deadline acquire_deadline;
    void *acquire_deadline_opaque;
    character_save_journal_v2_player_store_command_uuid candidate_uuid;
    void *candidate_uuid_opaque;
    character_save_journal_v2_player_store_file_load file_load;
    void *file_load_opaque;
    /* Optional shadow-only capture, shared by cold recovery and live saves. */
    character_save_journal_v2_prepared_stage_observer stage_observer;
    void *stage_observer_opaque;
    /* Optional durable replacement for stage_observer.  When present it wins
     * for both startup recovery and live saves; the two observers are never
     * combined.  The owner retains no ownership, so callers must keep it alive
     * through shutdown. */
    character_player_snapshot_v1_handoff *snapshot_handoff;
} character_save_journal_v2_process_owner_configuration;

typedef struct character_save_journal_v2_process_owner {
    /* Configuration pointers and buffer ownership remain with the caller. */
    character_save_journal_v2_process_owner_configuration configuration;
    character_save_journal_v2_live_ops live_ops;
    character_save_journal_v2_writer_context held_writer;
    character_save_journal_v2_player_store player_store;
    player_store_binding player_store_binding;
    character_save_journal_v2_recovery_report recovery_report;
    character_save_journal_v2_recovery_result recovery_result;
    character_save_journal_v2_process_owner_state state;
    character_save_journal_v2_process_owner_startup_result startup_result;
    character_save_journal_v2_process_owner_shutdown_result shutdown_result;
    character_save_journal_v2_process_owner_snapshot_tick_result snapshot_tick_result;
    int snapshot_handoff_result;
    int writer_held;
    int player_store_installed;
    int operation_active;
    int shutdown_requested;
} character_save_journal_v2_process_owner;

/* Initializes only caller-provided storage.  Configuration validity is
 * checked at start so callers can construct it incrementally. */
void character_save_journal_v2_process_owner_init(
    character_save_journal_v2_process_owner *owner,
    const character_save_journal_v2_process_owner_configuration *configuration);

/* Exact startup order: validate/READY, deadline, UUID, live_ops, durable
 * bootstrap, recovery receipt replay, then one PlayerStore installation. */
character_save_journal_v2_process_owner_startup_result
character_save_journal_v2_process_owner_start(
    character_save_journal_v2_process_owner *owner);

/* Restores the prior global PlayerStore before closing the held writer.  A
 * call re-entered during startup requests deferred cancellation; it never
 * tears resources out from under the active startup frame.  Shutdown is
 * idempotent and never closes or otherwise mutates transport. */
character_save_journal_v2_process_owner_shutdown_result
character_save_journal_v2_process_owner_shutdown(
    character_save_journal_v2_process_owner *owner);

/* Processes a bounded number of durable snapshot tokens only when the owner
 * is READY and its PlayerStore is idle.  Hosts choose when to call this;
 * neither live saves nor startup recovery invoke it implicitly. */
character_save_journal_v2_process_owner_snapshot_tick_result
character_save_journal_v2_process_owner_snapshot_tick(
    character_save_journal_v2_process_owner *owner, unsigned int limit);

#endif
