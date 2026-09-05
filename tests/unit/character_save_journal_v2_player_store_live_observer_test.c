/*
 * Covers the normal live PlayerStore -> held-v3 path.  It uses the production
 * writer bootstrap, live-ops adapter, RPC transport, and absent-head bootstrap
 * with a deterministic in-process wire fixture; no service or database runs.
 */
#include "character_save_journal_v2_player_store.h"
#include "character_save_journal_v2.h"
#include "mstruct.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char world[] = "m3-live-player-store";
static const char instance[] = "11111111-1111-4111-8111-111111111111";
static const char character[] = "33333333-3333-4333-8333-333333333333";
static const char command[] = "20000000-0000-4000-8000-000000000002";
static const unsigned char name[] = "M3alpha";
static const unsigned char payload[] = "live-player-store-record";
static const char acquire_deadline[] = "2026-09-03T00:00:00Z";
static const char save_deadline[] = "2026-09-03T00:02:00Z";

typedef enum wire_call {
    WIRE_NONE, WIRE_ASSERT, WIRE_ACQUIRE, WIRE_RENEW, WIRE_ROUTE,
    WIRE_SEED, WIRE_RECEIPT
} wire_call;

typedef struct wire_fixture {
    int connection_ok, transaction_ok, valid, seeded;
    int assert_calls, acquire_calls, renew_calls, route_calls, seed_calls;
    int receipt_calls, clear_calls, close_calls;
    wire_call current_call;
} wire_fixture;

typedef struct fixture {
    char root[PATH_MAX];
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_player_store store;
    character_save_journal_v2_live_ops live_ops;
    character_save_journal_v2_rpc_transport transport;
    player_record_serializer_limits limits;
    creature player;
    char buffer[256];
    wire_fixture wire;
    int serializer_calls, observer_calls, prepared_at_observer;
    int receipt_calls_at_observer;
    int receipt_after_observer;
} fixture;

static fixture *current;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "character_save_journal_v2_player_store_live_observer: %s\n",
        message);
    return 1;
}

static int join_path(char *output, size_t capacity, const char *root,
    const char *relative)
{
    int length=snprintf(output,capacity,"%s/%s",root,relative);
    return length<0||(size_t)length>=capacity ? -1 : 0;
}

static int make_directory(const char *root, const char *relative)
{
    char path[PATH_MAX];
    return join_path(path,sizeof(path),root,relative)||mkdir(path,0700) ? -1 : 0;
}

static int remove_tree(const char *path)
{
    DIR *directory;
    struct dirent *entry;
    struct stat status;
    char child[PATH_MAX];

    if(lstat(path,&status)) return errno==ENOENT ? 0 : -1;
    if(!S_ISDIR(status.st_mode)) return unlink(path);
    directory=opendir(path);
    if(!directory) return -1;
    while((entry=readdir(directory))) {
        if(!strcmp(entry->d_name,".")||!strcmp(entry->d_name,"..")) continue;
        if(snprintf(child,sizeof(child),"%s/%s",path,entry->d_name)<0||
           remove_tree(child)) {
            (void)closedir(directory);
            return -1;
        }
    }
    return closedir(directory)||rmdir(path) ? -1 : 0;
}

static int exists(const char *root, const char *relative)
{
    char path[PATH_MAX];
    struct stat status;
    return !join_path(path,sizeof(path),root,relative)&&!lstat(path,&status);
}

static int command_exists(const char *root, const char *suffix)
{
    char relative[128];
    int length=snprintf(relative,sizeof(relative),
        "character-save-journal/%s.%s",command,suffix);
    return length>=0&&(size_t)length<sizeof(relative)&&exists(root,relative);
}

