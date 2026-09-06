/*
 * The native shadow owner must retain its own stable copies of the validated
 * runtime root and world id.  The generic runtime supplies both as borrowed
 * pointers whose lifetime ends when start returns (or when the environment is
 * changed), while process_owner retains them for the whole writer lifetime.
 *
 * This translation unit includes the native adapter behind renamed external
 * boundaries.  That exercises the real private start/shutdown operations
 * without opening a database connection or replacing the production APIs.
 */
#include <stddef.h>
#include <stdlib.h>

void *test_malloc(size_t size);
void test_free(void *memory);

#define malloc test_malloc
#define free test_free
#define PQconnectdb test_PQconnectdb
#define PQstatus test_PQstatus
#define PQexec test_PQexec
#define PQresultStatus test_PQresultStatus
#define PQntuples test_PQntuples
#define PQnfields test_PQnfields
#define PQgetvalue test_PQgetvalue
#define PQgetlength test_PQgetlength
#define PQresultErrorField test_PQresultErrorField
#define PQclear test_PQclear
#define PQfinish test_PQfinish
#define character_save_journal_v2_uuid_generate_native test_uuid_generate_native
#define player_store_default_load test_player_store_default_load
#define character_save_journal_v2_process_owner_init test_process_owner_init
#define character_save_journal_v2_process_owner_start test_process_owner_start
#define character_save_journal_v2_process_owner_shutdown test_process_owner_shutdown
#define character_save_journal_v2_process_owner_snapshot_tick test_process_owner_snapshot_tick
#define character_save_journal_v2_rpc_transport_native_init test_rpc_transport_native_init
#define character_save_journal_v2_rpc_transport_native_start test_rpc_transport_native_start
#define character_save_journal_v2_rpc_transport_close test_rpc_transport_close
#define character_save_journal_v2_deadline_native_init test_deadline_native_init
#define character_save_journal_v2_deadline_native_callback test_deadline_native_callback
#define character_player_snapshot_v1_capture_native_init test_snapshot_capture_native_init
#define character_player_snapshot_v1_handoff_init test_snapshot_handoff_init
#define character_player_snapshot_v1_handoff_enable_receipt_pair test_snapshot_handoff_enable_receipt_pair
#define character_player_snapshot_v1_receipt_pair_commit test_snapshot_receipt_pair_commit
#define player_snapshot_v1_native_abi_supported test_snapshot_native_abi_supported
#define character_player_snapshot_v1_artifact_load_metadata_for_command test_artifact_load_metadata_for_command
#define character_player_snapshot_v1_read_rehearsal_load test_read_rehearsal_load

#include "character_save_journal_v2_runtime_native.c"

#undef PQconnectdb
#undef PQstatus
#undef PQexec
#undef PQresultStatus
#undef PQntuples
#undef PQnfields
#undef PQgetvalue
#undef PQgetlength
#undef PQresultErrorField
#undef PQclear
#undef PQfinish
#undef character_save_journal_v2_uuid_generate_native
#undef player_store_default_load
#undef character_save_journal_v2_process_owner_init
#undef character_save_journal_v2_process_owner_start
#undef character_save_journal_v2_process_owner_shutdown
#undef character_save_journal_v2_process_owner_snapshot_tick
#undef character_save_journal_v2_rpc_transport_native_init
#undef character_save_journal_v2_rpc_transport_native_start
#undef character_save_journal_v2_rpc_transport_close
#undef character_save_journal_v2_deadline_native_init
#undef character_save_journal_v2_deadline_native_callback
#undef character_player_snapshot_v1_capture_native_init
#undef character_player_snapshot_v1_handoff_init
#undef character_player_snapshot_v1_handoff_enable_receipt_pair
#undef character_player_snapshot_v1_receipt_pair_commit
#undef player_snapshot_v1_native_abi_supported
#undef character_player_snapshot_v1_artifact_load_metadata_for_command
#undef character_player_snapshot_v1_read_rehearsal_load
#undef malloc
#undef free

#include <stdio.h>
#include <limits.h>
#include <sys/stat.h>
#include <unistd.h>

static union {
    long alignment;
    unsigned char bytes[64];
} fake_connection_storage, fake_result_storage;

static int connect_calls;
static int malloc_calls;
static int free_calls;
static int fail_malloc;
static int process_owner_init_calls;
static int process_owner_start_calls;
static int process_owner_shutdown_calls;
static int process_owner_snapshot_tick_calls;
static int transport_close_calls;
static int default_load_calls;
static int artifact_metadata_load_calls;
static int read_rehearsal_load_calls;
static int supplied_artifact_metadata_result;
static character_player_snapshot_v1_read_rehearsal_result
    supplied_read_rehearsal_result;
static int supplied_artifact_metadata_directory_fd;
static char supplied_artifact_metadata_command[37];
static const character_player_snapshot_v1_read_rehearsal *supplied_rehearsal;
static int snapshot_capture_native_init_calls;
static int snapshot_handoff_init_calls;
static int snapshot_handoff_enable_receipt_pair_calls;
static int snapshot_receipt_pair_commit_calls;
static int snapshot_native_abi_supported;
static int snapshot_native_abi_calls;
static size_t snapshot_native_abi_char_bits;
static size_t snapshot_native_abi_short_bits;
static size_t snapshot_native_abi_long_bits;
static int snapshot_native_abi_long_covers_i64;
static int snapshot_native_abi_player_wire_value;
static character_save_journal_v2_process_owner_startup_result
    supplied_process_owner_start_result;
