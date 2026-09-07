#include "character_save_journal_v2_bootstrap.h"

#include "character_save_journal_v2.h"

#include <string.h>
#include <unistd.h>

/* This sentinel is only the identity required to make a read-only v2 wire
 * valid.  Bootstrap never stages, prepares, publishes, or persists it. */
#define BOOTSTRAP_ABSENT_PRECONDITION_UUID "00000000-0000-0000-0000-000000000001"

static int bootstrap_copy_text(char *output, size_t output_size,
                               const char *input, size_t input_limit)
{
    size_t length;

    if(!output || !output_size || !input) return -1;
    for(length=0;length<=input_limit;length++)
        if(!input[length]) break;
    if(!length || length>input_limit || length>=output_size) return -1;
    memcpy(output,input,length);
    output[length]=0;
    return 0;
}

static int bootstrap_name_hex(const unsigned char *name, size_t length,
                              char output[CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX+1])
{
    static const char digits[]="0123456789abcdef";
    size_t index;

    if(!name || !length || length>CHARACTER_SAVE_JOURNAL_V2_NAME_MAX) return -1;
    for(index=0;index<length;index++) {
        output[index*2]=digits[name[index]>>4];
        output[index*2+1]=digits[name[index]&15];
    }
    output[length*2]=0;
    return 0;
}

static int bootstrap_tuple_same(const character_save_journal_v2_writer_tuple *left,
                                const character_save_journal_v2_writer_tuple *right)
{
    return left && right &&
        !memcmp(left->world_id,right->world_id,sizeof(left->world_id)) &&
        !memcmp(left->writer_instance_id,right->writer_instance_id,
                sizeof(left->writer_instance_id)) &&
        left->writer_epoch==right->writer_epoch;
}

/* The common v2 precondition owns all trusted-tree traversal.  This helper
 * merely turns the already-bound route and held tuple into its strict absent
 * wire, so bootstrap shares the exact descriptor-rooted trust boundary used
 * by a normal save without acquiring a stage or journal identity. */
static int bootstrap_absent_wire(
    const character_save_journal_v2_writer_tuple *tuple,
    const character_save_journal_v2_bound_route_v3 *route,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length,
    character_save_journal_v2_wire *wire)
{
    char route_world[CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX+1];

    if(!tuple || !route || !canonical_legacy_name || !wire ||
       route->head_state!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED ||
       route->head_revision ||
       route->legacy_name_length!=canonical_legacy_name_length ||
       !canonical_legacy_name_length ||
       memcmp(route->legacy_name,canonical_legacy_name,
              canonical_legacy_name_length) ||
       route->storage_format!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1 ||
       /* v3 does not expose imported evidence; M7a RPC owns that rejection. */
       (route->lifecycle!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED &&
        route->lifecycle!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING &&
        route->lifecycle!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE)) return -1;
    memset(wire,0,sizeof(*wire));
    memset(route_world,0,sizeof(route_world));
    if(bootstrap_copy_text(wire->world_id,sizeof(wire->world_id),tuple->world_id,
       CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX) ||
       bootstrap_copy_text(route_world,sizeof(route_world),route->world_id,
       CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX) ||
       strcmp(wire->world_id,route_world) ||
       bootstrap_copy_text(wire->writer_instance_id,
       sizeof(wire->writer_instance_id),tuple->writer_instance_id,
       CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN) ||
       bootstrap_copy_text(wire->character_id,sizeof(wire->character_id),
       route->character_id,CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN) ||
       bootstrap_name_hex(canonical_legacy_name,canonical_legacy_name_length,
       wire->legacy_name_key_hex) ||
       bootstrap_copy_text(wire->legacy_shard,sizeof(wire->legacy_shard),
       route->legacy_shard,2) ||
       bootstrap_copy_text(wire->command_uuid,sizeof(wire->command_uuid),
       BOOTSTRAP_ABSENT_PRECONDITION_UUID,
       CHARACTER_SAVE_JOURNAL_V2_UUID_LEN)) goto failed;
    wire->state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    wire->writer_epoch=tuple->writer_epoch;
    wire->writer_revision=1;
    wire->expected_state=CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    wire->storage_format=(uint16_t)route->storage_format;
    memset(wire->post_sha256,'0',CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN);
    if(character_save_journal_v2_request_sha256(wire,wire->request_sha256))
        goto failed;
    memset(route_world,0,sizeof(route_world));
    return 0;
failed:
    memset(route_world,0,sizeof(route_world));
    memset(wire,0,sizeof(*wire));
    return -1;
}

