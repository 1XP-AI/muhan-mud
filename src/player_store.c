#include "player_store.h"

static const player_store_ops file_store = {
    file_player_store_save,
    file_player_store_load
};

/* Store callbacks by value: callers often build test repositories on the
 * stack, so retaining the player_store_ops pointer would outlive it. */
static player_store_ops active_store = {
    file_player_store_save,
    file_player_store_load
};

int player_store_set(const player_store_ops *ops)
{
    if(!ops || !ops->save || !ops->load)
        return -1;

    active_store = *ops;
    return 0;
}

void player_store_reset(void)
{
    active_store = file_store;
}

int save_ply(char *name, struct creature *player)
{
    return active_store.save(name, player);
}

int load_ply(char *name, struct creature **player)
{
    if(!player)
        return PLAYER_STORE_IO_ERROR;
    *player = 0;
    return active_store.load(name, player);
}
