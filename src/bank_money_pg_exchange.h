#ifndef MUHAN_BANK_MONEY_PG_EXCHANGE_H
#define MUHAN_BANK_MONEY_PG_EXCHANGE_H
#include <libpq-fe.h>
/* Internal bounded exchange shared by native read/commit adapters. NULL means
 * unknown transport outcome; discard the borrowed connection. Caller clears
 * a non-NULL result. No diagnostics or credentials are retained. */
PGresult *bank_money_pg_exchange(PGconn *,const char *,int,const Oid *,const char *const *,const int *,const int *,int);
#endif
