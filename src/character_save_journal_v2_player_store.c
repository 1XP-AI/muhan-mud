#include "character_save_journal_v2_player_store.h"

#include "mstruct.h"

#include <string.h>

static unsigned long player_store_text_length(const char *text, unsigned long maximum)
{
    unsigned long length;

    if(!text) return maximum + 1;
    for(length=0; length<=maximum; length++)
        if(!text[length]) return length;
    return maximum + 1;
}

static int player_store_uuid_valid(const char *text)
{
    unsigned long index;

    if(player_store_text_length(text,CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH)!=
       CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH) return 0;
    for(index=0;index<CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH;index++) {
        if(index==8||index==13||index==18||index==23) {
            if(text[index]!='-') return 0;
        } else if(!((text[index]>='0'&&text[index]<='9')||
                    (text[index]>='a'&&text[index]<='f'))) return 0;
    }
    return 1;
}

static int player_store_live_ready(const character_save_journal_v2_player_store *store)
{
    return store&&store->live_ops&&store->live_ops->transport&&
        character_save_journal_v2_rpc_transport_get_state(store->live_ops->transport)==
        CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_READY;
}

static int player_store_configured(const character_save_journal_v2_player_store *store)
{
    return store&&store->held_writer&&player_store_live_ready(store)&&
        store->buffer&&store->buffer_capacity&&
        store->buffer_capacity<=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_BUFFER_MAX&&
        store->lease_deadline&&store->command_uuid&&store->file_load;
}

static void player_store_finish(character_save_journal_v2_player_store *store)
{
    store->active_player=0;
    store->buffer_length=0;
    store->state=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE;
}

static int player_store_serialize_bounded(
    character_save_journal_v2_player_store *store)
{
    unsigned long written=0;

    if(!store||!store->active_player||!store->buffer||
       !store->buffer_capacity||
       store->buffer_capacity>CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_BUFFER_MAX)
        return -1;
    store->buffer_length=0;
    if(player_record_serialize_bounded(store->active_player,0,store->buffer,
       store->buffer_capacity,&written,&store->serializer_limits)!=
       PLAYER_RECORD_SERIALIZER_OK||written>store->buffer_capacity) return -1;
    store->buffer_length=written;
    return 0;
}

/* protocol_save_held_v3 receives only the already bounded caller buffer.  The
 * legacy record encoder has no route-dependent fields, so producing it before
 * protocol composition keeps the adapter's externally visible ordering exact. */
static int player_store_serialized(void *opaque,
    const character_save_journal_v2_writer_tuple *writer,
    const character_save_journal_v2_bound_route_v3 *route,
    const char *command_uuid, const unsigned char **bytes_out, size_t *length_out)
{
    character_save_journal_v2_player_store *store=
        (character_save_journal_v2_player_store *)opaque;

    (void)writer;
    (void)route;
    (void)command_uuid;
    if(bytes_out) *bytes_out=0;
    if(length_out) *length_out=0;
    if(!store||!bytes_out||!length_out||!store->buffer||
       store->buffer_length>store->buffer_capacity) return -1;
    *bytes_out=(const unsigned char *)store->buffer;
    *length_out=(size_t)store->buffer_length;
    return 0;
}

void character_save_journal_v2_player_store_init(
    character_save_journal_v2_player_store *store,
    character_save_journal_v2_writer_context *held_writer,
    character_save_journal_v2_live_ops *live_ops,
    char *buffer, unsigned long buffer_capacity,
    const player_record_serializer_limits *serializer_limits,
    character_save_journal_v2_player_store_lease_deadline lease_deadline,
    void *lease_deadline_opaque,
    character_save_journal_v2_player_store_command_uuid command_uuid,
    void *command_uuid_opaque,
    character_save_journal_v2_player_store_file_load file_load,
    void *file_load_opaque)
{
    if(!store) return;
    memset(store,0,sizeof(*store));
    store->held_writer=held_writer;
    store->live_ops=live_ops;
    store->buffer=buffer;
    store->buffer_capacity=buffer_capacity;
    if(serializer_limits) store->serializer_limits=*serializer_limits;
    store->lease_deadline=lease_deadline;
    store->lease_deadline_opaque=lease_deadline_opaque;
    store->command_uuid=command_uuid;
    store->command_uuid_opaque=command_uuid_opaque;
    store->file_load=file_load;
    store->file_load_opaque=file_load_opaque;
    store->state=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE;
}

