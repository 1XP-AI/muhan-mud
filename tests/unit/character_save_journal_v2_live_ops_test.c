#include "character_save_journal_v2_live_ops.h"

#include <stdio.h>
#include <string.h>

typedef struct live_fake {
    int ok, transaction, status, kind, calls, clears, closes;
    const char *sqlstate;
    const char *epoch;
    char name[15], expiry[64];
} live_fake;

static int expect(int yes, const char *what)
{
    if(yes) return 0;
    fprintf(stderr,"character_save_journal_v2_live_ops: %s\n",what);
    return 1;
}

static int fake_ok(void *connection)
{ return ((live_fake *)connection)->ok; }
static int fake_transaction(void *connection)
{ return ((live_fake *)connection)->transaction; }
static void *fake_exec(void *connection, const char *sql, int count,
    const unsigned int *types, const char *const *values, const int *lengths,
    const int *formats, int result_format)
{
    live_fake *fake=(live_fake *)connection;
    (void)count; (void)types; (void)lengths; (void)formats;
    (void)result_format;
    fake->calls++;
    fake->kind=strstr(sql,"m3_assert_writer_session") ? 1 :
        (strstr(sql,"route_v3") ? 2 : (strstr(sql,"acquire_game") ? 3 :
        (strstr(sql,"renew_game") ? 4 : (strstr(sql,"record_legacy") ? 5 : 0))));
    if(fake->kind==2 && values && values[1]) {
        strncpy(fake->name,values[1],sizeof(fake->name)-1);
        fake->name[sizeof(fake->name)-1]=0;
    }
    if((fake->kind==3 || fake->kind==4) && values) {
        strncpy(fake->expiry,values[fake->kind==3 ? 2 : 3],
                sizeof(fake->expiry)-1);
        fake->expiry[sizeof(fake->expiry)-1]=0;
    }
    return fake;
}
static int fake_status(void *result)
{ live_fake *fake=(live_fake *)result; return fake->kind==1 ? 1 : fake->status; }
static int fake_rows(void *result)
{ (void)result; return 1; }
static int fake_columns(void *result)
{
    live_fake *fake=(live_fake *)result;
    return fake->kind==1 || fake->kind==5 ? 1 :
        (fake->kind==2 ? 10 : 2);
}
static const char *fake_value(void *result, int row, int column)
{
    live_fake *fake=(live_fake *)result;
    static const char *route[]={
        "m3-world","92000000-0000-0000-0000-000000000001","M3hero",
        "11","1","active","","existing",
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","7"
    };
    (void)row;
    if(fake->kind==1) return "t";
    if(fake->kind==2) return route[column];
    if(fake->kind==3 || fake->kind==4)
        return column ? "2026-09-03T00:01:00Z" :
            (fake->epoch ? fake->epoch : "7");
    return "";
}
static int fake_length(void *result, int row, int column)
{ return (int)strlen(fake_value(result,row,column)); }
static const char *fake_sqlstate(void *result)
{
    live_fake *fake=(live_fake *)result;
    return fake->kind==1 ? 0 : fake->sqlstate;
}
static void fake_clear(void *result)
{ ((live_fake *)result)->clears++; }
static void fake_close(void *connection)
{ ((live_fake *)connection)->closes++; }
static const character_save_journal_v2_rpc_transport_operations fake_ops={
    fake_ok,fake_transaction,fake_exec,fake_status,fake_rows,fake_columns,
    fake_value,fake_length,fake_sqlstate,fake_clear,fake_close
};

static void ready(character_save_journal_v2_rpc_transport *transport,
                  live_fake *fake)
{
    memset(fake,0,sizeof(*fake));
    fake->ok=1;
    fake->transaction=1;
    fake->status=1;
    character_save_journal_v2_rpc_transport_init(transport);
    (void)character_save_journal_v2_rpc_transport_start(transport,&fake_ops,0,
                                                          fake);
}

static void tuple(character_save_journal_v2_writer_tuple *value)
{
    memset(value,0,sizeof(*value));
    strcpy(value->world_id,"m3-world");
    strcpy(value->writer_instance_id,"94000000-0000-0000-0000-000000000001");
}

static void receipt(character_save_journal_v2_receipt *value)
{
    static const char hash[]=
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    memset(value,0,sizeof(*value));
    value->world_id="m3-world";
    value->legacy_name_key=(const unsigned char *)"M3hero";
    value->legacy_name_key_length=6;
    value->character_id="92000000-0000-0000-0000-000000000001";
    value->command_id="93000000-0000-0000-0000-000000000001";
    value->writer_instance_id="94000000-0000-0000-0000-000000000001";
    value->request_sha256=hash;
    value->writer_epoch=7;
    value->writer_revision=1;
    value->expected_state="absent";
    value->post_sha256=hash;
    value->storage_format=1;
}

