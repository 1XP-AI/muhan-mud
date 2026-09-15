/*
 * RED-first M3 save boundary: exercise the legacy save_all_ply() ->
 * savegame() -> save_ply() dispatch against the real journal PlayerStore.
 * Every external edge is an injected in-process fixture; the only writes are
 * to this test's mkdtemp root.
 */
#ifndef USE_M3_RUNTIME
#error "this test must compile in the M3 runtime graph"
#endif

#include "character_save_journal_v2.h"
#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_live_ops.h"
#include "character_save_journal_v2_player_store.h"
#include "character_save_journal_v2_process_owner.h"
#include "character_save_journal_v2_publish.h"
#include "character_save_journal_v2_rpc_transport.h"
#include "character_save_journal_v2_runtime.h"
#include "character_save_journal_v2_writer.h"
#include "character_player_snapshot_v1_handoff.h"
#include "mstruct.h"
#include "mextern.h"
#include "player_store.h"

#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char world[]="m3-save-all-shadow";
static const char instance[]="11111111-1111-4111-8111-111111111111";
static const char character[]="33333333-3333-4333-8333-333333333333";
static const char journal_command[]="20000000-0000-4000-8000-000000000002";
static const char player_name[]="M3alpha";
static const char acquire_deadline[]="2026-09-03T00:00:00Z";
static const char save_deadline[]="2026-09-03T00:02:00Z";
static const char serialized[]="save-all-journal-record";

typedef struct environment_fixture {
    const char *mode;
    const char *root;
    const char *conninfo_path;
} environment_fixture;

typedef struct secret_fixture {
    int reads;
    const char *bytes;
    size_t length;
} secret_fixture;

typedef enum wire_call {
    WIRE_NONE, WIRE_ASSERT, WIRE_ACQUIRE, WIRE_RENEW, WIRE_ROUTE, WIRE_SEED,
    WIRE_RECEIPT
} wire_call;

typedef struct wire_fixture {
    int valid;
    int acquire_calls;
    int renew_calls;
    int route_calls;
    int seed_calls;
    int receipt_calls;
    int close_calls;
    int seeded;
    wire_call current_call;
} wire_fixture;

typedef struct shadow_fixture {
    char root[PATH_MAX];
    wire_fixture wire;
    character_save_journal_v2_rpc_transport transport;
    character_save_journal_v2_process_owner owner;
    character_player_snapshot_v1_handoff handoff;
    player_record_serializer_limits limits;
    char buffer[256];
    int start_calls;
    int shutdown_calls;
    int serializer_calls;
    int uuid_calls;
    int deadline_calls;
} shadow_fixture;

static int legacy_save_calls;
static int merror_calls;
static shadow_fixture *active_shadow;

void merror(message,kind)
char *message;
char kind;
{
    (void)message;
    (void)kind;
    merror_calls++;
}

void add_obj_crt(obj_ptr,ply_ptr)
object *obj_ptr;
creature *ply_ptr;
{ (void)obj_ptr; (void)ply_ptr; }

void del_obj_crt(obj_ptr,ply_ptr)
object *obj_ptr;
creature *ply_ptr;
{ (void)obj_ptr; (void)ply_ptr; }

void print(fd,text)
int fd;
char *text;
{ (void)fd; (void)text; }

int file_player_store_save(name,player)
char *name;
creature *player;
{
    legacy_save_calls++;
    return name&&player&&!strcmp(name,player_name)&&!strcmp(player->name,player_name) ?
        PLAYER_STORE_OK : PLAYER_STORE_IO_ERROR;
}

int file_player_store_load(name,player)
char *name;
creature **player;
{
    (void)name;
    if(player) *player=0;
    return PLAYER_STORE_NOT_FOUND;
}

