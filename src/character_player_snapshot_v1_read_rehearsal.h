#ifndef CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_H
#define CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_H

/* Detached M3 read rehearsal.  It is intentionally not a PlayerStore
 * installation: callers opt in per read and gameplay always receives the
 * immutable legacy FileStore result. */
#include "character_player_snapshot_v1_artifact.h"
#include "character_save_journal_v2_writer.h"

#include "player_store.h"

typedef enum character_player_snapshot_v1_read_rehearsal_result {
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_MATCHED = 0,
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DISABLED = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_RECEIPT_MISMATCH = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DECODE_MISMATCH = 4,
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_SEMANTIC_MISMATCH = 5,
    CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_LEGACY_ERROR = 6
} character_player_snapshot_v1_read_rehearsal_result;

/* This is caller-owned immutable input, not process activation state.  A null
 * rehearsal (or an incomplete one) leaves observation disabled. */
typedef struct character_player_snapshot_v1_read_rehearsal {
    const character_save_journal_v2_writer_context *writer;
    int artifact_directory_fd;
    const character_player_snapshot_v1_artifact_metadata *expected_artifact;
} character_player_snapshot_v1_read_rehearsal;

/* Always loads `name` via player_store_default_load.  When an explicit
 * rehearsal is supplied, it verifies the DB_ACKED receipt/head evidence,
 * immutable artifact, decoder, and persisted semantics only for diagnostics.
 * No rehearsal failure may replace, modify, or reject the legacy result. */
int character_player_snapshot_v1_read_rehearsal_load(
    const character_player_snapshot_v1_read_rehearsal *rehearsal,
    char *name, struct creature **player,
    character_player_snapshot_v1_read_rehearsal_result *result_out);

#endif
