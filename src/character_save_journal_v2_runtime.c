#include "character_save_journal_v2_runtime.h"

#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <unistd.h>

static void runtime_wipe(void *memory, size_t length)
{
    volatile unsigned char *cursor=(volatile unsigned char *)memory;
    while(length) {
        *cursor++=0;
        length--;
    }
}

static const char *runtime_environment_get(void *opaque, const char *name)
{
    (void)opaque;
    return getenv(name);
}

static int runtime_file_open(void *opaque, const char *path)
{
    (void)opaque;
    return open(path,O_RDONLY|O_CLOEXEC|O_NOFOLLOW);
}

static int runtime_file_stat(void *opaque, int descriptor, struct stat *status)
{
    (void)opaque;
    return fstat(descriptor,status);
}

static long runtime_file_read(void *opaque, int descriptor, void *buffer, size_t length)
{
    (void)opaque;
    return (long)read(descriptor,buffer,length);
}

static int runtime_file_close(void *opaque, int descriptor)
{
    (void)opaque;
    return close(descriptor);
}

static const character_save_journal_v2_runtime_file_operations runtime_default_file_operations={
    runtime_file_open,runtime_file_stat,runtime_file_read,runtime_file_close
};

static const char *runtime_get_environment(const character_save_journal_v2_runtime *runtime,
                                           const char *name)
{
    if(runtime->has_dependencies&&runtime->dependencies.environment_get)
        return runtime->dependencies.environment_get(runtime->dependencies.environment_opaque,name);
    return runtime_environment_get(0,name);
}

static size_t runtime_bounded_length(const char *value, size_t maximum)
{
    size_t length=0;
    if(!value) return maximum+1;
    while(length<=maximum) {
        if(!value[length]) return length;
        length++;
    }
    return maximum+1;
}

static int runtime_text_safe(const char *value, size_t length)
{
    size_t index;
    if(!length) return 0;
    for(index=0;index<length;index++) {
        unsigned char byte=(unsigned char)value[index];
        if(byte<0x20||byte==0x7f) return 0;
    }
    return 1;
}

/* Keep this grammar identical to private.m3_shadow_valid_world and the
 * durable writer tuple: one lowercase letter, then at most 63 routing bytes. */
static int runtime_world_id_safe(const char *value, size_t length)
{
    size_t index;
    if(!value||!length||length>CHARACTER_SAVE_JOURNAL_V2_RUNTIME_WORLD_ID_MAX||
       value[0]<'a'||value[0]>'z') return 0;
    for(index=1;index<length;index++) {
        if(!((value[index]>='a'&&value[index]<='z')||
             (value[index]>='0'&&value[index]<='9')||
             value[index]=='_'||value[index]=='-')) return 0;
    }
    return 1;
}

static int runtime_copy_environment(char *destination, size_t capacity, const char *value,
                                    int require_absolute)
{
    size_t length=runtime_bounded_length(value,capacity-1);
    if(length>=capacity||!runtime_text_safe(value,length)) return 0;
    if(require_absolute&&value[0]!='/') return 0;
    memcpy(destination,value,length);
    destination[length]=0;
    return 1;
}

static int runtime_stat_safe(const struct stat *status)
{
    mode_t permissions;
    if(!S_ISREG(status->st_mode)||status->st_nlink!=1||status->st_uid!=geteuid()) return 0;
    permissions=(mode_t)(status->st_mode&0777);
    if(!(permissions&S_IRUSR)||permissions&~(S_IRUSR|S_IWUSR)) return 0;
    if(status->st_size<=0||status->st_size>(off_t)(CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX+1)) return 0;
    return 1;
}

static int runtime_same_file(const struct stat *first, const struct stat *last)
{
    return first->st_dev==last->st_dev&&first->st_ino==last->st_ino&&
           first->st_size==last->st_size&&first->st_mode==last->st_mode&&
           first->st_uid==last->st_uid&&first->st_gid==last->st_gid&&
           first->st_nlink==last->st_nlink&&first->st_mtime==last->st_mtime&&
           first->st_ctime==last->st_ctime&&
#if defined(__APPLE__)
           first->st_mtimespec.tv_nsec==last->st_mtimespec.tv_nsec&&
           first->st_ctimespec.tv_nsec==last->st_ctimespec.tv_nsec;
#elif defined(__linux__) || defined(__FreeBSD__) || defined(__NetBSD__) || defined(__OpenBSD__)
           first->st_mtim.tv_nsec==last->st_mtim.tv_nsec&&
           first->st_ctim.tv_nsec==last->st_ctim.tv_nsec;
#else
           1;
#endif
}

