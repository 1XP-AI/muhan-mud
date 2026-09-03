#include "character_save_journal_v2_protocol.h"

#include "character_save_journal_v2.h"

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "v2 protocol requires O_NOFOLLOW"
#endif

typedef struct protocol_sha256 {
    uint32_t h[8];
    uint64_t bits;
    unsigned char block[64];
    unsigned int used;
} protocol_sha256;

typedef struct protocol_receipt_guard {
    character_save_journal_v2_receipt_callback receipt;
    void *receipt_opaque;
    character_save_journal_v2_protocol_report *report;
} protocol_receipt_guard;

static uint32_t protocol_rotr(value, shift)
uint32_t value;
unsigned int shift;
{
    return (value >> shift) | (value << (32 - shift));
}

static void protocol_sha_block(state, input)
protocol_sha256 *state;
const unsigned char *input;
{
    static const uint32_t constants[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
    };
    uint32_t words[64], a, b, c, d, e, f, g, h, one, two;
    unsigned int i;
    for(i = 0; i < 16; i++)
        words[i] = ((uint32_t)input[i * 4] << 24) |
                   ((uint32_t)input[i * 4 + 1] << 16) |
                   ((uint32_t)input[i * 4 + 2] << 8) | input[i * 4 + 3];
    for(i = 16; i < 64; i++)
        words[i] = (protocol_rotr(words[i - 15], 7) ^
                    protocol_rotr(words[i - 15], 18) ^ (words[i - 15] >> 3)) +
                   words[i - 16] +
                   (protocol_rotr(words[i - 2], 17) ^
                    protocol_rotr(words[i - 2], 19) ^ (words[i - 2] >> 10)) +
                   words[i - 7];
    a = state->h[0]; b = state->h[1]; c = state->h[2]; d = state->h[3];
    e = state->h[4]; f = state->h[5]; g = state->h[6]; h = state->h[7];
    for(i = 0; i < 64; i++) {
        one = h + (protocol_rotr(e, 6) ^ protocol_rotr(e, 11) ^
                   protocol_rotr(e, 25)) + ((e & f) ^ ((~e) & g)) +
              constants[i] + words[i];
        two = (protocol_rotr(a, 2) ^ protocol_rotr(a, 13) ^
               protocol_rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c));
        h = g; g = f; f = e; e = d + one; d = c; c = b; b = a; a = one + two;
    }
    state->h[0] += a; state->h[1] += b; state->h[2] += c; state->h[3] += d;
    state->h[4] += e; state->h[5] += f; state->h[6] += g; state->h[7] += h;
    memset(words, 0, sizeof(words));
}

static void protocol_sha_init(state)
protocol_sha256 *state;
{
    static const uint32_t initial[8] = {
        0x6a09e667U,0xbb67ae85U,0x3c6ef372U,0xa54ff53aU,
        0x510e527fU,0x9b05688cU,0x1f83d9abU,0x5be0cd19U
    };
    memcpy(state->h, initial, sizeof(initial));
    state->bits = 0;
    state->used = 0;
}

static void protocol_sha_update(state, bytes, length)
protocol_sha256 *state;
const unsigned char *bytes;
size_t length;
{
    size_t take;
    state->bits += (uint64_t)length * 8;
    while(length) {
        take = 64 - state->used;
        if(take > length) take = length;
        memcpy(state->block + state->used, bytes, take);
        state->used += (unsigned int)take;
        bytes += take;
        length -= take;
        if(state->used == 64) {
            protocol_sha_block(state, state->block);
            state->used = 0;
        }
    }
}

static void protocol_sha_final(state, output)
protocol_sha256 *state;
unsigned char output[32];
{
    unsigned int i;
    uint64_t bits = state->bits;
    state->block[state->used++] = 0x80;
    if(state->used > 56) {
        while(state->used < 64) state->block[state->used++] = 0;
        protocol_sha_block(state, state->block);
        state->used = 0;
    }
    while(state->used < 56) state->block[state->used++] = 0;
    for(i = 0; i < 8; i++) state->block[63 - i] = (unsigned char)(bits >> (i * 8));
    protocol_sha_block(state, state->block);
    for(i = 0; i < 8; i++) {
        output[i * 4] = (unsigned char)(state->h[i] >> 24);
        output[i * 4 + 1] = (unsigned char)(state->h[i] >> 16);
        output[i * 4 + 2] = (unsigned char)(state->h[i] >> 8);
        output[i * 4 + 3] = (unsigned char)state->h[i];
    }
}

