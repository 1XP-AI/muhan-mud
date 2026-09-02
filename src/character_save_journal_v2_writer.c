#include "character_save_journal_v2_writer.h"
#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
#include "character_save_journal_v2.h"
#include <signal.h>
#endif

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>

#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
static void v2_writer_crash_after(event)
character_save_journal_v2_crash_cutpoint event;
{
  const char *text=getenv("M3_V2_CRASH_CUTPOINT");
  char *end;
  unsigned long selected;
  if(!text||!*text) return;
  selected=strtoul(text,&end,10);
  if(*end||selected!=(unsigned long)event) return;
  (void)kill(getpid(),SIGKILL);
  _exit(127);
}
#else
#define v2_writer_crash_after(event) ((void)0)
#endif

#ifndef O_NOFOLLOW
#error "v2 writer lock requires O_NOFOLLOW"
#endif
#ifndef O_DIRECTORY
#error "v2 writer lock requires O_DIRECTORY"
#endif
#ifndef F_DUPFD_CLOEXEC
#error "v2 writer requires atomic F_DUPFD_CLOEXEC"
#endif

#define V2_WRITER_RUNTIME_UID 10001
#define V2_WRITER_TEXT_MAX 512

typedef struct v2_writer_private_state {
  int root_fd;
  int journal_fd;
  int lock_fd;
  character_save_journal_v2_writer_tuple tuple;
  const character_save_journal_v2_writer_context *owner_context;
  pid_t owner_pid;
  uint64_t generation;
} v2_writer_private_state;

static uid_t v2_writer_trusted_uid = V2_WRITER_RUNTIME_UID;
static v2_writer_private_state v2_writer_active_state;
static v2_writer_private_state *v2_writer_active;
static uint64_t v2_writer_generation_sequence;

#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
static int v2_writer_fail_lock_fsync;
static int v2_writer_fail_journal_fsync;
static int v2_writer_fail_epoch_create_once;
static int v2_writer_fail_temp_cleanup_fsync_once;
static int v2_writer_crash_after_instance_temp_create;
static int v2_writer_crash_after_epoch_temp_create;
static int v2_writer_fail_close_kind;
static int v2_writer_pause_ready_fd = -1;
static int v2_writer_pause_release_fd = -1;
static unsigned int v2_writer_lock_fsync_count;
static unsigned int v2_writer_journal_fsync_count;
#endif
#ifndef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
#define v2_writer_crash_after_instance_temp_create 0
#define v2_writer_crash_after_epoch_temp_create 0
#endif

static size_t v2_writer_bounded(s, limit)
const char *s;
size_t limit;
{
  size_t i;
  if(!s) {
    return limit+1;
  }
  for(i=0;i<=limit;i++) {
    if(!s[i]) {
      return i;
    }
  }
  return limit+1;
}

static int v2_writer_uuid(s)
const char *s;
{
  size_t i;
  if(!s||v2_writer_bounded(s,36)!=36) {
    return 0;
  }
  for(i=0;i<36;i++) {
    if(i==8||i==13||i==18||i==23) {
      if(s[i]!='-') {
        return 0;
      }
    } else if(!((s[i]>='0'&&s[i]<='9')||(s[i]>='a'&&s[i]<='f'))) {
      return 0;
    }
  }
  return 1;
}

static int v2_writer_world(s)
const char *s;
{
  size_t i;
  size_t n=v2_writer_bounded(s,CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX);
  if(!n||n>CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX||
     s[0]<'a'||s[0]>'z') {
    return 0;
  }
  for(i=1;i<n;i++) {
    if(!((s[i]>='a'&&s[i]<='z')||(s[i]>='0'&&s[i]<='9')||
         s[i]=='_'||s[i]=='-')) {
      return 0;
    }
  }
  return 1;
}

static int v2_writer_zero_tail(s, length, limit)
const char *s;
size_t length;
size_t limit;
{
  size_t i;
  if(!s||length>limit||s[length]) {
    return 0;
  }
  for(i=length+1;i<=limit;i++) {
    if(s[i]) {
      return 0;
    }
  }
  return 1;
}

static int v2_writer_u64(s, out)
const char *s;
uint64_t *out;
{
  uint64_t value=0;
  uint64_t digit;
  size_t i;
  size_t n=v2_writer_bounded(s,19);
  if(!out||!n||n>19||(n>1&&s[0]=='0')) {
    return -1;
  }
  for(i=0;i<n;i++) {
    if(s[i]<'0'||s[i]>'9') {
      return -1;
    }
    digit=(uint64_t)(s[i]-'0');
    if(value>((uint64_t)INT64_MAX-digit)/10) {
      return -1;
    }
    value=value*10+digit;
  }
  if(!value) {
    return -1;
  }
  *out=value;
  return 0;
}

