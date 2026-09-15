#include "character_player_snapshot_v1_capture_native.h"

#include "player_snapshot_v1.h"

#include <stdlib.h>
#include <string.h>

extern int read_crt_player(int fd, creature *player);
extern void free_crt(creature *player);

static int capture_native_decode(opaque,stage_fd,player_out)
void *opaque;
int stage_fd;
struct creature **player_out;
{
    creature *player;
    (void)opaque;
    if(player_out)*player_out=0;
    if(stage_fd<0||!player_out)return -1;
    player=(creature *)malloc(sizeof(*player));
    if(!player)return -1;
    memset(player,0,sizeof(*player));
    if(read_crt_player(stage_fd,player)<0) {
        /* The bounded decoder scrubs every runtime link before returning,
         * including partial-read failures.  Force the player-only free path. */
        player->type=PLAYER;
        free_crt(player);
        return -1;
    }
    *player_out=player;
    return 0;
}

static void capture_native_release(opaque,player)
void *opaque;
struct creature *player;
{
    (void)opaque;
    if(!player)return;
    player->type=PLAYER;
    free_crt(player);
}

void character_player_snapshot_v1_capture_native_init(capture)
character_player_snapshot_v1_capture *capture;
{
    character_player_snapshot_v1_capture_init(capture,capture_native_decode,0,
        capture_native_release,0);
}
