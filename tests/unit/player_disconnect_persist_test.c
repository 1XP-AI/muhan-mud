#include "mstruct.h"
#include "player_store.h"
#include "player_disconnect_persist.h"
#include <assert.h>
#include <string.h>
#include <stdio.h>

static int stage, save_result, queue_result, freed, queued;
static creature player;
void uninit_ply(creature *p)
{ assert(p==&player && stage==0); stage=1; p->gold++; }
int save_ply(char *name,creature *p)
{ assert(p==&player && !strcmp(name,"Exitplayer") && stage==1 && p->gold==11); stage=2; return save_result; }
int player_recovery_enqueue(creature *p)
{ assert(p==&player && stage==2); stage=3; queued++; if(!queue_result) p->fd=-1; return queue_result; }
void free_crt(creature *p)
{ assert(p==&player); assert(p->fd<0 || stage==2); freed++; }
void log_f(char *format,...) { (void)format; }
static creature *reset(int fd,int save,int queue)
{
    memset(&player,0,sizeof(player));strcpy(player.name,"Exitplayer");
    player.fd=fd;player.gold=10;stage=freed=queued=0;
    save_result=save;queue_result=queue;return &player;
}
int main(void)
{
    creature *owned=reset(9,PLAYER_STORE_OK,0);
    assert(!player_disconnect_persist(&owned)&&!owned&&freed==1&&!queued&&stage==2);
    owned=reset(9,PLAYER_STORE_IO_ERROR,0);
    assert(!player_disconnect_persist(&owned)&&!owned&&!freed&&queued==1&&stage==3);
    assert(player.fd==-1&&player.gold==11);
    owned=reset(9,PLAYER_STORE_IO_ERROR,-1);
    assert(player_disconnect_persist(&owned)!=0&&owned==&player&&!freed&&queued==1);
    owned=reset(-1,PLAYER_STORE_IO_ERROR,-1);
    assert(!player_disconnect_persist(&owned)&&!owned&&freed==1&&!stage&&!queued);
    assert(!player_disconnect_persist(&owned));
    assert(player_disconnect_persist(NULL)!=0);
    puts("player_disconnect_persist_test: ownership and uninit-before-save ordering passed");
    return 0;
}
