#include "bank_money_coordinate_native.h"
#include "bank_money_read_native.h"
#include "bank_money_plan_native.h"
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
static int coordinate(void *connection,const char *planner,const char *node,const char *script,const char *root,
    const char *const args[11],int timeout_ms,const unsigned char *expected_player,size_t expected_length,bank_money_coordinate_result *out)
{
    bank_money_read_result input;
    unsigned char *planned=NULL; size_t length=0; uint64_t revision=0;
    const char *plan_args[4],*commit_args[11]; char expected[32],amount[32]; uint64_t resolved=0;
    int i,status=BANK_MONEY_COMMIT_NOT_SENT;
    if(!out) return BANK_MONEY_COMMIT_INVALID;
    memset(out,0,sizeof(*out)); memset(&input,0,sizeof(input));
    if(!connection||!args||!planner||planner[0]!='/'||!node||node[0]!='/'||!script||script[0]!='/'
       ||!root||root[0]!='/'||timeout_ms<1||timeout_ms>10000) return BANK_MONEY_COMMIT_INVALID;
    for(i=0;i<11;i++) if(!args[i]||!args[i][0]) return BANK_MONEY_COMMIT_INVALID;
    if(bank_money_read_native(connection,args,timeout_ms,&input)) goto done;
    if(expected_player) {
        size_t pl;
        if(input.frame_length<8) goto done;
        pl=(size_t)input.frame[0]*16777216U+(size_t)input.frame[1]*65536U+(size_t)input.frame[2]*256U+input.frame[3];
        if(pl!=expected_length||pl>input.frame_length-8||memcmp(input.frame+8,expected_player,pl)) goto done;
    }
    snprintf(expected,sizeof(expected),"%llu",(unsigned long long)input.revision);
    if(strcmp(expected,args[8])) goto done;
    plan_args[0]=args[9]; plan_args[1]=args[10]; plan_args[2]=input.player_hash; plan_args[3]=input.bank_hash;
    if(bank_money_plan_resolved_native(planner,plan_args,input.frame,input.frame_length,timeout_ms,&planned,&length,&resolved)) goto done;
    for(i=0;i<11;i++) commit_args[i]=args[i];
    snprintf(amount,sizeof(amount),"%llu",(unsigned long long)resolved); commit_args[10]=amount;
    status=bank_money_commit_prepared_native(connection,node,script,root,commit_args,planned,length,timeout_ms,&revision);
    if(status==BANK_MONEY_COMMIT_CONFIRMED) {
        out->frame=planned; planned=NULL; out->frame_length=length; out->revision=revision;
        out->amount=resolved;
    }
done:
    free(input.frame); free(planned); return status;
}
int bank_money_coordinate_native(void *c,const char *planner,const char *node,const char *script,const char *root,const char *const args[11],int ms,bank_money_coordinate_result *out)
{return coordinate(c,planner,node,script,root,args,ms,NULL,0,out);}
int bank_money_coordinate_checked_native(void *c,const char *planner,const char *node,const char *script,const char *root,const char *const args[11],int ms,const unsigned char *expected,size_t length,bank_money_coordinate_result *out)
{
    if(!expected||!length||length>4194304) {if(out) memset(out,0,sizeof(*out)); return BANK_MONEY_COMMIT_INVALID;}
    return coordinate(c,planner,node,script,root,args,ms,expected,length,out);
}
