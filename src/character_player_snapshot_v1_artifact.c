/* Immutable, descriptor-relative player snapshot evidence.  It deliberately
 * remains a test-only boundary: production's live OBJECTS must not link it. */
#include "character_player_snapshot_v1_artifact.h"

#include "cdto_v1.h"
#include "player_snapshot_v1.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_NOFOLLOW
#error "O_NOFOLLOW is required"
#endif

#define CPSA_HEADER_MAX 2048U
#define CPSA_FILE_MAX (CPSA_HEADER_MAX + CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS)
#define CPSA_SUFFIX ".player-snapshot-v1"

#ifdef CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TESTING
static int cpsa_test_fault;
void character_player_snapshot_v1_artifact_test_fail_next(fault)
int fault;
{ cpsa_test_fault = fault; }
void character_player_snapshot_v1_artifact_test_reset_faults(void)
{ cpsa_test_fault = 0; }
static int cpsa_fault(fault)
int fault;
{
    if(cpsa_test_fault != fault) return 0;
    cpsa_test_fault = 0;
    errno = EIO;
    return 1;
}
#else
static int cpsa_fault(fault)
int fault;
{ (void)fault; return 0; }
#endif

static unsigned long cpsa_bounded(value, maximum)
const char *value;
unsigned long maximum;
{
    unsigned long index;
    if(!value) return maximum + 1U;
    for(index = 0; index <= maximum; ++index) if(!value[index]) return index;
    return maximum + 1U;
}

static int cpsa_hex(value)
char value;
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }

static int cpsa_uuid(value)
const char *value;
{
    unsigned long index;
    if(cpsa_bounded(value, CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH) !=
       CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH) return 0;
    for(index = 0; index < CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH; ++index)
        if(index == 8U || index == 13U || index == 18U || index == 23U) {
            if(value[index] != '-') return 0;
        } else if(!cpsa_hex(value[index])) return 0;
    return 1;
}

static int cpsa_hash(value)
const char *value;
{
    unsigned long index;
    if(cpsa_bounded(value, CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH) !=
       CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH) return 0;
    for(index = 0; index < CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH; ++index)
        if(!cpsa_hex(value[index])) return 0;
    return 1;
}

static int cpsa_world(value)
const char *value;
{
    unsigned long index, length;
    length = cpsa_bounded(value, CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEXT_MAX);
    if(!length || length > CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_TEXT_MAX ||
       value[0] < 'a' || value[0] > 'z') return 0;
    for(index = 1U; index < length; ++index)
        if(!((value[index] >= 'a' && value[index] <= 'z') ||
             (value[index] >= '0' && value[index] <= '9') ||
             value[index] == '_' || value[index] == '-')) return 0;
    return 1;
}

static int cpsa_name_hex(value)
const char *value;
{
    unsigned long index, length;
    length = cpsa_bounded(value, 28U);
    if(length < 2U || length > 28U || (length & 1U)) return 0;
    for(index = 0; index < length; ++index) if(!cpsa_hex(value[index])) return 0;
    return 1;
}

static int cpsa_metadata_valid(value, snapshots)
const character_player_snapshot_v1_artifact_metadata *value;
int snapshots;
{
    if(!value || !cpsa_world(value->world_id) || !cpsa_uuid(value->character_id) ||
       !cpsa_uuid(value->command_id) || !cpsa_uuid(value->writer_instance_id) ||
       !cpsa_name_hex(value->canonical_name_hex) || !cpsa_hash(value->request_sha256) ||
       !cpsa_hash(value->source_post_sha256) ||
       cpsa_bounded(value->snapshot_format,
       sizeof(CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT) - 1U) !=
       sizeof(CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT) - 1U ||
       strcmp(value->snapshot_format, CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FORMAT) ||
       !value->writer_epoch || !value->writer_revision || !value->source_octets ||
       value->writer_epoch > INT64_MAX || value->writer_revision > INT64_MAX ||
       value->source_octets > INT64_MAX || value->storage_format <= 0) return 0;
    if(snapshots && (!cpsa_hash(value->snapshot_sha256) || !value->snapshot_octets ||
       value->snapshot_octets > CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS))
        return 0;
    return 1;
}

static int cpsa_close(fd, directory)
int fd;
int directory;
{
    int result;
    result = close(fd);
    if(cpsa_fault(directory ? 6 : 3)) return -1;
    return result;
}

