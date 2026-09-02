#include "character_save_journal_v2_writer.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static const char world_id[]="m3-bootstrap";
static const char candidate[]="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
static const char other_candidate[]="bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

typedef struct acquire_state {
  uint64_t epoch;
  int calls;
  int mismatch;
  int fail;
  int ready_fd;
  int release_fd;
  character_save_journal_v2_writer_tuple request;
  character_save_journal_v2_writer_tuple granted;
} acquire_state;

static int expect(condition,message)
int condition;
const char *message;
{
  if(condition) return 0;
  fprintf(stderr,"character_save_journal_v2_writer_bootstrap_test: %s\n",message);
  return 1;
}

static int join(out,out_size,root,leaf)
char *out;
size_t out_size;
const char *root;
const char *leaf;
{
  int n=snprintf(out,out_size,"%s/%s",root,leaf);
  return n<0||(size_t)n>=out_size?-1:0;
}

static int write_all(fd,bytes,length)
int fd;
const void *bytes;
size_t length;
{
  const char *p=(const char *)bytes;
  ssize_t n;
  while(length) {
    n=write(fd,p,length);
    if(n<0&&errno==EINTR) continue;
    if(n<=0) return -1;
    p+=n;
    length-=(size_t)n;
  }
  return 0;
}

static int write_leaf(root,leaf,bytes,length)
const char *root;
const char *leaf;
const void *bytes;
size_t length;
{
  char path[PATH_MAX];
  int fd,result=0;
  if(join(path,sizeof(path),root,leaf)!=0) return -1;
  fd=open(path,O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);
  if(fd<0) return -1;
  if(fchmod(fd,0600)!=0||write_all(fd,bytes,length)!=0||fsync(fd)!=0) result=-1;
  if(close(fd)!=0) result=-1;
  return result;
}

static int read_leaf(root,leaf,out,out_size)
const char *root;
const char *leaf;
char *out;
size_t out_size;
{
  char path[PATH_MAX];
  int fd;
  ssize_t n;
  if(!out||out_size<2||join(path,sizeof(path),root,leaf)!=0) return -1;
  fd=open(path,O_RDONLY|O_NOFOLLOW);
  if(fd<0) return -1;
  n=read(fd,out,out_size-1);
  if(n<0||(size_t)n==out_size-1||close(fd)!=0) return -1;
  out[n]=0;
  return 0;
}

static int setup(root,root_size)
char *root;
size_t root_size;
{
  char journal[PATH_MAX];
  if(!realpath("/tmp",root)||
     strlen(root)+strlen("/m3-writer-bootstrap.XXXXXX")>=root_size) return -1;
  strcat(root,"/m3-writer-bootstrap.XXXXXX");
  if(!mkdtemp(root)||chmod(root,0700)!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||mkdir(journal,0700)!=0) return -1;
  return 0;
}

static int teardown(root)
const char *root;
{
  static const char *const leaves[]={".m3-writer.lock","writer-instance.v2",
    "writer-epoch.v2",".writer-instance.v2.tmp",".writer-epoch.v2.tmp"};
  char journal[PATH_MAX],path[PATH_MAX];
  size_t i;
  if(join(journal,sizeof(journal),root,"character-save-journal")!=0) return -1;
  for(i=0;i<sizeof(leaves)/sizeof(leaves[0]);i++) {
    if(join(path,sizeof(path),journal,leaves[i])!=0) return -1;
    if(unlink(path)!=0) {
      (void)rmdir(path);
    }
  }
  return rmdir(journal)==0&&rmdir(root)==0?0:-1;
}

static int acquire_epoch(arg,request,granted)
void *arg;
const character_save_journal_v2_writer_tuple *request;
character_save_journal_v2_writer_tuple *granted;
{
  acquire_state *state=(acquire_state *)arg;
  char signal;
  state->calls++;
  if(state->ready_fd>=0) {
    if(write(state->ready_fd,"x",1)!=1||read(state->release_fd,&signal,1)!=1) return -1;
  }
  if(state->fail) return -1;
  *granted=*request;
  granted->writer_epoch=state->epoch;
  if(state->mismatch) strcpy(granted->writer_instance_id,other_candidate);
  state->request=*request;
  state->granted=*granted;
  return 0;
}

