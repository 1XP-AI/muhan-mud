#include "character_save_journal_v2_route.h"

#include <errno.h>
#include <dirent.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

static const char world_id[] = "m3-contract";
static const char character_one[] = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
static const char character_two[] = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

typedef struct route_mock {
    int calls;
    character_save_journal_v2_route_lookup_result result;
    int input_exact;
    const char *expected_world;
    const unsigned char *expected_name;
    size_t expected_name_length;
    character_save_journal_v2_route_reply reply;
    character_save_journal_v2_writer_context *owner_to_change;
    const char *reopen_root;
    int owner_action;
    int close_result;
    int reopen_result;
} route_mock;

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr,"character_save_journal_v2_route_test: %s\n",message);
    return 1;
}

static int join(out, out_size, left, right)
char *out;
size_t out_size;
const char *left;
const char *right;
{
    int n=snprintf(out,out_size,"%s/%s",left,right);
    return n<0||(size_t)n>=out_size?-1:0;
}

static int write_all(fd, bytes, length)
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

static int write_leaf(root, leaf, bytes, length)
const char *root;
const char *leaf;
const void *bytes;
size_t length;
{
    char path[PATH_MAX];
    int fd;
    int result=0;
    if(join(path,sizeof(path),root,leaf)!=0) return -1;
    fd=open(path,O_WRONLY|O_CREAT|O_TRUNC|O_NOFOLLOW,0600);
    if(fd<0) return -1;
    if(fchmod(fd,0600)!=0||write_all(fd,bytes,length)!=0||fsync(fd)!=0) result=-1;
    if(close(fd)!=0) result=-1;
    return result;
}

static int fixture(root)
char *root;
{
    static const char instance[]="version=2\nkind=writer-instance\nwriter_instance_id=11111111-1111-4111-8111-111111111111\n";
    static const char epoch[]="version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=7\n";
    char journal[PATH_MAX];
    if(join(journal,sizeof(journal),root,"character-save-journal")!=0||mkdir(journal,0700)!=0) return -1;
    return write_leaf(journal,"writer-instance.v2",instance,sizeof(instance)-1)||
           write_leaf(journal,"writer-epoch.v2",epoch,sizeof(epoch)-1)?-1:0;
}

static int replace_epoch(root, epoch)
const char *root;
unsigned int epoch;
{
    char journal[PATH_MAX];
    char bytes[256];
    int n;
    if(join(journal,sizeof(journal),root,"character-save-journal")!=0) return -1;
    n=snprintf(bytes,sizeof(bytes),"version=2\nkind=writer-epoch\nworld_id=m3-contract\nwriter_instance_id=11111111-1111-4111-8111-111111111111\nwriter_epoch=%u\n",epoch);
    return n<0||(size_t)n>=sizeof(bytes)?-1:write_leaf(journal,"writer-epoch.v2",bytes,(size_t)n);
}

static int teardown(root)
const char *root;
{
    char journal[PATH_MAX];
    char path[PATH_MAX];
    const char *leaves[]={".m3-writer.lock","writer-instance.v2","writer-epoch.v2"};
    size_t i;
    if(join(journal,sizeof(journal),root,"character-save-journal")!=0) return -1;
    for(i=0;i<sizeof(leaves)/sizeof(leaves[0]);i++) {
        if(join(path,sizeof(path),journal,leaves[i])!=0) return -1;
        (void)unlink(path);
    }
    return rmdir(journal)==0&&rmdir(root)==0?0:-1;
}

static int has_only_writer_fixture(root)
const char *root;
{
    DIR *directory;
    struct dirent *entry;
    char journal[PATH_MAX];
    int root_count=0;
    int journal_count=0;
    if(!(directory=opendir(root))) return 0;
    while((entry=readdir(directory))!=0) {
        if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,"..")) continue;
        if(strcmp(entry->d_name,"character-save-journal")) {
            closedir(directory);
            return 0;
        }
        root_count++;
    }
    if(closedir(directory)!=0||root_count!=1||
       join(journal,sizeof(journal),root,"character-save-journal")!=0||
       !(directory=opendir(journal))) return 0;
    while((entry=readdir(directory))!=0) {
        if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,"..")) continue;
        if(strcmp(entry->d_name,".m3-writer.lock")&&
           strcmp(entry->d_name,"writer-instance.v2")&&
           strcmp(entry->d_name,"writer-epoch.v2")) {
            closedir(directory);
            return 0;
        }
        journal_count++;
    }
    return closedir(directory)==0&&journal_count==3;
}

