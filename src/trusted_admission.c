#include "mtype.h"
#include "trusted_admission.h"

#include <stdlib.h>
#include <string.h>
#include <limits.h>

typedef unsigned int ta_u32;

typedef struct ta_sha256_ctx {
    ta_u32 state[8];
    ta_u32 bit_hi;
    ta_u32 bit_lo;
    unsigned char block[64];
    unsigned int used;
} ta_sha256_ctx;

typedef struct ta_replay_entry {
    char nonce[TRUSTED_ADMISSION_NONCE_LEN + 1];
    long expires_at;
} ta_replay_entry;

static ta_replay_entry replay_cache[TRUSTED_ADMISSION_REPLAY_LIMIT];
static char configured_secret[513];
static int configured_mode = 2; /* 2 means environment not inspected yet. */

static ta_u32 ta_rotr(value, shift)
ta_u32 value;
unsigned int shift;
{
    return (value >> shift) | (value << (32 - shift));
}

static ta_u32 ta_load32(p)
const unsigned char *p;
{
    return ((ta_u32)p[0] << 24) | ((ta_u32)p[1] << 16) |
           ((ta_u32)p[2] << 8) | (ta_u32)p[3];
}

static void ta_store32(p, value)
unsigned char *p;
ta_u32 value;
{
    p[0] = (unsigned char)(value >> 24);
    p[1] = (unsigned char)(value >> 16);
    p[2] = (unsigned char)(value >> 8);
    p[3] = (unsigned char)value;
}

