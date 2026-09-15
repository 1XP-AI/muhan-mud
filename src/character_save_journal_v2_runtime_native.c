#include "character_save_journal_v2_runtime_native.h"
#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
#include "player_snapshot_v1.h"
#include "character_player_snapshot_v1_receipt_pair.h"
#include "character_player_snapshot_v1_read_rehearsal.h"
#include "character_save_journal_v2_uuid_native.h"
#endif

#include <libpq-fe.h>
#include <string.h>

#if defined(__linux__) && !defined(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PROBE_ONLY)
#include <stdlib.h>
#include <stdio.h>
#include <limits.h>
#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>

#define RUNTIME_NATIVE_SERIALIZER_BUFFER_CAPACITY (8UL * 1024UL * 1024UL)
#define RUNTIME_NATIVE_SERIALIZER_MAX_DEPTH 64UL
#define RUNTIME_NATIVE_SERIALIZER_MAX_OBJECTS 8192UL
#define RUNTIME_NATIVE_SNAPSHOT_HANDOFF_ENV "MUD_M3_PLAYER_SNAPSHOT_V1"
#define RUNTIME_NATIVE_SNAPSHOT_HANDOFF_VALUE "handoff"
#define RUNTIME_NATIVE_READ_REHEARSAL_COMMAND_ENV \
    "MUD_M3_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_COMMAND_UUID"
#define RUNTIME_NATIVE_SNAPSHOT_IDLE_CADENCE_SECONDS 1L
#define RUNTIME_NATIVE_SNAPSHOT_IDLE_FAILURE_LOG_SECONDS 60L
#define RUNTIME_NATIVE_ACTIVATION_RESERVATION_DIRECTORY \
    "onboarding-activation-reservations"

#ifndef O_DIRECTORY
#define O_DIRECTORY 0
#endif
#ifndef O_NOFOLLOW
#define O_NOFOLLOW 0
#endif
#ifndef O_CLOEXEC
#define O_CLOEXEC 0
#endif

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

static int runtime_native_snapshot_handoff_enabled(void)
{
    const char *value=getenv(RUNTIME_NATIVE_SNAPSHOT_HANDOFF_ENV);

    return value && !strcmp(value,RUNTIME_NATIVE_SNAPSHOT_HANDOFF_VALUE) &&
        player_snapshot_v1_native_abi_supported(CHAR_BIT,
            sizeof(short)*CHAR_BIT,sizeof(long)*CHAR_BIT,
            LONG_MIN<=INT64_MIN&&LONG_MAX>=INT64_MAX,PLAYER);
}

static int runtime_native_read_rehearsal_command_uuid(
    char output[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH+1])
{
    const char *value;
    unsigned int index;

    if(!output) return 0;
    value=getenv(RUNTIME_NATIVE_READ_REHEARSAL_COMMAND_ENV);
    if(!value) return 0;
    for(index=0;index<CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH;
        index++) {
        if(!value[index]) return 0;
        if(index==8U||index==13U||index==18U||index==23U) {
            if(value[index]!='-') return 0;
        } else if(!((value[index]>='0'&&value[index]<='9')||
                    (value[index]>='a'&&value[index]<='f')))
            return 0;
    }
    if(value[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH]) return 0;
    if(value[14]!='4'||(value[19]!='8'&&value[19]!='9'&&
        value[19]!='a'&&value[19]!='b')) return 0;
    memcpy(output,value,CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH+1);
    return 1;
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
    character_save_journal_v2_runtime_native *native=
        (character_save_journal_v2_runtime_native *)opaque;
    character_player_snapshot_v1_read_rehearsal rehearsal;

    if(!native||runtime_native_active_owner!=native||!native->shadow_active||
       !native->read_rehearsal_armed||
       native->read_rehearsal_artifact_directory_fd<0)
        return player_store_default_load(name,player);
    memset(&rehearsal,0,sizeof(rehearsal));
    rehearsal.writer=&native->process_owner.held_writer;
    rehearsal.artifact_directory_fd=native->read_rehearsal_artifact_directory_fd;
    rehearsal.expected_artifact=&native->read_rehearsal_artifact;
    return character_player_snapshot_v1_read_rehearsal_load(&rehearsal,name,
        player,&native->read_rehearsal_last_result);
}

