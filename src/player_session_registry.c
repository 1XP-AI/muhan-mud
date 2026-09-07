#include "player_session_registry.h"
#include <string.h>
static int bounded(const char *value,size_t max)
{size_t n;if(!value) return 0;for(n=0;n<=max&&value[n];n++) {}return n>0&&n<=max;}
int player_session_registry_add(player_session_registry *r,player_session_store *ctx)
{
    int i,empty=-1;
    if(!r||r->busy||!ctx||!ctx->configured||ctx->busy
       ||!bounded(ctx->fields[0],64)||!bounded(ctx->fields[1],14)) return -1;
    if(r->world[0]&&strcmp(r->world,ctx->fields[0])) return -1;
    for(i=0;i<PLAYER_SESSION_REGISTRY_CAPACITY;i++) {
        if(!r->slots[i].context) {if(empty<0) empty=i;continue;}
        if(r->slots[i].context==ctx||!strcmp(r->slots[i].name,ctx->fields[1])) return -1;
    }
    if(empty<0) return -1;
    strcpy(r->world,ctx->fields[0]);strcpy(r->slots[empty].name,ctx->fields[1]);r->slots[empty].context=ctx;return 0;
}
int player_session_registry_remove(player_session_registry *r,player_session_store *ctx)
{
    int i;
    if(!r||r->busy||!ctx||ctx->busy||ctx->pending) return -1;
    for(i=0;i<PLAYER_SESSION_REGISTRY_CAPACITY;i++) if(r->slots[i].context==ctx) {
        memset(&r->slots[i],0,sizeof(r->slots[i]));return 0;
    }
    return -1;
}
static int dispatch(player_session_registry *r,char *name,struct creature *player,struct creature **out)
{
    player_session_store *ctx=NULL;player_store_ops ops;int i,result=PLAYER_STORE_IO_ERROR;
    if(out) *out=NULL;
    if(!r||r->busy||!bounded(name,14)) return result;
    for(i=0;i<PLAYER_SESSION_REGISTRY_CAPACITY;i++) if(r->slots[i].context&&!strcmp(r->slots[i].name,name)) {ctx=r->slots[i].context;break;}
    if(!ctx||!ctx->configured||strcmp(ctx->fields[0],r->world)||strcmp(ctx->fields[1],name)) return result;
    r->busy=1;ops=player_session_store_build(ctx);
    if(out) result=ops.load(ops.opaque,name,out);else result=ops.save(ops.opaque,name,player);
    r->busy=0;return result;
}
static int load(void *r,char *name,struct creature **out)
{return out?dispatch(r,name,NULL,out):PLAYER_STORE_IO_ERROR;}
static int save(void *r,char *name,struct creature *player)
{return dispatch(r,name,player,NULL);}
player_store_ops player_session_registry_build(player_session_registry *r)
{player_store_ops ops;ops.load=load;ops.save=save;ops.opaque=r;return ops;}
