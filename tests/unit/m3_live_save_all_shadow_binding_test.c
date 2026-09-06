/*
 * TDD boundary: save_all_ply() is the production bulk-save caller and must
 * reach the currently bound PlayerStore only after an exact M3 shadow start.
 * RED: this test did not exist while the M3 shadow binding was introduced;
 * it links the real files2.c -> command8.c -> save_ply() chain.
 */
#ifndef USE_M3_RUNTIME
#error "this boundary must compile in the M3 runtime graph"
#endif

#include "character_save_journal_v2_runtime.h"
#include "mstruct.h"
#include "mextern.h"
#include "player_store.h"

#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct save_environment {
    const char *mode;
    const char *muhan_home;
    const char *world_id;
    const char *conninfo_file;
} save_environment;

typedef struct shadow_binding {
    int start_calls;
    int shutdown_calls;
    int journal_save_calls;
    player_store_binding binding;
} shadow_binding;

typedef struct secret_file {
    int reads;
    const char *bytes;
    size_t length;
} secret_file;

static int legacy_save_calls;
static int merror_calls;

void merror(str, errtype)
char *str;
char errtype;
{
    (void)str;
    (void)errtype;
    merror_calls++;
}

void add_obj_crt(obj_ptr, ply_ptr)
object *obj_ptr;
creature *ply_ptr;
{
    (void)obj_ptr;
    (void)ply_ptr;
}

void del_obj_crt(obj_ptr, ply_ptr)
object *obj_ptr;
creature *ply_ptr;
{
    (void)obj_ptr;
    (void)ply_ptr;
}

void print(fd, text)
int fd;
char *text;
{
    (void)fd;
    (void)text;
}

int file_player_store_save(name, player)
char *name;
creature *player;
{
    legacy_save_calls++;
    return name && player && !strcmp(name, "M3batch") &&
        !strcmp(player->name, "M3batch") ? PLAYER_STORE_OK :
        PLAYER_STORE_IO_ERROR;
}

int file_player_store_load(name, player)
char *name;
creature **player;
{
    (void)name;
    if(player) *player=0;
    return PLAYER_STORE_NOT_FOUND;
}

static int expect(condition, message)
int condition;
const char *message;
{
    if(condition) return 0;
    fprintf(stderr, "m3_live_save_all_shadow_binding_test: %s\n", message);
    return 1;
}

static const char *save_getenv(opaque, name)
void *opaque;
const char *name;
{
    save_environment *environment=(save_environment *)opaque;

    if(!strcmp(name, "MUD_M3_MODE")) return environment->mode;
    if(!strcmp(name, "MUHAN_HOME")) return environment->muhan_home;
    if(!strcmp(name, "MUD_M3_WORLD_ID")) return environment->world_id;
    if(!strcmp(name, "MUD_M3_CONNINFO_FILE")) return environment->conninfo_file;
    return 0;
}

static int secret_open(opaque, path)
void *opaque;
const char *path;
{
    (void)opaque;
    return path && !strcmp(path, "/m3-secret") ? 7 : -1;
}

static int secret_stat(opaque, descriptor, status)
void *opaque;
int descriptor;
struct stat *status;
{
    secret_file *file=(secret_file *)opaque;

    if(descriptor!=7 || !status) return -1;
    memset(status,0,sizeof(*status));
    status->st_mode=S_IFREG|0600;
    status->st_nlink=1;
    status->st_uid=geteuid();
    status->st_size=(off_t)file->length;
    status->st_mtime=1;
    status->st_ctime=1;
    return 0;
}

static long secret_read(opaque, descriptor, buffer, length)
void *opaque;
int descriptor;
void *buffer;
size_t length;
{
    secret_file *file=(secret_file *)opaque;

    if(descriptor!=7 || !buffer) return -1;
    if(file->reads++) return 0;
    if(length<file->length) return -1;
    memcpy(buffer,file->bytes,file->length);
    return (long)file->length;
}

static int secret_close(opaque, descriptor)
void *opaque;
int descriptor;
{
    (void)opaque;
    return descriptor==7 ? 0 : -1;
}

static const character_save_journal_v2_runtime_file_operations secret_operations={
    secret_open,secret_stat,secret_read,secret_close
};

static int journal_save(opaque, name, player)
void *opaque;
char *name;
creature *player;
{
    shadow_binding *shadow=(shadow_binding *)opaque;

    shadow->journal_save_calls++;
    return name && player && !strcmp(name, "M3batch") &&
        !strcmp(player->name, "M3batch") ? PLAYER_STORE_OK :
        PLAYER_STORE_IO_ERROR;
}

