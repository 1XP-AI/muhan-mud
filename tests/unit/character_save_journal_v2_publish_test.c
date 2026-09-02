#include "character_save_journal_v2_publish.h"

#include <dirent.h>
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

static const char world[]="m3-contract";
static const char instance[]="11111111-1111-4111-8111-111111111111";
static const char character[]="22222222-2222-4222-8222-222222222222";
static const unsigned char name[]="M3hero";
static const char payload[]="091b-2 exact staged bytes\n";

typedef struct mock {
    int calls;
    int forged;
    int reopen_writer;
    const char *root;
    character_save_journal_v2_writer_context *context;
} mock;

static int expect(ok,label)
int ok;
const char *label;
{ if(ok)return 0;fprintf(stderr,"character_save_journal_v2_publish_test: %s\n",label);return 1; }

static int path(out,size,root,leaf)
char *out; size_t size; const char *root,*leaf;
{ int n=snprintf(out,size,"%s/%s",root,leaf);return n<0||(size_t)n>=size?-1:0; }

static int write_all(fd,bytes,length)
int fd; const void *bytes; size_t length;
{ const char *p=(const char *)bytes;ssize_t n;while(length){n=write(fd,p,length);if(n<0&&errno==EINTR)continue;if(n<=0)return -1;p+=n;length-=(size_t)n;}return 0; }

static int read_exact_fd(fd,byte)
int fd;
char *byte;
{
    ssize_t n;
    do n=read(fd,byte,1); while(n<0&&errno==EINTR);
    return n==1?0:-1;
}

static int write_exact_fd(fd,byte)
int fd;
char byte;
{
    ssize_t n;
    do n=write(fd,&byte,1); while(n<0&&errno==EINTR);
    return n==1?0:-1;
}

static int leaf(root,relative,bytes,length)
const char *root,*relative; const void *bytes; size_t length;
{ char full[PATH_MAX];int fd,result=-1;if(path(full,sizeof(full),root,relative))return -1;fd=open(full,O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);if(fd<0)return -1;if(!write_all(fd,bytes,length)&&!fsync(fd))result=0;if(close(fd))result=-1;return result; }

static int hash(root,bytes,length,out)
const char *root,*bytes; size_t length; char out[65];
{ char full[PATH_MAX];int fd,result;if(path(full,sizeof(full),root,"hash-input"))return -1;fd=open(full,O_RDWR|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);if(fd<0||write_all(fd,bytes,length)||lseek(fd,0,SEEK_SET)<0){if(fd>=0)close(fd);return -1;}result=character_save_journal_v2_hash_fd(fd,out);close(fd);unlink(full);return result; }

