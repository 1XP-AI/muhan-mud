#include "character_save_journal_v2_player_store.h"
#include "mstruct.h"
#include "player_recovery.h"
#include "player_store.h"
#include "character_save_journal_v2.h"

#include <stdio.h>
#include <string.h>
#include <unistd.h>

typedef enum test_mode { TEST_NORMAL, TEST_SERIALIZER_FAILURE, TEST_ROUTE_DRIFT,
    TEST_DEFERRED, TEST_INVALID_FREEZE, TEST_REJECTED_FREEZE,
    TEST_LOCAL_INCOMPLETE, TEST_REENTRANT, TEST_SEED_FAILURE,
    TEST_SEED_REBIND_DRIFT, TEST_SEED_SUCCESS, TEST_V4_FOUND,
    TEST_V4_NO_CANDIDATE, TEST_V4_RESOLVER_ERROR, TEST_V4_REJECTED,
    TEST_V4_INTER_BIND_MISMATCH } test_mode;

typedef struct fixture {
    character_save_journal_v2_player_store store;
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_live_ops live_ops;
    character_save_journal_v2_rpc_transport transport;
    player_record_serializer_limits limits;
    char buffer[256];
    creature player;
    int validate_calls,deadline_calls,renew_calls,uuid_calls,serializer_calls;
    int bootstrap_calls,seed_calls;
    int protocol_calls,load_calls,observer_calls,observer_result;
    int resolver_calls,stage_route_calls,prepared_calls,publish_calls,receipt_calls;
    int serializer_failure,renew_failure,nested_result,revision;
    int recovery_forwarding_required,recovery_forwarding_calls,recovery_forwarding_bad;
    creature *serialized_player;
    int copy_calls,copy_failure,copy_wire_bad;
    test_mode mode;
    char loaded_name[16];
} fixture;

static fixture *current;
static int recovery_free_calls;
static creature *recovery_last_freed;
static int expect(int value,const char *what)
{ if(value)return 0; fprintf(stderr,"character_save_journal_v2_player_store: %s\n",what); return 1; }
static void tuple(character_save_journal_v2_writer_tuple *out)
{ memset(out,0,sizeof(*out)); strcpy(out->world_id,"m3-world"); strcpy(out->writer_instance_id,"10000000-0000-4000-8000-000000000001"); out->writer_epoch=7; }

character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_dup_held_root_fd(
    const character_save_journal_v2_writer_context *writer,int *fd)
{ if(!current||writer!=&current->writer)return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID;*fd=dup(STDERR_FILENO);return *fd<0?CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID:CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK; }

int character_save_journal_v2_request_sha256(
    const character_save_journal_v2_wire *wire,char out[65])
{ (void)wire;memset(out,'b',64);out[64]=0;return 0; }

int character_save_journal_v2_copy_existing_at(int fd,
    const character_save_journal_v2_wire *wire,unsigned char *buffer,
    size_t capacity,size_t *length)
{
    static const char original[]="original-record-with-password";
    current->copy_calls++;
    if(fd<0||capacity<sizeof(original)||wire->state!=CHARACTER_SAVE_JOURNAL_V2_PREPARED||
       wire->expected_state!=CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING||
       wire->writer_epoch!=7||wire->writer_revision!=10||wire->storage_format!=1||
       strcmp(wire->world_id,"m3-world")||
       strcmp(wire->writer_instance_id,"10000000-0000-4000-8000-000000000001")||
       strcmp(wire->character_id,"30000000-0000-4000-8000-000000000002")||
       strcmp(wire->command_uuid,"20000000-0000-4000-8000-000000000002")||
       strcmp(wire->legacy_name_key_hex,"4d336865726f")||strcmp(wire->legacy_shard,"66")||
       strlen(wire->expected_sha256)!=64||wire->expected_sha256[0]!='a'||
       strcmp(wire->expected_sha256,wire->post_sha256)||wire->request_sha256[0]!='b')
        current->copy_wire_bad=1;
    memcpy(buffer,original,sizeof(original));*length=sizeof(original);
    return current->copy_failure?-1:0;
}

