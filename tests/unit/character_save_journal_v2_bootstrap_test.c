#include "character_save_journal_v2_bootstrap.h"
#include "character_save_journal_v2.h"

#include <dirent.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef enum bootstrap_mode {
    BOOTSTRAP_EXISTING, BOOTSTRAP_PRESENT, BOOTSTRAP_REJECTED,
    BOOTSTRAP_DEFERRED, BOOTSTRAP_DRIFT, BOOTSTRAP_SUCCESS,
    BOOTSTRAP_BAD_TREE, BOOTSTRAP_BAD_SHARD, BOOTSTRAP_BAD_FORMAT,
    BOOTSTRAP_TUPLE_MISMATCH, BOOTSTRAP_STALE,
    BOOTSTRAP_IMPORTED_UNCLAIMED, BOOTSTRAP_PROVISIONING,
    BOOTSTRAP_BAD_LIFECYCLE
} bootstrap_mode;

#define BOOTSTRAP_TEST_ROOT_CAP 128

typedef struct fixture {
    /* The fixture root is deliberately short so each derived test path fits
     * PATH_MAX independently.  This keeps the GCC -Wformat-truncation build
     * contract explicit instead of relying on a chained snprintf proof. */
    char root[BOOTSTRAP_TEST_ROOT_CAP];
    int root_fd, route_calls, validate_calls, seed_calls;
    bootstrap_mode mode;
} fixture;

static fixture *current;

static int expect(int yes, const char *what)
{ if(yes) return 0; fprintf(stderr,"character_save_journal_v2_bootstrap: %s\n",what); return 1; }

static void route_reply(character_save_journal_v2_bound_route_v3 *out,
    character_save_journal_v2_route_head_state head)
{
    memset(out,0,sizeof(*out));
    strcpy(out->world_id,"m3-world");
    strcpy(out->character_id,"92000000-0000-0000-0000-000000000001");
    memcpy(out->legacy_name,"M3hero",6); out->legacy_name_length=6;
    strcpy(out->legacy_shard,"11"); out->storage_format=1;
    out->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    if(current->mode==BOOTSTRAP_IMPORTED_UNCLAIMED)
        out->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED;
    if(current->mode==BOOTSTRAP_PROVISIONING)
        out->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING;
    if(current->mode==BOOTSTRAP_BAD_LIFECYCLE)
        out->lifecycle=(character_save_journal_v2_route_lifecycle)99;
    out->head_state=head;
}

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_dup_held_root_fd(
    const character_save_journal_v2_writer_context *writer, int *out)
{
    (void)writer;
    if(!current||!out) return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
    *out=fcntl(current->root_fd,F_DUPFD_CLOEXEC,0);
    return *out<0 ? CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID :
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK;
}

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_writer_tuple *out)
{
    (void)writer;
    if(!current||!out) return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;
    current->validate_calls++;
    if(current->mode==BOOTSTRAP_STALE&&current->validate_calls==2)
        return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE;
    memset(out,0,sizeof(*out)); strcpy(out->world_id,"m3-world");
    if(current->mode==BOOTSTRAP_TUPLE_MISMATCH)
        strcpy(out->world_id,"other-world");
    strcpy(out->writer_instance_id,"94000000-0000-0000-0000-000000000001");
    out->writer_epoch=7;
    return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK;
}