static int cpsa_fsync(fd, directory)
int fd;
int directory;
{
    if(cpsa_fault(directory ? 4 : 2)) return -1;
    return fsync(fd);
}

static void *cpsa_malloc(size)
size_t size;
{
    if(cpsa_fault(5)) return 0;
    return malloc(size);
}

static int cpsa_root_duplicate(directory_fd)
int directory_fd;
{
    int duplicate;
    struct stat metadata;
    if(directory_fd < 0) return -1;
    duplicate = fcntl(directory_fd, F_DUPFD_CLOEXEC, 3);
    if(duplicate < 0) return -1;
    if(fstat(duplicate, &metadata) < 0 || !S_ISDIR(metadata.st_mode) ||
       metadata.st_uid != geteuid() || (metadata.st_mode & 0777) != 0700) {
        cpsa_close(duplicate, 1);
        return -1;
    }
    return duplicate;
}

static int cpsa_file_safe(fd, directory, before)
int fd;
const struct stat *directory;
struct stat *before;
{
    if(fstat(fd, before) < 0 || !S_ISREG(before->st_mode) || before->st_nlink != 1 ||
       before->st_uid != directory->st_uid || (before->st_mode & 0777) != 0600 ||
       before->st_size < 0 || (uint64_t)before->st_size > CPSA_FILE_MAX) return -1;
    return 0;
}

static int cpsa_same_file(left, right)
const struct stat *left;
const struct stat *right;
{
    return left->st_dev == right->st_dev && left->st_ino == right->st_ino &&
       left->st_mode == right->st_mode && left->st_uid == right->st_uid &&
       left->st_nlink == right->st_nlink && left->st_size == right->st_size;
}

static int cpsa_write_all(fd, bytes, length)
int fd;
const uint8_t *bytes;
size_t length;
{
    ssize_t amount;
    while(length) {
        if(cpsa_fault(1)) return -1;
        amount = write(fd, bytes, length);
        if(amount < 0 && errno == EINTR) continue;
        if(amount <= 0) return -1;
        bytes += amount;
        length -= (size_t)amount;
    }
    return 0;
}

/* SHA-256 is deliberately local: CDTO's digest helper is not public. */
typedef struct cpsa_sha256 { uint32_t h[8]; uint64_t bits; uint8_t block[64]; size_t used; } cpsa_sha256;
static uint32_t cpsa_rotr(value, count) uint32_t value; unsigned int count;
{ return (value >> count) | (value << (32U - count)); }
static void cpsa_sha_block(context, block) cpsa_sha256 *context; const uint8_t *block;
{
    static const uint32_t constants[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U };
    uint32_t words[64], a,b,c,d,e,f,g,h,t1,t2;
    unsigned int index;
    for(index=0; index<16U; ++index) words[index]=((uint32_t)block[index*4U]<<24)|((uint32_t)block[index*4U+1U]<<16)|((uint32_t)block[index*4U+2U]<<8)|block[index*4U+3U];
    for(index=16U; index<64U; ++index) words[index]=words[index-16U]+(cpsa_rotr(words[index-15U],7)^cpsa_rotr(words[index-15U],18)^(words[index-15U]>>3))+words[index-7U]+(cpsa_rotr(words[index-2U],17)^cpsa_rotr(words[index-2U],19)^(words[index-2U]>>10));
    a=context->h[0]; b=context->h[1]; c=context->h[2]; d=context->h[3]; e=context->h[4]; f=context->h[5]; g=context->h[6]; h=context->h[7];
    for(index=0; index<64U; ++index) { t1=h+(cpsa_rotr(e,6)^cpsa_rotr(e,11)^cpsa_rotr(e,25))+((e&f)^((~e)&g))+constants[index]+words[index]; t2=(cpsa_rotr(a,2)^cpsa_rotr(a,13)^cpsa_rotr(a,22))+((a&b)^(a&c)^(b&c)); h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2; }
    context->h[0]+=a; context->h[1]+=b; context->h[2]+=c; context->h[3]+=d; context->h[4]+=e; context->h[5]+=f; context->h[6]+=g; context->h[7]+=h;
}
static void cpsa_sha_digest(bytes, length, output) const uint8_t *bytes; size_t length; uint8_t output[32];
{
    cpsa_sha256 context; uint64_t bits; unsigned int index;
    context.h[0]=0x6a09e667U; context.h[1]=0xbb67ae85U; context.h[2]=0x3c6ef372U; context.h[3]=0xa54ff53aU; context.h[4]=0x510e527fU; context.h[5]=0x9b05688cU; context.h[6]=0x1f83d9abU; context.h[7]=0x5be0cd19U; context.bits=0; context.used=0;
    while(length) { size_t take=64U-context.used; if(take>length) take=length; memcpy(context.block+context.used,bytes,take); context.used+=take; bytes+=take; length-=take; context.bits+=(uint64_t)take*8U; if(context.used==64U) { cpsa_sha_block(&context,context.block); context.used=0; } }
    bits=context.bits; context.block[context.used++]=0x80U; if(context.used>56U) { while(context.used<64U) context.block[context.used++]=0; cpsa_sha_block(&context,context.block); context.used=0; } while(context.used<56U) context.block[context.used++]=0; for(index=0; index<8U; ++index) context.block[63U-index]=(uint8_t)(bits>>(index*8U)); cpsa_sha_block(&context,context.block);
    for(index=0; index<8U; ++index) { output[index*4U]=(uint8_t)(context.h[index]>>24); output[index*4U+1U]=(uint8_t)(context.h[index]>>16); output[index*4U+2U]=(uint8_t)(context.h[index]>>8); output[index*4U+3U]=(uint8_t)context.h[index]; }
}
static void cpsa_hex_digest(bytes, length, output) const uint8_t *bytes; size_t length; char output[65];
{ static const char digits[]="0123456789abcdef"; uint8_t digest[32]; unsigned int index; cpsa_sha_digest(bytes,length,digest); for(index=0;index<32U;++index) { output[index*2U]=digits[digest[index]>>4]; output[index*2U+1U]=digits[digest[index]&15U]; } output[64]=0; }

