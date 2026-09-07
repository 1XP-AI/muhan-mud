/* Characterizes existing non-atomic behavior; NOT a cutover acceptance test. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "mtype.h"
#include "mstruct.h"
#ifdef MUHAN_BANK_MONEY_ROUTING
#include "bank_money_route.h"
#include <assert.h>
#endif

extern int deposit(creature *,cmd *);
extern int withdraw(creature *,cmd *);
static long bank_balance, saved_gold;
static int fail_save, bank_calls, player_calls, load_calls;
static char order[4];
int bank_store_load(char *name, object **out)
{
    (void)name;
    load_calls++;
    *out=calloc(1,sizeof(**out));
    if(!*out) return -1;
    (*out)->value=bank_balance;
    return 0;
}
int bank_store_save(char *name, object *bank)
{
    (void)name; order[bank_calls+player_calls]='B'; bank_calls++;
    if(fail_save) return -1;
    bank_balance=bank->value; return 0;
}
int savegame_nomsg(creature *player)
{
    order[bank_calls+player_calls]='P'; player_calls++;
    saved_gold=player->gold; return 0;
}
void free_obj(object *bank) { free(bank); }
void zero(void *p,int n) { memset(p,0,(size_t)n); }
int print(int fd,char *format,...) { (void)fd; (void)format; return 0; }
int utf8_ends_with(unsigned char *s,unsigned char *suffix)
{
    size_t n=strlen((char *)s),m=strlen((char *)suffix);
    return n>=m&&!memcmp(s+n-m,suffix,m);
}
static int scenario(int taking,int failing)
{
    creature player; room bank_room; cmd command;
    memset(&player,0,sizeof(player)); memset(&bank_room,0,sizeof(bank_room));
    memset(&command,0,sizeof(command)); memset(order,0,sizeof(order));
    F_SET(&bank_room,RBANK); player.parent_rom=&bank_room;
    strcpy(player.name,"Bankhero"); player.gold=100; bank_balance=50;
    command.num=2; strcpy(command.str[1],"모두");
    fail_save=failing; bank_calls=player_calls=0; saved_gold=-1;
    if(taking) withdraw(&player,&command); else deposit(&player,&command);
    if(strcmp(order,"BP") || bank_calls!=1 || player_calls!=1) return 1;
    if(saved_gold!=(taking?150:0)) return 1;
    if(bank_balance!=(failing?50:(taking?0:150))) return 1;
    /* Failed deposit loses 100; failed withdrawal duplicates 50 durably.
     * Preserve this explicit baseline until the command transaction replaces it. */
    if(!failing && saved_gold+bank_balance!=150) return 1;
    if(failing && saved_gold+bank_balance==(long)150) return 1;
    return 0;
}
static int differential(int argc,char **argv)
{
    creature player; room bank_room; cmd command;
    long values[3]; char *end; int i;
    if(argc!=5 || (strcmp(argv[1],"deposit") && strcmp(argv[1],"withdraw"))) return 2;
    for(i=0;i<3;i++) {
        if(strlen(argv[i+2])>9 || !argv[i+2][0]) return 2;
        values[i]=strtol(argv[i+2],&end,10);
        if(*end || values[i]<0 || values[i]>300000001) return 2;
    }
    memset(&player,0,sizeof(player)); memset(&bank_room,0,sizeof(bank_room));
    memset(&command,0,sizeof(command));
    F_SET(&bank_room,RBANK); player.parent_rom=&bank_room;
    strcpy(player.name,"Bankhero"); player.gold=values[0]; bank_balance=values[1];
    saved_gold=values[0]; command.num=2;
    snprintf(command.str[1],sizeof(command.str[1]),"%ld냥",values[2]);
    if(!strcmp(argv[1],"deposit")) deposit(&player,&command); else withdraw(&player,&command);
    printf("%s %ld %ld\n",bank_calls==1 && player_calls==1?"OK":"REJECT",saved_gold,bank_balance);
    return 0;
}
#ifdef MUHAN_BANK_MONEY_ROUTING
static int selection, transfer_status, transfer_calls;
static int choose(void *ctx,const creature *player)
{ (void)ctx; (void)player; return selection; }
static int transfer(void *ctx,const creature *player,const cmd *command,int taking,bank_money_ack *ack)
{
    (void)ctx; (void)player; (void)command;
    transfer_calls++;
    ack->amount=25; ack->player_gold=taking?125:75; ack->bank_gold=taking?25:75;
    return transfer_status;
}
static void route_scenarios(void)
{
    creature player; room bank_room; cmd command;
    bank_money_route_ops ops;
    int taking,status;
    memset(&ops,0,sizeof(ops)); ops.select=choose; ops.transfer=transfer;
    assert(bank_money_route_set(&ops)==0);
    for(taking=0;taking<2;taking++) for(status=-1;status<=2;status++) {
        memset(&player,0,sizeof(player)); memset(&bank_room,0,sizeof(bank_room));
        memset(&command,0,sizeof(command)); F_SET(&bank_room,RBANK);
        player.parent_rom=&bank_room; player.gold=100; command.num=2;
        strcpy(command.str[1],"25냥"); selection=1; transfer_status=status;
        transfer_calls=load_calls=bank_calls=player_calls=0;
        if(taking) withdraw(&player,&command); else deposit(&player,&command);
        assert(transfer_calls==1 && load_calls==0 && bank_calls==0 && player_calls==0);
        assert(player.gold==(status==1?(taking?125:75):100));
    }
    ops.transfer=NULL; assert(bank_money_route_set(&ops)==0);
    transfer_calls=load_calls=bank_calls=player_calls=0; player.gold=100;
    deposit(&player,&command);
    assert(transfer_calls==0 && load_calls==0 && bank_calls==0 && player_calls==0 && player.gold==100);
    selection=-1; withdraw(&player,&command);
    assert(load_calls==0 && bank_calls==0 && player_calls==0 && player.gold==100);
    bank_money_route_reset();
    puts("GREEN actual bank commands: selected route never falls back, only confirmed commit changes wallet");
}
#endif
int main(int argc,char **argv)
{
    if(argc>1) return differential(argc,argv);
    if(scenario(0,0)||scenario(1,0)||scenario(0,1)||scenario(1,1)) return 1;
#ifdef MUHAN_BANK_MONEY_ROUTING
    route_scenarios();
#endif
    puts("legacy bank characterization: success conserves value; failed bank saves still persist player (known non-atomic baseline)");
    return 0;
}
