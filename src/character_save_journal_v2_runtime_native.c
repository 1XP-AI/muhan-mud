#include "character_save_journal_v2_runtime_native.h"
#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
#include "character_save_journal_v2_uuid_native.h"
#endif

#include <libpq-fe.h>
#include <string.h>

#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
#include <stdlib.h>

#define RUNTIME_NATIVE_SERIALIZER_BUFFER_CAPACITY (8UL * 1024UL * 1024UL)
#define RUNTIME_NATIVE_SERIALIZER_MAX_DEPTH 64UL
#define RUNTIME_NATIVE_SERIALIZER_MAX_OBJECTS 8192UL

/* The process-wide PlayerStore can have only one native shadow owner.  Keep
 * the lifecycle sentinel outside caller storage so first-time init may still
 * accept an ordinary uninitialized automatic object safely. */
static character_save_journal_v2_runtime_native *runtime_native_active_owner;

static void runtime_native_wipe(void *memory, unsigned long length)
{
    volatile unsigned char *cursor=(volatile unsigned char *)memory;

    while(length) {
        *cursor++=0;
        length--;
    }
}

static int runtime_native_copy_text(char *destination,
    unsigned long capacity, const char *source)
{
    const char *ending;
    unsigned long length;

    if(!destination||!capacity||!source||!source[0]) return 0;
    ending=(const char *)memchr(source,0,(size_t)capacity);
    if(!ending) return 0;
    length=(unsigned long)(ending-source);
    memcpy(destination,source,(size_t)length+1);
    return 1;
}

static int runtime_native_bounded_text(const char *source,
    unsigned long capacity)
{
    return source&&source[0]&&memchr(source,0,(size_t)capacity)!=0;
}

static int runtime_native_command_uuid(void *opaque,
    char output[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH+1])
{
    (void)opaque;
    return character_save_journal_v2_uuid_generate_native(output)==
        CHARACTER_SAVE_JOURNAL_V2_UUID_OK ? 0 : -1;
}

static int runtime_native_file_load(void *opaque, char *name,
    struct creature **player)
{
    (void)opaque;
    return file_player_store_load(name,player);
}

/* This owns exactly the resources constructed by runtime_native_shadow_start.
 * process_owner intentionally borrows the transport and buffer, therefore its
 * reset/close must precede transport close and buffer wiping. */
static void runtime_native_shadow_shutdown(void *opaque)
{
    character_save_journal_v2_runtime_native *native=
        (character_save_journal_v2_runtime_native *)opaque;

    if(!native) return;
    (void)character_save_journal_v2_process_owner_shutdown(&native->process_owner);
    character_save_journal_v2_rpc_transport_close(&native->transport_native.transport);
    if(native->serializer_buffer) {
        runtime_native_wipe(native->serializer_buffer,
            native->serializer_buffer_capacity);
        free(native->serializer_buffer);
        native->serializer_buffer=0;
    }
    native->serializer_buffer_capacity=0;
    runtime_native_wipe(native->muhan_home,sizeof(native->muhan_home));
    runtime_native_wipe(native->world_id,sizeof(native->world_id));
    native->shadow_active=0;
    if(runtime_native_active_owner==native) runtime_native_active_owner=0;
}

/* The only live path that sees a conninfo.  PQconnectdb is invoked once here,
 * and rpc_transport_native_start accepts ownership of that PGconn whether its
 * readiness assertion succeeds or fails. */
