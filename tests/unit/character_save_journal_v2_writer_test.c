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

static const char instance_id[] = "11111111-1111-4111-8111-111111111111";
static const char world_id[] = "m3-contract";

static int expect(condition, message)
int condition;
const char *message;
{
  if(condition) {
    return 0;
  }
  fprintf(stderr, "character_save_journal_v2_writer_test: %s\n", message);
  return 1;
}

static int join(out, out_size, root, leaf)
char *out; size_t out_size; const char *root; const char *leaf;
{
  int n=snprintf(out,out_size,"%s/%s",root,leaf);
  return n<0||(size_t)n>=out_size?-1:0;
}

static int write_all(fd, bytes, length)
int fd; const void *bytes; size_t length;
{
  const char *p=(const char *)bytes;
  ssize_t n;
  while(length) {
    n=write(fd,p,length);
    if(n<0&&errno==EINTR) {
      continue;
    }
    if(n<=0) {
      return -1;
    }
    p+=n;
    length-=(size_t)n;
  }
  return 0;
}

static int write_leaf(root, leaf, bytes, length)
const char *root; const char *leaf; const void *bytes; size_t length;
{
  char path[PATH_MAX];
  int fd;
  int result=0;
  if(join(path,sizeof(path),root,leaf)!=0) {
    return -1;
  }
  fd=open(path,O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);
  if(fd<0) {
    return -1;
  }
  if(fchmod(fd,0600)!=0||write_all(fd,bytes,length)!=0||fsync(fd)!=0) {
    result=-1;
  }
  if(close(fd)!=0) {
    result=-1;
  }
  return result;
}

static int fixture(root)
char *root;
{
  static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
  static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
  char journal[PATH_MAX];
  if(join(journal,sizeof(journal),root,"character-save-journal")!=0||
     mkdir(journal,0700)!=0) {
    return -1;
  }
  return write_leaf(journal,"writer-instance.v2",instance,sizeof(instance)-1)||
         write_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch)-1)?-1:0;
}

static int replace_epoch(root, bytes, length)
const char *root; const void *bytes; size_t length;
{
  char journal[PATH_MAX];
  if(join(journal,sizeof(journal),root,"character-save-journal")!=0) {
    return -1;
  }
  return write_leaf(journal,"writer-epoch.v2",bytes,length);
}

static int remove_flat(path)
const char *path;
{
  char child[PATH_MAX];
  const char *leaves[]={".m3-writer.lock","writer-instance.v2","writer-epoch.v2"};
  size_t i;
  for(i=0;i<sizeof(leaves)/sizeof(leaves[0]);i++) {
    if(join(child,sizeof(child),path,leaves[i])!=0) {
      return -1;
    }
    unlink(child);
  }
  return rmdir(path);
}

static int teardown(root)
const char *root;
{
  char journal[PATH_MAX];
  if(join(journal,sizeof(journal),root,"character-save-journal")!=0) {
    return -1;
  }
  return remove_flat(journal)==0&&rmdir(root)==0?0:-1;
}

