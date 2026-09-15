#ifndef ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_H
#define ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_H

/*
 * Feature-off, local evidence only.  A caller that already holds an exact
 * activation reservation may record that tuple beside an existing immutable
 * PlayerSnapshotV1 artifact and journal receipt.  This never chooses a
 * command, queries a candidate, or alters either save authority.
 */
#include "character_player_snapshot_v1_artifact.h"
#include "character_save_journal_v2_ack.h"
#include "onboarding_snapshot_command_consumer.h"

typedef enum onboarding_activation_snapshot_receipt_result {
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_RECORDED = 0,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_EXACT_RETRY = 1,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_NO_PROOF = 2,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_BOUNDARY_MISMATCH = 3,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CONFLICT = 4,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_INVALID = -1,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_CORRUPT = -2,
    ONBOARDING_ACTIVATION_SNAPSHOT_RECEIPT_IO_ERROR = -3
} onboarding_activation_snapshot_receipt_result;

/* `reservation_directory_fd` is an already-open private directory supplied
 * by the caller.  This function stores only the validated reservation tuple
 * after the artifact and receipt each name that exact command and character.
 * It does not open an activation-binding pathname or modify artifact/receipt
 * data, so normal FileStore and M3 receipt authority remain unchanged. */
onboarding_activation_snapshot_receipt_result
onboarding_activation_snapshot_receipt_record(
    int reservation_directory_fd,
    const onboarding_snapshot_command_reservation *reservation,
    const character_player_snapshot_v1_artifact_metadata *artifact,
    const character_save_journal_v2_receipt *receipt);

/* Read exactly one previously recorded proof selected by command UUID. */
onboarding_activation_snapshot_receipt_result
onboarding_activation_snapshot_receipt_read(
    int reservation_directory_fd, const char *command_id,
    onboarding_snapshot_command_reservation *reservation_out);

#endif