static int fixture(root)
char *root;
{ static const char one[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
  static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
  char p[PATH_MAX];
  if(path(p,sizeof(p),root,"player")||mkdir(p,0700)||path(p,sizeof(p),root,"player/11")||mkdir(p,0700)||path(p,sizeof(p),root,"character-save-stage")||mkdir(p,0700)||path(p,sizeof(p),root,"character-save-journal")||mkdir(p,0700))return -1;
  return leaf(root,"character-save-journal/writer-instance.v2",one,sizeof(one)-1)||leaf(root,"character-save-journal/writer-epoch.v2",epoch,sizeof(epoch)-1)?-1:0; }

static int remove_dir(root,relative)
const char *root,*relative;
{ char p[PATH_MAX],child[PATH_MAX];DIR *d;struct dirent *e;if(path(p,sizeof(p),root,relative)||!(d=opendir(p)))return -1;while((e=readdir(d))){if(!strcmp(e->d_name,".")||!strcmp(e->d_name,".."))continue;if(snprintf(child,sizeof(child),"%s/%s",p,e->d_name)<0||unlink(child)){closedir(d);return -1;}}if(closedir(d))return -1;return rmdir(p); }

static int teardown(root)
char *root;
{ char p[PATH_MAX];if(remove_dir(root,"character-save-stage")||remove_dir(root,"character-save-journal")||remove_dir(root,"player/11")||path(p,sizeof(p),root,"player")||rmdir(p)||rmdir(root))return -1;return 0; }

static character_save_journal_v2_route_lookup_result lookup(opaque,callback_world,input,input_length,reply)
void *opaque; const char *callback_world; const unsigned char *input; size_t input_length; character_save_journal_v2_route_reply *reply;
{
    mock *m=(mock *)opaque;
    m->calls++;
    memset(reply,0,sizeof(*reply));
    if(strcmp(callback_world,world)||input!=name||input_length!=sizeof(name)-1)
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    if(m->reopen_writer) {
        m->reopen_writer=0;
        if(!m->root||!m->context||
           character_save_journal_v2_writer_close(m->context)!=0||
           character_save_journal_v2_writer_open(m->root,world,m->context)!=0)
            return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    }
    reply->status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count=1;
    strcpy(reply->world_id,world);
    strcpy(reply->character_id,m->forged?"33333333-3333-4333-8333-333333333333":character);
    memcpy(reply->legacy_name,name,sizeof(name)-1);
    reply->legacy_name_length=sizeof(name)-1;
    strcpy(reply->legacy_shard,"11");
    reply->storage_format=1;
    reply->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

static int prepare_bytes(root,command,existing,bytes,length)
const char *root,*command;
int existing;
const void *bytes;
size_t length;
{
    static const char preimage[]="091b-2 exact existing preimage\n";
    character_save_journal_v2_wire wire;
    memset(&wire,0,sizeof(wire));
    wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    strcpy(wire.writer_instance_id,instance);
    strcpy(wire.character_id,character);
    strcpy(wire.world_id,world);
    strcpy(wire.legacy_name_key_hex,"4d336865726f");
    strcpy(wire.legacy_shard,"11");
    strcpy(wire.command_uuid,command);
    wire.writer_epoch=7;
    wire.writer_revision=1;
    wire.expected_state=existing?CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING:
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    wire.storage_format=1;
    if(existing&&(leaf(root,"player/11/M3hero",preimage,sizeof(preimage)-1)||
                  hash(root,preimage,sizeof(preimage)-1,wire.expected_sha256))) return -1;
    return hash(root,bytes,length,wire.post_sha256)||
        character_save_journal_v2_request_sha256(&wire,wire.request_sha256)||
        character_save_journal_v2_prepare(root,&wire,bytes,length)?-1:0;
}

static int prepare(root,command,existing)
const char *root,*command;
int existing;
{
    return prepare_bytes(root,command,existing,payload,sizeof(payload)-1);
}

static int exists(root,relative)
const char *root,*relative;
{ char p[PATH_MAX];struct stat st;return !path(p,sizeof(p),root,relative)&&lstat(p,&st)==0; }

static int published(root,command)
const char *root,*command;
{ char rel[96],p[PATH_MAX],buf[1500];int fd;ssize_t n;if(snprintf(rel,sizeof(rel),"character-save-journal/%s.published",command)<0||path(p,sizeof(p),root,rel))return 0;fd=open(p,O_RDONLY|O_NOFOLLOW);if(fd<0)return 0;n=read(fd,buf,sizeof(buf)-1);close(fd);if(n<0)return 0;buf[n]=0;return strstr(buf,"state=LEGACY_PUBLISHED\n")!=0; }

static int published_temp(root,command)
const char *root,*command;
{
    char relative[96];
    int n=snprintf(relative,sizeof(relative),"character-save-journal/%s.published.tmp",command);
    return n<0||(size_t)n>=sizeof(relative)?0:exists(root,relative);
}

static int prepared(root,command)
const char *root,*command;
{
    char relative[96];
    int n=snprintf(relative,sizeof(relative),"character-save-journal/%s.prepared",command);
    return n<0||(size_t)n>=sizeof(relative)?0:exists(root,relative);
}

static int hash_leaf(root,relative,out)
const char *root,*relative;
char out[65];
{
    char full[PATH_MAX];
    int fd,result;
    if(path(full,sizeof(full),root,relative)) return -1;
    fd=open(full,O_RDONLY|O_NOFOLLOW);
    if(fd<0) return -1;
    result=character_save_journal_v2_hash_fd(fd,out);
    if(close(fd)!=0) result=-1;
    return result;
}

static int stat_leaf(root,relative,out)
const char *root,*relative;
struct stat *out;
{
    char full[PATH_MAX];
    return path(full,sizeof(full),root,relative)||lstat(full,out)!=0?-1:0;
}

static int same_stat(left,right)
const struct stat *left,*right;
{
    return left->st_dev==right->st_dev&&left->st_ino==right->st_ino&&
        left->st_nlink==right->st_nlink&&left->st_size==right->st_size;
}

static int copy_leaf(root,from,to)
const char *root,*from,*to;
{
    char full[PATH_MAX],bytes[1600];
    int fd;
    ssize_t count;
    if(path(full,sizeof(full),root,from)) return -1;
    fd=open(full,O_RDONLY|O_NOFOLLOW);
    if(fd<0) return -1;
    do count=read(fd,bytes,sizeof(bytes)); while(count<0&&errno==EINTR);
    if(close(fd)!=0||count<0||(size_t)count==sizeof(bytes)) return -1;
    return leaf(root,to,bytes,(size_t)count);
}

static int exact_leaf(root,relative,expected,length)
const char *root,*relative,*expected;
size_t length;
{
    char full[PATH_MAX],actual[1600];
    int fd;
    ssize_t count,extra;
    if(length>sizeof(actual)||path(full,sizeof(full),root,relative)) return 0;
    fd=open(full,O_RDONLY|O_NOFOLLOW);
    if(fd<0) return 0;
    do count=read(fd,actual,length); while(count<0&&errno==EINTR);
    do extra=read(fd,actual,1); while(extra<0&&errno==EINTR);
    if(close(fd)!=0) return 0;
    return count==(ssize_t)length&&extra==0&&!memcmp(actual,expected,length);
}

static int read_leaf(root,relative,out,out_size,length)
const char *root,*relative;
char *out;
size_t out_size,*length;
{
    char full[PATH_MAX];
    int fd;
    ssize_t count,extra;
    if(!out||!length||!out_size||path(full,sizeof(full),root,relative)) return -1;
    fd=open(full,O_RDONLY|O_NOFOLLOW);
    if(fd<0) return -1;
    do count=read(fd,out,out_size); while(count<0&&errno==EINTR);
    do extra=read(fd,out,1); while(extra<0&&errno==EINTR);
    if(close(fd)!=0) return -1;
    if(count<0||extra!=0||(size_t)count==out_size) return -1;
    *length=(size_t)count;
    return 0;
}

static int test_publish(root,context)
char *root; character_save_journal_v2_writer_context *context;
{ static const char cmd[]="30000000-0000-0000-0000-000000000001";mock m;int failed=0;memset(&m,0,sizeof(m));if(prepare(root,cmd,0))return 1;failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,cmd)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&m.calls==1,"held writer plus internally bound route must publish exact stage");failed+=expect(!exists(root,"character-save-stage/30000000-0000-0000-0000-000000000001.stage")&&published(root,cmd)&&exists(root,"player/11/M3hero"),"no-replace promotion consumes stage and durable journal follows posthash");m.calls=0;failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,cmd)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&m.calls==1,"published exact retry must be idempotent");return failed; }

