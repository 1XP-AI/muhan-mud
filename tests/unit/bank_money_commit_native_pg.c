#include "bank_money_commit_native.h"
#include "bank_money_coordinate_native.h"
#include "bank_money_live_native.h"
#include "bank_money_result_native.h"
#include "bank_money_command_native.h"
#include "player_snapshot_v1.h"
#include "bank_money_read_native.h"
#include <libpq-fe.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
/* Disposable descriptor fixture; production owns these globals in global.c. */
struct { creature *ply; iobuf *io; extra *extr; } Ply[PMAX];
int Tablesize=1;
static int selected(void *ctx,const creature *p) {(void)ctx;(void)p;return 1;}
int main(int argc,char **argv)
{
    unsigned char *frame;
    size_t length;
    PGconn *c; PGresult *role; uint64_t revision; int status;
    if(argc!=12) return 2;
    frame=(unsigned char *)malloc(8388617); if(!frame) return 2;
    length=fread(frame,1,8388617,stdin);
    if(ferror(stdin)||!feof(stdin)) { free(frame); return 2; }
    c=PQconnectdb("host=127.0.0.1 dbname=postgres user=mud_writer_login connect_timeout=3");
    if(PQstatus(c)!=CONNECTION_OK) { free(frame); PQfinish(c); return 2; }
    role=PQexec(c,"set role mud_writer");
    if(PQresultStatus(role)!=PGRES_COMMAND_OK) { free(frame); PQclear(role); PQfinish(c); return 2; }
    PQclear(role);
    if(getenv("BANK_TRANSFER_COORDINATE") && !strcmp(getenv("BANK_TRANSFER_COORDINATE"),"1")) {
      bank_money_command_context context; bank_money_route_ops ops; bank_money_ack ack; cmd command; long before;
      creature *player=NULL; extra ext; iobuf io;
      bank_money_read_result current; size_t pl;
      bank_money_live_request request;
      memset(&ext,0,sizeof(ext)); memset(&io,0,sizeof(io));
      if(strlen(argv[1])>36||strlen(argv[3])>36||strlen(argv[4])>36||strlen(argv[5])>128) { free(frame); PQfinish(c); return 2; }
      strcpy(ext.character_id,argv[1]); strcpy(ext.auth_user_id,argv[3]);
      strcpy(ext.db_session_id,argv[4]); strcpy(ext.db_gateway_instance_id,argv[5]);
      if(bank_money_read_native(c,(const char *const *)(argv+1),2000,&current)) {free(frame); PQfinish(c); return 2;}
      pl=(size_t)current.frame[0]*16777216U+(size_t)current.frame[1]*65536U+(size_t)current.frame[2]*256U+current.frame[3];
      status=player_snapshot_v1_decode_clone(current.frame+8,pl,&player); free(current.frame);
      if(status) {free(frame); PQfinish(c); return 2;}
      player->fd=0;
      if(getenv("BANK_TRANSFER_LIVE_DRIFT")&&!strcmp(getenv("BANK_TRANSFER_LIVE_DRIFT"),"gold")) player->gold=player->gold==100?101:100;
      if(getenv("BANK_TRANSFER_LIVE_DRIFT")&&!strcmp(getenv("BANK_TRANSFER_LIVE_DRIFT"),"level")) player->level=player->level==1?2:1;
      Ply[0].ply=player; Ply[0].io=&io; Ply[0].extr=&ext;
      request.world_id=argv[2]; request.writer_id=argv[6]; request.writer_epoch=argv[7];
      request.command_id=argv[8]; request.expected_revision=argv[9]; request.direction=argv[10]; request.amount=argv[11];
      memset(&context,0,sizeof(context)); memset(&command,0,sizeof(command));
      context.connection=c; context.request=request; context.timeout_ms=2000;
      context.planner=getenv("BANK_TRANSFER_PLANNER"); context.node=getenv("BANK_TRANSFER_PENDING_NODE");
      context.script=getenv("BANK_TRANSFER_PENDING_CLI"); context.root=getenv("BANK_TRANSFER_PENDING_ROOT");
      if(strlen(argv[11])>24) {free(frame); player_snapshot_v1_free_clone(player); PQfinish(c);return 2;}
      command.num=2; strcpy(command.str[1],argv[11]); before=player->gold;
      memset(&ops,0,sizeof(ops)); ops.select=selected; ops.transfer=bank_money_command_native; ops.context=&context;
      bank_money_route_set(&ops);
      status=bank_money_route_dispatch(player,&command,!strcmp(argv[10],"withdraw"),&ack);
      if((context.status==BANK_MONEY_COMMIT_CONFIRMED)!=(status==BANK_MONEY_COMMITTED)) abort();
      if(status==BANK_MONEY_COMMITTED) {
        if(player->gold!=ack.player_gold||player->gold==before) abort();
      } else if(player->gold!=before) abort();
      status=context.status; revision=context.revision; bank_money_route_reset();
      memset(Ply,0,sizeof(Ply));
      player_snapshot_v1_free_clone(player);
    } else if(getenv("BANK_TRANSFER_PENDING_ROOT") && getenv("BANK_TRANSFER_PENDING_ROOT")[0])
      status=bank_money_commit_prepared_native(c,getenv("BANK_TRANSFER_PENDING_NODE"),getenv("BANK_TRANSFER_PENDING_CLI"),getenv("BANK_TRANSFER_PENDING_ROOT"),
        (const char *const *)(argv+1),frame,length,2000,&revision);
    else status=bank_money_commit_native(c,(const char *const *)(argv+1),frame,length,2000,&revision);
    free(frame); PQfinish(c);
    if(status==BANK_MONEY_COMMIT_CONFIRMED||status==BANK_MONEY_COMMIT_RETRY) {
        printf("%s %llu\n",status==BANK_MONEY_COMMIT_CONFIRMED?"COMMITTED":"EXACT_RETRY",(unsigned long long)revision);
        return 0;
    }
    return status==BANK_MONEY_COMMIT_REJECTED?1:status==BANK_MONEY_COMMIT_UNKNOWN?3:status==BANK_MONEY_COMMIT_NOT_SENT?4:2;
}
