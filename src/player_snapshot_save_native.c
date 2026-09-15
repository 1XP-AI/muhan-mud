#include "player_snapshot_save_native.h"
#include "player_snapshot_v1.h"
#include "bank_money_pg_exchange.h"
#include "bank_money_plan_native.h"
#include <stdlib.h>
#include <string.h>
int player_snapshot_save_native(void *connection,const char *const text[8],
    const unsigned char *payload,size_t length,int timeout_ms,uint64_t *revision)
{
    const char *values[9];int lengths[9]={0},formats[9]={0};Oid types[9]={0};
    PGresult *r;creature *clone=NULL;uint64_t expected=0,parsed=0;
    const unsigned char *raw;size_t n;int i,status=PLAYER_SNAPSHOT_SAVE_UNKNOWN;
    if(!revision) return PLAYER_SNAPSHOT_SAVE_INVALID;
    *revision=0;
    if(!connection||!text||!payload||length<48||length>4194304||timeout_ms<1||timeout_ms>10000)
        return PLAYER_SNAPSHOT_SAVE_INVALID;
    for(i=0;i<8;i++) {
        if(!text[i]) return PLAYER_SNAPSHOT_SAVE_INVALID;
        for(n=0;n<=512&&text[i][n];n++) {}
        if(!n||n>512) return PLAYER_SNAPSHOT_SAVE_INVALID;
        values[i]=text[i];
    }
    if(text[6][0]=='0'&&text[6][1]) return PLAYER_SNAPSHOT_SAVE_INVALID;
    for(n=0;text[6][n];n++) {
        unsigned digit=(unsigned)(text[6][n]-'0');
        if(digit>9||expected>((uint64_t)INT64_MAX-2-digit)/10) return PLAYER_SNAPSHOT_SAVE_INVALID;
        expected=expected*10+digit;
    }
    if(strlen(text[7])!=64) return PLAYER_SNAPSHOT_SAVE_INVALID;
    for(i=0;i<64;i++) if(!((text[7][i]>='0'&&text[7][i]<='9')||(text[7][i]>='a'&&text[7][i]<='f')))
        return PLAYER_SNAPSHOT_SAVE_INVALID;
    if(player_snapshot_v1_decode_clone(payload,length,&clone)) return PLAYER_SNAPSHOT_SAVE_INVALID;
    i=strcmp(clone->name,text[1]);player_snapshot_v1_free_clone(clone);
    if(i) return PLAYER_SNAPSHOT_SAVE_INVALID;
    values[8]=(const char *)payload;lengths[8]=(int)length;formats[8]=1;types[8]=17;
    r=bank_money_pg_exchange(connection,
      "select * from private.commit_player_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9)",
      9,types,values,lengths,formats,timeout_ms);
    if(!r) return status;
    if(PQresultStatus(r)==PGRES_FATAL_ERROR) {
        const char *state=PQresultErrorField(r,PG_DIAG_SQLSTATE);
        if(state&&(!strcmp(state,"P0001")||!strcmp(state,"22023")||!strcmp(state,"40001")
           ||!strcmp(state,"40P01")||!strcmp(state,"42501")||!strcmp(state,"55P03")||!strcmp(state,"57014")))
            status=PLAYER_SNAPSHOT_SAVE_REJECTED;
        goto done;
    }
    if(PQresultStatus(r)!=PGRES_TUPLES_OK||PQntuples(r)!=1||PQnfields(r)!=2
       ||PQgetisnull(r,0,0)||PQgetisnull(r,0,1)||PQftype(r,0)!=25||PQftype(r,1)!=20
       ||PQfformat(r,0)!=1||PQfformat(r,1)!=1||PQgetlength(r,0,1)!=8) goto done;
    raw=(const unsigned char *)PQgetvalue(r,0,1);
    if(raw[0]&128) goto done;
    for(i=0;i<8;i++) parsed=(parsed<<8)|raw[i];
    if(parsed!=expected+1) goto done;
    if(PQgetlength(r,0,0)==9&&!memcmp(PQgetvalue(r,0,0),"COMMITTED",9)) status=PLAYER_SNAPSHOT_SAVE_COMMITTED;
    else if(PQgetlength(r,0,0)==11&&!memcmp(PQgetvalue(r,0,0),"EXACT_RETRY",11)) status=PLAYER_SNAPSHOT_SAVE_RETRY;
    if(status>0) *revision=parsed;
done:
    PQclear(r);return status;
}
int player_snapshot_save_prepared_native(void *connection,const char *node,const char *script,const char *root,
    const char *const values[8],const unsigned char *payload,size_t length,int timeout_ms,uint64_t *revision)
{
    const char *args[11];unsigned char *echo=NULL;size_t echoed=0;int i,result;
    if(!revision) return PLAYER_SNAPSHOT_SAVE_INVALID;
    *revision=0;
    if(!connection||!node||node[0]!='/'||!script||script[0]!='/'||!root||root[0]!='/'||!values
       ||!payload||length<48||length>4194304||timeout_ms<1||timeout_ms>10000) return PLAYER_SNAPSHOT_SAVE_NOT_SENT;
    args[0]=script;args[1]="--prepare";args[2]=root;
    for(i=0;i<8;i++) {if(!values[i]) return PLAYER_SNAPSHOT_SAVE_NOT_SENT;args[i+3]=values[i];}
    result=player_snapshot_process_native(node,args,11,payload,length,timeout_ms,&echo,&echoed);
    if(result||echoed!=length||!echo||memcmp(echo,payload,length)) {
        free(echo);return PLAYER_SNAPSHOT_SAVE_NOT_SENT;
    }
    free(echo);
    return player_snapshot_save_native(connection,values,payload,length,timeout_ms,revision);
}
