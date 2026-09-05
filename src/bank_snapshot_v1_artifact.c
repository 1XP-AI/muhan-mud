/* Immutable, descriptor-relative BankSnapshotV1 evidence.  This artifact
 * boundary intentionally has no dependency on bank.c or bank_store.c. */
#include "bank_snapshot_v1_artifact.h"

#include "bank_snapshot_v1.h"

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

#define BSA_HEADER_MAX 1024U
#define BSA_FILE_MAX (BSA_HEADER_MAX + BANK_SNAPSHOT_V1_ARTIFACT_MAX_BANK_OCTETS)
#define BSA_SUFFIX ".bank-snapshot-v1"
#define BSA_TEMP_NAME_SIZE 128U

#ifdef BANK_SNAPSHOT_V1_ARTIFACT_TESTING
static int bsa_test_fault;
static unsigned int bsa_test_file_fsync_count;
static unsigned int bsa_test_directory_fsync_count;
void bank_snapshot_v1_artifact_test_fail_next(fault) int fault;
{ bsa_test_fault=fault; }
void bank_snapshot_v1_artifact_test_reset_faults(void)
{ bsa_test_fault=0; bsa_test_file_fsync_count=0U; bsa_test_directory_fsync_count=0U; }
void bank_snapshot_v1_artifact_test_fsync_counts(file_count,directory_count)
unsigned int *file_count; unsigned int *directory_count;
{ if(file_count) *file_count=bsa_test_file_fsync_count; if(directory_count) *directory_count=bsa_test_directory_fsync_count; }
static int bsa_fault(fault) int fault;
{ if(bsa_test_fault!=fault) return 0; bsa_test_fault=0; errno=EIO; return 1; }
#else
static int bsa_fault(fault) int fault;
{ (void)fault; return 0; }
#endif

static unsigned long bsa_bounded(value, maximum) const char *value; unsigned long maximum;
{ unsigned long i; if(!value) return maximum+1U; for(i=0;i<=maximum;++i) if(!value[i]) return i; return maximum+1U; }
static int bsa_hex(value) char value;
{ return (value>='0'&&value<='9')||(value>='a'&&value<='f'); }
static int bsa_uuid(value) const char *value;
{ unsigned long i; if(bsa_bounded(value,BANK_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH)!=BANK_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH) return 0; for(i=0;i<BANK_SNAPSHOT_V1_ARTIFACT_UUID_LENGTH;++i) { if(i==8U||i==13U||i==18U||i==23U) { if(value[i]!='-') return 0; } else if(!bsa_hex(value[i])) return 0; } return 1; }
static int bsa_hash(value) const char *value;
{ unsigned long i; if(bsa_bounded(value,BANK_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH)!=BANK_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH) return 0; for(i=0;i<BANK_SNAPSHOT_V1_ARTIFACT_HASH_LENGTH;++i) if(!bsa_hex(value[i])) return 0; return 1; }
static int bsa_world(value) const char *value;
{ unsigned long i,length=bsa_bounded(value,BANK_SNAPSHOT_V1_ARTIFACT_TEXT_MAX); if(!length||length>BANK_SNAPSHOT_V1_ARTIFACT_TEXT_MAX||value[0]<'a'||value[0]>'z') return 0; for(i=1U;i<length;++i) if(!((value[i]>='a'&&value[i]<='z')||(value[i]>='0'&&value[i]<='9')||value[i]=='_'||value[i]=='-')) return 0; return 1; }
static int bsa_name_hex(value) const char *value;
{ unsigned long i,length=bsa_bounded(value,28U); if(length<2U||length>28U||(length&1U)) return 0; for(i=0;i<length;++i) if(!bsa_hex(value[i])) return 0; return 1; }
static int bsa_metadata_valid(value, bank) const bank_snapshot_v1_artifact_metadata *value; int bank;
{ if(!value||!bsa_bounded(value->artifact_format,sizeof(value->artifact_format)-1U)||strcmp(value->artifact_format,BANK_SNAPSHOT_V1_ARTIFACT_FORMAT)||!bsa_world(value->world_id)||!bsa_uuid(value->character_id)||!bsa_uuid(value->command_id)||!bsa_name_hex(value->canonical_name_hex)||!bsa_hash(value->request_sha256)||!bsa_hash(value->source_post_sha256)||!value->writer_epoch||!value->writer_revision||value->writer_epoch>INT64_MAX||value->writer_revision>INT64_MAX) return 0; if(bank&&(!bsa_hash(value->bank_sha256)||!value->bank_octets||value->bank_octets>BANK_SNAPSHOT_V1_ARTIFACT_MAX_BANK_OCTETS)) return 0; return 1; }

