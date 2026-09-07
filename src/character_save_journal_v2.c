#include "character_save_journal_v2.h"
#include "utf8_text.h"

#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
#include <signal.h>
#endif

#ifndef O_NOFOLLOW
#error "v2 staged journal requires O_NOFOLLOW"
#endif

#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
static void v2_crash_after(event)
character_save_journal_v2_crash_cutpoint event;
{
    const char *text = getenv("M3_V2_CRASH_CUTPOINT");
    char *end;
    unsigned long selected;
    if(!text || !*text) return;
    selected = strtoul(text, &end, 10);
    if(*end || selected != (unsigned long)event) return;
    (void)kill(getpid(), SIGKILL);
    _exit(127);
}
#else
#define v2_crash_after(event) ((void)0)
#endif
#ifndef O_DIRECTORY
#error "v2 staged journal requires O_DIRECTORY"
#endif

#define V2_RUNTIME_UID 10001
#define V2_TEXT_MAX 1400

typedef struct v2_sha256 {
    uint32_t h[8];
    uint64_t bits;
    unsigned char block[64];
    unsigned int used;
} v2_sha256;

typedef struct v2_tree {
    int root_fd, player_fd, shard_fd, journal_fd, stage_fd;
} v2_tree;

static uid_t v2_trusted_uid = V2_RUNTIME_UID;
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
static int v2_fail_stage_file, v2_fail_stage_dir, v2_fail_journal_file, v2_fail_journal_dir;
static unsigned int v2_stage_file_syncs, v2_stage_dir_syncs, v2_journal_file_syncs, v2_journal_dir_syncs;
static int v2_hash_ready_fd = -1, v2_hash_release_fd = -1;
static int v2_live_ready_fd = -1, v2_live_release_fd = -1;
static char v2_pause_component[64];
static int v2_component_ready_fd = -1, v2_component_release_fd = -1;
static int v2_write_eintr_once, v2_write_short_once;
static int v2_write_zero_once, v2_write_eio_once, v2_fail_close_kind;
#endif

static uint32_t v2_rotr(x, n)
uint32_t x;
unsigned int n;
{
    return (x >> n) | (x << (32 - n));
}

static void v2_sha_block(s, p)
v2_sha256 *s;
const unsigned char *p;
{
    static const uint32_t k[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U };
    uint32_t w[64], a, b, c, d, e, f, g, h, t1, t2;
    unsigned int i;
    for(i = 0; i < 16; i++) w[i] = ((uint32_t)p[i*4] << 24) | ((uint32_t)p[i*4+1] << 16) | ((uint32_t)p[i*4+2] << 8) | p[i*4+3];
    for(i = 16; i < 64; i++) w[i] = (v2_rotr(w[i-15],7)^v2_rotr(w[i-15],18)^(w[i-15]>>3)) + w[i-16] + (v2_rotr(w[i-2],17)^v2_rotr(w[i-2],19)^(w[i-2]>>10)) + w[i-7];
    a=s->h[0]; b=s->h[1]; c=s->h[2]; d=s->h[3]; e=s->h[4]; f=s->h[5]; g=s->h[6]; h=s->h[7];
    for(i=0;i<64;i++) { t1=h+(v2_rotr(e,6)^v2_rotr(e,11)^v2_rotr(e,25))+((e&f)^((~e)&g))+k[i]+w[i]; t2=(v2_rotr(a,2)^v2_rotr(a,13)^v2_rotr(a,22))+((a&b)^(a&c)^(b&c)); h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2; }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e; s->h[5]+=f; s->h[6]+=g; s->h[7]+=h;
}

static void v2_sha_init(s)
v2_sha256 *s;
{
    static const uint32_t initial[8] = {0x6a09e667U,0xbb67ae85U,0x3c6ef372U,0xa54ff53aU,0x510e527fU,0x9b05688cU,0x1f83d9abU,0x5be0cd19U};
    memcpy(s->h, initial, sizeof(initial)); s->bits = 0; s->used = 0;
}

static void v2_sha_update(s, data, length)
v2_sha256 *s;
const unsigned char *data;
size_t length;
{
    size_t take;
    s->bits += (uint64_t)length * 8;
    while(length) { take = 64 - s->used; if(take > length) take = length; memcpy(s->block+s->used,data,take); s->used += (unsigned int)take; data += take; length -= take; if(s->used == 64) { v2_sha_block(s,s->block); s->used=0; } }
}

static void v2_sha_final(s, out)
v2_sha256 *s;
unsigned char out[32];
{
    unsigned int i; uint64_t bits = s->bits;
    s->block[s->used++] = 0x80;
    if(s->used > 56) { while(s->used < 64) s->block[s->used++] = 0; v2_sha_block(s,s->block); s->used=0; }
    while(s->used < 56) s->block[s->used++] = 0;
    for(i=0;i<8;i++) s->block[63-i] = (unsigned char)(bits >> (i*8));
    v2_sha_block(s,s->block);
    for(i=0;i<8;i++) { out[i*4]=(unsigned char)(s->h[i]>>24); out[i*4+1]=(unsigned char)(s->h[i]>>16); out[i*4+2]=(unsigned char)(s->h[i]>>8); out[i*4+3]=(unsigned char)s->h[i]; }
}

static void v2_hex(bytes, out)
const unsigned char bytes[32];
char out[65];
{
    static const char hex[]="0123456789abcdef"; unsigned int i;
    for(i=0;i<32;i++) { out[i*2]=hex[bytes[i]>>4]; out[i*2+1]=hex[bytes[i]&15]; } out[64]=0;
}

static uint32_t v2_rol32(value, shift)
uint32_t value;
unsigned int shift;
{
    return (value << shift) | (value >> (32 - shift));
}

static uint32_t v2_load_u32_be(bytes)
const unsigned char *bytes;
{
    return ((uint32_t)bytes[0] << 24) |
           ((uint32_t)bytes[1] << 16) |
           ((uint32_t)bytes[2] << 8) |
           (uint32_t)bytes[3];
}

