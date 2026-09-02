/*
 * TDD record:
 * RED: this contract was compiled before the runtime module existed and the
 * missing public header caused compilation to fail.
 * GREEN: cc -std=gnu89 -fcommon -Wall -Wextra -Werror -I src
 *   tests/unit/character_save_journal_v2_runtime_test.c
 *   src/character_save_journal_v2_runtime.c -o /tmp/muhan-unit/m3_runtime_test
 * Sanitizer: the same command with -O1 -fno-omit-frame-pointer
 *   -fsanitize=address,undefined must exit successfully.
 */
#include "character_save_journal_v2_runtime.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct fake_environment {
    const char *mode;
    const char *muhan_home;
    const char *world_id;
    const char *conninfo_file;
} fake_environment;

typedef struct fake_database {
    int connect_calls, connection_ok_calls, execute_calls, status_calls;
    int rows_calls, columns_calls, value_calls, value_length_calls, sqlstate_calls;
    int clear_calls, finish_calls;
    int connection_ok, status, rows, columns;
    const char *value;
    const char *sqlstate;
    char sql[80];
    char supplied_conninfo[128];
} fake_database;

typedef struct fake_files {
    int open_calls;
} fake_files;

typedef struct fake_secret_file {
    int open_calls, stat_calls, read_calls, close_calls;
    const char *bytes;
    size_t length;
    int short_read, close_error, mutate_during_read;
} fake_secret_file;

typedef struct fake_shadow {
    int start_calls, shutdown_calls, start_result;
    char supplied_home[128];
    char supplied_world_id[128];
    char supplied_conninfo[128];
} fake_shadow;

static int expect(int condition, const char *message)
{
    if(condition) return 0;
    fprintf(stderr,"character_save_journal_v2_runtime_test: %s\n",message);
    return 1;
}

static const char *fake_getenv(void *opaque, const char *name)
{
    fake_environment *environment=(fake_environment *)opaque;
    if(!strcmp(name,"MUD_M3_MODE")) return environment->mode;
    if(!strcmp(name,"MUHAN_HOME")) return environment->muhan_home;
    if(!strcmp(name,"MUD_M3_WORLD_ID")) return environment->world_id;
    if(!strcmp(name,"MUD_M3_CONNINFO_FILE")) return environment->conninfo_file;
    return 0;
}

static void *fake_connect(void *opaque, const char *conninfo)
{
    fake_database *database=(fake_database *)opaque;
    database->connect_calls++;
    (void)snprintf(database->supplied_conninfo,sizeof(database->supplied_conninfo),"%s",conninfo);
    return database;
}

static int fake_connection_ok(void *connection)
{ fake_database *database=(fake_database *)connection; database->connection_ok_calls++; return database->connection_ok; }

static void *fake_exec(void *connection, const char *sql)
{
    fake_database *database=(fake_database *)connection;
    database->execute_calls++;
    (void)snprintf(database->sql,sizeof(database->sql),"%s",sql);
    return database;
}

static int fake_status(void *result)
{ fake_database *database=(fake_database *)result; database->status_calls++; return database->status; }
static int fake_rows(void *result)
{ fake_database *database=(fake_database *)result; database->rows_calls++; return database->rows; }
static int fake_columns(void *result)
{ fake_database *database=(fake_database *)result; database->columns_calls++; return database->columns; }
static const char *fake_value(void *result, int row, int column)
{ fake_database *database=(fake_database *)result; database->value_calls++; return row==0&&column==0 ? database->value : 0; }
static int fake_value_length(void *result, int row, int column)
{ fake_database *database=(fake_database *)result; database->value_length_calls++; return row==0&&column==0&&database->value ? (int)strlen(database->value) : -1; }
static const char *fake_sqlstate(void *result)
{ fake_database *database=(fake_database *)result; database->sqlstate_calls++; return database->sqlstate; }
static void fake_clear(void *result)
{ fake_database *database=(fake_database *)result; database->clear_calls++; }
static void fake_finish(void *connection)
{ fake_database *database=(fake_database *)connection; database->finish_calls++; }

static const character_save_journal_v2_runtime_database_operations fake_database_operations={
    fake_connect,fake_connection_ok,fake_exec,fake_status,fake_rows,fake_columns,
    fake_value,fake_value_length,fake_sqlstate,fake_clear,fake_finish
};

