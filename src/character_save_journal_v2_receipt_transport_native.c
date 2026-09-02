#include "character_save_journal_v2_receipt_transport_native.h"

#include <libpq-fe.h>

static void *receipt_native_connect(void *opaque)
{
    character_save_journal_v2_receipt_transport_native *native=(character_save_journal_v2_receipt_transport_native *)opaque;
    if(!native||!native->conninfo) return 0;
    return PQconnectdb(native->conninfo);
}
static int receipt_native_connection_ok(void *connection)
{ return PQstatus((PGconn *)connection)==CONNECTION_OK; }
static void *receipt_native_exec(void *connection, const char *sql, int count,
                                 const unsigned int *types, const char *const *values,
                                 const int *lengths, const int *formats, int result_format)
{ return PQexecParams((PGconn *)connection,sql,count,(const Oid *)types,values,lengths,formats,result_format); }
static int receipt_native_status(void *result)
{ return PQresultStatus((const PGresult *)result)==PGRES_TUPLES_OK ? CHARACTER_SAVE_JOURNAL_V2_RECEIPT_TRANSPORT_TUPLES_OK : 0; }
static const char *receipt_native_sqlstate(void *result)
{ return PQresultErrorField((const PGresult *)result,PG_DIAG_SQLSTATE); }
static void receipt_native_clear(void *result)
{ PQclear((PGresult *)result); }
static void receipt_native_finish(void *connection)
{ PQfinish((PGconn *)connection); }
static const character_save_journal_v2_receipt_transport_operations receipt_native_operations={
    receipt_native_connect,receipt_native_connection_ok,receipt_native_exec,
    receipt_native_status,receipt_native_sqlstate,receipt_native_clear,receipt_native_finish
};
void character_save_journal_v2_receipt_transport_native_init(
    character_save_journal_v2_receipt_transport_native *native,
    const char *conninfo)
{
    if(!native) return;
    native->conninfo=conninfo;
    native->transport.operations=&receipt_native_operations;
    native->transport.operations_opaque=native;
}