int character_save_journal_v2_bootstrap_absent_head(const character_save_journal_v2_writer_context *writer,character_save_journal_v2_live_ops *ops,const unsigned char *name,size_t length)
{ (void)writer;(void)ops;(void)name;(void)length;return -1; }

/* Recovery composition uses the legacy facade, so provide only inert test
 * fallbacks for its untouched FileStore and ownership dependencies. */
int file_player_store_save(char *name, creature *player)
{ (void)name;(void)player;return PLAYER_STORE_IO_ERROR; }
int file_player_store_load(char *name, creature **player)
{ (void)name;if(player)*player=0;return PLAYER_STORE_NOT_FOUND; }
void free_crt(creature *player)
{ recovery_free_calls++;recovery_last_freed=player; }
void log_f()
{ }

character_save_journal_v2_writer_context_status character_save_journal_v2_writer_validate_held(const character_save_journal_v2_writer_context *writer,character_save_journal_v2_writer_tuple *out)
{ if(!current||writer!=&current->writer||!out)return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_INVALID; current->validate_calls++; tuple(out); return CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK; }
character_save_journal_v2_rpc_transport_state character_save_journal_v2_rpc_transport_get_state(const character_save_journal_v2_rpc_transport *transport)
{ return transport?transport->state:CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_CLOSED; }
character_save_journal_v2_rpc_transport_outcome character_save_journal_v2_live_ops_writer_epoch_renew(void *opaque,const character_save_journal_v2_writer_tuple *held,const char *deadline)
{ character_save_journal_v2_live_ops *ops=(character_save_journal_v2_live_ops *)opaque; if(!current||ops!=&current->live_ops||!held||!deadline||strcmp(held->world_id,"m3-world")||held->writer_epoch!=7)return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID; current->renew_calls++; if(strcmp(deadline,"2026-09-03T00:02:00Z"))return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID; return current->renew_failure?CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_DEFERRED:CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK; }
character_save_journal_v2_route_lookup_result character_save_journal_v2_live_ops_route_lookup_v3(void *opaque,const char *world,const unsigned char *name,size_t length,character_save_journal_v2_route_reply_v3 *reply)
{ fixture *test=current;(void)world;(void)name;(void)length;if(!test||opaque!=&test->live_ops||!reply)return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;test->stage_route_calls++;memset(reply,0,sizeof(*reply));reply->status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;reply->row_count=1;strcpy(reply->world_id,"m3-world");strcpy(reply->character_id,test->mode==TEST_V4_INTER_BIND_MISMATCH?"30000000-0000-4000-8000-000000000003":"30000000-0000-4000-8000-000000000002");memcpy(reply->legacy_name,"M3hero",6);reply->legacy_name_length=6;strcpy(reply->legacy_shard,"66");reply->storage_format=1;reply->lifecycle=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;reply->head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT;return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK; }
character_save_journal_v2_receipt_result character_save_journal_v2_live_ops_receipt_callback(void *opaque,const character_save_journal_v2_receipt *receipt)
{ (void)opaque;(void)receipt;return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED; }
int player_record_serialize_bounded(creature *player,char perm_only,char *buffer,unsigned long capacity,unsigned long *written,const player_record_serializer_limits *limits)
{ static const char bytes[]="bounded-record"; if(!current||player!=&current->player||perm_only||!buffer||!written||!limits||capacity<sizeof(bytes))return PLAYER_RECORD_SERIALIZER_INVALID; current->serialized_player=player;current->serializer_calls++; if(current->serializer_failure){*written=0;return PLAYER_RECORD_SERIALIZER_NO_SPACE;} memcpy(buffer,bytes,sizeof(bytes));*written=sizeof(bytes);return PLAYER_RECORD_SERIALIZER_OK; }

