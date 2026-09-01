#ifndef PLAYER_RECOVERY_H
#define PLAYER_RECOVERY_H

#include "mtype.h"

struct creature;

/* A disconnected player remains owned here until its complete snapshot is
 * persisted.  The fixed limit keeps a storage outage from consuming memory
 * without bound. */
#define PLAYER_RECOVERY_LIMIT PMAX
#define PLAYER_RECOVERY_EMERGENCY_LIMIT PMAX

int player_recovery_enqueue(struct creature *player);
int player_recovery_retry_one(void);
int player_recovery_pending(void);
int player_recovery_login_blocked(void);
void player_recovery_reset(void);

#endif
