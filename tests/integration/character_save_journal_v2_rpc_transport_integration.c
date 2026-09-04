/* Disposable PG17 harness: conninfo remains outside the transport. */
#include "character_save_journal_v2_rpc_transport_native.h"

#include <libpq-fe.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static const char world[] = "m3-rpc-contract";
static const char bad_cast_world[] = "m3-rpc-bad-cast";
static const char character[] = "92000000-0000-0000-0000-000000000001";
static const char writer[] = "94000000-0000-0000-0000-000000000001";
static const char successor[] = "94000000-0000-0000-0000-000000000002";
static const char command[] = "93000000-0000-0000-0000-000000000001";
static const char command_second[] = "93000000-0000-0000-0000-000000000002";
static const char request[] =
    "0aa61468867b3da964d144350b287079c4bcffeec2a5938fb109a1cbe0902b4f";
static const char hash[] =
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
static const char request_second[] =
    "278ae46f1aa952a7bd263b845ec981b3aa7dba8b8fb1208224f2da0b5f3b99ec";
static const char hash_second[] =
    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

static int fail(const char *what)
{
    fprintf(stderr, "m3 RPC transport integration: %s\n", what);
    return 1;
}

static void pg_failure(const char *label, PGconn *connection, PGresult *result)
{
    const char *state = result ? PQresultErrorField(result, PG_DIAG_SQLSTATE) : 0;

    /* Do not print the caller's conninfo: libpq diagnostics give the useful
     * connection/result context without retaining credentials. */
    fprintf(stderr, "m3 RPC transport integration: %s: PQerrorMessage=%s PQresultStatus=%s SQLSTATE=%s\n",
        label, connection ? PQerrorMessage(connection) : "no PGconn",
        result ? (PQresultStatus(result) == PGRES_TUPLES_OK ? "PGRES_TUPLES_OK" :
                  PQresultStatus(result) == PGRES_COMMAND_OK ? "PGRES_COMMAND_OK" :
                  "non-success") : "n/a", state ? state : "n/a");
}

static int expiry(char output[64])
{
    time_t now = time(0) + 120;
    struct tm *utc = gmtime(&now);

    return utc && strftime(output, 64, "%Y-%m-%dT%H:%M:%SZ", utc) > 0;
}

static PGconn *connect_database(const char *conninfo, const char *label)
{
    PGconn *connection = PQconnectdb(conninfo);

    if (connection && PQstatus(connection) == CONNECTION_OK)
        return connection;
    pg_failure(label, connection, 0);
    if (connection)
        PQfinish(connection);
    return 0;
}

static PGresult *command_exec(PGconn *connection, const char *sql,
    const char *label)
{
    PGresult *result = PQexec(connection, sql);

    if (result && PQresultStatus(result) == PGRES_COMMAND_OK)
        return result;
    pg_failure(label, connection, result);
    if (result)
        PQclear(result);
    return 0;
}

static int tuples_bool(PGconn *connection, const char *sql, const char *label)
{
    PGresult *result = PQexec(connection, sql);
    int answer = result && PQresultStatus(result) == PGRES_TUPLES_OK &&
        PQntuples(result) == 1 && PQnfields(result) == 1 &&
        !PQgetisnull(result, 0, 0) && !strcmp(PQgetvalue(result, 0, 0), "t");

    if (!answer)
        pg_failure(label, connection, result);
    if (result)
        PQclear(result);
    return answer;
}