static character_save_journal_v2_rpc_transport_outcome
    supplied_transport_start_result;
static character_player_snapshot_v1_capture *snapshot_handoff_capture;
static character_player_snapshot_v1_handoff_receipt_pair
    snapshot_handoff_receipt_pair;
static const character_save_journal_v2_writer_context *
    snapshot_receipt_pair_writer;
static int snapshot_receipt_pair_artifact_directory_fd;
static const character_player_snapshot_v1_artifact_metadata *
    snapshot_receipt_pair_artifact_key;
static character_save_journal_v2_process_owner *snapshot_tick_owner;
static unsigned int snapshot_tick_limit;
static character_save_journal_v2_process_owner_snapshot_tick_result
    supplied_snapshot_tick_result;
static long supplied_idle_time;
static int idle_diagnostic_calls;
static char idle_diagnostic[128];
static char supplied_conninfo[64];

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr, "FAIL: %s\n", message);
    return 1;
}

static int bytes_are_zero(const void *memory, size_t length)
{
    const unsigned char *cursor=(const unsigned char *)memory;
    while(length) {
        if(*cursor++) return 0;
        length--;
    }
    return 1;
}

PGconn *test_PQconnectdb(const char *conninfo)
{
    connect_calls++;
    (void)snprintf(supplied_conninfo,sizeof(supplied_conninfo),"%s",conninfo);
    return (PGconn *)&fake_connection_storage;
}

ConnStatusType test_PQstatus(const PGconn *connection)
{
    (void)connection;
    return CONNECTION_OK;
}

PGresult *test_PQexec(PGconn *connection, const char *sql)
{
    (void)connection;
    (void)sql;
    return (PGresult *)&fake_result_storage;
}

ExecStatusType test_PQresultStatus(const PGresult *result)
{
    (void)result;
    return PGRES_TUPLES_OK;
}

int test_PQntuples(const PGresult *result)
{
    (void)result;
    return 1;
}

int test_PQnfields(const PGresult *result)
{
    (void)result;
    return 1;
}

char *test_PQgetvalue(const PGresult *result, int row, int column)
{
    static char value[]="t";
    (void)result;
    (void)row;
    (void)column;
    return value;
}

int test_PQgetlength(const PGresult *result, int row, int column)
{
    (void)result;
    (void)row;
    (void)column;
    return 1;
}

char *test_PQresultErrorField(const PGresult *result, int field)
{
    (void)result;
    (void)field;
    return 0;
}

void test_PQclear(PGresult *result)
{
    (void)result;
}

void test_PQfinish(PGconn *connection)
{
    (void)connection;
}

character_save_journal_v2_uuid_result test_uuid_generate_native(
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH+1])
{
    memcpy(output,"12345678-1234-4234-8234-123456789abc",
        CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH+1);
    return CHARACTER_SAVE_JOURNAL_V2_UUID_OK;
}

int test_player_store_default_load(char *name, struct creature **player)
{
    static unsigned char fake_player_storage;

    default_load_calls++;
    if(strcmp(name,"legacy")) return PLAYER_STORE_NOT_FOUND;
    *player=(struct creature *)&fake_player_storage;
    return PLAYER_STORE_OK;
}

int test_artifact_load_metadata_for_command(int directory_fd,
    const char command_id[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH+1],
    character_player_snapshot_v1_artifact_metadata *metadata)
{
    artifact_metadata_load_calls++;
    supplied_artifact_metadata_directory_fd=directory_fd;
    (void)snprintf(supplied_artifact_metadata_command,
        sizeof(supplied_artifact_metadata_command),"%s",command_id);
    if(metadata) {
        memset(metadata,0,sizeof(*metadata));
        if(supplied_artifact_metadata_result==
           CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK)
            memcpy(metadata->command_id,command_id,sizeof(metadata->command_id));
    }
    return supplied_artifact_metadata_result;
}

int test_read_rehearsal_load(
    const character_player_snapshot_v1_read_rehearsal *rehearsal,
    char *name, struct creature **player,
    character_player_snapshot_v1_read_rehearsal_result *result_out)
{
    read_rehearsal_load_calls++;
    supplied_rehearsal=rehearsal;
    if(result_out) *result_out=supplied_read_rehearsal_result;
    return test_player_store_default_load(name,player);
}

void test_process_owner_init(character_save_journal_v2_process_owner *owner,
    const character_save_journal_v2_process_owner_configuration *configuration)
{
    process_owner_init_calls++;
    memset(owner,0,sizeof(*owner));
    owner->configuration=*configuration;
    owner->state=CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_NEW;
}

character_save_journal_v2_process_owner_startup_result
test_process_owner_start(character_save_journal_v2_process_owner *owner)
{
    process_owner_start_calls++;
    if(supplied_process_owner_start_result!=
       CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK)
        return supplied_process_owner_start_result;
    owner->state=CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY;
    return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK;
}

