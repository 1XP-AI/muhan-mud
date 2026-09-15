#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include "bank_store.h"
#include "character_save_journal_v2_runtime.h"
#include "mstruct.h"

/* The target intentionally links only the legacy bank seam.  Include the
 * runtime implementation here so this fixture can drive its real mode gate
 * without widening the production build or Makefile surface. */
#include "character_save_journal_v2_runtime.c"

extern int load_bank(char *name, object **object);
extern int save_bank(char *name, object *object);

static const char original_tail[] = "old-tail-must-survive";

typedef struct bank_sentinel {
    void *expected_opaque;
    int save_calls;
    int load_calls;
    int opaque_matches;
} bank_sentinel;

typedef struct shadow_runtime_fixture {
    const char *mode;
    const char *home;
    const char *world;
    const char *conninfo_path;
    const char *conninfo;
    int start_calls;
    int shutdown_calls;
} shadow_runtime_fixture;

static int sentinel_save(void *opaque, char *name, object *object)
{
    bank_sentinel *sentinel=(bank_sentinel *)opaque;

    (void)name;
    (void)object;
    sentinel->save_calls++;
    if(opaque==sentinel->expected_opaque)
        sentinel->opaque_matches++;
    return BANK_STORE_OK;
}

static int sentinel_load(void *opaque, char *name, object **output)
{
    static object loaded;
    bank_sentinel *sentinel=(bank_sentinel *)opaque;

    (void)name;
    sentinel->load_calls++;
    if(opaque==sentinel->expected_opaque)
        sentinel->opaque_matches++;
    *output=&loaded;
    return BANK_STORE_OK;
}

static const char *shadow_runtime_getenv(void *opaque, const char *name)
{
    shadow_runtime_fixture *fixture=(shadow_runtime_fixture *)opaque;

    if(!strcmp(name,"MUD_M3_MODE")) return fixture->mode;
    if(!strcmp(name,"MUHAN_HOME")) return fixture->home;
    if(!strcmp(name,"MUD_M3_WORLD_ID")) return fixture->world;
    if(!strcmp(name,"MUD_M3_CONNINFO_FILE")) return fixture->conninfo_path;
    return 0;
}

static int shadow_runtime_start(void *opaque, const char *home,
    const char *world, const char *conninfo)
{
    shadow_runtime_fixture *fixture=(shadow_runtime_fixture *)opaque;

    fixture->start_calls++;
    return strcmp(home,fixture->home)||strcmp(world,fixture->world)||
        strcmp(conninfo,fixture->conninfo) ? -1 : 0;
}

static void shadow_runtime_shutdown(void *opaque)
{
    shadow_runtime_fixture *fixture=(shadow_runtime_fixture *)opaque;

    fixture->shutdown_calls++;
}

static const character_save_journal_v2_runtime_shadow_operations
shadow_runtime_operations={ shadow_runtime_start,shadow_runtime_shutdown };

void free_obj(object *object)
{
    free(object);
}

int write_obj(int fd, object *object, char permanent_only)
{
    (void)permanent_only;
    return write(fd, &object->value, sizeof(object->value)) ==
        sizeof(object->value) ? 0 : -1;
}

int read_obj(int fd, object *object)
{
    return read(fd, &object->value, sizeof(object->value)) ==
        sizeof(object->value) ? 0 : -1;
}

static int expect(int condition, const char *message)
{
    if(condition)
        return 0;
    fprintf(stderr, "bank_legacy_abi_test: %s\n", message);
    return 1;
}

