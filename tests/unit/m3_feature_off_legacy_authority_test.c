/*
 * Feature-off M3 must leave the legacy PlayerStore authoritative.  This uses
 * the real FileStore replacement/load path, while its shadow starter is a
 * deliberate failure: absent and off modes must never invoke it.
 */
#include "character_save_journal_v2_runtime.h"
#include "mstruct.h"
#include "player_path.h"
#include "player_store.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct feature_off_environment {
    const char *mode;
    const char *muhan_home;
    const char *world_id;
    const char *conninfo_file;
} feature_off_environment;

typedef struct failing_shadow {
    int start_calls;
    player_store_binding binding;
} failing_shadow;

static int write_calls;
static int read_calls;

void merror(char *message, char kind)
{
    (void)message;
    (void)kind;
    abort();
}

void zero(void *memory, int size)
{
    memset(memory, 0, (size_t)size);
}

void free_crt(creature *player)
{
    free(player);
}

int utf8_validate(const unsigned char *text, unsigned long length)
{
    (void)text;
    (void)length;
    return 1;
}

unsigned long utf8_codepoint_len(const unsigned char *text)
{
    return text ? (unsigned long)strlen((const char *)text) : 0;
}

int write_crt(int descriptor, creature *player, char perm_only)
{
    unsigned char record[sizeof(creature) + sizeof(int)];

    (void)player;
    (void)perm_only;
    memset(record, 0, sizeof(record));
    record[0] = 'L';
    record[1] = 'E';
    record[2] = 'G';
    record[3] = '1';
    write_calls++;
    return write(descriptor, record, sizeof(record)) ==
        (ssize_t)sizeof(record) ? 0 : -1;
}

int read_crt_player(int descriptor, creature *player)
{
    unsigned char record[sizeof(creature) + sizeof(int)];

    read_calls++;
    if(read(descriptor, record, sizeof(record)) != (ssize_t)sizeof(record) ||
       memcmp(record, "LEG1", 4))
        return -1;
    strcpy(player->name, "Legacy");
    return 0;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "m3_feature_off_legacy_authority_test: %s\n", message);
    return 1;
}

static const char *feature_off_getenv(void *opaque, const char *name)
{
    feature_off_environment *environment=(feature_off_environment *)opaque;

    if(!strcmp(name, "MUD_M3_MODE")) return environment->mode;
    if(!strcmp(name, "MUHAN_HOME")) return environment->muhan_home;
    if(!strcmp(name, "MUD_M3_WORLD_ID")) return environment->world_id;
    if(!strcmp(name, "MUD_M3_CONNINFO_FILE")) return environment->conninfo_file;
    return 0;
}

/* This is the one side effect the real shadow process owner would make to
 * legacy save authority.  A deliberately failing starter installs it first,
 * so absent/off modes prove that runtime dispatch never reaches an owner
 * which could bind PlayerStore. */
static int shadow_owner_save(void *opaque, char *name, creature *player)
{
    (void)opaque;
    (void)name;
    (void)player;
    return PLAYER_STORE_IO_ERROR;
}

static int shadow_owner_load(void *opaque, char *name, creature **player)
{
    (void)opaque;
    (void)name;
    if(player) *player=0;
    return PLAYER_STORE_IO_ERROR;
}

static int failing_shadow_start(void *opaque, const char *muhan_home,
    const char *world_id, const char *conninfo)
{
    failing_shadow *shadow=(failing_shadow *)opaque;
    player_store_ops owner_store;

    (void)muhan_home;
    (void)world_id;
    (void)conninfo;
    shadow->start_calls++;
    owner_store.save=shadow_owner_save;
    owner_store.load=shadow_owner_load;
    owner_store.opaque=shadow;
    if(player_store_bind(&owner_store,&shadow->binding))
        return -1;
    return -1;
}

static void failing_shadow_shutdown(void *opaque)
{
    failing_shadow *shadow=(failing_shadow *)opaque;

    if(shadow && shadow->binding.active)
        (void)player_store_unbind(&shadow->binding);
}

static const character_save_journal_v2_runtime_shadow_operations
failing_shadow_operations={ failing_shadow_start, failing_shadow_shutdown };

