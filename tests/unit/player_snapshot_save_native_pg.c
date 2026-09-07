#include "player_snapshot_save_native.h"
#include <libpq-fe.h>
#include <stdio.h>
#include <stdlib.h>
int main(int argc,char **argv)
{
    unsigned char *payload;size_t length;PGconn *c;PGresult *r;
    uint64_t revision=99;int status;
    if(argc!=9&&argc!=12) return 2;
    payload=malloc(4194305);if(!payload) return 2;
    length=fread(payload,1,4194305,stdin);
    if(ferror(stdin)) {free(payload);return 2;}
    c=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    if(PQstatus(c)!=CONNECTION_OK) {free(payload);PQfinish(c);return 2;}
    r=PQexec(c,"set role mud_writer");
    if(PQresultStatus(r)!=PGRES_COMMAND_OK) {PQclear(r);free(payload);PQfinish(c);return 2;}
    PQclear(r);
    if(argc==12) status=player_snapshot_save_prepared_native(c,argv[9],argv[10],argv[11],
        (const char *const *)(argv+1),payload,length,1500,&revision);
    else status=player_snapshot_save_native(c,(const char *const *)(argv+1),payload,length,1500,&revision);
    free(payload);PQfinish(c);
    if(status<=0&&revision) abort();
    printf("%d %llu\n",status,(unsigned long long)revision);
    return 0;
}
