/* TDD boundary: PlayerSnapshotV1 may be rehearsed only as receipt-bound
 * evidence.  The returned player always remains the legacy FileStore result. */
#include "character_player_snapshot_v1_read_rehearsal.h"

#include "character_save_journal_v2_ack.h"
#include "cdto_v1.h"
#include "player_snapshot_v1.h"
#include "player_store.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char WORLD[]="m3-read";
static const char INSTANCE[]="11111111-1111-4111-8111-111111111111";
static const char CHARACTER[]="90000000-0000-4000-8000-000000000006";
static const char COMMAND[]="10000000-0000-4000-8000-000000000001";
static const char NAME_HEX[]="4d33616c706861";
static const char HASH_A[]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
static const char HASH_B[]="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

static creature legacy_player;
static creature snapshot_player;
static character_save_journal_v2_wire receipt_wire;
static character_player_snapshot_v1_artifact_metadata loaded_artifact;
static character_player_snapshot_v1_artifact_metadata expected_artifact;
static int ack_result;
static int artifact_result;
static int decode_result;
static int semantic_equal;
static int legacy_loads;
static int artifact_loads;
static int decode_calls;

int file_player_store_save(char *name, struct creature *player)
{ (void)name; (void)player; return PLAYER_STORE_IO_ERROR; }

int file_player_store_load(char *name, struct creature **player)
{
    legacy_loads++;
    if(!name || !player || strcmp(name,"M3alpha")) return PLAYER_STORE_NOT_FOUND;
    *player=&legacy_player;
    return PLAYER_STORE_OK;
}

character_save_journal_v2_ack_marker_result
character_save_journal_v2_ack_marker_verify(writer, command_id, wire_out)
const character_save_journal_v2_writer_context *writer;
const char *command_id;
character_save_journal_v2_wire *wire_out;
{
    if(!writer || !command_id || !wire_out || strcmp(command_id,COMMAND))
        return CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_INVALID_ARGUMENT;
    if(ack_result==CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_ACKED)
        *wire_out=receipt_wire;
    return (character_save_journal_v2_ack_marker_result)ack_result;
}

int character_player_snapshot_v1_artifact_load(directory_fd,key,metadata,
    snapshot,snapshot_length)
int directory_fd;
const character_player_snapshot_v1_artifact_metadata *key;
character_player_snapshot_v1_artifact_metadata *metadata;
uint8_t **snapshot;
size_t *snapshot_length;
{
    static uint8_t bytes[]={1};
    artifact_loads++;
    if(directory_fd!=73 || !key || !metadata || !snapshot || !snapshot_length)
        return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID;
    if(artifact_result!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK)
        return artifact_result;
    *metadata=loaded_artifact;
    *snapshot=bytes;
    *snapshot_length=sizeof(bytes);
    return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK;
}

void character_player_snapshot_v1_artifact_free(uint8_t *snapshot)
{ (void)snapshot; }

int player_snapshot_v1_decode_clone(wire, length, out)
const uint8_t *wire;
size_t length;
creature **out;
{
    (void)wire; (void)length;
    decode_calls++;
    if(!out) return CDTO_V1_INVALID_ARGUMENT;
    *out=decode_result==CDTO_V1_OK ? &snapshot_player : 0;
    return decode_result;
}

void player_snapshot_v1_free_clone(creature *player)
{ (void)player; }

int player_snapshot_v1_equal_persisted(left,right)
const creature *left;
const creature *right;
{ return left==&legacy_player && right==&snapshot_player && semantic_equal; }

static int expect(int ok,const char *text)
{ if(ok)return 0;fprintf(stderr,"character_player_snapshot_v1_read_rehearsal_test: %s\n",text);return 1; }

static void fixture(void)
{
    memset(&legacy_player,0,sizeof(legacy_player));
    memset(&snapshot_player,0,sizeof(snapshot_player));
    legacy_player.type=snapshot_player.type=PLAYER;
    strcpy(legacy_player.name,"M3alpha");
    strcpy(snapshot_player.name,"M3alpha");
    memset(&receipt_wire,0,sizeof(receipt_wire));
    receipt_wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(receipt_wire.world_id,WORLD);
    strcpy(receipt_wire.character_id,CHARACTER);
    strcpy(receipt_wire.command_uuid,COMMAND);
    strcpy(receipt_wire.legacy_name_key_hex,NAME_HEX);
    strcpy(receipt_wire.request_sha256,HASH_A);
    strcpy(receipt_wire.post_sha256,HASH_B);
    strcpy(receipt_wire.writer_instance_id,INSTANCE);
    receipt_wire.writer_epoch=7;
    receipt_wire.writer_revision=9;
    receipt_wire.storage_format=1;
    memset(&loaded_artifact,0,sizeof(loaded_artifact));
    strcpy(loaded_artifact.world_id,WORLD);
    strcpy(loaded_artifact.character_id,CHARACTER);
    strcpy(loaded_artifact.command_id,COMMAND);
    strcpy(loaded_artifact.canonical_name_hex,NAME_HEX);
    strcpy(loaded_artifact.request_sha256,HASH_A);
    strcpy(loaded_artifact.source_post_sha256,HASH_B);
    strcpy(loaded_artifact.writer_instance_id,INSTANCE);
    strcpy(loaded_artifact.snapshot_sha256,HASH_A);
    strcpy(loaded_artifact.snapshot_format,CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT);
    loaded_artifact.writer_epoch=7;
    loaded_artifact.writer_revision=9;
    loaded_artifact.storage_format=1;
    loaded_artifact.source_octets=1;
    loaded_artifact.snapshot_octets=1;
    expected_artifact=loaded_artifact;
    ack_result=CHARACTER_SAVE_JOURNAL_V2_ACK_MARKER_ACKED;
    artifact_result=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK;
    decode_result=CDTO_V1_OK;
    semantic_equal=1;
    legacy_loads=artifact_loads=decode_calls=0;
}

