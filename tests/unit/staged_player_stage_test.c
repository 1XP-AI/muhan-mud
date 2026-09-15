/*
 * Staged MUD1O provisioning must not publish or otherwise initialize a
 * creature before the Gateway has committed.  This test links only the staged
 * check from player.c (with section GC) so it exercises the production
 * function without constructing a world.
 */
#include "mstruct.h"

#include <stdio.h>
#include <string.h>

extern int init_staged_ply();

static int expect(int value, const char *message)
{
    if(value) return 0;
    fprintf(stderr, "FAIL: %s\n", message);
    return 1;
}

int main(void)
{
    creature player;
    creature before;
    room room;
    int failed = 0;

    memset(&player, 0, sizeof(player));
    memset(&room, 0, sizeof(room));
    player.fd = 7;
    player.rom_num = 1;
    strcpy(player.name, "Staged");
    before = player;

    failed += expect(init_staged_ply(&player) == 0,
                     "an unpublished staged player must be accepted");
    failed += expect(memcmp(&player, &before, sizeof(player)) == 0,
                     "staging must not mutate persisted or runtime player state");
    failed += expect(room.first_ply == 0 && room.beenhere == 0,
                     "staging must not insert into a room or increment beenhere");

    player.parent_rom = &room;
    before = player;
    failed += expect(init_staged_ply(&player) < 0,
                     "an already-published player must not be staged again");
    failed += expect(memcmp(&player, &before, sizeof(player)) == 0,
                     "a rejected stage request must leave the player unchanged");
    failed += expect(init_staged_ply(0) < 0,
                     "a null staged player must be rejected");

    return failed ? 1 : 0;
}
