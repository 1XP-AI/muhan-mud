#include "character_save_journal_v2_rpc_transport_native.h"

#include <libpq-fe.h>
#include <stdio.h>

static void native_connection_failure(PGconn *connection)
{
    /* Deliberately log libpq diagnostics only: neither conninfo nor a
     * password is retained by, or available to, this adapter. */
    fprintf(stderr, "m3 rpc transport: connection failure: PQerrorMessage=%s PQresultStatus=n/a SQLSTATE=n/a\n",
        connection ? PQerrorMessage(connection) : "no PGconn");
}

static int native_connection_ok(void *connection)
{
    return PQstatus((PGconn *)connection) == CONNECTION_OK;
}

static int native_transaction_status(void *connection)
{
    return PQtransactionStatus((PGconn *)connection) == PQTRANS_IDLE;
}

static void *native_exec(void *connection, const char *sql, int count,
    const unsigned int *types, const char *const *values, const int *lengths,
    const int *formats, int result_format)
{
    PGconn *pg_connection = (PGconn *)connection;
    PGresult *result = PQexecParams(pg_connection, sql, count, (const Oid *)types,
        values, lengths, formats, result_format);
    const char *state;

    if (!result) {
        native_connection_failure(pg_connection);
        return 0;
    }
    if (PQresultStatus(result) != PGRES_TUPLES_OK) {
        state = PQresultErrorField(result, PG_DIAG_SQLSTATE);
        fprintf(stderr, "m3 rpc transport: result failure: PQerrorMessage=%s PQresultStatus=%d SQLSTATE=%s\n",
            PQerrorMessage(pg_connection), (int)PQresultStatus(result),
            state ? state : "n/a");
    }
    return result;
}

static int native_status(void *result)
{
    return PQresultStatus((const PGresult *)result) == PGRES_TUPLES_OK ?
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_TUPLES_OK : 0;
}

static int native_rows(void *result)
{
    return PQntuples((const PGresult *)result);
}

static int native_columns(void *result)
{
    return PQnfields((const PGresult *)result);
}

static const char *native_value(void *result, int row, int column)
{
    return PQgetisnull((const PGresult *)result, row, column) ? 0 :
        PQgetvalue((const PGresult *)result, row, column);
}

static int native_length(void *result, int row, int column)
{
    return PQgetisnull((const PGresult *)result, row, column) ? -1 :
        PQgetlength((const PGresult *)result, row, column);
}

static const char *native_sqlstate(void *result)
{
    return PQresultErrorField((const PGresult *)result, PG_DIAG_SQLSTATE);
}

static void native_clear(void *result)
{
    PQclear((PGresult *)result);
}

static void native_finish(void *connection)
{
    PQfinish((PGconn *)connection);
}

static const character_save_journal_v2_rpc_transport_operations
native_operations = {
    native_connection_ok,
    native_transaction_status,
    native_exec,
    native_status,
    native_rows,
    native_columns,
    native_value,
    native_length,
    native_sqlstate,
    native_clear,
    native_finish
};

void character_save_journal_v2_rpc_transport_native_init(
    character_save_journal_v2_rpc_transport_native *native)
{
    if (native)
        character_save_journal_v2_rpc_transport_init(&native->transport);
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_rpc_transport_native_start(
    character_save_journal_v2_rpc_transport_native *native, void *connection)
{
    if (!native) {
        if (connection)
            PQfinish((PGconn *)connection);
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE;
    }
    if (connection && PQstatus((PGconn *)connection) != CONNECTION_OK)
        native_connection_failure((PGconn *)connection);
    return character_save_journal_v2_rpc_transport_start(&native->transport,
        &native_operations, 0, connection);
}