static int setup_root(fixture *test)
{
    char template_path[PATH_MAX], temporary_root[PATH_MAX];
    int length;

    if(!realpath("/tmp",temporary_root)) return -1;
    length=snprintf(template_path,sizeof(template_path),
        "%s/muhan-v2-live-player-store-XXXXXX",temporary_root);
    if(length<0||(size_t)length>=sizeof(template_path)||!mkdtemp(template_path))
        return -1;
    strcpy(test->root,template_path);
    return make_directory(test->root,"player")||
        make_directory(test->root,"player/66")||
        make_directory(test->root,"character-save-stage")||
        make_directory(test->root,"character-save-journal");
}

static int text_equal(const char *actual, const char *expected)
{ return actual&&expected&&!strcmp(actual,expected); }

static int wire_values(const char *const *values, int count,
    const char *const *expected)
{
    int index;
    if(!values||!expected) return 0;
    for(index=0;index<count;index++)
        if(!text_equal(values[index],expected[index])) return 0;
    return 1;
}

static int wire_connection_ok(void *connection)
{ return connection&&((wire_fixture *)connection)->connection_ok; }

static int wire_transaction_ok(void *connection)
{ return connection&&((wire_fixture *)connection)->transaction_ok; }

static void *wire_exec(void *connection, const char *sql, int count,
    const unsigned int *types, const char *const *values, const int *lengths,
    const int *formats, int result_format)
{
    wire_fixture *wire=(wire_fixture *)connection;
    static const char *const acquire_values[]={world,instance,acquire_deadline};
    static const char *const renew_values[]={world,instance,"7",save_deadline};
    static const char *const route_values[]={world,(const char *)name};
    static const char *const seed_values[]={world,(const char *)name,character,
        instance,"7","1"};

    (void)types;
    (void)lengths;
    (void)formats;
    (void)result_format;
    if(!wire||!sql) return 0;
    wire->current_call=WIRE_NONE;
    if(strstr(sql,"m3_assert_writer_session")) {
        wire->current_call=WIRE_ASSERT;
        wire->assert_calls++;
        if(count) wire->valid=0;
    } else if(strstr(sql,"acquire_game_world_writer_epoch")) {
        wire->current_call=WIRE_ACQUIRE;
        wire->acquire_calls++;
        if(count!=3||!wire_values(values,count,acquire_values)) wire->valid=0;
    } else if(strstr(sql,"renew_game_world_writer_epoch")) {
        wire->current_call=WIRE_RENEW;
        wire->renew_calls++;
        if(count!=4||!wire_values(values,count,renew_values)) wire->valid=0;
    } else if(strstr(sql,"resolve_game_character_writer_route_v3")) {
        wire->current_call=WIRE_ROUTE;
        wire->route_calls++;
        if(count!=2||!wire_values(values,count,route_values)) wire->valid=0;
    } else if(strstr(sql,"seed_game_character_absent_head")) {
        wire->current_call=WIRE_SEED;
        wire->seed_calls++;
        if(count!=6||wire->seeded||!wire_values(values,count,seed_values))
            wire->valid=0;
        wire->seeded=1;
    } else if(strstr(sql,"record_legacy_published_receipt")) {
        wire->current_call=WIRE_RECEIPT;
        wire->receipt_calls++;
        if(count!=12||!values||!text_equal(values[0],world)||
           !text_equal(values[1],(const char *)name)||
           !text_equal(values[2],character)||!text_equal(values[3],command)||
           !text_equal(values[4],instance)||!text_equal(values[6],"7")||
           !text_equal(values[7],"1")||!text_equal(values[8],"absent")||
           values[9]||!text_equal(values[11],"1")) wire->valid=0;
    } else {
        wire->valid=0;
    }
    return wire;
}

static int wire_status(void *result)
{ return result&&((wire_fixture *)result)->valid ? 1 : 2; }

static int wire_rows(void *result)
{ return result ? 1 : 0; }

static int wire_columns(void *result)
{
    wire_fixture *wire=(wire_fixture *)result;
    if(!wire) return 0;
    return wire->current_call==WIRE_ROUTE ? 10 :
        ((wire->current_call==WIRE_ACQUIRE||wire->current_call==WIRE_RENEW) ?
        2 : 1);
}