static int cpsa_canonical_snapshot(snapshot, length)
const uint8_t *snapshot;
size_t length;
{
    creature *decoded;
    uint8_t *encoded;
    size_t encoded_length;
    int result;
    if(!snapshot || !length || length > CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_MAX_SNAPSHOT_OCTETS)
        return -1;
    decoded=0;
    encoded=0;
    encoded_length=0;
    if(player_snapshot_v1_decode_clone(snapshot,length,&decoded)!=CDTO_V1_OK||
       !decoded) return -1;
    result=player_snapshot_v1_encode_loaded(decoded,&encoded,&encoded_length);
    player_snapshot_v1_free_clone(decoded);
    if(result!=CDTO_V1_OK||!encoded||encoded_length!=length||
       memcmp(encoded,snapshot,length)) {
        cdto_v1_free_wire(encoded);
        return -1;
    }
    cdto_v1_free_wire(encoded);
    return 0;
}

static int cpsa_number(value, length, output)
const char *value;
unsigned long length;
uint64_t *output;
{
    uint64_t parsed; unsigned long index;
    if(!length || length>19U || value[0]=='0') return -1;
    parsed=0;
    for(index=0;index<length;++index) { if(value[index]<'0'||value[index]>'9'||parsed>((uint64_t)INT64_MAX-(uint64_t)(value[index]-'0'))/10U) return -1; parsed=parsed*10U+(uint64_t)(value[index]-'0'); }
    if(!parsed) return -1;
    *output=parsed;
    return 0;
}
static int cpsa_text(destination, capacity, value, length)
char *destination; unsigned long capacity; const char *value; unsigned long length;
{ if(length>=capacity) return -1; memcpy(destination,value,length); destination[length]=0; return 0; }
static int cpsa_line(cursor, remaining, expected, value, value_length)
const char **cursor; unsigned long *remaining; const char *expected; const char **value; unsigned long *value_length;
{
    const char *newline; unsigned long prefix=(unsigned long)strlen(expected), consumed;
    if(*remaining<prefix+2U || memcmp(*cursor,expected,prefix) || (*cursor)[prefix]!='=') return -1;
    newline=(const char *)memchr(*cursor+prefix+1U,'\n',*remaining-prefix-1U); if(!newline) return -1;
    *value=*cursor+prefix+1U; *value_length=(unsigned long)(newline-*value);
    consumed=(unsigned long)(newline-*cursor)+1U;
    *cursor=newline+1U; *remaining-=consumed;
    return 0;
}