character_save_journal_v2_process_owner_shutdown_result
test_process_owner_shutdown(character_save_journal_v2_process_owner *owner)
{
    process_owner_shutdown_calls++;
    owner->state=CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED;
    return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK;
}

character_save_journal_v2_process_owner_snapshot_tick_result
test_process_owner_snapshot_tick(character_save_journal_v2_process_owner *owner,
    unsigned int limit)
{
    process_owner_snapshot_tick_calls++;
    snapshot_tick_owner=owner;
    snapshot_tick_limit=limit;
    return supplied_snapshot_tick_result;
}

static long test_idle_clock(void *opaque)
{
    (void)opaque;
    return supplied_idle_time;
}

static void test_idle_diagnostic(void *opaque, const char *message)
{
    (void)opaque;
    idle_diagnostic_calls++;
    (void)snprintf(idle_diagnostic,sizeof(idle_diagnostic),"%s",message);
}

void test_snapshot_capture_native_init(
    character_player_snapshot_v1_capture *capture)
{
    snapshot_capture_native_init_calls++;
    memset(capture,0,sizeof(*capture));
}

void test_snapshot_handoff_init(character_player_snapshot_v1_handoff *handoff,
    character_player_snapshot_v1_capture *capture)
{
    snapshot_handoff_init_calls++;
    snapshot_handoff_capture=capture;
    memset(handoff,0,sizeof(*handoff));
    handoff->capture=capture;
}

void test_snapshot_handoff_enable_receipt_pair(
    character_player_snapshot_v1_handoff *handoff,
    character_player_snapshot_v1_handoff_receipt_pair pair)
{
    snapshot_handoff_enable_receipt_pair_calls++;
    snapshot_handoff_receipt_pair=pair;
    if(handoff) handoff->receipt_pair=pair;
}

character_player_snapshot_v1_receipt_pair_result
test_snapshot_receipt_pair_commit(
    const character_save_journal_v2_writer_context *writer,
    int artifact_directory_fd,
    const character_player_snapshot_v1_artifact_metadata *artifact_key)
{
    snapshot_receipt_pair_commit_calls++;
    snapshot_receipt_pair_writer=writer;
    snapshot_receipt_pair_artifact_directory_fd=artifact_directory_fd;
    snapshot_receipt_pair_artifact_key=artifact_key;
    return CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_OK;
}

int test_snapshot_native_abi_supported(size_t char_bits, size_t short_bits,
    size_t long_bits, int long_covers_i64, int player_wire_value)
{
    snapshot_native_abi_calls++;
    snapshot_native_abi_char_bits=char_bits;
    snapshot_native_abi_short_bits=short_bits;
    snapshot_native_abi_long_bits=long_bits;
    snapshot_native_abi_long_covers_i64=long_covers_i64;
    snapshot_native_abi_player_wire_value=player_wire_value;
    return snapshot_native_abi_supported;
}

void *test_malloc(size_t size)
{
    malloc_calls++;
    if(fail_malloc) return 0;
    return malloc(size);
}

void test_free(void *memory)
{
    if(memory) free_calls++;
    free(memory);
}

void test_rpc_transport_native_init(
    character_save_journal_v2_rpc_transport_native *native)
{
    memset(native,0,sizeof(*native));
    native->transport.state=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_NEW;
}

character_save_journal_v2_rpc_transport_outcome
test_rpc_transport_native_start(
    character_save_journal_v2_rpc_transport_native *native, void *connection)
{
    if(!native||connection!=(void *)&fake_connection_storage)
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    if(supplied_transport_start_result!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return supplied_transport_start_result;
    native->transport.connection=connection;
    native->transport.state=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;
    return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
}

void test_rpc_transport_close(character_save_journal_v2_rpc_transport *transport)
{
    transport_close_calls++;
    if(transport) {
        transport->connection=0;
        transport->state=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED;
    }
}

void test_deadline_native_init(character_save_journal_v2_deadline_native *native,
    int64_t extension_seconds)
{
    (void)extension_seconds;
    memset(native,0,sizeof(*native));
}

int test_deadline_native_callback(void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_DEADLINE_OUTPUT_SIZE])
{
    (void)opaque;
    memcpy(output,"2030-01-01T00:00:00Z",21);
    return 0;
}

static void reset_fakes(void)
{
    connect_calls=0;
    malloc_calls=0;
    free_calls=0;
    fail_malloc=0;
    process_owner_init_calls=0;
    process_owner_start_calls=0;
    process_owner_shutdown_calls=0;
    process_owner_snapshot_tick_calls=0;
    transport_close_calls=0;
    default_load_calls=0;
    artifact_metadata_load_calls=0;
    read_rehearsal_load_calls=0;
    supplied_artifact_metadata_result=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK;
    supplied_read_rehearsal_result=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_MATCHED;
    supplied_artifact_metadata_directory_fd=-1;
    memset(supplied_artifact_metadata_command,0,
        sizeof(supplied_artifact_metadata_command));
    supplied_rehearsal=0;
    snapshot_capture_native_init_calls=0;
    snapshot_handoff_init_calls=0;
    snapshot_handoff_enable_receipt_pair_calls=0;
    snapshot_receipt_pair_commit_calls=0;
    snapshot_native_abi_supported=1;
    snapshot_native_abi_calls=0;
    snapshot_native_abi_char_bits=0U;
    snapshot_native_abi_short_bits=0U;
    snapshot_native_abi_long_bits=0U;
    snapshot_native_abi_long_covers_i64=0;
    snapshot_native_abi_player_wire_value=-1;
    supplied_process_owner_start_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK;
    supplied_transport_start_result=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
    snapshot_handoff_capture=0;
    snapshot_handoff_receipt_pair=0;
    snapshot_receipt_pair_writer=0;
    snapshot_receipt_pair_artifact_directory_fd=-1;
    snapshot_receipt_pair_artifact_key=0;
    snapshot_tick_owner=0;
    snapshot_tick_limit=0;
    supplied_snapshot_tick_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OK;
    supplied_idle_time=0;
    idle_diagnostic_calls=0;
    memset(idle_diagnostic,0,sizeof(idle_diagnostic));
    memset(supplied_conninfo,0,sizeof(supplied_conninfo));
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_COMMAND_UUID");
}

