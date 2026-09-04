#include "character_player_snapshot_v1_handoff.h"

#include "character_save_journal_v2.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#ifndef O_BINARY
#define O_BINARY 0
#endif
#ifndef O_CLOEXEC
#error "PlayerSnapshotV1 handoff requires O_CLOEXEC"
#endif
#ifndef O_DIRECTORY
#error "PlayerSnapshotV1 handoff requires O_DIRECTORY"
#endif
#ifndef O_NOFOLLOW
#error "PlayerSnapshotV1 handoff requires O_NOFOLLOW"
#endif
#ifndef AT_SYMLINK_NOFOLLOW
#error "PlayerSnapshotV1 handoff requires AT_SYMLINK_NOFOLLOW"
#endif

#define CPSH_RECORD_MAX 256U
#define CPSH_SUFFIX ".handoff"
#define CPSH_TEMP_SUFFIX ".handoff.tmp"
#define CPSH_SOURCE_SUFFIX ".source"
#define CPSH_CONSUMED_SUFFIX ".source.consumed"
#define CPSH_CONSUMED_TEMP_SUFFIX ".source.consumed.tmp"
#define CPSH_POISON_SUFFIX ".poison"
#define CPSH_NAME_MAX 64U
#define CPSH_FAULT_WRITE 1
#define CPSH_FAULT_FILE_FSYNC 2
#define CPSH_FAULT_LINK 3
#define CPSH_FAULT_UNLINK 4
#define CPSH_FAULT_DIRECTORY_FSYNC 5
#define CPSH_FAULT_SOURCE_DIRECTORY_FSYNC 6
#define CPSH_FAULT_QUEUE_CREATE_FSYNC 7
#define CPSH_FAULT_SOURCE_RENAME 8

/* cpsh_source_valid has one recoverable-invalid state: its fixed private
 * temporary name is local, but a copy interrupted before its digest matched.
 * That is not evidence of corruption in the V2 command or a captured image. */
#define CPSH_SOURCE_OK 0
#define CPSH_SOURCE_ABSENT 1
#define CPSH_SOURCE_PARTIAL_TEMP 2

typedef struct cpsh_record {
    char command_uuid[37];
    char request_sha256[65];
    char writer_instance_id[37];
    uint64_t writer_epoch;
} cpsh_record;

#ifdef CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_TESTING
static int cpsh_test_fault;
void character_player_snapshot_v1_handoff_test_fail_next(fault)
int fault;
{ cpsh_test_fault=fault; }
void character_player_snapshot_v1_handoff_test_reset_faults(void)
{ cpsh_test_fault=0; }
static int cpsh_fault(fault)
int fault;
{
    if(cpsh_test_fault!=fault)return 0;
    cpsh_test_fault=0;errno=EIO;return 1;
}
#else
static int cpsh_fault(fault)
int fault;
{ (void)fault;return 0; }
#endif

static void cpsh_increment(value)
uint64_t *value;
{ if(value&&*value<UINT64_MAX)(*value)++; }

static int cpsh_close(fd)
int fd;
{ return close(fd); }

static int cpsh_sync(fd)
int fd;
{
    int result;
    do result=fsync(fd);while(result<0&&errno==EINTR);
    return result;
}

static int cpsh_fsync_kind(fd,fault)
int fd,fault;
{
    if(cpsh_fault(fault))return -1;
    return cpsh_sync(fd);
}

static int cpsh_fsync(fd,directory)
int fd,directory;
{ return cpsh_fsync_kind(fd,directory ? CPSH_FAULT_DIRECTORY_FSYNC :
    CPSH_FAULT_FILE_FSYNC); }

static int cpsh_write_all(fd,bytes,length)
int fd;const char *bytes;size_t length;
{
    ssize_t count;
    while(length) {
        if(cpsh_fault(CPSH_FAULT_WRITE))
            return -1;
        count=write(fd,bytes,length);
        if(count<0&&errno==EINTR)continue;
        if(count<=0)return -1;
        bytes+=count;length-=(size_t)count;
    }
    return 0;
}

static int cpsh_hex(value)
char value;
{ return (value>='0'&&value<='9')||(value>='a'&&value<='f'); }

static unsigned long cpsh_bounded(value,maximum)
const char *value;unsigned long maximum;
{
    unsigned long index;
    if(!value)return maximum+1U;
    for(index=0;index<=maximum;index++)if(!value[index])return index;
    return maximum+1U;
}

static int cpsh_uuid(value)
const char *value;
{
    unsigned long index;
    if(cpsh_bounded(value,36U)!=36U)return 0;
    for(index=0;index<36U;index++) {
        if(index==8U||index==13U||index==18U||index==23U) {
            if(value[index]!='-')return 0;
        } else if(!cpsh_hex(value[index]))return 0;
    }
    return 1;
}

static int cpsh_hash(value)
const char *value;
{
    unsigned long index;
    if(cpsh_bounded(value,64U)!=64U)return 0;
    for(index=0;index<64U;index++)if(!cpsh_hex(value[index]))return 0;
    return 1;
}

static int cpsh_record_valid(record)
const cpsh_record *record;
{
    return record&&cpsh_uuid(record->command_uuid)&&
        cpsh_hash(record->request_sha256)&&
        cpsh_uuid(record->writer_instance_id)&&record->writer_epoch&&
        record->writer_epoch<=(uint64_t)INT64_MAX;
}

static int cpsh_text_copy(destination,capacity,source,length)
char *destination;size_t capacity;const char *source;size_t length;
{
    if(!destination||!capacity||!source||length>=capacity)return -1;
    memcpy(destination,source,length);destination[length]=0;return 0;
}

static int cpsh_number(value,length,output)
const char *value;size_t length;uint64_t *output;
{
    uint64_t number;size_t index;
    if(!value||!output||!length||length>19U||value[0]=='0')return -1;
    number=0;
    for(index=0;index<length;index++) {
        if(value[index]<'0'||value[index]>'9'||
           number>((uint64_t)INT64_MAX-(uint64_t)(value[index]-'0'))/10U)
            return -1;
        number=number*10U+(uint64_t)(value[index]-'0');
    }
    if(!number)return -1;
    *output=number;return 0;
}

static int cpsh_line(cursor,remaining,name,value,length)
const char **cursor;size_t *remaining;const char *name;
const char **value;size_t *length;
{
    const char *newline;size_t prefix,used;
    if(!cursor||!remaining||!name||!value||!length)return -1;
    prefix=strlen(name);
    if(*remaining<prefix+2U||memcmp(*cursor,name,prefix)||
       (*cursor)[prefix]!='=')return -1;
    newline=(const char *)memchr(*cursor+prefix+1U,'\n',
       *remaining-prefix-1U);
    if(!newline)return -1;
    *value=*cursor+prefix+1U;*length=(size_t)(newline-*value);
    used=(size_t)(newline-*cursor)+1U;
    *cursor=newline+1U;*remaining-=used;return 0;
}

static int cpsh_encode(output,capacity,record)
char *output;size_t capacity;const cpsh_record *record;
{
    int length;
    if(!output||!capacity||!cpsh_record_valid(record))return -1;
    length=snprintf(output,capacity,
        "version=1\ncommand_uuid=%s\nrequest_sha256=%s\n"
        "writer_instance_id=%s\nwriter_epoch=%llu\n",
        record->command_uuid,record->request_sha256,
        record->writer_instance_id,(unsigned long long)record->writer_epoch);
    return length<0||(size_t)length>=capacity ? -1:length;
}

