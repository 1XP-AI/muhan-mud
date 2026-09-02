#include "character_save_journal_v2_route.h"

#include <stdint.h>
#include <string.h>

/* Private writer/route linkage: the generation is output-only and is not
 * declared by the public writer API, because it is never caller authority. */
extern character_save_journal_v2_writer_context_status
character_save_journal_v2_writer_validate_held_for_route(
    const character_save_journal_v2_writer_context *context,
    character_save_journal_v2_writer_tuple *tuple_out,
    uint64_t *generation_out);

static size_t route_bounded(text, limit)
const char *text;
size_t limit;
{
    size_t i;
    if(!text) return limit+1;
    for(i=0;i<=limit;i++) if(!text[i]) return i;
    return limit+1;
}

static int route_lower_hex(text, length)
const char *text;
size_t length;
{
    size_t i;
    if(!text||route_bounded(text,length)!=length) return 0;
    for(i=0;i<length;i++) {
        if(!((text[i]>='0'&&text[i]<='9')||(text[i]>='a'&&text[i]<='f'))) return 0;
    }
    return 1;
}

static int route_uuid(text)
const char *text;
{
    size_t i;
    if(!text||route_bounded(text,CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN)!=
       CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN) return 0;
    for(i=0;i<CHARACTER_SAVE_JOURNAL_V2_WRITER_UUID_LEN;i++) {
        if(i==8||i==13||i==18||i==23) {
            if(text[i]!='-') return 0;
        } else if(!((text[i]>='0'&&text[i]<='9')||
                    (text[i]>='a'&&text[i]<='f'))) return 0;
    }
    return 1;
}