static int read_rehearsal_root_create(char root[])
{
    char directory[128];

    if(!mkdtemp(root)) return -1;
    if(snprintf(directory,sizeof(directory),"%s/%s",root,
       CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY)<0||
       mkdir(directory,0700)) {
        rmdir(root);
        return -1;
    }
    return 0;
}

static void read_rehearsal_root_remove(const char *root)
{
    char directory[128];

    if(snprintf(directory,sizeof(directory),"%s/%s",root,
       CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY)>=0)
        (void)rmdir(directory);
    (void)rmdir(root);
}

static int test_native_owns_root_and_world_for_process_lifetime(void)
{
    character_save_journal_v2_runtime_native native;
    char root[64]="/tmp/muhan-runtime-owned";
    char world[32]="world-a";
    char conninfo[32]="dbname=muhan";
    struct creature *loaded=0;
    int failed=0;

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations!=0,
        "Linux native runtime must expose shadow operations");
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,root,world,conninfo)==0,
        "valid shadow start must succeed");
    failed|=expect(connect_calls==1&&process_owner_init_calls==1&&
        process_owner_start_calls==1,"shadow start must construct one owner");
    failed|=expect(!native.process_owner.configuration.snapshot_handoff&&
        !native.snapshot_handoff_enabled&&!snapshot_capture_native_init_calls&&
        !snapshot_handoff_init_calls&&!snapshot_handoff_enable_receipt_pair_calls,
        "snapshot handoff must stay OFF unless the native opt-in token is set");
    failed|=expect(character_save_journal_v2_runtime_native_snapshot_tick(&native,1)==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF&&
        !process_owner_snapshot_tick_calls,
        "OFF native composition must not create a consumer scheduling path");
    failed|=expect(!strcmp(supplied_conninfo,"dbname=muhan"),
        "conninfo must be used only to establish the connection");
    failed|=expect(native.process_owner.configuration.root==native.muhan_home&&
        native.process_owner.configuration.root!=root,
        "process owner root must point at native-owned storage");
    failed|=expect(native.process_owner.configuration.world_id==native.world_id&&
        native.process_owner.configuration.world_id!=world,
        "process owner world id must point at native-owned storage");
    failed|=expect(native.process_owner.configuration.file_load(
        native.process_owner.configuration.file_load_opaque,"legacy",&loaded)==
        PLAYER_STORE_OK&&loaded&&default_load_calls==1,
        "native owner file fallback must use the PlayerStore default seam");
    failed|=expect(!native.read_rehearsal_armed&&
        !artifact_metadata_load_calls&&!read_rehearsal_load_calls&&
        native.read_rehearsal_last_result==
        CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DISABLED,
        "absent selector must do no artifact or receipt rehearsal work");
    root[1]='X';
    world[0]='x';
    failed|=expect(!strcmp(native.process_owner.configuration.root,
        "/tmp/muhan-runtime-owned"),
        "mutating the caller root must not alter the live owner");
    failed|=expect(!strcmp(native.process_owner.configuration.world_id,"world-a"),
        "mutating the caller world id must not alter the live owner");

    native.dependencies.shadow_operations->shutdown(
        native.dependencies.shadow_opaque);
    failed|=expect(process_owner_shutdown_calls==1&&transport_close_calls==1,
        "shutdown must release owner before transport");
    failed|=expect(native.serializer_buffer==0&&
        native.serializer_buffer_capacity==0&&!native.shadow_active,
        "shutdown must release native serializer state");
    failed|=expect(bytes_are_zero(native.muhan_home,sizeof(native.muhan_home))&&
        bytes_are_zero(native.world_id,sizeof(native.world_id)),
        "shutdown must clear native-owned routing strings");
    return failed;
}