static int test_load_and_parser(root)
char *root;
{
  character_save_journal_v2_writer_context c;
  character_save_journal_v2_writer_tuple tuple;
  char journal[PATH_MAX];
  char instance[PATH_MAX];
  static const char epoch_ok[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
  static const char trailing[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\nextra\n";
  static const char epoch_zero[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=0\n";
  static const char epoch_big[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=9223372036854775808\n";
  static const char epoch_other_instance[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=22222222-2222-4222-8222-222222222222\nwriter_epoch=7\n";
  char nul[256];
  int n;
  int failed=0;
  memset(&c,0xa5,sizeof(c));
  memset(&tuple,0,sizeof(tuple));
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)==0&&
                 character_save_journal_v2_writer_validate_held(&c,&tuple)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 tuple.writer_epoch==7&&!strcmp(tuple.writer_instance_id,instance_id),
                 "exact persisted tuple must load under lifetime lock");
  failed+=expect(character_save_journal_v2_writer_close(&c)==0,"exact context close must release all ownership");
  failed+=expect(character_save_journal_v2_writer_open(root,"other-world",&c)<0,"requested world must exactly match persisted epoch tuple");
  failed+=expect(replace_epoch(root,trailing,sizeof(trailing)-1)==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"trailing epoch bytes must reject");
  failed+=expect(replace_epoch(root,epoch_zero,sizeof(epoch_zero)-1)==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"epoch zero must reject");
  failed+=expect(replace_epoch(root,epoch_big,sizeof(epoch_big)-1)==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"epoch above INT64_MAX must reject");
  failed+=expect(replace_epoch(root,epoch_other_instance,sizeof(epoch_other_instance)-1)==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"instance and epoch leaves must exactly agree");
  n=snprintf(nul,sizeof(nul),"version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=%s\nwriter_epoch=7\nignored",instance_id);
  nul[n-(int)strlen("ignored")]=0;
  failed+=expect(n>0&&replace_epoch(root,nul,(size_t)n)==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"embedded NUL plus raw trailing bytes must reject");
  memset(nul,'x',sizeof(nul));
  failed+=expect(replace_epoch(root,nul,sizeof(nul))==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"overlong epoch leaf must reject");
  failed+=expect(replace_epoch(root,epoch_ok,sizeof(epoch_ok)-1)==0,"valid epoch fixture must restore");
  failed+=expect(join(journal,sizeof(journal),root,"character-save-journal")==0&&join(instance,sizeof(instance),journal,"writer-instance.v2")==0&&unlink(instance)==0&&character_save_journal_v2_writer_open(root,world_id,&c)<0,"missing instance must reject");
  return failed;
}

static int test_invalid_world_resets_context(root)
const char *root;
{
  character_save_journal_v2_writer_context context, zero;
  int failed = 0;
  (void)root;
  memset(&context,0xa5,sizeof(context));
  memset(&zero,0,sizeof(zero));
  failed+=expect(character_save_journal_v2_writer_open(root,"M3-invalid",&context)<0&&
                 !memcmp(&context,&zero,sizeof(context)),
                 "invalid world must reset the opaque output handle before return");
  return failed;
}

static int test_existing_opener_syncs_created_lock(root)
char *root;
{
  char journal[PATH_MAX],lock[PATH_MAX],signal;
  int ready[2],release[2],status,failed=0;
  pid_t child;
  unsigned int lock_syncs,journal_syncs;
  character_save_journal_v2_writer_context context;

  if(join(journal,sizeof(journal),root,"character-save-journal")||
     join(lock,sizeof(lock),journal,".m3-writer.lock")||unlink(lock)!=0||
     pipe(ready)!=0||pipe(release)!=0) {
    return 1;
  }
  child=fork();
  if(child==0) {
      character_save_journal_v2_writer_context held;
      close(ready[0]);
      close(release[1]);
      character_save_journal_v2_writer_pause_after_create_for_test(ready[1],release[0]);
      if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) {
        _exit(2);
      }
      _exit(character_save_journal_v2_writer_close(&held)==0?0:3);
  }
  if(child<0) {
    return 1;
  }
  close(ready[1]);
  close(release[0]);
  alarm(5);
  if(read(ready[0],&signal,1)!=1) {
    return 1;
  }
  character_save_journal_v2_writer_reset_fsync_counts_for_test();
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&context)==0,
                 "existing opener must acquire while creator is paused before flock");
  character_save_journal_v2_writer_fsync_counts_for_test(&lock_syncs,&journal_syncs);
  failed+=expect(lock_syncs==1&&journal_syncs==1,
                 "successful existing opener must fsync lock and journal directory");
  failed+=expect(character_save_journal_v2_writer_close(&context)==0,
                 "existing opener must release before paused creator continues");
  if(write(release[1],"x",1)!=1) {
    return failed+1;
  }
  close(ready[0]);
  close(release[1]);
  if(waitpid(child,&status,0)!=child) {
    return failed+1;
  }
  alarm(0);
  failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0,
                 "paused creator must complete after existing opener releases");
  return failed;
}

