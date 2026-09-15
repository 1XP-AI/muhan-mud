#ifndef MUHAN_BANK_MONEY_READ_NATIVE_H
#define MUHAN_BANK_MONEY_READ_NATIVE_H
#include <stddef.h>
#include <stdint.h>
typedef struct bank_money_read_result {
    uint64_t revision;
    unsigned char *frame;
    size_t frame_length;
    char player_hash[65],bank_hash[65];
} bank_money_read_result;
/* Borrow an already authenticated idle PGconn. Arguments are character,
 * world, actor, session, gateway, writer, epoch. No connection establishment
 * or credentials here. 1..10000 ms wall-clock deadline covers query exchange.
 * On any failure discard the connection (possibly unfinished query), and no
 * result is published. Free successful frame with free(). */
int bank_money_read_native(void *,const char *const [7],int,bank_money_read_result *);
#endif