/* The legacy name is at most 14 bytes, so its SHA-1 input is exactly one
 * padded block.  This clone stays private to the test-only v2 boundary and
 * avoids acquiring a player_path/live-writer dependency. */
static void v2_legacy_name_sha1(bytes, length, out)
const unsigned char *bytes;
size_t length;
unsigned char out[20];
{
    uint32_t h0, h1, h2, h3, h4, words[80];
    uint32_t a, b, c, d, e, f, constant, next;
    unsigned char block[64];
    uint64_t bits;
    unsigned int i;

    memset(block, 0, sizeof(block));
    memcpy(block, bytes, length);
    block[length] = 0x80;
    bits = (uint64_t)length * 8;
    for(i = 0; i < 8; i++)
        block[63 - i] = (unsigned char)(bits >> (i * 8));
    for(i = 0; i < 16; i++) words[i] = v2_load_u32_be(block + i * 4);
    for(i = 16; i < 80; i++)
        words[i] = v2_rol32(words[i - 3] ^ words[i - 8] ^
                            words[i - 14] ^ words[i - 16], 1);

    h0 = 0x67452301U;
    h1 = 0xefcdab89U;
    h2 = 0x98badcfeU;
    h3 = 0x10325476U;
    h4 = 0xc3d2e1f0U;
    a = h0; b = h1; c = h2; d = h3; e = h4;
    for(i = 0; i < 80; i++) {
        if(i < 20) {
            f = (b & c) | ((~b) & d);
            constant = 0x5a827999U;
        }
        else if(i < 40) {
            f = b ^ c ^ d;
            constant = 0x6ed9eba1U;
        }
        else if(i < 60) {
            f = (b & c) | (b & d) | (c & d);
            constant = 0x8f1bbcdcU;
        }
        else {
            f = b ^ c ^ d;
            constant = 0xca62c1d6U;
        }
        next = v2_rol32(a, 5) + f + e + constant + words[i];
        e = d; d = c; c = v2_rol32(b, 30); b = a; a = next;
    }
    h0 += a; h1 += b; h2 += c; h3 += d; h4 += e;
    out[0]=(unsigned char)(h0>>24); out[1]=(unsigned char)(h0>>16);
    out[2]=(unsigned char)(h0>>8); out[3]=(unsigned char)h0;
    out[4]=(unsigned char)(h1>>24); out[5]=(unsigned char)(h1>>16);
    out[6]=(unsigned char)(h1>>8); out[7]=(unsigned char)h1;
    out[8]=(unsigned char)(h2>>24); out[9]=(unsigned char)(h2>>16);
    out[10]=(unsigned char)(h2>>8); out[11]=(unsigned char)h2;
    out[12]=(unsigned char)(h3>>24); out[13]=(unsigned char)(h3>>16);
    out[14]=(unsigned char)(h3>>8); out[15]=(unsigned char)h3;
    out[16]=(unsigned char)(h4>>24); out[17]=(unsigned char)(h4>>16);
    out[18]=(unsigned char)(h4>>8); out[19]=(unsigned char)h4;
    memset(block, 0, sizeof(block));
    memset(words, 0, sizeof(words));
}

