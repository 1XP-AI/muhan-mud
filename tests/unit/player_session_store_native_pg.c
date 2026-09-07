#include "player_session_store_native.h"
#include "player_session_registry.h"
#include "player_snapshot_v1.h"
#include "player_recovery.h"
#include <libpq-fe.h>
#include <assert.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <sys/socket.h>
extern int savegame_nomsg(creature *);
static int save_errors;
void merror(char *message,char kind)
{(void)message;if(kind==FATAL) abort();save_errors++;}
void log_f(char *message,...) {(void)message;}
void del_active(creature *player) {(void)player;abort();}
int file_player_store_save(char *name,creature *p) {(void)name;(void)p;abort();}
int file_player_store_load(char *name,creature **p) {(void)name;(void)p;abort();}
int main(int argc,char **argv)
{
    PGconn *db;PGresult *r;player_session_store ctx,peer;player_store_ops ops;player_session_registry registry;
    player_store_binding binding;creature *loaded=NULL,*again=(creature *)1,copy;
    if(argc!=10) return 2;
    db=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    assert(PQstatus(db)==CONNECTION_OK);r=PQexec(db,"set role mud_writer");
    assert(PQresultStatus(r)==PGRES_COMMAND_OK);PQclear(r);
    memset(&ctx,0,sizeof(ctx));memset(&binding,0,sizeof(binding));
    memset(&peer,0,sizeof(peer));memset(&registry,0,sizeof(registry));
    assert(!player_session_store_init(&ctx,db,argv[1],argv[2],argv[3],argv[4],argv[5],argv[6],argv[7],argv[8],1500));
    assert(!player_session_store_init(&peer,db,argv[1],"Peerhero",argv[3],argv[4],"c9300000-0000-0000-0000-000000000001",argv[6],argv[7],argv[8],1500));
    assert(!player_session_registry_add(&registry,&ctx));assert(!player_session_registry_add(&registry,&peer));
    assert(player_session_registry_add(&registry,&ctx)!=0);
    ops=player_session_registry_build(&registry);assert(!player_store_bind(&ops,&binding));
    assert(load_ply("Missinghero",&again)==PLAYER_STORE_IO_ERROR&&!again);
    {
        creature *p=NULL,*decoded=NULL,detached,normalized,before;
        object pack,blade,pack_before,blade_before;otag inventory,equipment;
        unsigned char *expected=NULL;size_t expected_length=0;
        assert(load_ply("Peerhero",&p)==PLAYER_STORE_OK);
        assert(!p->first_obj);
        detached=*p;detached.fd=-1;detached.gold=201;
        memset(&pack,0,sizeof(pack));memset(&blade,0,sizeof(blade));
        memset(&inventory,0,sizeof(inventory));memset(&equipment,0,sizeof(equipment));
        strcpy(pack.name,"Apack");strcpy(blade.name,"Zblade");blade.wearflag=1;
        inventory.obj=&pack;equipment.obj=&blade;
        detached.first_obj=&inventory;detached.ready[0]=&blade;
        // Independently encode the expected persisted, unequipped inventory.
        normalized=detached;normalized.ready[0]=NULL;inventory.next_tag=&equipment;
        pack.parent_crt=&normalized;blade.parent_crt=&normalized;
        assert(!player_snapshot_v1_encode_loaded(&normalized,&expected,&expected_length));
        inventory.next_tag=NULL;pack.parent_crt=&detached;blade.parent_crt=NULL;
        before=detached;pack_before=pack;blade_before=blade;
        {
            PGconn *broken=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
            creature *queued=NULL,*unchanged=NULL;PGresult *check;
            const char *read_args[6]={argv[1],"Peerhero",argv[3],argv[4],peer.fields[4],"0"};
            assert(PQstatus(broken)==CONNECTION_OK);assert(!shutdown(PQsocket(broken),SHUT_RDWR));
            peer.connection=broken;
            assert(savegame_nomsg(&detached)==PLAYER_STORE_IO_ERROR);
            assert(peer.status==PLAYER_SNAPSHOT_SAVE_UNKNOWN&&!peer.committed_revision);
            // A separate healthy reader proves this request never committed.
            check=PQexecParams(db,"select * from private.read_player_paired_snapshot($1,$2,$3,$4,$5,$6)",6,NULL,read_args,NULL,NULL,1);
            assert(PQresultStatus(check)==PGRES_TUPLES_OK&&PQntuples(check)==1);
            assert(!player_snapshot_v1_decode_clone((const unsigned char *)PQgetvalue(check,0,0),(size_t)PQgetlength(check,0,0),&unchanged));
            assert(unchanged->gold==100&&!unchanged->first_obj);
            PQclear(check);player_snapshot_v1_free_clone(unchanged);
            assert(!player_snapshot_v1_decode_clone(expected,expected_length,&queued));
            assert(!player_recovery_enqueue(queued)&&player_recovery_pending()==1);
            assert(player_recovery_retry_one()==PLAYER_STORE_IO_ERROR&&player_recovery_pending()==1);
            PQfinish(broken);peer.connection=db;
            assert(player_recovery_retry_one()==PLAYER_STORE_OK);
            assert(peer.status==PLAYER_SNAPSHOT_SAVE_COMMITTED&&peer.committed_revision==1);
            assert(!player_recovery_pending()&&!player_recovery_login_blocked());
            queued=NULL; /* queue's actual free_crt released the owned graph */
        }
        assert(savegame_nomsg(&detached)==PLAYER_STORE_OK);
        assert(peer.pending_length==expected_length&&!memcmp(peer.pending,expected,expected_length));
        assert(!memcmp(&detached,&before,sizeof(detached))&&!memcmp(&pack,&pack_before,sizeof(pack))
            &&!memcmp(&blade,&blade_before,sizeof(blade))&&!inventory.next_tag);
        assert(!player_snapshot_v1_decode_clone(peer.pending,peer.pending_length,&decoded));
        assert(decoded->first_obj&&!strcmp(decoded->first_obj->obj->name,"Apack"));
        assert(decoded->first_obj->next_tag&&!strcmp(decoded->first_obj->next_tag->obj->name,"Zblade"));
        assert(!decoded->first_obj->next_tag->next_tag&&!decoded->ready[0]);
        assert(savegame_nomsg(&detached)==PLAYER_STORE_OK&&peer.status==PLAYER_SNAPSHOT_SAVE_RETRY);
        assert(!memcmp(&detached,&before,sizeof(detached))&&!memcmp(&blade,&blade_before,sizeof(blade))&&!inventory.next_tag);
        assert(player_session_registry_remove(&registry,&peer)!=0);
        {
            PGconn *broken=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
            assert(PQstatus(broken)==CONNECTION_OK);
            // Shut down only this fixture-owned connection, not the DB server.
            assert(!shutdown(PQsocket(broken),SHUT_RDWR));peer.connection=broken;
            assert(!player_recovery_enqueue(decoded));
            assert(!player_recovery_enqueue(decoded)); /* same pointer: no duplicate ownership */
            assert(decoded->fd==-1&&player_recovery_pending()==1&&player_recovery_login_blocked());
            assert(player_recovery_retry_one()==PLAYER_STORE_IO_ERROR);
            assert(peer.status==PLAYER_SNAPSHOT_SAVE_UNKNOWN);
            assert(player_recovery_pending()==1&&player_recovery_login_blocked());
            assert(peer.pending_length==expected_length&&!memcmp(peer.pending,expected,expected_length));
            assert(player_session_registry_remove(&registry,&peer)!=0);
            PQfinish(broken);peer.connection=db;
            assert(player_recovery_retry_one()==PLAYER_STORE_OK);
            assert(peer.status==PLAYER_SNAPSHOT_SAVE_RETRY);
            decoded=NULL; /* actual free_crt owns destruction after confirmed save */
            assert(!player_recovery_pending()&&!player_recovery_login_blocked());
            assert(player_recovery_retry_one()==PLAYER_STORE_NOT_FOUND);
            assert(peer.pending_length==expected_length&&!memcmp(peer.pending,expected,expected_length));
            assert(player_session_registry_remove(&registry,&peer)!=0); /* durable lifecycle still unresolved */
        }
        free(expected);
        player_snapshot_v1_free_clone(p);
    }
    memset(&copy,0,sizeof(copy));strcpy(copy.name,argv[2]);
    assert(save_ply(argv[2],&copy)==PLAYER_STORE_IO_ERROR);
    assert(load_ply(argv[2],&loaded)==PLAYER_STORE_OK);
    assert(load_ply(argv[2],&again)==PLAYER_STORE_IO_ERROR&&!again);
    assert(!strcmp(ctx.fields[6],"0"));
    // savegame uses a shallow copy; disconnect/recovery need not have a live fd.
    copy=*loaded;copy.fd=-1;copy.gold=strtol(argv[9],NULL,10);
    assert(savegame_nomsg(&copy)==PLAYER_STORE_OK);
    assert(ctx.status==PLAYER_SNAPSHOT_SAVE_COMMITTED&&ctx.committed_revision==1);
    assert(!strcmp(ctx.fields[6],"0"));
    assert(savegame_nomsg(&copy)==PLAYER_STORE_OK&&ctx.status==PLAYER_SNAPSHOT_SAVE_RETRY);
    copy.gold++;assert(savegame_nomsg(&copy)==PLAYER_STORE_IO_ERROR);
    assert(save_errors==2);
    assert(!strcmp(ctx.fields[6],"0"));
    copy.gold--;assert(savegame_nomsg(&copy)==PLAYER_STORE_OK);
    assert(player_session_store_adopt(&ctx,&copy,"c9280000-0000-0000-0000-000000000001")!=0);
    assert(!strcmp(ctx.fields[6],"0")&&ctx.pending);
    puts("READY");fflush(stdout);
    assert(getchar()=='R');
    copy.gold++;assert(player_session_store_adopt(&ctx,&copy,"c9280000-0000-0000-0000-000000000001")!=0);
    copy.gold--;
    assert(!player_session_store_adopt(&ctx,&copy,"c9280000-0000-0000-0000-000000000001"));
    assert(!strcmp(ctx.fields[6],"1")&&!ctx.pending);
    assert(!strcmp(ctx.fields[5],"c9280000-0000-0000-0000-000000000001"));
    copy.gold++;
    assert(savegame_nomsg(&copy)==PLAYER_STORE_OK);
    assert(ctx.status==PLAYER_SNAPSHOT_SAVE_COMMITTED&&ctx.committed_revision==2);
    assert(!strcmp(ctx.fields[6],"1"));
    assert(savegame_nomsg(&copy)==PLAYER_STORE_OK&&ctx.status==PLAYER_SNAPSHOT_SAVE_RETRY);
    assert(save_errors==2);
    puts("READY2");fflush(stdout);
    assert(getchar()=='R');
    assert(!player_session_store_adopt(&ctx,&copy,"c9280000-0000-0000-0000-000000000002"));
    assert(!strcmp(ctx.fields[6],"2")&&!ctx.pending);
    assert(!player_session_registry_remove(&registry,&ctx));
    assert(save_ply(argv[2],&copy)==PLAYER_STORE_IO_ERROR);
    assert(player_session_registry_remove(&registry,&peer)!=0);
    assert(player_store_unbind(&binding)==PLAYER_STORE_UNBIND_RESTORED);
    player_session_store_dispose(&ctx);player_snapshot_v1_free_clone(loaded);PQfinish(db);
    player_session_store_dispose(&peer);
    return 0;
}