character_save_journal_v2_route_error character_save_journal_v2_route_bind_v3(
    const character_save_journal_v2_writer_context *writer,
    const unsigned char *name, size_t length,
    character_save_journal_v2_route_lookup_v3 lookup, void *opaque,
    character_save_journal_v2_bound_route_v3 *out)
{
    (void)writer;(void)lookup;(void)opaque;
    if(!current||!out||length!=6||memcmp(name,"M3hero",6))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT;
    current->route_calls++;
    if(current->route_calls==1) {
        route_reply(out,current->mode==BOOTSTRAP_EXISTING ?
            CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING :
            CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED);
        if(current->mode==BOOTSTRAP_BAD_SHARD) strcpy(out->legacy_shard,"22");
        if(current->mode==BOOTSTRAP_BAD_FORMAT) out->storage_format=2;
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK;
    }
    route_reply(out,CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT);
    if(current->mode==BOOTSTRAP_DRIFT)
        strcpy(out->character_id,"92000000-0000-0000-0000-000000000002");
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_live_ops_seed_absent_head(
    character_save_journal_v2_live_ops *ops,
    const character_save_journal_v2_writer_tuple *tuple,
    const character_save_journal_v2_bound_route_v3 *route)
{
    (void)ops;
    if(!current||!tuple||!route||current->route_calls!=1||
       current->validate_calls!=2||strcmp(tuple->world_id,"m3-world")||
       tuple->writer_epoch!=7||route->head_state!=
       CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED)
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    current->seed_calls++;
    return current->mode==BOOTSTRAP_REJECTED ?
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED :
        (current->mode==BOOTSTRAP_DEFERRED ?
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED :
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK);
}

character_save_journal_v2_route_lookup_result
character_save_journal_v2_live_ops_route_lookup_v3(void *opaque,
    const char *world, const unsigned char *name, size_t length,
    character_save_journal_v2_route_reply_v3 *reply)
{ (void)opaque;(void)world;(void)name;(void)length;(void)reply; return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE; }

static int setup(fixture *test, bootstrap_mode mode, int create_live)
{
    char player[PATH_MAX], shard[PATH_MAX], journal[PATH_MAX], stage[PATH_MAX], live[PATH_MAX];
    int descriptor;

    memset(test,0,sizeof(*test)); current=test; test->root_fd=-1; test->mode=mode;
    strcpy(test->root,"/tmp/m3-bootstrap.XXXXXX");
    if(!mkdtemp(test->root)||chmod(test->root,0700) ||
       snprintf(player,sizeof(player),"%s/player",test->root)>=(int)sizeof(player) ||
       snprintf(shard,sizeof(shard),"%s/player/11",test->root)>=(int)sizeof(shard) ||
       snprintf(journal,sizeof(journal),"%s/character-save-journal",test->root)>=(int)sizeof(journal) ||
       snprintf(stage,sizeof(stage),"%s/character-save-stage",test->root)>=(int)sizeof(stage) ||
       mkdir(player,0700)||mkdir(shard,0700)||mkdir(journal,0700)||mkdir(stage,0700)) return -1;
    if(mode==BOOTSTRAP_BAD_TREE&&chmod(stage,0755)) return -1;
    if(create_live) {
        if(snprintf(live,sizeof(live),"%s/player/11/M3hero",test->root)>=(int)sizeof(live)) return -1;
        descriptor=open(live,O_WRONLY|O_CREAT|O_EXCL|O_CLOEXEC,0600);
        if(descriptor<0||close(descriptor)) return -1;
    }
    test->root_fd=open(test->root,O_RDONLY|O_DIRECTORY|O_CLOEXEC);
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    return test->root_fd<0 ? -1 : 0;
}

static void teardown(fixture *test)
{
    char player[PATH_MAX], shard[PATH_MAX], journal[PATH_MAX], stage[PATH_MAX], live[PATH_MAX];
    if(test->root_fd>=0) (void)close(test->root_fd);
    snprintf(player,sizeof(player),"%s/player",test->root);
    snprintf(shard,sizeof(shard),"%s/player/11",test->root);
    snprintf(journal,sizeof(journal),"%s/character-save-journal",test->root);
    snprintf(stage,sizeof(stage),"%s/character-save-stage",test->root);
    snprintf(live,sizeof(live),"%s/player/11/M3hero",test->root);
    (void)unlink(live); (void)chmod(stage,0700); (void)rmdir(stage); (void)rmdir(journal);
    (void)rmdir(shard); (void)rmdir(player); (void)rmdir(test->root);
    current=0;
}

static int directory_empty(const char *path)
{
    DIR *directory;
    struct dirent *entry;
    int empty=1;

    directory=opendir(path);
    if(!directory) return 0;
    while((entry=readdir(directory))!=0)
        if(strcmp(entry->d_name,".")&&strcmp(entry->d_name,"..")) {
            empty=0;
            break;
        }
    if(closedir(directory)) empty=0;
    return empty;
}

static int no_local_artifacts(const fixture *test)
{
    char journal[PATH_MAX], stage[PATH_MAX];

    return snprintf(journal,sizeof(journal),"%s/character-save-journal",test->root)<
           (int)sizeof(journal) &&
        snprintf(stage,sizeof(stage),"%s/character-save-stage",test->root)<
           (int)sizeof(stage) && directory_empty(journal) && directory_empty(stage);
}

static int run_case(bootstrap_mode mode, int present, int expected,
    int routes, int seeds, const char *what)
{
    fixture test; character_save_journal_v2_writer_context writer;
    character_save_journal_v2_live_ops ops; int failed;
    if(setup(&test,mode,present)) return 1;
    memset(&writer,0,sizeof(writer)); memset(&ops,0,sizeof(ops));
    failed=expect(character_save_journal_v2_bootstrap_absent_head(&writer,&ops,
        (const unsigned char *)"M3hero",6)==expected&&test.route_calls==routes&&
        test.seed_calls==seeds&&no_local_artifacts(&test),what);
    teardown(&test);
    return failed;
}

int main(void)
{
    return run_case(BOOTSTRAP_EXISTING,1,0,1,0,
        "actual existing canonical file skips seed") |
        run_case(BOOTSTRAP_PRESENT,1,-1,1,0,
        "uninitialized route with actual present file never seeds") |
        run_case(BOOTSTRAP_REJECTED,0,-1,1,1,
        "rejected seed leaves route unbound and no local mutation") |
        run_case(BOOTSTRAP_DEFERRED,0,-1,1,1,
        "deferred seed leaves route unbound and no local mutation") |
        run_case(BOOTSTRAP_BAD_TREE,0,-1,1,0,
        "untrusted tree component fails before seed") |
        run_case(BOOTSTRAP_BAD_SHARD,0,-1,1,0,
        "route shard not matching canonical name fails before seed") |
        run_case(BOOTSTRAP_BAD_FORMAT,0,-1,1,0,
        "route storage format mismatch fails before seed") |
        run_case(BOOTSTRAP_TUPLE_MISMATCH,0,-1,1,0,
        "held writer tuple/route identity mismatch fails before seed") |
        run_case(BOOTSTRAP_STALE,0,-1,1,0,
        "writer stale after absent observation fails before seed") |
        run_case(BOOTSTRAP_IMPORTED_UNCLAIMED,0,0,2,1,
        "imported-unclaimed route with no local imported evidence seeds and rebinds") |
        run_case(BOOTSTRAP_PROVISIONING,0,0,2,1,
        "provisioning route seeds and rebinds") |
        run_case(BOOTSTRAP_BAD_LIFECYCLE,0,-1,1,0,
        "invalid lifecycle fails closed before seed") |
        run_case(BOOTSTRAP_DRIFT,0,-1,2,1,
        "seed success with identity drift fails exact rebind") |
        run_case(BOOTSTRAP_SUCCESS,0,0,2,1,
        "actual absent file seeds once then rebinds absent revision zero");
}