static int test_forged_route(root,context)
char *root; character_save_journal_v2_writer_context *context;
{ static const char cmd[]="30000000-0000-0000-0000-000000000002";char p[PATH_MAX];mock m;int failed=0;memset(&m,0,sizeof(m));m.forged=1;if(path(p,sizeof(p),root,"player/11/M3hero")||unlink(p)||prepare(root,cmd,0))return 1;failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,cmd)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY&&m.calls==1,"forgeable callback route fields must not become file authority");failed+=expect(exists(root,"character-save-stage/30000000-0000-0000-0000-000000000002.stage")&&!exists(root,"player/11/M3hero"),"forged route rejection preserves stage and leaves live absent");return failed; }

static int test_root_binding(root_a,root_b,context)
char *root_a,*root_b; character_save_journal_v2_writer_context *context;
{ static const char cmd[]="30000000-0000-0000-0000-000000000003";mock m;int failed=0;(void)root_a;memset(&m,0,sizeof(m));if(prepare(root_b,cmd,0))return 1;failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,cmd)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_JOURNAL,"A-lock writer must not publish command found only under B root");failed+=expect(exists(root_b,"character-save-stage/30000000-0000-0000-0000-000000000003.stage")&&!exists(root_b,"player/11/M3hero"),"B-root evidence remains untouched by A held writer");return failed; }

static int remove_live(root)
char *root;
{
    char p[PATH_MAX];
    return path(p,sizeof(p),root,"player/11/M3hero")||
        (unlink(p)!=0&&errno!=ENOENT)?-1:0;
}

static int test_state_matrix(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char existing[]="30000000-0000-0000-0000-000000000010";
    static const char recovered[]="30000000-0000-0000-0000-000000000011";
    static const char stage_missing[]="30000000-0000-0000-0000-000000000012";
    static const char corrupt[]="30000000-0000-0000-0000-000000000013";
    char p[PATH_MAX];
    mock m;
    int failed=0;
    memset(&m,0,sizeof(m));
    if(prepare(root,existing,1)) return 1;
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,existing)==0&&
        published(root,existing)&&exists(root,"character-save-journal/30000000-0000-0000-0000-000000000010.prepared"),
        "existing exact prehash publishes while immutable PREPARED evidence remains");
    if(remove_live(root)||prepare(root,recovered,0)||
       path(p,sizeof(p),root,"character-save-stage/30000000-0000-0000-0000-000000000011.stage")||unlink(p)||
       leaf(root,"player/11/M3hero",payload,sizeof(payload)-1)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,recovered)==0&&published(root,recovered),
        "consumed stage plus exact live post re-fsyncs parent then publishes recovery state");
    if(remove_live(root)||prepare(root,stage_missing,0)||
       path(p,sizeof(p),root,"character-save-stage/30000000-0000-0000-0000-000000000012.stage")||unlink(p)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,stage_missing)!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        !published(root,stage_missing),"stage missing plus live missing freezes PREPARED evidence");
    if(prepare(root,corrupt,0)||
       leaf(root,"character-save-stage/30000000-0000-0000-0000-000000000013.stage","bad",3)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,corrupt)!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        exists(root,"character-save-stage/30000000-0000-0000-0000-000000000013.stage")&&!exists(root,"player/11/M3hero"),
        "posthash mismatch must preserve stage and never create live leaf");
    return failed;
}

static int test_unsafe_leaf_matrix(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    const char *commands[]={"30000000-0000-0000-0000-000000000020","30000000-0000-0000-0000-000000000021","30000000-0000-0000-0000-000000000022","30000000-0000-0000-0000-000000000023"};
    char p[PATH_MAX],anchor[PATH_MAX];
    mock m;
    int failed=0;
    if(remove_live(root)||prepare(root,commands[0],0)||path(p,sizeof(p),root,"player/11/M3hero")||symlink("nowhere",p)) return 1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,commands[0])!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,"live symlink must reject no-follow publish");
    if(unlink(p)||prepare(root,commands[1],0)||mkfifo(p,0600)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,commands[1])!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,"live FIFO must reject nonblocking regular-file contract");
    if(unlink(p)||prepare(root,commands[2],0)||path(anchor,sizeof(anchor),root,"anchor")||leaf(root,"anchor","x",1)||link(anchor,p)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,commands[2])!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,"hard-linked live leaf must reject single-link contract");
    if(unlink(p)||unlink(anchor)||prepare(root,commands[3],0)||leaf(root,"player/11/M3hero","x",1)||chmod(p,0644)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,commands[3])!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,"wrong-mode live leaf must reject without chmod repair");
    unlink(p);
    return failed;
}

static int test_unsafe_stage_leaf_matrix(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    const char *commands[]={
        "30000000-0000-0000-0000-000000000024",
        "30000000-0000-0000-0000-000000000025",
        "30000000-0000-0000-0000-000000000026"};
    const char *kind[]={
        "stage symlink must reject O_NOFOLLOW without evidence mutation",
        "stage FIFO must reject regular-file contract without evidence mutation",
        "stage hardlink must reject single-link contract without evidence mutation"};
    char stage[PATH_MAX],anchor[PATH_MAX];
    mock m;
    int i,failed=0;

    for(i=0;i<3;i++) {
        if(remove_live(root)||prepare(root,commands[i],0)||
           snprintf(stage,sizeof(stage),
                    "%s/character-save-stage/%s.stage",root,commands[i])<0||
           unlink(stage)!=0) return failed+1;
        if(i==0) {
            if(symlink("nowhere",stage)!=0) return failed+1;
        } else if(i==1) {
            if(mkfifo(stage,0600)!=0) return failed+1;
        } else {
            if(path(anchor,sizeof(anchor),root,"stage-hardlink-anchor")||
               leaf(root,"stage-hardlink-anchor","x",1)||link(anchor,stage)!=0)
                return failed+1;
        }
        memset(&m,0,sizeof(m));
        failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,
            lookup,&m,commands[i])==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_STAGE&&
            exists(root,stage+strlen(root)+1)&&prepared(root,commands[i])&&
            !exists(root,"player/11/M3hero")&&!published(root,commands[i]),kind[i]);
        if(i==2) {
            if(unlink(anchor)!=0) return failed+1;
        }
        if(unlink(stage)!=0) return failed+1;
    }
    return failed;
}