static size_t v2_bounded(s, limit)
const char *s;
size_t limit;
{ size_t i; if(!s) return limit+1; for(i=0;i<=limit;i++) if(!s[i]) return i; return limit+1; }
static int v2_lower_hex(s, length)
const char *s; size_t length;
{ size_t i; if(!s || v2_bounded(s,length)!=length) return 0; for(i=0;i<length;i++) if(!((s[i]>='0'&&s[i]<='9')||(s[i]>='a'&&s[i]<='f'))) return 0; return 1; }
static int v2_uuid(s)
const char *s;
{ size_t i; if(!v2_lower_hex("00000000000000000000000000000000",32) || !s || v2_bounded(s,36)!=36) return 0; for(i=0;i<36;i++) { if(i==8||i==13||i==18||i==23) { if(s[i]!='-') return 0; } else if(!((s[i]>='0'&&s[i]<='9')||(s[i]>='a'&&s[i]<='f'))) return 0; } return 1; }
static int v2_world(s)
const char *s;
{ size_t i,n=v2_bounded(s,CHARACTER_SAVE_JOURNAL_V2_WORLD_ID_MAX); if(!n||n>CHARACTER_SAVE_JOURNAL_V2_WORLD_ID_MAX||s[0]<'a'||s[0]>'z') return 0; for(i=1;i<n;i++) if(!((s[i]>='a'&&s[i]<='z')||(s[i]>='0'&&s[i]<='9')||s[i]=='_'||s[i]=='-')) return 0; return 1; }
static int v2_name_hex(w)
const character_save_journal_v2_wire *w;
{
    static const char hex[] = "0123456789abcdef";
    unsigned char decoded[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    unsigned char canonical[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    unsigned char sha1[20], byte;
    size_t i, n, byte_length;
    unsigned long consumed, codepoint, codepoints;
    int all_spaces;

    if(!w) return 0;
    n = v2_bounded(w->legacy_name_key_hex,
                   CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX);
    if(n < 2 || n > CHARACTER_SAVE_JOURNAL_V2_NAME_HEX_MAX || (n & 1) ||
       !v2_lower_hex(w->legacy_name_key_hex, n)) return 0;
    byte_length = n / 2;
    all_spaces = 1;
    for(i = 0; i < n; i += 2) {
        byte = (unsigned char)((w->legacy_name_key_hex[i] <= '9' ?
              w->legacy_name_key_hex[i] - '0' :
              w->legacy_name_key_hex[i] - 'a' + 10) << 4);
        byte |= (unsigned char)(w->legacy_name_key_hex[i + 1] <= '9' ?
                w->legacy_name_key_hex[i + 1] - '0' :
                w->legacy_name_key_hex[i + 1] - 'a' + 10);
        if(!byte || byte < 0x20 || byte == 0x7f || byte == '/' ||
           byte == '\\' || byte == ':') return 0;
        if(byte != ' ') all_spaces = 0;
        decoded[i / 2] = byte;
    }
    decoded[byte_length] = 0;
    if(all_spaces || !utf8_validate(decoded, (unsigned long)byte_length) ||
       !strcmp((const char *)decoded, ".") ||
       !strcmp((const char *)decoded, "..")) return 0;

    i = 0;
    codepoints = 0;
    while(i < byte_length) {
        if(!utf8_next_codepoint(decoded + i,
                                (unsigned long)(byte_length - i),
                                &consumed, &codepoint)) return 0;
        i += (size_t)consumed;
        codepoints++;
    }
    if(codepoints < 1 || codepoints > 12) return 0;

    memcpy(canonical, decoded, byte_length + 1);
    for(i = 0; i < byte_length; i++)
        if(canonical[i] >= 'A' && canonical[i] <= 'Z')
            canonical[i] = (unsigned char)(canonical[i] + ('a' - 'A'));
    if(canonical[0] >= 'a' && canonical[0] <= 'z')
        canonical[0] = (unsigned char)(canonical[0] - ('a' - 'A'));
    if(memcmp(canonical, decoded, byte_length + 1) != 0) return 0;

    v2_legacy_name_sha1(decoded, byte_length, sha1);
    if(w->legacy_shard[0] != hex[sha1[0] >> 4] ||
       w->legacy_shard[1] != hex[sha1[0] & 15]) return 0;
    memset(decoded, 0, sizeof(decoded));
    memset(canonical, 0, sizeof(canonical));
    memset(sha1, 0, sizeof(sha1));
    return 1;
}
static const char *v2_expected_name(state)
character_save_journal_v2_expected_state state;
{ return state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ? "existing" : state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT ? "absent" : 0; }

int character_save_journal_v2_stage_leaf(command_uuid,out,out_size)
const char *command_uuid; char *out; size_t out_size;
{ int n; if(!v2_uuid(command_uuid)||!out) return -1; n=snprintf(out,out_size,"%s.stage",command_uuid); return n<0||(size_t)n>=out_size ? -1 : 0; }

static int v2_wire_valid(w, require_request)
const character_save_journal_v2_wire *w; int require_request;
{
    char leaf[CHARACTER_SAVE_JOURNAL_V2_STAGE_LEAF_MAX+1];
    if(!w || w->state != CHARACTER_SAVE_JOURNAL_V2_PREPARED || !v2_uuid(w->writer_instance_id) || !v2_uuid(w->character_id) || !v2_uuid(w->command_uuid) || !v2_world(w->world_id) || !v2_name_hex(w) || !v2_lower_hex(w->legacy_shard,2) || !w->writer_epoch || w->writer_epoch>(uint64_t)INT64_MAX || !w->writer_revision || w->writer_revision>(uint64_t)INT64_MAX || !w->storage_format || w->storage_format > 32767 || !v2_lower_hex(w->post_sha256,64) || !v2_expected_name(w->expected_state) || (require_request && !v2_lower_hex(w->request_sha256,64)) || character_save_journal_v2_stage_leaf(w->command_uuid,leaf,sizeof(leaf)) != 0) return 0;
    if(w->expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING) return v2_lower_hex(w->expected_sha256,64);
    return !w->expected_sha256[0];
}

int character_save_journal_v2_request_sha256(w,out)
const character_save_journal_v2_wire *w; char out[65];
{
    char envelope[1024], leaf[44]; int n; unsigned char raw[32]; v2_sha256 sha;
    if(!out || !v2_wire_valid(w,0) || character_save_journal_v2_stage_leaf(w->command_uuid,leaf,sizeof(leaf)) != 0) return -1;
    n=snprintf(envelope,sizeof(envelope),"m3-shadow-receipt-v2\nworld_id=%s\ncharacter_id=%s\nlegacy_name_key_hex=%s\nlegacy_shard=%s\ncommand_uuid=%s\nwriter_instance_id=%s\nwriter_epoch=%" PRIu64 "\nwriter_revision=%" PRIu64 "\nexpected_state=%s\nexpected_sha256=%s\npost_sha256=%s\nstorage_format=%u\nstaged_leaf=%s\n",w->world_id,w->character_id,w->legacy_name_key_hex,w->legacy_shard,w->command_uuid,w->writer_instance_id,w->writer_epoch,w->writer_revision,v2_expected_name(w->expected_state),w->expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT?"-":w->expected_sha256,w->post_sha256,(unsigned int)w->storage_format,leaf);
    if(n < 0 || (size_t)n >= sizeof(envelope)) return -1;
    v2_sha_init(&sha);
    v2_sha_update(&sha, (unsigned char *)envelope, (size_t)n);
    v2_sha_final(&sha, raw);
    v2_hex(raw, out);
    return 0;
}

static int v2_dir_ok(fd)
int fd;
{ struct stat st; return fd>=0 && fstat(fd,&st)==0 && S_ISDIR(st.st_mode) && st.st_uid==v2_trusted_uid && (st.st_mode&07777)==0700; }
static int v2_stat_file_ok(st, links)
const struct stat *st;
unsigned int links;
{ return st && links && S_ISREG(st->st_mode) && st->st_uid==v2_trusted_uid && (st->st_mode&07777)==0600 && st->st_nlink==links; }
static int v2_file_ok(fd)
int fd;
{ struct stat st; return fd>=0 && fstat(fd,&st)==0 && v2_stat_file_ok(&st,1); }
static int v2_open_component(parent,name)
int parent; const char *name;
{ struct stat before, after; int fd;
  if(fstatat(parent,name,&before,AT_SYMLINK_NOFOLLOW)<0 || !S_ISDIR(before.st_mode) || (before.st_mode&07777)!=0700 || before.st_uid!=v2_trusted_uid)return -1;
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
  if(v2_pause_component[0] && !strcmp(v2_pause_component,name)) {
      char signal;
      ssize_t count;
      do count=write(v2_component_ready_fd,"x",1);
      while(count<0&&errno==EINTR);
      if(count!=1)return -1;
      do count=read(v2_component_release_fd,&signal,1);
      while(count<0&&errno==EINTR);
      v2_pause_component[0]=0;
      if(count!=1)return -1;
  }
#endif
  fd=openat(parent,name,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC);
  if(!v2_dir_ok(fd) || fstat(fd,&after)<0 || before.st_dev!=after.st_dev || before.st_ino!=after.st_ino) { if(fd>=0) close(fd); return -1; } return fd; }
static int v2_open_root(path)
const char *path;
{
    int fd,next; const char *p,*q; char component[128]; size_t n;
    if(!path || path[0] != '/') return -1;
    fd = open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if(fd < 0) return -1;
    p = path + 1;
    while(*p) { q=strchr(p,'/'); n=q?(size_t)(q-p):strlen(p); if(!n||n>=sizeof(component)||(n==1&&p[0]=='.')||(n==2&&p[0]=='.'&&p[1]=='.')) { close(fd); return -1; } memcpy(component,p,n); component[n]=0; next=openat(fd,component,O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC); close(fd); if(next<0) return -1; fd=next; p=q?q+1:p+n; }
    if(!v2_dir_ok(fd)) { close(fd); return -1; } return fd;
}
static void v2_tree_close(t)
v2_tree *t;
{ if(t->stage_fd>=0)close(t->stage_fd); if(t->journal_fd>=0)close(t->journal_fd); if(t->shard_fd>=0)close(t->shard_fd); if(t->player_fd>=0)close(t->player_fd); if(t->root_fd>=0)close(t->root_fd); memset(t,0,sizeof(*t)); t->root_fd=t->player_fd=t->shard_fd=t->journal_fd=t->stage_fd=-1; }
/* The descriptor forms are capability boundaries: callers retain their held
 * root descriptor and this slice owns only an atomic CLOEXEC duplicate. */
static int v2_tree_open_fd(root_fd,shard,t)
int root_fd; const char *shard; v2_tree *t;
{ memset(t,0,sizeof(*t)); t->root_fd=t->player_fd=t->shard_fd=t->journal_fd=t->stage_fd=-1;
  if(root_fd<0 || (t->root_fd=fcntl(root_fd,F_DUPFD_CLOEXEC,0))<0 || !v2_dir_ok(t->root_fd))goto bad;
  t->player_fd=v2_open_component(t->root_fd,"player"); if(t->player_fd<0)goto bad;
  t->shard_fd=v2_open_component(t->player_fd,shard); if(t->shard_fd<0)goto bad;
  t->journal_fd=v2_open_component(t->root_fd,"character-save-journal"); if(t->journal_fd<0)goto bad;
  t->stage_fd=v2_open_component(t->root_fd,"character-save-stage"); if(t->stage_fd<0)goto bad;
  return 0;
bad:v2_tree_close(t);return -1; }
static int v2_journal_tree_open_fd(root_fd,t)
int root_fd; v2_tree *t;
{ memset(t,0,sizeof(*t));t->root_fd=t->player_fd=t->shard_fd=t->journal_fd=t->stage_fd=-1;
  if(root_fd<0 || (t->root_fd=fcntl(root_fd,F_DUPFD_CLOEXEC,0))<0 || !v2_dir_ok(t->root_fd))goto bad;
  t->journal_fd=v2_open_component(t->root_fd,"character-save-journal");if(t->journal_fd<0)goto bad;return 0;
bad:v2_tree_close(t);return -1; }

static ssize_t v2_write_operation(fd, bytes, length)
int fd;
const void *bytes;
size_t length;
{
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
    if(v2_write_eintr_once) {
        v2_write_eintr_once = 0;
        errno = EINTR;
        return -1;
    }
    if(v2_write_zero_once) {
        v2_write_zero_once = 0;
        return 0;
    }
    if(v2_write_eio_once) {
        v2_write_eio_once = 0;
        errno = EIO;
        return -1;
    }
    if(v2_write_short_once && length > 1) {
        v2_write_short_once = 0;
        return write(fd, bytes, length / 2);
    }
#endif
    return write(fd, bytes, length);
}

static int v2_write_all(fd,bytes,length)
int fd; const void *bytes; size_t length;
{ const char *p=(const char *)bytes; ssize_t n; while(length) { n=v2_write_operation(fd,p,length); if(n<0&&errno==EINTR)continue; if(n<=0)return -1; p+=n; length-=(size_t)n; } return 0; }

static int v2_close_file(fd, kind)
int fd, kind;
{
    int result;
    (void)kind;
    result = close(fd);
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
    if(v2_fail_close_kind == kind) {
        v2_fail_close_kind = 0;
        errno = EIO;
        return -1;
    }
#endif
    return result;
}
static int v2_sync(fd,kind)
int fd,kind;
{
  int r;
  (void)kind;
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
    if((kind==1&&v2_fail_stage_file)||(kind==2&&v2_fail_stage_dir)||(kind==3&&v2_fail_journal_file)||(kind==4&&v2_fail_journal_dir)){errno=EIO;return -1;}
    if(kind==1)v2_stage_file_syncs++; else if(kind==2)v2_stage_dir_syncs++; else if(kind==3)v2_journal_file_syncs++; else if(kind==4)v2_journal_dir_syncs++;
#endif
    do r=fsync(fd); while(r<0&&errno==EINTR); return r;
}

static int v2_hash_fd_links(fd,out,links)
int fd; char out[65]; unsigned int links;
{ struct stat st,after; unsigned char buffer[8192], raw[32]; uint64_t total=0; ssize_t n; v2_sha256 sha;
  if(!out||!links||fstat(fd,&st)<0||!v2_stat_file_ok(&st,links)||st.st_size<0||(uint64_t)st.st_size>CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES||lseek(fd,0,SEEK_SET)<0)return -1;
  #ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
  if(v2_hash_ready_fd>=0){do n=write(v2_hash_ready_fd,"x",1);while(n<0&&errno==EINTR);if(n!=1)return -1;if(v2_hash_release_fd>=0){char x;do n=read(v2_hash_release_fd,&x,1);while(n<0&&errno==EINTR);if(n!=1)return -1;}}
  #endif
  v2_sha_init(&sha); for(;;){n=read(fd,buffer,sizeof(buffer));if(n<0&&errno==EINTR)continue;if(n<0)return -1;if(!n)break;if((uint64_t)n>CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES-total){errno=EFBIG;return -1;}v2_sha_update(&sha,buffer,(size_t)n);total+=(uint64_t)n;}if(fstat(fd,&after)<0||!v2_stat_file_ok(&after,links)||after.st_dev!=st.st_dev||after.st_ino!=st.st_ino||after.st_size!=st.st_size){errno=EAGAIN;return -1;}v2_sha_final(&sha,raw);v2_hex(raw,out);return 0; }

int character_save_journal_v2_hash_fd(fd,out)
int fd; char out[65];
{ return v2_hash_fd_links(fd,out,1); }

int character_save_journal_v2_hash_fd_two_links(fd,out)
int fd; char out[65];
{ return v2_hash_fd_links(fd,out,2); }

/* This is deliberately shared by the path and held-descriptor preparation
 * paths.  A PREPARED record is evidence of an observed live precondition,
 * not merely a later publish-time hope. */
static int v2_decode_live_leaf(wire, leaf)
const character_save_journal_v2_wire *wire;
char leaf[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
{
    size_t i, count;
    unsigned int high, low;
    if(!wire || !leaf || !v2_name_hex(wire)) return -1;
    count = strlen(wire->legacy_name_key_hex) / 2;
    for(i = 0; i < count; i++) {
        high = wire->legacy_name_key_hex[i * 2] <= '9' ?
            (unsigned int)(wire->legacy_name_key_hex[i * 2] - '0') :
            (unsigned int)(wire->legacy_name_key_hex[i * 2] - 'a' + 10);
        low = wire->legacy_name_key_hex[i * 2 + 1] <= '9' ?
            (unsigned int)(wire->legacy_name_key_hex[i * 2 + 1] - '0') :
            (unsigned int)(wire->legacy_name_key_hex[i * 2 + 1] - 'a' + 10);
        leaf[i] = (char)((high << 4) | low);
    }
    leaf[count] = 0;
    return 0;
}

static int v2_live_precondition(tree, wire)
v2_tree *tree;
const character_save_journal_v2_wire *wire;
{
    char leaf[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX + 1];
    char digest[CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN + 1];
    int fd = -1, result = -1;
    if(!tree || !wire || v2_decode_live_leaf(wire, leaf) != 0) goto done;
    fd = openat(tree->shard_fd, leaf,
                O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(fd < 0) {
        if(errno == ENOENT &&
           wire->expected_state == CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT)
            result = 0;
        goto done;
    }
#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
    if(v2_live_ready_fd >= 0) {
        char signal;
        ssize_t count;
        do count = write(v2_live_ready_fd, "x", 1);
        while(count < 0 && errno == EINTR);
        if(count != 1) goto done;
        do count = read(v2_live_release_fd, &signal, 1);
        while(count < 0 && errno == EINTR);
        v2_live_ready_fd = -1;
        if(count != 1) goto done;
    }
#endif
    if(wire->expected_state != CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ||
       character_save_journal_v2_hash_fd(fd, digest) != 0 ||
       strcmp(digest, wire->expected_sha256) != 0) goto done;
    if(v2_close_file(fd, 2) != 0) {
        fd = -1;
        goto done;
    }
    fd = -1;
    result = 0;
done:
    if(fd >= 0) close(fd);
    memset(leaf, 0, sizeof(leaf));
    memset(digest, 0, sizeof(digest));
    return result;
}

int character_save_journal_v2_copy_existing_at(int root_fd,
    const character_save_journal_v2_wire *wire, unsigned char *buffer,
    size_t capacity, size_t *length_out)
{
    v2_tree tree;
    struct stat before, after, named;
    char leaf[CHARACTER_SAVE_JOURNAL_V2_NAME_MAX+1], digest[65];
    unsigned char raw[32], extra;
    v2_sha256 sha;
    size_t total=0, i;
    ssize_t count;
    int fd=-1, result=-1;
    if(length_out) *length_out=0;
    if(!length_out || !buffer || !capacity ||
       capacity>CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES ||
       !v2_wire_valid(wire,1) ||
       wire->expected_state!=CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING ||
       v2_decode_live_leaf(wire,leaf) ||
       v2_tree_open_fd(root_fd,wire->legacy_shard,&tree)) return -1;
    fd=openat(tree.shard_fd,leaf,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
    if(fd<0 || fstat(fd,&before) || !v2_stat_file_ok(&before,1) ||
       before.st_size<=0 || (uint64_t)before.st_size>(uint64_t)capacity) goto done;
    while(total<(size_t)before.st_size) {
        count=read(fd,buffer+total,(size_t)before.st_size-total);
        if(count<0 && errno==EINTR) continue;
        if(count<=0) goto done;
        total+=(size_t)count;
    }
    do count=read(fd,&extra,1); while(count<0 && errno==EINTR);
    if(count!=0 || fstat(fd,&after) || !v2_stat_file_ok(&after,1) ||
       before.st_size!=after.st_size ||
       fstatat(tree.shard_fd,leaf,&named,AT_SYMLINK_NOFOLLOW) ||
       !v2_stat_file_ok(&named,1) || named.st_dev!=after.st_dev ||
       named.st_ino!=after.st_ino) goto done;
    /* Hash the bytes returned, not a second read of a mutable file. */
    v2_sha_init(&sha); v2_sha_update(&sha,buffer,total);
    v2_sha_final(&sha,raw); v2_hex(raw,digest);
    if(strcmp(digest,wire->expected_sha256)) goto done;
    if(v2_close_file(fd,2)) { fd=-1; goto done; }
    fd=-1; *length_out=total; result=0;
done:
    if(fd>=0) close(fd);
    v2_tree_close(&tree);
    if(result) {
        volatile unsigned char *wipe=buffer;
        for(i=0;i<total;i++) wipe[i]=0;
    }
    memset(raw,0,sizeof(raw)); memset(&sha,0,sizeof(sha));
    memset(digest,0,sizeof(digest));
    return result;
}

static int v2_format(w,out,out_size)
const character_save_journal_v2_wire *w; char *out; size_t out_size;
{ char leaf[44]; int n; if(character_save_journal_v2_stage_leaf(w->command_uuid,leaf,sizeof(leaf))!=0)return -1; n=snprintf(out,out_size,"version=2\nstate=PREPARED\nwriter_instance_id=%s\ncharacter_id=%s\nrequest_sha256=%s\nworld_id=%s\nlegacy_name_key_hex=%s\nlegacy_shard=%s\ncommand_uuid=%s\nwriter_epoch=%" PRIu64 "\nwriter_revision=%" PRIu64 "\nexpected_state=%s\nexpected_sha256=%s\npost_sha256=%s\nstorage_format=%u\nstaged_leaf=%s\n",w->writer_instance_id,w->character_id,w->request_sha256,w->world_id,w->legacy_name_key_hex,w->legacy_shard,w->command_uuid,w->writer_epoch,w->writer_revision,v2_expected_name(w->expected_state),w->expected_state==CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT?"-":w->expected_sha256,w->post_sha256,(unsigned int)w->storage_format,leaf);return n<0||(size_t)n>=out_size?-1:n; }

int character_save_journal_v2_stage_at(root_fd,w,stage_bytes,stage_length)
int root_fd; const character_save_journal_v2_wire *w; const void *stage_bytes;
size_t stage_length;
{
    v2_tree t;
    char request[65], leaf[44], digest[65];
    int stage_fd = -1, result = -1;
    if(!stage_bytes || stage_length > CHARACTER_SAVE_JOURNAL_V2_READ_MAX_BYTES ||
       !v2_wire_valid(w,1) ||
       character_save_journal_v2_request_sha256(w,request) != 0 ||
       strcmp(request,w->request_sha256) ||
       character_save_journal_v2_stage_leaf(w->command_uuid,leaf,sizeof(leaf)) != 0 ||
       v2_tree_open_fd(root_fd,w->legacy_shard,&t) != 0) return -1;
    stage_fd = openat(t.stage_fd,leaf,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|
                      O_NONBLOCK|O_CLOEXEC,0600);
    if(!v2_file_ok(stage_fd) || v2_write_all(stage_fd,stage_bytes,stage_length) != 0 ||
       v2_sync(stage_fd,1) != 0) goto out;
    v2_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_FILE_FSYNC);
    if(v2_close_file(stage_fd,1) != 0) {
        stage_fd = -1;
        goto out;
    }
    stage_fd = -1;
    if(v2_sync(t.stage_fd,2) != 0) goto out;
    v2_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_STAGE_DIR_FSYNC);
    stage_fd = openat(t.stage_fd,leaf,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
    if(character_save_journal_v2_hash_fd(stage_fd,digest) != 0 ||
       strcmp(digest,w->post_sha256)) goto out;
    if(v2_close_file(stage_fd,2) != 0) {
        stage_fd = -1;
        goto out;
    }
    stage_fd = -1;
    result = 0;
out:
    if(stage_fd >= 0) close(stage_fd);
    memset(request,0,sizeof(request));
    memset(leaf,0,sizeof(leaf));
    memset(digest,0,sizeof(digest));
    v2_tree_close(&t);
    return result;
}

int character_save_journal_v2_prepare_absent_shard_at(root_fd,w)
int root_fd; const character_save_journal_v2_wire *w;
{
    v2_tree t;
    int result=-1;
    memset(&t,0,sizeof(t));
    t.root_fd=t.player_fd=t.shard_fd=t.journal_fd=t.stage_fd=-1;
    /* Validate the complete canonical name/shard wire before any mkdir. */
    if(!v2_wire_valid(w,1)||w->expected_state!=CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT)
        return -1;
    t.root_fd=fcntl(root_fd,F_DUPFD_CLOEXEC,0);
    if(!v2_dir_ok(t.root_fd)) goto done;
    t.player_fd=v2_open_component(t.root_fd,"player");
    t.journal_fd=v2_open_component(t.root_fd,"character-save-journal");
    t.stage_fd=v2_open_component(t.root_fd,"character-save-stage");
    if(t.player_fd<0||t.journal_fd<0||t.stage_fd<0) goto done;
    if(mkdirat(t.player_fd,w->legacy_shard,0700)!=0&&errno!=EEXIST) goto done;
    t.shard_fd=v2_open_component(t.player_fd,w->legacy_shard);
    if(t.shard_fd<0) goto done;
    /* Sync even an existing directory: a previous attempt may have stopped
     * between mkdir and parent fsync. No player bytes are written here. */
    if(fsync(t.shard_fd)!=0||fsync(t.player_fd)!=0) goto done;
    result=0;
done:
    v2_tree_close(&t);
    return result;
}

int character_save_journal_v2_live_precondition_at(root_fd,w)
int root_fd; const character_save_journal_v2_wire *w;
{
    v2_tree t;
    int result;
    if(!v2_wire_valid(w,1) || v2_tree_open_fd(root_fd,w->legacy_shard,&t) != 0)
        return -1;
    result = v2_live_precondition(&t,w);
    v2_tree_close(&t);
    return result;
}

static int v2_staged_bytes_match(tree,w)
v2_tree *tree;
const character_save_journal_v2_wire *w;
{
    char leaf[44], digest[65];
    int fd = -1, result = -1;
    if(!tree || !w ||
       character_save_journal_v2_stage_leaf(w->command_uuid,leaf,sizeof(leaf)) != 0)
        goto out;
    fd = openat(tree->stage_fd,leaf,O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC);
    if(character_save_journal_v2_hash_fd(fd,digest) != 0 ||
       strcmp(digest,w->post_sha256)) goto out;
    if(v2_close_file(fd,2) != 0) {
        fd = -1;
        goto out;
    }
    fd = -1;
    result = 0;
out:
    if(fd >= 0) close(fd);
    memset(leaf,0,sizeof(leaf));
    memset(digest,0,sizeof(digest));
    return result;
}

int character_save_journal_v2_commit_prepared_at(root_fd,w)
int root_fd; const character_save_journal_v2_wire *w;
{
    v2_tree t;
    char request[65], jleaf[48], text[V2_TEXT_MAX];
    int journal_fd = -1, result = -1, n;
    if(!v2_wire_valid(w,1) ||
       character_save_journal_v2_request_sha256(w,request) != 0 ||
       strcmp(request,w->request_sha256) ||
       v2_tree_open_fd(root_fd,w->legacy_shard,&t) != 0) return -1;
    /* The stage was made durable before the live observation.  Re-read it at
     * the commit boundary so a replaced or altered stage can never acquire a
     * PREPARED record after that observation. */
    if(v2_staged_bytes_match(&t,w) != 0) goto out;
    n = snprintf(jleaf,sizeof(jleaf),"%s.prepared",w->command_uuid);
    if(n < 0 || (size_t)n >= sizeof(jleaf) || v2_format(w,text,sizeof(text)) < 0)
        goto out;
    journal_fd = openat(t.journal_fd,jleaf,O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW|
                        O_NONBLOCK|O_CLOEXEC,0600);
    if(!v2_file_ok(journal_fd) ||
       v2_write_all(journal_fd,text,strlen(text)) != 0 ||
       v2_sync(journal_fd,3) != 0) goto out;
    v2_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_FILE_FSYNC);
    if(v2_close_file(journal_fd,3) != 0) {
        journal_fd = -1;
        goto out;
    }
    journal_fd = -1;
    if(v2_sync(t.journal_fd,4) != 0) goto out;
    v2_crash_after(CHARACTER_SAVE_JOURNAL_V2_CRASH_PREPARED_JOURNAL_DIR_FSYNC);
    result = 0;
out:
    if(journal_fd >= 0) close(journal_fd);
    memset(request,0,sizeof(request));
    memset(jleaf,0,sizeof(jleaf));
    memset(text,0,sizeof(text));
    v2_tree_close(&t);
    return result;
}

int character_save_journal_v2_prepare_at(root_fd,w,stage_bytes,stage_length)
int root_fd; const character_save_journal_v2_wire *w; const void *stage_bytes;
size_t stage_length;
{
    if(character_save_journal_v2_stage_at(root_fd,w,stage_bytes,stage_length) != 0 ||
       character_save_journal_v2_live_precondition_at(root_fd,w) != 0 ||
       character_save_journal_v2_commit_prepared_at(root_fd,w) != 0) return -1;
    return 0;
}

int character_save_journal_v2_prepare(root,w,stage_bytes,stage_length)
const char *root; const character_save_journal_v2_wire *w; const void *stage_bytes; size_t stage_length;
{
    int root_fd, result;
    root_fd = v2_open_root(root);
    if(root_fd < 0) return -1;
    result = character_save_journal_v2_prepare_at(root_fd, w, stage_bytes,
                                                   stage_length);
    if(close(root_fd) != 0) result = -1;
    return result;
}

static int v2_parse_u64(s,out)
const char *s; uint64_t *out;
{ uint64_t v=0,d; size_t i,n=v2_bounded(s,19);if(!n||n>19||(n>1&&s[0]=='0'))return -1;for(i=0;i<n;i++){if(s[i]<'0'||s[i]>'9')return -1;d=s[i]-'0';if(v>((uint64_t)INT64_MAX-d)/10)return -1;v=v*10+d;}if(!v)return -1;*out=v;return 0; }

static int v2_copy_text(destination, destination_size, source)
char *destination;
size_t destination_size;
const char *source;
{
    size_t length;
    if(!destination || !destination_size || !source) return -1;
    length = v2_bounded(source, destination_size - 1);
    if(length > destination_size - 1) return -1;
    memcpy(destination, source, length + 1);
    return 0;
}

static int v2_parse(text,w)
char *text; character_save_journal_v2_wire *w;
{
    static const char *keys[] = {
        "version=", "state=", "writer_instance_id=", "character_id=",
        "request_sha256=", "world_id=", "legacy_name_key_hex=",
        "legacy_shard=", "command_uuid=", "writer_epoch=",
        "writer_revision=", "expected_state=", "expected_sha256=",
        "post_sha256=", "storage_format=", "staged_leaf="
    };
    character_save_journal_v2_wire parsed;
    char *line, *next, *values[16], leaf[44], request[65];
    uint64_t storage_format;
    int i;

    if(!text || !w) return -1;
    memset(&parsed, 0, sizeof(parsed));
    line = text;
    for(i = 0; i < 16; i++) {
        next = strchr(line, '\n');
        if(!next || strncmp(line, keys[i], strlen(keys[i]))) goto bad;
        *next = 0;
        values[i] = line + strlen(keys[i]);
        line = next + 1;
    }
    if(*line || strcmp(values[0], "2") ||
       strcmp(values[1], "PREPARED")) goto bad;

    parsed.state = CHARACTER_SAVE_JOURNAL_V2_PREPARED;
    if(v2_copy_text(parsed.writer_instance_id,
                    sizeof(parsed.writer_instance_id), values[2]) != 0 ||
       v2_copy_text(parsed.character_id,
                    sizeof(parsed.character_id), values[3]) != 0 ||
       v2_copy_text(parsed.request_sha256,
                    sizeof(parsed.request_sha256), values[4]) != 0 ||
       v2_copy_text(parsed.world_id,
                    sizeof(parsed.world_id), values[5]) != 0 ||
       v2_copy_text(parsed.legacy_name_key_hex,
                    sizeof(parsed.legacy_name_key_hex), values[6]) != 0 ||
       v2_copy_text(parsed.legacy_shard,
                    sizeof(parsed.legacy_shard), values[7]) != 0 ||
       v2_copy_text(parsed.command_uuid,
                    sizeof(parsed.command_uuid), values[8]) != 0 ||
       v2_parse_u64(values[9], &parsed.writer_epoch) != 0 ||
       v2_parse_u64(values[10], &parsed.writer_revision) != 0) goto bad;

    if(!strcmp(values[11], "existing")) {
        parsed.expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_EXISTING;
        if(!v2_lower_hex(values[12], CHARACTER_SAVE_JOURNAL_V2_HASH_HEX_LEN) ||
           v2_copy_text(parsed.expected_sha256,
                        sizeof(parsed.expected_sha256), values[12]) != 0) goto bad;
    }
    else if(!strcmp(values[11], "absent")) {
        parsed.expected_state = CHARACTER_SAVE_JOURNAL_V2_EXPECT_ABSENT;
        if(strcmp(values[12], "-")) goto bad;
    }
    else goto bad;

    if(v2_copy_text(parsed.post_sha256,
                    sizeof(parsed.post_sha256), values[13]) != 0 ||
       v2_parse_u64(values[14], &storage_format) != 0 ||
       storage_format > 32767) goto bad;
    parsed.storage_format = (uint16_t)storage_format;
    if(character_save_journal_v2_stage_leaf(parsed.command_uuid, leaf,
                                             sizeof(leaf)) != 0 ||
       strcmp(leaf, values[15]) || !v2_wire_valid(&parsed, 1) ||
       character_save_journal_v2_request_sha256(&parsed, request) != 0 ||
       strcmp(request, parsed.request_sha256)) goto bad;

    *w = parsed;
    memset(&parsed, 0, sizeof(parsed));
    return 0;
bad:
    memset(&parsed, 0, sizeof(parsed));
    memset(w, 0, sizeof(*w));
    return -1;
}

int character_save_journal_v2_read_prepared_at(root_fd,command_uuid,out)
int root_fd; const char *command_uuid; character_save_journal_v2_wire *out;
{
    v2_tree tree, verify;
    character_save_journal_v2_wire parsed;
    char leaf[48], text[V2_TEXT_MAX], extra;
    int fd, formatted, result;
    size_t count;
    ssize_t received;

    if(!out) return -1;
    memset(out, 0, sizeof(*out));
    memset(&parsed, 0, sizeof(parsed));
    fd = -1;
    result = -1;
    count = 0;
    if(!v2_uuid(command_uuid) || v2_journal_tree_open_fd(root_fd, &tree) != 0)
        return -1;
    formatted = snprintf(leaf, sizeof(leaf), "%s.prepared", command_uuid);
    if(formatted < 0 || (size_t)formatted >= sizeof(leaf)) goto out;
    fd = openat(tree.journal_fd, leaf,
                O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC);
    if(!v2_file_ok(fd)) goto out;

    while(count < sizeof(text) - 1) {
        received = read(fd, text + count, sizeof(text) - 1 - count);
        if(received < 0 && errno == EINTR) continue;
        if(received < 0) goto out;
        if(received == 0) break;
        count += (size_t)received;
    }
    if(count == sizeof(text) - 1) {
        do received = read(fd, &extra, 1);
        while(received < 0 && errno == EINTR);
        if(received != 0) goto out;
    }
    if(memchr(text, 0, count) != 0) goto out;
    text[count] = 0;
    if(v2_parse(text, &parsed) != 0 ||
       strcmp(parsed.command_uuid, command_uuid)) goto out;
    if(v2_close_file(fd, 4) != 0) {
        fd = -1;
        goto out;
    }
    fd = -1;
    if(v2_tree_open_fd(root_fd, parsed.legacy_shard, &verify) != 0) goto out;
    v2_tree_close(&verify);
    *out = parsed;
    result = 0;
out:
    if(fd >= 0) close(fd);
    if(result != 0) memset(out, 0, sizeof(*out));
    memset(&parsed, 0, sizeof(parsed));
    memset(text, 0, sizeof(text));
    v2_tree_close(&tree);
    return result;
}

int character_save_journal_v2_read_prepared(root,command_uuid,out)
const char *root; const char *command_uuid; character_save_journal_v2_wire *out;
{
    int root_fd, result;
    if(out) memset(out, 0, sizeof(*out));
    root_fd = v2_open_root(root);
    if(root_fd < 0) return -1;
    result = character_save_journal_v2_read_prepared_at(root_fd, command_uuid, out);
    if(close(root_fd) != 0) {
        if(out) memset(out, 0, sizeof(*out));
        result = -1;
    }
    return result;
}

#ifdef CHARACTER_SAVE_JOURNAL_V2_TESTING
void character_save_journal_v2_set_trusted_uid_for_test(uid_t uid){v2_trusted_uid=uid;}
void character_save_journal_v2_fail_fsync_for_test(int a,int b,int c,int d){v2_fail_stage_file=a!=0;v2_fail_stage_dir=b!=0;v2_fail_journal_file=c!=0;v2_fail_journal_dir=d!=0;}
void character_save_journal_v2_fsync_counts_for_test(unsigned int *a,unsigned int *b,unsigned int *c,unsigned int *d){if(a)*a=v2_stage_file_syncs;if(b)*b=v2_stage_dir_syncs;if(c)*c=v2_journal_file_syncs;if(d)*d=v2_journal_dir_syncs;}
void character_save_journal_v2_pause_hash_after_fstat_for_test(int ready,int release){v2_hash_ready_fd=ready;v2_hash_release_fd=release;}
void character_save_journal_v2_pause_live_precondition_after_open_for_test(int ready,int release){v2_live_ready_fd=ready;v2_live_release_fd=release;}
void character_save_journal_v2_pause_component_after_lstat_for_test(component,ready,release)
const char *component; int ready,release;
{ size_t length=component?strlen(component):0;memset(v2_pause_component,0,sizeof(v2_pause_component));if(length&&length<sizeof(v2_pause_component))memcpy(v2_pause_component,component,length+1);v2_component_ready_fd=ready;v2_component_release_fd=release; }
void character_save_journal_v2_write_faults_for_test(eintr_once,short_once,zero_once,eio_once)
int eintr_once,short_once,zero_once,eio_once;
{ v2_write_eintr_once=eintr_once!=0;v2_write_short_once=short_once!=0;v2_write_zero_once=zero_once!=0;v2_write_eio_once=eio_once!=0; }
void character_save_journal_v2_fail_close_once_for_test(int close_kind){v2_fail_close_kind=close_kind;}
#endif
