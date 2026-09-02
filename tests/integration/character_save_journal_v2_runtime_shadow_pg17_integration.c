/*
 * Native M3 shadow-runtime E2E harness for a disposable PostgreSQL 17
 * server.  Unlike the process-owner fixture, this enters through
 * character_save_journal_v2_runtime_start(MUD_M3_MODE=shadow), so the
 * runtime's conninfo handoff, native libpq transport, process owner, and
 * PlayerStore binding are all production code.
 *
 * The shell harness runs "fault" with a real database receipt trigger that
 * rejects the ACK after local publication, then SIGKILLs this process.  It
 * runs "recover" after dropping that trigger: startup must replay the exact
 * durable journal into PostgreSQL before it installs the live PlayerStore.
 */
#include "character_save_journal_v2.h"
#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_process_owner.h"
#include "character_save_journal_v2_publish.h"
#include "character_save_journal_v2_rpc_transport.h"
#include "character_save_journal_v2_runtime.h"
#include "character_save_journal_v2_runtime_native.h"
#include "character_save_journal_v2_writer.h"
#include "mstruct.h"
#include "player_record_serializer.h"
#include "player_store.h"

#include <dirent.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#if defined(__linux__)
static const char player_name[] = "M3shadow";
static int fallback_save_calls;

/* Runtime-native's ordinary file load callback and PlayerStore's fallback
 * must resolve at link time, but neither belongs to this save-only fixture.
 * These sentinels also make a post-shutdown stale PlayerStore binding fail.
 */
int file_player_store_save(char *name, creature *player)
{
    (void)name;
    (void)player;
    fallback_save_calls++;
    return PLAYER_STORE_CORRUPT;
}

int file_player_store_load(char *name, creature **player)
{
    (void)name;
    if(player) *player = 0;
    return PLAYER_STORE_NOT_FOUND;
}

static int fail(const char *message)
{
    fprintf(stderr, "m3 runtime shadow PG17 integration: %s\n", message);
    return 1;
}

static int suffix(const char *text, const char *ending)
{
    size_t text_length = strlen(text);
    size_t ending_length = strlen(ending);

    return text_length >= ending_length &&
        !memcmp(text + text_length - ending_length, ending, ending_length);
}

static int journal_markers(const char *root, const char *marker)
{
    char path[1024];
    char ending[32];
    DIR *directory;
    struct dirent *entry;
    int count = 0;

    if(!root || snprintf(path, sizeof(path), "%s/character-save-journal", root) >=
       (int)sizeof(path) ||
       snprintf(ending, sizeof(ending), ".%s", marker) >= (int)sizeof(ending))
        return -1;
    directory = opendir(path);
    if(!directory) return -1;
    while((entry = readdir(directory)) != 0)
        if(suffix(entry->d_name, ending)) count++;
    if(closedir(directory) != 0) return -1;
    return count;
}

static int regular_0600(const char *path)
{
    struct stat status;

    return lstat(path, &status) == 0 && S_ISREG(status.st_mode) &&
        (status.st_mode & 07777) == 0600 && status.st_nlink == 1;
}

static int live_matches_serializer(const char *root, const creature *player)
{
    char live[1024];
    unsigned char expected[65536];
    unsigned char actual[65536];
    player_record_serializer_limits limits;
    unsigned long expected_length=0;
    int descriptor;
    ssize_t length;

    if(!root||!player||snprintf(live,sizeof(live),"%s/player/d2/%s",
       root,player_name)>=(int)sizeof(live)) return 0;
    limits.max_depth=64;
    limits.max_objects=8192;
    if(player_record_serialize_bounded((creature *)player,0,(char *)expected,
       sizeof(expected),&expected_length,&limits)!=PLAYER_RECORD_SERIALIZER_OK||
       expected_length>sizeof(actual)) return 0;
    descriptor=open(live,O_RDONLY|O_CLOEXEC|O_NOFOLLOW);
    if(descriptor<0) return 0;
    length=read(descriptor,actual,sizeof(actual));
    if(close(descriptor)!=0) return 0;
    return length==(ssize_t)expected_length&&
        !memcmp(actual,expected,expected_length);
}

static int runtime_conninfo_wiped(const character_save_journal_v2_runtime *runtime)
{
    size_t index;

    if(!runtime || runtime->conninfo_length != 0) return 0;
    for(index = 0; index < sizeof(runtime->conninfo); index++)
        if(runtime->conninfo[index]) return 0;
    return 1;
}

static int shadow_ready(const character_save_journal_v2_runtime *runtime,
    const character_save_journal_v2_runtime_native *native)
{
#if defined(__linux__)
    return runtime && native && runtime->shadow_active && native->shadow_active &&
        runtime_conninfo_wiped(runtime) && native->serializer_buffer &&
        native->serializer_buffer_capacity &&
        native->transport_native.transport.connection &&
        character_save_journal_v2_rpc_transport_get_state(
            &native->transport_native.transport) ==
              CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY &&
        native->process_owner.state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_READY &&
        native->process_owner.writer_held && native->process_owner.player_store_installed;
#else
    (void)runtime;
    (void)native;
    return 0;
#endif
}

