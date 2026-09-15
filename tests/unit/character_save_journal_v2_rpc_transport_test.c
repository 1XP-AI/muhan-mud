#include "character_save_journal_v2_rpc_transport.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

typedef struct fake {
    int ok, transaction, execs, clears, closes, status, kind, rows;
    int clear_order, close_order, sequence, assert_null;
    int seed_fixed, seed_calls;
    const char *sqlstate, *legacy_shard, *route_world, *route_name;
    const char *route_storage, *route_lifecycle, *epoch_value;
    const char *v3_head_state, *v3_head_sha256, *v3_head_revision;
} fake;

static int expect(int yes, const char *what)
{ if(yes) return 0; fprintf(stderr,"rpc_transport: %s\n",what); return 1; }
static int f_ok(void *connection) { return ((fake *)connection)->ok; }
static int f_transaction(void *connection) { return ((fake *)connection)->transaction==1; }
static void *f_exec(void *connection, const char *sql, int count,
                    const unsigned int *types, const char *const *values,
                    const int *lengths, const int *formats, int result_format)
{
    fake *f=(fake *)connection;
    (void)types; (void)lengths; (void)formats;
    (void)result_format;
    f->execs++;
    f->kind=strstr(sql,"m3_assert_writer_session") ? 1 :
        (strstr(sql,"route_v3") ? 7 : (strstr(sql,"resolve_game") ? 2 : (strstr(sql,"acquire_game") ? 3 :
        (strstr(sql,"renew_game") ? 4 : (strstr(sql,"seal_game") ? 5 :
        (strstr(sql,"seed_game_character_absent_head") ? 8 : 6))))));
    if(f->kind==8) {
        f->seed_calls++;
        f->seed_fixed=count==6 &&
            !strcmp(sql,"select private.seed_game_character_absent_head($1::text,$2::text,$3::uuid,$4::uuid,$5::bigint,$6::smallint)") &&
            values && !strcmp(values[0],"m3-world") && !strcmp(values[1],"M3hero") &&
            !strcmp(values[2],"92000000-0000-0000-0000-000000000001") &&
            !strcmp(values[3],"94000000-0000-0000-0000-000000000001") &&
            !strcmp(values[4],"1") && !strcmp(values[5],"1");
    }
    if(f->kind==1&&f->assert_null) return 0;
    return f;
}
static int f_status(void *result)
{ fake *f=(fake *)result; return f->kind==1 ? 1 : (f->status ? f->status : 1); }
static int f_rows(void *result) { fake *f=(fake *)result; return f->kind==1 ? 1 : f->rows; }
static int f_columns(void *result)
{ fake *f=(fake *)result; return f->kind==2 ? 7 : (f->kind==7 ? 10 : ((f->kind==3||f->kind==4) ? 2 : 1)); }
static const char *f_value(void *result, int row, int column)
{
    fake *f=(fake *)result;
    static const char *route[]={"m3-world","92000000-0000-0000-0000-000000000001","M3hero","11","1","imported_unclaimed",""};
    static const char *route_v3[]={"m3-world","92000000-0000-0000-0000-000000000001","M3hero","11","1","imported_unclaimed","","absent","","0"};
    (void)row;
    if(f->kind==1) return "t";
    if(f->kind==2) {
        if(column==0&&f->route_world) return f->route_world;
        if(column==2&&f->route_name) return f->route_name;
        if(column==4&&f->route_storage) return f->route_storage;
        if(column==5&&f->route_lifecycle) return f->route_lifecycle;
        return column==3&&f->legacy_shard ? f->legacy_shard : route[column];
    }
    if(f->kind==7) {
        if(column==0&&f->route_world) return f->route_world;
        if(column==2&&f->route_name) return f->route_name;
        if(column==4&&f->route_storage) return f->route_storage;
        if(column==5&&f->route_lifecycle) return f->route_lifecycle;
        if(column==7&&f->v3_head_state) return f->v3_head_state;
        if(column==8&&f->v3_head_sha256) return f->v3_head_sha256;
        if(column==9&&f->v3_head_revision) return f->v3_head_revision;
        return column==3&&f->legacy_shard ? f->legacy_shard : route_v3[column];
    }
    if(f->kind==3||f->kind==4) return column ? "2026-09-02T00:00:00Z" :
        (f->epoch_value ? f->epoch_value : "1");
    return "";
}
static int f_length(void *result, int row, int column)
{ const char *value=f_value(result,row,column); return value ? (int)strlen(value) : -1; }
static const char *f_sqlstate(void *result) { fake *f=(fake *)result; return f->kind==1 ? 0 : f->sqlstate; }
static void f_clear(void *result)
{ fake *f=(fake *)result; f->clears++; f->clear_order=++f->sequence; }
static void f_close(void *connection)
{ fake *f=(fake *)connection; f->closes++; f->close_order=++f->sequence; }
static const character_save_journal_v2_rpc_transport_operations ops={
    f_ok,f_transaction,f_exec,f_status,f_rows,f_columns,f_value,f_length,
    f_sqlstate,f_clear,f_close
};

