/* Bounded, metadata-only observation of one raw legacy bank record.  The
 * legacy reader is intentionally not called: it can recurse and terminate on
 * allocation failure.  This small parser only recreates a detached graph,
 * then delegates semantic validation and topology bounds to BankSnapshotV1. */
#include "bank_evidence.h"

#include "bank_snapshot_v1.h"
#include "bank_store.h"
#include "cdto_v1.h"
#include "player_path.h"

#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "bank evidence requires O_NOFOLLOW"
#endif

typedef struct be_sha256 {
    uint32_t state[8], bit_hi, bit_lo;
    unsigned char block[64];
    unsigned int used;
} be_sha256;

typedef struct be_parse {
    const unsigned char *cursor;
    const unsigned char *end;
    uint32_t nodes;
    uint32_t max_depth;
} be_parse;

#ifdef BANK_EVIDENCE_TESTING
static int be_test_fault;
static void (*be_test_after_read)(void *);
static void *be_test_after_read_opaque;
void bank_evidence_test_fail_next(fault) int fault;
{ be_test_fault=fault; }
void bank_evidence_test_set_after_read(callback,opaque)
void (*callback)(void *); void *opaque;
{ be_test_after_read=callback; be_test_after_read_opaque=opaque; }
void bank_evidence_test_reset(void)
{ be_test_fault=0; be_test_after_read=0; be_test_after_read_opaque=0; }
static int be_fault(fault) int fault;
{ if(be_test_fault!=fault)return 0;be_test_fault=0;errno=EIO;return 1; }
#else
static int be_fault(fault) int fault;
{ (void)fault; return 0; }
#endif

static void *be_malloc(size) size_t size;
{ if(be_fault(1))return 0; return malloc(size); }
static ssize_t be_read(fd,buffer,length) int fd; void *buffer; size_t length;
{ if(be_fault(2))return -1; return read(fd,buffer,length); }
static int be_close(fd) int fd;
{ int result=close(fd); if(be_fault(3))return -1; return result; }

static void be_reset(out,result) bank_evidence *out; bank_evidence_result result;
{
    memset(out,0,sizeof(*out));
    out->version=BANK_EVIDENCE_VERSION;
    out->result=result;
}

static int be_name_valid(name) const char *name;
{
    return name&&player_name_is_valid((const unsigned char *)name,
        PLAYER_NAME_MIN_CODEPOINTS,PLAYER_NAME_MAX_CODEPOINTS);
}

static int be_file_safe(value) const struct stat *value;
{
    return value && S_ISREG(value->st_mode) && value->st_nlink==1 &&
        value->st_uid==geteuid() && (value->st_mode&07777)==0600 &&
        value->st_size>=0 && (uint64_t)value->st_size<=BANK_EVIDENCE_MAX_OCTETS;
}

static int be_same_file(left,right) const struct stat *left; const struct stat *right;
{
    if(!left||!right||left->st_dev!=right->st_dev||left->st_ino!=right->st_ino||
       left->st_mode!=right->st_mode||left->st_uid!=right->st_uid||
       left->st_nlink!=right->st_nlink||left->st_size!=right->st_size||
       left->st_mtime!=right->st_mtime||left->st_ctime!=right->st_ctime)return 0;
#if defined(__APPLE__)
    return left->st_mtimespec.tv_nsec==right->st_mtimespec.tv_nsec&&
        left->st_ctimespec.tv_nsec==right->st_ctimespec.tv_nsec;
#elif defined(__linux__) || defined(__FreeBSD__) || defined(__NetBSD__) || defined(__OpenBSD__)
    return left->st_mtim.tv_nsec==right->st_mtim.tv_nsec&&
        left->st_ctim.tv_nsec==right->st_ctim.tv_nsec;
#else
    return 1;
#endif
}

