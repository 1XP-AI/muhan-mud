#include "mstruct.h"
#include "mextern.h"
#include "bank_money_live_native.h"
#include "bank_money_live_snapshot.h"
#include <stdlib.h>
#include <string.h>
typedef struct live_binding {
    char character[37],actor[37],session[37],gateway[129];
    const extra *ext;
    const iobuf *io;
    long gold;
} live_binding;
static int uuid(const char value[37])
{
    int i;
    if(value[36]) return 0;
    for(i=0;i<36;i++) {
        char c=value[i];
        if(i==8||i==13||i==18||i==23) { if(c!='-') return 0; }
        else if(!((c>='0'&&c<='9')||(c>='a'&&c<='f'))) return 0;
    }
    return 1;
}
static int capture(int fd,const creature *player,live_binding *out)
{
    const extra *ext; size_t n; int i;
    memset(out,0,sizeof(*out));
    if(fd<0||fd>=PMAX||fd>=Tablesize||Ply[fd].ply!=player||!Ply[fd].io||!Ply[fd].extr) return -1;
    if(player->fd!=fd) return -1;
    ext=Ply[fd].extr;
    if(ext->onboarding_mode||!uuid(ext->character_id)||!uuid(ext->auth_user_id)||!uuid(ext->db_session_id)) return -1;
    n=0; while(n<sizeof(ext->db_gateway_instance_id)&&ext->db_gateway_instance_id[n]) n++;
    if(n<1||n>128) return -1;
    for(i=0;i<(int)n;i++) {
        char c=ext->db_gateway_instance_id[i];
        if(!((c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='-'||c=='_'||c=='.')) return -1;
    }
    memcpy(out->character,ext->character_id,37); memcpy(out->actor,ext->auth_user_id,37);
    memcpy(out->session,ext->db_session_id,37); memcpy(out->gateway,ext->db_gateway_instance_id,n+1);
    out->ext=ext; out->io=Ply[fd].io; out->gold=player->gold;
    return 0;
}
int bank_money_live_native(void *connection,const creature *player,const bank_money_live_request *request,
    const char *planner,const char *node,const char *script,const char *root,int timeout_ms,bank_money_coordinate_result *out)
{
    live_binding before,after; const char *args[11]; int fd,status;
    unsigned char *expected=NULL,*current=NULL; size_t expected_length=0,current_length=0;
    if(!out) return BANK_MONEY_COMMIT_INVALID;
    memset(out,0,sizeof(*out));
    if(!player||!request) return BANK_MONEY_COMMIT_NOT_SENT;
    fd=player->fd;
    if(capture(fd,player,&before)) return BANK_MONEY_COMMIT_NOT_SENT;
    if(bank_money_live_snapshot(player,&expected,&expected_length)) return BANK_MONEY_COMMIT_NOT_SENT;
    args[0]=before.character; args[1]=request->world_id; args[2]=before.actor;
    args[3]=before.session; args[4]=before.gateway; args[5]=request->writer_id;
    args[6]=request->writer_epoch; args[7]=request->command_id; args[8]=request->expected_revision;
    args[9]=request->direction; args[10]=request->amount;
    status=bank_money_coordinate_checked_native(connection,planner,node,script,root,args,timeout_ms,expected,expected_length,out);
    if(status>0 && (capture(fd,player,&after)||before.ext!=after.ext||before.io!=after.io||before.gold!=after.gold
       ||strcmp(before.character,after.character)||strcmp(before.actor,after.actor)
       ||strcmp(before.session,after.session)||strcmp(before.gateway,after.gateway)
       ||bank_money_live_snapshot(player,&current,&current_length)
       ||current_length!=expected_length||memcmp(current,expected,expected_length))) {
        free(out->frame); memset(out,0,sizeof(*out));
        status=BANK_MONEY_COMMIT_UNKNOWN;
    }
    free(expected); free(current);
    return status;
}
