/*
 * Process-owner E2E fixture for a disposable PostgreSQL 17 server.
 *
 * TDD evidence is emitted by the companion shell harness: it first proves
 * the 130 baseline contract is RED, then applies 130 and drives these three
 * real-process phases.  This file deliberately uses the production owner,
 * PlayerStore, legacy bounded serializer, journal, and native libpq transport;
 * the test-only UID setters only make a disposable caller-owned MUHAN_HOME
 * usable under the invoking uid.
 */
#include "character_save_journal_v2_process_owner.h"
#include "character_save_journal_v2_rpc_transport_native.h"
#include "character_save_journal_v2.h"
#include "character_save_journal_v2_ack.h"
#include "character_save_journal_v2_publish.h"
#include "character_save_journal_v2_writer.h"
#include "mstruct.h"
#include "player_record_serializer.h"
#include "player_store.h"

#include <libpq-fe.h>

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <time.h>
#include <unistd.h>

static const char world_id[] = "m3-process-owner";
static const char player_name[] = "M3owner";
static const char writer_id[] = "95000000-0000-4000-8000-000000000001";
static const char command_id[] = "96000000-0000-4000-8000-000000000001";

/* player_store.c retains the legacy file fallback in its global initializer.
 * The owner must replace that fallback before this test calls save_ply; these
 * sentinels make an accidental fallback use fail, while keeping this focused
 * harness independent of the unrelated legacy loader's full link closure. */
int file_player_store_save(char *name, creature *player)
{
    (void)name;
    (void)player;
    return PLAYER_STORE_IO_ERROR;
}

int file_player_store_load(char *name, creature **player)
{
    (void)name;
    if(player) *player = 0;
    return PLAYER_STORE_NOT_FOUND;
}

static int fail(const char *message)
{
    fprintf(stderr, "m3 process-owner PG17 integration: %s\n", message);
    return 1;
}

static int injected_deadline(void *opaque, char output[64])
{
    const char *deadline = (const char *)opaque;

    if(!deadline || strlen(deadline) >= 64 || deadline[0] == 0)
        return -1;
    strcpy(output, deadline);
    return 0;
}

/* The load fallback is required by the public owner configuration but no load
 * is dispatched in this save-only E2E.  It must not participate in save,
 * serialization, publication, receipt, or recovery. */
static int unused_load(void *opaque, char *name, struct creature **player)
{
    (void)opaque;
    (void)name;
    if(player) *player = 0;
    return PLAYER_STORE_NOT_FOUND;
}

static void trust_disposable_home(void)
{
    uid_t uid = getuid();

    character_save_journal_v2_set_trusted_uid_for_test(uid);
    character_save_journal_v2_writer_set_trusted_uid_for_test(uid);
    character_save_journal_v2_publish_set_trusted_uid_for_test(uid);
    character_save_journal_v2_ack_set_trusted_uid_for_test(uid);
}

static int path_for(char output[1024], const char *root, const char *leaf)
{
    int written = snprintf(output, 1024, "%s/character-save-journal/%s",
        root, leaf);

    return written > 0 && written < 1024 ? 0 : -1;
}

static int regular_0600(const char *path)
{
    struct stat status;

    return lstat(path, &status) == 0 && S_ISREG(status.st_mode) &&
        (status.st_mode & 07777) == 0600 && status.st_nlink == 1;
}

static int journal_shape(const char *root, const char *marker)
{
    char prepared[1024], state[1024];
    char prepared_leaf[64], state_leaf[64];

    if(snprintf(prepared_leaf, sizeof(prepared_leaf), "%s.prepared", command_id) >=
       (int)sizeof(prepared_leaf) ||
       snprintf(state_leaf, sizeof(state_leaf), "%s.%s", command_id, marker) >=
       (int)sizeof(state_leaf) || path_for(prepared, root, prepared_leaf) ||
       path_for(state, root, state_leaf))
        return 0;
    return regular_0600(prepared) && regular_0600(state);
}

