#include "mstruct.h"
#include <assert.h>
#include <unistd.h>
#include <signal.h>
#include <sys/wait.h>
#include <errno.h>
#include <string.h>
#include <stdio.h>

int Tablesize;
struct { creature *ply; iobuf *io; extra *extr; } Ply[PMAX];
extern volatile sig_atomic_t Deadchildren;
extern void reap_children(void);
static pid_t sleeper=-1;
static int inspected;
int file_exists(char *path)
{
    assert(strstr(path,"lookup.-1")==NULL);
    inspected++;
    return 0; /* Never open/delete real authentication files in this test. */
}
void log_f(char *format,...) { (void)format; }
static void deadline(int sig)
{
    int status;
    (void)sig;
    if(sleeper>0) { kill(sleeper,SIGKILL); while(waitpid(sleeper,&status,0)<0&&errno==EINTR) {} }
    _exit(90);
}
static pid_t exited_child(void)
{
    pid_t pid=fork(); siginfo_t info;
    assert(pid>=0);
    if(pid==0) _exit(0);
    memset(&info,0,sizeof(info));
    assert(waitid(P_PID,(id_t)pid,&info,WEXITED|WNOWAIT)==0);
    assert(info.si_pid==pid);
    return pid;
}
int main(void)
{
    pid_t owned,a,b; int status;
    signal(SIGALRM,deadline);
    sleeper=fork(); assert(sleeper>=0);
    if(sleeper==0) { for(;;) pause(); }
    owned=exited_child(); assert(waitpid(owned,&status,0)==owned);
    /* A synchronous planner already reaped its child; SIGCHLD hint is stale.
     * The live authentication child must not make the game loop wait. */
    Deadchildren=1; alarm(1); reap_children(); alarm(0);
    assert(inspected==0); assert(kill(sleeper,0)==0);
    kill(sleeper,SIGKILL); assert(waitpid(sleeper,&status,0)==sleeper); sleeper=-1;
    /* Signals may coalesce. Process every waitable child, not just the hint
     * count plus one child silently discarded by a final wait4. */
    a=exited_child(); b=exited_child(); Deadchildren=1;
    alarm(1); reap_children(); alarm(0);
    assert(inspected==2 && Deadchildren==0);
    assert(waitpid(a,&status,WNOHANG)==-1 && errno==ECHILD);
    assert(waitpid(b,&status,WNOHANG)==-1 && errno==ECHILD);
    Deadchildren=1; reap_children(); assert(inspected==2);
    puts("GREEN real child reaper: stale hint never waits for live child; all completed children processed");
    return 0;
}