static int test_acquire_and_renew(void)
{
    character_save_journal_v2_rpc_transport transport;
    character_save_journal_v2_live_ops ops;
    character_save_journal_v2_writer_tuple request, granted, before;
    live_fake fake;
    int bad=0;

    ready(&transport,&fake);
    character_save_journal_v2_live_ops_init(&ops,&transport,"2026-09-03T00:00:00Z");
    tuple(&request);
    memset(&granted,0xa5,sizeof(granted));
    bad|=expect(!character_save_journal_v2_live_ops_writer_epoch_acquire(
        &ops,&request,&granted) && !strcmp(granted.world_id,request.world_id) &&
        !strcmp(granted.writer_instance_id,request.writer_instance_id) &&
        granted.writer_epoch==7 && !strcmp(fake.expiry,"2026-09-03T00:00:00Z"),
        "acquire composes a positive echoed writer tuple with caller expiry");
    before=granted;
    fake.status=2;
    fake.sqlstate="57014";
    bad|=expect(character_save_journal_v2_live_ops_writer_epoch_acquire(
        &ops,&request,&granted) && !memcmp(&granted,&before,sizeof(granted)),
        "failed acquire preserves its output byte-for-byte");
    fake.status=1;
    fake.sqlstate=0;
    request.writer_epoch=7;
    bad|=expect(character_save_journal_v2_live_ops_writer_epoch_renew(
        &ops,&request,"2026-09-03T00:02:00Z")==
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK &&
        !strcmp(fake.expiry,"2026-09-03T00:02:00Z"),
        "explicit renewal forwards the caller-provided expiry");
    character_save_journal_v2_rpc_transport_close(&transport);
    return bad;
}

static int test_route_bytes_and_output_contract(void)
{
    character_save_journal_v2_rpc_transport transport;
    character_save_journal_v2_live_ops ops;
    character_save_journal_v2_route_reply_v3 reply, before;
    live_fake fake;
    unsigned char embedded_nul[]={'M','3',0,'h'};
    int calls, bad=0;

    ready(&transport,&fake);
    character_save_journal_v2_live_ops_init(&ops,&transport,"2026-09-03T00:00:00Z");
    memset(&reply,0xa5,sizeof(reply));
    bad|=expect(character_save_journal_v2_live_ops_route_lookup_v3(&ops,
        "m3-world",(const unsigned char *)"M3hero",6,&reply)==
        CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK && reply.row_count==1 &&
        reply.status==CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK &&
        reply.head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING &&
        reply.head_revision==7 && !strcmp(fake.name,"M3hero"),
        "route v3 converts bounded canonical bytes and fills a validated reply");
    before=reply;
    calls=fake.calls;
    bad|=expect(character_save_journal_v2_live_ops_route_lookup_v3(&ops,
        "m3-world",embedded_nul,sizeof(embedded_nul),&reply)==
        CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE && fake.calls==calls &&
        !memcmp(&reply,&before,sizeof(reply)),
        "embedded NUL is rejected before I/O without output mutation");
    fake.status=2;
    fake.sqlstate="P0001";
    bad|=expect(character_save_journal_v2_live_ops_route_lookup_v3(&ops,
        "m3-world",(const unsigned char *)"M3hero",6,&reply)==
        CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE &&
        !memcmp(&reply,&before,sizeof(reply)),
        "rejected transport route conservatively fails without output mutation");
    character_save_journal_v2_rpc_transport_close(&transport);
    return bad;
}

static int test_receipt_mapping(void)
{
    character_save_journal_v2_rpc_transport transport;
    character_save_journal_v2_live_ops ops;
    character_save_journal_v2_receipt value;
    live_fake fake;
    int bad=0;

    ready(&transport,&fake);
    character_save_journal_v2_live_ops_init(&ops,&transport,"2026-09-03T00:00:00Z");
    receipt(&value);
    bad|=expect(character_save_journal_v2_live_ops_receipt_callback(&ops,&value)==
        CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED,"receipt success maps acknowledged");
    fake.status=2;
    fake.sqlstate="57014";
    bad|=expect(character_save_journal_v2_live_ops_receipt_callback(&ops,&value)==
        CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED,"receipt deferred maps deferred");
    fake.sqlstate="22023";
    bad|=expect(character_save_journal_v2_live_ops_receipt_callback(&ops,&value)==
        CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE,"invalid maps exact freeze");
    fake.sqlstate="P0001";
    bad|=expect(character_save_journal_v2_live_ops_receipt_callback(&ops,&value)==
        CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE,"rejected maps exact freeze");
    fake.sqlstate="08006";
    bad|=expect(character_save_journal_v2_live_ops_receipt_callback(&ops,&value)==
        CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED && fake.closes==1,
        "unavailable receipt maps deferred and closes the transport");
    character_save_journal_v2_rpc_transport_close(&transport);
    return bad;
}

int main(void)
{ return test_acquire_and_renew()|test_route_bytes_and_output_contract()|test_receipt_mapping(); }