static void ready(fake *f)
{ memset(f,0,sizeof(*f)); f->ok=1; f->transaction=1; f->rows=1; }
static void init_ready(character_save_journal_v2_rpc_transport *t, fake *f)
{ character_save_journal_v2_rpc_transport_init(t); (void)character_save_journal_v2_rpc_transport_start(t,&ops,0,f); }
static void receipt(character_save_journal_v2_receipt *r)
{
    static const char hash[]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    memset(r,0,sizeof(*r)); r->world_id="m3-world";
    r->legacy_name_key=(const unsigned char *)"M3hero"; r->legacy_name_key_length=6;
    r->character_id="92000000-0000-0000-0000-000000000001";
    r->command_id="93000000-0000-0000-0000-000000000001";
    r->writer_instance_id="94000000-0000-0000-0000-000000000001";
    r->request_sha256=hash; r->writer_epoch=1; r->writer_revision=1;
    r->expected_state="absent"; r->post_sha256=hash; r->storage_format=1;
}

static int test_lifecycle(void)
{
    fake kept, incoming, reopened; character_save_journal_v2_rpc_transport t; int bad=0;
    ready(&kept); ready(&incoming); ready(&reopened); character_save_journal_v2_rpc_transport_init(&t);
    bad|=expect(character_save_journal_v2_rpc_transport_start(&t,&ops,0,&kept)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,"initial start");
    bad|=expect(character_save_journal_v2_rpc_transport_start(&t,&ops,0,&kept)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE,"same ready connection is rejected without transfer");
    bad|=expect(kept.closes==0&&character_save_journal_v2_rpc_transport_get_state(&t)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY,"same ready connection remains owned and open");
    bad|=expect(character_save_journal_v2_rpc_transport_start(&t,&ops,0,&incoming)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE,"repeated start rejected");
    bad|=expect(kept.closes==0&&incoming.closes==1&&character_save_journal_v2_rpc_transport_get_state(&t)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY,"repeated start retains ready connection and owns incoming");
    character_save_journal_v2_rpc_transport_close(&t);
    character_save_journal_v2_rpc_transport_close(&t);
    bad|=expect(kept.closes==1,"close is idempotent");
    bad|=expect(character_save_journal_v2_rpc_transport_start(&t,&ops,0,&reopened)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,"closed transport can start a new owned session");
    character_save_journal_v2_rpc_transport_close(&t);
    return bad|expect(reopened.closes==1,"reopened session closes exactly once");
}

static int test_null_session_assertion_fails_closed(void)
{
    fake startup, call; character_save_journal_v2_rpc_transport t;
    character_save_journal_v2_rpc_route route; int before, bad=0;
    ready(&startup); startup.assert_null=1;
    character_save_journal_v2_rpc_transport_init(&t);
    bad|=expect(character_save_journal_v2_rpc_transport_start(&t,&ops,0,&startup)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE,"NULL startup assertion is unavailable");
    bad|=expect(startup.closes==1&&!t.connection&&character_save_journal_v2_rpc_transport_get_state(&t)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE,"NULL startup assertion closes once");
    before=startup.execs;
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE&&startup.execs==before,"failed startup performs no later I/O");
    character_save_journal_v2_rpc_transport_close(&t);
    bad|=expect(startup.closes==1,"failed startup cannot double close");
    ready(&call); init_ready(&t,&call); call.assert_null=1;
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE,"NULL per-call assertion is unavailable");
    bad|=expect(call.closes==1&&!t.connection&&character_save_journal_v2_rpc_transport_get_state(&t)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE_STATE,"NULL per-call assertion closes once");
    before=call.execs;
    return bad|expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE&&call.execs==before,"NULL assertion failure performs no later I/O");
}