static int test_lock_lifetime(root)
char *root;
{
  int ready[2];
  int release[2];
  int status;
  int failed=0;
  pid_t child;
  character_save_journal_v2_writer_context c;
  char journal[PATH_MAX];
  char lock[PATH_MAX];
  struct stat before;
  struct stat after;
  static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
  static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
  if(join(journal,sizeof(journal),root,"character-save-journal")||
     write_leaf(journal,"writer-instance.v2",instance,sizeof(instance)-1)||
     write_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch)-1)||
     pipe(ready)||pipe(release)) {
    return 1;
  }
  child=fork();
  if(child==0) {
    char x;
    character_save_journal_v2_writer_context held;
    close(ready[0]);
    close(release[1]);
    if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) {
      _exit(2);
    }
    if(write(ready[1],"x",1)!=1) {
      _exit(3);
    }
    if(read(release[0],&x,1)!=1) {
      _exit(4);
    }
    _exit(character_save_journal_v2_writer_close(&held)==0?0:5);
  }
  if(child<0) {
    return 1;
  }
  close(ready[1]);
  close(release[0]);
  if(read(ready[0],&status,1)!=1) {
    return 1;
  }
  if(join(lock,sizeof(lock),journal,".m3-writer.lock")||stat(lock,&before)!=0) {
    return 1;
  }
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0&&errno==EWOULDBLOCK&&stat(lock,&after)==0&&before.st_dev==after.st_dev&&before.st_ino==after.st_ino,"fork-before-open loser must leave lock leaf unchanged");
  if(write(release[1],"x",1)!=1) {
    return failed+1;
  }
  close(ready[0]);
  close(release[1]);
  if(waitpid(child,&status,0)!=child) {
    return failed+1;
  }
  failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0&&character_save_journal_v2_writer_open(root,world_id,&c)==0,"release must permit exactly one later acquire");
  failed+=expect(character_save_journal_v2_writer_close(&c)==0,"later context close must succeed");
  return failed;
}