static int fake_open(void *opaque, const char *path)
{ fake_files *files=(fake_files *)opaque; (void)path; files->open_calls++; return -1; }

static const character_save_journal_v2_runtime_file_operations fake_file_operations={
    fake_open,0,0,0
};

static int fake_secret_open(void *opaque, const char *path)
{ fake_secret_file *file=(fake_secret_file *)opaque; (void)path; file->open_calls++; return 7; }
static int fake_secret_stat(void *opaque, int descriptor, struct stat *status)
{
    fake_secret_file *file=(fake_secret_file *)opaque;
    (void)descriptor; file->stat_calls++; memset(status,0,sizeof(*status));
    status->st_mode=S_IFREG|0600; status->st_nlink=1; status->st_uid=geteuid(); status->st_size=(off_t)file->length;
    status->st_mtime=11; status->st_ctime=13;
#if defined(__APPLE__)
    status->st_mtimespec.tv_nsec=file->mutate_during_read&&file->read_calls ? 2 : 1;
    status->st_ctimespec.tv_nsec=3;
#elif defined(__linux__) || defined(__FreeBSD__) || defined(__NetBSD__) || defined(__OpenBSD__)
    status->st_mtim.tv_nsec=file->mutate_during_read&&file->read_calls ? 2 : 1;
    status->st_ctim.tv_nsec=3;
#endif
    return 0;
}
static long fake_secret_read(void *opaque, int descriptor, void *buffer, size_t length)
{
    fake_secret_file *file=(fake_secret_file *)opaque;
    size_t count;
    (void)descriptor; file->read_calls++;
    if(file->short_read&&file->read_calls>1) return 0;
    if(file->read_calls>1) return 0;
    count=file->short_read ? file->length-1 : file->length;
    if(count>length) count=length;
    memcpy(buffer,file->bytes,count);
    return (long)count;
}
static int fake_secret_close(void *opaque, int descriptor)
{ fake_secret_file *file=(fake_secret_file *)opaque; (void)descriptor; file->close_calls++; return file->close_error ? -1 : 0; }
static const character_save_journal_v2_runtime_file_operations fake_secret_file_operations={
    fake_secret_open,fake_secret_stat,fake_secret_read,fake_secret_close
};

static int fake_shadow_start(void *opaque, const char *muhan_home,
                             const char *world_id, const char *conninfo)
{
    fake_shadow *shadow=(fake_shadow *)opaque;
    shadow->start_calls++;
    (void)snprintf(shadow->supplied_home,sizeof(shadow->supplied_home),"%s",muhan_home);
    (void)snprintf(shadow->supplied_world_id,sizeof(shadow->supplied_world_id),"%s",world_id);
    (void)snprintf(shadow->supplied_conninfo,sizeof(shadow->supplied_conninfo),"%s",conninfo);
    return shadow->start_result;
}

static void fake_shadow_shutdown(void *opaque)
{ fake_shadow *shadow=(fake_shadow *)opaque; shadow->shutdown_calls++; }

static const character_save_journal_v2_runtime_shadow_operations fake_shadow_operations={
    fake_shadow_start,fake_shadow_shutdown
};

static int bytes_are_zero(const void *bytes, size_t length)
{
    const unsigned char *cursor=(const unsigned char *)bytes;
    while(length) {
        if(*cursor++) return 0;
        length--;
    }
    return 1;
}

static void fake_database_ready(fake_database *database)
{
    memset(database,0,sizeof(*database));
    database->connection_ok=1;
    database->status=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_TUPLES_OK;
    database->rows=1; database->columns=1; database->value="t";
}

static void runtime_init(character_save_journal_v2_runtime *runtime,
                         fake_environment *environment, fake_database *database,
                         fake_files *files, int fake_file_system)
{
    character_save_journal_v2_runtime_dependencies dependencies;
    memset(&dependencies,0,sizeof(dependencies));
    dependencies.environment_get=fake_getenv;
    dependencies.environment_opaque=environment;
    dependencies.database_operations=&fake_database_operations;
    dependencies.database_opaque=database;
    if(fake_file_system) {
        dependencies.file_operations=&fake_file_operations;
        dependencies.file_opaque=files;
    }
    character_save_journal_v2_runtime_init(runtime,&dependencies);
}

