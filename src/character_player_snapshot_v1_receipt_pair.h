#ifndef CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_H
#define CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_H

#include "character_player_snapshot_v1_artifact.h"
#include "character_save_journal_v2_writer.h"

/* Detached, opt-in/test-only bridge from a durable PlayerSnapshotV1 artifact
 * to the legacy manifest.  It never runs a receipt callback or selects a
 * pathname; the held writer and artifact-directory descriptors are its only
 * capabilities. */
typedef enum character_player_snapshot_v1_receipt_pair_result {
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_OK = 0,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_EXACT_RETRY = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_NOT_ACKED = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_LOCAL_INCOMPLETE = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_FROZEN = 4,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_INVALID = 5,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_CONTEXT = 6,
    CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_IO_ERROR = 7
} character_player_snapshot_v1_receipt_pair_result;

/* Load the immutable artifact named by `artifact_key`, bind it to exact
 * DB_ACKED journal evidence, then create or verify `<command>.manifest` in
 * that same directory.  Mismatched/corrupt evidence is never overwritten. */
character_player_snapshot_v1_receipt_pair_result
character_player_snapshot_v1_receipt_pair_commit(
    const character_save_journal_v2_writer_context *writer,
    int artifact_directory_fd,
    const character_player_snapshot_v1_artifact_metadata *artifact_key);

#endif
