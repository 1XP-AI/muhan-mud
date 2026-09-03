#ifndef CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_H
#define CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_H

#include "character_player_snapshot_v1_artifact.h"
#include "character_save_journal_v2_recovery.h"

#include <stdint.h>

#define CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY \
    "character-player-snapshot-v1-outbox"

struct creature;

typedef enum character_player_snapshot_v1_capture_result {
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK = 0,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID = 1,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_CONTEXT = 2,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_PREPARED = 3,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE = 4,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DECODE = 5,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_ENCODE = 6,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_ARTIFACT = 7,
    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_IO_ERROR = 8
} character_player_snapshot_v1_capture_result;

typedef int (*character_player_snapshot_v1_capture_decode)(
    void *opaque, int stage_fd, struct creature **player_out);
typedef void (*character_player_snapshot_v1_capture_release)(
    void *opaque, struct creature *player);

typedef struct character_player_snapshot_v1_capture_report {
    uint64_t attempted;
    uint64_t recorded;
    uint64_t exact_retries;
    uint64_t failed;
    character_player_snapshot_v1_capture_result last_result;
    int last_artifact_result;
    char last_source_post_sha256[65];
} character_player_snapshot_v1_capture_report;

typedef struct character_player_snapshot_v1_capture {
    character_player_snapshot_v1_capture_decode decode;
    void *decode_opaque;
    character_player_snapshot_v1_capture_release release;
    void *release_opaque;
    character_player_snapshot_v1_capture_report report;
} character_player_snapshot_v1_capture;

void character_player_snapshot_v1_capture_init(
    character_player_snapshot_v1_capture *capture,
    character_player_snapshot_v1_capture_decode decode,
    void *decode_opaque,
    character_player_snapshot_v1_capture_release release,
    void *release_opaque);

/* Matches character_save_journal_v2_prepared_stage_observer.  It rereads the
 * immutable PREPARED record and stage through the held writer capability,
 * then writes only a detached PlayerSnapshotV1 artifact.  Every nonzero
 * result is diagnostic: the protocol/recovery caller remains authoritative. */
int character_player_snapshot_v1_capture_observe(
    void *opaque,
    const character_save_journal_v2_writer_context *writer,
    const char *command_uuid);

#endif
