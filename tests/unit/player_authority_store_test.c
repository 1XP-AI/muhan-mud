#include "mstruct.h"
#include "player_authority_store.h"
#include <assert.h>
#include <string.h>
static int selection=1,db_calls,legacy_calls,db_status;
static creature loaded;
int file_player_store_save(char *n,creature *p) {(void)n;(void)p;legacy_calls++;return 0;}
int file_player_store_load(char *n,creature **p) {(void)n;*p=&loaded;legacy_calls++;return 0;}
static int choose(void *c,const char *name) {(void)c;assert(!strcmp(name,"Hero"));return selection;}
static int db_save(void *c,char *n,creature *p) {(void)c;assert(!strcmp(n,p->name));db_calls++;return db_status;}
static int legacy_save(void *c,char *n,creature *p) {(void)c;return file_player_store_save(n,p);}
static int db_load(void *c,char *n,creature **p) {(void)c;(void)n;db_calls++;*p=db_status?NULL:&loaded;return db_status;}
static int legacy_load(void *c,char *n,creature **p) {(void)c;return file_player_store_load(n,p);}
int main(void)
{
    player_authority_store store; player_store_ops ops; player_store_binding binding;
    creature live,copy,*out;int mode;
    memset(&store,0,sizeof(store));memset(&binding,0,sizeof(binding));memset(&live,0,sizeof(live));
    strcpy(live.name,"Hero");live.fd=7;copy=live;copy.fd=-1;
    store.select=choose;store.legacy.save=legacy_save;store.legacy.load=legacy_load;
    store.database.save=db_save;store.database.load=db_load;
    ops=player_authority_store_build(&store);assert(!player_store_bind(&ops,&binding));
    assert(!save_ply("Hero",&live));assert(!save_ply("Hero",&copy));assert(db_calls==2&&!legacy_calls);
    db_status=PLAYER_STORE_IO_ERROR;
    assert(save_ply("Hero",&copy)==PLAYER_STORE_IO_ERROR&&!legacy_calls);
    out=(creature *)1;assert(load_ply("Hero",&out)==PLAYER_STORE_IO_ERROR&&!out&&!legacy_calls);
    for(mode=-1;mode<=2;mode++) if(mode!=0&&mode!=1) {
        int before=db_calls;selection=mode;
        assert(save_ply("Hero",&copy)==PLAYER_STORE_IO_ERROR&&db_calls==before&&!legacy_calls);
    }
    selection=1;assert(save_ply("Other",&copy)==PLAYER_STORE_IO_ERROR);
    selection=0;assert(!save_ply("Hero",&copy)&&legacy_calls==1);
    store.select=NULL;assert(save_ply("Hero",&copy)==PLAYER_STORE_IO_ERROR&&legacy_calls==1);
    assert(player_store_unbind(&binding)==PLAYER_STORE_UNBIND_RESTORED);return 0;
}
