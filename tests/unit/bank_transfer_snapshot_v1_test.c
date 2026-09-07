#include "bank_transfer_snapshot_v1.h"
#include "cdto_v1.h"
#include <assert.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

static size_t length32(const unsigned char *p)
{
    return (size_t)p[0]*16777216U+(size_t)p[1]*65536U+(size_t)p[2]*256U+p[3];
}
int main(int argc,char **argv)
{
    creature player, original, *clone;
    object bank, bank_original;
    otag root, *decoded;
    unsigned char *wire, *again;
    size_t length, again_length, pl, bl;
    memset(&player,0,sizeof(player)); player.type=PLAYER;
    strcpy(player.name,"Pairhero"); player.gold=100;
    memset(&bank,0,sizeof(bank)); strcpy(bank.name,"bank"); bank.value=50;
    memset(&root,0,sizeof(root)); root.obj=&bank;
    original=player; bank_original=bank;
    assert(bank_transfer_snapshot_v1_encode(&player,&root,&wire,&length)==CDTO_V1_OK);
    if(argc==2 && strcmp(argv[1],"--frame")==0) {
        assert(fwrite(wire,1,length,stdout)==length);
        free(wire);
        return 0;
    }
    pl=length32(wire); bl=length32(wire+4);
    assert(length==8+pl+bl);
    assert(player_snapshot_v1_decode_clone(wire+8,pl,&clone)==CDTO_V1_OK);
    assert(bank_snapshot_v1_decode(wire+8+pl,bl,&decoded)==CDTO_V1_OK);
    assert(clone->gold==100 && decoded->obj->value==50);
    assert(player_snapshot_v1_equal_persisted(&player,clone));
    player_snapshot_v1_free_clone(clone); bank_snapshot_v1_free(decoded);
    assert(bank_transfer_snapshot_v1_encode(&player,&root,&again,&again_length)==CDTO_V1_OK);
    assert(length==again_length && memcmp(wire,again,length)==0);
    free(wire); free(again);
    /* Bank failure occurs after a valid player encoding; publish neither. */
    root.next_tag=&root;
    wire=(unsigned char *)1; length=99;
    assert(bank_transfer_snapshot_v1_encode(&player,&root,&wire,&length)!=CDTO_V1_OK);
    assert(wire==NULL && length==0); root.next_tag=NULL;
    bank_transfer_snapshot_v1_test_fail_allocation(1);
    assert(bank_transfer_snapshot_v1_encode(&player,&root,&wire,&length)!=CDTO_V1_OK);
    assert(wire==NULL && length==0);
    bank_transfer_snapshot_v1_test_fail_allocation(0);
    assert(bank_transfer_snapshot_v1_encode(NULL,&root,&wire,&length)!=CDTO_V1_OK);
    assert(wire==NULL && length==0);
    assert(memcmp(&player,&original,sizeof(player))==0);
    assert(memcmp(&bank,&bank_original,sizeof(bank))==0);
    puts("GREEN paired C snapshot: deterministic frame, unchanged source, no partial output on failure");
    return 0;
}
