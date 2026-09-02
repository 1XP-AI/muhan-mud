#include "character_save_journal_v2_live_ops.h"

#include <limits.h>
#include <string.h>

static int live_ops_ready(const character_save_journal_v2_live_ops *ops)
{
    return ops && ops->transport &&
        character_save_journal_v2_rpc_transport_get_state(ops->transport) ==
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;
}

void character_save_journal_v2_live_ops_init(
    character_save_journal_v2_live_ops *ops,
    character_save_journal_v2_rpc_transport *transport,
    const char *acquire_lease_expires_at)
{
    if(!ops) return;
    ops->transport=transport;
    ops->acquire_lease_expires_at=acquire_lease_expires_at;
}

int character_save_journal_v2_live_ops_writer_epoch_acquire(
    void *opaque, const character_save_journal_v2_writer_tuple *request,
    character_save_journal_v2_writer_tuple *granted)
{
    character_save_journal_v2_live_ops *ops=
        (character_save_journal_v2_live_ops *)opaque;
    character_save_journal_v2_writer_tuple next;
    unsigned long long epoch=0;
    char expires_at[64];
    character_save_journal_v2_rpc_transport_outcome outcome;

    if(!granted || !request || !live_ops_ready(ops) ||
       !ops->acquire_lease_expires_at) return -1;
    memset(&next,0,sizeof(next));
    memset(expires_at,0,sizeof(expires_at));
    outcome=character_save_journal_v2_rpc_transport_acquire(ops->transport,
        request->world_id,request->writer_instance_id,
        ops->acquire_lease_expires_at,&epoch,expires_at);
    if(outcome!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK||!epoch||
       epoch>(unsigned long long)LLONG_MAX) return -1;
    strcpy(next.world_id,request->world_id);
    strcpy(next.writer_instance_id,request->writer_instance_id);
    next.writer_epoch=(uint64_t)epoch;
    *granted=next;
    return 0;
}

character_save_journal_v2_rpc_transport_outcome
character_save_journal_v2_live_ops_writer_epoch_renew(
    void *opaque, const character_save_journal_v2_writer_tuple *held,
    const char *lease_expires_at)
{
    character_save_journal_v2_live_ops *ops=
        (character_save_journal_v2_live_ops *)opaque;
    char expires_at[64];

    if(!held || !live_ops_ready(ops) || !lease_expires_at ||
       !held->writer_epoch ||
       held->writer_epoch>(uint64_t)LLONG_MAX)
        return CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID;
    memset(expires_at,0,sizeof(expires_at));
    return character_save_journal_v2_rpc_transport_renew(ops->transport,
        held->world_id,held->writer_instance_id,
        (unsigned long long)held->writer_epoch,lease_expires_at,expires_at);
}

static int live_ops_name_key(const unsigned char *bytes, size_t length,
    char key[CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX+1])
{
    if(!bytes || !length ||
       length>CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX ||
       memchr(bytes,0,length)) return 0;
    memcpy(key,bytes,length);
    key[length]=0;
    return 1;
}

static int live_ops_lifecycle(const char *text,
    character_save_journal_v2_route_lifecycle *out)
{
    if(!strcmp(text,"imported_unclaimed"))
        *out=CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED;
    else if(!strcmp(text,"provisioning"))
        *out=CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING;
    else if(!strcmp(text,"active"))
        *out=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
    else return 0;
    return 1;
}

static int live_ops_head(const char *text,
    character_save_journal_v2_route_head_state *out)
{
    if(!strcmp(text,"existing"))
        *out=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING;
    else if(!strcmp(text,"absent"))
        *out=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT;
    else if(!strcmp(text,"uninitialized"))
        *out=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED;
    else return 0;
    return 1;
}

character_save_journal_v2_route_lookup_result
character_save_journal_v2_live_ops_route_lookup_v3(
    void *opaque, const char *persisted_world_id,
    const unsigned char *canonical_legacy_name,
    size_t canonical_legacy_name_length,
    character_save_journal_v2_route_reply_v3 *reply)
{
    character_save_journal_v2_live_ops *ops=
        (character_save_journal_v2_live_ops *)opaque;
    character_save_journal_v2_rpc_route_v3 route;
    character_save_journal_v2_route_reply_v3 next;
    char key[CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX+1];

    if(!reply || !live_ops_ready(ops) ||
       !live_ops_name_key(canonical_legacy_name,canonical_legacy_name_length,
                          key))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    memset(&route,0,sizeof(route));
    if(character_save_journal_v2_rpc_transport_lookup_route_v3(ops->transport,
        persisted_world_id,key,&route)!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    memset(&next,0,sizeof(next));
    if(!live_ops_lifecycle(route.lifecycle,&next.lifecycle) ||
       !live_ops_head(route.head_state,&next.head_state) ||
       route.head_revision>(unsigned long long)LLONG_MAX)
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_FAILURE;
    next.status=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK;
    next.row_count=1;
    memcpy(next.world_id,route.world_id,sizeof(next.world_id));
    memcpy(next.character_id,route.character_id,sizeof(next.character_id));
    memcpy(next.legacy_name,canonical_legacy_name,
           canonical_legacy_name_length);
    next.legacy_name_length=canonical_legacy_name_length;
    memcpy(next.legacy_shard,route.legacy_shard,sizeof(next.legacy_shard));
    next.storage_format=route.storage_format;
    next.head_revision=(uint64_t)route.head_revision;
    if(next.head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING)
        memcpy(next.head_sha256,route.head_sha256,sizeof(next.head_sha256));
    *reply=next;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK;
}

character_save_journal_v2_receipt_result
character_save_journal_v2_live_ops_receipt_callback(
    void *opaque, const character_save_journal_v2_receipt *receipt)
{
    character_save_journal_v2_live_ops *ops=
        (character_save_journal_v2_live_ops *)opaque;
    character_save_journal_v2_rpc_transport_outcome outcome;

    if(!live_ops_ready(ops)) return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    outcome=character_save_journal_v2_rpc_transport_receipt(ops->transport,
                                                              receipt);
    if(outcome==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK)
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_ACKED;
    if(outcome==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_INVALID)
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_INVALID_FREEZE;
    if(outcome==CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_REJECTED)
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_REJECTED_FREEZE;
    return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
}
