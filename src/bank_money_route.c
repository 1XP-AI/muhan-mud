#include "bank_money_route.h"
#include "mstruct.h"
#include <string.h>
static bank_money_route_ops route;
int bank_money_route_set(const bank_money_route_ops *ops)
{
    if(!ops || !ops->select) return -1;
    route=*ops; return 0;
}
void bank_money_route_reset(void) { memset(&route,0,sizeof(route)); }
int bank_money_route_dispatch(creature *player,const cmd *command,int withdraw,bank_money_ack *ack)
{
    bank_money_ack reply;
    int selected;
    if(!player || !command || !ack || (withdraw!=0 && withdraw!=1)) return BANK_MONEY_REJECTED;
    memset(ack,0,sizeof(*ack));
    if(!route.select) return BANK_MONEY_LEGACY;
    selected=route.select(route.context,player);
    if(selected==0) return BANK_MONEY_LEGACY;
    if(selected!=1 || !route.transfer) return BANK_MONEY_REJECTED;
    memset(&reply,0,sizeof(reply));
    if(route.transfer(route.context,player,command,withdraw,&reply)!=1 || reply.amount<=0
       || reply.player_gold<0 || reply.bank_gold<0 || (!withdraw && reply.bank_gold>300000000L)) return BANK_MONEY_REJECTED;
    *ack=reply;
    player->gold=reply.player_gold;
    return BANK_MONEY_COMMITTED;
}