static int backend_gone(PGconn *super, int pid, const char *label)
{
    char sql[192];
    int attempt;
    PGresult *result;
    int gone;

    if (snprintf(sql, sizeof(sql),
            "select not exists (select 1 from pg_stat_activity where pid=%d)",
            pid) >= (int)sizeof(sql))
        return fail("backend verification SQL overflow");
    /* PQfinish is asynchronous from the observer's perspective.  Poll with a
     * bounded 1.25 second budget instead of treating one 50ms snapshot as a
     * lifecycle proof. */
    for (attempt = 0; attempt < 50; attempt++) {
        result = PQexec(super, sql);
        gone = result && PQresultStatus(result) == PGRES_TUPLES_OK &&
            PQntuples(result) == 1 && PQnfields(result) == 1 &&
            !PQgetisnull(result, 0, 0) &&
            !strcmp(PQgetvalue(result, 0, 0), "t");
        if (result)
            PQclear(result);
        if (gone)
            return 1;
        result = attempt == 49 ? 0 : PQexec(super, "select pg_sleep(0.025)");
        if (attempt != 49 && (!result ||
            PQresultStatus(result) != PGRES_TUPLES_OK)) {
            pg_failure("backend shutdown polling query", super, result);
            if (result)
                PQclear(result);
            return fail("backend shutdown polling query failed");
        }
        if (result)
            PQclear(result);
        if (attempt != 49 && PQstatus(super) != CONNECTION_OK) {
            pg_failure("backend shutdown polling connection", super, 0);
            return 0;
        }
    }
    fprintf(stderr, "m3 RPC transport integration: %s timed out after 1250ms waiting for backend pid %d to exit\n",
        label, pid);
    return 0;
}

static int terminate_backend(PGconn *super, int pid)
{
    char sql[128];

    if (snprintf(sql, sizeof(sql), "select pg_terminate_backend(%d)", pid) >=
        (int)sizeof(sql))
        return fail("terminate backend SQL overflow");
    return tuples_bool(super, sql,
        "disposable super connection did not terminate writer backend");
}

static int expire_sealed_predecessor(const char *conninfo)
{
    PGconn *connection = connect_database(conninfo, "disposable super");
    PGresult *result;
    int answer;

    if (!connection)
        return 0;
    /* This fixture needs a sealed predecessor that is already expired, while
     * the persisted epoch invariant still requires expires_at > issued_at.
     * Rebase both timestamps in one statement instead of making expiry depend
     * on how long the preceding transport assertions happened to take. */
    result = command_exec(connection,
        "update private.game_character_writer_epochs set issued_at="
        "clock_timestamp()-interval '2 seconds', expires_at="
        "clock_timestamp()-interval '1 second' where world_id='m3-rpc-contract'",
        "expire sealed predecessor");
    answer = result != 0;
    if (result)
        PQclear(result);
    PQfinish(connection);
    return answer;
}

static int begin_before_start_rejected(const char *conninfo)
{
    PGconn *connection = connect_database(conninfo, "writer");
    PGresult *result;
    character_save_journal_v2_rpc_transport_native native;
    int answer;

    if (!connection)
        return fail("BEGIN-before-start writer connection unavailable");
    result = command_exec(connection, "begin", "BEGIN before start");
    if (!result) {
        PQfinish(connection);
        return 1;
    }
    PQclear(result);
    character_save_journal_v2_rpc_transport_native_init(&native);
    answer = character_save_journal_v2_rpc_transport_native_start(&native,
        connection);
    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE)
        return fail("BEGIN-before-start was not rejected");
    return 0;
}

static int role_drift_rejected(const char *writer_conninfo,
    const char *super_conninfo)
{
    PGconn *connection = connect_database(writer_conninfo, "writer");
    PGconn *super = 0;
    PGresult *result;
    character_save_journal_v2_rpc_transport_native native;
    character_save_journal_v2_rpc_route route;
    int pid;
    int answer;

    if (!connection)
        return fail("role-drift writer connection unavailable");
    character_save_journal_v2_rpc_transport_native_init(&native);
    if (character_save_journal_v2_rpc_transport_native_start(&native,
            connection))
        return fail("role-drift writer transport did not start");
    pid = PQbackendPID(connection);
    result = command_exec(connection, "set role none", "SET ROLE NONE");
    if (!result) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return 1;
    }
    PQclear(result);
    if (!tuples_bool(connection, "select current_user = 'mud_writer_login'",
            "SET ROLE NONE did not make current_user mud_writer_login")) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return 1;
    }
    super = connect_database(super_conninfo, "disposable super");
    if (!super) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("role-drift super connection unavailable");
    }
    answer = character_save_journal_v2_rpc_transport_lookup_route(
        &native.transport, world, "M3hero", &route);
    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE ||
        character_save_journal_v2_rpc_transport_get_state(&native.transport) !=
            CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE ||
        native.transport.connection)
        answer = fail("role drift did not close the writer session as UNAVAILABLE");
    else if (character_save_journal_v2_rpc_transport_lookup_route(
                 &native.transport, world, "M3hero", &route) !=
                 CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE ||
             native.transport.connection)
        answer = fail("role-drift UNAVAILABLE transport retried");
    else if (!backend_gone(super, pid,
                 "role-drift transport left writer backend open"))
        answer = 1;
    else
        answer = 0;
    character_save_journal_v2_rpc_transport_close(&native.transport);
    character_save_journal_v2_rpc_transport_close(&native.transport);
    PQfinish(super);
    return answer;
}