static int test_native_read_rehearsal_is_exact_default_off_and_diagnostic_only(void)
{
    static const char selector[]="10000000-0000-4000-8000-000000000001";
    character_save_journal_v2_runtime_native native;
    char root[]="/tmp/muhan-read-rehearsal-XXXXXX";
    struct creature *loaded;
    int failed=0;
    int mismatch;

    reset_fakes();
    failed|=expect(read_rehearsal_root_create(root)==0,
        "read rehearsal fixture must create its private immutable directory");
    if(failed) return failed;
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_COMMAND_UUID",
        "not-a-command-uuid",1)==0,"test must set an invalid selector");
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,root,"world-a","dbname=muhan")==0,
        "invalid selector must not reject the native shadow owner");
    loaded=0;
    failed|=expect(!native.read_rehearsal_armed&&!artifact_metadata_load_calls&&
        native.process_owner.configuration.file_load(
        native.process_owner.configuration.file_load_opaque,"legacy",&loaded)==
        PLAYER_STORE_OK&&loaded&&default_load_calls==1&&!read_rehearsal_load_calls,
        "invalid selector must retain the exact default FileStore callback");
    native.dependencies.shadow_operations->shutdown(native.dependencies.shadow_opaque);

    reset_fakes();
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_COMMAND_UUID",
        selector,1)==0,"test must preserve an exact selector for corrupt evidence");
    supplied_artifact_metadata_result=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT;
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,root,"world-a","dbname=muhan")==0,
        "malformed selected artifact must not reject the native shadow owner");
    loaded=0;
    failed|=expect(!native.read_rehearsal_armed&&artifact_metadata_load_calls==1&&
        native.read_rehearsal_last_result==
        CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH&&
        native.process_owner.configuration.file_load(
        native.process_owner.configuration.file_load_opaque,"legacy",&loaded)==
        PLAYER_STORE_OK&&loaded&&default_load_calls==1&&!read_rehearsal_load_calls,
        "malformed selected artifact must fail diagnostic-only and retain legacy load");
    native.dependencies.shadow_operations->shutdown(native.dependencies.shadow_opaque);

    reset_fakes();
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_COMMAND_UUID",
        selector,1)==0,"test must set an exact selector");
    supplied_artifact_metadata_result=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND;
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,root,"world-a","dbname=muhan")==0,
        "unavailable selected artifact must not reject the native shadow owner");
    loaded=0;
    failed|=expect(!native.read_rehearsal_armed&&artifact_metadata_load_calls==1&&
        !strcmp(supplied_artifact_metadata_command,selector)&&
        native.read_rehearsal_last_result==
        CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH&&
        native.process_owner.configuration.file_load(
        native.process_owner.configuration.file_load_opaque,"legacy",&loaded)==
        PLAYER_STORE_OK&&loaded&&default_load_calls==1&&!read_rehearsal_load_calls,
        "missing exact artifact must remain a closed diagnostic with no receipt work");
    native.dependencies.shadow_operations->shutdown(native.dependencies.shadow_opaque);

    reset_fakes();
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_COMMAND_UUID",
        selector,1)==0,"test must restore an exact selector");
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,root,"world-a","dbname=muhan")==0,
        "exact selected artifact must arm after the native shadow is live");
    failed|=expect(native.read_rehearsal_armed&&artifact_metadata_load_calls==1&&
        native.read_rehearsal_artifact_directory_fd>=0&&
        !strcmp(native.read_rehearsal_artifact.command_id,selector),
        "arm must retain only the exact immutable selected metadata");
    for(mismatch=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_MATCHED;
        mismatch<=CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_SEMANTIC_MISMATCH;
        mismatch++) {
        supplied_read_rehearsal_result=
            (character_player_snapshot_v1_read_rehearsal_result)mismatch;
        loaded=0;
        failed|=expect(native.process_owner.configuration.file_load(
            native.process_owner.configuration.file_load_opaque,"legacy",&loaded)==
            PLAYER_STORE_OK&&loaded&&read_rehearsal_load_calls==mismatch+1&&
            default_load_calls==mismatch+1&&supplied_rehearsal&&
            supplied_rehearsal->writer==&native.process_owner.held_writer&&
            supplied_rehearsal->expected_artifact==&native.read_rehearsal_artifact&&
            native.read_rehearsal_last_result==mismatch,
            "every rehearsal result must preserve the caller-bound legacy result");
    }
    native.dependencies.shadow_operations->shutdown(native.dependencies.shadow_opaque);
    failed|=expect(!native.read_rehearsal_armed&&
        native.read_rehearsal_artifact_directory_fd==-1&&
        bytes_are_zero(&native.read_rehearsal_artifact,
        sizeof(native.read_rehearsal_artifact)),
        "shutdown must close and wipe the owned rehearsal capability");
    read_rehearsal_root_remove(root);
    return failed;
}