static int test_crash_after_live_promotion(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000050";
    struct stat stage_st,live_st;
    mock m;
    pid_t child;
    int status,failed=0;

    if(remove_live(root)||prepare(root,command,0)||
       character_save_journal_v2_writer_close(context)!=0) return 1;
    child=fork();
    if(child==0) {
        character_save_journal_v2_writer_context child_context;
        memset(&child_context,0,sizeof(child_context));
        memset(&m,0,sizeof(m));
        if(character_save_journal_v2_writer_open(root,world,&child_context)!=0) _exit(2);
        character_save_journal_v2_publish_crash_after_live_promotion_for_test(1);
        (void)character_save_journal_v2_publish(&child_context,name,sizeof(name)-1,
                                                 lookup,&m,command);
        _exit(3);
    }
    if(child<0||waitpid(child,&status,0)!=child) return 1;
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==91,
        "fork child exits 91 immediately after stage-to-live promotion");
    failed+=expect(exists(root,"character-save-stage/30000000-0000-0000-0000-000000000050.stage")&&
        exists(root,"player/11/M3hero")&&
        exists(root,"character-save-journal/30000000-0000-0000-0000-000000000050.prepared")&&
        !published(root,command)&&exact_leaf(root,"player/11/M3hero",payload,sizeof(payload)-1)&&
        exact_leaf(root,"character-save-stage/30000000-0000-0000-0000-000000000050.stage",payload,sizeof(payload)-1)&&
        stat_leaf(root,"character-save-stage/30000000-0000-0000-0000-000000000050.stage",&stage_st)==0&&
        stat_leaf(root,"player/11/M3hero",&live_st)==0&&stage_st.st_nlink==2&&
        live_st.st_nlink==2&&stage_st.st_dev==live_st.st_dev&&stage_st.st_ino==live_st.st_ino,
        "parent observes PREPARED, exact two-name stage/live pair, and no publication marker");
    if(character_save_journal_v2_writer_open(root,world,context)!=0) return failed+1;
    memset(&m,0,sizeof(m));
    character_save_journal_v2_publish_operation_faults_for_test(1,0,0,0);
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,
        lookup,&m,command)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE&&
        !published(root,command)&&prepared(root,command)&&
        stat_leaf(root,"character-save-stage/30000000-0000-0000-0000-000000000050.stage",&stage_st)==0&&
        stat_leaf(root,"player/11/M3hero",&live_st)==0&&stage_st.st_nlink==2&&
        live_st.st_nlink==2&&stage_st.st_dev==live_st.st_dev&&stage_st.st_ino==live_st.st_ino,
        "first recovery destination fsync fault preserves the exact two-name pair");
    memset(&m,0,sizeof(m));
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,
        lookup,&m,command)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&published(root,command)&&
        exists(root,"character-save-journal/30000000-0000-0000-0000-000000000050.prepared"),
        "retry fsyncs recovered live parent and writes immutable published marker");
    return failed;
}

static int test_growing_stage_rejected(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000051";
    unsigned char *bytes;
    char stage[PATH_MAX],signal;
    mock m;
    pid_t child;
    int ready[2],release[2],fd,status,failed=0;

    bytes=(unsigned char *)malloc(CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES);
    if(!bytes) return 1;
    memset(bytes,'G',CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES);
    if(remove_live(root)||prepare_bytes(root,command,0,bytes,
            CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES)||
       snprintf(stage,sizeof(stage),"%s/character-save-stage/%s.stage",root,command)<0||
       pipe(ready)!=0||pipe(release)!=0) {
        free(bytes);
        return 1;
    }
    child=fork();
    if(child==0) {
        close(ready[1]);
        close(release[0]);
        if(read_exact_fd(ready[0],&signal)!=0) _exit(2);
        fd=open(stage,O_WRONLY|O_APPEND|O_NOFOLLOW);
        if(fd<0||write_exact_fd(fd,'!')!=0||close(fd)!=0) _exit(3);
        if(write_exact_fd(release[1],'x')!=0) _exit(4);
        _exit(0);
    }
    if(child<0) {
        close(ready[0]);
        close(ready[1]);
        close(release[0]);
        close(release[1]);
        free(bytes);
        return 1;
    }
    close(ready[0]);
    close(release[1]);
    memset(&m,0,sizeof(m));
    character_save_journal_v2_pause_hash_after_fstat_for_test(ready[1],release[0]);
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,
        lookup,&m,command)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_STAGE,
        "64 MiB stage growth during hash is rejected before any live promotion");
    character_save_journal_v2_pause_hash_after_fstat_for_test(-1,-1);
    close(ready[1]);
    close(release[0]);
    if(waitpid(child,&status,0)!=child) {
        free(bytes);
        return failed+1;
    }
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0&&prepared(root,command)&&
        exists(root,"character-save-stage/30000000-0000-0000-0000-000000000051.stage")&&
        !exists(root,"player/11/M3hero")&&!published(root,command),
        "pipe-coordinated growth preserves PREPARED and creates no live or published evidence");
    free(bytes);
    return failed;
}