static int cpsh_parse(bytes,length,record)
const char *bytes;size_t length;cpsh_record *record;
{
    static const char *names[]={"version","command_uuid","request_sha256",
        "writer_instance_id","writer_epoch"};
    const char *cursor,*value;size_t remaining,value_length,index;
    char canonical[CPSH_RECORD_MAX];int canonical_length;
    if(!bytes||!record||!length||length>=CPSH_RECORD_MAX||
       bytes[length-1U]!='\n'||memchr(bytes,0,length))return -1;
    memset(record,0,sizeof(*record));cursor=bytes;remaining=length;
    for(index=0;index<5U;index++) {
        if(cpsh_line(&cursor,&remaining,names[index],&value,&value_length))return -1;
        if(index==0U) { if(value_length!=1U||value[0]!='1')return -1; }
        else if(index==1U) { if(cpsh_text_copy(record->command_uuid,
            sizeof(record->command_uuid),value,value_length))return -1; }
        else if(index==2U) { if(cpsh_text_copy(record->request_sha256,
            sizeof(record->request_sha256),value,value_length))return -1; }
        else if(index==3U) { if(cpsh_text_copy(record->writer_instance_id,
            sizeof(record->writer_instance_id),value,value_length))return -1; }
        else if(cpsh_number(value,value_length,&record->writer_epoch))return -1;
    }
    if(remaining||!cpsh_record_valid(record))return -1;
    canonical_length=cpsh_encode(canonical,sizeof(canonical),record);
    return canonical_length<0||(size_t)canonical_length!=length||
        memcmp(canonical,bytes,length) ? -1:0;
}

static int cpsh_equal(left,right)
const cpsh_record *left,*right;
{
    return left&&right&&!strcmp(left->command_uuid,right->command_uuid)&&
        !strcmp(left->request_sha256,right->request_sha256)&&
        !strcmp(left->writer_instance_id,right->writer_instance_id)&&
        left->writer_epoch==right->writer_epoch;
}

static int cpsh_names(record,name,temp,source,source_temp,consumed)
const cpsh_record *record;char name[CPSH_NAME_MAX];char temp[CPSH_NAME_MAX];
char source[CPSH_NAME_MAX];char source_temp[CPSH_NAME_MAX];
char consumed[CPSH_NAME_MAX];
{
    int first,second,third,fourth,fifth;
    if(!record||!name||!temp||!source||!source_temp||!consumed||
       !cpsh_record_valid(record))return -1;
    first=snprintf(name,CPSH_NAME_MAX,"%s%s",record->command_uuid,CPSH_SUFFIX);
    second=snprintf(temp,CPSH_NAME_MAX,"%s%s",record->command_uuid,CPSH_TEMP_SUFFIX);
    third=snprintf(source,CPSH_NAME_MAX,"%s%s",record->command_uuid,CPSH_SOURCE_SUFFIX);
    fourth=snprintf(source_temp,CPSH_NAME_MAX,"%s%s",record->command_uuid,
        CPSH_SOURCE_SUFFIX ".tmp");
    fifth=snprintf(consumed,CPSH_NAME_MAX,"%s%s",record->command_uuid,CPSH_CONSUMED_SUFFIX);
    return first!=44||second!=48||third!=43||fourth!=47||fifth!=52 ? -1:0;
}

static int cpsh_consumed_temp_name(record,consumed_temp)
const cpsh_record *record;char consumed_temp[CPSH_NAME_MAX];
{
    int length;
    if(!record||!consumed_temp||!cpsh_record_valid(record))return -1;
    length=snprintf(consumed_temp,CPSH_NAME_MAX,"%s%s",record->command_uuid,
        CPSH_CONSUMED_TEMP_SUFFIX);
    return length!=56 ? -1:0;
}

static int cpsh_root_safe(fd,uid_out)
int fd;uid_t *uid_out;
{
    struct stat status;
    if(fd<0||fstat(fd,&status)||!S_ISDIR(status.st_mode)||
       (status.st_mode&07777)!=0700)return 0;
    if(uid_out)*uid_out=status.st_uid;
    return 1;
}

static int cpsh_queue_safe(fd,uid)
int fd;uid_t uid;
{
    struct stat status;
    return fd>=0&&!fstat(fd,&status)&&S_ISDIR(status.st_mode)&&
        status.st_uid==uid&&(status.st_mode&07777)==0700;
}

static int cpsh_file_safe(fd,directory,before)
int fd;const struct stat *directory;struct stat *before;
{
    return fd>=0&&directory&&before&&!fstat(fd,before)&&
        S_ISREG(before->st_mode)&&before->st_uid==directory->st_uid&&
        (before->st_mode&07777)==0600&&before->st_nlink==1&&
        before->st_size>0&&(uint64_t)before->st_size<CPSH_RECORD_MAX;
}

static int cpsh_new_file_safe(fd,directory,before)
int fd;const struct stat *directory;struct stat *before;
{
    return fd>=0&&directory&&before&&!fstat(fd,before)&&
        S_ISREG(before->st_mode)&&before->st_uid==directory->st_uid&&
        (before->st_mode&07777)==0600&&before->st_nlink==1&&
        before->st_size==0;
}

/* A retained source is a private copied file. Rename-only promotion makes
 * its consumer-visible final have exactly one name, and it is never linked
 * to the legacy save stage. */
static int cpsh_source_file_safe(fd,directory,before)
int fd;const struct stat *directory;struct stat *before;
{
    return fd>=0&&directory&&before&&!fstat(fd,before)&&
        S_ISREG(before->st_mode)&&before->st_uid==directory->st_uid&&
        (before->st_mode&07777)==0600&&before->st_nlink==1&&
        before->st_size>0&&
        (uint64_t)before->st_size<=CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES;
}

/* A short temporary can be retried or dropped only when it is demonstrably a
 * private queue leaf.  An external link, bad mode, or oversized temp is not a
 * partial copy and must instead take the unsafe-poison path. */
static int cpsh_source_temp_local(fd,directory,before)
int fd;const struct stat *directory;struct stat *before;
{
    return fd>=0&&directory&&before&&!fstat(fd,before)&&
        S_ISREG(before->st_mode)&&before->st_uid==directory->st_uid&&
        (before->st_mode&07777)==0600&&before->st_nlink==1&&
        before->st_size>=0&&
        (uint64_t)before->st_size<=CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES;
}

/* Fixed handoff temporary leaves are created only by this queue.  A malformed
 * one is reclaimable only when it still proves to be a private, regular,
 * one-linked file owned by the trusted queue uid; symlinks, directories,
 * hardlinks, and oversized files remain untouched and are marked as poison. */
static int cpsh_reclaim_local_temp(queue,name,maximum,fault)
int queue;const char *name;uint64_t maximum;int fault;
{
    struct stat directory,status;
    if(!name||fstat(queue,&directory))return -1;
    if(fstatat(queue,name,&status,AT_SYMLINK_NOFOLLOW))
        return errno==ENOENT ? 0:-1;
    if(!S_ISREG(status.st_mode)||status.st_uid!=directory.st_uid||
       (status.st_mode&07777)!=0600||status.st_nlink!=1||
       status.st_size<0||(uint64_t)status.st_size>maximum)return -1;
    if(unlinkat(queue,name,0)||cpsh_fsync_kind(queue,fault))return -1;
    return 0;
}

/* Preserve inspectable local poison without leaving it in the active queue.
 * The destination is a fixed ignored suffix and rename is followed by a queue
 * fsync; only a regular one-linked queue-owned leaf may be moved there.
 * Return one for an unsafe leaf so the identity can be durably marked without
 * unlinking or renaming evidence that this queue does not own. */
static int cpsh_quarantine_local_leaf(queue,name,maximum)
int queue;const char *name;uint64_t maximum;
{
    struct stat directory,status;
    char quarantined[CPSH_NAME_MAX];
    int length;
    if(!name||fstat(queue,&directory))return -1;
    if(fstatat(queue,name,&status,AT_SYMLINK_NOFOLLOW))
        return errno==ENOENT ? 0:-1;
    if(!S_ISREG(status.st_mode)||status.st_uid!=directory.st_uid||
       (status.st_mode&07777)!=0600||status.st_nlink!=1||
       status.st_size<0||(uint64_t)status.st_size>maximum)return 1;
    length=snprintf(quarantined,sizeof(quarantined),"%s.poison",name);
    if(length<0||(size_t)length>=sizeof(quarantined))return -1;
    if(fstatat(queue,quarantined,&status,AT_SYMLINK_NOFOLLOW)==0) {
        /* A previous quarantine already preserves this identity's evidence.
         * Reclaim a recreated active leaf rather than overwriting that file. */
        if(unlinkat(queue,name,0)||cpsh_fsync(queue,1))return -1;
    } else if(errno!=ENOENT||renameat(queue,name,queue,quarantined)||
       cpsh_fsync(queue,1))return -1;
    memset(quarantined,0,sizeof(quarantined));
    return 0;
}

