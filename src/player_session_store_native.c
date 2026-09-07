#include "player_session_store_native.h"
#include "player_paired_route_native.h"
#include "player_paired_load_native.h"
#include "bank_money_live_snapshot.h"
#include "bank_money_plan_native.h"
#include "player_snapshot_v1.h"
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
int player_session_store_init(player_session_store *ctx,void *connection,const char *world,const char *name,
    const char *writer,const char *epoch,const char *command,const char *node,const char *script,const char *root,int timeout_ms)
{
    const char *values[5];int indexes[5]={0,1,2,3,5},i;size_t n;
    if(!ctx||ctx->configured||!connection||!node||node[0]!='/'||!script||script[0]!='/'||!root||root[0]!='/'||timeout_ms<1||timeout_ms>10000) return -1;
    values[0]=world;values[1]=name;values[2]=writer;values[3]=epoch;values[4]=command;
    for(i=0;i<5;i++) {
        if(!values[i]) return -1;
        for(n=0;n<=128&&values[i][n];n++) {}
        if(!n||n>128||(i==1&&n>14)) return -1;
    }
    memset(ctx,0,sizeof(*ctx));
    for(i=0;i<5;i++) strcpy(ctx->fields[indexes[i]],values[i]);
    ctx->connection=connection;ctx->node=node;ctx->script=script;ctx->root=root;
    ctx->timeout_ms=timeout_ms;ctx->configured=1;return 0;
}
static int matches(player_session_store *ctx,const char *name)
{
    size_t n;
    if(!ctx||!ctx->configured||ctx->busy||!name) return 0;
    for(n=0;n<=14&&name[n];n++) {}
    return n>0&&n<=14&&!strcmp(name,ctx->fields[1]);
}
static int load(void *opaque,char *name,creature **out)
{
    player_session_store *ctx=opaque;player_paired_route_context route;int status=PLAYER_STORE_IO_ERROR;
    if(!out) return status;
    *out=NULL;
    if(!matches(ctx,name)||ctx->loaded) return status;
    ctx->busy=1;memset(&route,0,sizeof(route));route.connection=ctx->connection;
    route.world=ctx->fields[0];route.writer=ctx->fields[2];route.epoch=ctx->fields[3];route.timeout_ms=ctx->timeout_ms;
    if(player_paired_route_select(&route,name)!=1) goto done;
    status=player_paired_load_native(&route,name,out);
    if(status!=PLAYER_STORE_OK) goto done;
    strcpy(ctx->fields[4],route.last.character_id);strcpy(ctx->fields[7],route.last.player_hash);
    strcpy(ctx->owner,route.last.owner_user_id);
    snprintf(ctx->fields[6],sizeof(ctx->fields[6]),"%llu",(unsigned long long)route.last.revision);
    ctx->loaded=1;
done:
    ctx->busy=0;return status;
}
static int save(void *opaque,char *name,creature *player)
{
    player_session_store *ctx=opaque;unsigned char *payload=NULL;size_t length=0;
    const char *args[8];int i,result=PLAYER_STORE_IO_ERROR;
    if(!matches(ctx,name)||!ctx->loaded||!player||!memchr(player->name,0,sizeof(player->name))||strcmp(player->name,name)) return result;
    ctx->busy=1;ctx->status=PLAYER_SNAPSHOT_SAVE_INVALID;ctx->committed_revision=0;
    if(bank_money_live_snapshot(player,&payload,&length)) goto done;
    if(ctx->pending) {
        if(ctx->pending_length!=length||memcmp(ctx->pending,payload,length)) goto done;
    } else {ctx->pending=payload;ctx->pending_length=length;payload=NULL;}
    for(i=0;i<8;i++) args[i]=ctx->fields[i];
    ctx->status=player_snapshot_save_prepared_native(ctx->connection,ctx->node,ctx->script,ctx->root,
        args,ctx->pending,ctx->pending_length,ctx->timeout_ms,&ctx->committed_revision);
    if(ctx->status==PLAYER_SNAPSHOT_SAVE_COMMITTED||ctx->status==PLAYER_SNAPSHOT_SAVE_RETRY) result=PLAYER_STORE_OK;
done:
    free(payload);ctx->busy=0;return result;
}
player_store_ops player_session_store_build(player_session_store *ctx)
{player_store_ops ops;ops.save=save;ops.load=load;ops.opaque=ctx;return ops;}
int player_session_store_adopt(player_session_store *ctx,const creature *live,const char *command)
{
    player_paired_route_context route;creature *current=NULL;
    unsigned char *wire=NULL,*echo=NULL;size_t length=0,echoed=0;const char *args[11];int i,result=-1;
    if(!ctx||!ctx->configured||!ctx->loaded||ctx->busy||!ctx->pending||!live||!command
       ||(ctx->status!=PLAYER_SNAPSHOT_SAVE_COMMITTED&&ctx->status!=PLAYER_SNAPSHOT_SAVE_RETRY)) return -1;
    for(i=0;i<36;i++) {
        char c=command[i];
        if(i==8||i==13||i==18||i==23) {if(c!='-') return -1;}
        else if(!((c>='0'&&c<='9')||(c>='a'&&c<='f'))) return -1;
    }
    if(command[36]||!strcmp(command,ctx->fields[5])) return -1;
    ctx->busy=1;
    if(bank_money_live_snapshot(live,&wire,&length)||length!=ctx->pending_length||memcmp(wire,ctx->pending,length)) goto done;
    free(wire);wire=NULL;
    memset(&route,0,sizeof(route));route.connection=ctx->connection;route.world=ctx->fields[0];
    route.writer=ctx->fields[2];route.epoch=ctx->fields[3];route.timeout_ms=ctx->timeout_ms;
    if(player_paired_route_select(&route,ctx->fields[1])!=1||route.last.revision!=ctx->committed_revision
       ||strcmp(route.last.character_id,ctx->fields[4])||strcmp(route.last.owner_user_id,ctx->owner)) goto done;
    if(player_paired_load_native(&route,ctx->fields[1],&current)!=PLAYER_STORE_OK
       ||player_snapshot_v1_encode_loaded(current,&wire,&length)||length!=ctx->pending_length||memcmp(wire,ctx->pending,length)) goto done;
    args[0]=ctx->script;args[1]="--verify-resolved";args[2]=ctx->root;
    for(i=0;i<8;i++) args[i+3]=ctx->fields[i];
    if(player_snapshot_process_native(ctx->node,args,11,ctx->pending,ctx->pending_length,ctx->timeout_ms,&echo,&echoed)
       ||echoed!=ctx->pending_length||!echo||memcmp(echo,ctx->pending,echoed)) goto done;
    strcpy(ctx->fields[5],command);strcpy(ctx->fields[7],route.last.player_hash);
    snprintf(ctx->fields[6],sizeof(ctx->fields[6]),"%llu",(unsigned long long)route.last.revision);
    free(ctx->pending);ctx->pending=NULL;ctx->pending_length=0;ctx->status=0;ctx->committed_revision=0;result=0;
done:
    free(wire);free(echo);player_snapshot_v1_free_clone(current);ctx->busy=0;return result;
}
void player_session_store_dispose(player_session_store *ctx)
{if(ctx&&!ctx->busy) {free(ctx->pending);memset(ctx,0,sizeof(*ctx));}}
