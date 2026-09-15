#include "onboarding_session.h"
#include "mtype.h"
#include "player_path.h"
#include "trusted_admission.h"

#include <fcntl.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#ifndef O_BINARY
#define O_BINARY 0
#endif

#ifndef O_NONBLOCK
#define O_NONBLOCK 0
#endif

#define ONBOARDING_SESSION_REPLAY_LIMIT (2 * PMAX)

typedef unsigned int os_u32;

typedef struct onboarding_replay_entry {
    char nonce[ONBOARDING_ADMISSION_NONCE_LEN + 1];
    long expires_at;
} onboarding_replay_entry;

typedef struct os_sha256_ctx {
    os_u32 state[8];
    os_u32 bit_hi;
    os_u32 bit_lo;
    unsigned char block[64];
    unsigned int used;
} os_sha256_ctx;

static onboarding_replay_entry replay_cache[ONBOARDING_SESSION_REPLAY_LIMIT];
static long replay_now;

static os_u32 os_rotr(value, shift)
os_u32 value;
unsigned int shift;
{
    return (value >> shift) | (value << (32 - shift));
}

static os_u32 os_load32(value)
const unsigned char *value;
{
    return ((os_u32)value[0] << 24) | ((os_u32)value[1] << 16) |
           ((os_u32)value[2] << 8) | (os_u32)value[3];
}

static void os_store32(value, out)
os_u32 value;
unsigned char *out;
{
    out[0] = (unsigned char)(value >> 24);
    out[1] = (unsigned char)(value >> 16);
    out[2] = (unsigned char)(value >> 8);
    out[3] = (unsigned char)value;
}