int player_record_serialize_bounded(creature *player, char perm_only,
    char *buffer, unsigned long capacity, unsigned long *written,
    const player_record_serializer_limits *limits)
{
    if(!active_shadow||!player||perm_only||!buffer||!written||!limits||
       strcmp(player->name,player_name)||capacity<sizeof(serialized))
        return PLAYER_RECORD_SERIALIZER_INVALID;
    active_shadow->serializer_calls++;
    memcpy(buffer,serialized,sizeof(serialized)-1);
    *written=sizeof(serialized)-1;
    return PLAYER_RECORD_SERIALIZER_OK;
}

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr,"m3_save_all_ply_receipt_path_test: %s\n",message);
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

static int path_exists(const char *root, const char *relative)
{
    char path[PATH_MAX];
    struct stat status;
    return !join_path(path,sizeof(path),root,relative)&&!lstat(path,&status);
}

static int command_exists(const char *root, const char *suffix)
{
    char relative[160];
    int length=snprintf(relative,sizeof(relative),"character-save-journal/%s.%s",
        journal_command,suffix);
    return length>=0&&(size_t)length<sizeof(relative)&&path_exists(root,relative);
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

static const char *fixture_getenv(void *opaque, const char *name)
{
    environment_fixture *environment=(environment_fixture *)opaque;
    if(!strcmp(name,"MUD_M3_MODE")) return environment->mode;
    if(!strcmp(name,"MUHAN_HOME")) return environment->root;
    if(!strcmp(name,"MUD_M3_WORLD_ID")) return world;
    if(!strcmp(name,"MUD_M3_CONNINFO_FILE")) return environment->conninfo_path;
    return 0;
}

static int secret_open(void *opaque, const char *path)
{ (void)opaque; return path&&!strcmp(path,"/test-m3.conninfo") ? 7 : -1; }

static int secret_stat(void *opaque, int descriptor, struct stat *status)
{
    secret_fixture *file=(secret_fixture *)opaque;
    if(descriptor!=7||!status) return -1;
    memset(status,0,sizeof(*status));
    status->st_mode=S_IFREG|0600;
    status->st_nlink=1;
    status->st_uid=geteuid();
    status->st_size=(off_t)file->length;
    return 0;
}

static long secret_read(void *opaque, int descriptor, void *buffer, size_t length)
{
    secret_fixture *file=(secret_fixture *)opaque;
    if(descriptor!=7||!buffer) return -1;
    if(file->reads++) return 0;
    if(length<file->length) return -1;
    memcpy(buffer,file->bytes,file->length);
    return (long)file->length;
}

static int secret_close(void *opaque, int descriptor)
{ (void)opaque; return descriptor==7 ? 0 : -1; }

static const character_save_journal_v2_runtime_file_operations secret_operations={
    secret_open,secret_stat,secret_read,secret_close
};

static int text_equal(const char *actual, const char *expected)
{ return actual&&expected&&!strcmp(actual,expected); }

static int wire_connection_ok(void *connection)
{ return connection&&((wire_fixture *)connection)->valid; }

static int wire_transaction_ok(void *connection)
{ return connection&&((wire_fixture *)connection)->valid; }

static void *wire_exec(void *connection, const char *sql, int count,
    const unsigned int *types, const char *const *values, const int *lengths,
    const int *formats, int result_format)
{
    wire_fixture *wire=(wire_fixture *)connection;
    (void)types; (void)lengths; (void)formats; (void)result_format;
    if(!wire||!sql) return 0;
    wire->current_call=WIRE_NONE;
    if(strstr(sql,"m3_assert_writer_session")) {
        wire->current_call=WIRE_ASSERT;
        if(count) wire->valid=0;
    } else if(strstr(sql,"acquire_game_world_writer_epoch")) {
        wire->current_call=WIRE_ACQUIRE;
        wire->acquire_calls++;
        if(count!=3||!values||!text_equal(values[0],world)||
           !text_equal(values[1],instance)||!text_equal(values[2],acquire_deadline))
            wire->valid=0;
    } else if(strstr(sql,"renew_game_world_writer_epoch")) {
        wire->current_call=WIRE_RENEW;
        wire->renew_calls++;
        if(count!=4||!values||!text_equal(values[0],world)||
           !text_equal(values[1],instance)||!text_equal(values[2],"7")||
           !text_equal(values[3],save_deadline)) wire->valid=0;
    } else if(strstr(sql,"resolve_game_character_writer_route_v3")) {
        wire->current_call=WIRE_ROUTE;
        wire->route_calls++;
        if(count!=2||!values||!text_equal(values[0],world)||
           !text_equal(values[1],player_name)) wire->valid=0;
    } else if(strstr(sql,"seed_game_character_absent_head")) {
        wire->current_call=WIRE_SEED;
        wire->seed_calls++;
        if(count!=6||wire->seeded||!values||!text_equal(values[0],world)||
           !text_equal(values[1],player_name)||
           !text_equal(values[2],character)||!text_equal(values[3],instance)||
           !text_equal(values[4],"7")||!text_equal(values[5],"1"))
            wire->valid=0;
        wire->seeded=1;
    } else if(strstr(sql,"record_legacy_published_receipt")) {
        wire->current_call=WIRE_RECEIPT;
        wire->receipt_calls++;
        if(count!=12||!values||!text_equal(values[0],world)||
           !text_equal(values[1],player_name)||!text_equal(values[2],character)||
           !text_equal(values[3],journal_command)||!text_equal(values[4],instance)||
           !text_equal(values[6],"7")||!text_equal(values[7],"1")||
           !text_equal(values[8],"absent")||values[9]||!text_equal(values[11],"1"))
            wire->valid=0;
    } else wire->valid=0;
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
        ((wire->current_call==WIRE_ACQUIRE||wire->current_call==WIRE_RENEW) ? 2 : 1);
}

static const char *wire_value(void *result, int row, int column)
{
    wire_fixture *wire=(wire_fixture *)result;
    static const char *route_uninitialized[]={world,character,"M3alpha","66","1",
        "active","","uninitialized","","0"};
    static const char *route_absent[]={world,character,"M3alpha","66","1","active","",
        "absent","","0"};
    static const char *epoch[]={"7","2026-09-03T00:01:00Z"};
    if(!wire||row) return 0;
    if(wire->current_call==WIRE_ASSERT) return column==0 ? "t" : 0;
    if(wire->current_call==WIRE_ACQUIRE||wire->current_call==WIRE_RENEW)
        return column>=0&&column<2 ? epoch[column] : 0;
    if(wire->current_call==WIRE_ROUTE) return column>=0&&column<10 ?
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
{ (void)result; }

static void wire_close(void *connection)
{ if(connection) ((wire_fixture *)connection)->close_calls++; }

static const character_save_journal_v2_rpc_transport_operations wire_operations={
    wire_connection_ok,wire_transaction_ok,wire_exec,wire_status,wire_rows,
    wire_columns,wire_value,wire_value_length,wire_sqlstate,wire_clear,wire_close
};

static int lease_deadline(void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX+1])
{
    shadow_fixture *shadow=(shadow_fixture *)opaque;

    if(shadow!=active_shadow) return -1;
    strcpy(output,shadow->deadline_calls++ ? save_deadline : acquire_deadline);
    return 0;
}

static int command_uuid(void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH+1])
{
    shadow_fixture *shadow=(shadow_fixture *)opaque;

    if(shadow!=active_shadow) return -1;
    strcpy(output,shadow->uuid_calls++ ? journal_command : instance);
    return 0;
}