static character_save_journal_v2_route_lookup_result
lookup(opaque, callback_world, name, name_length, reply)
void *opaque;
const char *callback_world;
const unsigned char *name;
size_t name_length;
character_save_journal_v2_route_reply *reply;
{
    route_mock *mock=(route_mock *)opaque;
    mock->calls++;
    if(strcmp(callback_world,mock->expected_world)!=0||name!=mock->expected_name||
       name_length!=mock->expected_name_length||strcmp(callback_world,world_id)!=0) mock->input_exact=0;
    *reply=mock->reply;
    if(mock->owner_action==1) {
        memset(mock->owner_to_change,0x5a,sizeof(*mock->owner_to_change));
    } else if(mock->owner_action==2) {
        mock->close_result=character_save_journal_v2_writer_close(mock->owner_to_change);
    } else if(mock->owner_action==3) {
        mock->close_result=character_save_journal_v2_writer_close(mock->owner_to_change);
        mock->reopen_result=character_save_journal_v2_writer_open(mock->reopen_root,
                                                                    world_id,
                                                                    mock->owner_to_change);
    }
    return mock->result;
}

static void valid_reply(mock, name, name_length, shard, character_id)
route_mock *mock;
const unsigned char *name;
size_t name_length;
const char *shard;
const char *character_id;
{
    memset(mock,0,sizeof(*mock));
    mock->input_exact=1;
    mock->expected_world=world_id;
    mock->expected_name=name;
    mock->expected_name_length=name_length;
    mock->reply.status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    mock->result=CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
    mock->reply.row_count=1;
    strcpy(mock->reply.world_id,world_id);
    strcpy(mock->reply.character_id,character_id);
    memcpy(mock->reply.legacy_name,name,name_length);
    mock->reply.legacy_name_length=name_length;
    strcpy(mock->reply.legacy_shard,shard);
    mock->reply.storage_format=CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    mock->reply.lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
}

static int unchanged(before, after)
const character_save_journal_v2_bound_route *before;
const character_save_journal_v2_bound_route *after;
{
    return memcmp(before,after,sizeof(*before))==0;
}

static int test_bind_and_same_shard(root)
char *root;
{
    static const unsigned char first[]="M300001";
    static const unsigned char second[]="M300025";
    static const character_save_journal_v2_route_lifecycle lifecycles[]={
        CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED,
        CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING,
        CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE
    };
    character_save_journal_v2_writer_context context;
    character_save_journal_v2_bound_route one,two;
    route_mock mock;
    unsigned int i;
    int failed=0;
    if(character_save_journal_v2_writer_open(root,world_id,&context)!=0) return 1;
    valid_reply(&mock,first,sizeof(first)-1,"4d",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,first,sizeof(first)-1,lookup,&mock,&one)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK&&mock.calls==1&&mock.input_exact,"held context must call lookup once with exact persisted world and raw caller bytes");
    failed+=expect(!strcmp(one.world_id,world_id)&&!strcmp(one.character_id,character_one)&&one.legacy_name_length==sizeof(first)-1&&!memcmp(one.legacy_name,first,sizeof(first)-1)&&!strcmp(one.legacy_shard,"4d")&&one.storage_format==1&&one.lifecycle==CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE,"valid route reply must bind immutable exact values");
    valid_reply(&mock,second,sizeof(second)-1,"4d",character_two);
    mock.reply.has_imported_file_sha256=1;
    memset(mock.reply.imported_file_sha256,'a',64);
    mock.reply.imported_file_sha256[64]=0;
    failed+=expect(character_save_journal_v2_route_bind(&context,second,sizeof(second)-1,lookup,&mock,&two)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK&&mock.calls==1&&!strcmp(two.character_id,character_two)&&strcmp(one.character_id,two.character_id),"same shard callback IDs must remain distinct callback-supplied character IDs");
    failed+=expect(two.has_imported_file_sha256&&two.imported_file_sha256[0]=='a'&&two.imported_file_sha256[63]=='a',"optional canonical imported hash must copy only after reply validation");
    for(i=0;i<sizeof(lifecycles)/sizeof(lifecycles[0]);i++) {
        valid_reply(&mock,first,sizeof(first)-1,"4d",character_one);
        mock.reply.lifecycle=lifecycles[i];
        failed+=expect(character_save_journal_v2_route_bind(&context,first,
                                                             sizeof(first)-1,
                                                             lookup,&mock,&one)==
                       CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK&&mock.calls==1&&
                       one.lifecycle==lifecycles[i],
                       "every allowed lifecycle must bind positively");
    }
    failed+=expect(character_save_journal_v2_writer_close(&context)==0,"held context must close after binding");
    return failed;
}

