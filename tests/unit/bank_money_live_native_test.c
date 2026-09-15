#include "mstruct.h"
#include "bank_money_live_native.h"
#include <assert.h>
#include <stdlib.h>
#include <string.h>
struct { creature *ply; iobuf *io; extra *extr; } Ply[PMAX];
int Tablesize=2;
static creature player,other;
static extra ext;
static iobuf io;
static int calls,change;
static bank_money_live_request request={"muhan","33333333-3333-4333-8333-333333333333","1",
    "44444444-4444-4444-8444-444444444444","7","deposit","25"};
int bank_money_live_snapshot(const creature *p,unsigned char **out,size_t *length)
{
    *out=malloc(sizeof(p->gold)+sizeof(p->level)); assert(*out);
    *length=sizeof(p->gold)+sizeof(p->level);
    memcpy(*out,&p->gold,sizeof(p->gold)); memcpy(*out+sizeof(p->gold),&p->level,sizeof(p->level)); return 0;
}
int bank_money_coordinate_checked_native(void *c,const char *planner,const char *node,const char *script,const char *root,
    const char *const args[11],int ms,const unsigned char *expected,size_t expected_length,bank_money_coordinate_result *out)
{
    assert(c==(void *)1 && ms==2000); (void)planner;(void)node;(void)script;(void)root;
    calls++;
    assert(expected && expected_length==sizeof(player.gold)+sizeof(player.level));
    assert(!strcmp(args[0],ext.character_id) && !strcmp(args[2],ext.auth_user_id));
    assert(!strcmp(args[3],ext.db_session_id) && !strcmp(args[4],ext.db_gateway_instance_id));
    assert(!strcmp(args[1],request.world_id) && !strcmp(args[5],request.writer_id));
    assert(!strcmp(args[6],"1") && !strcmp(args[7],request.command_id));
    assert(!strcmp(args[8],"7") && !strcmp(args[9],"deposit") && !strcmp(args[10],"25"));
    if(change==1) Ply[1].ply=&other;
    if(change==2) ext.db_session_id[0]='a';
    if(change==3) player.gold++;
    if(change==4) Ply[1].io=NULL;
    if(change==5) player.level++;
    out->frame=malloc(1); out->frame_length=1; out->revision=8;
    return BANK_MONEY_COMMIT_CONFIRMED;
}
static void setup(void)
{
    memset(&player,0,sizeof(player)); memset(&ext,0,sizeof(ext)); memset(&io,0,sizeof(io));
    player.fd=1; player.gold=100; Ply[1].ply=&player; Ply[1].extr=&ext; Ply[1].io=&io;
    strcpy(ext.auth_user_id,"11111111-1111-4111-8111-111111111111");
    strcpy(ext.character_id,"22222222-2222-4222-8222-222222222222");
    strcpy(ext.db_session_id,"55555555-5555-4555-8555-555555555555");
    strcpy(ext.db_gateway_instance_id,"gateway-1"); calls=0; change=0;
}
static int run(bank_money_coordinate_result *out)
{ return bank_money_live_native((void *)1,&player,&request,"/planner","/node","/prepare","/pending",2000,out); }
int main(void)
{
    bank_money_coordinate_result out; int i;
    setup(); assert(run(&out)==BANK_MONEY_COMMIT_CONFIRMED && calls==1 && out.revision==8); free(out.frame);
    for(i=1;i<=5;i++) { setup(); change=i; assert(run(&out)==BANK_MONEY_COMMIT_UNKNOWN && calls==1 && !out.frame && !out.revision); }
    for(i=0;i<7;i++) {
        setup();
        if(i==0) ext.db_session_id[0]=0;
        if(i==1) memset(ext.character_id,'x',sizeof(ext.character_id));
        if(i==2) Ply[1].ply=&other;
        if(i==3) player.fd=-1;
        if(i==4) player.fd=PMAX;
        if(i==5) ext.onboarding_mode='P';
        if(i==6) strcpy(ext.db_gateway_instance_id,"invalid|gateway");
        assert(run(&out)==BANK_MONEY_COMMIT_NOT_SENT && calls==0 && !out.frame);
    }
    return 0;
}
