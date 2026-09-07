#ifndef MUHAN_BANK_MONEY_COMMIT_NATIVE_H
#define MUHAN_BANK_MONEY_COMMIT_NATIVE_H
#include <stddef.h>
#include <stdint.h>
enum bank_money_commit_status {
    BANK_MONEY_COMMIT_INVALID=-2, BANK_MONEY_COMMIT_REJECTED=-1,
    BANK_MONEY_COMMIT_UNKNOWN=0, BANK_MONEY_COMMIT_CONFIRMED=1, BANK_MONEY_COMMIT_RETRY=2
};
/* Borrow connected idle writer PGconn. Text args: seven authority fields,
 * command UUID, expected revision, direction, amount. Frame is Rust output.
 * UNKNOWN includes missing/malformed acknowledgement: preserve this exact
 * command and bytes and discard connection; NEVER invent a replacement command.
 * Only CONFIRMED/RETRY publishes revision. No live wallet mutation here. */
int bank_money_commit_native(void *,const char *const [11],const unsigned char *,size_t,int,uint64_t *);
#endif