static void runtime_shadow_init(character_save_journal_v2_runtime *runtime,
                                fake_environment *environment, fake_database *database,
                                fake_secret_file *file, fake_shadow *shadow)
{
    character_save_journal_v2_runtime_dependencies dependencies;
    memset(&dependencies,0,sizeof(dependencies));
    dependencies.environment_get=fake_getenv;
    dependencies.environment_opaque=environment;
    dependencies.database_operations=&fake_database_operations;
    dependencies.database_opaque=database;
    dependencies.file_operations=&fake_secret_file_operations;
    dependencies.file_opaque=file;
    dependencies.shadow_operations=&fake_shadow_operations;
    dependencies.shadow_opaque=shadow;
    character_save_journal_v2_runtime_init(runtime,&dependencies);
}

static int make_secret_bytes(char *path, size_t path_size, const char *contents, size_t length, mode_t mode)
{
    int descriptor;
    (void)snprintf(path,path_size,"/tmp/m3-runtime-test-XXXXXX");
    descriptor=mkstemp(path);
    if(descriptor<0) return -1;
    if(fchmod(descriptor,mode)!=0 || write(descriptor,contents,length)!=(ssize_t)length || close(descriptor)!=0) {
        (void)unlink(path);
        return -1;
    }
    return 0;
}

static int make_secret_file(char *path, size_t path_size, const char *contents, mode_t mode)
{ return make_secret_bytes(path,path_size,contents,strlen(contents),mode); }

static int test_disabled_never_touches_file_or_database(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; fake_files files; int failed=0;
    memset(&environment,0,sizeof(environment)); fake_database_ready(&database); memset(&files,0,sizeof(files));
    runtime_init(&runtime,&environment,&database,&files,1);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,"absent mode must be disabled");
    failed|=expect(files.open_calls==0&&database.connect_calls==0,"absent mode must not call file or database callbacks");
    environment.mode="off";
    runtime_init(&runtime,&environment,&database,&files,1);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,"off mode must be disabled");
    failed|=expect(files.open_calls==0&&database.connect_calls==0,"off mode must not call file or database callbacks");
    return failed;
}

static int test_probe_ready_and_exact_assertion(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; char path[64]; int failed=0;
    failed|=expect(make_secret_file(path,sizeof(path),"host=localhost dbname=m3\n",0600)==0,"cannot create secret file");
    memset(&environment,0,sizeof(environment)); environment.mode="probe"; environment.world_id="world-a"; environment.conninfo_file=path;
    fake_database_ready(&database); runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY,"valid probe must be ready");
    failed|=expect(database.connect_calls==1&&database.execute_calls==1&&database.clear_calls==1&&database.finish_calls==1,"valid probe must own one database round trip");
    failed|=expect(!strcmp(database.sql,"select private.m3_assert_writer_session()"),"writer assertion SQL must be exact");
    failed|=expect(!strcmp(database.supplied_conninfo,"host=localhost dbname=m3"),"only a trailing newline may be normalized");
    failed|=expect(runtime.conninfo_length==0&&runtime.conninfo[0]==0,"ready probe must not retain conninfo bytes");
    character_save_journal_v2_runtime_shutdown(&runtime);
    failed|=expect(character_save_journal_v2_runtime_get_state(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,"shutdown must clear ready state");
    (void)unlink(path);
    return failed;
}

static int test_fail_closed_database_evidence(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; char path[64]; int failed=0;
    failed|=expect(make_secret_file(path,sizeof(path),"dbname=m3",0600)==0,"cannot create secret file");
    memset(&environment,0,sizeof(environment)); environment.mode="probe"; environment.world_id="world-a"; environment.conninfo_file=path;
    fake_database_ready(&database); database.value="f"; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"false assertion result must fail");
    fake_database_ready(&database); database.rows=2; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"extra database row must fail");
    fake_database_ready(&database); database.columns=2; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"wrong database shape must fail");
    fake_database_ready(&database); database.sqlstate="08006"; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"SQLSTATE must fail closed");
    fake_database_ready(&database); database.connection_ok=0; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"offline database must fail closed");
    (void)unlink(path);
    return failed;
}