/* An unsafe leaf cannot be reclaimed, but its UUID still needs a local,
 * fsynced diagnostic that later scans recognize and capacity accounting keeps.
 * The marker lives outside every active token/source spelling, so it never
 * masks an untrusted leaf or gives it capture authority. */
static int cpsh_mark_poison(queue,command)
int queue;const char *command;
{
    static const char bytes[]="poison=1\n";
    struct stat directory,before;
    char marker[CPSH_NAME_MAX];
    int file=-1,result=-1,close_result,length;
    if(!command||!cpsh_uuid(command)||fstat(queue,&directory))return -1;
    length=snprintf(marker,sizeof(marker),"%s%s",command,CPSH_POISON_SUFFIX);
    if(length!=43)return -1;
    if(fstatat(queue,marker,&before,AT_SYMLINK_NOFOLLOW)==0) {
        result=S_ISREG(before.st_mode)&&before.st_uid==directory.st_uid&&
            (before.st_mode&07777)==0600&&before.st_nlink==1&&
            before.st_size>0&&(uint64_t)before.st_size<CPSH_RECORD_MAX ? 0:-1;
        goto done;
    }
    if(errno!=ENOENT)goto done;
    file=openat(queue,marker,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC|O_BINARY,
        0600);
    if(file<0||!cpsh_new_file_safe(file,&directory,&before)||
       cpsh_write_all(file,bytes,sizeof(bytes)-1U)||cpsh_fsync(file,0))goto done;
    close_result=cpsh_close(file);file=-1;
    if(close_result||cpsh_fsync(queue,1))goto done;
    result=0;
done:
    if(file>=0&&cpsh_close(file))result=-1;
    memset(marker,0,sizeof(marker));
    return result;
}

/* Move every known leaf for one malformed local identity out of candidate
 * discovery.  It never overwrites preserved evidence or follows a link; an
 * unsafe leaf remains untouched but gets a separate durable poison marker. */
static int cpsh_quarantine_identity(queue,command)
int queue;const char *command;
{
    char name[CPSH_NAME_MAX],temp[CPSH_NAME_MAX],source[CPSH_NAME_MAX];
    char source_temp[CPSH_NAME_MAX],consumed[CPSH_NAME_MAX];
    char consumed_temp[CPSH_NAME_MAX];
    int result,unsafe=0;
    if(!command||!cpsh_uuid(command)||
       snprintf(name,sizeof(name),"%s%s",command,CPSH_SUFFIX)!=44||
       snprintf(temp,sizeof(temp),"%s%s",command,CPSH_TEMP_SUFFIX)!=48||
       snprintf(source,sizeof(source),"%s%s",command,CPSH_SOURCE_SUFFIX)!=43||
       snprintf(source_temp,sizeof(source_temp),"%s%s",command,
       CPSH_SOURCE_SUFFIX ".tmp")!=47||
       snprintf(consumed,sizeof(consumed),"%s%s",command,
       CPSH_CONSUMED_SUFFIX)!=52||
       snprintf(consumed_temp,sizeof(consumed_temp),"%s%s",command,
       CPSH_CONSUMED_TEMP_SUFFIX)!=56)return -1;
    result=cpsh_quarantine_local_leaf(queue,name,CPSH_RECORD_MAX);
    if(result<0)return -1;
    if(result>0)unsafe=1;
    result=cpsh_quarantine_local_leaf(queue,temp,CPSH_RECORD_MAX);
    if(result<0)return -1;
    if(result>0)unsafe=1;
    result=cpsh_quarantine_local_leaf(queue,source,
        CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES);
    if(result<0)return -1;
    if(result>0)unsafe=1;
    result=cpsh_quarantine_local_leaf(queue,source_temp,
        CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES);
    if(result<0)return -1;
    if(result>0)unsafe=1;
    result=cpsh_quarantine_local_leaf(queue,consumed,CPSH_RECORD_MAX);
    if(result<0)return -1;
    if(result>0)unsafe=1;
    result=cpsh_quarantine_local_leaf(queue,consumed_temp,CPSH_RECORD_MAX);
    if(result<0)return -1;
    if(result>0)unsafe=1;
    return cpsh_mark_poison(queue,command) ? -1:unsafe;
}

static int cpsh_hash_source(fd,digest)
int fd;char digest[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN+1];
{
    struct stat status;
    if(fd<0||fstat(fd,&status))return -1;
    if(status.st_nlink==1)return character_save_journal_v2_hash_fd(fd,digest);
    return -1;
}

static int cpsh_same_file(left,right)
const struct stat *left,*right;
{
    return left&&right&&left->st_dev==right->st_dev&&
        left->st_ino==right->st_ino&&left->st_uid==right->st_uid&&
        left->st_mode==right->st_mode&&left->st_size==right->st_size;
}

static int cpsh_open_queue(root,uid,create)
int root;uid_t uid;int create;
{
    int queue;
    if(create&&mkdirat(root,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,0700)==0) {
    }
    else if(create&&errno!=EEXIST)return -1;
    queue=openat(root,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DIRECTORY,
        O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC|O_BINARY);
    if(queue<0)return errno==ENOENT&&!create ? -2:-1;
    if(!cpsh_queue_safe(queue,uid)) { cpsh_close(queue);return -1; }
    /* A replay cannot know whether a preceding mkdir parent fsync reached
     * stable storage, so every producer open revalidates that parent. */
    if(create&&cpsh_fsync_kind(root,CPSH_FAULT_QUEUE_CREATE_FSYNC)) {
        cpsh_close(queue);return -1;
    }
    return queue;
}

/* A link+unlink publish needs repair after a power loss between those calls:
 * final and temp then name the same inode.  Only the fixed private temp name
 * is eligible for removal, and it must be exactly that inode. */
static int cpsh_repair_final(queue,name,temp)
int queue;const char *name,*temp;
{
    struct stat directory,final_status,temp_status;
    if(fstat(queue,&directory)||fstatat(queue,name,&final_status,
       AT_SYMLINK_NOFOLLOW))return errno==ENOENT ? 0:-1;
    if(!S_ISREG(final_status.st_mode)||final_status.st_uid!=directory.st_uid||
       (final_status.st_mode&07777)!=0600)return -1;
    if(final_status.st_nlink==1) {
        if(fstatat(queue,temp,&temp_status,AT_SYMLINK_NOFOLLOW)==0)return -1;
        return errno==ENOENT ? 0:-1;
    }
    if(final_status.st_nlink!=2||fstatat(queue,temp,&temp_status,
       AT_SYMLINK_NOFOLLOW)||!cpsh_same_file(&final_status,&temp_status)||
       unlinkat(queue,temp,0)||cpsh_sync(queue))return -1;
    return 0;
}

/* 0 record read, 1 absent, -1 malformed/unsafe/I/O. */
static int cpsh_read_record_leaf(queue,name,record)
int queue;const char *name;cpsh_record *record;
{
    char bytes[CPSH_RECORD_MAX];
    struct stat directory,before,after;
    int file,result=0,close_result;
    ssize_t count;
    size_t used=0;
    if(!record||!name||fstat(queue,&directory))
        return -1;
    file=openat(queue,name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC|O_BINARY);
    if(file<0)return errno==ENOENT ? 1:-1;
    if(!cpsh_file_safe(file,&directory,&before))result=-1;
    while(!result&&used<(size_t)before.st_size) {
        count=read(file,bytes+used,(size_t)before.st_size-used);
        if(count<0&&errno==EINTR)continue;
        if(count<=0){result=-1;break;}
        used+=(size_t)count;
    }
    if(!result) {
        do count=read(file,bytes+used,1U);while(count<0&&errno==EINTR);
        if(count!=0||fstat(file,&after)||!cpsh_same_file(&before,&after))result=-1;
    }
    close_result=cpsh_close(file);
    if(close_result)result=-1;
    if(result||cpsh_parse(bytes,used,record))return -1;
    return 0;
}

/* Token and consumed-record publication each use their own fixed temporary
 * name.  Repair that private pair before accepting the final leaf. */