static uint32_t be_rotr(value,shift) uint32_t value; unsigned int shift;
{ return (value>>shift)|(value<<(32U-shift)); }
static uint32_t be_load32(value) const unsigned char *value;
{ return ((uint32_t)value[0]<<24)|((uint32_t)value[1]<<16)|((uint32_t)value[2]<<8)|value[3]; }
static void be_store32(value,out) uint32_t value; unsigned char *out;
{ out[0]=(unsigned char)(value>>24);out[1]=(unsigned char)(value>>16);out[2]=(unsigned char)(value>>8);out[3]=(unsigned char)value; }
static void be_sha_block(ctx,block) be_sha256 *ctx; const unsigned char *block;
{
    static const uint32_t k[64]={
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U};
    uint32_t w[64],a,b,c,d,e,f,g,h,t1,t2;
    unsigned int i;
    for(i=0;i<16;i++)w[i]=be_load32(block+4*i);
    for(i=16;i<64;i++)w[i]=w[i-16]+(be_rotr(w[i-15],7)^be_rotr(w[i-15],18)^(w[i-15]>>3))+w[i-7]+(be_rotr(w[i-2],17)^be_rotr(w[i-2],19)^(w[i-2]>>10));
    a=ctx->state[0];b=ctx->state[1];c=ctx->state[2];d=ctx->state[3];e=ctx->state[4];f=ctx->state[5];g=ctx->state[6];h=ctx->state[7];
    for(i=0;i<64;i++){t1=h+(be_rotr(e,6)^be_rotr(e,11)^be_rotr(e,25))+((e&f)^((~e)&g))+k[i]+w[i];t2=(be_rotr(a,2)^be_rotr(a,13)^be_rotr(a,22))+((a&b)^(a&c)^(b&c));h=g;g=f;f=e;e=d+t1;d=c;c=b;b=a;a=t1+t2;}
    ctx->state[0]+=a;ctx->state[1]+=b;ctx->state[2]+=c;ctx->state[3]+=d;ctx->state[4]+=e;ctx->state[5]+=f;ctx->state[6]+=g;ctx->state[7]+=h;
}
static void be_sha_init(ctx) be_sha256 *ctx;
{ ctx->state[0]=0x6a09e667U;ctx->state[1]=0xbb67ae85U;ctx->state[2]=0x3c6ef372U;ctx->state[3]=0xa54ff53aU;ctx->state[4]=0x510e527fU;ctx->state[5]=0x9b05688cU;ctx->state[6]=0x1f83d9abU;ctx->state[7]=0x5be0cd19U;ctx->bit_hi=ctx->bit_lo=ctx->used=0; }
static void be_sha_update(ctx,bytes,length) be_sha256 *ctx; const unsigned char *bytes; size_t length;
{
    unsigned int take; uint32_t old;
    while(length){take=64U-ctx->used;if(take>length)take=(unsigned int)length;memcpy(ctx->block+ctx->used,bytes,take);ctx->used+=take;bytes+=take;length-=take;old=ctx->bit_lo;ctx->bit_lo+=(uint32_t)take<<3;if(ctx->bit_lo<old)ctx->bit_hi++;ctx->bit_hi+=(uint32_t)take>>29;if(ctx->used==64U){be_sha_block(ctx,ctx->block);ctx->used=0;}}
}
static void be_sha_hex(ctx,out) be_sha256 *ctx; char out[65];
{
    static const char hex[]="0123456789abcdef"; unsigned char digest[32]; unsigned int i;
    ctx->block[ctx->used++]=0x80;if(ctx->used>56U){while(ctx->used<64U)ctx->block[ctx->used++]=0;be_sha_block(ctx,ctx->block);ctx->used=0;}while(ctx->used<56U)ctx->block[ctx->used++]=0;be_store32(ctx->bit_hi,ctx->block+56);be_store32(ctx->bit_lo,ctx->block+60);be_sha_block(ctx,ctx->block);for(i=0;i<8;i++)be_store32(ctx->state[i],digest+4*i);for(i=0;i<32;i++){out[2*i]=hex[digest[i]>>4];out[2*i+1]=hex[digest[i]&15U];}out[64]=0;
}

static void be_free_graph(tag) otag *tag;
{
    otag *next;
    while(tag){next=tag->next_tag;if(tag->obj){be_free_graph(tag->obj->first_obj);free(tag->obj);}free(tag);tag=next;}
}