static int test_probe_rejects_invalid_mode_and_secret_files(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; static const char nul_secret[]={'d','b','=',0,'m','3'}; char path[64], link_path[80], large[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX+2]; int failed=0;
    memset(&environment,0,sizeof(environment)); environment.mode="shadow"; environment.world_id="world-a"; environment.conninfo_file="/not-used"; fake_database_ready(&database);
    runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&database.connect_calls==0,"unsupported mode must fail before database access");
    environment.mode="probe"; environment.conninfo_file="relative"; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"relative secret path must fail");
    environment.conninfo_file="/not-used"; environment.world_id=0; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"probe must require a world id");
    environment.world_id="world-a"; environment.conninfo_file=0; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"probe must require a conninfo file");
    failed|=expect(make_secret_file(path,sizeof(path),"dbname=m3",0640)==0,"cannot create unsafe secret file");
    environment.conninfo_file=path; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"group-readable secret file must fail");
    (void)snprintf(link_path,sizeof(link_path),"%s.link",path); (void)unlink(link_path); (void)unlink(path);
    failed|=expect(make_secret_file(path,sizeof(path),"dbname=m3",0600)==0,"cannot create symlink target");
    failed|=expect(symlink(path,link_path)==0,"cannot create symlink"); environment.conninfo_file=link_path; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"symlink secret file must fail");
    (void)unlink(link_path); (void)unlink(path);
    failed|=expect(make_secret_file(path,sizeof(path),"db\nname=m3",0600)==0,"cannot create control secret file"); environment.conninfo_file=path; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"embedded control byte must fail");
    (void)unlink(path); failed|=expect(make_secret_bytes(path,sizeof(path),nul_secret,sizeof(nul_secret),0600)==0,"cannot create NUL secret file"); environment.conninfo_file=path; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"NUL secret byte must fail");
    (void)unlink(path); failed|=expect(make_secret_file(path,sizeof(path),"",0600)==0,"cannot create empty secret file"); environment.conninfo_file=path; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"empty secret file must fail");
    (void)unlink(path); memset(large,'a',sizeof(large)); large[sizeof(large)-1]='\0';
    failed|=expect(make_secret_file(path,sizeof(path),large,0600)==0,"cannot create oversized secret file"); environment.conninfo_file=path; runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,"oversized secret file must fail");
    (void)unlink(path);
    return failed;
}

static int test_probe_rejects_short_read_and_close_error(void)
{
    character_save_journal_v2_runtime runtime;
    character_save_journal_v2_runtime_dependencies dependencies;
    fake_environment environment; fake_database database; fake_secret_file file; int failed=0;
    memset(&environment,0,sizeof(environment)); environment.mode="probe"; environment.world_id="world-a"; environment.conninfo_file="/fake-secret";
    fake_database_ready(&database); memset(&file,0,sizeof(file)); file.bytes="dbname=m3"; file.length=strlen(file.bytes); file.short_read=1;
    memset(&dependencies,0,sizeof(dependencies)); dependencies.environment_get=fake_getenv; dependencies.environment_opaque=&environment;
    dependencies.database_operations=&fake_database_operations; dependencies.database_opaque=&database;
    dependencies.file_operations=&fake_secret_file_operations; dependencies.file_opaque=&file;
    character_save_journal_v2_runtime_init(&runtime,&dependencies);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&file.close_calls==1&&database.connect_calls==0,"short read must fail before database access and close once");
    fake_database_ready(&database); memset(&file,0,sizeof(file)); file.bytes="dbname=m3"; file.length=strlen(file.bytes); file.close_error=1;
    dependencies.database_opaque=&database; dependencies.file_opaque=&file; character_save_journal_v2_runtime_init(&runtime,&dependencies);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&file.close_calls==1&&database.connect_calls==0,"close error must fail before database access");
    return failed;
}

static int test_probe_requires_writer_world_id_grammar(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; char path[64], maximum[66];
    const char *invalid[]={"Aworld","9world","-world","_world","world.name","world/child",0};
    size_t index;
    int failed=0;
    memset(maximum,'a',sizeof(maximum)-1); maximum[64]=0;
    failed|=expect(make_secret_file(path,sizeof(path),"dbname=m3",0600)==0,"cannot create secret file");
    memset(&environment,0,sizeof(environment)); environment.mode="probe"; environment.conninfo_file=path;
    environment.world_id="a"; fake_database_ready(&database); runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY,"one lowercase letter must be a valid writer world id");
    environment.world_id=maximum; fake_database_ready(&database); runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY,"64 lowercase letters must be a valid writer world id");
    maximum[64]='a'; maximum[65]=0; environment.world_id=maximum; fake_database_ready(&database); runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&database.connect_calls==0,"65 byte world id must fail before database access");
    for(index=0;invalid[index];index++) {
        environment.world_id=invalid[index]; fake_database_ready(&database); runtime_init(&runtime,&environment,&database,0,0);
        failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&database.connect_calls==0,"world id outside writer grammar must fail before database access");
    }
    (void)unlink(path);
    return failed;
}

