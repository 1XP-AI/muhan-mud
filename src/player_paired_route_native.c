#include "player_paired_route_native.h"
#include "bank_money_pg_exchange.h"
#include <string.h>
static void uuid_text(const unsigned char *p,char out[37])
{
    static const char hex[]="0123456789abcdef";int i,j=0;
    for(i=0;i<16;i++) {
        if(i==4||i==6||i==8||i==10) out[j++]='-';
        out[j++]=hex[p[i]>>4];out[j++]=hex[p[i]&15];
    }
    out[j]=0;
}
int player_paired_route_select(void *opaque,const char *name)
{
    player_paired_route_context *ctx=opaque;player_paired_route value;
    PGresult *r;const char *args[4];const unsigned char *p;int i,j,status=-1;
    const Oid types[5]={2950,2950,20,25,25};const int sizes[5]={16,16,8,64,64};
    if(!ctx) return -1;
    memset(&ctx->last,0,sizeof(ctx->last));memset(&value,0,sizeof(value));
    if(!ctx->connection||!ctx->world||!ctx->writer||!ctx->epoch||!name) return -1;
    args[0]=ctx->world;args[1]=name;args[2]=ctx->writer;args[3]=ctx->epoch;
    r=bank_money_pg_exchange(ctx->connection,"select * from private.resolve_player_paired_route($1,$2,$3,$4)",4,NULL,args,NULL,NULL,ctx->timeout_ms);
    if(!r) return -1;
    if(PQresultStatus(r)!=PGRES_TUPLES_OK||PQntuples(r)!=1||PQnfields(r)!=5) goto done;
    for(i=0;i<5;i++) if(PQgetisnull(r,0,i)||PQftype(r,i)!=types[i]||PQfformat(r,i)!=1||PQgetlength(r,0,i)!=sizes[i]) goto done;
    uuid_text((const unsigned char *)PQgetvalue(r,0,0),value.character_id);
    uuid_text((const unsigned char *)PQgetvalue(r,0,1),value.owner_user_id);
    p=(const unsigned char *)PQgetvalue(r,0,2);if(p[0]&128) goto done;
    for(i=0;i<8;i++) value.revision=(value.revision<<8)|p[i];
    for(i=3;i<5;i++) {
        p=(const unsigned char *)PQgetvalue(r,0,i);
        for(j=0;j<64;j++) if(!((p[j]>='0'&&p[j]<='9')||(p[j]>='a'&&p[j]<='f'))) goto done;
        memcpy(i==3?value.player_hash:value.bank_hash,p,64);
    }
    ctx->last=value;status=1;
done:
    PQclear(r);return status;
}