static int bsa_close(fd, directory) int fd; int directory;
{ int result=close(fd); if(bsa_fault(directory?6:3)) return -1; return result; }
static int bsa_fsync(fd, directory) int fd; int directory;
{ int result,eintr_fault;
#ifdef BANK_SNAPSHOT_V1_ARTIFACT_TESTING
  if(directory) ++bsa_test_directory_fsync_count; else ++bsa_test_file_fsync_count;
#endif
  eintr_fault=directory?8:7;
  for(;;) { if(bsa_fault(directory?4:2)) return -1; if(bsa_fault(eintr_fault)) { errno=EINTR; result=-1; } else result=fsync(fd); if(result<0&&errno==EINTR) continue; return result; }
}
static void *bsa_malloc(size) size_t size;
{ if(bsa_fault(5)) return 0; return malloc(size); }
static int bsa_root_duplicate(directory_fd) int directory_fd;
{ int duplicate; struct stat value; if(directory_fd<0) return -1; duplicate=fcntl(directory_fd,F_DUPFD_CLOEXEC,3); if(duplicate<0) return -1; if(fstat(duplicate,&value)<0||!S_ISDIR(value.st_mode)||value.st_uid!=geteuid()||(value.st_mode&0777)!=0700) { bsa_close(duplicate,1); return -1; } return duplicate; }
static int bsa_file_safe(fd, directory, before) int fd; const struct stat *directory; struct stat *before;
{ if(fstat(fd,before)<0||!S_ISREG(before->st_mode)||before->st_nlink!=1||before->st_uid!=directory->st_uid||(before->st_mode&0777)!=0600||before->st_size<0||(uint64_t)before->st_size>BSA_FILE_MAX) return -1; return 0; }
static int bsa_same_file(left,right) const struct stat *left; const struct stat *right;
{ return left->st_dev==right->st_dev&&left->st_ino==right->st_ino&&left->st_mode==right->st_mode&&left->st_uid==right->st_uid&&left->st_nlink==right->st_nlink&&left->st_size==right->st_size; }
static int bsa_write_all(fd,bytes,length) int fd; const uint8_t *bytes; size_t length;
{ ssize_t amount; while(length) { if(bsa_fault(1)) return -1; amount=write(fd,bytes,length); if(amount<0&&errno==EINTR) continue; if(amount<=0) return -1; bytes+=amount; length-=(size_t)amount; } return 0; }

