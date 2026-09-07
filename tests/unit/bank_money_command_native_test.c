#include "mstruct.h"
#include "bank_money_command_native.h"
#include "bank_money_result_native.h"
#include <assert.h>
#include <string.h>
static int calls;
int bank_money_live_native(void *c,const creature *p,const bank_money_live_request *r,
 const char *a,const char *b,const char *d,const char *e,int ms,bank_money_coordinate_result *out)
{
 (void)c;(void)p;(void)a;(void)b;(void)d;(void)e;(void)ms;
 assert(!strcmp(r->amount,"모두")&&!strcmp(r->direction,"withdraw")); calls++;
 memset(out,0,sizeof(*out)); out->revision=1; return BANK_MONEY_COMMIT_CONFIRMED;
}
int bank_money_result_native(const creature *p,int s,uint64_t rev,int w,const bank_money_coordinate_result *r,bank_money_ack *a)
{(void)p;(void)s;(void)r;assert(!rev&&w);a->amount=25;a->player_gold=125;a->bank_gold=25;return 1;}
int main(void)
{
 bank_money_command_context context; creature p; cmd command; bank_money_ack ack;
 memset(&context,0,sizeof(context)); memset(&p,0,sizeof(p)); memset(&command,0,sizeof(command));
 context.request.expected_revision="0"; command.num=2; strcpy(command.str[1],"모두");
 assert(bank_money_command_native(&context,&p,&command,1,&ack)==1);
 assert(calls==1&&context.used&&context.revision==1&&ack.amount==25);
 assert(!bank_money_command_native(&context,&p,&command,1,&ack)&&calls==1&&!ack.amount);
 context.used=0; context.request.expected_revision="-1";
 assert(!bank_money_command_native(&context,&p,&command,1,&ack)&&calls==1);
 context.used=0; context.request.expected_revision="0"; memset(command.str[1],'a',25);
 assert(!bank_money_command_native(&context,&p,&command,1,&ack)&&calls==1);
 return 0;
}