static int test_absent_leaf_creation_race(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000053";
    static const char sentinel[]="sentinel written after absent precondition\n";
    char digest[65], expected[65], signal;
    int ready[2],release[2],status,failed=0;
    pid_t child;
    mock m;
    if(remove_live(root)||prepare(root,command,0)||pipe(ready)!=0||pipe(release)!=0) return 1;
    child=fork();
    if(child==0) {
        close(ready[1]); close(release[0]);
        if(read_exact_fd(ready[0],&signal)!=0 ||
           leaf(root,"player/11/M3hero",sentinel,sizeof(sentinel)-1) ||
           write_exact_fd(release[1],'x')!=0) _exit(2);
        _exit(0);
    }
    if(child<0) return 1;
    close(ready[0]); close(release[1]);
    memset(&m,0,sizeof(m));
    character_save_journal_v2_publish_pause_before_absent_link_for_test(ready[1],release[0]);
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,
        command)!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "absent publish must reject a leaf created after its precondition read");
    character_save_journal_v2_publish_pause_before_absent_link_for_test(-1,-1);
    close(ready[1]); close(release[0]);
    if(waitpid(child,&status,0)!=child) return failed+1;
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0&&
        hash(root,sentinel,sizeof(sentinel)-1,expected)==0&&
        hash_leaf(root,"player/11/M3hero",digest)==0&&!strcmp(expected,digest)&&
        prepared(root,command)&&
        exists(root,"character-save-stage/30000000-0000-0000-0000-000000000053.stage")&&
        !published(root,command),
        "creation race preserves byte-exact sentinel, PREPARED, stage, and no marker");
    return failed;
}

static int test_conflicting_marker_temp_fails_closed(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000054";
    static const char partial[]="partial marker evidence\n";
    char relative[96], expected[65], actual[65];
    mock m;
    int failed=0;
    if(remove_live(root)||prepare(root,command,0)||
       snprintf(relative,sizeof(relative),"character-save-journal/%s.published.tmp",command)<0||
       leaf(root,relative,partial,sizeof(partial)-1)||
       hash(root,partial,sizeof(partial)-1,expected)) return 1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,
        command)!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        hash_leaf(root,relative,actual)==0&&!strcmp(expected,actual)&&prepared(root,command)&&
        exists(root,"character-save-stage/30000000-0000-0000-0000-000000000054.stage")&&
        !exists(root,"player/11/M3hero")&&!published(root,command),
        "partial marker temp fails closed and remains byte-exactly untouched");
    return failed;
}

static int test_marker_temp_link_count_fails_closed(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000056";
    char target[96],temporary[96],alias[96],marker[1600];
    size_t marker_length;
    struct stat target_before,temp_before,alias_before;
    struct stat target_after,temp_after,alias_after;
    char full_temp[PATH_MAX],full_alias[PATH_MAX];
    mock m;
    int failed=0;
    memset(&m,0,sizeof(m));
    if(remove_live(root)||prepare(root,command,0)) return 1;
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)==0,
        "link-count fixture publishes exact marker");
    if(failed) return failed;
    if(snprintf(target,sizeof(target),"character-save-journal/%s.published",command)<0||
       snprintf(temporary,sizeof(temporary),"character-save-journal/%s.published.tmp",command)<0||
       snprintf(alias,sizeof(alias),"character-save-journal/%s.published.tmp.alias",command)<0||
       copy_leaf(root,target,temporary)||path(full_temp,sizeof(full_temp),root,temporary)||
       path(full_alias,sizeof(full_alias),root,alias)||link(full_temp,full_alias)!=0||
       read_leaf(root,target,marker,sizeof(marker),&marker_length)||
       !exact_leaf(root,temporary,marker,marker_length)||
       !exact_leaf(root,alias,marker,marker_length)||stat_leaf(root,target,&target_before)||
       stat_leaf(root,temporary,&temp_before)||stat_leaf(root,alias,&alias_before)) return 1;
    failed+=expect(target_before.st_nlink==1&&temp_before.st_nlink==2&&alias_before.st_nlink==2&&
        temp_before.st_dev==alias_before.st_dev&&temp_before.st_ino==alias_before.st_ino&&
        character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)!=
            CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        exact_leaf(root,target,marker,marker_length)&&
        exact_leaf(root,temporary,marker,marker_length)&&
        exact_leaf(root,alias,marker,marker_length)&&stat_leaf(root,target,&target_after)==0&&
        stat_leaf(root,temporary,&temp_after)==0&&stat_leaf(root,alias,&alias_after)==0&&
        same_stat(&target_before,&target_after)&&same_stat(&temp_before,&temp_after)&&
        same_stat(&alias_before,&alias_after),
        "target nlink1 plus temp alias nlink2 fails closed without changing any marker name");
    return failed;
}

static int test_marker_target_alias_fails_closed(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-00000000005c";
    char target[96],alias[96],marker[1600];
    size_t marker_length;
    struct stat target_before,alias_before,target_after,alias_after;
    char full_target[PATH_MAX],full_alias[PATH_MAX];
    mock m;
    int failed=0;
    memset(&m,0,sizeof(m));
    if(remove_live(root)||prepare(root,command,0)||
       character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)!=0||
       snprintf(target,sizeof(target),"character-save-journal/%s.published",command)<0||
       snprintf(alias,sizeof(alias),"character-save-journal/%s.published.alias",command)<0||
       path(full_target,sizeof(full_target),root,target)||
       path(full_alias,sizeof(full_alias),root,alias)||link(full_target,full_alias)!=0||
       read_leaf(root,target,marker,sizeof(marker),&marker_length)||
       !exact_leaf(root,alias,marker,marker_length)||stat_leaf(root,target,&target_before)||
       stat_leaf(root,alias,&alias_before)) return 1;
    memset(&m,0,sizeof(m));
    failed+=expect(target_before.st_nlink==2&&alias_before.st_nlink==2&&
        target_before.st_dev==alias_before.st_dev&&target_before.st_ino==alias_before.st_ino&&
        !published_temp(root,command)&&
        character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)!=
            CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK&&
        prepared(root,command)&&exists(root,"player/11/M3hero")&&
        exact_leaf(root,target,marker,marker_length)&&
        exact_leaf(root,alias,marker,marker_length)&&stat_leaf(root,target,&target_after)==0&&
        stat_leaf(root,alias,&alias_after)==0&&same_stat(&target_before,&target_after)&&
        same_stat(&alias_before,&alias_after),
        "unpaired target hardlink fails closed without changing completed evidence");
    return failed;
}