int main(void)
{
    char root[]="/tmp/muhan-feature-off.XXXXXX";
    char player_root[512];
    char player_file[512];
    char saved_muhan_home[512];
    char *player_file_slash;
    const char *prior_muhan_home;
    character_save_journal_v2_runtime runtime;
    character_save_journal_v2_runtime_dependencies dependencies;
    feature_off_environment environment;
    failing_shadow shadow;
    creature input;
    creature *loaded=0;
    int failed=0;

    player_root[0]=0;
    player_file[0]=0;
    prior_muhan_home=getenv("MUHAN_HOME");
    if(prior_muhan_home && snprintf(saved_muhan_home, sizeof(saved_muhan_home),
        "%s", prior_muhan_home) >= (int)sizeof(saved_muhan_home)) {
        fprintf(stderr, "feature-off fixture: MUHAN_HOME is too long\n");
        return 1;
    }
    if(!mkdtemp(root)) {
        perror("feature-off fixture");
        return 1;
    }
    if(setenv("MUHAN_HOME", root, 1) < 0) {
        perror("feature-off fixture environment");
        rmdir(root);
        return 1;
    }
    if(snprintf(player_root, sizeof(player_root), "%s/player", root) >=
       (int)sizeof(player_root) || mkdir(player_root, 0700) < 0) {
        perror("feature-off player root");
        failed=1;
        goto cleanup;
    }
    memset(&dependencies, 0, sizeof(dependencies));
    memset(&environment, 0, sizeof(environment));
    memset(&shadow, 0, sizeof(shadow));
    dependencies.environment_get=feature_off_getenv;
    dependencies.environment_opaque=&environment;
    dependencies.shadow_operations=&failing_shadow_operations;
    dependencies.shadow_opaque=&shadow;

    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "absent mode must not activate the failing shadow authority");
    environment.mode="off";
    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "off mode must not let the shadow owner bind PlayerStore");

    /* A shadow-looking mode with an incomplete or malformed exact tuple is
     * not feature-off, but it must still fail closed before it can replace the
     * legacy PlayerStore binding. */
    environment.mode="shadow";
    environment.world_id="world-a";
    environment.conninfo_file="/tmp/m3-unused.conninfo";
    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "shadow without MUHAN_HOME must preserve legacy PlayerStore authority");
    environment.muhan_home="relative";
    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "invalid MUHAN_HOME must preserve legacy PlayerStore authority");
    environment.muhan_home=root;
    environment.world_id="World";
    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "invalid MUD_M3_WORLD_ID must preserve legacy PlayerStore authority");
    environment.world_id="world-a";
    environment.conninfo_file="relative";
    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "invalid MUD_M3_CONNINFO_FILE must preserve legacy PlayerStore authority");
    environment.mode="Shadow";
    character_save_journal_v2_runtime_init(&runtime, &dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
        CHARACTER_SAVE_JOURNAL_V2_RUNTIME_FAILED && shadow.start_calls==0 &&
        !shadow.binding.active,
        "non-exact MUD_M3_MODE must preserve legacy PlayerStore authority");

    memset(&input, 0, sizeof(input));
    strcpy(input.name, "Legacy");
    player_store_reset();
    failed+=expect(save_ply("Legacy", &input)==PLAYER_STORE_OK,
        "a legacy save must succeed without shadow or receipt authority");
    failed+=expect(load_ply("Legacy", &loaded)==PLAYER_STORE_OK && loaded &&
        !strcmp(loaded->name, "Legacy") && write_calls==1 && read_calls==1,
        "the successful legacy bytes must remain loadable from the player file");
    if(loaded)
        free_crt(loaded);

cleanup:
    /* This test runs in its own process, but leave neither its player bytes
     * nor a changed fixture environment behind for repeated local runs. */
    if(player_root[0] && player_path_from_name("Legacy", player_file,
       sizeof(player_file)) == 0) {
        unlink(player_file);
        player_file_slash=strrchr(player_file, '/');
        if(player_file_slash) {
            *player_file_slash=0;
            rmdir(player_file);
        }
    }
    if(player_root[0])
        rmdir(player_root);
    rmdir(root);
    if(prior_muhan_home)
        setenv("MUHAN_HOME", saved_muhan_home, 1);
    else
        unsetenv("MUHAN_HOME");

    return failed ? 1 : 0;
}
