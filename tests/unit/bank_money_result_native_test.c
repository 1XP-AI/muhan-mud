#include "bank_transfer_snapshot_v1.h"
#include "bank_money_result_native.h"
#include <assert.h>
#include <stdlib.h>
#include <string.h>
void merror(char *s,char c) {(void)s;(void)c;abort();}
void del_active(creature *p) {(void)p;abort();}
int main(void)
{
    creature player,planned,before; object bank; otag root;
    bank_money_coordinate_result result; bank_money_ack ack; int status;
    memset(&player,0,sizeof(player)); player.type=PLAYER; player.gold=100;
    strcpy(player.name,"Pairhero"); planned=player; planned.gold=75; before=player;
    memset(&bank,0,sizeof(bank)); strcpy(bank.name,"bank"); bank.value=75;
    memset(&root,0,sizeof(root)); root.obj=&bank;
    memset(&result,0,sizeof(result)); result.revision=1; result.amount=25;
    assert(!bank_transfer_snapshot_v1_encode(&planned,&root,&result.frame,&result.frame_length));
    assert(bank_money_result_native(&player,1,0,0,&result,&ack)==1);
    assert(ack.amount==25 && ack.player_gold==75 && ack.bank_gold==75);
    assert(!memcmp(&player,&before,sizeof(player)));
    for(status=-3;status<=2;status++) if(status!=1) {
        assert(!bank_money_result_native(&player,status,0,0,&result,&ack));
        assert(!ack.amount && !ack.player_gold && !ack.bank_gold);
    }
    assert(!bank_money_result_native(&player,1,1,0,&result,&ack));
    assert(!bank_money_result_native(&player,1,UINT64_MAX,0,&result,&ack));
    assert(!bank_money_result_native(&player,1,0,1,&result,&ack));
    player.level=1;
    assert(!bank_money_result_native(&player,1,0,0,&result,&ack)); player=before;
    result.frame[result.frame_length-1]^=1;
    assert(!bank_money_result_native(&player,1,0,0,&result,&ack));
    result.frame[result.frame_length-1]^=1; result.frame_length--;
    assert(!bank_money_result_native(&player,1,0,0,&result,&ack));
    free(result.frame); return 0;
}