static int v2_writer_dir_ok(fd)
int fd;
{
  struct stat st;
  return fd>=0&&fstat(fd,&st)==0&&S_ISDIR(st.st_mode)&&
         st.st_uid==v2_writer_trusted_uid&&(st.st_mode&07777)==0700;
}

static int v2_writer_file_ok(fd)
int fd;
{
  struct stat st;
  return fd>=0&&fstat(fd,&st)==0&&S_ISREG(st.st_mode)&&
         st.st_uid==v2_writer_trusted_uid&&(st.st_mode&07777)==0600&&
         st.st_nlink==1;
}

static int v2_writer_open_root(path)
const char *path;
{
  int fd;
  int next;
  const char *p;
  const char *q;
  char part[128];
  size_t n;
  if(!path||path[0]!='/') {
    return -1;
  }
  fd=open("/",O_RDONLY|O_DIRECTORY|O_CLOEXEC);
  if(fd<0) {
    return -1;
  }
  p=path+1;
  while(*p) {
    q=strchr(p,'/');
    n=q?(size_t)(q-p):strlen(p);
    if(!n||n>=sizeof(part)||(n==1&&p[0]=='.')||
       (n==2&&p[0]=='.'&&p[1]=='.')) {
      close(fd);
      return -1;
    }
    memcpy(part,p,n);
    part[n]=0;
    next=openat(fd,part,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(close(fd)!=0) {
      if(next>=0) {
        close(next);
      }
      return -1;
    }
    if(next<0) {
      return -1;
    }
    fd=next;
    p=q?q+1:p+n;
  }
  if(!v2_writer_dir_ok(fd)) {
    close(fd);
    return -1;
  }
  return fd;
}

static int v2_writer_open_component(parent, name)
int parent;
const char *name;
{
  struct stat before;
  struct stat after;
  int fd;
  if(fstatat(parent,name,&before,AT_SYMLINK_NOFOLLOW)!=0||
     !S_ISDIR(before.st_mode)||before.st_uid!=v2_writer_trusted_uid||
     (before.st_mode&07777)!=0700) {
    return -1;
  }
  fd=openat(parent,name,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
  if(!v2_writer_dir_ok(fd)||fstat(fd,&after)!=0||
     before.st_dev!=after.st_dev||before.st_ino!=after.st_ino) {
    if(fd>=0) {
      close(fd);
    }
    return -1;
  }
  return fd;
}

static int v2_writer_sync(fd, kind)
int fd,kind;
{
  int result;
  (void)kind;
#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
  if(kind==1) {
      v2_writer_lock_fsync_count++;
  } else if(kind==2) {
      v2_writer_journal_fsync_count++;
  }
  if((kind==1&&v2_writer_fail_lock_fsync)||
     (kind==2&&v2_writer_fail_journal_fsync)||
     (kind==3&&v2_writer_fail_temp_cleanup_fsync_once)) {
      if(kind==3) {
        v2_writer_fail_temp_cleanup_fsync_once=0;
      }
      errno=EIO;
      return -1;
  }
#endif
  do {
      result=fsync(fd);
  } while(result<0&&errno==EINTR);
  return result;
}

static int v2_writer_close_once(fd, kind)
int fd,kind;
{
  int result=close(fd);
  (void)kind;
#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
  if(v2_writer_fail_close_kind&&v2_writer_fail_close_kind==kind) {
      v2_writer_fail_close_kind=0;
      errno=EIO;
      return -1;
  }
#endif
  return result;
}

static void v2_writer_context_reset(context)
character_save_journal_v2_writer_context *context;
{
  memset(context,0,sizeof(*context));
}

static void v2_writer_state_reset(state)
v2_writer_private_state *state;
{
  memset(state,0,sizeof(*state));
  state->root_fd=-1;
  state->journal_fd=-1;
  state->lock_fd=-1;
}

/* Handle bytes are never read.  Only this private owner registry can grant
 * authority, so copied, zeroed, and garbage handles stay unauthoritative. */
static int v2_writer_owner_matches(context)
const character_save_journal_v2_writer_context *context;
{
  return context&&v2_writer_active&&
         context==v2_writer_active->owner_context&&
         v2_writer_active->owner_pid==getpid()&&
         v2_writer_active->generation!=0;
}

static uint64_t v2_writer_next_generation(void)
{
  if(v2_writer_generation_sequence==UINT64_MAX) {
    return 0;
  }
  v2_writer_generation_sequence++;
  return v2_writer_generation_sequence;
}

static int v2_writer_same_descriptor(parent, leaf, fd, file)
int parent;
const char *leaf;
int fd;
int file;
{
  struct stat from_parent;
  struct stat from_fd;
  if(fd<0||fstat(fd,&from_fd)!=0||
     fstatat(parent,leaf,&from_parent,AT_SYMLINK_NOFOLLOW)!=0||
     from_parent.st_dev!=from_fd.st_dev||from_parent.st_ino!=from_fd.st_ino) {
    return 0;
  }
  return file?v2_writer_file_ok(fd):v2_writer_dir_ok(fd);
}

static int v2_writer_read_leaf(journal_fd, leaf, text, text_size)
int journal_fd;
const char *leaf;
char *text;
size_t text_size;
{
  int fd;
  int result=-1;
  size_t count=0;
  ssize_t n;
  char extra;
  if(!text||text_size<2) {
    return -1;
  }
  text[0]=0;
  fd=openat(journal_fd,leaf,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
  if(!v2_writer_file_ok(fd)) {
    goto out;
  }
  while(count<text_size-1) {
    n=read(fd,text+count,text_size-1-count);
    if(n<0&&errno==EINTR) {
      continue;
    }
    if(n<0) {
      goto out;
    }
    if(n==0) {
      break;
    }
    count+=(size_t)n;
  }
  if(count==text_size-1) {
    do {
      n=read(fd,&extra,1);
    } while(n<0&&errno==EINTR);
    if(n!=0) {
      goto out;
    }
  }
  if(memchr(text,0,count)!=0) {
    goto out;
  }
  text[count]=0;
  if(v2_writer_close_once(fd,0)!=0) {
    fd=-1;
    goto out;
  }
  fd=-1;
  result=0;
out:
  if(fd>=0) {
    close(fd);
  }
  if(result!=0) {
    memset(text,0,text_size);
  }
  return result;
}

static int v2_writer_split(text, keys, count, values)
char *text;
const char *const *keys;
unsigned int count;
char **values;
{
  unsigned int i;
  char *line=text;
  char *next;
  for(i=0;i<count;i++) {
    next=strchr(line,'\n');
    if(!next||strncmp(line,keys[i],strlen(keys[i]))!=0) {
      return -1;
    }
    *next=0;
    values[i]=line+strlen(keys[i]);
    line=next+1;
  }
  return *line? -1:0;
}

static int v2_writer_load_instance(journal_fd, out)
int journal_fd;
char out[37];
{
  static const char *const keys[]={"version=","kind=","writer_instance_id="};
  char text[V2_WRITER_TEXT_MAX];
  char *values[3];
  int result=-1;
  memset(out,0,37);
  if(v2_writer_read_leaf(journal_fd,"writer-instance.v2",text,sizeof(text))!=0) {
    goto out;
  }
  if(v2_writer_split(text,keys,3,values)!=0||strcmp(values[0],"2")||
     strcmp(values[1],"writer-instance")||!v2_writer_uuid(values[2])) {
    goto out;
  }
  memcpy(out,values[2],37);
  result=0;
out:
  memset(text,0,sizeof(text));
  if(result!=0) {
    memset(out,0,37);
  }
  return result;
}

static int v2_writer_load_epoch(journal_fd, expected_world, expected_instance, epoch)
int journal_fd;
const char *expected_world;
const char *expected_instance;
uint64_t *epoch;
{
  static const char *const keys[]={"version=","kind=","world_id=","writer_instance_id=","writer_epoch="};
  char text[V2_WRITER_TEXT_MAX];
  char *values[5];
  int result=-1;
  if(!epoch) {
    return -1;
  }
  *epoch=0;
  if(v2_writer_read_leaf(journal_fd,"writer-epoch.v2",text,sizeof(text))!=0) {
    goto out;
  }
  if(v2_writer_split(text,keys,5,values)!=0||strcmp(values[0],"2")||
     strcmp(values[1],"writer-epoch")||!v2_writer_world(values[2])||
     strcmp(values[2],expected_world)||!v2_writer_uuid(values[3])||
     strcmp(values[3],expected_instance)||v2_writer_u64(values[4],epoch)!=0) {
    goto out;
  }
  result=0;
out:
  memset(text,0,sizeof(text));
  if(result!=0) {
    *epoch=0;
  }
  return result;
}

/* A deterministic same-directory temporary name makes incomplete installs
 * visible and non-authoritative.  It is never recovered or promoted later. */
static int v2_writer_temp_absent(journal_fd, leaf)
int journal_fd;
const char *leaf;
{
  struct stat st;
  if(fstatat(journal_fd,leaf,&st,AT_SYMLINK_NOFOLLOW)==0) {
    errno=EEXIST;
    return -1;
  }
  return errno==ENOENT?0:-1;
}

/* These two exact names are non-authoritative installation scratch space.
 * The lock serializes writers; unlinkat keeps every lookup beneath the held
 * journal descriptor and never follows a temporary symlink. */
static int v2_writer_remove_temp_leaf(journal_fd, leaf, removed)
int journal_fd;
const char *leaf;
int *removed;
{
  if(!removed) {
    return -1;
  }
  if(unlinkat(journal_fd,leaf,0)==0) {
    *removed=1;
    return 0;
  }
  return errno==ENOENT?0:-1;
}

static int v2_writer_cleanup_temps(journal_fd)
int journal_fd;
{
  int removed=0;
  if(v2_writer_remove_temp_leaf(journal_fd,".writer-instance.v2.tmp",
                                &removed)!=0||
     v2_writer_remove_temp_leaf(journal_fd,".writer-epoch.v2.tmp",
                                &removed)!=0) {
    return -1;
  }
  return removed?v2_writer_sync(journal_fd,3):0;
}

static int v2_writer_load_instance_optional(journal_fd, out, present)
int journal_fd;
char out[37];
int *present;
{
  struct stat st;
  if(!out||!present) {
    return -1;
  }
  memset(out,0,37);
  *present=0;
  if(fstatat(journal_fd,"writer-instance.v2",&st,AT_SYMLINK_NOFOLLOW)!=0) {
    return errno==ENOENT?0:-1;
  }
  if(!S_ISREG(st.st_mode)||st.st_uid!=v2_writer_trusted_uid||
     (st.st_mode&07777)!=0600||st.st_nlink!=1) {
    return -1;
  }
  if(v2_writer_load_instance(journal_fd,out)!=0) {
    return -1;
  }
  *present=1;
  return 0;
}

static int v2_writer_load_epoch_optional(journal_fd, expected_world,
                                         expected_instance, epoch, present)
int journal_fd;
const char *expected_world;
const char *expected_instance;
uint64_t *epoch;
int *present;
{
  struct stat st;
  if(!epoch||!present) {
    return -1;
  }
  *epoch=0;
  *present=0;
  if(fstatat(journal_fd,"writer-epoch.v2",&st,AT_SYMLINK_NOFOLLOW)!=0) {
    return errno==ENOENT?0:-1;
  }
  if(!S_ISREG(st.st_mode)||st.st_uid!=v2_writer_trusted_uid||
     (st.st_mode&07777)!=0600||st.st_nlink!=1) {
    return -1;
  }
  if(v2_writer_load_epoch(journal_fd,expected_world,expected_instance,epoch)!=0) {
    return -1;
  }
  *present=1;
  return 0;
}

static int v2_writer_write_all(fd, bytes, length)
int fd;
const void *bytes;
size_t length;
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

/* linkat is the portable no-replace atomic install: it cannot overwrite an
 * existing authority leaf, unlike rename(2).  The temp link is removed only
 * after the final name is present. */
static int v2_writer_install_new_leaf(journal_fd, leaf, temp, bytes, length,
                                      crash_after_temp_create)
int journal_fd;
const char *leaf;
const char *temp;
const char *bytes;
size_t length;
int crash_after_temp_create;
{
  struct stat st;
  int fd=-1;
  int installed=0;
  int result=-1;
  if(v2_writer_temp_absent(journal_fd,temp)!=0||
     fstatat(journal_fd,leaf,&st,AT_SYMLINK_NOFOLLOW)==0||
     errno!=ENOENT) {
    return -1;
  }
  fd=openat(journal_fd,temp,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC,0600);
  if(fd<0||fchmod(fd,0600)!=0||!v2_writer_file_ok(fd)||
     v2_writer_write_all(fd,bytes,length)!=0||v2_writer_sync(fd,0)!=0) {
    goto out;
  }
  if(v2_writer_close_once(fd,0)!=0) {
    fd=-1;
    goto out;
  }
  fd=-1;
#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
  if(crash_after_temp_create) {
    (void)kill(getpid(),SIGKILL);
    _exit(127);
  }
#else
  (void)crash_after_temp_create;
#endif
  if(linkat(journal_fd,temp,journal_fd,leaf,0)!=0) {
    goto out;
  }
  installed=1;
  if(unlinkat(journal_fd,temp,0)!=0||v2_writer_sync(journal_fd,2)!=0) {
    goto out;
  }
  result=0;
out:
  if(fd>=0) {
    close(fd);
  }
  if(result!=0&&!installed) {
    (void)unlinkat(journal_fd,temp,0);
  }
  return result;
}

static int v2_writer_create_instance(journal_fd, instance)
int journal_fd;
const char *instance;
{
  char text[V2_WRITER_TEXT_MAX];
  int n;
  n=snprintf(text,sizeof(text),"version=2\nkind=writer-instance\nwriter_instance_id=%s\n",
             instance);
  if(n<0||(size_t)n>=sizeof(text)) {
    return -1;
  }
  return v2_writer_install_new_leaf(journal_fd,"writer-instance.v2",
                                    ".writer-instance.v2.tmp",text,(size_t)n,
                                    v2_writer_crash_after_instance_temp_create);
}

static int v2_writer_create_epoch(journal_fd, world, instance, epoch)
int journal_fd;
const char *world;
const char *instance;
uint64_t epoch;
{
  char text[V2_WRITER_TEXT_MAX];
  int n;
#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
  if(v2_writer_fail_epoch_create_once) {
    v2_writer_fail_epoch_create_once=0;
    errno=EIO;
    return -1;
  }
#endif
  n=snprintf(text,sizeof(text),"version=2\nkind=writer-epoch\nworld_id=%s\nwriter_instance_id=%s\nwriter_epoch=%" PRIu64 "\n",
             world,instance,epoch);
  if(n<0||(size_t)n>=sizeof(text)) {
    return -1;
  }
  return v2_writer_install_new_leaf(journal_fd,"writer-epoch.v2",
                                    ".writer-epoch.v2.tmp",text,(size_t)n,
                                    v2_writer_crash_after_epoch_temp_create);
}

static int v2_writer_granted_tuple_matches(request, granted, existing_epoch)
const character_save_journal_v2_writer_tuple *request;
const character_save_journal_v2_writer_tuple *granted;
int existing_epoch;
{
  size_t world_length;
  if(!request||!granted||!v2_writer_world(granted->world_id)||
     !v2_writer_uuid(granted->writer_instance_id)||!granted->writer_epoch||
     granted->writer_epoch>(uint64_t)INT64_MAX||
     strcmp(granted->world_id,request->world_id)!=0||
     strcmp(granted->writer_instance_id,request->writer_instance_id)!=0) {
    return 0;
  }
  world_length=v2_writer_bounded(granted->world_id,
                                 CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX);
  if(!v2_writer_zero_tail(granted->world_id,world_length,
                          CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX)) {
    return 0;
  }
  return !existing_epoch||granted->writer_epoch==request->writer_epoch;
}

static int v2_writer_open_lock(journal_fd, lock_out)
int journal_fd;
int *lock_out;
{
  int fd,created=0;
  if(!lock_out) {
    return -1;
  }
  *lock_out=-1;
  fd=openat(journal_fd,".m3-writer.lock",O_RDWR|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
  if(fd<0&&errno==ENOENT) {
      fd=openat(journal_fd,".m3-writer.lock",
                O_RDWR|O_CREAT|O_EXCL|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC,0600);
      if(fd>=0) {
          created=1;
      } else if(errno==EEXIST) {
          fd=openat(journal_fd,".m3-writer.lock",
                    O_RDWR|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
      }
  }
  if(!v2_writer_file_ok(fd)) {
      if(fd>=0) {
        close(fd);
      }
      return -1;
  }
  (void)created;
#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
  if(created && v2_writer_pause_ready_fd>=0) {
      char signal;
      ssize_t count;
      do {
          count=write(v2_writer_pause_ready_fd,"x",1);
      } while(count<0&&errno==EINTR);
      if(count!=1) {
          close(fd);
          return -1;
      }
      do {
          count=read(v2_writer_pause_release_fd,&signal,1);
      } while(count<0&&errno==EINTR);
      v2_writer_pause_ready_fd=-1;
      v2_writer_pause_release_fd=-1;
      if(count!=1) {
          close(fd);
          return -1;
      }
  }
#endif
  if(flock(fd,LOCK_EX|LOCK_NB)!=0) {
      close(fd);
      return -1;
  }
  if(v2_writer_sync(fd,1)!=0) {
      close(fd);
      return -1;
  }
  v2_writer_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_LOCK_FILE_FSYNC);
  if(v2_writer_sync(journal_fd,2)!=0) {
      close(fd);
      return -1;
  }
  v2_writer_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_WRITER_JOURNAL_DIR_FSYNC);
  *lock_out=fd;
  return 0;
}

int character_save_journal_v2_writer_open(root, world_id, out)
const char *root;
const char *world_id;
character_save_journal_v2_writer_context *out;
{
  v2_writer_private_state next;
  if(!out) {
    return -1;
  }
  if(v2_writer_active) {
    /* Never erase the active owner through a second open request. */
    if(out!=v2_writer_active->owner_context) {
      v2_writer_context_reset(out);
    }
    errno=EBUSY;
    return -1;
  }
  v2_writer_context_reset(out);
  if(!v2_writer_world(world_id)) {
    return -1;
  }
  v2_writer_state_reset(&next);
  next.root_fd=v2_writer_open_root(root);
  if(next.root_fd<0) {
    goto bad;
  }
  next.journal_fd=v2_writer_open_component(next.root_fd,"character-save-journal");
  if(next.journal_fd<0) {
    goto bad;
  }
  if(v2_writer_open_lock(next.journal_fd,&next.lock_fd)!=0) {
    goto bad;
  }
  if(v2_writer_load_instance(next.journal_fd,next.tuple.writer_instance_id)!=0||
     v2_writer_load_epoch(next.journal_fd,world_id,next.tuple.writer_instance_id,
                          &next.tuple.writer_epoch)!=0) {
    goto bad;
  }
  strcpy(next.tuple.world_id,world_id);
  next.generation=v2_writer_next_generation();
  if(!next.generation) {
    errno=EOVERFLOW;
    goto bad;
  }
  v2_writer_active_state=next;
  v2_writer_active_state.owner_context=out;
  v2_writer_active_state.owner_pid=getpid();
  v2_writer_active=&v2_writer_active_state;
  v2_writer_state_reset(&next);
  return 0;
bad:
  if(next.lock_fd>=0) {
    close(next.lock_fd);
  }
  if(next.journal_fd>=0) {
    close(next.journal_fd);
  }
  if(next.root_fd>=0) {
    close(next.root_fd);
  }
  v2_writer_state_reset(&next);
  v2_writer_context_reset(out);
  return -1;
}

int character_save_journal_v2_writer_bootstrap(root, world_id,
                                                 writer_instance_candidate,
                                                 acquire, acquire_argument, out)
const char *root;
const char *world_id;
const char *writer_instance_candidate;
character_save_journal_v2_writer_epoch_acquire acquire;
void *acquire_argument;
character_save_journal_v2_writer_context *out;
{
  v2_writer_private_state next;
  character_save_journal_v2_writer_tuple request;
  character_save_journal_v2_writer_tuple granted;
  int instance_present;
  int epoch_present;
  int acquired;
  if(!out) {
    return -1;
  }
  if(v2_writer_active) {
    if(out!=v2_writer_active->owner_context) {
      v2_writer_context_reset(out);
    }
    errno=EBUSY;
    return -1;
  }
  v2_writer_context_reset(out);
  if(!v2_writer_world(world_id)||!v2_writer_uuid(writer_instance_candidate)||
     !acquire) {
    return -1;
  }
  v2_writer_state_reset(&next);
  next.root_fd=v2_writer_open_root(root);
  if(next.root_fd<0) {
    goto bad;
  }
  next.journal_fd=v2_writer_open_component(next.root_fd,"character-save-journal");
  if(next.journal_fd<0||v2_writer_open_lock(next.journal_fd,&next.lock_fd)!=0) {
    goto bad;
  }
  if(v2_writer_cleanup_temps(next.journal_fd)!=0||
     v2_writer_load_instance_optional(next.journal_fd,
                                      next.tuple.writer_instance_id,
                                      &instance_present)!=0) {
    goto bad;
  }
  if(!instance_present) {
    memcpy(next.tuple.writer_instance_id,writer_instance_candidate,
           CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN+1);
  }
  strcpy(next.tuple.world_id,world_id);
  if(v2_writer_load_epoch_optional(next.journal_fd,next.tuple.world_id,
                                   next.tuple.writer_instance_id,
                                   &next.tuple.writer_epoch,
                                   &epoch_present)!=0) {
    goto bad;
  }
  /* An epoch cannot exist without the instance that names it.  Check the
   * complete authority state before creating anything or calling the DB. */
  if(!instance_present&&epoch_present) {
    errno=EINVAL;
    goto bad;
  }
  if(!instance_present) {
    if(v2_writer_create_instance(next.journal_fd,writer_instance_candidate)!=0) {
      goto bad;
    }
  }
  request=next.tuple;
  memset(&granted,0,sizeof(granted));
  acquired=acquire(acquire_argument,&request,&granted);
  if(acquired!=0||!v2_writer_granted_tuple_matches(&request,&granted,
                                                    epoch_present)) {
    errno=EINVAL;
    goto bad;
  }
  if(!epoch_present) {
    if(v2_writer_create_epoch(next.journal_fd,request.world_id,
                              request.writer_instance_id,
                              granted.writer_epoch)!=0) {
      goto bad;
    }
  }
  next.tuple=granted;
  next.generation=v2_writer_next_generation();
  if(!next.generation) {
    errno=EOVERFLOW;
    goto bad;
  }
  v2_writer_active_state=next;
  v2_writer_active_state.owner_context=out;
  v2_writer_active_state.owner_pid=getpid();
  v2_writer_active=&v2_writer_active_state;
  memset(&request,0,sizeof(request));
  memset(&granted,0,sizeof(granted));
  v2_writer_state_reset(&next);
  return 0;
bad:
  memset(&request,0,sizeof(request));
  memset(&granted,0,sizeof(granted));
  if(next.lock_fd>=0) {
    close(next.lock_fd);
  }
  if(next.journal_fd>=0) {
    close(next.journal_fd);
  }
  if(next.root_fd>=0) {
    close(next.root_fd);
  }
  v2_writer_state_reset(&next);
  v2_writer_context_reset(out);
  return -1;
}

static character_save_journal_v2_writer_context_status
v2_writer_validate_snapshot(context,tuple_out,generation_out)
const character_save_journal_v2_writer_context *context;
character_save_journal_v2_writer_tuple *tuple_out;
uint64_t *generation_out;
{
  v2_writer_private_state *state;
  character_save_journal_v2_writer_tuple next;
  char instance[CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN + 1];
  uint64_t epoch;
  int lock_result;
  if(!tuple_out||!v2_writer_owner_matches(context)) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
  }
  state=v2_writer_active;
  if(!v2_writer_world(state->tuple.world_id)||
     !v2_writer_zero_tail(state->tuple.world_id,
                          v2_writer_bounded(state->tuple.world_id,
                                            CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX),
                          CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX)||
     !v2_writer_uuid(state->tuple.writer_instance_id)||!state->tuple.writer_epoch||
     state->tuple.writer_epoch>(uint64_t)INT64_MAX||!state->generation) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
  }
  if(!v2_writer_dir_ok(state->root_fd)||
     !v2_writer_same_descriptor(state->root_fd,"character-save-journal",
                                state->journal_fd,0)) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
  }
  if(!v2_writer_same_descriptor(state->journal_fd,".m3-writer.lock",
                                state->lock_fd,1)) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK;
  }
  do {
    lock_result=flock(state->lock_fd,LOCK_EX|LOCK_NB);
  } while(lock_result<0&&errno==EINTR);
  if(lock_result!=0) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK;
  }
  if(v2_writer_load_instance(state->journal_fd,instance)!=0||
     strcmp(instance,state->tuple.writer_instance_id)!=0||
     v2_writer_load_epoch(state->journal_fd,state->tuple.world_id,instance,&epoch)!=0||
     epoch!=state->tuple.writer_epoch) {
    memset(instance,0,sizeof(instance));
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE;
  }
  memset(&next,0,sizeof(next));
  memcpy(next.world_id,state->tuple.world_id,sizeof(next.world_id));
  memcpy(next.writer_instance_id,state->tuple.writer_instance_id,
         sizeof(next.writer_instance_id));
  next.writer_epoch=state->tuple.writer_epoch;
  *tuple_out=next;
  if(generation_out) {
    *generation_out=state->generation;
  }
  memset(&next,0,sizeof(next));
  memset(instance,0,sizeof(instance));
  return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK;
}

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held(context,tuple_out)
const character_save_journal_v2_writer_context *context;
character_save_journal_v2_writer_tuple *tuple_out;
{
  return v2_writer_validate_snapshot(context,tuple_out,0);
}

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_dup_held_root_fd(context,root_fd_out)
const character_save_journal_v2_writer_context *context;
int *root_fd_out;
{
  character_save_journal_v2_writer_tuple tuple;
  character_save_journal_v2_writer_context_status status;
  int duplicate;
  if(!root_fd_out) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
  }
  *root_fd_out=-1;
  status=v2_writer_validate_snapshot(context,&tuple,0);
  memset(&tuple,0,sizeof(tuple));
  if(status!=CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
    return status;
  }
  /* A dup followed by F_SETFD leaks this descriptor across a concurrent exec.
   * Both supported targets provide the atomic form; do not add a racy fallback. */
  duplicate=fcntl(v2_writer_active->root_fd,F_DUPFD_CLOEXEC,0);
  if(duplicate<0) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
  }
  *root_fd_out=duplicate;
  return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK;
}