static int cpsa_header(output, capacity, value)
char *output;
size_t capacity;
const character_player_snapshot_v1_artifact_metadata *value;
{
    int length;
    length=snprintf(output,capacity,
        "version=1\nworld_id=%s\ncharacter_id=%s\ncommand_id=%s\n"
        "canonical_name_hex=%s\nrequest_sha256=%s\nsource_post_sha256=%s\n"
        "writer_instance_id=%s\nwriter_epoch=%llu\nwriter_revision=%llu\n"
        "storage_format=%d\nsnapshot_format=%s\nsource_octets=%llu\n"
        "snapshot_sha256=%s\nsnapshot_octets=%llu\n\n",
        value->world_id,value->character_id,value->command_id,value->canonical_name_hex,
        value->request_sha256,value->source_post_sha256,value->writer_instance_id,
        (unsigned long long)value->writer_epoch,(unsigned long long)value->writer_revision,
        (int)value->storage_format,value->snapshot_format,
        (unsigned long long)value->source_octets,value->snapshot_sha256,
        (unsigned long long)value->snapshot_octets);
    return length<0 || (size_t)length>=capacity ? -1 : length;
}

static int cpsa_parse_header(bytes, length, output, header_length)
const uint8_t *bytes;
size_t length;
character_player_snapshot_v1_artifact_metadata *output;
size_t *header_length;
{
    static const char *names[] = { "version", "world_id", "character_id",
        "command_id", "canonical_name_hex", "request_sha256",
        "source_post_sha256", "writer_instance_id", "writer_epoch",
        "writer_revision", "storage_format", "snapshot_format", "source_octets",
        "snapshot_sha256", "snapshot_octets" };
    const char *cursor,*value; unsigned long remaining,value_length,index;
    uint64_t number; char canonical[CPSA_HEADER_MAX]; int canonical_length;
    size_t boundary;
    if(!bytes || !output || !header_length || length<3U || length>CPSA_FILE_MAX) return -1;
    for(boundary=0;boundary+1U<length;++boundary) if(bytes[boundary]=='\n'&&bytes[boundary+1U]=='\n') break;
    if(boundary+1U>=length || boundary+2U>CPSA_HEADER_MAX || memchr(bytes,0,boundary+2U)) return -1;
    memset(output,0,sizeof(*output)); cursor=(const char *)bytes; remaining=(unsigned long)(boundary+1U);
    for(index=0;index<15U;++index) {
        if(cpsa_line(&cursor,&remaining,names[index],&value,&value_length)) return -1;
        if(index==0U) { if(value_length!=1U||value[0]!='1') return -1; }
        else if(index==1U) { if(cpsa_text(output->world_id,sizeof(output->world_id),value,value_length)) return -1; }
        else if(index==2U) { if(cpsa_text(output->character_id,sizeof(output->character_id),value,value_length)) return -1; }
        else if(index==3U) { if(cpsa_text(output->command_id,sizeof(output->command_id),value,value_length)) return -1; }
        else if(index==4U) { if(cpsa_text(output->canonical_name_hex,sizeof(output->canonical_name_hex),value,value_length)) return -1; }
        else if(index==5U) { if(cpsa_text(output->request_sha256,sizeof(output->request_sha256),value,value_length)) return -1; }
        else if(index==6U) { if(cpsa_text(output->source_post_sha256,sizeof(output->source_post_sha256),value,value_length)) return -1; }
        else if(index==7U) { if(cpsa_text(output->writer_instance_id,sizeof(output->writer_instance_id),value,value_length)) return -1; }
        else if(index==8U) { if(cpsa_number(value,value_length,&number)) return -1; output->writer_epoch=number; }
        else if(index==9U) { if(cpsa_number(value,value_length,&number)) return -1; output->writer_revision=number; }
        else if(index==10U) { if(cpsa_number(value,value_length,&number)||number>(uint64_t)INT16_MAX) return -1; output->storage_format=(int16_t)number; }
        else if(index==11U) { if(cpsa_text(output->snapshot_format,sizeof(output->snapshot_format),value,value_length)) return -1; }
        else if(index==12U) { if(cpsa_number(value,value_length,&number)) return -1; output->source_octets=number; }
        else if(index==13U) { if(cpsa_text(output->snapshot_sha256,sizeof(output->snapshot_sha256),value,value_length)) return -1; }
        else { if(cpsa_number(value,value_length,&number)) return -1; output->snapshot_octets=number; }
    }
    if(remaining || !cpsa_metadata_valid(output,1)) return -1;
    canonical_length=cpsa_header(canonical,sizeof(canonical),output);
    if(canonical_length<0 || (size_t)canonical_length!=boundary+2U ||
       memcmp(canonical,bytes,boundary+2U)) return -1;
    *header_length=boundary+2U;
    return 0;
}

