/* Optional M3 receipt-pair boundary.  It is deliberately absent from the
 * default live object graph: legacy publication stays authoritative and
 * nonblocking unless the M3 runtime explicitly opts in. */
#include "character_player_snapshot_v1_receipt_pair.h"

#include "character_save_journal_v2_ack.h"
#include "character_snapshot_shadow_outbox.h"

#include <string.h>

static int receipt_pair_matches(artifact, wire)
const character_player_snapshot_v1_artifact_metadata *artifact;
const character_save_journal_v2_wire *wire;
{
    return artifact && wire &&
        !strcmp(artifact->world_id, wire->world_id) &&
        !strcmp(artifact->character_id, wire->character_id) &&
        !strcmp(artifact->command_id, wire->command_uuid) &&
        !strcmp(artifact->canonical_name_hex, wire->legacy_name_key_hex) &&
        !strcmp(artifact->request_sha256, wire->request_sha256) &&
        !strcmp(artifact->source_post_sha256, wire->post_sha256) &&
        !strcmp(artifact->writer_instance_id, wire->writer_instance_id) &&
        artifact->writer_epoch == wire->writer_epoch &&
        artifact->writer_revision == wire->writer_revision &&
        artifact->storage_format == (int16_t)wire->storage_format;
}

static void receipt_pair_manifest(manifest, artifact)
character_snapshot_shadow_outbox_manifest *manifest;
const character_player_snapshot_v1_artifact_metadata *artifact;
{
    memset(manifest, 0, sizeof(*manifest));
    strcpy(manifest->world_id, artifact->world_id);
    strcpy(manifest->character_id, artifact->character_id);
    strcpy(manifest->command_id, artifact->command_id);
    strcpy(manifest->canonical_name_hex, artifact->canonical_name_hex);
    strcpy(manifest->request_sha256, artifact->request_sha256);
    strcpy(manifest->post_sha256, artifact->source_post_sha256);
    strcpy(manifest->writer_instance_id, artifact->writer_instance_id);
    strcpy(manifest->snapshot_format, CHARACTER_SNAPSHOT_SHADOW_OUTBOX_FORMAT);
    manifest->writer_epoch = artifact->writer_epoch;
    manifest->writer_revision = artifact->writer_revision;
    manifest->storage_format = artifact->storage_format;
    manifest->snapshot_octets = artifact->source_octets;
}

character_player_snapshot_v1_receipt_pair_result
character_player_snapshot_v1_receipt_pair_commit(writer, artifact_directory_fd,
    artifact_key)
const character_save_journal_v2_writer_context *writer;
int artifact_directory_fd;
const character_player_snapshot_v1_artifact_metadata *artifact_key;
{
    character_save_journal_v2_ack_marker_result ack_result;
    character_player_snapshot_v1_artifact_metadata artifact;
    character_snapshot_shadow_outbox_manifest manifest, existing;
    character_save_journal_v2_wire wire;
    uint8_t *snapshot = 0;
    size_t snapshot_length = 0;
    int artifact_result, manifest_result;

    memset(&artifact, 0, sizeof(artifact));
    memset(&manifest, 0, sizeof(manifest));
    memset(&existing, 0, sizeof(existing));
    memset(&wire, 0, sizeof(wire));
    if(!writer || artifact_directory_fd < 0 || !artifact_key)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_INVALID;
    ack_result = character_save_journal_v2_ack_marker_verify(writer,
        artifact_key->command_id, &wire);
    if(ack_result == CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_NOT_ACKED)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_NOT_ACKED;
    if(ack_result == CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_LOCAL_INCOMPLETE)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_LOCAL_INCOMPLETE;
    if(ack_result == CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_INVALID_ARGUMENT)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_INVALID;
    if(ack_result == CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_CONTEXT)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_CONTEXT;
    if(ack_result != CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_ACKED)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_IO_ERROR;
    artifact_result = character_player_snapshot_v1_artifact_load(
        artifact_directory_fd, artifact_key, &artifact, &snapshot, &snapshot_length);
    character_player_snapshot_v1_artifact_free(snapshot);
    snapshot = 0;
    if(artifact_result == CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_IO_ERROR;
    if(artifact_result != CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK ||
       !receipt_pair_matches(&artifact, &wire))
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_FROZEN;
    receipt_pair_manifest(&manifest, &artifact);
    manifest_result = character_snapshot_shadow_outbox_retry(artifact_directory_fd,
        &manifest, &existing);
    if(manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_EXACT_RETRY;
    if(manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CONFLICT ||
       manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_FROZEN;
    if(manifest_result != CHARACTER_SNAPSHOT_SHADOW_OUTBOX_NOT_FOUND)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_IO_ERROR;
    manifest_result = character_snapshot_shadow_outbox_write(artifact_directory_fd,
        &manifest);
    if(manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_OK)
        return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_OK;
    if(manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXISTS) {
        manifest_result = character_snapshot_shadow_outbox_retry(artifact_directory_fd,
            &manifest, &existing);
        if(manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_EXACT_RETRY)
            return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_EXACT_RETRY;
        if(manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CONFLICT ||
           manifest_result == CHARACTER_SNAPSHOT_SHADOW_OUTBOX_CORRUPT)
            return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_FROZEN;
    }
    return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_IO_ERROR;
}