character_save_journal_v2_protocol_result character_save_journal_v2_protocol_save_held_v3(const character_save_journal_v2_writer_context *writer,const character_save_journal_v2_protocol_held_request_v3 *request,const character_save_journal_v2_protocol_operations_v3 *operations,character_save_journal_v2_protocol_report *report)
{ const unsigned char *bytes=0;size_t length=0;int serialized; if(!current||writer!=&current->writer||!request||!operations||!report||current->renew_calls!=current->protocol_calls+1||current->uuid_calls!=current->protocol_calls+1||strcmp(request->command_uuid,"20000000-0000-4000-8000-000000000002")||operations->route_lookup!=character_save_journal_v2_live_ops_route_lookup_v3||operations->route_opaque!=&current->live_ops||operations->receipt!=character_save_journal_v2_live_ops_receipt_callback||operations->receipt_opaque!=&current->live_ops)return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;if(current->recovery_forwarding_required){current->recovery_forwarding_calls++;if(request->canonical_legacy_name!=(const unsigned char *)current->player.name||request->canonical_legacy_name_length!=strlen(current->player.name)||memcmp(request->canonical_legacy_name,current->player.name,request->canonical_legacy_name_length))current->recovery_forwarding_bad=1;}current->protocol_calls++;memset(report,0,sizeof(*report));if(current->mode==TEST_ROUTE_DRIFT){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED;serialized=operations->serialize(operations->serialize_opaque,0,0,request->command_uuid,&bytes,&length);return serialized?CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER:CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH;}if(current->mode==TEST_REENTRANT){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;current->nested_result=character_save_journal_v2_player_store_save(operations->serialize_opaque,(char *)"M3hero",&current->player);if(report->reached!=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH)return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;}serialized=operations->serialize(operations->serialize_opaque,0,0,request->command_uuid,&bytes,&length);if(serialized||!bytes||!length){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER;}if(operations->observe_prepared_stage){report->snapshot_attempted=1;report->snapshot_result=operations->observe_prepared_stage(operations->observe_prepared_stage_opaque,writer,request->command_uuid);}if(current->mode==TEST_DEFERRED){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;report->ack_result=CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED;}if(current->mode==TEST_INVALID_FREEZE){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;report->ack_result=CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_FROZEN;}if(current->mode==TEST_REJECTED_FREEZE){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;report->ack_result=CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_FROZEN;}if(current->mode==TEST_LOCAL_INCOMPLETE){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;report->ack_result=CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK;}report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED;current->revision++;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK; }

static int resolve_candidate(void *opaque,
    const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route_v3 *route,
    const unsigned char *name, size_t name_length,
    character_save_journal_v2_protocol_candidate_v4 *candidate)
{ fixture *test=(fixture *)opaque;if(!test||test!=current||!writer||!route||!name||name_length!=6||memcmp(name,"M3hero",6)||!candidate)return -1;test->resolver_calls++;if(test->mode==TEST_V4_RESOLVER_ERROR)return -7;if(test->mode==TEST_V4_NO_CANDIDATE)return 0;memset(candidate,0,sizeof(*candidate));strcpy(candidate->command_uuid,"20000000-0000-4000-8000-000000000002");candidate->writer=*writer;strcpy(candidate->character_id,test->mode==TEST_V4_REJECTED?"30000000-0000-4000-8000-000000000003":"30000000-0000-4000-8000-000000000002");memcpy(candidate->canonical_legacy_name,name,name_length);candidate->canonical_legacy_name_length=name_length;return 1; }