static int test_transaction_and_error_order(void)
{
    fake f; character_save_journal_v2_rpc_transport t; int bad=0, before, transaction_outcome;
    ready(&f); init_ready(&t,&f); f.transaction=2; before=f.execs;
    transaction_outcome=character_save_journal_v2_rpc_transport_seal(&t,"m3-world","94000000-0000-0000-0000-000000000001",1);
    bad|=expect(transaction_outcome==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE,"active transaction fails closed");
    bad|=expect(f.execs==before&&f.closes==1,"transaction failure executes no RPC and closes once");
    ready(&f); init_ready(&t,&f); f.status=2; f.sqlstate="08006"; f.sequence=0; f.clears=0; f.closes=0;
    bad|=expect(character_save_journal_v2_rpc_transport_seal(&t,"m3-world","94000000-0000-0000-0000-000000000001",1)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE,"class 08 unavailable");
    bad|=expect(f.clears==2&&f.closes==1&&f.clear_order<f.close_order,"clear happens before unavailable close");
    before=f.execs;
    bad|=expect(character_save_journal_v2_rpc_transport_seal(&t,"m3-world","94000000-0000-0000-0000-000000000001",1)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_UNAVAILABLE&&f.execs==before,"unavailable never retries");
    return bad;
}

static int test_total_mapping_and_outputs(void)
{
    fake f; character_save_journal_v2_rpc_transport t; character_save_journal_v2_rpc_route route; character_save_journal_v2_rpc_route_v3 route_v3; character_save_journal_v2_receipt r; unsigned long long epoch=77; char expires[64]="stale"; unsigned char nul_name[]={'M','3',0,'x'}; int bad=0,before;
    ready(&f); init_ready(&t,&f); f.status=2; f.sqlstate="22001";
    memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID,"class 22 maps invalid");
    character_save_journal_v2_rpc_transport_close(&t);
    ready(&f); init_ready(&t,&f); f.status=2; f.sqlstate="P0001";
    bad|=expect(character_save_journal_v2_rpc_transport_seal(&t,"m3-world","94000000-0000-0000-0000-000000000001",1)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED,"P0001 maps rejected");
    character_save_journal_v2_rpc_transport_close(&t);
    ready(&f); init_ready(&t,&f); f.status=2; f.sqlstate="57014";
    bad|=expect(character_save_journal_v2_rpc_transport_seal(&t,"m3-world","94000000-0000-0000-0000-000000000001",1)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED,"other SQLSTATE maps deferred");
    character_save_journal_v2_rpc_transport_close(&t);
    ready(&f); init_ready(&t,&f); f.rows=2;
    memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"malformed result defers without stale route output");
    f.rows=1; f.legacy_shard="A1"; memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"uppercase legacy shard defers and clears route");
    f.legacy_shard="1"; memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"short legacy shard defers and clears route");
    f.legacy_shard=0; f.route_world="other-world"; memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"mismatched response world defers and clears route");
    f.route_world=0; f.route_name="M3other"; memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"mismatched response name defers and clears route");
    f.route_name=0; f.route_storage="2"; memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"unsupported response storage format defers and clears route");
    f.route_storage=0; f.route_lifecycle="suspended"; memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route.world_id[0]==0,"ineligible response lifecycle defers and clears route");
    f.route_lifecycle=0; f.epoch_value="1"; strcpy(expires,"stale");
    bad|=expect(character_save_journal_v2_rpc_transport_renew(&t,"m3-world","94000000-0000-0000-0000-000000000001",2,"2026-09-02T00:00:00Z",expires)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&expires[0]==0,"renew response epoch mismatch defers and clears expiry");
    memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"Bad_world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID&&route.world_id[0]==0,"route output is cleared before invalid input");
    bad|=expect(character_save_journal_v2_rpc_transport_acquire(&t,"m3-world","bad","x",&epoch,expires)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID&&epoch==0&&expires[0]==0,"acquire clears outputs and rejects malformed UUID");
    memset(&route,0x5a,sizeof(route));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world-overflow-beyond-the-64-character-bound-xxxxxxxxxxxxxxxxxxxxxxxx","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID&&route.world_id[0]==0,"overlong world is rejected without stale route output");
    strcpy(expires,"stale");
    bad|=expect(character_save_journal_v2_rpc_transport_renew(&t,"m3-world","94000000-0000-0000-0000-000000000001",(unsigned long long)LLONG_MAX+1ULL,"2026-09-02T00:00:00Z",expires)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID&&expires[0]==0,"numeric overflow clears renew output");
    receipt(&r); r.legacy_name_key=nul_name; r.legacy_name_key_length=sizeof(nul_name);
    bad|=expect(character_save_journal_v2_rpc_transport_receipt(&t,&r)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID,"embedded NUL receipt name is rejected");
    memset(&route_v3,0x5a,sizeof(route_v3)); f.rows=2;
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route_v3.world_id[0]==0,"malformed v3 row count defers and clears output");
    f.rows=1; f.v3_head_state="existing"; f.v3_head_sha256=""; memset(&route_v3,0x5a,sizeof(route_v3));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route_v3.world_id[0]==0,"v3 existing head requires a hash");
    f.v3_head_state="absent"; f.v3_head_sha256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; memset(&route_v3,0x5a,sizeof(route_v3));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route_v3.world_id[0]==0,"v3 absent head rejects a hash");
    f.v3_head_sha256=""; f.v3_head_revision="-1"; memset(&route_v3,0x5a,sizeof(route_v3));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route_v3.world_id[0]==0,"v3 negative revision defers and clears output");
    f.v3_head_revision="0"; f.v3_head_state="wrong"; memset(&route_v3,0x5a,sizeof(route_v3));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route_v3.world_id[0]==0,"v3 unknown head state defers and clears output");
    f.v3_head_state="uninitialized"; f.v3_head_revision="1"; memset(&route_v3,0x5a,sizeof(route_v3));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED&&route_v3.world_id[0]==0,"v3 uninitialized head requires revision zero");
    f.v3_head_state=0; f.v3_head_revision=0; memset(&route_v3,0x5a,sizeof(route_v3));
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"Bad_world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID&&route_v3.world_id[0]==0,"v3 output is cleared before invalid input");
    before=f.execs;
    bad|=expect(character_save_journal_v2_rpc_transport_seed_absent_head(&t,
        "m3-world","M3hero","bad","94000000-0000-0000-0000-000000000001",1,1)==
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID&&f.execs==before,
        "seed rejects malformed exact tuple before RPC");
    return bad;
}