static int bootstrap_live_file_absent(
    const character_save_journal_v2_writer_context *writer,
    const character_save_journal_v2_writer_tuple *tuple,
    const character_save_journal_v2_bound_route_v3 *route,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length)
{
    character_save_journal_v2_wire wire;
    int root=-1, result=-1;

    memset(&wire,0,sizeof(wire));
    if(bootstrap_absent_wire(tuple,route,canonical_legacy_name,
       canonical_legacy_name_length,&wire) ||
       character_save_journal_v2_writer_dup_held_root_fd(writer,&root)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
       character_save_journal_v2_prepare_absent_shard_at(root,&wire) ||
       character_save_journal_v2_live_precondition_at(root,&wire)) goto done;
    result=0;
done:
    if(root>=0 && close(root)) result=-1;
    memset(&wire,0,sizeof(wire));
    return result;
}

static int bootstrap_route_same(
    const character_save_journal_v2_bound_route_v3 *left,
    const character_save_journal_v2_bound_route_v3 *right)
{
    return !memcmp(left->world_id,right->world_id,sizeof(left->world_id)) &&
        !memcmp(left->character_id,right->character_id,
                sizeof(left->character_id)) &&
        !memcmp(left->legacy_name,right->legacy_name,
                sizeof(left->legacy_name)) &&
        left->legacy_name_length==right->legacy_name_length &&
        !memcmp(left->legacy_shard,right->legacy_shard,
                sizeof(left->legacy_shard)) &&
        left->storage_format==right->storage_format &&
        left->lifecycle==right->lifecycle;
}

int character_save_journal_v2_bootstrap_absent_head(
    const character_save_journal_v2_writer_context *writer,
    character_save_journal_v2_live_ops *live_ops,
    const unsigned char *canonical_legacy_name, size_t canonical_legacy_name_length)
{
    character_save_journal_v2_bound_route_v3 initial, rebound;
    character_save_journal_v2_writer_tuple tuple, revalidated;

    memset(&initial,0,sizeof(initial));
    memset(&rebound,0,sizeof(rebound));
    memset(&tuple,0,sizeof(tuple));
    memset(&revalidated,0,sizeof(revalidated));
    if(!writer || !live_ops ||
       character_save_journal_v2_route_bind_v3(writer,canonical_legacy_name,
           canonical_legacy_name_length,
           character_save_journal_v2_live_ops_route_lookup_v3,live_ops,
           &initial)!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK) goto failed;
    if(initial.head_state!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED) {
        memset(&initial,0,sizeof(initial));
        return 0;
    }
    if(character_save_journal_v2_writer_validate_held(writer,&tuple)!=
           CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
       bootstrap_live_file_absent(writer,&tuple,&initial,canonical_legacy_name,
           canonical_legacy_name_length) ||
       character_save_journal_v2_writer_validate_held(writer,&revalidated)!=
           CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
       !bootstrap_tuple_same(&tuple,&revalidated) ||
       character_save_journal_v2_live_ops_seed_absent_head(live_ops,&tuple,
           &initial)!=CHARACTER_SAVE_JOURNAL_V2_RPC_TRANSPORT_OK ||
       character_save_journal_v2_route_bind_v3(writer,canonical_legacy_name,
           canonical_legacy_name_length,
           character_save_journal_v2_live_ops_route_lookup_v3,live_ops,
           &rebound)!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK ||
       !bootstrap_route_same(&initial,&rebound) ||
       rebound.head_state!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_ABSENT ||
       rebound.head_revision!=0) goto failed;
    memset(&tuple,0,sizeof(tuple));
    memset(&revalidated,0,sizeof(revalidated));
    memset(&initial,0,sizeof(initial));
    memset(&rebound,0,sizeof(rebound));
    return 0;
failed:
    memset(&tuple,0,sizeof(tuple));
    memset(&revalidated,0,sizeof(revalidated));
    memset(&initial,0,sizeof(initial));
    memset(&rebound,0,sizeof(rebound));
    return -1;
}
