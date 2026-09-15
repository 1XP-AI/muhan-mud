#include "bank_store.h"

static int default_file_save(void *opaque, char *name, struct object *object)
{
    (void)opaque;
    return file_bank_store_save(name, object);
}

static int default_file_load(void *opaque, char *name, struct object **object)
{
    (void)opaque;
    return file_bank_store_load(name, object);
}

static const bank_store_ops file_store = {
    default_file_save,
    default_file_load,
    0
};

static bank_store_ops active_store = {
    default_file_save,
    default_file_load,
    0
};

static bank_store_binding *active_binding;

static int bank_store_ops_valid(const bank_store_ops *ops)
{
    return ops && ops->save && ops->load;
}

static int bank_store_result_normalize(int result)
{
    return result == BANK_STORE_OK ? BANK_STORE_OK : BANK_STORE_ERROR;
}

static void bank_store_binding_clear(bank_store_binding *binding)
{
    binding->previous.save = 0;
    binding->previous.load = 0;
    binding->previous.opaque = 0;
    binding->active = 0;
}

static int bank_store_load_from(const bank_store_ops *store,
    char *name, struct object **object)
{
    if(!object)
        return BANK_STORE_ERROR;
    *object = 0;
    if(bank_store_result_normalize(store->load(store->opaque, name, object))
       != BANK_STORE_OK) {
        /* The facade never exposes a failed load as a usable bank object.  A
         * custom store that allocates before failing remains responsible for
         * its own cleanup, because this generic boundary cannot know its
         * allocator. */
        *object = 0;
        return BANK_STORE_ERROR;
    }
    return BANK_STORE_OK;
}

int bank_store_set(const bank_store_ops *ops)
{
    if(!bank_store_ops_valid(ops))
        return -1;
    active_store = *ops;
    active_binding = 0;
    return 0;
}

void bank_store_reset(void)
{
    active_store = file_store;
    active_binding = 0;
}

int bank_store_bind(const bank_store_ops *ops, bank_store_binding *binding)
{
    if(!bank_store_ops_valid(ops) || !binding || binding->active ||
       active_binding)
        return -1;
    binding->previous = active_store;
    binding->active = 1;
    active_store = *ops;
    active_binding = binding;
    return 0;
}

bank_store_unbind_result bank_store_unbind(bank_store_binding *binding)
{
    if(!binding)
        return BANK_STORE_UNBIND_INVALID;
    if(!binding->active)
        return BANK_STORE_UNBIND_NOT_CURRENT;
    if(active_binding != binding) {
        bank_store_binding_clear(binding);
        return BANK_STORE_UNBIND_NOT_CURRENT;
    }
    active_store = binding->previous;
    active_binding = 0;
    bank_store_binding_clear(binding);
    return BANK_STORE_UNBIND_RESTORED;
}

int bank_store_save(char *name, struct object *object)
{
    return bank_store_result_normalize(active_store.save(active_store.opaque,
        name, object));
}

int bank_store_load(char *name, struct object **object)
{
    return bank_store_load_from(&active_store, name, object);
}