int main(void)
{
    char root[] = "/tmp/muhan-bank-store-XXXXXX";
    char player[512], bank[512], bank_file[512], conninfo_path[512];
    object input, *output = 0;
    struct stat st;
    bank_sentinel sentinel;
    bank_store_binding sentinel_binding;
    bank_store_ops sentinel_operations;
    shadow_runtime_fixture runtime_fixture;
    character_save_journal_v2_runtime_dependencies dependencies;
    character_save_journal_v2_runtime runtime;
    int before_shadow_save_calls, before_shadow_load_calls;
    int before_shadow_opaque_matches;
    int fd, failed = 0;

    if(!mkdtemp(root)) {
        perror("mkdtemp");
        return 1;
    }
    snprintf(player, sizeof(player), "%s/player", root);
    snprintf(bank, sizeof(bank), "%s/bank", player);
    snprintf(bank_file, sizeof(bank_file), "%s/Legacy", bank);
    snprintf(conninfo_path, sizeof(conninfo_path), "%s/m3.conninfo", root);
    if(mkdir(player, 0700) < 0 || mkdir(bank, 0700) < 0 ||
       setenv("MUHAN_HOME", root, 1) < 0) {
        perror("bank fixture setup");
        return 1;
    }
    fd = open(bank_file, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if(fd < 0 || write(fd, original_tail, sizeof(original_tail) - 1) !=
       sizeof(original_tail) - 1 || close(fd) < 0) {
        perror("bank fixture write");
        return 1;
    }

    fd=open(conninfo_path,O_WRONLY|O_CREAT|O_TRUNC,0600);
    if(fd<0 || write(fd,"host=unit-test",14)!=14 || close(fd)<0) {
        perror("runtime conninfo fixture");
        return 1;
    }

    memset(&sentinel,0,sizeof(sentinel));
    memset(&sentinel_binding,0,sizeof(sentinel_binding));
    sentinel.expected_opaque=&sentinel;
    sentinel_operations.save=sentinel_save;
    sentinel_operations.load=sentinel_load;
    sentinel_operations.opaque=&sentinel;
    bank_store_reset();
    failed+=expect(bank_store_bind(&sentinel_operations,&sentinel_binding)==0 &&
                   sentinel_binding.active,
                   "fixture must hold a bank-store sentinel binding");

    memset(&runtime_fixture,0,sizeof(runtime_fixture));
    runtime_fixture.mode="off";
    runtime_fixture.home=root;
    runtime_fixture.world="m3-shadow-bank";
    runtime_fixture.conninfo_path=conninfo_path;
    runtime_fixture.conninfo="host=unit-test";
    memset(&dependencies,0,sizeof(dependencies));
    dependencies.environment_get=shadow_runtime_getenv;
    dependencies.environment_opaque=&runtime_fixture;
    dependencies.shadow_operations=&shadow_runtime_operations;
    dependencies.shadow_opaque=&runtime_fixture;
    character_save_journal_v2_runtime_init(&runtime,&dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
                   CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED &&
                   runtime_fixture.start_calls==0 &&
                   runtime_fixture.shutdown_calls==0 && sentinel_binding.active &&
                   !sentinel.save_calls && !sentinel.load_calls &&
                   !sentinel.opaque_matches,
                   "off mode must leave bank binding and callbacks untouched");
    failed+=expect(bank_store_save("sentinel",&input)==BANK_STORE_OK &&
                   bank_store_load("sentinel",&output)==BANK_STORE_OK && output &&
                   sentinel.save_calls==1 && sentinel.load_calls==1 &&
                   sentinel.opaque_matches==2,
                   "sentinel must retain its exact opaque bank binding after off mode");
    output=0;

    before_shadow_save_calls=sentinel.save_calls;
    before_shadow_load_calls=sentinel.load_calls;
    before_shadow_opaque_matches=sentinel.opaque_matches;
    runtime_fixture.mode="shadow";
    character_save_journal_v2_runtime_init(&runtime,&dependencies);
    failed+=expect(character_save_journal_v2_runtime_start(&runtime)==
                   CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY &&
                   runtime_fixture.start_calls==1 && sentinel_binding.active &&
                   sentinel.save_calls==before_shadow_save_calls &&
                   sentinel.load_calls==before_shadow_load_calls &&
                   sentinel.opaque_matches==before_shadow_opaque_matches,
                   "shadow startup must not bind or call the bank store");
    failed+=expect(bank_store_save("sentinel",&input)==BANK_STORE_OK &&
                   bank_store_load("sentinel",&output)==BANK_STORE_OK && output &&
                   sentinel_binding.active &&
                   sentinel.save_calls==before_shadow_save_calls+1 &&
                   sentinel.load_calls==before_shadow_load_calls+1 &&
                   sentinel.opaque_matches==before_shadow_opaque_matches+2,
                   "READY shadow runtime must retain the held bank binding and opaque store");
    output=0;
    before_shadow_save_calls=sentinel.save_calls;
    before_shadow_load_calls=sentinel.load_calls;
    before_shadow_opaque_matches=sentinel.opaque_matches;
    character_save_journal_v2_runtime_shutdown(&runtime);
    failed+=expect(runtime_fixture.shutdown_calls==1 && sentinel_binding.active &&
                   sentinel.save_calls==before_shadow_save_calls &&
                   sentinel.load_calls==before_shadow_load_calls &&
                   sentinel.opaque_matches==before_shadow_opaque_matches,
                   "shadow shutdown must not unbind or call the bank store");
    failed+=expect(bank_store_unbind(&sentinel_binding)==
                   BANK_STORE_UNBIND_RESTORED && !sentinel_binding.active,
                   "the unchanged sentinel binding must restore the file bank store");

    memset(&input, 0, sizeof(input));
    input.value = 1234;
    failed += expect(setenv("MUD_M3_MODE", "off", 1) == 0,
                     "off-mode fixture must set the M3 environment");
    failed += expect(save_bank("Legacy", &input) == 0,
                     "off mode must retain file-backed bank save success");
    failed += expect(stat(bank_file, &st) == 0 && st.st_size ==
                     (off_t)(sizeof(original_tail) - 1),
                     "legacy save must retain its existing non-truncating I/O behavior");
    failed += expect(load_bank("Legacy", &output) == 0 && output &&
                     output->value == 1234,
                     "off mode must retain the default bank file store");
    free(output);
    output = 0;
    input.value = 5678;
    failed += expect(setenv("MUD_M3_MODE", "shadow", 1) == 0,
                     "shadow-mode fixture must set the M3 environment");
    failed += expect(save_bank("Legacy", &input) == 0 &&
                     load_bank("Legacy", &output) == 0 && output &&
                     output->value == 5678,
                     "shadow mode must not route bank save/load through PlayerStore");
    free(output);
    output = 0;
    output = (object *)1;
    failed += expect(load_bank("Missing", &output) == -1 && !output,
                     "legacy file failures must retain the -1 result and clear output");

    unlink(bank_file);
    unlink(conninfo_path);
    rmdir(bank);
    rmdir(player);
    rmdir(root);
    return failed ? 1 : 0;
}
