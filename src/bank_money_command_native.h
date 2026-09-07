#ifndef MUHAN_BANK_MONEY_COMMAND_NATIVE_H
#define MUHAN_BANK_MONEY_COMMAND_NATIVE_H
#include "bank_money_live_native.h"
#include "bank_money_route.h"
/* Single-command, exclusively owned context. Caller supplies a fresh immutable
 * command identity, qualified connection and recovery-fenced source revision.
 * Not a global route installer or a durable pending fence. Never reuse/reset
 * after an attempt; retain the original durable request on ambiguous outcome. */
typedef struct bank_money_command_context {
    void *connection;
    bank_money_live_request request;
    const char *planner,*node,*script,*root;
    int timeout_ms,used,status;
    uint64_t revision;
} bank_money_command_context;
int bank_money_command_native(void *,const struct creature *,const struct cmd *,int,bank_money_ack *);
#endif
