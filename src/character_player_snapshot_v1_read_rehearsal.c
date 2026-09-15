#include "character_player_snapshot_v1_read_rehearsal.h"

#include "character_save_journal_v2_ack.h"
#include "cdto_v1.h"
#include "player_snapshot_v1.h"

#include <string.h>

static int rehearsal_metadata_equal(left,right)
const character_player_snapshot_v1_artifact_metadata *left;
const character_player_snapshot_v1_artifact_metadata *right;
{
    return left && right &&
        !strcmp(left->world_id,right->world_id) &&
        !strcmp(left->character_id,right->character_id) &&
        !strcmp(left->command_id,right->command_id) &&
        !strcmp(left->canonical_name_hex,right->canonical_name_hex) &&
        !strcmp(left->request_sha256,right->request_sha256) &&
        !strcmp(left->source_post_sha256,right->source_post_sha256) &&
        !strcmp(left->writer_instance_id,right->writer_instance_id) &&
        !strcmp(left->snapshot_sha256,right->snapshot_sha256) &&
        !strcmp(left->snapshot_format,right->snapshot_format) &&
        left->writer_epoch==right->writer_epoch &&
        left->writer_revision==right->writer_revision &&
        left->source_octets==right->source_octets &&
        left->snapshot_octets==right->snapshot_octets &&
        left->storage_format==right->storage_format;
}

static int rehearsal_receipt_matches(artifact,wire)
const character_player_snapshot_v1_artifact_metadata *artifact;
const character_save_journal_v2_wire *wire;
{
    return artifact && wire &&
        !strcmp(artifact->world_id,wire->world_id) &&
        !strcmp(artifact->character_id,wire->character_id) &&
        !strcmp(artifact->command_id,wire->command_uuid) &&
        !strcmp(artifact->canonical_name_hex,wire->legacy_name_key_hex) &&
        !strcmp(artifact->request_sha256,wire->request_sha256) &&
        !strcmp(artifact->source_post_sha256,wire->post_sha256) &&
        !strcmp(artifact->writer_instance_id,wire->writer_instance_id) &&
        artifact->writer_epoch==wire->writer_epoch &&
        artifact->writer_revision==wire->writer_revision &&
        artifact->storage_format==(int16_t)wire->storage_format;
}

static int rehearsal_enabled(value)
const character_player_snapshot_v1_read_rehearsal *value;
{
    return value && value->writer && value->artifact_directory_fd>=0 &&
        value->expected_artifact;
}

int character_player_snapshot_v1_read_rehearsal_load(rehearsal,name,player,
    result_out)
const character_player_snapshot_v1_read_rehearsal *rehearsal;
char *name;
struct creature **player;
character_player_snapshot_v1_read_rehearsal_result *result_out;
{
    character_player_snapshot_v1_artifact_metadata artifact;
    character_save_journal_v2_wire wire;
    character_save_journal_v2_ack_marker_result receipt_result;
    uint8_t *snapshot;
    size_t snapshot_length;
    struct creature *decoded;
    int legacy_result,artifact_result,decode_result;
    character_player_snapshot_v1_read_rehearsal_result result;

    result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DISABLED;
    legacy_result=player_store_default_load(name,player);
    if(legacy_result!=PLAYER_STORE_OK || !player || !*player) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_LEGACY_ERROR;
        goto done;
    }
    if(!rehearsal_enabled(rehearsal)) goto done;

    memset(&artifact,0,sizeof(artifact));
    memset(&wire,0,sizeof(wire));
    snapshot=0;
    snapshot_length=0U;
    decoded=0;
    receipt_result=character_save_journal_v2_ack_marker_verify(
        rehearsal->writer,rehearsal->expected_artifact->command_id,&wire);
    if(receipt_result!=CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_ACKED) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_RECEIPT_MISMATCH;
        goto done;
    }
    artifact_result=character_player_snapshot_v1_artifact_load(
        rehearsal->artifact_directory_fd,rehearsal->expected_artifact,
        &artifact,&snapshot,&snapshot_length);
    if(artifact_result!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK ||
       !rehearsal_metadata_equal(&artifact,rehearsal->expected_artifact)) {
        character_player_snapshot_v1_artifact_free(snapshot);
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH;
        goto done;
    }
    if(!rehearsal_receipt_matches(&artifact,&wire)) {
        character_player_snapshot_v1_artifact_free(snapshot);
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_RECEIPT_MISMATCH;
        goto done;
    }
    decode_result=player_snapshot_v1_decode_clone(snapshot,snapshot_length,
        &decoded);
    character_player_snapshot_v1_artifact_free(snapshot);
    if(decode_result!=CDTO_V1_OK || !decoded) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DECODE_MISMATCH;
        goto done;
    }
    if(!player_snapshot_v1_equal_persisted(*player,decoded))
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_SEMANTIC_MISMATCH;
    else
        result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_MATCHED;
    player_snapshot_v1_free_clone(decoded);
done:
    if(result_out) *result_out=result;
    return legacy_result;
}
