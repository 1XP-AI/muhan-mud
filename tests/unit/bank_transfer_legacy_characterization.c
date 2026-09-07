/* Characterizes existing non-atomic behavior; NOT a cutover acceptance test. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "mtype.h"
#include "mstruct.h"

extern int deposit(creature *,cmd *);
extern int withdraw(creature *,cmd *);
static long bank_balance, saved_gold;
static int fail_save, bank_calls, player_calls;
static char order[4];
int bank_store_load(char *name, object **out)
{
    (void)name;
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
int main(void)
{
    if(scenario(0,0)||scenario(1,0)||scenario(0,1)||scenario(1,1)) return 1;
    puts("legacy bank characterization: success conserves value; failed bank saves still persist player (known non-atomic baseline)");
    return 0;
}