static int test_native_snapshot_handoff_opt_in_has_only_explicit_tick(void)
{
    character_save_journal_v2_runtime_native native;
    int failed=0;

    reset_fakes();
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1","handoff",1)==0,
        "test must enable the explicit native snapshot opt-in");
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
        "world-a","dbname=muhan")==0,
        "opted-in native shadow start must succeed");
    failed|=expect(native.snapshot_handoff_enabled&&
        native.process_owner.configuration.snapshot_handoff==&native.snapshot_handoff&&
        snapshot_capture_native_init_calls==1&&snapshot_handoff_init_calls==1&&
        snapshot_handoff_enable_receipt_pair_calls==1&&
        snapshot_handoff_receipt_pair!=0&&
        snapshot_handoff_receipt_pair==test_snapshot_receipt_pair_commit&&
        native.snapshot_handoff.receipt_pair==test_snapshot_receipt_pair_commit&&
        snapshot_native_abi_calls==1&&snapshot_native_abi_char_bits==CHAR_BIT&&
        snapshot_native_abi_short_bits==16U&&snapshot_native_abi_long_bits==64U&&
        snapshot_native_abi_long_covers_i64&&
        snapshot_native_abi_player_wire_value==0&&
        snapshot_handoff_capture==&native.snapshot_capture&&
        native.snapshot_handoff.capture==&native.snapshot_capture&&
        !process_owner_snapshot_tick_calls,
        "native owner must own the handoff observer but never drain it during startup");
    failed|=expect(native.snapshot_handoff.receipt_pair&&
        native.snapshot_handoff.receipt_pair(0,37,0)==
        CHARACTER_PLAYER_SNAPSHOT_V1_RECEIPT_PAIR_OK&&
        snapshot_receipt_pair_commit_calls==1&&
        !snapshot_receipt_pair_writer&&
        snapshot_receipt_pair_artifact_directory_fd==37&&
        !snapshot_receipt_pair_artifact_key,
        "opt-in must retain the receipt-pair callback ABI and test behavior");
    failed|=expect(character_save_journal_v2_runtime_native_snapshot_tick(&native,7)==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OK&&
        process_owner_snapshot_tick_calls==1&&snapshot_tick_owner==
        &native.process_owner&&snapshot_tick_limit==7,
        "only the host-called native tick may schedule the bounded consumer");
    native.dependencies.shadow_operations->shutdown(
        native.dependencies.shadow_opaque);
    failed|=expect(!native.snapshot_handoff_enabled&&
        bytes_are_zero(&native.snapshot_handoff,sizeof(native.snapshot_handoff))&&
        bytes_are_zero(&native.snapshot_capture,sizeof(native.snapshot_capture)),
        "shutdown must clear native-owned optional handoff state after owner shutdown");
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed;
}

static int test_native_snapshot_handoff_rejects_near_miss_environment_values(void)
{
    static const char *const rejected[] = {"", "on", "HANDOFF", "handoff "};
    character_save_journal_v2_runtime_native native;
    unsigned int index;
    int failed=0;

    for(index=0;index<sizeof(rejected)/sizeof(rejected[0]);index++) {
        reset_fakes();
        failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1",rejected[index],1)==0,
            "test must install a rejected PlayerSnapshot environment value");
        character_save_journal_v2_runtime_native_init(&native);
        failed|=expect(native.dependencies.shadow_operations->start(
            native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
            "world-a","dbname=muhan")==0,
            "rejected PlayerSnapshot opt-in value must not reject M3 shadow startup");
        failed|=expect(!native.snapshot_handoff_enabled&&
            !native.process_owner.configuration.snapshot_handoff&&
            !snapshot_capture_native_init_calls&&!snapshot_handoff_init_calls&&
            !snapshot_handoff_enable_receipt_pair_calls&&
            character_save_journal_v2_runtime_native_snapshot_tick(&native,1)==
            CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF&&
            !process_owner_snapshot_tick_calls,
            "only MUD_M3_PLAYER_SNAPSHOT_V1=handoff may initiate PlayerSnapshot capture/outbox work");
        native.dependencies.shadow_operations->shutdown(
            native.dependencies.shadow_opaque);
    }
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed;
}

static int test_native_snapshot_handoff_abi_mismatch_stays_silent(void)
{
    character_save_journal_v2_runtime_native native;
    int failed=0;

    reset_fakes();
    snapshot_native_abi_supported=0;
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1","handoff",1)==0,
        "test must enable the explicit native snapshot opt-in");
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
        "world-a","dbname=muhan")==0,
        "ABI-mismatched native shadow start must preserve the primary owner");
    failed|=expect(snapshot_native_abi_calls==1&&
        !native.snapshot_handoff_enabled&&
        !native.process_owner.configuration.snapshot_handoff&&
        !snapshot_capture_native_init_calls&&!snapshot_handoff_init_calls,
        "unsupported ABI must leave the opt-in observer silently disabled");
    failed|=expect(character_save_journal_v2_runtime_native_snapshot_tick(&native,1)==
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF&&
        !process_owner_snapshot_tick_calls,
        "unsupported ABI must never partially schedule a handoff capture");
    native.dependencies.shadow_operations->shutdown(
        native.dependencies.shadow_opaque);
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed;
}

static int test_native_rejects_unbounded_borrowed_strings_before_connect(void)
{
    character_save_journal_v2_runtime_native native;
    char root[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PATH_MAX+1];
    char world[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_WORLD_ID_MAX+2];
    int failed=0;

    reset_fakes();
    memset(root,'a',sizeof(root));
    root[0]='/';
    root[sizeof(root)-1]=0;
    memset(world,'a',sizeof(world));
    world[sizeof(world)-1]=0;
    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,root,"world-a","dbname=muhan")!=0,
        "overlong root must fail before native construction");
    failed|=expect(connect_calls==0&&native.serializer_buffer==0,
        "overlong root must not connect or allocate");
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan",world,"dbname=muhan")!=0,
        "overlong world id must fail before native construction");
    failed|=expect(connect_calls==0&&native.serializer_buffer==0,
        "overlong world id must not connect or allocate");
    failed|=expect(bytes_are_zero(native.muhan_home,sizeof(native.muhan_home))&&
        bytes_are_zero(native.world_id,sizeof(native.world_id)),
        "rejected input must not leave copied routing state");
    return failed;
}

