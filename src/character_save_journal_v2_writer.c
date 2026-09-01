#include "character_save_journal_v2_writer.h"

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "v2 writer lock requires O_NOFOLLOW"
#endif
#ifndef O_DIRECTORY
#error "v2 writer lock requires O_DIRECTORY"
#endif

#define V2_WRITER_RUNTIME_UID 10001
#define V2_WRITER_TEXT_MAX 512

static uid_t v2_writer_trusted_uid = V2_WRITER_RUNTIME_UID;

#ifdef CHARACTER_SAVE_JOURNAL_V2_WRITER_TESTING
static int v2_writer_fail_lock_fsync;
static int v2_writer_fail_journal_fsync;
static int v2_writer_fail_close_kind;
static int v2_writer_pause_ready_fd = -1;
static int v2_writer_pause_release_fd = -1;
static unsigned int v2_writer_lock_fsync_count;
static unsigned int v2_writer_journal_fsync_count;
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
     (kind==2&&v2_writer_fail_journal_fsync)) {
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
  context->root_fd=-1;
  context->journal_fd=-1;
  context->lock_fd=-1;
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
  if(v2_writer_sync(fd,1)!=0||v2_writer_sync(journal_fd,2)!=0) {
      close(fd);
      return -1;
  }
  *lock_out=fd;
  return 0;
}

int character_save_journal_v2_writer_open(root, world_id, out)
const char *root;
const char *world_id;
character_save_journal_v2_writer_context *out;
{
  character_save_journal_v2_writer_context next;
  if(!out) {
    return -1;
  }
  v2_writer_context_reset(out);
  if(!v2_writer_world(world_id)) {
    return -1;
  }
  v2_writer_context_reset(&next);
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
  if(v2_writer_load_instance(next.journal_fd,next.writer_instance_id)!=0||
     v2_writer_load_epoch(next.journal_fd,world_id,next.writer_instance_id,
                          &next.writer_epoch)!=0) {
    goto bad;
  }
  strcpy(next.world_id,world_id);
  *out=next;
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
  v2_writer_context_reset(out);
  return -1;
}

int character_save_journal_v2_writer_close(context)
character_save_journal_v2_writer_context *context;
{
  int result=0;
  if(!context) {
    return -1;
  }
  if(context->lock_fd>=0&&v2_writer_close_once(context->lock_fd,1)!=0) {
      result=-1;
  }
  context->lock_fd=-1;
  if(context->journal_fd>=0&&v2_writer_close_once(context->journal_fd,2)!=0) {
      result=-1;
  }
  context->journal_fd=-1;
  if(context->root_fd>=0&&v2_writer_close_once(context->root_fd,3)!=0) {
      result=-1;
  }
  context->root_fd=-1;
  memset(context->world_id,0,sizeof(context->world_id));
  memset(context->writer_instance_id,0,sizeof(context->writer_instance_id));
  context->writer_epoch=0;
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