static int terminated_backend_rejected(const char *writer_conninfo,
    const char *super_conninfo)
{
    PGconn *connection = connect_database(writer_conninfo, "writer");
    PGconn *super = 0;
    character_save_journal_v2_rpc_transport_native native;
    character_save_journal_v2_rpc_route route;
    int pid;
    int answer;

    if (!connection)
        return fail("disconnect writer connection unavailable");
    character_save_journal_v2_rpc_transport_native_init(&native);
    if (character_save_journal_v2_rpc_transport_native_start(&native,
            connection))
        return fail("disconnect writer transport did not start");
    pid = PQbackendPID(connection);
    super = connect_database(super_conninfo, "disposable super");
    if (!super) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("disconnect super connection unavailable");
    }
    if (!terminate_backend(super, pid)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        PQfinish(super);
        return 1;
    }
    answer = character_save_journal_v2_rpc_transport_lookup_route(
        &native.transport, world, "M3hero", &route);
    if (answer != CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE ||
        character_save_journal_v2_rpc_transport_get_state(&native.transport) !=
            CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE ||
        native.transport.connection)
        answer = fail("terminated writer backend did not become UNAVAILABLE");
    else if (character_save_journal_v2_rpc_transport_lookup_route(
                 &native.transport, world, "M3hero", &route) !=
                 CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE ||
             native.transport.connection)
        answer = fail("terminated writer backend retried internally");
    else if (!backend_gone(super, pid,
                 "terminated writer backend remained visible"))
        answer = 1;
    else
        answer = 0;
    character_save_journal_v2_rpc_transport_close(&native.transport);
    character_save_journal_v2_rpc_transport_close(&native.transport);
    if (character_save_journal_v2_rpc_transport_get_state(&native.transport) !=
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED)
        answer = fail("close after unavailable transport was not idempotent");
    PQfinish(super);
    return answer;
}