static int test_active_native_reinitialization_is_non_destructive(void)
{
    character_save_journal_v2_runtime_native native;
    const character_save_journal_v2_runtime_shadow_operations *operations;
    char *serializer_buffer;
    int failed=0;

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    operations=native.dependencies.shadow_operations;
    failed|=expect(operations&&operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
        "world-a","dbname=muhan")==0,
        "active native reinitialization fixture must start");
    serializer_buffer=native.serializer_buffer;

    character_save_journal_v2_runtime_native_init(&native);
    failed|=expect(native.shadow_active&&native.serializer_buffer==serializer_buffer&&
        native.process_owner.configuration.root==native.muhan_home&&
        native.process_owner.configuration.world_id==native.world_id,
        "native init must not overwrite an active process owner");
    failed|=expect(process_owner_shutdown_calls==0&&transport_close_calls==0,
        "active native init guard must not close live resources");

    operations->shutdown(&native);
    failed|=expect(process_owner_shutdown_calls==1&&transport_close_calls==1&&
        !native.serializer_buffer&&!native.shadow_active,
        "guarded native owner must retain its one explicit shutdown path");
    return failed;
}

static int test_native_start_failures_shutdown_and_remain_retryable(void)
{
    character_save_journal_v2_runtime_native native;
    const character_save_journal_v2_runtime_shadow_operations *operations;
    char overlong_world[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_WORLD_ID_MAX+2];
    int failed=0;

    memset(overlong_world,'a',sizeof(overlong_world));
    overlong_world[sizeof(overlong_world)-1]=0;

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    operations=native.dependencies.shadow_operations;
    failed|=expect(operations&&operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned",overlong_world,"dbname=muhan")!=0,
        "copy failure fixture must reject an overlong world id");
    failed|=expect(!native.shadow_active&&!native.serializer_buffer&&
        !native.serializer_buffer_capacity&&
        bytes_are_zero(native.muhan_home,sizeof(native.muhan_home))&&
        bytes_are_zero(native.world_id,sizeof(native.world_id)),
        "copy failure must clear all partially constructed native state");
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")==0,
        "copy failure must release the native owner for a retry");
    operations->shutdown(native.dependencies.shadow_opaque);

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    operations=native.dependencies.shadow_operations;
    fail_malloc=1;
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")!=0,
        "allocation failure fixture must reject serializer allocation");
    failed|=expect(malloc_calls==1&&!connect_calls&&!free_calls&&
        !native.shadow_active&&!native.serializer_buffer&&
        bytes_are_zero(native.muhan_home,sizeof(native.muhan_home))&&
        bytes_are_zero(native.world_id,sizeof(native.world_id)),
        "allocation failure must clear copied state without leaking a buffer");
    fail_malloc=0;
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")==0,
        "allocation failure must release the native owner for a retry");
    operations->shutdown(native.dependencies.shadow_opaque);

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    operations=native.dependencies.shadow_operations;
    supplied_transport_start_result=
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")!=0,
        "transport-start failure fixture must fail native startup");
    failed|=expect(connect_calls==1&&transport_close_calls==1&&free_calls==1&&
        !native.shadow_active&&!native.serializer_buffer&&
        !native.serializer_buffer_capacity&&
        bytes_are_zero(native.muhan_home,sizeof(native.muhan_home))&&
        bytes_are_zero(native.world_id,sizeof(native.world_id)),
        "transport-start failure must close and clear all native resources");
    supplied_transport_start_result=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK;
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")==0,
        "transport-start failure must release the native owner for a retry");
    operations->shutdown(native.dependencies.shadow_opaque);

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    operations=native.dependencies.shadow_operations;
    supplied_process_owner_start_result=
        (character_save_journal_v2_process_owner_startup_result)1;
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")!=0,
        "process-owner startup failure fixture must fail native startup");
    failed|=expect(process_owner_init_calls==1&&process_owner_start_calls==1&&
        process_owner_shutdown_calls==1&&transport_close_calls==1&&free_calls==1&&
        !native.shadow_active&&!native.serializer_buffer&&
        !native.serializer_buffer_capacity&&
        bytes_are_zero(native.muhan_home,sizeof(native.muhan_home))&&
        bytes_are_zero(native.world_id,sizeof(native.world_id)),
        "process-owner startup failure must unwind owner, transport, and buffer");
    supplied_process_owner_start_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK;
    failed|=expect(operations->start(native.dependencies.shadow_opaque,
        "/tmp/muhan-runtime-owned","world-a","dbname=muhan")==0,
        "process-owner failure must release the native owner for a retry");
    operations->shutdown(native.dependencies.shadow_opaque);
    return failed;
}