static int test_exact_duplicate_marker_temp_reconciles(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000057";
    char target[96],temporary[96];
    mock m;
    int failed=0;
    memset(&m,0,sizeof(m));
    if(remove_live(root)||prepare(root,command,0)||
       character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)!=0||
       snprintf(target,sizeof(target),"character-save-journal/%s.published",command)<0||
       snprintf(temporary,sizeof(temporary),"character-save-journal/%s.published.tmp",command)<0||
       copy_leaf(root,target,temporary)) return 1;
    memset(&m,0,sizeof(m));
    failed+=expect(published_temp(root,command)&&
        character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)==0&&
        published(root,command)&&!published_temp(root,command),
        "exact single-link duplicate marker temp is removed only by verified reconciliation");
    return failed;
}

static int test_corrupt_absent_link_pair_fails_closed(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-00000000005d";
    static const char corrupt[]="corrupt shared absent-link evidence\n";
    char stage[96],live[96],saved[1600];
    size_t saved_length;
    struct stat stage_before,live_before,stage_after,live_after;
    char full_stage[PATH_MAX],full_live[PATH_MAX];
    mock m;
    int failed=0;
    memset(&m,0,sizeof(m));
    if(remove_live(root)||prepare(root,command,0)||
       snprintf(stage,sizeof(stage),"character-save-stage/%s.stage",command)<0||
       snprintf(live,sizeof(live),"player/11/M3hero")<0||
       path(full_stage,sizeof(full_stage),root,stage)||path(full_live,sizeof(full_live),root,live)||
       link(full_stage,full_live)!=0||leaf(root,stage,corrupt,sizeof(corrupt)-1)||
       read_leaf(root,stage,saved,sizeof(saved),&saved_length)||
       !exact_leaf(root,live,saved,saved_length)||stat_leaf(root,stage,&stage_before)||
       stat_leaf(root,live,&live_before)) return 1;
    memset(&m,0,sizeof(m));
    failed+=expect(stage_before.st_nlink==2&&live_before.st_nlink==2&&
        stage_before.st_dev==live_before.st_dev&&stage_before.st_ino==live_before.st_ino&&
        character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command)==
            CHARACTER_SAVE_JOURNAL_V2_PUBLISH_LIVE&&prepared(root,command)&&!published(root,command)&&
        exact_leaf(root,stage,saved,saved_length)&&exact_leaf(root,live,saved,saved_length)&&
        stat_leaf(root,stage,&stage_after)==0&&stat_leaf(root,live,&live_after)==0&&
        same_stat(&stage_before,&stage_after)&&same_stat(&live_before,&live_after),
        "corrupt exact absent-link pair is hashed before cleanup and remains intact");
    return failed;
}

static int test_marker_destination_creation_race(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000055";
    static const char sentinel[]="competing immutable marker bytes\n";
    char relative[96], expected[65], actual[65], signal;
    int ready[2],release[2],status,failed=0;
    pid_t child;
    mock m;
    if(remove_live(root)||prepare(root,command,0)||pipe(ready)!=0||pipe(release)!=0||
       snprintf(relative,sizeof(relative),"character-save-journal/%s.published",command)<0||
       hash(root,sentinel,sizeof(sentinel)-1,expected)) return 1;
    child=fork();
    if(child==0) {
        close(ready[1]); close(release[0]);
        if(read_exact_fd(ready[0],&signal)!=0||
           leaf(root,relative,sentinel,sizeof(sentinel)-1)||write_exact_fd(release[1],'x')!=0)
            _exit(2);
        _exit(0);
    }
    if(child<0) return 1;
    close(ready[0]); close(release[1]);
    memset(&m,0,sizeof(m));
    character_save_journal_v2_publish_pause_before_marker_link_for_test(ready[1],release[0]);
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,
        command)!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK,
        "marker promotion must reject a target created after earlier reads");
    character_save_journal_v2_publish_pause_before_marker_link_for_test(-1,-1);
    close(ready[1]); close(release[0]);
    if(waitpid(child,&status,0)!=child) return failed+1;
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0&&
        hash_leaf(root,relative,actual)==0&&!strcmp(expected,actual)&&prepared(root,command)&&
        published_temp(root,command)&&!published(root,command),
        "marker destination race preserves conflicting target and exact retry temp evidence");
    return failed;
}

static int test_route_generation_change_rejected(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char command[]="30000000-0000-0000-0000-000000000052";
    mock m;
    int failed=0;

    if(remove_live(root)||prepare(root,command,0)) return 1;
    memset(&m,0,sizeof(m));
    m.reopen_writer=1;
    m.root=root;
    m.context=context;
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,
        lookup,&m,command)==CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IDENTITY&&m.calls==1,
        "route callback writer close/reopen invalidates its held generation snapshot");
    failed+=expect(prepared(root,command)&&
        exists(root,"character-save-stage/30000000-0000-0000-0000-000000000052.stage")&&
        !exists(root,"player/11/M3hero")&&!published(root,command),
        "stale route snapshot leaves stage, live, and journal evidence unchanged");
    return failed;
}

static int publish_with_mock(context,command)
character_save_journal_v2_writer_context *context;
const char *command;
{
    mock m;
    memset(&m,0,sizeof(m));
    return character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,command);
}