static int test_owner_registry(root)
char *root;
{
  character_save_journal_v2_writer_context owner,copied,zero,garbage;
  character_save_journal_v2_writer_tuple before,after;
  pid_t child;
  int status;
  int stdin_flags;
  int failed=0;
  if(character_save_journal_v2_writer_open(root,world_id,&owner)!=0) return 1;
  copied=owner;
  memset(&zero,0,sizeof(zero));
  memset(&garbage,0xa5,sizeof(garbage));
  memset(&before,0xa5,sizeof(before)); after=before;
  failed+=expect(character_save_journal_v2_writer_validate_held(&copied,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID&&
                 !memcmp(&before,&after,sizeof(before)),
                 "copied handle must fail registry validation without changing snapshot");
  after=before;
  failed+=expect(character_save_journal_v2_writer_validate_held(&zero,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID&&
                 !memcmp(&before,&after,sizeof(before)),
                 "zero handle must fail registry validation without changing snapshot");
  after=before;
  failed+=expect(character_save_journal_v2_writer_validate_held(&garbage,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID&&
                 !memcmp(&before,&after,sizeof(before)),
                 "garbage handle must fail before its state pointer is dereferenced");
  child=fork();
  if(child==0) {
    character_save_journal_v2_writer_tuple child_tuple;
    if(character_save_journal_v2_writer_validate_held(&owner,&child_tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID||
       character_save_journal_v2_writer_close(&owner)==0) _exit(2);
    _exit(0);
  }
  if(child<0||waitpid(child,&status,0)!=child) return failed+1;
  failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0,
                 "fork-after-open child must neither validate nor close parent ownership");
  stdin_flags=fcntl(0,F_GETFD);
  failed+=expect(character_save_journal_v2_writer_close(&copied)<0&&
                 character_save_journal_v2_writer_close(&zero)<0&&
                 character_save_journal_v2_writer_close(&garbage)<0&&
                 (stdin_flags<0||fcntl(0,F_GETFD)==stdin_flags),
                 "foreign and zero close must not close fd zero or the active owner");
  failed+=expect(character_save_journal_v2_writer_validate_held(&owner,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 after.writer_epoch==7&&
                 character_save_journal_v2_writer_close(&owner)==0,
                 "original owner must remain usable and close after foreign rejection");
  return failed;
}

/* Handle storage is deliberately not a credential.  The registered object
 * address, pid, and private generation carry all authority instead. */
static int test_opaque_handle_boundaries(root)
char *root;
{
  character_save_journal_v2_writer_context owner,copied,zero;
  character_save_journal_v2_writer_tuple first,second,before,after;
  int failed=0;
  memset(&owner,0xa5,sizeof(owner));
  memset(&zero,0,sizeof(zero));
  memset(&before,0xa5,sizeof(before));
  after=before;
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,0)<0&&
                 character_save_journal_v2_writer_open(0,world_id,&owner)<0&&
                 character_save_journal_v2_writer_open(root,0,&owner)<0&&
                 !memcmp(&owner,&zero,sizeof(owner)),
                 "NULL writer_open arguments must fail and leave no usable handle");
  failed+=expect(character_save_journal_v2_writer_validate_held(0,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID&&
                 !memcmp(&before,&after,sizeof(before)),
                 "NULL context validation must not write a tuple");
  if(character_save_journal_v2_writer_open(root,world_id,&owner)!=0) return failed+1;
  failed+=expect(character_save_journal_v2_writer_validate_held(&owner,0)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID,
                 "NULL tuple validation must fail without creating authority");
  failed+=expect(character_save_journal_v2_writer_validate_held(&owner,&first)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK,
                 "exact owner must validate before opaque byte corruption");
  copied=owner;
  memset(&owner,0x5a,sizeof(owner));
  failed+=expect(character_save_journal_v2_writer_validate_held(&owner,&second)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&
                 !strcmp(second.world_id,world_id)&&second.writer_epoch==7&&
                 !memcmp(&first,&second,sizeof(first)),
                 "owner handle byte corruption must not alter the immutable tuple snapshot");
  first.writer_epoch=99;
  failed+=expect(character_save_journal_v2_writer_validate_held(&owner,&second)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&second.writer_epoch==7,
                 "caller mutation of a returned tuple must not create authority");
  failed+=expect(character_save_journal_v2_writer_close(&owner)==0&&
                 character_save_journal_v2_writer_validate_held(&owner,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID&&
                 character_save_journal_v2_writer_close(&owner)<0&&
                 character_save_journal_v2_writer_close(&copied)<0,
                 "post-close validate and exact or copied close must be invalid");
  if(character_save_journal_v2_writer_open(root,world_id,&owner)!=0) return failed+1;
  after=before;
  failed+=expect(character_save_journal_v2_writer_validate_held(&copied,&after)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID&&
                 !memcmp(&before,&after,sizeof(before))&&
                 character_save_journal_v2_writer_close(&copied)<0&&
                 character_save_journal_v2_writer_close(&owner)==0,
                 "a stale pre-close copy must remain invalid across same-address reopen");
  return failed;
}

static int test_leaf_contract_and_faults(root)
char *root;
{
  character_save_journal_v2_writer_context c;
  char journal[PATH_MAX];
  char lock[PATH_MAX];
  int fd;
  int failed=0;
  if(join(journal,sizeof(journal),root,"character-save-journal")||
     join(lock,sizeof(lock),journal,".m3-writer.lock")) {
    return 1;
  }
  fd=open(lock,O_RDWR|O_NOFOLLOW);
  if(fd<0) {
    return 1;
  }
  close(fd);
  if(chmod(lock,0644)!=0) {
    return 1;
  }
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,"existing wrong lock mode must reject without repair");
  if(chmod(lock,0600)!=0) {
    return failed+1;
  }
  if(unlink(lock)!=0||mkdir(lock,0700)!=0) {
    return failed+1;
  }
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,"existing non-regular lock leaf must reject without replacement");
  if(rmdir(lock)!=0) {
    return failed+1;
  }
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)==0&&character_save_journal_v2_writer_close(&c)==0,"lock recreation after explicit fixture removal must succeed");
  {
      char alias[PATH_MAX];
      if(join(alias,sizeof(alias),journal,".m3-writer.lock-link")!=0||
         link(lock,alias)!=0) {
        return failed+1;
      }
      failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,"existing hard-linked lock leaf must reject unchanged");
      if(unlink(alias)!=0) {
        return failed+1;
      }
  }
  character_save_journal_v2_writer_set_trusted_uid_for_test(getuid()==(uid_t)0?(uid_t)1:(uid_t)0);
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,"wrong trusted owner must reject without repair");
  character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
  character_save_journal_v2_writer_fail_close_once_for_test(1);
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)==0&&character_save_journal_v2_writer_close(&c)<0&&character_save_journal_v2_writer_open(root,world_id,&c)==0,"close uncertainty must consume descriptors and permit later acquire");
  character_save_journal_v2_writer_fail_close_once_for_test(0);
  failed+=expect(character_save_journal_v2_writer_close(&c)==0,"post-uncertainty context must close normally");
  unlink(lock);
  character_save_journal_v2_writer_fail_fsync_for_test(1,0);
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,"new lock file fsync fault must fail closed");
  character_save_journal_v2_writer_fail_fsync_for_test(0,0);
  if(unlink(lock)!=0) {
    return failed+1;
  }
  character_save_journal_v2_writer_fail_fsync_for_test(0,1);
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,"new lock directory fsync fault must fail closed");
  character_save_journal_v2_writer_fail_fsync_for_test(0,0);
  character_save_journal_v2_writer_reset_fsync_counts_for_test();
  if(unlink(lock)!=0) {
    return failed+1;
  }
  character_save_journal_v2_writer_fail_fsync_for_test(1,0);
  failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)<0,
                 "creator file fsync fault must retain lock leaf for a retry");
  {
      unsigned int first_file,first_dir,second_file,second_dir;
      character_save_journal_v2_writer_fsync_counts_for_test(&first_file,&first_dir);
      character_save_journal_v2_writer_fail_fsync_for_test(0,0);
      failed+=expect(character_save_journal_v2_writer_open(root,world_id,&c)==0,
                     "retry after creator fsync fault must acquire existing lock");
      character_save_journal_v2_writer_fsync_counts_for_test(&second_file,&second_dir);
      failed+=expect(second_file==first_file+1&&second_dir==first_dir+1,
                     "retry must rerun both lock and journal fsync operations");
      failed+=expect(character_save_journal_v2_writer_close(&c)==0,
                     "retry context must close normally");
  }
  return failed;
}