static int test_probe_detects_nanosecond_secret_mutation_and_wipes_on_failure(void)
{
    character_save_journal_v2_runtime runtime;
    character_save_journal_v2_runtime_dependencies dependencies;
    fake_environment environment; fake_database database; fake_secret_file file; int failed=0;
    memset(&environment,0,sizeof(environment)); environment.mode="probe"; environment.world_id="world-a"; environment.conninfo_file="/fake-secret";
    fake_database_ready(&database); memset(&file,0,sizeof(file)); file.bytes="dbname=m3"; file.length=strlen(file.bytes); file.mutate_during_read=1;
    memset(&dependencies,0,sizeof(dependencies)); dependencies.environment_get=fake_getenv; dependencies.environment_opaque=&environment;
    dependencies.database_operations=&fake_database_operations; dependencies.database_opaque=&database;
    dependencies.file_operations=&fake_secret_file_operations; dependencies.file_opaque=&file;
    character_save_journal_v2_runtime_init(&runtime,&dependencies);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&database.connect_calls==0,"same-second nanosecond secret mutation must fail before database access");
    failed|=expect(runtime.conninfo_length==0&&bytes_are_zero(runtime.conninfo,sizeof(runtime.conninfo)),"failed probe must wipe every conninfo byte");
    return failed;
}

static int test_probe_wipes_conninfo_without_losing_ready_state(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; char path[64]; int failed=0;
    failed|=expect(make_secret_file(path,sizeof(path),"dbname=m3",0600)==0,"cannot create secret file");
    memset(&environment,0,sizeof(environment)); environment.mode="probe"; environment.world_id="world-a"; environment.conninfo_file=path;
    fake_database_ready(&database); runtime_init(&runtime,&environment,&database,0,0);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY,"valid probe must remain ready after conninfo cleanup");
    failed|=expect(character_save_journal_v2_runtime_get_state(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY&&runtime.conninfo_length==0&&bytes_are_zero(runtime.conninfo,sizeof(runtime.conninfo)),"ready probe must wipe conninfo without changing ready state");
    (void)unlink(path);
    return failed;
}

static int test_shadow_requires_explicit_environment_before_conninfo_io(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; fake_secret_file file;
    fake_shadow shadow; int failed=0;
    memset(&environment,0,sizeof(environment)); environment.mode="shadow";
    environment.world_id="world-a"; environment.conninfo_file="/fake-secret";
    fake_database_ready(&database); memset(&file,0,sizeof(file));
    file.bytes="dbname=m3"; file.length=strlen(file.bytes); memset(&shadow,0,sizeof(shadow));
    runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,
                   "shadow must require explicit MUHAN_HOME");
    failed|=expect(file.open_calls==0&&shadow.start_calls==0&&database.connect_calls==0,
                   "missing MUHAN_HOME must fail before conninfo, shadow, and probe database access");
    environment.muhan_home="relative"; runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&file.open_calls==0,
                   "shadow must reject a relative MUHAN_HOME before conninfo access");
    environment.muhan_home="/muhan/../escape"; runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&file.open_calls==0,
                   "shadow must reject a non-canonical MUHAN_HOME before conninfo access");
    environment.muhan_home="/muhan"; environment.world_id="World";
    runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED&&file.open_calls==0,
                   "shadow must validate the writer world grammar before conninfo access");
    return failed;
}