static int test_durability_fault_matrix(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    const char *commands[]={
        "30000000-0000-0000-0000-000000000030",
        "30000000-0000-0000-0000-000000000031",
        "30000000-0000-0000-0000-000000000032",
        "30000000-0000-0000-0000-000000000033",
        "30000000-0000-0000-0000-000000000034",
        "30000000-0000-0000-0000-000000000035",
        "30000000-0000-0000-0000-000000000036",
        "30000000-0000-0000-0000-000000000037",
        "30000000-0000-0000-0000-000000000038",
        "30000000-0000-0000-0000-000000000039",
        "30000000-0000-0000-0000-00000000003a",
        "30000000-0000-0000-0000-00000000003b",
        "30000000-0000-0000-0000-00000000003c",
        "30000000-0000-0000-0000-00000000003d"};
    int failed=0;
    if(remove_live(root)||prepare(root,commands[0],0)) return 1;
    character_save_journal_v2_publish_faults_for_test(1,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[0])==0,"journal EINTR write retries");
    if(remove_live(root)||prepare(root,commands[1],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,1,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[1])==0,"journal short write retries");
    if(remove_live(root)||prepare(root,commands[2],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,1,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[2])!=0&&!published(root,commands[2]),"journal zero write freezes PREPARED");
    if(remove_live(root)||prepare(root,commands[3],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,1,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[3])!=0&&!published(root,commands[3]),"journal EIO write freezes PREPARED");
    if(remove_live(root)||prepare(root,commands[4],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,1,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[4])!=0&&!published(root,commands[4]),"live-parent fsync failure cannot publish in same attempt");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[4])==0&&published(root,commands[4]),"retry reconciles interrupted two-link live promotion");
    if(remove_live(root)||prepare(root,commands[5],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,1,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[5])!=0&&!published(root,commands[5]),"live no-replace promotion failure preserves stage and journal");
    if(remove_live(root)||prepare(root,commands[6],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,1,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[6])!=0&&!published(root,commands[6])&&published_temp(root,commands[6]),"journal file fsync failure leaves exact reusable temp evidence");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[6])==0&&published(root,commands[6])&&!published_temp(root,commands[6]),"retry fsyncs and reuses exact temp after file fsync failure");
    if(remove_live(root)||prepare(root,commands[7],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,1,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[7])!=0&&!published(root,commands[7])&&published_temp(root,commands[7]),"journal no-replace promotion failure leaves temporary evidence");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[7])==0&&published(root,commands[7])&&!published_temp(root,commands[7]),"retry reuses exact temp after final promotion fault");
    if(remove_live(root)||prepare(root,commands[8],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,1,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[8])!=0&&published(root,commands[8]),"journal-parent fsync ambiguity returns failure without cleanup");
    if(remove_live(root)||prepare(root,commands[9],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,1,0,0,0);
    failed+=expect(publish_with_mock(context,commands[9])!=0&&!published(root,commands[9]),"journal read close failure rejects before mutation");
    if(remove_live(root)||prepare(root,commands[10],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,1,0,0);
    failed+=expect(publish_with_mock(context,commands[10])!=0&&!published(root,commands[10]),"stage close failure rejects before live promotion");
    if(remove_live(root)||prepare(root,commands[11],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,1,0);
    failed+=expect(publish_with_mock(context,commands[11])!=0&&!published(root,commands[11]),"live reopen close failure is ambiguous and unpromoted");
    if(remove_live(root)||prepare(root,commands[12],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,1);
    failed+=expect(publish_with_mock(context,commands[12])!=0&&!published(root,commands[12])&&published_temp(root,commands[12]),"journal temp close ambiguity leaves exact evidence");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[12])==0&&published(root,commands[12])&&!published_temp(root,commands[12]),"retry fsyncs and reuses exact temp after close ambiguity");
    if(remove_live(root)||prepare(root,commands[13],0)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,commands[13])==0,"fault reset permits a fresh exact publish");
    return failed;
}

static int test_operation_fault_retries(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char stage_unlink[]="30000000-0000-0000-0000-000000000058";
    static const char stage_fsync[]="30000000-0000-0000-0000-000000000059";
    static const char marker_unlink[]="30000000-0000-0000-0000-00000000005a";
    static const char existing_journal[]="30000000-0000-0000-0000-00000000005b";
    char stage_rel[96],target_rel[96],temp_rel[96];
    struct stat stage_st,live_st,target_st,temp_st;
    int failed=0;

    if(remove_live(root)||prepare(root,stage_unlink,0)||
       snprintf(stage_rel,sizeof(stage_rel),"character-save-stage/%s.stage",stage_unlink)<0)
        return 1;
    character_save_journal_v2_publish_operation_faults_for_test(0,0,1,0);
    failed+=expect(publish_with_mock(context,stage_unlink)!=0&&!published(root,stage_unlink)&&
        stat_leaf(root,stage_rel,&stage_st)==0&&stat_leaf(root,"player/11/M3hero",&live_st)==0&&
        stage_st.st_nlink==2&&live_st.st_nlink==2&&stage_st.st_dev==live_st.st_dev&&
        stage_st.st_ino==live_st.st_ino,
        "stage unlink fault preserves the durable exact two-name promotion pair");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,stage_unlink)==0&&published(root,stage_unlink)&&
        !exists(root,stage_rel),"stage unlink retry reconciles without overwriting live");

    if(remove_live(root)||prepare(root,stage_fsync,0)||
       snprintf(stage_rel,sizeof(stage_rel),"character-save-stage/%s.stage",stage_fsync)<0)
        return failed+1;
    character_save_journal_v2_publish_operation_faults_for_test(0,1,0,0);
    failed+=expect(publish_with_mock(context,stage_fsync)!=0&&!published(root,stage_fsync)&&
        !exists(root,stage_rel)&&exists(root,"player/11/M3hero")&&
        exact_leaf(root,"player/11/M3hero",payload,sizeof(payload)-1),
        "stage-parent fsync fault after unlink retains the already durable exact live name");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,stage_fsync)==0&&published(root,stage_fsync),
        "stage-parent fsync retry converges from consumed stage evidence");

    if(remove_live(root)||prepare(root,marker_unlink,0)||
       snprintf(target_rel,sizeof(target_rel),"character-save-journal/%s.published",marker_unlink)<0||
       snprintf(temp_rel,sizeof(temp_rel),"character-save-journal/%s.published.tmp",marker_unlink)<0)
        return failed+1;
    character_save_journal_v2_publish_operation_faults_for_test(0,0,0,1);
    failed+=expect(publish_with_mock(context,marker_unlink)!=0&&published(root,marker_unlink)&&
        published_temp(root,marker_unlink)&&stat_leaf(root,target_rel,&target_st)==0&&
        stat_leaf(root,temp_rel,&temp_st)==0&&target_st.st_nlink==2&&temp_st.st_nlink==2&&
        target_st.st_dev==temp_st.st_dev&&target_st.st_ino==temp_st.st_ino,
        "journal-temp unlink fault after target link preserves exact transactional marker pair");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,marker_unlink)==0&&published(root,marker_unlink)&&
        !published_temp(root,marker_unlink),
        "journal-temp unlink retry verifies then durably reconciles the marker pair");

    if(remove_live(root)||prepare(root,existing_journal,1)) return failed+1;
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,1,0,0,0,0);
    failed+=expect(publish_with_mock(context,existing_journal)!=0&&published(root,existing_journal)&&
        published_temp(root,existing_journal),
        "existing-state journal-parent ambiguity retains marker evidence for recovery");
    character_save_journal_v2_publish_faults_for_test(0,0,0,0,0,0,0,0,0,0,0,0,0);
    failed+=expect(publish_with_mock(context,existing_journal)==0&&published(root,existing_journal)&&
        !published_temp(root,existing_journal),
        "existing-state journal-parent ambiguity retry converges without replacement retry");
    return failed;
}