/* This link-local seam is intentionally absent from the public header.  The
 * route uses it only to compare a callback-held private generation; callers
 * cannot supply a generation to gain authority. */
character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held_for_route(context,tuple_out,
                                                           generation_out)
const character_save_journal_v2_writer_context *context;
character_save_journal_v2_writer_tuple *tuple_out;
uint64_t *generation_out;
{
  if(!generation_out) {
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
  }
  return v2_writer_validate_snapshot(context,tuple_out,generation_out);
}

int character_save_journal_v2_writer_close(context)
character_save_journal_v2_writer_context *context;
{
  v2_writer_private_state *state;
  int result=0;
  if(!v2_writer_owner_matches(context)) {
    return -1;
  }
  state=v2_writer_active;
  if(state->lock_fd>=0&&v2_writer_close_once(state->lock_fd,1)!=0) {
      result=-1;
  }
  state->lock_fd=-1;
  if(state->journal_fd>=0&&v2_writer_close_once(state->journal_fd,2)!=0) {
      result=-1;
  }
  state->journal_fd=-1;
  if(state->root_fd>=0&&v2_writer_close_once(state->root_fd,3)!=0) {
      result=-1;
  }
  state->root_fd=-1;
  v2_writer_context_reset(context);
  v2_writer_active=0;
  v2_writer_state_reset(state);
  return result;
}