static const char *wire_value(void *result, int row, int column)
{
    wire_fixture *wire=(wire_fixture *)result;
    static const char *const route_uninitialized[]={world,character,"M3alpha",
        "66","1","active","","uninitialized","","0"};
    static const char *const route_absent[]={world,character,"M3alpha",
        "66","1","active","","absent","","0"};
    static const char *const epoch[]={"7","2026-09-03T00:01:00Z"};

    if(!wire||row) return 0;
    if(wire->current_call==WIRE_ASSERT) return column==0 ? "t" : 0;
    if(wire->current_call==WIRE_ACQUIRE||wire->current_call==WIRE_RENEW)
        return column>=0&&column<2 ? epoch[column] : 0;
    if(wire->current_call==WIRE_ROUTE)
        return column>=0&&column<10 ?
            (wire->seeded ? route_absent[column] : route_uninitialized[column]) : 0;
    return column==0 ? "" : 0;
}

static int wire_value_length(void *result, int row, int column)
{
    const char *value=wire_value(result,row,column);
    return value ? (int)strlen(value) : -1;
}

static const char *wire_sqlstate(void *result)
{ (void)result; return 0; }

static void wire_clear(void *result)
{ if(result) ((wire_fixture *)result)->clear_calls++; }

static void wire_close(void *connection)
{ if(connection) ((wire_fixture *)connection)->close_calls++; }

static const character_save_journal_v2_rpc_transport_operations wire_operations={
    wire_connection_ok,wire_transaction_ok,wire_exec,wire_status,wire_rows,
    wire_columns,wire_value,wire_value_length,wire_sqlstate,wire_clear,wire_close
};

int player_record_serialize_bounded(creature *player, char perm_only,
    char *buffer, unsigned long capacity, unsigned long *written,
    const player_record_serializer_limits *limits)
{
    fixture *test=current;
    if(!test||player!=&test->player||perm_only||!buffer||!written||!limits||
       capacity<sizeof(payload)) return PLAYER_RECORD_SERIALIZER_INVALID;
    test->serializer_calls++;
    memcpy(buffer,payload,sizeof(payload)-1);
    *written=sizeof(payload)-1;
    return PLAYER_RECORD_SERIALIZER_OK;
}

static int deadline(void *opaque, char output[64])
{
    if(opaque!=current) return -1;
    strcpy(output,save_deadline);
    return 0;
}

static int command_uuid(void *opaque, char output[37])
{
    if(opaque!=current) return -1;
    strcpy(output,command);
    return 0;
}

static int load(void *opaque, char *legacy_name, creature **player)
{
    (void)legacy_name;
    if(opaque!=current||!player) return PLAYER_STORE_IO_ERROR;
    *player=0;
    return PLAYER_STORE_NOT_FOUND;
}

static int fail_after_durable_prepared(void *opaque,
    const character_save_journal_v2_writer_context *writer,
    const char *command_uuid_value)
{
    fixture *test=(fixture *)opaque;
    if(test!=current||writer!=&test->writer||strcmp(command_uuid_value,command))
        return -99;
    test->observer_calls++;
    test->receipt_calls_at_observer=test->wire.receipt_calls;
    test->prepared_at_observer=command_exists(test->root,"prepared")&&
        !command_exists(test->root,"published")&&!command_exists(test->root,"acked")&&
        !exists(test->root,"player/66/M3alpha");
    return -73;
}

