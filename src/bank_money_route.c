#include "bank_money_route.h"
#include "mstruct.h"
#include <string.h>
#include <limits.h>
static bank_money_route_ops route;
int bank_money_route_set(const bank_money_route_ops *ops)
{
    if(!ops || !ops->select) return -1;
    route=*ops; return 0;
}
void bank_money_route_reset(void) { memset(&route,0,sizeof(route)); }
int bank_money_route_allows_legacy(const creature *player)
{
    if(!player) return 0;
    return !route.select || route.select(route.context,player)==0;
}
int bank_money_route_dispatch(creature *player,const cmd *command,int withdraw,bank_money_ack *ack)
{
    bank_money_ack reply;
    int selected;
    long before,expected;
    if(!player || !command || !ack || (withdraw!=0 && withdraw!=1)) return BANK_MONEY_REJECTED;
    memset(ack,0,sizeof(*ack));
    before=player->gold;
    if(!route.select) return BANK_MONEY_LEGACY;
    selected=route.select(route.context,player);
    if(selected==0) return BANK_MONEY_LEGACY;
    if(selected!=1 || !route.transfer) return BANK_MONEY_REJECTED;
    memset(&reply,0,sizeof(reply));
    if(route.transfer(route.context,player,command,withdraw,&reply)!=1 || reply.amount<=0
       || reply.player_gold<0 || reply.bank_gold<0 || (!withdraw && reply.bank_gold>300000000L)) return BANK_MONEY_REJECTED;
    /* Do not overwrite intervening gameplay or accept a reply for another
     * wallet/amount. Check bounds before arithmetic, including on 32-bit long. */
    if(before<0 || player->gold!=before) return BANK_MONEY_REJECTED;
    if(withdraw) {
        if(reply.amount>LONG_MAX-before || reply.amount>LONG_MAX-reply.bank_gold) return BANK_MONEY_REJECTED;
        expected=before+reply.amount;
    } else {
        if(reply.amount>before || reply.bank_gold<reply.amount) return BANK_MONEY_REJECTED;
        expected=before-reply.amount;
    }
    if(reply.player_gold!=expected) return BANK_MONEY_REJECTED;
    *ack=reply;
    player->gold=reply.player_gold;
    return BANK_MONEY_COMMITTED;
}
