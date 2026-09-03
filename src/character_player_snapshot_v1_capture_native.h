#ifndef CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_NATIVE_H
#define CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_NATIVE_H

#include "character_player_snapshot_v1_capture.h"

/* Binds the bounded legacy player-file decoder used by file_player_store.
 * The capture core continues to own all descriptor validation and lifetime. */
void character_player_snapshot_v1_capture_native_init(
    character_player_snapshot_v1_capture *capture);

#endif