static int journal_load(opaque, name, player)
void *opaque;
char *name;
creature **player;
{
    (void)opaque;
    (void)name;
    if(player) *player=0;
    return PLAYER_STORE_NOT_FOUND;
}

static int shadow_start(opaque, muhan_home, world_id, conninfo)
void *opaque;
const char *muhan_home;
const char *world_id;
const char *conninfo;
{
    shadow_binding *shadow=(shadow_binding *)opaque;
    player_store_ops journal_store;

    shadow->start_calls++;
    if(strcmp(muhan_home,"/muhan") || strcmp(world_id,"m3-world") ||
       strcmp(conninfo,"dbname=m3")) return -1;
    journal_store.save=journal_save;
    journal_store.load=journal_load;
    journal_store.opaque=shadow;
    return player_store_bind(&journal_store,&shadow->binding);
}

static void shadow_shutdown(opaque)
void *opaque;
{
    shadow_binding *shadow=(shadow_binding *)opaque;

    shadow->shutdown_calls++;
    if(shadow->binding.active)
        (void)player_store_unbind(&shadow->binding);
}

static const character_save_journal_v2_runtime_shadow_operations shadow_operations={
    shadow_start,shadow_shutdown
};

static void runtime_init(runtime, environment, file, shadow)
character_save_journal_v2_runtime *runtime;
save_environment *environment;
secret_file *file;
shadow_binding *shadow;
{
    character_save_journal_v2_runtime_dependencies dependencies;

    memset(&dependencies,0,sizeof(dependencies));
    dependencies.environment_get=save_getenv;
    dependencies.environment_opaque=environment;
    dependencies.file_operations=&secret_operations;
    dependencies.file_opaque=file;
    dependencies.shadow_operations=&shadow_operations;
    dependencies.shadow_opaque=shadow;
    character_save_journal_v2_runtime_init(runtime,&dependencies);
}

static void install_online_player(player, io)
creature *player;
iobuf *io;
{
    memset(Ply,0,sizeof(Ply));
    memset(player,0,sizeof(*player));
    memset(io,0,sizeof(*io));
    strcpy(player->name,"M3batch");
    player->fd=0;
    Ply[0].ply=player;
    Ply[0].io=io;
    Tablesize=1;
}

int main(void)
{
    character_save_journal_v2_runtime runtime;
    save_environment environment;
    secret_file file;
    shadow_binding shadow;
    creature player;
    iobuf io;
    int failed=0;

    memset(&environment,0,sizeof(environment));
    memset(&file,0,sizeof(file));
    memset(&shadow,0,sizeof(shadow));
    file.bytes="dbname=m3";
    file.length=strlen(file.bytes);
    environment.muhan_home="/muhan";
    environment.world_id="m3-world";
    environment.conninfo_file="/m3-secret";
    install_online_player(&player,&io);
    player_store_reset();

    runtime_init(&runtime,&environment,&file,&shadow);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,
        "absent MUD_M3_MODE must leave the live PlayerStore at FileStore");
    save_all_ply();
    failed+=expect(legacy_save_calls==1 && !shadow.start_calls &&
        !shadow.journal_save_calls,
        "save_all_ply must use legacy authority while the feature is absent");

    environment.mode="off";
    runtime_init(&runtime,&environment,&file,&shadow);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED,
        "off MUD_M3_MODE must leave the live PlayerStore at FileStore");
    save_all_ply();
    failed+=expect(legacy_save_calls==2 && !shadow.start_calls &&
        !shadow.journal_save_calls,
        "save_all_ply must retain legacy authority while the feature is off");

    environment.mode="shadow";
    runtime_init(&runtime,&environment,&file,&shadow);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY && shadow.start_calls==1 &&
        shadow.binding.active,
        "exact shadow mode must install the journal PlayerStore binding");
    save_all_ply();
    failed+=expect(legacy_save_calls==2 && shadow.journal_save_calls==1 &&
        !merror_calls,
        "production save_all_ply path must reach only the bound journal store in shadow");

    character_save_journal_v2_runtime_shutdown(&runtime);
    save_all_ply();
    failed+=expect(shadow.shutdown_calls==1 && !shadow.binding.active &&
        legacy_save_calls==3 && shadow.journal_save_calls==1,
        "shadow shutdown must restore legacy authority for later bulk saves");
    player_store_reset();
    memset(Ply,0,sizeof(Ply));
    Tablesize=0;
    return failed ? 1 : 0;
}