static int live_matches_serializer(const char *root, const creature *player)
{
    char live[1024];
    unsigned char expected[65536];
    unsigned char actual[65536];
    player_record_serializer_limits limits;
    unsigned long expected_length = 0;
    int descriptor;
    ssize_t length;

    if(snprintf(live, sizeof(live), "%s/player/a5/%s", root, player_name) >=
       (int)sizeof(live))
        return 0;
    limits.max_depth = 64;
    limits.max_objects = 8192;
    if(player_record_serialize_bounded((creature *)player, 0, (char *)expected,
       sizeof(expected), &expected_length, &limits) != PLAYER_RECORD_SERIALIZER_OK ||
       expected_length > sizeof(actual))
        return 0;
    descriptor = open(live, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
    if(descriptor < 0) return 0;
    length = read(descriptor, actual, sizeof(actual));
    if(close(descriptor) != 0) return 0;
    return length == (ssize_t)expected_length &&
        !memcmp(actual, expected, expected_length);
}

static int start_native(character_save_journal_v2_rpc_transport_native *native,
    const char *conninfo, PGconn **connection_out)
{
    PGconn *connection;

    connection = PQconnectdb(conninfo);
    if(!connection || PQstatus(connection) != CONNECTION_OK) {
        if(connection) PQfinish(connection);
        return -1;
    }
    character_save_journal_v2_rpc_transport_native_init(native);
    if(character_save_journal_v2_rpc_transport_native_start(native, connection) !=
       CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return -1;
    *connection_out = connection;
    return 0;
}

static int shutdown_owner_keeps_transport(
    character_save_journal_v2_process_owner *owner,
    character_save_journal_v2_rpc_transport_native *native, PGconn *connection)
{
    if(character_save_journal_v2_process_owner_shutdown(owner) !=
       CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_SHUTDOWN_OK)
        return -1;
    if(character_save_journal_v2_rpc_transport_get_state(&native->transport) !=
       CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY ||
       native->transport.connection != connection)
        return -1;
    character_save_journal_v2_rpc_transport_close(&native->transport);
    return character_save_journal_v2_rpc_transport_get_state(&native->transport) ==
       CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED &&
       native->transport.connection == 0 ? 0 : -1;
}

/* process_owner uses one UUID supplier for writer bootstrap and saves.  The
 * fixture changes only its phase: bootstrap gets writer_id; dispatched save
 * gets command_id.  It is an injected deterministic producer, not a protocol
 * or transport stub. */
typedef struct uuid_source {
    unsigned int calls;
} uuid_source;

static int deterministic_uuid(void *opaque, char output[37])
{
    uuid_source *source = (uuid_source *)opaque;

    if(!source) return -1;
    source->calls++;
    strcpy(output, source->calls == 1 ? writer_id : command_id);
    return 0;
}

static character_save_journal_v2_process_owner_startup_result
configure_and_start(character_save_journal_v2_process_owner *owner,
    character_save_journal_v2_rpc_transport_native *native, const char *root,
    const char *deadline, char *buffer, unsigned long buffer_capacity,
    uuid_source *source)
{
    character_save_journal_v2_process_owner_configuration configuration;

    memset(&configuration, 0, sizeof(configuration));
    configuration.root = root;
    configuration.world_id = world_id;
    configuration.transport = &native->transport;
    configuration.buffer = buffer;
    configuration.buffer_capacity = buffer_capacity;
    configuration.serializer_limits.max_depth = 64;
    configuration.serializer_limits.max_objects = 8192;
    configuration.acquire_deadline = injected_deadline;
    configuration.acquire_deadline_opaque = (void *)deadline;
    configuration.candidate_uuid = deterministic_uuid;
    configuration.candidate_uuid_opaque = source;
    configuration.file_load = unused_load;
    character_save_journal_v2_process_owner_init(owner, &configuration);
    return character_save_journal_v2_process_owner_start(owner);
}

static void init_player(creature *player)
{
    memset(player, 0, sizeof(*player));
    strcpy(player->name, player_name);
    player->level = 10;
    player->hpmax = player->hpcur = 20;
}

static int wait_for_go(const char *ready_path)
{
    char go_path[1024];
    int attempt;

    if(snprintf(go_path, sizeof(go_path), "%s.go", ready_path) >=
       (int)sizeof(go_path)) return -1;
    {
        int descriptor = open(ready_path, O_WRONLY | O_CREAT | O_TRUNC, 0600);
        if(descriptor < 0 || write(descriptor, "ready\n", 6) != 6 ||
           close(descriptor) != 0) return -1;
    }
    for(attempt = 0; attempt < 200; attempt++) {
        if(access(go_path, F_OK) == 0) return 0;
        usleep(50000);
    }
    return -1;
}

int main(int argc, char **argv)
{
    const char *conninfo = getenv("M3_PROCESS_OWNER_DATABASE_URL");
    const char *root = getenv("MUHAN_HOME");
    const char *deadline = getenv("M3_PROCESS_OWNER_DEADLINE");
    const char *mode;
    character_save_journal_v2_rpc_transport_native native;
    character_save_journal_v2_process_owner owner;
    PGconn *connection = 0;
    uuid_source source;
    creature player;
    char buffer[65536];
    int save_result;
    character_save_journal_v2_process_owner_startup_result startup_result;

    if(argc != 2 || !conninfo || !conninfo[0] || !root || !root[0] ||
       !deadline || !deadline[0]) return 2;
    mode = argv[1];
    if(strcmp(mode, "receipt-failure") && strcmp(mode, "receipt-crash") &&
       strcmp(mode, "recover-replay") && strcmp(mode, "stale")) return 2;
    trust_disposable_home();
    if(start_native(&native, conninfo, &connection)) return fail("native writer login unavailable");
    memset(&source, 0, sizeof(source));
    memset(buffer, 0, sizeof(buffer));
    startup_result = configure_and_start(&owner, &native, root, deadline,
        buffer, sizeof(buffer), &source);
    if(startup_result != CHARACTER_SAVE_JOURNAL_V2_PROCESS_OWNER_STARTUP_OK) {
        fprintf(stderr,
            "m3 process-owner PG17 integration: startup=%d recovery=%d state=%d "
            "shutdown=%d writer_held=%d store_installed=%d uuid_calls=%u\n",
            (int)startup_result, (int)owner.recovery_result, (int)owner.state,
            (int)owner.shutdown_result, owner.writer_held,
            owner.player_store_installed, source.calls);
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("process owner did not bootstrap or recover");
    }
    init_player(&player);

    if(!strcmp(mode, "receipt-failure") || !strcmp(mode, "receipt-crash")) {
        save_result = save_ply((char *)player_name, &player);
        if(save_result != PLAYER_STORE_OK ||
           owner.player_store.last_report.reached !=
             CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED ||
           owner.player_store.last_report.ack_result !=
             CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE ||
           !journal_shape(root, "published") || !live_matches_serializer(root, &player)) {
            character_save_journal_v2_process_owner_shutdown(&owner);
            character_save_journal_v2_rpc_transport_close(&native.transport);
            return fail("receipt rejection did not retain published legacy evidence");
        }
        if(!strcmp(mode, "receipt-crash")) {
            (void)kill(getpid(), SIGKILL);
            _exit(127);
        }
    } else if(!strcmp(mode, "recover-replay")) {
        if(owner.recovery_report.discovered != 1 || owner.recovery_report.visited != 1 ||
           !journal_shape(root, "acked") || !live_matches_serializer(root, &player)) {
            character_save_journal_v2_process_owner_shutdown(&owner);
            character_save_journal_v2_rpc_transport_close(&native.transport);
            return fail("recovery-before-install did not durably acknowledge evidence");
        }
        save_result = save_ply((char *)player_name, &player);
        if(save_result != PLAYER_STORE_IO_ERROR || source.calls != 2 ||
           owner.player_store.last_report.reached !=
             CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED ||
           !journal_shape(root, "acked")) {
            fprintf(stderr,
                "m3 process-owner PG17 integration: replay result=%d reached=%d "
                "uuid_calls=%u acked_shape=%d\n", save_result,
                (int)owner.player_store.last_report.reached, source.calls,
                journal_shape(root, "acked"));
            character_save_journal_v2_process_owner_shutdown(&owner);
            character_save_journal_v2_rpc_transport_close(&native.transport);
            return fail("same command replay mutated acknowledged local evidence");
        }
    } else {
        const char *ready_path = getenv("M3_PROCESS_OWNER_STALE_READY");
        if(!ready_path || wait_for_go(ready_path)) {
            character_save_journal_v2_process_owner_shutdown(&owner);
            character_save_journal_v2_rpc_transport_close(&native.transport);
            return fail("stale-writer coordination failed");
        }
        save_result = save_ply((char *)player_name, &player);
        if(save_result != PLAYER_STORE_IO_ERROR || source.calls != 1 ||
           owner.player_store.last_report.reached != 0 ||
           !journal_shape(root, "acked")) {
            fprintf(stderr,
                "m3 process-owner PG17 integration: stale result=%d reached=%d "
                "publish=%d ack=%d uuid_calls=%u acked_shape=%d\n",
                save_result, (int)owner.player_store.last_report.reached,
                (int)owner.player_store.last_report.publish_result,
                (int)owner.player_store.last_report.ack_result, source.calls,
                journal_shape(root, "acked"));
            character_save_journal_v2_process_owner_shutdown(&owner);
            character_save_journal_v2_rpc_transport_close(&native.transport);
            return fail("stale writer changed acknowledged local evidence");
        }
    }
    if(shutdown_owner_keeps_transport(&owner, &native, connection))
        return fail("process-owner transport shutdown ownership violated");
    puts("m3 process-owner PG17 native integration: ok");
    return 0;
}