static int shadow_stopped(const character_save_journal_v2_runtime *runtime,
    const character_save_journal_v2_runtime_native *native)
{
#if defined(__linux__)
    return runtime && native && !runtime->shadow_active &&
        runtime->current_state == CHARACTER_SAVE_JOURNAL_V2_RUNTIME_DISABLED &&
        !native->shadow_active && !native->serializer_buffer &&
        native->serializer_buffer_capacity == 0 &&
        native->transport_native.transport.connection == 0 &&
        character_save_journal_v2_rpc_transport_get_state(
            &native->transport_native.transport) ==
              CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED &&
        native->process_owner.state == CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STOPPED &&
        !native->process_owner.writer_held &&
        !native->process_owner.player_store_installed && runtime_conninfo_wiped(runtime);
#else
    (void)runtime;
    (void)native;
    return 0;
#endif
}

static void trust_disposable_home(void)
{
    uid_t uid = getuid();

    character_save_journal_v2_set_trusted_uid_for_test(uid);
    character_save_journal_v2_writer_set_trusted_uid_for_test(uid);
    character_save_journal_v2_publish_set_trusted_uid_for_test(uid);
    character_save_journal_v2_ack_set_trusted_uid_for_test(uid);
}

static void init_player(creature *player)
{
    memset(player, 0, sizeof(*player));
    strcpy(player->name, player_name);
    player->level = 10;
    player->hpmax = player->hpcur = 20;
}

static int stop_safely(character_save_journal_v2_runtime *runtime,
    character_save_journal_v2_runtime_native *native)
{
    creature player;
    int fallback_result;

    character_save_journal_v2_runtime_shutdown(runtime);
    character_save_journal_v2_runtime_shutdown(runtime);
    if(!shadow_stopped(runtime, native)) return -1;
    /* The native owner must have restored the original fallback before its
     * transport/buffer are released.  A valid player makes a stale M3
     * callback return IO_ERROR, while this sentinel returns CORRUPT. */
    init_player(&player);
    fallback_save_calls=0;
    fallback_result=save_ply((char *)player_name,&player);
    return fallback_result==PLAYER_STORE_CORRUPT&&fallback_save_calls==1 ? 0 : -1;
}

int main(int argc, char **argv)
{
    const char *root = getenv("MUHAN_HOME");
    const char *mode;
    character_save_journal_v2_runtime runtime;
    character_save_journal_v2_runtime_native native;
    creature player;
    char live[1024];
    int save_result;

    if(argc != 2 || !root || !root[0]) return 2;
    mode = argv[1];
    if(strcmp(mode, "fault") && strcmp(mode, "recover")) return 2;

    trust_disposable_home();
    character_save_journal_v2_runtime_native_init(&native);
    character_save_journal_v2_runtime_init(&runtime, &native.dependencies);
    if(character_save_journal_v2_runtime_start(&runtime) !=
       CHARACTER_SAVE_JOURNAL_V2_RUNTIME_READY || !shadow_ready(&runtime, &native)) {
        character_save_journal_v2_runtime_shutdown(&runtime);
        return fail("native shadow/process-owner/PG RPC boundary did not become READY");
    }

    init_player(&player);
    if(!strcmp(mode, "fault")) {
        save_result = save_ply((char *)player_name, &player);
        if(snprintf(live, sizeof(live), "%s/player/d2/%s", root, player_name) >=
           (int)sizeof(live) || save_result != PLAYER_STORE_OK ||
           native.process_owner.player_store.last_report.reached !=
             CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED ||
           native.process_owner.player_store.last_report.ack_result !=
             CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE ||
           !regular_0600(live) || !live_matches_serializer(root,&player) ||
           journal_markers(root, "published") != 1 ||
           journal_markers(root, "acked") != 0) {
            character_save_journal_v2_runtime_shutdown(&runtime);
            return fail("receipt rejection did not retain one durable published save");
        }
        (void)kill(getpid(), SIGKILL);
        return 127;
    }

    if(native.process_owner.recovery_report.discovered != 1 ||
       native.process_owner.recovery_report.visited != 1 ||
       journal_markers(root, "published") != 1 || journal_markers(root, "acked") != 1 ||
       !live_matches_serializer(root,&player) ||
       stop_safely(&runtime, &native))
        return fail("restart did not replay evidence or safely unwind native ownership");
    puts("m3 runtime shadow PG17 native integration: ok");
    return 0;
}
#else
int main(int argc, char **argv)
{
    (void)argc;
    (void)argv;
    return 77;
}
#endif