static int test_writer_handle_matrix(root,context)
char *root;
character_save_journal_v2_writer_context *context;
{
    static const char cmd[]="30000000-0000-0000-0000-000000000040";
    static const char epoch8[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=8\n";
    static const char epoch7[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
    character_save_journal_v2_writer_context copied,zero,garbage;
    mock m;
    pid_t child;
    int status,failed=0;
    if(remove_live(root)||prepare(root,cmd,0)) return 1;
    copied=*context;
    memset(&zero,0,sizeof(zero));
    memset(&garbage,0xa5,sizeof(garbage));
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(&copied,name,sizeof(name)-1,lookup,&m,cmd)!=0&&m.calls==0,
        "copied held-writer bytes cannot reach route or mutate");
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(&zero,name,sizeof(name)-1,lookup,&m,cmd)!=0&&m.calls==0,
        "zero writer handle cannot reach route or mutate");
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(&garbage,name,sizeof(name)-1,lookup,&m,cmd)!=0&&m.calls==0,
        "garbage writer handle cannot reach route or mutate");
    child=fork();
    if(child==0) {
        mock child_mock;
        memset(&child_mock,0,sizeof(child_mock));
        _exit(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&child_mock,cmd)!=0&&
              child_mock.calls==0?0:1);
    }
    if(child<0||waitpid(child,&status,0)!=child) return failed+1;
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0,"fork child cannot publish inherited writer handle");
    if(leaf(root,"character-save-journal/writer-epoch.v2",epoch8,sizeof(epoch8)-1)) return failed+1;
    memset(&m,0,sizeof(m));
    failed+=expect(character_save_journal_v2_publish(context,name,sizeof(name)-1,lookup,&m,cmd)!=0&&m.calls==0,
        "stale persisted writer epoch cannot publish");
    if(leaf(root,"character-save-journal/writer-epoch.v2",epoch7,sizeof(epoch7)-1)) return failed+1;
    return failed;
}

int main(void)
{
    char a[PATH_MAX],b[PATH_MAX];
    character_save_journal_v2_writer_context context;
    int failed;
    if(!realpath("/tmp",a)||strlen(a)+48>=sizeof(a)||!realpath("/tmp",b)||
       strlen(b)+48>=sizeof(b)) return 1;
    strcat(a,"/character-save-journal-v2-publish-a-XXXXXX");
    strcat(b,"/character-save-journal-v2-publish-b-XXXXXX");
    if(!mkdtemp(a)||!mkdtemp(b)||fixture(a)||fixture(b)) return 1;
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    if(character_save_journal_v2_writer_open(a,world,&context)) return 1;
    failed=test_publish(a,&context)+test_forged_route(a,&context)+
        test_root_binding(a,b,&context)+test_state_matrix(a,&context)+
        test_unsafe_leaf_matrix(a,&context)+test_unsafe_stage_leaf_matrix(a,&context)+
        test_crash_after_live_promotion(a,&context)+test_growing_stage_rejected(a,&context)+
        test_absent_leaf_creation_race(a,&context)+
        test_conflicting_marker_temp_fails_closed(a,&context)+
        test_marker_temp_link_count_fails_closed(a,&context)+
        test_marker_target_alias_fails_closed(a,&context)+
        test_exact_duplicate_marker_temp_reconciles(a,&context)+
        test_corrupt_absent_link_pair_fails_closed(a,&context)+
        test_marker_destination_creation_race(a,&context)+
        test_route_generation_change_rejected(a,&context)+
        test_durability_fault_matrix(a,&context)+test_operation_fault_retries(a,&context)+
        test_writer_handle_matrix(a,&context);
    failed+=expect(character_save_journal_v2_writer_close(&context)==0,"writer close");
    failed+=expect(teardown(a)==0&&teardown(b)==0,"fixture teardown");
    puts(failed?"character_save_journal_v2_publish_test: failed":
         "character_save_journal_v2_publish_test: ok");
    return failed?1:0;
}