character_save_journal_v2_protocol_result character_save_journal_v2_protocol_save_held_v4(const character_save_journal_v2_writer_context *writer,const character_save_journal_v2_protocol_held_request_v3 *request,const character_save_journal_v2_protocol_operations_v4 *operations,character_save_journal_v2_protocol_report *report)
{ character_save_journal_v2_writer_tuple held;character_save_journal_v2_bound_route_v3 route;character_save_journal_v2_protocol_candidate_v4 candidate;character_save_journal_v2_route_reply_v3 stage;const unsigned char *bytes=0;size_t length=0;char command_uuid[37];int found,serialized;if(!current||writer!=&current->writer||!request||!operations||!report||operations->route_lookup!=character_save_journal_v2_live_ops_route_lookup_v3||operations->route_opaque!=&current->live_ops||operations->resolve_candidate!=resolve_candidate||operations->resolve_candidate_opaque!=current)return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;current->protocol_calls++;memset(report,0,sizeof(*report));memset(&held,0,sizeof(held));tuple(&held);memset(&route,0,sizeof(route));strcpy(route.character_id,"30000000-0000-4000-8000-000000000002");strcpy(route.world_id,"m3-world");memcpy(route.legacy_name,"M3hero",6);route.legacy_name_length=6;strcpy(route.legacy_shard,"66");route.storage_format=1;route.head_state=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING;route.head_revision=9;memset(route.head_sha256,'a',64);memset(&candidate,0,sizeof(candidate));memset(command_uuid,0,sizeof(command_uuid));found=operations->resolve_candidate(operations->resolve_candidate_opaque,&held,&route,request->canonical_legacy_name,request->canonical_legacy_name_length,&candidate);if(found<0){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;}if(found==1&&strcmp(candidate.character_id,route.character_id)){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;}if(found==0&&operations->generate_uuid(operations->generate_uuid_opaque,command_uuid)){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;}if(operations->route_lookup(operations->route_opaque,held.world_id,request->canonical_legacy_name,request->canonical_legacy_name_length,&stage)!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK||strcmp(stage.character_id,route.character_id)){report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;}serialized=operations->serialize(operations->serialize_opaque,&held,&route,found==1?candidate.command_uuid:command_uuid,&bytes,&length);if(serialized||!bytes||!length)return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER;if(current->copy_calls&&(length!=sizeof("original-record-with-password")||memcmp(bytes,"original-record-with-password",length)))current->copy_wire_bad=1;current->prepared_calls++;current->publish_calls++;current->receipt_calls++;report->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED;return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK; }

static int deadline(void *opaque,char output[64])
{ fixture *test=(fixture *)opaque;test->deadline_calls++;strcpy(output,"2026-09-03T00:02:00Z");return 0; }
static int command_uuid(void *opaque,char output[37])
{ fixture *test=(fixture *)opaque;test->uuid_calls++;strcpy(output,"20000000-0000-4000-8000-000000000002");return 0; }
static int absent_bootstrap(void *opaque,const character_save_journal_v2_writer_context *writer,character_save_journal_v2_live_ops *ops,const unsigned char *name,size_t length)
{ fixture *test=(fixture *)opaque;if(test!=current||writer!=&test->writer||ops!=&test->live_ops||length!=6||memcmp(name,"M3hero",6)||test->renew_calls!=test->bootstrap_calls+1||test->uuid_calls!=test->protocol_calls||test->serializer_calls!=test->protocol_calls)return -1;test->bootstrap_calls++;if(test->mode==TEST_SEED_FAILURE||test->mode==TEST_SEED_REBIND_DRIFT||test->mode==TEST_SEED_SUCCESS)test->seed_calls++;return test->mode==TEST_SEED_FAILURE||test->mode==TEST_SEED_REBIND_DRIFT?-1:0; }
static int delegated_load(void *opaque,char *name,creature **player)
{ fixture *test=(fixture *)opaque;test->load_calls++;strcpy(test->loaded_name,name);*player=&test->player;return PLAYER_STORE_NOT_FOUND; }
static int observe_stage(void *opaque,const character_save_journal_v2_writer_context *writer,const char *command_uuid)
{ fixture *test=(fixture *)opaque;if(test!=current||writer!=&test->writer||strcmp(command_uuid,"20000000-0000-4000-8000-000000000002"))return -99;test->observer_calls++;return test->observer_result; }
static void setup(fixture *test,test_mode mode)
{ memset(test,0,sizeof(*test));current=test;test->mode=mode;test->transport.state=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;test->live_ops.transport=&test->transport;test->limits.max_depth=64;test->limits.max_objects=8192;strcpy(test->player.name,"M3hero");character_save_journal_v2_player_store_init(&test->store,&test->writer,&test->live_ops,test->buffer,sizeof(test->buffer),&test->limits,deadline,test,command_uuid,test,delegated_load,test);(void)character_save_journal_v2_player_store_set_absent_bootstrap(&test->store,absent_bootstrap,test); }
static int clean(const fixture *test)
{ return test->store.state==CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE&&test->store.buffer_length==0&&test->store.active_player==0; }

