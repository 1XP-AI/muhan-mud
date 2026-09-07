#ifndef MUHAN_BANK_MONEY_RESULT_NATIVE_H
#define MUHAN_BANK_MONEY_RESULT_NATIVE_H
#include "bank_money_coordinate_native.h"
#include "bank_money_route.h"
/* Convert only a fresh, confirmed coordinator result to a command reply.
 * Validates both codecs, revision and current normalized player state. Does
 * not mutate memory, establish authority, or recover historical requests. */
int bank_money_result_native(const struct creature *,int,uint64_t,int,
    const bank_money_coordinate_result *,bank_money_ack *);
#endif
