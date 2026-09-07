#include "player_paired_route_native.h"
#include <libpq-fe.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
int main(int argc,char **argv)
{
    player_paired_route_context ctx;player_paired_route zero;
    PGconn *c;PGresult *r;int status;
    if(argc!=5) return 2;
    memset(&ctx,0,sizeof(ctx));memset(&zero,0,sizeof(zero));
    c=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    if(PQstatus(c)!=CONNECTION_OK) {PQfinish(c);return 2;}
    r=PQexec(c,"set role mud_writer");
    if(PQresultStatus(r)!=PGRES_COMMAND_OK) {PQclear(r);PQfinish(c);return 2;}
    PQclear(r);ctx.connection=c;ctx.world=argv[1];ctx.writer=argv[3];ctx.epoch=argv[4];ctx.timeout_ms=2000;
    memset(&ctx.last,0x55,sizeof(ctx.last));
    status=player_paired_route_select(&ctx,argv[2]);PQfinish(c);
    if(status!=1) {if(memcmp(&ctx.last,&zero,sizeof(zero))) abort();return 1;}
    printf("%s %s %llu %s %s\n",ctx.last.character_id,ctx.last.owner_user_id,(unsigned long long)ctx.last.revision,ctx.last.player_hash,ctx.last.bank_hash);
    return 0;
}