/* A local SHA-256 keeps the immutable content hash independent of live code. */
typedef struct bsa_sha256 { uint32_t h[8]; uint64_t bits; uint8_t block[64]; size_t used; } bsa_sha256;
static uint32_t bsa_rotr(value,count) uint32_t value; unsigned int count; { return (value>>count)|(value<<(32U-count)); }
#if 0 /* retained only to keep the historical compact draft out of production */
static void bsa_sha_block(c,b) bsa_sha256 *c; const uint8_t *b;
{
 static const uint32_t k[64]={0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5cb0a9dcU,0x76f988daU,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5cb0a9dcU,0x76f988daU,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5cb0a9dcU,0x76f988daU,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5cb0a9dcU,0x76f988daU,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U};
 uint32_t w[64],a,b0,d,e,f,g,h,i,t1,t2; unsigned int n;
 for(n=0;n<16U;++n) w[n]=((uint32_t)b[n*4U]<<24)|((uint32_t)b[n*4U+1U]<<16)|((uint32_t)b[n*4U+2U]<<8)|b[n*4U+3U]; for(n=16U;n<64U;++n) w[n]=w[n-16U]+(bsa_rotr(w[n-15U],7)^bsa_rotr(w[n-15U],18)^(w[n-15U]>>3))+w[n-7U]+(bsa_rotr(w[n-2U],17)^bsa_rotr(w[n-2U],19)^(w[n-2U]>>10));
 a=c->h[0];b0=c->h[1];d=c->h[2];e=c->h[3];f=c->h[4];g=c->h[5];h=c->h[6];i=c->h[7]; for(n=0;n<64U;++n){t1=i+(bsa_rotr(f,6)^bsa_rotr(f,11)^bsa_rotr(f,25))+((f&g)^((~f)&h))+k[n]+w[n];t2=(bsa_rotr(a,2)^bsa_rotr(a,13)^bsa_rotr(a,22))+((a&b0)^(a&d)^(b0&d));i=h;h=g;g=f;f=e+t1;e=d;d=b0;b0=a;a=t1+t2;} c->h[0]+=a;c->h[1]+=b0;c->h[2]+=d;c->h[3]+=e;c->h[4]+=f;c->h[5]+=g;c->h[6]+=h;c->h[7]+=i;
}
#endif
static void bsa_sha_block(c,b) bsa_sha256 *c; const uint8_t *b;
{
    static const uint32_t k[64]={
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U};
    uint32_t w[64],a,b0,d,e,f,g,h,i,t1,t2; unsigned int n;
    for(n=0;n<16U;++n) w[n]=((uint32_t)b[n*4U]<<24)|((uint32_t)b[n*4U+1U]<<16)|((uint32_t)b[n*4U+2U]<<8)|b[n*4U+3U];
    for(n=16U;n<64U;++n) w[n]=w[n-16U]+(bsa_rotr(w[n-15U],7)^bsa_rotr(w[n-15U],18)^(w[n-15U]>>3))+w[n-7U]+(bsa_rotr(w[n-2U],17)^bsa_rotr(w[n-2U],19)^(w[n-2U]>>10));
    a=c->h[0];b0=c->h[1];d=c->h[2];e=c->h[3];f=c->h[4];g=c->h[5];h=c->h[6];i=c->h[7];
    for(n=0;n<64U;++n) { t1=i+(bsa_rotr(f,6)^bsa_rotr(f,11)^bsa_rotr(f,25))+((f&g)^((~f)&h))+k[n]+w[n]; t2=(bsa_rotr(a,2)^bsa_rotr(a,13)^bsa_rotr(a,22))+((a&b0)^(a&d)^(b0&d)); i=h;h=g;g=f;f=e+t1;e=d;d=b0;b0=a;a=t1+t2; }
    c->h[0]+=a;c->h[1]+=b0;c->h[2]+=d;c->h[3]+=e;c->h[4]+=f;c->h[5]+=g;c->h[6]+=h;c->h[7]+=i;
}
static void bsa_hex_digest(bytes,length,out) const uint8_t *bytes; size_t length; char out[65];
{ bsa_sha256 c; uint64_t bits; unsigned int n; static const char d[]="0123456789abcdef"; c.h[0]=0x6a09e667U;c.h[1]=0xbb67ae85U;c.h[2]=0x3c6ef372U;c.h[3]=0xa54ff53aU;c.h[4]=0x510e527fU;c.h[5]=0x9b05688cU;c.h[6]=0x1f83d9abU;c.h[7]=0x5be0cd19U;c.bits=0;c.used=0; while(length){size_t take=64U-c.used;if(take>length)take=length;memcpy(c.block+c.used,bytes,take);c.used+=take;bytes+=take;length-=take;c.bits+=(uint64_t)take*8U;if(c.used==64U){bsa_sha_block(&c,c.block);c.used=0;}} bits=c.bits;c.block[c.used++]=0x80U;if(c.used>56U){while(c.used<64U)c.block[c.used++]=0;bsa_sha_block(&c,c.block);c.used=0;}while(c.used<56U)c.block[c.used++]=0;for(n=0;n<8U;++n)c.block[63U-n]=(uint8_t)(bits>>(n*8U));bsa_sha_block(&c,c.block);for(n=0;n<8U;++n){out[n*8U]=d[c.h[n]>>28];out[n*8U+1U]=d[(c.h[n]>>24)&15U];out[n*8U+2U]=d[(c.h[n]>>20)&15U];out[n*8U+3U]=d[(c.h[n]>>16)&15U];out[n*8U+4U]=d[(c.h[n]>>12)&15U];out[n*8U+5U]=d[(c.h[n]>>8)&15U];out[n*8U+6U]=d[(c.h[n]>>4)&15U];out[n*8U+7U]=d[c.h[n]&15U];}out[64]=0; }

