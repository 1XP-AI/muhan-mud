#include "character_save_journal_v2_receipt_transport_native.h"

#include <libpq-fe.h>

#include <stdlib.h>
#include <string.h>

static const char world_id[]="m3-contract";
static const char character_id[]="92000000-0000-0000-0000-000000000001";
static const char writer_a[]="94000000-0000-0000-0000-000000000001";
static const char writer_b[]="94000000-0000-0000-0000-000000000002";
static const char command_1[]="93000000-0000-0000-0000-000000000001";
static const char command_2[]="93000000-0000-0000-0000-000000000002";
static const char command_3[]="93000000-0000-0000-0000-000000000003";
static const char hash_a[]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
static const char hash_b[]="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
static const char golden_request[]="eb83eb12f5875dad83f5f91e128c4874b2673b4089deff4967c58898d9c69171";
static const char wrong_request[]="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc";

static character_save_journal_v2_receipt receipt_for(const char *mode)
{
    character_save_journal_v2_receipt receipt;
    memset(&receipt,0,sizeof(receipt));
    receipt.world_id=world_id;
    receipt.legacy_name_key=(const unsigned char *)"M3hero";
    receipt.legacy_name_key_length=6;
    receipt.character_id=character_id;
    receipt.command_id=command_1;
    receipt.writer_instance_id=writer_a;
    receipt.request_sha256=golden_request;
    receipt.writer_epoch=1;
    receipt.writer_revision=1;
    receipt.expected_state="absent";
    receipt.expected_sha256=0;
    receipt.post_sha256=hash_a;
    receipt.storage_format=1;
    if(!strcmp(mode,"invalid")) {
        receipt.command_id=command_2;
        receipt.request_sha256=wrong_request;
        receipt.writer_revision=2;
        receipt.expected_state="existing";
        receipt.expected_sha256=hash_a;
        receipt.post_sha256=hash_b;
    } else if(!strcmp(mode,"rejected")) {
        receipt.command_id=command_3;
        receipt.writer_instance_id=writer_b;
        receipt.request_sha256=wrong_request;
        receipt.writer_revision=2;
        receipt.expected_state="existing";
        receipt.expected_sha256=hash_a;
        receipt.post_sha256=hash_b;
    }
    return receipt;
}

static int probe_native(character_save_journal_v2_receipt_transport_native *native)
{
    PGconn *connection;
    PGresult *result;
    int answer=1;

    connection=(PGconn *)native->transport.operations->connect(native);
    if(!connection||PQstatus(connection)!=CONNECTION_OK) goto done;
    result=PQexec(connection,"select current_user::text, session_user::text");
    if(!result||PQresultStatus(result)!=PGRES_TUPLES_OK||PQntuples(result)!=1||
       strcmp(PQgetvalue(result,0,0),"mud_writer")||
       strcmp(PQgetvalue(result,0,1),"postgres")) {
        if(result) PQclear(result);
        goto done;
    }
    PQclear(result);
    result=PQexec(connection,
        "select 1 from private.game_character_shadow_receipts limit 1");
    if(!result||PQresultStatus(result)!=PGRES_FATAL_ERROR||
       !PQresultErrorField(result,PG_DIAG_SQLSTATE)||
       strcmp(PQresultErrorField(result,PG_DIAG_SQLSTATE),"42501")) {
        if(result) PQclear(result);
        goto done;
    }
    PQclear(result);
    answer=0;
done:
    if(connection) PQfinish(connection);
    return answer;
}

int main(int argc, char **argv)
{
    const char *conninfo;
    const char *mode;
    character_save_journal_v2_receipt_transport_native native;
    character_save_journal_v2_receipt receipt;
    character_save_journal_v2_receipt_result expected;

    if(argc!=2) return 2;
    mode=argv[1];
    conninfo=getenv("M3_RECEIPT_TRANSPORT_DATABASE_URL");
    if(!conninfo||!conninfo[0]) return 2;
    character_save_journal_v2_receipt_transport_native_init(&native,conninfo);
    if(!strcmp(mode,"probe")) return probe_native(&native);
    if(!strcmp(mode,"acked"))
        expected=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    else if(!strcmp(mode,"denied"))
        expected=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    else if(!strcmp(mode,"invalid"))
        expected=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;
    else if(!strcmp(mode,"rejected"))
        expected=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    else return 2;
    receipt=receipt_for(mode);
    return character_save_journal_v2_receipt_transport_callback(&native.transport,&receipt)==expected ? 0 : 1;
}