int main(void)
{
    const char *conninfo = getenv("M3_RPC_TRANSPORT_DATABASE_URL");
    const char *super_conninfo = getenv("M3_RPC_TRANSPORT_SUPER_DATABASE_URL");
    PGconn *connection;
    character_save_journal_v2_rpc_transport_native native;
    character_save_journal_v2_rpc_route route;
    character_save_journal_v2_rpc_route_v3 route_v3;
    character_save_journal_v2_receipt receipt;
    char until[64];
    char renewed[64];
    unsigned long long epoch = 0;

    if (!conninfo || !super_conninfo)
        return fail("writer or disposable super conninfo is missing");
    if (!expiry(until))
        return fail("could not format initial lease expiry");
    if (begin_before_start_rejected(conninfo) ||
        role_drift_rejected(conninfo, super_conninfo) ||
        terminated_backend_rejected(conninfo, super_conninfo))
        return 1;
    connection = connect_database(conninfo, "main writer");
    if (!connection)
        return fail("main writer connection unavailable");
    character_save_journal_v2_rpc_transport_native_init(&native);
    if (character_save_journal_v2_rpc_transport_native_start(&native,
            connection))
        return fail("main writer transport did not start");
    if (character_save_journal_v2_rpc_transport_acquire(&native.transport,
            bad_cast_world, writer, "not-a-timestamp", &epoch, renewed) !=
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("bad timestamptz cast was not INVALID");
    }
    if (character_save_journal_v2_rpc_transport_lookup_route(&native.transport,
            world, "M3hero", &route) || strcmp(route.character_id, character) ||
        route.storage_format != 1) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("writer route lookup failed");
    }
    if (character_save_journal_v2_rpc_transport_lookup_route_v3(
            &native.transport, world, "M3hero", &route_v3) ||
        strcmp(route_v3.character_id, character) || route_v3.storage_format != 1 ||
        strcmp(route_v3.head_state, "uninitialized") || route_v3.head_sha256[0] ||
        route_v3.head_revision != 0) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("initial v3 writer route did not remain uninitialized");
    }
    if (character_save_journal_v2_rpc_transport_acquire(&native.transport,
            world, writer, until, &epoch, renewed) || epoch != 1) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("writer epoch acquire failed");
    }
    if (!expiry(until) || character_save_journal_v2_rpc_transport_renew(
            &native.transport, world, writer, epoch, until, renewed)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("writer epoch renew failed");
    }
    if (character_save_journal_v2_rpc_transport_seed_absent_head(
            &native.transport, world, "M3hero", character, writer, epoch, 1) ||
        character_save_journal_v2_rpc_transport_seed_absent_head(
            &native.transport, world, "M3hero", character, writer, epoch, 1)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("absent-head seed or exact retry failed");
    }
    if (character_save_journal_v2_rpc_transport_lookup_route_v3(
            &native.transport, world, "M3hero", &route_v3) ||
        strcmp(route_v3.head_state, "absent") || route_v3.head_sha256[0] ||
        route_v3.head_revision != 0) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("seed did not rebind to absent revision zero");
    }
    if (character_save_journal_v2_rpc_transport_seal(&native.transport,
            world, writer, epoch) || !expire_sealed_predecessor(super_conninfo) ||
        !expiry(until) || character_save_journal_v2_rpc_transport_acquire(
            &native.transport, world, successor, until, &epoch, renewed) || epoch != 2) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("sealed predecessor did not admit exact successor epoch");
    }
    if (character_save_journal_v2_rpc_transport_seed_absent_head(
            &native.transport, world, "M3hero", character, writer, 1, 1) !=
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED ||
        character_save_journal_v2_rpc_transport_seed_absent_head(
            &native.transport, world, "M3hero", character, successor, epoch, 1)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("stale predecessor or exact successor seed contract failed");
    }
    memset(&receipt, 0, sizeof(receipt));
    receipt.world_id = world;
    receipt.legacy_name_key = (const unsigned char *)"M3hero";
    receipt.legacy_name_key_length = 6;
    receipt.character_id = character;
    receipt.command_id = command;
    receipt.writer_instance_id = successor;
    receipt.request_sha256 = request;
    receipt.writer_epoch = epoch;
    receipt.writer_revision = 1;
    receipt.expected_state = "absent";
    receipt.post_sha256 = hash;
    receipt.storage_format = 1;
    if (character_save_journal_v2_rpc_transport_receipt(&native.transport,
            &receipt) || character_save_journal_v2_rpc_transport_receipt(
            &native.transport, &receipt)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("receipt RPC or exact retry failed");
    }
    if (character_save_journal_v2_rpc_transport_lookup_route_v3(
            &native.transport, world, "M3hero", &route_v3) ||
        strcmp(route_v3.head_state, "existing") || strcmp(route_v3.head_sha256, hash) ||
        route_v3.head_revision != 1) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("first save did not become v3 revision one");
    }
    receipt.command_id = command_second;
    receipt.request_sha256 = request_second;
    receipt.writer_revision = 2;
    receipt.expected_state = "existing";
    receipt.expected_sha256 = hash;
    receipt.post_sha256 = hash_second;
    if (character_save_journal_v2_rpc_transport_receipt(&native.transport,
            &receipt)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("second save receipt RPC failed");
    }
    if (character_save_journal_v2_rpc_transport_lookup_route_v3(
            &native.transport, world, "M3hero", &route_v3) ||
        strcmp(route_v3.head_state, "existing") ||
        strcmp(route_v3.head_sha256, hash_second) || route_v3.head_revision != 2) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("second save did not become v3 revision two");
    }
    if (character_save_journal_v2_rpc_transport_seal(&native.transport,
            world, successor, epoch)) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("writer epoch seal failed");
    }
    if (character_save_journal_v2_rpc_transport_seed_absent_head(
            &native.transport, world, "M3hero", character, successor, epoch, 1) !=
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED) {
        character_save_journal_v2_rpc_transport_close(&native.transport);
        return fail("sealed writer seed was not permanently rejected");
    }
    character_save_journal_v2_rpc_transport_close(&native.transport);
    return 0;
}