static int expect_reply_rejection(context, name, name_length, mock, expected_error, label)
const character_save_journal_v2_writer_context *context;
const unsigned char *name;
size_t name_length;
route_mock *mock;
character_save_journal_v2_route_error expected_error;
const char *label;
{
    character_save_journal_v2_bound_route before,out;
    int failed=0;
    memset(&before,0xa5,sizeof(before));
    out=before;
    failed+=expect(character_save_journal_v2_route_bind(context,name,name_length,lookup,mock,&out)==expected_error&&mock->calls==1&&unchanged(&before,&out),label);
    return failed;
}

static int test_rejections_and_immutable_output(root)
char *root;
{
    static const unsigned char name[]="M3hero";
    static const unsigned char embedded[]={'M','3',0,'h'};
    static const unsigned char invalid_utf8[]={'M',0xc0,0xaf};
    static const unsigned char lower[]="m3hero";
    static const unsigned char overlong[]="M3abcdefghijklmn";
    character_save_journal_v2_writer_context context;
    character_save_journal_v2_bound_route before,out;
    route_mock mock;
    int failed=0;
    if(character_save_journal_v2_writer_open(root,world_id,&context)!=0) return 1;
    memset(&before,0xa5,sizeof(before)); out=before;
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(0,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID&&
                   mock.calls==0&&unchanged(&before,&out),
                   "NULL context must reject before callback and preserve output");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,0,sizeof(name)-1,
                                                         lookup,&mock,&out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&
                   mock.calls==0&&unchanged(&before,&out),
                   "NULL name must reject before callback and preserve output");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,name,0,lookup,
                                                         &mock,&out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&
                   mock.calls==0&&unchanged(&before,&out),
                   "zero-length name must reject before callback and preserve output");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,name,
                                                         sizeof(name)-1,0,&mock,
                                                         &out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&
                   mock.calls==0&&unchanged(&before,&out),
                   "NULL lookup must reject before callback and preserve output");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,name,
                                                         sizeof(name)-1,lookup,
                                                         &mock,0)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&
                   mock.calls==0,
                   "NULL output must reject before callback");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_REJECTED;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS,"nonexact callback status must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.status=(character_save_journal_v2_route_callback_status)99;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS,"unknown callback status must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.result=CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_FAILURE,"callback transport failure must reject unchanged after exactly one call");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.result=(character_save_journal_v2_route_lookup_result)99;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_FAILURE,"unknown lookup enum must reject unchanged after exactly one call");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.row_count=2;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_CARDINALITY,"nonexact callback row count must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    strcpy(mock.reply.world_id,"other-world");
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_WORLD,"reply world mismatch must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.world_id[sizeof(world_id)]='x';
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_WORLD,"reply world trailing bytes must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11","AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA");
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_CHARACTER_ID,"noncanonical callback UUID must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.legacy_name[0]='X';
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_NAME,"callback name mismatch must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"aa",character_one);
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_SHARD,"non-derived callback shard must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.storage_format=2;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_FORMAT,"non-v1 storage format must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.lifecycle=(character_save_journal_v2_route_lifecycle)99;
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_LIFECYCLE,"unallowed lifecycle must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.has_imported_file_sha256=1;
    strcpy(mock.reply.imported_file_sha256,"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA");
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_IMPORTED_HASH,"noncanonical imported hash must reject unchanged");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.reply.imported_file_sha256[3]='x';
    failed+=expect_reply_rejection(&context,name,sizeof(name)-1,&mock,CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_IMPORTED_HASH,"absent imported hash with trailing bytes must reject unchanged");
    memset(&before,0xa5,sizeof(before)); out=before;
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,embedded,sizeof(embedded),lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&mock.calls==0&&unchanged(&before,&out),"embedded NUL input must reject before callback");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,invalid_utf8,sizeof(invalid_utf8),lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&mock.calls==0&&unchanged(&before,&out),"invalid UTF-8 input must reject before callback");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,lower,sizeof(lower)-1,lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&mock.calls==0&&unchanged(&before,&out),"noncanonical input must reject before callback");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&context,overlong,sizeof(overlong)-1,lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT&&mock.calls==0&&unchanged(&before,&out),"overlong input must reject before callback");
    failed+=expect(character_save_journal_v2_writer_close(&context)==0,"rejection context must close");
    return failed;
}