static int test_all_success_and_receipt_validation(void)
{
    fake f; character_save_journal_v2_rpc_transport t; character_save_journal_v2_rpc_route route; character_save_journal_v2_rpc_route_v3 route_v3; character_save_journal_v2_receipt r; unsigned long long epoch=0; char expires[64]; int bad=0;
    ready(&f); init_ready(&t,&f);
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route(&t,"m3-world","M3hero",&route)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,"route success");
    f.v3_head_state="existing";
    f.v3_head_sha256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    f.v3_head_revision="2";
    bad|=expect(character_save_journal_v2_rpc_transport_lookup_route_v3(&t,"m3-world","M3hero",&route_v3)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK&&route_v3.head_revision==2&&!strcmp(route_v3.head_state,"existing")&&!strcmp(route_v3.head_sha256,f.v3_head_sha256),"v3 route returns a validated effective existing head");
    bad|=expect(character_save_journal_v2_rpc_transport_acquire(&t,"m3-world","94000000-0000-0000-0000-000000000001","2026-09-02T00:00:00Z",&epoch,expires)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK&&epoch==1,"acquire success");
    bad|=expect(character_save_journal_v2_rpc_transport_renew(&t,"m3-world","94000000-0000-0000-0000-000000000001",epoch,"2026-09-02T00:00:00Z",expires)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,"renew success");
    bad|=expect(character_save_journal_v2_rpc_transport_seal(&t,"m3-world","94000000-0000-0000-0000-000000000001",epoch)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,"seal success");
    bad|=expect(character_save_journal_v2_rpc_transport_seed_absent_head(&t,
        "m3-world","M3hero","92000000-0000-0000-0000-000000000001",
        "94000000-0000-0000-0000-000000000001",epoch,1)==
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK&&f.seed_calls==1&&f.seed_fixed,
        "seed uses only the fixed parameterized M7a RPC");
    receipt(&r);
    bad|=expect(character_save_journal_v2_rpc_transport_receipt(&t,&r)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK,"receipt absent accepts null expected hash");
    r.expected_state="existing";
    bad|=expect(character_save_journal_v2_rpc_transport_receipt(&t,&r)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID,"existing requires expected hash");
    r.expected_sha256="AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";
    bad|=expect(character_save_journal_v2_rpc_transport_receipt(&t,&r)==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID,"uppercase expected hash rejected");
    return bad;
}

int main(void)
{ return test_lifecycle()|test_null_session_assertion_fails_closed()|test_transaction_and_error_order()|test_total_mapping_and_outputs()|test_all_success_and_receipt_validation(); }