static int test_shadow_transfers_one_scrubbed_conninfo_and_shutdowns_once(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; fake_secret_file file;
    fake_shadow shadow; int failed=0;
    memset(&environment,0,sizeof(environment)); environment.mode="shadow";
    environment.muhan_home="/muhan"; environment.world_id="world-a";
    environment.conninfo_file="/fake-secret";
    fake_database_ready(&database); memset(&file,0,sizeof(file));
    file.bytes="host=localhost dbname=m3\n"; file.length=strlen(file.bytes);
    memset(&shadow,0,sizeof(shadow)); runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY,
                   "valid shadow must become ready");
    failed|=expect(file.open_calls==1&&file.close_calls==1&&shadow.start_calls==1&&database.connect_calls==0,
                   "shadow must read the conninfo once and not run the probe connection");
    failed|=expect(!strcmp(shadow.supplied_home,"/muhan")&&!strcmp(shadow.supplied_world_id,"world-a")&&
                   !strcmp(shadow.supplied_conninfo,"host=localhost dbname=m3"),
                   "shadow must transfer only validated environment and normalized conninfo bytes");
    failed|=expect(runtime.conninfo_length==0&&bytes_are_zero(runtime.conninfo,sizeof(runtime.conninfo)),
                   "shadow must scrub conninfo immediately after startup");
    character_save_journal_v2_runtime_shutdown(&runtime);
    character_save_journal_v2_runtime_shutdown(&runtime);
    failed|=expect(shadow.shutdown_calls==1&&
                   character_save_journal_v2_runtime_get_state(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,
                   "shadow shutdown must be idempotent");
    return failed;
}

static int test_active_shadow_reinitialization_is_non_destructive(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; fake_secret_file file;
    fake_shadow shadow; int failed=0;

    memset(&environment,0,sizeof(environment)); environment.mode="shadow";
    environment.muhan_home="/muhan"; environment.world_id="world-a";
    environment.conninfo_file="/fake-secret";
    fake_database_ready(&database); memset(&file,0,sizeof(file));
    file.bytes="dbname=m3"; file.length=strlen(file.bytes);
    memset(&shadow,0,sizeof(shadow));
    runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==
                   CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY,
                   "active reinitialization fixture must become ready");

    character_save_journal_v2_runtime_init(&runtime,0);
    failed|=expect(runtime.current_state==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY&&
                   runtime.shadow_active&&runtime.dependencies.shadow_opaque==&shadow,
                   "init must not overwrite an active shadow runtime");
    failed|=expect(shadow.shutdown_calls==0,
                   "active init guard must not perform an implicit shutdown");

    character_save_journal_v2_runtime_shutdown(&runtime);
    failed|=expect(shadow.shutdown_calls==1&&
                   runtime.current_state==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,
                   "guarded runtime must retain its one explicit shutdown path");
    return failed;
}

static int test_shadow_start_failure_unwinds_and_scrubs(void)
{
    character_save_journal_v2_runtime runtime;
    fake_environment environment; fake_database database; fake_secret_file file;
    fake_shadow shadow; int failed=0;
    memset(&environment,0,sizeof(environment)); environment.mode="shadow";
    environment.muhan_home="/muhan"; environment.world_id="world-a";
    environment.conninfo_file="/fake-secret";
    fake_database_ready(&database); memset(&file,0,sizeof(file));
    file.bytes="dbname=m3"; file.length=strlen(file.bytes);
    memset(&shadow,0,sizeof(shadow)); shadow.start_result=-1;
    runtime_shadow_init(&runtime,&environment,&database,&file,&shadow);
    failed|=expect(character_save_journal_v2_runtime_start(&runtime)==CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED,
                   "shadow startup failure must fail closed");
    failed|=expect(shadow.start_calls==1&&shadow.shutdown_calls==1&&
                   runtime.conninfo_length==0&&bytes_are_zero(runtime.conninfo,sizeof(runtime.conninfo)),
                   "shadow startup failure must invoke the one inverse path and scrub conninfo");
    character_save_journal_v2_runtime_shutdown(&runtime);
    failed|=expect(shadow.shutdown_calls==1,"failed shadow shutdown must not invoke a second close");
    return failed;
}

int main(void)
{
    int failed=0;
    failed|=test_disabled_never_touches_file_or_database();
    failed|=test_probe_ready_and_exact_assertion();
    failed|=test_fail_closed_database_evidence();
    failed|=test_probe_rejects_invalid_mode_and_secret_files();
    failed|=test_probe_rejects_short_read_and_close_error();
    failed|=test_probe_requires_writer_world_id_grammar();
    failed|=test_probe_detects_nanosecond_secret_mutation_and_wipes_on_failure();
    failed|=test_probe_wipes_conninfo_without_losing_ready_state();
    failed|=test_shadow_requires_explicit_environment_before_conninfo_io();
    failed|=test_shadow_transfers_one_scrubbed_conninfo_and_shutdowns_once();
    failed|=test_active_shadow_reinitialization_is_non_destructive();
    failed|=test_shadow_start_failure_unwinds_and_scrubs();
    return failed ? 1 : 0;
}
