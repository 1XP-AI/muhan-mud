#include "player_snapshot_v1.h"
#include "bank_money_live_snapshot.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
void merror(char *message,char kind) { (void)message; (void)kind; abort(); }
void del_active(creature *player) { (void)player; abort(); }
int main(int argc,char **argv)
{
    FILE *file; unsigned int byte; unsigned char wire[16384],*actual=NULL; size_t size=0,length=0;
    creature *player=NULL,before; unsigned char *changed=NULL; size_t changed_length=0;
    assert(argc==2); file=fopen(argv[1],"r"); assert(file);
    while(fscanf(file,"%2x",&byte)==1) {assert(length<sizeof(wire)); wire[length++]=(unsigned char)byte;}
    fclose(file);
    assert(player_snapshot_v1_decode_clone(wire,length,&player)==0);
    assert(sizeof("test-password")<=sizeof(player->password));
    player->fd=7; strcpy(player->password,"test-password"); before=*player;
    assert(bank_money_live_snapshot(player,&actual,&size)==0);
    assert(size==length && !memcmp(actual,wire,size)); assert(!memcmp(player,&before,sizeof(before)));
    player->gold=player->gold==100?101:100;
    assert(bank_money_live_snapshot(player,&changed,&changed_length)==0);
    assert(changed_length==size && memcmp(changed,actual,size));
    free(changed); free(actual);
    player->type=MONSTER; actual=(unsigned char *)1; size=1;
    assert(bank_money_live_snapshot(player,&actual,&size)!=0 && !actual && !size);
    player_snapshot_v1_free_clone(player); return 0;
}
