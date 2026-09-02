#include <stdio.h>
#include <string.h>

#include "player_store.h"

struct creature {
    int marker;
};

static int file_save_calls;
static int file_load_calls;
static int memory_save_calls;
static int memory_load_calls;
static int memory_save_opaque_calls;
static int memory_load_opaque_calls;
static int save_context;
static int load_context;

int file_player_store_save(char *name, struct creature *player)
{
    file_save_calls++;
    return (!strcmp(name, "file") && player && player->marker == 7) ? 11 : -1;
}

int file_player_store_load(char *name, struct creature **player)
{
    static struct creature loaded = { 8 };
    file_load_calls++;
    if(strcmp(name, "file"))
        return -1;
    *player = &loaded;
    return 12;
}

static int memory_save(void *opaque, char *name, struct creature *player)
{
    memory_save_calls++;
    if(opaque == &save_context) memory_save_opaque_calls++;
    return (!strcmp(name, "memory") && player && player->marker == 7) ? 21 : -1;
}

static int memory_load(void *opaque, char *name, struct creature **player)
{
    static struct creature loaded = { 9 };
    memory_load_calls++;
    if(opaque == &save_context) memory_load_opaque_calls++;
    if(strcmp(name, "memory"))
        return -1;
    *player = &loaded;
    return 22;
}

static int failing_load(void *opaque, char *name, struct creature **player)
{
    (void)opaque;
    (void)name;
    (void)player;
    return PLAYER_STORE_CORRUPT;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "player_store_test: %s\n", message);
    return 1;
}

int main(void)
{
    struct creature input = { 7 };
    struct creature *output = 0;
    player_store_binding binding;
    player_store_binding second_binding;
    player_store_ops memory_store = { memory_save, memory_load,
                                      &save_context };
    player_store_ops failing_store = { memory_save, failing_load,
                                       &save_context };
    player_store_ops invalid_store = { memory_save, 0, &save_context };
    int failed = 0;

    failed += expect(PLAYER_STORE_OK == 0 &&
                     PLAYER_STORE_NOT_FOUND < 0 &&
                     PLAYER_STORE_CORRUPT < 0 &&
                     PLAYER_STORE_IO_ERROR < 0,
                     "repository results must preserve legacy success/failure checks");

    failed += expect(save_ply("file", &input) == 11, "default save must use FileStore");
    failed += expect(load_ply("file", &output) == 12 && output->marker == 8,
                     "default load must use FileStore");
    failed += expect(file_save_calls == 1 && file_load_calls == 1,
                     "FileStore call counts must be exact");

    failed += expect(player_store_set(&invalid_store) == -1,
                     "incomplete repositories must be rejected");
    failed += expect(player_store_set(&memory_store) == 0,
                     "complete repository must be accepted");
    memory_store.opaque = &load_context;
    output = 0;
    failed += expect(save_ply("memory", &input) == 21,
                     "injected save must use MemoryStore");
    failed += expect(load_ply("memory", &output) == 22 && output->marker == 9,
                     "injected load must use MemoryStore");
    failed += expect(memory_save_calls == 1 && memory_load_calls == 1,
                     "MemoryStore call counts must be exact");
    failed += expect(memory_save_opaque_calls == 1 && memory_load_opaque_calls == 1,
                     "callbacks must receive the copied opaque context");
    failed += expect(player_store_set(&invalid_store) == -1 &&
                     save_ply("memory", &input) == 21,
                     "invalid repository must preserve the active store");

    memset(&binding, 0, sizeof(binding));
    memset(&second_binding, 0, sizeof(second_binding));
    failed += expect(player_store_bind(&failing_store, &binding) == 0 &&
                     binding.active,
                     "managed binding must install over an unmanaged store");
    failed += expect(player_store_bind(&memory_store, &second_binding) == -1 &&
                     !second_binding.active,
                     "a second managed binding must not steal ownership");
    output = (struct creature *)1;
    failed += expect(load_ply("memory", &output) == PLAYER_STORE_CORRUPT &&
                     output == 0,
                     "managed binding must become the active repository");
    failed += expect(player_store_unbind(&binding) ==
                     PLAYER_STORE_UNBIND_RESTORED && !binding.active,
                     "current managed binding must restore the prior repository");
    failed += expect(save_ply("memory", &input) == 21 &&
                     memory_save_opaque_calls == 3,
                     "managed unbind must restore the prior repository by value");

    memset(&binding, 0, sizeof(binding));
    failed += expect(player_store_bind(&failing_store, &binding) == 0,
                     "released binding storage must be reusable after zero init");
    failed += expect(player_store_set(&memory_store) == 0,
                     "an explicit external set must supersede a managed binding");
    failed += expect(player_store_unbind(&binding) ==
                     PLAYER_STORE_UNBIND_NOT_CURRENT && !binding.active,
                     "stale unbind must not overwrite a newer external repository");
    failed += expect(save_ply("memory", &input) == 21 &&
                     memory_save_opaque_calls == 3,
                     "newer external repository must remain active after stale unbind");

    failed += expect(player_store_set(&failing_store) == 0,
                     "failing repository must be injectable");
    output = (struct creature *)1;
    failed += expect(load_ply("memory", &output) == PLAYER_STORE_CORRUPT && output == 0,
                     "wrapper must clear partial output on repository failure");
    failed += expect(load_ply("memory", 0) == PLAYER_STORE_IO_ERROR,
                     "wrapper must reject a null output pointer");

    player_store_reset();
    failed += expect(save_ply("file", &input) == 11 && file_save_calls == 2,
                     "reset must restore FileStore");

    if(failed)
        return 1;

    puts("player_store_test: ok");
    return 0;
}
