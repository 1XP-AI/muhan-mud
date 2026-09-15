#include "mstruct.h"
#include "player_authority_store.h"
#include "player_path.h"
#include <string.h>
static int valid_name(const char *name)
{
    size_t n=0;
    if(!name) return 0;
    while(n<=PLAYER_NAME_MAX_BYTES&&name[n]) n++;
    return n>0&&n<=PLAYER_NAME_MAX_BYTES&&player_name_is_valid((const unsigned char *)name,PLAYER_NAME_MIN_CODEPOINTS,PLAYER_NAME_MAX_CODEPOINTS);
}
static int dispatch(player_authority_store *store,char *name,creature *save,creature **load)
{
    const player_store_ops *provider;int selected,result=PLAYER_STORE_IO_ERROR;
    if(load) *load=NULL;
    if(!store||store->busy||!store->select||!valid_name(name)) return result;
    if(save&&(!memchr(save->name,0,sizeof(save->name))||strcmp(save->name,name))) return result;
    store->busy=1;
    selected=store->select(store->selection_context,name);
    if(selected==0) provider=&store->legacy;
    else if(selected==1) provider=&store->database;
    else goto done;
    if(save) {if(provider->save) result=provider->save(provider->opaque,name,save);}
    else if(load&&provider->load) result=provider->load(provider->opaque,name,load);
done:
    store->busy=0;
    if(load&&result!=PLAYER_STORE_OK) *load=NULL;
    return result;
}
static int save(void *context,char *name,creature *player)
{return player?dispatch(context,name,player,NULL):PLAYER_STORE_IO_ERROR;}
static int load(void *context,char *name,creature **player)
{return player?dispatch(context,name,NULL,player):PLAYER_STORE_IO_ERROR;}
player_store_ops player_authority_store_build(player_authority_store *store)
{player_store_ops ops;ops.save=save;ops.load=load;ops.opaque=store;return ops;}