static int bsa_canonical_bank(bank,length) const uint8_t *bank; size_t length;
{ otag *root; uint8_t *encoded; size_t encoded_length; int status; if(!bank||!length) return -1; if(length>BANK_SNAPSHOT_V1_ARTIFACT_MAX_BANK_OCTETS) return -2; root=0;encoded=0;encoded_length=0; status=bank_snapshot_v1_decode(bank,length,&root); if(status!=0||!root) return -1; status=bank_snapshot_v1_encode(root,&encoded,&encoded_length);bank_snapshot_v1_free(root);if(status!=0||!encoded||encoded_length!=length||memcmp(encoded,bank,length)){bank_snapshot_v1_free_wire(encoded);return -1;}bank_snapshot_v1_free_wire(encoded);return 0; }
static int bsa_number(value,length,out) const char *value; unsigned long length; uint64_t *out;
{ uint64_t n=0;unsigned long i;if(!length||length>19U||value[0]=='0')return -1;for(i=0;i<length;++i){if(value[i]<'0'||value[i]>'9'||n>((uint64_t)INT64_MAX-(uint64_t)(value[i]-'0'))/10U)return -1;n=n*10U+(uint64_t)(value[i]-'0');}if(!n)return -1;*out=n;return 0; }
static int bsa_text(dst,cap,value,length) char *dst;unsigned long cap;const char *value;unsigned long length;
{if(length>=cap)return -1;memcpy(dst,value,length);dst[length]=0;return 0;}
static int bsa_line(cursor,remaining,expected,value,value_length) const char **cursor;unsigned long *remaining;const char *expected;const char **value;unsigned long *value_length;
{const char *newline;unsigned long prefix=(unsigned long)strlen(expected),consumed;if(*remaining<prefix+2U||memcmp(*cursor,expected,prefix)||(*cursor)[prefix]!='=')return -1;newline=(const char *)memchr(*cursor+prefix+1U,'\n',*remaining-prefix-1U);if(!newline)return -1;*value=*cursor+prefix+1U;*value_length=(unsigned long)(newline-*value);consumed=(unsigned long)(newline-*cursor)+1U;*cursor=newline+1U;*remaining-=consumed;return 0;}
static int bsa_header(output,capacity,value) char *output;size_t capacity;const bank_snapshot_v1_artifact_metadata *value;
{int length=snprintf(output,capacity,"version=1\nartifact_format=%s\nworld_id=%s\ncharacter_id=%s\ncommand_id=%s\ncanonical_name_hex=%s\nrequest_sha256=%s\nsource_post_sha256=%s\nwriter_epoch=%llu\nwriter_revision=%llu\nbank_sha256=%s\nbank_octets=%llu\n\n",value->artifact_format,value->world_id,value->character_id,value->command_id,value->canonical_name_hex,value->request_sha256,value->source_post_sha256,(unsigned long long)value->writer_epoch,(unsigned long long)value->writer_revision,value->bank_sha256,(unsigned long long)value->bank_octets);return length<0||(size_t)length>=capacity?-1:length;}
static int bsa_parse_header(bytes,length,out,header_length) const uint8_t *bytes;size_t length;bank_snapshot_v1_artifact_metadata *out;size_t *header_length;
{static const char *names[]={"version","artifact_format","world_id","character_id","command_id","canonical_name_hex","request_sha256","source_post_sha256","writer_epoch","writer_revision","bank_sha256","bank_octets"};const char *cursor,*value;unsigned long remaining,value_length,i;uint64_t number;char canonical[BSA_HEADER_MAX];int canonical_length;size_t boundary;if(!bytes||!out||!header_length||length<3U||length>BSA_FILE_MAX)return -1;for(boundary=0;boundary+1U<length;++boundary)if(bytes[boundary]=='\n'&&bytes[boundary+1U]=='\n')break;if(boundary+1U>=length||boundary+2U>BSA_HEADER_MAX||memchr(bytes,0,boundary+2U))return -1;memset(out,0,sizeof(*out));cursor=(const char *)bytes;remaining=(unsigned long)(boundary+1U);for(i=0;i<12U;++i){if(bsa_line(&cursor,&remaining,names[i],&value,&value_length))return -1;if(i==0U){if(value_length!=1U||value[0]!='1')return -1;}else if(i==1U){if(bsa_text(out->artifact_format,sizeof(out->artifact_format),value,value_length))return -1;}else if(i==2U){if(bsa_text(out->world_id,sizeof(out->world_id),value,value_length))return -1;}else if(i==3U){if(bsa_text(out->character_id,sizeof(out->character_id),value,value_length))return -1;}else if(i==4U){if(bsa_text(out->command_id,sizeof(out->command_id),value,value_length))return -1;}else if(i==5U){if(bsa_text(out->canonical_name_hex,sizeof(out->canonical_name_hex),value,value_length))return -1;}else if(i==6U){if(bsa_text(out->request_sha256,sizeof(out->request_sha256),value,value_length))return -1;}else if(i==7U){if(bsa_text(out->source_post_sha256,sizeof(out->source_post_sha256),value,value_length))return -1;}else if(i==8U){if(bsa_number(value,value_length,&number))return -1;out->writer_epoch=number;}else if(i==9U){if(bsa_number(value,value_length,&number))return -1;out->writer_revision=number;}else if(i==10U){if(bsa_text(out->bank_sha256,sizeof(out->bank_sha256),value,value_length))return -1;}else{if(bsa_number(value,value_length,&number))return -1;out->bank_octets=number;}}if(remaining||!bsa_metadata_valid(out,1))return -1;canonical_length=bsa_header(canonical,sizeof(canonical),out);if(canonical_length<0||(size_t)canonical_length!=boundary+2U||memcmp(canonical,bytes,boundary+2U))return -1;*header_length=boundary+2U;return 0;}
static int bsa_read_existing(directory_fd,name,expected_command_id,metadata,bank,bank_length) int directory_fd;const char *name;const char *expected_command_id;bank_snapshot_v1_artifact_metadata *metadata;uint8_t **bank;size_t *bank_length;
{int fd,result;ssize_t amount;struct stat directory,before,after;uint8_t *bytes;size_t used,header_length;*bank=0;*bank_length=0U;if(fstat(directory_fd,&directory)<0)return -1;fd=openat(directory_fd,name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);if(fd<0)return errno==ENOENT?-2:-1;result=bsa_file_safe(fd,&directory,&before);bytes=0;used=0;if(!result&&!(bytes=(uint8_t *)bsa_malloc((size_t)before.st_size+1U)))result=-1;while(!result&&used<(size_t)before.st_size){amount=read(fd,bytes+used,(size_t)before.st_size-used);if(amount<0&&errno==EINTR)continue;if(amount<=0){result=-1;break;}used+=(size_t)amount;}if(!result){do amount=read(fd,bytes+used,1U);while(amount<0&&errno==EINTR);if(amount!=0||fstat(fd,&after)<0||!bsa_same_file(&before,&after))result=-1;}if(bsa_close(fd,0)<0)result=-1;if(result){free(bytes);return -1;}if(bsa_parse_header(bytes,used,metadata,&header_length)||!expected_command_id||strcmp(metadata->command_id,expected_command_id)||metadata->bank_octets!=(uint64_t)(used-header_length)||bsa_canonical_bank(bytes+header_length,used-header_length)){free(bytes);return 1;}{char digest[65];bsa_hex_digest(bytes+header_length,used-header_length,digest);if(strcmp(digest,metadata->bank_sha256)){free(bytes);return 1;}}memmove(bytes,bytes+header_length,used-header_length);*bank=bytes;*bank_length=used-header_length;return 0;}

