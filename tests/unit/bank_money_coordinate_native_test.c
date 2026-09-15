#include "bank_money_coordinate_native.h"
#include "bank_money_read_native.h"
#include "bank_money_plan_native.h"
#include <assert.h>
#include <stdlib.h>
#include <string.h>
static int phase,fail_read,fail_plan,commit_status;
static const char *request[11]={"character","world","actor","session","gateway","writer","1","command","7","deposit","25"};
int bank_money_read_native(void *c,const char *const a[7],int ms,bank_money_read_result *r)
{
    int i; assert(c==(void *)1 && ms==2000 && phase++==0);
    for(i=0;i<7;i++) assert(!strcmp(a[i],request[i]));
    if(fail_read) return -1;
    memset(r,0,sizeof(*r)); r->revision=7; r->frame=malloc(4); r->frame_length=4;
    memcpy(r->frame,"read",4); strcpy(r->player_hash,"playerhash"); strcpy(r->bank_hash,"bankhash"); return 0;
}
int bank_money_plan_resolved_native(const char *path,const char *const args[4],const unsigned char *in,size_t len,int ms,unsigned char **out,size_t *size,uint64_t *amount)
{
    assert(phase++==1 && ms==2000 && !strcmp(path,"/planner"));
    assert(!strcmp(args[0],"deposit") && !strcmp(args[1],"25"));
    assert(!strcmp(args[2],"playerhash") && !strcmp(args[3],"bankhash"));
    assert(len==4 && !memcmp(in,"read",4));
    if(fail_plan) return -1;
    *out=malloc(4); memcpy(*out,"plan",4); *size=4; *amount=25; return 0;
}
int bank_money_commit_prepared_native(void *c,const char *node,const char *script,const char *root,const char *const a[11],const unsigned char *frame,size_t len,int ms,uint64_t *rev)
{
    int i; assert(phase++==2 && c==(void *)1 && ms==2000);
    assert(!strcmp(node,"/node") && !strcmp(script,"/prepare") && !strcmp(root,"/pending"));
    for(i=0;i<11;i++) assert(!strcmp(a[i],request[i]));
    assert(len==4 && !memcmp(frame,"plan",4)); *rev=8; return commit_status;
}
static int run(bank_money_coordinate_result *out)
{ return bank_money_coordinate_native((void *)1,"/planner","/node","/prepare","/pending",request,2000,out); }
int main(void)
{
    bank_money_coordinate_result out; int status;
    for(status=-3;status<=2;status++) {
        phase=0; commit_status=status; memset(&out,0xa5,sizeof(out));
        assert(run(&out)==status && phase==3);
        if(status==BANK_MONEY_COMMIT_CONFIRMED) {
            assert(out.revision==8 && out.frame_length==4 && !memcmp(out.frame,"plan",4)); free(out.frame);
        } else assert(out.frame==NULL && out.frame_length==0 && out.revision==0);
    }
    phase=0; fail_read=1; assert(run(&out)==BANK_MONEY_COMMIT_NOT_SENT && phase==1 && !out.frame);
    phase=0; fail_read=0; fail_plan=1; assert(run(&out)==BANK_MONEY_COMMIT_NOT_SENT && phase==2 && !out.frame);
    phase=0; fail_plan=0; request[8]="6"; assert(run(&out)==BANK_MONEY_COMMIT_NOT_SENT && phase==1 && !out.frame);
    phase=0; request[8]="7";
    assert(bank_money_coordinate_checked_native((void *)1,"/planner","/node","/prepare","/pending",request,2000,(const unsigned char *)"different",9,&out)==BANK_MONEY_COMMIT_NOT_SENT && phase==1 && !out.frame);
    phase=0; request[8]="7"; request[10]=NULL; assert(run(&out)==BANK_MONEY_COMMIT_INVALID && phase==0 && !out.frame);
    return 0;
}
