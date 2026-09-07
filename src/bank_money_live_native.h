#ifndef MUHAN_BANK_MONEY_LIVE_NATIVE_H
#define MUHAN_BANK_MONEY_LIVE_NATIVE_H
#include "bank_money_coordinate_native.h"
struct creature;
typedef struct bank_money_live_request {
    const char *world_id,*writer_id,*writer_epoch;
    const char *command_id,*expected_revision,*direction,*amount;
} bank_money_live_request;
/* Descriptor-bound entry to the native coordinator, not a route installer.
 * Actor/character/session/gateway come only from the current Ply slot.
 * Runtime supplies world/writer/command/revision; DB revalidates authority.
 * No live mutation. If the slot/identity/wallet changed while awaiting commit,
 * discard output and return UNKNOWN: retain the original pending request.
 * Caller must still serialize other persisted state and fence pending work. */
int bank_money_live_native(void *,const struct creature *,const bank_money_live_request *,
    const char *,const char *,const char *,const char *,int,bank_money_coordinate_result *);
#endif