#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
void character_save_journal_v2_writer_set_trusted_uid_for_test(uid_t uid)
{
  v2_writer_trusted_uid=uid;
}
void character_save_journal_v2_writer_fail_fsync_for_test(lock_file,journal_dir)
int lock_file,journal_dir;
{
  v2_writer_fail_lock_fsync=lock_file!=0;
  v2_writer_fail_journal_fsync=journal_dir!=0;
}
void character_save_journal_v2_writer_fail_epoch_create_once_for_test(void)
{
  v2_writer_fail_epoch_create_once=1;
}
void character_save_journal_v2_writer_fail_temp_cleanup_fsync_once_for_test(void)
{
  v2_writer_fail_temp_cleanup_fsync_once=1;
}
void character_save_journal_v2_writer_crash_after_temp_create_for_test(instance_temp,
                                                                         epoch_temp)
int instance_temp,epoch_temp;
{
  v2_writer_crash_after_instance_temp_create=instance_temp!=0;
  v2_writer_crash_after_epoch_temp_create=epoch_temp!=0;
}
void character_save_journal_v2_writer_fail_close_once_for_test(close_kind)
int close_kind;
{
  v2_writer_fail_close_kind=close_kind;
}
void character_save_journal_v2_writer_pause_after_create_for_test(ready_fd,release_fd)
int ready_fd,release_fd;
{
  v2_writer_pause_ready_fd=ready_fd;
  v2_writer_pause_release_fd=release_fd;
}
void character_save_journal_v2_writer_reset_fsync_counts_for_test(void)
{
  v2_writer_lock_fsync_count=0;
  v2_writer_journal_fsync_count=0;
}
void character_save_journal_v2_writer_fsync_counts_for_test(lock_file,journal_dir)
unsigned int *lock_file,*journal_dir;
{
  if(lock_file) {
    *lock_file=v2_writer_lock_fsync_count;
  }
  if(journal_dir) {
    *journal_dir=v2_writer_journal_fsync_count;
  }
}
#endif
