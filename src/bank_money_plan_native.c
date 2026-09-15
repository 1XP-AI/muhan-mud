#define _GNU_SOURCE
#include "bank_money_plan_native.h"
#include <spawn.h>
#include <sys/socket.h>
#include <sys/wait.h>
#include <poll.h>
#include <time.h>
#include <unistd.h>
#include <fcntl.h>
#include <signal.h>
#include <errno.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define FRAME_MAX (8388608U+8U)
static int64_t clock_ms(void)
{
    struct timespec ts;
    if(clock_gettime(CLOCK_MONOTONIC,&ts)) return -1;
    return (int64_t)ts.tv_sec*1000+ts.tv_nsec/1000000;
}
static size_t get32(const unsigned char *p)
{ return (size_t)p[0]*16777216U+(size_t)p[1]*65536U+(size_t)p[2]*256U+p[3]; }
static int frame_valid(const unsigned char *p,size_t n)
{
    size_t a,b;
    if(!p||n<8||n>FRAME_MAX) return 0;
    a=get32(p); b=get32(p+4);
    return a>=48&&a<=4194304&&b>=55&&b<=4194304&&a+b+8==n;
}
static int exchange(const char *path,const char *const *args,int count,const unsigned char *input,size_t length,int timeout_ms,unsigned char **out,size_t *out_length,size_t prefix,int paired)
{
    int in[2]={-1,-1},output[2]={-1,-1},i,status=0,exited=0,eof=0,result=-1,actions_ready=0;
    pid_t pid=-1,waited;
    posix_spawn_file_actions_t actions;
    char *argv[18],*env[]={"LANG=C.UTF-8",NULL};
    unsigned char *bytes=NULL;
    size_t sent=0,used=0,limit=paired?FRAME_MAX+prefix:4194304U;
    ssize_t n;
    int64_t now,deadline;
    struct pollfd fds[2];
    if(out) *out=NULL;
    if(out_length) *out_length=0;
    if(!out||!out_length||!path||path[0]!='/'||!args||count<1||count>16||timeout_ms<1||timeout_ms>10000) return -1;
    if(paired?!frame_valid(input,length):(!input||length<48||length>4194304U)) return -1;
    for(i=0;i<3;i++) if(fcntl(i,F_GETFD)<0) return -1;
    argv[0]=(char *)path; argv[count+1]=NULL;
    for(i=0;i<count;i++) { if(!args[i]) return -1; argv[i+1]=(char *)args[i]; }
    now=clock_ms(); if(now<0) return -1; deadline=now+timeout_ms;
    bytes=(unsigned char *)malloc(limit+1); if(!bytes) goto done;
    if(socketpair(AF_UNIX,SOCK_STREAM|SOCK_CLOEXEC,0,in)||socketpair(AF_UNIX,SOCK_STREAM|SOCK_CLOEXEC,0,output)) goto done;
    if(posix_spawn_file_actions_init(&actions)) goto done;
    actions_ready=1;
    if(posix_spawn_file_actions_adddup2(&actions,in[1],0)||posix_spawn_file_actions_adddup2(&actions,output[1],1)
       ||posix_spawn_file_actions_addopen(&actions,2,"/dev/null",O_WRONLY,0)
       ||posix_spawn_file_actions_addclosefrom_np(&actions,3)) goto done;
    if(posix_spawn(&pid,path,&actions,NULL,argv,env)) { pid=-1; goto done; }
    close(in[1]); in[1]=-1; close(output[1]); output[1]=-1;
    if(fcntl(in[0],F_SETFL,O_NONBLOCK)||fcntl(output[0],F_SETFL,O_NONBLOCK)) goto done;
    for(;;) {
        now=clock_ms(); if(now<0||now>=deadline) goto done;
        if(sent<length) {
            n=send(in[0],input+sent,length-sent,MSG_NOSIGNAL);
            if(n>0) { sent+=(size_t)n; if(sent==length) shutdown(in[0],SHUT_WR); }
            else if(n<0&&errno!=EAGAIN&&errno!=EWOULDBLOCK&&errno!=EINTR) goto done;
        }
        if(!eof) {
            n=recv(output[0],bytes+used,limit+1-used,0);
            if(n>0) { used+=(size_t)n; if(used>limit) goto done; }
            else if(n==0) eof=1;
            else if(errno!=EAGAIN&&errno!=EWOULDBLOCK&&errno!=EINTR) goto done;
        }
        if(!exited) {
            waited=waitpid(pid,&status,WNOHANG);
            if(waited==pid) exited=1;
            else if(waited<0&&errno!=EINTR) { if(errno==ECHILD) pid=-1; goto done; }
        }
        if(exited&&eof) break;
        fds[0].fd=sent<length?in[0]:-1; fds[0].events=POLLOUT; fds[0].revents=0;
        fds[1].fd=eof?-1:output[0]; fds[1].events=POLLIN; fds[1].revents=0;
        /* Bounded reap polling also handles EOF arriving just before exit. */
        if(poll(fds,2,(int)(deadline-now<10?deadline-now:10))<0&&errno!=EINTR) goto done;
    }
    if(sent!=length||!WIFEXITED(status)||WEXITSTATUS(status)!=0||used<prefix) goto done;
    if(paired?!frame_valid(bytes+prefix,used-prefix):used<48) goto done;
    *out=bytes; *out_length=used; bytes=NULL; result=0;
done:
    if(actions_ready) posix_spawn_file_actions_destroy(&actions);
    for(i=0;i<2;i++) { if(in[i]>=0) close(in[i]); if(output[i]>=0) close(output[i]); }
    if(pid>0&&!exited) { kill(pid,SIGKILL); while(waitpid(pid,&status,0)<0&&errno==EINTR) {} }
    free(bytes); return result;
}
int bank_money_process_native(const char *path,const char *const *args,int count,const unsigned char *input,size_t length,int timeout_ms,unsigned char **out,size_t *out_length)
{ return exchange(path,args,count,input,length,timeout_ms,out,out_length,0,1); }
int player_snapshot_process_native(const char *path,const char *const *args,int count,const unsigned char *input,size_t length,int timeout_ms,unsigned char **out,size_t *out_length)
{ return exchange(path,args,count,input,length,timeout_ms,out,out_length,0,0); }
int bank_money_plan_native(const char *path,const char *const args[4],const unsigned char *input,size_t length,int timeout_ms,unsigned char **out,size_t *out_length)
{ return bank_money_process_native(path,args,4,input,length,timeout_ms,out,out_length); }
int bank_money_plan_resolved_native(const char *path,const char *const args[4],const unsigned char *input,size_t length,int timeout_ms,unsigned char **out,size_t *out_length,uint64_t *amount)
{
    const char *extended[5]; unsigned char *bytes=NULL; size_t size=0; uint64_t value=0; int i;
    if(out) *out=NULL;
    if(out_length) *out_length=0;
    if(amount) *amount=0;
    if(!args||!out||!out_length||!amount) return -1;
    for(i=0;i<4;i++) extended[i]=args[i];
    extended[4]="--resolved-amount-v1";
    if(exchange(path,extended,5,input,length,timeout_ms,&bytes,&size,8,1)) return -1;
    for(i=0;i<8;i++) value=(value<<8)|bytes[i];
    if(!value||value>INT64_MAX) {free(bytes); return -1;}
    memmove(bytes,bytes+8,size-8); *out=bytes; *out_length=size-8; *amount=value; return 0;
}