static int test_missing_bootstrap(void)
{
  char root[PATH_MAX],journal[PATH_MAX],instance[256],epoch[256];
  character_save_journal_v2_writer_context context;
  character_save_journal_v2_writer_tuple tuple;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=17; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 character_save_journal_v2_writer_validate_held(&context,&tuple)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 !strcmp(tuple.world_id,world_id)&&
                 !strcmp(tuple.writer_instance_id,candidate)&&tuple.writer_epoch==17,
                 "missing identity must bootstrap from caller candidate and one DB acquire");
  failed+=expect(read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))==0&&
                 !strcmp(instance,"version=2\nkind=writer-instance\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\n")&&
                 read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))==0&&
                 !strcmp(epoch,"version=2\nkind=writer-epoch\nworld_id=m3-bootstrap\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\nwriter_epoch=17\n"),
                 "bootstrap must durably install canonical instance and returned epoch");
  failed+=expect(character_save_journal_v2_writer_close(&context)==0,
                 "bootstrapped context must retain and release the original lock");
  failed+=expect(teardown(root)==0,"missing bootstrap fixture cleanup");
  return failed;
}

static int test_crash_retry_and_mismatch(void)
{
  static const char instance_bytes[]="version=2\nkind=writer-instance\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\n";
  char root[PATH_MAX],journal[PATH_MAX],before_instance[256],before_epoch[256],after[256];
  character_save_journal_v2_writer_context context;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||write_leaf(journal,"writer-instance.v2",
       instance_bytes,sizeof(instance_bytes)-1)!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=23; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 character_save_journal_v2_writer_close(&context)==0,
                 "instance-only crash retry must reacquire the same DB identity and persist epoch");
  failed+=expect(read_leaf(journal,"writer-instance.v2",before_instance,sizeof(before_instance))==0&&
                 read_leaf(journal,"writer-epoch.v2",before_epoch,sizeof(before_epoch))==0,
                 "crash retry must retain durable identity bytes for mismatch checks");
  memset(&state,0,sizeof(state)); state.epoch=23; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==23&&
                 !strcmp(state.granted.writer_instance_id,candidate)&&
                 state.granted.writer_epoch==23&&
                 character_save_journal_v2_writer_close(&context)==0&&
                 read_leaf(journal,"writer-instance.v2",after,sizeof(after))==0&&
                 !strcmp(after,before_instance),
                 "existing instance must ignore a fresh candidate and reacquire its durable tuple");
  memset(&state,0,sizeof(state)); state.epoch=23; state.mismatch=1; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==1&&
                 read_leaf(journal,"writer-epoch.v2",after,sizeof(after))==0&&
                 !strcmp(after,before_epoch),
                 "DB tuple mismatch must leave existing epoch byte-for-byte unchanged");
  failed+=expect(write_leaf(journal,".writer-instance.v2.tmp","partial",7)==0&&
                 write_leaf(journal,".writer-epoch.v2.tmp","partial",7)==0,
                 "test must create both exact leftover temporary artifacts");
  memset(&state,0,sizeof(state)); state.epoch=23; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==23&&
                 character_save_journal_v2_writer_close(&context)==0&&
                 read_leaf(journal,"writer-epoch.v2",after,sizeof(after))==0&&
                 !strcmp(after,before_epoch),
                 "leftover epoch temp must be removed before reacquiring durable authority");
  {
    char instance_temp[PATH_MAX],epoch_temp[PATH_MAX];
    failed+=expect(join(instance_temp,sizeof(instance_temp),journal,
                   ".writer-instance.v2.tmp")==0&&access(instance_temp,F_OK)!=0&&
                   errno==ENOENT&&join(epoch_temp,sizeof(epoch_temp),journal,
                   ".writer-epoch.v2.tmp")==0&&access(epoch_temp,F_OK)!=0&&
                   errno==ENOENT,
                   "successful bootstrap cleanup must remove both exact stale temp leaves");
  }
  failed+=expect(teardown(root)==0,"crash retry fixture cleanup");
  return failed;
}

static int test_restart_reuses_durable_tuple(void)
{
  char root[PATH_MAX],journal[PATH_MAX],instance[256],epoch[256],after_instance[256],after_epoch[256];
  character_save_journal_v2_writer_context context;
  character_save_journal_v2_writer_tuple tuple;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=53; state.ready_fd=-1; state.release_fd=-1;
  if(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
       acquire_epoch,&state,&context)!=0||character_save_journal_v2_writer_close(&context)!=0||
     read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))!=0||
     read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=53; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.world_id,world_id)&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==53&&
                 !strcmp(state.granted.world_id,world_id)&&
                 !strcmp(state.granted.writer_instance_id,candidate)&&
                 state.granted.writer_epoch==53&&
                 character_save_journal_v2_writer_validate_held(&context,&tuple)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 !strcmp(tuple.world_id,world_id)&&
                 !strcmp(tuple.writer_instance_id,candidate)&&tuple.writer_epoch==53&&
                 character_save_journal_v2_writer_close(&context)==0&&
                 read_leaf(journal,"writer-instance.v2",after_instance,sizeof(after_instance))==0&&
                 !strcmp(after_instance,instance)&&read_leaf(journal,"writer-epoch.v2",
                 after_epoch,sizeof(after_epoch))==0&&!strcmp(after_epoch,epoch),
                 "restart must reuse durable instance and epoch despite a fresh candidate");
  failed+=expect(teardown(root)==0,"restart reuse fixture cleanup");
  return failed;
}