static void ta_sha256_block(ctx, block)
ta_sha256_ctx *ctx;
const unsigned char *block;
{
    static const ta_u32 k[64] = {
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
    ta_u32 w[64], a, b, c, d, e, f, g, h, t1, t2;
    unsigned int i;

    for(i=0; i<16; i++) w[i] = ta_load32(block + 4 * i);
    for(i=16; i<64; i++) {
        ta_u32 s0 = ta_rotr(w[i-15], 7) ^ ta_rotr(w[i-15], 18) ^ (w[i-15] >> 3);
        ta_u32 s1 = ta_rotr(w[i-2], 17) ^ ta_rotr(w[i-2], 19) ^ (w[i-2] >> 10);
        w[i] = w[i-16] + s0 + w[i-7] + s1;
    }
    a=ctx->state[0]; b=ctx->state[1]; c=ctx->state[2]; d=ctx->state[3];
    e=ctx->state[4]; f=ctx->state[5]; g=ctx->state[6]; h=ctx->state[7];
    for(i=0; i<64; i++) {
        ta_u32 s1 = ta_rotr(e, 6) ^ ta_rotr(e, 11) ^ ta_rotr(e, 25);
        ta_u32 ch = (e & f) ^ ((~e) & g);
        ta_u32 s0 = ta_rotr(a, 2) ^ ta_rotr(a, 13) ^ ta_rotr(a, 22);
        ta_u32 maj = (a & b) ^ (a & c) ^ (b & c);
        t1 = h + s1 + ch + k[i] + w[i];
        t2 = s0 + maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    ctx->state[0]+=a; ctx->state[1]+=b; ctx->state[2]+=c; ctx->state[3]+=d;
    ctx->state[4]+=e; ctx->state[5]+=f; ctx->state[6]+=g; ctx->state[7]+=h;
}

static void ta_sha256_init(ctx)
ta_sha256_ctx *ctx;
{
    ctx->state[0]=0x6a09e667U; ctx->state[1]=0xbb67ae85U;
    ctx->state[2]=0x3c6ef372U; ctx->state[3]=0xa54ff53aU;
    ctx->state[4]=0x510e527fU; ctx->state[5]=0x9b05688cU;
    ctx->state[6]=0x1f83d9abU; ctx->state[7]=0x5be0cd19U;
    ctx->bit_hi = ctx->bit_lo = 0;
    ctx->used = 0;
}

static void ta_sha256_add_bits(ctx, bytes)
ta_sha256_ctx *ctx;
unsigned int bytes;
{
    ta_u32 old = ctx->bit_lo;
    ctx->bit_lo += ((ta_u32)bytes << 3);
    if(ctx->bit_lo < old) ctx->bit_hi++;
    ctx->bit_hi += ((ta_u32)bytes >> 29);
}

static void ta_sha256_update(ctx, data, len)
ta_sha256_ctx *ctx;
const unsigned char *data;
unsigned long len;
{
    unsigned int take;
    while(len) {
        take = 64 - ctx->used;
        if(take > len) take = (unsigned int)len;
        memcpy(ctx->block + ctx->used, data, take);
        ctx->used += take;
        data += take;
        len -= take;
        ta_sha256_add_bits(ctx, take);
        if(ctx->used == 64) {
            ta_sha256_block(ctx, ctx->block);
            ctx->used = 0;
        }
    }
}

static void ta_sha256_final(ctx, out)
ta_sha256_ctx *ctx;
unsigned char out[32];
{
    unsigned int i;
    ta_u32 hi = ctx->bit_hi, lo = ctx->bit_lo;
    ctx->block[ctx->used++] = 0x80;
    if(ctx->used > 56) {
        while(ctx->used < 64) ctx->block[ctx->used++] = 0;
        ta_sha256_block(ctx, ctx->block);
        ctx->used = 0;
    }
    while(ctx->used < 56) ctx->block[ctx->used++] = 0;
    ta_store32(ctx->block + 56, hi);
    ta_store32(ctx->block + 60, lo);
    ta_sha256_block(ctx, ctx->block);
    for(i=0; i<8; i++) ta_store32(out + 4*i, ctx->state[i]);
}

static int ta_is_ascii_secret(secret)
const char *secret;
{
    unsigned long i, n;
    if(!secret) return 0;
    n = strlen(secret);
    if(n < 32 || n >= sizeof(configured_secret)) return 0;
    for(i=0; i<n; i++)
        if((unsigned char)secret[i] < 0x20 || (unsigned char)secret[i] > 0x7e)
            return 0;
    return 1;
}

static void ta_configure_environment(void)
{
    const char *secret, *required;
    if(configured_mode != 2) return;
    secret = getenv("MUD_ADMISSION_SECRET");
    required = getenv("MUD_REQUIRE_TRUSTED_ADMISSION");
    if(required && strcmp(required, "1") != 0) {
        configured_mode = -1;
        return;
    }
    if(!secret) {
        configured_mode = required ? -1 : 0;
        return;
    }
    if(!ta_is_ascii_secret(secret)) {
        configured_mode = -1;
        return;
    }
    strcpy(configured_secret, secret);
    configured_mode = 1;
}

int trusted_admission_mode(void)
{
    ta_configure_environment();
    return configured_mode;
}

int trusted_admission_set_secret_for_test(secret)
const char *secret;
{
    memset(replay_cache, 0, sizeof(replay_cache));
    configured_secret[0] = 0;
    if(!secret) {
        configured_mode = 0;
        return 0;
    }
    if(!ta_is_ascii_secret(secret)) {
        configured_mode = -1;
        return -1;
    }
    strcpy(configured_secret, secret);
    configured_mode = 1;
    return 0;
}

void trusted_admission_reset_for_test(void)
{
    memset(replay_cache, 0, sizeof(replay_cache));
    configured_secret[0] = 0;
    configured_mode = 2;
}

int trusted_admission_hmac_hex(secret, signed_part, out)
const char *secret;
const char *signed_part;
char out[TRUSTED_ADMISSION_HMAC_HEX_LEN + 1];
{
    unsigned char key[64], digest[32], ipad[64], opad[64];
    ta_sha256_ctx ctx;
    unsigned long i, key_len;
    static const char hex[] = "0123456789abcdef";

    if(!secret || !signed_part || !out) return -1;
    memset(key, 0, sizeof(key));
    key_len = strlen(secret);
    if(key_len > sizeof(key)) {
        ta_sha256_init(&ctx);
        ta_sha256_update(&ctx, (const unsigned char *)secret, key_len);
        ta_sha256_final(&ctx, digest);
        memcpy(key, digest, sizeof(digest));
    }
    else memcpy(key, secret, key_len);
    for(i=0; i<64; i++) {
        ipad[i] = key[i] ^ 0x36;
        opad[i] = key[i] ^ 0x5c;
    }
    ta_sha256_init(&ctx);
    ta_sha256_update(&ctx, ipad, sizeof(ipad));
    ta_sha256_update(&ctx, (const unsigned char *)signed_part, strlen(signed_part));
    ta_sha256_final(&ctx, digest);
    ta_sha256_init(&ctx);
    ta_sha256_update(&ctx, opad, sizeof(opad));
    ta_sha256_update(&ctx, digest, sizeof(digest));
    ta_sha256_final(&ctx, digest);
    for(i=0; i<sizeof(digest); i++) {
        out[2*i] = hex[digest[i] >> 4];
        out[2*i+1] = hex[digest[i] & 15];
    }
    out[TRUSTED_ADMISSION_HMAC_HEX_LEN] = 0;
    return 0;
}

static int ta_lower_hex(c)
char c;
{
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f');
}

static int ta_strict_uuid(s)
const char *s;
{
    int i;
    if(strlen(s) != TRUSTED_ADMISSION_UUID_LEN) return 0;
    for(i=0; i<TRUSTED_ADMISSION_UUID_LEN; i++) {
        if(i == 8 || i == 13 || i == 18 || i == 23) {
            if(s[i] != '-') return 0;
        }
        else if(!ta_lower_hex(s[i])) return 0;
    }
    return 1;
}

static int ta_decode_name(hex, out)
const char *hex;
char *out;
{
    unsigned long i, n;
    unsigned int hi, lo, value;
    char canonical[PLAYER_NAME_MAX_BYTES + 1];

    n = strlen(hex);
    if(n < 2 || n > 28 || (n & 1)) return 0;
    for(i=0; i<n; i++) if(!ta_lower_hex(hex[i])) return 0;
    for(i=0; i<n; i+=2) {
        hi = (hex[i] <= '9') ? hex[i] - '0' : hex[i] - 'a' + 10;
        lo = (hex[i+1] <= '9') ? hex[i+1] - '0' : hex[i+1] - 'a' + 10;
        value = (hi << 4) | lo;
        /* C strings cannot represent an embedded NUL.  Reject it explicitly
         * so bytes after it are never silently omitted from name validation. */
        if(value == 0) return 0;
        out[i/2] = (char)value;
    }
    out[n/2] = 0;
    if(!player_name_is_valid((const unsigned char *)out,
                             PLAYER_NAME_MIN_CODEPOINTS,
                             PLAYER_NAME_MAX_CODEPOINTS))
        return 0;
    strcpy(canonical, out);
    for(i=0; canonical[i]; i++)
        if(canonical[i] >= 'A' && canonical[i] <= 'Z') canonical[i] += 'a' - 'A';
    if(canonical[0] >= 'a' && canonical[0] <= 'z') canonical[0] -= 'a' - 'A';
    return strcmp(canonical, out) == 0;
}

static int ta_time_value(s, value)
const char *s;
long *value;
{
    unsigned long i, n;
    long result = 0;
    n = strlen(s);
    if(!n) return 0;
    for(i=0; i<n; i++) {
        long digit;
        if(s[i] < '0' || s[i] > '9') return 0;
        digit = s[i] - '0';
        if(result > (LONG_MAX - digit) / 10) return 0;
        result = result * 10 + digit;
    }
    *value = result;
    return 1;
}

static int ta_constant_time_equal(a, b, n)
const char *a;
const char *b;
unsigned long n;
{
    unsigned char different = 0;
    unsigned long i;
    for(i=0; i<n; i++) different |= (unsigned char)(a[i] ^ b[i]);
    return different == 0;
}

static int ta_consume_nonce(nonce, expires_at, now)
const char *nonce;
long expires_at;
long now;
{
    int i, empty = -1;
    for(i=0; i<TRUSTED_ADMISSION_REPLAY_LIMIT; i++) {
        if(replay_cache[i].nonce[0] && replay_cache[i].expires_at < now)
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

int trusted_admission_validate(line, now, ticket)
const char *line;
long now;
trusted_admission_ticket *ticket;
{
    char copy[TRUSTED_ADMISSION_BOUND_MAX_LINE + 1];
    char signed_copy[TRUSTED_ADMISSION_BOUND_MAX_LINE + 1];
    char *part[9], expected[TRUSTED_ADMISSION_HMAC_HEX_LEN + 1];
    trusted_admission_ticket value;
    unsigned long len, signed_len;
    int i, bars = 0, bound, mac;
    long expires_at;

    if(!ticket) return -1;
    memset(ticket,0,sizeof(*ticket)); memset(&value,0,sizeof(value));
    if(!line || trusted_admission_mode() != 1) return -1;
    bound=!strncmp(line,"MUD2|",5); mac=bound?8:6;
    len = strlen(line);
    if(len && line[len-1] == '\n') len--;
    if(!len || len > (unsigned long)(bound?TRUSTED_ADMISSION_BOUND_MAX_LINE:TRUSTED_ADMISSION_MAX_LINE)) return -1;
    memcpy(copy, line, len);
    copy[len] = 0;
    part[0] = copy;
    for(i=0; i<(int)len; i++) {
        if((unsigned char)copy[i] < 0x20 || (unsigned char)copy[i] > 0x7e)
            return -1;
        if(copy[i] == '|') {
            if(++bars > mac) return -1;
            copy[i] = 0;
            part[bars] = copy + i + 1;
        }
    }
    if(bars != mac || strcmp(part[0], bound?"MUD2":"MUD1") || !ta_time_value(part[1], &expires_at))
        return -1;
    if(strlen(part[2]) != TRUSTED_ADMISSION_NONCE_LEN) return -1;
    for(i=0; i<TRUSTED_ADMISSION_NONCE_LEN; i++) if(!ta_lower_hex(part[2][i])) return -1;
    if(!ta_strict_uuid(part[3]) || !ta_strict_uuid(part[4]) || !ta_decode_name(part[5], value.name))
        return -1;
    if(bound) {
        if(!ta_strict_uuid(part[6]) || !part[7][0] || strlen(part[7])>128) return -1;
        for(i=0;part[7][i];i++) {
            char c=part[7][i];
            if(!((c>='a'&&c<='z')||(c>='A'&&c<='Z')||(c>='0'&&c<='9')||c=='-'||c=='_'||c=='.')) return -1;
        }
        strcpy(value.session_id,part[6]); strcpy(value.gateway_instance_id,part[7]);
    }
    if(strlen(part[mac]) != TRUSTED_ADMISSION_HMAC_HEX_LEN) return -1;
    for(i=0; i<TRUSTED_ADMISSION_HMAC_HEX_LEN; i++) if(!ta_lower_hex(part[mac][i])) return -1;
    if(expires_at < now || expires_at > now + 30) return -1;

    signed_len = len - TRUSTED_ADMISSION_HMAC_HEX_LEN - 1;
    memcpy(signed_copy, line, signed_len);
    signed_copy[signed_len] = 0;
    if(trusted_admission_hmac_hex(configured_secret, signed_copy, expected) < 0 ||
       !ta_constant_time_equal(expected, part[mac], TRUSTED_ADMISSION_HMAC_HEX_LEN))
        return -1;
    if(ta_consume_nonce(part[2], expires_at, now) < 0) return -1;
    *ticket=value;
    strcpy(ticket->user_id, part[3]);
    strcpy(ticket->character_id, part[4]);
    strcpy(ticket->nonce, part[2]);
    ticket->expires_at = expires_at;
    return 0;
}