static int test_normal_and_name_rejection(void)
{ fixture test;player_store_ops ops;int failed=0;setup(&test,TEST_NORMAL);ops=character_save_journal_v2_player_store_build(&test.store);failed+=expect(ops.opaque==&test.store&&ops.save&&ops.load,"build returns a by-value opaque PlayerStore dispatch");failed+=expect(ops.save(ops.opaque,"M3other",&test.player)==PLAYER_STORE_IO_ERROR&&!test.validate_calls&&!test.deadline_calls&&!test.protocol_calls&&test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NONE&&clean(&test),"name/player mismatch rejects before writer or callbacks with a fresh report");failed+=expect(ops.save(ops.opaque,"M3hero",&test.player)==PLAYER_STORE_OK&&test.validate_calls==1&&test.deadline_calls==1&&test.renew_calls==1&&test.bootstrap_calls==1&&!test.seed_calls&&test.uuid_calls==1&&test.serializer_calls==1&&test.protocol_calls==1&&test.revision==1&&clean(&test),"existing or absent head skips seed and retains normal save ordering");failed+=expect(ops.save(ops.opaque,"M3other",&test.player)==PLAYER_STORE_IO_ERROR&&test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NONE&&test.protocol_calls==1&&clean(&test),"idle pre-dispatch rejection clears the prior save report");return failed; }
static int test_pre_renew_failure(void)
{ fixture test;setup(&test,TEST_NORMAL);test.renew_failure=1;return expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.validate_calls==1&&test.deadline_calls==1&&test.renew_calls==1&&!test.uuid_calls&&!test.serializer_calls&&!test.protocol_calls&&clean(&test),"pre-renew failure makes no protocol or serialization mutation"); }
static int test_serializer_failure_and_route_drift(void)
{ fixture test;int failed=0;setup(&test,TEST_SERIALIZER_FAILURE);test.serializer_failure=1;failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.serializer_calls==1&&!test.protocol_calls&&test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NONE&&clean(&test),"serializer failure clears caller transient state before protocol mutation");setup(&test,TEST_ROUTE_DRIFT);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.serializer_calls==1&&test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED&&clean(&test),"pre-publish route drift remains a PlayerStore I/O error");return failed; }
static int test_absent_seed_gate(void)
{ fixture test;int failed=0;setup(&test,TEST_SEED_FAILURE);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.bootstrap_calls==1&&test.seed_calls==1&&!test.uuid_calls&&!test.serializer_calls&&!test.protocol_calls&&!test.observer_calls&&test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NONE&&clean(&test),"rejected deferred or malformed seed stops before serializer stage journal and publish");setup(&test,TEST_SEED_REBIND_DRIFT);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.seed_calls==1&&!test.uuid_calls&&!test.serializer_calls&&!test.protocol_calls&&clean(&test),"successful seed with rebinding drift makes no local mutation");setup(&test,TEST_SEED_SUCCESS);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.seed_calls==1&&test.uuid_calls==1&&test.serializer_calls==1&&test.protocol_calls==1&&test.revision==1&&clean(&test),"exact seed and absent-revision-zero rebind enter unchanged save_held_v3 path");return failed; }
static int test_post_publish_outcomes_and_sequential_revision(void)
{ fixture test;int failed=0;setup(&test,TEST_DEFERRED);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.store.last_report.ack_result==CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED&&clean(&test),"published deferred receipt maps to successful legacy save and journal recovery");setup(&test,TEST_INVALID_FREEZE);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.store.last_report.ack_result==CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE&&clean(&test),"published malformed-receipt freeze remains a durable legacy save");setup(&test,TEST_REJECTED_FREEZE);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.store.last_report.ack_result==CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE&&clean(&test),"published fence or CAS freeze remains a durable legacy save");setup(&test,TEST_LOCAL_INCOMPLETE);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.store.last_report.ack_result==CHARACTER_SAVE_JOURNAL_V2_ACK_DB_ACKED_LOCAL_INCOMPLETE&&clean(&test),"published DB_ACKED-local-incomplete receipt maps to journal recovery success");setup(&test,TEST_NORMAL);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.revision==2&&test.protocol_calls==2&&clean(&test),"sequential saves retain held ownership and delegate consecutive revisions to v3");return failed; }
static int test_load_and_reentrant_rejection(void)
{ fixture test;creature *loaded=0;int failed=0;setup(&test,TEST_NORMAL);failed+=expect(character_save_journal_v2_player_store_load(&test.store,"M3hero",&loaded)==PLAYER_STORE_NOT_FOUND&&loaded==&test.player&&test.load_calls==1&&!strcmp(test.loaded_name,"M3hero"),"load delegates unchanged to caller file store");setup(&test,TEST_REENTRANT);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.nested_result==PLAYER_STORE_IO_ERROR&&test.validate_calls==1&&test.renew_calls==1&&test.protocol_calls==1&&clean(&test),"reentrant save rejects before a second validation or renewal");return failed; }