static int runtime_native_shadow_start(void *opaque, const char *muhan_home,
    const char *world_id, const char *conninfo)
{
    character_save_journal_v2_runtime_native *native=
        (character_save_journal_v2_runtime_native *)opaque;
    character_save_journal_v2_process_owner_configuration configuration;
    PGconn *connection;

    if(!native||native->shadow_active||native->serializer_buffer||
       runtime_native_active_owner) return -1;
    runtime_native_active_owner=native;
    runtime_native_wipe(native->muhan_home,sizeof(native->muhan_home));
    runtime_native_wipe(native->world_id,sizeof(native->world_id));
    if(!runtime_native_copy_text(native->muhan_home,
           sizeof(native->muhan_home),muhan_home)||
       !runtime_native_copy_text(native->world_id,
           sizeof(native->world_id),world_id)||
       !runtime_native_bounded_text(conninfo,
           CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX+1UL)) {
        runtime_native_wipe(native->muhan_home,sizeof(native->muhan_home));
        runtime_native_wipe(native->world_id,sizeof(native->world_id));
        runtime_native_active_owner=0;
        return -1;
    }
    native->serializer_buffer=(char *)malloc(RUNTIME_NATIVE_SERIALIZER_BUFFER_CAPACITY);
    if(!native->serializer_buffer) {
        runtime_native_active_owner=0;
        return -1;
    }
    native->serializer_buffer_capacity=RUNTIME_NATIVE_SERIALIZER_BUFFER_CAPACITY;
    character_save_journal_v2_rpc_transport_native_init(&native->transport_native);
    connection=PQconnectdb(conninfo);
    if(character_save_journal_v2_rpc_transport_native_start(&native->transport_native,
        connection)!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK) return -1;
    character_save_journal_v2_deadline_native_init(&native->deadline_native,120);
    memset(&configuration,0,sizeof(configuration));
    configuration.root=native->muhan_home;
    configuration.world_id=native->world_id;
    configuration.transport=&native->transport_native.transport;
    configuration.buffer=native->serializer_buffer;
    configuration.buffer_capacity=native->serializer_buffer_capacity;
    configuration.serializer_limits.max_depth=RUNTIME_NATIVE_SERIALIZER_MAX_DEPTH;
    configuration.serializer_limits.max_objects=RUNTIME_NATIVE_SERIALIZER_MAX_OBJECTS;
    configuration.acquire_deadline=character_save_journal_v2_deadline_native_callback;
    configuration.acquire_deadline_opaque=&native->deadline_native;
    configuration.candidate_uuid=runtime_native_command_uuid;
    configuration.candidate_uuid_opaque=0;
    configuration.file_load=runtime_native_file_load;
    configuration.file_load_opaque=0;
    character_save_journal_v2_process_owner_init(&native->process_owner,&configuration);
    if(character_save_journal_v2_process_owner_start(&native->process_owner)!=
       CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK) return -1;
    native->shadow_active=1;
    return 0;
}

static const character_save_journal_v2_runtime_shadow_operations
runtime_native_shadow_operations={
    runtime_native_shadow_start,runtime_native_shadow_shutdown
};
#endif

static void *runtime_native_connect(void *opaque, const char *conninfo)
{
    (void)opaque;
    return PQconnectdb(conninfo);
}

static int runtime_native_connection_ok(void *connection)
{ return PQstatus((PGconn *)connection)==CONNECTION_OK; }

static void *runtime_native_exec(void *connection, const char *sql)
{ return PQexec((PGconn *)connection,sql); }

static int runtime_native_result_status(void *result)
{ return PQresultStatus((const PGresult *)result)==PGRES_TUPLES_OK ? CHARACTER_SAVE_JOURNAL_V2_RUNTIME_TUPLES_OK : 0; }

static int runtime_native_result_rows(void *result)
{ return PQntuples((const PGresult *)result); }

static int runtime_native_result_columns(void *result)
{ return PQnfields((const PGresult *)result); }

static const char *runtime_native_result_value(void *result, int row, int column)
{ return PQgetvalue((const PGresult *)result,row,column); }

static int runtime_native_result_value_length(void *result, int row, int column)
{ return PQgetlength((const PGresult *)result,row,column); }

static const char *runtime_native_result_sqlstate(void *result)
{ return PQresultErrorField((const PGresult *)result,PG_DIAG_SQLSTATE); }

static void runtime_native_result_clear(void *result)
{ PQclear((PGresult *)result); }

static void runtime_native_connection_finish(void *connection)
{ PQfinish((PGconn *)connection); }

static const character_save_journal_v2_runtime_database_operations runtime_native_database_operations={
    runtime_native_connect,runtime_native_connection_ok,runtime_native_exec,
    runtime_native_result_status,runtime_native_result_rows,runtime_native_result_columns,
    runtime_native_result_value,runtime_native_result_value_length,
    runtime_native_result_sqlstate,runtime_native_result_clear,
    runtime_native_connection_finish
};

void character_save_journal_v2_runtime_native_init(
    character_save_journal_v2_runtime_native *native)
{
    if(!native) return;
#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
    if(runtime_native_active_owner==native) return;
#endif
    memset(native,0,sizeof(*native));
    native->dependencies.environment_get=0;
    native->dependencies.environment_opaque=0;
    native->dependencies.database_operations=&runtime_native_database_operations;
    native->dependencies.database_opaque=0;
    native->dependencies.file_operations=0;
    native->dependencies.file_opaque=0;
#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
    character_save_journal_v2_rpc_transport_native_init(&native->transport_native);
    native->dependencies.shadow_operations=&runtime_native_shadow_operations;
    native->dependencies.shadow_opaque=native;
#else
    native->dependencies.shadow_operations=0;
    native->dependencies.shadow_opaque=0;
#endif
}