static int load_with(character_player_snapshot_v1_read_rehearsal *rehearsal,
    character_player_snapshot_v1_read_rehearsal_result *outcome,
    creature **player)
{
    return character_player_snapshot_v1_read_rehearsal_load(rehearsal,
        "M3alpha",player,outcome);
}

static int receipt_mismatch_case(rehearsal,field)
character_player_snapshot_v1_read_rehearsal *rehearsal;
int field;
{
    character_player_snapshot_v1_read_rehearsal_result outcome;
    creature *player;

    fixture();
    switch(field) {
    case 0: strcpy(receipt_wire.world_id,"m3-other"); break;
    case 1: strcpy(receipt_wire.character_id,"80000000-0000-4000-8000-000000000006"); break;
    case 2: strcpy(receipt_wire.command_uuid,"20000000-0000-4000-8000-000000000001"); break;
    case 3: strcpy(receipt_wire.legacy_name_key_hex,"4d33616c706862"); break;
    case 4: strcpy(receipt_wire.request_sha256,HASH_B); break;
    case 5: strcpy(receipt_wire.post_sha256,HASH_A); break;
    case 6: strcpy(receipt_wire.writer_instance_id,"22222222-2222-4222-8222-222222222222"); break;
    case 7: receipt_wire.writer_epoch++; break;
    case 8: receipt_wire.writer_revision++; break;
    default: receipt_wire.storage_format=2; break;
    }
    player=0;
    return load_with(rehearsal,&outcome,&player)==PLAYER_STORE_OK&&
        player==&legacy_player&&
        outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_RECEIPT_MISMATCH&&
        legacy_loads==1&&artifact_loads==1&&!decode_calls;
}

int main(void)
{
    character_save_journal_v2_writer_context writer;
    character_player_snapshot_v1_read_rehearsal rehearsal;
    character_player_snapshot_v1_read_rehearsal_result outcome;
    creature *player;
    int failed=0;

    fixture(); memset(&writer,0,sizeof(writer));
    rehearsal.writer=&writer;
    rehearsal.artifact_directory_fd=73;
    rehearsal.expected_artifact=&expected_artifact;
    player=0;
    failed+=expect(load_with(&rehearsal,&outcome,&player)==PLAYER_STORE_OK&&
        player==&legacy_player&&outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_MATCHED&&
        legacy_loads==1&&artifact_loads==1&&decode_calls==1,
        "exact receipt/head and artifact evidence rehearse without replacing legacy state");

    fixture(); player=0;
    failed+=expect(load_with(0,&outcome,&player)==PLAYER_STORE_OK&&player==&legacy_player&&
        outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DISABLED&&
        legacy_loads==1&&!artifact_loads&&!decode_calls,
        "absent rehearsal remains default-off and only returns FileStore state");

    { int field; for(field=0;field<10;field++) failed+=expect(
        receipt_mismatch_case(&rehearsal,field),
        "every receipt/head identity field must match exactly before decode"); }

    fixture(); strcpy(loaded_artifact.snapshot_sha256,HASH_B); player=0;
    failed+=expect(load_with(&rehearsal,&outcome,&player)==PLAYER_STORE_OK&&player==&legacy_player&&
        outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH&&
        legacy_loads==1&&artifact_loads==1&&!decode_calls,
        "a candidate digest mismatch falls back deterministically to legacy state");

    fixture(); artifact_result=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT; player=0;
    failed+=expect(load_with(&rehearsal,&outcome,&player)==PLAYER_STORE_OK&&player==&legacy_player&&
        outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH&&
        legacy_loads==1&&artifact_loads==1&&!decode_calls,
        "a corrupt digest artifact falls back deterministically to legacy state");

    fixture(); decode_result=CDTO_V1_INVALID_FIELD_LENGTH; player=0;
    failed+=expect(load_with(&rehearsal,&outcome,&player)==PLAYER_STORE_OK&&player==&legacy_player&&
        outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DECODE_MISMATCH&&
        legacy_loads==1&&artifact_loads==1&&decode_calls==1,
        "a snapshot decode mismatch falls back deterministically to legacy state");

    fixture(); semantic_equal=0; player=0;
    failed+=expect(load_with(&rehearsal,&outcome,&player)==PLAYER_STORE_OK&&player==&legacy_player&&
        outcome==CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_SEMANTIC_MISMATCH&&
        legacy_loads==1&&artifact_loads==1&&decode_calls==1,
        "a semantic mismatch falls back deterministically to legacy state");
    return failed?1:0;
}
