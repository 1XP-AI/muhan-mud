#include "mstruct.h"
#include "bank_money_command_native.h"
#include "bank_money_result_native.h"
#include <stdlib.h>
#include <string.h>
int bank_money_command_native(void *opaque,const creature *player,const cmd *command,int withdraw,bank_money_ack *ack)
{
    bank_money_command_context *ctx=(bank_money_command_context *)opaque;
    bank_money_live_request request; bank_money_coordinate_result result;
    uint64_t expected=0; const char *p; size_t n; int ok=0;
    if(!ack) return 0;
    memset(ack,0,sizeof(*ack));
    if(!ctx||ctx->used) return 0;
    ctx->used=1; ctx->status=BANK_MONEY_COMMIT_NOT_SENT; ctx->revision=0;
    if(!player||!command||command->num!=2||(withdraw!=0&&withdraw!=1)) return 0;
    n=0; while(n<sizeof(command->str[1])&&command->str[1][n]) n++;
    if(!n||n>24) return 0;
    p=ctx->request.expected_revision;
    if(!p||!*p||strlen(p)>20||(*p=='0'&&p[1])) return 0;
    for(;*p;p++) {
        if(*p<'0'||*p>'9'||expected>(UINT64_MAX-(unsigned)(*p-'0'))/10) return 0;
        expected=expected*10+(unsigned)(*p-'0');
    }
    if(expected==UINT64_MAX) return 0;
    request=ctx->request; request.direction=withdraw?"withdraw":"deposit"; request.amount=command->str[1];
    memset(&result,0,sizeof(result));
    ctx->status=bank_money_live_native(ctx->connection,player,&request,ctx->planner,ctx->node,ctx->script,ctx->root,ctx->timeout_ms,&result);
    if(ctx->status==BANK_MONEY_COMMIT_CONFIRMED) {
        ok=bank_money_result_native(player,ctx->status,expected,withdraw,&result,ack);
        if(ok) ctx->revision=result.revision;
        else ctx->status=BANK_MONEY_COMMIT_UNKNOWN;
    }
    free(result.frame); return ok;
}