static int test_stage_observer_forwarding_is_non_authoritative(void)
{ fixture test;int failed=0;setup(&test,TEST_NORMAL);test.observer_result=-73;character_save_journal_v2_player_store_set_stage_observer(&test.store,observe_stage,&test);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.observer_calls==1&&test.revision==1&&test.store.last_report.snapshot_attempted==1&&test.store.last_report.snapshot_result==-73&&clean(&test),"configured stage observer is forwarded while its failure remains diagnostic");return failed; }

static int test_v4_defers_caller_buffer_until_candidate_stage_identity(void)
{ fixture test;char before[sizeof(test.buffer)];int failed=0;setup(&test,TEST_V4_RESOLVER_ERROR);memset(test.buffer,'R',sizeof(test.buffer));memcpy(before,test.buffer,sizeof(before));character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.protocol_calls==1&&test.resolver_calls==1&&!test.serializer_calls&&!memcmp(test.buffer,before,sizeof(before))&&!test.stage_route_calls&&!test.prepared_calls&&!test.publish_calls&&!test.receipt_calls&&clean(&test),"v4 resolver ERROR must leave the caller buffer and every later mutation untouched");setup(&test,TEST_V4_REJECTED);memset(test.buffer,'C',sizeof(test.buffer));memcpy(before,test.buffer,sizeof(before));character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.protocol_calls==1&&test.resolver_calls==1&&!test.serializer_calls&&!memcmp(test.buffer,before,sizeof(before))&&!test.stage_route_calls&&!test.prepared_calls&&!test.publish_calls&&!test.receipt_calls&&clean(&test),"v4 rejected candidate must leave the caller buffer and every later mutation untouched");setup(&test,TEST_V4_INTER_BIND_MISMATCH);memset(test.buffer,'I',sizeof(test.buffer));memcpy(before,test.buffer,sizeof(before));character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.protocol_calls==1&&test.resolver_calls==1&&!test.serializer_calls&&!memcmp(test.buffer,before,sizeof(before))&&test.stage_route_calls==1&&!test.prepared_calls&&!test.publish_calls&&!test.receipt_calls&&clean(&test),"v4 inter-bind identity mismatch must precede encoder and every durable mutation");setup(&test,TEST_V4_FOUND);memset(test.buffer,'F',sizeof(test.buffer));character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.resolver_calls==1&&test.stage_route_calls==1&&test.serializer_calls==1&&test.prepared_calls==1&&test.publish_calls==1&&test.receipt_calls==1&&clean(&test),"v4 FOUND serializes only after the candidate and stage identity gates");setup(&test,TEST_V4_NO_CANDIDATE);memset(test.buffer,'N',sizeof(test.buffer));character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);failed+=expect(character_save_journal_v2_player_store_save(&test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.resolver_calls==1&&test.uuid_calls==1&&test.stage_route_calls==1&&test.serializer_calls==1&&test.prepared_calls==1&&test.publish_calls==1&&test.receipt_calls==1&&clean(&test),"v4 NO_CANDIDATE retains native UUID fallback and deferred serialization");return failed; }