static int test_stale_tuple_and_lock_loser(root)
char *root;
{
    static const unsigned char name[]="M3hero";
    character_save_journal_v2_writer_context held,loser;
    character_save_journal_v2_bound_route before,out;
    route_mock mock;
    pid_t child;
    int status;
    int failed=0;
    if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) return 1;
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    memset(&before,0xa5,sizeof(before)); out=before;
    if(replace_epoch(root,8)!=0) return 1;
    failed+=expect(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_STALE&&mock.calls==0&&unchanged(&before,&out),"replaced persisted tuple must reject before callback and preserve output");
    if(replace_epoch(root,7)!=0) return failed+1;
    child=fork();
    if(child==0) {
        route_mock child_mock;
        character_save_journal_v2_bound_route child_out;
        valid_reply(&child_mock,name,sizeof(name)-1,"11",character_one);
        if(character_save_journal_v2_writer_open(root,world_id,&loser)==0) _exit(2);
        if(character_save_journal_v2_route_bind(&loser,name,sizeof(name)-1,lookup,&child_mock,&child_out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK||child_mock.calls!=0) _exit(3);
        _exit(0);
    }
    if(child<0||waitpid(child,&status,0)!=child) return failed+1;
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0,"lock loser must finish at writer_open and perform no bind callback");
    failed+=expect(has_only_writer_fixture(root),"lock loser must leave no stage, journal, route-cache, or other artifact");
    {
        char journal[PATH_MAX];
        char lock[PATH_MAX];
        if(join(journal,sizeof(journal),root,"character-save-journal")||
           join(lock,sizeof(lock),journal,".m3-writer.lock")||unlink(lock)!=0||
           write_leaf(journal,".m3-writer.lock","x",1)!=0) return failed+1;
        valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
        memset(&before,0xa5,sizeof(before)); out=before;
        failed+=expect(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_LOCK&&mock.calls==0&&unchanged(&before,&out),"replaced lock descriptor must reject before callback and preserve output");
    }
    failed+=expect(character_save_journal_v2_writer_close(&held)==0,"held context must close after lock loser test");
    return failed;
}

/* The earlier RED supplied raw fds, tuple, and lock flag without writer_open;
 * the opaque API now makes the nearest available forgery a copied handle. */
