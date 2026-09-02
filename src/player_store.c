#include "player_store.h"

static int default_file_save(void *opaque, char *name, struct creature *player)
{
    (void)opaque;
    return file_player_store_save(name, player);
}

static int default_file_load(void *opaque, char *name, struct creature **player)
{
    (void)opaque;
    return file_player_store_load(name, player);
}

static const player_store_ops file_store = {
    default_file_save,
    default_file_load,
    0
};

/* Store callbacks by value: callers often build test repositories on the
 * stack, so retaining the player_store_ops pointer would outlive it. */
static player_store_ops active_store = {
    default_file_save,
    default_file_load,
    0
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
    return active_store.save(active_store.opaque, name, player);
}

int load_ply(char *name, struct creature **player)
{
    if(!player)
        return PLAYER_STORE_IO_ERROR;
    *player = 0;
    return active_store.load(active_store.opaque, name, player);
}
