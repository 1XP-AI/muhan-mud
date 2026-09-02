#include "character_save_journal_v2_receipt_transport_native.h"
#include "character_save_journal_v2_protocol.h"

#include <libpq-fe.h>

#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static const char world_id[]="m3-restart";
static const char character_id[]="92500000-0000-0000-0000-000000000001";
static const char writer_a[]="94500000-0000-0000-0000-000000000001";
static const char command_1[]="93500000-0000-0000-0000-000000000001";
static const char hash_a[]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
static const unsigned char protocol_payload[]="m3-restart-protocol-payload";
static const char record_sql[]=
    "select private.record_legacy_published_receipt($1::text,$2::text,$3::uuid,$4::uuid,$5::uuid,$6::text,$7::bigint,$8::bigint,$9::text,$10::text,$11::text,$12::smallint)";
static const unsigned int record_types[]={25,25,2950,2950,2950,25,20,20,25,25,25,21};
static const int record_formats[]={0,0,0,0,0,0,0,0,0,0,0,0};

static int receipt_for(character_save_journal_v2_receipt *receipt)
{
    const char *request=getenv("M3_RPC_RESTART_REQUEST_SHA256");
    if(!receipt||!request||strlen(request)!=64) return -1;
    memset(receipt,0,sizeof(*receipt));
    receipt->world_id=world_id;
    receipt->legacy_name_key=(const unsigned char *)"M3restart";
    receipt->legacy_name_key_length=9;
    receipt->character_id=character_id;
    receipt->command_id=command_1;
    receipt->writer_instance_id=writer_a;
    receipt->request_sha256=request;
    receipt->writer_epoch=1;
    receipt->writer_revision=1;
    receipt->expected_state="absent";
    receipt->expected_sha256=0;
    receipt->post_sha256=hash_a;
    receipt->storage_format=1;
    return 0;
}

static void protocol_trust_local_test_root(void)
{
    character_save_journal_v2_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_writer_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_publish_set_trusted_uid_for_test(getuid());
    character_save_journal_v2_ack_set_trusted_uid_for_test(getuid());
}