static int test_orphan_epoch_and_failed_create_retry(void)
{
  static const char epoch_bytes[]="version=2\nkind=writer-epoch\nworld_id=m3-bootstrap\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\nwriter_epoch=67\n";
  static const char instance_bytes[]="version=2\nkind=writer-instance\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\n";
  char root[PATH_MAX],journal[PATH_MAX],instance[256],epoch[256];
  character_save_journal_v2_writer_context context;
  character_save_journal_v2_writer_tuple tuple;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||write_leaf(journal,"writer-epoch.v2",
       epoch_bytes,sizeof(epoch_bytes)-1)!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=67; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==0&&
                 read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))!=0&&
                 read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))==0&&
                 !strcmp(epoch,epoch_bytes),
                 "orphaned epoch must reject before creating an instance or acquiring DB authority");
  failed+=expect(teardown(root)==0,"orphan epoch fixture cleanup");

  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0) return failed+1;
  memset(&state,0,sizeof(state)); state.epoch=71; state.fail=1; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==1&&
                 read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))==0&&
                 !strcmp(instance,instance_bytes)&&
                 read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))!=0,
                 "failed first acquire must leave its newly durable instance for retry");
  memset(&state,0,sizeof(state)); state.epoch=71; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==0&&
                 !strcmp(state.granted.writer_instance_id,candidate)&&
                 state.granted.writer_epoch==71&&
                 character_save_journal_v2_writer_validate_held(&context,&tuple)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 !strcmp(tuple.writer_instance_id,candidate)&&tuple.writer_epoch==71&&
                 character_save_journal_v2_writer_close(&context)==0&&
                 read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))==0&&
                 !strcmp(instance,instance_bytes),
                 "retry after DB-acquired-before-epoch window must use the created durable instance");
  failed+=expect(teardown(root)==0,"failed create retry fixture cleanup");
  return failed;
}

static int test_epoch_install_failure_retry(void)
{
  static const char instance_bytes[]="version=2\nkind=writer-instance\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\n";
  char root[PATH_MAX],journal[PATH_MAX],instance[256],epoch[256];
  character_save_journal_v2_writer_context context;
  character_save_journal_v2_writer_tuple tuple;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=79; state.ready_fd=-1; state.release_fd=-1;
  character_save_journal_v2_writer_fail_epoch_create_once_for_test();
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==0&&
                 !strcmp(state.granted.writer_instance_id,candidate)&&
                 state.granted.writer_epoch==79&&
                 read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))==0&&
                 !strcmp(instance,instance_bytes)&&
                 read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))!=0,
                 "DB grant before epoch persistence failure must retain only the created instance");
  memset(&state,0,sizeof(state)); state.epoch=79; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==0&&
                 !strcmp(state.granted.writer_instance_id,candidate)&&
                 state.granted.writer_epoch==79&&
                 character_save_journal_v2_writer_validate_held(&context,&tuple)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 !strcmp(tuple.writer_instance_id,candidate)&&tuple.writer_epoch==79&&
                 character_save_journal_v2_writer_close(&context)==0,
                 "retry after epoch persistence failure must reacquire the original durable instance");
  failed+=expect(teardown(root)==0,"epoch install retry fixture cleanup");
  return failed;
}