static int route_utf8(bytes, length, codepoints)
const unsigned char *bytes;
size_t length;
size_t *codepoints;
{
    size_t i=0;
    size_t points=0;
    unsigned char first;
    while(i<length) {
        first=bytes[i++];
        if(first<0x80) {
            points++;
            continue;
        }
        if(first>=0xc2&&first<=0xdf) {
            if(i>=length||bytes[i]<0x80||bytes[i]>0xbf) return 0;
            i++;
        } else if(first==0xe0) {
            if(i+1>=length||bytes[i]<0xa0||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first>=0xe1&&first<=0xec) {
            if(i+1>=length||bytes[i]<0x80||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first==0xed) {
            if(i+1>=length||bytes[i]<0x80||bytes[i]>0x9f||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first>=0xee&&first<=0xef) {
            if(i+1>=length||bytes[i]<0x80||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf) return 0;
            i+=2;
        } else if(first==0xf0) {
            if(i+2>=length||bytes[i]<0x90||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf||
               bytes[i+2]<0x80||bytes[i+2]>0xbf) return 0;
            i+=3;
        } else if(first>=0xf1&&first<=0xf3) {
            if(i+2>=length||bytes[i]<0x80||bytes[i]>0xbf||
               bytes[i+1]<0x80||bytes[i+1]>0xbf||
               bytes[i+2]<0x80||bytes[i+2]>0xbf) return 0;
            i+=3;
        } else if(first==0xf4) {
            if(i+2>=length||bytes[i]<0x80||bytes[i]>0x8f||
               bytes[i+1]<0x80||bytes[i+1]>0xbf||
               bytes[i+2]<0x80||bytes[i+2]>0xbf) return 0;
            i+=3;
        } else return 0;
        points++;
    }
    if(codepoints) *codepoints=points;
    return 1;
}

static int route_canonical_name(name, length)
const unsigned char *name;
size_t length;
{
    size_t i;
    size_t points;
    int all_spaces=1;
    if(!name||!length||length>CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX) return 0;
    for(i=0;i<length;i++) {
        if(!name[i]||name[i]<0x20||name[i]==0x7f||name[i]=='/'||
           name[i]=='\\'||name[i]==':') return 0;
        if(name[i]!=' ') all_spaces=0;
        if((i==0&&name[i]>='a'&&name[i]<='z')||
           (i>0&&name[i]>='A'&&name[i]<='Z')) return 0;
    }
    if(all_spaces||(length==1&&name[0]=='.')||
       (length==2&&name[0]=='.'&&name[1]=='.')||
       !route_utf8(name,length,&points)||!points||points>12) return 0;
    return 1;
}

static uint32_t route_rol32(value, shift)
uint32_t value;
unsigned int shift;
{
    return (value<<shift)|(value>>(32-shift));
}

static uint32_t route_load_u32_be(bytes)
const unsigned char *bytes;
{
    return ((uint32_t)bytes[0]<<24)|((uint32_t)bytes[1]<<16)|
           ((uint32_t)bytes[2]<<8)|(uint32_t)bytes[3];
}

/* Route names are capped at 14 bytes, so SHA-1 always has one padded block. */
static void route_sha1(name, length, out)
const unsigned char *name;
size_t length;
unsigned char out[20];
{
    uint32_t h0=0x67452301U,h1=0xefcdab89U,h2=0x98badcfeU;
    uint32_t h3=0x10325476U,h4=0xc3d2e1f0U;
    uint32_t words[80],a,b,c,d,e,f,constant,next;
    unsigned char block[64];
    uint64_t bits=(uint64_t)length*8;
    unsigned int i;
    memset(block,0,sizeof(block));
    memcpy(block,name,length);
    block[length]=0x80;
    for(i=0;i<8;i++) block[63-i]=(unsigned char)(bits>>(i*8));
    for(i=0;i<16;i++) words[i]=route_load_u32_be(block+i*4);
    for(i=16;i<80;i++) words[i]=route_rol32(words[i-3]^words[i-8]^words[i-14]^words[i-16],1);
    a=h0;b=h1;c=h2;d=h3;e=h4;
    for(i=0;i<80;i++) {
        if(i<20) { f=(b&c)|((~b)&d); constant=0x5a827999U; }
        else if(i<40) { f=b^c^d; constant=0x6ed9eba1U; }
        else if(i<60) { f=(b&c)|(b&d)|(c&d); constant=0x8f1bbcdcU; }
        else { f=b^c^d; constant=0xca62c1d6U; }
        next=route_rol32(a,5)+f+e+constant+words[i];
        e=d;d=c;c=route_rol32(b,30);b=a;a=next;
    }
    h0+=a;h1+=b;h2+=c;h3+=d;h4+=e;
    out[0]=(unsigned char)(h0>>24);out[1]=(unsigned char)(h0>>16);
    out[2]=(unsigned char)(h0>>8);out[3]=(unsigned char)h0;
    out[4]=(unsigned char)(h1>>24);out[5]=(unsigned char)(h1>>16);
    out[6]=(unsigned char)(h1>>8);out[7]=(unsigned char)h1;
    out[8]=(unsigned char)(h2>>24);out[9]=(unsigned char)(h2>>16);
    out[10]=(unsigned char)(h2>>8);out[11]=(unsigned char)h2;
    out[12]=(unsigned char)(h3>>24);out[13]=(unsigned char)(h3>>16);
    out[14]=(unsigned char)(h3>>8);out[15]=(unsigned char)h3;
    out[16]=(unsigned char)(h4>>24);out[17]=(unsigned char)(h4>>16);
    out[18]=(unsigned char)(h4>>8);out[19]=(unsigned char)h4;
    memset(block,0,sizeof(block));
    memset(words,0,sizeof(words));
}

static int route_lifecycle_valid(value)
character_save_journal_v2_route_lifecycle value;
{
    return value==CHARACTER_SAVE_JOURNAL_V2_ROUTE_IMPORTED_UNCLAIMED||
           value==CHARACTER_SAVE_JOURNAL_V2_ROUTE_PROVISIONING||
           value==CHARACTER_SAVE_JOURNAL_V2_ROUTE_ACTIVE;
}

static int route_reply_name_valid(reply, name, name_length)
const character_save_journal_v2_route_reply *reply;
const unsigned char *name;
size_t name_length;
{
    size_t i;
    if(reply->legacy_name_length!=name_length||
       reply->legacy_name_length>CHARACTER_SAVE_JOURNAL_V2_ROUTE_NAME_MAX||
       !route_canonical_name(reply->legacy_name,reply->legacy_name_length)||
       memcmp(reply->legacy_name,name,name_length)!=0) return 0;
    for(i=reply->legacy_name_length;i<sizeof(reply->legacy_name);i++) {
        if(reply->legacy_name[i]!=0) return 0;
    }
    return 1;
}

static int route_all_zero(bytes, length)
const char *bytes;
size_t length;
{
    size_t i;
    for(i=0;i<length;i++) if(bytes[i]) return 0;
    return 1;
}

static int route_zero_tail(bytes, length, limit)
const char *bytes;
size_t length;
size_t limit;
{
    size_t i;
    if(!bytes||length>limit||bytes[length]) return 0;
    for(i=length+1;i<=limit;i++) if(bytes[i]) return 0;
    return 1;
}

static character_save_journal_v2_route_error route_context_status(status)
character_save_journal_v2_writer_context_status status;
{
    if(status==CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_STALE)
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_STALE;
    if(status==CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_LOCK)
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_LOCK;
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID;
}

static int route_tuple_same(left, right)
const character_save_journal_v2_writer_tuple *left;
const character_save_journal_v2_writer_tuple *right;
{
    return !memcmp(left->world_id,right->world_id,sizeof(left->world_id))&&
           !memcmp(left->writer_instance_id,right->writer_instance_id,
                   sizeof(left->writer_instance_id))&&
           left->writer_epoch==right->writer_epoch;
}

character_save_journal_v2_route_error character_save_journal_v2_route_bind(
    context, canonical_legacy_name, canonical_legacy_name_length, lookup,
    lookup_opaque, out)
const character_save_journal_v2_writer_context *context;
const unsigned char *canonical_legacy_name;
size_t canonical_legacy_name_length;
character_save_journal_v2_route_lookup lookup;
void *lookup_opaque;
character_save_journal_v2_bound_route *out;
{
    character_save_journal_v2_writer_context_status context_status;
    character_save_journal_v2_writer_tuple tuple;
    character_save_journal_v2_writer_tuple after_tuple;
    character_save_journal_v2_route_reply reply;
    character_save_journal_v2_bound_route next;
    unsigned char sha1[20];
    static const char hex[]="0123456789abcdef";
    character_save_journal_v2_route_error error;
    character_save_journal_v2_route_lookup_result lookup_result;
    uint64_t held_generation;
    uint64_t after_generation;
    if(!out||!lookup||!route_canonical_name(canonical_legacy_name,
                                             canonical_legacy_name_length))
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_INVALID_ARGUMENT;
    context_status=character_save_journal_v2_writer_validate_held_for_route(
        context,&tuple,&held_generation);
    if(context_status!=CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK)
        return route_context_status(context_status);
    memset(&reply,0,sizeof(reply));
    lookup_result=lookup(lookup_opaque,tuple.world_id,canonical_legacy_name,
                         canonical_legacy_name_length,&reply);
    memset(&after_tuple,0,sizeof(after_tuple));
    context_status=character_save_journal_v2_writer_validate_held_for_route(
        context,&after_tuple,&after_generation);
    if(context_status!=CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK||
       after_generation!=held_generation||!route_tuple_same(&tuple,&after_tuple)) {
        error=context_status==CHARACTER_SAVE_JOURNAL_V2_WRITER_CONTEXT_OK?
            CHARACTER_SAVE_JOURNAL_V2_ROUTE_CONTEXT_INVALID:
            route_context_status(context_status);
        memset(&after_tuple,0,sizeof(after_tuple));
        memset(&reply,0,sizeof(reply));
        memset(&tuple,0,sizeof(tuple));
        return error;
    }
    memset(&after_tuple,0,sizeof(after_tuple));
    if(lookup_result!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_LOOKUP_OK) {
        memset(&reply,0,sizeof(reply));
        memset(&tuple,0,sizeof(tuple));
        return CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_FAILURE;
    }
    if(reply.status!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS_OK)
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_STATUS;
    else if(reply.row_count!=1) error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_CALLBACK_CARDINALITY;
    else if(route_bounded(reply.world_id,CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX)!=
            strlen(tuple.world_id)||!route_zero_tail(reply.world_id,
            strlen(tuple.world_id),CHARACTER_SAVE_JOURNAL_V2_WRITER_WORLD_MAX)||
            strcmp(reply.world_id,tuple.world_id)!=0)
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_WORLD;
    else if(!route_uuid(reply.character_id))
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_CHARACTER_ID;
    else if(!route_reply_name_valid(&reply,canonical_legacy_name,
                                    canonical_legacy_name_length))
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_NAME;
    else if(!route_lower_hex(reply.legacy_shard,2))
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_SHARD;
    else if(reply.storage_format!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_STORAGE_LEGACY_C_ABI_V1)
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_FORMAT;
    else if(!route_lifecycle_valid(reply.lifecycle))
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_LIFECYCLE;
    else if(reply.has_imported_file_sha256!=0&&reply.has_imported_file_sha256!=1)
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_IMPORTED_HASH;
    else if(reply.has_imported_file_sha256&&
            !route_lower_hex(reply.imported_file_sha256,
                             CHARACTER_SAVE_JOURNAL_V2_ROUTE_HASH_HEX_LEN))
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_IMPORTED_HASH;
    else if(!reply.has_imported_file_sha256&&
            !route_all_zero(reply.imported_file_sha256,
                            sizeof(reply.imported_file_sha256)))
        error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_IMPORTED_HASH;
    else {
        route_sha1(canonical_legacy_name,canonical_legacy_name_length,sha1);
        if(reply.legacy_shard[0]!=hex[sha1[0]>>4]||
           reply.legacy_shard[1]!=hex[sha1[0]&15])
            error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_REPLY_SHARD;
        else error=CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK;
    }
    if(error!=CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK) {
        memset(sha1,0,sizeof(sha1));
        memset(&reply,0,sizeof(reply));
        memset(&tuple,0,sizeof(tuple));
        return error;
    }
    memset(&next,0,sizeof(next));
    memcpy(next.world_id,reply.world_id,sizeof(next.world_id));
    memcpy(next.character_id,reply.character_id,sizeof(next.character_id));
    memcpy(next.legacy_name,reply.legacy_name,sizeof(next.legacy_name));
    next.legacy_name_length=reply.legacy_name_length;
    memcpy(next.legacy_shard,reply.legacy_shard,sizeof(next.legacy_shard));
    next.storage_format=reply.storage_format;
    next.lifecycle=reply.lifecycle;
    next.has_imported_file_sha256=reply.has_imported_file_sha256;
    memcpy(next.imported_file_sha256,reply.imported_file_sha256,
           sizeof(next.imported_file_sha256));
    *out=next;
    memset(sha1,0,sizeof(sha1));
    memset(&reply,0,sizeof(reply));
    memset(&next,0,sizeof(next));
    memset(&tuple,0,sizeof(tuple));
    return CHARACTER_SAVE_JOURNAL_V2_ROUTE_OK;
}
