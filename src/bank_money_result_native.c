#include "bank_money_result_native.h"
#include "bank_money_live_snapshot.h"
#include "player_snapshot_v1.h"
#include "bank_snapshot_v1.h"
#include <limits.h>
#include <stdlib.h>
#include <string.h>
static size_t be32(const unsigned char *p)
{return (size_t)p[0]*16777216U+(size_t)p[1]*65536U+(size_t)p[2]*256U+p[3];}
int bank_money_result_native(const creature *live,int status,uint64_t expected_revision,int withdraw,
    const bank_money_coordinate_result *result,bank_money_ack *ack)
{
    creature *next=NULL,*current=NULL; otag *bank=NULL;
    unsigned char *wire=NULL; size_t length=0,pl,bl; long amount,gold; int ok=0;
    if(!ack) return 0;
    memset(ack,0,sizeof(*ack));
    if(!live||!result||status!=BANK_MONEY_COMMIT_CONFIRMED||expected_revision==UINT64_MAX
       ||result->revision!=expected_revision+1||!result->amount||result->amount>(uint64_t)LONG_MAX
       ||(withdraw!=0&&withdraw!=1)||!result->frame||result->frame_length<8) return 0;
    pl=be32(result->frame); bl=be32(result->frame+4);
    if(!pl||!bl||pl>4194304||bl>4194304||result->frame_length!=8+pl+bl) return 0;
    if(player_snapshot_v1_decode_clone(result->frame+8,pl,&next)
       ||bank_snapshot_v1_decode(result->frame+8+pl,bl,&bank)
       ||bank_money_live_snapshot(live,&wire,&length)
       ||player_snapshot_v1_decode_clone(wire,length,&current)) goto done;
    amount=(long)result->amount;
    if(current->gold<0||next->gold<0||bank->obj->value<0) goto done;
    if(withdraw) {
        if(amount>LONG_MAX-current->gold||amount>LONG_MAX-bank->obj->value) goto done;
        gold=current->gold+amount;
    } else {
        if(amount>current->gold||bank->obj->value<amount||bank->obj->value>300000000L) goto done;
        gold=current->gold-amount;
    }
    if(next->gold!=gold) goto done;
    /* Only gold may differ. Compare complete normalized inventory and stats. */
    next->gold=current->gold;
    if(!player_snapshot_v1_equal_persisted(next,current)) goto done;
    ack->amount=amount; ack->player_gold=gold; ack->bank_gold=bank->obj->value; ok=1;
done:
    free(wire); player_snapshot_v1_free_clone(next); player_snapshot_v1_free_clone(current);
    bank_snapshot_v1_free(bank); return ok;
}