static void reset_recovery_composition(void)
{
    player_recovery_reset();
    player_store_reset();
    recovery_free_calls=0;
    recovery_last_freed=0;
}

static int test_recovery_retry_composes_with_v2_player_store(void)
{
    fixture test;
    player_store_ops ops;
    creature extra;
    static creature bounded[PLAYER_RECOVERY_LIMIT+
                            PLAYER_RECOVERY_EMERGENCY_LIMIT+1];
    int failed=0;
    int index;

    reset_recovery_composition();
    setup(&test,TEST_ROUTE_DRIFT);
    test.recovery_forwarding_required=1;
    ops=character_save_journal_v2_player_store_build(&test.store);
    failed+=expect(player_store_set(&ops)==0&&
                   player_recovery_enqueue(&test.player)==0,
                   "recovery retry installs the opt-in v2 PlayerStore only in the test harness");
    failed+=expect(player_recovery_retry_one()==PLAYER_STORE_IO_ERROR&&
                   player_recovery_pending()==1&&!recovery_free_calls&&
                   test.protocol_calls==1&&
                   test.store.last_report.reached==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED&&
                   test.recovery_forwarding_calls==1&&!test.recovery_forwarding_bad&&
                   test.serialized_player==&test.player,
                   "pre-PUBLISHED result retains recovery ownership while forwarding the exact name and player");
    test.mode=TEST_DEFERRED;
    failed+=expect(player_recovery_retry_one()==PLAYER_STORE_OK&&
                   player_recovery_pending()==0&&recovery_free_calls==1&&
                   recovery_last_freed==&test.player&&test.protocol_calls==2&&
                   test.recovery_forwarding_calls==2&&!test.recovery_forwarding_bad&&
                   test.serialized_player==&test.player,
                   "PUBLISHED durable result releases exactly the retried player");
    failed+=expect(player_recovery_retry_one()==PLAYER_STORE_NOT_FOUND&&
                   player_recovery_pending()==0&&recovery_free_calls==1&&
                   test.protocol_calls==2,
                   "a repeated retry after durable removal is idempotent");

    reset_recovery_composition();
    setup(&test,TEST_V4_REJECTED);
    ops=character_save_journal_v2_player_store_build(&test.store);
    (void)character_save_journal_v2_player_store_set_candidate_resolver(
        &test.store,resolve_candidate,&test);
    failed+=expect(player_store_set(&ops)==0&&
                   player_recovery_enqueue(&test.player)==0&&
                   player_recovery_retry_one()==PLAYER_STORE_IO_ERROR&&
                   player_recovery_pending()==1&&!recovery_free_calls&&
                   test.resolver_calls==1&&!test.serializer_calls,
                   "v2 identity mismatch is non-durable and never frees the queued player");

    reset_recovery_composition();
    setup(&test,TEST_DEFERRED);
    ops=character_save_journal_v2_player_store_build(&test.store);
    memset(&extra,0,sizeof(extra));
    strcpy(extra.name,"M3other");
    failed+=expect(player_store_set(&ops)==0&&
                   player_recovery_enqueue(&test.player)==0&&
                   player_recovery_enqueue(&extra)==0&&
                   player_recovery_retry_one()==PLAYER_STORE_OK&&
                   player_recovery_pending()==1&&recovery_free_calls==1&&
                   recovery_last_freed==&test.player,
                   "one durable v2 result removes one queued entry and leaves the next entry owned by recovery");

    reset_recovery_composition();
    memset(bounded,0,sizeof(bounded));
    for(index=0;index<PLAYER_RECOVERY_LIMIT+PLAYER_RECOVERY_EMERGENCY_LIMIT;
        index++) {
        snprintf(bounded[index].name,sizeof(bounded[index].name),"Q%d",index);
        failed+=expect(player_recovery_enqueue(&bounded[index])==0,
                       "recovery retains its fixed normal and emergency queue bounds");
    }
    snprintf(bounded[index].name,sizeof(bounded[index].name),"Overflow");
    failed+=expect(player_recovery_enqueue(&bounded[index])==-1&&
                   player_recovery_pending()==PLAYER_RECOVERY_LIMIT+
                   PLAYER_RECOVERY_EMERGENCY_LIMIT,
                   "recovery rejects ownership once its bounded queue is exhausted");
    reset_recovery_composition();
    return failed;
}