static void os_sha256_block(ctx, block)
os_sha256_ctx *ctx;
const unsigned char *block;
{
    static const os_u32 k[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,
        0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,
        0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,
        0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,
        0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,
        0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,
        0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,
        0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,
        0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
    };
    os_u32 w[64], a, b, c, d, e, f, g, h, t1, t2;
    unsigned int i;

    for(i=0; i<16; i++) w[i] = os_load32(block + 4 * i);
    for(i=16; i<64; i++) {
        os_u32 s0 = os_rotr(w[i-15], 7) ^ os_rotr(w[i-15], 18) ^ (w[i-15] >> 3);
        os_u32 s1 = os_rotr(w[i-2], 17) ^ os_rotr(w[i-2], 19) ^ (w[i-2] >> 10);
        w[i] = w[i-16] + s0 + w[i-7] + s1;
    }
    a=ctx->state[0]; b=ctx->state[1]; c=ctx->state[2]; d=ctx->state[3];
    e=ctx->state[4]; f=ctx->state[5]; g=ctx->state[6]; h=ctx->state[7];
    for(i=0; i<64; i++) {
        os_u32 s1 = os_rotr(e, 6) ^ os_rotr(e, 11) ^ os_rotr(e, 25);
        os_u32 ch = (e & f) ^ ((~e) & g);
        os_u32 s0 = os_rotr(a, 2) ^ os_rotr(a, 13) ^ os_rotr(a, 22);
        os_u32 maj = (a & b) ^ (a & c) ^ (b & c);
        t1 = h + s1 + ch + k[i] + w[i];
        t2 = s0 + maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    ctx->state[0]+=a; ctx->state[1]+=b; ctx->state[2]+=c; ctx->state[3]+=d;
    ctx->state[4]+=e; ctx->state[5]+=f; ctx->state[6]+=g; ctx->state[7]+=h;
}

static void os_sha256_init(ctx)
os_sha256_ctx *ctx;
{
    ctx->state[0]=0x6a09e667U; ctx->state[1]=0xbb67ae85U;
    ctx->state[2]=0x3c6ef372U; ctx->state[3]=0xa54ff53aU;
    ctx->state[4]=0x510e527fU; ctx->state[5]=0x9b05688cU;
    ctx->state[6]=0x1f83d9abU; ctx->state[7]=0x5be0cd19U;
    ctx->bit_hi = ctx->bit_lo = 0;
    ctx->used = 0;
}

static void os_sha256_add_bits(ctx, bytes)
os_sha256_ctx *ctx;
unsigned int bytes;
{
    os_u32 old = ctx->bit_lo;
    ctx->bit_lo += ((os_u32)bytes << 3);
    if(ctx->bit_lo < old) ctx->bit_hi++;
    ctx->bit_hi += ((os_u32)bytes >> 29);
}

static void os_sha256_update(ctx, data, length)
os_sha256_ctx *ctx;
const unsigned char *data;
unsigned long length;
{
    unsigned int take;
    while(length) {
        take = 64 - ctx->used;
        if(take > length) take = (unsigned int)length;
        memcpy(ctx->block + ctx->used, data, take);
        ctx->used += take;
        data += take;
        length -= take;
        os_sha256_add_bits(ctx, take);
        if(ctx->used == 64) {
            os_sha256_block(ctx, ctx->block);
            ctx->used = 0;
        }
    }
}

static void os_sha256_final(ctx, out)
os_sha256_ctx *ctx;
unsigned char out[32];
{
    unsigned int i;
    os_u32 hi = ctx->bit_hi, lo = ctx->bit_lo;
    ctx->block[ctx->used++] = 0x80;
    if(ctx->used > 56) {
        while(ctx->used < 64) ctx->block[ctx->used++] = 0;
        os_sha256_block(ctx, ctx->block);
        ctx->used = 0;
    }
    while(ctx->used < 56) ctx->block[ctx->used++] = 0;
    os_store32(hi, ctx->block + 56);
    os_store32(lo, ctx->block + 60);
    os_sha256_block(ctx, ctx->block);
    for(i=0; i<8; i++) os_store32(ctx->state[i], out + 4 * i);
}

static int consume_nonce(context, nonce, expires_at)
void *context;
const char *nonce;
long expires_at;
{
    int i, empty;

    (void)context;
    if(!nonce) return -1;
    empty = -1;
    for(i=0; i<ONBOARDING_SESSION_REPLAY_LIMIT; i++) {
        if(replay_cache[i].nonce[0] && replay_cache[i].expires_at < replay_now)
            replay_cache[i].nonce[0] = 0;
        if(replay_cache[i].nonce[0]) {
            if(strcmp(replay_cache[i].nonce, nonce) == 0) return -1;
        }
        else if(empty < 0) empty = i;
    }
    if(empty < 0) return -1;
    strcpy(replay_cache[empty].nonce, nonce);
    replay_cache[empty].expires_at = expires_at;
    return 0;
}

int onboarding_session_mode(void)
{
    const char *enabled;

    enabled = getenv("MUD_ENABLE_ONBOARDING");
    if(!enabled || strcmp(enabled, "0") == 0) return 0;
    if(strcmp(enabled, "1") != 0) return -1;
    return trusted_admission_mode() == 1 ? 1 : -1;
}

int onboarding_session_validate_ticket(line, secret, now, ticket)
const char *line;
const char *secret;
long now;
onboarding_admission_ticket *ticket;
{
    replay_now = now;
    return onboarding_admission_validate_ticket(line, secret, now,
                                                consume_nonce, 0, ticket);
}

void onboarding_session_reset_for_test(void)
{
    memset(replay_cache, 0, sizeof(replay_cache));
    replay_now = 0;
}

int onboarding_session_is_protocol_line(line)
const unsigned char *line;
{
    return line && !strncmp((const char *)line, "MUD1O", 5);
}

int onboarding_session_file_sha256(name, out)
const char *name;
char out[ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1];
{
    unsigned char data[4096], digest[32];
    static const char hex[] = "0123456789abcdef";
    os_sha256_ctx ctx;
    struct stat st;
    int fd, n;
    unsigned long total;
    unsigned int i;

    if(out) memset(out, 0, ONBOARDING_ADMISSION_SHA256_HEX_LEN + 1);
    if(!name || !out)
        return -1;
    fd = player_path_open_readonly(name);
    if(fd < 0) return -1;
    if(fstat(fd, &st) < 0 || !S_ISREG(st.st_mode)) {
        close(fd);
        return -1;
    }
    os_sha256_init(&ctx);
    total = 0;
    while((n = read(fd, data, sizeof(data))) > 0) {
        if((unsigned long)n > PLAYER_PATH_READ_MAX_BYTES - total) {
            close(fd);
            memset(data, 0, sizeof(data));
            memset(digest, 0, sizeof(digest));
            memset(&ctx, 0, sizeof(ctx));
            return -1;
        }
        total += (unsigned long)n;
        os_sha256_update(&ctx, data, (unsigned long)n);
    }
    if(n < 0) {
        close(fd);
        memset(data, 0, sizeof(data));
        return -1;
    }
    if(close(fd) < 0) {
        memset(data, 0, sizeof(data));
        return -1;
    }
    os_sha256_final(&ctx, digest);
    for(i=0; i<sizeof(digest); i++) {
        out[2*i] = hex[digest[i] >> 4];
        out[2*i+1] = hex[digest[i] & 15];
    }
    out[ONBOARDING_ADMISSION_SHA256_HEX_LEN] = 0;
    memset(data, 0, sizeof(data));
    memset(digest, 0, sizeof(digest));
    memset(&ctx, 0, sizeof(ctx));
    return 0;
}

int onboarding_session_claim_allow_live(challenged_at, now)
long challenged_at;
long now;
{
    if(challenged_at <= 0 || now < challenged_at) return 0;
    if(challenged_at > LONG_MAX - ONBOARDING_CLAIM_ALLOW_WINDOW_SECONDS)
        return 0;
    return now <= challenged_at + ONBOARDING_CLAIM_ALLOW_WINDOW_SECONDS;
}

void onboarding_session_zeroize_claim_memory(password, password_size,
                                              input, input_size)
void *password;
unsigned long password_size;
void *input;
unsigned long input_size;
{
    volatile unsigned char *cursor;
    unsigned long i;

    cursor = (volatile unsigned char *)password;
    if(cursor)
        for(i = 0; i < password_size; i++) cursor[i] = 0;
    cursor = (volatile unsigned char *)input;
    if(cursor)
        for(i = 0; i < input_size; i++) cursor[i] = 0;
}