static int cpsh_read_record(queue,name,temp,record)
int queue;const char *name,*temp;cpsh_record *record;
{
    if(!record||!name||!temp||cpsh_repair_final(queue,name,temp))return -1;
    return cpsh_read_record_leaf(queue,name,record);
}

static int cpsh_leaf_command(name,suffix,length,command)
const char *name,*suffix;size_t length;char command[37];
{
    if(!name||!suffix||!command||cpsh_bounded(name,CPSH_NAME_MAX-1U)!=length||
       strcmp(name+36U,suffix))return 0;
    memcpy(command,name,36U);command[36]=0;
    return cpsh_uuid(command);
}

/* Source-only reservations and consumed records are candidates as well as
 * ordinary tokens.  A consumer can therefore finish a source reservation
 * whose token publication failed, and can resume cleanup after a durable
 * artifact even when the token has already been removed. */
static int cpsh_candidate_command(name,command)
const char *name;char command[37];
{
    return cpsh_leaf_command(name,CPSH_SUFFIX,44U,command)||
        cpsh_leaf_command(name,CPSH_TEMP_SUFFIX,48U,command)||
        cpsh_leaf_command(name,CPSH_SOURCE_SUFFIX,43U,command)||
        cpsh_leaf_command(name,CPSH_SOURCE_SUFFIX ".tmp",47U,command)||
        cpsh_leaf_command(name,CPSH_CONSUMED_SUFFIX,52U,command)||
        cpsh_leaf_command(name,CPSH_CONSUMED_TEMP_SUFFIX,56U,command);
}

/* A poison spelling is durable evidence for the entire command identity.
 * It is intentionally not an active consumer candidate, but it must survive
 * restart and exert exactly the same capacity backpressure as a reservation. */
static int cpsh_poison_command(name,command)
const char *name;char command[37];
{
    return cpsh_leaf_command(name,CPSH_SUFFIX CPSH_POISON_SUFFIX,51U,command)||
        cpsh_leaf_command(name,CPSH_TEMP_SUFFIX CPSH_POISON_SUFFIX,55U,command)||
        cpsh_leaf_command(name,CPSH_SOURCE_SUFFIX CPSH_POISON_SUFFIX,50U,command)||
        cpsh_leaf_command(name,CPSH_SOURCE_SUFFIX ".tmp" CPSH_POISON_SUFFIX,
        54U,command)||
        cpsh_leaf_command(name,CPSH_CONSUMED_SUFFIX CPSH_POISON_SUFFIX,
        59U,command)||
        cpsh_leaf_command(name,CPSH_CONSUMED_TEMP_SUFFIX CPSH_POISON_SUFFIX,
        63U,command)||
        cpsh_leaf_command(name,CPSH_POISON_SUFFIX,43U,command);
}

/* Count durable command identities, not their leaves.  Interrupted source
 * reservations and every poison spelling therefore consume bounded capacity. */
static int cpsh_reservation_name(name,command)
const char *name;char command[37];
{
    return cpsh_candidate_command(name,command)||cpsh_poison_command(name,command);
}

static int cpsh_count(queue,count_out)
int queue;unsigned int *count_out;
{
    DIR *directory;struct dirent *entry;int duplicate,close_result,seen;
    char commands[CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING][37],command[37];
    unsigned int count=0,index;
    if(!count_out)return -1;
    *count_out=0;
    duplicate=fcntl(queue,F_DUPFD_CLOEXEC,3);
    if(duplicate<0)return -1;
    if(lseek(duplicate,0,SEEK_SET)<0) { cpsh_close(duplicate);return -1; }
    directory=fdopendir(duplicate);
    if(!directory) { cpsh_close(duplicate);return -1; }
    errno=0;
    while((entry=readdir(directory))!=0) {
        if(!cpsh_reservation_name(entry->d_name,command))continue;
        seen=0;
        for(index=0;index<count;index++)
            if(!strcmp(commands[index],command)) { seen=1;break; }
        if(seen)continue;
        count++;
        if(count>CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING) {
            closedir(directory);return -1;
        }
        memcpy(commands[count-1U],command,sizeof(command));
    }
    close_result=closedir(directory);
    if(errno||close_result)return -1;
    *count_out=count;return 0;
}

static int cpsh_open_child(root,name,uid)
int root;const char *name;uid_t uid;
{
    int child=openat(root,name,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC|O_BINARY);
    if(child<0)return -1;
    if(!cpsh_queue_safe(child,uid)) { cpsh_close(child);return -1; }
    return child;
}

static int cpsh_stage_file_safe(fd,directory,before)
int fd;const struct stat *directory;struct stat *before;
{
    return fd>=0&&directory&&before&&!fstat(fd,before)&&
        S_ISREG(before->st_mode)&&before->st_uid==directory->st_uid&&
        (before->st_mode&07777)==0600&&before->st_nlink==1&&
        before->st_size>0&&
        (uint64_t)before->st_size<=CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES;
}

/* A power loss before rename can leave a fully fsynced private source under
 * its fixed temporary name.  Its UUID plus V2 PREPARED record are sufficient
 * to verify and rename that reservation even after legacy publish removed
 * stage.  A partial private temp is reported separately so a retry can
 * replace it while stage still exists, or drain can explicitly drop that one
 * snapshot after publication rather than poisoning later commands. */