static int test_save_existing_preserves_and_wipes(void)
{
    fixture test;
    char before[sizeof(test.buffer)];
    int failed=0;
    size_t index,mode_index;
    const test_mode rejected[]={TEST_V4_REJECTED,TEST_V4_RESOLVER_ERROR,
                                TEST_V4_INTER_BIND_MISMATCH};
    setup(&test,TEST_V4_FOUND);
    memset(test.buffer,'P',sizeof(test.buffer));
    memcpy(before,test.buffer,sizeof(before));
    failed+=expect(character_save_journal_v2_player_store_save_existing(
        &test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&
        !test.protocol_calls&&!test.copy_calls&&!test.serializer_calls&&
        !memcmp(before,test.buffer,sizeof(before)),"existing save requires the v4 resolver");
    character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);
    failed+=expect(character_save_journal_v2_player_store_save_existing(
        &test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.copy_calls==1&&
        !test.copy_wire_bad&&!test.serializer_calls&&test.publish_calls==1&&clean(&test),
        "existing save publishes exact persisted bytes without serializing scrubbed player");
    for(index=0;index<sizeof("original-record-with-password");index++)
        failed+=expect(test.buffer[index]==0,"copied credential bytes are wiped after success");
    for(mode_index=0;mode_index<sizeof(rejected)/sizeof(rejected[0]);mode_index++) {
        setup(&test,rejected[mode_index]);
        memset(test.buffer,'R',sizeof(test.buffer));memcpy(before,test.buffer,sizeof(before));
        character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);
        failed+=expect(character_save_journal_v2_player_store_save_existing(
            &test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&!test.copy_calls&&
            !test.serializer_calls&&!memcmp(before,test.buffer,sizeof(before))&&clean(&test),
            "rejected existing candidate leaves caller bytes untouched");
    }
    setup(&test,TEST_V4_NO_CANDIDATE);
    character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);
    failed+=expect(character_save_journal_v2_player_store_save_existing(
        &test.store,"M3hero",&test.player)==PLAYER_STORE_OK&&test.copy_calls==1&&
        test.uuid_calls==1&&!test.copy_wire_bad&&!test.serializer_calls&&clean(&test),
        "existing no-candidate path preserves bytes with native UUID fallback");
    setup(&test,TEST_V4_FOUND);test.copy_failure=1;
    character_save_journal_v2_player_store_set_candidate_resolver(&test.store,resolve_candidate,&test);
    failed+=expect(character_save_journal_v2_player_store_save_existing(
        &test.store,"M3hero",&test.player)==PLAYER_STORE_IO_ERROR&&test.copy_calls==1&&
        !test.serializer_calls&&!test.publish_calls&&clean(&test),"copy failure never publishes");
    for(index=0;index<sizeof("original-record-with-password");index++)
        failed+=expect(test.buffer[index]==0,"copied credential bytes are wiped after failure");
    return failed;
}

int main(void)
{ return test_save_existing_preserves_and_wipes()|test_normal_and_name_rejection()|test_pre_renew_failure()|test_serializer_failure_and_route_drift()|test_absent_seed_gate()|test_post_publish_outcomes_and_sequential_revision()|test_load_and_reentrant_rejection()|test_stage_observer_forwarding_is_non_authoritative()|test_v4_defers_caller_buffer_until_candidate_stage_identity()|test_recovery_retry_composes_with_v2_player_store(); }