static int test_concurrent_bootstrap_lock(void)
{
  char root[PATH_MAX],signal;
  int ready[2],release[2],status,failed=0;
  pid_t child;
  acquire_state state;
  character_save_journal_v2_writer_context context;
  if(setup(root,sizeof(root))!=0||pipe(ready)!=0||pipe(release)!=0) return 1;
  child=fork();
  if(child==0) {
    close(ready[0]); close(release[1]);
    memset(&state,0,sizeof(state)); state.epoch=31; state.ready_fd=ready[1]; state.release_fd=release[0];
    if(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
         acquire_epoch,&state,&context)!=0) _exit(2);
    _exit(character_save_journal_v2_writer_close(&context)==0?0:3);
  }
  if(child<0) return 1;
  close(ready[1]); close(release[0]);
  alarm(5);
  if(read(ready[0],&signal,1)!=1) return 1;
  memset(&state,0,sizeof(state)); state.epoch=31; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&errno==EWOULDBLOCK&&state.calls==0,
                 "concurrent bootstrap must serialize before invoking a second DB acquire");
  if(write(release[1],"x",1)!=1) return failed+1;
  close(ready[0]); close(release[1]);
  if(waitpid(child,&status,0)!=child) return failed+1;
  alarm(0);
  failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0,
                 "lock holder must finish bootstrap after concurrent rejection");
  failed+=expect(teardown(root)==0,"concurrent bootstrap fixture cleanup");
  return failed;
}

static int test_rejected_authority_and_callback(void)
{
  static const char instance_bytes[]="version=2\nkind=writer-instance\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\n";
  static const char epoch_bytes[]="version=2\nkind=writer-epoch\nworld_id=m3-bootstrap\nwriter_instance_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\nwriter_epoch=41\n";
  char root[PATH_MAX],journal[PATH_MAX],instance[256],epoch[256],after[256],target[PATH_MAX];
  character_save_journal_v2_writer_context context;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||write_leaf(journal,"writer-instance.v2",
       "malformed",9)!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=41; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==0,
                 "malformed durable instance must reject before DB acquire");
  failed+=expect(teardown(root)==0,"malformed authority fixture cleanup");

  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||join(target,sizeof(target),journal,
       "writer-instance.v2")!=0||symlink("/dev/null",target)!=0) return failed+1;
  memset(&state,0,sizeof(state)); state.epoch=41; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==0,
                 "symlink identity target must reject before DB acquire");
  failed+=expect(teardown(root)==0,"symlink authority fixture cleanup");

  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||write_leaf(journal,"writer-instance.v2",
       instance_bytes,sizeof(instance_bytes)-1)!=0||write_leaf(journal,
       "writer-epoch.v2",epoch_bytes,sizeof(epoch_bytes)-1)!=0||
       read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))!=0||
       read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))!=0) return failed+1;
  memset(&state,0,sizeof(state)); state.epoch=41; state.fail=1; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==1&&
                 read_leaf(journal,"writer-instance.v2",after,sizeof(after))==0&&
                 !strcmp(after,instance)&&read_leaf(journal,"writer-epoch.v2",
                 after,sizeof(after))==0&&!strcmp(after,epoch),
                 "DB callback failure must preserve both existing identity leaves byte-for-byte");
  memset(&state,0,sizeof(state)); state.epoch=0; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==1&&
                 read_leaf(journal,"writer-epoch.v2",after,sizeof(after))==0&&
                 !strcmp(after,epoch),
                 "nonpositive DB epoch must reject without changing existing authority");
  memset(&state,0,sizeof(state)); state.epoch=(uint64_t)INT64_MAX+1U; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==1&&
                 read_leaf(journal,"writer-epoch.v2",after,sizeof(after))==0&&
                 !strcmp(after,epoch),
                 "overflow DB epoch must reject without changing existing authority");
  failed+=expect(teardown(root)==0,"rejected callback fixture cleanup");
  return failed;
}

static int test_temp_cleanup_fail_closed(void)
{
  char root[PATH_MAX],journal[PATH_MAX],temp[PATH_MAX];
  character_save_journal_v2_writer_context context;
  acquire_state state;
  int failed=0;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||write_leaf(journal,".writer-instance.v2.tmp",
       "partial",7)!=0) return 1;
  memset(&state,0,sizeof(state)); state.epoch=89; state.ready_fd=-1; state.release_fd=-1;
  character_save_journal_v2_writer_fail_temp_cleanup_fsync_once_for_test();
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==0,
                 "temporary cleanup directory fsync failure must reject before DB acquire");
  failed+=expect(join(temp,sizeof(temp),journal,".writer-instance.v2.tmp")==0&&
                 access(temp,F_OK)!=0&&errno==ENOENT,
                 "failed cleanup fsync must never promote or retain the removed temp leaf");
  failed+=expect(teardown(root)==0,"cleanup fsync failure fixture cleanup");

  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0||join(temp,sizeof(temp),journal,
       ".writer-epoch.v2.tmp")!=0||mkdir(temp,0700)!=0) return failed+1;
  memset(&state,0,sizeof(state)); state.epoch=89; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)<0&&state.calls==0&&
                 access(temp,F_OK)==0,
                 "directory temporary residue must fail closed before DB acquire");
  failed+=expect(teardown(root)==0,"directory temporary fixture cleanup");
  return failed;
}

