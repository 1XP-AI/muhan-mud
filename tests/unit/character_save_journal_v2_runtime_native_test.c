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
#define character_save_journal_v2_rpc_transport_native_init test_rpc_transport_native_init
#define character_save_journal_v2_rpc_transport_native_start test_rpc_transport_native_start
#define character_save_journal_v2_rpc_transport_close test_rpc_transport_close
#define character_save_journal_v2_deadline_native_init test_deadline_native_init
#define character_save_journal_v2_deadline_native_callback test_deadline_native_callback

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
#undef character_save_journal_v2_rpc_transport_native_init
#undef character_save_journal_v2_rpc_transport_native_start
#undef character_save_journal_v2_rpc_transport_close
#undef character_save_journal_v2_deadline_native_init
#undef character_save_journal_v2_deadline_native_callback

#include <stdio.h>

static union {
    long alignment;
    unsigned char bytes[64];
} fake_connection_storage, fake_result_storage;

static int connect_calls;
static int process_owner_init_calls;
static int process_owner_start_calls;
static int process_owner_shutdown_calls;
static int transport_close_calls;
static int default_load_calls;
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
    process_owner_init_calls=0;
    process_owner_start_calls=0;
    process_owner_shutdown_calls=0;
    transport_close_calls=0;
    default_load_calls=0;
    memset(supplied_conninfo,0,sizeof(supplied_conninfo));
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

int main(void)
{
    int failed=0;
    failed|=test_native_owns_root_and_world_for_process_lifetime();
    failed|=test_native_rejects_unbounded_borrowed_strings_before_connect();
    failed|=test_active_native_reinitialization_is_non_destructive();
    if(failed) return 1;
    printf("character_save_journal_v2_runtime_native_test: ok\n");
    return 0;
}