static int test_live_player_store_observer_failure_is_diagnostic(void)
{
    fixture test;
    character_save_journal_v2_writer_tuple held;
    int failed=0, save_result;

    memset(&test,0,sizeof(test));
    current=&test;
    if(setup_root(&test)) {
        fprintf(stderr,"character_save_journal_v2_player_store_live_observer: root setup failed\n");
        return 1;
    }
    test.wire.connection_ok=1;
    test.wire.transaction_ok=1;
    test.wire.valid=1;
    character_save_journal_v2_rpc_transport_init(&test.transport);
    failed+=expect(character_save_journal_v2_rpc_transport_start(&test.transport,
        &wire_operations,&test.wire,&test.wire)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,
        "the deterministic wire fixture starts a real ready transport");
    character_save_journal_v2_live_ops_init(&test.live_ops,&test.transport,
        acquire_deadline);
    failed+=expect(!character_save_journal_v2_writer_bootstrap(test.root,world,
        instance,character_save_journal_v2_live_ops_writer_epoch_acquire,
        &test.live_ops,&test.writer) && test.wire.acquire_calls==1,
        "production writer bootstrap acquires and persists legitimate held authority");
    test.live_ops.acquire_lease_expires_at=0;
    memset(&held,0,sizeof(held));
    failed+=expect(character_save_journal_v2_writer_validate_held(&test.writer,&held)==
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK&&!strcmp(held.world_id,world)&&
        !strcmp(held.writer_instance_id,instance)&&held.writer_epoch==7,
        "the live save receives a validated held writer tuple");
    test.limits.max_depth=64;
    test.limits.max_objects=8192;
    strcpy(test.player.name,(const char *)name);
    character_save_journal_v2_player_store_init(&test.store,&test.writer,
        &test.live_ops,test.buffer,sizeof(test.buffer),&test.limits,deadline,
        &test,command_uuid,&test,load,&test);
    failed+=expect(character_save_journal_v2_player_store_set_stage_observer(
        &test.store,fail_after_durable_prepared,&test)==0,
        "the live PlayerStore accepts its configured diagnostic observer");
    save_result=character_save_journal_v2_player_store_save(&test.store,
        (char *)name,&test.player);
    test.receipt_after_observer=test.wire.receipt_calls==1&&test.observer_calls==1&&
        command_exists(test.root,"published");
    failed+=expect(save_result==PLAYER_STORE_OK,
        "the seeded held-v3 save returns PlayerStore OK");
    failed+=expect(test.wire.valid,
        "the production adapters send the exact expected wire requests");
    failed+=expect(test.wire.acquire_calls==1&&test.wire.renew_calls==1,
        "writer bootstrap acquisition and save renewal each occur once");
    failed+=expect(test.wire.route_calls==4,
        "initial uninitialized route, exact rebind, held-v3 route, and publish rebind all occur");
    failed+=expect(test.wire.seed_calls==1,
        "the real absent-head bootstrap uses exactly one seed");
    failed+=expect(test.serializer_calls==1&&test.observer_calls==1&&
        test.prepared_at_observer&&test.receipt_calls_at_observer==0&&
        test.wire.receipt_calls==1&&
        test.receipt_after_observer,
        "the observer fails only after PREPARED and before the receipt");
    failed+=expect(test.store.last_report.snapshot_attempted==1&&
        test.store.last_report.snapshot_result==-73&&
        test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED&&
        test.store.last_report.ack_result==CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED,
        "observer failure is retained only as a diagnostic after ACK");
    failed+=expect(command_exists(test.root,"published")&&
        command_exists(test.root,"acked")&&exists(test.root,"player/66/M3alpha")&&
        test.store.state==CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE&&
        !test.store.buffer_length&&!test.store.active_player,
        "post-PREPARED observer failure leaves published legacy evidence and PlayerStore OK state");
    if(character_save_journal_v2_writer_close(&test.writer)) failed++;
    character_save_journal_v2_rpc_transport_close(&test.transport);
    failed+=expect(test.wire.close_calls==1,
        "the real transport closes its fixture connection exactly once");
    if(remove_tree(test.root)) failed++;
    current=0;
    return failed;
}

int main(void)
{
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    return test_live_player_store_observer_failure_is_diagnostic();
}