static int test_native_idle_tick_is_cadenced_bounded_and_silent_when_off(void)
{
    character_save_journal_v2_runtime_native native;
    int failed=0;

    reset_fakes();
    character_save_journal_v2_runtime_native_init(&native);
    character_save_journal_v2_runtime_native_snapshot_idle_configure(&native,
        test_idle_clock,0,test_idle_diagnostic,0);
    supplied_idle_time=100;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(!process_owner_snapshot_tick_calls&&!idle_diagnostic_calls,
        "default-off native idle hook must not schedule or diagnose a tick");

    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1","handoff",1)==0,
        "test must enable the explicit native snapshot opt-in");
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
        "world-a","dbname=muhan")==0,
        "idle tick fixture must start its native owner");
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==1&&snapshot_tick_limit==1,
        "idle hook must consume at most one durable handoff per cadence");
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==1,
        "same clock token must not schedule a second handoff tick");
    supplied_idle_time=101;
    supplied_snapshot_tick_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_BUSY;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==2&&!idle_diagnostic_calls,
        "BUSY must stay a silent cadence-limited no-op");
    supplied_idle_time=102;
    supplied_snapshot_tick_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_NOT_READY;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==3&&!idle_diagnostic_calls,
        "NOT_READY must stay a silent cadence-limited no-op");
    native.dependencies.shadow_operations->shutdown(
        native.dependencies.shadow_opaque);
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed;
}

static int test_native_idle_tick_retries_handoff_failure_with_throttled_log(void)
{
    character_save_journal_v2_runtime_native native;
    int failed=0;

    reset_fakes();
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1","handoff",1)==0,
        "test must enable the explicit native snapshot opt-in");
    character_save_journal_v2_runtime_native_init(&native);
    character_save_journal_v2_runtime_native_snapshot_idle_configure(&native,
        test_idle_clock,0,test_idle_diagnostic,0);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
        "world-a","dbname=muhan")==0,
        "handoff retry fixture must start its native owner");
    supplied_snapshot_tick_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_HANDOFF_FAILED;
    supplied_idle_time=200;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==1&&idle_diagnostic_calls==1&&
        strstr(idle_diagnostic,"handoff")!=0,
        "first handoff failure must retain legacy authority and log once");
    supplied_idle_time=201;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==2&&idle_diagnostic_calls==1,
        "handoff failure must retry on the next cadence without log spam");
    supplied_idle_time=260;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==3&&idle_diagnostic_calls==2,
        "throttled handoff failure diagnostics must resume after their interval");
    native.dependencies.shadow_operations->shutdown(
        native.dependencies.shadow_opaque);
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed;
}

static int test_native_idle_tick_survives_clock_failures_rollbacks_and_limits(void)
{
    character_save_journal_v2_runtime_native native;
    int failed=0;

    reset_fakes();
    failed|=expect(setenv("MUD_M3_PLAYER_SNAPSHOT_V1","handoff",1)==0,
        "test must enable the explicit native snapshot opt-in");
    character_save_journal_v2_runtime_native_init(&native);
    character_save_journal_v2_runtime_native_snapshot_idle_configure(&native,
        test_idle_clock,0,test_idle_diagnostic,0);
    failed|=expect(native.dependencies.shadow_operations->start(
        native.dependencies.shadow_opaque,"/tmp/muhan-runtime-owned",
        "world-a","dbname=muhan")==0,
        "clock edge fixture must start its native owner");
    supplied_snapshot_tick_result=
        CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_HANDOFF_FAILED;
    supplied_idle_time=LONG_MAX;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==1&&idle_diagnostic_calls==1,
        "LONG_MAX must schedule one bounded tick and diagnostic without overflow");
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==1&&idle_diagnostic_calls==1,
        "LONG_MAX must not wrap cadence into repeated ticks");
    supplied_idle_time=10;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==2&&idle_diagnostic_calls==2,
        "clock rollback must reset cadence and diagnostic throttle once");
    supplied_idle_time=11;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==3&&idle_diagnostic_calls==2,
        "rollback epoch must preserve one-second cadence and throttling");
    supplied_idle_time=-1;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==3&&idle_diagnostic_calls==2,
        "negative or failed wall-clock reads must leave the idle state unchanged");
    supplied_idle_time=70;
    character_save_journal_v2_runtime_native_snapshot_idle_tick(&native);
    failed|=expect(process_owner_snapshot_tick_calls==4&&idle_diagnostic_calls==3,
        "throttle must resume after a rollback-safe sixty-second interval");
    native.dependencies.shadow_operations->shutdown(
        native.dependencies.shadow_opaque);
    (void)unsetenv("MUD_M3_PLAYER_SNAPSHOT_V1");
    return failed;
}

int main(void)
{
    int failed=0;
    failed|=test_native_owns_root_and_world_for_process_lifetime();
    failed|=test_native_read_rehearsal_is_exact_default_off_and_diagnostic_only();
    failed|=test_native_snapshot_handoff_opt_in_has_only_explicit_tick();
    failed|=test_native_snapshot_handoff_rejects_near_miss_environment_values();
    failed|=test_native_snapshot_handoff_abi_mismatch_stays_silent();
    failed|=test_native_rejects_unbounded_borrowed_strings_before_connect();
    failed|=test_active_native_reinitialization_is_non_destructive();
    failed|=test_native_start_failures_shutdown_and_remain_retryable();
    failed|=test_native_idle_tick_is_cadenced_bounded_and_silent_when_off();
    failed|=test_native_idle_tick_retries_handoff_failure_with_throttled_log();
    failed|=test_native_idle_tick_survives_clock_failures_rollbacks_and_limits();
    if(failed) return 1;
    printf("character_save_journal_v2_runtime_native_test: ok\n");
    return 0;
}
