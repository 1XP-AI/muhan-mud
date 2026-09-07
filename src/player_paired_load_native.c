#include "player_paired_load_native.h"
#include "player_paired_route_native.h"
#include "player_snapshot_v1.h"
#include "player_store.h"
#include "bank_money_pg_exchange.h"
#include <stdio.h>
#include <string.h>
int player_paired_load_native(void *opaque,char *name,creature **out)
{
    player_paired_route_context *ctx=opaque;const char *args[6];char revision[32];
    PGresult *r;creature *clone=NULL;int status=PLAYER_STORE_IO_ERROR;
    if(!out) return status;
    *out=NULL;
    if(!ctx||!ctx->connection||!name||!ctx->world||!ctx->writer||!ctx->epoch||!ctx->last.character_id[0]) return status;
    snprintf(revision,sizeof(revision),"%llu",(unsigned long long)ctx->last.revision);
    args[0]=ctx->world;args[1]=name;args[2]=ctx->writer;args[3]=ctx->epoch;args[4]=ctx->last.character_id;args[5]=revision;
    r=bank_money_pg_exchange(ctx->connection,"select * from private.read_player_paired_snapshot($1,$2,$3,$4,$5,$6)",6,NULL,args,NULL,NULL,ctx->timeout_ms);
    if(!r) return status;
    if(PQresultStatus(r)!=PGRES_TUPLES_OK||PQntuples(r)!=1||PQnfields(r)!=2
       ||PQgetisnull(r,0,0)||PQgetisnull(r,0,1)||PQftype(r,0)!=17||PQftype(r,1)!=25
       ||PQfformat(r,0)!=1||PQfformat(r,1)!=1||PQgetlength(r,0,0)<48||PQgetlength(r,0,0)>4194304
       ||PQgetlength(r,0,1)!=64||memcmp(PQgetvalue(r,0,1),ctx->last.player_hash,64)) goto done;
    status=PLAYER_STORE_CORRUPT;
    if(player_snapshot_v1_decode_clone((const unsigned char *)PQgetvalue(r,0,0),(size_t)PQgetlength(r,0,0),&clone)
       ||strcmp(clone->name,name)) goto done;
    *out=clone;clone=NULL;status=PLAYER_STORE_OK;
done:
    player_snapshot_v1_free_clone(clone);PQclear(r);return status;
}
