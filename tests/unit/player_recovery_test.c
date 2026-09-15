#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mstruct.h"
#include "player_recovery.h"
#include "player_store.h"

static int save_calls;
static int free_calls;
static int saves_before_success;

int file_player_store_save(char *name, creature *player)
{
    (void)name;
    (void)player;
    return PLAYER_STORE_IO_ERROR;
}

int file_player_store_load(char *name, creature **player)
{
    (void)name;
    (void)player;
    return PLAYER_STORE_NOT_FOUND;
}

static int test_save(void *opaque, char *name, creature *player)
{
    (void)opaque;
    (void)name;
    (void)player;
    save_calls++;
    return save_calls > saves_before_success ? PLAYER_STORE_OK : PLAYER_STORE_IO_ERROR;
}

static int test_load(void *opaque, char *name, creature **player)
{
    (void)opaque;
    (void)name;
    (void)player;
    return PLAYER_STORE_NOT_FOUND;
}

void free_crt(creature *player)
{
    free_calls++;
    free(player);
}

void log_f()
{
}

static creature *new_player(const char *name)
{
    creature *player = (creature *)calloc(1, sizeof(creature));
    if(player) strcpy(player->name, name);
    return player;
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "player_recovery_test: %s\n", message);
    return 1;
}

int main(void)
{
    player_store_ops store = { test_save, test_load, 0 };
    creature *player, *quarantined;
    int i, failed = 0;

    if(player_store_set(&store) < 0) return 1;

    player = new_player("Recover");
    player->fd = 19;
    saves_before_success = 1;
    save_calls = free_calls = 0;
    failed += expect(player_recovery_enqueue(player) == 0,
                     "failed disconnect must transfer ownership to recovery");
    failed += expect(player->fd == -1,
                     "recovery ownership must clear a stale disconnected fd");
    failed += expect(player_recovery_pending() == 1 && player_recovery_login_blocked(),
                     "any queued player must globally block new logins");
    failed += expect(player_recovery_retry_one() == PLAYER_STORE_IO_ERROR,
                     "failed retry must retain player ownership");
    failed += expect(player_recovery_pending() == 1 && free_calls == 0,
                     "failed retry must not free unsaved player");
    failed += expect(player_recovery_retry_one() == PLAYER_STORE_OK,
                     "next retry must persist the queued player");
    failed += expect(player_recovery_pending() == 0 && free_calls == 1,
                     "successful retry must release recovery ownership");

    for(i=0; i<PLAYER_RECOVERY_LIMIT; i++) {
        char name[20];
        sprintf(name, "Queue%d", i);
        failed += expect(player_recovery_enqueue(new_player(name)) == 0,
                         "queue capacity must accept bounded entries");
    }
    quarantined = new_player("Emergency");
    failed += expect(player_recovery_enqueue(quarantined) == 0,
                     "normal queue exhaustion must transfer ownership to emergency quarantine");
    failed += expect(player_recovery_pending() == PLAYER_RECOVERY_LIMIT + 1 &&
                     player_recovery_login_blocked(),
                     "emergency quarantine must keep login globally blocked");
    player_recovery_reset();
    player_store_reset();

    if(failed) return 1;
    puts("player_recovery_test: ok");
    return 0;
}