static int runtime_read_conninfo(character_save_journal_v2_runtime *runtime, const char *path)
{
    const character_save_journal_v2_runtime_file_operations *operations;
    void *opaque;
    struct stat before, after;
    int descriptor=-1, close_result=-1, good=0;
    size_t expected, received=0, length;
    unsigned char extra;
    long count;
    if(runtime->has_dependencies&&runtime->dependencies.file_operations) {
        operations=runtime->dependencies.file_operations;
        opaque=runtime->dependencies.file_opaque;
    } else {
        operations=&runtime_default_file_operations;
        opaque=0;
    }
    if(!operations->open_readonly_nofollow||!operations->descriptor_stat||!operations->read_bytes||!operations->close_descriptor) return 0;
    descriptor=operations->open_readonly_nofollow(opaque,path);
    if(descriptor<0) goto done;
    if(operations->descriptor_stat(opaque,descriptor,&before)!=0||!runtime_stat_safe(&before)) goto done;
    expected=(size_t)before.st_size;
    while(received<expected) {
        count=operations->read_bytes(opaque,descriptor,runtime->conninfo+received,expected-received);
        if(count<=0||(size_t)count>expected-received) goto done;
        received+=(size_t)count;
    }
    count=operations->read_bytes(opaque,descriptor,&extra,1);
    if(count!=0) goto done;
    if(operations->descriptor_stat(opaque,descriptor,&after)!=0||!runtime_same_file(&before,&after)) goto done;
    length=received;
    if(length&&runtime->conninfo[length-1]=='\n') length--;
    if(length>CHARACTER_SAVE_JOURNAL_V2_RUNTIME_CONNINFO_MAX||!runtime_text_safe(runtime->conninfo,length)) goto done;
    runtime->conninfo[length]=0;
    runtime->conninfo_length=length;
    good=1;
done:
    if(descriptor>=0) close_result=operations->close_descriptor(opaque,descriptor);
    if(close_result!=0) good=0;
    if(!good) {
        runtime_wipe(runtime->conninfo,sizeof(runtime->conninfo));
        runtime->conninfo_length=0;
    }
    return good;
}

static int runtime_database_operations_complete(const character_save_journal_v2_runtime_database_operations *operations)
{
    return operations&&operations->connect&&operations->connection_ok&&operations->exec&&
           operations->result_status&&operations->result_rows&&operations->result_columns&&
           operations->result_value&&operations->result_value_length&&operations->result_sqlstate&&
           operations->result_clear&&operations->connection_finish;
}

static int runtime_assert_writer_session(character_save_journal_v2_runtime *runtime)
{
    const character_save_journal_v2_runtime_database_operations *operations;
    void *opaque, *connection=0, *result=0;
    const char *value, *sqlstate;
    int good=0;
    if(!runtime->has_dependencies) return 0;
    operations=runtime->dependencies.database_operations;
    opaque=runtime->dependencies.database_opaque;
    if(!runtime_database_operations_complete(operations)) return 0;
    connection=operations->connect(opaque,runtime->conninfo);
    if(!connection) goto done;
    if(!operations->connection_ok(connection)) goto done;
    result=operations->exec(connection,"select private.m3_assert_writer_session()");
    if(!result) goto done;
    sqlstate=operations->result_sqlstate(result);
    if(operations->result_status(result)!=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_TUPLES_OK||
       (sqlstate&&sqlstate[0])||operations->result_rows(result)!=1||
       operations->result_columns(result)!=1||operations->result_value_length(result,0,0)!=1) goto done;
    value=operations->result_value(result,0,0);
    if(value&&value[0]=='t') good=1;
done:
    if(result) operations->result_clear(result);
    if(connection) operations->connection_finish(connection);
    return good;
}

void character_save_journal_v2_runtime_init(
    character_save_journal_v2_runtime *runtime,
    const character_save_journal_v2_runtime_dependencies *dependencies)
{
    if(!runtime) return;
    runtime_wipe(runtime,sizeof(*runtime));
    if(dependencies) {
        runtime->dependencies=*dependencies;
        runtime->has_dependencies=1;
    }
    runtime->current_state=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED;
}

character_save_journal_v2_runtime_state
character_save_journal_v2_runtime_start(character_save_journal_v2_runtime *runtime)
{
    const char *mode, *world_id, *conninfo_path;
    char path[CHARACTER_SAVE_JOURNAL_V2_RUNTIME_PATH_MAX];
    size_t mode_length, world_id_length;
    if(!runtime) return CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED;
    character_save_journal_v2_runtime_shutdown(runtime);
    mode=runtime_get_environment(runtime,"MUD_M3_MODE");
    if(!mode) return runtime->current_state;
    mode_length=runtime_bounded_length(mode,5);
    if(mode_length==3&&!memcmp(mode,"off",3)) return runtime->current_state;
    if(mode_length!=5||memcmp(mode,"probe",5)) {
        runtime->current_state=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED;
        return runtime->current_state;
    }
    world_id=runtime_get_environment(runtime,"MUD_M3_WORLD_ID");
    conninfo_path=runtime_get_environment(runtime,"MUD_M3_CONNINFO_FILE");
    world_id_length=runtime_bounded_length(world_id,CHARACTER_SAVE_JOURNAL_V2_RUNTIME_WORLD_ID_MAX);
    if(!runtime_world_id_safe(world_id,world_id_length)||
       !runtime_copy_environment(path,sizeof(path),conninfo_path,1)||
       !runtime_read_conninfo(runtime,path)||!runtime_assert_writer_session(runtime)) {
        runtime_wipe(runtime->conninfo,sizeof(runtime->conninfo));
        runtime->conninfo_length=0;
        runtime->current_state=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED;
        return runtime->current_state;
    }
    runtime_wipe(runtime->conninfo,sizeof(runtime->conninfo));
    runtime->conninfo_length=0;
    runtime->current_state=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY;
    return runtime->current_state;
}

void character_save_journal_v2_runtime_shutdown(character_save_journal_v2_runtime *runtime)
{
    if(!runtime) return;
    runtime_wipe(runtime->conninfo,sizeof(runtime->conninfo));
    runtime->conninfo_length=0;
    runtime->current_state=CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED;
}

character_save_journal_v2_runtime_state
character_save_journal_v2_runtime_get_state(const character_save_journal_v2_runtime *runtime)
{
    if(!runtime) return CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED;
    return runtime->current_state;
}