int character_save_journal_v2_player_store_set_stage_observer(
    character_save_journal_v2_player_store *store,
    character_save_journal_v2_prepared_stage_observer observer,
    void *observer_opaque)
{
    if(!store||store->state!=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE)
        return -1;
    store->stage_observer=observer;
    store->stage_observer_opaque=observer_opaque;
    return 0;
}

player_store_ops character_save_journal_v2_player_store_build(
    character_save_journal_v2_player_store *store)
{
    player_store_ops operations;

    operations.save=character_save_journal_v2_player_store_save;
    operations.load=character_save_journal_v2_player_store_load;
    operations.opaque=store;
    return operations;
}

int character_save_journal_v2_player_store_save(
    void *opaque, char *name, struct creature *player)
{
    character_save_journal_v2_player_store *store=
        (character_save_journal_v2_player_store *)opaque;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_protocol_held_request_v3 request;
    character_save_journal_v2_protocol_operations_v3 operations;
    char deadline[CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX+1];
    char command_uuid[CHARACTER_SAVE_JOURNAL_V2_UUID_TEXT_LENGTH+1];
    unsigned long name_length, player_name_length;
    character_save_journal_v2_protocol_result result;

    /* A recursive dispatch is inert and must not erase the outer protocol's
     * in-progress report.  Every non-recursive attempt starts with a fresh
     * observable report, including calls rejected before writer validation. */
    if(!store) return PLAYER_STORE_IO_ERROR;
    if(store->state!=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_IDLE)
        return PLAYER_STORE_IO_ERROR;
    store->buffer_length=0;
    store->active_player=0;
    memset(&store->last_report,0,sizeof(store->last_report));
    if(!name||!player) return PLAYER_STORE_IO_ERROR;
    name_length=player_store_text_length(name,CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX);
    player_name_length=player_store_text_length(player->name,sizeof(player->name)-1);
    if(!name_length||name_length>CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX||
       player_name_length!=name_length||memcmp(name,player->name,name_length))
        return PLAYER_STORE_IO_ERROR;

    store->state=CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_SAVING;
    memset(&tuple,0,sizeof(tuple));
    if(character_save_journal_v2_writer_validate_held(store->held_writer,&tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) goto failed;
    if(!player_store_configured(store)) goto failed;
    memset(deadline,0,sizeof(deadline));
    if(store->lease_deadline(store->lease_deadline_opaque,deadline)||
       !player_store_text_length(deadline,
          CHARACTER_SAVE_JOURNAL_V2_PLAYER_STORE_DEADLINE_MAX)) goto failed;
    if(character_save_journal_v2_live_ops_writer_epoch_renew(store->live_ops,
       &tuple,deadline)!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK) goto failed;
    memset(command_uuid,0,sizeof(command_uuid));
    if(store->command_uuid(store->command_uuid_opaque,command_uuid)||
       !player_store_uuid_valid(command_uuid)) goto failed;
    store->active_player=player;
    if(player_store_serialize_bounded(store)) goto failed;
    store->active_player=0;

    memset(&request,0,sizeof(request));
    request.canonical_legacy_name=(const unsigned char *)name;
    request.canonical_legacy_name_length=(size_t)name_length;
    request.command_uuid=command_uuid;
    memset(&operations,0,sizeof(operations));
    operations.route_lookup=character_save_journal_v2_live_ops_route_lookup_v3;
    operations.route_opaque=store->live_ops;
    operations.serialize=player_store_serialized;
    operations.serialize_opaque=store;
    operations.receipt=character_save_journal_v2_live_ops_receipt_callback;
    operations.receipt_opaque=store->live_ops;
    operations.observe_prepared_stage=store->stage_observer;
    operations.observe_prepared_stage_opaque=store->stage_observer_opaque;
    result=character_save_journal_v2_protocol_save_held_v3(store->held_writer,
        &request,&operations,&store->last_report);
    (void)result;
    /* After PUBLISHED the legacy file and its local journal evidence are the
     * durable authority.  Receipt defer/freeze/local-marker repair belongs to
     * journal recovery, not the in-memory player recovery queue.  Reporting a
     * legacy save failure here would retain and reserialize the player against
     * an already-published precondition. */
    if(store->last_report.reached>=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED) {
        player_store_finish(store);
        return PLAYER_STORE_OK;
    }
failed:
    player_store_finish(store);
    return PLAYER_STORE_IO_ERROR;
}

int character_save_journal_v2_player_store_load(
    void *opaque, char *name, struct creature **player)
{
    character_save_journal_v2_player_store *store=
        (character_save_journal_v2_player_store *)opaque;

    if(!store||!store->file_load) return PLAYER_STORE_IO_ERROR;
    return store->file_load(store->file_load_opaque,name,player);
}