static character_save_journal_v2_route_lookup_result
protocol_route_lookup(void *opaque, const char *persisted_world,
                      const unsigned char *legacy_name, size_t length,
                      character_save_journal_v2_route_reply *reply)
{
    (void)opaque;
    if(!reply || strcmp(persisted_world,world_id) || length != 9 ||
       memcmp(legacy_name,"M3restart",9))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    memset(reply,0,sizeof(*reply));
    reply->status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    reply->row_count=1;
    strcpy(reply->world_id,world_id);
    strcpy(reply->character_id,character_id);
    memcpy(reply->legacy_name,"M3restart",9);
    reply->legacy_name_length=9;
    strcpy(reply->legacy_shard,"19");
    reply->storage_format=CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1;
    reply->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

static int protocol_serialize(void *opaque,
                              const character_save_journal_v2_writer_tuple *writer,
                              const character_save_journal_v2_bound_route *route,
                              const char *command_id,
                              const unsigned char **bytes, size_t *length)
{
    (void)opaque;
    if(!writer || !route || !bytes || !length || strcmp(writer->world_id,world_id) ||
       strcmp(writer->writer_instance_id,writer_a) || writer->writer_epoch != 1 ||
       strcmp(route->character_id,character_id) || strcmp(command_id,command_1))
        return -1;
    *bytes=protocol_payload;
    *length=sizeof(protocol_payload)-1;
    return 0;
}

static int receipt_callback(const char *conninfo,
                            character_save_journal_v2_receipt_result expected)
{
    character_save_journal_v2_receipt_transport_native native;
    character_save_journal_v2_receipt receipt;
    if(receipt_for(&receipt)) return 2;
    character_save_journal_v2_receipt_transport_native_init(&native,conninfo);
    return character_save_journal_v2_receipt_transport_callback(&native.transport,&receipt)==expected ? 0 : 1;
}

static int result_is(PGresult *result, ExecStatusType status, const char *state)
{
    const char *actual;
    if(!result||PQresultStatus(result)!=status) return 0;
    if(!state) return 1;
    actual=PQresultErrorField(result,PG_DIAG_SQLSTATE);
    return actual&&!strcmp(actual,state);
}

static int rpc_result(const char *conninfo, const char *sql, int expect_epoch,
                      const char *expect_state)
{
    PGconn *connection=0;
    PGresult *result=0;
    int answer=1;
    connection=PQconnectdb(conninfo);
    if(!connection||PQstatus(connection)!=CONNECTION_OK) goto done;
    result=PQexec(connection,sql);
    if(expect_state) {
        if(result_is(result,PGRES_FATAL_ERROR,expect_state)) answer=0;
    } else if(result_is(result,PGRES_TUPLES_OK,0) &&
              (!expect_epoch || (PQntuples(result)==1 &&
                atoi(PQgetvalue(result,0,0))==expect_epoch))) answer=0;
done:
    if(result) PQclear(result);
    if(connection) PQfinish(connection);
    return answer;
}

static int notify_ready(void)
{
    const char *fd_text=getenv("M3_RPC_RESTART_READY_FD");
    char *end=0;
    long fd;
    if(!fd_text||!fd_text[0]) return -1;
    errno=0;
    fd=strtol(fd_text,&end,10);
    if(errno||!end||*end||fd<0||fd>2147483647L) return -1;
    return write((int)fd,"ready\n",6)==6 ? 0 : -1;
}

static int flush_send(PGconn *connection)
{
    struct timespec deadline, now;
    struct pollfd descriptor;
    long remaining;
    int result, waited;
    if(!connection || clock_gettime(CLOCK_MONOTONIC,&deadline)) return -1;
    deadline.tv_sec+=5;
    for(;;) {
        result=PQflush(connection);
        if(result<=0) return result;
        if(clock_gettime(CLOCK_MONOTONIC,&now)) return -1;
        remaining=(deadline.tv_sec-now.tv_sec)*1000L+
                  (deadline.tv_nsec-now.tv_nsec)/1000000L;
        if(remaining<=0) return -1;
        descriptor.fd=PQsocket(connection);
        if(descriptor.fd<0) return -1;
        descriptor.events=POLLOUT;
        descriptor.revents=0;
        waited=poll(&descriptor,1,(int)remaining);
        if(waited<0 && errno==EINTR) continue;
        if(waited<=0 || (descriptor.revents&(POLLERR|POLLHUP|POLLNVAL))) return -1;
    }
}

static character_save_journal_v2_receipt_result
receipt_after_send_wait(const char *conninfo,
                        const character_save_journal_v2_receipt *receipt)
{
    PGconn *connection=0;
    const char *values[12];
    char epoch[32], revision[32], storage[16];
    int answer=CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    if(!receipt || !receipt->world_id || !receipt->legacy_name_key ||
       !receipt->character_id || !receipt->command_id ||
       !receipt->writer_instance_id || !receipt->request_sha256 ||
       !receipt->expected_state || !receipt->post_sha256 ||
       snprintf(epoch,sizeof(epoch),"%llu",receipt->writer_epoch) < 0 ||
       snprintf(revision,sizeof(revision),"%llu",receipt->writer_revision) < 0 ||
       snprintf(storage,sizeof(storage),"%u",(unsigned int)receipt->storage_format) < 0)
        return answer;
    connection=PQconnectdb(conninfo);
    if(!connection || PQstatus(connection)!=CONNECTION_OK) goto done;
    values[0]=receipt->world_id;
    values[1]=(const char *)receipt->legacy_name_key;
    values[2]=receipt->character_id;
    values[3]=receipt->command_id;
    values[4]=receipt->writer_instance_id;
    values[5]=receipt->request_sha256;
    values[6]=epoch;
    values[7]=revision;
    values[8]=receipt->expected_state;
    values[9]=receipt->expected_sha256;
    values[10]=receipt->post_sha256;
    values[11]=storage;
    if(PQsetnonblocking(connection,1) ||
       !PQsendQueryParams(connection,record_sql,12,(const Oid *)record_types,values,
                          0,record_formats,0) || flush_send(connection) || notify_ready())
        goto done;
    for(;;) pause();
done:
    if(connection) PQfinish(connection);
    return answer;
}

static character_save_journal_v2_receipt_result
protocol_async_receipt(void *opaque, const character_save_journal_v2_receipt *receipt)
{
    const char *expected=getenv("M3_RPC_RESTART_REQUEST_SHA256");
    const char *conninfo=(const char *)opaque;
    if(!expected || !receipt || strcmp(receipt->request_sha256,expected))
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    return receipt_after_send_wait(conninfo,receipt);
}

static int protocol_after_send_wait(const char *conninfo)
{
    const char *root=getenv("M3_RPC_RESTART_ROOT");
    character_save_journal_v2_protocol_request request;
    character_save_journal_v2_protocol_operations operations;
    character_save_journal_v2_protocol_report report;
    if(!root || !root[0]) return 2;
    protocol_trust_local_test_root();
    memset(&request,0,sizeof(request));
    memset(&operations,0,sizeof(operations));
    request.root=root;
    request.world_id=world_id;
    request.canonical_legacy_name=(const unsigned char *)"M3restart";
    request.canonical_legacy_name_length=9;
    request.command_uuid=command_1;
    request.writer_revision=1;
    operations.route_lookup=protocol_route_lookup;
    operations.serialize=protocol_serialize;
    operations.receipt=protocol_async_receipt;
    operations.receipt_opaque=(void *)conninfo;
    return character_save_journal_v2_protocol_save(&request,&operations,&report) ==
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK ? 0 : 1;
}

static int protocol_recover(const char *conninfo)
{
    const char *root=getenv("M3_RPC_RESTART_ROOT");
    character_save_journal_v2_receipt_transport_native native;
    character_save_journal_v2_protocol_report report;
    if(!root || !root[0]) return 2;
    protocol_trust_local_test_root();
    character_save_journal_v2_receipt_transport_native_init(&native,conninfo);
    return character_save_journal_v2_protocol_recover(root,world_id,
        character_save_journal_v2_receipt_transport_callback,&native.transport,
        &report)==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK ? 0 : 1;
}

static int outcome_unknown(const char *conninfo, int before_send)
{
    character_save_journal_v2_receipt receipt;
    PGconn *connection=0;
    const char *values[12];
    char epoch[]="1", revision[]="1", storage[]="1";
    if(receipt_for(&receipt)) return 2;
    connection=PQconnectdb(conninfo);
    if(!connection||PQstatus(connection)!=CONNECTION_OK) goto done;
    if(before_send) {
        if(notify_ready()) goto done;
        for(;;) pause();
    }
    values[0]=receipt.world_id;
    values[1]=(const char *)receipt.legacy_name_key;
    values[2]=receipt.character_id;
    values[3]=receipt.command_id;
    values[4]=receipt.writer_instance_id;
    values[5]=receipt.request_sha256;
    values[6]=epoch;
    values[7]=revision;
    values[8]=receipt.expected_state;
    values[9]=receipt.expected_sha256;
    values[10]=receipt.post_sha256;
    values[11]=storage;
    if(PQsetnonblocking(connection,1) ||
       !PQsendQueryParams(connection,record_sql,12,(const Oid *)record_types,values,
                          0,record_formats,0) || flush_send(connection) || notify_ready()) goto done;
    for(;;) pause();
done:
    if(connection) PQfinish(connection);
    return 2;
}

static int rpc_outcome_unknown(const char *conninfo, const char *sql)
{
    PGconn *connection=0;
    int answer=2;
    connection=PQconnectdb(conninfo);
    if(!connection||PQstatus(connection)!=CONNECTION_OK) goto done;
    if(PQsetnonblocking(connection,1) || !PQsendQuery(connection,sql) ||
       flush_send(connection) || notify_ready()) goto done;
    for(;;) pause();
done:
    if(connection) PQfinish(connection);
    return answer;
}

int main(int argc, char **argv)
{
    const char *conninfo=getenv("M3_RECEIPT_TRANSPORT_DATABASE_URL");
    const char *mode;
    if(argc!=2||!conninfo||!conninfo[0]) return 2;
    mode=argv[1];
    if(!strcmp(mode,"record-acked"))
        return receipt_callback(conninfo,CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED);
    if(!strcmp(mode,"record-deferred"))
        return receipt_callback(conninfo,CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED);
    if(!strcmp(mode,"record-rejected"))
        return receipt_callback(conninfo,CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE);
    if(!strcmp(mode,"record-before-send-wait")) return outcome_unknown(conninfo,1);
    if(!strcmp(mode,"record-after-send-wait")) return outcome_unknown(conninfo,0);
    if(!strcmp(mode,"protocol-after-send-wait")) return protocol_after_send_wait(conninfo);
    if(!strcmp(mode,"protocol-recover")) return protocol_recover(conninfo);
    if(!strcmp(mode,"acquire-a-after-send-wait")) return rpc_outcome_unknown(conninfo,
        "select writer_epoch from private.acquire_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,clock_timestamp()+interval '3 minutes')");
    if(!strcmp(mode,"renew-a-after-send-wait")) return rpc_outcome_unknown(conninfo,
        "select writer_epoch from private.renew_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,1::bigint,clock_timestamp()+interval '3 minutes')");
    if(!strcmp(mode,"seal-a-after-send-wait")) return rpc_outcome_unknown(conninfo,
        "select private.seal_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,1::bigint)");
    if(!strcmp(mode,"acquire-a")) return rpc_result(conninfo,
        "select writer_epoch from private.acquire_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,clock_timestamp()+interval '3 minutes')",1,0);
    if(!strcmp(mode,"renew-a")) return rpc_result(conninfo,
        "select writer_epoch from private.renew_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,1::bigint,clock_timestamp()+interval '3 minutes')",1,0);
    if(!strcmp(mode,"seal-a")) return rpc_result(conninfo,
        "select private.seal_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,1::bigint)",0,0);
    if(!strcmp(mode,"seal-a-p0001")) return rpc_result(conninfo,
        "select private.seal_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,1::bigint)",0,"P0001");
    if(!strcmp(mode,"renew-a-p0001")) return rpc_result(conninfo,
        "select writer_epoch from private.renew_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000001'::uuid,1::bigint,clock_timestamp()+interval '3 minutes')",0,"P0001");
    if(!strcmp(mode,"acquire-b")) return rpc_result(conninfo,
        "select writer_epoch from private.acquire_game_world_writer_epoch('m3-restart','94500000-0000-0000-0000-000000000002'::uuid,clock_timestamp()+interval '3 minutes')",2,0);
    return 2;
}