static int journal_load(void *opaque, char *name, creature **player)
{
    (void)name;
    if(opaque!=active_shadow) return PLAYER_STORE_IO_ERROR;
    if(player) *player=0;
    return PLAYER_STORE_NOT_FOUND;
}

static int shadow_start(void *opaque, const char *root, const char *world_id,
    const char *conninfo)
{
    shadow_fixture *shadow=(shadow_fixture *)opaque;
    character_save_journal_v2_process_owner_configuration configuration;

    if(!shadow||strcmp(root,shadow->root)||strcmp(world_id,world)||
       strcmp(conninfo,"dbname=fixture")) return -1;
    shadow->start_calls++;
    shadow->wire.valid=1;
    active_shadow=shadow;
    character_save_journal_v2_rpc_transport_init(&shadow->transport);
    if(character_save_journal_v2_rpc_transport_start(&shadow->transport,
       &wire_operations,&shadow->wire,&shadow->wire)!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return -1;
    memset(&configuration,0,sizeof(configuration));
    shadow->limits.max_depth=64;
    shadow->limits.max_objects=8192;
    character_player_snapshot_v1_handoff_init(&shadow->handoff,0);
    configuration.root=root;
    configuration.world_id=world_id;
    configuration.transport=&shadow->transport;
    configuration.buffer=shadow->buffer;
    configuration.buffer_capacity=sizeof(shadow->buffer);
    configuration.serializer_limits=shadow->limits;
    configuration.acquire_deadline=lease_deadline;
    configuration.acquire_deadline_opaque=shadow;
    configuration.candidate_uuid=command_uuid;
    configuration.candidate_uuid_opaque=shadow;
    configuration.file_load=journal_load;
    configuration.file_load_opaque=shadow;
    /* This is the real optional process-owner handoff.  It emits the durable
     * token/source pair while the normal save remains journal-authoritative. */
    configuration.snapshot_handoff=&shadow->handoff;
    character_save_journal_v2_process_owner_init(&shadow->owner,&configuration);
    if(character_save_journal_v2_process_owner_start(&shadow->owner)!=
       CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK)
        goto failed;
    return 0;
failed:
    (void)character_save_journal_v2_process_owner_shutdown(&shadow->owner);
    character_save_journal_v2_rpc_transport_close(&shadow->transport);
    active_shadow=0;
    return -1;
}

static void shadow_shutdown(void *opaque)
{
    shadow_fixture *shadow=(shadow_fixture *)opaque;
    if(!shadow) return;
    shadow->shutdown_calls++;
    (void)character_save_journal_v2_process_owner_shutdown(&shadow->owner);
    character_save_journal_v2_rpc_transport_close(&shadow->transport);
    if(active_shadow==shadow) active_shadow=0;
}

static const character_save_journal_v2_runtime_shadow_operations shadow_operations={
    shadow_start,shadow_shutdown
};

static void runtime_init(character_save_journal_v2_runtime *runtime,
    environment_fixture *environment, secret_fixture *secret, shadow_fixture *shadow)
{
    character_save_journal_v2_runtime_dependencies dependencies;
    memset(&dependencies,0,sizeof(dependencies));
    dependencies.environment_get=fixture_getenv;
    dependencies.environment_opaque=environment;
    dependencies.file_operations=&secret_operations;
    dependencies.file_opaque=secret;
    dependencies.shadow_operations=&shadow_operations;
    dependencies.shadow_opaque=shadow;
    character_save_journal_v2_runtime_init(runtime,&dependencies);
}

static void install_online_player(creature *player, iobuf *io)
{
    memset(Ply,0,sizeof(Ply));
    memset(player,0,sizeof(*player));
    memset(io,0,sizeof(*io));
    strcpy(player->name,player_name);
    player->fd=0;
    Ply[0].ply=player;
    Ply[0].io=io;
    Tablesize=1;
}

int main(void)
{
    char root_template[PATH_MAX];
    character_save_journal_v2_runtime runtime;
    environment_fixture environment;
    secret_fixture secret;
    shadow_fixture shadow;
    creature player;
    iobuf io;
    character_save_journal_v2_runtime_state start_result;
    int failed=0;

    if(!realpath("/tmp",root_template)||
       strlen(root_template)+sizeof("/muhan-m3-save-all-receipt-XXXXXX")>
       sizeof(root_template)) {
        return 1;
    }
    strcat(root_template,"/muhan-m3-save-all-receipt-XXXXXX");
    if(!mkdtemp(root_template)) {
        perror("m3 save-all fixture");
        return 1;
    }
    memset(&environment,0,sizeof(environment));
    memset(&secret,0,sizeof(secret));
    memset(&shadow,0,sizeof(shadow));
    strcpy(shadow.root,root_template);
    if(make_directory(shadow.root,"player")||make_directory(shadow.root,"player/66")||
       make_directory(shadow.root,"character-save-stage")||
       make_directory(shadow.root,"character-save-journal")) {
        (void)remove_tree(shadow.root);
        return 1;
    }
    environment.root=shadow.root;
    environment.conninfo_path="/test-m3.conninfo";
    secret.bytes="dbname=fixture";
    secret.length=strlen(secret.bytes);
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
    install_online_player(&player,&io);
    player_store_reset();

    runtime_init(&runtime,&environment,&secret,&shadow);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,
        "absent MUD_M3_MODE leaves normal bulk saves on FileStore");
    save_all_ply();
    failed+=expect(legacy_save_calls==1&&!shadow.start_calls&&!shadow.wire.receipt_calls,
        "absent mode performs one legacy save and no journal receipt");

    environment.mode="off";
    runtime_init(&runtime,&environment,&secret,&shadow);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,
        "off MUD_M3_MODE leaves normal bulk saves on FileStore");
    save_all_ply();
    failed+=expect(legacy_save_calls==2&&!shadow.start_calls&&!shadow.wire.receipt_calls,
        "off mode performs one further legacy save and no journal receipt");

    environment.mode="shadow";
    runtime_init(&runtime,&environment,&secret,&shadow);
    start_result=character_save_journal_v2_runtime_start(&runtime);
    failed+=expect(start_result==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY&&shadow.start_calls==1&&
        shadow.owner.player_store_binding.active,
        "exact shadow mode binds the production journal PlayerStore");
    save_all_ply();
    failed+=expect(legacy_save_calls==2&&shadow.serializer_calls==1&&
        shadow.wire.acquire_calls==1&&shadow.wire.renew_calls==1&&
        shadow.wire.route_calls==4&&shadow.wire.seed_calls==1&&
        shadow.wire.receipt_calls==1&&shadow.wire.valid&&!merror_calls,
        "one normal bulk save reaches the seeded production journal receipt path exactly once");
    failed+=expect(command_exists(shadow.root,"published")&&command_exists(shadow.root,"acked")&&
        path_exists(shadow.root,"player/66/M3alpha")&&
        shadow.owner.player_store.last_report.reached==
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED,
        "the receipt follows durable journal publication and acknowledgement");
    failed+=expect(path_exists(shadow.root,
        "character-player-snapshot-v1-handoff/20000000-0000-4000-8000-000000000002.handoff")&&
        path_exists(shadow.root,
        "character-player-snapshot-v1-handoff/20000000-0000-4000-8000-000000000002.source")&&
        shadow.handoff.report.enqueued==1&&
        shadow.owner.player_store.last_report.snapshot_attempted==1&&
        shadow.owner.player_store.last_report.snapshot_result==
        CHARACTER_PLAYER_SNAPSHOT_V1_HANDOFF_OK,
        "the optional production handoff leaves durable PREPARED evidence without gating ACK");

    character_save_journal_v2_runtime_shutdown(&runtime);
    failed+=expect(shadow.shutdown_calls==1&&!shadow.owner.player_store_binding.active&&
        shadow.wire.close_calls==1,
        "shadow shutdown restores the legacy binding and closes only the fixture transport");
    player_store_reset();
    memset(Ply,0,sizeof(Ply));
    Tablesize=0;
    if(remove_tree(shadow.root)) failed++;
    return failed ? 1 : 0;
}