static int test_sigkill_temp_crash_recovery(void)
{
  char root[PATH_MAX],journal[PATH_MAX],temp[PATH_MAX],instance[256],epoch[256];
  character_save_journal_v2_writer_context context;
  acquire_state state;
  int status,failed=0;
  pid_t child;
  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0) return 1;
  child=fork();
  if(child==0) {
    memset(&state,0,sizeof(state)); state.epoch=97; state.ready_fd=-1; state.release_fd=-1;
    character_save_journal_v2_writer_crash_after_temp_create_for_test(1,0);
    (void)character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                                                     acquire_epoch,&state,&context);
    _exit(2);
  }
  if(child<0||waitpid(child,&status,0)!=child) return 1;
  failed+=expect(WIFSIGNALED(status)&&WTERMSIG(status)==SIGKILL&&
                 join(temp,sizeof(temp),journal,".writer-instance.v2.tmp")==0&&
                 access(temp,F_OK)==0,
                 "SIGKILL during instance temp creation must leave only a non-authoritative temp");
  memset(&state,0,sizeof(state)); state.epoch=97; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 character_save_journal_v2_writer_close(&context)==0&&
                 read_leaf(journal,"writer-instance.v2",instance,sizeof(instance))==0&&
                 read_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch))==0&&
                 access(temp,F_OK)!=0&&errno==ENOENT,
                 "instance temp crash retry must clean residue and install durable tuple");
  memset(&state,0,sizeof(state)); state.epoch=97; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==97&&
                 character_save_journal_v2_writer_close(&context)==0,
                 "instance temp crash recovery must ignore a fresh candidate on restart");
  failed+=expect(teardown(root)==0,"instance temp SIGKILL fixture cleanup");

  if(setup(root,sizeof(root))!=0||join(journal,sizeof(journal),root,
       "character-save-journal")!=0) return failed+1;
  child=fork();
  if(child==0) {
    memset(&state,0,sizeof(state)); state.epoch=101; state.ready_fd=-1; state.release_fd=-1;
    character_save_journal_v2_writer_crash_after_temp_create_for_test(0,1);
    (void)character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                                                     acquire_epoch,&state,&context);
    _exit(2);
  }
  if(child<0||waitpid(child,&status,0)!=child) return failed+1;
  failed+=expect(WIFSIGNALED(status)&&WTERMSIG(status)==SIGKILL&&
                 join(temp,sizeof(temp),journal,".writer-epoch.v2.tmp")==0&&
                 access(temp,F_OK)==0&&read_leaf(journal,"writer-instance.v2",
                 instance,sizeof(instance))==0&&read_leaf(journal,"writer-epoch.v2",
                 epoch,sizeof(epoch))!=0,
                 "SIGKILL after DB grant during epoch temp creation must retain its durable instance");
  memset(&state,0,sizeof(state)); state.epoch=101; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,other_candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==0&&
                 character_save_journal_v2_writer_close(&context)==0&&
                 access(temp,F_OK)!=0&&errno==ENOENT&&read_leaf(journal,
                 "writer-epoch.v2",epoch,sizeof(epoch))==0,
                 "epoch temp crash retry must clean residue, reacquire, and ignore fresh candidate");
  memset(&state,0,sizeof(state)); state.epoch=101; state.ready_fd=-1; state.release_fd=-1;
  failed+=expect(character_save_journal_v2_writer_bootstrap(root,world_id,candidate,
                  acquire_epoch,&state,&context)==0&&state.calls==1&&
                 !strcmp(state.request.writer_instance_id,candidate)&&
                 state.request.writer_epoch==101&&
                 character_save_journal_v2_writer_close(&context)==0,
                 "epoch temp crash recovery must re-acquire the same durable epoch on restart");
  failed+=expect(teardown(root)==0,"epoch temp SIGKILL fixture cleanup");
  return failed;
}

int main(void)
{
  int failed;
  character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
  failed=test_missing_bootstrap();
  failed+=test_crash_retry_and_mismatch();
  failed+=test_restart_reuses_durable_tuple();
  failed+=test_orphan_epoch_and_failed_create_retry();
  failed+=test_epoch_install_failure_retry();
  failed+=test_concurrent_bootstrap_lock();
  failed+=test_rejected_authority_and_callback();
  failed+=test_temp_cleanup_fail_closed();
  failed+=test_sigkill_temp_crash_recovery();
  return failed?1:0;
}
