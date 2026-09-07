#include "player_session_store_native.h"
#include "player_snapshot_v1.h"
#include <libpq-fe.h>
#include <assert.h>
#include <stdlib.h>
#include <string.h>
int file_player_store_save(char *name,creature *p) {(void)name;(void)p;abort();}
int file_player_store_load(char *name,creature **p) {(void)name;(void)p;abort();}
int main(int argc,char **argv)
{
    PGconn *db;PGresult *r;player_session_store ctx;player_store_ops ops;
    player_store_binding binding;creature *loaded=NULL,*again=(creature *)1,copy;
    if(argc!=10) return 2;
    db=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    assert(PQstatus(db)==CONNECTION_OK);r=PQexec(db,"set role mud_writer");
    assert(PQresultStatus(r)==PGRES_COMMAND_OK);PQclear(r);
    memset(&ctx,0,sizeof(ctx));memset(&binding,0,sizeof(binding));
    assert(!player_session_store_init(&ctx,db,argv[1],argv[2],argv[3],argv[4],argv[5],argv[6],argv[7],argv[8],1500));
    ops=player_session_store_build(&ctx);assert(!player_store_bind(&ops,&binding));
    memset(&copy,0,sizeof(copy));strcpy(copy.name,argv[2]);
    assert(save_ply(argv[2],&copy)==PLAYER_STORE_IO_ERROR);
    assert(load_ply(argv[2],&loaded)==PLAYER_STORE_OK);
    assert(load_ply(argv[2],&again)==PLAYER_STORE_IO_ERROR&&!again);
    assert(!strcmp(ctx.fields[6],"0"));
    // savegame uses a shallow copy; disconnect/recovery need not have a live fd.
    copy=*loaded;copy.fd=-1;copy.gold=strtol(argv[9],NULL,10);
    assert(save_ply(argv[2],&copy)==PLAYER_STORE_OK);
    assert(ctx.status==PLAYER_SNAPSHOT_SAVE_COMMITTED&&ctx.committed_revision==1);
    assert(!strcmp(ctx.fields[6],"0"));
    assert(save_ply(argv[2],&copy)==PLAYER_STORE_OK&&ctx.status==PLAYER_SNAPSHOT_SAVE_RETRY);
    copy.gold++;assert(save_ply(argv[2],&copy)==PLAYER_STORE_IO_ERROR);
    assert(!strcmp(ctx.fields[6],"0"));
    assert(player_store_unbind(&binding)==PLAYER_STORE_UNBIND_RESTORED);
    player_session_store_dispose(&ctx);player_snapshot_v1_free_clone(loaded);PQfinish(db);
    return 0;
}
