#include "character_save_journal_v2_runtime_native.h"

#include <libpq-fe.h>

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
    native->dependencies.environment_get=0;
    native->dependencies.environment_opaque=0;
    native->dependencies.database_operations=&runtime_native_database_operations;
    native->dependencies.database_opaque=0;
    native->dependencies.file_operations=0;
    native->dependencies.file_opaque=0;
}