static void protocol_sha_hex(bytes, output)
const unsigned char bytes[32];
char output[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
{
    static const char hex[] = "0123456789abcdef";
    unsigned int i;
    for(i = 0; i < 32; i++) {
        output[i * 2] = hex[bytes[i] >> 4];
        output[i * 2 + 1] = hex[bytes[i] & 15];
    }
    output[64] = 0;
}

static int protocol_hash_bytes(bytes, length, output)
const unsigned char *bytes;
size_t length;
char output[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
{
    protocol_sha256 state;
    unsigned char raw[32];
    if(!bytes || !output || length > CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES)
        return -1;
    protocol_sha_init(&state);
    protocol_sha_update(&state, bytes, length);
    protocol_sha_final(&state, raw);
    protocol_sha_hex(raw, output);
    memset(raw, 0, sizeof(raw));
    memset(&state, 0, sizeof(state));
    return 0;
}

static int protocol_uuid(text)
const char *text;
{
    size_t i;
    if(!text || strlen(text) != CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) return 0;
    for(i = 0; i < CHARACTER_SAVE_JOURNAL_V2_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(text[i] != '-') return 0;
        } else if(!((text[i] >= '0' && text[i] <= '9') ||
                    (text[i] >= 'a' && text[i] <= 'f'))) return 0;
    }
    return 1;
}

static int protocol_name_hex(name, length, output, output_size)
const unsigned char *name;
size_t length;
char *output;
size_t output_size;
{
    static const char hex[] = "0123456789abcdef";
    size_t i;
    if(!name || !length || length > CHARACTER_SAVE_JOURNAL_V2_NAME_MAX ||
       output_size < length * 2 + 1) return -1;
    for(i = 0; i < length; i++) {
        output[i * 2] = hex[name[i] >> 4];
        output[i * 2 + 1] = hex[name[i] & 15];
    }
    output[length * 2] = 0;
    return 0;
}

static int protocol_wire_matches(left, right)
const character_save_journal_v2_wire *left;
const character_save_journal_v2_wire *right;
{
    return left && right && left->state == right->state &&
        !strcmp(left->writer_instance_id, right->writer_instance_id) &&
        !strcmp(left->character_id, right->character_id) &&
        !strcmp(left->request_sha256, right->request_sha256) &&
        !strcmp(left->world_id, right->world_id) &&
        !strcmp(left->legacy_name_key_hex, right->legacy_name_key_hex) &&
        !strcmp(left->legacy_shard, right->legacy_shard) &&
        !strcmp(left->command_uuid, right->command_uuid) &&
        left->writer_epoch == right->writer_epoch &&
        left->writer_revision == right->writer_revision &&
        left->expected_state == right->expected_state &&
        !strcmp(left->expected_sha256, right->expected_sha256) &&
        !strcmp(left->post_sha256, right->post_sha256) &&
        left->storage_format == right->storage_format;
}

static int protocol_tuple_matches(left, right)
const character_save_journal_v2_writer_tuple *left;
const character_save_journal_v2_writer_tuple *right;
{
    return left && right && !strcmp(left->world_id, right->world_id) &&
        !strcmp(left->writer_instance_id, right->writer_instance_id) &&
        left->writer_epoch == right->writer_epoch;
}

static int protocol_writer_matches(writer, expected)
const character_save_journal_v2_writer_context *writer;
const character_save_journal_v2_writer_tuple *expected;
{
    character_save_journal_v2_writer_tuple observed;
    int result;
    memset(&observed, 0, sizeof(observed));
    result = character_save_journal_v2_writer_validate_held(writer, &observed) ==
        CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK &&
        protocol_tuple_matches(expected, &observed);
    memset(&observed, 0, sizeof(observed));
    return result;
}

static void protocol_report_zero(report)
character_save_journal_v2_protocol_report *report;
{
    if(report) memset(report, 0, sizeof(*report));
}

static character_save_journal_v2_protocol_result protocol_ack_result(value)
character_save_journal_v2_ack_result value;
{
    if(value == CHARACTER_SAVE_JOURNAL_V2_ACK_ACKED)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK;
    if(value == CHARACTER_SAVE_JOURNAL_V2_ACK_DEFERRED)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_DEFERRED;
    if(value == CHARACTER_SAVE_JOURNAL_V2_ACK_INVALID_FREEZE ||
       value == CHARACTER_SAVE_JOURNAL_V2_ACK_REJECTED_FREEZE)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK_FROZEN;
    return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ACK;
}

static character_save_journal_v2_receipt_result protocol_receipt_once(opaque, receipt)
void *opaque;
const character_save_journal_v2_receipt *receipt;
{
    protocol_receipt_guard *guard = opaque;
    if(!guard || !guard->receipt || !receipt || !protocol_uuid(receipt->command_id))
        return CHARACTER_SAVE_JOURNAL_V2_RECEIPT_DEFERRED;
    if(guard->report)
        guard->report->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_RECEIPT;
    return guard->receipt(guard->receipt_opaque, receipt);
}

character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_save_held_v3(writer, request, operations,
                                                 report_out)
const character_save_journal_v2_writer_context *writer;
const character_save_journal_v2_protocol_held_request_v3 *request;
const character_save_journal_v2_protocol_operations_v3 *operations;
character_save_journal_v2_protocol_report *report_out;
{
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_bound_route_v3 route;
    character_save_journal_v2_wire wire, reread;
    character_save_journal_v2_route_error route_result;
    const unsigned char *bytes=0;
    size_t length=0;
    character_save_journal_v2_publish_result published;
    character_save_journal_v2_ack_result acknowledged;
    character_save_journal_v2_protocol_result result;
    int root_fd=-1;
    protocol_report_zero(report_out);
    if(!writer||!request||!operations||!report_out||
       !request->canonical_legacy_name||!request->canonical_legacy_name_length||
       !protocol_uuid(request->command_uuid)||!operations->route_lookup||
       !operations->serialize||!operations->receipt)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    memset(&tuple,0,sizeof(tuple));
    memset(&route,0,sizeof(route));
    memset(&wire,0,sizeof(wire));
    memset(&reread,0,sizeof(reread));
    if(character_save_journal_v2_writer_validate_held(writer,&tuple)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_WRITER_LOCKED;
    route_result=character_save_journal_v2_route_bind_v3(writer,
        request->canonical_legacy_name,request->canonical_legacy_name_length,
        operations->route_lookup,operations->route_opaque,&route);
    if(route_result!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;
        goto done;
    }
    report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;
    /* The route is the sole revision authority.  Do this before serializer
     * invocation or descriptor duplication so rejected heads leave no local
     * evidence or filesystem mutation. */
    if(route.head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_UNINITIALIZED||
       route.head_revision>=(uint64_t)INT64_MAX) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;
        goto done;
    }
    if(operations->serialize(operations->serialize_opaque,&tuple,&route,
                             request->command_uuid,&bytes,&length)!=0||!bytes||
       length>CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER;
        goto done;
    }
    report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED;
    if(!protocol_writer_matches(writer,&tuple)) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    wire.state=CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    memcpy(wire.writer_instance_id,tuple.writer_instance_id,
           sizeof(wire.writer_instance_id));
    memcpy(wire.character_id,route.character_id,sizeof(wire.character_id));
    memcpy(wire.world_id,tuple.world_id,sizeof(wire.world_id));
    if(protocol_name_hex(request->canonical_legacy_name,
                         request->canonical_legacy_name_length,
                         wire.legacy_name_key_hex,sizeof(wire.legacy_name_key_hex))||
       route.legacy_name_length!=request->canonical_legacy_name_length||
       memcmp(route.legacy_name,request->canonical_legacy_name,
              request->canonical_legacy_name_length)||
       route.storage_format!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1||
       snprintf(wire.legacy_shard,sizeof(wire.legacy_shard),"%s",
                route.legacy_shard)!=2||
       snprintf(wire.command_uuid,sizeof(wire.command_uuid),"%s",
                request->command_uuid)!=CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;
        goto done;
    }
    wire.writer_epoch=tuple.writer_epoch;
    wire.writer_revision=route.head_revision+1;
    wire.expected_state=route.head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING?
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING:
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    if(route.head_state==CHARACTER_SAVE_JOURNAL_V2_ROUTE_HEAD_EXISTING)
        memcpy(wire.expected_sha256,route.head_sha256,sizeof(wire.expected_sha256));
    wire.storage_format=(uint16_t)route.storage_format;
    if(protocol_hash_bytes(bytes,length,wire.post_sha256)||
       character_save_journal_v2_request_sha256(&wire,wire.request_sha256)) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    if(character_save_journal_v2_writer_dup_held_root_fd(writer,&root_fd)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    /* A command UUID is an immutable journal identity.  Recovery owns every
     * valid PREPARED record already present for it; a new save must reject
     * before creating a stage whose newer route revision could contaminate
     * that recovery evidence. */
    if(character_save_journal_v2_read_prepared_at(root_fd,
       request->command_uuid,&reread)==0) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    memset(&reread,0,sizeof(reread));
    if(character_save_journal_v2_stage_at(root_fd,&wire,bytes,length)) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED;
    if(!protocol_writer_matches(writer,&tuple)||
       character_save_journal_v2_live_precondition_at(root_fd,&wire)||
       !protocol_writer_matches(writer,&tuple)||
       character_save_journal_v2_commit_prepared_at(root_fd,&wire)||
       character_save_journal_v2_read_prepared_at(root_fd,request->command_uuid,
                                                   &reread)||
       !protocol_wire_matches(&wire,&reread)) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    if(close(root_fd)) {
        root_fd=-1;
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    root_fd=-1;
    report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED;
    if(operations->observe_prepared_stage) {
        report_out->snapshot_attempted=1;
        report_out->snapshot_result=operations->observe_prepared_stage(
            operations->observe_prepared_stage_opaque,writer,
            request->command_uuid);
    }
    published=character_save_journal_v2_publish_v3(writer,
        request->canonical_legacy_name,request->canonical_legacy_name_length,
        operations->route_lookup,operations->route_opaque,request->command_uuid);
    report_out->publish_result=published;
    if(published!=CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK) {
        result=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH;
        goto done;
    }
    report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    acknowledged=character_save_journal_v2_ack(writer,request->command_uuid,
                                                operations->receipt,
                                                operations->receipt_opaque);
    report_out->ack_result=acknowledged;
    result=protocol_ack_result(acknowledged);
    if(result==CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK)
        report_out->reached=CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED;
done:
    if(root_fd>=0) close(root_fd);
    memset(&tuple,0,sizeof(tuple));
    memset(&route,0,sizeof(route));
    memset(&wire,0,sizeof(wire));
    memset(&reread,0,sizeof(reread));
    return result;
}

character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_save(request, operations, report_out)
const character_save_journal_v2_protocol_request *request;
const character_save_journal_v2_protocol_operations *operations;
character_save_journal_v2_protocol_report *report_out;
{
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_bound_route route;
    character_save_journal_v2_wire wire, reread;
    character_save_journal_v2_route_error route_result;
    const unsigned char *bytes = 0;
    size_t length = 0;
    character_save_journal_v2_publish_result published;
    character_save_journal_v2_ack_result acknowledged;
    character_save_journal_v2_protocol_result result;
    int close_result, root_fd = -1;
    protocol_report_zero(report_out);
    if(!request || !operations || !report_out || !request->root || !request->world_id ||
       !request->canonical_legacy_name || !request->canonical_legacy_name_length ||
       !protocol_uuid(request->command_uuid) || !request->writer_revision ||
       !operations->route_lookup || !operations->serialize || !operations->receipt)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    memset(&writer, 0, sizeof(writer));
    memset(&tuple, 0, sizeof(tuple));
    memset(&route, 0, sizeof(route));
    memset(&wire, 0, sizeof(wire));
    memset(&reread, 0, sizeof(reread));
    if(character_save_journal_v2_writer_open(request->root, request->world_id, &writer) != 0)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_WRITER_LOCKED;
    if(character_save_journal_v2_writer_validate_held(&writer, &tuple) !=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    route_result = character_save_journal_v2_route_bind(&writer,
        request->canonical_legacy_name, request->canonical_legacy_name_length,
        operations->route_lookup, operations->route_opaque, &route);
    if(route_result != CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;
        goto done;
    }
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_ROUTE_EPOCH;
    if(operations->serialize(operations->serialize_opaque, &tuple, &route,
                             request->command_uuid, &bytes, &length) != 0 || !bytes ||
       length > CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SERIALIZER;
        goto done;
    }
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SERIALIZED;
    if(!protocol_writer_matches(&writer, &tuple)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    wire.state = CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    memcpy(wire.writer_instance_id, tuple.writer_instance_id,
           sizeof(wire.writer_instance_id));
    memcpy(wire.character_id, route.character_id, sizeof(wire.character_id));
    memcpy(wire.world_id, tuple.world_id, sizeof(wire.world_id));
    if(protocol_name_hex(request->canonical_legacy_name,
                         request->canonical_legacy_name_length,
                         wire.legacy_name_key_hex,
                         sizeof(wire.legacy_name_key_hex)) != 0 ||
       route.legacy_name_length != request->canonical_legacy_name_length ||
       memcmp(route.legacy_name, request->canonical_legacy_name,
              request->canonical_legacy_name_length) != 0 ||
       route.storage_format == 0 ||
       snprintf(wire.legacy_shard, sizeof(wire.legacy_shard), "%s",
                route.legacy_shard) != 2 ||
       snprintf(wire.command_uuid, sizeof(wire.command_uuid), "%s",
                request->command_uuid) != CHARACTER_SAVE_JOURNAL_V2_UUID_LEN) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_ROUTE;
        goto done;
    }
    wire.writer_epoch = tuple.writer_epoch;
    wire.writer_revision = (uint64_t)request->writer_revision;
    wire.expected_state = route.has_imported_file_sha256 ?
        CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING : CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
    if(route.has_imported_file_sha256)
        memcpy(wire.expected_sha256, route.imported_file_sha256,
               sizeof(wire.expected_sha256));
    wire.storage_format = (uint16_t)route.storage_format;
    if(protocol_hash_bytes(bytes, length, wire.post_sha256) != 0 ||
       character_save_journal_v2_request_sha256(&wire, wire.request_sha256) != 0) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    if(character_save_journal_v2_writer_dup_held_root_fd(&writer, &root_fd) !=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    if(character_save_journal_v2_stage_at(root_fd, &wire, bytes, length) != 0) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_STAGED;
    if(!protocol_writer_matches(&writer, &tuple)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    if(character_save_journal_v2_live_precondition_at(root_fd, &wire) != 0) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    if(!protocol_writer_matches(&writer, &tuple)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
        goto done;
    }
    if(character_save_journal_v2_commit_prepared_at(root_fd, &wire) != 0 ||
       character_save_journal_v2_read_prepared_at(root_fd, request->command_uuid,
                                                   &reread) != 0 ||
       !protocol_wire_matches(&wire, &reread)) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    if(close(root_fd) != 0) {
        root_fd = -1;
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PREPARE;
        goto done;
    }
    root_fd = -1;
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PREPARED;
    published = character_save_journal_v2_publish(&writer,
        request->canonical_legacy_name, request->canonical_legacy_name_length,
        operations->route_lookup, operations->route_opaque, request->command_uuid);
    report_out->publish_result = published;
    if(published != CHARACTER_SAVE_JOURNAL_V2_PUBLISH_OK) {
        result = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_PUBLISH;
        goto done;
    }
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_PUBLISHED;
    acknowledged = character_save_journal_v2_ack(&writer, request->command_uuid,
        operations->receipt, operations->receipt_opaque);
    report_out->ack_result = acknowledged;
    result = protocol_ack_result(acknowledged);
    if(result == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK)
        report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DB_ACKED;
done:
    if(root_fd >= 0) close(root_fd);
    close_result = character_save_journal_v2_writer_close(&writer);
    memset(&tuple, 0, sizeof(tuple));
    memset(&route, 0, sizeof(route));
    memset(&wire, 0, sizeof(wire));
    memset(&reread, 0, sizeof(reread));
    if(close_result != 0 && result == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    return result;
}

static character_save_journal_v2_protocol_result protocol_recover_held(writer,
    receipt, receipt_opaque, report_out)
const character_save_journal_v2_writer_context *writer;
character_save_journal_v2_receipt_callback receipt;
void *receipt_opaque;
character_save_journal_v2_protocol_report *report_out;
{
    protocol_receipt_guard guard;
    character_save_journal_v2_recovery_report recovery;
    character_save_journal_v2_recovery_result recovered;
    memset(&guard, 0, sizeof(guard));
    memset(&recovery, 0, sizeof(recovery));
    guard.receipt = receipt;
    guard.receipt_opaque = receipt_opaque;
    guard.report = report_out;
    recovered = character_save_journal_v2_recovery_run(writer,
        protocol_receipt_once, &guard, &recovery);
    report_out->recovery_result = recovered;
    if(recovered != CHARACTER_SAVE_JOURNAL_V2_RECOVERY_OK)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_RECOVERY;
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_DRAINED;
    return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK;
}

character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_recover(root, world_id, receipt,
                                            receipt_opaque, report_out)
const char *root;
const char *world_id;
character_save_journal_v2_receipt_callback receipt;
void *receipt_opaque;
character_save_journal_v2_protocol_report *report_out;
{
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_protocol_result result;
    int close_result;
    protocol_report_zero(report_out);
    if(!root || !world_id || !receipt || !report_out)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    memset(&writer, 0, sizeof(writer));
    if(character_save_journal_v2_writer_open(root, world_id, &writer) != 0)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_WRITER_LOCKED;
    result = protocol_recover_held(&writer, receipt, receipt_opaque, report_out);
    close_result = character_save_journal_v2_writer_close(&writer);
    if(close_result != 0 && result == CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    return result;
}

character_save_journal_v2_protocol_result
character_save_journal_v2_protocol_drain_and_install(root, world_id,
                                                       operations, report_out)
const char *root;
const char *world_id;
const character_save_journal_v2_protocol_cutover_operations *operations;
character_save_journal_v2_protocol_report *report_out;
{
    character_save_journal_v2_writer_context writer;
    character_save_journal_v2_writer_tuple tuple_a, tuple_before_seal;
    character_save_journal_v2_writer_tuple expected_b, observed_b;
    character_save_journal_v2_protocol_result result;
    int close_result;
    protocol_report_zero(report_out);
    if(!root || !world_id || !operations || !operations->receipt ||
       !operations->attest_drained || !operations->seal ||
       !operations->install_next_writer || !report_out)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INVALID_ARGUMENT;
    memset(&writer, 0, sizeof(writer));
    memset(&tuple_a, 0, sizeof(tuple_a));
    memset(&tuple_before_seal, 0, sizeof(tuple_before_seal));
    memset(&expected_b, 0, sizeof(expected_b));
    memset(&observed_b, 0, sizeof(observed_b));
    if(character_save_journal_v2_writer_open(root, world_id, &writer) != 0)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_WRITER_LOCKED;
    result = protocol_recover_held(&writer, operations->receipt,
                                   operations->receipt_opaque, report_out);
    if(result != CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK) {
        (void)character_save_journal_v2_writer_close(&writer);
        return result;
    }
    if(character_save_journal_v2_writer_validate_held(&writer, &tuple_a) !=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
       operations->attest_drained(operations->lifecycle_opaque, &tuple_a) != 0 ||
       character_save_journal_v2_writer_validate_held(&writer,
                                                       &tuple_before_seal) !=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
       !protocol_tuple_matches(&tuple_a, &tuple_before_seal)) {
        (void)character_save_journal_v2_writer_close(&writer);
        memset(&tuple_a, 0, sizeof(tuple_a));
        memset(&tuple_before_seal, 0, sizeof(tuple_before_seal));
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_RECOVERY;
    }
    if(operations->seal(operations->lifecycle_opaque, &tuple_a) != 0) {
        (void)character_save_journal_v2_writer_close(&writer);
        memset(&tuple_a, 0, sizeof(tuple_a));
        memset(&tuple_before_seal, 0, sizeof(tuple_before_seal));
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_SEAL;
    }
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_SEALED;
    close_result = character_save_journal_v2_writer_close(&writer);
    if(close_result != 0) return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
    if(operations->install_next_writer(operations->lifecycle_opaque, &tuple_a,
                                       &expected_b) != 0)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INSTALL;
    if(character_save_journal_v2_writer_open(root, world_id, &writer) != 0)
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INSTALL;
    if(character_save_journal_v2_writer_validate_held(&writer, &observed_b) !=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK ||
       !protocol_tuple_matches(&expected_b, &observed_b) ||
       strcmp(tuple_a.world_id, expected_b.world_id) ||
       !strcmp(tuple_a.writer_instance_id, expected_b.writer_instance_id) ||
       expected_b.writer_epoch <= tuple_a.writer_epoch) {
        (void)character_save_journal_v2_writer_close(&writer);
        memset(&tuple_a, 0, sizeof(tuple_a));
        memset(&tuple_before_seal, 0, sizeof(tuple_before_seal));
        memset(&expected_b, 0, sizeof(expected_b));
        memset(&observed_b, 0, sizeof(observed_b));
        return CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_INSTALL;
    }
    report_out->reached = CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_CUTPOINT_NEXT_WRITER;
    close_result = character_save_journal_v2_writer_close(&writer);
    memset(&tuple_a, 0, sizeof(tuple_a));
    memset(&tuple_before_seal, 0, sizeof(tuple_before_seal));
    memset(&expected_b, 0, sizeof(expected_b));
    memset(&observed_b, 0, sizeof(observed_b));
    return close_result == 0 ? CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_OK :
        CHARACTER_SAVE_JOURNAL_V2_PROTOCOL_WRITER;
}
