#include "bank_money_read_native.h"
#include <libpq-fe.h>
#include <poll.h>
#include <time.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>

static int64_t now_ms(void)
{
    struct timespec ts;
    if(clock_gettime(CLOCK_MONOTONIC,&ts)!=0) return -1;
    return (int64_t)ts.tv_sec*1000+ts.tv_nsec/1000000;
}
static int wait_socket(PGconn *c,short events,int64_t deadline)
{
    struct pollfd fd;
    int64_t now;
    int result;
    fd.fd=PQsocket(c); fd.events=events; fd.revents=0;
    if(fd.fd<0) return -1;
    for(;;) {
        now=now_ms();
        if(now<0 || now>=deadline) return -1;
        result=poll(&fd,1,(int)(deadline-now));
        if(result<0 && errno==EINTR) continue;
        if(result<=0 || (fd.revents&(POLLERR|POLLHUP|POLLNVAL))) return -1;
        return 0;
    }
}
static void put32(unsigned char *p,size_t n)
{ p[0]=(unsigned char)(n>>24); p[1]=(unsigned char)(n>>16); p[2]=(unsigned char)(n>>8); p[3]=(unsigned char)n; }
static int valid_hash(const char *p)
{
    int i;
    for(i=0;i<64;i++) if(!((p[i]>='0'&&p[i]<='9')||(p[i]>='a'&&p[i]<='f'))) return 0;
    return 1;
}
int bank_money_read_native(void *connection,const char *const values[7],int timeout_ms,bank_money_read_result *out)
{
    PGconn *c=(PGconn *)connection;
    PGresult *result=NULL,*next;
    bank_money_read_result value;
    int original_mode,i,flush,ok=-1;
    int64_t start,deadline;
    size_t pl,bl;
    const unsigned char *revision;
    static const Oid types[5]={20,17,17,25,25};
    if(!out) return -1;
    memset(out,0,sizeof(*out)); memset(&value,0,sizeof(value));
    if(!c||!values||timeout_ms<1||timeout_ms>10000||PQstatus(c)!=CONNECTION_OK||PQtransactionStatus(c)!=PQTRANS_IDLE) return -1;
    for(i=0;i<7;i++) if(!values[i]) return -1;
    start=now_ms(); if(start<0) return -1; deadline=start+timeout_ms;
    original_mode=PQisnonblocking(c);
    if(PQsetnonblocking(c,1)!=0) return -1;
    if(!PQsendQueryParams(c,"select * from private.read_qualified_money_transfer_state($1,$2,$3,$4,$5,$6,$7)",7,NULL,values,NULL,NULL,1)) goto done;
    while((flush=PQflush(c))==1) if(wait_socket(c,POLLOUT,deadline)!=0) goto done;
    if(flush<0) goto done;
    for(;;) {
        if(now_ms()<0 || now_ms()>=deadline) goto done;
        if(!PQconsumeInput(c)) goto done;
        if(PQisBusy(c)) { if(wait_socket(c,POLLIN,deadline)!=0) goto done; continue; }
        next=PQgetResult(c);
        if(!next) break;
        if(result) { PQclear(next); goto done; }
        result=next;
    }
    if(!result||PQresultStatus(result)!=PGRES_TUPLES_OK||PQntuples(result)!=1||PQnfields(result)!=5) goto done;
    for(i=0;i<5;i++) if(PQgetisnull(result,0,i)||PQftype(result,i)!=types[i]||PQfformat(result,i)!=1) goto done;
    if(PQgetlength(result,0,0)!=8||PQgetlength(result,0,3)!=64||PQgetlength(result,0,4)!=64) goto done;
    if(!valid_hash(PQgetvalue(result,0,3))||!valid_hash(PQgetvalue(result,0,4))) goto done;
    revision=(const unsigned char *)PQgetvalue(result,0,0);
    if(revision[0]&128) goto done;
    for(i=0;i<8;i++) value.revision=(value.revision<<8)|revision[i];
    pl=(size_t)PQgetlength(result,0,1); bl=(size_t)PQgetlength(result,0,2);
    if(pl<48||pl>4194304||bl<55||bl>4194304) goto done;
    value.frame=(unsigned char *)malloc(8+pl+bl); if(!value.frame) goto done;
    value.frame_length=8+pl+bl; put32(value.frame,pl); put32(value.frame+4,bl);
    memcpy(value.frame+8,PQgetvalue(result,0,1),pl); memcpy(value.frame+8+pl,PQgetvalue(result,0,2),bl);
    memcpy(value.player_hash,PQgetvalue(result,0,3),64); memcpy(value.bank_hash,PQgetvalue(result,0,4),64);
    if(PQsetnonblocking(c,original_mode)!=0) goto done;
    *out=value; value.frame=NULL; ok=0;
done:
    free(value.frame); if(result) PQclear(result);
    return ok;
}