static int runtime_native_read_rehearsal_artifact_directory_open(
    character_save_journal_v2_runtime_native *native)
{
    struct stat status;
    int root,directory;

    if(!native||!native->muhan_home[0]) return -1;
    root=-1;
    directory=-1;
    root=open(native->muhan_home,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(root<0) goto failed;
    directory=openat(root,CHARACTER_PLAYER_SNAPSHOT_V1_CAPTURE_DIRECTORY,
        O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(directory<0||fstat(directory,&status)||!S_ISDIR(status.st_mode)||
       status.st_uid!=geteuid()||(status.st_mode&0777)!=0700) goto failed;
    if(close(root)) {
        close(directory);
        return -1;
    }
    native->read_rehearsal_artifact_directory_fd=directory;
    return 0;
failed:
    if(directory>=0) close(directory);
    if(root>=0) close(root);
    return -1;
}

static void runtime_native_read_rehearsal_arm(
    character_save_journal_v2_runtime_native *native)
{
    char command_id[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH+1];
    int artifact_result;

    if(!native||!native->shadow_active||
       !runtime_native_read_rehearsal_command_uuid(command_id)) return;
    if(runtime_native_read_rehearsal_artifact_directory_open(native)) {
        native->read_rehearsal_last_result=
            CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH;
        return;
    }
    artifact_result=character_player_snapshot_v1_artifact_load_metadata_for_command(
        native->read_rehearsal_artifact_directory_fd,command_id,
        &native->read_rehearsal_artifact);
    if(artifact_result!=CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK) {
        close(native->read_rehearsal_artifact_directory_fd);
        native->read_rehearsal_artifact_directory_fd=-1;
        runtime_native_wipe(&native->read_rehearsal_artifact,
            sizeof(native->read_rehearsal_artifact));
        native->read_rehearsal_last_result=
            CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_ARTIFACT_MISMATCH;
        return;
    }
    native->read_rehearsal_armed=1;
}

/* This is the native caller that owns the directory FD.  The generic owner,
 * adapter, bridge, and command layer receive only a borrowed descriptor. */
static int runtime_native_activation_reservation_directory_open(
    character_save_journal_v2_runtime_native *native)
{
    struct stat status;
    int root, directory;

    if(!native || !native->muhan_home[0]) return -1;
    root=-1; directory=-1;
    root=open(native->muhan_home,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(root<0) goto failed;
    if(mkdirat(root,RUNTIME_NATIVE_ACTIVATION_RESERVATION_DIRECTORY,0700) &&
       errno!=EEXIST) goto failed;
    directory=openat(root,RUNTIME_NATIVE_ACTIVATION_RESERVATION_DIRECTORY,
        O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
    if(directory<0 || fstat(directory,&status) || !S_ISDIR(status.st_mode) ||
       status.st_uid!=geteuid() || (status.st_mode&0777)!=0700) goto failed;
    close(root);
    native->activation_reservation_directory_fd=directory;
    return 0;
failed:
    if(directory>=0) close(directory);
    if(root>=0) close(root);
    return -1;
}

/* This owns exactly the resources constructed by runtime_native_shadow_start.
 * process_owner intentionally borrows the transport and buffer, therefore its
 * reset/close must precede transport close and buffer wiping. */
static void runtime_native_shadow_shutdown(void *opaque)
{
    character_save_journal_v2_runtime_native *native=
        (character_save_journal_v2_runtime_native *)opaque;

    if(!native) return;
    /* Unbind first in the callback's view: process_owner shutdown restores
     * the prior PlayerStore before any native-owned rehearsal state can go. */
    native->read_rehearsal_armed=0;
    (void)character_save_journal_v2_process_owner_shutdown(&native->process_owner);
    if(native->activation_reservation_directory_fd>=0) {
        close(native->activation_reservation_directory_fd);
        native->activation_reservation_directory_fd=-1;
    }
    if(native->read_rehearsal_artifact_directory_fd>=0) {
        close(native->read_rehearsal_artifact_directory_fd);
        native->read_rehearsal_artifact_directory_fd=-1;
    }
    runtime_native_wipe(&native->read_rehearsal_artifact,
        sizeof(native->read_rehearsal_artifact));
    native->read_rehearsal_last_result=
        CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DISABLED;
    character_save_journal_v2_rpc_transport_close(&native->transport_native.transport);
    if(native->serializer_buffer) {
        runtime_native_wipe(native->serializer_buffer,
            native->serializer_buffer_capacity);
        free(native->serializer_buffer);
        native->serializer_buffer=0;
    }
    native->serializer_buffer_capacity=0;
    runtime_native_wipe(&native->snapshot_handoff,sizeof(native->snapshot_handoff));
    runtime_native_wipe(&native->snapshot_capture,sizeof(native->snapshot_capture));
    native->snapshot_handoff_enabled=0;
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
        goto failed;
    }
    native->serializer_buffer=(char *)malloc(RUNTIME_NATIVE_SERIALIZER_BUFFER_CAPACITY);
    if(!native->serializer_buffer) goto failed;
    native->serializer_buffer_capacity=RUNTIME_NATIVE_SERIALIZER_BUFFER_CAPACITY;
    character_save_journal_v2_rpc_transport_native_init(&native->transport_native);
    connection=PQconnectdb(conninfo);
    if(character_save_journal_v2_rpc_transport_native_start(&native->transport_native,
        connection)!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK) goto failed;
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
    configuration.file_load_opaque=native;
    native->snapshot_handoff_enabled=0;
    if(runtime_native_snapshot_handoff_enabled()) {
        character_player_snapshot_v1_capture_native_init(&native->snapshot_capture);
        character_player_snapshot_v1_handoff_init(&native->snapshot_handoff,
            &native->snapshot_capture);
        character_player_snapshot_v1_handoff_enable_receipt_pair(
            &native->snapshot_handoff,
            character_player_snapshot_v1_receipt_pair_commit);
        configuration.snapshot_handoff=&native->snapshot_handoff;
        native->snapshot_handoff_enabled=1;
    }
    character_save_journal_v2_process_owner_init(&native->process_owner,&configuration);
    if(character_save_journal_v2_process_owner_start(&native->process_owner)!=
       CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK) {
        /* Fixed numeric outcomes only: never print connection or player data. */
        fprintf(stderr,"M3 owner startup failed: startup=%d recovery=%d\n",
            (int)native->process_owner.startup_result,
            (int)native->process_owner.recovery_result);
        {
            unsigned int outcome;
            for(outcome=0;outcome<=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_IO;outcome++)
                if(native->process_owner.recovery_report.publish_results[outcome])
                    fprintf(stderr,"M3 recovery publish: outcome=%u count=%u\n",
                        outcome,native->process_owner.recovery_report.publish_results[outcome]);
            for(outcome=0;outcome<=CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE;outcome++)
                if(native->process_owner.recovery_report.ack_results[outcome])
                    fprintf(stderr,"M3 recovery ack: outcome=%u count=%u\n",
                        outcome,native->process_owner.recovery_report.ack_results[outcome]);
        }
        goto failed;
    }
    native->shadow_active=1;
    runtime_native_read_rehearsal_arm(native);
    if(native->snapshot_handoff_enabled)
        (void)runtime_native_activation_reservation_directory_open(native);
    return 0;

failed:
    runtime_native_shadow_shutdown(native);
    return -1;
}

static const character_save_journal_v2_runtime_shadow_operations
runtime_native_shadow_operations={
    runtime_native_shadow_start,runtime_native_shadow_shutdown
};

character_save_journal_v2_process_owner_snapshot_tick_result
character_save_journal_v2_runtime_native_snapshot_tick(
    character_save_journal_v2_runtime_native *native, unsigned int limit)
{
    if(!native || !native->shadow_active || !native->snapshot_handoff_enabled)
        return CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_OFF;
    return character_save_journal_v2_process_owner_snapshot_tick(
        &native->process_owner,limit);
}

int character_save_journal_v2_runtime_native_activation_reservation_directory_fd(
    const character_save_journal_v2_runtime_native *native)
{
    if(!native || !native->shadow_active || !native->snapshot_handoff_enabled)
        return -1;
    return native->activation_reservation_directory_fd;
}

void character_save_journal_v2_runtime_native_snapshot_idle_configure(
    character_save_journal_v2_runtime_native *native,
    long (*clock)(void *opaque), void *clock_opaque,
    void (*diagnostic)(void *opaque, const char *message),
    void *diagnostic_opaque)
{
    if(!native) return;
    native->snapshot_idle_clock=clock;
    native->snapshot_idle_clock_opaque=clock_opaque;
    native->snapshot_idle_diagnostic=diagnostic;
    native->snapshot_idle_diagnostic_opaque=diagnostic_opaque;
    native->snapshot_idle_next_at=0;
    native->snapshot_idle_last_clock_at=0;
    native->snapshot_idle_last_failure_log_at=0;
    native->snapshot_idle_clock_seen=0;
    native->snapshot_idle_cadence_exhausted=0;
    native->snapshot_idle_failure_logged=0;
}

/* The host still supplies wall time, so an invalid read is ignored and a
 * backward step starts a fresh cadence epoch.  Saturation deliberately
 * stops at LONG_MAX rather than wrapping the next one-second deadline. */
static int runtime_native_snapshot_idle_due(
    character_save_journal_v2_runtime_native *native, long now)
{
    if(now<0) return 0;
    if(native->snapshot_idle_clock_seen &&
       now<native->snapshot_idle_last_clock_at) {
        native->snapshot_idle_failure_logged=0;
        native->snapshot_idle_cadence_exhausted=0;
    } else if(native->snapshot_idle_cadence_exhausted) {
        native->snapshot_idle_last_clock_at=now;
        native->snapshot_idle_clock_seen=1;
        return 0;
    } else if(native->snapshot_idle_clock_seen &&
       now<native->snapshot_idle_next_at) {
        native->snapshot_idle_last_clock_at=now;
        return 0;
    }
    native->snapshot_idle_last_clock_at=now;
    native->snapshot_idle_clock_seen=1;
    if(now==LONG_MAX) {
        native->snapshot_idle_next_at=LONG_MAX;
        native->snapshot_idle_cadence_exhausted=1;
    } else
        native->snapshot_idle_next_at=now+
            RUNTIME_NATIVE_SNAPSHOT_IDLE_CADENCE_SECONDS;
    return 1;
}

static int runtime_native_snapshot_idle_failure_log_due(
    character_save_journal_v2_runtime_native *native, long now)
{
    if(!native->snapshot_idle_failure_logged) return 1;
    if(now<native->snapshot_idle_last_failure_log_at) return 1;
    return now-native->snapshot_idle_last_failure_log_at>=
        RUNTIME_NATIVE_SNAPSHOT_IDLE_FAILURE_LOG_SECONDS;
}

void character_save_journal_v2_runtime_native_snapshot_idle_tick(
    character_save_journal_v2_runtime_native *native)
{
    character_save_journal_v2_process_owner_snapshot_tick_result result;
    long now;

    if(!native||!native->snapshot_handoff_enabled||!native->shadow_active||
       !native->snapshot_idle_clock) return;
    now=native->snapshot_idle_clock(native->snapshot_idle_clock_opaque);
    if(!runtime_native_snapshot_idle_due(native,now)) return;
    result=character_save_journal_v2_runtime_native_snapshot_tick(native,1);
    if(result!=CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SNAPSHOT_TICK_HANDOFF_FAILED)
        return;
    if(runtime_native_snapshot_idle_failure_log_due(native,now)) {
        if(native->snapshot_idle_diagnostic) native->snapshot_idle_diagnostic(
            native->snapshot_idle_diagnostic_opaque,
            "M3 PlayerSnapshotV1 handoff tick failed; will retry");
        native->snapshot_idle_last_failure_log_at=now;
        native->snapshot_idle_failure_logged=1;
    }
}
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
    native->activation_reservation_directory_fd=-1;
    native->read_rehearsal_artifact_directory_fd=-1;
    native->read_rehearsal_last_result=
        CHARACTER_PLAYER_SNAPSHOT_V1_READ_REHEARSAL_DISABLED;
    character_save_journal_v2_rpc_transport_native_init(&native->transport_native);
    native->dependencies.shadow_operations=&runtime_native_shadow_operations;
    native->dependencies.shadow_opaque=native;
#else
    native->dependencies.shadow_operations=0;
    native->dependencies.shadow_opaque=0;
#endif
}