int bank_snapshot_v1_artifact_filename(metadata,output,output_size) const bank_snapshot_v1_artifact_metadata *metadata;char *output;size_t output_size;
{int length;if(!metadata||!output||!output_size||!bsa_uuid(metadata->command_id))return -1;length=snprintf(output,output_size,"%s%s",metadata->command_id,BSA_SUFFIX);return length<0||(size_t)length>=output_size?-1:0;}
static int bsa_equal(left,right) const bank_snapshot_v1_artifact_metadata *left;const bank_snapshot_v1_artifact_metadata *right;
{return !strcmp(left->artifact_format,right->artifact_format)&&!strcmp(left->world_id,right->world_id)&&!strcmp(left->character_id,right->character_id)&&!strcmp(left->command_id,right->command_id)&&!strcmp(left->canonical_name_hex,right->canonical_name_hex)&&!strcmp(left->request_sha256,right->request_sha256)&&!strcmp(left->source_post_sha256,right->source_post_sha256)&&!strcmp(left->bank_sha256,right->bank_sha256)&&left->writer_epoch==right->writer_epoch&&left->writer_revision==right->writer_revision&&left->bank_octets==right->bank_octets;}
#if 0 /* superseded compact draft */
int bank_snapshot_v1_artifact_store(directory_fd,metadata,bank,bank_length) int directory_fd;bank_snapshot_v1_artifact_metadata *metadata;const uint8_t *bank;size_t bank_length;
{int root,fd,result,close_result,header_length,read_result,canonical;char name[BANK_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE],header[BSA_HEADER_MAX],digest[65];struct stat directory,created,sealed,published;bank_snapshot_v1_artifact_metadata existing;uint8_t *existing_bank;size_t existing_length;if(!bsa_metadata_valid(metadata,0))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;canonical=bsa_canonical_bank(bank,bank_length);if(canonical==-2)return BANK_SNAPSHOT_V1_ARTIFACT_LIMIT;if(canonical)return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;if(metadata->bank_octets&&metadata->bank_octets!=(uint64_t)bank_length)return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;bsa_hex_digest(bank,bank_length,digest);if(metadata->bank_sha256[0]&&(!bsa_hash(metadata->bank_sha256)||strcmp(metadata->bank_sha256,digest)))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;strcpy(metadata->bank_sha256,digest);metadata->bank_octets=(uint64_t)bank_length;if(!bsa_metadata_valid(metadata,1)||(header_length=bsa_header(header,sizeof(header),metadata))<0||character_player_snapshot_v1_artifact_filename){} if(bank_snapshot_v1_artifact_filename(metadata,name,sizeof(name)))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;root=bsa_root_duplicate(directory_fd);if(root<0)return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;fd=openat(root,name,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC,0600);if(fd<0){if(errno!=EEXIST){bsa_close(root,1);return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;}existing_bank=0;existing_length=0U;memset(&existing,0,sizeof(existing));read_result=bsa_read_existing(root,name,&existing,&existing_bank,&existing_length);close_result=bsa_close(root,1);if(close_result<0){free(existing_bank);return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;}if(read_result<0)return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;if(read_result>0)return BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT;result=bsa_equal(metadata,&existing)&&existing_length==bank_length&&!memcmp(existing_bank,bank,bank_length);free(existing_bank);return result?BANK_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY:BANK_SNAPSHOT_V1_ARTIFACT_CONFLICT;}result=0;if(fstat(root,&directory)<0||bsa_file_safe(fd,&directory,&created)<0||bsa_write_all(fd,(const uint8_t *)header,(size_t)header_length)<0||bsa_write_all(fd,bank,bank_length)<0||bsa_fsync(fd,0)<0)result=-1;if(!result&&(bsa_file_safe(fd,&directory,&sealed)<0||created.st_dev!=sealed.st_dev||created.st_ino!=sealed.st_ino||sealed.st_size!=(off_t)((size_t)header_length+bank_length)||fstatat(root,name,&published,AT_SYMLINK_NOFOLLOW)<0||!bsa_same_file(&sealed,&published)))result=-1;if(bsa_close(fd,0)<0)result=-1;if(!result&&bsa_fsync(root,1)<0)result=-1;if(bsa_close(root,1)<0)result=-1;return result?BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR:BANK_SNAPSHOT_V1_ARTIFACT_OK;}
#endif
static int bsa_open_temp(directory_fd,name,capacity) int directory_fd; char *name; size_t capacity;
{ static unsigned long serial; unsigned long attempt; int fd,length; for(attempt=0U;attempt<1024U;++attempt) { ++serial; length=snprintf(name,capacity,".bank-snapshot-v1.tmp.%ld.%lu",(long)getpid(),serial); if(length<0||(size_t)length>=capacity)return -1; fd=openat(directory_fd,name,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC,0600); if(fd>=0)return fd; if(errno!=EEXIST)return -1; } errno=EEXIST; return -1; }
static int bsa_durable_existing(directory_fd,name) int directory_fd;const char *name;
{ int fd,result;struct stat directory,before,after,published;if(fstat(directory_fd,&directory)<0)return -1;fd=openat(directory_fd,name,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);if(fd<0)return -1;result=bsa_file_safe(fd,&directory,&before);if(!result&&bsa_fsync(fd,0)<0)result=-1;if(!result&&(bsa_file_safe(fd,&directory,&after)<0||!bsa_same_file(&before,&after)||fstatat(directory_fd,name,&published,AT_SYMLINK_NOFOLLOW)<0||!bsa_same_file(&after,&published)))result=-1;if(bsa_close(fd,0)<0)result=-1;if(!result&&bsa_fsync(directory_fd,1)<0)result=-1;return result;}
static int bsa_existing_result(directory_fd,name,metadata,bank,bank_length)
int directory_fd;const char *name;const bank_snapshot_v1_artifact_metadata *metadata;const uint8_t *bank;size_t bank_length;
{ int read_result,result;bank_snapshot_v1_artifact_metadata existing;uint8_t *existing_bank;size_t existing_length;existing_bank=0;existing_length=0U;memset(&existing,0,sizeof(existing));read_result=bsa_read_existing(directory_fd,name,metadata->command_id,&existing,&existing_bank,&existing_length);if(read_result<0)return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;if(read_result>0)return BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT;result=bsa_equal(metadata,&existing)&&existing_length==bank_length&&!memcmp(existing_bank,bank,bank_length);free(existing_bank);if(!result)return BANK_SNAPSHOT_V1_ARTIFACT_CONFLICT;return bsa_durable_existing(directory_fd,name)<0?BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR:BANK_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY;}
int bank_snapshot_v1_artifact_store(directory_fd, metadata, bank, bank_length)
int directory_fd; bank_snapshot_v1_artifact_metadata *metadata; const uint8_t *bank; size_t bank_length;
{ int root,fd,result,header_length,canonical,published;char name[BANK_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE],temp[BSA_TEMP_NAME_SIZE],header[BSA_HEADER_MAX],digest[65];struct stat directory,created,sealed,visible;
  if(!metadata)return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;
  if(!metadata->artifact_format[0])strcpy(metadata->artifact_format,BANK_SNAPSHOT_V1_ARTIFACT_FORMAT);
  if(!bsa_metadata_valid(metadata,0))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;
  canonical=bsa_canonical_bank(bank,bank_length);if(canonical==-2)return BANK_SNAPSHOT_V1_ARTIFACT_LIMIT;if(canonical)return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;
  if(metadata->bank_octets&&metadata->bank_octets!=(uint64_t)bank_length)return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;
  bsa_hex_digest(bank,bank_length,digest);if(metadata->bank_sha256[0]&&(!bsa_hash(metadata->bank_sha256)||strcmp(metadata->bank_sha256,digest)))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;
  strcpy(metadata->bank_sha256,digest);metadata->bank_octets=(uint64_t)bank_length;
  if(!bsa_metadata_valid(metadata,1)||(header_length=bsa_header(header,sizeof(header),metadata))<0||bank_snapshot_v1_artifact_filename(metadata,name,sizeof(name)))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;
  root=bsa_root_duplicate(directory_fd);if(root<0)return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;
  fd=bsa_open_temp(root,temp,sizeof(temp));if(fd<0){bsa_close(root,1);return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;}
  result=0;published=0;
  if(fstat(root,&directory)<0||bsa_file_safe(fd,&directory,&created)<0||bsa_write_all(fd,(const uint8_t *)header,(size_t)header_length)<0||bsa_write_all(fd,bank,bank_length)<0||bsa_fsync(fd,0)<0)result=-1;
  if(!result&&(bsa_file_safe(fd,&directory,&sealed)<0||created.st_dev!=sealed.st_dev||created.st_ino!=sealed.st_ino||sealed.st_size!=(off_t)((size_t)header_length+bank_length)))result=-1;
  /* linkat(O_EXCL temp -> final) is our no-replace atomic publication:
   * readers can first observe only the already-fsynced complete inode. */
  if(!result&&linkat(root,temp,root,name,0)<0) { if(errno==EEXIST) result=1; else result=-1; } else if(!result) { published=1; if(unlinkat(root,temp,0)<0||fstatat(root,name,&visible,AT_SYMLINK_NOFOLLOW)<0||!bsa_same_file(&sealed,&visible))result=-1; }
  if(bsa_close(fd,0)<0)result=-1;
  if(!published)unlinkat(root,temp,0);
  if(result==1)result=bsa_existing_result(root,name,metadata,bank,bank_length);
  else if(!result&&bsa_fsync(root,1)<0)result=-1;
  if(bsa_close(root,1)<0)result=-1;
  if(result==0)return BANK_SNAPSHOT_V1_ARTIFACT_OK;
  if(result==BANK_SNAPSHOT_V1_ARTIFACT_EXACT_RETRY||result==BANK_SNAPSHOT_V1_ARTIFACT_CONFLICT||result==BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT)return result;
  return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;
}
int bank_snapshot_v1_artifact_load(directory_fd,key,metadata,bank,bank_length) int directory_fd;const bank_snapshot_v1_artifact_metadata *key;bank_snapshot_v1_artifact_metadata *metadata;uint8_t **bank;size_t *bank_length;
{int root,result,close_result;char name[BANK_SNAPSHOT_V1_ARTIFACT_FILENAME_SIZE];if(metadata)memset(metadata,0,sizeof(*metadata));if(bank)*bank=0;if(bank_length)*bank_length=0U;if(!key||!metadata||!bank||!bank_length||bank_snapshot_v1_artifact_filename(key,name,sizeof(name)))return BANK_SNAPSHOT_V1_ARTIFACT_INVALID;root=bsa_root_duplicate(directory_fd);if(root<0)return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;result=bsa_read_existing(root,name,key->command_id,metadata,bank,bank_length);close_result=bsa_close(root,1);if(close_result<0){free(*bank);*bank=0;*bank_length=0U;memset(metadata,0,sizeof(*metadata));return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;}if(result==-2)return BANK_SNAPSHOT_V1_ARTIFACT_NOT_FOUND;if(result<0)return BANK_SNAPSHOT_V1_ARTIFACT_IO_ERROR;if(result>0)return BANK_SNAPSHOT_V1_ARTIFACT_CORRUPT;return BANK_SNAPSHOT_V1_ARTIFACT_OK;}
void bank_snapshot_v1_artifact_free(bank) uint8_t *bank; { free(bank); }