static int test_root_duplicate_is_cloexec(root)
char *root;
{
  character_save_journal_v2_writer_context context;
  int duplicate=-1, flags, failed=0;
  if(character_save_journal_v2_writer_open(root,world_id,&context)!=0) return 1;
  failed+=expect(character_save_journal_v2_writer_dup_held_root_fd(&context,&duplicate)==
                 CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&duplicate>=0&&
                 (flags=fcntl(duplicate,F_GETFD))>=0&&(flags&FD_CLOEXEC),
                 "root descriptor duplicate must be atomically returned CLOEXEC");
  if(duplicate>=0&&close(duplicate)!=0) failed++;
  failed+=expect(character_save_journal_v2_writer_close(&context)==0,
                 "CLOEXEC duplicate test context must close normally");
  return failed;
}

int main(void)
{
  char temp_template[]="/tmp/character-save-journal-v2-writer-test-XXXXXX";
  char root[PATH_MAX];
  int failed;
  if(!realpath("/tmp",root)||
     strlen(root)+strlen("/character-save-journal-v2-writer-test-XXXXXX")>=sizeof(root)) {
    return 1;
  }
  strcat(root,"/character-save-journal-v2-writer-test-XXXXXX");
  (void)temp_template;
  if(!mkdtemp(root)||fixture(root)!=0) {
    return 1;
  }
  character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
  failed=test_load_and_parser(root)+test_invalid_world_resets_context(root)+
         test_lock_lifetime(root)+test_owner_registry(root)+
         test_opaque_handle_boundaries(root)+
         test_existing_opener_syncs_created_lock(root)+
         test_leaf_contract_and_faults(root)+test_root_duplicate_is_cloexec(root);
  failed+=expect(teardown(root)==0,"fixture teardown must remove only exact fixture leaves");
  puts(failed?"character_save_journal_v2_writer_test: failed":"character_save_journal_v2_writer_test: ok");
  return failed?1:0;
}