static bank_evidence_result be_parse_node(parse,parent,depth,out)
be_parse *parse; object *parent; uint32_t depth; otag **out;
{
    object *value; otag *tag,**tail,*child; int count,i; size_t remain;
    bank_evidence_result result;
    *out=0;
    if(depth>OBJECT_GRAPH_V1_MAX_DEPTH)return BANK_EVIDENCE_LIMIT;
    if(parse->nodes==OBJECT_GRAPH_V1_MAX_NODES)return BANK_EVIDENCE_LIMIT;
    remain=(size_t)(parse->end-parse->cursor);
    if(remain<sizeof(*value)+sizeof(count))return BANK_EVIDENCE_CORRUPT;
    value=(object *)be_malloc(sizeof(*value));
    tag=(otag *)be_malloc(sizeof(*tag));
    if(!value||!tag){free(value);free(tag);return BANK_EVIDENCE_IO_ERROR;}
    memcpy(value,parse->cursor,sizeof(*value));parse->cursor+=sizeof(*value);
    memcpy(&count,parse->cursor,sizeof(count));parse->cursor+=sizeof(count);
    value->first_obj=0;value->parent_obj=parent;value->parent_rom=0;value->parent_crt=0;
    tag->obj=value;tag->next_tag=0;*out=tag;
    if(count<0){result=BANK_EVIDENCE_CORRUPT;goto failed;}
    if(count>(int)OBJECT_GRAPH_V1_PLAYER_MAX_LIST_ITEMS){result=BANK_EVIDENCE_LIMIT;goto failed;}
    ++parse->nodes;if(depth>parse->max_depth)parse->max_depth=depth;
    tail=&value->first_obj;
    for(i=0;i<count;++i){
        result=be_parse_node(parse,value,depth+1U,&child);
        if(result!=BANK_EVIDENCE_OK)goto failed;
        *tail=child;tail=&child->next_tag;
    }
    return BANK_EVIDENCE_OK;
failed:
    be_free_graph(tag);
    *out=0;
    return result;
}

bank_evidence_result bank_evidence_inspect(name,out)
const char *name; bank_evidence *out;
{
    int fd,close_result,encode_result;
    struct stat before,after;
    unsigned char *bytes;
    size_t length,used;
    ssize_t amount;
    otag *root;
    uint8_t *wire;
    size_t wire_length;
    be_parse parse;
    bank_evidence_result result;
    be_sha256 sha;

    if(!out)return BANK_EVIDENCE_IO_ERROR;
    be_reset(out,BANK_EVIDENCE_CORRUPT);
    if(!be_name_valid(name)){out->result=BANK_EVIDENCE_INVALID_INPUT;return out->result;}
    fd=file_bank_store_open_readonly(name);
    if(fd<0){out->result=errno==ENOENT?BANK_EVIDENCE_NOT_FOUND:BANK_EVIDENCE_IO_ERROR;return out->result;}
    bytes=0;root=0;wire=0;wire_length=0U;result=BANK_EVIDENCE_IO_ERROR;
    if(fstat(fd,&before)<0||!be_file_safe(&before))goto done;
    length=(size_t)before.st_size;
    if(!length){result=BANK_EVIDENCE_CORRUPT;goto done;}
    bytes=(unsigned char *)be_malloc(length);
    if(!bytes)goto done;
    used=0U;
    while(used<length){amount=be_read(fd,bytes+used,length-used);if(amount<0&&errno==EINTR)continue;if(amount<=0)goto done;used+=(size_t)amount;}
#ifdef BANK_EVIDENCE_TESTING
    if(be_test_after_read)be_test_after_read(be_test_after_read_opaque);
#endif
    if(fstat(fd,&after)<0||!be_file_safe(&after)||!be_same_file(&before,&after)||
       file_bank_store_validate_open_readonly(name,fd)<0)goto done;
    close_result=be_close(fd);fd=-1;
    if(close_result<0)goto done;
    parse.cursor=bytes;parse.end=bytes+length;parse.nodes=0U;parse.max_depth=0U;
    result=be_parse_node(&parse,0,0U,&root);
    if(result!=BANK_EVIDENCE_OK)goto done;
    if(parse.cursor!=parse.end){result=BANK_EVIDENCE_CORRUPT;goto done;}
    if(be_fault(1)){result=BANK_EVIDENCE_IO_ERROR;goto done;}
    encode_result=bank_snapshot_v1_encode(root,&wire,&wire_length);
    if(encode_result!=CDTO_V1_OK){result=encode_result==CDTO_V1_SIZE_LIMIT_EXCEEDED?BANK_EVIDENCE_LIMIT:BANK_EVIDENCE_CORRUPT;goto done;}
    be_sha_init(&sha);be_sha_update(&sha,bytes,length);be_sha_hex(&sha,out->sha256);
    out->octet_count=(uint64_t)length;out->item_count=parse.nodes-1U;out->max_depth=parse.max_depth;result=BANK_EVIDENCE_OK;
done:
    if(fd>=0&&be_close(fd)<0)result=BANK_EVIDENCE_IO_ERROR;
    bank_snapshot_v1_free_wire(wire);be_free_graph(root);free(bytes);
    if(result!=BANK_EVIDENCE_OK)be_reset(out,result);
    else out->result=BANK_EVIDENCE_OK;
    return out->result;
}