static int test_owner_only_handles(root)
char *root;
{
    static const unsigned char name[]="M3hero";
    character_save_journal_v2_writer_context held,copied,zero,garbage;
    character_save_journal_v2_bound_route before,out;
    route_mock mock;
    pid_t child;
    int status;
    int failed=0;
    if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) return 1;
    copied=held;
    memset(&zero,0,sizeof(zero));
    memset(&garbage,0xa5,sizeof(garbage));
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    memset(&before,0xa5,sizeof(before)); out=before;
    failed+=expect(character_save_journal_v2_route_bind(&copied,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID&&
                   mock.calls==0&&unchanged(&before,&out),
                   "copied handle must not reach callback");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    memset(&before,0xa5,sizeof(before)); out=before;
    failed+=expect(character_save_journal_v2_route_bind(&zero,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID&&
                   mock.calls==0&&unchanged(&before,&out),
                   "zero handle must not reach callback");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    memset(&before,0xa5,sizeof(before)); out=before;
    failed+=expect(character_save_journal_v2_route_bind(&garbage,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID&&
                   mock.calls==0&&unchanged(&before,&out),
                   "garbage handle must not dereference or reach callback");
    child=fork();
    if(child==0) {
        route_mock child_mock;
        character_save_journal_v2_bound_route child_before,child_out;
        valid_reply(&child_mock,name,sizeof(name)-1,"11",character_one);
        memset(&child_before,0xa5,sizeof(child_before)); child_out=child_before;
        if(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,lookup,
                                                &child_mock,&child_out)!=
           CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID||
           child_mock.calls!=0||!unchanged(&child_before,&child_out)||
           character_save_journal_v2_writer_close(&held)==0) _exit(2);
        _exit(0);
    }
    if(child<0||waitpid(child,&status,0)!=child) return failed+1;
    failed+=expect(WIFEXITED(status)&&WEXITSTATUS(status)==0,
                   "fork-after-open child must not bind or close parent handle");
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    failed+=expect(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK&&
                   mock.calls==1,
                   "original owner must continue binding after copied and child rejection");
    failed+=expect(character_save_journal_v2_writer_close(&copied)<0&&
                   character_save_journal_v2_writer_close(&zero)<0&&
                   character_save_journal_v2_writer_close(&garbage)<0&&
                   character_save_journal_v2_writer_close(&held)==0,
                   "foreign closes must not consume the exact active owner");
    return failed;
}

static int test_callback_revalidates_exact_held_owner(root)
char *root;
{
    static const unsigned char name[]="M3hero";
    character_save_journal_v2_writer_context held;
    character_save_journal_v2_bound_route before,out;
    route_mock mock;
    int failed=0;
    if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) return 1;
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.owner_to_change=&held;
    mock.owner_action=1;
    failed+=expect(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK&&mock.calls==1&&
                   character_save_journal_v2_writer_close(&held)==0,
                   "callback-only handle byte corruption must not revoke exact owner cleanup");
    if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) return failed+1;
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.owner_to_change=&held;
    mock.owner_action=2;
    memset(&before,0xa5,sizeof(before)); out=before;
    failed+=expect(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID&&mock.calls==1&&
                   mock.close_result==0&&unchanged(&before,&out)&&
                   character_save_journal_v2_writer_close(&held)<0,
                   "callback exact-owner close must reject its reply and leave output unchanged");
    if(character_save_journal_v2_writer_open(root,world_id,&held)!=0) return failed+1;
    valid_reply(&mock,name,sizeof(name)-1,"11",character_one);
    mock.owner_to_change=&held;
    mock.reopen_root=root;
    mock.owner_action=3;
    memset(&before,0xa5,sizeof(before)); out=before;
    failed+=expect(character_save_journal_v2_route_bind(&held,name,sizeof(name)-1,
                                                         lookup,&mock,&out)==
                   CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID&&mock.calls==1&&
                   mock.close_result==0&&mock.reopen_result==0&&
                   unchanged(&before,&out)&&
                   character_save_journal_v2_writer_close(&held)==0,
                   "callback close plus same-address reopen must reject the stale held reply");
    return failed;
}

int main(void)
{
    char root[PATH_MAX];
    int failed;
    if(!realpath("/tmp",root)||strlen(root)+strlen("/character-save-journal-v2-route-test-XXXXXX")>=sizeof(root)) return 1;
    strcat(root,"/character-save-journal-v2-route-test-XXXXXX");
    if(!mkdtemp(root)||fixture(root)!=0) return 1;
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    failed=test_bind_and_same_shard(root)+test_rejections_and_immutable_output(root)+test_stale_tuple_and_lock_loser(root)+test_owner_only_handles(root)+test_callback_revalidates_exact_held_owner(root);
    failed+=expect(teardown(root)==0,"fixture teardown must remove only exact writer leaves");
    puts(failed?"character_save_journal_v2_route_test: failed":"character_save_journal_v2_route_test: ok");
    return failed?1:0;
}
