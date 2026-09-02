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

/* At most one managed binding owns the facade.  Plain set/reset calls are
 * explicit takeovers: they clear this token so a stale owner can never
 * restore over a newer choice. */
static player_store_binding *active_binding;

static int player_store_ops_valid(const player_store_ops *ops)
{
    return ops && ops->save && ops->load;
}

static void player_store_binding_clear(player_store_binding *binding)
{
    binding->previous.save = 0;
    binding->previous.load = 0;
    binding->previous.opaque = 0;
    binding->active = 0;
}

int player_store_set(const player_store_ops *ops)
{
    if(!player_store_ops_valid(ops))
        return -1;

    active_store = *ops;
    active_binding = 0;
    return 0;
}

void player_store_reset(void)
{
    active_store = file_store;
    active_binding = 0;
}

int player_store_bind(const player_store_ops *ops,
    player_store_binding *binding)
{
    if(!player_store_ops_valid(ops) || !binding || binding->active ||
       active_binding)
        return -1;

    binding->previous = active_store;
    binding->active = 1;
    active_store = *ops;
    active_binding = binding;
    return 0;
}

player_store_unbind_result player_store_unbind(
    player_store_binding *binding)
{
    if(!binding)
        return PLAYER_STORE_UNBIND_INVALID;
    if(!binding->active)
        return PLAYER_STORE_UNBIND_NOT_CURRENT;
    if(active_binding != binding) {
        player_store_binding_clear(binding);
        return PLAYER_STORE_UNBIND_NOT_CURRENT;
    }

    active_store = binding->previous;
    active_binding = 0;
    player_store_binding_clear(binding);
    return PLAYER_STORE_UNBIND_RESTORED;
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
