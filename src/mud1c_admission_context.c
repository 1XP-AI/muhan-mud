#include "mud1c_admission_context.h"

#include <limits.h>
#include <stdio.h>
#include <string.h>

#if MUD1C_ADMISSION_CONTEXT_ENABLED

typedef unsigned int mc_u32;

typedef struct mc_sha256_ctx {
    mc_u32 state[8];
    mc_u32 bit_hi;
    mc_u32 bit_lo;
    unsigned char block[64];
    unsigned int used;
} mc_sha256_ctx;

static mc_u32 mc_rotr(value, shift)
mc_u32 value;
unsigned int shift;
{ return (value >> shift) | (value << (32U - shift)); }

static mc_u32 mc_load32(p)
const unsigned char *p;
{
    return ((mc_u32)p[0] << 24) | ((mc_u32)p[1] << 16) |
        ((mc_u32)p[2] << 8) | (mc_u32)p[3];
}

static void mc_store32(p, value)
unsigned char *p;
mc_u32 value;
{
    p[0] = (unsigned char)(value >> 24); p[1] = (unsigned char)(value >> 16);
    p[2] = (unsigned char)(value >> 8); p[3] = (unsigned char)value;
}

static void mc_sha256_block(ctx, block)
mc_sha256_ctx *ctx;
const unsigned char *block;
{
    static const mc_u32 k[64] = {
        0x428a2f98U,0x71374491U,0xb5c0fbcfU,0xe9b5dba5U,0x3956c25bU,0x59f111f1U,0x923f82a4U,0xab1c5ed5U,
        0xd807aa98U,0x12835b01U,0x243185beU,0x550c7dc3U,0x72be5d74U,0x80deb1feU,0x9bdc06a7U,0xc19bf174U,
        0xe49b69c1U,0xefbe4786U,0x0fc19dc6U,0x240ca1ccU,0x2de92c6fU,0x4a7484aaU,0x5cb0a9dcU,0x76f988daU,
        0x983e5152U,0xa831c66dU,0xb00327c8U,0xbf597fc7U,0xc6e00bf3U,0xd5a79147U,0x06ca6351U,0x14292967U,
        0x27b70a85U,0x2e1b2138U,0x4d2c6dfcU,0x53380d13U,0x650a7354U,0x766a0abbU,0x81c2c92eU,0x92722c85U,
        0xa2bfe8a1U,0xa81a664bU,0xc24b8b70U,0xc76c51a3U,0xd192e819U,0xd6990624U,0xf40e3585U,0x106aa070U,
        0x19a4c116U,0x1e376c08U,0x2748774cU,0x34b0bcb5U,0x391c0cb3U,0x4ed8aa4aU,0x5b9cca4fU,0x682e6ff3U,
        0x748f82eeU,0x78a5636fU,0x84c87814U,0x8cc70208U,0x90befffaU,0xa4506cebU,0xbef9a3f7U,0xc67178f2U
    };
    mc_u32 w[64], a, b, c, d, e, f, g, h, t1, t2;
    unsigned int i;
    for(i=0U; i<16U; ++i) w[i] = mc_load32(block + 4U * i);
    for(i=16U; i<64U; ++i) {
        mc_u32 s0 = mc_rotr(w[i-15U], 7U) ^ mc_rotr(w[i-15U], 18U) ^ (w[i-15U] >> 3);
        mc_u32 s1 = mc_rotr(w[i-2U], 17U) ^ mc_rotr(w[i-2U], 19U) ^ (w[i-2U] >> 10);
        w[i] = w[i-16U] + s0 + w[i-7U] + s1;
    }
    a=ctx->state[0]; b=ctx->state[1]; c=ctx->state[2]; d=ctx->state[3];
    e=ctx->state[4]; f=ctx->state[5]; g=ctx->state[6]; h=ctx->state[7];
    for(i=0U; i<64U; ++i) {
        mc_u32 s1 = mc_rotr(e, 6U) ^ mc_rotr(e, 11U) ^ mc_rotr(e, 25U);
        mc_u32 ch = (e & f) ^ ((~e) & g);
        mc_u32 s0 = mc_rotr(a, 2U) ^ mc_rotr(a, 13U) ^ mc_rotr(a, 22U);
        mc_u32 maj = (a & b) ^ (a & c) ^ (b & c);
        t1 = h + s1 + ch + k[i] + w[i]; t2 = s0 + maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    ctx->state[0]+=a; ctx->state[1]+=b; ctx->state[2]+=c; ctx->state[3]+=d;
    ctx->state[4]+=e; ctx->state[5]+=f; ctx->state[6]+=g; ctx->state[7]+=h;
}

static void mc_sha256_init(ctx)
mc_sha256_ctx *ctx;
{
    ctx->state[0]=0x6a09e667U; ctx->state[1]=0xbb67ae85U; ctx->state[2]=0x3c6ef372U; ctx->state[3]=0xa54ff53aU;
    ctx->state[4]=0x510e527fU; ctx->state[5]=0x9b05688cU; ctx->state[6]=0x1f83d9abU; ctx->state[7]=0x5be0cd19U;
    ctx->bit_hi = ctx->bit_lo = 0U; ctx->used = 0U;
}

static void mc_add_bits(ctx, bytes)
mc_sha256_ctx *ctx;
unsigned int bytes;
{
    mc_u32 old = ctx->bit_lo;
    ctx->bit_lo += ((mc_u32)bytes << 3);
    if(ctx->bit_lo < old) ++ctx->bit_hi;
    ctx->bit_hi += ((mc_u32)bytes >> 29);
}

static void mc_sha256_update(ctx, data, length)
mc_sha256_ctx *ctx;
const unsigned char *data;
size_t length;
{
    unsigned int take;
    while(length) {
        take = 64U - ctx->used;
        if((size_t)take > length) take = (unsigned int)length;
        memcpy(ctx->block + ctx->used, data, take);
        ctx->used += take; data += take; length -= take; mc_add_bits(ctx, take);
        if(ctx->used == 64U) { mc_sha256_block(ctx, ctx->block); ctx->used = 0U; }
    }
}

static void mc_sha256_final(ctx, out)
mc_sha256_ctx *ctx;
unsigned char out[32];
{
    unsigned int i;
    mc_u32 hi = ctx->bit_hi, lo = ctx->bit_lo;
    ctx->block[ctx->used++] = 0x80U;
    if(ctx->used > 56U) {
        while(ctx->used < 64U) ctx->block[ctx->used++] = 0U;
        mc_sha256_block(ctx, ctx->block); ctx->used = 0U;
    }
    while(ctx->used < 56U) ctx->block[ctx->used++] = 0U;
    mc_store32(ctx->block + 56U, hi); mc_store32(ctx->block + 60U, lo);
    mc_sha256_block(ctx, ctx->block);
    for(i=0U; i<8U; ++i) mc_store32(out + 4U * i, ctx->state[i]);
}

static size_t mc_bounded(value, maximum)
const char *value;
size_t maximum;
{
    size_t index;
    if(!value) return maximum + 1U;
    for(index=0U; index<=maximum; ++index) if(!value[index]) return index;
    return maximum + 1U;
}

static int mc_hex(value)
unsigned char value;
{ return (value >= '0' && value <= '9') || (value >= 'a' && value <= 'f'); }

static int mc_world(value)
const char *value;
{
    size_t index, length = mc_bounded(value, MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX);
    if(!length || length > MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX || value[0] < 'a' || value[0] > 'z') return 0;
    for(index=1U; index<length; ++index)
        if(!((value[index] >= 'a' && value[index] <= 'z') || (value[index] >= '0' && value[index] <= '9') || value[index] == '_' || value[index] == '-')) return 0;
    return 1;
}

static int mc_uuid(value)
const char *value;
{
    size_t index;
    if(mc_bounded(value, MUD1C_ADMISSION_CONTEXT_UUID_LEN) != MUD1C_ADMISSION_CONTEXT_UUID_LEN) return 0;
    for(index=0U; index<MUD1C_ADMISSION_CONTEXT_UUID_LEN; ++index)
        if(index == 8U || index == 13U || index == 18U || index == 23U) { if(value[index] != '-') return 0; }
        else if(!mc_hex((unsigned char)value[index])) return 0;
    return 1;
}

static int mc_key(value)
const char *value;
{
    size_t index, length = mc_bounded(value, MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX);
    if(length < 2U || length > MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX || (length & 1U)) return 0;
    for(index=0U; index<length; ++index) if(!mc_hex((unsigned char)value[index])) return 0;
    return 1;
}

static int mc_context(context)
const mud1c_admission_context *context;
{ return context && mc_world(context->world_id) && mc_uuid(context->actor_id) && mc_uuid(context->character_id) && mc_key(context->canonical_legacy_name_key) && context->expires_at >= 0L; }

static int mc_secret(secret)
const char *secret;
{
    size_t index, length = mc_bounded(secret, 512U);
    if(length < 32U || length > 512U) return 0;
    for(index=0U; index<length; ++index) if((unsigned char)secret[index] < 0x20U || (unsigned char)secret[index] > 0x7eU) return 0;
    return 1;
}

static void mc_hex_encode(bytes, length, output)
const unsigned char *bytes;
size_t length;
char *output;
{
    static const char hex[] = "0123456789abcdef";
    size_t index;
    for(index=0U; index<length; ++index) { output[2U * index] = hex[bytes[index] >> 4]; output[2U * index + 1U] = hex[bytes[index] & 15U]; }
    output[2U * length] = 0;
}

static int mc_hex_decode(value, length, output)
const unsigned char *value;
size_t length;
unsigned char *output;
{
    size_t index;
    unsigned int high, low;
    if(length & 1U) return 0;
    for(index=0U; index<length; index+=2U) {
        if(!mc_hex(value[index]) || !mc_hex(value[index + 1U])) return 0;
        high = value[index] <= '9' ? value[index] - '0' : value[index] - 'a' + 10U;
        low = value[index + 1U] <= '9' ? value[index + 1U] - '0' : value[index + 1U] - 'a' + 10U;
        output[index / 2U] = (unsigned char)((high << 4) | low);
    }
    return 1;
}

static int mc_time(value, length, parsed)
const unsigned char *value;
size_t length;
long *parsed;
{
    size_t index;
    long result = 0L, digit;
    if(!length || length > 19U || !parsed) return 0;
    for(index=0U; index<length; ++index) {
        if(value[index] < '0' || value[index] > '9') return 0;
        digit = (long)(value[index] - '0');
        if(result > (LONG_MAX - digit) / 10L) return 0;
        result = result * 10L + digit;
    }
    *parsed = result;
    return 1;
}

static int mc_constant_time_equal(left, right, length)
const char *left;
const char *right;
size_t length;
{
    unsigned char different = 0U;
    size_t index;
    for(index=0U; index<length; ++index) different |= (unsigned char)(left[index] ^ right[index]);
    return different == 0U;
}

static int mc_window(expires_at, now)
long expires_at;
long now;
{
    if(expires_at < now) return 0;
    if(now > LONG_MAX - MUD1C_ADMISSION_CONTEXT_MAX_TTL_SECONDS) return expires_at == now;
    return expires_at <= now + MUD1C_ADMISSION_CONTEXT_MAX_TTL_SECONDS;
}

static size_t mc_decimal(value, output)
long value;
unsigned char *output;
{
    unsigned char reversed[20];
    size_t length = 0U, index;
    do {
        reversed[length++] = (unsigned char)('0' + (value % 10L));
        value /= 10L;
    } while(value);
    if(output) for(index=0U; index<length; ++index) output[index] = reversed[length - index - 1U];
    return length;
}

static int mc_signed(context, output, output_size, written)
const mud1c_admission_context *context;
unsigned char *output;
size_t output_size;
size_t *written;
{
    char nonce_hex[MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN + 1U];
    size_t world_length, key_length, time_length, total, offset;
    if(!mc_context(context) || !output || !written) return 0;
    mc_hex_encode(context->nonce, sizeof(context->nonce), nonce_hex);
    world_length = strlen(context->world_id);
    key_length = strlen(context->canonical_legacy_name_key);
    time_length = mc_decimal(context->expires_at, NULL);
    total = 5U + world_length + MUD1C_ADMISSION_CONTEXT_UUID_LEN * 2U +
        key_length + time_length + MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN + 6U;
    if(total >= output_size || total > MUD1C_ADMISSION_CONTEXT_MAX_WIRE) return 0;
    offset = 0U;
    memcpy(output + offset, "MUD1C", 5U); offset += 5U;
    output[offset++] = '|'; memcpy(output + offset, context->world_id, world_length); offset += world_length;
    output[offset++] = '|'; memcpy(output + offset, context->actor_id, MUD1C_ADMISSION_CONTEXT_UUID_LEN); offset += MUD1C_ADMISSION_CONTEXT_UUID_LEN;
    output[offset++] = '|'; memcpy(output + offset, context->character_id, MUD1C_ADMISSION_CONTEXT_UUID_LEN); offset += MUD1C_ADMISSION_CONTEXT_UUID_LEN;
    output[offset++] = '|'; memcpy(output + offset, context->canonical_legacy_name_key, key_length); offset += key_length;
    output[offset++] = '|'; offset += mc_decimal(context->expires_at, output + offset);
    output[offset++] = '|'; memcpy(output + offset, nonce_hex, MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN); offset += MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN;
    output[offset] = 0;
    *written = offset;
    return 1;
}

#endif

int mud1c_admission_context_enabled(void)
{ return MUD1C_ADMISSION_CONTEXT_ENABLED ? 1 : 0; }

int mud1c_admission_context_hmac_sha256_hex(secret, signed_bytes, signed_length, output)
const char *secret;
const unsigned char *signed_bytes;
size_t signed_length;
char output[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
{
#if MUD1C_ADMISSION_CONTEXT_ENABLED
    unsigned char key[64], digest[32], ipad[64], opad[64];
    mc_sha256_ctx ctx;
    size_t index, key_length;
    if(output) output[0] = 0;
    if(!output || !signed_bytes || !secret) return MUD1C_ADMISSION_CONTEXT_INVALID;
    key_length = mc_bounded(secret, 512U);
    if(key_length > 512U) return MUD1C_ADMISSION_CONTEXT_INVALID;
    memset(key, 0, sizeof(key));
    if(key_length > sizeof(key)) { mc_sha256_init(&ctx); mc_sha256_update(&ctx, (const unsigned char *)secret, key_length); mc_sha256_final(&ctx, digest); memcpy(key, digest, sizeof(digest)); }
    else memcpy(key, secret, key_length);
    for(index=0U; index<sizeof(key); ++index) { ipad[index] = key[index] ^ 0x36U; opad[index] = key[index] ^ 0x5cU; }
    mc_sha256_init(&ctx); mc_sha256_update(&ctx, ipad, sizeof(ipad)); mc_sha256_update(&ctx, signed_bytes, signed_length); mc_sha256_final(&ctx, digest);
    mc_sha256_init(&ctx); mc_sha256_update(&ctx, opad, sizeof(opad)); mc_sha256_update(&ctx, digest, sizeof(digest)); mc_sha256_final(&ctx, digest);
    mc_hex_encode(digest, sizeof(digest), output);
    memset(key, 0, sizeof(key)); memset(digest, 0, sizeof(digest)); memset(ipad, 0, sizeof(ipad)); memset(opad, 0, sizeof(opad));
    return MUD1C_ADMISSION_CONTEXT_OK;
#else
    (void)secret; (void)signed_bytes; (void)signed_length;
    if(output) output[0] = 0;
    return MUD1C_ADMISSION_CONTEXT_DISABLED;
#endif
}

int mud1c_admission_context_parse(wire, wire_length, context, mac_hex)
const unsigned char *wire;
size_t wire_length;
mud1c_admission_context *context;
char mac_hex[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
{
#if MUD1C_ADMISSION_CONTEXT_ENABLED
    size_t field_start[8], field_length[8], index, field = 0U;
    mud1c_admission_context candidate;
    if(context) memset(context, 0, sizeof(*context));
    if(mac_hex) memset(mac_hex, 0, MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U);
    if(!wire || !context || !mac_hex || !wire_length || wire_length > MUD1C_ADMISSION_CONTEXT_MAX_WIRE) return MUD1C_ADMISSION_CONTEXT_INVALID;
    field_start[0] = 0U;
    for(index=0U; index<wire_length; ++index) {
        if(wire[index] < 0x20U || wire[index] > 0x7eU) return MUD1C_ADMISSION_CONTEXT_INVALID;
        if(wire[index] == '|') { if(field == 7U) return MUD1C_ADMISSION_CONTEXT_INVALID; field_length[field] = index - field_start[field]; ++field; field_start[field] = index + 1U; }
    }
    if(field != 7U) return MUD1C_ADMISSION_CONTEXT_INVALID;
    field_length[field] = wire_length - field_start[field];
    if(field_length[0] != 5U || memcmp(wire + field_start[0], "MUD1C", 5U) || field_length[1] > MUD1C_ADMISSION_CONTEXT_WORLD_ID_MAX || field_length[2] != MUD1C_ADMISSION_CONTEXT_UUID_LEN || field_length[3] != MUD1C_ADMISSION_CONTEXT_UUID_LEN || field_length[4] > MUD1C_ADMISSION_CONTEXT_LEGACY_NAME_KEY_MAX || field_length[6] != MUD1C_ADMISSION_CONTEXT_NONCE_HEX_LEN || field_length[7] != MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN) return MUD1C_ADMISSION_CONTEXT_INVALID;
    memset(&candidate, 0, sizeof(candidate));
    memcpy(candidate.world_id, wire + field_start[1], field_length[1]);
    memcpy(candidate.actor_id, wire + field_start[2], field_length[2]);
    memcpy(candidate.character_id, wire + field_start[3], field_length[3]);
    memcpy(candidate.canonical_legacy_name_key, wire + field_start[4], field_length[4]);
    if(!mc_time(wire + field_start[5], field_length[5], &candidate.expires_at) || !mc_hex_decode(wire + field_start[6], field_length[6], candidate.nonce) || !mc_context(&candidate)) return MUD1C_ADMISSION_CONTEXT_INVALID;
    for(index=0U; index<field_length[7]; ++index) if(!mc_hex(wire[field_start[7] + index])) return MUD1C_ADMISSION_CONTEXT_INVALID;
    memcpy(mac_hex, wire + field_start[7], field_length[7]);
    *context = candidate;
    return MUD1C_ADMISSION_CONTEXT_OK;
#else
    (void)wire; (void)wire_length;
    if(context) memset(context, 0, sizeof(*context));
    if(mac_hex) mac_hex[0] = 0;
    return MUD1C_ADMISSION_CONTEXT_DISABLED;
#endif
}

int mud1c_admission_context_format(context, secret, output, output_size, written)
const mud1c_admission_context *context;
const char *secret;
unsigned char *output;
size_t output_size;
size_t *written;
{
#if MUD1C_ADMISSION_CONTEXT_ENABLED
    char mac[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    size_t signed_length;
    if(written) *written = 0U;
    if(output && output_size) output[0] = 0;
    if(!output || !output_size || !written || !mc_secret(secret) || !mc_signed(context, output, output_size, &signed_length)) return MUD1C_ADMISSION_CONTEXT_INVALID;
    if(signed_length + 1U + MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN >= output_size || signed_length + 1U + MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN > MUD1C_ADMISSION_CONTEXT_MAX_WIRE || mud1c_admission_context_hmac_sha256_hex(secret, output, signed_length, mac) != MUD1C_ADMISSION_CONTEXT_OK) { output[0] = 0; return MUD1C_ADMISSION_CONTEXT_INVALID; }
    output[signed_length] = '|'; memcpy(output + signed_length + 1U, mac, MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN); output[signed_length + 1U + MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN] = 0;
    *written = signed_length + 1U + MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN;
    return MUD1C_ADMISSION_CONTEXT_OK;
#else
    (void)context; (void)secret;
    if(output && output_size) output[0] = 0;
    if(written) *written = 0U;
    return MUD1C_ADMISSION_CONTEXT_DISABLED;
#endif
}

int mud1c_admission_context_validate(wire, wire_length, secret, now, context)
const unsigned char *wire;
size_t wire_length;
const char *secret;
long now;
mud1c_admission_context *context;
{
#if MUD1C_ADMISSION_CONTEXT_ENABLED
    char supplied[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    char expected[MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN + 1U];
    mud1c_admission_context candidate;
    size_t signed_length;
    if(context) memset(context, 0, sizeof(*context));
    if(!context || !mc_secret(secret) || mud1c_admission_context_parse(wire, wire_length, &candidate, supplied) != MUD1C_ADMISSION_CONTEXT_OK || !mc_window(candidate.expires_at, now)) return MUD1C_ADMISSION_CONTEXT_INVALID;
    signed_length = wire_length - MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN - 1U;
    if(mud1c_admission_context_hmac_sha256_hex(secret, wire, signed_length, expected) != MUD1C_ADMISSION_CONTEXT_OK || !mc_constant_time_equal(expected, supplied, MUD1C_ADMISSION_CONTEXT_HMAC_HEX_LEN)) return MUD1C_ADMISSION_CONTEXT_INVALID;
    *context = candidate;
    return MUD1C_ADMISSION_CONTEXT_OK;
#else
    (void)wire; (void)wire_length; (void)secret; (void)now;
    if(context) memset(context, 0, sizeof(*context));
    return MUD1C_ADMISSION_CONTEXT_DISABLED;
#endif
}