static int cpsa_read_existing(directory_fd, name, metadata, snapshot, snapshot_length)
int directory_fd;
const char *name;
character_player_snapshot_v1_artifact_metadata *metadata;
uint8_t **snapshot;
size_t *snapshot_length;
{
    int fd,result; ssize_t amount; struct stat directory,before,after; uint8_t *bytes;
    size_t used,header_length;
    *snapshot=0; *snapshot_length=0U;
    if(fstat(directory_fd,&directory)<0) return -1;
    fd=openat(directory_fd,name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
    if(fd<0) return errno==ENOENT ? -2 : -1;
    result=cpsa_file_safe(fd,&directory,&before); bytes=0; used=0;
    if(!result && !(bytes=(uint8_t *)cpsa_malloc((size_t)before.st_size+1U))) result=-1;
    while(!result && used<(size_t)before.st_size) { amount=read(fd,bytes+used,(size_t)before.st_size-used); if(amount<0&&errno==EINTR) continue; if(amount<=0) { result=-1; break; } used+=(size_t)amount; }
    if(!result) { do amount=read(fd,bytes+used,1U); while(amount<0&&errno==EINTR); if(amount!=0||fstat(fd,&after)<0||!cpsa_same_file(&before,&after)) result=-1; }
    if(cpsa_close(fd,0)<0) result=-1;
    if(result) { free(bytes); return -1; }
    if(cpsa_parse_header(bytes,used,metadata,&header_length) ||
       metadata->snapshot_octets != (uint64_t)(used-header_length) ||
       cpsa_canonical_snapshot(bytes+header_length,used-header_length)) { free(bytes); return 1; }
    {
        char digest[65]; cpsa_hex_digest(bytes+header_length,used-header_length,digest);
        if(strcmp(digest,metadata->snapshot_sha256)) { free(bytes); return 1; }
    }
    memmove(bytes,bytes+header_length,used-header_length);
    *snapshot=bytes; *snapshot_length=used-header_length;
    return 0;
}

int character_player_snapshot_v1_artifact_filename(metadata, output, output_size)
const character_player_snapshot_v1_artifact_metadata *metadata;
char *output;
size_t output_size;
{
    int length;
    if(!metadata || !output || !output_size || !cpsa_uuid(metadata->command_id)) return -1;
    length=snprintf(output,output_size,"%s%s",metadata->command_id,CPSA_SUFFIX);
    return length<0 || (size_t)length>=output_size ? -1 : 0;
}

static int cpsa_equal(left,right)
const character_player_snapshot_v1_artifact_metadata *left;
const character_player_snapshot_v1_artifact_metadata *right;
{
    return !strcmp(left->world_id,right->world_id) && !strcmp(left->character_id,right->character_id) &&
       !strcmp(left->command_id,right->command_id) && !strcmp(left->canonical_name_hex,right->canonical_name_hex) &&
       !strcmp(left->request_sha256,right->request_sha256) && !strcmp(left->source_post_sha256,right->source_post_sha256) &&
       !strcmp(left->writer_instance_id,right->writer_instance_id) && !strcmp(left->snapshot_sha256,right->snapshot_sha256) &&
       !strcmp(left->snapshot_format,right->snapshot_format) && left->writer_epoch==right->writer_epoch &&
       left->writer_revision==right->writer_revision && left->storage_format==right->storage_format &&
       left->source_octets==right->source_octets && left->snapshot_octets==right->snapshot_octets;
}

int character_player_snapshot_v1_artifact_store(directory_fd, metadata, snapshot, snapshot_length)
int directory_fd;
character_player_snapshot_v1_artifact_metadata *metadata;
const uint8_t *snapshot;
size_t snapshot_length;
{
    int root,fd,result,close_result,header_length,read_result;
    char name[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE],header[CPSA_HEADER_MAX],digest[65];
    struct stat directory,created,sealed,published;
    character_player_snapshot_v1_artifact_metadata existing;
    uint8_t *existing_snapshot;
    size_t existing_length;
    if(!cpsa_metadata_valid(metadata,0) || cpsa_canonical_snapshot(snapshot,snapshot_length) ||
       character_player_snapshot_v1_artifact_filename(metadata,name,sizeof(name))) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID;
    cpsa_hex_digest(snapshot,snapshot_length,digest);
    if(metadata->snapshot_sha256[0] && (!cpsa_hash(metadata->snapshot_sha256)||strcmp(metadata->snapshot_sha256,digest))) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID;
    if(metadata->snapshot_octets && metadata->snapshot_octets!=(uint64_t)snapshot_length) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID;
    strcpy(metadata->snapshot_sha256,digest); metadata->snapshot_octets=(uint64_t)snapshot_length;
    if(!cpsa_metadata_valid(metadata,1) || (header_length=cpsa_header(header,sizeof(header),metadata))<0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID;
    root=cpsa_root_duplicate(directory_fd); if(root<0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR;
    fd=openat(root,name,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC,0600);
    if(fd<0) {
        if(errno!=EEXIST) { cpsa_close(root,1); return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR; }
        existing_snapshot=0; existing_length=0U; memset(&existing,0,sizeof(existing));
        read_result=cpsa_read_existing(root,name,&existing,&existing_snapshot,&existing_length);
        close_result=cpsa_close(root,1); if(close_result<0) { free(existing_snapshot); return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR; }
        if(read_result<0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR;
        if(read_result>0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT;
        result=cpsa_equal(metadata,&existing) && existing_length==snapshot_length && !memcmp(existing_snapshot,snapshot,snapshot_length);
        free(existing_snapshot); return result ? CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY : CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CONFLICT;
    }
    result=0;
    if(fstat(root,&directory)<0 || cpsa_file_safe(fd,&directory,&created)<0 ||
       cpsa_write_all(fd,(const uint8_t *)header,(size_t)header_length)<0 ||
       cpsa_write_all(fd,snapshot,snapshot_length)<0 || cpsa_fsync(fd,0)<0) result=-1;
    if(!result && (cpsa_file_safe(fd,&directory,&sealed)<0 || created.st_dev!=sealed.st_dev || created.st_ino!=sealed.st_ino ||
       sealed.st_size!=(off_t)((size_t)header_length+snapshot_length) || fstatat(root,name,&published,AT_SYMLINK_NOFOLLOW)<0 || !cpsa_same_file(&sealed,&published))) result=-1;
    if(cpsa_close(fd,0)<0) result=-1;
    if(!result && cpsa_fsync(root,1)<0) result=-1;
    if(cpsa_close(root,1)<0) result=-1;
    return result ? CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR : CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK;
}

int character_player_snapshot_v1_artifact_load(directory_fd,key,metadata,snapshot,snapshot_length)
int directory_fd;
const character_player_snapshot_v1_artifact_metadata *key;
character_player_snapshot_v1_artifact_metadata *metadata;
uint8_t **snapshot;
size_t *snapshot_length;
{
    int root,result,close_result;
    char name[CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];
    if(metadata) memset(metadata,0,sizeof(*metadata));
    if(snapshot) *snapshot=0;
    if(snapshot_length) *snapshot_length=0U;
    if(!key || !metadata || !snapshot || !snapshot_length || character_player_snapshot_v1_artifact_filename(key,name,sizeof(name))) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_INVALID;
    root=cpsa_root_duplicate(directory_fd); if(root<0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR;
    result=cpsa_read_existing(root,name,metadata,snapshot,snapshot_length); close_result=cpsa_close(root,1);
    if(close_result<0) { free(*snapshot); *snapshot=0; *snapshot_length=0U; memset(metadata,0,sizeof(*metadata)); return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR; }
    if(result==-2) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_NOT_FOUND;
    if(result<0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_IO_ERROR;
    if(result>0) return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_CORRUPT;
    return CHARACTER_PLAYER_SNAPSHOT_V1_ARTIFACT_OK;
}

void character_player_snapshot_v1_artifact_free(snapshot)
uint8_t *snapshot;
{ free(snapshot); }