static int cpsh_recover_source_temp(queue,name,temp,post_sha256)
int queue;const char *name,*temp,*post_sha256;
{
    struct stat directory,before,after,visible;
    char digest[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN+1];
    int source=-1,result=-1,close_result;
    if(!name||!temp||!post_sha256||fstat(queue,&directory))goto done;
    if(fstatat(queue,name,&visible,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT)
        goto done;
    source=openat(queue,temp,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC|O_BINARY);
    if(source<0) {
        result=errno==ENOENT ? 1:-1;
        goto done;
    }
    if(!cpsh_source_temp_local(source,&directory,&before)) {
        result=-1;goto done;
    }
    if(!cpsh_source_file_safe(source,&directory,&before)||
       cpsh_hash_source(source,digest)||strcmp(digest,post_sha256)||
       !cpsh_source_file_safe(source,&directory,&after)||
       !cpsh_same_file(&before,&after)) {
        /* The exact fixed temp may be a partially written private copy.  Do
         * not call it a command corruption: caller will reclaim it only
         * through cpsh_reclaim_local_temp's narrow local-file checks. */
        result=CPSH_SOURCE_PARTIAL_TEMP;goto done;
    }
    close_result=cpsh_close(source);source=-1;
    if(close_result) { result=-1;goto done; }
    /* A consumer-visible source is born by one atomic rename.  Do not use
     * final+temp as a recoverable link/unlink interval: it is externally
     * indistinguishable from an injected two-link pair and must be poisoned. */
    if(fstatat(queue,name,&visible,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT||
       cpsh_fault(CPSH_FAULT_SOURCE_RENAME)||renameat(queue,temp,queue,name)||
       cpsh_fsync_kind(queue,CPSH_FAULT_SOURCE_DIRECTORY_FSYNC)||
       fstatat(queue,name,&visible,AT_SYMLINK_NOFOLLOW)||visible.st_nlink!=1) {
        result=-1;goto done;
    }
    if(fstatat(queue,temp,&after,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT||
       !cpsh_same_file(&before,&visible)) { result=-1;goto done; }
    result=0;
done:
    if(source>=0&&cpsh_close(source))result=-1;
    memset(digest,0,sizeof(digest));
    return result;
}

/* CPSH_SOURCE_OK valid, CPSH_SOURCE_ABSENT absent,
 * CPSH_SOURCE_PARTIAL_TEMP local incomplete temp, -1 unsafe/altered/I/O. */
static int cpsh_source_valid(queue,name,temp,post_sha256)
int queue;const char *name,*temp,*post_sha256;
{
    struct stat directory,before,after,visible;
    char digest[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN+1];
    int source,result=0,close_result,recovered;
    if(!name||!temp||!post_sha256||fstat(queue,&directory))return -1;
    if(fstatat(queue,name,&visible,AT_SYMLINK_NOFOLLOW)==0) {
        /* A source final and its fixed temporary must never coexist.  Rename
         * publication has no two-name recovery state, so every such pair
         * (including two links to one inode) is untrusted queue poison. */
        if(fstatat(queue,temp,&after,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT)
            return -1;
    } else if(errno==ENOENT) {
        recovered=cpsh_recover_source_temp(queue,name,temp,post_sha256);
        if(recovered==1)return CPSH_SOURCE_ABSENT;
        if(recovered)return recovered;
    } else return -1;
    source=openat(queue,name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC|O_BINARY);
    if(source<0)return -1;
    if(!cpsh_source_file_safe(source,&directory,&before)||
       cpsh_hash_source(source,digest)||strcmp(digest,post_sha256)||
       !cpsh_source_file_safe(source,&directory,&after)||
       !cpsh_same_file(&before,&after))result=-1;
    close_result=cpsh_close(source);
    if(close_result)result=-1;
    memset(digest,0,sizeof(digest));
    return result ? -1:CPSH_SOURCE_OK;
}

static int cpsh_copy_stage(stage,target)
int stage,target;
{
    char bytes[8192];
    ssize_t count;
    while(1) {
        do count=read(stage,bytes,sizeof(bytes));while(count<0&&errno==EINTR);
        if(count<0)return -1;
        if(!count)break;
        if(cpsh_write_all(target,bytes,(size_t)count))return -1;
    }
    return 0;
}

/* The private copy never changes stage's one-link legacy invariant.  There
 * are at most MAX_PENDING identities and each copy is limited by the V2
 * 64MiB stage ceiling, so retained bytes are bounded at 8GiB. */
static int cpsh_reserve_source(root,uid,queue,record,wire,source,temp)
int root,queue;uid_t uid;const cpsh_record *record;
const character_save_journal_v2_wire *wire;const char *source,*temp;
{
    struct stat directory,stage_before,stage_after,source_before,source_after;
    char stage_leaf[CHARACTER_SAVE_JOURNAL_V2_STAGE_LEAF_MAX+1];
    char digest[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN+1];
    int state,stage_directory=-1,stage=-1,target=-1,result=
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    unsigned int count;
    (void)record;
    if(!wire||!source||!temp)return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
    state=cpsh_source_valid(queue,source,temp,wire->post_sha256);
    if(state==CPSH_SOURCE_PARTIAL_TEMP) {
        if(cpsh_reclaim_local_temp(queue,temp,
           CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES,
           CPSH_FAULT_SOURCE_DIRECTORY_FSYNC))
            return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
        state=cpsh_source_valid(queue,source,temp,wire->post_sha256);
    }
    if(state==CPSH_SOURCE_OK)return cpsh_fsync_kind(queue,
       CPSH_FAULT_SOURCE_DIRECTORY_FSYNC) ?
       CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR :
       CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK;
    if(state<0)return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
    if(cpsh_count(queue,&count))return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(count>=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING)
        return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_FULL;
    stage_directory=cpsh_open_child(root,"character-save-stage",uid);
    if(stage_directory<0||character_save_journal_v2_stage_leaf(
       wire->command_uuid,stage_leaf,sizeof(stage_leaf)))goto done;
    stage=openat(stage_directory,stage_leaf,
        O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC|O_BINARY);
    if(fstat(queue,&directory)||!cpsh_stage_file_safe(stage,&directory,
       &stage_before)||character_save_journal_v2_hash_fd(stage,digest)||
       strcmp(digest,wire->post_sha256)||lseek(stage,0,SEEK_SET)<0)goto done;
    if(unlinkat(queue,temp,0)&&errno!=ENOENT)goto done;
    target=openat(queue,temp,O_RDWR|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC|O_BINARY,
        0600);
    if(target<0||!cpsh_new_file_safe(target,&directory,&source_before)||
       cpsh_copy_stage(stage,target)||cpsh_fsync(target,0)||
       !cpsh_source_file_safe(target,&directory,&source_after)||
       cpsh_hash_source(target,digest)||strcmp(digest,wire->post_sha256)||
       !cpsh_stage_file_safe(stage,&directory,&stage_after)||
       !cpsh_same_file(&stage_before,&stage_after))goto done;
    if(cpsh_close(target)) { target=-1;goto done; }
    target=-1;
    if(cpsh_close(stage)) { stage=-1;goto done; }
    stage=-1;
    /* Source publication is rename-only: a valid temp replaces the absent
     * final in one namespace operation, so consumers never observe an owned
     * two-link source.  A pre-existing final is not retried or replaced. */
    if(fstatat(queue,temp,&source_before,AT_SYMLINK_NOFOLLOW)||
       fstatat(queue,source,&source_after,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT||
       cpsh_fault(CPSH_FAULT_SOURCE_RENAME)||renameat(queue,temp,queue,source))
        goto done;
    if(fstatat(queue,source,&source_after,AT_SYMLINK_NOFOLLOW)||
       !cpsh_same_file(&source_before,&source_after)||
       cpsh_fsync_kind(queue,CPSH_FAULT_SOURCE_DIRECTORY_FSYNC)||
       fstatat(queue,source,&source_after,AT_SYMLINK_NOFOLLOW)||
       source_after.st_nlink!=1||
       fstatat(queue,temp,&source_before,AT_SYMLINK_NOFOLLOW)==0||errno!=ENOENT)
        goto done;
    result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK;
done:
    if(target>=0&&cpsh_close(target)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(stage>=0&&cpsh_close(stage)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(stage_directory>=0&&cpsh_close(stage_directory)&&
       result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    memset(stage_leaf,0,sizeof(stage_leaf));memset(digest,0,sizeof(digest));
    return result;
}

static int cpsh_publish_record(root,uid,queue,record,wire)
int root,queue;uid_t uid;const cpsh_record *record;
const character_save_journal_v2_wire *wire;
{
    char bytes[CPSH_RECORD_MAX],name[CPSH_NAME_MAX],temp[CPSH_NAME_MAX];
    char source[CPSH_NAME_MAX],source_temp[CPSH_NAME_MAX],consumed[CPSH_NAME_MAX];
    struct stat directory,created,after_link,visible;
    int file=-1,result=0,close_result,read_result,source_result;
    cpsh_record existing;
    if(cpsh_encode(bytes,sizeof(bytes),record)<0||
       cpsh_names(record,name,temp,source,source_temp,consumed))
        return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
    read_result=cpsh_read_record(queue,name,temp,&existing);
    if(read_result==0) {
        if(!cpsh_equal(record,&existing))return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
        source_result=cpsh_reserve_source(root,uid,queue,record,wire,source,
            source_temp);
        if(source_result!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)return source_result;
        /* A visible final after an earlier failed fsync is not a completed
         * handoff.  Re-sync it before reporting exact retry. */
        return cpsh_fsync(queue,1) ? CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR :
            CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY;
    }
    if(read_result<0)return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
    source_result=cpsh_reserve_source(root,uid,queue,record,wire,source,
        source_temp);
    if(source_result!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)return source_result;

    /* An interrupted pre-link attempt leaves only this fixed private temp.
     * It has no published meaning, so a later PREPARED replay may discard it. */
    if(unlinkat(queue,temp,0)&&errno!=ENOENT)return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    file=openat(queue,temp,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC|O_BINARY,
        0600);
    if(file<0)return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(fstat(queue,&directory)||!cpsh_new_file_safe(file,&directory,&created)||
       cpsh_write_all(file,bytes,strlen(bytes))||cpsh_fsync(file,0))result=-1;
    close_result=cpsh_close(file);file=-1;
    if(close_result)result=-1;
    if(result)return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(cpsh_fault(CPSH_FAULT_LINK)||
       linkat(queue,temp,queue,name,0)) {
        if(errno==EEXIST) {
            read_result=cpsh_read_record(queue,name,temp,&existing);
            if(read_result==0&&cpsh_equal(record,&existing)&&
               !cpsh_fsync(queue,1))return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY;
            return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
        }
        return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    }
    if(fstatat(queue,name,&visible,AT_SYMLINK_NOFOLLOW)||
       fstatat(queue,temp,&after_link,AT_SYMLINK_NOFOLLOW)||
       !cpsh_same_file(&visible,&after_link)||unlinkat(queue,temp,0))
        return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(cpsh_fsync(queue,1))return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    return CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK;
}

/* The durable consumed record closes the artifact/cleanup gap.  Capture has
 * already durably stored (or exactly retried) the artifact before this record
 * is published.  If power fails during later cleanup, the record lets a
 * future drain remove either order of token/source leaves without decoding
 * again or orphaning a private source. */
static int cpsh_publish_consumed_record(queue,record,name,temp)
int queue;const cpsh_record *record;const char *name,*temp;
{
    char bytes[CPSH_RECORD_MAX];
    struct stat directory,created,after_link,visible;
    int file=-1,result=0,close_result,read_result;
    cpsh_record existing;
    if(cpsh_encode(bytes,sizeof(bytes),record)<0||!name||!temp)return -1;
    read_result=cpsh_read_record(queue,name,temp,&existing);
    if(read_result==0)
        return cpsh_equal(record,&existing)&&!cpsh_fsync(queue,1) ? 0:-1;
    if(read_result<0)return -1;
    if(unlinkat(queue,temp,0)&&errno!=ENOENT)return -1;
    file=openat(queue,temp,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_CLOEXEC|O_BINARY,
        0600);
    if(file<0)return -1;
    if(fstat(queue,&directory)||!cpsh_new_file_safe(file,&directory,&created)||
       cpsh_write_all(file,bytes,strlen(bytes))||cpsh_fsync(file,0))result=-1;
    close_result=cpsh_close(file);file=-1;
    if(close_result)result=-1;
    if(result)return -1;
    if(linkat(queue,temp,queue,name,0)) {
        if(errno==EEXIST) {
            read_result=cpsh_read_record(queue,name,temp,&existing);
            if(read_result==0&&cpsh_equal(record,&existing)&&
               !cpsh_fsync(queue,1))return 0;
        }
        return -1;
    }
    if(fstatat(queue,name,&visible,AT_SYMLINK_NOFOLLOW)||
       fstatat(queue,temp,&after_link,AT_SYMLINK_NOFOLLOW)||
       !cpsh_same_file(&visible,&after_link)||unlinkat(queue,temp,0)||
       cpsh_fsync(queue,1))return -1;
    return 0;
}

static int cpsh_record_from_prepared(record,wire,tuple,command)
cpsh_record *record;const character_save_journal_v2_wire *wire;
const character_save_journal_v2_writer_tuple *tuple;const char *command;
{
    if(!record||!wire||!tuple||!command||strcmp(wire->command_uuid,command)||
       strcmp(wire->world_id,tuple->world_id)||
       strcmp(wire->writer_instance_id,tuple->writer_instance_id)||
       wire->writer_epoch!=tuple->writer_epoch)return -1;
    memset(record,0,sizeof(*record));
    if(cpsh_text_copy(record->command_uuid,sizeof(record->command_uuid),
       wire->command_uuid,strlen(wire->command_uuid))||
       cpsh_text_copy(record->request_sha256,sizeof(record->request_sha256),
       wire->request_sha256,strlen(wire->request_sha256))||
       cpsh_text_copy(record->writer_instance_id,sizeof(record->writer_instance_id),
       wire->writer_instance_id,strlen(wire->writer_instance_id)))return -1;
    record->writer_epoch=wire->writer_epoch;
    return cpsh_record_valid(record) ? 0:-1;
}

static int cpsh_record_from_prepared_at(root,tuple,command,record,wire_out)
int root;const character_save_journal_v2_writer_tuple *tuple;
const char *command;cpsh_record *record;
character_save_journal_v2_wire *wire_out;
{
    character_save_journal_v2_wire wire;
    int result=-1;
    memset(&wire,0,sizeof(wire));
    if(root>=0&&tuple&&command&&record&&
       !character_save_journal_v2_read_prepared_at(root,command,&wire)&&
       !cpsh_record_from_prepared(record,&wire,tuple,command)) {
        if(wire_out)*wire_out=wire;
        result=0;
    }
    memset(&wire,0,sizeof(wire));
    return result;
}

static void cpsh_last(handoff,result,capture_result,command)
character_player_snapshot_v1_handoff *handoff;
character_player_snapshot_v1_handoff_result result;
int capture_result;const char *command;
{
    if(!handoff)return;
    handoff->report.last_result=result;
    handoff->report.last_capture_result=capture_result;
    memset(handoff->report.last_command_id,0,
        sizeof(handoff->report.last_command_id));
    if(command&&cpsh_uuid(command))
        memcpy(handoff->report.last_command_id,command,36U);
}

static int cpsh_report_quarantined(handoff,queue,command)
character_player_snapshot_v1_handoff *handoff;int queue;const char *command;
{
    int unsafe;
    if(!handoff||(unsafe=cpsh_quarantine_identity(queue,command))<0)return -1;
    cpsh_increment(&handoff->report.quarantined_poison);
    cpsh_increment(&handoff->report.reclaimed_poison);
    cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT,
        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID,command);
    return unsafe;
}

void character_player_snapshot_v1_handoff_init(handoff,capture)
character_player_snapshot_v1_handoff *handoff;
character_player_snapshot_v1_capture *capture;
{
    if(!handoff)return;
    memset(handoff,0,sizeof(*handoff));handoff->capture=capture;
    handoff->report.last_result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
    handoff->report.last_capture_result=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID;
}

int character_player_snapshot_v1_handoff_observe(opaque,writer,command_uuid)
void *opaque;const character_save_journal_v2_writer_context *writer;
const char *command_uuid;
{
    character_player_snapshot_v1_handoff *handoff=
        (character_player_snapshot_v1_handoff *)opaque;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire;
    cpsh_record record;
    uid_t uid=0;
    int root=-1,queue=-1;
    character_player_snapshot_v1_handoff_result result=
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
    if(!handoff||!writer||!command_uuid||!cpsh_uuid(command_uuid))goto done;
    memset(&tuple,0,sizeof(tuple));memset(&wire,0,sizeof(wire));
    if(character_save_journal_v2_writer_validate_held(writer,&tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||
       character_save_journal_v2_writer_dup_held_root_fd(writer,&root)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||!cpsh_root_safe(root,&uid)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CONTEXT;goto done;
    }
    if(character_save_journal_v2_read_prepared_at(root,command_uuid,&wire)||
       cpsh_record_from_prepared(&record,&wire,&tuple,command_uuid)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_PREPARED;goto done;
    }
    queue=cpsh_open_queue(root,uid,1);
    if(queue<0) { result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;goto done; }
    result=(character_player_snapshot_v1_handoff_result)
        cpsh_publish_record(root,uid,queue,&record,&wire);
done:
    if(queue>=0&&cpsh_close(queue)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(root>=0&&cpsh_close(root)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    cpsh_last(handoff,result,CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID,command_uuid);
    if(handoff) {
        if(result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)cpsh_increment(&handoff->report.enqueued);
        else if(result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_EXACT_RETRY)cpsh_increment(&handoff->report.exact_retries);
        else if(result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_FULL)cpsh_increment(&handoff->report.full);
        else cpsh_increment(&handoff->report.failed);
    }
    memset(&tuple,0,sizeof(tuple));memset(&wire,0,sizeof(wire));memset(&record,0,sizeof(record));
    return result;
}

static int cpsh_compare_name(left,right)
const void *left,*right;
{ return strcmp((const char *)left,(const char *)right); }

static int cpsh_candidates(queue,names,count_out)
int queue;char names[CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING][CPSH_NAME_MAX];
unsigned int *count_out;
{
    DIR *directory;struct dirent *entry;int duplicate,close_result,seen,active,poison;
    char command[37],commands[CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING][37];
    unsigned char active_flags[CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING];
    unsigned char poison_flags[CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING];
    unsigned int count=0,identities=0,index;
    if(!names||!count_out)return -1;
    *count_out=0;
    duplicate=fcntl(queue,F_DUPFD_CLOEXEC,3);
    if(duplicate<0)return -1;
    if(lseek(duplicate,0,SEEK_SET)<0) { cpsh_close(duplicate);return -1; }
    directory=fdopendir(duplicate);
    if(!directory) { cpsh_close(duplicate);return -1; }
    errno=0;
    while((entry=readdir(directory))!=0) {
        active=cpsh_candidate_command(entry->d_name,command);
        if(!active&&!cpsh_poison_command(entry->d_name,command))continue;
        poison=cpsh_poison_command(entry->d_name,command);
        seen=0;
        for(index=0;index<identities;index++)
            if(!memcmp(commands[index],command,36U)) { seen=1;break; }
        if(!seen) {
            if(identities==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING) {
                closedir(directory);return -1;
            }
            memcpy(commands[identities],command,sizeof(command));
            active_flags[identities]=(unsigned char)active;
            poison_flags[identities]=(unsigned char)poison;
            identities++;
        } else {
            active_flags[index]|=(unsigned char)active;
            poison_flags[index]|=(unsigned char)poison;
        }
    }
    close_result=closedir(directory);
    if(errno||close_result)return -1;
    for(index=0;index<identities;index++) {
        if(!active_flags[index]||poison_flags[index])continue;
        if(snprintf(names[count],CPSH_NAME_MAX,"%s%s",commands[index],CPSH_SUFFIX)!=44) {
            return -1;
        }
        count++;
    }
    qsort(names,count,sizeof(names[0]),cpsh_compare_name);
    *count_out=count;return 0;
}

static int cpsh_record_matches_prepared(writer,record,wire_out)
const character_save_journal_v2_writer_context *writer;const cpsh_record *record;
character_save_journal_v2_wire *wire_out;
{
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire;
    uid_t uid;
    int root=-1,result=-1;
    if(!writer||!cpsh_record_valid(record))return -1;
    memset(&tuple,0,sizeof(tuple));memset(&wire,0,sizeof(wire));
    if(character_save_journal_v2_writer_validate_held(writer,&tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||
       character_save_journal_v2_writer_dup_held_root_fd(writer,&root)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||!cpsh_root_safe(root,&uid)||
       character_save_journal_v2_read_prepared_at(root,record->command_uuid,&wire)||
       strcmp(wire.command_uuid,record->command_uuid)||
       strcmp(wire.request_sha256,record->request_sha256)||
       strcmp(wire.writer_instance_id,record->writer_instance_id)||
       wire.writer_epoch!=record->writer_epoch)goto done;
    if(wire_out)*wire_out=wire;
    result=0;
done:
    if(root>=0&&cpsh_close(root))result=-1;
    memset(&tuple,0,sizeof(tuple));memset(&wire,0,sizeof(wire));
    return result;
}

/* Every removal either reaches a successful parent fsync or leaves the name
 * for this durable consumed record to retry.  Even an already-absent name is
 * re-fsynced: that closes the crash window after a previous unlink succeeded
 * but its parent fsync failed. */
static int cpsh_remove_record(queue,name,temp,record)
int queue;const char *name,*temp;const cpsh_record *record;
{
    cpsh_record existing;
    int state,reclaimed=0;
    if(!name||!temp||!record)return -1;
    state=cpsh_read_record(queue,name,temp,&existing);
    if(state<0) {
        /* A malformed fixed temporary is local queue poison, not durable
         * command evidence.  Reclaim it durably, then re-read the final
         * record; a future identity must not be held behind this one. */
        if(cpsh_reclaim_local_temp(queue,temp,CPSH_RECORD_MAX,
           CPSH_FAULT_DIRECTORY_FSYNC))return -1;
        reclaimed=1;
        state=cpsh_read_record(queue,name,temp,&existing);
    }
    if(state==0) {
        if(!cpsh_equal(record,&existing)||unlinkat(queue,name,0)||
           cpsh_fsync(queue,1))return -1;
    } else if(state==1) {
        if(cpsh_fsync(queue,1))return -1;
    } else return -1;
    state=cpsh_read_record_leaf(queue,temp,&existing);
    if(state==0) {
        if(!cpsh_equal(record,&existing)||unlinkat(queue,temp,0)||
           cpsh_fsync(queue,1))return -1;
    } else if(state<0) {
        if(cpsh_reclaim_local_temp(queue,temp,CPSH_RECORD_MAX,
           CPSH_FAULT_DIRECTORY_FSYNC))return -1;
        reclaimed=1;
    }
    memset(&existing,0,sizeof(existing));
    return reclaimed;
}

static int cpsh_remove_source(queue,name,temp,post_sha256)
int queue;const char *name,*temp,*post_sha256;
{
    int state;
    state=cpsh_source_valid(queue,name,temp,post_sha256);
    if(state==0) {
        if(unlinkat(queue,name,0)||
           cpsh_fsync_kind(queue,CPSH_FAULT_SOURCE_DIRECTORY_FSYNC))return -1;
        return 0;
    }
    if(state==1)
        return cpsh_fsync_kind(queue,CPSH_FAULT_SOURCE_DIRECTORY_FSYNC) ? -1:0;
    return -1;
}

static int cpsh_finish_consumed(queue,record,post_sha256)
int queue;const cpsh_record *record;const char *post_sha256;
{
    char name[CPSH_NAME_MAX],temp[CPSH_NAME_MAX],source[CPSH_NAME_MAX];
    char source_temp[CPSH_NAME_MAX],consumed[CPSH_NAME_MAX];
    char consumed_temp[CPSH_NAME_MAX];
    int first,third;
    if(cpsh_names(record,name,temp,source,source_temp,consumed)||
       cpsh_consumed_temp_name(record,consumed_temp))return -1;
    first=cpsh_remove_record(queue,name,temp,record);
    if(first<0||cpsh_remove_source(queue,source,source_temp,post_sha256))
        return -1;
    third=cpsh_remove_record(queue,consumed,consumed_temp,record);
    if(third<0)return -1;
    return first||third ? 1:0;
}

/* Only an absent legacy stage permits dropping a partial private source after
 * publication.  A readable, safe stage might still repair the reservation;
 * unsafe or inaccessible stage state is left for a later diagnostic pass. */
static int cpsh_stage_absent(root,uid,wire)
int root;uid_t uid;const character_save_journal_v2_wire *wire;
{
    struct stat directory,before;
    char leaf[CHARACTER_SAVE_JOURNAL_V2_STAGE_LEAF_MAX+1];
    int stage_directory=-1,stage=-1,result=-1;
    if(root<0||!wire||character_save_journal_v2_stage_leaf(
       wire->command_uuid,leaf,sizeof(leaf)))goto done;
    stage_directory=cpsh_open_child(root,"character-save-stage",uid);
    if(stage_directory<0||fstat(stage_directory,&directory))goto done;
    stage=openat(stage_directory,leaf,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|
        O_CLOEXEC|O_BINARY);
    if(stage<0) {
        result=errno==ENOENT ? 1:-1;
        goto done;
    }
    result=cpsh_stage_file_safe(stage,&directory,&before) ? 0:-1;
done:
    if(stage>=0&&cpsh_close(stage))result=-1;
    if(stage_directory>=0&&cpsh_close(stage_directory))result=-1;
    memset(leaf,0,sizeof(leaf));
    return result;
}

/* This is deliberately not consumed cleanup: no artifact exists.  It removes
 * only a valid token (plus a reclaimable fixed token temp) after the source is
 * confirmed absent, so the report can say the snapshot was dropped rather
 * than imply that capture succeeded. */
static int cpsh_drop_unavailable_snapshot(queue,record,wire)
int queue;const cpsh_record *record;const character_save_journal_v2_wire *wire;
{
    char name[CPSH_NAME_MAX],temp[CPSH_NAME_MAX],source[CPSH_NAME_MAX];
    char source_temp[CPSH_NAME_MAX],consumed[CPSH_NAME_MAX];
    int removed;
    if(!record||!wire||cpsh_names(record,name,temp,source,source_temp,consumed)||
       cpsh_source_valid(queue,source,source_temp,wire->post_sha256)!=
       CPSH_SOURCE_ABSENT)return -1;
    removed=cpsh_remove_record(queue,name,temp,record);
    return removed<0 ? -1:0;
}

int character_player_snapshot_v1_handoff_drain(handoff,writer,limit)
character_player_snapshot_v1_handoff *handoff;
const character_save_journal_v2_writer_context *writer;
unsigned int limit;
{
    char names[CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING][CPSH_NAME_MAX];
    char name[CPSH_NAME_MAX],temp[CPSH_NAME_MAX],source_name[CPSH_NAME_MAX];
    char source_temp[CPSH_NAME_MAX],consumed[CPSH_NAME_MAX];
    char consumed_temp[CPSH_NAME_MAX],command[37];
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_wire wire;
    cpsh_record record;
    struct stat directory,source_status;
    uid_t uid=0;
    int root=-1,queue=-1,source=-1,read_result,consumed_result,
        capture_result,source_result,stage_result,cleanup_result,poison_result,result=
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_INVALID;
    unsigned int count,index,done=0;
    if(!handoff||!handoff->capture||!writer||!limit||
       limit>CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_MAX_PENDING)goto finish;
    memset(&tuple,0,sizeof(tuple));
    if(character_save_journal_v2_writer_validate_held(writer,&tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||
       character_save_journal_v2_writer_dup_held_root_fd(writer,&root)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||!cpsh_root_safe(root,&uid)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CONTEXT;goto finish;
    }
    queue=cpsh_open_queue(root,uid,0);
    if(queue==-2) { result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK;goto finish; }
    if(queue<0||cpsh_candidates(queue,names,&count)) {
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;goto finish;
    }
    result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK;
    for(index=0;index<count&&done<limit;index++) {
        if(snprintf(temp,sizeof(temp),"%.*s%s",36,names[index],
           CPSH_TEMP_SUFFIX)!=48||
           snprintf(command,sizeof(command),"%.*s",36,names[index])!=36||
           !cpsh_uuid(command)||
           snprintf(consumed,sizeof(consumed),"%s%s",command,
           CPSH_CONSUMED_SUFFIX)!=52||
           snprintf(consumed_temp,sizeof(consumed_temp),"%s%s",command,
           CPSH_CONSUMED_TEMP_SUFFIX)!=56) {
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
        }
        memset(&record,0,sizeof(record));
        memset(&wire,0,sizeof(wire));
        /* A durable consumed record makes every post-artifact cleanup order
         * idempotent, including source-absent/token-present after a crash. */
        consumed_result=cpsh_read_record(queue,consumed,consumed_temp,&record);
        if(consumed_result<0||
           (consumed_result==0&&cpsh_record_matches_prepared(writer,&record,
           &wire))) {
            poison_result=cpsh_report_quarantined(handoff,queue,command);
            if(poison_result>=0) {
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                if(!poison_result)done++;
                continue;
            }
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
        }
        if(consumed_result==0) {
            cleanup_result=cpsh_finish_consumed(queue,&record,wire.post_sha256);
            if(cleanup_result<0) {
                cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR,
                    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK,record.command_uuid);
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;break;
            }
            cpsh_increment(&handoff->report.consumed);done++;
            if(cleanup_result) {
                /* The identity is durably gone, but keep a diagnostic result
                 * for the malformed local leaf that we reclaimed. */
                cpsh_increment(&handoff->report.reclaimed_poison);
                cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT,
                    CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK,record.command_uuid);
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
            } else cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK,
                CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK,record.command_uuid);
            continue;
        }
        read_result=cpsh_read_record(queue,names[index],temp,&record);
        if(read_result==1) {
            /* A source may be durable while token publication failed.  Its
             * UUID is self-identifying, and V2's durable prepared record is
             * the authoritative metadata until the source is consumed. */
            if(cpsh_record_from_prepared_at(root,&tuple,command,&record,&wire)) {
                poison_result=cpsh_report_quarantined(handoff,queue,command);
                if(poison_result>=0) {
                    result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                    if(!poison_result)done++;
                    continue;
                }
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
            }
        } else if(read_result||cpsh_record_matches_prepared(writer,&record,&wire)) {
            poison_result=cpsh_report_quarantined(handoff,queue,command);
            if(poison_result>=0) {
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                if(!poison_result)done++;
                continue;
            }
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
        }
        if(cpsh_names(&record,name,temp,source_name,source_temp,consumed)||
           cpsh_consumed_temp_name(&record,consumed_temp)||
           strcmp(name,names[index])||fstat(queue,&directory)) {
            poison_result=cpsh_report_quarantined(handoff,queue,command);
            if(poison_result>=0) {
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                if(!poison_result)done++;
                continue;
            }
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
        }
        source_result=cpsh_source_valid(queue,source_name,source_temp,
           wire.post_sha256);
        if(source_result==CPSH_SOURCE_PARTIAL_TEMP) {
            /* The observer may have crashed while copying private source.  A
             * retry rebuilds it from still-live stage; after publication the
             * stage is gone, so drop only this un-captured identity. */
            source_result=cpsh_reserve_source(root,uid,queue,&record,&wire,
                source_name,source_temp);
            if(source_result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
                source_result=cpsh_source_valid(queue,source_name,source_temp,
                    wire.post_sha256);
            if(source_result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR) {
                stage_result=cpsh_stage_absent(root,uid,&wire);
                if(stage_result==1&&
                   !cpsh_drop_unavailable_snapshot(queue,&record,&wire)) {
                    cpsh_increment(&handoff->report.dropped);done++;
                    cpsh_last(handoff,
                        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DROPPED,
                        CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_SOURCE,
                        record.command_uuid);
                    result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_DROPPED;
                    continue;
                }
            }
            if(source_result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT) {
                poison_result=cpsh_report_quarantined(handoff,queue,command);
                if(poison_result>=0) {
                    result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                    if(!poison_result)done++;
                    continue;
                }
            }
            if(source_result!=CPSH_SOURCE_OK) {
                result=source_result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT ?
                    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT :
                    CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
                break;
            }
        }
        if(source_result!=CPSH_SOURCE_OK) {
            poison_result=cpsh_report_quarantined(handoff,queue,command);
            if(poison_result>=0) {
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                if(!poison_result)done++;
                continue;
            }
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
        }
        source=openat(queue,source_name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|
            O_CLOEXEC|O_BINARY);
        if(source<0||!cpsh_source_file_safe(source,&directory,&source_status)) {
            if(source>=0)cpsh_close(source);
            source=-1;
            poison_result=cpsh_report_quarantined(handoff,queue,command);
            if(poison_result>=0) {
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;
                if(!poison_result)done++;
                continue;
            }
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CORRUPT;break;
        }
        capture_result=character_player_snapshot_v1_capture_consume(handoff->capture,
            writer,record.command_uuid,record.request_sha256,
            record.writer_instance_id,record.writer_epoch,source);
        if(cpsh_close(source)) {
            source=-1;
            if(capture_result==CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK) {
                cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR,
                    capture_result,record.command_uuid);
                result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;break;
            }
        }
        source=-1;
        if(capture_result!=CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_OK) {
            cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CAPTURE,
                capture_result,record.command_uuid);
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CAPTURE;break;
        }
        if(cpsh_publish_consumed_record(queue,&record,consumed,consumed_temp)) {
            cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR,
                capture_result,record.command_uuid);
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;break;
        }
        if(cpsh_fault(CPSH_FAULT_UNLINK)||
           cpsh_finish_consumed(queue,&record,wire.post_sha256)) {
            cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR,
                capture_result,record.command_uuid);
            result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;break;
        }
        cpsh_increment(&handoff->report.consumed);done++;
        cpsh_last(handoff,CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK,
            capture_result,record.command_uuid);
    }
finish:
    if(source>=0&&cpsh_close(source)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(queue>=0&&cpsh_close(queue)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(root>=0&&cpsh_close(root)&&result==CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        result=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_IO_ERROR;
    if(handoff&&result!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK&&
       result!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_CAPTURE)
        cpsh_last(handoff,(character_player_snapshot_v1_handoff_result)result,
            CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_INVALID,0);
    if(handoff&&result!=CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK)
        cpsh_increment(&handoff->report.failed);
    memset(&tuple,0,sizeof(tuple));memset(&wire,0,sizeof(wire));
    memset(&record,0,sizeof(record));
    return result;
}
