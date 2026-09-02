#ifndef CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_H
#define CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_H

/*
 * Stateful, caller-owned PlayerStore facade for the opt-in M3 live shadow.
 * It owns neither the writer nor the RPC transport; callers retain both and
 * must keep them alive for every dispatched save.
 */
#include "character_save_journal_v2_live_ops.h"
#include "character_save_journal_v2_protocol.h"
#include "character_save_journal_v2_uuid.h"
#include "player_record_serializer.h"
#include "player_store.h"

#define CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_BUFFER_MAX \
    (64UL * 1024UL * 1024UL)
#define CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX 63

typedef int (*character_save_journal_v2_player_store_lease_deadline)(
    void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX + 1]);

typedef int (*character_save_journal_v2_player_store_command_uuid)(
    void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH + 1]);

typedef int (*character_save_journal_v2_player_store_file_load)(
    void *opaque, char *name, struct creature **player);

typedef enum character_save_journal_v2_player_store_state {
    CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE = 0,
    CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_SAVING = 1
} character_save_journal_v2_player_store_state;

typedef struct character_save_journal_v2_player_store {
    character_save_journal_v2_writer_context *held_writer;
    character_save_journal_v2_live_ops *live_ops;
    char *buffer;
    unsigned long buffer_capacity;
    player_record_serializer_limits serializer_limits;
    character_save_journal_v2_player_store_lease_deadline lease_deadline;
    void *lease_deadline_opaque;
    character_save_journal_v2_player_store_command_uuid command_uuid;
    void *command_uuid_opaque;
    character_save_journal_v2_player_store_file_load file_load;
    void *file_load_opaque;

    /* Observable caller-owned state.  Transient references are reset after
     * every dispatched save.  last_report is reset before each non-reentrant
     * attempt and then retained for its outcome; an inert reentrant rejection
     * must not clobber the outer attempt's report.  Buffer bytes remain
     * caller-owned throughout. */
    unsigned long buffer_length;
    character_save_journal_v2_player_store_state state;
    character_save_journal_v2_protocol_report last_report;

    /* Private per-call references used by the protocol serializer callback.
     * They never outlive player_store_save's dynamic extent. */
    struct creature *active_player;
} character_save_journal_v2_player_store;

void character_save_journal_v2_player_store_init(
    character_save_journal_v2_player_store *store,
    character_save_journal_v2_writer_context *held_writer,
    character_save_journal_v2_live_ops *live_ops,
    char *buffer, unsigned long buffer_capacity,
    const player_record_serializer_limits *serializer_limits,
    character_save_journal_v2_player_store_lease_deadline lease_deadline,
    void *lease_deadline_opaque,
    character_save_journal_v2_player_store_command_uuid command_uuid,
    void *command_uuid_opaque,
    character_save_journal_v2_player_store_file_load file_load,
    void *file_load_opaque);

/* Produces an opaque PlayerStore dispatch value.  No heap allocation, global
 * registration, connection ownership, or libpq dependency is involved. */
player_store_ops character_save_journal_v2_player_store_build(
    character_save_journal_v2_player_store *store);

int character_save_journal_v2_player_store_save(
    void *opaque, char *name, struct creature *player);
int character_save_journal_v2_player_store_load(
    void *opaque, char *name, struct creature **player);

#endif
