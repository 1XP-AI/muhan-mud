#include "bank_money_commit_native.h"
#include "bank_money_pg_exchange.h"
#include <string.h>
#include <stdlib.h>
#include <errno.h>
static size_t get32(const unsigned char *p)
{ return (size_t)p[0]*16777216U+(size_t)p[1]*65536U+(size_t)p[2]*256U+p[3]; }
int bank_money_commit_native(void *connection,const char *const text[11],const unsigned char *frame,size_t length,int timeout_ms,uint64_t *revision)
{
    const char *values[13]; int lengths[13]={0},formats[13]={0}; Oid types[13]={0};
    PGresult *result; size_t pl,bl; int i,status=BANK_MONEY_COMMIT_UNKNOWN;
    uint64_t parsed=0,expected; char *end; const unsigned char *raw;
    if(!revision) return BANK_MONEY_COMMIT_INVALID;
    *revision=0;
    if(!text||!frame||length<8||timeout_ms<1||timeout_ms>10000) return BANK_MONEY_COMMIT_INVALID;
    for(i=0;i<11;i++) { if(!text[i]) return BANK_MONEY_COMMIT_INVALID; values[i]=text[i]; }
    if(!text[8][0]) return BANK_MONEY_COMMIT_INVALID;
    for(i=0;text[8][i];i++) if(text[8][i]<'0'||text[8][i]>'9') return BANK_MONEY_COMMIT_INVALID;
    errno=0; expected=strtoull(text[8],&end,10);
    if(errno||*end||expected>INT64_MAX-2) return BANK_MONEY_COMMIT_INVALID;
    pl=get32(frame); bl=get32(frame+4);
    if(pl<48||pl>4194304||bl<55||bl>4194304||length!=8+pl+bl) return BANK_MONEY_COMMIT_INVALID;
    values[11]=(const char *)frame+8; values[12]=(const char *)frame+8+pl;
    lengths[11]=(int)pl; lengths[12]=(int)bl; formats[11]=formats[12]=1; types[11]=types[12]=17;
    result=bank_money_pg_exchange((PGconn *)connection,
      "select * from private.commit_qualified_money_transfer($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)",
      13,types,values,lengths,formats,timeout_ms);
    if(!result) return BANK_MONEY_COMMIT_UNKNOWN;
    if(PQresultStatus(result)==PGRES_FATAL_ERROR) {
        /* Only an explicit completed server error is a rejected transaction. */
        const char *state=PQresultErrorField(result,PG_DIAG_SQLSTATE);
        if(state&&(!strcmp(state,"P0001")||!strcmp(state,"22023")||!strcmp(state,"40001")
           ||!strcmp(state,"40P01")||!strcmp(state,"42501")||!strcmp(state,"55P03")||!strcmp(state,"57014"))) status=BANK_MONEY_COMMIT_REJECTED;
        goto done;
    }
    if(PQresultStatus(result)!=PGRES_TUPLES_OK||PQntuples(result)!=1||PQnfields(result)!=2
       ||PQgetisnull(result,0,0)||PQgetisnull(result,0,1)||PQftype(result,0)!=25||PQftype(result,1)!=20
       ||PQfformat(result,0)!=1||PQfformat(result,1)!=1||PQgetlength(result,0,1)!=8) goto done;
    raw=(const unsigned char *)PQgetvalue(result,0,1);
    if(raw[0]&128) goto done;
    for(i=0;i<8;i++) parsed=(parsed<<8)|raw[i];
    if(parsed!=expected+1) goto done;
    if(PQgetlength(result,0,0)==9&&!memcmp(PQgetvalue(result,0,0),"COMMITTED",9)) status=BANK_MONEY_COMMIT_CONFIRMED;
    else if(PQgetlength(result,0,0)==11&&!memcmp(PQgetvalue(result,0,0),"EXACT_RETRY",11)) status=BANK_MONEY_COMMIT_RETRY;
    if(status>0) *revision=parsed;
done:
    PQclear(result); return status;
}
