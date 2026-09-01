#include "mstruct.h"
#include "mextern.h"
#include "player_store.h"
#include "player_recovery.h"

#include <string.h>

/* The first PMAX slots are the normal invariant-preserving queue.  The
 * emergency half only owns state if an impossible-at-runtime invariant breach
 * is observed; it keeps the pointer safe while the server fails closed. */
static creature *pending[PLAYER_RECOVERY_LIMIT + PLAYER_RECOVERY_EMERGENCY_LIMIT];
static int next_slot;

#define PLAYER_RECOVERY_TOTAL (PLAYER_RECOVERY_LIMIT + PLAYER_RECOVERY_EMERGENCY_LIMIT)

int player_recovery_pending(void)
{
    int i, count = 0;

    for(i=0; i<PLAYER_RECOVERY_TOTAL; i++)
        if(pending[i]) count++;
    return(count);
}

int player_recovery_login_blocked(void)
{
    return(player_recovery_pending() > 0);
}

int player_recovery_enqueue(creature *player)
{
    int i;

    if(!player || !player->name[0]) return(-1);
    for(i=0; i<PLAYER_RECOVERY_TOTAL; i++) {
        if(pending[i] == player)
            return(0);
    }
    for(i=0; i<PLAYER_RECOVERY_LIMIT; i++)
        if(!pending[i]) {
            pending[i] = player;
            player->fd = -1;
            log_f("player recovery: queued unsaved player %s\n", player->name);
            return(0);
        }

    for(i=PLAYER_RECOVERY_LIMIT; i<PLAYER_RECOVERY_TOTAL; i++)
        if(!pending[i]) {
            pending[i] = player;
            player->fd = -1;
            log_f("player recovery: invariant breach; emergency quarantine for %s\n",
                  player->name);
            return(0);
        }

    log_f("player recovery: emergency quarantine exhausted for %s\n", player->name);
    return(-1);
}

/* One attempt per call preserves round-robin fairness and lets io.c rate-limit
 * retry work while the primary server loop remains responsive. */
int player_recovery_retry_one(void)
{
    creature *player;
    int i, slot, result;

    for(i=0; i<PLAYER_RECOVERY_TOTAL; i++) {
        slot = (next_slot + i) % PLAYER_RECOVERY_TOTAL;
        if(!pending[slot]) continue;
        next_slot = (slot + 1) % PLAYER_RECOVERY_TOTAL;
        player = pending[slot];
        result = save_ply(player->name, player);
        if(result != PLAYER_STORE_OK)
            return(result);
        pending[slot] = 0;
        log_f("player recovery: saved queued player %s\n", player->name);
        free_crt(player);
        return(PLAYER_STORE_OK);
    }
    return(PLAYER_STORE_NOT_FOUND);
}

/* Test-only lifecycle support.  Production never calls this; queued players
 * stay owned by recovery until a successful save. */
void player_recovery_reset(void)
{
    int i;

    for(i=0; i<PLAYER_RECOVERY_TOTAL; i++) {
        if(pending[i]) {
            free_crt(pending[i]);
            pending[i] = 0;
        }
    }
    next_slot = 0;
}
